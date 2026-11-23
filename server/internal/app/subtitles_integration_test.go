package app

import (
	"context"
	"fmt"
	"math/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	subfake "github.com/mmzlopasa/locarr/server/internal/subtitles/providers/fake"
	subsync "github.com/mmzlopasa/locarr/server/internal/subtitles/sync"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/tracks/trackstest"
)

type subEnv struct {
	*libraryEnv
	subs    *subfake.Server
	subsURL string
}

// withSubtitles points the subtitle service at the fake providers and
// configures OpenSubtitles (with a user) and SubDL.
func withSubtitles(t *testing.T, le *libraryEnv) *subEnv {
	t.Helper()
	f := subfake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	le.deps.Subtitles.URLs = map[string]string{"opensubtitles": srv.URL + "/opensubtitles/api/v1", "subdl": srv.URL + "/subdl/api/v1",
		"jimaku": srv.URL + "/jimaku/api"}
	se := &subEnv{libraryEnv: le, subs: f, subsURL: srv.URL}
	se.expect(se.do("PUT", "/api/v1/subtitles/providers/opensubtitles", map[string]any{"enabled": true, "apiKey": subfake.OSKey,
		"username": subfake.OSUser, "password": subfake.OSPassword}), 200)
	se.expect(se.do("PUT", "/api/v1/subtitles/providers/subdl", map[string]any{"enabled": true, "apiKey": subfake.SubDLKey}), 200)
	return se
}

type apiSubtitleItem struct {
	Ref           string
	HasFile       bool
	ProfileSource string
	Wants         []struct {
		Key, Status string
		SubtitleId  int64
	}
	Subtitles []struct {
		ID               int64
		Path, Language   string
		Source, Provider string
		ProviderId       string
		Score            *int
		SyncState        string
		SyncMessage      string
		SyncOffsetMs     int64
		Forced           bool
		HearingImpaired  bool
		Format           string
		Encoding         string
	}
	Explanation   struct{ Code, Message string }
	SyncAvailable bool
}

func (se *subEnv) item(ref string) apiSubtitleItem {
	se.t.Helper()
	var it apiSubtitleItem
	se.expect(se.do("GET", "/api/v1/subtitles/items/"+ref, nil), 200).json(&it)
	return it
}

// dump logs an item's subtitles and timeline when the test failed.
func (se *subEnv) dump(ref string) {
	if !se.t.Failed() {
		return
	}
	r := se.do("GET", "/api/v1/subtitles/items/"+ref, nil)
	se.t.Logf("item: %s", r.body)
	var tl apiTimeline
	se.do("GET", "/api/v1/timeline?item="+ref, nil).json(&tl)
	for _, ev := range tl.Events {
		se.t.Logf("timeline %s: %s %s", ev.Type, ev.Message, ev.Data)
	}
	jobs, _ := se.deps.Jobs.List(context.Background(), "", 50)
	for _, j := range jobs {
		se.t.Logf("job %s %s %s", j.Kind, j.State, j.Error)
	}
}

func (it apiSubtitleItem) status(key string) string {
	for _, w := range it.Wants {
		if w.Key == key {
			return w.Status
		}
	}
	return ""
}

func (it apiSubtitleItem) lang(l string) (int64, string, string, string) {
	for _, s := range it.Subtitles {
		if s.Language == l && !s.Forced {
			return s.ID, s.Path, s.Provider, s.ProviderId
		}
	}
	return 0, "", "", ""
}

func (se *subEnv) profile(name string, langs ...map[string]any) int64 {
	se.t.Helper()
	var p struct{ ID int64 }
	se.expect(se.do("POST", "/api/v1/subtitles/profiles", map[string]any{"name": name, "languages": langs, "skipEmbedded": true}), 201).json(&p)
	return p.ID
}

