// Package deluge is a client for the Deluge Web UI JSON-RPC API, written from
// its public documentation.
package deluge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

func init() {
	downloaders.Register(downloaders.Implementation{
		Name:        "deluge",
		DisplayName: "Deluge",
		Protocol:    downloaders.Torrent,
		DefaultPort: 8112,
		Fields: []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldUseTLS, downloaders.FieldURLBase,
			downloaders.FieldPassword, downloaders.FieldAllowSelfSigned},
		Required:     []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldPassword},
		Capabilities: caps,
		New:          func(c downloaders.Config) (downloaders.Client, error) { return New(c) },
	})
}

// Categories need the Label plugin; without it they are reported unavailable at
// run time (EnsureCategory returns ErrUnsupported, items have no category).
var caps = downloaders.Capabilities{
	Protocol: downloaders.Torrent, Categories: true, CreateCategories: true, SeedLimits: true, Sequential: true,
	FirstLastPiece: true, Priority: true, SavePath: true, FreeSpace: true, DeletesData: true,
}

// Client talks to one Deluge Web UI (and through it, its daemon).
type Client struct {
	endpoint string
	cfg      downloaders.Config
	http     *http.Client
	nextID   atomic.Int64

	mu       sync.Mutex
	loggedIn bool
}

// New returns a client for cfg.
func New(cfg downloaders.Config) (*Client, error) {
	base, err := cfg.BaseURL()
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(nil)
	hc := *cfg.Client()
	hc.Jar = jar
	return &Client{endpoint: strings.TrimSuffix(base.String(), "/") + "/json", cfg: cfg, http: &hc}, nil
}

// Capabilities implements downloaders.Client.
func (c *Client) Capabilities() downloaders.Capabilities { return caps }

// rpcError is an error object of a JSON-RPC answer.
type rpcError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// errNotAuthenticated is Deluge's error code for a missing or expired session.
const errNotAuthenticated = 1

func (c *Client) raw(ctx context.Context, method string, params []any, out any) (*rpcError, error) {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"method": method, "params": params, "id": c.nextID.Add(1)})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, downloaders.ClassifyTransportError(err)
	}
	defer resp.Body.Close()
	b, err := downloaders.ReadLimited(resp.Body, downloaders.MaxResponse)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, downloaders.StatusError(resp.StatusCode)
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%w: answer is not Deluge JSON-RPC", downloaders.ErrWrongURL)
	}
	if r.Error != nil {
		return r.Error, nil
	}
	if out != nil && len(r.Result) > 0 && string(r.Result) != "null" {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return nil, downloaders.ClientError("unexpected %s answer", method)
		}
	}
	return nil, nil
}

// login signs in and makes sure the Web UI is connected to a daemon.
func (c *Client) login(ctx context.Context) error {
	var ok bool
	rerr, err := c.raw(ctx, "auth.login", []any{c.cfg.Password}, &ok)
	if err != nil {
		return err
	}
	if rerr != nil {
		return downloaders.ClientError("%s", rerr.Message)
	}
	if !ok {
		return downloaders.ErrAuth
	}
	var connected bool
	if rerr, err := c.raw(ctx, "web.connected", nil, &connected); err != nil || rerr != nil {
		return firstErr(err, rerr)
	}
	if !connected {
		var hosts [][]any
		if rerr, err := c.raw(ctx, "web.get_hosts", nil, &hosts); err != nil || rerr != nil {
			return firstErr(err, rerr)
		}
		if len(hosts) == 0 {
			return downloaders.ClientError("the Web UI has no daemon to connect to")
		}
		id, _ := hosts[0][0].(string)
		if rerr, err := c.raw(ctx, "web.connect", []any{id}, nil); err != nil || rerr != nil {
			return firstErr(err, rerr)
		}
		// Connecting is asynchronous; wait briefly for it.
		for i := 0; i < 20; i++ {
			if rerr, err := c.raw(ctx, "web.connected", nil, &connected); err == nil && rerr == nil && connected {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		if !connected {
			return downloaders.ClientError("the Web UI could not connect to the Deluge daemon")
		}
	}
	c.loggedIn = true
	return nil
}

func firstErr(err error, rerr *rpcError) error {
	if err != nil {
		return err
	}
	return downloaders.ClientError("%s", rerr.Message)
}

// call runs an authenticated method, signing in (again) when needed.
func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if !c.loggedIn {
			if err := c.login(ctx); err != nil {
				return err
			}
		}
		rerr, err := c.raw(ctx, method, params, out)
		if err != nil {
			return err
		}
		if rerr == nil {
			return nil
		}
		if rerr.Code == errNotAuthenticated && attempt == 0 {
			c.loggedIn = false
			continue
		}
		return downloaders.ClientError("%s", rerr.Message)
	}
	return downloaders.ErrAuth
}

