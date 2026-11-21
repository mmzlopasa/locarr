package decision

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func profile(kind quality.Kind, name string) quality.Profile {
	for _, p := range quality.DefaultProfiles() {
		if p.Kind == kind && p.Name == name {
			p.ID = 1
			return p
		}
	}
	panic(name)
}

func movieCtx() Context {
	return Context{
		Wanted:      Wanted{Kind: quality.Movies, Title: "Blade Runner 2049", Year: 2017, RuntimeMinutes: 164, OriginalLanguage: "en"},
		Profile:     profile(quality.Movies, "HD-1080p"),
		Definitions: quality.DefaultDefinitions(),
		DelayProfiles: []quality.DelayProfile{func() quality.DelayProfile {
			d := quality.DefaultDelayProfile()
			return d
		}()},
		Mode: ModeSearch,
		Now:  now,
	}
}

func seriesCtx() Context {
	c := movieCtx()
	c.Wanted = Wanted{Kind: quality.Series, Title: "The Expanse", RuntimeMinutes: 45, OriginalLanguage: "en",
		Episodes: []Episode{{Season: 3, Episode: 5, Absolute: 31}, {Season: 3, Episode: 6, Absolute: 32}}}
	c.Profile = profile(quality.Series, "HD-1080p")
	return c
}

func cand(title string, size float64) Candidate {
	return Candidate{GUID: title, Title: title, Size: int64(size * (1 << 30)), Protocol: Torrent, IndexerID: 1, IndexerName: "Tracker",
		PublishDate: now.Add(-48 * time.Hour), Seeders: intp(50)}
}

func intp(n int) *int { return &n }

func codes(d Decision) []string {
	out := []string{}
	for _, r := range d.Rejections {
		out = append(out, r.Code)
	}
	return out
}

type blocked map[string]string

func (b blocked) Blocked(_ context.Context, c *Candidate, _ *parser.Release) (string, bool) {
	r, ok := b[c.Title]
	return r, ok
}

const good = "Blade.Runner.2049.2017.1080p.BluRay.x264-SPARKS"

