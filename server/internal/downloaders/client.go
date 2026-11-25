// Package downloaders talks to download clients: five torrent clients
// (qBittorrent, Transmission, Deluge, rTorrent, aria2) and two usenet clients
// (SABnzbd, NZBGet), each in its own sub-package behind one interface. Every
// client is written from its public API documentation only (CONTRIBUTING.md).
package downloaders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Protocol is how a client downloads.
type Protocol string

// Protocols.
const (
	Torrent Protocol = "torrent"
	Usenet  Protocol = "usenet"
)

// State is a client item's state, normalised across clients. The client's own
// state is kept next to it (Item.RawState).
type State string

// Normalised states.
const (
	StateQueued      State = "queued"
	StateDownloading State = "downloading"
	StatePaused      State = "paused"
	StateSeeding     State = "seeding"
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
	StateWarning     State = "warning"
)

// Valid reports whether s is a known state.
func (s State) Valid() bool {
	switch s {
	case StateQueued, StateDownloading, StatePaused, StateSeeding, StateCompleted, StateFailed, StateWarning:
		return true
	}
	return false
}

// Priority is a queue position request.
type Priority string

// Priorities.
const (
	PriorityNormal Priority = "normal"
	PriorityTop    Priority = "top"
	PriorityLast   Priority = "last"
)

// Valid reports whether p is a known priority.
func (p Priority) Valid() bool { return p == PriorityNormal || p == PriorityTop || p == PriorityLast }

// Capabilities describe what a client can do, so callers (and the web forms)
// only offer what works.
type Capabilities struct {
	Protocol Protocol `json:"protocol"`
	// Categories: the client has categories or labels that group Locarr's items.
	Categories bool `json:"categories"`
	// CreateCategories: missing categories can be created through the API.
	CreateCategories bool `json:"createCategories"`
	// DirectoryCategories: the client has no categories, so each category is a
	// sub-folder of the client's download folder (aria2).
	DirectoryCategories bool `json:"directoryCategories"`
	Tags                bool `json:"tags"`
	SeedLimits          bool `json:"seedLimits"`
	Sequential          bool `json:"sequential"`
	FirstLastPiece      bool `json:"firstLastPiece"`
	Priority            bool `json:"priority"`
	SavePath            bool `json:"savePath"`
	FreeSpace           bool `json:"freeSpace"`
	// DeletesData: Remove(deleteData=true) deletes the downloaded files. When
	// false the caller deletes the item's content itself (through fs.SafePath).
	DeletesData bool `json:"deletesData"`
}

// AddRequest describes one download to add. Exactly one source is set.
type AddRequest struct {
	Magnet      string
	TorrentFile []byte
	TorrentURL  string
	NZB         []byte
	NZBURL      string
	// Name is the release title; usenet clients use it as the job name.
	Name     string
	Category string
	// SavePath overrides the client's download folder (as the client sees it).
	SavePath string
	Paused   bool
	Priority Priority
	// SeedRatio and SeedTime are seeding goals; nil = the client's default.
	SeedRatio      *float64
	SeedTime       *time.Duration
	Tags           []string
	Sequential     bool
	FirstLastPiece bool
}