// The acceptance flow for movies: a movie is imported through the fakes →
// Locarr searches the providers, scores, downloads and saves the Croatian
// subtitle (converted from Windows-1250) next to the file and records it →
// a manual search picks another → delete with blacklist → shift and FPS tools.
func TestSubtitlesMovieFlow(t *testing.T) {
	le := newLibraryEnv(t)
	se := withSubtitles(t, le)

	var prov []struct {
		Name           string
		Enabled        bool
		QuotaRemaining *int
		Account        string
		ApiKey         struct{ Configured bool }
	}
	se.expect(se.do("POST", "/api/v1/subtitles/providers/opensubtitles/test", nil), 200)
	se.expect(se.do("GET", "/api/v1/subtitles/providers", nil), 200).json(&prov)
	if len(prov) != 3 || prov[0].Name != "opensubtitles" || !prov[0].ApiKey.Configured || prov[0].QuotaRemaining == nil ||
		*prov[0].QuotaRemaining != 20 || !strings.Contains(prov[0].Account, "locarr") {
		t.Fatalf("providers: %+v", prov)
	}
	// Keys are never returned.
	raw := se.expect(se.do("GET", "/api/v1/subtitles/providers", nil), 200)
	if strings.Contains(string(raw.body), subfake.OSKey) || strings.Contains(string(raw.body), subfake.OSPassword) {
		t.Fatal("a provider secret leaked")
	}
	// Jimaku cannot be enabled without its key.
	if r := se.do("PUT", "/api/v1/subtitles/providers/jimaku", map[string]any{"enabled": true}); r.StatusCode != 400 {
		t.Fatalf("jimaku without a key: %d", r.StatusCode)
	}

	pid := se.profile("Hrvatski + English", map[string]any{"language": "hr", "forced": false, "hearingImpaired": "include"},
		map[string]any{"language": "en", "forced": false, "hearingImpaired": "include"})
	se.expect(se.do("PUT", "/api/v1/subtitles/settings", map[string]any{"defaultProfileMovies": pid}), 200)
	se.expect(se.do("DELETE", fmt.Sprintf("/api/v1/subtitles/profiles/%d", pid), nil), 409)

	m := le.addMovie(10378, true)
	ref := fmt.Sprintf("movie:%d", m.ID)
	defer se.dump(ref)
	if it := se.item(ref); it.HasFile || it.Explanation.Code != "no_file" {
		t.Fatalf("before import: %+v", it)
	}
	waitFor(t, "the grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.client.finish(t)
	le.poll()
	waitFor(t, "the import", func() bool { return le.movie(m.ID).File != nil })
	waitFor(t, "the Croatian subtitle", func() bool { return se.item(ref).status("hr") == "present" })

	it := se.item(ref)
	video := le.movie(m.ID).File.Path
	want := strings.TrimSuffix(video, ".mkv") + ".hr.srt"
	_, path, provider, pidHR := it.lang("hr")
	if path != want || provider != "opensubtitles" || pidHR != "9100001" {
		t.Fatalf("hr subtitle: %s from %s/%s (want %s): %+v", path, provider, pidHR, want, it.Subtitles)
	}
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Đuro, ćao!") || strings.Contains(string(b), "\r") {
		t.Fatalf("saved subtitle is not UTF-8 with LF: %q", b)
	}
	// The English subtitle came with the release (imported as a sidecar) and was found on disk.
	if it.status("en") != "present" {
		t.Fatalf("en: %+v", it.Wants)
	}
	for _, s := range it.Subtitles {
		if s.Language == "en" && s.Source != "disk" {
			t.Fatalf("en should be from disk: %+v", s)
		}
		if s.Language == "hr" && (s.Score == nil || *s.Score != 90 || s.Encoding != "windows-1250") {
			t.Fatalf("hr score/encoding: %+v", s)
		}
	}
	if it.Explanation.Code != "complete" {
		t.Fatalf("explanation: %+v", it.Explanation)
	}
	tl := le.timeline(m.ID)
	if !strings.Contains(tl.types(), "subtitle_downloaded") {
		t.Fatalf("timeline: %s", tl.types())
	}
	found := false
	for _, ev := range tl.Events {
		if ev.Type == "subtitle_downloaded" && strings.Contains(ev.Message, "subtitle hr found on opensubtitles, score 90, saved as "+filepath.Base(want)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no download event with the score: %+v", tl.Events)
	}

	// Manual search: scored results from both providers; pick SubDL's.
	var ms struct {
		Results []struct {
			Key, Provider, ID, Language string
			Score                       int
			Downloaded                  bool
			Rejected                    *string
		}
		Providers []struct {
			Provider string
			Results  int
		}
	}
	se.expect(se.do("GET", "/api/v1/subtitles/items/"+ref+"/results?language=hr", nil), 200).json(&ms)
	if len(ms.Providers) < 2 {
		t.Fatalf("providers asked: %+v", ms.Providers)
	}
	pick := ""
	for _, r := range ms.Results {
		if r.ID == "9100001" && !r.Downloaded {
			t.Fatal("the downloaded subtitle is not marked")
		}
		if r.Provider == "subdl" && r.Rejected == nil {
			pick = r.Key
		}
	}
	if pick == "" {
		t.Fatalf("no SubDL result: %+v", ms.Results)
	}
	if ms.Results[0].ID != "9100001" {
		t.Fatalf("best result first: %+v", ms.Results[0])
	}
	var sub struct {
		ID         int64
		Provider   string
		ProviderId string
		Path       string
	}
	se.expect(se.do("POST", "/api/v1/subtitles/items/"+ref+"/download", map[string]any{"key": pick}), 200).json(&sub)
	if sub.Provider != "subdl" || sub.Path != want {
		t.Fatalf("manual pick: %+v", sub)
	}
	var bin struct {
		Entries []struct{ OriginalPath string }
	}
	se.expect(se.do("GET", "/api/v1/recyclebin", nil), 200).json(&bin)
	recycled := false
	for _, e := range bin.Entries {
		if e.OriginalPath == want {
			recycled = true
		}
	}
	if !recycled {
		t.Fatalf("the replaced subtitle is not in the recycle bin: %+v", bin)
	}
	se.expect(se.do("POST", "/api/v1/subtitles/items/"+ref+"/download", map[string]any{"key": "nope"}), 410)

	// Tools: preview, shift, frame rate.
	var pv struct {
		Total int
		Lines []struct {
			StartMs, EndMs int64
			Text           string
		}
	}
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/subtitles/%d/preview?lines=2", sub.ID), nil), 200).json(&pv)
	if pv.Total != 6 || len(pv.Lines) != 2 || pv.Lines[0].StartMs != 2000 || !strings.Contains(pv.Lines[0].Text, "šumo") {
		t.Fatalf("preview: %+v", pv)
	}
	se.expect(se.do("POST", fmt.Sprintf("/api/v1/subtitles/%d/shift", sub.ID), map[string]any{"offsetMs": 1500}), 200)
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/subtitles/%d/preview?lines=1", sub.ID), nil), 200).json(&pv)
	if pv.Lines[0].StartMs != 3500 {
		t.Fatalf("after shift: %+v", pv.Lines)
	}
	se.expect(se.do("POST", fmt.Sprintf("/api/v1/subtitles/%d/fps", sub.ID), map[string]any{"from": "25", "to": "23.976"}), 200)
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/subtitles/%d/preview?lines=1", sub.ID), nil), 200).json(&pv)
	if pv.Lines[0].StartMs != 3649 && pv.Lines[0].StartMs != 3650 {
		t.Fatalf("after fps: %+v", pv.Lines)
	}
	se.expect(se.do("POST", fmt.Sprintf("/api/v1/subtitles/%d/fps", sub.ID), map[string]any{"from": "25", "to": "abc"}), 400)

	// Delete with blacklist: the next automatic search takes neither the
	// blacklisted one nor the one downloaded before, but the next best.
	se.expect(se.do("DELETE", fmt.Sprintf("/api/v1/subtitles/%d?blacklist=true", sub.ID), nil), 204)
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("the deleted subtitle is still there")
	}
	var bl []struct{ Provider, ProviderId string }
	se.expect(se.do("GET", "/api/v1/subtitles/items/"+ref+"/blacklist", nil), 200).json(&bl)
	if len(bl) != 1 || bl[0].Provider != "subdl" {
		t.Fatalf("blacklist: %+v", bl)
	}
	var wanted struct {
		Total int
		Items []struct {
			Ref     string
			Missing []string
		}
	}
	se.expect(se.do("GET", "/api/v1/subtitles/wanted?kind=movie", nil), 200).json(&wanted)
	if wanted.Total != 1 || wanted.Items[0].Ref != ref || len(wanted.Items[0].Missing) != 1 || wanted.Items[0].Missing[0] != "hr" {
		t.Fatalf("wanted: %+v", wanted)
	}
	var out struct {
		Downloaded []struct{ Provider, ProviderId string }
		Missing    []string
	}
	se.expect(se.do("POST", "/api/v1/subtitles/items/"+ref+"/search", nil), 200).json(&out)
	if len(out.Downloaded) != 1 || out.Downloaded[0].ProviderId != "9100003" {
		t.Fatalf("search after blacklist: %+v", out)
	}
	se.expect(se.do("GET", "/api/v1/subtitles/wanted?kind=movie", nil), 200).json(&wanted)
	if wanted.Total != 0 {
		t.Fatalf("still wanted: %+v", wanted)
	}
}

