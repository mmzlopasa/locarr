package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/requests"
)

func TestMapQuality(t *testing.T) {
	cases := []struct {
		name string
		kind quality.Kind
		want string
	}{
		{"WEBDL-1080p", quality.Movies, "webdl-1080p"},
		{"Bluray-1080p Remux", quality.Series, "remux-1080p"},
		{"Remux-2160p", quality.Movies, "remux-2160p"},
		{"BR-DISK", quality.Movies, "brdisk"},
		{"DVDSCR", quality.Movies, "screener"},
		{"Raw-HD", quality.Series, "rawhd"},
		{"SDTV", quality.Series, "sdtv"},
		{"FLAC 24bit", quality.Music, "flac-24"},
		{"MP3-VBR-V0", quality.Music, "mp3-v0"},
		{"OGG Vorbis Q10", quality.Music, "vorbis"},
		{"AAC-320", quality.Music, "aac"},
		{"Unknown", quality.Music, quality.UnknownMusic},
		{"Unknown", quality.Movies, quality.UnknownVideo},
		{"APE", quality.Music, ""},
		{"FLAC", quality.Movies, ""},
	}
	for _, c := range cases {
		if got := MapQuality(c.name, c.kind); got != c.want {
			t.Errorf("%s/%s: %q, want %q", c.name, c.kind, got, c.want)
		}
	}
}

func TestConvertProfile(t *testing.T) {
	q := func(id int64, name string, allowed bool) ProfileItem {
		return ProfileItem{Quality: &QualityRef{ID: id, Name: name}, Allowed: allowed}
	}
	p := QualityProfile{Name: "HD", UpgradeAllowed: true, Cutoff: 1001, MinFormatScore: 10, CutoffFormatScore: 200,
		Items: []ProfileItem{q(0, "Unknown", false), q(4, "HDTV-720p", true),
			{ID: 1001, Name: "WEB 1080p", Allowed: true, Items: []ProfileItem{q(3, "WEBDL-1080p", true), q(15, "WEBRip-1080p", true)}},
			q(7, "Bluray-1080p", true), q(55, "Bluray-9000p", true)},
		FormatScores: []FormatScore{{FormatID: 1, Score: -1000}, {FormatID: 2, Score: 0}}}
	conv := ConvertProfile(p, quality.Movies, []string{"English", "Klingon"})
	items := conv.Profile.Items
	if len(items) != 4 || items[0].Quality != "bluray-1080p" || items[1].Name != "WEB 1080p" || items[3].Quality != "unknown" {
		t.Fatalf("items %+v", items)
	}
	if conv.Profile.Cutoff != "group:WEB 1080p" || !conv.Profile.UpgradesAllowed || conv.Profile.MinFormatScore != 10 {
		t.Fatalf("profile %+v", conv.Profile)
	}
	if len(conv.FormatScores) != 1 || conv.FormatScores[1] != -1000 {
		t.Fatalf("scores %+v", conv.FormatScores)
	}
	if len(conv.Profile.Languages) != 1 || conv.Profile.Languages[0] != "en" || len(conv.Warnings) != 2 {
		t.Fatalf("languages %v warnings %v", conv.Profile.Languages, conv.Warnings)
	}
	if err := conv.Profile.Validate(); err != nil {
		t.Fatal(err)
	}
	// A cutoff that is not allowed becomes the best allowed item.
	p.Cutoff = 0
	conv = ConvertProfile(p, quality.Movies, nil)
	if conv.Profile.Cutoff != "bluray-1080p" {
		t.Fatalf("cutoff %q", conv.Profile.Cutoff)
	}
}

