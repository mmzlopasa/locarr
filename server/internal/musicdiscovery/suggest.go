package musicdiscovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
)

// Query selects suggestions.
type Query struct {
	// Seeds are library artists (MusicBrainz ids) to start from; empty = all.
	Seeds []string
	// Tag keeps suggestions with this tag (case-insensitive).
	Tag string
	// MinScore keeps suggestions scoring at least this (0–100).
	MinScore int
	// Providers limits the answers used (empty = every active provider).
	Providers []string
	// IncludeLibrary keeps artists already in the library (an artist's "similar artists").
	IncludeLibrary bool
	Offset, Limit  int
}

// SeedRef is a library artist that points to a suggestion.
type SeedRef struct {
	MBID     string
	Name     string
	ArtistID int64
}

// Suggestion is a suggested artist.
type Suggestion struct {
	MBID           string
	Name           string
	Disambiguation string
	// Score is 0–100.
	Score int
	// Seeds are the library artists pointing to it, strongest first (at most 5).
	Seeds     []SeedRef
	SeedCount int
	Providers []string
	Tags      []string
	// Cover is the Cover Art Archive image name ("" = none).
	Cover      string
	LastFMURL  string
	SpotifyURL string
	// Confidence is the best certainty of the MusicBrainz id (below 0.8: matched by name).
	Confidence float64
	// ArtistID is set when the artist is in the library.
	ArtistID int64

	raw        float64
	enrichedAt *time.Time
}

// TagCount is a tag and how many suggestions carry it.
type TagCount struct {
	Name  string
	Count int
}

// Result is a page of suggestions.
type Result struct {
	Items []Suggestion
	Total int
	Tags  []TagCount
}

type aggKey struct {
	seeds, providers string
	library          bool
}

type aggregated struct {
	items []Suggestion
}

type cached struct {
	version string
	res     aggregated
}

