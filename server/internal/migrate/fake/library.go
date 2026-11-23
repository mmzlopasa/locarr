package fake

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

// Movie is a fake Radarr movie; File is relative to its folder ("" = none).
type Movie struct {
	ID, TMDb  int64
	IMDb      string
	Title     string
	Year      int
	Monitored bool
	File      string
	Quality   string
	Tags      []int
}

// Episode is a fake Sonarr episode.
type Episode struct {
	ID, FileID     int64
	Season, Number int
	Title, AirDate string
	Monitored      bool
	File           string // relative to the series folder
	Quality, Group string
}

// Series is a fake Sonarr series.
type Series struct {
	ID, TVDB, TMDb int64
	Title          string
	Year           int
	Type           string
	Monitored      bool
	Seasons        map[int]bool
	Episodes       []Episode
	Tags           []int
}

// Track is a fake Lidarr track file.
type Track struct {
	ID       int64
	Disc     int
	Position int
	Title    string
	File     string // relative to the artist folder
}

// Album is a fake Lidarr album.
type Album struct {
	ID        int64
	MBID      string
	Title     string
	Type      string
	Date      string
	Monitored bool
	Tracks    []Track
}

// Artist is a fake Lidarr artist.
type Artist struct {
	ID        int64
	MBID      string
	Name      string
	Monitored bool
	Albums    []Album
}

// User is a fake request application user.
type User struct {
	ID          int64
	Username    string
	Email       string
	Permissions int64
}

// Library is what the fakes serve. Roots are the folders as the
// applications see them (inside their own containers).
type Library struct {
	MovieRoot, SeriesRoot, MusicRoot string
	Tags                             map[int64]string
	Movies                           []Movie
	Series                           []Series
	Artists                          []Artist
	Users                            []User
	// Now anchors the dates.
	Now time.Time
}

// Default is a small realistic setup: three movies known to the fake TMDb
// (one without a file), two series (one with a missing file) and one artist.
func Default() *Library {
	lib := &Library{MovieRoot: "/movies", SeriesRoot: "/tv", MusicRoot: "/music", Now: time.Now().UTC(),
		Tags: map[int64]string{1: "4k", 2: "kids"}}
	lib.Movies = []Movie{
		{ID: 1, TMDb: 10378, IMDb: "tt1254207", Title: "Big Buck Bunny", Year: 2008, Monitored: true,
			File: "Big.Buck.Bunny.2008.1080p.BluRay.x264-OPEN.mkv", Quality: "Bluray-1080p", Tags: []int{2}},
		{ID: 2, TMDb: 45745, IMDb: "tt1727587", Title: "Sintel", Year: 2010, Monitored: true,
			File: "Sintel.2010.2160p.WEB-DL.DDP5.1.HDR.H.265-BLEND.mkv", Quality: "WEBDL-2160p", Tags: []int{1}},
		{ID: 3, TMDb: 133701, IMDb: "tt2285752", Title: "Tears of Steel", Year: 2012, Monitored: false},
	}
	lib.Series = []Series{
		{ID: 1, TVDB: 434343, TMDb: 800434, Title: "Cosmos Laundromat", Year: 2015, Type: "standard", Monitored: true,
			Seasons: map[int]bool{0: false, 1: true}, Episodes: []Episode{
				{ID: 11, Season: 0, Number: 1, Title: "Making Of", AirDate: "2015-07-01"},
				{ID: 12, FileID: 101, Season: 1, Number: 1, Title: "First Cycle", AirDate: "2015-08-10", Monitored: true,
					File: "Season 01/Cosmos.Laundromat.S01E01.1080p.WEB-DL.x264-OPEN.mkv", Quality: "WEBDL-1080p", Group: "OPEN"},
				{ID: 13, FileID: 102, Season: 1, Number: 2, Title: "Second Cycle", AirDate: "2015-08-17", Monitored: true,
					File: "Season 01/Cosmos.Laundromat.S01E02.1080p.WEB-DL.x264-OPEN.mkv", Quality: "WEBDL-1080p", Group: "OPEN"},
				{ID: 14, Season: 1, Number: 3, Title: "Third Cycle", AirDate: "2015-08-24", Monitored: false},
			}},
		{ID: 2, TVDB: 424242, TMDb: 800424, Title: "Elephants Dream", Year: 2006, Type: "standard", Monitored: true,
			Seasons: map[int]bool{1: true}, Episodes: []Episode{
				{ID: 21, FileID: 201, Season: 1, Number: 1, Title: "Proog", AirDate: "2006-03-24", Monitored: true,
					File: "Season 01/Elephants.Dream.S01E01.720p.HDTV.x264-OLD.mkv", Quality: "HDTV-720p", Group: "OLD"},
				{ID: 22, FileID: 202, Season: 1, Number: 2, Title: "Emo", AirDate: "2006-03-31", Monitored: true,
					File: "Season 01/Elephants.Dream.S01E02.720p.HDTV.x264-OLD.mkv", Quality: "HDTV-720p", Group: "OLD"},
			}},
	}
	lib.Artists = []Artist{{ID: 1, MBID: mbfake.ArtistOpen, Name: "Open Source Artist", Monitored: true, Albums: []Album{
		{ID: 11, MBID: mbfake.GroupFreeAlbum, Title: "Free Album", Type: "Album", Date: "2020-03-14", Monitored: true, Tracks: []Track{
			{ID: 111, Disc: 1, Position: 1, Title: "Opening", File: "Free Album (2020)/01 - Opening.flac"},
			{ID: 112, Disc: 1, Position: 2, Title: "Second Light", File: "Free Album (2020)/02 - Second Light.flac"}}},
		{ID: 12, MBID: mbfake.GroupFreeEP, Title: "Free EP", Type: "EP", Date: "2021-06-01", Monitored: false},
	}}}
	lib.Users = []User{
		{ID: 1, Username: "admin", Email: "owner@example.org", Permissions: 2},
		{ID: 2, Username: "mama", Email: "mama@example.org", Permissions: 32 | 256},
		{ID: 3, Username: "helper", Email: "helper@example.org", Permissions: 16 | 32},
	}
	return lib
}

