package decision

import (
	"context"
	"slices"
	"testing"
)

func adultCtx() Context {
	c := movieCtx()
	c.Wanted = Wanted{Kind: c.Wanted.Kind, Title: "Scene Title", RuntimeMinutes: 30,
		Adult: &Adult{Scene: true, Studios: []string{"Studio A"}, Date: "2024-01-15", Title: "Scene Title"}}
	c.BlockedTerms = []string{"forbiddenterm", "two words"}
	return c
}

func TestAdultSceneDecision(t *testing.T) {
	ctx := adultCtx()
	cases := []struct {
		title string
		code  string // "" = accepted
	}{
		{"StudioA.24.01.15.Performer.One.Scene.Title.1080p.MP4-GRP", ""},
		{"Studio.A.24.01.15.Performer.One.Scene.Title.1080p.WEB-DL-GRP", ""},
		{"StudioB.24.01.15.Performer.One.Scene.Title.1080p.MP4-GRP", CodeWrongScene},
		{"StudioA.24.01.16.Performer.One.Scene.Title.1080p.MP4-GRP", CodeWrongScene},
		{"StudioA.24.01.15.Forbiddenterm.Scene.Title.1080p.MP4-GRP", CodeBlockedTag},
		{"StudioA.24.01.15.Two.Words.Scene.Title.1080p.MP4-GRP", CodeBlockedTag},
		{"StudioA.24.01.15.TwoWords.Scene.Title.1080p.MP4-GRP", CodeBlockedTag},
		{"Performer One Scene Title 1080p", ""},
		{"Performer One Other Title 1080p", CodeWrongScene},
	}
	for _, c := range cases {
		d := EvaluateOne(context.Background(), &ctx, cand(c.title, 1.5))
		got := codes(d)
		if c.code == "" {
			if d.Status != StatusAccepted && !onlyTemporary(d) {
				t.Errorf("%s: rejected %v", c.title, got)
			}
			continue
		}
		if !slices.Contains(got, c.code) {
			t.Errorf("%s: codes %v, want %s", c.title, got, c.code)
		}
	}
}

func onlyTemporary(d Decision) bool {
	for _, r := range d.Rejections {
		if !r.Temporary {
			return false
		}
	}
	return true
}

func TestBlockedTermWords(t *testing.T) {
	terms := []string{"abc"}
	if _, ok := BlockedTerm("xabcx.1080p", terms); ok {
		t.Error("a term inside another word must not match")
	}
	if _, ok := BlockedTerm("Studio.ABC.Title", terms); !ok {
		t.Error("a whole word must match regardless of case")
	}
}

func TestAdultMovieDecision(t *testing.T) {
	ctx := movieCtx()
	ctx.Wanted.Adult = &Adult{Title: "Movie Title", Year: 2023}
	d := EvaluateOne(context.Background(), &ctx, cand("Movie.Title.2023.1080p.WEB-DL.x264-GRP", 4))
	if slices.Contains(codes(d), CodeWrongScene) || slices.Contains(codes(d), CodeWrongYear) {
		t.Errorf("rejected: %v", codes(d))
	}
	d = EvaluateOne(context.Background(), &ctx, cand("Movie.Title.2015.1080p.WEB-DL.x264-GRP", 4))
	if !slices.Contains(codes(d), CodeWrongYear) {
		t.Errorf("want wrong_year: %v", codes(d))
	}
}