// Test implements downloaders.Client.
func (c *Client) Test(ctx context.Context) (downloaders.Status, error) {
	var version string
	if err := c.call(ctx, "daemon.info", nil, &version); err != nil || version == "" {
		// Older daemons: the libtorrent-independent version call.
		if err2 := c.call(ctx, "daemon.get_version", nil, &version); err2 != nil {
			if err != nil {
				return downloaders.Status{}, err
			}
			return downloaders.Status{}, err2
		}
	}
	st := downloaders.Status{Version: version}
	var free int64
	if c.call(ctx, "core.get_free_space", nil, &free) == nil && free >= 0 {
		st.FreeSpace = &free
	}
	var dir string
	if c.call(ctx, "core.get_config_value", []any{"download_location"}, &dir) == nil {
		st.SavePath = dir
	}
	return st, nil
}

func (c *Client) hasLabels(ctx context.Context) (bool, error) {
	var plugins []string
	if err := c.call(ctx, "core.get_enabled_plugins", nil, &plugins); err != nil {
		return false, err
	}
	return slices.ContainsFunc(plugins, func(p string) bool { return strings.EqualFold(p, "Label") }), nil
}

// Add implements downloaders.Client.
func (c *Client) Add(ctx context.Context, r downloaders.AddRequest) (string, error) {
	if err := r.Validate(downloaders.Torrent); err != nil {
		return "", err
	}
	opts := map[string]any{"add_paused": r.Paused}
	if r.SavePath != "" {
		opts["download_location"] = r.SavePath
	}
	if r.SeedRatio != nil {
		opts["stop_at_ratio"] = true
		opts["stop_ratio"] = *r.SeedRatio
	}
	if r.Sequential {
		opts["sequential_download"] = true
	}
	if r.FirstLastPiece {
		opts["prioritize_first_last_pieces"] = true
	}
	var (
		hash   string
		method string
		params []any
	)
	switch {
	case r.Magnet != "":
		m, err := downloaders.ParseMagnet(r.Magnet)
		if err != nil {
			return "", err
		}
		hash, method, params = m.InfoHash, "core.add_torrent_magnet", []any{r.Magnet, opts}
	case len(r.TorrentFile) > 0:
		ti, err := downloaders.ParseTorrent(r.TorrentFile)
		if err != nil {
			return "", err
		}
		hash, method = ti.InfoHash, "core.add_torrent_file"
		params = []any{ti.Name + ".torrent", base64.StdEncoding.EncodeToString(r.TorrentFile), opts}
	default:
		method, params = "core.add_torrent_url", []any{r.TorrentURL, opts}
	}
	var got *string
	if err := c.call(ctx, method, params, &got); err != nil {
		// Already in the session: Deluge refuses a duplicate; the existing one is ours.
		if hash != "" && strings.Contains(strings.ToLower(err.Error()), "already") {
			got = &hash
		} else {
			return "", err
		}
	}
	if got != nil && *got != "" {
		hash = downloaders.NormaliseHash(*got)
	}
	if hash == "" {
		return "", downloaders.ClientError("Deluge did not return the torrent")
	}
	if r.Category != "" {
		if ok, err := c.hasLabels(ctx); err == nil && ok {
			label := strings.ToLower(r.Category)
			if err := c.ensureLabel(ctx, label); err != nil {
				return hash, err
			}
			if err := c.call(ctx, "label.set_torrent", []any{hash, label}, nil); err != nil {
				return hash, err
			}
		}
	}
	if r.Priority == downloaders.PriorityTop || r.Priority == downloaders.PriorityLast {
		_ = c.SetPriority(ctx, hash, r.Priority)
	}
	return hash, nil
}

