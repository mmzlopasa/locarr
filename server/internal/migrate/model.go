// Package migrate imports an existing setup (P10): it reads Sonarr, Radarr,
// Lidarr, Prowlarr, Bazarr, Overseerr/Seerr, qBittorrent and SABnzbd through
// their public HTTP APIs, never changing anything on them (D-003), maps what
// it reads onto Locarr with explicit rules (mapping.go), builds a plan that
// says what will be created, merged or skipped and why (plan.go), and applies
// it through the Locarr services (apply.go).
package migrate

import (
	"encoding/json"
	"time"
)

// Kind is an application Locarr can import from.
type Kind string

// Source kinds.
const (
	Sonarr      Kind = "sonarr"
	Radarr      Kind = "radarr"
	Lidarr      Kind = "lidarr"
	Prowlarr    Kind = "prowlarr"
	Bazarr      Kind = "bazarr"
	Overseerr   Kind = "overseerr"
	Seerr       Kind = "seerr"
	QBittorrent Kind = "qbittorrent"
	SABnzbd     Kind = "sabnzbd"
)

// Kinds lists every source kind.
var Kinds = []Kind{Sonarr, Radarr, Lidarr, Prowlarr, Bazarr, Overseerr, Seerr, QBittorrent, SABnzbd}

// Valid reports a known kind.
func (k Kind) Valid() bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Label is how the application is named in messages.
func (k Kind) Label() string {
	return map[Kind]string{Sonarr: "Sonarr", Radarr: "Radarr", Lidarr: "Lidarr", Prowlarr: "Prowlarr", Bazarr: "Bazarr",
		Overseerr: "Overseerr", Seerr: "Seerr", QBittorrent: "qBittorrent", SABnzbd: "SABnzbd"}[k]
}

// Scopes says what a source can give.
type Scopes struct {
	Settings bool `json:"settings"`
	Library  bool `json:"library"`
	Users    bool `json:"users"`
	History  bool `json:"history"`
}

// Supported returns what can be read from a kind.
func (k Kind) Supported() Scopes {
	switch k {
	case Sonarr, Radarr, Lidarr:
		return Scopes{Settings: true, Library: true, History: true}
	case Prowlarr, QBittorrent, SABnzbd:
		return Scopes{Settings: true}
	case Bazarr:
		return Scopes{Settings: true, Library: true}
	case Overseerr, Seerr:
		return Scopes{Settings: true, Users: true}
	}
	return Scopes{}
}

