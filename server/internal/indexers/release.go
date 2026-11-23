// Package indexers manages Torznab and Newznab indexers (directly or imported
// from Prowlarr or Jackett), searches them in parallel, syncs their RSS feeds
// and keeps their health. Releases come out raw: interpreting release names
// and deciding what to grab belongs to the parser and decision packages.
//
// Consumers use:
//
//	Service.Search / Service.SearchStream   interactive and automatic searches
//	Service.SubscribeRSS                    new releases found by the RSS sync
//	Service.SeenSince                       the same releases from the database
//	Service.Fetch                           the .torrent/.nzb of a release (counts a grab)
package indexers

import (
	"time"
)

// Protocol is how a release is downloaded.
type Protocol string

// Protocols.
const (
	Torrent Protocol = "torrent"
	Usenet  Protocol = "usenet"
)

// Kind is the API an indexer speaks.
type Kind string

// Indexer kinds.
const (
	Torznab Kind = "torznab"
	Newznab Kind = "newznab"
	// Native is a built-in (or custom) site definition searched directly.
	Native Kind = "native"
)

// stored is the kind written to the database: a native site is stored as a
// torrent (Torznab-like) indexer with its definition next to it.
func (k Kind) stored() Kind {
	if k == Native {
		return Torznab
	}
	return k
}

// Protocol returns the protocol of an indexer kind.
func (k Kind) Protocol() Protocol {
	if k == Newznab {
		return Usenet
	}
	return Torrent
}

// Release is one result from an indexer, as the indexer described it. Zero
// values and nil pointers mean "not given".
type Release struct {
	GUID            string    `json:"guid"`
	Title           string    `json:"title"`
	IndexerID       int64     `json:"indexerId"`
	IndexerName     string    `json:"indexerName"`
	IndexerPriority int       `json:"indexerPriority"`
	Protocol        Protocol  `json:"protocol"`
	Size            int64     `json:"size,omitempty"`
	Files           *int      `json:"files,omitempty"`
	Grabs           *int      `json:"grabs,omitempty"`
	Seeders         *int      `json:"seeders,omitempty"`
	Leechers        *int      `json:"leechers,omitempty"`
	Peers           *int      `json:"peers,omitempty"`
	PublishDate     time.Time `json:"publishDate,omitzero"`
	Categories      []int     `json:"categories"`
	// DownloadURL is the .torrent/.nzb link with the indexer's API key replaced
	// by a placeholder; fetch it with Service.Fetch, never directly.
	DownloadURL string `json:"downloadUrl,omitempty"`
	MagnetURL   string `json:"magnetUrl,omitempty"`
	InfoHash    string `json:"infoHash,omitempty"` // lower-case hex
	InfoURL     string `json:"infoUrl,omitempty"`  // details page

	IMDBID    string `json:"imdbId,omitempty"` // "tt0123456"
	TMDBID    int64  `json:"tmdbId,omitempty"`
	TVDBID    int64  `json:"tvdbId,omitempty"`
	TVMazeID  int64  `json:"tvmazeId,omitempty"`
	RageID    int64  `json:"rageId,omitempty"`
	Season    string `json:"season,omitempty"`
	Episode   string `json:"episode,omitempty"`
	Year      int    `json:"year,omitempty"`
	Artist    string `json:"artist,omitempty"`
	Album     string `json:"album,omitempty"`
	Author    string `json:"author,omitempty"`
	BookTitle string `json:"bookTitle,omitempty"`
	Group     string `json:"group,omitempty"`

	DownloadVolumeFactor *float64      `json:"downloadVolumeFactor,omitempty"`
	UploadVolumeFactor   *float64      `json:"uploadVolumeFactor,omitempty"`
	MinimumRatio         *float64      `json:"minimumRatio,omitempty"`
	MinimumSeedTime      time.Duration `json:"minimumSeedTime,omitempty"`
	// Flags are lower-case markers: freeleech, halfleech, doubleupload, plus
	// any tags the indexer sent (internal, scene, …).
	Flags []string `json:"flags"`
	// Attrs are every Newznab/Torznab attribute as sent (lower-case names).
	Attrs map[string][]string `json:"attrs,omitempty"`
}

// Key is the de-duplication key: the info hash when known, else indexer + GUID.
func (r *Release) Key() string {
	if r.InfoHash != "" {
		return "h:" + r.InfoHash
	}
	return "g:" + r.GUID
}

// SearchType is the kind of search.
type SearchType string

// Search types.
const (
	SearchAny   SearchType = "search"
	SearchTV    SearchType = "tv"
	SearchMovie SearchType = "movie"
	SearchMusic SearchType = "music"
	SearchBook  SearchType = "book"
)

// Source says who started a search.
type Source string

// Search sources.
const (
	SourceInteractive Source = "interactive"
	SourceAutomatic   Source = "automatic"
	SourceRSS         Source = "rss"
	SourceTest        Source = "test"
)

// Query is a search over several indexers.
type Query struct {
	Type       SearchType
	Term       string
	Categories []int // empty: each indexer's categories for Type
	IndexerIDs []int64
	IMDBID     string
	TMDBID     int64
	TVDBID     int64
	TVMazeID   int64
	Season     string
	Episode    string
	Artist     string
	Album      string
	Author     string
	Title      string
	Limit      int
	Offset     int

	Source  Source // default interactive
	ItemRef string // optional, e.g. "movie:42"; recorded in the search log
	UserID  int64  // optional, recorded in the search log
}

// Status is how one indexer did in a search.
type Status string

// Indexer search statuses.
const (
	StatusOK          Status = "ok"
	StatusError       Status = "error"
	StatusSkipped     Status = "skipped"
	StatusRateLimited Status = "rate_limited"
	StatusBackoff     Status = "backoff"
	StatusUnsupported Status = "unsupported"
)

// IndexerResult is one indexer's part of a search.
type IndexerResult struct {
	IndexerID   int64
	IndexerName string
	Status      Status
	Count       int
	Cached      bool
	Duration    time.Duration
	ErrorCode   string
	Error       string
}

// Event is one step of a streamed search: an indexer finished, with its
// releases that were not already sent.
type Event struct {
	Indexer  IndexerResult
	Releases []Release
}

// Result is a finished search.
type Result struct {
	Releases []Release
	Indexers []IndexerResult
	Duration time.Duration
}
