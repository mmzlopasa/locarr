package streamero_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero/fake"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

var loopback = httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}}

type env struct {
	ctx  context.Context
	db   *db.DB
	fake *fake.Server
	url  string
	svc  *streamero.Service
	st   *settings.Store
	auth *auth.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	d := db.OpenTest(t)
	box, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	f := fake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	st := settings.New(d, box)
	q := jobs.New(d, slog.Default(), jobs.Options{})
	svc := &streamero.Service{DB: d, Settings: st, Roots: &rootfolders.Service{DB: d}, Timeline: &timeline.Store{DB: d}, Jobs: q,
		Policy: func(context.Context) (httpx.Policy, error) { return loopback, nil }}
	q.Register(streamero.TaskScan, svc.RunScan, jobs.KindOptions{})
	q.Register(streamero.TaskIndex, func(context.Context, *jobs.Job) error { return nil }, jobs.KindOptions{})
	return &env{ctx: ctx, db: d, fake: f, url: srv.URL, svc: svc, st: st, auth: auth.NewService(d, box, "setup")}
}

func (e *env) configure(t *testing.T, c streamero.Config) {
	t.Helper()
	c.Enabled, c.URL = true, e.url
	key := fake.AdminKey
	if _, err := e.svc.Save(e.ctx, streamero.Input{Config: c, APIKey: &key}); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{"http://s:8787/": "http://s:8787", "https://m.example.com/api/v1": "https://m.example.com",
		" http://s/sub/ ": "http://s/sub"} {
		if got, err := streamero.NormalizeURL(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "ftp://x", "http://u:p@x", "streamero:8787", "http://x/?a=1"} {
		if _, err := streamero.NormalizeURL(bad); !errors.Is(err, streamero.ErrBadURL) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestPathsAndSuggestions(t *testing.T) {
	maps := []streamero.PathMapping{{Locarr: "/data", Streamero: "/media"}, {Locarr: "/data/movies-4k", Streamero: "/uhd"}}
	if got := streamero.ToStreamero("/data/movies/Sintel (2010)/a.mkv", maps); got != "/media/movies/Sintel (2010)/a.mkv" {
		t.Fatal(got)
	}
	if got := streamero.ToStreamero("/data/movies-4k/x", maps); got != "/uhd/x" {
		t.Fatal("longest mapping must win:", got)
	}
	if got := streamero.ToStreamero("/other/x", maps); got != "/other/x" {
		t.Fatal(got)
	}
	libs := []streamero.Library{{ID: "m", Paths: []string{"/media/movies"}}, {ID: "s", Paths: []string{"/media/shows"}},
		{ID: "all", Paths: []string{"/media"}}}
	roots := []rootfolders.Folder{{ID: 1, Path: "/data/movies"}, {ID: 2, Path: "/data/shows"}, {ID: 3, Path: "/elsewhere"}}
	views := streamero.Suggest(libs, roots, maps)
	if len(views[0].Suggested) != 1 || views[0].Suggested[0] != 1 || len(views[2].Suggested) != 2 {
		t.Fatalf("%+v", views)
	}
	c := streamero.Config{PathMappings: maps}
	if got := streamero.Resolve([]string{"/data/shows/Cosmos/S01E01.mkv"}, roots, c, libs); strings.Join(got, ",") != "all,s" {
		t.Fatal(got)
	}
	// A root folder mapped by hand wins over paths.
	c.LibraryMap = []streamero.LibraryMapping{{RootFolderID: 2, LibraryIDs: []string{"s", "gone"}}}
	if got := streamero.Resolve([]string{"/data/shows/Cosmos/S01E01.mkv"}, roots, c, libs); strings.Join(got, ",") != "s" {
		t.Fatal(got)
	}
	if got := streamero.Resolve([]string{"/elsewhere/x.mkv"}, roots, streamero.Config{}, libs); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestCheck(t *testing.T) {
	e := newEnv(t)
	res, err := e.svc.Check(e.ctx, e.url, fake.AdminKey)
	if err != nil || res.Info.Name != "Fake Streamero" || len(res.Libraries) != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := e.svc.Check(e.ctx, e.url, "sk_wrong"); !errors.Is(err, streamero.ErrAuth) {
		t.Fatal(err)
	}
	if _, err := e.svc.Check(e.ctx, e.url, fake.ViewerKey); !errors.Is(err, streamero.ErrNotAdmin) {
		t.Fatal(err)
	}
	if _, err := e.svc.Check(e.ctx, e.url+"/nothing-here", fake.AdminKey); !errors.Is(err, streamero.ErrNotStreamero) {
		t.Fatal(err)
	}
	// The default policy (as for indexers) refuses loopback.
	e.svc.Policy = nil
	if _, err := e.svc.Check(e.ctx, e.url, fake.AdminKey); !errors.Is(err, httpx.ErrBlocked) {
		t.Fatal("loopback must be refused by default:", err)
	}
	// Saving switched on checks the key; an unusable key is not stored.
	e.svc.Policy = func(context.Context) (httpx.Policy, error) { return loopback, nil }
	bad := "sk_wrong"
	if _, err := e.svc.Save(e.ctx, streamero.Input{Config: streamero.Config{Enabled: true, URL: e.url}, APIKey: &bad}); !errors.Is(err, streamero.ErrAuth) {
		t.Fatal(err)
	}
	if ok, _ := e.st.Configured(e.ctx, streamero.APIKey); ok {
		t.Fatal("a refused key was stored")
	}
}

func TestScansAreBatchedPerLibrary(t *testing.T) {
	e := newEnv(t)
	roots := &rootfolders.Service{DB: e.db, Roots: []string{"/"}}
	if _, err := e.db.W.ExecContext(e.ctx, `INSERT INTO root_folders (id, path, kind, created_at) VALUES (1, '/data/movies', 'movies', 0),
		(2, '/data/shows', 'series', 0)`); err != nil {
		t.Fatal(err)
	}
	e.svc.Roots = roots
	e.configure(t, streamero.Config{PathMappings: []streamero.PathMapping{{Locarr: "/data", Streamero: "/media"}}})
	n := streamero.Notifier{S: e.svc}
	if n.Name() != "Streamero" || !mediaserver.RecordsOutcome(n) {
		t.Fatal(n.Name())
	}
	for _, c := range []mediaserver.Change{
		{ItemRef: "movie:1", Action: "imported", Title: "Sintel", Year: 2010, Paths: []string{"/data/movies/Sintel (2010)/Sintel.mkv"}},
		{ItemRef: "movie:2", Action: "upgraded", Title: "Tears", Paths: []string{"/data/movies/Tears/Tears.mkv"}},
		{ItemRef: "episode:5", Action: "imported", Title: "Cosmos", Paths: []string{"/data/shows/Cosmos/S01E01.mkv"}},
		{ItemRef: "movie:9", Action: "imported", Title: "Lost", Paths: []string{"/nowhere/x.mkv"}},
	} {
		if err := n.Notify(e.ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	// One debounced job for all of them.
	list, err := e.svc.Jobs.List(e.ctx, jobs.Queued, 10)
	if err != nil {
		t.Fatal(err)
	}
	scanJobs := 0
	for _, j := range list {
		if j.Kind == streamero.TaskScan {
			scanJobs++
			if time.Until(j.RunAt) < 20*time.Second {
				t.Fatal("scan job not debounced:", j.RunAt)
			}
		}
	}
	if scanJobs != 1 {
		t.Fatalf("%d scan jobs", scanJobs)
	}
	if err := e.svc.RunScan(e.ctx, &jobs.Job{Attempt: 1, MaxAttempts: 6}); err != nil {
		t.Fatal(err)
	}
	scans, _, _, _ := e.fake.Snapshot()
	if strings.Join(scans, ",") != fake.MoviesID+","+fake.ShowsID {
		t.Fatalf("scans %v", scans)
	}
	tl := &timeline.Store{DB: e.db}
	evs, err := tl.List(e.ctx, "movie:1", 10)
	if err != nil || len(evs) != 1 || evs[0].Type != timeline.TypeNotified || !strings.Contains(evs[0].Message, "Movies") {
		t.Fatalf("%+v %v", evs, err)
	}
	if evs, _ := tl.List(e.ctx, "movie:9", 10); len(evs) != 1 || !strings.Contains(evs[0].Message, "no Streamero library contains /nowhere/x.mkv") {
		t.Fatalf("%+v", evs)
	}
	if _, _, pending, _ := e.svc.StatusInfo(e.ctx); pending != 0 {
		t.Fatal("pending left:", pending)
	}

	// Failures: kept for a retry, recorded only when the last attempt fails.
	e.fake.FailScans = true
	_ = n.Notify(e.ctx, mediaserver.Change{ItemRef: "movie:3", Action: "renamed", Paths: []string{"/data/movies/X/x.mkv"}})
	if err := e.svc.RunScan(e.ctx, &jobs.Job{Attempt: 1, MaxAttempts: 6}); err == nil {
		t.Fatal("want an error so the job retries")
	}
	if evs, _ := tl.List(e.ctx, "movie:3", 10); len(evs) != 0 {
		t.Fatalf("recorded before the last attempt: %+v", evs)
	}
	if err := e.svc.RunScan(e.ctx, &jobs.Job{Attempt: 6, MaxAttempts: 6}); err == nil {
		t.Fatal("want an error")
	}
	if evs, _ := tl.List(e.ctx, "movie:3", 10); len(evs) != 1 || !strings.Contains(evs[0].Message, "could not notify Streamero") {
		t.Fatalf("%+v", evs)
	}
	if st, _, pending, _ := e.svc.StatusInfo(e.ctx); pending != 0 || st.LastScanError == "" {
		t.Fatalf("%+v %d", st, pending)
	}
}

func TestIndexAndLinks(t *testing.T) {
	e := newEnv(t)
	e.configure(t, streamero.Config{PublicURL: "https://watch.example.com"})
	n, err := e.svc.Refresh(e.ctx)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	titles := []streamero.Title{{Kind: "movie", TMDbID: 45745}, {Kind: "movie", TMDbID: 10378}, {Kind: "series", TVDbID: 434343},
		{Kind: "series", TMDbID: 45745}}
	links, err := e.svc.Links(e.ctx, titles)
	if err != nil {
		t.Fatal(err)
	}
	if links[0] != "https://watch.example.com/item/"+fake.SintelID || links[2] != "https://watch.example.com/item/"+fake.CosmosID ||
		links[1] != "" || links[3] != "" {
		t.Fatalf("%v", links)
	}
	// Known items with ids are not read again; new ones are, removed ones go.
	calls := e.fake.DetailCalls
	e.fake.AddItem(fake.Item{ID: "new-1", LibraryID: fake.MoviesID, Kind: "movie", Title: "Big Buck Bunny", IDs: map[string]string{"Tmdb": "10378"}})
	if n, err := e.svc.Refresh(e.ctx); err != nil || n != 3 || e.fake.DetailCalls != calls+1 {
		t.Fatal(n, err, e.fake.DetailCalls-calls)
	}
	e.fake.Reset()
	if n, _ := e.svc.Refresh(e.ctx); n != 2 {
		t.Fatal(n)
	}
	// Off: no links.
	if _, err := e.svc.Save(e.ctx, streamero.Input{Config: streamero.Config{URL: e.url}}); err != nil {
		t.Fatal(err)
	}
	if links, _ := e.svc.Links(e.ctx, titles); len(links) != 0 {
		t.Fatal(links)
	}
}

func TestProviderSignIn(t *testing.T) {
	e := newEnv(t)
	p := streamero.Provider{S: e.svc}
	e.auth.RegisterProvider(p)
	if len(e.auth.Providers(e.ctx)) != 1 {
		t.Fatal("listed while switched off")
	}
	e.configure(t, streamero.Config{SignIn: streamero.SignIn{Enabled: true}})
	if ps := e.auth.Providers(e.ctx); len(ps) != 2 || ps[1].ID != "streamero" {
		t.Fatalf("%+v", ps)
	}
	login := func(user, pw, code string) (auth.Principal, error) {
		pr, _, _, err := e.auth.LoginWithProvider(e.ctx, "streamero", auth.Credentials{Username: user, Password: pw, TOTPCode: code}, auth.Device{})
		return pr, err
	}
	if _, err := login("ana", "wrong-pass", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if _, err := login("off", fake.Password, ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal("disabled Streamero account:", err)
	}
	// Unknown account and auto-create off: refused.
	if _, err := login("ana", fake.Password, ""); !errors.Is(err, auth.ErrIdentityNotLinked) {
		t.Fatal(err)
	}
	// Auto-create on: a requester, even for a Streamero administrator.
	e.configure(t, streamero.Config{SignIn: streamero.SignIn{Enabled: true, AutoCreate: true}})
	pr, err := login("admin", fake.Password, "")
	if err != nil || pr.User.Role != auth.RoleRequester || pr.User.Username != "admin" {
		t.Fatalf("%+v %v", pr.User, err)
	}
	// The local "admin" name was free here; a second account with a taken name gets a suffix.
	if _, err := e.auth.CreateUser(e.ctx, auth.NewUser{Username: "ana", Password: "ana-password", Role: auth.RoleRequester}); err != nil {
		t.Fatal(err)
	}
	pr2, err := login("ana", fake.Password, "")
	if err != nil || pr2.User.Username != "ana-2" {
		t.Fatalf("%+v %v", pr2.User, err)
	}
	// Signing in again finds the same user.
	if again, err := login("admin", fake.Password, ""); err != nil || again.User.ID != pr.User.ID {
		t.Fatal(err)
	}
	// Two-factor step.
	if _, err := login("tom", fake.Password, ""); !errors.Is(err, auth.ErrTOTPRequired) {
		t.Fatal(err)
	}
	if _, err := login("tom", fake.Password, "000000"); !errors.Is(err, auth.ErrInvalidTOTP) {
		t.Fatal(err)
	}
	// Linking: tom → the local user ana, by an administrator.
	local, err := e.auth.CreateUser(e.ctx, auth.NewUser{Username: "tomislav", Password: "tom-password", Role: auth.RoleManager})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.auth.Link(e.ctx, local.ID, "streamero", fake.TomID, "tom"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.auth.Link(e.ctx, local.ID+999, "streamero", fake.TomID, "tom"); err == nil {
		t.Fatal("linked a missing user")
	}
	if _, err := e.auth.Link(e.ctx, pr.User.ID, "streamero", fake.TomID, "tom"); !errors.Is(err, auth.ErrIdentityTaken) {
		t.Fatal(err)
	}
	if got, err := login("tom", fake.Password, fake.TOTPCode); err != nil || got.User.ID != local.ID || got.User.Role != auth.RoleManager {
		t.Fatalf("%+v %v", got.User, err)
	}
	// A disabled Locarr user stays disabled.
	yes := true
	adminP, _ := setupAdmin(t, e)
	if _, err := e.auth.UpdateUser(e.ctx, adminP, local.ID, auth.UserChange{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, err := login("tom", fake.Password, fake.TOTPCode); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal(err)
	}
	// Every Streamero session used for a check was ended at once.
	_, logins, logouts, open := e.fake.Snapshot()
	if logins == 0 || logins != logouts || open != 0 {
		t.Fatalf("logins %d logouts %d open %d", logins, logouts, open)
	}
	// Switched off: the provider disappears.
	e.configure(t, streamero.Config{})
	if _, err := login("admin", fake.Password, ""); !errors.Is(err, auth.ErrUnknownProvider) {
		t.Fatal(err)
	}
}

func setupAdmin(t *testing.T, e *env) (auth.Principal, string) {
	t.Helper()
	p, tok, err := e.auth.Setup(e.ctx, "setup", "root", "root-password", "en", auth.Device{})
	if err != nil {
		t.Fatal(err)
	}
	return p, tok
}
