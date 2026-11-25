package downloads

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
)

func TestDeriveMapping(t *testing.T) {
	cases := []struct {
		remote, local, wantRemote, wantLocal string
		ok                                   bool
	}{
		// The owner's case: qBittorrent has /mnt/jellyfin/data/downloads at /downloads, Locarr the parent at /data.
		{"/downloads/Some.Movie.2024.1080p", "/data/downloads/Some.Movie.2024.1080p", "/downloads", "/data/downloads", true},
		{"/downloads/Some.Movie.2024.1080p/", "/data/downloads/Some.Movie.2024.1080p", "/downloads", "/data/downloads", true},
		{"/downloads//Some.Movie.2024.1080p", "/data/downloads/Some.Movie.2024.1080p/", "/downloads", "/data/downloads", true},
		{"/downloads/movies/Film.mkv", "/data/downloads/movies/Film.mkv", "/downloads", "/data/downloads", true},
		{"/downloads/complete/Film", "/data/torrents/Film", "/downloads/complete", "/data/torrents", true},
		{"/mnt/storage/downloads/Film", "/data/downloads/Film", "/mnt/storage", "/data", true},
		{`D:\Torrents\Film`, "/data/torrents/Film", `D:\Torrents`, "/data/torrents", true},
		{"/Film", "/data/downloads/Film", "", "", false},                // a single element cannot give a folder
		{"/downloads/Film", "/data/downloads/Other", "", "", false},     // nothing in common
		{"/data/downloads/Film", "/data/downloads/Film", "", "", false}, // same path: nothing to map
		{"/downloads/../etc/Film", "/data/etc/Film", "", "", false},
		{"downloads/Film", "/data/downloads/Film", "", "", false},
	}
	for _, c := range cases {
		r, l, ok := DeriveMapping(c.remote, c.local)
		if ok != c.ok || r != c.wantRemote || l != c.wantLocal {
			t.Errorf("DeriveMapping(%q, %q) = %q, %q, %v; want %q, %q, %v", c.remote, c.local, r, l, ok, c.wantRemote, c.wantLocal, c.ok)
		}
	}
}

func writeFile(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The client reports /downloads/<name>; the files sit at <data>/downloads/<name>.
func TestSuggestsMappingForInvisibleDownload(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.addClient(t, "qBit", "faketorrent", "10.1.5.16", nil)
	link, hash := testkit.RandomMagnet("Some.Movie.2024.1080p")
	d, err := e.s.Grab(ctx, GrabRequest{DownloadURL: link, Protocol: downloaders.Torrent, Title: "Some.Movie.2024.1080p", MediaKind: KindMovies})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(e.data, "downloads", "Some.Movie.2024.1080p", "movie.mkv"), 1000)
	// A same-named folder of another size elsewhere is not taken.
	writeFile(t, filepath.Join(e.data, "torrents", "Some.Movie.2024.1080p", "x.mkv"), 10)
	e.fakes["10.1.5.16"].set(hash, func(it *downloaders.Item) {
		it.State, it.RawState, it.Progress, it.Size = downloaders.StateSeeding, "uploading", 1, 1000
		it.SavePath, it.ContentPath = "/downloads", "/downloads/Some.Movie.2024.1080p"
	})
	_ = e.s.Poll(ctx)
	got, _ := e.s.Get(ctx, d.ID)
	if got.Status != StatusActive || !strings.Contains(got.Message, "not visible") || !strings.Contains(got.Message, "/data") && !strings.Contains(got.Message, e.data) {
		t.Fatalf("download = %+v", got)
	}
	sugs, err := e.s.Suggestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sugs) != 1 {
		t.Fatalf("suggestions = %+v", sugs)
	}
	s := sugs[0]
	if s.Host != "10.1.5.16" || s.RemotePath != "/downloads" || s.LocalPath != filepath.Join(e.data, "downloads") || s.ClientID != c.ID ||
		s.FoundAt != filepath.Join(e.data, "downloads", "Some.Movie.2024.1080p") || len(s.DownloadIDs) != 1 || s.DownloadIDs[0] != d.ID {
		t.Fatalf("suggestion = %+v", s)
	}
	q, _ := e.s.Queue(ctx, false)
	if len(q) != 1 || q[0].Suggestion == nil || q[0].Suggestion.RemotePath != "/downloads" {
		t.Fatalf("queue = %+v", q)
	}
	// Nothing is added without the user.
	if ms, _ := e.s.Mappings(ctx); len(ms) != 0 {
		t.Fatalf("mappings added silently: %+v", ms)
	}
	if _, err := e.s.CreateMapping(ctx, Mapping{Host: s.Host, RemotePath: s.RemotePath, LocalPath: s.LocalPath}); err != nil {
		t.Fatal(err)
	}
	if sugs, _ := e.s.Suggestions(ctx); len(sugs) != 0 {
		t.Fatalf("suggestion after adding it: %+v", sugs)
	}
	_ = e.s.Poll(ctx)
	got, _ = e.s.Get(ctx, d.ID)
	if got.Status != StatusCompleted || got.LocalPath != s.FoundAt {
		t.Fatalf("after the mapping: %+v", got)
	}
}

