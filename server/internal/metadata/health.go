package metadata

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrRateLimited is returned when the provider kept asking Locarr to slow down.
var ErrRateLimited = errors.New("the metadata provider is rate limiting Locarr")

// Provider states for health checks.
const (
	StateOK          = "ok"
	StateInvalidKey  = "invalid_key"
	StateRateLimited = "rate_limited"
	StateUnavailable = "unavailable"
)

// ProviderStatus is the outcome of a provider's latest request.
type ProviderStatus struct {
	Provider string
	State    string
	Error    string
	At       time.Time
	// Since is when the provider entered this state.
	Since time.Time
}

// Health remembers how each provider's latest request went (for the
// health checks). A nil *Health records nothing.
type Health struct {
	mu sync.Mutex
	m  map[string]ProviderStatus
}

// Record notes the outcome of one request. "Not found" and "no key" say
// nothing about the provider's health and are ignored.
func (h *Health) Record(provider string, err error) {
	if h == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrNoKey) {
		return
	}
	st := ProviderStatus{Provider: provider, State: StateOK, At: time.Now().UTC()}
	switch {
	case err == nil:
	case errors.Is(err, ErrInvalidKey):
		st.State, st.Error = StateInvalidKey, err.Error()
	case errors.Is(err, ErrRateLimited) || strings.Contains(err.Error(), "rate limited"):
		st.State, st.Error = StateRateLimited, err.Error()
	case errors.Is(err, ErrUnavailable):
		st.State, st.Error = StateUnavailable, err.Error()
	default:
		// Context cancellations and similar are not the provider's fault.
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m == nil {
		h.m = map[string]ProviderStatus{}
	}
	st.Since = st.At
	if prev, ok := h.m[provider]; ok && prev.State == st.State {
		st.Since = prev.Since
	}
	h.m[provider] = st
}

// Snapshot returns every provider's latest status, by name.
func (h *Health) Snapshot() []ProviderStatus {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]ProviderStatus, 0, len(h.m))
	for _, s := range h.m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}
