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

// TestAdultSceneNamingForms checks the ways general public sites name one
// scene (synthetic, non-explicit names): studio and date in any order and
// format, or no date and the title with a performer or the studio, and that
// another scene is still rejected.
func TestAdultSceneNamingForms(t *testing.T) {
	ctx := movieCtx()
	ctx.Wanted = Wanted{Kind: ctx.Wanted.Kind, Title: "Outdoor Picnic Scene with Performer Seven", RuntimeMinutes: 30,
		Adult: &Adult{Scene: true, Studios: []string{"Studio H"}, Date: "2020-08-19", Title: "Outdoor Picnic Scene with Performer Seven",
			Performers: []string{"Performer Seven"}}}
	cases := []struct {
		title string
		code  string // "" = accepted
	}{
		{"StudioH.20.08.19.Performer.Seven.Outdoor.Picnic.Scene.XXX.1080p.MP4-GRP", ""},
		{"Studio H 20 08 19 Performer Seven 720p", ""},
		{"[StudioH] Performer Seven - Outdoor Picnic Scene (19.08.2020)", ""},
		{"[StudioH] Performer Seven - Outdoor Picnic Scene (2020)", ""},
		{"STUDIOH - OUTDOOR PICNIC SCENE WITH PERFORMER SEVEN SD", ""},
		{"Performer Seven - Outdoor Picnic Scene - Studio H 1080p", ""},
		{"Outdoor Picnic Scene with Performer Seven 1080p", ""},
		{"StudioH.20.08.20.Performer.Seven.Outdoor.Picnic.Scene.1080p", CodeWrongScene},
		{"StudioX.20.08.19.Performer.Seven.Outdoor.Picnic.Scene.1080p", CodeWrongScene},
		{"[StudioH] Performer Seven - Outdoor Picnic Scene (16.08.2016)", CodeWrongScene},
		{"[StudioH] Performer Seven - Outdoor Picnic Scene (2016)", CodeWrongYear},
		// The same studio and day, another scene (other performer, other title).
		{"StudioH.20.08.19.Performer.Nine.Rainy.Garden.Visit.XXX.1080p.MP4-GRP", CodeWrongScene},
		// The name cut short still names the performer or most of the title.
		{"StudioH 20 08 19 Performer Seven Outdoor Pic", ""},
		{"StudioH.20.08.19.Outdoor.Picnic.XXX.720p", ""},
		{"StudioH.20.08.19.1080p.MP4-GRP", ""},
		{"[StudioH] Performer Eight - Indoor Picnic Scene (2020)", CodeWrongScene},
		{"Performer Seven - Studio H", CodeWrongScene},
		{"Outdoor Picnic Scene 1080p", CodeWrongScene},
	}
	for _, c := range cases {
		d := EvaluateOne(context.Background(), &ctx, cand(c.title, 1.0))
		got := codes(d)
		if c.code == "" {
			if slices.Contains(got, CodeWrongScene) || slices.Contains(got, CodeWrongYear) {
				t.Errorf("%s: rejected %v", c.title, got)
			}
			continue
		}
		if !slices.Contains(got, c.code) {
			t.Errorf("%s: codes %v, want %s", c.title, got, c.code)
		}
	}
	// A one-word title alone is not enough.
	ctx.Wanted.Adult.Title, ctx.Wanted.Adult.Performers = "Picnic", nil
	if d := EvaluateOne(context.Background(), &ctx, cand("Picnic 1080p", 1.0)); !slices.Contains(codes(d), CodeWrongScene) {
		t.Errorf("one-word title accepted: %v", codes(d))
	}
}
