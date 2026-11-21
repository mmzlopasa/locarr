package app

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library/tags"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

// musicEnv is an environment with a fake MusicBrainz and Cover Art
// Archive, a fake indexer, a download client that "downloads" albums and a
// music root folder.
type musicEnv struct {
	*env
	mb        *mbfake.Server
	client    *flowClient
	rootID    int64
	lossless  int64
	lossy     int64
	standard  int64
	music     string
	downloads string
	// files decides what a finished download holds (by release name).
	files func(name string) []mbfake.File
}

func newMusicEnv(t *testing.T) *musicEnv {
	t.Helper()
	mb := mbfake.New()
	mbSrv := httptest.NewServer(mb)
	t.Cleanup(mbSrv.Close)
	e := newEnvWith(t, func(c *config.Config) {
		c.MusicBrainzURL, c.CoverArtURL = mbSrv.URL+"/mb", mbSrv.URL+"/mb/coverart"
	}, noLimits)
	// The one-request-per-second rule has its own test (metadata/musicbrainz).
	e.deps.MusicBrainz.Limiter = metadata.NewLimiter(1000, 1000)
	e.setup()
	_, base := startFakes(t)
	me := &musicEnv{env: e, mb: mb, music: filepath.Join(e.data, "media", "music"), downloads: filepath.Join(e.data, "downloads")}
	if err := os.MkdirAll(me.music, 0o755); err != nil {
		t.Fatal(err)
	}
	me.files = func(name string) []mbfake.File {
		format := "flac"
		if strings.Contains(name, "MP3") {
			format = "mp3"
		}
		return mbfake.ReleaseFiles(mbfake.ReleaseStandard, mbfake.FileOptions{Format: format})
	}
	me.client = newFlowClient(me.downloads)
	me.client.write = func(t *testing.T, folder, name string) {
		writeFiles(t, folder, me.files(name))
	}
	e.deps.Downloads.NewClient = func(downloaders.Implementation, downloaders.Config) (downloaders.Client, error) { return me.client, nil }
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	e.expect(e.do("POST", "/api/v1/indexers", map[string]any{"name": "Fake Torznab", "kind": "torznab", "baseUrl": base + "/torznab",
		"apiKey": "fakekey"}), 201)
	e.expect(e.do("POST", "/api/v1/downloadclients", map[string]any{"name": "Flow", "implementation": "apifake", "host": "flow.lan",
		"password": "x", "categories": map[string]string{"movies": "", "series": "", "music": "music"}}), 201)
	var rf struct{ ID int64 }
	e.expect(e.do("POST", "/api/v1/rootfolders", map[string]any{"path": me.music, "kind": "music"}), 201).json(&rf)
	me.rootID = rf.ID
	var profiles []struct {
		ID         int64
		Name, Kind string
	}
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	for _, p := range profiles {
		if p.Kind == "music" && p.Name == "Lossless" {
			me.lossless = p.ID
		}
		if p.Kind == "music" && p.Name == "Lossy" {
			me.lossy = p.ID
		}
	}
	var mps []struct {
		ID   int64
		Name string
	}
	e.expect(e.do("GET", "/api/v1/music/metadataprofiles", nil), 200).json(&mps)
	for _, p := range mps {
		if p.Name == "Standard" {
			me.standard = p.ID
		}
	}
	if me.lossless == 0 || me.standard == 0 {
		t.Fatal("no music profiles")
	}
	return me
}

