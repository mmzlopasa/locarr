package requests

import (
	"sort"
	"time"
)

// Rule is a quota: at most Limit requests in Days days (Limit 0 = unlimited).
type Rule struct {
	Limit int `json:"limit"`
	Days  int `json:"days"`
}

// Usage is how much of a quota is used.
type Usage struct {
	Rule
	Used      int
	Remaining int // meaningless when Unlimited
	Unlimited bool
	Custom    bool
	// NextSlot is when the oldest counted request stops counting (only set
	// when nothing remains).
	NextSlot *time.Time
}

// Compute counts the requests made at the given times against rule at now.
func Compute(rule Rule, made []time.Time, now time.Time) Usage {
	u := Usage{Rule: rule}
	if rule.Days <= 0 {
		u.Days = 7
	}
	if rule.Limit <= 0 {
		u.Unlimited = true
	}
	from := now.Add(-time.Duration(u.Days) * 24 * time.Hour)
	var in []time.Time
	for _, t := range made {
		if t.After(from) {
			in = append(in, t)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Before(in[j]) })
	u.Used = len(in)
	if u.Unlimited {
		return u
	}
	u.Remaining = max(0, rule.Limit-u.Used)
	if u.Remaining == 0 && len(in) > 0 {
		// The (Used-Limit+1)th oldest must drop out for one slot to open.
		t := in[u.Used-rule.Limit].Add(time.Duration(u.Days) * 24 * time.Hour)
		u.NextSlot = &t
	}
	return u
}
