package indexers

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

// SourceKind is an application whose indexers can be imported.
type SourceKind string

// Source kinds.
const (
	Prowlarr SourceKind = "prowlarr"
	Jackett  SourceKind = "jackett"
)

// SourceError is a failure talking to Prowlarr or Jackett.
type SourceError struct{ Message string }

func (e *SourceError) Error() string { return e.Message }

// IndexerSource is a saved Prowlarr or Jackett instance.
type IndexerSource struct {
	ID           int64
	Kind         SourceKind
	Name         string
	URL          string
	Exclude      []string
	IndexerCount int
	LastSyncAt   *time.Time
	LastError    string
	CreatedAt    time.Time
}

// SourceInput adds a source.
type SourceInput struct {
	Kind    SourceKind
	Name    string
	URL     string
	APIKey  string
	Exclude []string
}

// SourceItem is one indexer configured on a source.
type SourceItem struct {
	Ref             string
	Name            string
	Kind            Kind
	BaseURL         string // feed base; the API path is always /api
	EnabledOnSource bool
	Action          string // add, update, unchanged, disable, excluded
	IndexerID       int64
}

// FeedURL is the full Torznab/Newznab endpoint of the item.
func (i SourceItem) FeedURL() string { return i.BaseURL + "/api" }

// SyncResult counts what a sync did.
type SyncResult struct {
	Source                              IndexerSource
	Added, Updated, Disabled, Unchanged int
}

func (s *Service) checkSource(ctx context.Context, in *SourceInput) error {
	if in.Kind != Prowlarr && in.Kind != Jackett {
		return invalid("kind", "must be prowlarr or jackett")
	}
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	pol, err := s.policy(ctx)
	if err != nil {
		return err
	}
	if err := pol.CheckURL(in.URL); err != nil {
		return invalid("url", err.Error())
	}
	if u, _ := url.Parse(in.URL); u.RawQuery != "" {
		return invalid("url", "must not contain query parameters")
	}
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.APIKey == "" {
		return invalid("apiKey", "is required")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = map[SourceKind]string{Prowlarr: "Prowlarr", Jackett: "Jackett"}[in.Kind]
	}
	return nil
}