// Validate checks that exactly one source is set and it matches the protocol.
func (r *AddRequest) Validate(p Protocol) error {
	n := 0
	for _, set := range []bool{r.Magnet != "", len(r.TorrentFile) > 0, r.TorrentURL != "", len(r.NZB) > 0, r.NZBURL != ""} {
		if set {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("%w: exactly one source (magnet, torrent, NZB or URL) is needed", ErrInvalidRequest)
	}
	torrent := r.Magnet != "" || len(r.TorrentFile) > 0 || r.TorrentURL != ""
	if torrent != (p == Torrent) {
		return fmt.Errorf("%w: a %s client cannot take this download", ErrInvalidRequest, p)
	}
	if r.Priority == "" {
		r.Priority = PriorityNormal
	}
	if !r.Priority.Valid() {
		return fmt.Errorf("%w: unknown priority %q", ErrInvalidRequest, r.Priority)
	}
	return nil
}

// File is one file of an item.
type File struct {
	Path     string  `json:"path"` // relative to the item's save path
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"` // 0..1
}

// Item is one download as the client reports it.
type Item struct {
	// ID is the client-side ID: the lower-case info hash for torrents, the
	// nzo_id (SABnzbd) or NZBID (NZBGet) for usenet.
	ID       string
	Name     string
	Category string
	State    State
	RawState string
	// Busy: the client is still checking, moving, repairing or unpacking the
	// files, so they are not ready to import even when the download is done.
	Busy          bool
	Progress      float64 // 0..1
	Size          int64
	Downloaded    int64
	Uploaded      int64
	DownloadSpeed int64 // bytes per second
	UploadSpeed   int64
	ETA           *time.Duration
	Ratio         float64
	SeedingTime   time.Duration
	// SavePath is the folder the item is saved in; ContentPath is the item's
	// file or folder inside it. Both as the client sees them.
	SavePath    string
	ContentPath string
	Message     string
	AddedAt     time.Time
	Tags        []string
	// Seeds is the number of connected seeds (torrents; -1 = unknown).
	Seeds int
	// SeedGoalReached: the client stopped seeding because a ratio or time limit was met.
	SeedGoalReached bool
	Files           []File
}

// Category is a client category with its folder (if the client has one).
type Category struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath,omitempty"`
}

// Status is what Test learns about a reachable client.
type Status struct {
	Version   string
	FreeSpace *int64 // bytes in the default download folder, when the client reports it
	// SavePath is the client's default download folder as the client sees it
	// ("" when its API does not say).
	SavePath string
}

// Client is a download client. Implementations are safe for concurrent use.
type Client interface {
	Capabilities() Capabilities
	// Test checks the connection and credentials.
	Test(ctx context.Context) (Status, error)
	// Add adds a download and returns its client-side ID.
	Add(ctx context.Context, req AddRequest) (string, error)
	// List returns the items in category ("" = all items).
	List(ctx context.Context, category string) ([]Item, error)
	// Files lists the files of one item.
	Files(ctx context.Context, id string) ([]File, error)
	Remove(ctx context.Context, id string, deleteData bool) error
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
	SetPriority(ctx context.Context, id string, p Priority) error
	Categories(ctx context.Context) ([]Category, error)
	// EnsureCategory creates the category when it is missing (ErrUnsupported when
	// the client cannot create categories through its API).
	EnsureCategory(ctx context.Context, name, savePath string) error
}

// Errors. Clients wrap them with details; callers test with errors.Is.
var (
	// ErrAuth: the client refused the credentials.
	ErrAuth = errors.New("authentication failed")
	// ErrUnreachable: no connection (refused, timeout, DNS, TLS).
	ErrUnreachable = errors.New("client unreachable")
	// ErrTLS: the certificate is not trusted (self-signed?).
	ErrTLS = errors.New("TLS certificate not trusted")
	// ErrWrongURL: something answered, but not the client's API (wrong URL base or port).
	ErrWrongURL = errors.New("no client API at this address")
	// ErrBlocked: the address is not allowed (link-local or cloud metadata).
	ErrBlocked = errors.New("address not allowed")
	// ErrItemNotFound: no item with that ID.
	ErrItemNotFound = errors.New("download not found in the client")
	// ErrUnsupported: the client cannot do this.
	ErrUnsupported = errors.New("not supported by this client")
	// ErrInvalidRequest: the request is not valid for this client.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrClient: the client returned an error.
	ErrClient = errors.New("client error")
	// ErrResponseTooLarge: the answer exceeded the size cap.
	ErrResponseTooLarge = errors.New("response too large")
)

// ClientError wraps ErrClient with the client's message.
func ClientError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClient, fmt.Sprintf(format, args...))
}

// NormaliseHash returns a lower-case hex info hash, trimmed.
func NormaliseHash(h string) string { return strings.ToLower(strings.TrimSpace(h)) }

// SameID compares client IDs case-insensitively (info hashes come in either case).
func SameID(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }
