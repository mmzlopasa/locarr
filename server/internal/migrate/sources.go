package migrate

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

// Detected is what a test of a source found.
type Detected struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

// ReadOptions say what to read from a source.
type ReadOptions struct {
	Scopes Scopes
	// HistorySince limits the history; BlocklistLimit and HistoryLimit cap them.
	HistorySince   time.Time
	HistoryLimit   int
	BlocklistLimit int
	// Progress reports done of total steps (may be nil).
	Progress func(done, total int, what string)
}

func (o *ReadOptions) progress(done, total int, what string) {
	if o.Progress != nil {
		o.Progress(done, total, what)
	}
}

// Detect checks that conn is the application it claims, reachable with its
// key, and of a supported version.
func Detect(ctx context.Context, conn Conn, pol httpx.Policy) (Detected, error) {
	c, err := newClient(conn, pol)
	if err != nil {
		return Detected{}, err
	}
	return detect(ctx, c)
}

func detect(ctx context.Context, c *client) (Detected, error) {
	k := c.conn.Kind
	switch k {
	case Sonarr, Radarr, Lidarr, Prowlarr:
		st, err := arrDetect(ctx, c)
		return Detected{AppName: firstNonEmpty(st.AppName, k.Label()), Version: st.Version}, err
	case Bazarr:
		var st struct {
			Data struct {
				Version string `json:"bazarr_version"`
			} `json:"data"`
		}
		if err := c.get(ctx, "/api/system/status", nil, &st); err != nil {
			return Detected{}, err
		}
		if st.Data.Version == "" {
			return Detected{}, &SourceError{Code: "wrong_app", Message: "this address does not answer like Bazarr"}
		}
		return Detected{AppName: "Bazarr", Version: strings.TrimPrefix(st.Data.Version, "v")}, nil
	case Overseerr, Seerr:
		var st struct {
			Version string `json:"version"`
		}
		if err := c.get(ctx, "/api/v1/status", nil, &st); err != nil {
			return Detected{}, err
		}
		if st.Version == "" {
			return Detected{}, &SourceError{Code: "wrong_app", Message: "this address does not answer like " + k.Label()}
		}
		// The status call is public: check the key with a call that needs it.
		if err := c.get(ctx, "/api/v1/settings/main", nil, nil); err != nil {
			return Detected{}, err
		}
		return Detected{AppName: k.Label(), Version: st.Version}, nil
	case QBittorrent:
		if err := c.qbitLogin(ctx); err != nil {
			return Detected{}, err
		}
		v, err := c.getText(ctx, "/api/v2/app/version")
		if err != nil {
			return Detected{}, err
		}
		v = strings.TrimPrefix(v, "v")
		if !versionAtLeast(v, minVersion[QBittorrent]) {
			return Detected{}, &SourceError{Code: "unsupported_version", Message: "qBittorrent " + v + " is too old; 4.1 or later is needed"}
		}
		return Detected{AppName: "qBittorrent", Version: v}, nil
	case SABnzbd:
		var st struct {
			Version string `json:"version"`
		}
		if err := c.get(ctx, "/api", url.Values{"mode": {"version"}}, &st); err != nil {
			return Detected{}, err
		}
		// mode=version needs no key; get_config does.
		var cfg struct {
			Status *bool  `json:"status"`
			Error  string `json:"error"`
		}
		if err := c.get(ctx, "/api", url.Values{"mode": {"get_config"}, "section": {"misc"}}, &cfg); err != nil {
			return Detected{}, err
		}
		if cfg.Error != "" {
			return Detected{}, &SourceError{Code: "unauthorized", Message: "SABnzbd refused the API key (" + cfg.Error + ")"}
		}
		if st.Version == "" {
			return Detected{}, &SourceError{Code: "wrong_app", Message: "this address does not answer like SABnzbd"}
		}
		return Detected{AppName: "SABnzbd", Version: st.Version}, nil
	}
	return Detected{}, &SourceError{Code: "wrong_app", Message: "unknown application"}
}

func firstNonEmpty(list ...string) string {
	for _, s := range list {
		if s != "" {
			return s
		}
	}
	return ""
}

// Read reads a source. Only GET requests are sent (and qBittorrent's sign-in).
func Read(ctx context.Context, conn Conn, pol httpx.Policy, o ReadOptions) (*Snapshot, error) {
	c, err := newClient(conn, pol)
	if err != nil {
		return nil, err
	}
	d, err := detect(ctx, c)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Kind: conn.Kind, URL: c.conn.URL, AppName: d.AppName, Version: d.Version, Key: SourceKey(conn.Kind, conn.URL)}
	if o.HistoryLimit <= 0 {
		o.HistoryLimit = 5000
	}
	if o.BlocklistLimit <= 0 {
		o.BlocklistLimit = 5000
	}
	switch conn.Kind {
	case Sonarr, Radarr, Lidarr:
		err = readArr(ctx, c, s, o)
	case Prowlarr:
		err = readProwlarr(ctx, c, s)
	case Bazarr:
		err = readBazarr(ctx, c, s)
	case Overseerr, Seerr:
		err = readSeerr(ctx, c, s, o)
	case QBittorrent:
		err = readQBit(ctx, c, s)
	case SABnzbd:
		err = readSAB(ctx, c, s)
	}
	return s, err
}