func TestTranslateNaming(t *testing.T) {
	cases := []struct {
		in, want string
		unknown  int
		tokens   []naming.Token
	}{
		{"{Movie Title} ({Release Year}) {[Quality Full]}{-Release Group}", "{Title} ({Year}) {[QualityFull]}{-ReleaseGroup}", 0, naming.MovieTokens},
		{"{Movie CleanTitle} {(Release Year)}", "{CleanTitle} {(Year)}", 0, naming.MovieTokens},
		{"{Series Title} - S{season:00}E{episode:00} - {Episode Title}", "{SeriesTitle} - S{Season}E{Episode} - {EpisodeTitle}", 0, naming.EpisodeTokens},
		{"Season {season:00}", "Season {Season}", 0, naming.SeasonFolderTokens},
		{"{Series TitleYear}", "{SeriesTitleYear}", 0, naming.SeriesFolderTokens},
		{"{track:00} - {Track Title}", "{TrackNumber} - {TrackTitle}", 0, naming.TrackTokens},
		{"{Movie Title} {Custom Thing}", "{Title} {Custom Thing}", 1, nil},
	}
	for _, c := range cases {
		got, unknown := TranslateNaming(c.in)
		if got != c.want || len(unknown) != c.unknown {
			t.Errorf("%q: %q %v", c.in, got, unknown)
		}
		if c.tokens != nil {
			if err := naming.Validate(got, c.tokens); err != nil {
				t.Errorf("%q: %v", got, err)
			}
		}
	}
	if MapColon("dash") != library.ColonDash || MapColon(float64(0)) != library.ColonDelete || MapMultiEpisodeStyle(float64(5)) != "prefixed_range" {
		t.Fatal("colon / multi-episode style")
	}
}

func TestMapPermissions(t *testing.T) {
	if r, _ := MapPermissions(2); r != auth.RoleAdmin {
		t.Fatal("admin")
	}
	r, p := MapPermissions(32 | 256 | 1024)
	if r != auth.RoleRequester || !p.Has(auth.PermRequestMovies|auth.PermRequestSeries|auth.PermRequestMusic|auth.PermAutoApproveMovies|auth.PermRequest4K) ||
		p.Has(auth.PermAutoApproveSeries) || p.Has(auth.PermManageRequests) {
		t.Fatalf("requester %v %b", r, p)
	}
	r, p = MapPermissions(16 | 262144)
	if r != auth.RoleManager || !p.Has(auth.PermManageRequests|auth.PermRequestMovies) || p.Has(auth.PermRequestSeries) {
		t.Fatalf("manager %v %b", r, p)
	}
}

func TestMapRequestStatus(t *testing.T) {
	cases := map[[2]int]requests.Status{{1, 2}: requests.StatusPending, {2, 5}: requests.StatusAvailable, {2, 4}: requests.StatusPartial,
		{2, 3}: requests.StatusProcessing, {2, 2}: requests.StatusApproved, {3, 1}: requests.StatusDeclined, {4, 1}: requests.StatusFailed,
		{5, 5}: requests.StatusAvailable}
	for in, want := range cases {
		if got := MapRequestStatus(in[0], in[1]); got != want {
			t.Errorf("%v: %s want %s", in, got, want)
		}
	}
}

func TestTranslatorAndSuggestions(t *testing.T) {
	data := t.TempDir()
	data, _ = filepath.EvalSymlinks(data)
	tv := filepath.Join(data, "media", "tv")
	if err := os.MkdirAll(tv, 0o755); err != nil {
		t.Fatal(err)
	}
	tr := Translator{DataRoots: []string{data}, Mappings: []PathMapping{{From: "/tv", To: tv}, {SourceID: 2, From: "/tv/anime", To: "/elsewhere"},
		{From: `D:\Media`, To: filepath.Join(data, "media")}}}
	if got, ok := tr.Translate(1, "/tv/Show/S01E01.mkv"); !ok || got != filepath.Join(tv, "Show", "S01E01.mkv") {
		t.Fatalf("%s %v", got, ok)
	}
	if _, ok := tr.Translate(2, "/tv/anime/x.mkv"); ok {
		t.Fatal("a mapping outside the data roots must be refused")
	}
	if got, ok := tr.Translate(1, `D:\Media\tv\Show`); !ok || got != filepath.Join(tv, "Show") {
		t.Fatalf("windows path: %s %v", got, ok)
	}
	if _, ok := tr.Translate(1, "/tvshows/x"); ok {
		t.Fatal("/tvshows must not match /tv")
	}
	if _, ok := tr.Translate(1, "/tv/../../etc/passwd"); ok {
		t.Fatal("traversal")
	}
	sug := SuggestMappings(1, []string{"/tv", "/movies"}, []string{tv}, []string{data})
	if len(sug) != 1 || sug[0].From != "/tv" || sug[0].To != tv {
		t.Fatalf("suggestions %+v", sug)
	}
}