func writeFiles(t *testing.T, folder string, files []mbfake.File) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(folder, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type apiArtist struct {
	ID              int64
	Name            string
	Path            string
	Albums          int
	MonitoredAlbums int `json:"monitoredAlbums"`
	Files           int
	Cover           string
}

type apiAlbum struct {
	ID            int64
	Title         string
	PrimaryType   string `json:"primaryType"`
	Monitored     bool
	LibraryStatus string `json:"libraryStatus"`
	TrackCount    int    `json:"trackCount"`
	FileCount     int    `json:"fileCount"`
	Path          string
	Release       *struct {
		ID         int64
		Mbid       string
		TrackCount int `json:"trackCount"`
	}
	Quality *struct{ ID string }
}

type apiAlbumDetail struct {
	Album    apiAlbum
	Releases []struct {
		ID      int64
		Mbid    string
		Country string
	}
	Tracks []struct {
		Disc, Position int
		Title          string
		File           *struct {
			Path    string
			Quality struct{ ID string }
		}
	}
}

func (me *musicEnv) addArtist(mbid string, search bool, monitor string) apiArtist {
	me.t.Helper()
	var a apiArtist
	me.expect(me.do("POST", "/api/v1/music/artists", map[string]any{"mbid": mbid, "rootFolderId": me.rootID, "qualityProfileId": me.lossless,
		"metadataProfileId": me.standard, "monitor": monitor, "searchOnAdd": search}), 201).json(&a)
	return a
}

func (me *musicEnv) albums(artistID int64) map[string]apiAlbum {
	me.t.Helper()
	var list []apiAlbum
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/music/artists/%d/albums", artistID), nil), 200).json(&list)
	out := map[string]apiAlbum{}
	for _, a := range list {
		out[a.Title] = a
	}
	return out
}

func (me *musicEnv) album(id int64) apiAlbumDetail {
	me.t.Helper()
	var d apiAlbumDetail
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/music/albums/%d", id), nil), 200).json(&d)
	return d
}

func (me *musicEnv) timeline(id int64) apiTimeline {
	me.t.Helper()
	var tl apiTimeline
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/timeline?item=album:%d", id), nil), 200).json(&tl)
	return tl
}

func (me *musicEnv) downloadsFor(id int64) int {
	var n int
	_ = me.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM downloads WHERE json_extract(item_ref, '$.ref') = ?`, fmt.Sprintf("album:%d", id)).Scan(&n)
	return n
}

func (me *musicEnv) poll() {
	me.t.Helper()
	if err := me.deps.Downloads.Poll(context.Background()); err != nil {
		me.t.Fatal(err)
	}
}

func (me *musicEnv) jobsIdle() bool {
	var n int
	_ = me.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM jobs WHERE state IN ('queued', 'running')`).Scan(&n)
	return n == 0
}

