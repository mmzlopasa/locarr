package health

import (
	"context"
	"errors"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.0", "1.1.9", true}, {"1.2.0", "v1.2.0", false}, {"1.10.0", "1.9.3", true}, {"1.0.0", "dev", false},
		{"2.0.0-rc1", "1.9.0", true}, {"garbage", "1.0.0", false}, {"1.0.0", "1.0.1", false},
	} {
		if got := NewerVersion(c.latest, c.current); got != c.want {
			t.Errorf("%s vs %s: %v", c.latest, c.current, got)
		}
	}
}

func TestRunTracksNewIssuesOnce(t *testing.T) {
	s := &Service{}
	var issues []Issue
	s.Register("fake", func(context.Context) ([]Issue, error) { return issues, nil })
	s.Register("broken", func(context.Context) ([]Issue, error) { return nil, errors.New("boom") })
	var published []string
	s.OnNew = func(_ context.Context, is Issue) { published = append(published, is.Key) }
	issues = []Issue{{Key: "a", Severity: SeverityError, Code: "x"}, {Key: "b", Severity: SeverityInfo, Code: "y"}}
	rep := s.Run(context.Background())
	if len(rep.Issues) != 3 || rep.Issues[0].Key != "a" || rep.Issues[2].Key != "b" {
		t.Fatalf("%+v", rep.Issues)
	}
	if len(published) != 2 || published[0] != "a" || published[1] != "check:broken" {
		t.Fatalf("published: %v", published)
	}
	since := rep.Issues[0].Since
	s.Run(context.Background())
	if len(published) != 2 {
		t.Fatalf("published twice: %v", published)
	}
	issues = nil
	s.Run(context.Background())
	issues = []Issue{{Key: "a", Severity: SeverityError}}
	rep = s.Run(context.Background())
	if len(published) != 3 || !rep.Issues[0].Since.After(since) && !rep.Issues[0].Since.Equal(since) {
		t.Fatalf("a problem that comes back is new again: %v", published)
	}
	s.SetStatus("streamero", false, StreameroIssue("refused"))
	if e, w, _ := s.Counts(); e != 1 || w != 1 {
		t.Fatal(e, w)
	}
	rep = s.Run(context.Background())
	found := false
	for _, is := range rep.Issues {
		found = found || is.Code == "streamero_unreachable"
	}
	if !found {
		t.Fatal("reported issue missing")
	}
}
