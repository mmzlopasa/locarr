// Package sabnzbd is the SABnzbd client, written from SABnzbd's public API
// documentation: every call is an HTTP request to <base>/api with a mode, the
// API key and output=json.
package sabnzbd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

// Name is the implementation identifier.
const Name = "sabnzbd"

var caps = downloaders.Capabilities{
	Protocol:         downloaders.Usenet,
	Categories:       true,
	CreateCategories: true,
	Priority:         true,
	FreeSpace:        true,
	DeletesData:      true,
}

func init() {
	downloaders.Register(downloaders.Implementation{
		Name:        Name,
		DisplayName: "SABnzbd",
		Protocol:    downloaders.Usenet,
		DefaultPort: 8080,
		Fields: []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldUseTLS,
			downloaders.FieldURLBase, downloaders.FieldAPIKey, downloaders.FieldAllowSelfSigned},
		Required:     []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldAPIKey},
		Capabilities: caps,
		New:          func(c downloaders.Config) (downloaders.Client, error) { return New(c) },
	})
}

// SAB priorities (API values).
const (
	prioDefault = -100
	prioPaused  = -2
	prioLow     = -1
	prioNormal  = 0
	prioHigh    = 1
)

// historyLimit bounds how much history is read per poll.
const historyLimit = 1000

// Client talks to one SABnzbd instance.
type Client struct {
	base   *url.URL
	apiKey string
	http   *http.Client
}

// New returns a client for cfg.
func New(cfg downloaders.Config) (*Client, error) {
	base, err := cfg.BaseURL()
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, fmt.Errorf("%w: SABnzbd needs an API key", downloaders.ErrInvalidRequest)
	}
	return &Client{base: base, apiKey: key, http: cfg.Client()}, nil
}

// Capabilities implements downloaders.Client.
func (c *Client) Capabilities() downloaders.Capabilities { return caps }

func (c *Client) apiURL(mode string, params url.Values) string {
	q := url.Values{}
	for k, v := range params {
		q[k] = v
	}
	q.Set("mode", mode)
	q.Set("output", "json")
	q.Set("apikey", c.apiKey)
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api"
	u.RawQuery = q.Encode()
	return u.String()
}

