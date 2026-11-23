package notify

import (
	"context"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

type recorder struct{ got []Event }

func (r *recorder) Name() string { return "test" }
func (r *recorder) Deliver(_ context.Context, ev Event) error {
	r.got = append(r.got, ev)
	return nil
}

func TestHub(t *testing.T) {
	d := db.OpenTest(t)
	ctx := context.Background()
	for _, n := range []string{"ana", "ivo", "mia"} {
		if _, err := d.W.Exec(`INSERT INTO users (username, username_key, password_hash, role, created_at, updated_at) VALUES (?, ?, 'x', 'requester', 0, 0)`,
			n, n); err != nil {
			t.Fatal(err)
		}
	}
	rec := &recorder{}
	h := &Hub{DB: d, Sinks: []Sink{rec}}
	// The actor is never told about their own action; duplicates are dropped.
	h.Publish(ctx, Event{Type: RequestCreated, Recipients: []int64{1, 2, 2, 3}, Actor: &User{ID: 3, Username: "mia"}, Title: "Sintel (2010)",
		Params: map[string]any{"is4k": true}})
	h.Publish(ctx, Event{Type: RequestAvailable, Recipients: []int64{1}, Title: "Sintel (2010)"})
	if len(rec.got) != 2 || len(rec.got[0].Recipients) != 2 || rec.got[0].At.IsZero() {
		t.Fatalf("sink: %+v", rec.got)
	}
	p, err := h.List(ctx, 1, ListOptions{})
	if err != nil || len(p.Items) != 2 || p.Unread != 2 || p.Items[0].Type != RequestAvailable || p.LatestID != p.Items[0].ID {
		t.Fatalf("list: %+v %v", p, err)
	}
	if p.Items[1].Actor == nil || p.Items[1].Actor.Username != "mia" || p.Items[1].Params["is4k"] != true {
		t.Fatalf("stored event: %+v", p.Items[1])
	}
	// Polling with since returns only newer ones, oldest first.
	first := p.Items[1].ID
	p, _ = h.List(ctx, 1, ListOptions{Since: first})
	if len(p.Items) != 1 || p.Items[0].Type != RequestAvailable {
		t.Fatalf("since: %+v", p.Items)
	}
	if n, err := h.MarkRead(ctx, 1, []int64{first}, false); err != nil || n != 1 {
		t.Fatalf("mark: %d %v", n, err)
	}
	// Another user's ids are not touched.
	if n, _ := h.MarkRead(ctx, 2, []int64{first}, false); n != 1 {
		t.Fatalf("other user unread %d", n)
	}
	if p, _ := h.List(ctx, 1, ListOptions{UnreadOnly: true}); len(p.Items) != 1 {
		t.Fatalf("unread only: %+v", p.Items)
	}
	if n, _ := h.MarkRead(ctx, 1, nil, true); n != 0 {
		t.Fatal("mark all")
	}
	if _, err := h.MarkRead(ctx, 1, nil, false); err != ErrNothingToMark {
		t.Fatal(err)
	}
	if n, err := h.Prune(ctx, time.Now().Add(time.Hour)); err != nil || n != 2 {
		t.Fatalf("prune: %d %v", n, err)
	}
	if p, _ := h.List(ctx, 2, ListOptions{}); len(p.Items) != 1 {
		t.Fatal("unread notifications must survive pruning")
	}
}