func TestSuggestionWalkFindsDeepFolderAndStaysInDataRoots(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.addClient(t, "qBit", "faketorrent", "seedbox", nil)
	writeFile(t, filepath.Join(e.data, "media", "incoming", "Deep.Film.mkv"), 500)
	if got := e.s.findLocal(ctx, "/srv/dl/Deep.Film.mkv", "", 500, false); got != "" {
		t.Fatalf("found without the walk: %s", got)
	}
	got := e.s.findLocal(ctx, "/srv/dl/Deep.Film.mkv", "", 500, true)
	if got != filepath.Join(e.data, "media", "incoming", "Deep.Film.mkv") {
		t.Fatalf("walk found %q", got)
	}
	sug := e.s.suggestion(nil, c, "/srv/dl/Deep.Film.mkv", got, SourceDownload)
	if sug == nil || sug.RemotePath != "/srv/dl" || sug.LocalPath != filepath.Join(e.data, "media", "incoming") {
		t.Fatalf("suggestion = %+v", sug)
	}
	// A symbolic link out of the data roots is never followed.
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "Leak.mkv"), 5)
	if err := os.Symlink(outside, filepath.Join(e.data, "downloads", "link")); err != nil {
		if err := os.MkdirAll(filepath.Join(e.data, "downloads"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(e.data, "downloads", "link")); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.s.findLocal(ctx, "/downloads/link/Leak.mkv", "", 5, true); got != "" {
		t.Fatalf("followed a link out of the data roots: %s", got)
	}
}

func TestClientFolderChecks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.addClient(t, "qBit", "faketorrent", "10.1.5.16", nil)
	f := newFake(downloaders.Torrent)
	f.savePath = "/downloads"
	f.cats = []downloaders.Category{{Name: "movies-locarr", SavePath: "/downloads/movies"}, {Name: "other", SavePath: "/elsewhere"}}
	e.fakes["10.1.5.16"] = f
	if err := os.MkdirAll(filepath.Join(e.data, "downloads", "movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := e.s.Test(ctx, c.ID, ClientInput{Name: "qBit", Implementation: "faketorrent", Host: "10.1.5.16", Port: 1234,
		CategoryMovies: "movies-locarr"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PathChecks) != 2 {
		t.Fatalf("checks = %+v", res.PathChecks)
	}
	for _, pc := range res.PathChecks {
		if pc.Visible || pc.Suggestion == nil || pc.Suggestion.Source != SourceFolder {
			t.Fatalf("check = %+v", pc)
		}
	}
	if s := res.PathChecks[0].Suggestion; s.RemotePath != "/downloads" || s.LocalPath != filepath.Join(e.data, "downloads") {
		t.Fatalf("default folder suggestion = %+v", s)
	}
	// A finished download is stronger evidence than a folder name.
	writeFile(t, filepath.Join(e.data, "downloads", "movies", "Film.mkv"), 42)
	f.items["h"] = downloaders.Item{ID: "h", Name: "Film.mkv", Progress: 1, Size: 42, SavePath: "/downloads/movies",
		ContentPath: "/downloads/movies/Film.mkv"}
	res, _ = e.s.Test(ctx, c.ID, ClientInput{Name: "qBit", Implementation: "faketorrent", Host: "10.1.5.16", Port: 1234,
		CategoryMovies: "movies-locarr"})
	if s := res.PathChecks[1].Suggestion; s == nil || s.Source != SourceDownload || s.RemotePath != "/downloads" {
		t.Fatalf("category suggestion = %+v", s)
	}
	if _, err := e.s.CreateMapping(ctx, Mapping{Host: "10.1.5.16", RemotePath: "/downloads", LocalPath: filepath.Join(e.data, "downloads")}); err != nil {
		t.Fatal(err)
	}
	res, _ = e.s.Test(ctx, c.ID, ClientInput{Name: "qBit", Implementation: "faketorrent", Host: "10.1.5.16", Port: 1234,
		CategoryMovies: "movies-locarr"})
	for _, pc := range res.PathChecks {
		if !pc.Visible || pc.Suggestion != nil {
			t.Fatalf("after the mapping: %+v", pc)
		}
	}
	// The poll keeps the checks for the client list and the health checks.
	e.advance(10 * time.Minute)
	_ = e.s.Poll(ctx)
	if sn := e.s.Snapshots()[c.ID]; len(sn.PathChecks) != 2 || !sn.PathChecks[0].Visible {
		t.Fatalf("snapshot checks = %+v", sn.PathChecks)
	}
}
