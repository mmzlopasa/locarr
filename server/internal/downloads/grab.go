package downloads

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

// Release file size caps.
const (
	MaxTorrentFile = 10 << 20
	MaxNZBFile     = 64 << 20
	fetchTimeout   = 60 * time.Second
	// recentAge decides between a client's recent and older priority.
	recentAge = 14 * 24 * time.Hour
)

// GrabRequest sends one release to a download client.
type GrabRequest struct {
	// DownloadURL is an http(s) link to a .torrent or .nzb, or a magnet link.
	DownloadURL string
	Protocol    downloaders.Protocol
	Title       string
	IndexerID   *int64
	Indexer     string
	GUID        string
	// ItemRef is opaque JSON for the importer (which movie, episodes, album).
	ItemRef   json.RawMessage
	MediaKind string
	// Tags select clients: a client with tags only takes releases sharing one.
	Tags []string
	// ClientID forces one client.
	ClientID *int64
	// SeedRatio and SeedTime override the client's seeding goals (indexer settings).
	SeedRatio   *float64
	SeedTime    *time.Duration
	PublishedAt *time.Time
}

// Grabber is what search pages and importers use to send a release to a client.
type Grabber interface {
	Grab(ctx context.Context, req GrabRequest) (Download, error)
}

var _ Grabber = (*Service)(nil)

// pickClients returns the clients to try, best first: enabled, same protocol,
// tag rule, then priority; clients of equal priority take turns.
func (s *Service) pickClients(ctx context.Context, req GrabRequest) ([]Client, error) {
	all, err := s.Clients(ctx)
	if err != nil {
		return nil, err
	}
	if req.ClientID != nil {
		for _, c := range all {
			if c.ID == *req.ClientID {
				if c.Protocol != req.Protocol {
					return nil, invalid("client %q is a %s client", c.Name, c.Protocol)
				}
				return []Client{c}, nil
			}
		}
		return nil, ErrNotFound
	}
	tags := make([]string, 0, len(req.Tags))
	for _, t := range req.Tags {
		tags = append(tags, strings.ToLower(strings.TrimSpace(t)))
	}
	var out []Client
	for _, c := range all {
		if !c.Enabled || c.Protocol != req.Protocol {
			continue
		}
		if len(c.Tags) > 0 && !slices.ContainsFunc(c.Tags, func(t string) bool { return slices.Contains(tags, t) }) {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, ErrNoClient
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Priority < out[b].Priority })
	// Round-robin within the best priority group.
	n := 0
	for n < len(out) && out[n].Priority == out[0].Priority {
		n++
	}
	s.mu.Lock()
	s.init()
	key := string(req.Protocol) + "/" + strings.Join(tags, ",")
	turn := s.rr[key] % n
	s.rr[key]++
	s.mu.Unlock()
	group := append(append([]Client{}, out[turn:n]...), out[:turn]...)
	return append(group, out[n:]...), nil
}

// fetched is a release file (or magnet) ready for a client.
type fetched struct {
	magnet  string
	torrent []byte
	nzb     []byte
	hash    string
}

