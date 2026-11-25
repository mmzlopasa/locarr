// Package transmission is a client for the Transmission RPC protocol, written
// from its public specification.
package transmission

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

func init() {
	downloaders.Register(downloaders.Implementation{
		Name:           "transmission",
		DisplayName:    "Transmission",
		Protocol:       downloaders.Torrent,
		DefaultPort:    9091,
		DefaultURLBase: "/transmission",
		Fields: []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort, downloaders.FieldUseTLS, downloaders.FieldURLBase,
			downloaders.FieldUsername, downloaders.FieldPassword, downloaders.FieldAllowSelfSigned},
		Required:     []downloaders.Field{downloaders.FieldHost, downloaders.FieldPort},
		Capabilities: caps,
		New:          func(c downloaders.Config) (downloaders.Client, error) { return New(c) },
	})
}

// Categories are labels: they need no creation.
var caps = downloaders.Capabilities{
	Protocol: downloaders.Torrent, Categories: true, SeedLimits: true, Priority: true, SavePath: true,
	FreeSpace: true, DeletesData: true,
}

// sessionHeader carries the anti-CSRF session ID the server hands out with a 409.
const sessionHeader = "X-Transmission-Session-Id"

// Client talks to one Transmission instance.
type Client struct {
	endpoint string
	cfg      downloaders.Config
	http     *http.Client

	mu        sync.Mutex
	sessionID string
}

// New returns a client for cfg. The RPC endpoint is <URL base>/rpc; an empty
// URL base means the default "/transmission".
func New(cfg downloaders.Config) (*Client, error) {
	if strings.Trim(cfg.URLBase, "/ ") == "" {
		cfg.URLBase = "/transmission"
	}
	base, err := cfg.BaseURL()
	if err != nil {
		return nil, err
	}
	return &Client{endpoint: strings.TrimSuffix(base.String(), "/") + "/rpc", cfg: cfg, http: cfg.Client()}, nil
}

// Capabilities implements downloaders.Client.
func (c *Client) Capabilities() downloaders.Capabilities { return caps }

type rpcResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

// call runs one RPC method and decodes its arguments into out.
func (c *Client) call(ctx context.Context, method string, args any, out any) error {
	body, err := json.Marshal(map[string]any{"method": method, "arguments": args})
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		c.mu.Lock()
		if c.sessionID != "" {
			req.Header.Set(sessionHeader, c.sessionID)
		}
		c.mu.Unlock()
		if c.cfg.Username != "" || c.cfg.Password != "" {
			req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return downloaders.ClassifyTransportError(err)
		}
		b, rerr := downloaders.ReadLimited(resp.Body, downloaders.MaxResponse)
		_ = resp.Body.Close()
		if rerr != nil {
			return rerr
		}
		if resp.StatusCode == http.StatusConflict {
			id := resp.Header.Get(sessionHeader)
			if id == "" {
				return fmt.Errorf("%w: 409 without a session ID", downloaders.ErrWrongURL)
			}
			c.mu.Lock()
			c.sessionID = id
			c.mu.Unlock()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return downloaders.StatusError(resp.StatusCode)
		}
		var r rpcResponse
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%w: answer is not Transmission RPC", downloaders.ErrWrongURL)
		}
		if r.Result != "success" {
			return downloaders.ClientError("%s", r.Result)
		}
		if out != nil && len(r.Arguments) > 0 {
			if err := json.Unmarshal(r.Arguments, out); err != nil {
				return downloaders.ClientError("unexpected %s answer", method)
			}
		}
		return nil
	}
	return fmt.Errorf("%w: session ID handshake failed", downloaders.ErrWrongURL)
}

