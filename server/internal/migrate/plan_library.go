package migrate

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/requests"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/subtitles"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// localIndex is what the library already holds, by provider id.
type localIndex struct {
	movies  map[int64]localItem // tmdb
	series  map[int64]localItem // tvdb
	seriesT map[int64]localItem // tmdb
	artists map[string]localItem
}

type localItem struct {
	id      int64
	hasFile bool
}

func (p *planner) loadIndex(ctx context.Context) (*localIndex, error) {
	ix := &localIndex{movies: map[int64]localItem{}, series: map[int64]localItem{}, seriesT: map[int64]localItem{}, artists: map[string]localItem{}}
	rows, err := p.l.DB.R.QueryContext(ctx, `SELECT m.item_id, m.tmdb_id, EXISTS (SELECT 1 FROM media_files f WHERE f.item_id = m.item_id) FROM movies m`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, tmdb int64
		var has bool
		if err := rows.Scan(&id, &tmdb, &has); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ix.movies[tmdb] = localItem{id, has}
	}
	_ = rows.Close()
	rows, err = p.l.DB.R.QueryContext(ctx, `SELECT item_id, tvdb_id, tmdb_id FROM series`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, tvdb, tmdb int64
		if err := rows.Scan(&id, &tvdb, &tmdb); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if tvdb > 0 {
			ix.series[tvdb] = localItem{id: id}
		}
		if tmdb > 0 {
			ix.seriesT[tmdb] = localItem{id: id}
		}
	}
	_ = rows.Close()
	rows, err = p.l.DB.R.QueryContext(ctx, `SELECT item_id, mbid FROM artists`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var mbid string
		if err := rows.Scan(&id, &mbid); err != nil {
			return nil, err
		}
		ix.artists[strings.ToLower(mbid)] = localItem{id: id}
	}
	return ix, rows.Err()
}

func (p *planner) index() *localIndex {
	if p.local == nil {
		ix, err := p.loadIndex(p.ctx)
		if err != nil {
			p.plan.Warnings = append(p.plan.Warnings, "the library could not be read: "+err.Error())
			ix = &localIndex{movies: map[int64]localItem{}, series: map[int64]localItem{}, seriesT: map[int64]localItem{}, artists: map[string]localItem{}}
		}
		p.local = ix
	}
	return p.local
}