// fetchRelease downloads the release through the server: size capped, timed
// out, guarded against private-metadata addresses, no redirects to other hosts
// (except to a magnet link, which indexers use for magnet-only releases).
func (s *Service) fetchRelease(ctx context.Context, rawURL string, proto downloaders.Protocol) (fetched, error) {
	rawURL = strings.TrimSpace(rawURL)
	if strings.HasPrefix(strings.ToLower(rawURL), "magnet:") {
		if proto != downloaders.Torrent {
			return fetched{}, invalid("a magnet link is a torrent")
		}
		m, err := downloaders.ParseMagnet(rawURL)
		if err != nil {
			return fetched{}, invalid("%s", err.Error())
		}
		return fetched{magnet: rawURL, hash: m.InfoHash}, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fetched{}, invalid("download URL must be http(s) or a magnet link")
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fetched{}, invalid("bad download URL")
	}
	req.Header.Set("User-Agent", "Locarr")
	hc := downloaders.NewHTTPClient(downloaders.HTTPOptions{Timeout: fetchTimeout, AllowLinkLocal: s.AllowLinkLocal})
	resp, err := hc.Do(req)
	if err != nil {
		var redir *downloaders.CrossHostRedirect
		if errors.As(err, &redir) && strings.EqualFold(redir.Location.Scheme, "magnet") && proto == downloaders.Torrent {
			return s.fetchRelease(ctx, redir.Location.String(), proto)
		}
		return fetched{}, fmt.Errorf("%w: %s", ErrFetch, downloaders.ClassifyTransportError(err).Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fetched{}, fmt.Errorf("%w: the indexer answered HTTP %d", ErrFetch, resp.StatusCode)
	}
	limit := int64(MaxTorrentFile)
	if proto == downloaders.Usenet {
		limit = MaxNZBFile
	}
	body, err := downloaders.ReadLimited(resp.Body, limit)
	if err != nil {
		return fetched{}, fmt.Errorf("%w: %s", ErrFetch, err.Error())
	}
	if proto == downloaders.Usenet {
		head := body[:min(len(body), 4096)]
		if !bytes.Contains(bytes.ToLower(head), []byte("<nzb")) {
			return fetched{}, fmt.Errorf("%w: the file is not an NZB", ErrFetch)
		}
		return fetched{nzb: body}, nil
	}
	if t := bytes.TrimSpace(body); bytes.HasPrefix(bytes.ToLower(t), []byte("magnet:")) {
		return s.fetchRelease(ctx, string(t), proto)
	}
	ti, err := downloaders.ParseTorrent(body)
	if err != nil {
		return fetched{}, fmt.Errorf("%w: the file is not a valid torrent", ErrFetch)
	}
	return fetched{torrent: body, hash: ti.InfoHash}, nil
}

func (s *Service) ensureCategory(ctx context.Context, c Client, cl downloaders.Client, cat string) {
	if cat == "" || !cl.Capabilities().CreateCategories {
		return
	}
	s.mu.Lock()
	s.init()
	known := s.categories[c.ID][strings.ToLower(cat)]
	s.mu.Unlock()
	if known {
		return
	}
	if err := cl.EnsureCategory(ctx, cat, ""); err != nil {
		s.Log.Warn("create download client category", "client", c.Name, "category", cat, "err", err)
		return
	}
	s.mu.Lock()
	if s.categories[c.ID] == nil {
		s.categories[c.ID] = map[string]bool{}
	}
	s.categories[c.ID][strings.ToLower(cat)] = true
	s.mu.Unlock()
}

// Grab fetches the release, picks a client, adds it and records the download.
func (s *Service) Grab(ctx context.Context, req GrabRequest) (Download, error) {
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 500 {
		return Download{}, invalid("title is required (up to 500 characters)")
	}
	if req.Protocol != downloaders.Torrent && req.Protocol != downloaders.Usenet {
		return Download{}, invalid("protocol must be torrent or usenet")
	}
	switch req.MediaKind {
	case "", KindMovies, KindSeries, KindMusic:
	default:
		return Download{}, invalid("unknown media kind %q", req.MediaKind)
	}
	if len(req.ItemRef) == 0 {
		req.ItemRef = json.RawMessage("{}")
	}
	if !json.Valid(req.ItemRef) || len(req.ItemRef) > 16<<10 {
		return Download{}, invalid("itemRef must be JSON (up to 16 KiB)")
	}
	if req.SeedRatio != nil && *req.SeedRatio < 0 {
		return Download{}, invalid("seed ratio cannot be negative")
	}
	clients, err := s.pickClients(ctx, req)
	if err != nil {
		return Download{}, err
	}
	f, err := s.fetchRelease(ctx, req.DownloadURL, req.Protocol)
	if err != nil {
		return Download{}, err
	}
	var lastErr error
	for _, c := range clients {
		d, err := s.grabWith(ctx, c, req, f)
		if err == nil {
			return d, nil
		}
		if errors.Is(err, ErrAlreadyQueued) || errors.Is(err, ErrInvalid) {
			return Download{}, err
		}
		s.Log.Warn("grab: client refused, trying the next one", "client", c.Name, "err", err)
		lastErr = err
	}
	return Download{}, lastErr
}

func (s *Service) grabWith(ctx context.Context, c Client, req GrabRequest, f fetched) (Download, error) {
	if f.hash != "" {
		var n int
		if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM downloads WHERE client_id = ? AND client_item_id = ?
			AND status IN ('active', 'completed', 'imported')`, c.ID, f.hash).Scan(&n); err != nil {
			return Download{}, err
		}
		if n > 0 {
			return Download{}, ErrAlreadyQueued
		}
	}
	cl, err := s.connect(ctx, c)
	if err != nil {
		return Download{}, err
	}
	cat := c.Category(req.MediaKind)
	s.ensureCategory(ctx, c, cl, cat)
	prio := c.RecentPriority
	if req.PublishedAt != nil && s.now().Sub(*req.PublishedAt) > recentAge {
		prio = c.OlderPriority
	}
	add := downloaders.AddRequest{Magnet: f.magnet, TorrentFile: f.torrent, NZB: f.nzb, Name: req.Title, Category: cat,
		Paused: c.InitialState == InitialPause, Priority: prio, SeedRatio: req.SeedRatio, SeedTime: req.SeedTime,
		Tags: req.Tags, Sequential: c.Sequential, FirstLastPiece: c.FirstLastPiece}
	if err := add.Validate(c.Protocol); err != nil {
		return Download{}, invalid("%s", err.Error())
	}
	id, err := cl.Add(ctx, add)
	if err != nil {
		return Download{}, err
	}
	if f.hash != "" && id == "" {
		id = f.hash
	}
	now := s.now()
	var d Download
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		var rowID int64
		err := tx.QueryRowContext(ctx, `INSERT INTO downloads (client_id, client_name, client_item_id, protocol, title,
			indexer_id, indexer, guid, info_hash, category, media_kind, item_ref, seed_ratio, seed_time, status, state,
			added_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', 'queued', ?, ?) RETURNING id`,
			c.ID, c.Name, strings.ToLower(id), string(req.Protocol), req.Title, req.IndexerID, req.Indexer, req.GUID, f.hash,
			cat, req.MediaKind, string(req.ItemRef), req.SeedRatio, nullDur(req.SeedTime), db.Millis(now), db.Millis(now)).Scan(&rowID)
		if isUnique(err) {
			return ErrAlreadyQueued
		}
		if err != nil {
			return err
		}
		if err := s.addEvent(ctx, tx, rowID, EventGrabbed, "", string(downloaders.StateQueued),
			fmt.Sprintf("sent to %s", c.Name), map[string]any{"client": c.Name, "indexer": req.Indexer, "category": cat}); err != nil {
			return err
		}
		d, err = scanDownload(tx.QueryRowContext(ctx, `SELECT `+downloadColumns+` FROM downloads WHERE id = ?`, rowID))
		return err
	})
	if err != nil {
		return Download{}, err
	}
	s.Bus.Publish(ctx, Event{Type: EventGrabbed, Download: d})
	s.Kick()
	return d, nil
}