// SourceKey is the identity imported items are linked under.
func SourceKey(k Kind, u string) string {
	return string(k) + "|" + strings.ToLower(strings.TrimRight(strings.TrimSpace(u), "/"))
}

func readArr(ctx context.Context, c *client, s *Snapshot, o ReadOptions) error {
	k := c.conn.Kind
	var err error
	if s.Tags, err = arrTags(ctx, c); err != nil {
		return err
	}
	// Settings are always read: the library needs profiles and root folders.
	if err := readArrSettings(ctx, c, s); err != nil {
		return err
	}
	switch k {
	case Sonarr:
		if major(s.Version) < 4 {
			if err := c.get(ctx, "/api/v3/languageprofile", nil, &rawLanguageProfiles{s}); err != nil {
				s.warn("language profiles could not be read: %v", err)
			}
		}
	case Lidarr:
		if s.MetadataProfiles, err = lidarrMetadataProfiles(ctx, c); err != nil {
			return err
		}
	}
	if o.Scopes.Settings {
		if err := readExclusions(ctx, c, s); err != nil {
			s.warn("list exclusions could not be read: %v", err)
		}
		if k == Radarr {
			if err := radarrCollections(ctx, c, s); err != nil {
				s.warn("collections could not be read: %v", err)
			}
		}
	}
	if o.Scopes.Library {
		switch k {
		case Radarr:
			err = radarrMovies(ctx, c, s, o)
		case Sonarr:
			err = sonarrSeries(ctx, c, s, o)
		case Lidarr:
			err = lidarrArtists(ctx, c, s, o)
		}
		if err != nil {
			return err
		}
	}
	if o.Scopes.History {
		if s.Blocklist, err = arrBlocklist(ctx, c, o.BlocklistLimit); err != nil {
			s.warn("blocklist could not be read: %v", err)
		}
		if s.History, err = arrHistory(ctx, c, o.HistorySince, o.HistoryLimit); err != nil {
			s.warn("history could not be read: %v", err)
		}
	}
	return nil
}

// rawLanguageProfiles decodes Sonarr v3 language profiles into the snapshot.
type rawLanguageProfiles struct{ s *Snapshot }

