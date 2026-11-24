package wanted

import (
	"slices"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/indexers"
)

func TestSearchTermList(t *testing.T) {
	got := searchTermList("StudioA 24 01 15", []string{"StudioA 24.01.15", " ", "Performer One Scene Title", "performer one scene title", "Scene Title"})
	want := []string{"StudioA 24 01 15", "Performer One Scene Title", "Scene Title"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}

func TestMergeResults(t *testing.T) {
	r := func(guid string) indexers.Release { return indexers.Release{IndexerID: 1, GUID: guid, Title: guid} }
	a := indexers.Result{Releases: []indexers.Release{r("a")}, Indexers: []indexers.IndexerResult{
		{IndexerID: 1, Status: indexers.StatusOK, Count: 1}, {IndexerID: 2, Status: indexers.StatusError, Error: "down"}}}
	b := indexers.Result{Releases: []indexers.Release{r("a"), r("b")}, Indexers: []indexers.IndexerResult{
		{IndexerID: 1, Status: indexers.StatusOK, Count: 2}, {IndexerID: 2, Status: indexers.StatusOK, Count: 3}}}
	m := mergeResults(mergeResults(indexers.Result{}, a), b)
	if len(m.Releases) != 2 || len(m.Indexers) != 2 {
		t.Fatalf("%+v", m)
	}
	if m.Indexers[0].Count != 3 || m.Indexers[1].Status != indexers.StatusOK || m.Indexers[1].Count != 3 || m.Indexers[1].Error != "" {
		t.Fatalf("%+v", m.Indexers)
	}
}
