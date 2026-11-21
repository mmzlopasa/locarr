package downloads

import (
	"context"
	"log/slog"
	"sync"
)

// EventType is what happened to a download.
type EventType string

// Bus events (and the matching download_events types).
const (
	EventGrabbed      EventType = "grabbed"
	EventState        EventType = "state"
	EventCompleted    EventType = "completed"
	EventFailed       EventType = "failed"
	EventRemoved      EventType = "removed"
	EventImported     EventType = "imported"
	EventImportFailed EventType = "import_failed"
	EventBlocklisted  EventType = "blocklisted"
	// EventProgress marks 25, 50 and 75 % (recorded, not published).
	EventProgress EventType = "progress"
)

// Event is published on the bus after the change is committed.
//
//   - EventCompleted (DownloadCompleted): the download is finished and its
//     content is visible locally at Download.LocalPath, ready to import.
//   - EventFailed (DownloadFailed): the client reported a failure, the files
//     are missing or the torrent stalled; Reason says which.
//   - EventRemoved (DownloadRemoved): removed by a user in Locarr or in the client.
//   - EventGrabbed: sent to a client.
type Event struct {
	Type     EventType
	Download Download
	// Reason is a stable code for failures and removals: client_failed,
	// missing_files, stalled, removed_in_client, removed_by_user, removed_after_import.
	Reason  string
	Message string
}

// Handler receives events. It runs on the tracker's goroutine: do quick work
// (for example enqueue an import job) and return.
type Handler func(ctx context.Context, ev Event)

// Bus is a small in-process publish/subscribe for download events.
type Bus struct {
	mu   sync.RWMutex
	subs map[int]Handler
	next int
	log  *slog.Logger
}

// Subscribe registers h for every event; the returned function unsubscribes.
func (b *Bus) Subscribe(h Handler) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs == nil {
		b.subs = map[int]Handler{}
	}
	id := b.next
	b.next++
	b.subs[id] = h
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
	}
}

// Publish delivers ev to every subscriber; a panicking subscriber is logged and skipped.
func (b *Bus) Publish(ctx context.Context, ev Event) {
	b.mu.RLock()
	hs := make([]Handler, 0, len(b.subs))
	for _, h := range b.subs {
		hs = append(hs, h)
	}
	b.mu.RUnlock()
	for _, h := range hs {
		func() {
			defer func() {
				if r := recover(); r != nil && b.log != nil {
					b.log.Error("download event handler panicked", "event", string(ev.Type), "panic", r)
				}
			}()
			h(ctx, ev)
		}()
	}
}