// A Croatian profile for series: an imported episode gets its subtitle.
func TestSubtitlesEpisodeAfterImport(t *testing.T) {
	sre := newSeriesEnv(t)
	se := withSubtitles(t, sre.libraryEnv)
	pid := se.profile("Hrvatski", map[string]any{"language": "hr", "forced": false, "hearingImpaired": "exclude"})
	se.expect(se.do("PUT", "/api/v1/subtitles/settings", map[string]any{"defaultProfileSeries": pid}), 200)
	s := sre.add(800424, "standard", "all", true)
	e1 := sre.episode(s.ID, 1, 1)
	waitFor(t, "the episode grab", func() bool { return len(sre.downloadsFor(e1.ItemRef)) >= 1 })
	sre.client.finish(t)
	sre.poll()
	waitFor(t, "the episode import", func() bool { return sre.episode(s.ID, 1, 1).File != nil })
	waitFor(t, "the episode subtitle", func() bool { return se.item(e1.ItemRef).status("hr") == "present" })
	it := se.item(e1.ItemRef)
	_, path, provider, id := it.lang("hr")
	video := sre.episode(s.ID, 1, 1).File.Path
	if path != strings.TrimSuffix(video, filepath.Ext(video))+".hr.srt" || provider != "opensubtitles" || id != "9424201" {
		t.Fatalf("episode subtitle %s %s %s: %+v", path, provider, id, it.Subtitles)
	}
	if !strings.Contains(sre.timeline(e1.ItemRef).types(), "subtitle_downloaded") {
		t.Fatal("no timeline entry")
	}
	// Turning subtitles off for the series empties what it misses.
	se.expect(se.do("POST", "/api/v1/subtitles/profiles/assign", map[string]any{"itemIds": []int64{s.ID}, "profileId": -1}), 200)
	if it := se.item(e1.ItemRef); it.Explanation.Code != "disabled" || it.ProfileSource != "none" {
		t.Fatalf("disabled: %+v", it.Explanation)
	}
}

