// Package qbittorrent is a client for the qBittorrent WebUI API (v2), written
// from its public API documentation.
package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

func init() {
	downloaders.Register(downloaders.Implementation{
		Name:        "qbittorrent",
		DisplayName: "qBittorrent",
		Protocol:    downloaders.Torrent,
		DefaultPort: 8080,
		DefaultUser: "admin",
		Fields: []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldUseTLS, downloaders.FieldURLBase,
			downloaders.FieldUsername, downloaders.FieldPassword, downloaders.FieldAllowSelfSigned},
		Required:     []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort},
		Capabilities: caps,
		New:          func(c downloaders.Config) (downloaders.Client, error) { return New(c) },
	})
}

var caps = downloaders.Capabilities{
	Protocol: downloaders.Torrent, Categories: true, CreateCategories: true, Tags: true, SeedLimits: true,
	Sequential: true, FirstLastPiece: true, Priority: true, SavePath: true, FreeSpace: true, DeletesData: true,
}

// Client talks to one qBittorrent instance.
type Client struct {
	base *url.URL
	cfg  downloaders.Config
	http *http.Client

	mu       sync.Mutex
	loggedIn bool
	apiVer   string
}

// New returns a client for cfg.
func New(cfg downloaders.Config) (*Client, error) {
	base, err := cfg.BaseURL()
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(nil)
	hc := *cfg.Client() // copy: the cookie jar is per client
	hc.Jar = jar
	return &Client{base: base, cfg: cfg, http: &hc}, nil
}

// Capabilities implements downloaders.Client.
func (c *Client) Capabilities() downloaders.Capabilities { return caps }

func (c *Client) endpoint(path string) string {
	return strings.TrimSuffix(c.base.String(), "/") + "/api/v2/" + path
}

func (c *Client) referer() string { return strings.TrimSuffix(c.base.String(), "/") + "/" }

// send sends one request; body is form values or a multipart body.
func (c *Client) send(ctx context.Context, method, path string, form url.Values, body io.Reader, contentType string) (int, []byte, error) {
	var rd io.Reader
	if form != nil {
		rd = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else if body != nil {
		rd = body
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), rd)
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// The WebUI's CSRF protection wants a Referer (or Origin) matching the host.
	req.Header.Set("Referer", c.referer())
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, downloaders.ClassifyTransportError(err)
	}
	defer resp.Body.Close()
	b, err := downloaders.ReadLimited(resp.Body, downloaders.MaxResponse)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, b, nil
}

func (c *Client) login(ctx context.Context) error {
	form := url.Values{"username": {c.cfg.Username}, "password": {c.cfg.Password}}
	status, b, err := c.send(ctx, http.MethodPost, "auth/login", form, nil, "")
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusForbidden:
		return fmt.Errorf("%w: too many failed attempts, the client banned this address", downloaders.ErrAuth)
	case status == http.StatusUnauthorized:
		// Newer versions answer 401 to a wrong password, and also when the Host
		// header does not match (a port published under another number).
		return fmt.Errorf("%w (or the client's host header validation refused the address)", downloaders.ErrAuth)
	case status == http.StatusNotFound:
		return fmt.Errorf("%w (HTTP 404)", downloaders.ErrWrongURL)
	case status == http.StatusNoContent: // 5.x: success without a body
	case status != http.StatusOK:
		return downloaders.StatusError(status)
	}
	text := strings.TrimSpace(string(b))
	if strings.EqualFold(text, "Fails.") {
		return downloaders.ErrAuth
	}
	if text != "Ok." && text != "" {
		return fmt.Errorf("%w: unexpected login answer", downloaders.ErrWrongURL)
	}
	c.loggedIn = true
	return nil
}

