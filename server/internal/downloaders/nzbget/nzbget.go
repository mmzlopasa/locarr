// Package nzbget is the NZBGet client, written from NZBGet's public API
// documentation: JSON-RPC over HTTP at <base>/jsonrpc with HTTP basic auth.
package nzbget

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

// Name is the implementation identifier.
const Name = "nzbget"

var caps = downloaders.Capabilities{
	Protocol:   downloaders.Usenet,
	Categories: true,
	Priority:   true,
	FreeSpace:  true,
	// Deleting a finished job only removes its history record; the caller
	// deletes the files itself.
	DeletesData: false,
}

func init() {
	downloaders.Register(downloaders.Implementation{
		Name:        Name,
		DisplayName: "NZBGet",
		Protocol:    downloaders.Usenet,
		DefaultPort: 6789,
		DefaultUser: "nzbget",
		Fields: []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldUseTLS,
			downloaders.FieldURLBase, downloaders.FieldUsername, downloaders.FieldPassword, downloaders.FieldAllowSelfSigned},
		Required:     []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort},
		Capabilities: caps,
		New:          func(c downloaders.Config) (downloaders.Client, error) { return New(c) },
	})
}

// NZBGet priorities.
const (
	prioVeryLow  = -100
	prioNormal   = 0
	prioVeryHigh = 100
)

// Client talks to one NZBGet instance.
type Client struct {
	endpoint string
	user     string
	pass     string
	http     *http.Client
	seq      atomic.Int64
}