var keys = []string{"name", "state", "progress", "total_wanted", "total_done", "total_uploaded", "download_payload_rate",
	"upload_payload_rate", "eta", "ratio", "seeding_time", "save_path", "download_location", "label", "time_added",
	"message", "is_finished", "num_seeds", "stop_at_ratio", "stop_ratio", "hash"}

type torrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"` // percent
	TotalWanted int64   `json:"total_wanted"`
	TotalDone   int64   `json:"total_done"`
	Uploaded    int64   `json:"total_uploaded"`
	DLRate      float64 `json:"download_payload_rate"`
	ULRate      float64 `json:"upload_payload_rate"`
	ETA         float64 `json:"eta"`
	Ratio       float64 `json:"ratio"`
	SeedingTime int64   `json:"seeding_time"`
	SavePath    string  `json:"save_path"`
	DownloadLoc string  `json:"download_location"`
	Label       string  `json:"label"`
	TimeAdded   float64 `json:"time_added"`
	Message     string  `json:"message"`
	IsFinished  bool    `json:"is_finished"`
	NumSeeds    int     `json:"num_seeds"`
	StopAtRatio bool    `json:"stop_at_ratio"`
	StopRatio   float64 `json:"stop_ratio"`
	Files       []struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	} `json:"files"`
	FileProgress []float64 `json:"file_progress"`
}

func (t torrent) item(id string) downloaders.Item {
	save := t.DownloadLoc
	if save == "" {
		save = t.SavePath
	}
	it := downloaders.Item{
		ID: downloaders.NormaliseHash(id), Name: t.Name, Category: t.Label, RawState: t.State, Progress: t.Progress / 100,
		Size: t.TotalWanted, Downloaded: t.TotalDone, Uploaded: t.Uploaded, DownloadSpeed: int64(t.DLRate),
		UploadSpeed: int64(t.ULRate), Ratio: max(t.Ratio, 0), SeedingTime: time.Duration(t.SeedingTime) * time.Second,
		SavePath: save, ContentPath: path.Join(save, t.Name), AddedAt: time.Unix(int64(t.TimeAdded), 0).UTC(), Seeds: t.NumSeeds,
	}
	if it.Progress > 1 {
		it.Progress = 1
	}
	switch t.State {
	case "Downloading":
		it.State = downloaders.StateDownloading
	case "Seeding":
		it.State = downloaders.StateSeeding
	case "Paused":
		if t.IsFinished || it.Progress >= 1 {
			it.State = downloaders.StateCompleted
			it.SeedGoalReached = t.StopAtRatio && t.StopRatio > 0 && t.Ratio >= t.StopRatio
		} else {
			it.State = downloaders.StatePaused
		}
	case "Queued":
		it.State = downloaders.StateQueued
		if t.IsFinished {
			it.State = downloaders.StateSeeding
		}
	case "Checking", "Allocating", "Moving":
		it.State, it.Busy = downloaders.StateQueued, true
		if t.IsFinished {
			it.State = downloaders.StateSeeding
		}
	case "Error":
		it.State = downloaders.StateFailed
	default:
		it.State = downloaders.StateWarning
	}
	if t.Message != "" && !strings.EqualFold(t.Message, "OK") {
		it.Message = t.Message
	}
	if it.State == downloaders.StateDownloading && t.ETA > 0 {
		d := time.Duration(t.ETA) * time.Second
		it.ETA = &d
	}
	return it
}

