// Package board keeps the "why didn't it land?" board (D-081): every
// monitored item that is missing (movie, episode, album) or misses subtitles,
// with the explainer's current answer. Answers are precomputed into
// board_items: a full rebuild runs in the background (at start and every
// RebuildInterval) and single items are recomputed shortly after something
// happens to them (timeline events, download events), so reading the board is
// a couple of indexed queries even with tens of thousands of items.
package board

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Item kinds.
const (
	KindMovie    = "movie"
	KindEpisode  = "episode"
	KindAlbum    = "album"
	KindSubtitle = "subtitle"
)

// Codes the board adds to the explainer's.
const (
	CodeStalled          = "stalled"
	CodePathNotVisible   = "path_not_visible"
	CodeSubNotSearched   = "subtitle_not_searched"
	CodeSubNotFound      = "subtitle_not_found"
	CodeSubNoProviders   = "subtitle_no_providers"
	CodeSubQuotaExceeded = "subtitle_quota"
)

// What usually fixes a group (the web client links to the page).
const (
	FixPathMapping    = "path_mapping"
	FixPermissions    = "permissions"
	FixClientCategory = "client_category"
	FixDiskSpace      = "disk_space"
	FixIndexers       = "indexers"
	FixClients        = "download_clients"
	FixSubProviders   = "subtitle_providers"
	FixManualImport   = "manual_import"
	FixProfile        = "profile"
)

// RebuildInterval is how often the whole board is recomputed.
const RebuildInterval = 15 * time.Minute

// Item is one row of the board.
type Item struct {
	Key              string
	Kind             string
	ItemRef          string
	ParentRef        string
	Title            string
	SortTitle        string
	Code             string
	Tone             string
	Fix              string
	Message          string
	Params           map[string]any
	Reasons          []timeline.ReasonCount
	Searches         int
	DownloadID       *int64
	QualityProfileID int64
	UpdatedAt        time.Time
}

// Service computes and reads the board.
type Service struct {
	DB       *db.DB
	Timeline *timeline.Store
	Movies   *movies.Service
	Series   *series.Service
	Music    *music.Service
	Log      *slog.Logger
	Now      func() time.Time
	mu       sync.Mutex // one writer at a time (rebuild or refresh)
	dirtyMu  sync.Mutex
	dirty    map[string]bool
	wake     chan struct{}
	// The summary is cached until the board changes.
	sumMu    sync.Mutex
	sumCache map[string]Summary
	gen      int64
	lastFull time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Mark asks for the items behind refs to be recomputed soon.
func (s *Service) Mark(refs ...string) {
	if len(refs) == 0 {
		return
	}
	s.dirtyMu.Lock()
	if s.dirty == nil {
		s.dirty = map[string]bool{}
	}
	for _, r := range refs {
		if r != "" {
			s.dirty[r] = true
		}
	}
	w := s.wakeChan()
	s.dirtyMu.Unlock()
	select {
	case w <- struct{}{}:
	default:
	}
}

// wakeChan is called with dirtyMu held.
func (s *Service) wakeChan() chan struct{} {
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	return s.wake
}

// Debounce is how long Run waits after a mark before recomputing, so a burst
// of events becomes one refresh.
const Debounce = 2 * time.Second

// Run recomputes marked items shortly after they are marked, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.dirtyMu.Lock()
	w := s.wakeChan()
	s.dirtyMu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(Debounce):
		}
		if err := s.Flush(ctx); err != nil && ctx.Err() == nil {
			s.log().Warn("board refresh", "err", err)
		}
	}
}

// Flush recomputes every marked item.
func (s *Service) Flush(ctx context.Context) error {
	s.dirtyMu.Lock()
	refs := make([]string, 0, len(s.dirty))
	for r := range s.dirty {
		refs = append(refs, r)
	}
	s.dirty = nil
	s.dirtyMu.Unlock()
	if len(refs) == 0 {
		return nil
	}
	return s.Refresh(ctx, refs)
}

// candidate is an item that belongs on the board before its explanation.
type candidate struct {
	key, kind, itemRef, parentRef, title, sortTitle string
	profileID                                       int64
	category                                        string // indexer category: movies, tv, music
	state                                           timeline.ItemState
	refs                                            []string
	// subtitles only
	sub *subFacts
}

