package indexers

import (
	"sync"
)

// bus hands releases that the RSS sync saw for the first time to in-process
// subscribers (the decision engine, the library). A subscriber that does not
// keep up loses batches rather than stalling the sync; every release is also
// in releases_seen, so it can catch up with SeenSince.
type bus struct {
	mu   sync.Mutex
	next int
	subs map[int]chan []Release
}

func newBus() *bus { return &bus{subs: map[int]chan []Release{}} }

func (b *bus) subscribe(buffer int) (<-chan []Release, func()) {
	if buffer < 1 {
		buffer = 16
	}
	ch := make(chan []Release, buffer)
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = ch
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			close(ch)
		})
	}
}

// publish sends a batch to every subscriber; it returns how many were full.
func (b *bus) publish(batch []Release) (dropped int) {
	if len(batch) == 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- batch:
		default:
			dropped++
		}
	}
	return dropped
}

// SubscribeRSS returns a channel of release batches the RSS sync saw for the
// first time, and a function that ends the subscription (closing the channel).
func (s *Service) SubscribeRSS(buffer int) (<-chan []Release, func()) {
	return s.bus.subscribe(buffer)
}
