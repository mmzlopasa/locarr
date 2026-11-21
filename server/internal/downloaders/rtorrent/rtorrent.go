// Package rtorrent talks to rTorrent over XML-RPC as exposed over HTTP (an
// SCGI mount such as /RPC2 behind a web server or a frontend). It follows the
// public rTorrent XML-RPC command reference.
package rtorrent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

func init() {
	downloaders.Register(downloaders.Implementation{
		Name:           "rtorrent",
		DisplayName:    "rTorrent",
		Protocol:       downloaders.Torrent,
		DefaultPort:    8000,
		DefaultURLBase: "/RPC2",
		Fields: []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldUseTLS, downloaders.FieldURLBase,
			downloaders.FieldUsername, downloaders.FieldPassword, downloaders.FieldDirectory, downloaders.FieldAllowSelfSigned},
		Required:     []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldURLBase},
		Capabilities: caps,
		New:          func(c downloaders.Config) (downloaders.Client, error) { return New(c) },
	})
}

var caps = downloaders.Capabilities{
	Protocol:   downloaders.Torrent,
	Categories: true, // d.custom1, free-form: nothing to create
	Priority:   true,
	SavePath:   true,
}

// maxTorrentFetch caps a .torrent fetched for a TorrentURL.
const maxTorrentFetch = 10 << 20

// Client is an rTorrent client.
type Client struct {
	cfg      downloaders.Config
	endpoint string
	hc       *http.Client
}

var _ downloaders.Client = (*Client)(nil)

// New returns a client for cfg. The URL base is the XML-RPC endpoint path (usually /RPC2).
func New(cfg downloaders.Config) (*Client, error) {
	if strings.TrimSpace(cfg.URLBase) == "" {
		cfg.URLBase = "/RPC2"
	}
	u, err := cfg.BaseURL()
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, endpoint: u.String(), hc: cfg.Client()}, nil
}

// Capabilities implements downloaders.Client.
func (c *Client) Capabilities() downloaders.Capabilities { return caps }

// call performs one XML-RPC call.
func (c *Client) call(ctx context.Context, method string, params ...any) (any, error) {
	body, err := encodeCall(method, params...)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml")
	if c.cfg.Username != "" || c.cfg.Password != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, downloaders.ClassifyTransportError(err)
	}
	defer resp.Body.Close()
	data, err := downloaders.ReadLimited(resp.Body, downloaders.MaxResponse)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, downloaders.StatusError(resp.StatusCode)
	}
	v, err := decodeResponse(data)
	if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			return nil, mapFault(f)
		}
		return nil, fmt.Errorf("%w: %w", downloaders.ErrWrongURL, err)
	}
	return v, nil
}

func mapFault(f *Fault) error {
	msg := strings.ToLower(f.Message)
	if strings.Contains(msg, "could not find info-hash") || strings.Contains(msg, "info-hash not found") {
		return fmt.Errorf("%w: %s", downloaders.ErrItemNotFound, f.Message)
	}
	return downloaders.ClientError("%s", f.Error())
}

// Test implements downloaders.Client.
func (c *Client) Test(ctx context.Context) (downloaders.Status, error) {
	v, err := c.call(ctx, "system.client_version")
	if err != nil {
		return downloaders.Status{}, err
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return downloaders.Status{}, fmt.Errorf("%w: unexpected version answer", downloaders.ErrWrongURL)
	}
	return downloaders.Status{Version: s}, nil
}

func priorityValue(p downloaders.Priority) int {
	switch p {
	case downloaders.PriorityTop:
		return 3 // high
	case downloaders.PriorityLast:
		return 1 // low
	}
	return 2 // normal
}

