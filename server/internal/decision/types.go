// Package decision decides which releases to take for a wanted item. Each
// candidate goes through every check (spec); each check that fails records a
// machine code, an English message with the values involved and the values
// themselves. Accepted candidates are ranked deterministically. Nothing is
// stored here: callers (search, RSS sync) keep the Report in the timeline.
package decision

import (
	"context"
	"fmt"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// Protocols.
const (
	Usenet  = quality.Usenet
	Torrent = quality.Torrent
)

// IDs are external ids a release or a title can carry.
type IDs struct {
	TMDb        int    `json:"tmdb,omitempty"`
	TVDb        int    `json:"tvdb,omitempty"`
	IMDb        string `json:"imdb,omitempty"`
	MusicBrainz string `json:"musicBrainz,omitempty"`
}

// Candidate is one release found by an indexer. It is the engine's own
// input type; indexer results are adapted to it.
type Candidate struct {
	GUID     string `json:"guid"`
	Title    string `json:"title"`
	Size     int64  `json:"size"`     // bytes, 0 = unknown
	Protocol string `json:"protocol"` // usenet or torrent

	IndexerID   int64  `json:"indexerId"`
	IndexerName string `json:"indexerName"`
	// IndexerPriority: 1 is first; 0 counts as 25.
	IndexerPriority int      `json:"indexerPriority"`
	IndexerFlags    []string `json:"indexerFlags,omitempty"`
	// Indexer state (zero value = enabled for everything).
	IndexerDisabled                  bool      `json:"indexerDisabled,omitempty"`
	IndexerRSSDisabled               bool      `json:"indexerRssDisabled,omitempty"`
	IndexerAutomaticSearchDisabled   bool      `json:"indexerAutomaticSearchDisabled,omitempty"`
	IndexerInteractiveSearchDisabled bool      `json:"indexerInteractiveSearchDisabled,omitempty"`
	IndexerLimitedUntil              time.Time `json:"indexerLimitedUntil,omitempty"`
	// MinimumSeeders is the indexer's own setting (0 = use the context's).
	MinimumSeeders int `json:"minimumSeeders,omitempty"`

	Seeders     *int      `json:"seeders,omitempty"`
	Peers       *int      `json:"peers,omitempty"`
	PublishDate time.Time `json:"publishDate"`
	IDs         IDs       `json:"ids"`
	DownloadURL string    `json:"downloadUrl,omitempty"`
	InfoURL     string    `json:"infoUrl,omitempty"`
	// InfoHash (torrents) lets the blocklist match a release by hash.
	InfoHash string `json:"infoHash,omitempty"`
}

// Mode is why releases are being evaluated.
type Mode string

// Modes.
const (
	ModeRSS         Mode = "rss"
	ModeSearch      Mode = "search"      // automatic search
	ModeInteractive Mode = "interactive" // a person is choosing
)

// Episode is a wanted episode.
type Episode struct {
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	Absolute int    `json:"absolute,omitempty"`
	AirDate  string `json:"airDate,omitempty"` // YYYY-MM-DD
}

// Current is the file already in the library.
type Current struct {
	Quality  string          `json:"quality"`
	Revision parser.Revision `json:"revision"`
	Score    int             `json:"score"`
}

// Series types.
const (
	SeriesStandard = "standard"
	SeriesDaily    = "daily"
	SeriesAnime    = "anime"
)

// Wanted is the item releases are evaluated for.
type Wanted struct {
	Kind            quality.Kind `json:"kind"`
	Title           string       `json:"title"`
	AlternateTitles []string     `json:"alternateTitles,omitempty"`
	Year            int          `json:"year,omitempty"`
	IDs             IDs          `json:"ids"`
	// Series.
	SeriesType string    `json:"seriesType,omitempty"`
	Episodes   []Episode `json:"episodes,omitempty"`
	// Music.
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	// RuntimeMinutes is per episode (series), per film or per album (0 = typical).
	RuntimeMinutes   int      `json:"runtimeMinutes,omitempty"`
	OriginalLanguage string   `json:"originalLanguage,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	Current          *Current `json:"current,omitempty"`
	// Adult is set for adult scenes and movies (D-104): identity is judged by
	// studio, date and title instead of the usual title/year/episode checks.
	Adult *Adult `json:"adult,omitempty"`
}

// Adult is what an adult release must name.
type Adult struct {
	Scene bool `json:"scene"`
	// Studios lists the studio name and its aliases.
	Studios []string `json:"studios,omitempty"`
	// Date is the scene's release date (YYYY-MM-DD).
	Date  string `json:"date,omitempty"`
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
	// Performers help a match by title when the name has no date.
	Performers []string `json:"performers,omitempty"`
}

// Blocklist tells whether a release was blocked before (P4/P11 implement it).
type Blocklist interface {
	// Blocked returns a reason when the release must not be taken again.
	Blocked(ctx context.Context, c *Candidate, r *parser.Release) (reason string, blocked bool)
}

// Context is everything the engine needs besides the candidates.
type Context struct {
	Wanted          Wanted
	Profile         quality.Profile
	Formats         []*customformats.Compiled
	Definitions     []quality.Definition
	ReleaseProfiles []quality.ReleaseProfile
	DelayProfiles   []quality.DelayProfile // in evaluation order
	Blocklist       Blocklist
	Mode            Mode
	Now             time.Time
	// MinimumSeeders applies to torrents whose indexer has no own setting.
	MinimumSeeders int
	// RetentionDays drops older usenet posts (0 = unlimited).
	RetentionDays int
	// MinimumAgeMinutes waits for fresh usenet posts to propagate.
	MinimumAgeMinutes  int
	AllowHardcodedSubs bool
	Allow3D            bool
	// BlockedTerms is the adult tag blocklist (lower case); a release whose
	// name contains one as a word is rejected with blocked_tag.
	BlockedTerms []string
}

// Rejection is one reason a candidate was not taken.
type Rejection struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Temporary rejections may pass later (delay, minimum age, limits).
	Temporary bool           `json:"temporary,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
}

func (r Rejection) String() string { return r.Code + ": " + r.Message }

// Statuses.
const (
	StatusAccepted = "accepted"
	StatusPending  = "pending" // only temporary rejections
	StatusRejected = "rejected"
)

// RankKey shows what the ranking compared (in order).
type RankKey struct {
	QualityPosition   int    `json:"qualityPosition"` // 0 = the profile's best item
	Score             int    `json:"score"`
	Revision          int    `json:"revision"`
	ProtocolPreferred bool   `json:"protocolPreferred"`
	IndexerPriority   int    `json:"indexerPriority"`
	Seeders           int    `json:"seeders"`
	SizeDistance      int64  `json:"sizeDistance"` // bytes from the preferred size (-1 = no preference)
	AgeMinutes        int64  `json:"ageMinutes"`
	Tiebreak          string `json:"-"`
}

// Decision is the verdict on one candidate.
type Decision struct {
	Candidate     Candidate             `json:"candidate"`
	Release       parser.Release        `json:"release"`
	Quality       quality.Quality       `json:"quality"`
	CustomFormats []customformats.Match `json:"customFormats"`
	Score         int                   `json:"score"`
	Status        string                `json:"status"`
	Rank          int                   `json:"rank,omitempty"` // 1 = best, accepted only
	Rejections    []Rejection           `json:"rejections"`
	RankKey       RankKey               `json:"rankKey"`
}

// Accepted reports whether nothing stood in the way.
func (d *Decision) Accepted() bool { return d.Status == StatusAccepted }

// Report is the outcome for a list of candidates: accepted first in rank
// order, then pending, then rejected (each group in input order).
type Report struct {
	Kind        quality.Kind `json:"kind"`
	ProfileID   int64        `json:"profileId"`
	ProfileName string       `json:"profileName"`
	Mode        Mode         `json:"mode"`
	EvaluatedAt time.Time    `json:"evaluatedAt"`
	Accepted    int          `json:"accepted"`
	Pending     int          `json:"pending"`
	Rejected    int          `json:"rejected"`
	// BestGUID is the top accepted candidate ("" when none).
	BestGUID  string     `json:"bestGuid,omitempty"`
	Decisions []Decision `json:"decisions"`
}

// Best returns the top accepted decision, or nil.
func (r *Report) Best() *Decision {
	if len(r.Decisions) > 0 && r.Decisions[0].Accepted() {
		return &r.Decisions[0]
	}
	return nil
}

func formatSize(b int64) string {
	if b < 0 {
		b = 0
	}
	gb := float64(b) / (1 << 30)
	if gb >= 1 {
		return trimZero(fmt.Sprintf("%.1f", gb)) + " GB"
	}
	return trimZero(fmt.Sprintf("%.0f", float64(b)/(1<<20))) + " MB"
}

func trimZero(s string) string {
	if len(s) > 2 && s[len(s)-2:] == ".0" {
		return s[:len(s)-2]
	}
	return s
}

func formatMinutes(m int64) string {
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	if m%60 == 0 {
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}