// call performs an authenticated API call, logging in first and once more when
// the session has expired (403).
func (c *Client) call(ctx context.Context, method, path string, form url.Values, mp func() (io.Reader, string)) (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if !c.loggedIn {
			if err := c.login(ctx); err != nil {
				return 0, nil, err
			}
		}
		var (
			body io.Reader
			ct   string
		)
		if mp != nil {
			body, ct = mp()
		}
		status, b, err := c.send(ctx, method, path, form, body, ct)
		if err != nil {
			return status, nil, err
		}
		if status == http.StatusForbidden && attempt == 0 {
			c.loggedIn = false
			continue
		}
		return status, b, nil
	}
	return http.StatusForbidden, nil, downloaders.ErrAuth
}

// expectOK runs a call that must answer 200.
func (c *Client) expectOK(ctx context.Context, path string, form url.Values) ([]byte, error) {
	method := http.MethodPost
	if form == nil {
		method = http.MethodGet
	}
	status, b, err := c.call(ctx, method, path, form, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, downloaders.StatusError(status)
	}
	return b, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return c.expectOK(ctx, path, nil)
}

// webAPIVersion returns the WebUI API version ("2.11.2"), cached.
func (c *Client) webAPIVersion(ctx context.Context) (string, error) {
	c.mu.Lock()
	v := c.apiVer
	c.mu.Unlock()
	if v != "" {
		return v, nil
	}
	b, err := c.get(ctx, "app/webapiVersion", nil)
	if err != nil {
		return "", err
	}
	v = strings.TrimSpace(string(b))
	c.mu.Lock()
	c.apiVer = v
	c.mu.Unlock()
	return v, nil
}

// atLeast compares dotted versions.
func atLeast(v string, want ...int) bool {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	for i, w := range want {
		n := 0
		if i < len(parts) {
			n, _ = strconv.Atoi(parts[i])
		}
		if n != w {
			return n > w
		}
	}
	return true
}

// Test implements downloaders.Client.
func (c *Client) Test(ctx context.Context) (downloaders.Status, error) {
	b, err := c.get(ctx, "app/version", nil)
	if err != nil {
		return downloaders.Status{}, err
	}
	st := downloaders.Status{Version: strings.TrimSpace(string(b))}
	if st.Version == "" || strings.Contains(st.Version, "<") {
		return downloaders.Status{}, fmt.Errorf("%w: no version in the answer", downloaders.ErrWrongURL)
	}
	if _, err := c.webAPIVersion(ctx); err != nil {
		return downloaders.Status{}, err
	}
	if b, err := c.get(ctx, "sync/maindata", nil); err == nil {
		var md struct {
			ServerState struct {
				FreeSpace *int64 `json:"free_space_on_disk"`
			} `json:"server_state"`
		}
		if json.Unmarshal(b, &md) == nil && md.ServerState.FreeSpace != nil {
			st.FreeSpace = md.ServerState.FreeSpace
		}
	}
	return st, nil
}

