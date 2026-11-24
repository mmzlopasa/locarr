package musicdiscovery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/lastfm"
	lfmfake "github.com/mmzlopasa/locarr/server/internal/metadata/lastfm/fake"
	"github.com/mmzlopasa/locarr/server/internal/metadata/listenbrainz"
	lbfake "github.com/mmzlopasa/locarr/server/internal/metadata/listenbrainz/fake"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
	"github.com/mmzlopasa/locarr/server/internal/metadata/spotify"
	spfake "github.com/mmzlopasa/locarr/server/internal/metadata/spotify/fake"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

type env struct {
	s   *Service
	sp  *spfake.Server
	lfm *lfmfake.Server
	d   *db.DB
}

func newEnv(t *testing.T) env {
	t.Helper()
	d := db.OpenTest(t)
	box, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	st := settings.New(d, box)
	cache := &metadata.Cache{DB: d}
	mux := http.NewServeMux()
	mb := mbfake.New()
	lfm := lfmfake.New()
	sp := spfake.New()
	mux.Handle("/mb/", mb)
	mux.Handle("/lastfm/", lfm)
	mux.Handle("/lb/", lbfake.New())
	mux.Handle("/spotify/", sp)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	fast := func() *metadata.Limiter { return metadata.NewLimiter(1000, 1000) }
	s := &Service{DB: d, Settings: st, Cache: cache}
	s.MB = musicbrainz.New(srv.URL+"/mb", srv.URL+"/mb/coverart", cache)
	s.MB.Limiter = fast()
	s.LastFM = lastfm.New(srv.URL+"/lastfm/", s.LastFMKeyValue)
	s.LastFM.Limiter = fast()
	s.LB = listenbrainz.New(srv.URL+"/lb", srv.URL+"/lb")
	s.LB.Limiter = fast()
	s.Spotify = spotify.New(srv.URL+"/spotify/v1", srv.URL+"/spotify", s.SpotifyCreds)
	s.Spotify.Limiter = fast()
	return env{s: s, sp: sp, lfm: lfm, d: d}
}

