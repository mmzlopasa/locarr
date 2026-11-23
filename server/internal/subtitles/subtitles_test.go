package subtitles

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers/fake"
)

func bunny() *Target {
	return &Target{Ref: library.MovieRef(1), Kind: providers.KindMovie, ItemTitle: "Big Buck Bunny", Year: 2008, IMDbID: "tt1254207", TMDbID: 10378,
		Path: "/m/Big Buck Bunny (2008)/Big Buck Bunny (2008) [Bluray-1080p][x264]-LOCARR.mkv", SceneName: "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR"}
}

func TestScoreMovie(t *testing.T) {
	tg := bunny()
	file := tg.Release()
	hr := Want{Language: "hr", HearingImpaired: HIInclude}
	exact := Score(tg, file, hr, providers.Result{Language: "hr", IMDbID: "tt1254207", Year: 2008, Releases: []string{"Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR"},
		Trusted: true, Downloads: 150})
	if exact.Score != 90 || exact.Rejected != "" {
		t.Fatalf("exact release: %d %v %s", exact.Score, exact.Parts, exact.Rejected)
	}
	other := Score(tg, file, hr, providers.Result{Language: "hr", IMDbID: "tt1254207", Year: 2008, Releases: []string{"Big.Buck.Bunny.2008.720p.WEB-DL.H264-OTHER"}})
	if other.Score != 34 {
		t.Fatalf("other release: %d %v", other.Score, other.Parts)
	}
	hash := Score(tg, file, hr, providers.Result{Language: "hr", HashMatch: true, Releases: []string{"whatever"}})
	if hash.Score != 100 {
		t.Fatalf("hash: %d", hash.Score)
	}
	hi := Score(tg, file, hr, providers.Result{Language: "hr", HearingImpaired: true, IMDbID: "tt1254207"})
	if !strings.Contains(partsString(hi.Parts), "hearing_impaired-2") {
		t.Fatalf("hi penalty: %v", hi.Parts)
	}
	if r := Score(tg, file, Want{Language: "hr", HearingImpaired: HIExclude}, providers.Result{Language: "hr", HearingImpaired: true}); r.Rejected != "hearing_impaired" {
		t.Fatalf("hi excluded: %s", r.Rejected)
	}
	if r := Score(tg, file, hr, providers.Result{Language: "hr", Forced: true}); r.Rejected != "forced_only" {
		t.Fatalf("forced: %s", r.Rejected)
	}
	if r := Score(tg, file, Want{Language: "hr", Forced: true}, providers.Result{Language: "hr"}); r.Rejected != "not_forced" {
		t.Fatalf("not forced: %s", r.Rejected)
	}
	if r := Score(tg, file, hr, providers.Result{Language: "hr", IMDbID: "tt0000001"}); r.Rejected != "other_movie" {
		t.Fatalf("other movie: %s", r.Rejected)
	}
	mt := Score(tg, file, hr, providers.Result{Language: "hr", IMDbID: "tt1254207", MachineTranslated: true})
	if mt.Score != 0 {
		t.Fatalf("machine translated: %d", mt.Score)
	}
	list := []Scored{other, exact, Score(tg, file, hr, providers.Result{Language: "en"})}
	Rank(list)
	if list[0].Score != 90 || list[2].Rejected != "language" {
		t.Fatalf("rank: %+v", list)
	}
}

func partsString(ps []ScorePart) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Code)
		if p.Points < 0 {
			b.WriteString(strconv.Itoa(p.Points))
		}
		b.WriteString(" ")
	}
	return b.String()
}

