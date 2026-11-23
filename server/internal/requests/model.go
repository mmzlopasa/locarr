// Package requests lets people ask for titles (P8, replacing a separate
// request app): a movie, seasons of a series, an album or a whole artist.
// Users who manage requests approve or decline them; approval hands the title
// to the library (movies, series, music services), and the request follows
// the library until the title is available, telling the requester through
// notify (D-054 … D-057). Problems with available titles are reported as
// issues.
package requests

import (
	"errors"
	"fmt"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/notify"
)

// Kind is what a request asks for.
type Kind string

// Kinds.
const (
	KindMovie  Kind = "movie"
	KindSeries Kind = "series"
	KindAlbum  Kind = "album"
	KindArtist Kind = "artist"
)

// Valid reports a known kind.
func (k Kind) Valid() bool {
	return k == KindMovie || k == KindSeries || k == KindAlbum || k == KindArtist
}

// QuotaKind is the quota a kind counts against: movie, series or music.
func (k Kind) QuotaKind() string {
	switch k {
	case KindMovie:
		return QuotaMovie
	case KindSeries:
		return QuotaSeries
	}
	return QuotaMusic
}

// Quota kinds.
const (
	QuotaMovie  = "movie"
	QuotaSeries = "series"
	QuotaMusic  = "music"
)

// QuotaKinds lists the quota kinds.
var QuotaKinds = []string{QuotaMovie, QuotaSeries, QuotaMusic}

// Status is a request's state (see state.go).
type Status string

// Statuses.
const (
	StatusPending    Status = "pending"
	StatusApproved   Status = "approved"
	StatusDeclined   Status = "declined"
	StatusCancelled  Status = "cancelled"
	StatusProcessing Status = "processing"
	StatusPartial    Status = "partial"
	StatusAvailable  Status = "available"
	StatusFailed     Status = "failed"
)

// Errors (the API maps them to codes).
var (
	ErrNotFound         = errors.New("request not found")
	ErrForbidden        = errors.New("you are not allowed to do this")
	ErrInvalid          = errors.New("invalid request")
	ErrAlreadyRequested = errors.New("already requested")
	ErrAlreadyAvailable = errors.New("already available")
	ErrNothingToRequest = errors.New("nothing left to request")
	ErrNotPending       = errors.New("the request is not in a state that allows this")
	ErrDefaultsMissing  = errors.New("no quality profile or root folder is set for this kind of request (Settings → Requests)")
	ErrNotAvailable     = errors.New("the title is not available in the library")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// QuotaError is returned when a quota is used up.
type QuotaError struct {
	Kind     string
	Limit    int
	Days     int
	NextSlot *time.Time
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("request quota used up: %d %s requests in %d days", e.Limit, e.Kind, e.Days)
}

// Season is one requested season.
type Season struct {
	Number int
	Status Status
}

// Explanation is the library's current answer for the item (timeline.Explain).
type Explanation struct {
	Code    string         `json:"code"`
	Tone    string         `json:"tone"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params"`
}

// Progress sums up the active downloads of a request.
type Progress struct {
	Percent   float64
	Downloads int
	State     string
	ETA       *int64
}

// Request is a stored request.
type Request struct {
	ID            int64
	Kind          Kind
	Is4K          bool
	Status        Status
	TMDbID        int64
	MBID          string
	ArtistMBID    string
	Title         string
	Subtitle      string
	Year          int
	Poster        string
	AllSeasons    bool
	FutureSeasons bool
	Seasons       []Season
	RequestedBy   notify.User
	DecidedBy     *notify.User
	AutoApproved  bool
	DecidedAt     *time.Time
	DeclineReason string
	ProfileID     *int64
	RootFolderID  *int64
	Tags          []string
	ItemRef       string
	Explanation   *Explanation
	CreatedAt     time.Time
	UpdatedAt     time.Time
	AvailableAt   *time.Time
	CommentCount  int
	Progress      *Progress
}

// DisplayTitle is the title with its year ("Sintel (2010)").
func (r *Request) DisplayTitle() string {
	t := r.Title
	if r.Subtitle != "" && (r.Kind == KindAlbum) {
		t = r.Subtitle + " - " + t
	}
	if r.Year > 0 {
		t = fmt.Sprintf("%s (%d)", t, r.Year)
	}
	return t
}

// SeasonNumbers lists the requested season numbers.
func (r *Request) SeasonNumbers() []int {
	out := make([]int, 0, len(r.Seasons))
	for _, s := range r.Seasons {
		out = append(out, s.Number)
	}
	return out
}

// Comment is a comment on a request or issue.
type Comment struct {
	ID        int64
	User      notify.User
	Body      string
	CreatedAt time.Time
}