type subFacts struct {
	missing    []string
	searches   int
	last, next *time.Time
	lastResult string
	asked      int
}

func (s *Service) movieCandidates(ctx context.Context, ids []int64) ([]candidate, error) {
	if s.Movies == nil {
		return nil, nil
	}
	list, err := s.Movies.Missing(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]candidate, 0, len(list))
	for _, m := range list {
		ref := library.Ref{Kind: library.RefMovie, ID: m.ID}.String()
		title := m.Title
		if m.Year > 0 {
			title = fmt.Sprintf("%s (%d)", m.Title, m.Year)
		}
		out = append(out, candidate{key: ref, kind: KindMovie, itemRef: ref, title: title, sortTitle: m.SortTitle, profileID: m.QualityProfileID,
			category: "movies", refs: []string{ref},
			state: timeline.ItemState{Ref: ref, Monitored: true, Available: m.Available, AvailableFrom: m.AvailableFrom}})
	}
	return out, nil
}

// episodeCandidates: monitored aired episodes without a file. where narrows
// the episodes ("" = all).
func (s *Service) episodeCandidates(ctx context.Context, where string, args ...any) ([]candidate, error) {
	q := `SELECT e.id, e.season_id, e.series_id, i.title, i.sort_title, e.season_number, e.episode_number, e.title, i.quality_profile_id
		FROM episodes e JOIN media_items i ON i.id = e.series_id JOIN seasons sn ON sn.id = e.season_id
		WHERE i.monitored = 1 AND sn.monitored = 1 AND e.monitored = 1 AND e.removed = 0 AND e.file_id IS NULL
		AND e.air_date_utc IS NOT NULL AND e.air_date_utc <= ?` + where // #nosec G202 -- where is placeholders only
	rows, err := s.DB.R.QueryContext(ctx, q, append([]any{db.Millis(s.now())}, args...)...) // #nosec G202 -- fixed fragments
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []candidate{}
	for rows.Next() {
		var (
			id, seasonID, seriesID, profile int64
			st, sortTitle, epTitle          string
			sn, en                          int
		)
		if err := rows.Scan(&id, &seasonID, &seriesID, &st, &sortTitle, &sn, &en, &epTitle, &profile); err != nil {
			return nil, err
		}
		ref := library.Ref{Kind: library.RefEpisode, ID: id}.String()
		title := fmt.Sprintf("%s S%02dE%02d", st, sn, en)
		if epTitle != "" {
			title += " - " + epTitle
		}
		out = append(out, candidate{key: ref, kind: KindEpisode, itemRef: ref, parentRef: library.Ref{Kind: library.RefSeries, ID: seriesID}.String(),
			title: title, sortTitle: fmt.Sprintf("%s %04d %04d", sortTitle, sn, en), profileID: profile, category: "tv",
			refs:  []string{ref, library.Ref{Kind: library.RefSeason, ID: seasonID}.String()},
			state: timeline.ItemState{Ref: ref, Monitored: true, Available: true}})
	}
	return out, rows.Err()
}

func (s *Service) albumCandidate(st music.AlbumState, profile int64) candidate {
	ref := library.Ref{Kind: library.RefAlbum, ID: st.ID}.String()
	return candidate{key: ref, kind: KindAlbum, itemRef: ref, parentRef: library.Ref{Kind: library.RefArtist, ID: st.ArtistID}.String(),
		title: st.ArtistName + " - " + st.Title, sortTitle: strings.ToLower(st.ArtistName + " " + st.ReleaseDate + " " + st.Title), profileID: profile,
		category: "music", refs: []string{ref},
		state: timeline.ItemState{Ref: ref, Monitored: true, Available: true, HasFile: st.FileCount > 0, CutoffMet: false}}
}