func fileOnDisk(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func sourceNote(s *Snapshot) string {
	return fmt.Sprintf("imported from %s (%s %s)", s.Name, s.Kind.Label(), s.Version)
}

// ---- movies (applied in batches) ----

type movieJob struct {
	in        movies.Imported
	sourceID  int64
	profileID int64
	tmdb      int64
	local     string
}

func (p *planner) planMovies() {
	for _, s := range p.snaps {
		if s.Kind != Radarr || !p.opts.source(s.SourceID).Library {
			continue
		}
		ix := p.index()
		for _, m := range s.Movies {
			key := idKey(s.SourceID, "movie", m.TMDbID)
			it := &Item{Key: key, SourceID: s.SourceID, Category: "movie", Name: fmt.Sprintf("%s (%d)", m.Title, m.Year)}
			if m.TMDbID <= 0 {
				it.Action, it.Reason = ActionSkip, "no_tmdb_id"
				p.add(it)
				continue
			}
			local, ok := p.tr.Translate(s.SourceID, m.Path)
			var file *movies.ImportedFile
			fileFound := false
			if m.File != nil {
				fp, fok := p.tr.Translate(s.SourceID, m.File.Path)
				fileFound = fok && fileOnDisk(fp)
				if !fileFound {
					it.attn(AttnFileMissing)
					it.note("file not found at %s", fp)
				}
				file = &movies.ImportedFile{Path: fp, Size: m.File.Size, Quality: MapQuality(m.File.Quality, quality.Movies),
					ReleaseGroup: m.File.ReleaseGroup, SceneName: m.File.SceneName, Edition: m.File.Edition, Revision: m.File.Revision,
					AddedAt: m.File.DateAdded}
				for _, l := range m.File.Languages {
					if c := MapLanguage(l); c != "" && c != "any" && c != "original" {
						file.Languages = append(file.Languages, c)
					}
				}
			}
			if ex, found := ix.movies[m.TMDbID]; found {
				it.LocalRef = library.MovieRef(ex.id).String()
				if !ex.hasFile && fileFound {
					it.Action, it.Reason = ActionUpdate, "link_file"
				} else {
					it.Action, it.Reason = ActionSkip, "already_in_library"
				}
			} else {
				switch {
				case !ok:
					it.Action, it.Reason = ActionSkip, "outside_data_roots"
					it.note("%s is outside the folders Locarr may use", local)
					it.attn(AttnPath)
				case !p.rootPlanned(local, rootfolders.KindMovies):
					it.Action, it.Reason = ActionSkip, "no_root_folder"
					it.note("no Locarr root folder contains %s", local)
					it.attn(AttnPath)
				default:
					it.Action = ActionCreate
					if !p.profileKnown(s.SourceID, m.QualityProfileID) {
						it.attn(AttnProfileFallback)
					}
				}
			}
			if it.Action != ActionSkip {
				in := movies.Imported{TMDbID: m.TMDbID, IMDbID: m.IMDbID, Title: m.Title, OriginalTitle: m.OriginalTitle,
					OriginalLanguage: MapLanguage(m.OriginalLanguage), Year: m.Year, Overview: m.Overview, Runtime: m.Runtime,
					Status: titleStatus(m.Status), Studio: m.Studio, Genres: m.Genres, Certification: m.Certification, InCinemas: m.InCinemas,
					DigitalRelease: m.DigitalRelease, PhysicalRelease: m.PhysicalRelease, CollectionID: m.CollectionTMDbID,
					CollectionName: m.CollectionTitle, Poster: m.Poster, Monitored: m.Monitored, MinimumAvailability: MapAvailability(m.MinimumAvailability),
					Tags: p.tagNames(s, m.Tags), Path: local, Note: sourceNote(s), Data: map[string]any{"source": s.Name, "sourceId": m.ID}}
				if fileFound {
					in.File = file
				}
				it.batch = "movies"
				it.data = &movieJob{in: in, sourceID: s.SourceID, profileID: m.QualityProfileID, tmdb: m.TMDbID, local: local}
				it.link = &linkInfo{sourceKey: s.Key, entity: "movie", external: strconv.FormatInt(m.TMDbID, 10)}
			}
			p.add(it)
		}
	}
}

func titleStatus(s string) string {
	switch strings.ToLower(s) {
	case "released":
		return "Released"
	case "incinemas":
		return "In Production"
	case "announced":
		return "Planned"
	case "tba":
		return "Rumored"
	}
	return s
}

// applyMovies imports a batch of movie items through the movie service.
func (p *planner) applyMovies(ctx context.Context, items []*Item) {
	list := make([]movies.Imported, 0, len(items))
	ok := make([]*Item, 0, len(items))
	roots, _ := p.l.Roots.Records(ctx)
	for _, it := range items {
		j := it.data.(*movieJob)
		root, found := pickRoot(roots, j.local, rootfolders.KindMovies)
		if !found && it.Action == ActionCreate {
			it.Status, it.Error = "failed", "no root folder contains "+j.local
			continue
		}
		j.in.RootFolderID = root.ID
		var fallback bool
		j.in.QualityProfileID, fallback = p.profileFor(ctx, j.sourceID, j.profileID, quality.Movies)
		if fallback && it.Action == ActionCreate {
			it.attn(AttnProfileFallback)
		}
		list = append(list, j.in)
		ok = append(ok, it)
	}
	res := p.l.Movies.ImportMovies(ctx, list)
	for i, r := range res {
		it := ok[i]
		if r.Err != nil {
			it.Status, it.Error = "failed", r.Err.Error()
			continue
		}
		it.Status, it.LocalRef = "done", library.MovieRef(r.ID).String()
		if r.Created {
			p.refresh = append(p.refresh, it.LocalRef)
		}
		if r.FileMissing {
			it.attn(AttnFileMissing)
		}
	}
}

// ---- series ----

func (p *planner) planSeries() {
	for _, s := range p.snaps {
		if s.Kind != Sonarr || !p.opts.source(s.SourceID).Library {
			continue
		}
		ix := p.index()
		for _, v := range s.Series {
			v := v
			key := idKey(s.SourceID, "series", v.TVDbID)
			it := &Item{Key: key, SourceID: s.SourceID, Category: "series", Name: fmt.Sprintf("%s (%d)", v.Title, v.Year)}
			if v.TVDbID <= 0 && v.TMDbID <= 0 {
				it.Action, it.Reason = ActionSkip, "no_provider_id"
				p.add(it)
				continue
			}
			local, ok := p.tr.Translate(s.SourceID, v.Path)
			files := make([]series.ImportedFile, 0, len(v.Files))
			missing := 0
			for _, f := range v.Files {
				fp, fok := p.tr.Translate(s.SourceID, f.Path)
				if !fok || !fileOnDisk(fp) {
					missing++
					continue
				}
				sf := series.ImportedFile{Key: strconv.FormatInt(f.ID, 10), Path: fp, Quality: MapQuality(f.Quality, quality.Series),
					ReleaseGroup: f.ReleaseGroup, SceneName: f.SceneName, Revision: f.Revision, AddedAt: f.DateAdded}
				for _, l := range f.Languages {
					if c := MapLanguage(l); c != "" && c != "any" && c != "original" {
						sf.Languages = append(sf.Languages, c)
					}
				}
				files = append(files, sf)
			}
			if missing > 0 {
				it.attn(AttnFileMissing)
				it.note("%d of %d files not found", missing, len(v.Files))
			} else if len(v.Files) > 0 {
				it.note("%d files found", len(v.Files))
			}
			ex, found := ix.series[v.TVDbID]
			if !found && v.TMDbID > 0 {
				ex, found = ix.seriesT[v.TMDbID]
			}
			switch {
			case found:
				it.LocalRef = library.SeriesRef(ex.id).String()
				if p.unlinkedEpisodes(ex.id, v, files) > 0 {
					it.Action, it.Reason = ActionUpdate, "link_files"
				} else {
					it.Action, it.Reason = ActionSkip, "already_in_library"
				}
			case !ok:
				it.Action, it.Reason = ActionSkip, "outside_data_roots"
				it.note("%s is outside the folders Locarr may use", local)
				it.attn(AttnPath)
			case !p.rootPlanned(local, rootfolders.KindSeries):
				it.Action, it.Reason = ActionSkip, "no_root_folder"
				it.note("no Locarr root folder contains %s", local)
				it.attn(AttnPath)
			default:
				it.Action = ActionCreate
				if !p.profileKnown(s.SourceID, v.QualityProfileID) {
					it.attn(AttnProfileFallback)
				}
			}
			if it.Action != ActionSkip {
				in := series.Imported{TMDbID: v.TMDbID, TVDbID: v.TVDbID, IMDbID: v.IMDbID, TVMazeID: v.TVMazeID, Title: v.Title,
					OriginalLanguage: MapLanguage(v.OriginalLanguage), Year: v.Year, Overview: v.Overview, Status: MapSeriesStatus(v.Status),
					Network: v.Network, Runtime: v.Runtime, Genres: v.Genres, Certification: v.Certification, FirstAired: v.FirstAired,
					SeriesType: MapSeriesType(v.SeriesType), SeasonFolder: v.SeasonFolder, MonitorNewSeasons: MapMonitorNew(v.MonitorNewItems, false),
					Monitored: v.Monitored, Tags: p.tagNames(s, v.Tags), Path: local, Files: files, Note: sourceNote(s),
					Data: map[string]any{"source": s.Name, "sourceId": v.ID}}
				for _, sn := range v.Seasons {
					in.Seasons = append(in.Seasons, series.ImportedSeason{Number: sn.Number, Monitored: sn.Monitored})
				}
				for _, e := range v.Episodes {
					ie := series.ImportedEpisode{Season: e.Season, Number: e.Number, Absolute: e.Absolute, TVDbID: e.TVDbID, Title: e.Title,
						AirDate: e.AirDate, AirDateUTC: e.AirDateUTC, Runtime: e.Runtime, Monitored: e.Monitored}
					if e.FileID > 0 {
						ie.FileKey = strconv.FormatInt(e.FileID, 10)
					}
					in.Episodes = append(in.Episodes, ie)
				}
				sid, prof := s.SourceID, v.QualityProfileID
				it.link = &linkInfo{sourceKey: s.Key, entity: "series", external: strconv.FormatInt(v.TVDbID, 10)}
				it.run = func(ctx context.Context) (string, error) {
					if it.Action == ActionCreate {
						root, ok := p.rootFor(ctx, local, rootfolders.KindSeries)
						if !ok {
							return "", fmt.Errorf("no root folder contains %s", local)
						}
						in.RootFolderID = root.ID
						var fb bool
						in.QualityProfileID, fb = p.profileFor(ctx, sid, prof, quality.Series)
						if fb {
							it.attn(AttnProfileFallback)
						}
					}
					o := p.l.Series.ImportSeries(ctx, in)
					if o.Err != nil {
						return "", o.Err
					}
					ref := library.SeriesRef(o.ID).String()
					if o.Created {
						p.refresh = append(p.refresh, ref)
					}
					if len(o.Missing) > 0 {
						it.attn(AttnFileMissing)
					}
					return ref, nil
				}
			}
			p.add(it)
		}
	}
}

// ---- artists ----

func (p *planner) planArtists() {
	for _, s := range p.snaps {
		if s.Kind != Lidarr || !p.opts.source(s.SourceID).Library || p.l.Music == nil {
			continue
		}
		ix := p.index()
		for _, a := range s.Artists {
			key := idKey(s.SourceID, "artist", a.MBID)
			it := &Item{Key: key, SourceID: s.SourceID, Category: "artist", Name: a.Name}
			if a.MBID == "" {
				it.Action, it.Reason = ActionSkip, "no_mbid"
				p.add(it)
				continue
			}
			local, ok := p.tr.Translate(s.SourceID, a.Path)
			in := music.ImportedArtist{MBID: a.MBID, Name: a.Name, SortName: a.SortName, Disambiguation: a.Disambiguation, Type: a.Type,
				Genres: a.Genres, Monitored: a.Monitored, MonitorNewAlbums: MapMonitorNew(a.MonitorNewItems, true), Tags: p.tagNames(s, a.Tags),
				Path: local, Note: sourceNote(s), Data: map[string]any{"source": s.Name, "sourceId": a.ID}}
			files, missing := 0, 0
			for _, al := range a.Albums {
				ia := music.ImportedAlbum{MBID: al.MBID, Title: al.Title, Disambiguation: al.Disambiguation, PrimaryType: al.Type,
					SecondaryTypes: al.SecondaryTypes, ReleaseDate: al.ReleaseDate, Monitored: al.Monitored}
				for _, f := range al.Files {
					fp, fok := p.tr.Translate(s.SourceID, f.Path)
					if !fok || !fileOnDisk(fp) {
						missing++
						continue
					}
					files++
					ia.Files = append(ia.Files, music.ImportedTrackFile{Path: fp, Quality: MapQuality(f.Quality, quality.Music), Disc: f.Disc,
						Position: f.Position, RecordingMBID: f.RecordingMBID, Title: f.Title, DurationMS: f.DurationMS, ReleaseGroup: f.ReleaseGroup,
						SceneName: f.SceneName})
				}
				in.Albums = append(in.Albums, ia)
			}
			it.note("%d albums, %d files found", len(a.Albums), files)
			if missing > 0 {
				it.attn(AttnFileMissing)
				it.note("%d files not found", missing)
			}
			if ex, found := ix.artists[strings.ToLower(a.MBID)]; found {
				it.LocalRef = fmt.Sprintf("artist:%d", ex.id)
				it.Action, it.Reason = ActionUpdate, "merge_albums"
				if !p.artistHasNews(ex.id, in) {
					it.Action, it.Reason = ActionSkip, "already_in_library"
				}
			} else {
				switch {
				case !ok:
					it.Action, it.Reason = ActionSkip, "outside_data_roots"
					it.attn(AttnPath)
				case !p.rootPlanned(local, rootfolders.KindMusic):
					it.Action, it.Reason = ActionSkip, "no_root_folder"
					it.note("no Locarr root folder contains %s", local)
					it.attn(AttnPath)
				default:
					it.Action = ActionCreate
				}
			}
			if it.Action != ActionSkip {
				sid, prof, meta := s.SourceID, a.QualityProfileID, a.MetadataProfileID
				it.link = &linkInfo{sourceKey: s.Key, entity: "artist", external: strings.ToLower(a.MBID)}
				it.run = func(ctx context.Context) (string, error) {
					if it.Action == ActionCreate {
						root, ok := p.rootFor(ctx, local, rootfolders.KindMusic)
						if !ok {
							return "", fmt.Errorf("no root folder contains %s", local)
						}
						in.RootFolderID = root.ID
						var fb bool
						in.QualityProfileID, fb = p.profileFor(ctx, sid, prof, quality.Music)
						if fb {
							it.attn(AttnProfileFallback)
						}
						in.MetadataProfileID = p.id(idKey(sid, "metadataprofile", meta))
						if in.MetadataProfileID == 0 {
							if list, err := p.l.Music.Profiles(ctx); err == nil && len(list) > 0 {
								in.MetadataProfileID = list[0].ID
								for _, mp := range list {
									if mp.Name == "Standard" {
										in.MetadataProfileID = mp.ID
									}
								}
							}
						}
					}
					o := p.l.Music.ImportArtist(ctx, in)
					if o.Err != nil {
						return "", o.Err
					}
					ref := fmt.Sprintf("artist:%d", o.ID)
					if o.Created {
						p.refresh = append(p.refresh, ref)
					}
					return ref, nil
				}
			}
			p.add(it)
		}
	}
}

// ---- users ----

func randomPassword() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (p *planner) planUsers(s *Snapshot, so SourceOptions) {
	if (s.Kind != Overseerr && s.Kind != Seerr) || !so.Users {
		return
	}
	for _, u := range s.Users {
		key := idKey(s.SourceID, "user", u.ID)
		name := u.Username
		if _, err := auth.NormalizeUsername(name); err != nil || name == "" {
			name = strings.SplitN(u.Email, "@", 2)[0]
		}
		if n, err := auth.NormalizeUsername(name); err == nil {
			name = n
		} else {
			name = fmt.Sprintf("user%d", u.ID)
		}
		role, perms := MapPermissions(u.Permissions)
		it := &Item{Key: key, SourceID: s.SourceID, Category: "user", Name: name}
		it.note("role %s", role)
		exists := func(id int64) bool {
			for _, x := range p.users {
				if x.ID == id {
					return true
				}
			}
			return false
		}
		byName := func(n string) (int64, bool) {
			for _, x := range p.users {
				if strings.EqualFold(x.Username, n) {
					return x.ID, true
				}
			}
			return 0, false
		}
		d := p.decideNamed(it, s, "user", strconv.FormatInt(u.ID, 10), name, hashOf([]any{name, role, perms, u.Quotas}), exists, byName)
		if d.targetID > 0 {
			p.setID(key, d.targetID)
		}
		quotas := u.Quotas
		if d.action == ActionSkip {
			p.add(it)
			continue
		}
		if d.targetID > 0 {
			// Merged into an existing account: its role and password stay.
			target := d.targetID
			it.note("linked to the existing account")
			it.run = func(ctx context.Context) (string, error) {
				if err := p.setQuotas(ctx, target, quotas); err != nil {
					return "", err
				}
				return fmt.Sprintf("user:%d", target), nil
			}
			p.add(it)
			continue
		}
		it.attn(AttnPassword)
		it.note("signs in after an administrator sets a password")
		uname := d.name
		it.run = func(ctx context.Context) (string, error) {
			pp := perms
			usr, err := p.l.Auth.CreateUser(ctx, auth.NewUser{Username: uname, Password: randomPassword(), Role: role, Permissions: &pp})
			if err != nil {
				return "", err
			}
			p.setID(key, usr.ID)
			if err := p.setQuotas(ctx, usr.ID, quotas); err != nil {
				return "", err
			}
			return fmt.Sprintf("user:%d", usr.ID), nil
		}
		p.add(it)
	}
	if len(s.DefaultQuotas) > 0 {
		it := &Item{Key: idKey(s.SourceID, "requestdefaults", "quotas"), SourceID: s.SourceID, Category: "requestdefaults",
			Name: "Default request quotas"}
		q := s.DefaultQuotas
		d := p.decideNamed(it, s, "requestdefaults", "quotas", it.Name, hashOf(q), func(int64) bool { return true },
			func(string) (int64, bool) { return 0, false })
		if d.action == ActionCreate {
			it.Action = ActionUpdate
		}
		if it.Action != ActionSkip {
			it.run = func(ctx context.Context) (string, error) {
				rules := map[string]requests.Rule{}
				for k, v := range q {
					rules[k] = requests.Rule{Limit: v.Limit, Days: max(1, min(v.Days, 365))}
				}
				return "settings:requests", p.l.Requests.SetDefaultQuotas(ctx, rules)
			}
		}
		p.add(it)
	}
}

func (p *planner) setQuotas(ctx context.Context, userID int64, q map[string]Quota) error {
	if len(q) == 0 {
		return nil
	}
	o := requests.Overrides{}
	for k, v := range q {
		o[k] = requests.Rule{Limit: v.Limit, Days: max(1, min(v.Days, 365))}
	}
	return p.l.Requests.SetOverrides(ctx, userID, o)
}

// userFor resolves (at apply time) the Locarr user of a source user.
func (p *planner) userFor(sourceID, srcUser int64) int64 {
	if id := p.id(idKey(sourceID, "user", srcUser)); id > 0 {
		return id
	}
	return p.opts.ActorID
}

// libraryRef finds the library item of a TMDb id (movie or series) now.
func (p *planner) libraryRef(ctx context.Context, kind string, tmdb, tvdb int64) string {
	if kind == "tv" {
		if v, err := p.l.Series.ByProviderID(ctx, tmdb, tvdb); err == nil {
			return library.SeriesRef(v.ID).String()
		}
		return ""
	}
	if m, err := p.l.Movies.ByTMDb(ctx, tmdb); err == nil {
		return library.MovieRef(m.ID).String()
	}
	return ""
}

// ---- requests and issues ----

func (p *planner) planRequests(s *Snapshot, so SourceOptions) {
	if (s.Kind != Overseerr && s.Kind != Seerr) || !so.Users {
		return
	}
	for _, r := range s.Requests {
		key := idKey(s.SourceID, "request", r.ID)
		title := firstNonEmpty(r.Title, fmt.Sprintf("TMDb %d", r.TMDbID))
		it := &Item{Key: key, SourceID: s.SourceID, Category: "request", Name: title}
		status := MapRequestStatus(r.Status, r.MediaStatus)
		it.note("%s", status)
		it.link = &linkInfo{sourceKey: s.Key, entity: "request", external: strconv.FormatInt(r.ID, 10), hash: "1"}
		if _, ok := p.linkOf(s, "request", strconv.FormatInt(r.ID, 10)); ok {
			it.Action, it.Reason = ActionSkip, "unchanged"
			p.add(it)
			continue
		}
		if p.id(idKey(s.SourceID, "user", r.RequestedBy)) == 0 && !p.userPlanned(s.SourceID, r.RequestedBy) {
			it.note("its user is not imported: it is shown as requested by you")
		}
		it.Action = ActionCreate
		req, sid := r, s.SourceID
		it.run = func(ctx context.Context) (string, error) {
			kind := requests.KindMovie
			if req.Type == "tv" {
				kind = requests.KindSeries
			}
			in := requests.Imported{Kind: kind, Is4K: req.Is4K, Status: status, TMDbID: req.TMDbID, Title: title, Year: req.Year,
				RequestedBy: p.userFor(sid, req.RequestedBy), CreatedAt: req.CreatedAt, UpdatedAt: req.UpdatedAt,
				ItemRef: p.libraryRef(ctx, req.Type, req.TMDbID, req.TVDbID)}
			if req.ModifiedBy > 0 && status != requests.StatusPending {
				by := p.userFor(sid, req.ModifiedBy)
				at := req.UpdatedAt
				in.DecidedBy, in.DecidedAt = &by, &at
			}
			if status == requests.StatusAvailable {
				at := req.UpdatedAt
				in.AvailableAt = &at
			}
			if kind == requests.KindSeries {
				in.AllSeasons = len(req.Seasons) == 0
				for _, sn := range req.Seasons {
					in.Seasons = append(in.Seasons, requests.Season{Number: sn.Number, Status: MapRequestStatus(sn.Status, 0)})
				}
				if len(req.Seasons) > 0 {
					for i := range in.Seasons {
						if status == requests.StatusAvailable || status == requests.StatusPartial || status == requests.StatusProcessing {
							in.Seasons[i].Status = status
						}
					}
				}
			}
			id, err := p.l.Requests.ImportRequest(ctx, in)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("request:%d", id), nil
		}
		p.add(it)
	}
}

func (p *planner) userPlanned(sourceID, srcUser int64) bool {
	k := idKey(sourceID, "user", srcUser)
	for _, it := range p.plan.Items {
		if it.Key == k && it.Action != ActionSkip {
			return true
		}
	}
	return false
}

func (p *planner) planIssues(s *Snapshot, so SourceOptions) {
	if (s.Kind != Overseerr && s.Kind != Seerr) || !so.Users {
		return
	}
	for _, is := range s.Issues {
		key := idKey(s.SourceID, "issue", is.ID)
		title := firstNonEmpty(is.Title, fmt.Sprintf("TMDb %d", is.TMDbID))
		it := &Item{Key: key, SourceID: s.SourceID, Category: "issue", Name: title}
		it.link = &linkInfo{sourceKey: s.Key, entity: "issue", external: strconv.FormatInt(is.ID, 10), hash: "1"}
		if _, ok := p.linkOf(s, "issue", strconv.FormatInt(is.ID, 10)); ok {
			it.Action, it.Reason = ActionSkip, "unchanged"
			p.add(it)
			continue
		}
		it.Action = ActionCreate
		x, sid := is, s.SourceID
		it.run = func(ctx context.Context) (string, error) {
			kind := requests.KindMovie
			mt := "movie"
			if x.MediaType == "tv" {
				kind, mt = requests.KindSeries, "tv"
			}
			in := requests.ImportedIssue{Kind: kind, Type: MapIssueType(x.Type), Resolved: x.Status == 2, TMDbID: x.TMDbID, Title: title,
				ReportedBy: p.userFor(sid, x.CreatedBy), CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt, ItemRef: p.libraryRef(ctx, mt, x.TMDbID, 0)}
			if x.Season > 0 {
				sn := x.Season
				in.Season = &sn
			}
			if x.Episode > 0 {
				ep := x.Episode
				in.Episode = &ep
			}
			for i, c := range x.Comments {
				if i == 0 {
					in.Message = c.Message
					continue
				}
				in.Comments = append(in.Comments, requests.ImportedComment{UserID: p.userFor(sid, c.UserID), Body: c.Message, CreatedAt: c.CreatedAt})
			}
			if in.Resolved && x.ModifiedBy > 0 {
				by := p.userFor(sid, x.ModifiedBy)
				at := x.UpdatedAt
				in.ResolvedBy, in.ResolvedAt = &by, &at
			}
			id, err := p.l.Requests.ImportIssue(ctx, in)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("issue:%d", id), nil
		}
		p.add(it)
	}
}

// ---- subtitles ----

func (p *planner) planSubtitles(s *Snapshot, so SourceOptions) {
	if s.Kind != Bazarr {
		return
	}
	target := p.l.Subtitles
	unavailable := func(it *Item) {
		it.Action, it.Reason = ActionSkip, "subtitles_unavailable"
		it.note("subtitles are not available in this Locarr version")
	}
	var existing map[string]int64
	if target != nil {
		existing, _ = target.Profiles(p.ctx)
	}
	if so.Settings {
		for _, sp := range s.SubtitleProfiles {
			key := idKey(s.SourceID, "subtitleprofile", sp.ID)
			it := &Item{Key: key, SourceID: s.SourceID, Category: "subtitleprofile", Name: sp.Name}
			if target == nil {
				unavailable(it)
				p.add(it)
				continue
			}
			byName := func(n string) (int64, bool) { id, ok := existing[strings.ToLower(n)]; return id, ok }
			exists := func(id int64) bool {
				for _, x := range existing {
					if x == id {
						return true
					}
				}
				return false
			}
			d := p.decideNamed(it, s, "subtitleprofile", strconv.FormatInt(sp.ID, 10), sp.Name, hashOf(sp.Languages), exists, byName)
			if d.targetID > 0 {
				p.setID(key, d.targetID)
			}
			if d.action != ActionSkip {
				prof, tgt := sp, d.targetID
				prof.Name = d.name
				it.run = func(ctx context.Context) (string, error) {
					id, err := target.SaveProfile(ctx, tgt, prof)
					if err != nil {
						return "", err
					}
					p.setID(key, id)
					return fmt.Sprintf("subtitleprofile:%d", id), nil
				}
			}
			p.add(it)
		}
		for _, pr := range s.SubtitleProviders {
			key := idKey(s.SourceID, "subtitleprovider", pr.Name)
			it := &Item{Key: key, SourceID: s.SourceID, Category: "subtitleprovider", Name: pr.Name}
			if target == nil {
				unavailable(it)
				p.add(it)
				continue
			}
			name, ok := target.Provider(pr.Name)
			if !ok {
				it.Action, it.Reason = ActionSkip, "unsupported_provider"
				it.note("Locarr has no %s provider", pr.Name)
				it.attn(AttnUnsupported)
				p.add(it)
				continue
			}
			fields := map[string]string{}
			for k, v := range pr.Fields {
				lk := strings.ToLower(k)
				if lk != "username" && lk != "password" && lk != "apikey" && lk != "api_key" {
					continue
				}
				if masked(v) {
					it.attn(AttnSecret)
					continue
				}
				if v != "" {
					if lk == "api_key" {
						lk = "apikey"
					}
					fields[lk] = v
				}
			}
			if contains(it.Attention, AttnSecret) {
				it.note("Bazarr hides its credentials: enter them in Subtitles → Providers")
			} else if needsKey(name) && fields["apikey"] == "" {
				it.attn(AttnSecret)
				it.note("Locarr needs an API key for %s; it stays off until you add one", name)
			}
			d := p.decideNamed(it, s, "subtitleprovider", pr.Name, name, hashOf(fields), func(int64) bool { return true },
				func(string) (int64, bool) { return 0, false })
			if d.action == ActionCreate {
				it.Action = ActionUpdate
			}
			if it.Action != ActionSkip {
				it.run = func(ctx context.Context) (string, error) {
					return "subtitleprovider:" + name, target.EnableProvider(ctx, name, fields)
				}
			}
			p.add(it)
		}
	}
	if so.Library && len(s.SubtitleAssignments) > 0 {
		it := &Item{Key: idKey(s.SourceID, "subtitleassignments", "all"), SourceID: s.SourceID, Category: "subtitleassignments",
			Name: fmt.Sprintf("Subtitle profiles of %d titles", len(s.SubtitleAssignments))}
		if target == nil {
			unavailable(it)
			p.add(it)
			return
		}
		list, sid := s.SubtitleAssignments, s.SourceID
		d := p.decideNamed(it, s, "subtitleassignments", "all", it.Name, hashOf(list), func(int64) bool { return true },
			func(string) (int64, bool) { return 0, false })
		if d.action == ActionCreate {
			it.Action = ActionUpdate
		}
		if it.Action != ActionSkip {
			it.run = func(ctx context.Context) (string, error) {
				byProfile := map[int64][]int64{}
				missing := 0
				for _, a := range list {
					pid := p.id(idKey(sid, "subtitleprofile", a.ProfileID))
					item := p.subtitleItem(ctx, a)
					if pid == 0 || item == 0 {
						missing++
						continue
					}
					byProfile[pid] = append(byProfile[pid], item)
				}
				n := 0
				for pid, ids := range byProfile {
					c, err := target.Assign(ctx, ids, pid)
					if err != nil {
						return "", err
					}
					n += c
				}
				if missing > 0 {
					it.note("%d titles are not in the library or their profile was not imported", missing)
				}
				return fmt.Sprintf("subtitleassignments:%d", n), nil
			}
		}
		p.add(it)
	}
}

// subtitleItem finds the library item of a Bazarr title: through the ids
// Bazarr gives, or through the Radarr/Sonarr snapshot of the same run.
func (p *planner) subtitleItem(ctx context.Context, a SubtitleAssignment) int64 {
	tmdb, tvdb := a.TMDbID, a.TVDbID
	for _, s := range p.snaps {
		if a.Kind == "movie" && s.Kind == Radarr && tmdb == 0 {
			for _, m := range s.Movies {
				if m.ID == a.AppID {
					tmdb = m.TMDbID
				}
			}
		}
		if a.Kind == "series" && s.Kind == Sonarr && tvdb == 0 {
			for _, v := range s.Series {
				if v.ID == a.AppID {
					tvdb = v.TVDbID
				}
			}
		}
	}
	if a.Kind == "movie" {
		if tmdb > 0 {
			if m, err := p.l.Movies.ByTMDb(ctx, tmdb); err == nil {
				return m.ID
			}
		}
		if a.IMDbID != "" {
			var id int64
			if p.l.DB.R.QueryRowContext(ctx, `SELECT item_id FROM movies WHERE imdb_id = ?`, a.IMDbID).Scan(&id) == nil {
				return id
			}
		}
		return 0
	}
	if tvdb > 0 {
		if v, err := p.l.Series.ByProviderID(ctx, 0, tvdb); err == nil {
			return v.ID
		}
	}
	if a.IMDbID != "" {
		var id int64
		if p.l.DB.R.QueryRowContext(ctx, `SELECT item_id FROM series WHERE imdb_id = ?`, a.IMDbID).Scan(&id) == nil {
			return id
		}
	}
	return 0
}

// ---- blocklist and history ----

// sourceItemRef maps a source's movie/series/artist id to a library ref (at apply time).
func (p *planner) sourceItemRef(ctx context.Context, s *Snapshot, itemID int64) string {
	switch s.Kind {
	case Radarr:
		for _, m := range s.Movies {
			if m.ID == itemID {
				if x, err := p.l.Movies.ByTMDb(ctx, m.TMDbID); err == nil {
					return library.MovieRef(x.ID).String()
				}
			}
		}
	case Sonarr:
		for _, v := range s.Series {
			if v.ID == itemID {
				if x, err := p.l.Series.ByProviderID(ctx, v.TMDbID, v.TVDbID); err == nil {
					return library.SeriesRef(x.ID).String()
				}
			}
		}
	case Lidarr:
		for _, a := range s.Artists {
			if a.ID == itemID {
				if x, err := p.l.Music.ArtistByMBID(ctx, a.MBID); err == nil {
					return fmt.Sprintf("artist:%d", x.ID)
				}
			}
		}
	}
	return ""
}

func maxID[T any](list []T, id func(T) int64) int64 {
	var m int64
	for _, x := range list {
		m = max(m, id(x))
	}
	return m
}

func (p *planner) planBlocklist(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.History || len(s.Blocklist) == 0 {
		return
	}
	since := int64(0)
	if l, ok := p.linkOf(s, "blocklist", "all"); ok {
		since, _ = strconv.ParseInt(l.hash, 10, 64)
	}
	var list []BlockItem
	for _, b := range s.Blocklist {
		if b.ID > since {
			list = append(list, b)
		}
	}
	it := &Item{Key: idKey(s.SourceID, "blocklist", "all"), SourceID: s.SourceID, Category: "blocklist",
		Name: fmt.Sprintf("%d blocked releases", len(list))}
	top := maxID(s.Blocklist, func(b BlockItem) int64 { return b.ID })
	it.link = &linkInfo{sourceKey: s.Key, entity: "blocklist", external: "all", hash: strconv.FormatInt(max(top, since), 10)}
	if len(list) == 0 {
		it.Action, it.Reason = ActionSkip, "unchanged"
		p.add(it)
		return
	}
	it.Action = ActionCreate
	it.run = func(ctx context.Context) (string, error) {
		have, _ := p.l.Downloads.Blocklist(ctx, 100000)
		seen := map[string]bool{}
		for _, b := range have {
			seen[strings.ToLower(b.Title)+"|"+b.InfoHash] = true
		}
		n := 0
		for _, b := range list {
			if seen[strings.ToLower(b.Title)+"|"+b.InfoHash] {
				continue
			}
			e := downloads.BlockEntry{Title: b.Title, Protocol: b.Protocol, Indexer: b.Indexer, InfoHash: b.InfoHash,
				Reason: "imported", Message: strings.TrimSpace(fmt.Sprintf("%s (blocked in %s on %s)", b.Message, s.Name, b.Date.Format("2006-01-02")))}
			if e.Protocol != "usenet" {
				e.Protocol = "torrent"
			}
			if ref := p.sourceItemRef(ctx, s, b.ItemID); ref != "" {
				if r, err := library.ParseRef(ref); err == nil {
					e.ItemRef = r.JSON()
				}
			}
			if _, err := p.l.Downloads.AddBlock(ctx, e); err != nil {
				return "", err
			}
			n++
		}
		return fmt.Sprintf("blocklist:%d", n), nil
	}
	p.add(it)
}

func (p *planner) planHistory(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.History || len(s.History) == 0 {
		return
	}
	since := int64(0)
	if l, ok := p.linkOf(s, "history", "all"); ok {
		since, _ = strconv.ParseInt(l.hash, 10, 64)
	}
	var list []HistoryItem
	for _, h := range s.History {
		if h.ID > since {
			list = append(list, h)
		}
	}
	it := &Item{Key: idKey(s.SourceID, "history", "all"), SourceID: s.SourceID, Category: "history",
		Name: fmt.Sprintf("%d history events", len(list))}
	top := maxID(s.History, func(h HistoryItem) int64 { return h.ID })
	it.link = &linkInfo{sourceKey: s.Key, entity: "history", external: "all", hash: strconv.FormatInt(max(top, since), 10)}
	if len(list) == 0 {
		it.Action, it.Reason = ActionSkip, "unchanged"
		p.add(it)
		return
	}
	it.Action = ActionCreate
	it.run = func(ctx context.Context) (string, error) {
		refs := map[int64]string{}
		n := 0
		err := p.l.DB.Tx(ctx, func(tx *sql.Tx) error {
			for _, h := range list {
				ref, ok := refs[h.ItemID]
				if !ok {
					ref = p.sourceItemRef(ctx, s, h.ItemID)
					refs[h.ItemID] = ref
				}
				if ref == "" {
					continue
				}
				if h.EpisodeID > 0 && s.Kind == Sonarr {
					if er := p.episodeRef(ctx, s, ref, h.ItemID, h.EpisodeID); er != "" {
						ref = er
					}
				}
				msg := historyMessage(s, h)
				if err := timeline.RecordAt(ctx, tx, ref, timeline.TypeHistory, msg, map[string]any{"source": s.Name, "event": h.EventType,
					"title": h.Title, "quality": h.Quality, "indexer": h.Indexer, "client": h.Client}, h.Date); err != nil {
					return err
				}
				n++
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("history:%d", n), nil
	}
	p.add(it)
}

func historyMessage(s *Snapshot, h HistoryItem) string {
	switch h.EventType {
	case "grabbed":
		m := fmt.Sprintf("in %s: grabbed %s", s.Name, h.Title)
		if h.Indexer != "" {
			m += " from " + h.Indexer
		}
		if h.Client != "" {
			m += " (sent to " + h.Client + ")"
		}
		return m
	case "imported":
		return fmt.Sprintf("in %s: imported %s", s.Name, h.Title)
	}
	return fmt.Sprintf("in %s: download failed: %s", s.Name, h.Title)
}

func (p *planner) episodeRef(ctx context.Context, s *Snapshot, seriesRef string, seriesID, episodeID int64) string {
	for _, v := range s.Series {
		if v.ID != seriesID {
			continue
		}
		for _, e := range v.Episodes {
			if e.ID == episodeID {
				var id int64
				if p.l.DB.R.QueryRowContext(ctx, `SELECT id FROM episodes WHERE series_id = ? AND season_number = ? AND episode_number = ?`,
					refID(seriesRef), e.Season, e.Number).Scan(&id) == nil {
					return library.EpisodeRef(id).String()
				}
			}
		}
	}
	return ""
}

func needsKey(name string) bool {
	info, ok := subtitles.ImplementationOf(name)
	return ok && info.NeedsKey
}

// unlinkedEpisodes counts the source's episode files (found here) whose
// episode in the library has no file yet.
func (p *planner) unlinkedEpisodes(seriesID int64, v Series, files []series.ImportedFile) int {
	onDisk := map[string]bool{}
	for _, f := range files {
		onDisk[f.Key] = true
	}
	rows, err := p.l.DB.R.QueryContext(p.ctx, `SELECT season_number, episode_number, file_id IS NOT NULL FROM episodes WHERE series_id = ?`, seriesID)
	if err != nil {
		return len(files)
	}
	defer rows.Close()
	has := map[[2]int]bool{}
	for rows.Next() {
		var sn, ep int
		var f bool
		if rows.Scan(&sn, &ep, &f) == nil {
			has[[2]int{sn, ep}] = f
		}
	}
	n := 0
	for _, e := range v.Episodes {
		if e.FileID > 0 && onDisk[strconv.FormatInt(e.FileID, 10)] && !has[[2]int{e.Season, e.Number}] {
			n++
		}
	}
	return n
}

// artistHasNews reports whether importing adds albums or files to an artist in the library.
func (p *planner) artistHasNews(artistID int64, in music.ImportedArtist) bool {
	rows, err := p.l.DB.R.QueryContext(p.ctx, `SELECT a.mbid, (SELECT count(*) FROM album_files f WHERE f.album_id = a.id) FROM albums a
		WHERE a.artist_id = ?`, artistID)
	if err != nil {
		return true
	}
	defer rows.Close()
	files := map[string]int{}
	for rows.Next() {
		var mbid string
		var n int
		if rows.Scan(&mbid, &n) == nil {
			files[strings.ToLower(mbid)] = n
		}
	}
	for _, al := range in.Albums {
		n, ok := files[strings.ToLower(al.MBID)]
		if !ok || (n == 0 && len(al.Files) > 0) {
			return true
		}
	}
	return false
}