// Add implements downloaders.Client.
func (c *Client) Add(ctx context.Context, req downloaders.AddRequest) (string, error) {
	if err := req.Validate(downloaders.Torrent); err != nil {
		return "", err
	}
	var cmds []any
	if req.Category != "" {
		cmds = append(cmds, "d.custom1.set="+escapeCmd(req.Category))
	}
	dir := req.SavePath
	if dir == "" {
		dir = c.cfg.Directory
	}
	if dir != "" {
		cmds = append(cmds, "d.directory.set="+escapeCmd(dir))
	}
	if req.Priority != "" && req.Priority != downloaders.PriorityNormal {
		cmds = append(cmds, "d.priority.set="+strconv.Itoa(priorityValue(req.Priority)))
	}
	cmds = append(cmds, "d.custom.set=addtime,"+strconv.FormatInt(time.Now().Unix(), 10))

	if req.TorrentURL != "" {
		b, err := fetchTorrent(ctx, c.hc, req.TorrentURL, maxTorrentFetch)
		if err != nil {
			return "", err
		}
		req.TorrentFile = b
	}
	if req.Magnet != "" {
		m, err := downloaders.ParseMagnet(req.Magnet)
		if err != nil {
			return "", err
		}
		method := "load.start"
		if req.Paused {
			method = "load.normal"
		}
		params := append([]any{"", req.Magnet}, cmds...)
		if _, err := c.call(ctx, method, params...); err != nil {
			return "", err
		}
		return m.InfoHash, nil
	}
	ti, err := downloaders.ParseTorrent(req.TorrentFile)
	if err != nil {
		return "", err
	}
	method := "load.raw_start"
	if req.Paused {
		method = "load.raw"
	}
	params := append([]any{"", req.TorrentFile}, cmds...)
	if _, err := c.call(ctx, method, params...); err != nil {
		return "", err
	}
	return ti.InfoHash, nil
}

// escapeCmd keeps a value from breaking out of an rTorrent command argument.
func escapeCmd(s string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
	if strings.ContainsAny(s, ",;{}\"\\ ") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return s
}

// listFields are the d.multicall2 columns, in order.
var listFields = []string{
	"d.hash=", "d.name=", "d.custom1=", "d.size_bytes=", "d.completed_bytes=", "d.left_bytes=",
	"d.down.rate=", "d.up.rate=", "d.ratio=", "d.state=", "d.is_active=", "d.is_open=", "d.complete=",
	"d.hashing=", "d.message=", "d.directory=", "d.is_multi_file=", "d.up.total=", "d.peers_complete=",
	"d.custom=addtime", "d.timestamp.started=", "d.timestamp.finished=", "d.priority=", "d.base_path=",
}

const (
	colHash = iota
	colName
	colCategory
	colSize
	colCompleted
	colLeft
	colDown
	colUp
	colRatio
	colState
	colActive
	colOpen
	colComplete
	colHashing
	colMessage
	colDirectory
	colMulti
	colUpTotal
	colSeeds
	colAddTime
	colStarted
	colFinished
	colPriority
	colBasePath
)

// List implements downloaders.Client.
func (c *Client) List(ctx context.Context, category string) ([]downloaders.Item, error) {
	params := []any{"", "main"}
	for _, f := range listFields {
		params = append(params, f)
	}
	v, err := c.call(ctx, "d.multicall2", params...)
	if err != nil {
		return nil, err
	}
	rows, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: unexpected multicall answer", downloaders.ErrWrongURL)
	}
	now := time.Now()
	out := make([]downloaders.Item, 0, len(rows))
	for _, r := range rows {
		row, ok := r.([]any)
		if !ok || len(row) < len(listFields) {
			return nil, downloaders.ClientError("unexpected multicall row")
		}
		it := toItem(row, now)
		if category != "" && !strings.EqualFold(it.Category, category) {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return ""
}

func num(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case bool:
		if x {
			return 1
		}
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	}
	return 0
}

