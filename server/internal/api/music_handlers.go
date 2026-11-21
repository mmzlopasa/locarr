package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// mapMusicError maps errors of the music library (nil = not one of them).
func mapMusicError(err error) *StatusError {
	if ie, ok := music.IsImportError(err); ok {
		return &StatusError{Status: http.StatusUnprocessableEntity, Code: ie.Code, Message: ie.Message}
	}
	table := []struct {
		err    error
		status int
		code   string
	}{
		{music.ErrNotFound, http.StatusNotFound, "not_found"},
		{music.ErrProfileNotFound, http.StatusNotFound, "not_found"},
		{music.ErrExists, http.StatusConflict, "already_exists"},
		{music.ErrPathTaken, http.StatusConflict, "path_taken"},
		{music.ErrProfileInUse, http.StatusConflict, "in_use"},
		{music.ErrInvalid, http.StatusBadRequest, "validation"},
	}
	for _, t := range table {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return nil
}

func (h *Handler) musicService() (*music.Service, error) {
	if h.Music == nil {
		return nil, &StatusError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "the music library is not running"}
	}
	return h.Music, nil
}

func toArtist(a music.Artist, st music.ArtistStats) gen.Artist {
	out := gen.Artist{ID: a.ID, ItemRef: a.Ref().String(), Mbid: a.MBID, Name: a.Name, SortName: a.SortName, Disambiguation: a.Disambiguation,
		Type: a.Type, Country: a.Country, Aliases: a.Aliases, Genres: a.Genres, Monitored: a.Monitored, MonitorNewAlbums: gen.MonitorNewAlbums(a.MonitorNewAlbums),
		QualityProfileId: a.QualityProfileID, MetadataProfileId: a.MetadataProfileID, RootFolderId: optI64(a.RootFolderID), Path: a.Path,
		Tags: a.Tags, AddedAt: a.AddedAt, UpdatedAt: a.UpdatedAt, RefreshedAt: optTime(a.RefreshedAt), Albums: st.Albums,
		MonitoredAlbums: st.MonitoredAlbums, AlbumsWithFiles: st.AlbumsWithFiles, Files: st.Files, SizeBytes: st.Size, LatestRelease: optStr(st.LatestDate)}
	if st.CoverGroup != "" {
		out.Cover = musicbrainz.CoverName(st.CoverGroup)
	}
	return out
}

func (h *Handler) artistOut(ctx context.Context, id int64) (*gen.Artist, error) {
	a, err := h.Music.Artist(ctx, id)
	if err != nil {
		return nil, err
	}
	st, err := h.Music.Stats(ctx, id)
	if err != nil {
		return nil, err
	}
	out := toArtist(a, st)
	return &out, nil
}

func toRelease(r music.Release) gen.AlbumRelease {
	return gen.AlbumRelease{ID: r.ID, Mbid: r.MBID, Title: r.Title, Status: r.Status, Date: r.Date, Country: r.Country, Disambiguation: r.Disambiguation,
		Label: r.Label, CatalogNumber: r.CatalogNumber, Formats: r.Formats, Format: r.FormatSummary(), TrackCount: r.TrackCount, MediaCount: r.MediaCount,
		TracksLoaded: r.TracksLoaded}
}

func toAlbum(st music.AlbumState) gen.Album {
	out := gen.Album{ID: st.ID, ItemRef: st.Ref().String(), ArtistId: st.ArtistID, ArtistName: st.ArtistName, Mbid: st.MBID, Title: st.Title,
		Disambiguation: st.Disambiguation, PrimaryType: st.PrimaryType, SecondaryTypes: st.SecondaryTypes, ReleaseDate: st.ReleaseDate,
		Monitored: st.Monitored, ArtistMonitored: st.ArtistMonitored, ReleaseLocked: st.ReleaseLocked, Filtered: st.Filtered, Path: st.Path,
		TrackCount: st.TrackCount, FileCount: st.FileCount, SizeBytes: st.Size, CutoffMet: st.CutoffMet, Available: st.Available,
		LibraryStatus: gen.AlbumStatus(st.Status()), Cover: musicbrainz.CoverName(st.MBID)}
	if st.Release != nil {
		out.Release = gen.NewOptAlbumRelease(toRelease(*st.Release))
	}
	if st.Quality != "" {
		out.Quality = gen.NewOptQuality(toQuality(st.Quality))
	}
	if d := st.Download; d != nil {
		out.Download = gen.NewOptMovieDownload(gen.MovieDownload{ID: d.ID, Title: d.Title, Progress: d.Progress, State: d.State, Status: d.Status})
	}
	return out
}

func toTrackFile(f music.TrackFile) gen.TrackFile {
	return gen.TrackFile{ID: f.ID, Path: f.Path, SizeBytes: f.Size, Quality: toQuality(f.Quality), Format: f.Format, BitDepth: f.BitDepth,
		SampleRate: f.SampleRate, Bitrate: f.Bitrate, DurationMs: f.DurationMs, ReleaseGroup: optStr(f.ReleaseGroup), SceneName: optStr(f.SceneName),
		DownloadId: optI64(f.DownloadID), AddedAt: f.AddedAt, Disc: gen.NewOptInt(f.Disc), Position: gen.NewOptInt(f.Position), Title: optStr(f.Title)}
}

