package wanted

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

func TestBackoff(t *testing.T) {
	want := []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour, 16 * time.Hour, 24 * time.Hour, 24 * time.Hour}
	for i, w := range want {
		if got := Backoff(i + 1); got != w {
			t.Errorf("Backoff(%d) = %s, want %s", i+1, got, w)
		}
	}
}

func TestCandidateAdapter(t *testing.T) {
	zero, two := 0.0, 2.0
	seeds := 7
	r := indexers.Release{GUID: "g", Title: "Movie.2020.1080p.WEB-DL", IndexerID: 3, IndexerName: "X", Protocol: indexers.Torrent,
		Size: 100, Seeders: &seeds, MagnetURL: "magnet:?xt=urn:btih:abc", InfoHash: "abc", TMDBID: 5, IMDBID: "tt1",
		Flags: []string{"Internal", "doubleupload"}, DownloadVolumeFactor: &zero, UploadVolumeFactor: &two}
	ind := &indexers.Indexer{ID: 3, Name: "X", Enabled: true, EnableRSS: false, EnableAutomatic: true, EnableInteractive: true, Priority: 10, MinimumSeeders: 4}
	c := Candidate(&r, ind)
	if c.DownloadURL != r.MagnetURL || c.InfoHash != "abc" || c.IDs.TMDb != 5 || !c.IndexerRSSDisabled || c.IndexerPriority != 10 || c.MinimumSeeders != 4 {
		t.Fatalf("%+v", c)
	}
	flags := map[string]bool{}
	for _, f := range c.IndexerFlags {
		flags[f] = true
	}
	if !flags["internal"] || !flags["double_upload"] || !flags["freeleech"] || len(flags) != 3 {
		t.Fatalf("flags: %v", c.IndexerFlags)
	}
	back := fromCandidate(c)
	if back.MagnetURL != r.MagnetURL || back.DownloadURL != "" || back.InfoHash != "abc" {
		t.Fatalf("round trip: %+v", back)
	}
}

func TestClassifyGrab(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.Local)
	cases := []struct {
		name         string
		err          error
		code         string
		indexer, bad bool
		until        bool
	}{
		{"disabled till", &znab.Error{Kind: znab.KindRateLimit, Code: 429, Message: "Indexer is disabled till 2026-11-23 12:53:00 due to recent failures."},
			GrabIndexerDisabled, true, false, true},
		{"429", &znab.Error{Kind: znab.KindRateLimit, Code: 429, Message: "HTTP 429", RetryAfter: 60}, GrabIndexerLimited, true, false, true},
		{"download failed 500", &znab.Error{Kind: znab.KindRateLimit, Code: 500, Message: "Download failed"}, GrabIndexerError, true, false, false},
		{"5xx", &znab.Error{Kind: znab.KindUnreachable, Code: 502, Message: "HTTP 502"}, GrabIndexerError, true, false, false},
		{"timeout", &znab.Error{Kind: znab.KindUnreachable, Message: "timeout"}, GrabIndexerUnreachable, true, false, false},
		{"404", &znab.Error{Kind: znab.KindNotFound, Code: 404, Message: "gone"}, GrabReleaseGone, false, true, false},
		{"html", &znab.Error{Kind: znab.KindBadResponse, Message: "the answer is not a .torrent or .nzb file (a web page)"}, GrabBadFile, false, true, false},
		{"loop", &znab.Error{Kind: znab.KindBadResponse, Message: "request x: too many redirects"}, GrabBadRedirect, false, false, false},
		{"bad torrent", fmt.Errorf("%w: %w", downloads.ErrFetch, downloads.ErrBadTorrent), GrabBadFile, false, true, false},
		{"grab limit", indexers.ErrGrabLimit, GrabLimitReached, true, false, false},
		{"no client", downloads.ErrNoClient, GrabNoClient, false, false, false},
		{"queued", downloads.ErrAlreadyQueued, GrabAlreadyQueued, false, false, false},
		{"other", errors.New("boom"), GrabOther, false, false, false},
	}
	for _, c := range cases {
		f := classifyGrab(c.err, now)
		if f.Code != c.code || f.IndexerProblem != c.indexer || f.Content != c.bad || (f.Until != nil) != c.until {
			t.Errorf("%s: %+v", c.name, f)
		}
	}
}

func TestPaceSpacesGrabsPerIndexer(t *testing.T) {
	s := &Service{GrabGap: 80 * time.Millisecond}
	ctx := context.Background()
	start := time.Now()
	for range 3 {
		if err := s.pace(ctx, 1); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 160*time.Millisecond {
		t.Fatalf("three grabs from one indexer in %s", d)
	}
	other := time.Now()
	if err := s.pace(ctx, 2); err != nil || time.Since(other) > 40*time.Millisecond {
		t.Fatal("another indexer waited")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.pace(cctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
}