// Provider trouble: bad key, rate limit, quota exhaustion, a provider that is down.
func TestSubtitleProviderFailures(t *testing.T) {
	le := newLibraryEnv(t)
	se := withSubtitles(t, le)
	pid := se.profile("hr", map[string]any{"language": "hr", "forced": false, "hearingImpaired": "include"})
	se.expect(se.do("PUT", "/api/v1/subtitles/settings", map[string]any{"defaultProfileMovies": pid}), 200)

	// A refused key.
	se.expect(se.do("PUT", "/api/v1/subtitles/providers/subdl", map[string]any{"apiKey": "wrong"}), 200)
	if r := se.do("POST", "/api/v1/subtitles/providers/subdl/test", nil); r.StatusCode != 502 || r.code() != "subtitle_provider_auth" {
		t.Fatalf("bad key: %d %s", r.StatusCode, r.body)
	}
	var prov []struct {
		Name, LastErrorCode string
		RetryAt             *time.Time
		QuotaRemaining      *int
		QuotaResetAt        *time.Time
	}
	get := func(name string) (string, *time.Time, *int, *time.Time) {
		se.expect(se.do("GET", "/api/v1/subtitles/providers", nil), 200).json(&prov)
		for _, p := range prov {
			if p.Name == name {
				return p.LastErrorCode, p.RetryAt, p.QuotaRemaining, p.QuotaResetAt
			}
		}
		t.Fatalf("no provider %s", name)
		return "", nil, nil, nil
	}
	if code, retry, _, _ := get("subdl"); code != "auth" || retry == nil || time.Until(*retry) < 11*time.Hour {
		t.Fatalf("auth backoff: %s %v", code, retry)
	}
	se.expect(se.do("POST", "/api/v1/subtitles/providers/subdl/health/reset", nil), 200)
	se.expect(se.do("PUT", "/api/v1/subtitles/providers/subdl", map[string]any{"apiKey": subfake.SubDLKey}), 200)

	m := le.addMovie(10378, true)
	ref := fmt.Sprintf("movie:%d", m.ID)
	// Rate limited and down: nothing found, both recorded, the item explains why.
	se.subs.Configure(func(f *subfake.Server) { f.OSRateLimit, f.SubDLDown, f.OSQuota = true, true, 1 })
	waitFor(t, "the grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.client.finish(t)
	le.poll()
	waitFor(t, "the import", func() bool { return le.movie(m.ID).File != nil })
	waitFor(t, "the first subtitle search", func() bool {
		code, _, _, _ := get("opensubtitles")
		return code == "rate_limited"
	})
	if code, _, _, _ := get("subdl"); code == "" {
		t.Fatal("SubDL being down was not recorded")
	}
	if it := se.item(ref); it.status("hr") != "missing" || it.Explanation.Code != "providers_unavailable" && it.Explanation.Code != "not_found" {
		t.Fatalf("after failures: %+v", it.Explanation)
	}
	tl := le.timeline(m.ID)
	if !strings.Contains(tl.types(), "subtitle_not_found") {
		t.Fatalf("timeline: %s", tl.types())
	}
	// Recovered, but a quota of one download: the first works, the second is refused.
	se.subs.Configure(func(f *subfake.Server) { f.OSRateLimit, f.SubDLDown = false, false })
	se.expect(se.do("POST", "/api/v1/subtitles/providers/opensubtitles/health/reset", nil), 200)
	se.expect(se.do("POST", "/api/v1/subtitles/providers/subdl/health/reset", nil), 200)
	var out struct{ Downloaded []struct{ Provider string } }
	se.expect(se.do("POST", "/api/v1/subtitles/items/"+ref+"/search", nil), 200).json(&out)
	if len(out.Downloaded) != 1 || out.Downloaded[0].Provider != "opensubtitles" {
		t.Fatalf("download: %+v", out)
	}
	if _, _, rem, _ := get("opensubtitles"); rem == nil || *rem != 0 {
		t.Fatalf("quota after one download: %v", rem)
	}
	var ms struct {
		Results []struct {
			Key, Provider, ID string
		}
	}
	se.expect(se.do("GET", "/api/v1/subtitles/items/"+ref+"/results", nil), 200).json(&ms)
	key := ""
	for _, r := range ms.Results {
		if r.Provider == "opensubtitles" && r.ID == "9100002" {
			key = r.Key
		}
	}
	if r := se.do("POST", "/api/v1/subtitles/items/"+ref+"/download", map[string]any{"key": key}); r.StatusCode != 429 || r.code() != "subtitle_quota" {
		t.Fatalf("over quota: %d %s", r.StatusCode, r.body)
	}
	_, _, rem, reset := get("opensubtitles")
	if rem == nil || *rem != 0 || reset == nil || !reset.After(time.Now()) {
		t.Fatalf("quota state: %v %v", rem, reset)
	}
}

// Audio sync through the API: a subtitle 2.5 s late is re-timed against the
// speech in the video (needs ffmpeg).
func TestSubtitleAudioSync(t *testing.T) {
	ff, ok := subsync.Find(os.Getenv("LOCARR_FFMPEG"))
	if !ok {
		t.Skip("no ffmpeg")
	}
	le := newLibraryEnv(t)
	se := withSubtitles(t, le)
	le.deps.Subtitles.FFmpeg = ff
	pid := se.profile("hr", map[string]any{"language": "hr", "forced": false, "hearingImpaired": "include"})
	se.expect(se.do("PUT", "/api/v1/subtitles/settings", map[string]any{"defaultProfileMovies": pid}), 200)
	se.subs.Configure(func(f *subfake.Server) { f.Catalog = nil }) // nothing to download: the subtitle comes from disk
	m := le.addMovie(10378, true)
	waitFor(t, "the grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.client.finish(t)
	le.poll()
	waitFor(t, "the import", func() bool { return le.movie(m.ID).File != nil })
	video := le.movie(m.ID).File.Path

	r := rand.New(rand.NewSource(7)) // #nosec G404 -- test data
	var cues []trackstest.Cue
	at := 2 * time.Second
	for range 40 {
		d := time.Duration(900+r.Intn(2500)) * time.Millisecond
		cues = append(cues, trackstest.Cue{Start: at, End: at + d, Text: "redak"})
		at += d + time.Duration(400+r.Intn(4000))*time.Millisecond
	}
	mkv := trackstest.MKV{Tracks: []trackstest.Track{{Kind: "audio", Codec: "A_PCM/INT/LIT", Language: "hrv", Default: true}},
		PCM: trackstest.Speech(cues, at+2*time.Second, subsync.Rate), Rate: subsync.Rate}
	if err := mkv.Write(video); err != nil {
		t.Fatal(err)
	}
	var srt strings.Builder
	for i, c := range cues {
		s, e := c.Start+2500*time.Millisecond, c.End+2500*time.Millisecond
		fmt.Fprintf(&srt, "%d\n%s --> %s\nRedak %d\n\n", i+1, srtTime(s), srtTime(e), i+1)
	}
	subPath := strings.TrimSuffix(video, ".mkv") + ".hr.srt"
	if err := os.WriteFile(subPath, []byte(srt.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := fmt.Sprintf("movie:%d", m.ID)
	se.expect(se.do("POST", "/api/v1/subtitles/scan", nil), 202)
	waitFor(t, "the subtitle on disk", func() bool { id, _, _, _ := se.item(ref).lang("hr"); return id > 0 })
	id, _, _, _ := se.item(ref).lang("hr")
	se.expect(se.do("POST", fmt.Sprintf("/api/v1/subtitles/%d/sync", id), map[string]any{"reference": "audio"}), 202)
	var state, msg string
	var off int64
	waitFor(t, "the sync", func() bool {
		for _, s := range se.item(ref).Subtitles {
			if s.ID == id {
				state, msg, off = s.SyncState, s.SyncMessage, s.SyncOffsetMs
			}
		}
		return state != "running" && state != ""
	})
	if state != "done" || off > -2460 || off < -2540 {
		t.Fatalf("sync: %s %q %d ms", state, msg, off)
	}
	var pv struct{ Lines []struct{ StartMs int64 } }
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/subtitles/%d/preview?lines=1", id), nil), 200).json(&pv)
	if d := pv.Lines[0].StartMs - cues[0].Start.Milliseconds(); d > 40 || d < -40 {
		t.Fatalf("first line at %d ms, want %d", pv.Lines[0].StartMs, cues[0].Start.Milliseconds())
	}
}

func srtTime(d time.Duration) string {
	ms := d / time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
