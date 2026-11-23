package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/notify/channels"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

var loopback = httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}}

type receiver struct {
	mu     sync.Mutex
	bodies []map[string]any
	fail   bool
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		http.Error(w, "nope", http.StatusInternalServerError)
		return
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	r.bodies = append(r.bodies, m)
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bodies)
}

func newService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	d := db.OpenTest(t)
	box, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: d, Box: box, Settings: settings.New(d, box), Jobs: jobs.New(d, slog.Default(), jobs.Options{}),
		Policy: func(context.Context) (httpx.Policy, error) { return loopback, nil }}
	s.Register()
	for i, u := range []struct{ name, role string }{{"boss", "admin"}, {"ana", "requester"}, {"iva", "requester"}} {
		if _, err := d.W.Exec(`INSERT INTO users (id, username, username_key, password_hash, role, permissions, language, created_at, updated_at)
			VALUES (?, ?, ?, 'x', ?, 0, ?, 0, 0)`, i+1, u.name, u.name, u.role, map[bool]string{true: "hr", false: "en"}[u.name == "ana"]); err != nil {
			t.Fatal(err)
		}
	}
	return s, d
}

// runJobs runs the queued delivery jobs once (attempt 1 of 1).
func runJobs(t *testing.T, s *Service) []error {
	t.Helper()
	list, err := s.Jobs.List(context.Background(), jobs.Queued, 100)
	if err != nil {
		t.Fatal(err)
	}
	var errs []error
	for i := len(list) - 1; i >= 0; i-- {
		j := list[i]
		j.Attempt = 1
		errs = append(errs, s.runDeliver(context.Background(), &j))
		if err := s.Jobs.Cancel(context.Background(), j.ID); err != nil && !errors.Is(err, jobs.ErrNotCancelable) {
			t.Fatal(err)
		}
	}
	return errs
}

func webhook(url string, events ...notify.Type) Input {
	return Input{Name: "hook", Kind: channels.Webhook, Enabled: true, Events: events, Config: channels.Config{URL: url}}
}