// fetchSourceItems lists the indexers configured on a source through its
// documented API: Prowlarr's GET /api/v1/indexer, Jackett's Torznab
// t=indexers on the aggregate "all" indexer.
func (s *Service) fetchSourceItems(ctx context.Context, kind SourceKind, base, apiKey string) ([]SourceItem, error) {
	pol, err := s.policy(ctx)
	if err != nil {
		return nil, err
	}
	hc, err := httpx.New(httpx.Options{Policy: pol, Timeout: 30 * time.Second, Secrets: []string{apiKey}})
	if err != nil {
		return nil, err
	}
	fail := func(err error) error {
		var ze *znab.Error
		if errors.As(err, &ze) {
			return &SourceError{Message: ze.Message}
		}
		return &SourceError{Message: httpx.Scrub(err.Error(), apiKey)}
	}
	switch kind {
	case Prowlarr:
		res, err := hc.Get(ctx, base+"/api/v1/indexer", http.Header{"X-Api-Key": {apiKey}, "Accept": {"application/json"}})
		if err != nil {
			return nil, fail(err)
		}
		if res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden {
			return nil, &SourceError{Message: "Prowlarr refused the API key"}
		}
		if res.Status != http.StatusOK {
			return nil, &SourceError{Message: fmt.Sprintf("Prowlarr answered HTTP %d", res.Status)}
		}
		var list []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			Protocol string `json:"protocol"`
			Enable   bool   `json:"enable"`
		}
		if err := json.Unmarshal(res.Body, &list); err != nil {
			return nil, &SourceError{Message: "Prowlarr's indexer list is not valid JSON"}
		}
		out := make([]SourceItem, 0, len(list))
		for _, x := range list {
			k := Torznab
			if strings.EqualFold(x.Protocol, "usenet") {
				k = Newznab
			}
			ref := strconv.FormatInt(x.ID, 10)
			out = append(out, SourceItem{Ref: ref, Name: strings.TrimSpace(x.Name), Kind: k, BaseURL: base + "/" + ref,
				EnabledOnSource: x.Enable})
		}
		return out, nil
	case Jackett:
		u := base + "/api/v2.0/indexers/all/results/torznab/api?" + url.Values{"apikey": {apiKey}, "t": {"indexers"},
			"configured": {"true"}}.Encode()
		res, err := hc.Get(ctx, u, nil)
		if err != nil {
			return nil, fail(err)
		}
		if res.Status == http.StatusUnauthorized || res.Status == http.StatusForbidden {
			return nil, &SourceError{Message: "Jackett refused the API key"}
		}
		if res.Status != http.StatusOK {
			return nil, &SourceError{Message: fmt.Sprintf("Jackett answered HTTP %d", res.Status)}
		}
		var x struct {
			XMLName  xml.Name `xml:"indexers"`
			Indexers []struct {
				ID         string `xml:"id,attr"`
				Configured string `xml:"configured,attr"`
				Title      string `xml:"title"`
			} `xml:"indexer"`
		}
		if err := xml.Unmarshal(res.Body, &x); err != nil {
			return nil, &SourceError{Message: "Jackett's indexer list is not valid XML (check the URL and API key)"}
		}
		out := make([]SourceItem, 0, len(x.Indexers))
		for _, i := range x.Indexers {
			id := strings.TrimSpace(i.ID)
			if id == "" || id == "all" || (i.Configured != "" && !strings.EqualFold(i.Configured, "true")) {
				continue
			}
			name := strings.TrimSpace(i.Title)
			if name == "" {
				name = id
			}
			out = append(out, SourceItem{Ref: id, Name: name, Kind: Torznab,
				BaseURL: base + "/api/v2.0/indexers/" + url.PathEscape(id) + "/results/torznab", EnabledOnSource: true})
		}
		return out, nil
	}
	return nil, invalid("kind", "unknown source")
}