// Snapshot is everything read from one source, close to the source's own
// shape (ids are the source's).
type Snapshot struct {
	SourceID int64  `json:"sourceId"`
	Kind     Kind   `json:"kind"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Key      string `json:"key"` // kind|url, the identity links are stored under
	AppName  string `json:"appName"`
	Version  string `json:"version"`

	Tags               map[int64]string    `json:"tags,omitempty"`
	QualityProfiles    []QualityProfile    `json:"qualityProfiles,omitempty"`
	QualityDefinitions []QualityDefinition `json:"qualityDefinitions,omitempty"`
	CustomFormats      []CustomFormat      `json:"customFormats,omitempty"`
	LanguageProfiles   []LanguageProfile   `json:"languageProfiles,omitempty"`
	ReleaseProfiles    []ReleaseProfile    `json:"releaseProfiles,omitempty"`
	DelayProfiles      []DelayProfile      `json:"delayProfiles,omitempty"`
	MetadataProfiles   []MetadataProfile   `json:"metadataProfiles,omitempty"`
	RootFolders        []RootFolder        `json:"rootFolders,omitempty"`
	Naming             map[string]any      `json:"naming,omitempty"`
	Indexers           []Indexer           `json:"indexers,omitempty"`
	DownloadClients    []DownloadClient    `json:"downloadClients,omitempty"`
	PathMappings       []RemotePathMapping `json:"pathMappings,omitempty"`
	ImportLists        []ImportList        `json:"importLists,omitempty"`
	Collections        []Collection        `json:"collections,omitempty"`
	Exclusions         []Exclusion         `json:"exclusions,omitempty"`
	Blocklist          []BlockItem         `json:"blocklist,omitempty"`
	History            []HistoryItem       `json:"history,omitempty"`

	Movies  []Movie  `json:"movies,omitempty"`
	Series  []Series `json:"series,omitempty"`
	Artists []Artist `json:"artists,omitempty"`

	Apps []App `json:"apps,omitempty"`

	SubtitleProfiles    []SubtitleProfile    `json:"subtitleProfiles,omitempty"`
	SubtitleProviders   []SubtitleProvider   `json:"subtitleProviders,omitempty"`
	SubtitleAssignments []SubtitleAssignment `json:"subtitleAssignments,omitempty"`

	Users              []User           `json:"users,omitempty"`
	Requests           []Request        `json:"requests,omitempty"`
	Issues             []Issue          `json:"issues,omitempty"`
	DefaultQuotas      map[string]Quota `json:"defaultQuotas,omitempty"`
	DefaultPermissions int64            `json:"defaultPermissions,omitempty"`

	Client *ClientConn `json:"client,omitempty"`

	Warnings []string `json:"warnings,omitempty"`
}

// QualityRef names a source quality.
type QualityRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ProfileItem is a quality or a group of a source profile.
type ProfileItem struct {
	ID      int64         `json:"id,omitempty"` // group id
	Name    string        `json:"name,omitempty"`
	Quality *QualityRef   `json:"quality,omitempty"`
	Items   []ProfileItem `json:"items,omitempty"`
	Allowed bool          `json:"allowed"`
}

// FormatScore is a custom format score in a source profile.
type FormatScore struct {
	FormatID int64  `json:"format"`
	Name     string `json:"name"`
	Score    int    `json:"score"`
}

// QualityProfile is a source quality profile. Items are worst first, as the
// applications list them.
type QualityProfile struct {
	ID                int64         `json:"id"`
	Name              string        `json:"name"`
	UpgradeAllowed    bool          `json:"upgradeAllowed"`
	Cutoff            int64         `json:"cutoff"`
	Items             []ProfileItem `json:"items"`
	MinFormatScore    int           `json:"minFormatScore"`
	CutoffFormatScore int           `json:"cutoffFormatScore"`
	FormatScores      []FormatScore `json:"formatItems"`
	// Language is the profile language (Radarr), by name.
	Language string `json:"language,omitempty"`
}

// QualityDefinition is a size limit in MB per minute.
type QualityDefinition struct {
	Quality   string   `json:"quality"`
	Min       float64  `json:"min"`
	Max       *float64 `json:"max"`
	Preferred *float64 `json:"preferred"`
}

// CustomFormat is a format in the shared JSON shape.
type CustomFormat struct {
	ID   int64           `json:"id"`
	Name string          `json:"name"`
	Raw  json.RawMessage `json:"raw"`
}

// LanguageProfile is a Sonarr v3 language profile.
type LanguageProfile struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Languages []string `json:"languages"` // allowed, by name
}

// ReleaseProfile requires or forbids terms.
type ReleaseProfile struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	Required  []string `json:"required"`
	Ignored   []string `json:"ignored"`
	Preferred int      `json:"preferred,omitempty"` // count of preferred terms (Sonarr v3), not imported
	IndexerID int64    `json:"indexerId"`
	Tags      []int64  `json:"tags"`
}

// DelayProfile is a source delay profile.
type DelayProfile struct {
	ID                     int64   `json:"id"`
	Order                  int     `json:"order"`
	EnableUsenet           bool    `json:"enableUsenet"`
	EnableTorrent          bool    `json:"enableTorrent"`
	PreferredProtocol      string  `json:"preferredProtocol"`
	UsenetDelay            int     `json:"usenetDelay"`
	TorrentDelay           int     `json:"torrentDelay"`
	BypassIfHighestQuality bool    `json:"bypassIfHighestQuality"`
	BypassIfAboveScore     bool    `json:"bypassIfAboveCustomFormatScore"`
	MinimumScore           int     `json:"minimumCustomFormatScore"`
	Tags                   []int64 `json:"tags"`
}

// MetadataProfile is a Lidarr metadata profile (allowed names only).
type MetadataProfile struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Primary   []string `json:"primary"`
	Secondary []string `json:"secondary"`
	Statuses  []string `json:"statuses"`
}

// RootFolder is a source root folder (the source's path).
type RootFolder struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

// Indexer is a source indexer; Fields are its settings by name.
type Indexer struct {
	ID                int64          `json:"id"`
	Name              string         `json:"name"`
	Implementation    string         `json:"implementation"`
	Protocol          string         `json:"protocol"`
	Enable            bool           `json:"enable"`
	EnableRSS         bool           `json:"enableRss"`
	EnableAutomatic   bool           `json:"enableAutomaticSearch"`
	EnableInteractive bool           `json:"enableInteractiveSearch"`
	Priority          int            `json:"priority"`
	Tags              []int64        `json:"tags"`
	Fields            map[string]any `json:"fields"`
	// Categories are Prowlarr's capabilities (Newznab category ids).
	Categories []int `json:"categories,omitempty"`
}

// DownloadClient is a source download client.
type DownloadClient struct {
	ID             int64          `json:"id"`
	Name           string         `json:"name"`
	Implementation string         `json:"implementation"`
	Protocol       string         `json:"protocol"`
	Enable         bool           `json:"enable"`
	Priority       int            `json:"priority"`
	Tags           []int64        `json:"tags"`
	Fields         map[string]any `json:"fields"`
}

// RemotePathMapping is a source remote path mapping (local = as the source sees it).
type RemotePathMapping struct {
	Host       string `json:"host"`
	RemotePath string `json:"remotePath"`
	LocalPath  string `json:"localPath"`
}

// ImportList is a source import list.
type ImportList struct {
	ID                  int64          `json:"id"`
	Name                string         `json:"name"`
	Implementation      string         `json:"implementation"`
	Enabled             bool           `json:"enabled"`
	RootFolderPath      string         `json:"rootFolderPath"`
	QualityProfileID    int64          `json:"qualityProfileId"`
	MinimumAvailability string         `json:"minimumAvailability"`
	Monitored           bool           `json:"monitored"`
	SearchOnAdd         bool           `json:"searchOnAdd"`
	Tags                []int64        `json:"tags"`
	Fields              map[string]any `json:"fields"`
}

// Collection is a Radarr collection.
type Collection struct {
	TMDbID              int64   `json:"tmdbId"`
	Title               string  `json:"title"`
	Monitored           bool    `json:"monitored"`
	QualityProfileID    int64   `json:"qualityProfileId"`
	RootFolderPath      string  `json:"rootFolderPath"`
	MinimumAvailability string  `json:"minimumAvailability"`
	SearchOnAdd         bool    `json:"searchOnAdd"`
	Tags                []int64 `json:"tags"`
}

// Exclusion keeps a title from being added by lists.
type Exclusion struct {
	TMDbID int64  `json:"tmdbId,omitempty"`
	TVDbID int64  `json:"tvdbId,omitempty"`
	MBID   string `json:"mbid,omitempty"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
}