func TestSpecs(t *testing.T) {
	x265 := customformats.Format{ID: 9, Name: "x265", Specs: []customformats.Spec{{Implementation: customformats.ReleaseTitle,
		Fields: map[string]any{"value": `x265|hevc`}}}}
	cx265, err := customformats.Compile(x265)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mut    func(c *Context, k *Candidate)
		title  string
		size   float64
		want   []string
		status string
	}{
		{"accepted", nil, good, 12, []string{}, StatusAccepted},
		{"obfuscated", nil, "a8f9e3b2c1d4e5f6a7b8c9d0e1f2a3b4", 12, []string{CodeParseFailed, CodeWrongTitle, CodeQualityNotAllowed}, StatusRejected},
		{"empty name", nil, "", 0, []string{CodeParseFailed, CodeWrongTitle, CodeQualityNotAllowed}, StatusRejected},
		{"episode for a movie", nil, "Blade.Runner.2049.S01E01.1080p.WEB-DL.x264-GRP", 2, []string{CodeWrongKind, CodeWrongTitle, CodeWrongYear}, StatusRejected},
		{"music for a movie", nil, "Blade Runner 2049 - Soundtrack (2017) [FLAC]", 0.5, []string{CodeWrongKind, CodeWrongTitle, CodeQualityNotAllowed}, StatusRejected},
		{"wrong title", nil, "Blade.Runner.1982.1080p.BluRay.x264-GRP", 10, []string{CodeWrongTitle, CodeWrongYear}, StatusRejected},
		{"alternate title", func(c *Context, _ *Candidate) { c.Wanted.AlternateTitles = []string{"Blade Runner Two"} },
			"Blade.Runner.Two.2017.1080p.BluRay.x264-GRP", 12, []string{}, StatusAccepted},
		{"ids win over the name", func(c *Context, k *Candidate) { c.Wanted.IDs.TMDb = 335984; k.IDs.TMDb = 335984 },
			"BR2049.2017.1080p.BluRay.x264-GRP", 12, []string{}, StatusAccepted},
		{"ids contradict", func(c *Context, k *Candidate) { c.Wanted.IDs.IMDb = "tt1856101"; k.IDs.IMDb = "tt0083658" },
			good, 12, []string{CodeWrongIDs}, StatusRejected},
		{"year one off is fine", nil, "Blade.Runner.2049.2018.1080p.BluRay.x264-GRP", 12, []string{}, StatusAccepted},
		{"quality not allowed", nil, "Blade.Runner.2049.2017.720p.BluRay.x264-GRP", 6, []string{CodeQualityNotAllowed}, StatusRejected},
		{"cam", nil, "Blade.Runner.2049.2017.HDCAM.x264-GRP", 1, []string{CodeQualityNotAllowed}, StatusRejected},
		{"too large", nil, good, 40, []string{CodeSizeTooLarge}, StatusRejected},
		{"too small", nil, good, 0.3, []string{CodeSizeTooSmall}, StatusRejected},
		{"unknown size passes", nil, good, 0, []string{}, StatusAccepted},
		{"upgrades disabled", func(c *Context, _ *Candidate) {
			c.Profile.UpgradesAllowed = false
			c.Wanted.Current = &Current{Quality: "hdtv-1080p"}
		}, good, 12, []string{CodeUpgradesDisabled}, StatusRejected},
		{"cutoff met", func(c *Context, _ *Candidate) {
			c.Wanted.Current = &Current{Quality: "webdl-1080p", Revision: parser.Revision{Version: 1}}
		},
			good, 12, []string{CodeCutoffMet}, StatusRejected},
		{"repack of the same quality after cutoff", func(c *Context, _ *Candidate) {
			c.Wanted.Current = &Current{Quality: "bluray-1080p", Revision: parser.Revision{Version: 1}}
		}, "Blade.Runner.2049.2017.REPACK.1080p.BluRay.x264-SPARKS", 12, []string{}, StatusAccepted},
		{"not an upgrade (worse)", func(c *Context, _ *Candidate) {
			c.Profile.Cutoff = "bluray-1080p"
			c.Wanted.Current = &Current{Quality: "webdl-1080p", Revision: parser.Revision{Version: 1}}
		}, "Blade.Runner.2049.2017.1080p.HDTV.x264-GRP", 8, []string{CodeNotAnUpgrade}, StatusRejected},
		{"not an upgrade (same)", func(c *Context, _ *Candidate) {
			c.Profile.Cutoff = "bluray-1080p"
			c.Wanted.Current = &Current{Quality: "webdl-1080p", Revision: parser.Revision{Version: 1}}
		}, "Blade.Runner.2049.2017.1080p.WEB-DL.x264-GRP", 8, []string{CodeNotAnUpgrade}, StatusRejected},
		{"upgrade by quality", func(c *Context, _ *Candidate) {
			c.Profile.Cutoff = "bluray-1080p"
			c.Wanted.Current = &Current{Quality: "webdl-1080p", Revision: parser.Revision{Version: 1}}
		}, good, 12, []string{}, StatusAccepted},
		{"upgrade by score", func(c *Context, _ *Candidate) {
			c.Profile.Cutoff = "bluray-1080p"
			c.Profile.FormatScores = []quality.FormatScore{{FormatID: 9, Score: 50}}
			c.Formats = []*customformats.Compiled{cx265}
			c.Wanted.Current = &Current{Quality: "webdl-1080p", Revision: parser.Revision{Version: 1}, Score: 0}
		}, "Blade.Runner.2049.2017.1080p.WEB-DL.x265-GRP", 6, []string{}, StatusAccepted},
		{"score too low", func(c *Context, _ *Candidate) {
			c.Profile.MinFormatScore = 10
		}, good, 12, []string{CodeScoreTooLow}, StatusRejected},
		{"negative score", func(c *Context, _ *Candidate) {
			c.Profile.FormatScores = []quality.FormatScore{{FormatID: 9, Score: -10000}}
			c.Formats = []*customformats.Compiled{cx265}
		}, "Blade.Runner.2049.2017.1080p.BluRay.x265-GRP", 10, []string{CodeScoreTooLow}, StatusRejected},
		{"language", func(c *Context, _ *Candidate) { c.Profile.Languages = []string{"original"} },
			"Blade.Runner.2049.2017.GERMAN.1080p.BluRay.x264-GRP", 12, []string{CodeLanguage}, StatusRejected},
		{"language multi passes original", func(c *Context, _ *Candidate) { c.Profile.Languages = []string{"original"} },
			"Blade.Runner.2049.2017.MULTi.1080p.BluRay.x264-GRP", 12, []string{}, StatusAccepted},
		{"language croatian wanted", func(c *Context, _ *Candidate) { c.Profile.Languages = []string{"hr"} },
			good, 12, []string{CodeLanguage}, StatusRejected},
		{"must contain", func(c *Context, _ *Candidate) {
			c.ReleaseProfiles = []quality.ReleaseProfile{{Name: "Groups", Enabled: true, Required: []string{"/-(FGT|NTb)$/"}}}
		}, good, 12, []string{CodeMustContain}, StatusRejected},
		{"must not contain", func(c *Context, _ *Candidate) {
			c.ReleaseProfiles = []quality.ReleaseProfile{{Name: "No SPARKS", Enabled: true, Ignored: []string{"sparks"}}}
		}, good, 12, []string{CodeMustNotContain}, StatusRejected},
		{"release profile for other tag", func(c *Context, _ *Candidate) {
			c.ReleaseProfiles = []quality.ReleaseProfile{{Name: "Anime only", Enabled: true, Ignored: []string{"sparks"}, Tags: []string{"anime"}}}
		}, good, 12, []string{}, StatusAccepted},
		{"blocklisted", func(c *Context, _ *Candidate) { c.Blocklist = blocked{good: "failed download"} }, good, 12, []string{CodeBlocklisted}, StatusRejected},
		{"seeders", func(c *Context, k *Candidate) { c.MinimumSeeders = 5; k.Seeders = intp(2) }, good, 12, []string{CodeSeeders}, StatusRejected},
		{"indexer minimum seeders", func(c *Context, k *Candidate) { k.MinimumSeeders = 100 }, good, 12, []string{CodeSeeders}, StatusRejected},
		{"retention", func(c *Context, k *Candidate) {
			k.Protocol = Usenet
			c.RetentionDays = 1
		}, good, 12, []string{CodeTooOld}, StatusRejected},
		{"minimum age", func(c *Context, k *Candidate) {
			k.Protocol = Usenet
			k.PublishDate = now.Add(-10 * time.Minute)
			c.MinimumAgeMinutes = 30
		}, good, 12, []string{CodeTooNew}, StatusPending},
		{"sample", nil, "Blade.Runner.2049.2017.1080p.BluRay.x264-SPARKS-sample", 0.1, []string{CodeSizeTooSmall, CodeSample}, StatusRejected},
		{"hardcoded subs", nil, "Blade.Runner.2049.2017.1080p.WEB-DL.HC.x264-GRP", 8, []string{CodeHardcodedSubs}, StatusRejected},
		{"hardcoded allowed", func(c *Context, _ *Candidate) { c.AllowHardcodedSubs = true },
			"Blade.Runner.2049.2017.1080p.WEB-DL.HC.x264-GRP", 8, []string{}, StatusAccepted},
		{"3d", nil, "Blade.Runner.2049.2017.3D.HSBS.1080p.BluRay.x264-GRP", 12, []string{CodeThreeD}, StatusRejected},
		{"indexer disabled", func(_ *Context, k *Candidate) { k.IndexerDisabled = true }, good, 12, []string{CodeIndexerDisabled}, StatusRejected},
		{"indexer not for rss", func(c *Context, k *Candidate) { c.Mode = ModeRSS; k.IndexerRSSDisabled = true }, good, 12, []string{CodeIndexerDisabled}, StatusRejected},
		{"indexer limited", func(_ *Context, k *Candidate) { k.IndexerLimitedUntil = now.Add(time.Hour) }, good, 12, []string{CodeIndexerLimited}, StatusPending},
		{"protocol disabled", func(c *Context, _ *Candidate) { c.DelayProfiles[0].TorrentEnabled = false }, good, 12, []string{CodeProtocolDisabled}, StatusRejected},
		{"delayed", func(c *Context, k *Candidate) {
			c.DelayProfiles[0].TorrentDelay = 120
			k.PublishDate = now.Add(-30 * time.Minute)
		}, good, 12, []string{CodeDelayed}, StatusPending},
		{"delay bypassed for best quality", func(c *Context, k *Candidate) {
			c.DelayProfiles[0].TorrentDelay = 120
			c.DelayProfiles[0].BypassIfHighestQuality = true
			k.PublishDate = now.Add(-30 * time.Minute)
		}, good, 12, []string{}, StatusAccepted},
		{"delay bypassed by score", func(c *Context, k *Candidate) {
			c.DelayProfiles[0].TorrentDelay = 120
			c.DelayProfiles[0].BypassIfAboveScore = true
			c.DelayProfiles[0].MinimumScore = 0
			k.PublishDate = now.Add(-30 * time.Minute)
		}, good, 12, []string{}, StatusAccepted},
		{"no delay for interactive", func(c *Context, k *Candidate) {
			c.DelayProfiles[0].TorrentDelay = 120
			c.Mode = ModeInteractive
			k.PublishDate = now.Add(-30 * time.Minute)
		}, good, 12, []string{}, StatusAccepted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := movieCtx()
			k := cand(tc.title, tc.size)
			if tc.mut != nil {
				tc.mut(&ctx, &k)
			}
			d := EvaluateOne(context.Background(), &ctx, k)
			if got := codes(d); !reflect.DeepEqual(got, tc.want) || d.Status != tc.status {
				t.Fatalf("got %v %s, want %v %s\n%v", got, d.Status, tc.want, tc.status, d.Rejections)
			}
			for _, r := range d.Rejections {
				if r.Message == "" {
					t.Errorf("%s has no message", r.Code)
				}
			}
		})
	}
}