// TestMusicFlow: an artist is added, its albums follow the metadata
// profile, the released ones are searched, the lossless release is grabbed,
// the download completes and is imported by matching its tags to the
// release, renamed with a cover, and every step is on the album's timeline.
func TestMusicFlow(t *testing.T) {
	me := newMusicEnv(t)

	var look []struct {
		Mbid           string
		Name           string
		Disambiguation string
		ArtistId       *int64 `json:"artistId"`
	}
	me.expect(me.do("GET", "/api/v1/music/artists/lookup?term=Open%20Source%20Artist", nil), 200).json(&look)
	if len(look) != 2 || look[0].Disambiguation == look[1].Disambiguation || look[0].ArtistId != nil {
		t.Fatalf("lookup: %+v", look)
	}

	a := me.addArtist(mbfake.ArtistOpen, true, "all")
	if a.Path != filepath.Join(me.music, "Open Source Artist") {
		t.Fatalf("artist path %s", a.Path)
	}
	albums := me.albums(a.ID)
	// Standard profile: albums and EPs without secondary types; live, remix and singles stay out.
	if len(albums) != 3 || albums["Free Album"].ID == 0 || albums["Free EP"].ID == 0 || albums["Next Album"].ID == 0 {
		t.Fatalf("albums: %+v", albums)
	}
	if albums["Next Album"].LibraryStatus != "unreleased" {
		t.Fatalf("future album: %+v", albums["Next Album"])
	}
	free := albums["Free Album"]
	// Searching after the add grabs the FLAC release (the MP3 one is not in the Lossless profile).
	waitFor(t, "album grab", func() bool { return me.downloadsFor(free.ID) == 1 })
	if got := me.client.added; len(got) != 1 || !strings.Contains(got[0], "[FLAC]") {
		t.Fatalf("grabbed %v", got)
	}
	waitFor(t, "background refresh", me.jobsIdle)
	d := me.album(free.ID)
	if len(d.Releases) != 2 || d.Album.Release == nil || d.Album.Release.Mbid != mbfake.ReleaseStandard || len(d.Tracks) != 5 {
		t.Fatalf("editions: %+v", d)
	}

	me.client.finish(t)
	me.poll()
	waitFor(t, "import", func() bool { return me.album(free.ID).Album.LibraryStatus == "downloaded" })
	d = me.album(free.ID)
	folder := filepath.Join(a.Path, "Free Album (2020)")
	if d.Album.Path != folder || d.Album.Quality == nil || d.Album.Quality.ID != "flac" {
		t.Fatalf("album after import: %+v", d.Album)
	}
	for _, tr := range d.Tracks {
		want := filepath.Join(folder, fmt.Sprintf("%02d - %s.flac", tr.Position, tr.Title))
		if tr.File == nil || tr.File.Path != want {
			t.Fatalf("track %d: %+v, want %s", tr.Position, tr.File, want)
		}
		tg, err := tags.Read(want)
		if err != nil || tg.Title != tr.Title {
			t.Fatalf("file %s: %v %+v", want, err, tg)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "cover.jpg")); err != nil {
		t.Fatal("no cover:", err)
	}
	// The download stays for seeding (hardlinks).
	if _, err := os.Stat(filepath.Join(me.downloads, me.client.added[0], "01. Open Source Artist - Opening.flac")); err != nil {
		t.Fatal("download changed:", err)
	}
	tl := me.timeline(free.ID)
	for _, typ := range []string{"search", "decision", "download_grabbed", "download_completed", "release_matched", "file_imported", "renamed",
		"cover_saved", "download_imported"} {
		if !strings.Contains(" "+tl.types()+" ", " "+typ+" ") {
			t.Fatalf("timeline lacks %s: %s", typ, tl.types())
		}
	}
	if tl.Explanation.Code != "available" {
		t.Fatalf("explanation %+v", tl.Explanation)
	}
	// The EP had no release anywhere: its explainer says so.
	waitFor(t, "EP search", func() bool { return me.timeline(albums["Free EP"].ID).Explanation.Code == "nothing_found" })
	// The future album is not searched.
	if c := me.timeline(albums["Next Album"].ID).Explanation.Code; c != "not_available_yet" {
		t.Fatalf("future album explanation %s", c)
	}

	// The artist list counts it all and has a cover.
	var list struct {
		Total int
		Items []apiArtist
	}
	me.expect(me.do("GET", "/api/v1/music/artists?q=open", nil), 200).json(&list)
	if list.Total != 1 || list.Items[0].Files != 5 || list.Items[0].Albums != 3 || !strings.HasPrefix(list.Items[0].Cover, "rg-") {
		t.Fatalf("artists: %+v", list)
	}
	r := me.do("GET", "/api/v1/images/coverart/250/"+list.Items[0].Cover, nil)
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "image/") {
		t.Fatalf("cover image: %d %s", r.StatusCode, r.Header.Get("Content-Type"))
	}

	// Wanted lists the EP (missing) but not the downloaded album or the future one.
	var wanted struct {
		Total int
		Items []apiAlbum
	}
	me.expect(me.do("GET", "/api/v1/music/albums?status=missing", nil), 200).json(&wanted)
	if wanted.Total != 1 || wanted.Items[0].Title != "Free EP" {
		t.Fatalf("wanted: %+v", wanted)
	}
	// Calendar has the album releases.
	var cal []struct{ Title string }
	me.expect(me.do("GET", "/api/v1/music/calendar?start=2020-03-01&end=2020-03-31", nil), 200).json(&cal)
	if len(cal) != 1 || cal[0].Title != "Free Album" {
		t.Fatalf("calendar: %+v", cal)
	}
	// The queue links the download to the album.
	var queue struct {
		Items []struct {
			Item *struct{ Ref, Title string }
		}
	}
	me.expect(me.do("GET", "/api/v1/queue", nil), 200).json(&queue)
	for _, it := range queue.Items {
		if it.Item != nil && it.Item.Ref != fmt.Sprintf("album:%d", free.ID) {
			t.Fatalf("queue link %+v", it.Item)
		}
	}

	// A metadata profile that takes live albums brings "Live at the Commons" in;
	// a refresh never removes albums with files.
	var mp struct{ ID int64 }
	me.expect(me.do("POST", "/api/v1/music/metadataprofiles", map[string]any{"name": "Live too", "primaryTypes": []string{"album"},
		"secondaryTypes": []string{"studio", "live"}, "releaseStatuses": []string{"official"}}), 201).json(&mp)
	me.expect(me.do("PATCH", fmt.Sprintf("/api/v1/music/artists/%d", a.ID), map[string]any{"metadataProfileId": mp.ID}), 200)
	waitFor(t, "profile refresh", func() bool { _, ok := me.albums(a.ID)["Live at the Commons"]; return ok })
	after := me.albums(a.ID)
	if _, ok := after["Free EP"]; ok {
		t.Fatal("the EP should leave with an albums-only profile")
	}
	if after["Free Album"].FileCount != 5 {
		t.Fatalf("files lost: %+v", after["Free Album"])
	}
	if r := me.do("DELETE", fmt.Sprintf("/api/v1/music/metadataprofiles/%d", mp.ID), nil); r.StatusCode != 409 {
		t.Fatalf("delete used profile: %d", r.StatusCode)
	}
}