// New returns a client for cfg.
func New(cfg downloaders.Config) (*Client, error) {
	base, err := cfg.BaseURL()
	if err != nil {
		return nil, err
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/jsonrpc"
	return &Client{endpoint: base.String(), user: cfg.Username, pass: cfg.Password, http: cfg.Client()}, nil
}

// Capabilities implements downloaders.Client.
func (c *Client) Capabilities() downloaders.Capabilities { return caps }

type rpcError struct {
	Name    string `json:"name"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// call runs one JSON-RPC method and decodes its result into out.
func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"method": method, "params": params, "id": c.seq.Add(1)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: cannot build request", downloaders.ErrInvalidRequest)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" || c.pass != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return downloaders.ClassifyTransportError(err)
	}
	defer resp.Body.Close()
	raw, err := downloaders.ReadLimited(resp.Body, downloaders.MaxResponse)
	if err != nil {
		if errors.Is(err, downloaders.ErrResponseTooLarge) {
			return err
		}
		return downloaders.ClassifyTransportError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return downloaders.StatusError(resp.StatusCode)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%w: the answer is not NZBGet JSON-RPC", downloaders.ErrWrongURL)
	}
	if env.Error != nil {
		msg := env.Error.Message
		if msg == "" {
			msg = env.Error.Name
		}
		if strings.Contains(strings.ToLower(msg), "access denied") {
			return fmt.Errorf("%w: %s", downloaders.ErrAuth, msg)
		}
		return downloaders.ClientError("%s: %s", method, msg)
	}
	if env.Result == nil {
		return fmt.Errorf("%w: the answer is not NZBGet JSON-RPC", downloaders.ErrWrongURL)
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("%w: unexpected answer to %s", downloaders.ErrClient, method)
		}
	}
	return nil
}

type statusInfo struct {
	DownloadRate    int64 `json:"DownloadRate"`
	FreeDiskSpaceLo int64 `json:"FreeDiskSpaceLo"`
	FreeDiskSpaceHi int64 `json:"FreeDiskSpaceHi"`
	FreeDiskSpaceMB int64 `json:"FreeDiskSpaceMB"`
}

func combine(lo, hi int64) int64 { return hi<<32 | (lo & 0xffffffff) }

// Test implements downloaders.Client.
func (c *Client) Test(ctx context.Context) (downloaders.Status, error) {
	var version string
	if err := c.call(ctx, "version", nil, &version); err != nil {
		return downloaders.Status{}, err
	}
	if version == "" {
		return downloaders.Status{}, fmt.Errorf("%w: no NZBGet version in the answer", downloaders.ErrWrongURL)
	}
	st := downloaders.Status{Version: version}
	var s statusInfo
	if err := c.call(ctx, "status", nil, &s); err != nil {
		return downloaders.Status{}, err
	}
	free := combine(s.FreeDiskSpaceLo, s.FreeDiskSpaceHi)
	if free == 0 && s.FreeDiskSpaceMB > 0 {
		free = s.FreeDiskSpaceMB << 20
	}
	st.FreeSpace = &free
	return st, nil
}

type group struct {
	NZBID             int64  `json:"NZBID"`
	NZBName           string `json:"NZBName"`
	Category          string `json:"Category"`
	Status            string `json:"Status"`
	DestDir           string `json:"DestDir"`
	FinalDir          string `json:"FinalDir"`
	FileSizeLo        int64  `json:"FileSizeLo"`
	FileSizeHi        int64  `json:"FileSizeHi"`
	RemainingSizeLo   int64  `json:"RemainingSizeLo"`
	RemainingSizeHi   int64  `json:"RemainingSizeHi"`
	PausedSizeLo      int64  `json:"PausedSizeLo"`
	PausedSizeHi      int64  `json:"PausedSizeHi"`
	ActiveDownloads   int    `json:"ActiveDownloads"`
	MaxPriority       int    `json:"MaxPriority"`
	PostInfoText      string `json:"PostInfoText"`
	PostStageProgress int    `json:"PostStageProgress"`
}

type historyEntry struct {
	NZBID        int64  `json:"NZBID"`
	Name         string `json:"Name"`
	Kind         string `json:"Kind"`
	Category     string `json:"Category"`
	Status       string `json:"Status"`
	ParStatus    string `json:"ParStatus"`
	UnpackStatus string `json:"UnpackStatus"`
	MoveStatus   string `json:"MoveStatus"`
	ScriptStatus string `json:"ScriptStatus"`
	DeleteStatus string `json:"DeleteStatus"`
	MarkStatus   string `json:"MarkStatus"`
	URLStatus    string `json:"UrlStatus"`
	DestDir      string `json:"DestDir"`
	FinalDir     string `json:"FinalDir"`
	FileSizeLo   int64  `json:"FileSizeLo"`
	FileSizeHi   int64  `json:"FileSizeHi"`
	HistoryTime  int64  `json:"HistoryTime"`
}

func (c *Client) groups(ctx context.Context) ([]group, error) {
	var out []group
	if err := c.call(ctx, "listgroups", []any{0}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) history(ctx context.Context) ([]historyEntry, error) {
	var out []historyEntry
	if err := c.call(ctx, "history", []any{false}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// parentDir returns the folder above p for both / and \ separated paths.
func parentDir(p string) string {
	p = strings.TrimRight(p, `/\`)
	i := strings.LastIndexAny(p, `/\`)
	if i <= 0 {
		return p
	}
	return p[:i]
}

func groupItem(g group, rate int64, first bool) downloaders.Item {
	it := downloaders.Item{
		ID:       strconv.FormatInt(g.NZBID, 10),
		Name:     g.NZBName,
		Category: g.Category,
		RawState: g.Status,
		Size:     combine(g.FileSizeLo, g.FileSizeHi),
		SavePath: parentDir(g.DestDir),
		Seeds:    -1,
	}
	if g.DestDir != "" {
		it.ContentPath = g.DestDir
	}
	remaining := combine(g.RemainingSizeLo, g.RemainingSizeHi)
	it.Downloaded = max(it.Size-remaining, 0)
	if it.Size > 0 {
		it.Progress = min(max(float64(it.Downloaded)/float64(it.Size), 0), 1)
	}
	switch g.Status {
	case "DOWNLOADING":
		it.State = downloaders.StateDownloading
		if first && rate > 0 {
			it.DownloadSpeed = rate
			eta := time.Duration(remaining/rate) * time.Second
			it.ETA = &eta
		}
	case "PAUSED":
		it.State = downloaders.StatePaused
	case "QUEUED", "FETCHING":
		it.State = downloaders.StateQueued
		paused := combine(g.PausedSizeLo, g.PausedSizeHi)
		if remaining > 0 && paused >= remaining {
			it.State = downloaders.StatePaused
		}
	default: // PP_QUEUED, LOADING_PARS, VERIFYING_SOURCES, REPAIRING, VERIFYING_REPAIRED,
		// RENAMING, UNPACKING, MOVING, EXECUTING_SCRIPT, PP_FINISHED
		it.State = downloaders.StateDownloading
		it.Busy = true
		it.Progress = 1
		it.Message = g.PostInfoText
	}
	return it
}

// failureDetail explains a failed or warning history entry from its sub-statuses.
func failureDetail(h historyEntry) string {
	var parts []string
	add := func(what, v string, bad ...string) {
		for _, b := range bad {
			if v == b {
				parts = append(parts, what+": "+strings.ToLower(v))
				return
			}
		}
	}
	add("download", h.DeleteStatus, "HEALTH", "DUPE", "BAD", "MANUAL", "SCAN", "COPY", "GOOD")
	add("par", h.ParStatus, "FAILURE", "MANUAL")
	add("unpack", h.UnpackStatus, "FAILURE", "SPACE", "PASSWORD")
	add("move", h.MoveStatus, "FAILURE")
	add("script", h.ScriptStatus, "FAILURE")
	add("url", h.URLStatus, "FAILURE", "SCAN_SKIPPED", "SCAN_FAILURE")
	add("mark", h.MarkStatus, "BAD")
	if len(parts) == 0 {
		return h.Status
	}
	return h.Status + " (" + strings.Join(parts, ", ") + ")"
}

func historyItem(h historyEntry) downloaders.Item {
	size := combine(h.FileSizeLo, h.FileSizeHi)
	it := downloaders.Item{
		ID:         strconv.FormatInt(h.NZBID, 10),
		Name:       h.Name,
		Category:   h.Category,
		RawState:   h.Status,
		Size:       size,
		Downloaded: size,
		Progress:   1,
		Seeds:      -1,
	}
	dir := h.FinalDir
	if dir == "" {
		dir = h.DestDir
	}
	it.ContentPath = dir
	if dir != "" {
		it.SavePath = parentDir(dir)
	}
	if h.HistoryTime > 0 {
		it.AddedAt = time.Unix(h.HistoryTime, 0).UTC()
	}
	kind, _, _ := strings.Cut(h.Status, "/")
	switch kind {
	case "SUCCESS":
		it.State = downloaders.StateCompleted
	case "WARNING":
		it.State = downloaders.StateWarning
		it.Message = failureDetail(h)
	default: // FAILURE, DELETED
		it.State = downloaders.StateFailed
		it.Message = failureDetail(h)
	}
	return it
}

// List implements downloaders.Client.
func (c *Client) List(ctx context.Context, category string) ([]downloaders.Item, error) {
	gs, err := c.groups(ctx)
	if err != nil {
		return nil, err
	}
	hs, err := c.history(ctx)
	if err != nil {
		return nil, err
	}
	var rate int64
	for _, g := range gs {
		if g.Status == "DOWNLOADING" {
			var s statusInfo
			if err := c.call(ctx, "status", nil, &s); err != nil {
				return nil, err
			}
			rate = s.DownloadRate
			break
		}
	}
	match := func(cat string) bool { return category == "" || strings.EqualFold(cat, category) }
	out := make([]downloaders.Item, 0, len(gs)+len(hs))
	first := true
	for _, g := range gs {
		it := groupItem(g, rate, first && g.Status == "DOWNLOADING")
		if g.Status == "DOWNLOADING" {
			first = false
		}
		if match(it.Category) {
			out = append(out, it)
		}
	}
	for _, h := range hs {
		if h.Kind == "DUP" {
			continue // duplicate-check placeholders, not downloads
		}
		it := historyItem(h)
		if match(it.Category) {
			out = append(out, it)
		}
	}
	return out, nil
}

func parseID(id string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil || n <= 0 {
		return 0, downloaders.ErrItemNotFound
	}
	return n, nil
}

// where reports whether id is queued or in the history.
func (c *Client) where(ctx context.Context, n int64) (inQueue, inHistory bool, err error) {
	gs, err := c.groups(ctx)
	if err != nil {
		return false, false, err
	}
	for _, g := range gs {
		if g.NZBID == n {
			return true, false, nil
		}
	}
	hs, err := c.history(ctx)
	if err != nil {
		return false, false, err
	}
	for _, h := range hs {
		if h.NZBID == n {
			return false, true, nil
		}
	}
	return false, false, nil
}

// edit runs editqueue (NZBGet 18+ form: command, parameter, IDs).
func (c *Client) edit(ctx context.Context, command, param string, n int64) error {
	var ok bool
	if err := c.call(ctx, "editqueue", []any{command, param, []int64{n}}, &ok); err != nil {
		return err
	}
	if !ok {
		return downloaders.ClientError("NZBGet refused %s", command)
	}
	return nil
}

func (c *Client) queued(ctx context.Context, id string) (int64, error) {
	n, err := parseID(id)
	if err != nil {
		return 0, err
	}
	inQueue, _, err := c.where(ctx, n)
	if err != nil {
		return 0, err
	}
	if !inQueue {
		return 0, downloaders.ErrItemNotFound
	}
	return n, nil
}

// Files implements downloaders.Client. Finished jobs have no file list in the
// API; their files are in the final folder.
func (c *Client) Files(ctx context.Context, id string) ([]downloaders.File, error) {
	n, err := parseID(id)
	if err != nil {
		return nil, err
	}
	inQueue, inHistory, err := c.where(ctx, n)
	if err != nil {
		return nil, err
	}
	if inHistory {
		return nil, nil
	}
	if !inQueue {
		return nil, downloaders.ErrItemNotFound
	}
	var files []struct {
		Filename        string `json:"Filename"`
		FileSizeLo      int64  `json:"FileSizeLo"`
		FileSizeHi      int64  `json:"FileSizeHi"`
		RemainingSizeLo int64  `json:"RemainingSizeLo"`
		RemainingSizeHi int64  `json:"RemainingSizeHi"`
	}
	if err := c.call(ctx, "listfiles", []any{0, 0, n}, &files); err != nil {
		return nil, err
	}
	out := make([]downloaders.File, 0, len(files))
	for _, f := range files {
		size := combine(f.FileSizeLo, f.FileSizeHi)
		p := 0.0
		if size > 0 {
			p = min(max(float64(size-combine(f.RemainingSizeLo, f.RemainingSizeHi))/float64(size), 0), 1)
		}
		out = append(out, downloaders.File{Path: f.Filename, Size: size, Progress: p})
	}
	return out, nil
}

var unsafeName = regexp.MustCompile(`[\x00-\x1f/\\:*?"<>|]`)

// Add implements downloaders.Client.
func (c *Client) Add(ctx context.Context, r downloaders.AddRequest) (string, error) {
	if err := r.Validate(downloaders.Usenet); err != nil {
		return "", err
	}
	name := unsafeName.ReplaceAllString(strings.TrimSpace(r.Name), "_")
	if name == "" {
		name = "locarr"
	}
	content := r.NZBURL
	if content == "" {
		content = base64.StdEncoding.EncodeToString(r.NZB)
	} else if u, err := url.Parse(content); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%w: NZB URL must be http or https", downloaders.ErrInvalidRequest)
	}
	prio := prioNormal
	switch r.Priority {
	case downloaders.PriorityTop:
		prio = prioVeryHigh
	case downloaders.PriorityLast:
		prio = prioVeryLow
	}
	params := []any{name + ".nzb", content, r.Category, prio, r.Priority == downloaders.PriorityTop, r.Paused,
		"", 0, "SCORE", []any{}}
	var id int64
	if err := c.call(ctx, "append", params, &id); err != nil {
		return "", err
	}
	if id <= 0 {
		return "", downloaders.ClientError("NZBGet did not accept the NZB")
	}
	return strconv.FormatInt(id, 10), nil
}

// Remove implements downloaders.Client. A queued job is deleted without a
// history record; a finished one loses its history record (files stay: see
// Capabilities.DeletesData).
func (c *Client) Remove(ctx context.Context, id string, _ bool) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	inQueue, inHistory, err := c.where(ctx, n)
	if err != nil {
		return err
	}
	switch {
	case inQueue:
		return c.edit(ctx, "GroupFinalDelete", "", n)
	case inHistory:
		return c.edit(ctx, "HistoryFinalDelete", "", n)
	}
	return downloaders.ErrItemNotFound
}

// Pause implements downloaders.Client.
func (c *Client) Pause(ctx context.Context, id string) error {
	n, err := c.queued(ctx, id)
	if err != nil {
		return err
	}
	return c.edit(ctx, "GroupPause", "", n)
}

// Resume implements downloaders.Client.
func (c *Client) Resume(ctx context.Context, id string) error {
	n, err := c.queued(ctx, id)
	if err != nil {
		return err
	}
	return c.edit(ctx, "GroupResume", "", n)
}

// SetPriority implements downloaders.Client.
func (c *Client) SetPriority(ctx context.Context, id string, p downloaders.Priority) error {
	if !p.Valid() {
		return fmt.Errorf("%w: unknown priority %q", downloaders.ErrInvalidRequest, p)
	}
	n, err := c.queued(ctx, id)
	if err != nil {
		return err
	}
	prio := prioNormal
	switch p {
	case downloaders.PriorityTop:
		prio = prioVeryHigh
	case downloaders.PriorityLast:
		prio = prioVeryLow
	}
	if err := c.edit(ctx, "GroupSetPriority", strconv.Itoa(prio), n); err != nil {
		return err
	}
	switch p {
	case downloaders.PriorityTop:
		return c.edit(ctx, "GroupMoveTop", "", n)
	case downloaders.PriorityLast:
		return c.edit(ctx, "GroupMoveBottom", "", n)
	}
	return nil
}

var categoryKey = regexp.MustCompile(`^(?i)category(\d+)\.(name|destdir)$`)

// Categories implements downloaders.Client.
func (c *Client) Categories(ctx context.Context) ([]downloaders.Category, error) {
	var opts []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	}
	if err := c.call(ctx, "config", nil, &opts); err != nil {
		return nil, err
	}
	byIndex := map[int]*downloaders.Category{}
	var order []int
	for _, o := range opts {
		m := categoryKey.FindStringSubmatch(o.Name)
		if m == nil {
			continue
		}
		i, _ := strconv.Atoi(m[1])
		cat, ok := byIndex[i]
		if !ok {
			cat = &downloaders.Category{}
			byIndex[i] = cat
			order = append(order, i)
		}
		if strings.EqualFold(m[2], "name") {
			cat.Name = o.Value
		} else {
			cat.SavePath = o.Value
		}
	}
	out := make([]downloaders.Category, 0, len(order))
	for _, i := range order {
		if byIndex[i].Name != "" {
			out = append(out, *byIndex[i])
		}
	}
	return out, nil
}

// EnsureCategory implements downloaders.Client. NZBGet categories live in its
// configuration file, which the API can only rewrite as a whole, so Locarr
// does not create them; it only checks that the category exists.
func (c *Client) EnsureCategory(ctx context.Context, name, _ string) error {
	cats, err := c.Categories(ctx)
	if err != nil {
		return err
	}
	for _, x := range cats {
		if strings.EqualFold(x.Name, strings.TrimSpace(name)) {
			return nil
		}
	}
	return fmt.Errorf("%w: create category %q in NZBGet (Settings → Categories)", downloaders.ErrUnsupported, name)
}