func (s *Service) albumCandidates(ctx context.Context, ids []int64) ([]candidate, error) {
	if s.Music == nil {
		return nil, nil
	}
	if ids == nil {
		list, err := s.Music.WantedAlbums(ctx, false)
		if err != nil {
			return nil, err
		}
		profiles := map[int64]int64{}
		out := make([]candidate, 0, len(list))
		for _, st := range list {
			p, ok := profiles[st.ArtistID]
			if !ok {
				if a, err := s.Music.Artist(ctx, st.ArtistID); err == nil {
					p = a.QualityProfileID
				}
				profiles[st.ArtistID] = p
			}
			out = append(out, s.albumCandidate(st, p))
		}
		return out, nil
	}
	out := []candidate{}
	today := s.now().UTC().Format("2006-01-02")
	for _, id := range ids {
		al, err := s.Music.Album(ctx, id)
		if err != nil {
			continue // gone
		}
		a, err := s.Music.Artist(ctx, al.ArtistID)
		if err != nil {
			continue
		}
		if !al.Monitored || !a.Monitored || al.ReleaseDate == "" || al.ReleaseDate > today {
			continue
		}
		st, err := s.Music.State(ctx, al, &a, nil)
		if err != nil {
			return nil, err
		}
		if !st.Available || st.Complete() {
			continue
		}
		out = append(out, s.albumCandidate(st, a.QualityProfileID))
	}
	return out, nil
}

// subtitleCandidates: files missing a wanted subtitle language. refs narrows
// them to those videos (nil = all).
func (s *Service) subtitleCandidates(ctx context.Context, refs []string) ([]candidate, error) {
	q := `SELECT st.item_ref, st.kind, st.item_id, mi.title, mi.sort_title, COALESCE(e.season_number, 0), COALESCE(e.episode_number, 0),
		st.missing, st.searches, st.last_search_at, st.next_search_at, st.last_result, st.providers_asked
		FROM subtitle_state st JOIN media_items mi ON mi.id = st.item_id
		LEFT JOIN episodes e ON st.kind = 'episode' AND e.id = st.entity_id
		WHERE st.missing_count > 0`
	var args []any
	if refs != nil {
		if len(refs) == 0 {
			return nil, nil
		}
		q += ` AND st.item_ref IN (` + strings.TrimSuffix(strings.Repeat("?,", len(refs)), ",") + `)` // #nosec G202 -- placeholders only
		for _, r := range refs {
			args = append(args, r)
		}
	}
	rows, err := s.DB.R.QueryContext(ctx, q, args...) // #nosec G202 -- placeholders only
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []candidate{}
	for rows.Next() {
		var (
			ref, kind, title, sortTitle, missing, result string
			itemID                                       int64
			sn, en, searches, asked                      int
			last, next                                   sql.NullInt64
		)
		if err := rows.Scan(&ref, &kind, &itemID, &title, &sortTitle, &sn, &en, &missing, &searches, &last, &next, &result, &asked); err != nil {
			return nil, err
		}
		sf := &subFacts{searches: searches, last: db.NullTime(last), next: db.NullTime(next), lastResult: result, asked: asked}
		_ = json.Unmarshal([]byte(missing), &sf.missing)
		parent := ""
		if kind == "episode" {
			title = fmt.Sprintf("%s S%02dE%02d", title, sn, en)
			sortTitle = fmt.Sprintf("%s %04d %04d", sortTitle, sn, en)
			parent = library.Ref{Kind: library.RefSeries, ID: itemID}.String()
		}
		out = append(out, candidate{key: KindSubtitle + ":" + ref, kind: KindSubtitle, itemRef: ref, parentRef: parent, title: title,
			sortTitle: sortTitle, sub: sf})
	}
	return out, rows.Err()
}