// ListArtists implements GET /music/artists.
func (h *Handler) ListArtists(ctx context.Context, p gen.ListArtistsParams) (*gen.ArtistList, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	f := music.ListFilter{Search: p.Q.Or(""), Tag: p.Tag.Or(""), Sort: string(p.Sort.Or(gen.ListArtistsSortName)), Desc: p.Desc.Or(false),
		Offset: p.Offset.Or(0), Limit: p.Limit.Or(100)}
	if v, ok := p.Monitored.Get(); ok {
		f.Monitored = &v
	}
	list, total, err := m.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := &gen.ArtistList{Total: total, Items: make([]gen.Artist, 0, len(list))}
	for _, it := range list {
		out.Items = append(out.Items, toArtist(it.Artist, it.ArtistStats))
	}
	return out, nil
}

// AddArtist implements POST /music/artists.
func (h *Handler) AddArtist(ctx context.Context, req *gen.AddArtistRequest) (*gen.Artist, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	a, err := m.Add(ctx, music.AddInput{MBID: req.Mbid, RootFolderID: req.RootFolderId, QualityProfileID: req.QualityProfileId,
		MetadataProfileID: req.MetadataProfileId, Monitored: req.Monitored.Or(true), Monitor: string(req.Monitor.Or(gen.AddArtistRequestMonitorAll)),
		MonitorNewAlbums: string(req.MonitorNewAlbums.Or(gen.MonitorNewAlbumsAll)), Tags: req.Tags, SearchOnAdd: req.SearchOnAdd.Or(false)})
	if err != nil {
		return nil, err
	}
	return h.artistOut(ctx, a.ID)
}

func toArtistLookup(r metadata.ArtistSummary, id *int64) gen.ArtistLookup {
	return gen.ArtistLookup{Mbid: r.MBID, Name: r.Name, SortName: r.SortName, Disambiguation: r.Disambiguation, Type: r.Type, Country: r.Country,
		Begin: optStr(r.Begin), End: optStr(r.End), Score: r.Score, ArtistId: optI64(id)}
}

// LookupArtists implements GET /music/artists/lookup.
func (h *Handler) LookupArtists(ctx context.Context, p gen.LookupArtistsParams) ([]gen.ArtistLookup, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	list, err := m.Lookup(ctx, p.Term)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ArtistLookup, 0, len(list))
	for _, r := range list {
		out = append(out, toArtistLookup(r.ArtistSummary, r.ArtistID))
	}
	return out, nil
}

// GetArtist implements GET /music/artists/{id}.
func (h *Handler) GetArtist(ctx context.Context, p gen.GetArtistParams) (*gen.Artist, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	if _, err := h.musicService(); err != nil {
		return nil, err
	}
	return h.artistOut(ctx, p.ID)
}

// UpdateArtist implements PATCH /music/artists/{id}.
func (h *Handler) UpdateArtist(ctx context.Context, req *gen.ArtistUpdate, p gen.UpdateArtistParams) (*gen.Artist, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	var u music.ArtistUpdate
	if v, ok := req.Monitored.Get(); ok {
		u.Monitored = &v
	}
	if v, ok := req.MonitorNewAlbums.Get(); ok {
		s := string(v)
		u.MonitorNewAlbums = &s
	}
	if v, ok := req.QualityProfileId.Get(); ok {
		u.QualityProfileID = &v
	}
	if v, ok := req.MetadataProfileId.Get(); ok {
		u.MetadataProfileID = &v
	}
	if req.Tags != nil {
		u.Tags = &req.Tags
	}
	if _, err := m.EditArtist(ctx, p.ID, u); err != nil {
		return nil, err
	}
	return h.artistOut(ctx, p.ID)
}

// DeleteArtist implements DELETE /music/artists/{id}.
func (h *Handler) DeleteArtist(ctx context.Context, p gen.DeleteArtistParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	m, err := h.musicService()
	if err != nil {
		return err
	}
	return m.DeleteArtist(ctx, p.ID, p.DeleteFiles.Or(false))
}

// ListArtistAlbums implements GET /music/artists/{id}/albums.
func (h *Handler) ListArtistAlbums(ctx context.Context, p gen.ListArtistAlbumsParams) ([]gen.Album, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	a, err := m.Artist(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	list, err := m.AlbumStates(ctx, &a)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Album, 0, len(list))
	for _, st := range list {
		out = append(out, toAlbum(st))
	}
	return out, nil
}

// RefreshArtist implements POST /music/artists/{id}/refresh.
func (h *Handler) RefreshArtist(ctx context.Context, p gen.RefreshArtistParams) (*gen.Artist, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	if _, err := m.Refresh(ctx, p.ID, false); err != nil {
		return nil, err
	}
	if m.RefreshRequested != nil {
		m.RefreshRequested(ctx, p.ID)
	}
	return h.artistOut(ctx, p.ID)
}

