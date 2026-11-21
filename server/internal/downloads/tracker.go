package downloads

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Snapshot is the last poll of one client.
type Snapshot struct {
	ClientID  int64
	CheckedAt time.Time
	OK        bool
	ErrorCode string
	Error     string
	Version   string
	FreeSpace *int64
	TestedAt  time.Time
	// Unknown are items in Locarr's categories that Locarr did not add; they
	// are shown but never touched.
	Unknown []downloaders.Item
}

// Snapshots returns the last poll result of every client.
func (s *Service) Snapshots() map[int64]Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	out := make(map[int64]Snapshot, len(s.snapshots))
	for id, sn := range s.snapshots {
		out[id] = *sn
	}
	return out
}

// ErrorCode maps a client error to a stable code for the API and the web.
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, downloaders.ErrAuth):
		return "auth_failed"
	case errors.Is(err, downloaders.ErrTLS):
		return "tls_untrusted"
	case errors.Is(err, downloaders.ErrBlocked):
		return "address_blocked"
	case errors.Is(err, downloaders.ErrWrongURL):
		return "wrong_url"
	case errors.Is(err, downloaders.ErrUnreachable):
		return "unreachable"
	case errors.Is(err, downloaders.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, downloaders.ErrInvalidRequest), errors.Is(err, ErrInvalid):
		return "invalid"
	}
	return "client_error"
}

// Poll polls every enabled client once and updates the tracked downloads.
func (s *Service) Poll(ctx context.Context) error {
	s.pollMu.Lock()
	defer s.pollMu.Unlock()
	clients, err := s.Clients(ctx)
	if err != nil {
		return err
	}
	mappings, err := s.Mappings(ctx)
	if err != nil {
		return err
	}
	stalled, err := settings.Get(ctx, s.Settings, StalledHours)
	if err != nil {
		stalled = StalledHours.Default
	}
	var wg sync.WaitGroup
	for _, c := range clients {
		if !c.Enabled {
			continue
		}
		wg.Add(1)
		go func(c Client) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := s.pollClient(cctx, c, mappings, time.Duration(stalled)*time.Hour); err != nil && ctx.Err() == nil {
				s.Log.Debug("poll download client", "client", c.Name, "err", err)
			}
		}(c)
	}
	wg.Wait()
	return nil
}

func (s *Service) snapshot(id int64) *Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	sn, ok := s.snapshots[id]
	if !ok {
		sn = &Snapshot{ClientID: id}
		s.snapshots[id] = sn
	}
	return sn
}