// subtitleProviders reports how many providers are enabled and how many of
// those have no quota left right now.
func (s *Service) subtitleProviders(ctx context.Context) (enabled, outOfQuota int, err error) {
	err = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN quota_remaining IS NOT NULL AND quota_remaining <= 0
		AND (quota_reset_at IS NULL OR quota_reset_at > ?) THEN 1 ELSE 0 END), 0) FROM subtitle_providers WHERE enabled = 1`,
		db.Millis(s.now())).Scan(&enabled, &outOfQuota)
	return enabled, outOfQuota, err
}

// env is what explaining many items at once shares.
type env struct {
	counts                 map[string]*timeline.Counts
	subEnabled, subNoQuota int
}

func (s *Service) newEnv(ctx context.Context) (*env, error) {
	e := &env{counts: map[string]*timeline.Counts{}}
	for _, cat := range []string{"movies", "tv", "music"} {
		c, err := s.Timeline.Counts(ctx, cat)
		if err != nil {
			return nil, err
		}
		e.counts[cat] = &c
	}
	var err error
	e.subEnabled, e.subNoQuota, err = s.subtitleProviders(ctx)
	return e, err
}

// explain turns a candidate into a board item (nil = it does not belong on the board).
func (s *Service) explain(ctx context.Context, e *env, c candidate) (*Item, error) {
	it := &Item{Key: c.key, Kind: c.kind, ItemRef: c.itemRef, ParentRef: c.parentRef, Title: c.title, SortTitle: strings.ToLower(c.sortTitle),
		QualityProfileID: c.profileID, Reasons: []timeline.ReasonCount{}, UpdatedAt: s.now().UTC()}
	if c.sub != nil {
		explainSubtitle(it, c.sub, e)
		return it, nil
	}
	f, err := s.Timeline.FactsCounted(ctx, c.refs, c.category, e.counts[c.category])
	if err != nil {
		return nil, err
	}
	ex := timeline.Explain(c.state, f)
	if ex.Code == timeline.ExplainAvailable || ex.Code == timeline.ExplainNotMonitored {
		return nil, nil
	}
	it.Code, it.Tone, it.Message, it.Params, it.Reasons, it.Searches = ex.Code, ex.Tone, ex.Message, ex.Params, ex.Reasons, f.Searches
	if it.Params == nil {
		it.Params = map[string]any{}
	}
	if f.Download != nil {
		id := f.Download.ID
		it.DownloadID = &id
	}
	switch ex.Code {
	case timeline.ExplainDownloading:
		d := f.Download
		if d.Progress >= 1 && strings.Contains(d.Message, "not visible") {
			it.Code, it.Tone, it.Fix = CodePathNotVisible, "danger", FixPathMapping
			it.Message = "the download finished in the client but Locarr cannot see its files: " + d.Message
			break
		}
		var stalled sql.NullInt64
		_ = s.DB.R.QueryRowContext(ctx, `SELECT stalled_since FROM downloads WHERE id = ?`, d.ID).Scan(&stalled)
		if stalled.Valid {
			it.Code, it.Tone = CodeStalled, "warning"
			it.Params["stalledSince"] = db.Time(stalled.Int64)
			it.Message = fmt.Sprintf("%s is stalled at %.0f%%: nothing downloaded and no seeds", d.Title, d.Progress*100)
		}
	case timeline.ExplainImportFailed:
		it.Fix = ClassifyImportFailure(f.ImportFailed)
	case timeline.ExplainNoIndexers:
		it.Fix = FixIndexers
	case timeline.ExplainNoClients:
		it.Fix = FixClients
	case timeline.ExplainAllRejected:
		it.Fix = FixProfile
	case timeline.ExplainNothingFound, timeline.ExplainGrabFailed:
		it.Fix = FixIndexers
	}
	return it, nil
}

func explainSubtitle(it *Item, sf *subFacts, e *env) {
	it.Reasons = []timeline.ReasonCount{}
	it.Searches = sf.searches
	it.Params = map[string]any{"languages": sf.missing, "searches": sf.searches}
	if sf.last != nil {
		it.Params["lastSearch"] = sf.last.UTC()
	}
	if sf.next != nil {
		it.Params["nextSearch"] = sf.next.UTC()
	}
	langs := strings.Join(sf.missing, ", ")
	switch {
	case e.subEnabled == 0:
		it.Code, it.Tone, it.Fix = CodeSubNoProviders, "danger", FixSubProviders
		it.Message = "missing subtitles (" + langs + ") but no subtitle provider is enabled"
	case e.subNoQuota >= e.subEnabled:
		it.Code, it.Tone, it.Fix = CodeSubQuotaExceeded, "warning", FixSubProviders
		it.Message = "missing subtitles (" + langs + "); every enabled provider has used up its download quota"
	case sf.searches == 0:
		it.Code, it.Tone = CodeSubNotSearched, "info"
		it.Message = "missing subtitles (" + langs + "); not searched yet"
	default:
		it.Code, it.Tone = CodeSubNotFound, "warning"
		if sf.lastResult != "" {
			it.Params["detail"] = sf.lastResult
		}
		it.Message = fmt.Sprintf("missing subtitles (%s); searched %d times without a good enough match", langs, sf.searches)
	}
}

// ClassifyImportFailure guesses what fixes a failed import from its message.
func ClassifyImportFailure(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "not visible") || strings.Contains(m, "no such file") || strings.Contains(m, "cannot read") ||
		strings.Contains(m, "does not exist") || strings.Contains(m, "not found on disk"):
		return FixPathMapping
	case strings.Contains(m, "permission denied") || strings.Contains(m, "operation not permitted") || strings.Contains(m, "read-only"):
		return FixPermissions
	case strings.Contains(m, "no space") || strings.Contains(m, "disk full") || strings.Contains(m, "quota"):
		return FixDiskSpace
	case strings.Contains(m, "category"):
		return FixClientCategory
	}
	return FixManualImport
}

func encodeItem(it *Item) (params, reasons string) {
	if it.Reasons == nil {
		it.Reasons = []timeline.ReasonCount{}
	}
	if it.Params == nil {
		it.Params = map[string]any{}
	}
	p, _ := json.Marshal(it.Params)
	r, _ := json.Marshal(it.Reasons)
	return string(p), string(r)
}

func upsert(ctx context.Context, tx *sql.Tx, it *Item) error {
	params, reasons := encodeItem(it)
	_, err := tx.ExecContext(ctx, `INSERT INTO board_items (key, kind, item_ref, parent_ref, title, sort_title, code, tone, fix, message, params,
		reasons, searches, download_id, quality_profile_id, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET kind = excluded.kind, item_ref = excluded.item_ref, parent_ref = excluded.parent_ref,
		title = excluded.title, sort_title = excluded.sort_title, code = excluded.code, tone = excluded.tone, fix = excluded.fix,
		message = excluded.message, params = excluded.params, reasons = excluded.reasons, searches = excluded.searches,
		download_id = excluded.download_id, quality_profile_id = excluded.quality_profile_id, updated_at = excluded.updated_at`,
		it.Key, it.Kind, it.ItemRef, it.ParentRef, it.Title, it.SortTitle, it.Code, it.Tone, it.Fix, it.Message, params, reasons, it.Searches,
		it.DownloadID, it.QualityProfileID, db.Millis(it.UpdatedAt))
	return err
}

// Rebuild recomputes the whole board.
func (s *Service) Rebuild(ctx context.Context) (int, error) {
	start := s.now()
	e, err := s.newEnv(ctx)
	if err != nil {
		return 0, err
	}
	var all []candidate
	mv, err := s.movieCandidates(ctx, nil)
	if err != nil {
		return 0, err
	}
	all = append(all, mv...)
	if s.Series != nil {
		eps, err := s.episodeCandidates(ctx, "")
		if err != nil {
			return 0, err
		}
		all = append(all, eps...)
	}
	al, err := s.albumCandidates(ctx, nil)
	if err != nil {
		return 0, err
	}
	all = append(all, al...)
	subs, err := s.subtitleCandidates(ctx, nil)
	if err != nil {
		return 0, err
	}
	all = append(all, subs...)
	items := make([]*Item, 0, len(all))
	for _, c := range all {
		it, err := s.explain(ctx, e, c)
		if err != nil {
			return 0, err
		}
		if it != nil {
			items = append(items, it)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Written in batches so other writers are not held up for long.
	for lo := 0; lo < len(items); lo += 500 {
		hi := min(lo+500, len(items))
		if err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
			for _, it := range items[lo:hi] {
				if err := upsert(ctx, tx, it); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return 0, err
		}
	}
	// Rows not written by this rebuild (nor refreshed since it started) are gone.
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM board_items WHERE updated_at < ?`, db.Millis(start)); err != nil {
		return 0, err
	}
	s.lastFull = s.now().UTC()
	s.changed()
	return len(items), nil
}

