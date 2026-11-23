package streamero

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Name is shown on timelines.
const Name = "Streamero"

// DefaultDebounce is how long library changes are collected before the scans are asked for.
const DefaultDebounce = 30 * time.Second

// scanAttempts is how often a batch is tried (with the queue's backoff) before it is given up.
const scanAttempts = 6

// Notifier is the mediaserver.Notifier for Streamero (D-067). Notify only
// stores the change and schedules a scan job, so it never blocks an import;
// the job collects the changes of ~30 s, asks Streamero to scan each affected
// library once and records the outcome on every item's timeline.
type Notifier struct{ S *Service }

// Name implements mediaserver.Notifier. It is empty while Streamero is off,
// so the library does not call it at all.
func (n Notifier) Name() string {
	if n.S == nil || !n.S.Enabled(context.Background()) {
		return ""
	}
	return Name
}

// RecordsOutcome implements mediaserver.SelfRecording: the scan job writes
// the timeline entry once Streamero has been asked.
func (Notifier) RecordsOutcome() bool { return true }

// Notify implements mediaserver.Notifier.
func (n Notifier) Notify(ctx context.Context, c mediaserver.Change) error {
	return n.S.Queue(ctx, c)
}

// Queue stores a change and makes sure a scan job runs after the debounce.
func (s *Service) Queue(ctx context.Context, c mediaserver.Change) error {
	paths := c.Paths
	if paths == nil {
		paths = []string{}
	}
	raw, _ := json.Marshal(paths)
	title := c.Title
	if c.Year > 0 {
		title = fmt.Sprintf("%s (%d)", c.Title, c.Year)
	}
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO mediaserver_pending (item_ref, action, title, paths, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ItemRef, c.Action, title, string(raw), db.Millis(s.now())); err != nil {
		return err
	}
	d := s.Debounce
	if d <= 0 {
		d = DefaultDebounce
	}
	_, _, err := s.Jobs.Enqueue(ctx, TaskScan, nil, jobs.EnqueueOptions{UniqueKey: TaskScan, RunAt: s.now().Add(d), MaxAttempts: scanAttempts})
	return err
}

type pending struct {
	id     int64
	ref    string
	action string
	paths  []string
	libs   []string
}

// RunScan is the scan job: it sends every stored change, then any that
// arrived meanwhile.
func (s *Service) RunScan(ctx context.Context, j *jobs.Job) error {
	final := j == nil || j.Attempt >= j.MaxAttempts
	for range 20 {
		n, err := s.scanOnce(ctx, final)
		if err != nil || n == 0 {
			return err
		}
	}
	return nil
}

func (s *Service) scanOnce(ctx context.Context, final bool) (int, error) {
	rows, err := s.loadPending(ctx)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	cl, c, err := s.Client(ctx)
	if errors.Is(err, ErrNotConfigured) {
		// Switched off meanwhile: forget the changes quietly.
		return 0, s.drop(ctx, rows)
	}
	if err != nil {
		return 0, err
	}
	libs, err := cl.Libraries(ctx)
	if err != nil {
		return s.failed(ctx, rows, err, final)
	}
	roots, err := s.Roots.List(ctx)
	if err != nil {
		return 0, err
	}
	names := map[string]string{}
	for _, l := range libs {
		names[l.ID] = l.Name
	}
	want := map[string]bool{}
	var done []pending
	for i := range rows {
		rows[i].libs = Resolve(rows[i].paths, roots, c, libs)
		if len(rows[i].libs) == 0 {
			p := ""
			if len(rows[i].paths) > 0 {
				p = ToStreamero(rows[i].paths[0], c.PathMappings)
			}
			s.record(ctx, rows[i].ref, false, "could not notify Streamero: no Streamero library contains "+p+
				" (check the path mappings and libraries in Settings → Media server)", nil)
			done = append(done, rows[i])
			continue
		}
		for _, id := range rows[i].libs {
			want[id] = true
		}
	}
	scanned := map[string]bool{}
	var scanErr error
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := cl.Scan(ctx, id); err != nil {
			scanErr = err
			continue
		}
		scanned[id] = true
	}
	var failed []pending
	for _, r := range rows {
		if len(r.libs) == 0 {
			continue
		}
		ok := true
		var ln []string
		for _, id := range r.libs {
			ok = ok && scanned[id]
			ln = append(ln, names[id])
		}
		if ok {
			s.record(ctx, r.ref, true, "notified Streamero: scanning "+strings.Join(ln, ", "), ln)
			done = append(done, r)
		} else {
			failed = append(failed, r)
		}
	}
	if err := s.drop(ctx, done); err != nil {
		return 0, err
	}
	if len(scanned) > 0 {
		now := s.now()
		s.setStatus(func(st *Status) { st.LastScanAt, st.LastScanError = &now, "" })
		s.QueueIndex(ctx, 2*time.Minute)
		s.health(ctx, true, "")
	}
	if len(failed) > 0 {
		return s.failed(ctx, failed, scanErr, final)
	}
	return len(rows), nil
}