func TestSizeMessage(t *testing.T) {
	ctx := movieCtx()
	ctx.Wanted.RuntimeMinutes = 110
	ctx.Definitions = []quality.Definition{{Kind: quality.Movies, Quality: "bluray-1080p", Min: 1, Max: 12 * 1024 / 110.0}}
	d := EvaluateOne(context.Background(), &ctx, cand(good, 18.4))
	if len(d.Rejections) != 1 || d.Rejections[0].String() != "size_too_large: 18.4 GB > 12 GB max for Bluray-1080p (110 min)" {
		t.Fatalf("%v", d.Rejections)
	}
	ctx.Wanted.RuntimeMinutes = 0
	d = EvaluateOne(context.Background(), &ctx, cand(good, 18.4))
	if !strings.Contains(d.Rejections[0].Message, "110 min, assumed") {
		t.Fatalf("%v", d.Rejections)
	}
}

func TestEpisodes(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(c *Context)
		title string
		want  []string
	}{
		{"wanted episode", nil, "The.Expanse.S03E05.1080p.WEB-DL.DDP5.1.H.264-GRP", []string{}},
		{"multi episode with one wanted", nil, "The.Expanse.S03E04E05.1080p.WEB-DL.DDP5.1.H.264-GRP", []string{}},
		{"wrong episode", nil, "The.Expanse.S03E09.1080p.WEB-DL.DDP5.1.H.264-GRP", []string{CodeWrongEpisode}},
		{"wrong season", nil, "The.Expanse.S02E05.1080p.WEB-DL.DDP5.1.H.264-GRP", []string{CodeWrongSeason}},
		{"season pack", nil, "The.Expanse.S03.1080p.BluRay.x264-GRP", []string{}},
		{"multi season pack", nil, "The.Expanse.S01-S04.1080p.BluRay.x264-GRP", []string{}},
		{"complete series", nil, "The.Expanse.Complete.Series.1080p.BluRay.x264-GRP", []string{}},
		{"no episode info", nil, "The.Expanse.1080p.WEB-DL.x264-GRP", []string{CodeWrongEpisode}},
		{"anime absolute", func(c *Context) { c.Wanted.SeriesType = SeriesAnime },
			"[SubsPlease] The Expanse - 31 (1080p) [ABCD1234].mkv", []string{}},
		{"anime wrong absolute", func(c *Context) { c.Wanted.SeriesType = SeriesAnime },
			"[SubsPlease] The Expanse - 40 (1080p) [ABCD1234].mkv", []string{CodeWrongEpisode}},
		{"daily", func(c *Context) {
			c.Wanted.SeriesType = SeriesDaily
			c.Wanted.Episodes = []Episode{{Season: 2024, Episode: 50, AirDate: "2024-03-15"}}
		}, "The.Expanse.2024.03.15.1080p.WEB.h264-GRP", []string{}},
		{"daily wrong date", func(c *Context) {
			c.Wanted.SeriesType = SeriesDaily
			c.Wanted.Episodes = []Episode{{Season: 2024, Episode: 50, AirDate: "2024-03-15"}}
		}, "The.Expanse.2024.03.16.1080p.WEB.h264-GRP", []string{CodeWrongEpisode}},
		{"movie name for a series", nil, "The.Expanse.2015.1080p.BluRay.x264-GRP", []string{CodeWrongEpisode}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := seriesCtx()
			if tc.mut != nil {
				tc.mut(&ctx)
			}
			k := cand(tc.title, 0)
			d := EvaluateOne(context.Background(), &ctx, k)
			if got := codes(d); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v: %v", got, tc.want, d.Rejections)
			}
		})
	}
	// season pack size counts the wanted episodes of that season
	ctx := seriesCtx()
	ctx.Definitions = []quality.Definition{{Kind: quality.Series, Quality: "bluray-1080p", Max: 10}}
	d := EvaluateOne(context.Background(), &ctx, cand("The.Expanse.S03.1080p.BluRay.x264-GRP", 0.8))
	if len(d.Rejections) != 0 {
		t.Fatalf("pack of 2×45 min at 0.8 GB: %v", d.Rejections)
	}
	d = EvaluateOne(context.Background(), &ctx, cand("The.Expanse.S03E05.1080p.BluRay.x264-GRP", 0.8))
	if codes(d)[0] != CodeSizeTooLarge {
		t.Fatalf("one episode at 0.8 GB: %v", d.Rejections)
	}
}