// Test implements downloaders.Client.
func (c *Client) Test(ctx context.Context) (downloaders.Status, error) {
	var s struct {
		Version     string `json:"version"`
		RPCVersion  int    `json:"rpc-version"`
		DownloadDir string `json:"download-dir"`
	}
	if err := c.call(ctx, "session-get", map[string]any{"fields": []string{"version", "rpc-version", "download-dir"}}, &s); err != nil {
		return downloaders.Status{}, err
	}
	if s.Version == "" {
		return downloaders.Status{}, fmt.Errorf("%w: no version in the answer", downloaders.ErrWrongURL)
	}
	if s.RPCVersion != 0 && s.RPCVersion < 16 {
		return downloaders.Status{}, downloaders.ClientError("Transmission %s is too old (3.00 or later is needed for labels)", s.Version)
	}
	st := downloaders.Status{Version: s.Version, SavePath: s.DownloadDir}
	if s.DownloadDir != "" {
		var fs struct {
			Size int64 `json:"size-bytes"`
		}
		if c.call(ctx, "free-space", map[string]any{"path": s.DownloadDir}, &fs) == nil && fs.Size >= 0 {
			st.FreeSpace = &fs.Size
		}
	}
	return st, nil
}

// Add implements downloaders.Client.
func (c *Client) Add(ctx context.Context, r downloaders.AddRequest) (string, error) {
	if err := r.Validate(downloaders.Torrent); err != nil {
		return "", err
	}
	args := map[string]any{"paused": r.Paused}
	switch {
	case r.Magnet != "":
		if _, err := downloaders.ParseMagnet(r.Magnet); err != nil {
			return "", err
		}
		args["filename"] = r.Magnet
	case len(r.TorrentFile) > 0:
		if _, err := downloaders.ParseTorrent(r.TorrentFile); err != nil {
			return "", err
		}
		args["metainfo"] = base64.StdEncoding.EncodeToString(r.TorrentFile)
	default:
		args["filename"] = r.TorrentURL
	}
	if r.SavePath != "" {
		args["download-dir"] = r.SavePath
	}
	if r.Category != "" {
		args["labels"] = []string{r.Category}
	}
	var res struct {
		Added *struct {
			Hash string `json:"hashString"`
			ID   int64  `json:"id"`
		} `json:"torrent-added"`
		Duplicate *struct {
			Hash string `json:"hashString"`
			ID   int64  `json:"id"`
		} `json:"torrent-duplicate"`
	}
	if err := c.call(ctx, "torrent-add", args, &res); err != nil {
		return "", err
	}
	var hash string
	switch {
	case res.Added != nil:
		hash = res.Added.Hash
	case res.Duplicate != nil:
		hash = res.Duplicate.Hash
	default:
		return "", downloaders.ClientError("torrent-add returned no torrent")
	}
	hash = downloaders.NormaliseHash(hash)
	set := map[string]any{"ids": []string{hash}}
	if r.Category != "" {
		set["labels"] = []string{r.Category}
	}
	if r.SeedRatio != nil {
		set["seedRatioLimit"] = *r.SeedRatio
		set["seedRatioMode"] = 1 // this torrent's own limit
	}
	if r.SeedTime != nil {
		set["seedIdleLimit"] = int(r.SeedTime.Minutes())
		set["seedIdleMode"] = 1
	}
	if len(set) > 1 {
		if err := c.call(ctx, "torrent-set", set, nil); err != nil {
			return hash, err
		}
	}
	if r.Priority == downloaders.PriorityTop || r.Priority == downloaders.PriorityLast {
		_ = c.SetPriority(ctx, hash, r.Priority)
	}
	return hash, nil
}

var fields = []string{"id", "hashString", "name", "status", "percentDone", "totalSize", "sizeWhenDone", "leftUntilDone",
	"downloadedEver", "uploadedEver", "rateDownload", "rateUpload", "eta", "uploadRatio", "secondsSeeding", "downloadDir",
	"error", "errorString", "labels", "addedDate", "isFinished", "peersSendingToUs", "recheckProgress", "metadataPercentComplete"}

