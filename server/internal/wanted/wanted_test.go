package wanted

import (
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers"
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
