package library

import "github.com/mmzlopasa/locarr/server/internal/settings"

// Transfer modes for imports.
const (
	TransferHardlink = "hardlink" // hardlink when on the same filesystem, else copy (default)
	TransferCopy     = "copy"
	TransferMove     = "move" // only when the user opted in
)

// Colon replacements in file names.
const (
	ColonDelete    = "delete"
	ColonDash      = "dash"       // "Title: Sub" -> "Title - Sub"
	ColonSpaceDash = "space_dash" // "Title:Sub" -> "Title - Sub" as well
)

// Media management settings (shared by every kind).
var (
	MovieFolderFormat  = settings.Key[string]{Name: "media.movieFolderFormat", Default: "{Title} ({Year})"}
	MovieFileFormat    = settings.Key[string]{Name: "media.movieFileFormat", Default: "{Title} ({Year}) {[Edition]} {[QualityFull]}{[HDR]}{[VideoCodec]}{-ReleaseGroup}"}
	RenameMovies       = settings.Key[bool]{Name: "media.renameMovies", Default: true}
	ColonReplacement   = settings.Key[string]{Name: "media.colonReplacement", Default: ColonDash}
	TransferMode       = settings.Key[string]{Name: "media.transferMode", Default: TransferHardlink}
	RecycleBinPath     = settings.Key[string]{Name: "media.recycleBinPath", Default: ""}
	RecycleBinDays     = settings.Key[int]{Name: "media.recycleBinRetentionDays", Default: 7}
	SetPermissions     = settings.Key[bool]{Name: "media.setPermissions", Default: false}
	FileMode           = settings.Key[string]{Name: "media.fileMode", Default: "0644"}
	FolderMode         = settings.Key[string]{Name: "media.folderMode", Default: "0755"}
	ExtraExtensions    = settings.Key[[]string]{Name: "media.extraExtensions", Default: []string{"srt", "ass", "ssa", "sub", "idx", "vtt", "sup"}}
	AvailabilityDelay  = settings.Key[int]{Name: "movies.availabilityDelayDays", Default: 0}
	MissingSearchHours = settings.Key[int]{Name: "wanted.missingSearchIntervalHours", Default: 6}
	SearchOnAddDefault = settings.Key[bool]{Name: "movies.searchOnAdd", Default: true}
)
