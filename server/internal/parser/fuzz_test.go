package parser

import (
	"testing"
	"time"
	"unicode/utf8"
)

// FuzzParse checks that no input makes the parser panic, hang or return
// nonsense (confidence outside 0..1, invalid UTF-8 in the title).
func FuzzParse(f *testing.F) {
	for _, cases := range loadGolden(f) {
		for i, c := range cases {
			if i%5 == 0 {
				f.Add(c.Name)
			}
		}
	}
	f.Add("S01E01-E999999999999999999")
	f.Add("[[[[((((")
	f.Add("- - - - 01 - 02 ~ 03")
	f.Fuzz(func(t *testing.T, name string) {
		start := time.Now()
		r := Parse(name)
		p := ParsePath(name)
		if d := time.Since(start); d > 250*time.Millisecond {
			t.Fatalf("parsing %q took %v", name, d)
		}
		for _, x := range []Release{r, p} {
			if x.Confidence < 0 || x.Confidence > 1 {
				t.Fatalf("confidence %v for %q", x.Confidence, name)
			}
			if utf8.ValidString(name) && !utf8.ValidString(x.Title) {
				t.Fatalf("invalid title %q for %q", x.Title, name)
			}
			if len(x.Episodes) > 1000 || len(x.AbsoluteEpisodes) > 1000 || len(x.Seasons) > 1000 {
				t.Fatalf("too many numbers for %q", name)
			}
		}
	})
}

func goldenNames(tb testing.TB) []string {
	var names []string
	for _, cases := range loadGolden(tb) {
		for _, c := range cases {
			names = append(names, c.Name)
		}
	}
	return names
}

// BenchmarkParse1000 parses 1000 golden names per iteration (target: < 50 ms).
func BenchmarkParse1000(b *testing.B) {
	names := goldenNames(b)[:1000]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, n := range names {
			_ = Parse(n)
		}
	}
}

// TestParseSpeed enforces the target outside benchmarks (with headroom for
// the race detector and short runs).
func TestParseSpeed(t *testing.T) {
	names := goldenNames(t)[:1000]
	limit := 50 * time.Millisecond
	if raceEnabled || testing.Short() {
		limit = time.Second
	}
	best := time.Hour
	for round := 0; round < 5; round++ {
		start := time.Now()
		for _, n := range names {
			_ = Parse(n)
		}
		best = min(best, time.Since(start))
	}
	t.Logf("1000 names in %v", best)
	if best > limit {
		t.Errorf("1000 names took %v, want < %v", best, limit)
	}
}
