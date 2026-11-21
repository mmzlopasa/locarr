package library

import "time"

// Availability is the earliest stage at which a movie is searched for.
type Availability string

// Minimum availabilities.
const (
	Announced Availability = "announced"
	InCinemas Availability = "in_cinemas"
	Released  Availability = "released"
)

// ValidAvailability reports a known value.
func ValidAvailability(a Availability) bool {
	return a == Announced || a == InCinemas || a == Released
}

// TheatricalToRelease is assumed between the cinema release and the home
// release when the metadata knows no digital or physical date (D-031).
const TheatricalToRelease = 90 * 24 * time.Hour

// ReleaseDates are a movie's known dates (zero = unknown).
type ReleaseDates struct {
	InCinemas time.Time
	Digital   time.Time
	Physical  time.Time
	// Released is the provider's status says the movie is out.
	Released bool
}

// ParseDate reads YYYY-MM-DD (zero time when empty or malformed).
func ParseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func earliest(ts ...time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if !t.IsZero() && (out.IsZero() || t.Before(out)) {
			out = t
		}
	}
	return out
}

// AvailableFrom returns when a movie reaches minimum (zero when it is unknown),
// shifted by delay (which may be negative to search early).
func AvailableFrom(d ReleaseDates, minimum Availability, delay time.Duration) time.Time {
	var at time.Time
	switch minimum {
	case Announced:
		return time.Unix(0, 0).UTC()
	case InCinemas:
		at = earliest(d.InCinemas, d.Digital, d.Physical)
	default:
		at = earliest(d.Digital, d.Physical)
		if at.IsZero() && !d.InCinemas.IsZero() {
			at = d.InCinemas.Add(TheatricalToRelease)
		}
	}
	if at.IsZero() {
		return at
	}
	return at.Add(delay)
}

// IsAvailable reports whether a movie with these dates has reached minimum at now.
// A movie whose provider says "Released" but has no dates counts as available.
func IsAvailable(d ReleaseDates, minimum Availability, delay time.Duration, now time.Time) bool {
	at := AvailableFrom(d, minimum, delay)
	if at.IsZero() {
		return d.Released
	}
	return !now.Before(at)
}