func TestScoreEpisode(t *testing.T) {
	tg := &Target{Ref: library.EpisodeRef(7), Kind: providers.KindEpisode, ItemTitle: "Elephants Dream", IMDbID: "tt9100424", TMDbID: 800424,
		Season: 1, Episode: 2, Path: "/tv/Elephants Dream/Season 01/Elephants Dream - S01E02 [WEBDL-1080p].mkv", SceneName: "Elephants.Dream.S01E02.1080p.WEB.h264-LOCARR"}
	file := tg.Release()
	w := Want{Language: "hr", HearingImpaired: HIInclude}
	ok := Score(tg, file, w, providers.Result{Language: "hr", Season: 1, Episode: 2, Releases: []string{"Elephants.Dream.S01E02.1080p.WEB.h264-LOCARR"}})
	if ok.Score != 72 || ok.Rejected != "" {
		t.Fatalf("episode: %d %v", ok.Score, ok.Parts)
	}
	if r := Score(tg, file, w, providers.Result{Language: "hr", Season: 1, Episode: 3}); r.Rejected != "other_episode" {
		t.Fatalf("other episode: %s", r.Rejected)
	}
}

func TestSidecarPathAndInfo(t *testing.T) {
	v := "/m/Movie.Name.(2008)/Movie.Name.(2008).mkv"
	for _, c := range []struct {
		forced, hi bool
		want       string
	}{{false, false, "/m/Movie.Name.(2008)/Movie.Name.(2008).hr.srt"}, {true, false, "/m/Movie.Name.(2008)/Movie.Name.(2008).hr.forced.srt"},
		{false, true, "/m/Movie.Name.(2008)/Movie.Name.(2008).hr.hi.srt"}} {
		if got := SidecarPath(v, "hr", c.forced, c.hi, "srt"); got != c.want {
			t.Errorf("got %s want %s", got, c.want)
		}
	}
	for suffix, want := range map[string][4]string{
		".hr.srt": {"hr", "", "", "srt"}, ".hrv.forced.srt": {"hr", "forced", "", "srt"}, ".en.sdh.ass": {"en", "", "hi", "ass"},
		".srt": {"unknown", "", "", "srt"}, ".croatian.srt": {"hr", "", "", "srt"},
	} {
		lang, forced, hi, format, ok := sidecarInfo(suffix)
		f, h := "", ""
		if forced {
			f = "forced"
		}
		if hi {
			h = "hi"
		}
		if !ok || lang != want[0] || f != want[1] || h != want[2] || format != want[3] {
			t.Errorf("%s: %s %v %v %s %v", suffix, lang, forced, hi, format, ok)
		}
	}
	if _, _, _, _, ok := sidecarInfo(".nfo"); ok {
		t.Error("nfo is not a subtitle")
	}
}

func TestPrepare(t *testing.T) {
	p, err := Prepare(fake.CP1250(strings.ReplaceAll(fake.SRT(fake.CroatianLines...), "\n", "\r\n")), "hr", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Encoding != "windows-1250" || p.Format != "srt" || p.Cues != 6 || strings.Contains(p.Text, "\r") || !strings.Contains(p.Text, "Đuro, ćao!") {
		t.Fatalf("%+v", p)
	}
	ass := "[Script Info]\nScriptType: v4.00+\n\n[V4+ Styles]\nFormat: Name\nStyle: Default\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,{\\b1}Bok{\\b0}\\Nsvijete\n"
	p, err = Prepare([]byte(ass), "hr", "srt")
	if err != nil || p.Format != "srt" || !strings.Contains(p.Text, "00:00:01,000 --> 00:00:02,000\nBok\nsvijete") {
		t.Fatalf("ass to srt: %+v %v", p, err)
	}
	if _, err := Prepare([]byte("<html>"), "hr", ""); err == nil {
		t.Fatal("html accepted")
	}
}

func TestWantKeyAndSatisfies(t *testing.T) {
	if (Want{Language: "en", HearingImpaired: HIOnly}).Key() != "en:hi" || (Want{Language: "hr", Forced: true}).Key() != "hr:forced" {
		t.Fatal("keys")
	}
	if !satisfies(Want{Language: "hr", HearingImpaired: HIInclude}, "hr", false, true) || satisfies(Want{Language: "hr", HearingImpaired: HIExclude}, "hr", false, true) ||
		satisfies(Want{Language: "hr", Forced: true}, "hr", false, false) {
		t.Fatal("satisfies")
	}
	if SearchBackoff(1) != 6*3600e9 || SearchBackoff(20) != 7*24*3600e9 {
		t.Fatal("backoff")
	}
}
