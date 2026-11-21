package metadata

import (
	"context"
	"strings"
	"time"

	_ "time/tzdata" // network time zones work in a minimal image

	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Series statuses (normalised from the provider's words).
const (
	SeriesContinuing = "continuing"
	SeriesEnded      = "ended"
	SeriesUpcoming   = "upcoming"
)

// TTLs of cached series answers.
const (
	TTLSeries = 12 * time.Hour
	TTLSeason = 12 * time.Hour
)

// SeriesSource picks who answers series lookups: "tmdb" (default) or "tvdb"
// (needs a TVDB key). Series keep the provider they were added with.
var SeriesSource = settings.Key[string]{Name: "metadata.seriesProvider", Default: "tmdb"}

// SeriesSummary is a series search result.
type SeriesSummary struct {
	TMDbID        int64   `json:"tmdbId"`
	TVDbID        int64   `json:"tvdbId"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"originalTitle"`
	Year          int     `json:"year"`
	FirstAired    string  `json:"firstAired"`
	Overview      string  `json:"overview"`
	Poster        string  `json:"poster"`
	Backdrop      string  `json:"backdrop"`
	Popularity    float64 `json:"popularity"`
	Rating        float64 `json:"rating"`
	Network       string  `json:"network"`
	Status        string  `json:"status"`
}

// Episode is one episode as the provider numbers it.
type Episode struct {
	Season   int    `json:"season"`
	Number   int    `json:"number"`
	Absolute int    `json:"absolute"` // 0 = the provider does not say
	TMDbID   int64  `json:"tmdbId"`
	TVDbID   int64  `json:"tvdbId"`
	Title    string `json:"title"`
	Overview string `json:"overview"`
	// AirDate is the day it airs in the network's time zone (YYYY-MM-DD).
	AirDate string `json:"airDate"`
	Runtime int    `json:"runtime"`
}

// Season is one season with its episodes (season 0 holds the specials).
type Season struct {
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	Overview string    `json:"overview"`
	Poster   string    `json:"poster"`
	AirDate  string    `json:"airDate"`
	Episodes []Episode `json:"episodes"`
}

// Series is a full series record with every season and episode.
type Series struct {
	SeriesSummary
	Source           string   `json:"source"` // tmdb, tvdb
	IMDbID           string   `json:"imdbId"`
	TVMazeID         int64    `json:"tvmazeId"`
	OriginalLanguage string   `json:"originalLanguage"`
	AlternateTitles  []string `json:"alternateTitles"`
	TimeZone         string   `json:"timeZone"`
	AirTime          string   `json:"airTime"` // HH:MM, "" = unknown
	Runtime          int      `json:"runtime"`
	Genres           []string `json:"genres"`
	Certification    string   `json:"certification"`
	Votes            int      `json:"votes"`
	LastAired        string   `json:"lastAired"`
	Seasons          []Season `json:"seasons"`
}

// SeriesProvider looks series up.
type SeriesProvider interface {
	// Name is "tmdb" or "tvdb".
	Name() string
	SearchSeries(ctx context.Context, query string, year int) ([]SeriesSummary, error)
	// Series returns a series by the provider's own id, with every episode.
	Series(ctx context.Context, id int64) (Series, error)
	// FindSeries returns the provider's id for an external id (source: imdb, tvdb, tmdb).
	FindSeries(ctx context.Context, source, id string) (int64, error)
}

// NormaliseSeriesStatus maps provider words to continuing/ended/upcoming.
func NormaliseSeriesStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ended", "canceled", "cancelled":
		return SeriesEnded
	case "in production", "planned", "pilot", "upcoming":
		return SeriesUpcoming
	}
	return SeriesContinuing
}

// countryZones is the main time zone of a broadcaster's country (only used
// when the provider names no zone; countries spanning several zones use the
// one most networks broadcast from).
var countryZones = map[string]string{
	"US": "America/New_York", "CA": "America/Toronto", "GB": "Europe/London", "IE": "Europe/Dublin", "HR": "Europe/Zagreb",
	"RS": "Europe/Belgrade", "BA": "Europe/Sarajevo", "SI": "Europe/Ljubljana", "DE": "Europe/Berlin", "AT": "Europe/Vienna",
	"CH": "Europe/Zurich", "FR": "Europe/Paris", "BE": "Europe/Brussels", "NL": "Europe/Amsterdam", "ES": "Europe/Madrid",
	"PT": "Europe/Lisbon", "IT": "Europe/Rome", "DK": "Europe/Copenhagen", "SE": "Europe/Stockholm", "NO": "Europe/Oslo",
	"FI": "Europe/Helsinki", "IS": "Atlantic/Reykjavik", "PL": "Europe/Warsaw", "CZ": "Europe/Prague", "HU": "Europe/Budapest",
	"RO": "Europe/Bucharest", "GR": "Europe/Athens", "TR": "Europe/Istanbul", "RU": "Europe/Moscow", "UA": "Europe/Kyiv",
	"JP": "Asia/Tokyo", "KR": "Asia/Seoul", "CN": "Asia/Shanghai", "TW": "Asia/Taipei", "HK": "Asia/Hong_Kong",
	"IN": "Asia/Kolkata", "TH": "Asia/Bangkok", "AU": "Australia/Sydney", "NZ": "Pacific/Auckland", "BR": "America/Sao_Paulo",
	"MX": "America/Mexico_City", "AR": "America/Argentina/Buenos_Aires", "CO": "America/Bogota", "ZA": "Africa/Johannesburg",
	"IL": "Asia/Jerusalem",
}

// CountryZone returns the time zone for a country code ("" when unknown).
func CountryZone(country string) string { return countryZones[strings.ToUpper(country)] }

// DefaultAirTime is assumed when the provider has no air time (D-046).
const DefaultAirTime = "20:00"

// AirTimeUTC turns an air date in a network's zone (and an air time, "" =
// DefaultAirTime) into the moment it airs. ok is false without a date.
func AirTimeUTC(date, airTime, zone string) (time.Time, bool) {
	if len(date) < 10 {
		return time.Time{}, false
	}
	if airTime == "" {
		airTime = DefaultAirTime
	}
	loc := time.UTC
	if zone != "" {
		if l, err := time.LoadLocation(zone); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", date[:10]+" "+normTime(airTime), loc)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02", date[:10], loc)
		if err != nil {
			return time.Time{}, false
		}
		t = t.Add(20 * time.Hour)
	}
	return t.UTC(), true
}

// normTime turns "9:00 PM", "21:00" or "21:00:00" into "21:00".
func normTime(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	for _, layout := range []string{"15:04", "15:04:05", "3:04 PM", "3:04PM", "03:04 PM"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("15:04")
		}
	}
	return s
}