// libraryVersion changes when artists are added or removed.
func (s *Service) libraryVersion(ctx context.Context) (string, error) {
	var n, top int64
	err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(item_id), 0) FROM artists`).Scan(&n, &top)
	return strconv.FormatInt(s.version.Load(), 10) + ":" + strconv.FormatInt(n, 10) + ":" + strconv.FormatInt(top, 10), err
}

// Suggest returns ranked suggestions. Each provider's answer counts by the
// provider's weight × its similarity × the id's confidence; providers that
// agree about one library artist combine as 1 − ∏(1 − w·s·c), and so do the
// library artists pointing to the same suggestion, so an artist similar to
// several of yours ranks above one similar to just one. The score is that
// combination × 100.
func (s *Service) Suggest(ctx context.Context, q Query) (Result, error) {
	active, err := s.Active(ctx)
	if err != nil {
		return Result{}, err
	}
	providers := active
	if len(q.Providers) > 0 {
		want := map[string]bool{}
		for _, p := range q.Providers {
			want[p] = true
		}
		providers = []string{}
		for _, p := range active {
			if want[p] {
				providers = append(providers, p)
			}
		}
	}
	seeds := make([]string, 0, len(q.Seeds))
	for _, id := range q.Seeds {
		seeds = append(seeds, strings.ToLower(strings.TrimSpace(id)))
	}
	sort.Strings(seeds)
	agg, err := s.aggregateFor(ctx, aggKey{seeds: strings.Join(seeds, ","), providers: strings.Join(providers, ","), library: q.IncludeLibrary})
	if err != nil {
		return Result{}, err
	}
	tag := strings.ToLower(strings.TrimSpace(q.Tag))
	counts := map[string]int{}
	var list []Suggestion
	for _, sg := range agg.items {
		if sg.Score < q.MinScore {
			continue
		}
		for _, t := range sg.Tags {
			counts[t]++
		}
		if tag != "" && !hasTag(sg.Tags, tag) {
			continue
		}
		list = append(list, sg)
	}
	out := Result{Total: len(list), Tags: []TagCount{}}
	for t, n := range counts {
		out.Tags = append(out.Tags, TagCount{Name: t, Count: n})
	}
	sort.Slice(out.Tags, func(i, j int) bool {
		if out.Tags[i].Count != out.Tags[j].Count {
			return out.Tags[i].Count > out.Tags[j].Count
		}
		return out.Tags[i].Name < out.Tags[j].Name
	})
	if len(out.Tags) > 30 {
		out.Tags = out.Tags[:30]
	}
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	off := min(max(q.Offset, 0), len(list))
	end := min(off+limit, len(list))
	out.Items = list[off:end]
	if out.Items == nil {
		out.Items = []Suggestion{}
	}
	return out, nil
}

func hasTag(tags []string, t string) bool {
	for _, x := range tags {
		if x == t {
			return true
		}
	}
	return false
}

func (s *Service) aggregateFor(ctx context.Context, k aggKey) (aggregated, error) {
	v, err := s.libraryVersion(ctx)
	if err != nil {
		return aggregated{}, err
	}
	ck := k.seeds + "|" + k.providers + "|" + strconv.FormatBool(k.library)
	s.mu.Lock()
	if c, ok := s.cache[ck]; ok && c.version == v {
		s.mu.Unlock()
		return c.res, nil
	}
	s.mu.Unlock()
	res, err := s.aggregate(ctx, k)
	if err != nil {
		return aggregated{}, err
	}
	s.mu.Lock()
	if s.cache == nil || len(s.cache) > 32 {
		s.cache = map[string]cached{}
	}
	s.cache[ck] = cached{version: v, res: res}
	s.mu.Unlock()
	return res, nil
}

// aggregate combines the stored answers of k's providers (comma-separated).
func (s *Service) aggregate(ctx context.Context, k aggKey) (aggregated, error) {
	use := map[string]bool{}
	for _, p := range strings.Split(k.providers, ",") {
		if p != "" {
			use[p] = true
		}
	}
	if len(use) == 0 {
		return aggregated{}, nil
	}
	library, err := s.LibrarySeeds(ctx)
	if err != nil {
		return aggregated{}, err
	}
	lib := map[string]Seed{}
	for _, sd := range library {
		lib[sd.MBID] = sd
	}
	only := map[string]bool{}
	for _, id := range strings.Split(k.seeds, ",") {
		if id != "" {
			only[id] = true
		}
	}
	excluded, err := s.excludedSet(ctx)
	if err != nil {
		return aggregated{}, err
	}
	type pair struct{ target, seed string }
	miss := map[pair]float64{} // ∏(1 − w·s·c) per target and seed
	providers := map[string]map[string]bool{}
	conf := map[string]float64{}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT seed_mbid, provider, target_mbid, score, confidence FROM discovery_similar`)
	if err != nil {
		return aggregated{}, err
	}
	for rows.Next() {
		var (
			seed, p, target string
			score, c        float64
		)
		if err := rows.Scan(&seed, &p, &target, &score, &c); err != nil {
			_ = rows.Close()
			return aggregated{}, err
		}
		if _, ok := lib[seed]; !ok || !use[p] || (len(only) > 0 && !only[seed]) || target == seed || excluded[target] || c < MinConfidence {
			continue
		}
		if _, in := lib[target]; in && !k.library {
			continue
		}
		key := pair{target, seed}
		m, ok := miss[key]
		if !ok {
			m = 1
		}
		miss[key] = m * (1 - min(1, Weights[p]*score*c))
		if providers[target] == nil {
			providers[target] = map[string]bool{}
		}
		providers[target][p] = true
		conf[target] = max(conf[target], c)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return aggregated{}, err
	}
	type contrib struct {
		seed string
		v    float64
	}
	byTarget := map[string][]contrib{}
	for k, m := range miss {
		byTarget[k.target] = append(byTarget[k.target], contrib{k.seed, 1 - m})
	}
	info, err := s.artistInfo(ctx)
	if err != nil {
		return aggregated{}, err
	}
	out := aggregated{items: make([]Suggestion, 0, len(byTarget))}
	for target, cs := range byTarget {
		a, ok := info[target]
		if !ok {
			continue
		}
		sort.Slice(cs, func(i, j int) bool { return cs[i].v > cs[j].v || (cs[i].v == cs[j].v && cs[i].seed < cs[j].seed) })
		missAll, raw := 1.0, 0.0
		sg := a
		for i, c := range cs {
			missAll *= 1 - c.v
			raw += c.v
			if i < 5 {
				sd := lib[c.seed]
				sg.Seeds = append(sg.Seeds, SeedRef(sd))
			}
		}
		sg.SeedCount = len(cs)
		sg.raw = raw
		sg.Score = int((1-missAll)*100 + 0.5)
		sg.Confidence = conf[target]
		for _, p := range Providers {
			if providers[target][p] {
				sg.Providers = append(sg.Providers, p)
			}
		}
		if in, ok := lib[target]; ok {
			sg.ArtistID = in.ArtistID
		}
		out.items = append(out.items, sg)
	}
	sort.Slice(out.items, func(i, j int) bool {
		a, b := out.items[i], out.items[j]
		switch {
		case a.Score != b.Score:
			return a.Score > b.Score
		case a.SeedCount != b.SeedCount:
			return a.SeedCount > b.SeedCount
		case a.raw != b.raw:
			return a.raw > b.raw
		}
		return a.Name < b.Name
	})
	return out, nil
}

