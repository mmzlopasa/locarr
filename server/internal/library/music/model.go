// Package music is the music library: MusicBrainz artists, albums (release
// groups) with their editions (releases) and track lists, metadata profiles
// that choose which albums an artist's library holds, the wanted source for
// album searches, the importer that matches a download's files to a release
// by tags, numbers, titles and durations, and the existing-library import.
// It plugs into the shared library core like movies do (D-028).
package music

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Errors.
var (
	ErrNotFound        = errors.New("not found in the music library")
	ErrExists          = errors.New("this artist is already in the library")
	ErrPathTaken       = errors.New("another library item uses this folder")
	ErrInvalid         = errors.New("invalid music request")
	ErrProfileInUse    = errors.New("the metadata profile is used by artists")
	ErrProfileNotFound = errors.New("metadata profile not found")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Monitor rules for new albums (and the initial choice when adding).
const (
	MonitorAll    = "all"
	MonitorNone   = "none"
	MonitorFuture = "future"
	// MonitorLatest (adding only) monitors the newest album.
	MonitorLatest = "latest"
)

// Settings.
var (
	ArtistFolderFormat   = settings.Key[string]{Name: "music.artistFolderFormat", Default: "{ArtistName}"}
	AlbumFolderFormat    = settings.Key[string]{Name: "music.albumFolderFormat", Default: "{AlbumTitle} ({ReleaseYear})"}
	TrackFormat          = settings.Key[string]{Name: "music.trackFormat", Default: "{TrackNumber} - {TrackTitle}"}
	MultiDiscTrackFormat = settings.Key[string]{Name: "music.multiDiscTrackFormat", Default: "{DiscNumber}-{TrackNumber} - {TrackTitle}"}
	DiscFolders          = settings.Key[bool]{Name: "music.discFolders", Default: false}
	DiscFolderFormat     = settings.Key[string]{Name: "music.discFolderFormat", Default: "Disc {DiscNumber}"}
	RenameTracks         = settings.Key[bool]{Name: "music.renameTracks", Default: true}
	ImportPartial        = settings.Key[bool]{Name: "music.importPartialAlbums", Default: false}
	WriteTags            = settings.Key[bool]{Name: "music.writeTags", Default: false}
	SaveCoverArt         = settings.Key[bool]{Name: "music.saveCoverArt", Default: true}
	MinimumMatchScore    = settings.Key[int]{Name: "music.minimumMatchScore", Default: 70}
)

// Artist is an artist in the library.
type Artist struct {
	ID                int64
	MBID              string
	Name              string
	SortName          string
	Disambiguation    string
	Type              string
	Country           string
	Aliases           []string
	Genres            []string
	Monitored         bool
	MonitorNewAlbums  string
	QualityProfileID  int64
	MetadataProfileID int64
	RootFolderID      *int64
	Path              string
	Tags              []string
	AddedAt           time.Time
	UpdatedAt         time.Time
	RefreshedAt       *time.Time
}

// Ref is the artist's item reference.
func (a *Artist) Ref() library.Ref { return library.Ref{Kind: library.RefArtist, ID: a.ID} }

// Album is a release group of an artist.
type Album struct {
	ID             int64
	ArtistID       int64
	MBID           string
	Title          string
	SortTitle      string
	Disambiguation string
	PrimaryType    string
	SecondaryTypes []string
	ReleaseDate    string
	Monitored      bool
	ReleaseID      *int64
	ReleaseLocked  bool
	Filtered       bool
	Path           string
	AddedAt        time.Time
	UpdatedAt      time.Time
}

// Ref is the album's item reference.
func (a *Album) Ref() library.Ref { return library.Ref{Kind: library.RefAlbum, ID: a.ID} }

// Year of the first release (0 = unknown).
func (a *Album) Year() int { return yearOf(a.ReleaseDate) }

func yearOf(d string) int {
	if len(d) >= 4 {
		y := 0
		for _, c := range d[:4] {
			if c < '0' || c > '9' {
				return 0
			}
			y = y*10 + int(c-'0')
		}
		return y
	}
	return 0
}

// ReleaseTime is when the album comes out: a partial date counts from the
// end of its month or year (zero when unknown).
func ReleaseTime(d string) time.Time {
	switch len(d) {
	case 10:
		t, _ := time.Parse("2006-01-02", d)
		return t
	case 7:
		t, err := time.Parse("2006-01", d)
		if err == nil {
			return t.AddDate(0, 1, -1)
		}
	case 4:
		t, err := time.Parse("2006", d)
		if err == nil {
			return t.AddDate(1, 0, -1)
		}
	}
	return time.Time{}
}

// Release is an edition of an album.
type Release struct {
	ID             int64
	AlbumID        int64
	MBID           string
	Title          string
	Status         string
	Date           string
	Country        string
	Disambiguation string
	Label          string
	CatalogNumber  string
	Barcode        string
	Formats        []string
	TrackCount     int
	MediaCount     int
	TracksLoaded   bool
}

// FormatSummary is "2×CD" or "Digital Media".
func (r *Release) FormatSummary() string {
	if len(r.Formats) == 0 {
		return ""
	}
	same := true
	for _, f := range r.Formats {
		if f != r.Formats[0] {
			same = false
		}
	}
	if same {
		if len(r.Formats) > 1 {
			return fmt.Sprintf("%d×%s", len(r.Formats), r.Formats[0])
		}
		return r.Formats[0]
	}
	return strings.Join(r.Formats, " + ")
}

// Track is one track of a release, with its file when the library has one.
type Track struct {
	ID            int64
	ReleaseID     int64
	MBID          string
	RecordingMBID string
	Disc          int
	Position      int
	Number        string
	Title         string
	LengthMs      int
	Artist        string
	File          *TrackFile
}

// TrackFile is a library file of an album.
type TrackFile struct {
	ID            int64 // media_files.id
	AlbumID       int64
	Path          string
	Size          int64
	Quality       string
	Score         int
	ReleaseGroup  string
	SceneName     string
	DownloadID    *int64
	AddedAt       time.Time
	Disc          int
	Position      int
	RecordingMBID string
	Title         string
	DurationMs    int
	Format        string
	BitDepth      int
	SampleRate    int
	Bitrate       int
}

func jsonList(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func jsonText(v any) string {
	b, _ := json.Marshal(v)
	if string(b) == "null" {
		return "[]"
	}
	return string(b)
}

func jsonObject(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