// Add implements downloaders.Client.
func (c *Client) Add(ctx context.Context, r downloaders.AddRequest) (string, error) {
	if err := r.Validate(downloaders.Torrent); err != nil {
		return "", err
	}
	var hash string
	switch {
	case r.Magnet != "":
		m, err := downloaders.ParseMagnet(r.Magnet)
		if err != nil {
			return "", err
		}
		hash = m.InfoHash
	case len(r.TorrentFile) > 0:
		ti, err := downloaders.ParseTorrent(r.TorrentFile)
		if err != nil {
			return "", err
		}
		hash = ti.InfoHash
	}
	fields := map[string]string{}
	if r.Magnet != "" {
		fields["urls"] = r.Magnet
	} else if r.TorrentURL != "" {
		fields["urls"] = r.TorrentURL
	}
	if r.SavePath != "" {
		fields["savepath"] = r.SavePath
	}
	if r.Category != "" {
		fields["category"] = r.Category
	}
	if len(r.Tags) > 0 {
		fields["tags"] = strings.Join(r.Tags, ",")
	}
	paused := strconv.FormatBool(r.Paused)
	fields["paused"] = paused  // API before 2.11
	fields["stopped"] = paused // API 2.11 and later
	if r.SeedRatio != nil {
		fields["ratioLimit"] = strconv.FormatFloat(*r.SeedRatio, 'f', 2, 64)
	}
	if r.SeedTime != nil {
		fields["seedingTimeLimit"] = strconv.FormatInt(int64(r.SeedTime.Minutes()), 10)
	}
	if r.Sequential {
		fields["sequentialDownload"] = "true"
	}
	if r.FirstLastPiece {
		fields["firstLastPiecePrio"] = "true"
	}
	mp := func() (io.Reader, string) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for k, v := range fields {
			_ = w.WriteField(k, v)
		}
		if len(r.TorrentFile) > 0 {
			fw, _ := w.CreateFormFile("torrents", "locarr.torrent")
			_, _ = fw.Write(r.TorrentFile)
		}
		_ = w.Close()
		return &buf, w.FormDataContentType()
	}
	status, b, err := c.call(ctx, http.MethodPost, "torrents/add", nil, mp)
	if err != nil {
		return "", err
	}
	if status == http.StatusUnsupportedMediaType {
		return "", downloaders.ClientError("the torrent file is not valid")
	}
	if status != http.StatusOK && status != http.StatusConflict {
		return "", downloaders.StatusError(status)
	}
	if strings.TrimSpace(string(b)) == "Fails." {
		if hash != "" {
			// Already added: the existing torrent is the one we want.
			if _, ok, _ := c.find(ctx, hash); ok {
				return hash, nil
			}
		}
		return "", downloaders.ClientError("qBittorrent refused the torrent")
	}
	if hash == "" {
		// Added from a URL: qBittorrent does not return the hash. Find the newest
		// torrent in the category added just now.
		return c.findAddedFromURL(ctx, r.Category)
	}
	if r.Priority == downloaders.PriorityTop || r.Priority == downloaders.PriorityLast {
		_ = c.SetPriority(ctx, hash, r.Priority)
	}
	return hash, nil
}