type torrent struct {
	Hash          string   `json:"hashString"`
	Name          string   `json:"name"`
	Status        int      `json:"status"`
	PercentDone   float64  `json:"percentDone"`
	TotalSize     int64    `json:"totalSize"`
	SizeWhenDone  int64    `json:"sizeWhenDone"`
	LeftUntilDone int64    `json:"leftUntilDone"`
	Downloaded    int64    `json:"downloadedEver"`
	Uploaded      int64    `json:"uploadedEver"`
	RateDownload  int64    `json:"rateDownload"`
	RateUpload    int64    `json:"rateUpload"`
	ETA           int64    `json:"eta"`
	Ratio         float64  `json:"uploadRatio"`
	SecondsSeed   int64    `json:"secondsSeeding"`
	DownloadDir   string   `json:"downloadDir"`
	Error         int      `json:"error"`
	ErrorString   string   `json:"errorString"`
	Labels        []string `json:"labels"`
	AddedDate     int64    `json:"addedDate"`
	IsFinished    bool     `json:"isFinished"`
	Peers         int      `json:"peersSendingToUs"`
	Metadata      float64  `json:"metadataPercentComplete"`
	Files         []struct {
		Name           string `json:"name"`
		Length         int64  `json:"length"`
		BytesCompleted int64  `json:"bytesCompleted"`
	} `json:"files"`
}

// Status values from the RPC specification.
const (
	statusStopped = iota
	statusCheckWait
	statusCheck
	statusDownloadWait
	statusDownload
	statusSeedWait
	statusSeed
)

var rawStatus = []string{"stopped", "check pending", "checking", "download pending", "downloading", "seed pending", "seeding"}

func (t torrent) item() downloaders.Item {
	raw := "unknown"
	if t.Status >= 0 && t.Status < len(rawStatus) {
		raw = rawStatus[t.Status]
	}
	done := t.LeftUntilDone == 0 && t.PercentDone >= 1
	it := downloaders.Item{
		ID: downloaders.NormaliseHash(t.Hash), Name: t.Name, RawState: raw, Progress: t.PercentDone,
		Size: t.SizeWhenDone, Downloaded: t.Downloaded, Uploaded: t.Uploaded, DownloadSpeed: t.RateDownload,
		UploadSpeed: t.RateUpload, Ratio: max(t.Ratio, 0), SeedingTime: time.Duration(t.SecondsSeed) * time.Second,
		SavePath: t.DownloadDir, ContentPath: path.Join(t.DownloadDir, t.Name), AddedAt: time.Unix(t.AddedDate, 0).UTC(),
		Seeds: t.Peers, Tags: t.Labels,
	}
	if len(t.Labels) > 0 {
		it.Category = t.Labels[0]
	}
	switch t.Status {
	case statusStopped:
		switch {
		case done && t.IsFinished:
			it.State, it.SeedGoalReached = downloaders.StateCompleted, true
		case done:
			it.State = downloaders.StateCompleted
		default:
			it.State = downloaders.StatePaused
		}
	case statusCheckWait, statusCheck:
		it.State, it.Busy = downloaders.StateQueued, true
		if done {
			it.State = downloaders.StateSeeding
		}
	case statusDownloadWait:
		it.State = downloaders.StateQueued
	case statusDownload:
		it.State = downloaders.StateDownloading
		it.Busy = t.Metadata < 1 && t.Metadata > 0
	case statusSeedWait, statusSeed:
		it.State = downloaders.StateSeeding
	default:
		it.State = downloaders.StateWarning
	}
	switch t.Error {
	case 1, 2: // tracker warning / tracker error
		if it.State != downloaders.StateSeeding && it.State != downloaders.StateCompleted {
			it.State = downloaders.StateWarning
		}
		it.Message = t.ErrorString
	case 3: // local error (missing files, disk full …)
		it.State = downloaders.StateFailed
		it.Message = t.ErrorString
	}
	if it.State == downloaders.StateDownloading && t.ETA >= 0 {
		d := time.Duration(t.ETA) * time.Second
		it.ETA = &d
	}
	return it
}