// LastRebuild is when the board was last rebuilt in full (zero = not yet).
func (s *Service) LastRebuild() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastFull
}

// expand turns refs into the board keys they concern and the candidates
// that still belong on the board.
func (s *Service) expand(ctx context.Context, refs []string) (keys map[string]bool, cands []candidate, err error) {
	keys = map[string]bool{}
	var movieIDs, albumIDs []int64
	var episodeIDs []any
	videoRefs := []string{}
	for _, raw := range refs {
		r, err := library.ParseRef(raw)
		if err != nil {
			continue
		}
		switch r.Kind {
		case library.RefMovie:
			movieIDs = append(movieIDs, r.ID)
			keys[r.String()] = true
			keys[KindSubtitle+":"+r.String()] = true
			videoRefs = append(videoRefs, r.String())
		case library.RefEpisode:
			episodeIDs = append(episodeIDs, r.ID)
			keys[r.String()] = true
			keys[KindSubtitle+":"+r.String()] = true
			videoRefs = append(videoRefs, r.String())
		case library.RefSeason, library.RefSeries:
			col := "season_id"
			if r.Kind == library.RefSeries {
				col = "series_id"
			}
			rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM episodes WHERE `+col+` = ?`, r.ID) // #nosec G202 -- constant column
			if err != nil {
				return nil, nil, err
			}
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err == nil {
					ref := library.Ref{Kind: library.RefEpisode, ID: id}.String()
					episodeIDs = append(episodeIDs, id)
					keys[ref] = true
					keys[KindSubtitle+":"+ref] = true
					videoRefs = append(videoRefs, ref)
				}
			}
			_ = rows.Close()
		case library.RefAlbum:
			albumIDs = append(albumIDs, r.ID)
			keys[r.String()] = true
		case library.RefArtist:
			rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM albums WHERE artist_id = ?`, r.ID)
			if err != nil {
				return nil, nil, err
			}
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err == nil {
					albumIDs = append(albumIDs, id)
					keys[library.Ref{Kind: library.RefAlbum, ID: id}.String()] = true
				}
			}
			_ = rows.Close()
		}
	}
	if len(movieIDs) > 0 {
		c, err := s.movieCandidates(ctx, movieIDs)
		if err != nil {
			return nil, nil, err
		}
		cands = append(cands, c...)
	}
	if len(episodeIDs) > 0 && s.Series != nil {
		for lo := 0; lo < len(episodeIDs); lo += 500 {
			hi := min(lo+500, len(episodeIDs))
			c, err := s.episodeCandidates(ctx, ` AND e.id IN (`+strings.TrimSuffix(strings.Repeat("?,", hi-lo), ",")+`)`, episodeIDs[lo:hi]...)
			if err != nil {
				return nil, nil, err
			}
			cands = append(cands, c...)
		}
	}
	if len(albumIDs) > 0 {
		c, err := s.albumCandidates(ctx, albumIDs)
		if err != nil {
			return nil, nil, err
		}
		cands = append(cands, c...)
	}
	for lo := 0; lo < len(videoRefs); lo += 500 {
		hi := min(lo+500, len(videoRefs))
		c, err := s.subtitleCandidates(ctx, videoRefs[lo:hi])
		if err != nil {
			return nil, nil, err
		}
		cands = append(cands, c...)
	}
	return keys, cands, nil
}