func toItem(row []any, now time.Time) downloaders.Item {
	it := downloaders.Item{
		ID:            downloaders.NormaliseHash(str(row[colHash])),
		Name:          str(row[colName]),
		Category:      str(row[colCategory]),
		Size:          num(row[colSize]),
		Downloaded:    num(row[colCompleted]),
		Uploaded:      num(row[colUpTotal]),
		DownloadSpeed: num(row[colDown]),
		UploadSpeed:   num(row[colUp]),
		Ratio:         float64(num(row[colRatio])) / 1000,
		Message:       strings.TrimSpace(str(row[colMessage])),
		Seeds:         int(num(row[colSeeds])),
	}
	left := num(row[colLeft])
	if it.Size > 0 {
		it.Progress = float64(it.Size-left) / float64(it.Size)
		it.Progress = max(0, min(1, it.Progress))
	}
	started := num(row[colState]) == 1
	active := num(row[colActive]) == 1
	complete := num(row[colComplete]) == 1
	hashing := num(row[colHashing]) != 0
	if complete {
		it.Progress = 1
	}

	dir := str(row[colDirectory])
	if base := str(row[colBasePath]); base != "" {
		it.ContentPath = base
	} else if num(row[colMulti]) == 1 {
		it.ContentPath = dir
	} else if dir != "" {
		it.ContentPath = path.Join(dir, it.Name)
	}
	if num(row[colMulti]) == 1 {
		it.SavePath = path.Dir(dir)
	} else {
		it.SavePath = dir
	}

	if t := num(row[colAddTime]); t > 0 {
		it.AddedAt = time.Unix(t, 0).UTC()
	} else if t := num(row[colStarted]); t > 0 {
		it.AddedAt = time.Unix(t, 0).UTC()
	}
	if f := num(row[colFinished]); f > 0 && complete {
		it.SeedingTime = now.Sub(time.Unix(f, 0)).Truncate(time.Second)
		if it.SeedingTime < 0 {
			it.SeedingTime = 0
		}
	}
	if !complete && it.DownloadSpeed > 0 && left > 0 {
		eta := time.Duration(left/it.DownloadSpeed) * time.Second
		it.ETA = &eta
	}

	// A tracker message ("Tracker: [...]") is informational; any other message
	// on a stopped item is a problem the user should see.
	problem := it.Message != "" && !strings.HasPrefix(strings.ToLower(it.Message), "tracker:")
	switch {
	case hashing:
		it.RawState, it.Busy = "hashing", true
		it.State = downloaders.StateQueued
	case !started && problem:
		it.RawState, it.State = "stopped", downloaders.StateWarning
	case !started && complete:
		it.RawState, it.State = "stopped", downloaders.StateCompleted
	case !started:
		it.RawState, it.State = "stopped", downloaders.StatePaused
	case !active:
		it.RawState, it.State = "paused", downloaders.StatePaused
	case complete:
		it.RawState, it.State = "seeding", downloaders.StateSeeding
	default:
		it.RawState, it.State = "downloading", downloaders.StateDownloading
	}
	if !started && !active && num(row[colOpen]) == 0 {
		it.RawState = "closed"
	}
	return it
}

// Files implements downloaders.Client.
func (c *Client) Files(ctx context.Context, id string) ([]downloaders.File, error) {
	v, err := c.call(ctx, "f.multicall", strings.ToUpper(id), "", "f.path=", "f.size_bytes=", "f.completed_chunks=", "f.size_chunks=")
	if err != nil {
		return nil, err
	}
	rows, ok := v.([]any)
	if !ok {
		return nil, downloaders.ClientError("unexpected file list")
	}
	out := make([]downloaders.File, 0, len(rows))
	for _, r := range rows {
		row, ok := r.([]any)
		if !ok || len(row) < 4 {
			return nil, downloaders.ClientError("unexpected file row")
		}
		f := downloaders.File{Path: str(row[0]), Size: num(row[1])}
		if total := num(row[3]); total > 0 {
			f.Progress = float64(num(row[2])) / float64(total)
		}
		out = append(out, f)
	}
	return out, nil
}

// Remove implements downloaders.Client. rTorrent cannot delete files over
// XML-RPC (Capabilities.DeletesData is false): the caller deletes them.
func (c *Client) Remove(ctx context.Context, id string, _ bool) error {
	_, err := c.call(ctx, "d.erase", strings.ToUpper(id))
	return err
}

// Pause implements downloaders.Client.
func (c *Client) Pause(ctx context.Context, id string) error {
	_, err := c.call(ctx, "d.stop", strings.ToUpper(id))
	return err
}

// Resume implements downloaders.Client.
func (c *Client) Resume(ctx context.Context, id string) error {
	h := strings.ToUpper(id)
	if _, err := c.call(ctx, "d.open", h); err != nil {
		return err
	}
	_, err := c.call(ctx, "d.start", h)
	return err
}

// SetPriority implements downloaders.Client (rTorrent download priority: low, normal, high).
func (c *Client) SetPriority(ctx context.Context, id string, p downloaders.Priority) error {
	if !p.Valid() {
		return fmt.Errorf("%w: unknown priority %q", downloaders.ErrInvalidRequest, p)
	}
	_, err := c.call(ctx, "d.priority.set", strings.ToUpper(id), priorityValue(p))
	return err
}

// Categories implements downloaders.Client: the d.custom1 values in use.
func (c *Client) Categories(ctx context.Context) ([]downloaders.Category, error) {
	v, err := c.call(ctx, "d.multicall2", "", "main", "d.custom1=")
	if err != nil {
		return nil, err
	}
	rows, _ := v.([]any)
	seen := map[string]bool{}
	var out []downloaders.Category
	for _, r := range rows {
		row, ok := r.([]any)
		if !ok || len(row) == 0 {
			continue
		}
		name := str(row[0])
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, downloaders.Category{Name: name})
	}
	return out, nil
}

// EnsureCategory implements downloaders.Client: rTorrent labels are free-form,
// so there is nothing to create.
func (c *Client) EnsureCategory(context.Context, string, string) error { return nil }
