package app

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

type apiHistory struct {
	Items []struct {
		ID        string
		Category  string
		Type      string
		ItemRef   string `json:"itemRef"`
		Title     string
		ItemTitle string `json:"itemTitle"`
		ItemGone  bool   `json:"itemGone"`
		Release   string
		Message   string
		Data      map[string]any
	}
	HasMore       bool `json:"hasMore"`
	RetentionDays int  `json:"retentionDays"`
}

// A movie that failed to grab is deleted and another one is added: the old
// entries keep the old title, the new movie gets a new id and its timeline
// does not inherit the old one's events (D-107). Messages keep no download
// tokens. Single entries and filtered sets can be deleted.
func TestHistoryKeepsTitlesAcrossDeleteAndAdd(t *testing.T) {
	token := strings.Repeat("Zq7", 50)
	var hits atomic.Int32
	le := newLibraryEnvWrapped(t, serveDownloads(&hits, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://tracker.invalid/8/download?apikey=k3y&file=Big.Buck.Bunny&link="+token, http.StatusFound)
	}))
	bunny := le.addMovie(10378, true)
	waitFor(t, "the failed grab", func() bool { return len(le.timeline(bunny.ID).grabFailures(t)) > 0 })

	var h apiHistory
	le.expect(le.do("GET", "/api/v1/history?category=failed", nil), 200).json(&h)
	if len(h.Items) == 0 || h.Items[0].ItemTitle != "Big Buck Bunny (2008)" || h.Items[0].ItemGone || h.RetentionDays != 365 {
		t.Fatalf("history before delete: %+v", h)
	}
	if h.Items[0].Data["code"] == nil || h.Items[0].Release == "" {
		t.Fatalf("history entry lacks the grab details: %+v", h.Items[0])
	}
	var raw string
	_ = le.deps.DB.R.QueryRow(`SELECT group_concat(message || data, ' ') FROM timeline_events`).Scan(&raw)
	if strings.Contains(raw, token) || strings.Contains(raw, "k3y") {
		t.Fatalf("a download token or key was stored: %s", raw)
	}

	le.expect(le.do("DELETE", fmt.Sprintf("/api/v1/movies/%d", bunny.ID), nil), 204)
	sintel := le.addMovie(45745, false)
	if sintel.ID == bunny.ID {
		t.Fatalf("the new movie reused the deleted movie's id %d", sintel.ID)
	}
	if n := len(le.timeline(sintel.ID).grabFailures(t)); n != 0 {
		t.Fatalf("the new movie inherited %d grab failures", n)
	}

	le.expect(le.do("GET", "/api/v1/history", nil), 200).json(&h)
	var failed, deleted int
	for _, e := range h.Items {
		if e.ItemRef != fmt.Sprintf("movie:%d", bunny.ID) {
			continue
		}
		if e.ItemTitle != "Big Buck Bunny (2008)" || !e.ItemGone {
			t.Fatalf("old entry relabelled: %+v", e)
		}
		switch e.Category {
		case "failed":
			failed++
		case "deleted":
			deleted++
		}
	}
	if failed == 0 || deleted != 1 {
		t.Fatalf("failed %d deleted %d: %+v", failed, deleted, h.Items)
	}

	// Delete one entry; deleting it again is a 404, malformed ids too.
	le.expect(le.do("GET", "/api/v1/history?category=deleted", nil), 200).json(&h)
	id := h.Items[0].ID
	le.expect(le.do("DELETE", "/api/v1/history/"+id, nil), 204)
	le.expect(le.do("DELETE", "/api/v1/history/"+id, nil), 404)
	le.expect(le.do("DELETE", "/api/v1/history/x:1", nil), 400)

	// Clearing failures only: the dry run counts, the real run deletes the
	// same number and leaves the rest, the blocklist and files alone.
	var cnt struct {
		Count   int64
		Deleted bool
	}
	le.expect(le.do("POST", "/api/v1/history/clear", map[string]any{"category": "failed", "dryRun": true}), 200).json(&cnt)
	if cnt.Count != int64(failed) || cnt.Deleted {
		t.Fatalf("dry run: %+v, want %d", cnt, failed)
	}
	le.expect(le.do("POST", "/api/v1/history/clear", map[string]any{"category": "failed"}), 200).json(&cnt)
	if cnt.Count != int64(failed) || !cnt.Deleted {
		t.Fatalf("clear: %+v", cnt)
	}
	le.expect(le.do("GET", "/api/v1/history?category=failed", nil), 200).json(&h)
	if len(h.Items) != 0 {
		t.Fatalf("failures left: %+v", h.Items)
	}
	var decisions int
	_ = le.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE type = 'decision'`).Scan(&decisions)
	if decisions == 0 {
		t.Fatal("clearing the history removed release decisions")
	}
	// Older than: nothing is a day old yet.
	le.expect(le.do("POST", "/api/v1/history/clear", map[string]any{"olderThanDays": 1}), 200).json(&cnt)
	if cnt.Count != 0 {
		t.Fatalf("older than a day: %+v", cnt)
	}
	le.expect(le.do("POST", "/api/v1/history/clear", map[string]any{"olderThanDays": 0}), 400)
}

// Clearing the history never reads, counts or deletes adult entries, and a
// manager without view_adult cannot delete one by id (D-103). Requesters
// may not clear at all.
func TestHistoryClearLeavesAdultEntries(t *testing.T) {
	e := newEnv(t, noLimits)
	e.setup()
	d := e.deps.DB
	now := db.Millis(time.Now())
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := d.W.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('movie:1', ?, 'grab_failed', 'x')`, now)
	adult := exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('scene:1', ?, 'grab_failed', 'x')`, now)
	exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES ('adult_movie:2#removed', ?, 'deleted', 'x')`, now)
	dl := exec(`INSERT INTO downloads (client_name, client_item_id, protocol, title, item_ref, status, state, added_at, updated_at)
		VALUES ('c', 'a', 'torrent', 't', '{"ref":"scene:1"}', 'failed', 'failed', ?, ?)`, now, now)
	adultDl := exec(`INSERT INTO download_events (download_id, at, type) VALUES (?, ?, 'failed')`, dl, now)

	mgr := e.user("mgr", "manager", nil)
	fam := e.user("fam", "requester", requesterPerms())
	var cnt struct{ Count int64 }
	e.as(mgr, func() {
		e.expect(e.do("POST", "/api/v1/history/clear", map[string]any{"dryRun": true}), 200).json(&cnt)
		if cnt.Count != 1 {
			t.Fatalf("dry run counted %d, want 1", cnt.Count)
		}
		e.expect(e.do("DELETE", fmt.Sprintf("/api/v1/history/t:%d", adult), nil), 404)
		e.expect(e.do("DELETE", fmt.Sprintf("/api/v1/history/d:%d", adultDl), nil), 404)
		e.expect(e.do("POST", "/api/v1/history/clear", map[string]any{}), 200).json(&cnt)
		if cnt.Count != 1 {
			t.Fatalf("cleared %d, want 1", cnt.Count)
		}
	})
	e.as(fam, func() {
		e.expect(e.do("POST", "/api/v1/history/clear", map[string]any{}), 403)
		e.expect(e.do("DELETE", "/api/v1/history/t:1", nil), 403)
	})
	var n int
	_ = d.R.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE item_ref LIKE 'scene:%' OR item_ref LIKE 'adult_movie:%'`).Scan(&n)
	if n != 2 {
		t.Fatalf("adult timeline entries left: %d, want 2", n)
	}
	_ = d.R.QueryRow(`SELECT COUNT(*) FROM download_events`).Scan(&n)
	if n != 1 {
		t.Fatalf("adult download events left: %d, want 1", n)
	}
}