// plan compares the source's list with what was imported before.
func (s *Service) plan(ctx context.Context, sourceID int64, items []SourceItem, exclude []string) ([]SourceItem, []Indexer, error) {
	var existing []Indexer
	if sourceID > 0 {
		all, err := s.List(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, ind := range all {
			if ind.SourceID != nil && *ind.SourceID == sourceID {
				existing = append(existing, ind)
			}
		}
	}
	byRef := map[string]Indexer{}
	for _, e := range existing {
		byRef[e.SourceRef] = e
	}
	listed := map[string]bool{}
	for i := range items {
		it := &items[i]
		listed[it.Ref] = true
		cur, ok := byRef[it.Ref]
		switch {
		case slices.Contains(exclude, it.Ref):
			it.Action = "excluded"
			if ok {
				it.IndexerID = cur.ID
			}
		case !ok:
			it.Action = "add"
		default:
			it.IndexerID = cur.ID
			if cur.BaseURL != it.BaseURL || cur.Kind != it.Kind || cur.APIPath != "/api" {
				it.Action = "update"
			} else {
				it.Action = "unchanged"
			}
		}
	}
	// Imported indexers the source no longer lists are disabled, never deleted.
	for _, e := range existing {
		if !listed[e.SourceRef] {
			act := "unchanged"
			if e.Enabled {
				act = "disable"
			}
			items = append(items, SourceItem{Ref: e.SourceRef, Name: e.Name, Kind: e.Kind, BaseURL: e.BaseURL,
				Action: act, IndexerID: e.ID})
		}
	}
	return items, existing, nil
}

// PreviewSource lists what importing from a source would do, without saving.
// With sourceID, the saved source's address and key are used.
func (s *Service) PreviewSource(ctx context.Context, sourceID int64, in SourceInput) ([]SourceItem, error) {
	exclude := []string{}
	if sourceID > 0 {
		src, key, err := s.sourceWithKey(ctx, sourceID)
		if err != nil {
			return nil, err
		}
		in = SourceInput{Kind: src.Kind, Name: src.Name, URL: src.URL, APIKey: key}
		exclude = src.Exclude
	} else if err := s.checkSource(ctx, &in); err != nil {
		return nil, err
	}
	items, err := s.fetchSourceItems(ctx, in.Kind, in.URL, in.APIKey)
	if err != nil {
		return nil, err
	}
	planned, _, err := s.plan(ctx, sourceID, items, exclude)
	return planned, err
}

// CreateSource saves a source and imports its indexers.
func (s *Service) CreateSource(ctx context.Context, in SourceInput) (SyncResult, error) {
	if err := s.checkSource(ctx, &in); err != nil {
		return SyncResult{}, err
	}
	// Check the source answers before saving it.
	if _, err := s.fetchSourceItems(ctx, in.Kind, in.URL, in.APIKey); err != nil {
		return SyncResult{}, err
	}
	excl, _ := json.Marshal(cleanRefs(in.Exclude))
	now := db.Millis(s.now())
	var id int64
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO indexer_sources (kind, name, url, api_key, exclude, created_at, updated_at)
			VALUES (?, ?, ?, x'', ?, ?, ?) RETURNING id`, string(in.Kind), in.Name, in.URL, string(excl), now, now).Scan(&id); err != nil {
			return err
		}
		ct, err := s.Box.Seal([]byte(in.APIKey), sourceAAD(id))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE indexer_sources SET api_key = ? WHERE id = ?`, ct, id)
		return err
	})
	if err != nil {
		return SyncResult{}, err
	}
	return s.SyncSource(ctx, id)
}

