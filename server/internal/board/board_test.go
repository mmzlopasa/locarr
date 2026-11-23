package board

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

func insertItems(t testing.TB, d *db.DB, items []*Item) {
	t.Helper()
	err := d.Tx(context.Background(), func(tx *sql.Tx) error {
		for _, it := range items {
			if it.UpdatedAt.IsZero() {
				it.UpdatedAt = time.Now()
			}
			if err := upsert(context.Background(), tx, it); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func item(key, kind, code, fix, tone string, reasons ...timeline.ReasonCount) *Item {
	return &Item{Key: key, Kind: kind, ItemRef: key, Title: key, SortTitle: key, Code: code, Fix: fix, Tone: tone, Searches: 2,
		Params: map[string]any{}, Reasons: reasons}
}

func TestSummarizeGroupsAndAddsUpReasons(t *testing.T) {
	d := db.OpenTest(t)
	s := &Service{DB: d}
	size := timeline.ReasonCount{Code: "size_too_large", Count: 3, Example: "too big"}
	lang := timeline.ReasonCount{Code: "language_not_wanted", Count: 1, Example: "German"}
	insertItems(t, d, []*Item{
		item("movie:1", KindMovie, "all_rejected", FixProfile, "warning", size, lang),
		item("movie:2", KindMovie, "all_rejected", FixProfile, "warning", size),
		item("episode:3", KindEpisode, "all_rejected", FixProfile, "warning", lang),
		item("movie:4", KindMovie, "import_failed", FixPathMapping, "danger"),
		item("movie:5", KindMovie, "import_failed", FixPermissions, "danger"),
		item("album:6", KindAlbum, "not_searched_yet", "", "info"),
		item("subtitle:movie:7", KindSubtitle, CodeSubNotFound, "", "warning"),
	})
	sum, err := s.Summarize(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Total != 7 || len(sum.Groups) != 5 {
		t.Fatalf("summary: %+v", sum)
	}
	// Danger first, then warnings by size.
	if sum.Groups[0].Tone != "danger" || sum.Groups[1].Tone != "danger" || sum.Groups[2].Code != "all_rejected" {
		t.Fatalf("order: %+v", sum.Groups)
	}
	g := sum.Groups[2]
	if g.Count != 3 || g.Kinds[KindMovie] != 2 || g.Kinds[KindEpisode] != 1 || g.Searches != 6 {
		t.Fatalf("group: %+v", g)
	}
	if len(g.Reasons) != 2 || g.Reasons[0].Code != "size_too_large" || g.Reasons[0].Count != 6 || g.Reasons[1].Count != 2 {
		t.Fatalf("reasons: %+v", g.Reasons)
	}
	only, err := s.Summarize(context.Background(), KindEpisode)
	if err != nil || only.Total != 1 || len(only.Groups) != 1 || only.Groups[0].Reasons[0].Code != "language_not_wanted" {
		t.Fatalf("kind filter: %+v %v", only, err)
	}
	list, total, err := s.List(context.Background(), ListFilter{Code: "import_failed", Fix: FixPathMapping})
	if err != nil || total != 1 || list[0].Key != "movie:4" {
		t.Fatalf("list: %+v %d %v", list, total, err)
	}
	list, total, err = s.List(context.Background(), ListFilter{Query: "MOVIE:"})
	if err != nil || total != 5 || len(list) != 5 {
		t.Fatalf("search: %d %v", total, err)
	}
}

func TestClassifyImportFailure(t *testing.T) {
	for msg, want := range map[string]string{
		"cannot read /data/downloads/x: no such file or directory":  FixPathMapping,
		"link /data/x: permission denied":                           FixPermissions,
		"copy: no space left on device":                             FixDiskSpace,
		"the client has no category movies":                         FixClientCategory,
		"no video file in the download matches the movie (sample?)": FixManualImport,
	} {
		if got := ClassifyImportFailure(msg); got != want {
			t.Errorf("%q: %s, want %s", msg, got, want)
		}
	}
}

func TestExplainSubtitle(t *testing.T) {
	it := &Item{}
	explainSubtitle(it, &subFacts{missing: []string{"hr"}}, &env{subEnabled: 0})
	if it.Code != CodeSubNoProviders || it.Fix != FixSubProviders {
		t.Fatal(it.Code)
	}
	explainSubtitle(it, &subFacts{missing: []string{"hr"}}, &env{subEnabled: 2, subNoQuota: 2})
	if it.Code != CodeSubQuotaExceeded {
		t.Fatal(it.Code)
	}
	explainSubtitle(it, &subFacts{missing: []string{"hr"}}, &env{subEnabled: 2})
	if it.Code != CodeSubNotSearched {
		t.Fatal(it.Code)
	}
	explainSubtitle(it, &subFacts{missing: []string{"hr"}, searches: 3, lastResult: "best match 40 < 60"}, &env{subEnabled: 2, subNoQuota: 1})
	if it.Code != CodeSubNotFound || it.Params["detail"] != "best match 40 < 60" {
		t.Fatal(it.Code, it.Params)
	}
}

// The board answers quickly with tens of thousands of items.
func TestBoardReadPerformance(t *testing.T) {
	if raceEnabled {
		t.Skip("timings mean nothing under the race detector")
	}
	d := db.OpenTest(t)
	s := &Service{DB: d}
	codes := []string{"all_rejected", "nothing_found", "not_searched_yet", "downloading", "import_failed", "waiting_delay"}
	items := make([]*Item, 0, 20_000)
	for i := 0; i < 20_000; i++ {
		kind := []string{KindMovie, KindEpisode, KindAlbum, KindSubtitle}[i%4]
		it := item(fmt.Sprintf("%s:%d", kind, i), kind, codes[i%len(codes)], "", "warning",
			timeline.ReasonCount{Code: fmt.Sprintf("r%d", i%7), Count: 1 + i%5, Example: "x"})
		it.SortTitle = fmt.Sprintf("title %06d", i)
		items = append(items, it)
	}
	insertItems(t, d, items)
	start := time.Now()
	sum, err := s.Summarize(context.Background(), "")
	took := time.Since(start)
	if err != nil || sum.Total != 20_000 {
		t.Fatal(err, sum.Total)
	}
	t.Logf("summary of 20000 items in %s", took)
	if took > 300*time.Millisecond {
		t.Errorf("summary took %s", took)
	}
	start = time.Now()
	list, total, err := s.List(context.Background(), ListFilter{Code: "all_rejected", Offset: 2000, Limit: 100})
	took = time.Since(start)
	if err != nil || len(list) != 100 || total < 3000 {
		t.Fatal(err, len(list), total)
	}
	t.Logf("page of 100 in %s", took)
	if took > 100*time.Millisecond {
		t.Errorf("page took %s", took)
	}
}