// call runs one API request and decodes the JSON answer into out (may be nil).
func (c *Client) call(ctx context.Context, method, mode string, params url.Values, body []byte, contentType string, out any) error {
	var rd *bytes.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if rd != nil {
		req, err = http.NewRequestWithContext(ctx, method, c.apiURL(mode, params), rd)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, c.apiURL(mode, params), nil)
	}
	if err != nil {
		return fmt.Errorf("%w: cannot build request", downloaders.ErrInvalidRequest)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
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
	if bytes.Contains(raw, []byte("Hostname verification failed")) {
		return downloaders.ClientError("SABnzbd refused the host name Locarr used; add it to host_whitelist in SABnzbd (Config → Special) or connect by IP address")
	}
	if resp.StatusCode != http.StatusOK {
		return downloaders.StatusError(resp.StatusCode)
	}
	// A missing or wrong key is answered in plain text, even with output=json.
	if text := strings.ToLower(strings.TrimSpace(string(raw))); strings.HasPrefix(text, "api key") {
		return fmt.Errorf("%w: SABnzbd says %q", downloaders.ErrAuth, strings.TrimSpace(string(raw)))
	}
	var status struct {
		Status json.RawMessage `json:"status"`
		Error  any             `json:"error"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return fmt.Errorf("%w: the answer is not SABnzbd JSON", downloaders.ErrWrongURL)
	}
	if msg, ok := status.Error.(string); ok && msg != "" {
		if strings.Contains(strings.ToLower(msg), "api key") {
			return fmt.Errorf("%w: %s", downloaders.ErrAuth, msg)
		}
		return downloaders.ClientError("%s", msg)
	}
	if string(status.Status) == "false" {
		return downloaders.ClientError("SABnzbd refused the request")
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%w: unexpected SABnzbd answer", downloaders.ErrClient)
		}
	}
	return nil
}

func (c *Client) get(ctx context.Context, mode string, params url.Values, out any) error {
	return c.call(ctx, http.MethodGet, mode, params, nil, "", out)
}

// Test implements downloaders.Client.
func (c *Client) Test(ctx context.Context) (downloaders.Status, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, "version", nil, &v); err != nil {
		return downloaders.Status{}, err
	}
	if v.Version == "" {
		return downloaders.Status{}, fmt.Errorf("%w: no SABnzbd version in the answer", downloaders.ErrWrongURL)
	}
	// mode=version needs no key; the queue does, so this checks the key.
	q, err := c.queue(ctx)
	if err != nil {
		return downloaders.Status{}, err
	}
	st := downloaders.Status{Version: v.Version}
	if gb, err := strconv.ParseFloat(strings.TrimSpace(q.DiskSpace1), 64); err == nil && gb >= 0 {
		free := int64(gb * (1 << 30))
		st.FreeSpace = &free
	}
	var cfg struct {
		Config struct {
			Misc struct {
				CompleteDir string `json:"complete_dir"`
			} `json:"misc"`
		} `json:"config"`
	}
	// A relative folder is relative to SABnzbd's own base folder, which the API does not give.
	if c.get(ctx, "get_config", url.Values{"section": {"misc"}, "keyword": {"complete_dir"}}, &cfg) == nil &&
		(strings.HasPrefix(cfg.Config.Misc.CompleteDir, "/") || (len(cfg.Config.Misc.CompleteDir) > 2 && cfg.Config.Misc.CompleteDir[1] == ':')) {
		st.SavePath = cfg.Config.Misc.CompleteDir
	}
	return st, nil
}

type queueSlot struct {
	NzoID      string `json:"nzo_id"`
	Filename   string `json:"filename"`
	Status     string `json:"status"`
	Cat        string `json:"cat"`
	MB         string `json:"mb"`
	MBLeft     string `json:"mbleft"`
	SizeText   string `json:"size"`
	SizeLeft   string `json:"sizeleft"`
	Percentage string `json:"percentage"`
	TimeLeft   string `json:"timeleft"`
	Priority   any    `json:"priority"`
	Labels     []any  `json:"labels"`
}

type queueData struct {
	Status     string      `json:"status"`
	Paused     bool        `json:"paused"`
	KBPerSec   string      `json:"kbpersec"`
	DiskSpace1 string      `json:"diskspace1"`
	Slots      []queueSlot `json:"slots"`
}

func (c *Client) queue(ctx context.Context) (queueData, error) {
	var out struct {
		Queue queueData `json:"queue"`
	}
	if err := c.get(ctx, "queue", nil, &out); err != nil {
		return queueData{}, err
	}
	return out.Queue, nil
}

type historySlot struct {
	NzoID       string `json:"nzo_id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Status      string `json:"status"`
	FailMessage string `json:"fail_message"`
	Bytes       int64  `json:"bytes"`
	Storage     string `json:"storage"`
	Path        string `json:"path"`
	Completed   int64  `json:"completed"`
	TimeAdded   int64  `json:"time_added"`
}

func (c *Client) history(ctx context.Context, category string) ([]historySlot, error) {
	p := url.Values{"start": {"0"}, "limit": {strconv.Itoa(historyLimit)}}
	if category != "" {
		p.Set("category", category)
	}
	var out struct {
		History struct {
			Slots []historySlot `json:"slots"`
		} `json:"history"`
	}
	if err := c.get(ctx, "history", p, &out); err != nil {
		return nil, err
	}
	return out.History.Slots, nil
}

// mb parses SABnzbd's megabyte strings into bytes.
func mb(s string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 {
		return 0
	}
	return int64(f * (1 << 20))
}

// sizeText parses SABnzbd's human sizes ("1000 B", "1.2 GB") into bytes.
func sizeText(s string) int64 {
	num, unit, ok := strings.Cut(strings.TrimSpace(s), " ")
	if !ok {
		return 0
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 {
		return 0
	}
	mult := map[string]float64{"B": 1, "K": 1 << 10, "KB": 1 << 10, "M": 1 << 20, "MB": 1 << 20, "G": 1 << 30, "GB": 1 << 30, "T": 1 << 40, "TB": 1 << 40}
	m, ok := mult[strings.ToUpper(unit)]
	if !ok {
		return 0
	}
	return int64(f * m)
}

// parseTimeLeft reads "H:MM:SS" or "D:HH:MM:SS".
func parseTimeLeft(s string) (time.Duration, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 4 {
		return 0, false
	}
	mult := []time.Duration{time.Second, time.Minute, time.Hour, 24 * time.Hour}
	var d time.Duration
	for i := range parts {
		n, err := strconv.Atoi(parts[len(parts)-1-i])
		if err != nil || n < 0 {
			return 0, false
		}
		d += time.Duration(n) * mult[i]
	}
	return d, true
}

func category(cat string) string {
	if cat == "*" {
		return ""
	}
	return cat
}

func queueItem(s queueSlot, speed int64) downloaders.Item {
	it := downloaders.Item{
		ID:       s.NzoID,
		Name:     s.Filename,
		Category: category(s.Cat),
		RawState: s.Status,
		Size:     mb(s.MB),
		Seeds:    -1,
	}
	left := mb(s.MBLeft)
	if it.Size == 0 {
		// Jobs under 10 KB show 0.00 MB; the text size is exact.
		it.Size, left = sizeText(s.SizeText), sizeText(s.SizeLeft)
	}
	it.Downloaded = max(it.Size-left, 0)
	if it.Size > 0 {
		it.Progress = float64(it.Downloaded) / float64(it.Size)
	} else if p, err := strconv.ParseFloat(s.Percentage, 64); err == nil {
		it.Progress = p / 100
	}
	it.Progress = min(max(it.Progress, 0), 1)
	switch strings.ToLower(s.Status) {
	case "downloading":
		it.State = downloaders.StateDownloading
		it.DownloadSpeed = speed
		if d, ok := parseTimeLeft(s.TimeLeft); ok {
			it.ETA = &d
		}
	case "paused":
		it.State = downloaders.StatePaused
	case "checking", "quickcheck", "verifying", "repairing", "extracting", "moving", "running":
		it.State = downloaders.StateDownloading
		it.Busy = true
	default: // Queued, Fetching, Grabbing, Propagating
		it.State = downloaders.StateQueued
	}
	return it
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

func historyItem(s historySlot) downloaders.Item {
	it := downloaders.Item{
		ID:         s.NzoID,
		Name:       s.Name,
		Category:   category(s.Category),
		RawState:   s.Status,
		Size:       s.Bytes,
		Downloaded: s.Bytes,
		Progress:   1,
		Seeds:      -1,
	}
	if s.TimeAdded > 0 {
		it.AddedAt = time.Unix(s.TimeAdded, 0).UTC()
	}
	switch strings.ToLower(s.Status) {
	case "completed":
		it.State = downloaders.StateCompleted
		it.ContentPath = s.Storage
		if it.ContentPath != "" {
			it.SavePath = parentDir(it.ContentPath)
		}
	case "failed":
		it.State = downloaders.StateFailed
		it.Message = s.FailMessage
		if it.Message == "" {
			it.Message = "download failed"
		}
		it.ContentPath = s.Storage
	default: // Queued, QuickCheck, Verifying, Repairing, Fetching, Extracting, Moving, Running
		it.State = downloaders.StateDownloading
		it.Busy = true
		it.Message = s.FailMessage
	}
	return it
}

// List implements downloaders.Client.
func (c *Client) List(ctx context.Context, cat string) ([]downloaders.Item, error) {
	q, err := c.queue(ctx)
	if err != nil {
		return nil, err
	}
	h, err := c.history(ctx, cat)
	if err != nil {
		return nil, err
	}
	speed := int64(0)
	if kb, err := strconv.ParseFloat(strings.TrimSpace(q.KBPerSec), 64); err == nil && kb > 0 {
		speed = int64(kb * 1024)
	}
	out := make([]downloaders.Item, 0, len(q.Slots)+len(h))
	match := func(c string) bool { return cat == "" || strings.EqualFold(c, cat) }
	for _, s := range q.Slots {
		it := queueItem(s, speed)
		if match(it.Category) {
			out = append(out, it)
		}
	}
	for _, s := range h {
		it := historyItem(s)
		if match(it.Category) {
			out = append(out, it)
		}
	}
	return out, nil
}

// where reports whether id is in the queue or in the history.
func (c *Client) where(ctx context.Context, id string) (inQueue, inHistory bool, err error) {
	q, err := c.queue(ctx)
	if err != nil {
		return false, false, err
	}
	for _, s := range q.Slots {
		if s.NzoID == id {
			return true, false, nil
		}
	}
	h, err := c.history(ctx, "")
	if err != nil {
		return false, false, err
	}
	for _, s := range h {
		if s.NzoID == id {
			return false, true, nil
		}
	}
	return false, false, nil
}

// Files implements downloaders.Client. SABnzbd lists files only for queued jobs;
// a finished job's files are in its storage folder.
func (c *Client) Files(ctx context.Context, id string) ([]downloaders.File, error) {
	inQueue, inHistory, err := c.where(ctx, id)
	if err != nil {
		return nil, err
	}
	if inHistory {
		return nil, nil
	}
	if !inQueue {
		return nil, downloaders.ErrItemNotFound
	}
	var out struct {
		Files []struct {
			Filename string `json:"filename"`
			MB       string `json:"mb"`
			MBLeft   string `json:"mbleft"`
			Bytes    string `json:"bytes"`
		} `json:"files"`
	}
	if err := c.get(ctx, "get_files", url.Values{"value": {id}}, &out); err != nil {
		return nil, err
	}
	files := make([]downloaders.File, 0, len(out.Files))
	for _, f := range out.Files {
		size := mb(f.MB)
		if b, err := strconv.ParseFloat(f.Bytes, 64); err == nil && b > 0 {
			size = int64(b)
		}
		p := 0.0
		if size > 0 {
			p = min(max(float64(size-mb(f.MBLeft))/float64(size), 0), 1)
		}
		files = append(files, downloaders.File{Path: f.Filename, Size: size, Progress: p})
	}
	return files, nil
}

type addResult struct {
	NzoIDs []string `json:"nzo_ids"`
}

func addPriority(r downloaders.AddRequest) int {
	switch r.Priority {
	case downloaders.PriorityTop:
		return prioHigh
	case downloaders.PriorityLast:
		return prioLow
	}
	if r.Paused {
		return prioPaused
	}
	return prioDefault
}

// Add implements downloaders.Client.
func (c *Client) Add(ctx context.Context, r downloaders.AddRequest) (string, error) {
	if err := r.Validate(downloaders.Usenet); err != nil {
		return "", err
	}
	p := url.Values{"priority": {strconv.Itoa(addPriority(r))}}
	if r.Category != "" {
		p.Set("cat", r.Category)
	}
	if r.Name != "" {
		p.Set("nzbname", r.Name)
	}
	var res addResult
	if r.NZBURL != "" {
		p.Set("name", r.NZBURL)
		if err := c.get(ctx, "addurl", p, &res); err != nil {
			return "", err
		}
	} else {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fname := r.Name
		if fname == "" {
			fname = "locarr"
		}
		fw, err := mw.CreateFormFile("name", sanitizeFilename(fname)+".nzb")
		if err != nil {
			return "", err
		}
		if _, err := fw.Write(r.NZB); err != nil {
			return "", err
		}
		if err := mw.Close(); err != nil {
			return "", err
		}
		if err := c.call(ctx, http.MethodPost, "addfile", p, buf.Bytes(), mw.FormDataContentType(), &res); err != nil {
			return "", err
		}
	}
	if len(res.NzoIDs) == 0 || res.NzoIDs[0] == "" {
		return "", downloaders.ClientError("SABnzbd did not accept the NZB")
	}
	id := res.NzoIDs[0]
	if r.Paused && r.Priority != downloaders.PriorityNormal {
		// The paused priority would replace the requested one, so pause separately.
		if err := c.Pause(ctx, id); err != nil {
			return id, err
		}
	}
	return id, nil
}

func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
}

// queueAction runs mode=queue&name=<action>&value=<id> and checks that SABnzbd
// knew the job.
func (c *Client) queueAction(ctx context.Context, action, id string, extra url.Values) error {
	p := url.Values{"name": {action}, "value": {id}}
	for k, v := range extra {
		p[k] = v
	}
	var res addResult
	if err := c.get(ctx, "queue", p, &res); err != nil {
		return err
	}
	return nil
}

// requireQueued returns ErrItemNotFound unless id is in the queue.
func (c *Client) requireQueued(ctx context.Context, id string) error {
	inQueue, _, err := c.where(ctx, id)
	if err != nil {
		return err
	}
	if !inQueue {
		return downloaders.ErrItemNotFound
	}
	return nil
}

// Remove implements downloaders.Client.
func (c *Client) Remove(ctx context.Context, id string, deleteData bool) error {
	inQueue, inHistory, err := c.where(ctx, id)
	if err != nil {
		return err
	}
	p := url.Values{"name": {"delete"}, "value": {id}}
	if deleteData {
		p.Set("del_files", "1")
	}
	switch {
	case inQueue:
		return c.get(ctx, "queue", p, nil)
	case inHistory:
		return c.get(ctx, "history", p, nil)
	}
	return downloaders.ErrItemNotFound
}

// Pause implements downloaders.Client.
func (c *Client) Pause(ctx context.Context, id string) error {
	if err := c.requireQueued(ctx, id); err != nil {
		return err
	}
	return c.queueAction(ctx, "pause", id, nil)
}

// Resume implements downloaders.Client.
func (c *Client) Resume(ctx context.Context, id string) error {
	if err := c.requireQueued(ctx, id); err != nil {
		return err
	}
	return c.queueAction(ctx, "resume", id, nil)
}

// SetPriority implements downloaders.Client.
func (c *Client) SetPriority(ctx context.Context, id string, p downloaders.Priority) error {
	if !p.Valid() {
		return fmt.Errorf("%w: unknown priority %q", downloaders.ErrInvalidRequest, p)
	}
	if err := c.requireQueued(ctx, id); err != nil {
		return err
	}
	v := prioNormal
	switch p {
	case downloaders.PriorityTop:
		v = prioHigh
	case downloaders.PriorityLast:
		v = prioLow
	}
	return c.queueAction(ctx, "priority", id, url.Values{"value2": {strconv.Itoa(v)}})
}

// Categories implements downloaders.Client.
func (c *Client) Categories(ctx context.Context) ([]downloaders.Category, error) {
	var out struct {
		Config struct {
			Categories []struct {
				Name string `json:"name"`
				Dir  string `json:"dir"`
			} `json:"categories"`
		} `json:"config"`
	}
	if err := c.get(ctx, "get_config", url.Values{"section": {"categories"}}, &out); err != nil {
		return nil, err
	}
	cats := make([]downloaders.Category, 0, len(out.Config.Categories))
	for _, x := range out.Config.Categories {
		if x.Name == "" || x.Name == "*" {
			continue
		}
		cats = append(cats, downloaders.Category{Name: x.Name, SavePath: x.Dir})
	}
	return cats, nil
}

// EnsureCategory implements downloaders.Client.
func (c *Client) EnsureCategory(ctx context.Context, name, savePath string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "*" {
		return fmt.Errorf("%w: category name is required", downloaders.ErrInvalidRequest)
	}
	cats, err := c.Categories(ctx)
	if err != nil {
		return err
	}
	for _, x := range cats {
		if strings.EqualFold(x.Name, name) {
			return nil
		}
	}
	p := url.Values{"section": {"categories"}, "keyword": {name}}
	if savePath != "" {
		p.Set("dir", savePath)
	}
	return c.get(ctx, "set_config", p, nil)
}
