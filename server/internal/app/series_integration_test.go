package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/fake"
	tmdbfake "github.com/mmzlopasa/locarr/server/internal/metadata/tmdb/fake"
)

// writeRelease writes what a finished release unpacks to: the files of a
// season pack (sample, subtitle and nfo included) or one episode file.
func writeRelease(t *testing.T, folder, name string) {
	t.Helper()
	for _, f := range fake.ReleaseFiles(name) {
		p := filepath.Join(folder, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		size := 3 << 20
		if strings.Contains(strings.ToLower(f), "sample") || !strings.HasSuffix(f, ".mkv") {
			size = 32 << 10
		}
		data := make([]byte, size)
		copy(data, f)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type seriesEnv struct {
	*libraryEnv
	tv        string
	tvRoot    int64
	tvProfile int64
}

func newSeriesEnv(t *testing.T) *seriesEnv {
	t.Helper()
	le := newLibraryEnv(t)
	le.client.write = writeRelease
	se := &seriesEnv{libraryEnv: le, tv: filepath.Join(le.data, "media", "tv")}
	var rf struct{ ID int64 }
	le.expect(le.do("POST", "/api/v1/rootfolders", map[string]any{"path": se.tv, "kind": "series"}), 201).json(&rf)
	se.tvRoot = rf.ID
	var profiles []struct {
		ID         int64
		Name, Kind string
	}
	le.expect(le.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	for _, p := range profiles {
		if p.Kind == "series" && p.Name == "HD-720p/1080p" {
			se.tvProfile = p.ID
		}
	}
	if se.tvProfile == 0 {
		t.Fatal("no series profile")
	}
	return se
}

type apiEpisode struct {
	ID            int64
	ItemRef       string
	SeasonNumber  int
	EpisodeNumber int
	Monitored     bool
	Aired         bool
	Wanted        string
	File          *struct {
		Path    string
		Quality struct{ ID string }
	}
}

type apiSeries struct {
	ID      int64
	Title   string
	Path    string
	Seasons []struct {
		ID        int64
		Number    int
		Monitored bool
	}
	Stats struct {
		EpisodeCount, EpisodeFileCount, MissingCount int
	}
}

func (se *seriesEnv) add(tmdbID int64, typ, monitor string, search bool) apiSeries {
	se.t.Helper()
	var s apiSeries
	se.expect(se.do("POST", "/api/v1/series", map[string]any{"tmdbId": tmdbID, "rootFolderId": se.tvRoot, "qualityProfileId": se.tvProfile,
		"seriesType": typ, "monitor": monitor, "searchOnAdd": search}), 201).json(&s)
	return s
}

func (se *seriesEnv) episodes(id int64) []apiEpisode {
	se.t.Helper()
	var list []apiEpisode
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/series/%d/episodes", id), nil), 200).json(&list)
	return list
}

func (se *seriesEnv) episode(id int64, season, number int) apiEpisode {
	se.t.Helper()
	for _, e := range se.episodes(id) {
		if e.SeasonNumber == season && e.EpisodeNumber == number {
			return e
		}
	}
	se.t.Fatalf("no S%02dE%02d", season, number)
	return apiEpisode{}
}

func (se *seriesEnv) downloadsFor(ref string) []string {
	rows, err := se.deps.DB.R.Query(`SELECT d.title FROM downloads d JOIN download_refs r ON r.download_id = d.id WHERE r.item_ref = ? ORDER BY d.id`, ref)
	if err != nil {
		se.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func (se *seriesEnv) timeline(ref string) apiTimeline {
	se.t.Helper()
	var tl apiTimeline
	se.expect(se.do("GET", "/api/v1/timeline?item="+ref, nil), 200).json(&tl)
	return tl
}

// The acceptance flow: add → episodes monitored → automatic season search
// (pack preferred over the single episode) → grab → completion → hardlink
// import of every episode of the pack → renamed into the season folder →
// timeline per episode.
func TestSeriesSeasonPackEndToEnd(t *testing.T) {
	se := newSeriesEnv(t)
	var look []struct {
		TmdbId, TvdbId int64
		Title          string
		SeriesId       *int64
	}
	se.expect(se.do("GET", "/api/v1/series/lookup?term=cosmos", nil), 200).json(&look)
	if len(look) != 1 || look[0].TmdbId != 800434 || look[0].SeriesId != nil {
		t.Fatalf("lookup: %+v", look)
	}
	se.expect(se.do("GET", "/api/v1/series/lookup?term=tvdb:434343", nil), 200).json(&look)
	if len(look) != 1 || look[0].Title != "Cosmos Laundromat" || look[0].TvdbId != 434343 {
		t.Fatalf("tvdb lookup: %+v", look)
	}

	s := se.add(800434, "standard", "all", true)
	if s.Path != filepath.Join(se.tv, "Cosmos Laundromat (2025)") && !strings.HasPrefix(s.Path, filepath.Join(se.tv, "Cosmos Laundromat (")) {
		t.Fatalf("path %s", s.Path)
	}
	se.expect(se.do("POST", "/api/v1/series", map[string]any{"tmdbId": 800434, "rootFolderId": se.tvRoot, "qualityProfileId": se.tvProfile}), 409)
	special := se.episode(s.ID, 0, 1)
	if special.Monitored {
		t.Fatal("specials must not be monitored by default")
	}
	e2 := se.episode(s.ID, 1, 2)
	if !e2.Monitored || !e2.Aired || e2.Wanted != "missing" {
		t.Fatalf("episode: %+v", e2)
	}
	var detail apiSeries
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/series/%d", s.ID), nil), 200).json(&detail)
	var season1 int64
	for _, sn := range detail.Seasons {
		if sn.Number == 1 {
			season1 = sn.ID
		}
	}
	defer func() {
		if t.Failed() {
			for _, ev := range se.timeline(fmt.Sprintf("season:%d", season1)).Events {
				t.Logf("season %s: %s %s", ev.Type, ev.Message, ev.Data)
			}
		}
	}()
	waitFor(t, "the season pack grab", func() bool { return len(se.downloadsFor(fmt.Sprintf("season:%d", season1))) == 1 })
	if got := se.downloadsFor(e2.ItemRef); len(got) != 1 || got[0] != "Cosmos.Laundromat.S01.720p.WEB-DL.AAC2.0.x264-LOCARR" {
		t.Fatalf("episode 2 is covered by %v (the pack must win over the single episode)", got)
	}
	// The queue names the season.
	var queue struct {
		Items []struct{ Item *struct{ Ref, Title string } }
	}
	se.poll()
	se.expect(se.do("GET", "/api/v1/queue", nil), 200).json(&queue)
	if len(queue.Items) != 1 || queue.Items[0].Item == nil || !strings.Contains(queue.Items[0].Item.Title, "season 1") {
		t.Fatalf("queue: %+v", queue.Items)
	}
	var ex struct{ Code string }
	se.expect(se.do("GET", "/api/v1/timeline/explain?item="+e2.ItemRef, nil), 200).json(&ex)
	if ex.Code != "downloading" {
		t.Fatalf("explain while the pack downloads: %s", ex.Code)
	}

	se.client.finish(t)
	se.poll()
	waitFor(t, "the pack import", func() bool { return se.episode(s.ID, 1, 3).File != nil })
	pack := filepath.Join(se.downloads, "Cosmos.Laundromat.S01.720p.WEB-DL.AAC2.0.x264-LOCARR")
	titles := map[int]string{1: "First Cycle", 2: "Second Cycle", 3: "Third Cycle"}
	for n := 1; n <= 3; n++ {
		e := se.episode(s.ID, 1, n)
		want := filepath.Join(s.Path, "Season 01", fmt.Sprintf("Cosmos Laundromat - S01E%02d - %s [WEBDL-720p]-LOCARR.mkv", n, titles[n]))
		if e.File == nil || e.File.Path != want || e.File.Quality.ID != "webdl-720p" {
			t.Fatalf("S01E%02d file %+v, want %s", n, e.File, want)
		}
		a, _ := os.Stat(filepath.Join(pack, fmt.Sprintf("Cosmos.Laundromat.S01E%02d.720p.WEB-DL.AAC2.0.x264-LOCARR.mkv", n)))
		b, _ := os.Stat(want)
		if a == nil || b == nil || !os.SameFile(a, b) {
			t.Fatalf("S01E%02d is not a hardlink of the download", n)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Path, "Season 01", "Cosmos Laundromat - S01E01 - First Cycle [WEBDL-720p]-LOCARR.en.srt")); err != nil {
		t.Fatal("subtitle sidecar not carried along")
	}
	entries, _ := os.ReadDir(filepath.Join(s.Path, "Season 01"))
	if len(entries) != 4 {
		t.Fatalf("season folder holds %d entries (sample or nfo imported?)", len(entries))
	}
	var status, msg string
	_ = se.deps.DB.R.QueryRow(`SELECT d.status, e.message FROM downloads d JOIN download_events e ON e.download_id = d.id AND e.type = 'imported'
		WHERE d.id IN (SELECT download_id FROM download_refs WHERE item_ref = ?)`, fmt.Sprintf("season:%d", season1)).Scan(&status, &msg)
	if status != "imported" || !strings.Contains(msg, "3 episodes") {
		t.Fatalf("download %s: %s", status, msg)
	}
	tl := se.timeline(e2.ItemRef)
	for _, typ := range []string{"search", "decision", "download_grabbed", "download_completed", "file_imported", "renamed", "download_imported"} {
		if !strings.Contains(" "+tl.types()+" ", " "+typ+" ") {
			t.Errorf("episode timeline misses %s: %s", typ, tl.types())
		}
	}
	if tl.Explanation.Code != "upgrade_wanted" { // 720p is below the profile's WEB 1080p cutoff
		t.Fatalf("explanation: %+v", tl.Explanation)
	}
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/series/%d", s.ID), nil), 200).json(&detail)
	if detail.Stats.EpisodeFileCount != 3 || detail.Stats.MissingCount != 0 {
		t.Fatalf("stats %+v", detail.Stats)
	}

	// Rename preview: nothing to do; a new template renames every file in place.
	var plan []struct{ From, To string }
	se.expect(se.do("POST", "/api/v1/series/rename/preview", map[string]any{"seriesIds": []int64{s.ID}}), 200).json(&plan)
	if len(plan) != 0 {
		t.Fatalf("rename plan: %+v", plan)
	}
	var naming map[string]any
	se.expect(se.do("GET", "/api/v1/settings/mediamanagement/series", nil), 200).json(&naming)
	naming["standardEpisodeFormat"] = "{SeriesTitle} {EpisodeCode}"
	se.expect(se.do("PUT", "/api/v1/settings/mediamanagement/series", naming), 200)
	se.expect(se.do("POST", "/api/v1/series/rename", map[string]any{"seriesIds": []int64{s.ID}}), 200).json(&plan)
	if len(plan) != 3 || !strings.HasSuffix(plan[0].To, "Season 01/Cosmos Laundromat S01E01.mkv") {
		t.Fatalf("rename: %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(s.Path, "Season 01", "Cosmos Laundromat S01E01.en.srt")); err != nil {
		t.Fatal("the sidecar did not follow the rename")
	}
	naming["standardEpisodeFormat"] = "{Title}"
	se.expect(se.do("PUT", "/api/v1/settings/mediamanagement/series", naming), 400)
}

// Episodes searched one by one (below the season pack threshold), the
// calendar and iCal feed, Wanted, monitoring and the refresh merge.
func TestSeriesEpisodesCalendarAndRefresh(t *testing.T) {
	se := newSeriesEnv(t)
	s := se.add(800424, "standard", "all", true)
	e1, e2 := se.episode(s.ID, 1, 1), se.episode(s.ID, 1, 2)
	defer func() {
		if t.Failed() {
			t.Logf("e1 timeline: %+v", se.timeline(e1.ItemRef))
		}
	}()
	waitFor(t, "two episode grabs", func() bool { return len(se.downloadsFor(e1.ItemRef)) == 1 && len(se.downloadsFor(e2.ItemRef)) == 1 })
	if got := se.downloadsFor(e1.ItemRef); got[0] != "Elephants.Dream.S01E01.1080p.WEB.h264-LOCARR" {
		t.Fatalf("E01: %v", got)
	}
	var n int
	_ = se.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&n)
	if n != 2 {
		t.Fatalf("%d downloads, want one per aired episode", n)
	}
	se.client.finish(t)
	se.poll()
	waitFor(t, "both imports", func() bool { return se.episode(s.ID, 1, 2).File != nil && se.episode(s.ID, 1, 1).File != nil })

	// Calendar: the two coming episodes, in the feed too.
	now := time.Now().UTC()
	var cal []struct {
		Kind                        string
		Title, EpisodeTitle         string
		SeasonNumber, EpisodeNumber int
		AirDateUtc                  *time.Time
	}
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/calendar?start=%s&end=%s", now.Format("2006-01-02"), now.AddDate(0, 0, 30).Format("2006-01-02")), nil), 200).json(&cal)
	got := 0
	for _, c := range cal {
		if c.Kind == "episode" && c.Title == "Elephants Dream" && c.AirDateUtc != nil {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("calendar: %+v", cal)
	}
	var feed struct{ Path string }
	se.expect(se.do("POST", "/api/v1/calendar/token", nil), 201).json(&feed)
	res := se.expect(se.req("GET", feed.Path, nil, nil), 200)
	if body := string(res.body); !strings.Contains(body, "SUMMARY:Elephants Dream - S01E03 - Proog 3") || !strings.Contains(body, "LOCATION:Orange Open Movie") {
		t.Fatalf("ical: %s", body)
	}

	// Wanted: nothing missing now; unmonitor/monitor episodes in bulk.
	var wanted struct{ Total int }
	se.expect(se.do("GET", "/api/v1/episodes/wanted", nil), 200).json(&wanted)
	if wanted.Total != 0 {
		t.Fatalf("wanted %d", wanted.Total)
	}
	e3 := se.episode(s.ID, 1, 3)
	var cnt struct{ Count int }
	se.expect(se.do("POST", "/api/v1/episodes/monitor", map[string]any{"episodeIds": []int64{e3.ID}, "monitored": false}), 200).json(&cnt)
	if cnt.Count != 1 || se.episode(s.ID, 1, 3).Monitored {
		t.Fatal("unmonitor")
	}
	se.expect(se.do("PATCH", fmt.Sprintf("/api/v1/series/%d/seasons/1", s.ID), map[string]any{"monitored": true}), 200)
	if !se.episode(s.ID, 1, 3).Monitored {
		t.Fatal("season monitoring did not reach the episode")
	}

	// Refresh: the provider renumbers E04 to E05, adds E04 and a season 2.
	se.tmdb.EditSeries(func(list []tmdbfake.TVSeries) []tmdbfake.TVSeries {
		ed := &list[1]
		eps := ed.Seasons[0].Episodes
		last := eps[3]
		last.Number = 5
		ed.Seasons[0].Episodes = append(append([]tmdbfake.TVEpisode{}, eps[:3]...),
			tmdbfake.TVEpisode{ID: 4240150, Number: 4, Title: "Inserted", AirDate: last.AirDate}, last)
		ed.Seasons = append(ed.Seasons, tmdbfake.TVSeason{Number: 2, Title: "Season 2",
			Episodes: []tmdbfake.TVEpisode{{ID: 4240201, Number: 1, Title: "New Season", AirDate: last.AirDate}}})
		return list
	})
	old4 := se.episode(s.ID, 1, 4)
	se.expect(se.do("POST", fmt.Sprintf("/api/v1/series/%d/refresh", s.ID), nil), 200)
	moved := se.episode(s.ID, 1, 5)
	if moved.ID != old4.ID {
		t.Fatalf("renumbered episode lost its identity: %d vs %d", moved.ID, old4.ID)
	}
	if ins := se.episode(s.ID, 1, 4); !ins.Monitored || ins.ID == old4.ID {
		t.Fatalf("inserted episode %+v", ins)
	}
	if ns := se.episode(s.ID, 2, 1); !ns.Monitored {
		t.Fatal("a new season is monitored (monitorNewSeasons = all)")
	}
	if tl := se.timeline(moved.ItemRef); !strings.Contains(tl.types(), "refreshed") {
		t.Fatalf("renumbering not on the timeline: %s", tl.types())
	}
	// An episode with a file that the provider drops is kept, unmonitored.
	se.tmdb.EditSeries(func(list []tmdbfake.TVSeries) []tmdbfake.TVSeries {
		list[1].Seasons[0].Episodes = list[1].Seasons[0].Episodes[1:]
		return list
	})
	se.expect(se.do("POST", fmt.Sprintf("/api/v1/series/%d/refresh", s.ID), nil), 200)
	var kept apiEpisode
	for _, e := range se.episodes(s.ID) {
		if e.ID == e1.ID {
			kept = e
		}
	}
	if kept.ID == 0 || kept.File == nil || kept.Monitored {
		t.Fatalf("dropped episode with a file: %+v", kept)
	}
}

// Anime (absolute numbers) and daily shows (air dates) end to end.
func TestSeriesAnimeAndDaily(t *testing.T) {
	se := newSeriesEnv(t)
	a := se.add(800454, "anime", "latest_season", true)
	if e := se.episode(a.ID, 1, 1); e.Monitored {
		t.Fatal("latest season only")
	}
	s2e1 := se.episode(a.ID, 2, 1)
	waitFor(t, "the anime grab", func() bool { return len(se.downloadsFor(s2e1.ItemRef)) == 1 })
	d := se.add(800464, "daily", "all", true)
	mar14 := se.episode(d.ID, 2025, 2)
	waitFor(t, "the daily grab", func() bool { return len(se.downloadsFor(mar14.ItemRef)) == 1 })
	se.client.finish(t)
	se.poll()
	waitFor(t, "imports", func() bool { return se.episode(a.ID, 2, 1).File != nil && se.episode(d.ID, 2025, 2).File != nil })
	if f := se.episode(a.ID, 2, 1).File.Path; !strings.HasSuffix(f, "Season 02/Spring Kurenai - S02E01 - 003 - Bloom 1 [WEBDL-1080p]-LOCARR.mkv") &&
		!strings.HasSuffix(f, "Season 02/Spring Kurenai - S02E01 - 003 - Bloom 1 [HDTV-1080p]-LOCARR.mkv") {
		t.Fatalf("anime name %s", f)
	}
	if f := se.episode(d.ID, 2025, 2).File.Path; !strings.HasSuffix(f, "The Open Desk - 2025-03-14 - March 14 [WEBDL-720p]-LOCARR.mkv") {
		t.Fatalf("daily name %s", f)
	}
	// A season pack offered to one episode is rejected with a reason.
	var rep struct {
		Decisions []struct {
			Decision struct {
				Status     string
				Candidate  struct{ Title string }
				Rejections []struct{ Code string }
			}
		}
	}
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/episodes/%d/releases", s2e1.ID), nil), 200).json(&rep)
	if len(rep.Decisions) == 0 {
		t.Fatal("interactive search found nothing")
	}
	// Scene mapping: releases call S02E01 absolute 7.
	se.expect(se.do("PATCH", fmt.Sprintf("/api/v1/episodes/%d", s2e1.ID), map[string]any{"sceneAbsolute": 7}), 200)
	var ep struct{ SceneAbsolute *int }
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/episodes/%d", s2e1.ID), nil), 200).json(&ep)
	if ep.SceneAbsolute == nil || *ep.SceneAbsolute != 7 {
		t.Fatalf("scene mapping %+v", ep)
	}
	se.expect(se.do("PATCH", fmt.Sprintf("/api/v1/episodes/%d", s2e1.ID), map[string]any{"clearScene": true}), 200)
	se.expect(se.req("GET", "/api/v1/episodes/999999", nil, nil), http.StatusNotFound)
}