func (s *Service) setSnapshot(id int64, fn func(*Snapshot)) {
	sn := s.snapshot(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(sn)
}

func (s *Service) pollClient(ctx context.Context, c Client, mappings []Mapping, stalledAfter time.Duration) error {
	cl, err := s.connect(ctx, c)
	fail := func(err error) error {
		s.setSnapshot(c.ID, func(sn *Snapshot) {
			sn.CheckedAt, sn.OK, sn.ErrorCode, sn.Error = s.now(), false, ErrorCode(err), err.Error()
		})
		return err
	}
	if err != nil {
		return fail(err)
	}
	if sn := s.snapshot(c.ID); s.now().Sub(sn.TestedAt) > testEvery || !sn.OK {
		st, err := cl.Test(ctx)
		if err != nil {
			return fail(err)
		}
		s.setSnapshot(c.ID, func(sn *Snapshot) { sn.Version, sn.FreeSpace, sn.TestedAt = st.Version, st.FreeSpace, s.now() })
	}
	caps := cl.Capabilities()
	cats := c.CategorySet()
	byCategory := (caps.Categories || caps.DirectoryCategories) && len(cats) > 0
	var items []downloaders.Item
	if byCategory {
		for _, cat := range cats {
			got, err := cl.List(ctx, cat)
			if err != nil {
				return fail(err)
			}
			items = append(items, got...)
		}
	} else {
		if items, err = cl.List(ctx, ""); err != nil {
			return fail(err)
		}
	}
	tracked, err := s.trackedFor(ctx, c.ID)
	if err != nil {
		return err
	}
	index := func(items []downloaders.Item) map[string]downloaders.Item {
		m := make(map[string]downloaders.Item, len(items))
		for _, it := range items {
			m[strings.ToLower(it.ID)] = it
		}
		return m
	}
	found := index(items)
	if byCategory {
		// A tracked item whose category changed in the client is still ours.
		for _, d := range tracked {
			if _, ok := found[strings.ToLower(d.ClientItemID)]; !ok {
				all, err := cl.List(ctx, "")
				if err != nil {
					return fail(err)
				}
				for k, v := range index(all) {
					if _, dup := found[k]; !dup {
						found[k] = v
					}
				}
				break
			}
		}
	}
	ours := make(map[string]bool, len(tracked))
	for _, d := range tracked {
		ours[strings.ToLower(d.ClientItemID)] = true
		it, ok := found[strings.ToLower(d.ClientItemID)]
		var itp *downloaders.Item
		if ok {
			itp = &it
		}
		if err := s.update(ctx, c, cl, d, itp, mappings, stalledAfter); err != nil {
			s.Log.Warn("update download", "download", d.ID, "err", err)
		}
	}
	var unknown []downloaders.Item
	if byCategory {
		for _, it := range items {
			if !ours[strings.ToLower(it.ID)] {
				unknown = append(unknown, it)
			}
		}
	}
	s.setSnapshot(c.ID, func(sn *Snapshot) {
		sn.CheckedAt, sn.OK, sn.ErrorCode, sn.Error, sn.Unknown = s.now(), true, "", "", unknown
	})
	return nil
}

// trackedFor returns the downloads of a client that are still followed.
func (s *Service) trackedFor(ctx context.Context, clientID int64) ([]Download, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+downloadColumns+` FROM downloads WHERE client_id = ?
		AND (status IN ('active', 'completed') OR (status IN ('imported', 'failed') AND missing_count < 2))`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Download
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func copyItem(d *Download, it downloaders.Item) {
	d.State, d.RawState, d.Progress = it.State, it.RawState, it.Progress
	d.Size, d.Downloaded = it.Size, it.Downloaded
	d.DownloadSpeed, d.UploadSpeed, d.ETA = it.DownloadSpeed, it.UploadSpeed, it.ETA
	d.Ratio, d.SeedingTime = it.Ratio, it.SeedingTime
	d.SavePath, d.ContentPath = it.SavePath, it.ContentPath
}

// update applies one poll result to one download.
func (s *Service) update(ctx context.Context, c Client, cl downloaders.Client, d Download, it *downloaders.Item,
	mappings []Mapping, stalledAfter time.Duration) error {
	now := s.now()
	before := d
	var events []Event
	type rec struct {
		typ      EventType
		from, to string
		msg      string
	}
	var recs []rec

	if it == nil {
		d.MissingCount++
		if d.MissingCount < 2 {
			d.UpdatedAt = now
			return save(ctx, s.DB.W, &d)
		}
		recs = append(recs, rec{EventRemoved, string(d.State), "", "removed in the client"})
		if d.Status == StatusActive || d.Status == StatusCompleted {
			d.Status = StatusRemoved
			d.FinishedAt = &now
			events = append(events, Event{Type: EventRemoved, Reason: "removed_in_client", Message: "removed in the client"})
		}
	} else {
		d.MissingCount = 0
		copyItem(&d, *it)
		if d.State != before.State {
			recs = append(recs, rec{EventState, string(before.State), string(d.State), it.Message})
		}
		// Quarter milestones for the per-title timeline.
		if d.Status == StatusActive && d.Progress < 1 && int(d.Progress*4) > int(before.Progress*4) {
			recs = append(recs, rec{EventProgress, string(d.State), string(d.State), fmt.Sprintf("%d%% downloaded", int(d.Progress*4)*25)})
		}
		msg := it.Message
		if d.Status == StatusActive {
			switch {
			case it.State == downloaders.StateFailed:
				reason := "client_failed"
				if strings.Contains(strings.ToLower(it.RawState), "missing") {
					reason = "missing_files"
				}
				if msg == "" {
					msg = "the client reports the download failed (" + it.RawState + ")"
				}
				d.Status, d.FinishedAt = StatusFailed, &now
				recs = append(recs, rec{EventFailed, string(before.State), string(d.State), msg})
				events = append(events, Event{Type: EventFailed, Reason: reason, Message: msg})
			case (it.State == downloaders.StateSeeding || it.State == downloaders.StateCompleted ||
				(it.State == downloaders.StatePaused && it.Progress >= 1)) && it.Progress >= 1 && !it.Busy:
				p := it.ContentPath
				if p == "" {
					p = it.SavePath
				}
				local, err := s.resolveLocal(mappings, c.Host, p)
				if err != nil {
					msg = fmt.Sprintf("finished, but %s is not visible to Locarr (add a remote path mapping or fix the /data mount)", p)
					d.LocalPath = ""
				} else {
					d.LocalPath = local
					d.Status, d.CompletedAt = StatusCompleted, &now
					msg = ""
					recs = append(recs, rec{EventCompleted, string(before.State), string(d.State), "ready to import"})
					events = append(events, Event{Type: EventCompleted, Message: "ready to import"})
				}
			case d.Protocol == downloaders.Torrent && it.State == downloaders.StateDownloading && it.Progress < 1 &&
				it.DownloadSpeed == 0 && it.Seeds <= 0 && stalledAfter > 0:
				if d.StalledSince == nil {
					d.StalledSince = &now
				} else if now.Sub(*d.StalledSince) >= stalledAfter {
					msg = fmt.Sprintf("stalled: nothing downloaded and no seeds for %s", stalledAfter)
					d.Status, d.FinishedAt = StatusFailed, &now
					recs = append(recs, rec{EventFailed, string(before.State), string(d.State), msg})
					events = append(events, Event{Type: EventFailed, Reason: "stalled", Message: msg})
				}
			default:
				if it.DownloadSpeed > 0 || it.State != downloaders.StateDownloading {
					d.StalledSince = nil
				}
			}
		}
		if d.Status == StatusImported && c.RemoveCompleted && s.seedGoalMet(d, *it) {
			if err := s.removeFromClient(ctx, cl, d, true); err != nil {
				msg = "could not remove after import: " + err.Error()
			} else {
				d.MissingCount = 2
				recs = append(recs, rec{EventRemoved, string(d.State), "", "removed after import"})
				events = append(events, Event{Type: EventRemoved, Reason: "removed_after_import", Message: "removed after import"})
			}
		}
		d.Message = msg
	}
	d.UpdatedAt = now
	err := s.DB.Tx(ctx, func(tx *sqlTx) error {
		if err := save(ctx, tx, &d); err != nil {
			return err
		}
		for _, r := range recs {
			if err := s.addEvent(ctx, tx, d.ID, r.typ, r.from, r.to, r.msg, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, ev := range events {
		ev.Download = d
		s.Bus.Publish(ctx, ev)
	}
	return nil
}

// seedGoalMet reports whether an imported download may leave the client.
func (s *Service) seedGoalMet(d Download, it downloaders.Item) bool {
	if d.Protocol == downloaders.Usenet {
		return true
	}
	if it.SeedGoalReached {
		return true
	}
	if d.SeedRatio == nil && d.SeedTime == nil {
		// No goal of our own: wait until the client stops seeding by its own rules.
		return it.State == downloaders.StateCompleted || it.State == downloaders.StatePaused
	}
	if d.SeedRatio != nil && *d.SeedRatio >= 0 && it.Ratio >= *d.SeedRatio {
		return true
	}
	return d.SeedTime != nil && it.SeedingTime >= *d.SeedTime
}

// removeFromClient removes the item and, when asked and the client cannot,
// deletes its content locally.
func (s *Service) removeFromClient(ctx context.Context, cl downloaders.Client, d Download, deleteData bool) error {
	if err := cl.Remove(ctx, d.ClientItemID, deleteData); err != nil && !errors.Is(err, downloaders.ErrItemNotFound) {
		return err
	}
	if deleteData && !cl.Capabilities().DeletesData {
		p := d.LocalPath
		if p == "" && d.ContentPath != "" {
			mappings, err := s.Mappings(ctx)
			if err != nil {
				return err
			}
			c, err := s.clientOf(ctx, d)
			if err == nil {
				p = Translate(mappings, c.Host, d.ContentPath)
			}
		}
		if p != "" {
			if err := s.deleteLocal(ctx, p); err != nil {
				return fmt.Errorf("removed from the client, but deleting the files failed: %w", err)
			}
		}
	}
	return nil
}

func (s *Service) clientOf(ctx context.Context, d Download) (Client, error) {
	if d.ClientID == nil {
		return Client{}, fmt.Errorf("%w: the download client was deleted", ErrNotFound)
	}
	return s.GetClient(ctx, *d.ClientID)
}