// Big generates a large library (performance tests): movies, series with
// episodes and artists, every item with a file.
func Big(movies, series, artists int) *Library {
	lib := &Library{MovieRoot: "/movies", SeriesRoot: "/tv", MusicRoot: "/music", Now: time.Now().UTC(), Tags: map[int64]string{1: "big"}}
	for i := 1; i <= movies; i++ {
		lib.Movies = append(lib.Movies, Movie{ID: int64(i), TMDb: int64(5000000 + i), IMDb: fmt.Sprintf("tt%08d", 5000000+i),
			Title: fmt.Sprintf("Generated Film %d", i), Year: 1950 + i%75, Monitored: i%3 != 0,
			File: fmt.Sprintf("Generated.Film.%d.%d.1080p.BluRay.x264-GEN.mkv", i, 1950+i%75), Quality: "Bluray-1080p"})
	}
	for i := 1; i <= series; i++ {
		v := Series{ID: int64(i), TVDB: int64(7000000 + i), TMDb: int64(8000000 + i), Title: fmt.Sprintf("Generated Show %d", i),
			Year: 1990 + i%35, Type: "standard", Monitored: true, Seasons: map[int]bool{1: true, 2: true}}
		for sn := 1; sn <= 2; sn++ {
			for e := 1; e <= 10; e++ {
				id := int64(i*100 + sn*20 + e)
				v.Episodes = append(v.Episodes, Episode{ID: id, FileID: id, Season: sn, Number: e, Title: fmt.Sprintf("Episode %d", e),
					AirDate: fmt.Sprintf("%d-01-%02d", 2000+sn, e), Monitored: true,
					File: fmt.Sprintf("Season %02d/Generated.Show.%d.S%02dE%02d.720p.WEB-DL-GEN.mkv", sn, i, sn, e), Quality: "WEBDL-720p", Group: "GEN"})
			}
		}
		lib.Series = append(lib.Series, v)
	}
	for i := 1; i <= artists; i++ {
		a := Artist{ID: int64(i), MBID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), Name: fmt.Sprintf("Generated Artist %d", i), Monitored: true}
		for al := 1; al <= 2; al++ {
			alb := Album{ID: int64(i*10 + al), MBID: fmt.Sprintf("00000000-0000-4000-9000-%010d%02d", i, al), Title: fmt.Sprintf("Album %d", al),
				Type: "Album", Date: fmt.Sprintf("%d-01-01", 2000+al), Monitored: true}
			for t := 1; t <= 3; t++ {
				alb.Tracks = append(alb.Tracks, Track{ID: int64(i*1000 + al*10 + t), Disc: 1, Position: t, Title: fmt.Sprintf("Track %d", t),
					File: fmt.Sprintf("Album %d/%02d - Track %d.flac", al, t, t)})
			}
			a.Albums = append(a.Albums, alb)
		}
		lib.Artists = append(lib.Artists, a)
	}
	return lib
}