func (r *rawLanguageProfiles) UnmarshalJSON(b []byte) error {
	var raw []struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Languages []struct {
			Language QualityRef `json:"language"`
			Allowed  bool       `json:"allowed"`
		} `json:"languages"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	for _, p := range raw {
		lp := LanguageProfile{ID: p.ID, Name: p.Name}
		for _, l := range p.Languages {
			if l.Allowed {
				lp.Languages = append(lp.Languages, l.Language.Name)
			}
		}
		r.s.LanguageProfiles = append(r.s.LanguageProfiles, lp)
	}
	return nil
}

func readExclusions(ctx context.Context, c *client, s *Snapshot) error {
	switch c.conn.Kind {
	case Radarr:
		var raw []struct {
			TMDbID int64  `json:"tmdbId"`
			Title  string `json:"movieTitle"`
			Year   int    `json:"movieYear"`
		}
		if err := c.get(ctx, "/api/v3/exclusions", nil, &raw); err != nil {
			if err2 := c.get(ctx, "/api/v3/importlistexclusion", nil, &raw); err2 != nil {
				return err
			}
		}
		for _, r := range raw {
			s.Exclusions = append(s.Exclusions, Exclusion{TMDbID: r.TMDbID, Title: r.Title, Year: r.Year})
		}
	case Sonarr:
		var raw []struct {
			TVDbID int64  `json:"tvdbId"`
			Title  string `json:"title"`
		}
		if err := c.get(ctx, "/api/v3/importlistexclusion", nil, &raw); err != nil {
			return err
		}
		for _, r := range raw {
			s.Exclusions = append(s.Exclusions, Exclusion{TVDbID: r.TVDbID, Title: r.Title})
		}
	case Lidarr:
		var raw []struct {
			ForeignID  string `json:"foreignId"`
			ArtistName string `json:"artistName"`
		}
		if err := c.get(ctx, "/api/v1/importlistexclusion", nil, &raw); err != nil {
			return err
		}
		for _, r := range raw {
			s.Exclusions = append(s.Exclusions, Exclusion{MBID: r.ForeignID, Title: r.ArtistName})
		}
	}
	return nil
}

type rawFileQuality struct {
	Quality  QualityRef `json:"quality"`
	Revision struct {
		Version int `json:"version"`
	} `json:"revision"`
}

type rawMediaFile struct {
	ID           int64          `json:"id"`
	Path         string         `json:"path"`
	Size         int64          `json:"size"`
	Quality      rawFileQuality `json:"quality"`
	ReleaseGroup string         `json:"releaseGroup"`
	SceneName    string         `json:"sceneName"`
	Edition      string         `json:"edition"`
	Languages    []QualityRef   `json:"languages"`
	DateAdded    time.Time      `json:"dateAdded"`
}

func (r *rawMediaFile) file() *MediaFile {
	f := &MediaFile{ID: r.ID, Path: r.Path, Size: r.Size, Quality: r.Quality.Quality.Name, Revision: r.Quality.Revision.Version,
		ReleaseGroup: r.ReleaseGroup, SceneName: r.SceneName, Edition: r.Edition, DateAdded: r.DateAdded}
	for _, l := range r.Languages {
		f.Languages = append(f.Languages, l.Name)
	}
	return f
}

func radarrMovies(ctx context.Context, c *client, s *Snapshot, o ReadOptions) error {
	type rawMovie struct {
		ID                  int64         `json:"id"`
		Title               string        `json:"title"`
		OriginalTitle       string        `json:"originalTitle"`
		OriginalLanguage    *QualityRef   `json:"originalLanguage"`
		Year                int           `json:"year"`
		TMDbID              int64         `json:"tmdbId"`
		IMDbID              string        `json:"imdbId"`
		Path                string        `json:"path"`
		Monitored           bool          `json:"monitored"`
		MinimumAvailability string        `json:"minimumAvailability"`
		QualityProfileID    int64         `json:"qualityProfileId"`
		Tags                []int64       `json:"tags"`
		Status              string        `json:"status"`
		Overview            string        `json:"overview"`
		Runtime             int           `json:"runtime"`
		InCinemas           string        `json:"inCinemas"`
		DigitalRelease      string        `json:"digitalRelease"`
		PhysicalRelease     string        `json:"physicalRelease"`
		Studio              string        `json:"studio"`
		Certification       string        `json:"certification"`
		Genres              []string      `json:"genres"`
		Images              []rawImage    `json:"images"`
		HasFile             bool          `json:"hasFile"`
		MovieFile           *rawMediaFile `json:"movieFile"`
		Collection          *struct {
			TMDbID int64  `json:"tmdbId"`
			Title  string `json:"title"`
			Name   string `json:"name"`
		} `json:"collection"`
	}
	o.progress(0, 1, "movies")
	err := list(ctx, c, "/api/v3/movie", nil, func(r rawMovie) {
		m := Movie{ID: r.ID, Title: r.Title, OriginalTitle: r.OriginalTitle, Year: r.Year, TMDbID: r.TMDbID, IMDbID: r.IMDbID,
			Path: r.Path, Monitored: r.Monitored, MinimumAvailability: r.MinimumAvailability, QualityProfileID: r.QualityProfileID,
			Tags: r.Tags, Status: r.Status, Overview: r.Overview, Runtime: r.Runtime, InCinemas: date(r.InCinemas),
			DigitalRelease: date(r.DigitalRelease), PhysicalRelease: date(r.PhysicalRelease), Studio: r.Studio,
			Certification: r.Certification, Genres: r.Genres, Poster: poster(r.Images)}
		if r.OriginalLanguage != nil {
			m.OriginalLanguage = r.OriginalLanguage.Name
		}
		if r.Collection != nil {
			m.CollectionTMDbID, m.CollectionTitle = r.Collection.TMDbID, firstNonEmpty(r.Collection.Title, r.Collection.Name)
		}
		if r.MovieFile != nil && r.MovieFile.Path != "" {
			m.File = r.MovieFile.file()
		}
		s.Movies = append(s.Movies, m)
	})
	o.progress(1, 1, "movies")
	return err
}

func radarrCollections(ctx context.Context, c *client, s *Snapshot) error {
	return c.get(ctx, "/api/v3/collection", nil, &s.Collections)
}

func sonarrSeries(ctx context.Context, c *client, s *Snapshot, o ReadOptions) error {
	type rawSeries struct {
		ID                int64       `json:"id"`
		Title             string      `json:"title"`
		Year              int         `json:"year"`
		TVDbID            int64       `json:"tvdbId"`
		TMDbID            int64       `json:"tmdbId"`
		IMDbID            string      `json:"imdbId"`
		TVMazeID          int64       `json:"tvMazeId"`
		Path              string      `json:"path"`
		Monitored         bool        `json:"monitored"`
		MonitorNewItems   string      `json:"monitorNewItems"`
		SeriesType        string      `json:"seriesType"`
		SeasonFolder      bool        `json:"seasonFolder"`
		QualityProfileID  int64       `json:"qualityProfileId"`
		LanguageProfileID int64       `json:"languageProfileId"`
		Tags              []int64     `json:"tags"`
		Status            string      `json:"status"`
		Network           string      `json:"network"`
		Runtime           int         `json:"runtime"`
		Overview          string      `json:"overview"`
		Genres            []string    `json:"genres"`
		Certification     string      `json:"certification"`
		FirstAired        string      `json:"firstAired"`
		OriginalLanguage  *QualityRef `json:"originalLanguage"`
		Seasons           []struct {
			SeasonNumber int  `json:"seasonNumber"`
			Monitored    bool `json:"monitored"`
		} `json:"seasons"`
		Images []rawImage `json:"images"`
	}
	raw, err := all[rawSeries](ctx, c, "/api/v3/series", nil)
	if err != nil {
		return err
	}
	s.Series = make([]Series, len(raw))
	for i, r := range raw {
		v := Series{ID: r.ID, Title: r.Title, Year: r.Year, TVDbID: r.TVDbID, TMDbID: r.TMDbID, IMDbID: r.IMDbID, TVMazeID: r.TVMazeID,
			Path: r.Path, Monitored: r.Monitored, MonitorNewItems: r.MonitorNewItems, SeriesType: r.SeriesType, SeasonFolder: r.SeasonFolder,
			QualityProfileID: r.QualityProfileID, LanguageProfileID: r.LanguageProfileID, Tags: r.Tags, Status: r.Status, Network: r.Network,
			Runtime: r.Runtime, Overview: r.Overview, Genres: r.Genres, Certification: r.Certification, FirstAired: date(r.FirstAired)}
		if r.OriginalLanguage != nil {
			v.OriginalLanguage = r.OriginalLanguage.Name
		}
		for _, sn := range r.Seasons {
			v.Seasons = append(v.Seasons, SeasonInfo{Number: sn.SeasonNumber, Monitored: sn.Monitored})
		}
		s.Series[i] = v
	}
	type rawEpisode struct {
		ID                    int64      `json:"id"`
		SeasonNumber          int        `json:"seasonNumber"`
		EpisodeNumber         int        `json:"episodeNumber"`
		AbsoluteEpisodeNumber int        `json:"absoluteEpisodeNumber"`
		TVDbID                int64      `json:"tvdbId"`
		Title                 string     `json:"title"`
		AirDate               string     `json:"airDate"`
		AirDateUTC            *time.Time `json:"airDateUtc"`
		Runtime               int        `json:"runtime"`
		Monitored             bool       `json:"monitored"`
		EpisodeFileID         int64      `json:"episodeFileId"`
	}
	return parallel(ctx, len(s.Series), 4, func(i int) error {
		v := &s.Series[i]
		q := url.Values{"seriesId": {strconv.FormatInt(v.ID, 10)}}
		eps, err := all[rawEpisode](ctx, c, "/api/v3/episode", q)
		if err != nil {
			return err
		}
		for _, e := range eps {
			v.Episodes = append(v.Episodes, Episode{ID: e.ID, Season: e.SeasonNumber, Number: e.EpisodeNumber, Absolute: e.AbsoluteEpisodeNumber,
				TVDbID: e.TVDbID, Title: e.Title, AirDate: e.AirDate, AirDateUTC: e.AirDateUTC, Runtime: e.Runtime, Monitored: e.Monitored,
				FileID: e.EpisodeFileID})
		}
		files, err := all[rawMediaFile](ctx, c, "/api/v3/episodefile", q)
		if err != nil {
			return err
		}
		for i := range files {
			v.Files = append(v.Files, *files[i].file())
		}
		return nil
	}, func(done int) { o.progress(done, len(s.Series), "series") })
}

// parallel runs fn for 0..n-1 on workers goroutines; the first error wins.
func parallel(ctx context.Context, n, workers int, fn func(int) error, progress func(done int)) error {
	if n == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu      sync.Mutex
		first   error
		done    int
		wg      sync.WaitGroup
		indexes = make(chan int)
	)
	for range min(workers, n) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indexes {
				err := fn(i)
				mu.Lock()
				done++
				if err != nil && first == nil {
					first = err
					cancel()
				}
				d := done
				mu.Unlock()
				if progress != nil && (d%25 == 0 || d == n) {
					progress(d)
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			break
		}
		indexes <- i
	}
	close(indexes)
	wg.Wait()
	if first != nil {
		return first
	}
	return context.Cause(ctx)
}

func lidarrMetadataProfiles(ctx context.Context, c *client) ([]MetadataProfile, error) {
	type allowed struct {
		Allowed bool `json:"allowed"`
	}
	var raw []struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Primary []struct {
			AlbumType QualityRef `json:"albumType"`
			allowed
		} `json:"primaryAlbumTypes"`
		Secondary []struct {
			AlbumType QualityRef `json:"albumType"`
			allowed
		} `json:"secondaryAlbumTypes"`
		Statuses []struct {
			ReleaseStatus QualityRef `json:"releaseStatus"`
			allowed
		} `json:"releaseStatuses"`
	}
	if err := c.get(ctx, "/api/v1/metadataprofile", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]MetadataProfile, 0, len(raw))
	for _, r := range raw {
		p := MetadataProfile{ID: r.ID, Name: r.Name}
		for _, x := range r.Primary {
			if x.Allowed {
				p.Primary = append(p.Primary, x.AlbumType.Name)
			}
		}
		for _, x := range r.Secondary {
			if x.Allowed {
				p.Secondary = append(p.Secondary, x.AlbumType.Name)
			}
		}
		for _, x := range r.Statuses {
			if x.Allowed {
				p.Statuses = append(p.Statuses, x.ReleaseStatus.Name)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func lidarrArtists(ctx context.Context, c *client, s *Snapshot, o ReadOptions) error {
	type rawArtist struct {
		ID                int64    `json:"id"`
		ArtistName        string   `json:"artistName"`
		ForeignArtistID   string   `json:"foreignArtistId"`
		SortName          string   `json:"sortName"`
		Disambiguation    string   `json:"disambiguation"`
		ArtistType        string   `json:"artistType"`
		Path              string   `json:"path"`
		Monitored         bool     `json:"monitored"`
		MonitorNewItems   string   `json:"monitorNewItems"`
		QualityProfileID  int64    `json:"qualityProfileId"`
		MetadataProfileID int64    `json:"metadataProfileId"`
		Tags              []int64  `json:"tags"`
		Genres            []string `json:"genres"`
	}
	type rawAlbum struct {
		ID             int64        `json:"id"`
		ArtistID       int64        `json:"artistId"`
		Title          string       `json:"title"`
		ForeignAlbumID string       `json:"foreignAlbumId"`
		Disambiguation string       `json:"disambiguation"`
		AlbumType      string       `json:"albumType"`
		SecondaryTypes []QualityRef `json:"secondaryTypes"`
		ReleaseDate    string       `json:"releaseDate"`
		Monitored      bool         `json:"monitored"`
	}
	artists, err := all[rawArtist](ctx, c, "/api/v1/artist", nil)
	if err != nil {
		return err
	}
	albums, err := all[rawAlbum](ctx, c, "/api/v1/album", nil)
	if err != nil {
		return err
	}
	byArtist := map[int64][]Album{}
	for _, a := range albums {
		al := Album{ID: a.ID, Title: a.Title, MBID: a.ForeignAlbumID, Disambiguation: a.Disambiguation, Type: a.AlbumType,
			ReleaseDate: date(a.ReleaseDate), Monitored: a.Monitored}
		for _, st := range a.SecondaryTypes {
			al.SecondaryTypes = append(al.SecondaryTypes, st.Name)
		}
		byArtist[a.ArtistID] = append(byArtist[a.ArtistID], al)
	}
	s.Artists = make([]Artist, len(artists))
	for i, a := range artists {
		s.Artists[i] = Artist{ID: a.ID, Name: a.ArtistName, MBID: a.ForeignArtistID, SortName: a.SortName, Disambiguation: a.Disambiguation,
			Type: a.ArtistType, Path: a.Path, Monitored: a.Monitored, MonitorNewItems: a.MonitorNewItems, QualityProfileID: a.QualityProfileID,
			MetadataProfileID: a.MetadataProfileID, Tags: a.Tags, Genres: a.Genres, Albums: byArtist[a.ID]}
	}
	type rawTrackFile struct {
		ID           int64          `json:"id"`
		AlbumID      int64          `json:"albumId"`
		Path         string         `json:"path"`
		Size         int64          `json:"size"`
		Quality      rawFileQuality `json:"quality"`
		ReleaseGroup string         `json:"releaseGroup"`
		SceneName    string         `json:"sceneName"`
	}
	type rawTrack struct {
		TrackFileID         int64  `json:"trackFileId"`
		AlbumID             int64  `json:"albumId"`
		MediumNumber        int    `json:"mediumNumber"`
		AbsoluteTrackNumber int    `json:"absoluteTrackNumber"`
		TrackNumber         string `json:"trackNumber"`
		Title               string `json:"title"`
		Duration            int    `json:"duration"`
		ForeignRecordingID  string `json:"foreignRecordingId"`
	}
	return parallel(ctx, len(s.Artists), 4, func(i int) error {
		a := &s.Artists[i]
		q := url.Values{"artistId": {strconv.FormatInt(a.ID, 10)}}
		files, err := all[rawTrackFile](ctx, c, "/api/v1/trackfile", q)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}
		tracks, err := all[rawTrack](ctx, c, "/api/v1/track", q)
		if err != nil {
			return err
		}
		byFile := map[int64]rawTrack{}
		for _, t := range tracks {
			if t.TrackFileID > 0 {
				byFile[t.TrackFileID] = t
			}
		}
		idx := map[int64]int{}
		for j := range a.Albums {
			idx[a.Albums[j].ID] = j
		}
		for _, f := range files {
			j, ok := idx[f.AlbumID]
			if !ok {
				continue
			}
			t := byFile[f.ID]
			pos := t.AbsoluteTrackNumber
			if pos == 0 {
				pos, _ = strconv.Atoi(strings.TrimLeft(t.TrackNumber, "ABCDabcd"))
			}
			a.Albums[j].Files = append(a.Albums[j].Files, TrackFile{ID: f.ID, Path: f.Path, Size: f.Size, Quality: f.Quality.Quality.Name,
				Disc: t.MediumNumber, Position: pos, RecordingMBID: t.ForeignRecordingID, Title: t.Title, DurationMS: t.Duration,
				ReleaseGroup: f.ReleaseGroup, SceneName: f.SceneName})
		}
		return nil
	}, func(done int) { o.progress(done, len(s.Artists), "artists") })
}

func readProwlarr(ctx context.Context, c *client, s *Snapshot) error {
	var err error
	if s.Tags, err = arrTags(ctx, c); err != nil {
		return err
	}
	if s.Indexers, err = arrIndexers(ctx, c); err != nil {
		return err
	}
	var apps []rawProvider
	if err := c.get(ctx, "/api/v1/applications", nil, &apps); err != nil {
		s.warn("applications could not be read: %v", err)
	}
	for _, a := range apps {
		f := fieldMap(a.Fields)
		app := App{Name: a.Name, Implementation: a.Implementation}
		app.BaseURL, _ = f["baseUrl"].(string)
		app.SyncLevel, _ = f["syncLevel"].(string)
		s.Apps = append(s.Apps, app)
	}
	return nil
}

// bazarrBool reads Bazarr's "True"/"False" strings (and real booleans).
func bazarrBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}

func readBazarr(ctx context.Context, c *client, s *Snapshot) error {
	var profiles []struct {
		ProfileID int64 `json:"profileId"`
		Name      string
		Cutoff    any `json:"cutoff"`
		Items     []struct {
			Language string `json:"language"`
			Forced   any    `json:"forced"`
			HI       any    `json:"hi"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/api/system/languages/profiles", nil, &profiles); err != nil {
		return err
	}
	for _, p := range profiles {
		sp := SubtitleProfile{ID: p.ProfileID, Name: p.Name, Cutoff: p.Cutoff != nil}
		for _, it := range p.Items {
			sp.Languages = append(sp.Languages, SubtitleLanguage{Code: strings.ToLower(it.Language), Forced: bazarrBool(it.Forced), HI: bazarrBool(it.HI)})
		}
		s.SubtitleProfiles = append(s.SubtitleProfiles, sp)
	}
	var settings map[string]any
	if err := c.get(ctx, "/api/system/settings", nil, &settings); err != nil {
		s.warn("provider settings could not be read: %v", err)
	} else {
		general, _ := settings["general"].(map[string]any)
		enabled, _ := general["enabled_providers"].([]any)
		for _, e := range enabled {
			name, _ := e.(string)
			if name == "" {
				continue
			}
			p := SubtitleProvider{Name: name, Fields: map[string]string{}}
			if sec, ok := settings[strings.ReplaceAll(name, ".", "")].(map[string]any); ok {
				for k, v := range sec {
					if str, ok := v.(string); ok {
						p.Fields[k] = str
					}
				}
			}
			s.SubtitleProviders = append(s.SubtitleProviders, p)
		}
	}
	var movies struct {
		Data []struct {
			RadarrID  int64  `json:"radarrId"`
			Title     string `json:"title"`
			ProfileID *int64 `json:"profileId"`
			TMDbID    int64  `json:"tmdbId"`
			IMDbID    string `json:"imdbId"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/api/movies", url.Values{"start": {"0"}, "length": {"-1"}}, &movies); err != nil {
		s.warn("movies could not be read: %v", err)
	}
	for _, m := range movies.Data {
		if m.ProfileID != nil && *m.ProfileID > 0 {
			s.SubtitleAssignments = append(s.SubtitleAssignments, SubtitleAssignment{Kind: "movie", AppID: m.RadarrID, TMDbID: m.TMDbID,
				IMDbID: m.IMDbID, Title: m.Title, ProfileID: *m.ProfileID})
		}
	}
	var series struct {
		Data []struct {
			SonarrID  int64  `json:"sonarrSeriesId"`
			Title     string `json:"title"`
			ProfileID *int64 `json:"profileId"`
			TVDbID    int64  `json:"tvdbId"`
			IMDbID    string `json:"imdbId"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/api/series", url.Values{"start": {"0"}, "length": {"-1"}}, &series); err != nil {
		s.warn("series could not be read: %v", err)
	}
	for _, v := range series.Data {
		if v.ProfileID != nil && *v.ProfileID > 0 {
			s.SubtitleAssignments = append(s.SubtitleAssignments, SubtitleAssignment{Kind: "series", AppID: v.SonarrID, TVDbID: v.TVDbID,
				IMDbID: v.IMDbID, Title: v.Title, ProfileID: *v.ProfileID})
		}
	}
	return nil
}