func (s *Service) failed(ctx context.Context, rows []pending, err error, final bool) (int, error) {
	msg := err.Error()
	s.setStatus(func(st *Status) { st.LastScanError = msg })
	s.health(ctx, false, msg)
	if !final {
		return 0, err
	}
	for _, r := range rows {
		s.record(ctx, r.ref, false, "could not notify Streamero: "+msg, nil)
	}
	if derr := s.drop(ctx, rows); derr != nil {
		return 0, derr
	}
	return 0, err
}

func (s *Service) health(ctx context.Context, ok bool, reason string) {
	if s.Health != nil {
		s.Health(ctx, ok, reason)
	}
}

func (s *Service) record(ctx context.Context, ref string, ok bool, msg string, libs []string) {
	if s.Timeline == nil || ref == "" {
		return
	}
	data := map[string]any{"server": Name, "ok": ok}
	if libs != nil {
		data["libraries"] = libs
	}
	if !ok {
		data["reason"] = strings.TrimPrefix(msg, "could not notify Streamero: ")
	}
	if err := s.Timeline.Record(ctx, ref, timeline.TypeNotified, msg, data, nil); err != nil {
		s.log().Warn("record media server notification", "item", ref, "err", err)
	}
}

func (s *Service) loadPending(ctx context.Context) ([]pending, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, item_ref, action, paths FROM mediaserver_pending ORDER BY id LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pending
	for rows.Next() {
		var (
			p   pending
			raw string
		)
		if err := rows.Scan(&p.id, &p.ref, &p.action, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &p.paths)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) drop(ctx context.Context, rows []pending) error {
	for _, r := range rows {
		if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM mediaserver_pending WHERE id = ?`, r.id); err != nil {
			return err
		}
	}
	return nil
}

// Resolve returns the Streamero libraries to scan for changed paths: the
// libraries chosen for the root folder that holds the path, otherwise every
// library with a folder that holds the path as Streamero sees it.
func Resolve(paths []string, roots []rootfolders.Folder, c Config, libs []Library) []string {
	set := map[string]bool{}
	known := map[string]bool{}
	for _, l := range libs {
		known[l.ID] = true
	}
	for _, p := range paths {
		p = path.Clean(p)
		var mapped []string
		best := -1
		for _, r := range roots {
			if under(p, path.Clean(r.Path)) && len(r.Path) > best {
				for _, m := range c.LibraryMap {
					if m.RootFolderID == r.ID && len(m.LibraryIDs) > 0 {
						best, mapped = len(r.Path), m.LibraryIDs
					}
				}
			}
		}
		if mapped != nil {
			for _, id := range mapped {
				if known[id] {
					set[id] = true
				}
			}
			continue
		}
		sp := ToStreamero(p, c.PathMappings)
		for _, l := range libs {
			for _, lp := range l.Paths {
				if under(sp, path.Clean(lp)) {
					set[l.ID] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