func cleanRefs(refs []string) []string {
	out := []string{}
	for _, r := range refs {
		if r = strings.TrimSpace(r); r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

func sourceAAD(id int64) string { return "indexer-source:" + strconv.FormatInt(id, 10) + ":apiKey" }

// SyncSource imports new indexers from a saved source, updates changed ones
// and disables the ones it no longer lists.
func (s *Service) SyncSource(ctx context.Context, id int64) (SyncResult, error) {
	src, key, err := s.sourceWithKey(ctx, id)
	if err != nil {
		return SyncResult{}, err
	}
	items, err := s.fetchSourceItems(ctx, src.Kind, src.URL, key)
	if err != nil {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE indexer_sources SET last_error = ?, updated_at = ? WHERE id = ?`,
			truncate(err.Error(), 500), db.Millis(s.now()), id)
		return SyncResult{}, err
	}
	planned, existing, err := s.plan(ctx, id, items, src.Exclude)
	if err != nil {
		return SyncResult{}, err
	}
	names := map[string]bool{}
	all, err := s.List(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	for _, ind := range all {
		names[strings.ToLower(ind.Name)] = true
	}
	byID := map[int64]Indexer{}
	for _, e := range existing {
		byID[e.ID] = e
	}
	res := SyncResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, it := range planned {
		switch it.Action {
		case "add":
			name := uniqueName(it.Name, src.Name, names)
			names[strings.ToLower(name)] = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				in := DefaultInput()
				in.Name, in.Kind, in.BaseURL, in.APIKey = name, it.Kind, it.BaseURL, &key
				in.Enabled = it.EnabledOnSource
				var caps *znab.Caps
				cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				if c, err := s.capsFor(cctx, in, nil); err == nil {
					caps = c
				}
				cancel()
				cats := DefaultCategories(caps)
				in.Categories = &cats
				sid := id
				_, err := s.insert(ctx, in, caps, &sid, it.Ref)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					s.Log.Warn("import indexer", "source", src.Name, "indexer", it.Name, "err", err)
					return
				}
				res.Added++
			}()
		case "update":
			cur := byID[it.IndexerID]
			in := inputOf(cur)
			in.Kind, in.BaseURL, in.APIPath, in.APIKey = it.Kind, it.BaseURL, "/api", &key
			if _, err := s.Update(ctx, cur.ID, in); err != nil {
				return res, err
			}
			res.Updated++
		case "disable":
			if _, err := s.DB.W.ExecContext(ctx, `UPDATE indexers SET enabled = 0, last_error = ?, last_error_code = 'removed',
				last_error_at = ?, updated_at = ? WHERE id = ?`, "no longer listed by "+src.Name, db.Millis(s.now()),
				db.Millis(s.now()), it.IndexerID); err != nil {
				return res, err
			}
			s.forget(it.IndexerID)
			res.Disabled++
		case "unchanged":
			res.Unchanged++
		}
	}
	wg.Wait()
	// Keep the stored key in step with the source's key (it may have been rotated).
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE indexer_sources SET last_sync_at = ?, last_error = NULL, updated_at = ? WHERE id = ?`,
		db.Millis(s.now()), db.Millis(s.now()), id); err != nil {
		return res, err
	}
	res.Source, err = s.Source(ctx, id)
	return res, err
}

func uniqueName(name, source string, taken map[string]bool) string {
	if name == "" {
		name = "Indexer"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	if !taken[strings.ToLower(name)] {
		return name
	}
	cand := name + " (" + source + ")"
	for n := 2; taken[strings.ToLower(cand)]; n++ {
		cand = fmt.Sprintf("%s (%s %d)", name, source, n)
	}
	return cand
}

const sourceColumns = `s.id, s.kind, s.name, s.url, s.exclude, s.last_sync_at, s.last_error, s.created_at,
	(SELECT count(*) FROM indexers i WHERE i.source_id = s.id)`

func scanSource(row interface{ Scan(...any) error }) (IndexerSource, error) {
	var (
		src        IndexerSource
		kind, excl string
		last       sql.NullInt64
		lastErr    sql.NullString
		created    int64
	)
	if err := row.Scan(&src.ID, &kind, &src.Name, &src.URL, &excl, &last, &lastErr, &created, &src.IndexerCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return src, ErrSourceMissing
		}
		return src, err
	}
	src.Kind = SourceKind(kind)
	_ = json.Unmarshal([]byte(excl), &src.Exclude)
	if src.Exclude == nil {
		src.Exclude = []string{}
	}
	src.LastSyncAt, src.LastError, src.CreatedAt = db.NullTime(last), lastErr.String, db.Time(created)
	return src, nil
}

// Sources lists saved sources.
func (s *Service) Sources(ctx context.Context) ([]IndexerSource, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+sourceColumns+` FROM indexer_sources s ORDER BY s.name, s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IndexerSource{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// Source returns one saved source.
func (s *Service) Source(ctx context.Context, id int64) (IndexerSource, error) {
	return scanSource(s.DB.R.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM indexer_sources s WHERE s.id = ?`, id))
}

func (s *Service) sourceWithKey(ctx context.Context, id int64) (IndexerSource, string, error) {
	src, err := s.Source(ctx, id)
	if err != nil {
		return src, "", err
	}
	var ct []byte
	if err := s.DB.R.QueryRowContext(ctx, `SELECT api_key FROM indexer_sources WHERE id = ?`, id).Scan(&ct); err != nil {
		return src, "", err
	}
	key, err := s.Box.Open(ct, sourceAAD(id))
	if err != nil {
		return src, "", err
	}
	return src, string(key), nil
}

// DeleteSource forgets a source; its indexers stay as ordinary indexers.
func (s *Service) DeleteSource(ctx context.Context, id int64) error {
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE indexers SET source_id = NULL, source_ref = NULL WHERE source_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM indexer_sources WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrSourceMissing
		}
		return nil
	})
}