// Refresh recomputes the board rows of the items behind refs (and removes
// those that no longer belong on it).
func (s *Service) Refresh(ctx context.Context, refs []string) error {
	keys, cands, err := s.expand(ctx, refs)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	e, err := s.newEnv(ctx)
	if err != nil {
		return err
	}
	items := []*Item{}
	for _, c := range cands {
		it, err := s.explain(ctx, e, c)
		if err != nil {
			return err
		}
		if it != nil {
			items = append(items, it)
		}
	}
	keep := map[string]bool{}
	for _, it := range items {
		keep[it.Key] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.changed()
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, it := range items {
			if err := upsert(ctx, tx, it); err != nil {
				return err
			}
		}
		for k := range keys {
			if keep[k] {
				continue
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM board_items WHERE key = ?`, k); err != nil {
				return err
			}
		}
		return nil
	})
}

// Group is one reason on the board.
type Group struct {
	Code, Tone, Fix string
	Count, Searches int
	Kinds           map[string]int
	Reasons         []timeline.ReasonCount
}

// Summary is the whole board grouped by reason.
type Summary struct {
	Total     int
	UpdatedAt time.Time
	Groups    []Group
}

// toneRank orders groups: what needs action first.
func toneRank(t string) int {
	switch t {
	case "danger":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	}
	return 3
}

// changed drops the cached summaries (called after every write).
func (s *Service) changed() {
	s.sumMu.Lock()
	s.gen++
	s.sumCache = nil
	s.sumMu.Unlock()
}

// Summarize groups the board by code and fix (kind "" = every kind). The
// result is cached until the board changes.
func (s *Service) Summarize(ctx context.Context, kind string) (Summary, error) {
	s.sumMu.Lock()
	if c, ok := s.sumCache[kind]; ok {
		s.sumMu.Unlock()
		return c, nil
	}
	gen := s.gen
	s.sumMu.Unlock()
	sum, err := s.summarize(ctx, kind)
	if err != nil {
		return sum, err
	}
	s.sumMu.Lock()
	if s.gen == gen {
		if s.sumCache == nil {
			s.sumCache = map[string]Summary{}
		}
		s.sumCache[kind] = sum
	}
	s.sumMu.Unlock()
	return sum, nil
}

func (s *Service) summarize(ctx context.Context, kind string) (Summary, error) {
	where, args := "", []any{}
	if kind != "" {
		where, args = " WHERE kind = ?", append(args, kind)
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT code, fix, MIN(tone), kind, COUNT(*), SUM(searches) FROM board_items`+where+
		` GROUP BY code, fix, kind`, args...) // #nosec G202 -- fixed fragments
	if err != nil {
		return Summary{}, err
	}
	byKey := map[string]*Group{}
	out := Summary{UpdatedAt: s.LastRebuild()}
	for rows.Next() {
		var (
			code, fix, tone, k string
			n, searches        int
		)
		if err := rows.Scan(&code, &fix, &tone, &k, &n, &searches); err != nil {
			_ = rows.Close()
			return Summary{}, err
		}
		key := code + "|" + fix
		g := byKey[key]
		if g == nil {
			g = &Group{Code: code, Fix: fix, Tone: tone, Kinds: map[string]int{KindMovie: 0, KindEpisode: 0, KindAlbum: 0, KindSubtitle: 0},
				Reasons: []timeline.ReasonCount{}}
			byKey[key] = g
		}
		g.Count += n
		g.Searches += searches
		g.Kinds[k] += n
		out.Total += n
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return Summary{}, err
	}
	// Rejection reasons of the items' last searches, added up per group.
	rrows, err := s.DB.R.QueryContext(ctx, `SELECT b.code, b.fix, json_extract(j.value, '$.code') AS rc, SUM(json_extract(j.value, '$.count')),
		MIN(json_extract(j.value, '$.example')) FROM board_items b, json_each(b.reasons) j WHERE b.reasons <> '[]'`+
		strings.Replace(where, " WHERE kind", " AND b.kind", 1)+` GROUP BY b.code, b.fix, rc`, args...) // #nosec G202 -- fixed fragments
	if err != nil {
		return Summary{}, err
	}
	for rrows.Next() {
		var (
			code, fix string
			rc        timeline.ReasonCount
			example   sql.NullString
			rcCode    sql.NullString
			count     sql.NullInt64
		)
		if err := rrows.Scan(&code, &fix, &rcCode, &count, &example); err != nil {
			_ = rrows.Close()
			return Summary{}, err
		}
		rc.Code, rc.Example, rc.Count = rcCode.String, example.String, int(count.Int64)
		if g := byKey[code+"|"+fix]; g != nil && rc.Code != "" {
			g.Reasons = append(g.Reasons, rc)
		}
	}
	_ = rrows.Close()
	for _, g := range byKey {
		sort.Slice(g.Reasons, func(i, j int) bool {
			if g.Reasons[i].Count != g.Reasons[j].Count {
				return g.Reasons[i].Count > g.Reasons[j].Count
			}
			return g.Reasons[i].Code < g.Reasons[j].Code
		})
		if len(g.Reasons) > 8 {
			g.Reasons = g.Reasons[:8]
		}
		out.Groups = append(out.Groups, *g)
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if toneRank(a.Tone) != toneRank(b.Tone) {
			return toneRank(a.Tone) < toneRank(b.Tone)
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Code+a.Fix < b.Code+b.Fix
	})
	if out.Groups == nil {
		out.Groups = []Group{}
	}
	return out, nil
}

// ListFilter selects board items.
type ListFilter struct {
	Code, Fix, Kind, Query string
	Offset, Limit          int
}

const itemColumns = `key, kind, item_ref, parent_ref, title, sort_title, code, tone, fix, message, params, reasons, searches,
	download_id, quality_profile_id, updated_at`

func scanItem(sc interface{ Scan(...any) error }) (Item, error) {
	var (
		it              Item
		params, reasons string
		dl              sql.NullInt64
		updated         int64
	)
	if err := sc.Scan(&it.Key, &it.Kind, &it.ItemRef, &it.ParentRef, &it.Title, &it.SortTitle, &it.Code, &it.Tone, &it.Fix, &it.Message,
		&params, &reasons, &it.Searches, &dl, &it.QualityProfileID, &updated); err != nil {
		return it, err
	}
	_ = json.Unmarshal([]byte(params), &it.Params)
	_ = json.Unmarshal([]byte(reasons), &it.Reasons)
	if it.Params == nil {
		it.Params = map[string]any{}
	}
	if it.Reasons == nil {
		it.Reasons = []timeline.ReasonCount{}
	}
	if dl.Valid {
		id := dl.Int64
		it.DownloadID = &id
	}
	it.UpdatedAt = db.Time(updated)
	return it, nil
}

// List returns one page of items, by title, and how many match.
func (s *Service) List(ctx context.Context, f ListFilter) ([]Item, int, error) {
	where := []string{"1 = 1"}
	args := []any{}
	if f.Code != "" {
		where, args = append(where, "code = ?"), append(args, f.Code)
	}
	if f.Fix != "" {
		where, args = append(where, "fix = ?"), append(args, f.Fix)
	}
	if f.Kind != "" {
		where, args = append(where, "kind = ?"), append(args, f.Kind)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		where, args = append(where, `lower(title) LIKE ? ESCAPE '\'`), append(args, like)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM board_items`+clause, args...).Scan(&total); err != nil { // #nosec G202
		return nil, 0, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+itemColumns+` FROM board_items`+clause+` ORDER BY sort_title, key LIMIT ? OFFSET ?`,
		append(args, f.Limit, max(f.Offset, 0))...) // #nosec G202 -- fixed fragments
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	s.liveProgress(ctx, out)
	return out, total, nil
}

// liveProgress replaces the stored progress of downloading items with the
// download's current one (progress is not an event, so rows can lag).
func (s *Service) liveProgress(ctx context.Context, items []Item) {
	for i := range items {
		it := &items[i]
		if it.DownloadID == nil || (it.Code != timeline.ExplainDownloading && it.Code != CodeStalled) {
			continue
		}
		var p float64
		if err := s.DB.R.QueryRowContext(ctx, `SELECT progress FROM downloads WHERE id = ?`, *it.DownloadID).Scan(&p); err == nil {
			it.Params["progress"] = p
		}
	}
}

// Get returns items by key.
func (s *Service) Get(ctx context.Context, keys []string) ([]Item, error) {
	out := []Item{}
	for lo := 0; lo < len(keys); lo += 500 {
		hi := min(lo+500, len(keys))
		args := make([]any, 0, hi-lo)
		for _, k := range keys[lo:hi] {
			args = append(args, k)
		}
		rows, err := s.DB.R.QueryContext(ctx, `SELECT `+itemColumns+` FROM board_items WHERE key IN (`+
			strings.TrimSuffix(strings.Repeat("?,", hi-lo), ",")+`)`, args...) // #nosec G202 -- placeholders only
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			it, err := scanItem(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, it)
		}
		_ = rows.Close()
	}
	return out, nil
}

// ErrNotOnBoard is returned for an action on an item that is not on the board.
var ErrNotOnBoard = errors.New("not on the board (any more)")

// RefOf returns the library ref behind a board key.
func RefOf(key string) (library.Ref, error) {
	return library.ParseRef(strings.TrimPrefix(key, KindSubtitle+":"))
}