func (c *Client) get(ctx context.Context, ids []string, extra ...string) ([]torrent, error) {
	args := map[string]any{"fields": append(slices.Clone(fields), extra...)}
	if ids != nil {
		args["ids"] = ids
	}
	var res struct {
		Torrents []torrent `json:"torrents"`
	}
	if err := c.call(ctx, "torrent-get", args, &res); err != nil {
		return nil, err
	}
	return res.Torrents, nil
}

// List implements downloaders.Client.
func (c *Client) List(ctx context.Context, category string) ([]downloaders.Item, error) {
	list, err := c.get(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]downloaders.Item, 0, len(list))
	for _, t := range list {
		if category != "" && !slices.Contains(t.Labels, category) {
			continue
		}
		out = append(out, t.item())
	}
	return out, nil
}

func (c *Client) one(ctx context.Context, id string, extra ...string) (torrent, error) {
	list, err := c.get(ctx, []string{downloaders.NormaliseHash(id)}, extra...)
	if err != nil {
		return torrent{}, err
	}
	for _, t := range list {
		if downloaders.SameID(t.Hash, id) {
			return t, nil
		}
	}
	return torrent{}, downloaders.ErrItemNotFound
}

// Files implements downloaders.Client.
func (c *Client) Files(ctx context.Context, id string) ([]downloaders.File, error) {
	t, err := c.one(ctx, id, "files")
	if err != nil {
		return nil, err
	}
	out := make([]downloaders.File, 0, len(t.Files))
	for _, f := range t.Files {
		p := 0.0
		if f.Length > 0 {
			p = float64(f.BytesCompleted) / float64(f.Length)
		}
		out = append(out, downloaders.File{Path: f.Name, Size: f.Length, Progress: p})
	}
	return out, nil
}

// act runs a method that takes "ids" on one existing torrent.
func (c *Client) act(ctx context.Context, method, id string, extra map[string]any) error {
	if _, err := c.one(ctx, id); err != nil {
		return err
	}
	args := map[string]any{"ids": []string{downloaders.NormaliseHash(id)}}
	for k, v := range extra {
		args[k] = v
	}
	return c.call(ctx, method, args, nil)
}

// Remove implements downloaders.Client.
func (c *Client) Remove(ctx context.Context, id string, deleteData bool) error {
	return c.act(ctx, "torrent-remove", id, map[string]any{"delete-local-data": deleteData})
}

// Pause implements downloaders.Client.
func (c *Client) Pause(ctx context.Context, id string) error {
	return c.act(ctx, "torrent-stop", id, nil)
}

// Resume implements downloaders.Client.
func (c *Client) Resume(ctx context.Context, id string) error {
	return c.act(ctx, "torrent-start", id, nil)
}

// SetPriority implements downloaders.Client.
func (c *Client) SetPriority(ctx context.Context, id string, p downloaders.Priority) error {
	switch p {
	case downloaders.PriorityTop:
		return c.act(ctx, "queue-move-top", id, nil)
	case downloaders.PriorityLast:
		return c.act(ctx, "queue-move-bottom", id, nil)
	case downloaders.PriorityNormal:
		return nil
	}
	return fmt.Errorf("%w: unknown priority %q", downloaders.ErrInvalidRequest, p)
}

// Categories implements downloaders.Client: the labels in use.
func (c *Client) Categories(ctx context.Context) ([]downloaders.Category, error) {
	list, err := c.get(ctx, nil)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, t := range list {
		for _, l := range t.Labels {
			seen[l] = true
		}
	}
	out := make([]downloaders.Category, 0, len(seen))
	for l := range seen {
		out = append(out, downloaders.Category{Name: l})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// EnsureCategory implements downloaders.Client: labels exist as soon as a torrent carries them.
func (c *Client) EnsureCategory(context.Context, string, string) error { return nil }

var _ downloaders.Client = (*Client)(nil)