// TestMusicImportMatching covers the importer: a deluxe (two disc) download
// switches the album's edition, a short download is refused unless partial
// albums are allowed, a cue image is refused, and an upgrade replaces the
// files into the recycle bin.
func TestMusicImportMatching(t *testing.T) {
	me := newMusicEnv(t)
	me.expect(me.do("PUT", "/api/v1/settings/music", musicSettings(nil)), 200)
	a := me.addArtist(mbfake.ArtistOpen, false, "all")
	waitFor(t, "refresh", me.jobsIdle)
	free := me.albums(a.ID)["Free Album"]
	in := filepath.Join(me.downloads, "manual")

	// Preview a deluxe download: the deluxe edition scores best.
	deluxe := filepath.Join(in, "deluxe")
	writeFiles(t, deluxe, mbfake.ReleaseFiles(mbfake.ReleaseDeluxe, mbfake.FileOptions{Format: "flac24"}))
	var pv struct {
		Files   int
		Matches []struct {
			Release struct{ Mbid string }
			Score   int
			Missing []string
			Extra   []string
		}
		Rejection *struct{ Code string }
	}
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/music/import/manual?albumId=%d&path=%s", free.ID, deluxe), nil), 200).json(&pv)
	if pv.Files != 8 || len(pv.Matches) != 2 || pv.Matches[0].Release.Mbid != mbfake.ReleaseDeluxe || pv.Matches[0].Score < 90 ||
		len(pv.Matches[1].Extra) != 3 || pv.Rejection != nil {
		t.Fatalf("preview: %+v", pv)
	}

	// Short download (two tracks missing): refused, then imported with allowPartial.
	short := filepath.Join(in, "short")
	writeFiles(t, short, mbfake.ReleaseFiles(mbfake.ReleaseStandard, mbfake.FileOptions{Format: "mp3", IDs: true, Skip: 2}))
	if r := me.expect(me.do("POST", "/api/v1/music/import/manual", map[string]any{"albumId": free.ID, "path": short, "releaseId": me.album(free.ID).Album.Release.ID}), 422); r.code() != "missing_tracks" {
		t.Fatal(r.code())
	}
	var res struct {
		Imported []string
		Missing  int
	}
	me.expect(me.do("POST", "/api/v1/music/import/manual", map[string]any{"albumId": free.ID, "path": short, "allowPartial": true,
		"releaseId": me.album(free.ID).Album.Release.ID}), 200).json(&res)
	if len(res.Imported) != 3 || res.Missing != 2 {
		t.Fatalf("partial: %+v", res)
	}
	if st := me.album(free.ID).Album; st.LibraryStatus != "partial" || st.FileCount != 3 {
		t.Fatalf("partial album: %+v", st)
	}
	// The album stays wanted while tracks are missing.
	if c := me.timeline(free.ID).Explanation.Code; c != "not_searched_yet" && c != "upgrade_wanted" {
		t.Fatalf("partial explanation %s", c)
	}

	// The deluxe FLAC 24-bit download: the edition switches, MP3s are upgraded into the recycle bin.
	me.expect(me.do("PATCH", fmt.Sprintf("/api/v1/music/albums/%d", free.ID), map[string]any{"releaseId": 0}), 200)
	me.expect(me.do("POST", "/api/v1/music/import/manual", map[string]any{"albumId": free.ID, "path": deluxe}), 200).json(&res)
	d := me.album(free.ID)
	if len(res.Imported) != 8 || d.Album.Release.Mbid != mbfake.ReleaseDeluxe || d.Album.LibraryStatus != "downloaded" || d.Album.Quality.ID != "flac-24" {
		t.Fatalf("deluxe import: %+v %+v", res, d.Album)
	}
	if p := d.Tracks[5].File.Path; p != filepath.Join(a.Path, "Free Album (2020)", "2-01 - Bonus One.flac") {
		t.Fatalf("multi-disc name %s", p)
	}
	var bin struct{ Entries []struct{ OriginalPath string } }
	me.expect(me.do("GET", "/api/v1/recyclebin", nil), 200).json(&bin)
	mp3s := 0
	for _, e := range bin.Entries {
		if strings.HasSuffix(e.OriginalPath, ".mp3") {
			mp3s++
		}
	}
	if mp3s != 3 {
		t.Fatalf("recycled %+v", bin.Entries)
	}
	// Importing the same quality again is not an upgrade.
	if r := me.expect(me.do("POST", "/api/v1/music/import/manual", map[string]any{"albumId": free.ID, "path": deluxe}), 422); r.code() != "not_an_upgrade" {
		t.Fatal(r.code())
	}

	// A cue sheet with one big file is reported as unsupported.
	cue := filepath.Join(in, "cue")
	files := mbfake.ReleaseFiles(mbfake.ReleaseStandard, mbfake.FileOptions{})
	writeFiles(t, cue, []mbfake.File{{Path: "Free Album.flac", Data: files[0].Data}, {Path: "Free Album.cue", Data: []byte("FILE \"Free Album.flac\" WAVE\n")}})
	ep := me.albums(a.ID)["Free EP"]
	_ = ep
	if r := me.expect(me.do("POST", "/api/v1/music/import/manual", map[string]any{"albumId": free.ID, "path": cue}), 422); r.code() != "cue_single_file" {
		t.Fatal(r.code(), string(r.body))
	}
	// Files of another album do not match well enough.
	if r := me.expect(me.do("POST", "/api/v1/music/import/manual", map[string]any{"albumId": ep.ID, "path": deluxe}), 422); r.code() != "match_too_weak" {
		t.Fatal(r.code(), string(r.body))
	}
}

