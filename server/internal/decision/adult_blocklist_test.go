package decision

import (
	"context"
	"slices"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// TestBlockedTermWholeWords: terms match whole words in any case and with any
// separator, phrases also written together, never inside a longer word.
func TestBlockedTermWholeWords(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"StudioA.24.01.15.Child.Title.1080p", true},
		{"studioa_24_01_15_CHILD_title", true},
		{"StudioA - Title - Children", true},
		{"StudioA.24.01.15.Under.Age.Title", true},
		{"StudioA.24.01.15.UnderAge.Title", true},
		{"StudioA.24.01.15.Under-Age.Title", true},
		{"StudioA.24.01.15.Pre_Teen.Title", true},
		{"StudioA.24.01.15.Childs.Title", false},
		{"StudioA.24.01.15.Lolipop.Title", false},
		{"StudioA.24.01.15.Childhood.Memories", false},
		{"StudioA.24.01.15.XPreteenX.Title", false},
		{"StudioA.24.01.15.Wonder.Agent.Title", false},
	}
	for _, c := range cases {
		if _, got := BlockedTerm(c.text, ProtectedAdultTerms); got != c.want {
			t.Errorf("%q: blocked %v, want %v", c.text, got, c.want)
		}
	}
}

// TestProtectedTermsAlwaysBlock: with an empty owner list the protected terms
// still block adult releases, the rejection names the term and its list, and
// a stage name listed for the scene does not count.
func TestProtectedTermsAlwaysBlock(t *testing.T) {
	ctx := adultCtx()
	ctx.BlockedTerms = nil
	ctx.Wanted.Adult.Performers = []string{"Lolita Cherry"}
	rej := func(title string) *Rejection {
		d := EvaluateOne(context.Background(), &ctx, cand(title, 1.5))
		for i := range d.Rejections {
			if d.Rejections[i].Code == CodeBlockedTag {
				return &d.Rejections[i]
			}
		}
		return nil
	}
	r := rej("StudioA.24.01.15.Child.Scene.Title.1080p.MP4-GRP")
	if r == nil || r.Params["term"] != "child" || r.Params["protected"] != true {
		t.Fatalf("protected term: %+v", r)
	}
	if r := rej("StudioA.24.01.15.Lolita.Cherry.Scene.Title.1080p.MP4-GRP"); r != nil {
		t.Errorf("a listed stage name blocked the release: %+v", r)
	}
	if r := rej("StudioA.24.01.15.Lolita.Scene.Title.1080p.MP4-GRP"); r == nil {
		t.Error("the term outside the stage name must block")
	}
	ctx.BlockedTerms = []string{"ownterm"}
	if r := rej("StudioA.24.01.15.Ownterm.Scene.Title.1080p.MP4-GRP"); r == nil || r.Params["protected"] != false || r.Params["term"] != "ownterm" {
		t.Errorf("own term: %+v", r)
	}
	// Ordinary movies are not adult content: the protected set does not apply.
	m := movieCtx()
	m.Wanted.Title = "Child Title"
	d := EvaluateOne(context.Background(), &m, cand("Child.Title.2017.1080p.WEB-DL.x264-GRP", 4))
	if slices.Contains(codes(d), CodeBlockedTag) {
		t.Errorf("movie blocked: %v", codes(d))
	}
}

// TestAdultQualityMessage: a scene name without a resolution is Unknown; a
// 1080p profile refuses it and says which quality and which profile, the
// open profile accepts it (D-109).
func TestAdultQualityMessage(t *testing.T) {
	ctx := adultCtx()
	title := "StudioA.24.01.15.Performer.One.Scene.Title.XXX.MP4-GRP"
	d := EvaluateOne(context.Background(), &ctx, cand(title, 0.5))
	var r *Rejection
	for i := range d.Rejections {
		if d.Rejections[i].Code == CodeQualityNotAllowed {
			r = &d.Rejections[i]
		}
	}
	if r == nil || r.Message != "Unknown is not allowed in profile HD-1080p" || r.Params["quality"] != "Unknown" || r.Params["profile"] != "HD-1080p" {
		t.Fatalf("rejection %+v", r)
	}
	ctx.Profile = quality.OpenProfile()
	d = EvaluateOne(context.Background(), &ctx, cand(title, 0.5))
	if d.Status != StatusAccepted && !onlyTemporary(d) {
		t.Errorf("open profile: %v", codes(d))
	}
}