func TestRoutingSharedAndPersonal(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	shared, mine, other := &receiver{}, &receiver{}, &receiver{}
	for _, r := range []*receiver{shared, mine, other} {
		srv := httptest.NewServer(r)
		t.Cleanup(srv.Close)
		r.mu.Lock()
		r.bodies = nil
		r.mu.Unlock()
		switch r {
		case shared:
			if _, err := s.Create(ctx, Owner{}, webhook(srv.URL, notify.RequestCreated, notify.LibraryImported)); err != nil {
				t.Fatal(err)
			}
		case mine:
			// An administrator may use webhooks for their own channel.
			if _, err := s.Create(ctx, Owner{UserID: 1, Admin: true}, webhook(srv.URL, notify.RequestAvailable)); err != nil {
				t.Fatal(err)
			}
		case other:
			if _, err := s.Create(ctx, Owner{UserID: 3, Admin: true}, webhook(srv.URL, notify.RequestAvailable, notify.RequestCreated)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// request.available for user 1: the shared channel did not choose it; user 3 is not a recipient.
	_ = s.Deliver(ctx, notify.Event{Type: notify.RequestAvailable, Recipients: []int64{1}, Title: "Sintel (2010)", RequestID: 4})
	// request.created goes to the shared channel; user 3 chose it but is not a recipient.
	_ = s.Deliver(ctx, notify.Event{Type: notify.RequestCreated, Recipients: []int64{1}, Title: "Sintel (2010)"})
	// A library event never reaches personal channels.
	_ = s.Deliver(ctx, notify.Event{Type: notify.LibraryImported, Title: "Sintel (2010)", ItemRef: "movie:1"})
	for _, err := range runJobs(t, s) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if shared.count() != 2 || mine.count() != 1 || other.count() != 0 {
		t.Fatalf("shared %d mine %d other %d", shared.count(), mine.count(), other.count())
	}
	if mine.bodies[0]["event"] != "request.available" || mine.bodies[0]["requestId"] != float64(4) {
		t.Fatalf("%v", mine.bodies[0])
	}
}

func TestRulesForPersonalChannels(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	ana := Owner{UserID: 2}
	if _, err := s.Create(ctx, ana, webhook("http://example.com/h", notify.RequestAvailable)); !errors.Is(err, ErrKindForbidden) {
		t.Fatal(err)
	}
	ntfy := Input{Name: "phone", Kind: channels.Ntfy, Enabled: true, Events: []notify.Type{notify.LibraryImported},
		Config: channels.Config{URL: "https://ntfy.sh", Topic: "ana-locarr"}}
	if _, err := s.Create(ctx, ana, ntfy); !errors.Is(err, ErrEventNotAllow) {
		t.Fatal(err)
	}
	ntfy.Events = []notify.Type{notify.RequestAvailable, notify.RequestAvailable, notify.IssueResolved}
	c, err := s.Create(ctx, ana, ntfy)
	if err != nil || len(c.Events) != 2 || !c.Personal() {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := s.Create(ctx, Owner{}, Input{Name: "x", Kind: "fax", Events: nil}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, Owner{}, Input{Name: "x", Kind: channels.Webhook, Events: []notify.Type{"nope"},
		Config: channels.Config{URL: "http://h"}}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	// A non-administrator's own channel cannot reach the local network.
	srv := httptest.NewServer(&receiver{})
	t.Cleanup(srv.Close)
	gotify := Input{Name: "g", Kind: channels.Gotify, Events: []notify.Type{notify.RequestAvailable}, Config: channels.Config{URL: srv.URL},
		Secrets: SecretsPatch{Token: ptr("tok")}}
	if err := s.Test(ctx, ana, 0, gotify); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatal("personal channel reached loopback:", err)
	}
	if err := s.Test(ctx, Owner{}, 0, gotify); err != nil {
		t.Fatal(err)
	}
}

func ptr(s string) *string { return &s }

func TestSecretsAndFailureState(t *testing.T) {
	s, d := newService(t)
	ctx := context.Background()
	r := &receiver{fail: true}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	in := webhook(srv.URL, notify.IssueCreated)
	in.Secrets = SecretsPatch{Secret: ptr("hmac-key-123")}
	c, err := s.Create(ctx, Owner{}, in)
	if err != nil || strings.Join(c.Secrets.Names(), ",") != "secret" {
		t.Fatal(c.Secrets.Names(), err)
	}
	var raw []byte
	if err := d.R.QueryRow(`SELECT secrets FROM notification_channels WHERE id = ?`, c.ID).Scan(&raw); err != nil ||
		strings.Contains(string(raw), "hmac-key-123") {
		t.Fatal("secret stored in clear")
	}
	_ = s.Deliver(ctx, notify.Event{Type: notify.IssueCreated, Title: "Sintel"})
	errs := runJobs(t, s)
	if len(errs) != 1 || errs[0] == nil {
		t.Fatal(errs)
	}
	c, _ = s.Get(ctx, c.ID)
	if c.Failures != 1 || c.LastError == "" || c.LastErrorAt == nil {
		t.Fatalf("%+v", c)
	}
	// Update keeps the secret when it is left out, then clears it with "".
	in.Secrets = SecretsPatch{}
	if c, _ = s.Update(ctx, c.ID, true, in); c.Secrets.Secret != "hmac-key-123" {
		t.Fatal("secret lost")
	}
	in.Secrets = SecretsPatch{Secret: ptr("")}
	if c, _ = s.Update(ctx, c.ID, true, in); c.Secrets.Secret != "" {
		t.Fatal("secret kept")
	}
	r.mu.Lock()
	r.fail = false
	r.mu.Unlock()
	_ = s.Deliver(ctx, notify.Event{Type: notify.IssueCreated, Title: "Sintel"})
	runJobs(t, s)
	c, _ = s.Get(ctx, c.ID)
	if c.Failures != 0 || c.LastSentAt == nil {
		t.Fatalf("%+v", c)
	}
	// Disabled channels get nothing.
	in.Enabled = false
	_, _ = s.Update(ctx, c.ID, true, in)
	_ = s.Deliver(ctx, notify.Event{Type: notify.IssueCreated, Title: "Sintel"})
	if n := len(runJobs(t, s)); n != 0 {
		t.Fatal("queued for a disabled channel")
	}
	if err := s.Delete(ctx, c.ID); err != nil || !errors.Is(s.Delete(ctx, c.ID), ErrNotFound) {
		t.Fatal(err)
	}
}

func TestMailSettings(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	pw := "smtp-secret"
	base := "https://locarr.example.com/"
	if err := s.SaveSettings(ctx, &base, &MailInput{SMTP: channels.SMTP{Host: "mail.example.com", Port: 587, From: "locarr@example.com"},
		Password: &pw}); err != nil {
		t.Fatal(err)
	}
	b, m, set, err := s.LoadSettings(ctx)
	if err != nil || b != "https://locarr.example.com" || m.Security != "starttls" || !set || m.Password != "" {
		t.Fatalf("%q %+v %v %v", b, m, set, err)
	}
	bad := "ftp://x"
	if err := s.SaveSettings(ctx, &bad, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.SaveSettings(ctx, nil, &MailInput{SMTP: channels.SMTP{Host: "a\r\nb"}}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