type seerrRef struct {
	ID int64 `json:"id"`
}

func readSeerr(ctx context.Context, c *client, s *Snapshot, o ReadOptions) error {
	var main struct {
		DefaultPermissions int64 `json:"defaultPermissions"`
		DefaultQuotas      struct {
			Movie struct {
				QuotaLimit int `json:"quotaLimit"`
				QuotaDays  int `json:"quotaDays"`
			} `json:"movie"`
			TV struct {
				QuotaLimit int `json:"quotaLimit"`
				QuotaDays  int `json:"quotaDays"`
			} `json:"tv"`
		} `json:"defaultQuotas"`
	}
	if err := c.get(ctx, "/api/v1/settings/main", nil, &main); err != nil {
		return err
	}
	s.DefaultPermissions = main.DefaultPermissions
	s.DefaultQuotas = map[string]Quota{}
	if main.DefaultQuotas.Movie.QuotaLimit > 0 {
		s.DefaultQuotas["movie"] = Quota{Limit: main.DefaultQuotas.Movie.QuotaLimit, Days: main.DefaultQuotas.Movie.QuotaDays}
	}
	if main.DefaultQuotas.TV.QuotaLimit > 0 {
		s.DefaultQuotas["series"] = Quota{Limit: main.DefaultQuotas.TV.QuotaLimit, Days: main.DefaultQuotas.TV.QuotaDays}
	}
	if !o.Scopes.Users {
		return nil
	}
	type rawUser struct {
		ID               int64     `json:"id"`
		Email            string    `json:"email"`
		Username         string    `json:"username"`
		PlexUsername     string    `json:"plexUsername"`
		JellyfinUsername string    `json:"jellyfinUsername"`
		DisplayName      string    `json:"displayName"`
		Permissions      int64     `json:"permissions"`
		UserType         int       `json:"userType"`
		MovieQuotaLimit  *int      `json:"movieQuotaLimit"`
		MovieQuotaDays   *int      `json:"movieQuotaDays"`
		TVQuotaLimit     *int      `json:"tvQuotaLimit"`
		TVQuotaDays      *int      `json:"tvQuotaDays"`
		CreatedAt        time.Time `json:"createdAt"`
	}
	users, err := seerrPages[rawUser](ctx, c, "/api/v1/user", nil)
	if err != nil {
		return err
	}
	for _, u := range users {
		x := User{ID: u.ID, Username: firstNonEmpty(u.Username, u.PlexUsername, u.JellyfinUsername), Email: u.Email,
			DisplayName: u.DisplayName, Permissions: u.Permissions, UserType: u.UserType, CreatedAt: u.CreatedAt, Quotas: map[string]Quota{}}
		if u.MovieQuotaLimit != nil && *u.MovieQuotaLimit > 0 {
			x.Quotas["movie"] = Quota{Limit: *u.MovieQuotaLimit, Days: deref(u.MovieQuotaDays, 7)}
		}
		if u.TVQuotaLimit != nil && *u.TVQuotaLimit > 0 {
			x.Quotas["series"] = Quota{Limit: *u.TVQuotaLimit, Days: deref(u.TVQuotaDays, 7)}
		}
		s.Users = append(s.Users, x)
	}
	type rawRequest struct {
		ID     int64  `json:"id"`
		Status int    `json:"status"`
		Type   string `json:"type"`
		Is4K   bool   `json:"is4k"`
		Media  struct {
			TMDbID    int64  `json:"tmdbId"`
			TVDbID    int64  `json:"tvdbId"`
			MediaType string `json:"mediaType"`
			Status    int    `json:"status"`
			Status4K  int    `json:"status4k"`
		} `json:"media"`
		RequestedBy *seerrRef `json:"requestedBy"`
		ModifiedBy  *seerrRef `json:"modifiedBy"`
		CreatedAt   time.Time `json:"createdAt"`
		UpdatedAt   time.Time `json:"updatedAt"`
		Seasons     []struct {
			SeasonNumber int `json:"seasonNumber"`
			Status       int `json:"status"`
		} `json:"seasons"`
	}
	reqs, err := seerrPages[rawRequest](ctx, c, "/api/v1/request", url.Values{"filter": {"all"}, "sort": {"added"}})
	if err != nil {
		return err
	}
	for _, r := range reqs {
		x := Request{ID: r.ID, Type: firstNonEmpty(r.Type, r.Media.MediaType), Status: r.Status, MediaStatus: r.Media.Status,
			TMDbID: r.Media.TMDbID, TVDbID: r.Media.TVDbID, Is4K: r.Is4K, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
		if r.Is4K {
			x.MediaStatus = r.Media.Status4K
		}
		if r.RequestedBy != nil {
			x.RequestedBy = r.RequestedBy.ID
		}
		if r.ModifiedBy != nil {
			x.ModifiedBy = r.ModifiedBy.ID
		}
		for _, sn := range r.Seasons {
			x.Seasons = append(x.Seasons, SeasonRequest{Number: sn.SeasonNumber, Status: sn.Status})
		}
		s.Requests = append(s.Requests, x)
	}
	type rawIssue struct {
		ID             int64 `json:"id"`
		IssueType      int   `json:"issueType"`
		Status         int   `json:"status"`
		ProblemSeason  int   `json:"problemSeason"`
		ProblemEpisode int   `json:"problemEpisode"`
		Media          struct {
			TMDbID    int64  `json:"tmdbId"`
			MediaType string `json:"mediaType"`
		} `json:"media"`
		CreatedBy  *seerrRef `json:"createdBy"`
		ModifiedBy *seerrRef `json:"modifiedBy"`
		Comments   []struct {
			User      *seerrRef `json:"user"`
			Message   string    `json:"message"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"comments"`
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	issues, err := seerrPages[rawIssue](ctx, c, "/api/v1/issue", url.Values{"filter": {"all"}, "sort": {"added"}})
	if err != nil {
		s.warn("issues could not be read: %v", err)
	}
	for _, r := range issues {
		if r.Comments == nil {
			var full rawIssue
			if err := c.get(ctx, "/api/v1/issue/"+strconv.FormatInt(r.ID, 10), nil, &full); err == nil {
				r.Comments = full.Comments
			}
		}
		x := Issue{ID: r.ID, Type: r.IssueType, Status: r.Status, MediaType: r.Media.MediaType, TMDbID: r.Media.TMDbID,
			Season: r.ProblemSeason, Episode: r.ProblemEpisode, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
		if r.CreatedBy != nil {
			x.CreatedBy = r.CreatedBy.ID
		}
		if r.ModifiedBy != nil {
			x.ModifiedBy = r.ModifiedBy.ID
		}
		for _, cm := range r.Comments {
			uid := x.CreatedBy
			if cm.User != nil {
				uid = cm.User.ID
			}
			x.Comments = append(x.Comments, Comment{UserID: uid, Message: cm.Message, CreatedAt: cm.CreatedAt})
		}
		s.Issues = append(s.Issues, x)
	}
	seerrTitles(ctx, c, s, o)
	return nil
}

func deref(p *int, def int) int {
	if p == nil || *p <= 0 {
		return def
	}
	return *p
}

// seerrPages reads every page of a take/skip listing.
func seerrPages[T any](ctx context.Context, c *client, path string, q url.Values) ([]T, error) {
	const take = 100
	var out []T
	for skip := 0; skip < 200000; skip += take {
		qq := url.Values{"take": {strconv.Itoa(take)}, "skip": {strconv.Itoa(skip)}}
		for k, v := range q {
			qq[k] = v
		}
		var page struct {
			PageInfo struct {
				Results int `json:"results"`
			} `json:"pageInfo"`
			Results []T `json:"results"`
		}
		if err := c.get(ctx, path, qq, &page); err != nil {
			return out, err
		}
		out = append(out, page.Results...)
		if len(page.Results) < take || len(out) >= page.PageInfo.Results {
			break
		}
	}
	return out, nil
}

// seerrTitles looks up the titles of requested and reported media (the
// request listing carries only ids).
func seerrTitles(ctx context.Context, c *client, s *Snapshot, o ReadOptions) {
	type key struct {
		kind string
		id   int64
	}
	type title struct {
		name string
		year int
	}
	want := map[key]bool{}
	for _, r := range s.Requests {
		want[key{mediaKind(r.Type), r.TMDbID}] = true
	}
	for _, is := range s.Issues {
		want[key{mediaKind(is.MediaType), is.TMDbID}] = true
	}
	keys := make([]key, 0, len(want))
	for k := range want {
		if k.id > 0 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].id < keys[j].id })
	found := make([]title, len(keys))
	_ = parallel(ctx, len(keys), 4, func(i int) error {
		k := keys[i]
		var r struct {
			Title         string `json:"title"`
			Name          string `json:"name"`
			ReleaseDate   string `json:"releaseDate"`
			FirstAirDate  string `json:"firstAirDate"`
			OriginalTitle string `json:"originalTitle"`
		}
		p := "/api/v1/movie/"
		if k.kind == "tv" {
			p = "/api/v1/tv/"
		}
		if err := c.get(ctx, p+strconv.FormatInt(k.id, 10), nil, &r); err != nil {
			return nil // a missing title only costs the name
		}
		y, _ := strconv.Atoi(firstNonEmpty(date(r.ReleaseDate), date(r.FirstAirDate)+"    ")[:4])
		found[i] = title{firstNonEmpty(r.Title, r.Name, r.OriginalTitle), y}
		return nil
	}, func(done int) { o.progress(done, len(keys), "titles") })
	byKey := map[key]title{}
	for i, k := range keys {
		byKey[k] = found[i]
	}
	for i := range s.Requests {
		r := &s.Requests[i]
		t := byKey[key{mediaKind(r.Type), r.TMDbID}]
		r.Title, r.Year = t.name, t.year
	}
	for i := range s.Issues {
		is := &s.Issues[i]
		is.Title = byKey[key{mediaKind(is.MediaType), is.TMDbID}].name
	}
}

func mediaKind(t string) string {
	if t == "tv" {
		return "tv"
	}
	return "movie"
}

// hostPort splits a source URL into what a download client needs.
func hostPort(raw string) (host string, port int, tls bool, base string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, false, ""
	}
	host = u.Hostname()
	tls = u.Scheme == "https"
	port, _ = strconv.Atoi(u.Port())
	if port == 0 {
		port = 80
		if tls {
			port = 443
		}
	}
	base = strings.TrimRight(u.Path, "/")
	return
}

func readQBit(ctx context.Context, c *client, s *Snapshot) error {
	host, port, tls, base := hostPort(c.conn.URL)
	conn := &ClientConn{Implementation: "qbittorrent", Host: host, Port: port, UseTLS: tls, URLBase: base, Username: c.conn.Username}
	var prefs struct {
		SavePath string `json:"save_path"`
	}
	if err := c.get(ctx, "/api/v2/app/preferences", nil, &prefs); err != nil {
		return err
	}
	conn.DefaultPath = prefs.SavePath
	var cats map[string]struct {
		Name     string `json:"name"`
		SavePath string `json:"savePath"`
	}
	if err := c.get(ctx, "/api/v2/torrents/categories", nil, &cats); err != nil {
		return err
	}
	for name, cat := range cats {
		p := cat.SavePath
		if p == "" {
			p = path.Join(prefs.SavePath, name)
		} else if !strings.HasPrefix(p, "/") && !strings.Contains(p, `:\`) {
			p = path.Join(prefs.SavePath, p)
		}
		conn.Categories = append(conn.Categories, ClientCategory{Name: firstNonEmpty(cat.Name, name), SavePath: p})
	}
	sort.Slice(conn.Categories, func(i, j int) bool { return conn.Categories[i].Name < conn.Categories[j].Name })
	s.Client = conn
	return nil
}

func readSAB(ctx context.Context, c *client, s *Snapshot) error {
	host, port, tls, base := hostPort(c.conn.URL)
	conn := &ClientConn{Implementation: "sabnzbd", Host: host, Port: port, UseTLS: tls, URLBase: base}
	var cfg struct {
		Config struct {
			Misc struct {
				CompleteDir string `json:"complete_dir"`
			} `json:"misc"`
			Categories []struct {
				Name string `json:"name"`
				Dir  string `json:"dir"`
			} `json:"categories"`
		} `json:"config"`
	}
	if err := c.get(ctx, "/api", url.Values{"mode": {"get_config"}}, &cfg); err != nil {
		return err
	}
	conn.DefaultPath = cfg.Config.Misc.CompleteDir
	for _, cat := range cfg.Config.Categories {
		if cat.Name == "*" {
			continue
		}
		p := cat.Dir
		if p == "" {
			p = path.Join(conn.DefaultPath, cat.Name)
		} else if !strings.HasPrefix(p, "/") && !strings.Contains(p, `:\`) {
			p = path.Join(conn.DefaultPath, p)
		}
		conn.Categories = append(conn.Categories, ClientCategory{Name: cat.Name, SavePath: p})
	}
	s.Client = conn
	return nil
}

// sameEndpoint reports whether two URLs (or host:port pairs) point at the same service.
func sameEndpoint(aHost string, aPort int, bHost string, bPort int) bool {
	norm := func(h string) string {
		h = strings.ToLower(strings.TrimSpace(h))
		if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
			return "localhost"
		}
		return h
	}
	return norm(aHost) == norm(bHost) && aPort == bPort
}

