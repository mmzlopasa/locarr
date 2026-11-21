package series

import "time"

// Monitor options when adding a series (or applying one later).
const (
	MonitorAll      = "all"
	MonitorFuture   = "future"
	MonitorMissing  = "missing"
	MonitorExisting = "existing"
	MonitorFirst    = "first_season"
	MonitorLatest   = "latest_season"
	MonitorPilot    = "pilot"
	MonitorNone     = "none"
)

// MonitorOptions lists the options.
var MonitorOptions = []string{MonitorAll, MonitorFuture, MonitorMissing, MonitorExisting, MonitorFirst, MonitorLatest, MonitorPilot, MonitorNone}

// ValidMonitor reports a known option.
func ValidMonitor(o string) bool {
	for _, m := range MonitorOptions {
		if m == o {
			return true
		}
	}
	return false
}

// EpisodeState is what a monitor option looks at.
type EpisodeState struct {
	Season  int
	Number  int
	AirUTC  *time.Time
	HasFile bool
}

// ApplyMonitor decides which episodes and seasons an option monitors.
// Specials (season 0) are never monitored by an option (D-042). seasons lists
// every season number, also those without episodes yet.
func ApplyMonitor(option string, eps []EpisodeState, seasons []int, now time.Time) ([]bool, map[int]bool) {
	first, latest := 0, 0
	for _, n := range seasons {
		if n <= 0 {
			continue
		}
		if first == 0 || n < first {
			first = n
		}
		if n > latest {
			latest = n
		}
	}
	pilot, pilotNumber := -1, 0
	for i, e := range eps {
		if e.Season == first && (pilot < 0 || e.Number < pilotNumber) {
			pilot, pilotNumber = i, e.Number
		}
	}
	out := make([]bool, len(eps))
	for i, e := range eps {
		if e.Season == 0 {
			continue
		}
		switch option {
		case MonitorAll:
			out[i] = true
		case MonitorFuture:
			out[i] = e.AirUTC == nil || e.AirUTC.After(now)
		case MonitorMissing:
			out[i] = !e.HasFile
		case MonitorExisting:
			out[i] = e.HasFile
		case MonitorFirst:
			out[i] = e.Season == first
		case MonitorLatest:
			out[i] = e.Season == latest
		case MonitorPilot:
			out[i] = i == pilot
		}
	}
	monitored := map[int]bool{}
	withEpisodes := map[int]bool{}
	for i, e := range eps {
		withEpisodes[e.Season] = true
		if out[i] {
			monitored[e.Season] = true
		}
	}
	for _, n := range seasons {
		if n == 0 || withEpisodes[n] {
			monitored[n] = monitored[n] || false
			continue
		}
		// A season without episodes yet: monitored when the option looks forward.
		monitored[n] = option == MonitorAll || option == MonitorFuture || (option == MonitorLatest && n == latest) ||
			(option == MonitorFirst && n == first)
	}
	return out, monitored
}