func TestMusic(t *testing.T) {
	ctx := movieCtx()
	ctx.Wanted = Wanted{Kind: quality.Music, Artist: "Pink Floyd", Album: "The Dark Side of the Moon", Year: 1973, RuntimeMinutes: 43}
	ctx.Profile = profile(quality.Music, "Lossless")
	cases := map[string][]string{
		"Pink Floyd - The Dark Side of the Moon (1973) [FLAC]":      {},
		"Pink_Floyd-The_Dark_Side_of_the_Moon-CD-FLAC-1973-GRP":     {},
		"Pink Floyd - The Dark Side of the Moon (1973) [MP3 320]":   {CodeQualityNotAllowed},
		"Pink Floyd - Wish You Were Here (1975) [FLAC]":             {CodeWrongTitle, CodeWrongYear},
		"Roger Waters - The Dark Side of the Moon (2023) [FLAC]":    {CodeWrongTitle, CodeWrongYear},
		"The.Dark.Side.of.the.Moon.1973.1080p.BluRay.x264-GRP":      {CodeWrongKind, CodeQualityNotAllowed},
		"Various Artists - The Dark Side of the Moon (1973) [FLAC]": {},
	}
	for name, want := range cases {
		d := EvaluateOne(context.Background(), &ctx, cand(name, 0))
		if got := codes(d); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v want %v: %v", name, got, want, d.Rejections)
		}
	}
}