// MovieFolder is a movie's folder name.
func (m Movie) MovieFolder() string { return fmt.Sprintf("%s (%d)", m.Title, m.Year) }

// SeriesFolder is a series' folder name.
func (v Series) SeriesFolder() string { return v.Title }

// WriteFiles creates the library's files under local roots (where Locarr
// sees the applications' folders). skip lists source paths not to create
// (to test missing files).
func (lib *Library) WriteFiles(movieRoot, seriesRoot, musicRoot string, skip map[string]bool) error {
	write := func(p string) error {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte("fake media "+filepath.Base(p)), 0o644)
	}
	for _, m := range lib.Movies {
		if m.File == "" {
			continue
		}
		rel := path.Join(m.MovieFolder(), m.File)
		if skip[path.Join(lib.MovieRoot, rel)] || movieRoot == "" {
			continue
		}
		if err := write(filepath.Join(movieRoot, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	for _, v := range lib.Series {
		for _, e := range v.Episodes {
			if e.File == "" || seriesRoot == "" {
				continue
			}
			rel := path.Join(v.SeriesFolder(), e.File)
			if skip[path.Join(lib.SeriesRoot, rel)] {
				continue
			}
			if err := write(filepath.Join(seriesRoot, filepath.FromSlash(rel))); err != nil {
				return err
			}
		}
	}
	for _, a := range lib.Artists {
		for _, al := range a.Albums {
			for _, t := range al.Tracks {
				if musicRoot == "" {
					continue
				}
				rel := path.Join(a.Name, t.File)
				if skip[path.Join(lib.MusicRoot, rel)] {
					continue
				}
				if err := write(filepath.Join(musicRoot, filepath.FromSlash(rel))); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (lib *Library) roots(kind string) []string {
	switch kind {
	case "radarr":
		return []string{lib.MovieRoot}
	case "sonarr":
		return []string{lib.SeriesRoot}
	case "lidarr":
		return []string{lib.MusicRoot}
	}
	return nil
}

func ladderItems(kind string) ([]any, int) {
	q := func(id int, name, source string, res int) map[string]any {
		return map[string]any{"quality": map[string]any{"id": id, "name": name, "source": source, "resolution": res}, "items": []any{}, "allowed": false}
	}
	allowed := func(m map[string]any) map[string]any { m["allowed"] = true; return m }
	if kind == "lidarr" {
		return []any{q(0, "Unknown", "", 0), q(1, "MP3-192", "", 0), allowed(q(2, "MP3-VBR-V0", "", 0)), allowed(q(4, "MP3-320", "", 0)),
			allowed(q(6, "FLAC", "", 0)), q(21, "FLAC 24bit", "", 0), q(99, "APE", "", 0)}, 6
	}
	remux := "Remux-1080p"
	if kind == "sonarr" {
		remux = "Bluray-1080p Remux"
	}
	group := map[string]any{"id": 1001, "name": "WEB 1080p", "allowed": true, "items": []any{
		allowed(q(3, "WEBDL-1080p", "web", 1080)), allowed(q(15, "WEBRip-1080p", "webRip", 1080))}}
	return []any{q(0, "Unknown", "unknown", 0), q(1, "SDTV", "television", 480), allowed(q(4, "HDTV-720p", "television", 720)),
		allowed(q(9, "HDTV-1080p", "television", 1080)), group, allowed(q(7, "Bluray-1080p", "bluray", 1080)), q(20, remux, "blurayRaw", 1080),
		q(31, "Bluray-2160p", "bluray", 2160)}, 1001
}

func (lib *Library) qualityProfiles(kind string) []any {
	items, cutoff := ladderItems(kind)
	p := map[string]any{"id": 1, "name": "HD-1080p", "upgradeAllowed": true, "cutoff": cutoff, "items": items, "minFormatScore": 0,
		"cutoffFormatScore": 100, "formatItems": []any{}}
	if kind != "lidarr" {
		p["formatItems"] = []any{map[string]any{"format": 1, "name": "x265 (HD)", "score": -1000}, map[string]any{"format": 2, "name": "Repack", "score": 5}}
	}
	if kind == "radarr" {
		p["language"] = map[string]any{"id": 1, "name": "English"}
	}
	if kind == "lidarr" {
		p["name"] = "Lossless"
	}
	return []any{p, map[string]any{"id": 2, "name": "Only Unknown", "upgradeAllowed": false, "cutoff": 0,
		"items": []any{map[string]any{"quality": map[string]any{"id": 0, "name": "Unknown"}, "items": []any{}, "allowed": false}}}}
}

func (lib *Library) qualityDefinitions(kind string) []any {
	if kind == "lidarr" {
		return []any{map[string]any{"quality": qualityRef(6, "FLAC"), "title": "FLAC", "minSize": 0, "maxSize": 0, "preferredSize": 0}}
	}
	return []any{
		map[string]any{"quality": qualityRef(7, "Bluray-1080p"), "title": "Bluray-1080p", "minSize": 4, "maxSize": 150, "preferredSize": 95},
		map[string]any{"quality": qualityRef(3, "WEBDL-1080p"), "title": "WEBDL-1080p", "minSize": 3, "maxSize": 137.3, "preferredSize": 50},
		map[string]any{"quality": qualityRef(98, "Bluray-576p Remux"), "title": "odd", "minSize": 1, "maxSize": 2},
	}
}

func (lib *Library) customFormats(kind string) []any {
	source := 6
	if kind == "sonarr" {
		source = 3
	}
	return []any{
		map[string]any{"id": 1, "name": "x265 (HD)", "includeCustomFormatWhenRenaming": false, "specifications": []any{
			map[string]any{"name": "x265", "implementation": "ReleaseTitleSpecification", "negate": false, "required": true,
				"fields": []any{field("value", `[xh][ ._-]?265|\bHEVC(\b|\d)`)}},
			map[string]any{"name": "Not 2160p", "implementation": "ResolutionSpecification", "negate": true, "required": true,
				"fields": []any{field("value", 2160)}}}},
		map[string]any{"id": 2, "name": "Repack", "specifications": []any{
			map[string]any{"name": "Repack", "implementation": "ReleaseTitleSpecification", "negate": false, "required": false,
				"fields": map[string]any{"value": `\b(repack|proper)\b`}}}},
		map[string]any{"id": 3, "name": "WEB source", "specifications": []any{
			map[string]any{"name": "WEB", "implementation": "SourceSpecification", "required": true, "fields": []any{field("value", source)}},
			map[string]any{"name": "Mystery", "implementation": "SomethingNewSpecification", "required": false, "fields": []any{field("value", 1)}}}},
	}
}

func (lib *Library) naming(kind string) map[string]any {
	switch kind {
	case "radarr":
		return map[string]any{"renameMovies": true, "replaceIllegalCharacters": true, "colonReplacementFormat": "dash",
			"standardMovieFormat": "{Movie Title} ({Release Year}) {Quality Full}", "movieFolderFormat": "{Movie Title} ({Release Year})"}
	case "sonarr":
		return map[string]any{"renameEpisodes": true, "replaceIllegalCharacters": true, "colonReplacementFormat": 4, "multiEpisodeStyle": 5,
			"standardEpisodeFormat": "{Series Title} - S{season:00}E{episode:00} - {Episode Title} {Quality Full}",
			"dailyEpisodeFormat":    "{Series Title} - {Air-Date} - {Episode Title} {Quality Full}",
			"animeEpisodeFormat":    "{Series Title} - S{season:00}E{episode:00} - {absolute:000} {Episode Title} {Custom Thing}",
			"seriesFolderFormat":    "{Series TitleYear}", "seasonFolderFormat": "Season {season:00}", "specialsFolderFormat": "Specials"}
	case "lidarr":
		return map[string]any{"renameTracks": false, "standardTrackFormat": "{track:00} - {Track Title}",
			"multiDiscTrackFormat": "{medium:00}-{track:00} - {Track Title}", "artistFolderFormat": "{Artist Name}"}
	}
	return map[string]any{}
}

func (lib *Library) arrIndexers(kind string) []any {
	cats := map[string][]int{"radarr": {2000, 2040, 2045}, "sonarr": {5000, 5040}, "lidarr": {3000, 3040}}[kind]
	return []any{
		map[string]any{"id": 1, "name": "Open Tracker", "implementation": "Torznab", "protocol": "torrent", "enableRss": true,
			"enableAutomaticSearch": true, "enableInteractiveSearch": true, "priority": 10, "tags": []int{},
			"fields": []any{field("baseUrl", "http://tracker.example:9117/open"), field("apiPath", "/api"), field("apiKey", "********"),
				field("categories", cats), field("animeCategories", []int{}), field("minimumSeeders", 3), field("seedCriteria.seedRatio", 1.5)}},
		map[string]any{"id": 2, "name": "Usenet Indexer", "implementation": "Newznab", "protocol": "usenet", "enableRss": true,
			"enableAutomaticSearch": true, "enableInteractiveSearch": true, "priority": 25, "tags": []int{},
			"fields": []any{field("baseUrl", "https://nzb.example"), field("apiPath", "/api"), field("apiKey", "plain-key-1"), field("categories", cats)}},
		map[string]any{"id": 3, "name": "Prowlarr Feed (Prowlarr)", "implementation": "Torznab", "protocol": "torrent", "enableRss": true,
			"enableAutomaticSearch": true, "enableInteractiveSearch": true, "priority": 25, "tags": []int{},
			"fields": []any{field("baseUrl", "http://prowlarr:9696/5/"), field("apiPath", "/api"), field("apiKey", "********"), field("categories", cats)}},
		map[string]any{"id": 4, "name": "Some RSS", "implementation": "TorrentRssIndexer", "protocol": "torrent", "enableRss": true,
			"priority": 25, "tags": []int{}, "fields": []any{field("baseUrl", "http://rss.example/feed")}},
	}
}

func (lib *Library) prowlarrIndexers() []any {
	return []any{
		map[string]any{"id": 5, "name": "Prowlarr Public", "implementation": "Cardigann", "protocol": "torrent", "enable": true, "priority": 20,
			"tags": []int{1}, "fields": []any{field("definitionFile", "public")},
			"capabilities": map[string]any{"categories": []any{map[string]any{"id": 2000, "name": "Movies", "subCategories": []any{map[string]any{"id": 2040}}},
				map[string]any{"id": 5000, "name": "TV", "subCategories": []any{}}}}},
		map[string]any{"id": 6, "name": "Generic Newznab", "implementation": "Newznab", "protocol": "usenet", "enable": true, "priority": 25,
			"tags": []int{}, "fields": []any{field("baseUrl", "https://nzb.example"), field("apiPath", "/api"), field("apiKey", "********")},
			"capabilities": map[string]any{"categories": []any{map[string]any{"id": 2000, "name": "Movies"}, map[string]any{"id": 3000, "name": "Audio"}}}},
	}
}

func (lib *Library) arrClients(kind string) []any {
	catField := map[string]string{"radarr": "movieCategory", "sonarr": "tvCategory", "lidarr": "musicCategory"}[kind]
	cat := map[string]string{"radarr": "radarr", "sonarr": "tv-sonarr", "lidarr": "music"}[kind]
	return []any{
		map[string]any{"id": 1, "name": "qBittorrent", "implementation": "QBittorrent", "protocol": "torrent", "enable": true, "priority": 1,
			"tags": []int{}, "fields": []any{field("host", "qbittorrent"), field("port", 8080), field("useSsl", false), field("urlBase", ""),
				field("username", "admin"), field("password", "********"), field(catField, cat)}},
		map[string]any{"id": 2, "name": "SABnzbd", "implementation": "Sabnzbd", "protocol": "usenet", "enable": true, "priority": 1,
			"tags": []int{}, "fields": []any{field("host", "sabnzbd"), field("port", 8080), field("apiKey", "********"), field(catField, cat)}},
		map[string]any{"id": 3, "name": "Blackhole", "implementation": "TorrentBlackhole", "protocol": "torrent", "enable": false,
			"priority": 1, "tags": []int{}, "fields": []any{field("torrentFolder", "/blackhole")}},
	}
}

func file(id int64, p string, size int64, qname string, group string, added time.Time) map[string]any {
	return map[string]any{"id": id, "path": p, "relativePath": path.Base(p), "size": size, "dateAdded": iso(added),
		"quality":      map[string]any{"quality": map[string]any{"id": 7, "name": qname}, "revision": map[string]any{"version": 1, "real": 0, "isRepack": false}},
		"releaseGroup": group, "sceneName": strings.TrimSuffix(path.Base(p), path.Ext(p)), "languages": []any{qualityRef(1, "English")},
		"mediaInfo": map[string]any{"videoCodec": "x264"}}
}

func tmdbPoster(id int64) []any {
	return []any{map[string]any{"coverType": "poster", "url": "/MediaCover/1/poster.jpg",
		"remoteUrl": fmt.Sprintf("https://image.tmdb.org/t/p/original/poster%d.jpg", id)}}
}

func (lib *Library) radarrMovies() []any {
	out := make([]any, 0, len(lib.Movies))
	for _, m := range lib.Movies {
		p := path.Join(lib.MovieRoot, m.MovieFolder())
		tags := m.Tags
		if tags == nil {
			tags = []int{}
		}
		x := map[string]any{"id": m.ID, "title": m.Title, "originalTitle": m.Title, "originalLanguage": qualityRef(1, "English"),
			"sortTitle": strings.ToLower(m.Title), "year": m.Year, "tmdbId": m.TMDb, "imdbId": m.IMDb, "path": p, "rootFolderPath": lib.MovieRoot,
			"monitored": m.Monitored, "minimumAvailability": "released", "qualityProfileId": 1, "tags": tags, "status": "released",
			"overview": "A film.", "runtime": 90, "inCinemas": fmt.Sprintf("%d-04-10T00:00:00Z", m.Year), "studio": "Blender",
			"genres": []string{"Animation"}, "certification": "PG", "images": tmdbPoster(m.TMDb), "hasFile": m.File != "",
			"added": iso(lib.Now.AddDate(-1, 0, 0)), "ratings": map[string]any{"tmdb": map[string]any{"value": 7.1, "votes": 100}}}
		if m.File != "" {
			x["movieFile"] = file(m.ID+1000, path.Join(p, m.File), 1500000000, m.Quality, "OPEN", lib.Now.AddDate(0, -6, 0))
		}
		if m.TMDb == 45745 {
			x["collection"] = map[string]any{"tmdbId": 777001, "title": "Open Movie Collection"}
		}
		out = append(out, x)
	}
	return out
}

func (lib *Library) sonarrSeries() []any {
	out := make([]any, 0, len(lib.Series))
	for _, v := range lib.Series {
		seasons := []any{}
		for n := 0; n < 30; n++ {
			if mon, ok := v.Seasons[n]; ok {
				seasons = append(seasons, map[string]any{"seasonNumber": n, "monitored": mon})
			}
		}
		tags := v.Tags
		if tags == nil {
			tags = []int{}
		}
		out = append(out, map[string]any{"id": v.ID, "title": v.Title, "sortTitle": strings.ToLower(v.Title), "year": v.Year, "tvdbId": v.TVDB,
			"tmdbId": v.TMDb, "imdbId": "", "tvMazeId": 0, "path": path.Join(lib.SeriesRoot, v.SeriesFolder()), "monitored": v.Monitored,
			"monitorNewItems": "all", "seriesType": v.Type, "seasonFolder": true, "qualityProfileId": 1, "languageProfileId": 1, "tags": tags,
			"status": "ended", "network": "Blender", "runtime": 12, "overview": "A show.", "genres": []string{"Animation"},
			"firstAired": fmt.Sprintf("%d-01-01T00:00:00Z", v.Year), "originalLanguage": qualityRef(1, "English"), "seasons": seasons,
			"images": []any{map[string]any{"coverType": "poster", "remoteUrl": "https://artworks.thetvdb.com/banners/poster.jpg"}}})
	}
	return out
}

func (lib *Library) seriesByID(id int64) *Series {
	for i := range lib.Series {
		if lib.Series[i].ID == id {
			return &lib.Series[i]
		}
	}
	return nil
}

func (lib *Library) sonarrEpisodes(seriesID int64) []any {
	v := lib.seriesByID(seriesID)
	out := []any{}
	if v == nil {
		return out
	}
	for _, e := range v.Episodes {
		out = append(out, map[string]any{"id": e.ID, "seriesId": v.ID, "seasonNumber": e.Season, "episodeNumber": e.Number,
			"absoluteEpisodeNumber": 0, "tvdbId": e.ID + 900000, "title": e.Title, "airDate": e.AirDate, "airDateUtc": e.AirDate + "T19:00:00Z",
			"runtime": 12, "monitored": e.Monitored, "hasFile": e.FileID > 0, "episodeFileId": e.FileID})
	}
	return out
}

func (lib *Library) sonarrFiles(seriesID int64) []any {
	v := lib.seriesByID(seriesID)
	out := []any{}
	if v == nil {
		return out
	}
	for _, e := range v.Episodes {
		if e.FileID > 0 {
			f := file(e.FileID, path.Join(lib.SeriesRoot, v.SeriesFolder(), e.File), 400000000, e.Quality, e.Group, lib.Now.AddDate(0, -3, 0))
			f["seriesId"], f["seasonNumber"] = v.ID, e.Season
			out = append(out, f)
		}
	}
	return out
}

func (lib *Library) lidarrArtists() []any {
	out := []any{}
	for _, a := range lib.Artists {
		out = append(out, map[string]any{"id": a.ID, "artistName": a.Name, "foreignArtistId": a.MBID, "sortName": a.Name,
			"disambiguation": "electronic duo", "artistType": "Group", "path": path.Join(lib.MusicRoot, a.Name), "monitored": a.Monitored,
			"monitorNewItems": "all", "qualityProfileId": 1, "metadataProfileId": 1, "tags": []int{}, "genres": []string{"electronic"}})
	}
	return out
}

func (lib *Library) lidarrAlbums() []any {
	out := []any{}
	for _, a := range lib.Artists {
		for _, al := range a.Albums {
			out = append(out, map[string]any{"id": al.ID, "artistId": a.ID, "title": al.Title, "foreignAlbumId": al.MBID, "disambiguation": "",
				"albumType": al.Type, "secondaryTypes": []any{}, "releaseDate": al.Date + "T00:00:00Z", "monitored": al.Monitored})
		}
	}
	return out
}

func (lib *Library) artistByID(id int64) *Artist {
	for i := range lib.Artists {
		if lib.Artists[i].ID == id {
			return &lib.Artists[i]
		}
	}
	return nil
}

func (lib *Library) lidarrTrackFiles(artistID int64) []any {
	out := []any{}
	a := lib.artistByID(artistID)
	if a == nil {
		return out
	}
	for _, al := range a.Albums {
		for _, t := range al.Tracks {
			out = append(out, map[string]any{"id": t.ID, "artistId": a.ID, "albumId": al.ID, "path": path.Join(lib.MusicRoot, a.Name, t.File),
				"size": 30000000, "quality": map[string]any{"quality": qualityRef(6, "FLAC"), "revision": map[string]any{"version": 1}},
				"releaseGroup": "", "sceneName": ""})
		}
	}
	return out
}

func (lib *Library) lidarrTracks(artistID int64) []any {
	out := []any{}
	a := lib.artistByID(artistID)
	if a == nil {
		return out
	}
	for _, al := range a.Albums {
		for _, t := range al.Tracks {
			out = append(out, map[string]any{"id": t.ID + 50000, "artistId": a.ID, "albumId": al.ID, "trackFileId": t.ID, "mediumNumber": t.Disc,
				"absoluteTrackNumber": t.Position, "trackNumber": fmt.Sprint(t.Position), "title": t.Title, "duration": 200000,
				"foreignRecordingId": fmt.Sprintf("rec-%d", t.ID), "hasFile": true})
		}
	}
	return out
}

func (lib *Library) blocklist(kind string) []any {
	key := map[string]string{"radarr": "movieId", "sonarr": "seriesId", "lidarr": "artistId"}[kind]
	return []any{map[string]any{"id": 1, key: 1, "sourceTitle": "Bad.Release.2008.1080p.CAM-NOPE", "date": iso(lib.Now.AddDate(0, 0, -10)),
		"protocol": "torrent", "indexer": "Open Tracker", "message": "fake or wrong", "torrentInfoHash": "ABCDEF0123456789ABCDEF0123456789ABCDEF01"}}
}

func (lib *Library) history(kind string) []any {
	key := map[string]string{"radarr": "movieId", "sonarr": "seriesId", "lidarr": "artistId"}[kind]
	ev := func(id int64, typ string, days int, title string) map[string]any {
		x := map[string]any{"id": id, key: 1, "eventType": typ, "sourceTitle": title, "date": iso(lib.Now.AddDate(0, 0, -days)),
			"quality": map[string]any{"quality": qualityRef(7, "Bluray-1080p")}, "data": map[string]any{"indexer": "Open Tracker", "downloadClientName": "qBittorrent"}}
		if kind == "sonarr" {
			x["episodeId"] = 12
		}
		return x
	}
	return []any{
		ev(4, "downloadFolderImported", 5, "Big.Buck.Bunny.2008.1080p.BluRay.x264-OPEN"),
		ev(3, "grabbed", 6, "Big.Buck.Bunny.2008.1080p.BluRay.x264-OPEN"),
		ev(2, "downloadFailed", 8, "Bad.Release.2008.1080p.CAM-NOPE"),
		ev(1, "grabbed", 400, "Too.Old.Release"),
	}
}

func (lib *Library) seerrUsers() []any {
	out := []any{}
	for _, u := range lib.Users {
		x := map[string]any{"id": u.ID, "email": u.Email, "username": u.Username, "plexUsername": nil, "displayName": u.Username,
			"permissions": u.Permissions, "userType": 2, "createdAt": iso(lib.Now.AddDate(-1, 0, 0)), "requestCount": 1}
		if u.ID == 2 {
			x["movieQuotaLimit"], x["movieQuotaDays"] = 3, 7
		}
		out = append(out, x)
	}
	return out
}

func (lib *Library) seerrRequests() []any {
	ref := func(id int64) map[string]any { return map[string]any{"id": id} }
	return []any{
		map[string]any{"id": 1, "status": 2, "type": "movie", "is4k": false, "createdAt": iso(lib.Now.AddDate(0, -2, 0)),
			"updatedAt": iso(lib.Now.AddDate(0, -1, 0)), "media": map[string]any{"tmdbId": 10378, "mediaType": "movie", "status": 5},
			"requestedBy": ref(2), "modifiedBy": ref(1), "seasons": []any{}},
		map[string]any{"id": 2, "status": 1, "type": "movie", "is4k": false, "createdAt": iso(lib.Now.AddDate(0, 0, -3)),
			"updatedAt": iso(lib.Now.AddDate(0, 0, -3)), "media": map[string]any{"tmdbId": 900002, "mediaType": "movie", "status": 2},
			"requestedBy": ref(2), "modifiedBy": nil},
		map[string]any{"id": 3, "status": 2, "type": "tv", "is4k": false, "createdAt": iso(lib.Now.AddDate(0, 0, -20)),
			"updatedAt": iso(lib.Now.AddDate(0, 0, -19)), "media": map[string]any{"tmdbId": 800434, "tvdbId": 434343, "mediaType": "tv", "status": 4},
			"requestedBy": ref(3), "modifiedBy": ref(1), "seasons": []any{map[string]any{"seasonNumber": 1, "status": 2}}},
		map[string]any{"id": 4, "status": 3, "type": "movie", "is4k": true, "createdAt": iso(lib.Now.AddDate(0, 0, -30)),
			"updatedAt": iso(lib.Now.AddDate(0, 0, -29)), "media": map[string]any{"tmdbId": 45745, "mediaType": "movie", "status": 1, "status4k": 1},
			"requestedBy": ref(99), "modifiedBy": ref(1)},
	}
}

func (lib *Library) seerrIssues() []any {
	return []any{map[string]any{"id": 1, "issueType": 3, "status": 1, "problemSeason": 0, "problemEpisode": 0,
		"media": map[string]any{"tmdbId": 10378, "mediaType": "movie"}, "createdBy": map[string]any{"id": 2}, "modifiedBy": nil,
		"createdAt": iso(lib.Now.AddDate(0, 0, -7)), "updatedAt": iso(lib.Now.AddDate(0, 0, -6)),
		"comments": []any{
			map[string]any{"id": 1, "user": map[string]any{"id": 2}, "message": "No Croatian subtitles", "createdAt": iso(lib.Now.AddDate(0, 0, -7))},
			map[string]any{"id": 2, "user": map[string]any{"id": 1}, "message": "Looking into it", "createdAt": iso(lib.Now.AddDate(0, 0, -6))}}}}
}
