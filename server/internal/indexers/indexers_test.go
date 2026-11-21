package indexers

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

func newService(t *testing.T) (*Service, *fake.Server, string) {
	t.Helper()
	d := db.OpenTest(t)
	box, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := New(d, box, settings.New(d, box), slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.PolicyOverride = &httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	f := fake.New("fakekey", "sourcekey")
	f.SlowDelay = 300 * time.Millisecond
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return s, f, srv.URL
}

func add(t *testing.T, s *Service, name, kind, base string) Indexer {
	t.Helper()
	in := DefaultInput()
	key := "fakekey"
	in.Name, in.Kind, in.BaseURL, in.APIKey = name, Kind(kind), base, &key
	i, err := s.Create(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{0, time.Minute, 5 * time.Minute, 10 * time.Minute, 20 * time.Minute}
	for n, w := range want {
		if got := Backoff(n); got != w {
			t.Errorf("Backoff(%d) = %v", n, got)
		}
	}
	if Backoff(50) != 12*time.Hour {
		t.Fatal(Backoff(50))
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter()
	now := time.Now()
	for i := 0; i < 3; i++ {
		if ok, _ := l.allow(1, 3, time.Minute, now.Add(time.Duration(i)*time.Second)); !ok {
			t.Fatal("refused early")
		}
	}
	ok, wait := l.allow(1, 3, time.Minute, now.Add(10*time.Second))
	if ok || wait != 50*time.Second {
		t.Fatal(ok, wait)
	}
	if ok, _ := l.allow(2, 3, time.Minute, now); !ok {
		t.Fatal("limits are per indexer")
	}
	if ok, _ := l.allow(1, 3, time.Minute, now.Add(61*time.Second)); !ok {
		t.Fatal("window did not slide")
	}
}

func TestDefaultCategoriesAndSearchCategories(t *testing.T) {
	c := &znab.Caps{Categories: []znab.Category{{ID: 2000, Subcat: []znab.Category{{ID: 2040}}}, {ID: 3000, Subcat: []znab.Category{{ID: 3030}, {ID: 3040}}},
		{ID: 5000}, {ID: 7000, Subcat: []znab.Category{{ID: 7020}}}, {ID: 100001}, {ID: 8000}}}
	got := DefaultCategories(c)
	if !slices.Equal(got.Movies, []int{2000, 2040}) || !slices.Equal(got.Music, []int{3000, 3040}) || !slices.Equal(got.TV, []int{5000}) ||
		!slices.Equal(got.Books, []int{3030, 7000, 7020}) {
		t.Fatalf("%+v", got)
	}
	if s := searchCategories([]int{2000, 2040, 5040, 100001}); !slices.Equal(s, []int{2000, 5040, 100001}) {
		t.Fatal(s)
	}
}

func TestBuildRequest(t *testing.T) {
	full := &znab.Caps{LimitMax: 100, LimitDefault: 50, Searching: []znab.SearchCap{
		{Type: znab.SearchGeneric, Available: true, Params: []string{"q"}},
		{Type: znab.SearchMovie, Available: true, Params: []string{"q", "imdbid"}},
		{Type: znab.SearchTV, Available: true, Params: []string{"q", "tvdbid", "season", "ep"}},
	}}
	ind := Indexer{Categories: Categories{Movies: []int{2000, 2040}, TV: []int{5000}}}
	r, ok := buildRequest(ind, full, Query{Type: SearchMovie, IMDBID: "tt1254207", TMDBID: 10378, Limit: 500})
	if !ok || r.Type != znab.SearchMovie || r.IMDBID != "tt1254207" || r.TMDBID != 0 || r.Limit != 100 || !slices.Equal(r.Categories, []int{2000}) {
		t.Fatalf("%+v %v", r, ok)
	}
	// Only an id the indexer does not take: nothing to search with.
	if _, ok := buildRequest(ind, full, Query{Type: SearchMovie, TMDBID: 10378}); ok {
		t.Fatal("unsupported query accepted")
	}
	// Music is not offered: falls back to a text search, artist folded into q.
	r, ok = buildRequest(ind, full, Query{Type: SearchMusic, Artist: "Someone", Album: "Record"})
	if !ok || r.Type != znab.SearchGeneric || r.Query != "Someone Record" {
		t.Fatalf("%+v", r)
	}
	r, ok = buildRequest(ind, full, Query{Type: SearchTV, Term: "show", TVDBID: 5, Season: "1", Episode: "2", Categories: []int{5040}})
	if !ok || r.TVDBID != 5 || r.Season != "1" || r.Episode != "2" || r.Query != "show" || !slices.Equal(r.Categories, []int{5040}) || r.Limit != 50 {
		t.Fatalf("%+v", r)
	}
}

func TestSearchAgainstFakes(t *testing.T) {
	s, f, base := newService(t)
	ctx := context.Background()
	tz := add(t, s, "Torznab", "torznab", base+"/torznab")
	if tz.Caps == nil || tz.Categories.Empty() || !slices.Contains(tz.Categories.Movies, 2040) {
		t.Fatalf("categories from caps: %+v", tz.Categories)
	}
	add(t, s, "Quirky", "torznab", base+"/quirky")
	nz := add(t, s, "Newznab", "newznab", base+"/newznab")
	broken := add(t, s, "Broken", "torznab", base+"/broken")
	add(t, s, "Slow", "torznab", base+"/slow")

	var events []Event
	res, err := s.SearchStream(ctx, Query{Type: SearchMovie, Term: "big buck bunny"}, func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[len(events)-1].Indexer.IndexerName != "Slow" {
		t.Fatalf("events arrive as indexers answer: %+v", events)
	}
	statuses := map[string]Status{}
	for _, ir := range res.Indexers {
		statuses[ir.IndexerName] = ir.Status
	}
	if statuses["Torznab"] != StatusOK || statuses["Quirky"] != StatusOK || statuses["Newznab"] != StatusOK || statuses["Slow"] != StatusOK {
		t.Fatalf("%+v", res.Indexers)
	}
	// "Broken" has no caps (the fake fails everything).
	if statuses["Broken"] != StatusError {
		t.Fatalf("%+v", statuses)
	}
	// Torznab and Quirky return the same info hash: one release. Newznab (usenet) is separate.
	var titles []string
	for _, r := range res.Releases {
		titles = append(titles, r.IndexerName+": "+r.Title)
		if strings.Contains(r.DownloadURL, "fakekey") || strings.Contains(r.GUID, "fakekey") {
			t.Fatalf("API key kept in release: %+v", r)
		}
	}
	if len(res.Releases) != 2 {
		t.Fatalf("dedupe: %v", titles)
	}
	for _, ir := range res.Indexers {
		if (ir.IndexerName == "Quirky" || ir.IndexerName == "Torznab") && ir.Count != 1 {
			t.Fatalf("%+v", ir)
		}
	}
	b, _ := s.Get(ctx, broken.ID)
	if b.Health.Failures != 1 || b.HealthStatus(time.Now()) != HealthFailing || !b.InBackoff(time.Now()) || b.Health.LastErrorCode == "" {
		t.Fatalf("health: %+v", b.Health)
	}
	// Second identical search: from the cache, Broken skipped while in backoff.
	before := f.Count("torznab")
	res, err = s.Search(ctx, Query{Type: SearchMovie, Term: "big buck bunny"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Count("torznab") != before {
		t.Fatal("identical query hit the indexer again")
	}
	for _, ir := range res.Indexers {
		if ir.IndexerName == "Broken" && ir.Status != StatusBackoff {
			t.Fatalf("%+v", ir)
		}
		if ir.IndexerName == "Torznab" && !ir.Cached {
			t.Fatalf("%+v", ir)
		}
	}
	// The preferred indexer wins a duplicate in the JSON search.
	if res.Releases[0].IndexerName != "Newznab" && res.Releases[0].IndexerName != "Quirky" && res.Releases[0].IndexerName != "Torznab" {
		t.Fatal(res.Releases[0])
	}
	// Fetch a release file through the service (counts a grab; the key is put back).
	var nzbRel Release
	for _, r := range res.Releases {
		if r.IndexerID == nz.ID {
			nzbRel = r
		}
	}
	file, err := s.Fetch(ctx, nzbRel)
	if err != nil || !strings.Contains(string(file.Data), "<nzb") || !strings.HasSuffix(file.Name, ".nzb") {
		t.Fatalf("%+v %v", file, err)
	}
	tok, err := s.DownloadToken(nzbRel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FetchToken(ctx, tz.ID, tok); err != ErrBadToken {
		t.Fatal("token for another indexer accepted")
	}
	if n, _ := s.GrabsToday(ctx, nz.ID); n != 1 {
		t.Fatal(n)
	}
	// Grab limit.
	in := inputOf(mustGet(t, s, nz.ID))
	one := 1
	in.GrabLimitPerDay = &one
	if _, err := s.Update(ctx, nz.ID, in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fetch(ctx, nzbRel); err != ErrGrabLimit {
		t.Fatal(err)
	}

	// The search log has every search with per-indexer outcomes.
	log, err := s.SearchLog(ctx, "", 10)
	if err != nil || len(log) != 2 || len(log[0].Indexers) != 5 || log[0].Query != "big buck bunny" {
		t.Fatalf("%+v %v", log, err)
	}
}

func mustGet(t *testing.T, s *Service, id int64) Indexer {
	t.Helper()
	i, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestRequestLimitAndAuthError(t *testing.T) {
	s, _, base := newService(t)
	ctx := context.Background()
	in := DefaultInput()
	key := "fakekey"
	two, minute := 2, 60
	in.Name, in.Kind, in.BaseURL, in.APIKey, in.RequestLimit, in.RequestPeriodSec = "Limited", Torznab, base+"/torznab", &key, &two, &minute
	ind, err := s.Create(ctx, in) // caps fetch during create is not counted (not through the limiter)
	if err != nil {
		t.Fatal(err)
	}
	statuses := []Status{}
	for _, q := range []string{"sintel", "tears", "elephants"} {
		r, _ := s.Search(ctx, Query{Term: q})
		statuses = append(statuses, r.Indexers[0].Status)
	}
	if !slices.Equal(statuses, []Status{StatusOK, StatusOK, StatusRateLimited}) {
		t.Fatal(statuses)
	}
	if mustGet(t, s, ind.ID).Health.Failures != 0 {
		t.Fatal("our own limit is not an indexer failure")
	}

	wrong := "wrong"
	in2 := DefaultInput()
	in2.Name, in2.Kind, in2.BaseURL, in2.APIKey = "Wrong key", Torznab, base+"/torznab", &wrong
	res, err := s.TestInput(ctx, in2, nil)
	if err != nil || res.OK || res.ErrorCode != "auth" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := s.TestInput(ctx, Input{Name: "x", Kind: Torznab, BaseURL: "http://169.254.169.254"}, nil); err == nil {
		t.Fatal("metadata address accepted")
	}
	s.PolicyOverride = &httpx.Policy{}
	s.limiter = newLimiter()
	r, _ := s.Search(ctx, Query{Term: "zzz"})
	if r.Indexers[0].Status != StatusError || r.Indexers[0].ErrorCode != "blocked" {
		t.Fatalf("loopback reachable without the override: %+v", r.Indexers)
	}
}

func TestRSSSync(t *testing.T) {
	s, _, base := newService(t)
	ctx := context.Background()
	add(t, s, "Torznab", "torznab", base+"/torznab")
	nz := add(t, s, "Newznab", "newznab", base+"/newznab")
	in := inputOf(nz)
	in.EnableRSS = false
	if _, err := s.Update(ctx, nz.ID, in); err != nil {
		t.Fatal(err)
	}
	ch, stop := s.SubscribeRSS(4)
	defer stop()
	since := time.Now().Add(-time.Second)
	res, err := s.SyncRSS(ctx)
	if err != nil || res.Indexers != 1 || res.New != len(fake.Catalog) {
		t.Fatalf("%+v %v", res, err)
	}
	batch := <-ch
	if len(batch) != len(fake.Catalog) {
		t.Fatal(len(batch))
	}
	// Nothing new the second time.
	res, err = s.SyncRSS(ctx)
	if err != nil || res.New != 0 || res.Fetched != len(fake.Catalog) {
		t.Fatalf("%+v %v", res, err)
	}
	seen, err := s.SeenSince(ctx, since, 0)
	if err != nil || len(seen) != len(fake.Catalog) || seen[0].IndexerName != "Torznab" {
		t.Fatalf("%d %v", len(seen), err)
	}
	// Retention pruning.
	s.Now = func() time.Time { return time.Now().Add(30 * 24 * time.Hour) }
	res, err = s.SyncRSS(ctx)
	if err != nil || res.Pruned != int64(len(fake.Catalog)) {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSourcesImportAndSync(t *testing.T) {
	s, f, base := newService(t)
	ctx := context.Background()
	for _, tc := range []struct {
		kind SourceKind
		url  string
	}{{Prowlarr, base + "/prowlarr"}, {Jackett, base + "/jackett"}} {
		items, err := s.PreviewSource(ctx, 0, SourceInput{Kind: tc.kind, URL: tc.url, APIKey: "sourcekey"})
		if err != nil || len(items) != 2 || items[0].Action != "add" {
			t.Fatalf("%s preview: %+v %v", tc.kind, items, err)
		}
		if _, err := s.PreviewSource(ctx, 0, SourceInput{Kind: tc.kind, URL: tc.url, APIKey: "nope"}); err == nil {
			t.Fatalf("%s: wrong key accepted", tc.kind)
		}
	}
	res, err := s.CreateSource(ctx, SourceInput{Kind: Prowlarr, URL: base + "/prowlarr", APIKey: "sourcekey"})
	if err != nil || res.Added != 2 || res.Source.IndexerCount != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	list, _ := s.List(ctx)
	if len(list) != 2 || list[0].SourceName != "Prowlarr" || list[0].Categories.Empty() {
		t.Fatalf("%+v", list)
	}
	r, err := s.Search(ctx, Query{Term: "sintel"})
	if err != nil || len(r.Releases) != 2 {
		t.Fatalf("search through the source's feeds: %+v %v", r, err)
	}
	// The source drops indexer 2 and adds 3: one added, one disabled, nothing deleted.
	f.Lock()
	f.ProwlarrIDs = []int{1, 3}
	f.Unlock()
	res, err = s.SyncSource(ctx, res.Source.ID)
	if err != nil || res.Added != 1 || res.Disabled != 1 || res.Unchanged != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	list, _ = s.List(ctx)
	enabled := 0
	for _, i := range list {
		if i.Enabled {
			enabled++
		}
	}
	if len(list) != 3 || enabled != 2 {
		t.Fatalf("%d indexers, %d enabled", len(list), enabled)
	}
	// Jackett with an exclusion; names that clash get the source name.
	res, err = s.CreateSource(ctx, SourceInput{Kind: Jackett, URL: base + "/jackett", APIKey: "sourcekey", Exclude: []string{"fakequirky"}})
	if err != nil || res.Added != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	list, _ = s.List(ctx)
	names := []string{}
	for _, i := range list {
		names = append(names, i.Name)
	}
	if !slices.Contains(names, "Fake Torznab (Jackett)") {
		t.Fatal(names)
	}
	if err := s.DeleteSource(ctx, res.Source.ID); err != nil {
		t.Fatal(err)
	}
	if list2, _ := s.List(ctx); len(list2) != len(list) {
		t.Fatal("deleting a source removed indexers")
	}
}
