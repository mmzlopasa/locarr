// Package aria2 talks to aria2 over its JSON-RPC interface, following the
// public aria2 manual (RPC interface section). aria2 has no categories: a
// category is a sub-folder of the download folder.
package aria2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

func init() {
	downloaders.Register(downloaders.Implementation{
		Name:        "aria2",
		DisplayName: "aria2",
		Protocol:    downloaders.Torrent,
		DefaultPort: 6800,
		Fields: []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldUseTLS, downloaders.FieldURLBase,
			downloaders.FieldAPIKey, downloaders.FieldDirectory, downloaders.FieldAllowSelfSigned},
		Required:     []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort},
		Capabilities: caps,
		New:          func(c downloaders.Config) (downloaders.Client, error) { return New(c) },
	})
}

var caps = downloaders.Capabilities{
	Protocol:            downloaders.Torrent,
	DirectoryCategories: true,
	SeedLimits:          true,
	FirstLastPiece:      true,
	Priority:            true,
	SavePath:            true,
}

const maxTorrentFetch = 10 << 20

// statusKeys are the fields asked for in every listing.
var statusKeys = []string{"gid", "status", "totalLength", "completedLength", "uploadLength", "downloadSpeed", "uploadSpeed",
	"infoHash", "dir", "files", "bittorrent", "errorCode", "errorMessage", "followedBy", "numSeeders", "seeder",
	"verifiedLength", "verifyIntegrityPending"}

// Client is an aria2 client.
type Client struct {
	cfg      downloaders.Config
	endpoint string
	hc       *http.Client
	seq      atomic.Int64
}

var _ downloaders.Client = (*Client)(nil)

// New returns a client for cfg. The RPC secret is cfg.APIKey; the endpoint is <URL base>/jsonrpc.
func New(cfg downloaders.Config) (*Client, error) {
	u, err := cfg.BaseURL()
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(u.Path, "/jsonrpc") {
		u.Path += "/jsonrpc"
	}
	return &Client{cfg: cfg, endpoint: u.String(), hc: cfg.Client()}, nil
}

