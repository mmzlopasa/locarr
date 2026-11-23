// Package health runs the system health checks (D-083): indexers, download
// clients, remote path mappings, root folders and disk space, the recycle
// bin, stuck imports, metadata and subtitle providers, Streamero, the clock,
// the database, backups and newer Locarr versions. Every issue has a stable
// code the clients translate, English text for logs, an anchor in
// docs/user/troubleshooting.md and the page where it is fixed. Issues that
// appear (warnings and errors) are published once as health.warning.
package health

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// Issue is one problem found by a check.
type Issue struct {
	Key      string
	Check    string
	Severity string
	Code     string
	Message  string
	Params   map[string]any
	Docs     string
	FixRoute string
	Since    time.Time
}

// CheckFunc finds the issues of one area.
type CheckFunc func(ctx context.Context) ([]Issue, error)

// Report is the result of the last run.
type Report struct {
	CheckedAt time.Time
	Issues    []Issue
}

type check struct {
	name string
	fn   CheckFunc
}

// Service runs the checks and remembers the result.
type Service struct {
	Log *slog.Logger
	// OnNew is called once for each warning or error that was not there in
	// the previous run (the app publishes health.warning).
	OnNew func(ctx context.Context, is Issue)
	Now   func() time.Time

	checks []check

	mu      sync.Mutex
	last    Report
	since   map[string]time.Time
	ran     bool
	running sync.Mutex
	// status holds problems reported from outside (Streamero scans, backups).
	status map[string]Issue
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Register adds a check.
func (s *Service) Register(name string, fn CheckFunc) {
	s.checks = append(s.checks, check{name: name, fn: fn})
}

// SetStatus records (or with ok, clears) a problem reported by another part
// of the server; the "reported" check returns it.
func (s *Service) SetStatus(key string, ok bool, is Issue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == nil {
		s.status = map[string]Issue{}
	}
	if ok {
		delete(s.status, key)
		return
	}
	is.Key = key
	s.status[key] = is
}

func (s *Service) reported() []Issue {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Issue, 0, len(s.status))
	for _, is := range s.status {
		out = append(out, is)
	}
	return out
}

func severityRank(sv string) int {
	switch sv {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	}
	return 2
}

// Run runs every check now.
func (s *Service) Run(ctx context.Context) Report {
	s.running.Lock()
	defer s.running.Unlock()
	var issues []Issue
	for _, c := range s.checks {
		list, err := c.fn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			issues = append(issues, Issue{Key: "check:" + c.name, Check: c.name, Severity: SeverityWarning, Code: "check_failed",
				Message: "the " + c.name + " check failed: " + err.Error(), Params: map[string]any{"check": c.name, "error": err.Error()},
				Docs: "health-checks"})
			continue
		}
		for i := range list {
			if list[i].Check == "" {
				list[i].Check = c.name
			}
		}
		issues = append(issues, list...)
	}
	for _, is := range s.reported() {
		if is.Check == "" {
			is.Check = "reported"
		}
		issues = append(issues, is)
	}
	now := s.now().UTC()
	s.mu.Lock()
	if s.since == nil {
		s.since = map[string]time.Time{}
	}
	var fresh []Issue
	seen := map[string]bool{}
	for i := range issues {
		is := &issues[i]
		if is.Params == nil {
			is.Params = map[string]any{}
		}
		seen[is.Key] = true
		if t, ok := s.since[is.Key]; ok {
			is.Since = t
		} else {
			is.Since = now
			s.since[is.Key] = now
			if is.Severity != SeverityInfo {
				fresh = append(fresh, *is)
			}
		}
	}
	for k := range s.since {
		if !seen[k] {
			delete(s.since, k)
		}
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if severityRank(issues[i].Severity) != severityRank(issues[j].Severity) {
			return severityRank(issues[i].Severity) < severityRank(issues[j].Severity)
		}
		return issues[i].Key < issues[j].Key
	})
	if issues == nil {
		issues = []Issue{}
	}
	s.last = Report{CheckedAt: now, Issues: issues}
	s.ran = true
	rep := s.last
	s.mu.Unlock()
	if s.OnNew != nil {
		for _, is := range fresh {
			s.OnNew(ctx, is)
		}
	}
	return rep
}

// Last returns the last report and whether the checks ran at all.
func (s *Service) Last() (Report, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.ran
}

// Counts returns the number of current issues by severity.
func (s *Service) Counts() (errs, warnings, info int) {
	rep, _ := s.Last()
	for _, is := range rep.Issues {
		switch is.Severity {
		case SeverityError:
			errs++
		case SeverityWarning:
			warnings++
		default:
			info++
		}
	}
	return errs, warnings, info
}