func (c *Client) status(ctx context.Context, filter map[string]any, extra ...string) (map[string]torrent, error) {
	if filter == nil {
		filter = map[string]any{}
	}
	var m map[string]torrent
	if err := c.call(ctx, "core.get_torrents_status", []any{filter, append(slices.Clone(keys), extra...)}, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// List implements downloaders.Client.
func (c *Client) List(ctx context.Context, category string) ([]downloaders.Item, error) {
	m, err := c.status(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]downloaders.Item, 0, len(m))
	for id, t := range m {
		if category != "" && !strings.EqualFold(t.Label, category) {
			continue
		}
		out = append(out, t.item(id))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AddedAt.Before(out[j].AddedAt) })
	return out, nil
}

func (c *Client) one(ctx context.Context, id string, extra ...string) (torrent, error) {
	m, err := c.status(ctx, map[string]any{"id": []string{downloaders.NormaliseHash(id)}}, extra...)
	if err != nil {
		return torrent{}, err
	}
	for k, t := range m {
		if downloaders.SameID(k, id) {
			return t, nil
		}
	}
	return torrent{}, downloaders.ErrItemNotFound
}

// Files implements downloaders.Client.
func (c *Client) Files(ctx context.Context, id string) ([]downloaders.File, error) {
	t, err := c.one(ctx, id, "files", "file_progress")
	if err != nil {
		return nil, err
	}
	out := make([]downloaders.File, 0, len(t.Files))
	for i, f := range t.Files {
		p := 0.0
		if i < len(t.FileProgress) {
			p = t.FileProgress[i]
		}
		out = append(out, downloaders.File{Path: f.Path, Size: f.Size, Progress: p})
	}
	return out, nil
}

// Remove implements downloaders.Client.
func (c *Client) Remove(ctx context.Context, id string, deleteData bool) error {
	if _, err := c.one(ctx, id); err != nil {
		return err
	}
	return c.call(ctx, "core.remove_torrent", []any{downloaders.NormaliseHash(id), deleteData}, nil)
}

func (c *Client) onIDs(ctx context.Context, method, id string) error {
	if _, err := c.one(ctx, id); err != nil {
		return err
	}
	return c.call(ctx, method, []any{[]string{downloaders.NormaliseHash(id)}}, nil)
}

// Pause implements downloaders.Client.
func (c *Client) Pause(ctx context.Context, id string) error {
	return c.onIDs(ctx, "core.pause_torrent", id)
}

// Resume implements downloaders.Client.
func (c *Client) Resume(ctx context.Context, id string) error {
	return c.onIDs(ctx, "core.resume_torrent", id)
}

// SetPriority implements downloaders.Client.
func (c *Client) SetPriority(ctx context.Context, id string, p downloaders.Priority) error {
	switch p {
	case downloaders.PriorityTop:
		return c.onIDs(ctx, "core.queue_top", id)
	case downloaders.PriorityLast:
		return c.onIDs(ctx, "core.queue_bottom", id)
	case downloaders.PriorityNormal:
		return nil
	}
	return fmt.Errorf("%w: unknown priority %q", downloaders.ErrInvalidRequest, p)
}

// Categories implements downloaders.Client: the Label plugin's labels.
func (c *Client) Categories(ctx context.Context) ([]downloaders.Category, error) {
	ok, err := c.hasLabels(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var labels []string
	if err := c.call(ctx, "label.get_labels", nil, &labels); err != nil {
		return nil, err
	}
	out := make([]downloaders.Category, 0, len(labels))
	for _, l := range labels {
		out = append(out, downloaders.Category{Name: l})
	}
	return out, nil
}

func (c *Client) ensureLabel(ctx context.Context, label string) error {
	var labels []string
	if err := c.call(ctx, "label.get_labels", nil, &labels); err != nil {
		return err
	}
	if slices.Contains(labels, label) {
		return nil
	}
	return c.call(ctx, "label.add", []any{label}, nil)
}

// EnsureCategory implements downloaders.Client. Deluge labels are lower case.
func (c *Client) EnsureCategory(ctx context.Context, name, _ string) error {
	ok, err := c.hasLabels(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: enable the Label plugin in Deluge to use categories", downloaders.ErrUnsupported)
	}
	return c.ensureLabel(ctx, strings.ToLower(name))
}

var _ downloaders.Client = (*Client)(nil)
