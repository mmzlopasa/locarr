package decision

import (
	"context"
	"slices"
	"testing"
)

func galleryCtx() Context {
	c := movieCtx()
	c.Wanted = Wanted{Kind: c.Wanted.Kind, Title: "Garden Set", RuntimeMinutes: 30,
		Adult: &Adult{Gallery: true, Studios: []string{"Studio A"}, Date: "2024-01-15", Title: "Garden Set", Performers: []string{"Performer One"},
			MinSize: 1 << 20, MaxSize: 2 << 30}}
	c.BlockedTerms = []string{"forbiddenterm"}
	return c
}

// TestGalleryDecision: an image set is matched by studio, date, title and
// performers like a scene, must be an image set (category or name), has no
// video quality and only a size range (synthetic names).
func TestGalleryDecision(t *testing.T) {
	ctx := galleryCtx()
	cases := []struct {
		title string
		gb    float64
		cats  []int
		code  string // "" = accepted
	}{
		{"StudioA.24.01.15.Performer.One.Garden.Set.XXX.IMAGESET-GRP", 0.2, nil, ""},
		{"StudioA.com_24.01.15.Performer.One.Garden.Set.XXX.iMAGESET-GRP[XC]", 0.2, nil, ""},
		{"StudioA.24.01.15.Performer.One.Garden.Set.XXX-GRP", 0.2, []int{6060}, ""},
		{"[Studio A] Performer One - Garden Set (2024-01-15) [85 pics]", 0.1, nil, ""},
		{"Performer One - Garden Set - Photoset", 0.1, nil, ""},
		{"StudioA.24.01.15.Performer.One.Garden.Set.XXX.1080p.MP4-GRP", 1.5, nil, CodeNotImageSet},
		{"StudioA.24.01.15.Performer.One.Garden.Set.XXX-GRP", 0.2, []int{6000}, CodeNotImageSet},
		{"StudioB.24.01.15.Performer.One.Garden.Set.IMAGESET-GRP", 0.2, nil, CodeWrongScene},
		{"StudioA.24.01.16.Performer.One.Garden.Set.IMAGESET-GRP", 0.2, nil, CodeWrongScene},
		{"StudioA.24.01.15.Forbiddenterm.Garden.Set.IMAGESET-GRP", 0.2, nil, CodeBlockedTag},
		{"StudioA.24.01.15.Underage.Garden.Set.IMAGESET-GRP", 0.2, nil, CodeBlockedTag},
		{"StudioA.24.01.15.Performer.One.Garden.Set.IMAGESET-GRP", 0.0001, nil, CodeSizeTooSmall},
		{"StudioA.24.01.15.Performer.One.Garden.Set.IMAGESET-GRP", 3, nil, CodeSizeTooLarge},
	}
	for _, c := range cases {
		cd := cand(c.title, c.gb)
		cd.Categories = c.cats
		d := EvaluateOne(context.Background(), &ctx, cd)
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

// TestGalleryRanksByImageCount: without video quality, more images win.
func TestGalleryRanksByImageCount(t *testing.T) {
	ctx := galleryCtx()
	rep := Evaluate(context.Background(), ctx, []Candidate{
		cand("StudioA.24.01.15.Performer.One.Garden.Set.x60.IMAGESET-GRP", 0.1),
		cand("StudioA.24.01.15.Performer.One.Garden.Set.x150.IMAGESET-GRP", 0.1),
	})
	if b := rep.Best(); b == nil || b.Candidate.Title != "StudioA.24.01.15.Performer.One.Garden.Set.x150.IMAGESET-GRP" {
		t.Fatalf("best %+v", rep.Decisions)
	}
}

// TestPerformerGallery: a set with neither studio nor date is judged by its
// title (a performer's name) and needs image words.
func TestPerformerGallery(t *testing.T) {
	ctx := galleryCtx()
	ctx.Wanted.Adult = &Adult{Gallery: true, Title: "Performer One", Performers: []string{"Performer One"}}
	for title, ok := range map[string]bool{
		"Performer One - Photoset 120 pics":   true,
		"Performer Two - Photoset 120 pics":   false,
		"Performer One - Beach Day 1080p.mp4": false,
	} {
		d := EvaluateOne(context.Background(), &ctx, cand(title, 0.1))
		if got := d.Status == StatusAccepted || onlyTemporary(d); got != ok {
			t.Errorf("%s: accepted %v, want %v (%v)", title, got, ok, codes(d))
		}
	}
}

// TestSceneRefusesImageSet: a scene search never takes the scene's photo set.
func TestSceneRefusesImageSet(t *testing.T) {
	ctx := adultCtx()
	for title, cats := range map[string][]int{
		"StudioA.24.01.15.Performer.One.Scene.Title.XXX.IMAGESET-GRP": nil,
		"StudioA.24.01.15.Performer.One.Scene.Title.XXX-GRP":          {6060},
	} {
		cd := cand(title, 0.2)
		cd.Categories = cats
		if got := codes(EvaluateOne(context.Background(), &ctx, cd)); !slices.Contains(got, CodeImageSet) {
			t.Errorf("%s: codes %v, want %s", title, got, CodeImageSet)
		}
	}
	// A video that mentions pictures stays a video.
	if got := codes(EvaluateOne(context.Background(), &ctx, cand("StudioA.24.01.15.Performer.One.Scene.Title.Pics.1080p.MP4-GRP", 1.5))); slices.Contains(got, CodeImageSet) {
		t.Errorf("video refused as image set: %v", got)
	}
}