// BlockItem is a blocklisted release.
type BlockItem struct {
	ID       int64     `json:"id"`
	Title    string    `json:"title"`
	Protocol string    `json:"protocol"`
	Indexer  string    `json:"indexer"`
	InfoHash string    `json:"infoHash"`
	Message  string    `json:"message"`
	Date     time.Time `json:"date"`
	// ItemID is the source's movie, series or artist id.
	ItemID int64 `json:"itemId"`
}

// HistoryItem is a grab, import or failure in the source's history.
type HistoryItem struct {
	ID        int64     `json:"id"`
	EventType string    `json:"eventType"`
	Title     string    `json:"title"`
	Date      time.Time `json:"date"`
	Quality   string    `json:"quality"`
	Indexer   string    `json:"indexer,omitempty"`
	Client    string    `json:"client,omitempty"`
	// ItemID is the source's movie, series or artist id; EpisodeID/AlbumID narrow it.
	ItemID    int64 `json:"itemId"`
	EpisodeID int64 `json:"episodeId,omitempty"`
	AlbumID   int64 `json:"albumId,omitempty"`
}

// MediaFile is a movie or episode file as the source sees it.
type MediaFile struct {
	ID           int64     `json:"id"`
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	Quality      string    `json:"quality"`
	Revision     int       `json:"revision"`
	ReleaseGroup string    `json:"releaseGroup"`
	SceneName    string    `json:"sceneName"`
	Edition      string    `json:"edition,omitempty"`
	Languages    []string  `json:"languages,omitempty"`
	DateAdded    time.Time `json:"dateAdded"`
}