func (h *Handler) queueAlbumSearches(ctx context.Context, ids []int64) (int, error) {
	n := 0
	for _, id := range ids {
		if _, err := h.Music.Album(ctx, id); err != nil {
			continue
		}
		if err := h.Wanted.Enqueue(ctx, library.Ref{Kind: library.RefAlbum, ID: id}, "manual"); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SearchArtist implements POST /music/artists/{id}/search.
func (h *Handler) SearchArtist(ctx context.Context, p gen.SearchArtistParams) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	a, err := m.Artist(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	list, err := m.AlbumStates(ctx, &a)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, st := range list {
		if st.Monitored && st.Available && !st.CutoffMet {
			ids = append(ids, st.ID)
		}
	}
	n, err := h.queueAlbumSearches(ctx, ids)
	return &gen.QueuedCount{Count: n}, err
}

// ListWantedAlbums implements GET /music/albums.
func (h *Handler) ListWantedAlbums(ctx context.Context, p gen.ListWantedAlbumsParams) (*gen.AlbumList, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	list, err := m.WantedAlbums(ctx, p.Status.Or(gen.ListWantedAlbumsStatusMissing) == gen.ListWantedAlbumsStatusCutoffUnmet)
	if err != nil {
		return nil, err
	}
	off, lim := p.Offset.Or(0), p.Limit.Or(100)
	out := &gen.AlbumList{Total: len(list), Items: []gen.Album{}}
	for i := off; i < len(list) && i < off+lim; i++ {
		out.Items = append(out.Items, toAlbum(list[i]))
	}
	return out, nil
}

// MonitorAlbums implements PUT /music/albums/monitor.
func (h *Handler) MonitorAlbums(ctx context.Context, req *gen.AlbumMonitorRequest) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	if err := m.SetMonitored(ctx, req.AlbumIds, req.Monitored); err != nil {
		return nil, err
	}
	return &gen.QueuedCount{Count: len(req.AlbumIds)}, nil
}

// SearchAlbums implements POST /music/albums/search.
func (h *Handler) SearchAlbums(ctx context.Context, req *gen.AlbumIdsRequest) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, err := h.musicService(); err != nil {
		return nil, err
	}
	n, err := h.queueAlbumSearches(ctx, req.AlbumIds)
	return &gen.QueuedCount{Count: n}, err
}

func (h *Handler) albumDetail(ctx context.Context, id int64) (*gen.AlbumDetail, error) {
	m := h.Music
	al, err := m.Album(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &gen.AlbumDetail{Releases: []gen.AlbumRelease{}, Tracks: []gen.AlbumTrack{}, ExtraFiles: []gen.TrackFile{}}
	rels, err := m.EnsureReleases(ctx, &al)
	if err != nil {
		out.TracksError = gen.NewOptString(err.Error())
		rels, _ = m.Releases(ctx, id)
	}
	al, _ = m.Album(ctx, id)
	for _, r := range rels {
		out.Releases = append(out.Releases, toRelease(r))
	}
	files, err := m.Files(ctx, id)
	if err != nil {
		return nil, err
	}
	extra := files
	if al.ReleaseID != nil {
		tracks, err := m.EnsureTracks(ctx, *al.ReleaseID)
		if err != nil {
			out.TracksError = gen.NewOptString(err.Error())
		} else {
			extra = music.LinkFiles(tracks, files)
			for _, t := range tracks {
				gt := gen.AlbumTrack{ID: t.ID, Mbid: t.MBID, RecordingMbid: t.RecordingMBID, Disc: t.Disc, Position: t.Position, Number: t.Number,
					Title: t.Title, LengthMs: t.LengthMs, Artist: t.Artist}
				if t.File != nil {
					gt.File = gen.NewOptTrackFile(toTrackFile(*t.File))
				}
				out.Tracks = append(out.Tracks, gt)
			}
		}
	}
	for _, f := range extra {
		out.ExtraFiles = append(out.ExtraFiles, toTrackFile(f))
	}
	st, err := m.State(ctx, al, nil, nil)
	if err != nil {
		return nil, err
	}
	out.Album = toAlbum(st)
	return out, nil
}

// GetAlbum implements GET /music/albums/{id}.
func (h *Handler) GetAlbum(ctx context.Context, p gen.GetAlbumParams) (*gen.AlbumDetail, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	if _, err := h.musicService(); err != nil {
		return nil, err
	}
	return h.albumDetail(ctx, p.ID)
}

// UpdateAlbum implements PATCH /music/albums/{id}.
func (h *Handler) UpdateAlbum(ctx context.Context, req *gen.AlbumUpdate, p gen.UpdateAlbumParams) (*gen.AlbumDetail, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	var u music.AlbumUpdate
	if v, ok := req.Monitored.Get(); ok {
		u.Monitored = &v
	}
	if v, ok := req.ReleaseId.Get(); ok {
		u.ReleaseID = &v
	}
	if _, err := m.EditAlbum(ctx, p.ID, u); err != nil {
		return nil, err
	}
	if u.Monitored != nil && *u.Monitored {
		h.Wanted.ResetBackoff(ctx, library.Ref{Kind: library.RefAlbum, ID: p.ID})
	}
	return h.albumDetail(ctx, p.ID)
}

// SearchAlbum implements POST /music/albums/{id}/search.
func (h *Handler) SearchAlbum(ctx context.Context, p gen.SearchAlbumParams) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, err := h.musicService(); err != nil {
		return nil, err
	}
	if _, err := h.Music.Album(ctx, p.ID); err != nil {
		return nil, err
	}
	n, err := h.queueAlbumSearches(ctx, []int64{p.ID})
	return &gen.QueuedCount{Count: n}, err
}