func (s *Service) artistInfo(ctx context.Context) (map[string]Suggestion, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT mbid, name, disambiguation, tags, cover_group, lastfm_url, spotify_id, enriched_at FROM discovery_artists`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Suggestion{}
	for rows.Next() {
		var (
			sg       Suggestion
			tags     string
			cover    string
			spotify  string
			enriched sql.NullInt64
		)
		if err := rows.Scan(&sg.MBID, &sg.Name, &sg.Disambiguation, &tags, &cover, &sg.LastFMURL, &spotify, &enriched); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tags), &sg.Tags)
		if sg.Tags == nil {
			sg.Tags = []string{}
		}
		if cover != "" {
			sg.Cover = musicbrainz.CoverName(cover)
		}
		if spotify != "" {
			sg.SpotifyURL = "https://open.spotify.com/artist/" + spotify
		}
		if enriched.Valid {
			t := db.Time(enriched.Int64)
			sg.enrichedAt = &t
		}
		out[sg.MBID] = sg
	}
	return out, rows.Err()
}

// Exclusion is an artist marked "not interested".
type Exclusion struct {
	MBID      string
	Name      string
	UserID    int64
	CreatedAt time.Time
}

func (s *Service) excludedSet(ctx context.Context) (map[string]bool, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT mbid FROM discovery_exclusions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ErrInvalidMBID is returned for an id that is not a MusicBrainz id.
var ErrInvalidMBID = errors.New("not a MusicBrainz artist id")

// Exclude marks an artist "not interested".
func (s *Service) Exclude(ctx context.Context, mbid, name string, userID int64) (Exclusion, error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if !musicbrainz.MBID.MatchString(mbid) {
		return Exclusion{}, ErrInvalidMBID
	}
	if strings.TrimSpace(name) == "" {
		_ = s.DB.R.QueryRowContext(ctx, `SELECT name FROM discovery_artists WHERE mbid = ?`, mbid).Scan(&name)
	}
	now := s.now()
	var uid any
	if userID > 0 {
		uid = userID
	}
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO discovery_exclusions (mbid, name, user_id, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (mbid) DO NOTHING`, mbid, strings.TrimSpace(name), uid, db.Millis(now)); err != nil {
		return Exclusion{}, err
	}
	s.Invalidate()
	return Exclusion{MBID: mbid, Name: strings.TrimSpace(name), UserID: userID, CreatedAt: now}, nil
}

// Include removes an artist from the "not interested" list.
func (s *Service) Include(ctx context.Context, mbid string) error {
	_, err := s.DB.W.ExecContext(ctx, `DELETE FROM discovery_exclusions WHERE mbid = ?`, strings.ToLower(strings.TrimSpace(mbid)))
	s.Invalidate()
	return err
}

// Exclusions lists the "not interested" artists, newest first.
func (s *Service) Exclusions(ctx context.Context) ([]Exclusion, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT mbid, name, COALESCE(user_id, 0), created_at FROM discovery_exclusions ORDER BY created_at DESC, mbid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Exclusion{}
	for rows.Next() {
		var (
			e  Exclusion
			at int64
		)
		if err := rows.Scan(&e.MBID, &e.Name, &e.UserID, &at); err != nil {
			return nil, err
		}
		e.CreatedAt = db.Time(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ProviderStatus is how a provider is doing.
type ProviderStatus struct {
	ID         string
	Enabled    bool
	Configured bool
	// Artists is how many library artists it has answered for; Failed how many failed last time.
	Artists   int
	Failed    int
	LastError string
	LastAt    *time.Time
}

// Status reports every provider.
func (s *Service) Status(ctx context.Context) ([]ProviderStatus, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderStatus, 0, len(Providers))
	for _, p := range Providers {
		st := ProviderStatus{ID: p, Enabled: cfg.Enabled(p), Configured: s.Configured(ctx, p)}
		var last sql.NullInt64
		if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE d.error = ''), COUNT(*) FILTER (WHERE d.error <> ''), MAX(d.fetched_at)
			FROM discovery_seeds d JOIN artists a ON a.mbid = d.seed_mbid WHERE d.provider = ?`, p).Scan(&st.Artists, &st.Failed, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			t := db.Time(last.Int64)
			st.LastAt = &t
		}
		_ = s.DB.R.QueryRowContext(ctx, `SELECT error FROM discovery_seeds WHERE provider = ? AND error <> '' ORDER BY fetched_at DESC LIMIT 1`, p).
			Scan(&st.LastError)
		out = append(out, st)
	}
	return out, nil
}

// LastRefresh is when any answer was last stored (nil = never).
func (s *Service) LastRefresh(ctx context.Context) (*time.Time, error) {
	var last sql.NullInt64
	if err := s.DB.R.QueryRowContext(ctx, `SELECT MAX(fetched_at) FROM discovery_seeds`).Scan(&last); err != nil {
		return nil, err
	}
	if !last.Valid {
		return nil, nil
	}
	t := db.Time(last.Int64)
	return &t, nil
}
