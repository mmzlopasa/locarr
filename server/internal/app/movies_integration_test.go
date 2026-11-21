package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
)

// The acceptance flow: add → automatic search → decision → grab → download
// → completion → hardlink import → renamed file → timeline with every step.
func TestMovieEndToEnd(t *testing.T) {
	le := newLibraryEnv(t)
	var look []struct {
		TmdbId  int64 `json:"tmdbId"`
		Title   string
		Poster  string
		MovieId *int64 `json:"movieId"`
	}
	le.expect(le.do("GET", "/api/v1/movies/lookup?term=big%20buck", nil), 200).json(&look)
	if len(look) != 1 || look[0].TmdbId != 10378 || look[0].MovieId != nil {
		t.Fatalf("lookup: %+v", look)
	}
	le.expect(le.do("GET", "/api/v1/movies/lookup?term=tt1254207", nil), 200).json(&look)
	if len(look) != 1 || look[0].Title != "Big Buck Bunny" {
		t.Fatalf("imdb lookup: %+v", look)
	}
	// Posters come through the server with an ETag.
	img := le.expect(le.do("GET", "/api/v1/images/tmdb/w342/"+look[0].Poster, nil), 200)
	etag := img.Header.Get("Etag")
	if etag == "" || !strings.HasPrefix(img.Header.Get("Content-Type"), "image/png") {
		t.Fatalf("image headers: %v", img.Header)
	}
	le.expect(le.req("GET", "/api/v1/images/tmdb/w342/"+look[0].Poster, nil, map[string]string{"If-None-Match": etag}), 304)
	le.expect(le.do("GET", "/api/v1/images/tmdb/w342/..%2Fsecret.png", nil), 400)

	m := le.addMovie(10378, true)
	if m.Path != filepath.Join(le.movies, "Big Buck Bunny (2008)") || !m.Available || m.LibraryStatus != "missing" {
		t.Fatalf("added: %+v", m)
	}
	le.expect(le.do("POST", "/api/v1/movies", map[string]any{"tmdbId": 10378, "rootFolderId": le.rootID, "qualityProfileId": le.profileID}), 409)

	waitFor(t, "the automatic search to grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.poll()
	if got := le.movie(m.ID); got.LibraryStatus != "downloading" {
		t.Fatalf("status while downloading: %s", got.LibraryStatus)
	}
	var ex struct{ Code string }
	le.expect(le.do("GET", fmt.Sprintf("/api/v1/timeline/explain?item=movie:%d", m.ID), nil), 200).json(&ex)
	if ex.Code != "downloading" {
		t.Fatalf("explain while downloading: %s", ex.Code)
	}
	var queue struct {
		Items []struct {
			Status string
			Item   *struct{ Ref, Title string }
		}
	}
	le.expect(le.do("GET", "/api/v1/queue", nil), 200).json(&queue)
	if len(queue.Items) != 1 || queue.Items[0].Item == nil || queue.Items[0].Item.Title != "Big Buck Bunny" {
		t.Fatalf("queue: %+v", queue.Items)
	}
	le.client.finish(t)
	le.poll()
	waitFor(t, "the import", func() bool { return le.movie(m.ID).File != nil })

	got := le.movie(m.ID)
	want := filepath.Join(m.Path, "Big Buck Bunny (2008) [Bluray-1080p][x264]-LOCARR.mkv")
	if got.File.Path != want || got.File.Quality.ID != "bluray-1080p" || got.LibraryStatus != "downloaded" {
		t.Fatalf("file: %+v (want %s)", got.File, want)
	}
	src := filepath.Join(le.downloads, "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR", "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR.mkv")
	a, _ := os.Stat(src)
	b, _ := os.Stat(want)
	if a == nil || b == nil || !os.SameFile(a, b) {
		t.Fatal("the import is not a hardlink of the download (or the source is gone)")
	}
	if _, err := os.Stat(strings.TrimSuffix(want, ".mkv") + ".en.srt"); err != nil {
		t.Fatal("subtitle sidecar not carried along")
	}
	entries, _ := os.ReadDir(m.Path)
	if len(entries) != 2 {
		t.Fatalf("library folder holds %d entries (sample or nfo imported?)", len(entries))
	}
	var status string
	_ = le.deps.DB.R.QueryRow(`SELECT status FROM downloads WHERE json_extract(item_ref, '$.ref') = ?`, fmt.Sprintf("movie:%d", m.ID)).Scan(&status)
	if status != "imported" {
		t.Fatalf("download status %s", status)
	}

	tl := le.timeline(m.ID)
	for _, typ := range []string{"added", "search", "decision", "download_grabbed", "download_progress", "download_completed", "file_imported",
		"renamed", "download_imported"} {
		if !strings.Contains(" "+tl.types()+" ", " "+typ+" ") {
			t.Errorf("timeline misses %s: %s", typ, tl.types())
		}
	}
	if tl.Explanation.Code != "available" {
		t.Fatalf("explanation: %+v", tl.Explanation)
	}
	for _, ev := range tl.Events {
		if ev.Type == "search" && !strings.Contains(string(ev.Data), `"name":"Fake Torznab"`) {
			t.Fatalf("search event without per-indexer counts: %s", ev.Data)
		}
		if ev.Type == "file_imported" && !strings.Contains(string(ev.Data), `"mode":"hardlink"`) {
			t.Fatalf("import event: %s", ev.Data)
		}
	}

	// Rename preview: nothing to do; after a template change one file.
	var plan []struct{ From, To string }
	le.expect(le.do("POST", "/api/v1/movies/rename/preview", map[string]any{"movieIds": []int64{m.ID}}), 200).json(&plan)
	if len(plan) != 0 {
		t.Fatalf("rename plan: %+v", plan)
	}
	var mm map[string]any
	le.expect(le.do("GET", "/api/v1/settings/mediamanagement", nil), 200).json(&mm)
	mm["movieFileFormat"] = "{Title} ({Year})"
	le.expect(le.do("PUT", "/api/v1/settings/mediamanagement", mm), 200)
	le.expect(le.do("POST", "/api/v1/movies/rename", map[string]any{"movieIds": []int64{m.ID}}), 200).json(&plan)
	if len(plan) != 1 || filepath.Base(plan[0].To) != "Big Buck Bunny (2008).mkv" {
		t.Fatalf("rename: %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(m.Path, "Big Buck Bunny (2008).en.srt")); err != nil {
		t.Fatal("sidecar not renamed with the file")
	}

	// Delete with files: the folder goes to the recycle bin.
	le.expect(le.do("DELETE", fmt.Sprintf("/api/v1/movies/%d?deleteFiles=true", m.ID), nil), 204)
	if _, err := os.Stat(m.Path); !os.IsNotExist(err) {
		t.Fatal("movie folder still there")
	}
	var bin struct {
		Entries []struct {
			ID           int64
			OriginalPath string `json:"originalPath"`
		}
	}
	le.expect(le.do("GET", "/api/v1/recyclebin", nil), 200).json(&bin)
	if len(bin.Entries) != 1 || bin.Entries[0].OriginalPath != m.Path {
		t.Fatalf("recycle bin: %+v", bin)
	}
	le.expect(le.do("POST", fmt.Sprintf("/api/v1/recyclebin/%d/restore", bin.Entries[0].ID), nil), 200)
	if _, err := os.Stat(filepath.Join(m.Path, "Big Buck Bunny (2008).mkv")); err != nil {
		t.Fatal("restore did not bring the file back")
	}
}

// A failed download is blocklisted and the next best release is grabbed.
func TestFailedDownloadGrabsNextBest(t *testing.T) {
	extra := fake.Release{Title: "Big.Buck.Bunny.2008.720p.WEBRip.x264-OTHER", Cat: 2040, Size: 400_000_000, Seeders: 50, Peers: 60,
		IMDB: "tt1254207", TMDB: 10378, Hash: "aab2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 5 * time.Hour}
	saved := fake.Catalog
	fake.Catalog = append(append([]fake.Release{}, saved...), extra)
	t.Cleanup(func() { fake.Catalog = saved })

	le := newLibraryEnv(t)
	le.client.fail["Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR"] = true
	m := le.addMovie(10378, true)
	waitFor(t, "the first grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.poll()
	le.client.finish(t) // the first one fails
	le.poll()
	waitFor(t, "the retry grab", func() bool { return le.downloadCount(m.ID) == 2 })
	le.client.mu.Lock()
	added := append([]string{}, le.client.added...)
	le.client.mu.Unlock()
	if len(added) != 2 || added[1] != "Big.Buck.Bunny.2008.720p.WEBRip.x264-OTHER" {
		t.Fatalf("grabs: %v", added)
	}
	var bl []struct{ Title, Reason string }
	le.expect(le.do("GET", "/api/v1/queue/blocklist", nil), 200).json(&bl)
	if len(bl) != 1 || bl[0].Title != "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR" || bl[0].Reason != "download_failed" {
		t.Fatalf("blocklist: %+v", bl)
	}
	tl := le.timeline(m.ID)
	if !strings.Contains(tl.types(), "download_failed") || !strings.Contains(tl.types(), "retry") {
		t.Fatalf("timeline: %s", tl.types())
	}
	// The retry's decision names the blocklisted release.
	found := false
	for _, ev := range tl.Events {
		if ev.Type == "decision" && strings.Contains(string(ev.Data), "blocklisted") {
			found = true
		}
	}
	if !found {
		t.Fatal("no decision lists the blocklisted release")
	}
}

// An RSS release that matches a monitored movie is grabbed.
func TestRSSGrab(t *testing.T) {
	le := newLibraryEnv(t)
	m := le.addMovie(133701, false) // Tears of Steel, 720p WEBRip in the feed
	if le.downloadCount(m.ID) != 0 {
		t.Fatal("grabbed without a search")
	}
	ctx := context.Background()
	ch, unsub := le.deps.Indexers.SubscribeRSS(4)
	defer unsub()
	if _, err := le.deps.Indexers.SyncRSS(ctx); err != nil {
		t.Fatal(err)
	}
	var batch []indexers.Release
	select {
	case batch = <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no RSS batch")
	}
	n, err := le.deps.Wanted.ProcessRSS(ctx, batch)
	if err != nil || n != 1 {
		t.Fatalf("rss grabs = %d, %v", n, err)
	}
	if le.downloadCount(m.ID) != 1 {
		t.Fatal("no download for the movie")
	}
	tl := le.timeline(m.ID)
	if !strings.Contains(tl.types(), "decision") || !strings.Contains(tl.Events[len(tl.Events)-1].Type, "added") {
		t.Fatalf("timeline: %s", tl.types())
	}
	// Unrelated movies are not touched by RSS.
	other := le.addMovie(900002, false)
	if _, err := le.deps.Wanted.ProcessRSS(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if le.downloadCount(other.ID) != 0 {
		t.Fatal("unrelated movie grabbed")
	}
}

// Manual import, then an upgrade that sends the old file to the recycle bin.
func TestManualImportAndUpgrade(t *testing.T) {
	le := newLibraryEnv(t)
	m := le.addMovie(133701, false)
	incoming := filepath.Join(le.downloads, "manual")
	writeMovieFolder(t, filepath.Join(incoming, "a"), "Tears.of.Steel.2012.720p.WEBRip.x264-LOCARR")
	writeMovieFolder(t, filepath.Join(incoming, "b"), "Tears.of.Steel.2012.1080p.BluRay.x264-BETTER")

	var prev []struct {
		Path      string
		Skipped   string
		MovieId   int64 `json:"movieId"`
		Quality   *struct{ ID string }
		Rejection *struct{ Code string }
	}
	le.expect(le.do("GET", "/api/v1/movies/import/manual?path="+filepath.Join(incoming, "a"), nil), 200).json(&prev)
	var main string
	for _, p := range prev {
		if p.Skipped == "" {
			main = p.Path
			if p.MovieId != m.ID || p.Quality == nil || p.Quality.ID != "webrip-720p" || p.Rejection != nil {
				t.Fatalf("preview: %+v", p)
			}
		} else if p.Skipped != "sample" {
			t.Fatalf("skipped: %+v", p)
		}
	}
	var res []struct {
		ImportedPath string `json:"importedPath"`
		Mode         string
		Error        *struct{ Code, Message string }
	}
	le.expect(le.do("POST", "/api/v1/movies/import/manual", map[string]any{"items": []map[string]any{{"path": main, "movieId": m.ID, "mode": "copy"}}}), 200).json(&res)
	if len(res) != 1 || res[0].Error != nil || res[0].Mode != "copy" {
		t.Fatalf("import: %+v", res)
	}
	first := res[0].ImportedPath
	if _, err := os.Stat(main); err != nil {
		t.Fatal("copy removed the source")
	}
	// The same quality again is refused with a reason.
	res = nil
	le.expect(le.do("POST", "/api/v1/movies/import/manual", map[string]any{"items": []map[string]any{{"path": main, "movieId": m.ID}}}), 200).json(&res)
	if res[0].Error == nil || (res[0].Error.Code != "not_an_upgrade" && res[0].Error.Code != "already_imported") {
		t.Fatalf("re-import: %+v", res)
	}
	res = nil
	better := filepath.Join(incoming, "b", "Tears.of.Steel.2012.1080p.BluRay.x264-BETTER.mkv")
	le.expect(le.do("POST", "/api/v1/movies/import/manual", map[string]any{"items": []map[string]any{{"path": better, "movieId": m.ID}}}), 200).json(&res)
	if res[0].Error != nil {
		t.Fatalf("upgrade: %+v", res[0])
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("old file still in the library")
	}
	var bin struct {
		Entries []struct{ OriginalPath string }
	}
	le.expect(le.do("GET", "/api/v1/recyclebin", nil), 200).json(&bin)
	found := false
	for _, e := range bin.Entries {
		found = found || e.OriginalPath == first
	}
	if !found {
		t.Fatalf("old file not in the recycle bin: %+v", bin)
	}
	got := le.movie(m.ID)
	if got.File == nil || got.File.Quality.ID != "bluray-1080p" {
		t.Fatalf("file after upgrade: %+v", got.File)
	}
	if tl := le.timeline(m.ID); !strings.Contains(tl.types(), "upgraded") || !strings.Contains(tl.types(), "recycled") {
		t.Fatalf("timeline: %s", tl.types())
	}
}

// Existing folders are found, matched and added with their files in place.
func TestImportExistingLibrary(t *testing.T) {
	le := newLibraryEnv(t)
	writeMovieFolder(t, filepath.Join(le.movies, "Sintel (2010)"), "Sintel.2010.1080p.WEB-DL.x264-OLD")
	writeMovieFolder(t, filepath.Join(le.movies, "Whatever {tmdb-133701}"), "tos")
	var scan []struct {
		Path  string
		Name  string
		Match *struct {
			TmdbId int64 `json:"tmdbId"`
		}
	}
	le.expect(le.do("GET", fmt.Sprintf("/api/v1/movies/import/scan?rootFolderId=%d", le.rootID), nil), 200).json(&scan)
	if len(scan) != 2 {
		t.Fatalf("scan: %+v", scan)
	}
	var items []map[string]any
	for _, f := range scan {
		if f.Match == nil {
			t.Fatalf("no match for %s", f.Name)
		}
		items = append(items, map[string]any{"path": f.Path, "tmdbId": f.Match.TmdbId})
	}
	var res []struct {
		Movie *apiMovie
		Error *struct{ Code, Message string }
	}
	le.expect(le.do("POST", "/api/v1/movies/import/existing", map[string]any{"rootFolderId": le.rootID, "qualityProfileId": le.profileID,
		"items": items}), 200).json(&res)
	for _, r := range res {
		if r.Error != nil || r.Movie == nil || r.Movie.File == nil {
			t.Fatalf("existing import: %+v %+v", r.Movie, r.Error)
		}
	}
	le.expect(le.do("GET", fmt.Sprintf("/api/v1/movies/import/scan?rootFolderId=%d", le.rootID), nil), 200).json(&scan)
	if len(scan) != 0 {
		t.Fatalf("still unmapped: %+v", scan)
	}
}

func TestCollectionsListsCalendarAndWanted(t *testing.T) {
	le := newLibraryEnv(t)
	le.addMovie(10378, false)
	var col struct {
		Name    string
		Members []struct {
			TmdbId  int64  `json:"tmdbId"`
			MovieId *int64 `json:"movieId"`
		}
	}
	le.expect(le.do("GET", "/api/v1/movies/collections/9001", nil), 200).json(&col)
	if len(col.Members) != 4 || col.Members[0].TmdbId != 10378 || col.Members[0].MovieId == nil {
		t.Fatalf("collection: %+v", col)
	}
	var added []apiMovie
	le.expect(le.do("POST", "/api/v1/movies/collections/9001", map[string]any{"rootFolderId": le.rootID, "qualityProfileId": le.profileID,
		"minimumAvailability": "in_cinemas"}), 200).json(&added)
	if len(added) != 3 {
		t.Fatalf("added: %+v", added)
	}
	var list struct {
		Total int
		Items []apiMovie
	}
	le.expect(le.do("GET", "/api/v1/movies?status=unavailable", nil), 200).json(&list)
	if list.Total != 1 || list.Items[0].Title != "The Open Horizon" {
		t.Fatalf("unavailable: %+v", list)
	}
	le.expect(le.do("GET", "/api/v1/movies?status=missing&sort=year&desc=true", nil), 200).json(&list)
	if list.Total != 3 || list.Items[0].Title != "Tears of Steel" {
		t.Fatalf("missing: %+v", list)
	}
	var ex struct{ Code string }
	le.expect(le.do("GET", fmt.Sprintf("/api/v1/timeline/explain?item=movie:%d", list.Items[0].ID), nil), 200).json(&ex)
	if ex.Code != "not_searched_yet" {
		t.Fatalf("explain: %s", ex.Code)
	}

	// Import list: the fake list holds Sintel and Tears of Steel (both in the library now) — exclude one first.
	var il struct{ ID int64 }
	le.expect(le.do("POST", "/api/v1/movies/importlists", map[string]any{"name": "Picks", "kind": "tmdb_list", "listId": "8100",
		"rootFolderId": le.rootID, "qualityProfileId": le.profileID}), 201).json(&il)
	var sr struct {
		Found int
		Added []string
	}
	le.expect(le.do("POST", fmt.Sprintf("/api/v1/movies/importlists/%d/sync", il.ID), nil), 200).json(&sr)
	if sr.Found != 2 || len(sr.Added) != 0 {
		t.Fatalf("sync: %+v", sr)
	}

	var cal []struct {
		Kind  string
		Title string
	}
	le.expect(le.do("GET", "/api/v1/calendar?start=2008-01-01&end=2008-12-31", nil), 200).json(&cal)
	if len(cal) != 2 || cal[0].Title != "Big Buck Bunny" {
		t.Fatalf("calendar: %+v", cal)
	}
	var feed struct{ Token, Path string }
	le.expect(le.do("POST", "/api/v1/calendar/token", nil), 201).json(&feed)
	r := le.req("GET", "/api/v1/calendar/feed.ics?token="+feed.Token+"&pastDays=365&futureDays=365", nil, map[string]string{"Authorization": ""})
	if r.StatusCode != 200 || !strings.Contains(string(r.body), "BEGIN:VCALENDAR") {
		t.Fatalf("feed: %d %s", r.StatusCode, r.body)
	}
	le.expect(le.req("GET", "/api/v1/calendar/feed.ics?token=lc_wrongwrongwrongwrongwrong", nil, map[string]string{"Authorization": ""}), 401)
}

func TestMetadataKeyIsValidatedAndNamingPreview(t *testing.T) {
	le := newLibraryEnv(t)
	r := le.expect(le.do("PUT", "/api/v1/settings/metadata", map[string]any{"tmdbApiKey": "wrong-key"}), 502)
	if r.code() != "metadata_key_invalid" {
		t.Fatal(r.code())
	}
	var ms struct {
		TmdbApiKey struct{ Configured bool }
		Language   string
	}
	le.expect(le.do("GET", "/api/v1/settings/metadata", nil), 200).json(&ms)
	if !ms.TmdbApiKey.Configured || ms.Language != "en-US" {
		t.Fatalf("%+v", ms)
	}
	var pv struct {
		Folder, File string
		FileError    string `json:"fileError"`
	}
	le.expect(le.do("POST", "/api/v1/settings/mediamanagement/preview", map[string]any{"movieFolderFormat": "{Title} ({Year}) {tmdb-{TmdbId}}",
		"movieFileFormat": "{Title}: {Nope}"}), 200).json(&pv)
	if pv.FileError == "" {
		t.Fatalf("preview: %+v", pv)
	}
	le.expect(le.do("POST", "/api/v1/settings/mediamanagement/preview", map[string]any{"movieFolderFormat": "{TitleThe} ({Year})",
		"movieFileFormat": "{Title} ({Year}) {[QualityFull]}{[HDR]}{-ReleaseGroup}"}), 200).json(&pv)
	if pv.Folder != "Matrix, The (1999)" || pv.File != "The Matrix (1999) [Bluray-1080p]-GROUP.mkv" {
		t.Fatalf("preview: %+v", pv)
	}
}