func TestRanking(t *testing.T) {
	ctx := movieCtx()
	ctx.Profile = profile(quality.Movies, "HD-720p/1080p")
	ctx.DelayProfiles[0].PreferredProtocol = Usenet
	x265, _ := customformats.Compile(customformats.Format{ID: 1, Name: "x265", Specs: []customformats.Spec{{Implementation: customformats.ReleaseTitle,
		Fields: map[string]any{"value": "x265"}}}})
	ctx.Formats = []*customformats.Compiled{x265}
	ctx.Profile.FormatScores = []quality.FormatScore{{FormatID: 1, Score: 10}}

	mk := func(title string, size float64, f func(c *Candidate)) Candidate {
		c := cand(title, size)
		if f != nil {
			f(&c)
		}
		return c
	}
	list := []Candidate{
		mk("Blade.Runner.2049.2017.720p.BluRay.x264-A", 6, nil),                                               // lower quality
		mk("Blade.Runner.2049.2017.1080p.BluRay.x264-B", 12, nil),                                             // base
		mk("Blade.Runner.2049.2017.1080p.BluRay.x265-C", 12, nil),                                             // + score
		mk("Blade.Runner.2049.2017.1080p.BluRay.x264-D", 12, func(c *Candidate) { c.Protocol = Usenet }),      // + protocol
		mk("Blade.Runner.2049.2017.1080p.BluRay.x264-E", 12, func(c *Candidate) { c.IndexerPriority = 1 }),    // + priority
		mk("Blade.Runner.2049.2017.1080p.BluRay.x264-F", 12, func(c *Candidate) { c.Seeders = intp(500) }),    // + seeders
		mk("Blade.Runner.2049.2017.REPACK.1080p.BluRay.x264-G", 12, nil),                                      // + revision
		mk("Blade.Runner.2049.2017.1080p.BluRay.x264-H", 5.6, nil),                                            // closer to preferred size
		mk("Blade.Runner.2049.2017.HDCAM.x264-I", 2, nil),                                                     // rejected
		mk("Blade.Runner.2049.2017.1080p.BluRay.x264-J", 12, func(c *Candidate) { c.IndexerDisabled = true }), // rejected
	}
	rep := Evaluate(context.Background(), ctx, list)
	var order []string
	for _, d := range rep.Decisions {
		order = append(order, d.Candidate.Title[strings.LastIndex(d.Candidate.Title, "-")+1:])
	}
	want := []string{"C", "G", "D", "E", "F", "H", "B", "A", "I", "J"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	if rep.Accepted != 8 || rep.Rejected != 2 || rep.BestGUID != list[2].GUID || rep.Decisions[0].Rank != 1 || rep.Decisions[7].Rank != 8 {
		t.Fatalf("report %+v", rep)
	}
	// Deterministic: shuffled input gives the same order.
	rev := make([]Candidate, len(list))
	for i := range list {
		rev[i] = list[len(list)-1-i]
	}
	rep2 := Evaluate(context.Background(), ctx, rev)
	for i := range rep.Decisions[:8] {
		if rep.Decisions[i].Candidate.GUID != rep2.Decisions[i].Candidate.GUID {
			t.Fatalf("not deterministic at %d", i)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	cases := map[string]string{"Amélie": "amelie", "Fast & Furious": "fastandfurious", "S.W.A.T.": "swat",
		"Čovjek koji je volio brodove": "covjekkojijeviliobrodove", "Léon: The Professional": "leontheprofessional"}
	cases["Čovjek koji je volio brodove"] = "covjekkojijevoliobrodove"
	for in, want := range cases {
		if got := CleanTitle(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
	if !TitleMatches("Office", []string{"The Office"}) && !TitleMatches("The Office", []string{"Office"}) {
		t.Error("article")
	}
}

// TestThousand checks the target: 1000 releases decided in well under a second.
func TestThousand(t *testing.T) {
	ctx := movieCtx()
	var list []Candidate
	for i := 0; i < 1000; i++ {
		list = append(list, cand(fmt.Sprintf("Blade.Runner.2049.2017.%s.BluRay.x264-G%d", []string{"720p", "1080p", "2160p"}[i%3], i), float64(i%40)))
	}
	start := time.Now()
	rep := Evaluate(context.Background(), ctx, list)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("1000 candidates took %v", d)
	}
	if len(rep.Decisions) != 1000 {
		t.Fatal(len(rep.Decisions))
	}
}