func (c *Client) findAddedFromURL(ctx context.Context, category string) (string, error) {
	start := time.Now().Add(-time.Minute)
	for i := 0; i < 20; i++ {
		items, err := c.info(ctx, url.Values{"category": {category}, "sort": {"added_on"}, "reverse": {"true"}, "limit": {"1"}})
		if err == nil && len(items) > 0 && items[0].AddedOn >= start.Unix() {
			return downloaders.NormaliseHash(items[0].Hash), nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return "", downloaders.ClientError("the torrent was sent but did not appear in the client")
}

type torrent struct {
	Hash             string  `json:"hash"`
	Name             string  `json:"name"`
	State            string  `json:"state"`
	Category         string  `json:"category"`
	Tags             string  `json:"tags"`
	Progress         float64 `json:"progress"`
	Size             int64   `json:"size"`
	TotalSize        int64   `json:"total_size"`
	Downloaded       int64   `json:"downloaded"`
	Uploaded         int64   `json:"uploaded"`
	DLSpeed          int64   `json:"dlspeed"`
	UPSpeed          int64   `json:"upspeed"`
	ETA              int64   `json:"eta"`
	Ratio            float64 `json:"ratio"`
	SeedingTime      int64   `json:"seeding_time"`
	SavePath         string  `json:"save_path"`
	ContentPath      string  `json:"content_path"`
	AddedOn          int64   `json:"added_on"`
	NumSeeds         int     `json:"num_seeds"`
	RatioLimit       float64 `json:"ratio_limit"`
	SeedingTimeLimit int64   `json:"seeding_time_limit"`
	MaxRatio         float64 `json:"max_ratio"`
	MaxSeedingTime   int64   `json:"max_seeding_time"`
}

func (c *Client) info(ctx context.Context, q url.Values) ([]torrent, error) {
	b, err := c.get(ctx, "torrents/info", q)
	if err != nil {
		return nil, err
	}
	var list []torrent
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%w: torrent list is not JSON", downloaders.ErrWrongURL)
	}
	return list, nil
}

// etaInfinity is what qBittorrent reports for "unknown".
const etaInfinity = 8640000

func mapState(raw string) (downloaders.State, bool) {
	switch raw {
	case "error", "missingFiles":
		return downloaders.StateFailed, false
	case "uploading", "stalledUP", "forcedUP", "queuedUP":
		return downloaders.StateSeeding, false
	case "checkingUP":
		return downloaders.StateSeeding, true
	case "pausedUP", "stoppedUP":
		return downloaders.StateCompleted, false
	case "downloading", "forcedDL", "stalledDL":
		return downloaders.StateDownloading, false
	case "metaDL", "forcedMetaDL", "allocating":
		return downloaders.StateDownloading, true
	case "checkingDL", "checkingResumeData", "moving":
		return downloaders.StateQueued, true
	case "queuedDL":
		return downloaders.StateQueued, false
	case "pausedDL", "stoppedDL":
		return downloaders.StatePaused, false
	}
	return downloaders.StateWarning, false
}

func (t torrent) item() downloaders.Item {
	st, busy := mapState(t.State)
	if t.State == "moving" {
		busy = true
	}
	it := downloaders.Item{
		ID: downloaders.NormaliseHash(t.Hash), Name: t.Name, Category: t.Category, State: st, RawState: t.State, Busy: busy,
		Progress: t.Progress, Size: t.Size, Downloaded: t.Downloaded, Uploaded: t.Uploaded,
		DownloadSpeed: t.DLSpeed, UploadSpeed: t.UPSpeed, Ratio: t.Ratio,
		SeedingTime: time.Duration(t.SeedingTime) * time.Second, SavePath: t.SavePath, ContentPath: t.ContentPath,
		AddedAt: time.Unix(t.AddedOn, 0).UTC(), Seeds: t.NumSeeds,
	}
	if it.Progress > 1 {
		it.Progress = 1
	}
	if t.ETA >= 0 && t.ETA < etaInfinity && st == downloaders.StateDownloading {
		d := time.Duration(t.ETA) * time.Second
		it.ETA = &d
	}
	if t.Tags != "" {
		for _, tag := range strings.Split(t.Tags, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				it.Tags = append(it.Tags, tag)
			}
		}
	}
	switch t.State {
	case "missingFiles":
		it.Message = "files are missing"
	case "error":
		it.Message = "the client reported an error"
	}
	// A completed, stopped torrent whose limits are met finished seeding.
	if st == downloaders.StateCompleted {
		ratioMet := t.MaxRatio >= 0 && t.Ratio >= t.MaxRatio && t.MaxRatio > 0
		timeMet := t.MaxSeedingTime > 0 && t.SeedingTime >= t.MaxSeedingTime*60
		it.SeedGoalReached = ratioMet || timeMet
	}
	return it
}

// List implements downloaders.Client.
func (c *Client) List(ctx context.Context, category string) ([]downloaders.Item, error) {
	q := url.Values{}
	if category != "" {
		q.Set("category", category)
	}
	list, err := c.info(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]downloaders.Item, 0, len(list))
	for _, t := range list {
		out = append(out, t.item())
	}
	return out, nil
}

func (c *Client) find(ctx context.Context, id string) (torrent, bool, error) {
	list, err := c.info(ctx, url.Values{"hashes": {downloaders.NormaliseHash(id)}})
	if err != nil {
		return torrent{}, false, err
	}
	for _, t := range list {
		if downloaders.SameID(t.Hash, id) {
			return t, true, nil
		}
	}
	return torrent{}, false, nil
}

// Files implements downloaders.Client.
func (c *Client) Files(ctx context.Context, id string) ([]downloaders.File, error) {
	status, b, err := c.call(ctx, http.MethodGet, "torrents/files?"+url.Values{"hash": {downloaders.NormaliseHash(id)}}.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, downloaders.ErrItemNotFound
	}
	if status != http.StatusOK {
		return nil, downloaders.StatusError(status)
	}
	var files []struct {
		Name     string  `json:"name"`
		Size     int64   `json:"size"`
		Progress float64 `json:"progress"`
	}
	if err := json.Unmarshal(b, &files); err != nil {
		return nil, downloaders.ClientError("file list is not JSON")
	}
	out := make([]downloaders.File, 0, len(files))
	for _, f := range files {
		out = append(out, downloaders.File{Path: f.Name, Size: f.Size, Progress: f.Progress})
	}
	return out, nil
}