// addArtist puts an artist in the library.
func (e env) addArtist(t *testing.T, mbid, name string) int64 {
	t.Helper()
	ctx := context.Background()
	now := db.Millis(time.Now())
	var qp int64
	if err := e.d.W.QueryRowContext(ctx, `INSERT INTO quality_profiles (name, kind, data, created_at, updated_at) VALUES ('q', 'music', '{}', ?, ?)
		ON CONFLICT (kind, name) DO UPDATE SET name = name RETURNING id`, now, now).Scan(&qp); err != nil {
		t.Fatal(err)
	}
	var mp int64
	if err := e.d.R.QueryRowContext(ctx, `SELECT MIN(id) FROM metadata_profiles`).Scan(&mp); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := e.d.W.QueryRowContext(ctx, `INSERT INTO media_items (kind, title, sort_title, quality_profile_id, path, added_at, updated_at)
		VALUES ('artist', ?, ?, ?, ?, ?, ?) RETURNING id`, name, name, qp, "/music/"+mbid, now, now).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.W.ExecContext(ctx, `INSERT INTO artists (item_id, mbid, metadata_profile_id) VALUES (?, ?, ?)`, id, mbid, mp); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{"The Beatles": "beatles", "Sigur Rós": "sigur ros", "Simon & Garfunkel": "simon and garfunkel",
		"  AC/DC ": "ac dc", "Mötley Crüe": "motley crue"}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestResolveFrom(t *testing.T) {
	one := []metadata.ArtistSummary{{MBID: "a", Name: "Creative Commons Choir", Score: 100}, {MBID: "b", Name: "Commons", Score: 70}}
	if r := resolveFrom("creative commons choir", one); r.MBID != "a" || r.Confidence != 0.9 {
		t.Fatalf("one exact: %+v", r)
	}
	two := []metadata.ArtistSummary{{MBID: "x", Name: "Open Source Artist", Score: 90}, {MBID: "y", Name: "Open Source Artist", Score: 100}}
	if r := resolveFrom("Open Source Artist", two); r.MBID != "y" || r.Confidence != 0.6 {
		t.Fatalf("two of the name: %+v", r)
	}
	tie := []metadata.ArtistSummary{{MBID: "x", Name: "Mono", Score: 100}, {MBID: "y", Name: "Mono", Score: 98}}
	if r := resolveFrom("Mono", tie); r.Confidence != 0.5 {
		t.Fatalf("tie: %+v", r)
	}
	if r := resolveFrom("Public Domain Orchestra", one); r.MBID != "" {
		t.Fatalf("no match: %+v", r)
	}
}

// The library has Open Source Artist; Last.fm, ListenBrainz and MusicBrainz
// all point to Creative Commons Choir, ListenBrainz also to the jazz trio of
// the same name. Last.fm's artist without an id that MusicBrainz does not
// know is left out.
func TestRefreshAndSuggest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.s.Settings.SetSecret(ctx, LastFMKey, lfmfake.Key); err != nil {
		t.Fatal(err)
	}
	e.addArtist(t, mbfake.ArtistOpen, "Open Source Artist")
	rep, err := e.s.Refresh(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Asked != 3 || rep.Failed != 0 || rep.Enriched != 2 {
		t.Fatalf("report: %+v", rep)
	}
	res, err := e.s.Suggest(ctx, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.Items[0].MBID != mbfake.ArtistChoir || res.Items[1].MBID != mbfake.ArtistOther {
		t.Fatalf("suggestions: %+v", res.Items)
	}
	choir := res.Items[0]
	// 1 − (1 − 1.0·0.92·0.95)(1 − 0.9·1·1)(1 − 0.6·0.7·1)
	lf, lb, mb := 0.92*0.95, 0.9, 0.6*0.7
	want := pct(1 - (1-lf)*(1-lb)*(1-mb))
	if choir.Score != want || strings.Join(choir.Providers, ",") != "lastfm,listenbrainz,musicbrainz" || choir.SeedCount != 1 ||
		choir.Seeds[0].Name != "Open Source Artist" {
		t.Fatalf("choir: %+v (want score %d)", choir, want)
	}
	// Tags from Last.fm, the cover from ListenBrainz's most listened album.
	if strings.Join(choir.Tags, ",") != "choral,ambient,dream pop" || choir.Cover != musicbrainz.CoverName(mbfake.GroupChoral) {
		t.Fatalf("enriched: %+v", choir)
	}
	if !strings.Contains(choir.LastFMURL, "last.fm") || choir.Confidence < 0.9 {
		t.Fatalf("links: %+v", choir)
	}
	// The trio: ListenBrainz only, 203/812 of the best score.
	trio := 0.9 * (203 / 812.0)
	if res.Items[1].Score != pct(1-(1-trio)) {
		t.Fatalf("trio: %+v", res.Items[1])
	}

	// Filters.
	if r, _ := e.s.Suggest(ctx, Query{Tag: "Dream Pop"}); r.Total != 1 || r.Items[0].MBID != mbfake.ArtistChoir {
		t.Fatalf("tag: %+v", r)
	}
	if r, _ := e.s.Suggest(ctx, Query{MinScore: 50}); r.Total != 1 {
		t.Fatalf("min score: %+v", r)
	}
	if r, _ := e.s.Suggest(ctx, Query{Providers: []string{MusicBrainz}}); r.Total != 1 || r.Items[0].Score != 42 {
		t.Fatalf("provider: %+v", r)
	}
	if r, _ := e.s.Suggest(ctx, Query{Limit: 1, Offset: 1}); r.Total != 2 || len(r.Items) != 1 || r.Items[0].MBID != mbfake.ArtistOther {
		t.Fatalf("paging: %+v", r)
	}
	if len(res.Tags) == 0 || res.Tags[0].Name != "ambient" || res.Tags[0].Count != 2 {
		t.Fatalf("tag counts: %+v", res.Tags)
	}

	// Not interested.
	if _, err := e.s.Exclude(ctx, mbfake.ArtistOther, "", 0); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.s.Suggest(ctx, Query{}); r.Total != 1 {
		t.Fatalf("excluded: %+v", r)
	}
	ex, _ := e.s.Exclusions(ctx)
	if len(ex) != 1 || ex[0].Name != "Open Source Artist" {
		t.Fatalf("exclusions: %+v", ex)
	}
	if err := e.s.Include(ctx, mbfake.ArtistOther); err != nil {
		t.Fatal(err)
	}

	// Once the choir is in the library it is no longer suggested, but it
	// shows among the artist's similar artists, and the choir's own answers
	// (asked on the next refresh) do not suggest Open Source Artist back.
	e.addArtist(t, mbfake.ArtistChoir, "Creative Commons Choir")
	if r, _ := e.s.Suggest(ctx, Query{}); r.Total != 1 || r.Items[0].MBID != mbfake.ArtistOther {
		t.Fatalf("after adding: %+v", r)
	}
	sim, _ := e.s.Suggest(ctx, Query{Seeds: []string{mbfake.ArtistOpen}, IncludeLibrary: true})
	if sim.Total != 2 || sim.Items[0].ArtistID == 0 {
		t.Fatalf("similar incl. library: %+v", sim)
	}
	n, err := e.s.Pending(ctx, mustSeeds(t, e.s), []string{LastFM, ListenBrainz, MusicBrainz})
	if err != nil || n != 3 {
		t.Fatalf("pending: %d %v", n, err)
	}
	if rep, err := e.s.Refresh(ctx, nil); err != nil || rep.Asked != 3 {
		t.Fatalf("second refresh asks only the new artist: %+v %v", rep, err)
	}
	if r, _ := e.s.Suggest(ctx, Query{}); r.Total != 1 || r.Items[0].SeedCount != 1 {
		t.Fatalf("after refresh: %+v", r)
	}
}

func mustSeeds(t *testing.T, s *Service) []Seed {
	t.Helper()
	list, err := s.LibrarySeeds(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// Two library artists pointing to one suggestion rank it above an equally
// similar artist only one points to.
func TestScoringAcrossSeeds(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addArtist(t, "00000000-0000-4000-8000-000000000001", "A")
	e.addArtist(t, "00000000-0000-4000-8000-000000000002", "B")
	cfg := ConfigKey.Default
	cfg.MusicBrainz, cfg.ListenBrainz = false, false
	cfg.LastFM = true
	if err := settings.Set(ctx, e.s.Settings, nil, ConfigKey, cfg); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Settings.SetSecret(ctx, LastFMKey, lfmfake.Key); err != nil {
		t.Fatal(err)
	}
	both, one := "00000000-0000-4000-8000-0000000000b0", "00000000-0000-4000-8000-0000000000c0"
	for _, seed := range []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"} {
		list := []Candidate{{MBID: both, Name: "Both", Score: 0.5, Confidence: 1}}
		if strings.HasSuffix(seed, "1") {
			list = append(list, Candidate{MBID: one, Name: "One", Score: 0.5, Confidence: 1})
		}
		if err := e.s.store(ctx, LastFM, seed, list, nil); err != nil {
			t.Fatal(err)
		}
	}
	r, err := e.s.Suggest(ctx, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 2 || r.Items[0].Name != "Both" || r.Items[0].Score != 75 || r.Items[1].Score != 50 || r.Items[0].SeedCount != 2 {
		t.Fatalf("%+v", r.Items)
	}
	// A guess below the confidence floor never counts.
	if err := e.s.store(ctx, LastFM, "00000000-0000-4000-8000-000000000002",
		[]Candidate{{MBID: one, Name: "One", Score: 1, Confidence: 0.4}}, nil); err != nil {
		t.Fatal(err)
	}
	r, _ = e.s.Suggest(ctx, Query{})
	for _, it := range r.Items {
		if it.Name == "One" && it.Score != 50 {
			t.Fatalf("low confidence counted: %+v", it)
		}
	}
}

// Spotify refuses related artists to new apps: the refresh records it and
// stops asking; with extended access the related artists map to MusicBrainz
// through MusicBrainz's links to Spotify.
func TestSpotify(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cfg := Config{Spotify: true, RefreshDays: 7}
	if err := settings.Set(ctx, e.s.Settings, nil, ConfigKey, cfg); err != nil {
		t.Fatal(err)
	}
	_ = e.s.Settings.SetSecret(ctx, SpotifyID, spfake.ClientID)
	_ = e.s.Settings.SetSecret(ctx, SpotifySecret, spfake.ClientSecret)
	e.addArtist(t, mbfake.ArtistOpen, "Open Source Artist")
	e.addArtist(t, "00000000-0000-4000-8000-000000000009", "Radiohead")
	rep, err := e.s.Refresh(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 || rep.Asked != 1 || e.s.SpotifyRelated() != "restricted" {
		t.Fatalf("restricted: %+v %s", rep, e.s.SpotifyRelated())
	}
	st, _ := e.s.Status(ctx)
	if st[3].ID != Spotify || st[3].Failed != 1 || !strings.Contains(st[3].LastError, "related artists") {
		t.Fatalf("status: %+v", st[3])
	}
	e.sp.Related = true
	e.s.ResetSpotify()
	if _, err := e.s.Refresh(ctx, []string{mbfake.ArtistOpen}); err != nil {
		t.Fatal(err)
	}
	r, _ := e.s.Suggest(ctx, Query{})
	if r.Total != 1 || r.Items[0].MBID != mbfake.ArtistChoir || r.Items[0].SpotifyURL == "" || r.Items[0].Confidence != 1 {
		t.Fatalf("spotify: %+v", r.Items)
	}
}

func TestBadKeyStops(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := settings.Set(ctx, e.s.Settings, nil, ConfigKey, Config{LastFM: true, RefreshDays: 7}); err != nil {
		t.Fatal(err)
	}
	_ = e.s.Settings.SetSecret(ctx, LastFMKey, "wrong")
	for i := 0; i < 4; i++ {
		e.addArtist(t, "00000000-0000-4000-8000-00000000000"+string(rune('1'+i)), "Artist "+string(rune('A'+i)))
	}
	rep, err := e.s.Refresh(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Asked != 1 || e.lfm.Count() != 1 {
		t.Fatalf("a refused key stops the provider: %+v, %d requests", rep, e.lfm.Count())
	}
	// The failed artist is not asked again within hours; the next one is (and stops the run again).
	if rep, _ := e.s.Refresh(ctx, nil); rep.Asked != 1 || e.lfm.Count() != 2 {
		t.Fatalf("next run: %+v, %d requests", rep, e.lfm.Count())
	}
}

func TestExcludeValidation(t *testing.T) {
	e := newEnv(t)
	if _, err := e.s.Exclude(context.Background(), "../etc", "", 0); !errors.Is(err, ErrInvalidMBID) {
		t.Fatal(err)
	}
}

func pct(v float64) int { return int(v*100 + 0.5) }
