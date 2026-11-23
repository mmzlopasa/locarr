package logging

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// Entry is one parsed log record.
type Entry struct {
	Seq     int64          `json:"seq"`
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"` // debug, info, warn, error
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields"`
}

// LevelRank orders levels; unknown levels rank as info.
func LevelRank(l string) int {
	switch strings.ToLower(l) {
	case "debug":
		return 0
	case "warn", "warning":
		return 2
	case "error":
		return 3
	}
	return 1
}

// Ring keeps the last records in memory for the log viewer and hands new ones
// to live subscribers. It is an io.Writer for the JSON handler: every Write is
// one record (already redacted).
type Ring struct {
	mu     sync.Mutex
	buf    []Entry
	next   int
	full   bool
	seq    int64
	subs   map[int]chan Entry
	subSeq int
}

// NewRing keeps the last size records.
func NewRing(size int) *Ring {
	if size <= 0 {
		size = 5000
	}
	return &Ring{buf: make([]Entry, size), subs: map[int]chan Entry{}}
}

// Write parses one JSON record.
func (r *Ring) Write(p []byte) (int, error) {
	var raw map[string]any
	if err := json.Unmarshal(p, &raw); err != nil {
		return len(p), nil
	}
	e := Entry{Fields: map[string]any{}}
	for k, v := range raw {
		switch k {
		case "time":
			if s, ok := v.(string); ok {
				e.Time, _ = time.Parse(time.RFC3339Nano, s)
			}
		case "level":
			s, _ := v.(string)
			e.Level = strings.ToLower(s)
			if e.Level == "warning" {
				e.Level = "warn"
			}
		case "msg":
			e.Message, _ = v.(string)
		default:
			e.Fields[k] = v
		}
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	r.mu.Lock()
	r.seq++
	e.Seq = r.seq
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	for _, ch := range r.subs {
		select {
		case ch <- e:
		default: // a slow reader misses records rather than slowing logging down
		}
	}
	r.mu.Unlock()
	return len(p), nil
}

// Filter selects entries.
type Filter struct {
	MinLevel string // "" = all
	Query    string // case-insensitive, in the message and the field values
}

// Match reports whether e passes f.
func (f Filter) Match(e Entry) bool {
	if f.MinLevel != "" && LevelRank(e.Level) < LevelRank(f.MinLevel) {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(e.Message), q) {
		return true
	}
	b, _ := json.Marshal(e.Fields)
	return strings.Contains(strings.ToLower(string(b)), q)
}

// Entries returns at most limit matching records, oldest first.
func (r *Ring) Entries(f Filter, limit int) []Entry {
	r.mu.Lock()
	all := make([]Entry, 0, len(r.buf))
	if r.full {
		all = append(all, r.buf[r.next:]...)
	}
	all = append(all, r.buf[:r.next]...)
	r.mu.Unlock()
	out := []Entry{}
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		if f.Match(all[i]) {
			out = append(out, all[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Subscribe returns a channel of new records and a function that ends the subscription.
func (r *Ring) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, 256)
	r.mu.Lock()
	r.subSeq++
	id := r.subSeq
	r.subs[id] = ch
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		delete(r.subs, id)
		r.mu.Unlock()
	}
}