// Movie is a Radarr movie.
type Movie struct {
	ID                  int64      `json:"id"`
	Title               string     `json:"title"`
	OriginalTitle       string     `json:"originalTitle"`
	OriginalLanguage    string     `json:"originalLanguage"`
	Year                int        `json:"year"`
	TMDbID              int64      `json:"tmdbId"`
	IMDbID              string     `json:"imdbId"`
	Path                string     `json:"path"`
	Monitored           bool       `json:"monitored"`
	MinimumAvailability string     `json:"minimumAvailability"`
	QualityProfileID    int64      `json:"qualityProfileId"`
	Tags                []int64    `json:"tags"`
	Status              string     `json:"status"`
	Overview            string     `json:"overview"`
	Runtime             int        `json:"runtime"`
	InCinemas           string     `json:"inCinemas"`
	DigitalRelease      string     `json:"digitalRelease"`
	PhysicalRelease     string     `json:"physicalRelease"`
	Studio              string     `json:"studio"`
	Certification       string     `json:"certification"`
	Genres              []string   `json:"genres"`
	Poster              string     `json:"poster"`
	CollectionTMDbID    int64      `json:"collectionTmdbId"`
	CollectionTitle     string     `json:"collectionTitle"`
	File                *MediaFile `json:"file,omitempty"`
}

// SeasonInfo is a season's monitoring.
type SeasonInfo struct {
	Number    int  `json:"number"`
	Monitored bool `json:"monitored"`
}

// Episode is a Sonarr episode.
type Episode struct {
	ID         int64      `json:"id"`
	Season     int        `json:"season"`
	Number     int        `json:"number"`
	Absolute   int        `json:"absolute"`
	TVDbID     int64      `json:"tvdbId"`
	Title      string     `json:"title"`
	AirDate    string     `json:"airDate"`
	AirDateUTC *time.Time `json:"airDateUtc,omitempty"`
	Runtime    int        `json:"runtime"`
	Monitored  bool       `json:"monitored"`
	FileID     int64      `json:"fileId"`
}

// Series is a Sonarr series with its episodes and files.
type Series struct {
	ID                int64        `json:"id"`
	Title             string       `json:"title"`
	Year              int          `json:"year"`
	TVDbID            int64        `json:"tvdbId"`
	TMDbID            int64        `json:"tmdbId"`
	IMDbID            string       `json:"imdbId"`
	TVMazeID          int64        `json:"tvMazeId"`
	Path              string       `json:"path"`
	Monitored         bool         `json:"monitored"`
	MonitorNewItems   string       `json:"monitorNewItems"`
	SeriesType        string       `json:"seriesType"`
	SeasonFolder      bool         `json:"seasonFolder"`
	QualityProfileID  int64        `json:"qualityProfileId"`
	LanguageProfileID int64        `json:"languageProfileId"`
	Tags              []int64      `json:"tags"`
	Status            string       `json:"status"`
	Network           string       `json:"network"`
	Runtime           int          `json:"runtime"`
	Overview          string       `json:"overview"`
	Genres            []string     `json:"genres"`
	Certification     string       `json:"certification"`
	FirstAired        string       `json:"firstAired"`
	OriginalLanguage  string       `json:"originalLanguage"`
	Seasons           []SeasonInfo `json:"seasons"`
	Episodes          []Episode    `json:"episodes"`
	Files             []MediaFile  `json:"files"`
}