// InteractiveSearchAlbum implements GET /music/albums/{id}/releases.
func (h *Handler) InteractiveSearchAlbum(ctx context.Context, p gen.InteractiveSearchAlbumParams) (*gen.InteractiveSearch, error) {
	pr, err := manager(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.musicService(); err != nil {
		return nil, err
	}
	ev, err := h.Wanted.Interactive(ctx, library.Ref{Kind: library.RefAlbum, ID: p.ID}, pr.User.ID)
	if err != nil {
		return nil, err
	}
	return h.interactiveOut(ev)
}

// interactiveOut converts an evaluation for the client (links stay on the server, D-016).
func (h *Handler) interactiveOut(ev wanted.Evaluation) (*gen.InteractiveSearch, error) {
	rep := ev.Report
	out := &gen.InteractiveSearch{Kind: gen.MediaKind(rep.Kind), ProfileName: rep.ProfileName, Accepted: rep.Accepted, Pending: rep.Pending,
		Rejected: rep.Rejected, Decisions: make([]gen.InteractiveDecision, 0, len(rep.Decisions)),
		DurationMs: ev.Search.Duration.Milliseconds(), Indexers: make([]gen.SearchIndexerResult, 0, len(ev.Search.Indexers))}
	for i := range rep.Decisions {
		d := rep.Decisions[i]
		r, _ := ev.Release(&d)
		d.Candidate.DownloadURL = ""
		gd, err := convert[gen.Decision](d)
		if err != nil {
			return nil, err
		}
		id := gen.InteractiveDecision{Decision: gd}
		if r.DownloadURL != "" {
			if tok, err := h.Indexers.DownloadToken(r); err == nil {
				id.DownloadToken = gen.NewOptString(tok)
			}
		} else if r.MagnetURL != "" {
			id.MagnetUrl = gen.NewOptString(r.MagnetURL)
		}
		out.Decisions = append(out.Decisions, id)
	}
	for _, ir := range ev.Search.Indexers {
		out.Indexers = append(out.Indexers, toSearchIndexer(ir))
	}
	return out, nil
}

// GrabAlbumRelease implements POST /music/albums/{id}/releases.
func (h *Handler) GrabAlbumRelease(ctx context.Context, req *gen.ManualGrabRequest, p gen.GrabAlbumReleaseParams) (*gen.QueueItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, err := h.musicService(); err != nil {
		return nil, err
	}
	g := wanted.ManualGrab{IndexerID: req.IndexerId, DownloadToken: req.DownloadToken.Or(""), MagnetURL: req.MagnetUrl.Or(""),
		Title: req.Title, Protocol: string(req.Protocol), GUID: req.GUID.Or("")}
	if t, ok := req.PublishDate.Get(); ok {
		g.PublishedAt = &t
	}
	if g.DownloadToken == "" && g.MagnetURL == "" {
		return nil, validation("downloadToken or magnetUrl is required")
	}
	d, err := h.Wanted.Grab(ctx, library.Ref{Kind: library.RefAlbum, ID: p.ID}, g)
	if err != nil {
		return nil, err
	}
	out := toQueueItem(downloads.QueueItem{Download: d})
	h.linkItems(ctx, []*gen.QueueItem{&out})
	return &out, nil
}

// albumLink is the queue link of an album download.
func (h *Handler) albumLink(ctx context.Context, ref library.Ref) *gen.QueueItemLink {
	if h.Music == nil {
		return nil
	}
	al, err := h.Music.Album(ctx, ref.ID)
	if err != nil {
		return nil
	}
	title := al.Title
	if a, err := h.Music.Artist(ctx, al.ArtistID); err == nil {
		title = a.Name + " - " + al.Title
	}
	return &gen.QueueItemLink{Ref: ref.String(), Kind: ref.Kind, ID: ref.ID, Title: title, Year: gen.NewOptInt(al.Year())}
}

func trackLabel(t music.Track) string { return fmt.Sprintf("%d-%d %s", t.Disc, t.Position, t.Title) }

func toMatches(an *music.Analysis) []gen.MusicReleaseMatch {
	out := []gen.MusicReleaseMatch{}
	for _, rm := range an.Matches {
		m := rm.Match
		g := gen.MusicReleaseMatch{Release: toRelease(rm.Release), Score: m.Score, Coverage: m.Coverage, TrackFit: m.TrackFit, AlbumFit: m.AlbumFit,
			ArtistFit: m.ArtistFit, DurationOk: m.DurationOK, IdMatch: m.IDMatch, Pairs: []gen.MusicMatchPair{}, Missing: []string{}, Extra: []string{},
			Notes: m.Notes}
		for _, p := range m.Pairs {
			f, t := an.Folder.Files[p.File], rm.Tracks[p.Track]
			g.Pairs = append(g.Pairs, gen.MusicMatchPair{Path: f.Path, RelativePath: f.Rel, Disc: t.Disc, Position: t.Position, TrackTitle: t.Title,
				FileTitle: optStr(f.Tags.Title), Score: p.Score, DurationDiffMs: p.DurationDiffMs, ById: p.ByID,
				Quality: gen.NewOptQuality(toQuality(music.QualityOf(&f.Tags)))})
		}
		for _, i := range m.Missing {
			g.Missing = append(g.Missing, trackLabel(rm.Tracks[i]))
		}
		for _, i := range m.Extra {
			g.Extra = append(g.Extra, an.Folder.Files[i].Rel)
		}
		out = append(out, g)
	}
	return out
}

// PreviewMusicImport implements GET /music/import/manual.
func (h *Handler) PreviewMusicImport(ctx context.Context, p gen.PreviewMusicImportParams) (*gen.MusicImportPreview, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	an, err := m.Analyse(ctx, p.AlbumId, p.Path, p.ReleaseId.Or(0))
	if err != nil {
		return nil, err
	}
	minScore, _ := settings.Get(ctx, h.Settings, music.MinimumMatchScore)
	out := &gen.MusicImportPreview{AlbumId: p.AlbumId, Path: an.Folder.Root, Files: len(an.Folder.Files), Unreadable: an.Folder.Unreadable,
		Cue: len(an.Folder.Cue) > 0, Cover: optStr(an.Folder.Cover), Matches: toMatches(&an), MinimumScore: minScore}
	if out.Unreadable == nil {
		out.Unreadable = []string{}
	}
	if an.Rejection != nil {
		out.Rejection = gen.NewOptError(gen.Error{Code: an.Rejection.Code, Message: an.Rejection.Message})
	}
	return out, nil
}

// MusicImport implements POST /music/import/manual.
func (h *Handler) MusicImport(ctx context.Context, req *gen.MusicImportRequest) (*gen.MusicImportResult, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	o := music.ImportOptions{ReleaseID: req.ReleaseId.Or(0), Force: req.Force.Or(false), Mode: string(req.Mode.Or(""))}
	if v, ok := req.AllowPartial.Get(); ok {
		o.AllowPartial = &v
	}
	res, err := m.Import(ctx, req.AlbumId, req.Path, o)
	if err != nil {
		return nil, err
	}
	out := &gen.MusicImportResult{AlbumId: res.AlbumID, Release: gen.NewOptAlbumRelease(toRelease(res.Release)), Score: res.Score,
		Imported: res.Imported, Skipped: []gen.MusicImportResultSkippedItem{}, Recycled: res.Recycled, Missing: res.Missing, Upgraded: res.Upgraded,
		Cover: optStr(res.CoverPath)}
	if out.Imported == nil {
		out.Imported = []string{}
	}
	if out.Recycled == nil {
		out.Recycled = []string{}
	}
	for _, s := range res.Skipped {
		out.Skipped = append(out.Skipped, gen.MusicImportResultSkippedItem{Path: s.Path, Reason: s.Reason})
	}
	return out, nil
}

// ScanExistingMusic implements GET /music/import/scan.
func (h *Handler) ScanExistingMusic(ctx context.Context, p gen.ScanExistingMusicParams) ([]gen.ExistingArtistFolder, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	list, err := m.ScanRootFolder(ctx, p.RootFolderId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ExistingArtistFolder, 0, len(list))
	for _, ea := range list {
		g := gen.ExistingArtistFolder{Path: ea.Path, Name: ea.Name, TagArtist: ea.TagArtist, Albums: []gen.ExistingAlbumFolder{},
			Candidates: []gen.ArtistLookup{}, ArtistId: optI64(ea.ArtistID), Error: optStr(ea.Error)}
		for _, al := range ea.Albums {
			g.Albums = append(g.Albums, gen.ExistingAlbumFolder{Path: al.Path, Name: al.Name, Album: al.Album, Year: al.Year, Files: al.Files})
		}
		if ea.Match != nil {
			g.Match = gen.NewOptArtistLookup(toArtistLookup(*ea.Match, nil))
		}
		for _, c := range ea.Candidates {
			g.Candidates = append(g.Candidates, toArtistLookup(c, nil))
		}
		out = append(out, g)
	}
	return out, nil
}

// ImportExistingMusic implements POST /music/import/existing.
func (h *Handler) ImportExistingMusic(ctx context.Context, req *gen.ImportExistingArtistsRequest) ([]gen.ImportExistingArtistResult, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	base := music.AddInput{RootFolderID: req.RootFolderId, QualityProfileID: req.QualityProfileId, MetadataProfileID: req.MetadataProfileId,
		Monitored: req.Monitored.Or(true), Monitor: string(req.Monitor.Or(gen.ImportExistingArtistsRequestMonitorAll)),
		MonitorNewAlbums: string(req.MonitorNewAlbums.Or(gen.MonitorNewAlbumsAll)), SearchOnAdd: req.SearchOnAdd.Or(false)}
	items := make([]music.ExistingImport, 0, len(req.Items))
	for _, it := range req.Items {
		in := base
		in.MBID = it.Mbid
		items = append(items, music.ExistingImport{Path: it.Path, AddInput: in})
	}
	added, errs := m.ImportExisting(ctx, items)
	out := make([]gen.ImportExistingArtistResult, 0, len(items))
	for i := range items {
		r := gen.ImportExistingArtistResult{Path: items[i].Path}
		if errs[i] != nil {
			r.Error = gen.NewOptError(errBody(errs[i]))
		}
		if added[i].ID > 0 {
			if a, err := h.artistOut(ctx, added[i].ID); err == nil {
				r.Artist = gen.NewOptArtist(*a)
			}
			if h.LinkExisting != nil && errs[i] == nil {
				h.LinkExisting(ctx, added[i].ID)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (h *Handler) profileOut(ctx context.Context, p music.MetadataProfile) gen.MetadataProfile {
	var n int
	_ = h.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE metadata_profile_id = ?`, p.ID).Scan(&n)
	return gen.MetadataProfile{ID: p.ID, Name: p.Name, PrimaryTypes: p.PrimaryTypes, SecondaryTypes: p.SecondaryTypes, ReleaseStatuses: p.ReleaseStatuses,
		Artists: n}
}

// ListMetadataProfiles implements GET /music/metadataprofiles.
func (h *Handler) ListMetadataProfiles(ctx context.Context) ([]gen.MetadataProfile, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	list, err := m.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.MetadataProfile, 0, len(list))
	for _, p := range list {
		out = append(out, h.profileOut(ctx, p))
	}
	return out, nil
}

func fromProfileInput(in *gen.MetadataProfileInput) music.MetadataProfile {
	p := music.MetadataProfile{Name: in.Name, PrimaryTypes: []string{}, SecondaryTypes: []string{}, ReleaseStatuses: []string{}}
	for _, v := range in.PrimaryTypes {
		p.PrimaryTypes = append(p.PrimaryTypes, string(v))
	}
	for _, v := range in.SecondaryTypes {
		p.SecondaryTypes = append(p.SecondaryTypes, string(v))
	}
	for _, v := range in.ReleaseStatuses {
		p.ReleaseStatuses = append(p.ReleaseStatuses, string(v))
	}
	return p
}

// CreateMetadataProfile implements POST /music/metadataprofiles.
func (h *Handler) CreateMetadataProfile(ctx context.Context, req *gen.MetadataProfileInput) (*gen.MetadataProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	p, err := m.SaveProfile(ctx, fromProfileInput(req))
	if err != nil {
		return nil, err
	}
	out := h.profileOut(ctx, p)
	return &out, nil
}

// UpdateMetadataProfile implements PUT /music/metadataprofiles/{id}.
func (h *Handler) UpdateMetadataProfile(ctx context.Context, req *gen.MetadataProfileInput, p gen.UpdateMetadataProfileParams) (*gen.MetadataProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	in := fromProfileInput(req)
	in.ID = p.ID
	saved, err := m.SaveProfile(ctx, in)
	if err != nil {
		return nil, err
	}
	out := h.profileOut(ctx, saved)
	return &out, nil
}

// DeleteMetadataProfile implements DELETE /music/metadataprofiles/{id}.
func (h *Handler) DeleteMetadataProfile(ctx context.Context, p gen.DeleteMetadataProfileParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	m, err := h.musicService()
	if err != nil {
		return err
	}
	return m.DeleteProfile(ctx, p.ID)
}

// GetMusicCalendar implements GET /music/calendar.
func (h *Handler) GetMusicCalendar(ctx context.Context, p gen.GetMusicCalendarParams) ([]gen.AlbumCalendarEntry, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	if p.End.Before(p.Start) || p.End.Sub(p.Start) > 400*24*time.Hour {
		return nil, validation("end must be after start and at most 400 days later")
	}
	list, err := m.Calendar(ctx, day(p.Start), day(p.End))
	if err != nil {
		return nil, err
	}
	out := make([]gen.AlbumCalendarEntry, 0, len(list))
	for _, e := range list {
		d, _ := time.Parse("2006-01-02", e.Date)
		out = append(out, gen.AlbumCalendarEntry{Date: d, AlbumId: e.AlbumID, ArtistId: e.ArtistID, ArtistName: e.ArtistName, Title: e.Title,
			Type: e.Type, Monitored: e.Monitored, HasFiles: e.HasFiles, Cover: musicbrainz.CoverName(e.Cover)})
	}
	return out, nil
}

// musicFeedEvents writes album releases into the iCal feed.
func (h *Handler) musicFeedEvents(ctx context.Context, start, end, stamp string, write func(uid, date, summary string)) {
	if h.Music == nil {
		return
	}
	list, err := h.Music.Calendar(ctx, start, end)
	if err != nil {
		return
	}
	for _, e := range list {
		write(fmt.Sprintf("locarr-album-%d@locarr", e.AlbumID), e.Date, fmt.Sprintf("%s - %s (album release)", e.ArtistName, e.Title))
	}
	_ = stamp
}

func (h *Handler) musicSettings(ctx context.Context) (*gen.MusicSettings, error) {
	var (
		out gen.MusicSettings
		err error
	)
	str := func(k settings.Key[string], dst *string) {
		if err == nil {
			*dst, err = settings.Get(ctx, h.Settings, k)
		}
	}
	boolean := func(k settings.Key[bool], dst *bool) {
		if err == nil {
			*dst, err = settings.Get(ctx, h.Settings, k)
		}
	}
	str(music.ArtistFolderFormat, &out.ArtistFolderFormat)
	str(music.AlbumFolderFormat, &out.AlbumFolderFormat)
	str(music.TrackFormat, &out.TrackFormat)
	str(music.MultiDiscTrackFormat, &out.MultiDiscTrackFormat)
	str(music.DiscFolderFormat, &out.DiscFolderFormat)
	str(metadata.MusicBrainzContact, &out.MusicBrainzContact)
	boolean(music.DiscFolders, &out.DiscFolders)
	boolean(music.RenameTracks, &out.RenameTracks)
	boolean(music.ImportPartial, &out.ImportPartialAlbums)
	boolean(music.WriteTags, &out.WriteTags)
	boolean(music.SaveCoverArt, &out.SaveCoverArt)
	if err == nil {
		out.MinimumMatchScore, err = settings.Get(ctx, h.Settings, music.MinimumMatchScore)
	}
	var colon string
	str(library.ColonReplacement, &colon)
	out.ColonReplacement = gen.NewOptColonReplacement(gen.ColonReplacement(colon))
	return &out, err
}

// GetMusicSettings implements GET /settings/music.
func (h *Handler) GetMusicSettings(ctx context.Context) (*gen.MusicSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.musicSettings(ctx)
}

type musicTemplate struct {
	name, value string
	tokens      []naming.Token
}

func musicTemplates(req *gen.MusicSettings) []musicTemplate {
	return []musicTemplate{{"artistFolderFormat", req.ArtistFolderFormat, naming.ArtistTokens}, {"albumFolderFormat", req.AlbumFolderFormat, naming.AlbumTokens},
		{"trackFormat", req.TrackFormat, naming.TrackTokens}, {"multiDiscTrackFormat", req.MultiDiscTrackFormat, naming.TrackTokens},
		{"discFolderFormat", req.DiscFolderFormat, naming.DiscTokens}}
}

// UpdateMusicSettings implements PUT /settings/music.
func (h *Handler) UpdateMusicSettings(ctx context.Context, req *gen.MusicSettings) (*gen.MusicSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	for _, t := range musicTemplates(req) {
		if err := naming.Validate(t.value, t.tokens); err != nil {
			return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_template", Message: t.name + ": " + err.Error()}
		}
	}
	if req.MinimumMatchScore < 40 || req.MinimumMatchScore > 100 {
		return nil, validation("minimumMatchScore must be between 40 and 100")
	}
	contact := strings.TrimSpace(req.MusicBrainzContact)
	if strings.ContainsAny(contact, "()\r\n") {
		return nil, validation("the contact cannot contain parentheses or line breaks")
	}
	err := h.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, f := range []func() error{
			func() error { return settings.Set(ctx, h.Settings, tx, music.ArtistFolderFormat, req.ArtistFolderFormat) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.AlbumFolderFormat, req.AlbumFolderFormat) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.TrackFormat, req.TrackFormat) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.MultiDiscTrackFormat, req.MultiDiscTrackFormat) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.DiscFolderFormat, req.DiscFolderFormat) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.DiscFolders, req.DiscFolders) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.RenameTracks, req.RenameTracks) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.ImportPartial, req.ImportPartialAlbums) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.WriteTags, req.WriteTags) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.SaveCoverArt, req.SaveCoverArt) },
			func() error { return settings.Set(ctx, h.Settings, tx, music.MinimumMatchScore, req.MinimumMatchScore) },
			func() error { return settings.Set(ctx, h.Settings, tx, metadata.MusicBrainzContact, contact) },
		} {
			if err := f(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return h.musicSettings(ctx)
}

// PreviewMusicNaming implements POST /settings/music/preview.
func (h *Handler) PreviewMusicNaming(ctx context.Context, req *gen.MusicSettings) (*gen.MusicNamingPreview, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	opts := naming.Options{Colon: string(req.ColonReplacement.Or(gen.ColonReplacementDash))}
	a := naming.ArtistInfo{Name: "The Beatles", Disambiguation: "UK rock band", MBID: "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"}
	al := naming.AlbumInfo{Title: "Abbey Road", Type: "album", ReleaseDate: "1969-09-26", MBID: "9162580e-5df4-32de-80cc-f45a8d8a9b1d",
		EditionDate: "2019-09-27", Country: "GB", Format: "2×CD", ReleaseDisambiguation: "50th anniversary", Label: "Apple Records", Discs: 2, Tracks: 40}
	t1 := naming.TrackInfo{Disc: 1, Number: 1, Title: "Come Together", Quality: "FLAC 24bit", Format: "flac", BitDepth: 24, SampleRate: 96000, Extension: ".flac"}
	t2 := t1
	t2.Disc, t2.Number, t2.Title = 2, 3, "Something (Studio Demo)"
	out := &gen.MusicNamingPreview{Tokens: make([]gen.NamingToken, 0, len(naming.TrackTokens))}
	for _, t := range naming.TrackTokens {
		out.Tokens = append(out.Tokens, gen.NamingToken{Name: t.Name, Example: t.Example})
	}
	errs := gen.MusicNamingPreviewErrors{}
	render := func(name, tmpl string, tokens []naming.Token, v naming.Values) string {
		if err := naming.Validate(tmpl, tokens); err != nil {
			errs[name] = err.Error()
			return ""
		}
		s, err := naming.Render(tmpl, v, opts)
		if err != nil {
			errs[name] = err.Error()
		}
		return s
	}
	out.ArtistFolder = render("artistFolderFormat", req.ArtistFolderFormat, naming.ArtistTokens, naming.MusicValues(a, nil, nil))
	out.AlbumFolder = render("albumFolderFormat", req.AlbumFolderFormat, naming.AlbumTokens, naming.MusicValues(a, &al, &t1))
	single := al
	single.Discs = 1
	out.Track = naming.WithExtension(render("trackFormat", req.TrackFormat, naming.TrackTokens, naming.MusicValues(a, &single, &t1)), ".flac")
	if req.DiscFolders {
		disc := render("discFolderFormat", req.DiscFolderFormat, naming.DiscTokens, naming.MusicValues(a, &al, &t2))
		out.MultiDiscTrack = disc + "/" + naming.WithExtension(render("multiDiscTrackFormat", req.TrackFormat, naming.TrackTokens, naming.MusicValues(a, &al, &t2)), ".flac")
	} else {
		out.MultiDiscTrack = naming.WithExtension(render("multiDiscTrackFormat", req.MultiDiscTrackFormat, naming.TrackTokens, naming.MusicValues(a, &al, &t2)), ".flac")
	}
	if len(errs) > 0 {
		out.Errors = gen.NewOptMusicNamingPreviewErrors(errs)
	}
	return out, nil
}

// GetCoverArt implements GET /images/coverart/{size}/{file}.
func (h *Handler) GetCoverArt(ctx context.Context, p gen.GetCoverArtParams) (gen.GetCoverArtRes, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	if h.CoverArt == nil {
		return nil, metadata.ErrUnavailable
	}
	img, err := h.CoverArt.Get(ctx, string(p.Size), p.File)
	if err != nil {
		return nil, err
	}
	cache := gen.NewOptString("private, max-age=604800")
	if inm, ok := p.IfNoneMatch.Get(); ok && strings.Contains(inm, img.ETag) {
		return &gen.GetCoverArtNotModified{ETag: gen.NewOptString(img.ETag), CacheControl: cache}, nil
	}
	f, err := os.Open(img.Path) // #nosec G304 -- inside the image cache, name validated
	if err != nil {
		return nil, err
	}
	return &gen.GetCoverArtOKHeaders{ETag: gen.NewOptString(img.ETag), CacheControl: cache, ContentType: img.ContentType,
		Response: gen.GetCoverArtOK{Data: f}}, nil
}

// explainMusic answers "why isn't it here?" for an album (and sums up an artist).
func (h *Handler) explainMusic(ctx context.Context, ref library.Ref) (timeline.Explanation, string, error) {
	if h.Music == nil {
		return timeline.Explanation{}, "", fmt.Errorf("%w: the music library is not running", library.ErrBadRef)
	}
	if ref.Kind == library.RefArtist {
		a, err := h.Music.Artist(ctx, ref.ID)
		if err != nil {
			return timeline.Explanation{}, "", err
		}
		list, err := h.Music.AlbumStates(ctx, &a)
		if err != nil {
			return timeline.Explanation{}, "", err
		}
		counts := map[string]any{"albums": len(list)}
		for _, st := range list {
			k := st.Status()
			n, _ := counts[k].(int)
			counts[k] = n + 1
		}
		return timeline.Explanation{Code: "artist", Tone: "info", Message: fmt.Sprintf("%d albums", len(list)), Params: counts,
			Reasons: []timeline.ReasonCount{}}, a.Name, nil
	}
	al, err := h.Music.Album(ctx, ref.ID)
	if err != nil {
		return timeline.Explanation{}, "", err
	}
	st, err := h.Music.State(ctx, al, nil, nil)
	if err != nil {
		return timeline.Explanation{}, "", err
	}
	facts, err := h.Timeline.Facts(ctx, ref.String(), "music")
	if err != nil {
		return timeline.Explanation{}, "", err
	}
	is := timeline.ItemState{Ref: ref.String(), Monitored: st.Monitored && st.ArtistMonitored, Available: st.Available,
		HasFile: st.FileCount > 0, CutoffMet: st.CutoffMet}
	if t := music.ReleaseTime(al.ReleaseDate); !t.IsZero() {
		is.AvailableFrom = &t
	}
	ex := timeline.Explain(is, facts)
	if st.FileCount > 0 && !st.Complete() && ex.Code == timeline.ExplainUpgradeWanted {
		ex.Params["files"], ex.Params["tracks"] = st.FileCount, st.TrackCount
		ex.Message = fmt.Sprintf("%d of %d tracks are in the library; Locarr keeps looking for the whole album", st.FileCount, st.TrackCount)
	}
	return ex, st.ArtistName + " - " + al.Title, nil
}
