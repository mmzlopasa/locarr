package series

import (
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Settings of the series library (media management → series).
var (
	RenameEpisodes        = settings.Key[bool]{Name: "media.renameEpisodes", Default: true}
	SeriesFolderFormat    = settings.Key[string]{Name: "media.seriesFolderFormat", Default: "{SeriesTitleYear}"}
	SeasonFolderFormat    = settings.Key[string]{Name: "media.seasonFolderFormat", Default: "Season {Season}"}
	SpecialsFolderFormat  = settings.Key[string]{Name: "media.specialsFolderFormat", Default: "Specials"}
	StandardEpisodeFormat = settings.Key[string]{Name: "media.standardEpisodeFormat",
		Default: "{SeriesTitle} - {EpisodeCode} - {EpisodeTitle} {[QualityFull]}{-ReleaseGroup}"}
	DailyEpisodeFormat = settings.Key[string]{Name: "media.dailyEpisodeFormat",
		Default: "{SeriesTitle} - {AirDate} - {EpisodeTitle} {[QualityFull]}{-ReleaseGroup}"}
	AnimeEpisodeFormat = settings.Key[string]{Name: "media.animeEpisodeFormat",
		Default: "{SeriesTitle} - {EpisodeCode} - {Absolute} - {EpisodeTitle} {[QualityFull]}{-ReleaseGroup}"}
	MultiEpisodeStyle = settings.Key[string]{Name: "media.multiEpisodeStyle", Default: naming.MultiPrefixedRange}
	// SeasonPackThreshold: a season is searched as a whole (a season pack)
	// once this many of its episodes are wanted, or when it has finished
	// airing with at least two wanted (D-051).
	SeasonPackThreshold = settings.Key[int]{Name: "series.seasonPackThreshold", Default: 3}
	SearchOnAddDefault  = settings.Key[bool]{Name: "series.searchOnAdd", Default: true}
)