// Capabilities implements downloaders.Client.
func (c *Client) Capabilities() downloaders.Capabilities { return caps }

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// call runs one RPC method and decodes its result into out (nil = ignore).
func (c *Client) call(ctx context.Context, method string, out any, params ...any) error {
	all := make([]any, 0, len(params)+1)
	if c.cfg.APIKey != "" {
		all = append(all, "token:"+c.cfg.APIKey)
	}
	all = append(all, params...)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": strconv.FormatInt(c.seq.Add(1), 10), "method": method, "params": all})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return downloaders.ClassifyTransportError(err)
	}
	defer resp.Body.Close()
	data, err := downloaders.ReadLimited(resp.Body, downloaders.MaxResponse)
	if err != nil {
		return err
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	// aria2 answers errors with HTTP 400 and a JSON-RPC error body.
	if jerr := json.Unmarshal(data, &r); jerr != nil || (r.Result == nil && r.Error == nil) {
		if resp.StatusCode != http.StatusOK {
			return downloaders.StatusError(resp.StatusCode)
		}
		return fmt.Errorf("%w: not a JSON-RPC answer", downloaders.ErrWrongURL)
	}
	if r.Error != nil {
		return mapError(r.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(r.Result, out); err != nil {
		return downloaders.ClientError("unexpected %s answer", method)
	}
	return nil
}

func mapError(e *rpcError) error {
	msg := strings.ToLower(e.Message)
	switch {
	case strings.Contains(msg, "unauthorized"):
		return fmt.Errorf("%w: wrong RPC secret", downloaders.ErrAuth)
	case strings.Contains(msg, "not found"):
		return fmt.Errorf("%w: %s", downloaders.ErrItemNotFound, e.Message)
	}
	return downloaders.ClientError("%s", e.Message)
}

// Test implements downloaders.Client.
func (c *Client) Test(ctx context.Context) (downloaders.Status, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.call(ctx, "aria2.getVersion", &v); err != nil {
		return downloaders.Status{}, err
	}
	if v.Version == "" {
		return downloaders.Status{}, fmt.Errorf("%w: no version in the answer", downloaders.ErrWrongURL)
	}
	return downloaders.Status{Version: v.Version}, nil
}

// baseDir is the configured download folder, or aria2's global "dir".
func (c *Client) baseDir(ctx context.Context) (string, error) {
	if d := strings.TrimSpace(c.cfg.Directory); d != "" {
		return path.Clean(d), nil
	}
	var opts map[string]string
	if err := c.call(ctx, "aria2.getGlobalOption", &opts); err != nil {
		return "", err
	}
	return path.Clean(opts["dir"]), nil
}

// Add implements downloaders.Client. The category becomes a sub-folder of the
// download folder unless an explicit save path is given.
func (c *Client) Add(ctx context.Context, req downloaders.AddRequest) (string, error) {
	if err := req.Validate(downloaders.Torrent); err != nil {
		return "", err
	}
	opts := map[string]string{}
	switch {
	case req.SavePath != "":
		opts["dir"] = req.SavePath
	case req.Category != "":
		if strings.ContainsAny(req.Category, "/\\") || req.Category == "." || req.Category == ".." {
			return "", fmt.Errorf("%w: a category cannot contain slashes", downloaders.ErrInvalidRequest)
		}
		base, err := c.baseDir(ctx)
		if err != nil {
			return "", err
		}
		opts["dir"] = path.Join(base, req.Category)
	}
	if req.Paused {
		opts["pause"] = "true"
	}
	if req.SeedRatio != nil {
		opts["seed-ratio"] = strconv.FormatFloat(*req.SeedRatio, 'f', -1, 64)
	}
	if req.SeedTime != nil {
		opts["seed-time"] = strconv.FormatInt(int64(req.SeedTime.Minutes()), 10)
	}
	if req.FirstLastPiece {
		opts["bt-prioritize-piece"] = "head,tail"
	}

	if req.TorrentURL != "" {
		b, err := fetchTorrent(ctx, c.hc, req.TorrentURL, maxTorrentFetch)
		if err != nil {
			return "", err
		}
		req.TorrentFile = b
	}
	var gid, hash string
	if req.Magnet != "" {
		m, err := downloaders.ParseMagnet(req.Magnet)
		if err != nil {
			return "", err
		}
		hash = m.InfoHash
		if err := c.call(ctx, "aria2.addUri", &gid, []string{req.Magnet}, opts); err != nil {
			return "", err
		}
	} else {
		ti, err := downloaders.ParseTorrent(req.TorrentFile)
		if err != nil {
			return "", err
		}
		hash = ti.InfoHash
		// Verify data that is already there, so a pre-placed download seeds at once.
		opts["check-integrity"] = "true"
		if err := c.call(ctx, "aria2.addTorrent", &gid, base64.StdEncoding.EncodeToString(req.TorrentFile), []string{}, opts); err != nil {
			return "", err
		}
	}
	if req.Priority == downloaders.PriorityTop || req.Priority == downloaders.PriorityLast {
		_ = c.position(ctx, gid, req.Priority) // only possible while waiting; best effort
	}
	return hash, nil
}

type status struct {
	GID             string `json:"gid"`
	Status          string `json:"status"`
	TotalLength     string `json:"totalLength"`
	CompletedLength string `json:"completedLength"`
	UploadLength    string `json:"uploadLength"`
	DownloadSpeed   string `json:"downloadSpeed"`
	UploadSpeed     string `json:"uploadSpeed"`
	InfoHash        string `json:"infoHash"`
	Dir             string `json:"dir"`
	Files           []struct {
		Path            string `json:"path"`
		Length          string `json:"length"`
		CompletedLength string `json:"completedLength"`
		Selected        string `json:"selected"`
	} `json:"files"`
	Bittorrent *struct {
		Info *struct {
			Name string `json:"name"`
		} `json:"info"`
		Mode string `json:"mode"`
	} `json:"bittorrent"`
	ErrorCode              string   `json:"errorCode"`
	ErrorMessage           string   `json:"errorMessage"`
	FollowedBy             []string `json:"followedBy"`
	NumSeeders             string   `json:"numSeeders"`
	Seeder                 string   `json:"seeder"`
	VerifiedLength         string   `json:"verifiedLength"`
	VerifyIntegrityPending string   `json:"verifyIntegrityPending"`
}

func n64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// metadata reports a magnet's metadata-only download.
func (s *status) metadata() bool {
	return (s.Bittorrent == nil || s.Bittorrent.Info == nil) && len(s.Files) > 0 && strings.HasPrefix(path.Base(s.Files[0].Path), "[METADATA]")
}

// id is the info hash for BitTorrent downloads, the gid otherwise.
func (s *status) id() string {
	if s.InfoHash != "" {
		return downloaders.NormaliseHash(s.InfoHash)
	}
	return s.GID
}

// all returns every download aria2 knows about (active, waiting, stopped),
// with removed ones and superseded magnet metadata downloads dropped, one entry per ID.
func (c *Client) all(ctx context.Context) ([]status, error) {
	var active, waiting, stopped []status
	if err := c.call(ctx, "aria2.tellActive", &active, statusKeys); err != nil {
		return nil, err
	}
	if err := c.call(ctx, "aria2.tellWaiting", &waiting, 0, 1000, statusKeys); err != nil {
		return nil, err
	}
	if err := c.call(ctx, "aria2.tellStopped", &stopped, 0, 1000, statusKeys); err != nil {
		return nil, err
	}
	byID := map[string]int{}
	var out []status
	for _, s := range append(append(active, waiting...), stopped...) {
		if s.Status == "removed" || (s.metadata() && len(s.FollowedBy) > 0) {
			continue
		}
		id := s.id()
		if i, ok := byID[id]; ok {
			// Prefer the real download over the metadata one.
			if out[i].metadata() && !s.metadata() {
				out[i] = s
			}
			continue
		}
		byID[id] = len(out)
		out = append(out, s)
	}
	return out, nil
}

// List implements downloaders.Client. A category is the sub-folder of the
// download folder an item is saved in.
func (c *Client) List(ctx context.Context, category string) ([]downloaders.Item, error) {
	base, err := c.baseDir(ctx)
	if err != nil {
		return nil, err
	}
	list, err := c.all(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]downloaders.Item, 0, len(list))
	for i := range list {
		it := toItem(&list[i], base)
		if category != "" && !strings.EqualFold(it.Category, category) {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func categoryOf(dir, base string) string {
	dir, base = path.Clean(dir), path.Clean(base)
	rel, ok := strings.CutPrefix(dir, base+"/")
	if !ok || base == "." || rel == "" {
		return ""
	}
	first, _, _ := strings.Cut(rel, "/")
	return first
}

func toItem(s *status, base string) downloaders.Item {
	it := downloaders.Item{
		ID:            s.id(),
		RawState:      s.Status,
		Size:          n64(s.TotalLength),
		Downloaded:    n64(s.CompletedLength),
		Uploaded:      n64(s.UploadLength),
		DownloadSpeed: n64(s.DownloadSpeed),
		UploadSpeed:   n64(s.UploadSpeed),
		SavePath:      s.Dir,
		Category:      categoryOf(s.Dir, base),
		Message:       s.ErrorMessage,
		Seeds:         -1,
	}
	if s.NumSeeders != "" {
		it.Seeds = int(n64(s.NumSeeders))
	}
	switch {
	case s.Bittorrent != nil && s.Bittorrent.Info != nil && s.Bittorrent.Info.Name != "":
		it.Name = s.Bittorrent.Info.Name
		it.ContentPath = path.Join(s.Dir, it.Name)
	case len(s.Files) > 0:
		it.Name = path.Base(s.Files[0].Path)
		it.ContentPath = s.Files[0].Path
	}
	if it.Size > 0 {
		it.Progress = min(1, float64(it.Downloaded)/float64(it.Size))
	}
	if it.Downloaded > 0 {
		it.Ratio = float64(it.Uploaded) / float64(it.Downloaded)
	}
	if left := it.Size - it.Downloaded; left > 0 && it.DownloadSpeed > 0 {
		eta := time.Duration(left/it.DownloadSpeed) * time.Second
		it.ETA = &eta
	}
	verifying := s.VerifiedLength != "" || s.VerifyIntegrityPending == "true"
	switch s.Status {
	case "active":
		switch {
		case verifying:
			it.State, it.Busy = downloaders.StateDownloading, true
			it.RawState = "active (verifying)"
		case s.Seeder == "true":
			it.State = downloaders.StateSeeding
			it.Progress = 1
			it.RawState = "active (seeding)"
		default:
			it.State = downloaders.StateDownloading
		}
	case "waiting":
		it.State = downloaders.StateQueued
	case "paused":
		it.State = downloaders.StatePaused
	case "error":
		it.State = downloaders.StateFailed
		if it.Message == "" {
			it.Message = "aria2 error " + s.ErrorCode
		}
	case "complete":
		it.State = downloaders.StateCompleted
		it.Progress = 1
	default:
		it.State = downloaders.StateWarning
	}
	if s.metadata() {
		it.Busy = true // only the metadata so far
		it.Progress = 0
	}
	return it
}

// find resolves an ID (info hash or gid) to aria2's status entry.
func (c *Client) find(ctx context.Context, id string) (status, error) {
	list, err := c.all(ctx)
	if err != nil {
		return status{}, err
	}
	for _, s := range list {
		if downloaders.SameID(s.id(), id) || s.GID == id {
			return s, nil
		}
	}
	return status{}, fmt.Errorf("%w: %s", downloaders.ErrItemNotFound, id)
}

// Files implements downloaders.Client.
func (c *Client) Files(ctx context.Context, id string) ([]downloaders.File, error) {
	s, err := c.find(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]downloaders.File, 0, len(s.Files))
	for _, f := range s.Files {
		p := f.Path
		if rel, ok := strings.CutPrefix(p, path.Clean(s.Dir)+"/"); ok {
			p = rel
		}
		ff := downloaders.File{Path: p, Size: n64(f.Length)}
		if ff.Size > 0 {
			ff.Progress = float64(n64(f.CompletedLength)) / float64(ff.Size)
		}
		out = append(out, ff)
	}
	return out, nil
}

// Remove implements downloaders.Client. aria2 never deletes files
// (Capabilities.DeletesData is false): the caller deletes them.
func (c *Client) Remove(ctx context.Context, id string, _ bool) error {
	found := false
	// A magnet has two gids (metadata and real download): remove both.
	var gids []status
	var active, waiting, stopped []status
	if err := c.call(ctx, "aria2.tellActive", &active, []string{"gid", "status", "infoHash"}); err != nil {
		return err
	}
	if err := c.call(ctx, "aria2.tellWaiting", &waiting, 0, 1000, []string{"gid", "status", "infoHash"}); err != nil {
		return err
	}
	if err := c.call(ctx, "aria2.tellStopped", &stopped, 0, 1000, []string{"gid", "status", "infoHash"}); err != nil {
		return err
	}
	for _, s := range append(append(active, waiting...), stopped...) {
		if downloaders.SameID(s.id(), id) || s.GID == id {
			gids = append(gids, s)
		}
	}
	for _, s := range gids {
		found = true
		switch s.Status {
		case "active", "waiting", "paused":
			if err := c.call(ctx, "aria2.forceRemove", nil, s.GID); err != nil && !errors.Is(err, downloaders.ErrItemNotFound) {
				return err
			}
			// forceRemove is asynchronous: wait (briefly) until it is stopped.
			for range 50 {
				var st status
				if err := c.call(ctx, "aria2.tellStatus", &st, s.GID, []string{"status"}); err != nil || st.Status == "removed" {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		if err := c.call(ctx, "aria2.removeDownloadResult", nil, s.GID); err != nil && !errors.Is(err, downloaders.ErrItemNotFound) {
			return err
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", downloaders.ErrItemNotFound, id)
	}
	return nil
}

// Pause implements downloaders.Client.
func (c *Client) Pause(ctx context.Context, id string) error {
	s, err := c.find(ctx, id)
	if err != nil {
		return err
	}
	if s.Status != "active" && s.Status != "waiting" {
		return nil
	}
	return c.call(ctx, "aria2.forcePause", nil, s.GID)
}

// Resume implements downloaders.Client.
func (c *Client) Resume(ctx context.Context, id string) error {
	s, err := c.find(ctx, id)
	if err != nil {
		return err
	}
	if s.Status != "paused" {
		return nil
	}
	return c.call(ctx, "aria2.unpause", nil, s.GID)
}

func (c *Client) position(ctx context.Context, gid string, p downloaders.Priority) error {
	switch p {
	case downloaders.PriorityTop:
		return c.call(ctx, "aria2.changePosition", nil, gid, 0, "POS_SET")
	case downloaders.PriorityLast:
		return c.call(ctx, "aria2.changePosition", nil, gid, 0, "POS_END")
	}
	return nil
}

// SetPriority implements downloaders.Client: moves a waiting download in the
// queue. Running and finished downloads have no queue position (no-op).
func (c *Client) SetPriority(ctx context.Context, id string, p downloaders.Priority) error {
	if !p.Valid() {
		return fmt.Errorf("%w: unknown priority %q", downloaders.ErrInvalidRequest, p)
	}
	s, err := c.find(ctx, id)
	if err != nil {
		return err
	}
	if s.Status != "waiting" && s.Status != "paused" {
		return nil
	}
	return c.position(ctx, s.GID, p)
}

// Categories implements downloaders.Client: the sub-folders in use.
func (c *Client) Categories(ctx context.Context) ([]downloaders.Category, error) {
	items, err := c.List(ctx, "")
	if err != nil {
		return nil, err
	}
	base, err := c.baseDir(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []downloaders.Category
	for _, it := range items {
		if it.Category == "" || seen[it.Category] {
			continue
		}
		seen[it.Category] = true
		out = append(out, downloaders.Category{Name: it.Category, SavePath: path.Join(base, it.Category)})
	}
	return out, nil
}

// EnsureCategory implements downloaders.Client: aria2 creates the folder when
// it saves into it, so there is nothing to do.
func (c *Client) EnsureCategory(context.Context, string, string) error { return nil }