// mustExist returns ErrItemNotFound for an unknown hash (most actions silently ignore those).
func (c *Client) mustExist(ctx context.Context, id string) error {
	_, ok, err := c.find(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return downloaders.ErrItemNotFound
	}
	return nil
}

// Remove implements downloaders.Client.
func (c *Client) Remove(ctx context.Context, id string, deleteData bool) error {
	if err := c.mustExist(ctx, id); err != nil {
		return err
	}
	_, err := c.expectOK(ctx, "torrents/delete", url.Values{"hashes": {downloaders.NormaliseHash(id)}, "deleteFiles": {strconv.FormatBool(deleteData)}})
	return err
}

func (c *Client) toggle(ctx context.Context, id string, stop bool) error {
	if err := c.mustExist(ctx, id); err != nil {
		return err
	}
	v, err := c.webAPIVersion(ctx)
	if err != nil {
		return err
	}
	var path string
	switch newAPI := atLeast(v, 2, 11); {
	case stop && newAPI:
		path = "torrents/stop"
	case stop:
		path = "torrents/pause"
	case newAPI:
		path = "torrents/start"
	default:
		path = "torrents/resume"
	}
	_, err = c.expectOK(ctx, path, url.Values{"hashes": {downloaders.NormaliseHash(id)}})
	return err
}

// Pause implements downloaders.Client.
func (c *Client) Pause(ctx context.Context, id string) error { return c.toggle(ctx, id, true) }

// Resume implements downloaders.Client.
func (c *Client) Resume(ctx context.Context, id string) error { return c.toggle(ctx, id, false) }

// SetPriority implements downloaders.Client. With torrent queueing turned off
// qBittorrent answers 409; there is no queue to move in, so that is not an error.
func (c *Client) SetPriority(ctx context.Context, id string, p downloaders.Priority) error {
	if !p.Valid() {
		return fmt.Errorf("%w: unknown priority %q", downloaders.ErrInvalidRequest, p)
	}
	if p == downloaders.PriorityNormal {
		return nil
	}
	path := "torrents/topPrio"
	if p == downloaders.PriorityLast {
		path = "torrents/bottomPrio"
	}
	status, _, err := c.call(ctx, http.MethodPost, path, url.Values{"hashes": {downloaders.NormaliseHash(id)}}, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusConflict {
		return downloaders.StatusError(status)
	}
	return nil
}

// Categories implements downloaders.Client.
func (c *Client) Categories(ctx context.Context) ([]downloaders.Category, error) {
	b, err := c.get(ctx, "torrents/categories", nil)
	if err != nil {
		return nil, err
	}
	var m map[string]struct {
		Name     string `json:"name"`
		SavePath string `json:"savePath"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, downloaders.ClientError("category list is not JSON")
	}
	out := make([]downloaders.Category, 0, len(m))
	for k, v := range m {
		name := v.Name
		if name == "" {
			name = k
		}
		out = append(out, downloaders.Category{Name: name, SavePath: v.SavePath})
	}
	return out, nil
}

// EnsureCategory implements downloaders.Client.
func (c *Client) EnsureCategory(ctx context.Context, name, savePath string) error {
	cats, err := c.Categories(ctx)
	if err != nil {
		return err
	}
	for _, cat := range cats {
		if cat.Name == name {
			return nil
		}
	}
	status, _, err := c.call(ctx, http.MethodPost, "torrents/createCategory", url.Values{"category": {name}, "savePath": {savePath}}, nil)
	if err != nil {
		return err
	}
	if status == http.StatusConflict {
		// Exists already (created in the meantime) or the name is not valid.
		cats, err := c.Categories(ctx)
		if err == nil {
			for _, cat := range cats {
				if cat.Name == name {
					return nil
				}
			}
		}
		return downloaders.ClientError("qBittorrent refused the category name %q", name)
	}
	if status != http.StatusOK {
		return downloaders.StatusError(status)
	}
	return nil
}

var _ downloaders.Client = (*Client)(nil)