// TrackFile is a Lidarr track file with the track it belongs to.
type TrackFile struct {
	ID            int64  `json:"id"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	Quality       string `json:"quality"`
	Disc          int    `json:"disc"`
	Position      int    `json:"position"`
	RecordingMBID string `json:"recordingMbid"`
	Title         string `json:"title"`
	DurationMS    int    `json:"durationMs"`
	ReleaseGroup  string `json:"releaseGroup"`
	SceneName     string `json:"sceneName"`
}

// Album is a Lidarr album (release group).
type Album struct {
	ID             int64       `json:"id"`
	Title          string      `json:"title"`
	MBID           string      `json:"mbid"`
	Disambiguation string      `json:"disambiguation"`
	Type           string      `json:"type"`
	SecondaryTypes []string    `json:"secondaryTypes"`
	ReleaseDate    string      `json:"releaseDate"`
	Monitored      bool        `json:"monitored"`
	Files          []TrackFile `json:"files"`
}

// Artist is a Lidarr artist with its albums.
type Artist struct {
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	MBID              string   `json:"mbid"`
	SortName          string   `json:"sortName"`
	Disambiguation    string   `json:"disambiguation"`
	Type              string   `json:"type"`
	Path              string   `json:"path"`
	Monitored         bool     `json:"monitored"`
	MonitorNewItems   string   `json:"monitorNewItems"`
	QualityProfileID  int64    `json:"qualityProfileId"`
	MetadataProfileID int64    `json:"metadataProfileId"`
	Tags              []int64  `json:"tags"`
	Genres            []string `json:"genres"`
	Albums            []Album  `json:"albums"`
}

// App is an application Prowlarr syncs indexers to.
type App struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
	BaseURL        string `json:"baseUrl"`
	SyncLevel      string `json:"syncLevel"`
}

// SubtitleLanguage is one language of a subtitle profile.
type SubtitleLanguage struct {
	Code   string `json:"code"`
	Forced bool   `json:"forced"`
	HI     bool   `json:"hi"`
}

// SubtitleProfile is a Bazarr language profile.
type SubtitleProfile struct {
	ID        int64              `json:"id"`
	Name      string             `json:"name"`
	Languages []SubtitleLanguage `json:"languages"`
	Cutoff    bool               `json:"cutoff"`
}

// SubtitleProvider is a provider Bazarr has enabled; Fields hold its
// settings where the API returns them (secrets may be masked).
type SubtitleProvider struct {
	Name   string            `json:"name"`
	Fields map[string]string `json:"fields,omitempty"`
}

// SubtitleAssignment is the profile Bazarr uses for a movie or series.
type SubtitleAssignment struct {
	Kind      string `json:"kind"`  // movie or series
	AppID     int64  `json:"appId"` // Radarr movie id or Sonarr series id
	TMDbID    int64  `json:"tmdbId,omitempty"`
	TVDbID    int64  `json:"tvdbId,omitempty"`
	IMDbID    string `json:"imdbId,omitempty"`
	Title     string `json:"title"`
	ProfileID int64  `json:"profileId"`
}

// Quota is a request limit per period.
type Quota struct {
	Limit int `json:"limit"`
	Days  int `json:"days"`
}

// User is a request application user.
type User struct {
	ID          int64            `json:"id"`
	Username    string           `json:"username"`
	Email       string           `json:"email"`
	DisplayName string           `json:"displayName"`
	Permissions int64            `json:"permissions"`
	UserType    int              `json:"userType"`
	Quotas      map[string]Quota `json:"quotas,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
}

// SeasonRequest is one requested season.
type SeasonRequest struct {
	Number int `json:"number"`
	Status int `json:"status"`
}

// Comment is a comment on an issue.
type Comment struct {
	UserID    int64     `json:"userId"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// Request is a request application request.
type Request struct {
	ID          int64           `json:"id"`
	Type        string          `json:"type"` // movie or tv
	Status      int             `json:"status"`
	MediaStatus int             `json:"mediaStatus"`
	TMDbID      int64           `json:"tmdbId"`
	TVDbID      int64           `json:"tvdbId"`
	Is4K        bool            `json:"is4k"`
	RequestedBy int64           `json:"requestedBy"`
	ModifiedBy  int64           `json:"modifiedBy"`
	Title       string          `json:"title"`
	Year        int             `json:"year"`
	Seasons     []SeasonRequest `json:"seasons,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// Issue is a request application issue.
type Issue struct {
	ID         int64     `json:"id"`
	Type       int       `json:"type"`
	Status     int       `json:"status"`
	MediaType  string    `json:"mediaType"`
	TMDbID     int64     `json:"tmdbId"`
	Title      string    `json:"title"`
	Season     int       `json:"season"`
	Episode    int       `json:"episode"`
	CreatedBy  int64     `json:"createdBy"`
	ModifiedBy int64     `json:"modifiedBy"`
	Comments   []Comment `json:"comments"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ClientCategory is a download client category and its folder (client's view).
type ClientCategory struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath"`
}

// ClientConn is how to reach a download client read as a source.
type ClientConn struct {
	Implementation string           `json:"implementation"`
	Host           string           `json:"host"`
	Port           int              `json:"port"`
	UseTLS         bool             `json:"useTls"`
	URLBase        string           `json:"urlBase"`
	Username       string           `json:"username"`
	DefaultPath    string           `json:"defaultPath"`
	Categories     []ClientCategory `json:"categories"`
}