func musicSettings(mod func(map[string]any)) map[string]any {
	s := map[string]any{"artistFolderFormat": "{ArtistName}", "albumFolderFormat": "{AlbumTitle} ({ReleaseYear})",
		"trackFormat": "{TrackNumber} - {TrackTitle}", "multiDiscTrackFormat": "{DiscNumber}-{TrackNumber} - {TrackTitle}", "discFolders": false,
		"discFolderFormat": "Disc {DiscNumber}", "renameTracks": true, "importPartialAlbums": false, "writeTags": false, "saveCoverArt": true,
		"minimumMatchScore": 70, "musicBrainzContact": "ops@example.org"}
	if mod != nil {
		mod(s)
	}
	return s
}

// TestMusicExistingLibrary: an existing artist folder is scanned, matched to
// MusicBrainz through its tags, accepted, and its files are linked in place.
func TestMusicExistingLibrary(t *testing.T) {
	me := newMusicEnv(t)
	ep := filepath.Join(me.music, "OSA", "2021 - Free EP")
	writeFiles(t, ep, mbfake.ReleaseFiles("9d2f6b4f-1a5e-4bad-9d54-0000000000a3", mbfake.FileOptions{IDs: true}))
	untagged := filepath.Join(me.music, "OSA", "Free Album")
	writeFiles(t, untagged, mbfake.ReleaseFiles(mbfake.ReleaseStandard, mbfake.FileOptions{}))
	var scan []struct {
		Path   string
		Albums []struct{ Name string }
		Match  *struct{ Mbid string }
	}
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/music/import/scan?rootFolderId=%d", me.rootID), nil), 200).json(&scan)
	if len(scan) != 1 || len(scan[0].Albums) != 2 || scan[0].Match == nil || scan[0].Match.Mbid != mbfake.ArtistOpen {
		t.Fatalf("scan: %+v", scan)
	}
	var res []struct {
		Artist *apiArtist
		Error  *struct{ Code string }
	}
	me.expect(me.do("POST", "/api/v1/music/import/existing", map[string]any{"rootFolderId": me.rootID, "qualityProfileId": me.lossless,
		"metadataProfileId": me.standard, "items": []map[string]any{{"path": scan[0].Path, "mbid": mbfake.ArtistOpen}}}), 200).json(&res)
	if len(res) != 1 || res[0].Artist == nil || res[0].Artist.Path != filepath.Join(me.music, "OSA") {
		t.Fatalf("import: %+v", res)
	}
	id := res[0].Artist.ID
	waitFor(t, "linking", func() bool {
		al := me.albums(id)
		return al["Free EP"].LibraryStatus == "downloaded" && al["Free Album"].LibraryStatus == "downloaded"
	})
	d := me.album(me.albums(id)["Free EP"].ID)
	if d.Tracks[0].File == nil || filepath.Dir(d.Tracks[0].File.Path) != ep {
		t.Fatalf("files moved: %+v", d.Tracks[0].File)
	}
	// Nothing was renamed or moved.
	if _, err := os.Stat(filepath.Join(ep, "01. Open Source Artist - EP One.flac")); err != nil {
		t.Fatal(err)
	}
	// The artist is in the library now: a new scan marks it.
	var again []struct{ ArtistId *int64 }
	me.expect(me.do("GET", fmt.Sprintf("/api/v1/music/import/scan?rootFolderId=%d", me.rootID), nil), 200).json(&again)
	if len(again) != 1 || again[0].ArtistId == nil {
		t.Fatalf("rescan: %+v", again)
	}
}

// TestMusicRSS: a monitored album is grabbed from an RSS release that names
// its artist and title.
func TestMusicRSS(t *testing.T) {
	me := newMusicEnv(t)
	a := me.addArtist(mbfake.ArtistOpen, false, "all")
	waitFor(t, "refresh", me.jobsIdle)
	free := me.albums(a.ID)["Free Album"]
	n, err := me.deps.Wanted.ProcessRSS(context.Background(), rssBatch(t, me))
	if err != nil || n != 1 {
		t.Fatalf("rss grabs %d %v", n, err)
	}
	if me.downloadsFor(free.ID) != 1 || !strings.Contains(me.client.added[0], "FLAC") {
		t.Fatalf("rss grab: %v", me.client.added)
	}
}

// rssBatch is what an RSS sync would hand over: the indexer's music releases.
func rssBatch(t *testing.T, me *musicEnv) []indexers.Release {
	t.Helper()
	res, err := me.deps.Indexers.Search(context.Background(), indexers.Query{Type: indexers.SearchAny, Term: "Open Source Artist"})
	if err != nil || len(res.Releases) < 2 {
		t.Fatalf("rss releases: %v %d", err, len(res.Releases))
	}
	return res.Releases
}
