package metadata

import (
	"context"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// MusicBrainzContact is the contact sent in the User-Agent (MusicBrainz asks
// every client to identify itself with a way to reach its operator).
var MusicBrainzContact = settings.Key[string]{Name: "metadata.musicbrainzContact", Default: ""}

// TTLs of cached music answers.
const (
	TTLArtist   = 24 * time.Hour
	TTLReleases = 12 * time.Hour
	TTLRelease  = 7 * 24 * time.Hour // a release's track list rarely changes
)

// Album primary types, secondary types and release statuses as MusicBrainz names them.
var (
	PrimaryTypes   = []string{"album", "ep", "single", "broadcast", "other"}
	SecondaryTypes = []string{"studio", "compilation", "soundtrack", "spokenword", "interview", "audiobook", "audio drama", "live",
		"remix", "dj-mix", "mixtape/street", "demo", "field recording"}
	ReleaseStatuses = []string{"official", "promotion", "bootleg", "pseudo-release", "withdrawn", "cancelled"}
)

// Alias is another name of an artist.
type Alias struct {
	Name     string `json:"name"`
	SortName string `json:"sortName"`
	Locale   string `json:"locale,omitempty"`
	Type     string `json:"type,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
}

// ArtistSummary is a search result.
type ArtistSummary struct {
	MBID           string `json:"mbid"`
	Name           string `json:"name"`
	SortName       string `json:"sortName"`
	Disambiguation string `json:"disambiguation"`
	Type           string `json:"type"` // Person, Group, Orchestra, Choir, Character, Other
	Country        string `json:"country"`
	Begin          string `json:"begin,omitempty"`
	End            string `json:"end,omitempty"`
	Score          int    `json:"score"`
}

// Artist is a full artist record.
type Artist struct {
	ArtistSummary
	Aliases []Alias  `json:"aliases"`
	Genres  []string `json:"genres"`
}

// ReleaseGroup is an album in the library's sense (all editions of it).
type ReleaseGroup struct {
	MBID             string   `json:"mbid"`
	Title            string   `json:"title"`
	PrimaryType      string   `json:"primaryType"`    // lower case; "" = unknown
	SecondaryTypes   []string `json:"secondaryTypes"` // lower case
	FirstReleaseDate string   `json:"firstReleaseDate"`
	Disambiguation   string   `json:"disambiguation"`
}

// Medium is one disc (or other medium) of a release.
type Medium struct {
	Position   int     `json:"position"`
	Format     string  `json:"format"`
	Title      string  `json:"title"`
	TrackCount int     `json:"trackCount"`
	Tracks     []Track `json:"tracks,omitempty"`
}

// Track is one track of a medium.
type Track struct {
	MBID          string `json:"mbid"`
	RecordingMBID string `json:"recordingMbid"`
	Number        string `json:"number"`
	Position      int    `json:"position"`
	Title         string `json:"title"`
	LengthMs      int    `json:"lengthMs"`
	Artist        string `json:"artist"`
}

// Release is one edition of a release group.
type Release struct {
	MBID             string   `json:"mbid"`
	ReleaseGroupMBID string   `json:"releaseGroupMbid"`
	Title            string   `json:"title"`
	Status           string   `json:"status"` // lower case
	Date             string   `json:"date"`
	Country          string   `json:"country"`
	Disambiguation   string   `json:"disambiguation"`
	Barcode          string   `json:"barcode"`
	Label            string   `json:"label"`
	CatalogNumber    string   `json:"catalogNumber"`
	Media            []Medium `json:"media"`
	// HasTracks: Media carry their track lists (a full lookup).
	HasTracks bool `json:"hasTracks"`
}

// TrackCount is the number of tracks on all media.
func (r *Release) TrackCount() int {
	n := 0
	for _, m := range r.Media {
		n += m.TrackCount
	}
	return n
}

// MusicProvider looks artists, albums and releases up (MusicBrainz).
type MusicProvider interface {
	SearchArtists(ctx context.Context, query string) ([]ArtistSummary, error)
	Artist(ctx context.Context, mbid string) (Artist, error)
	// ReleaseGroups lists every release group of an artist.
	ReleaseGroups(ctx context.Context, artistMBID string) ([]ReleaseGroup, error)
	// Releases lists the releases credited to an artist (media, no tracks).
	Releases(ctx context.Context, artistMBID string) ([]Release, error)
	// GroupReleases lists the releases of one release group (media, no tracks).
	GroupReleases(ctx context.Context, groupMBID string) ([]Release, error)
	// Release returns one release with its track lists.
	Release(ctx context.Context, mbid string) (Release, error)
}
