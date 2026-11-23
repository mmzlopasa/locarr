package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero"
	"net/http"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// mapSeriesError maps errors of the series package (nil = not one of them).
func mapSeriesError(err error) *StatusError {
	if ie, ok := series.IsImportError(err); ok {
		return &StatusError{Status: http.StatusUnprocessableEntity, Code: ie.Code, Message: ie.Message}
	}
	table := []struct {
		err    error
		status int
		code   string
	}{
		{series.ErrNotFound, http.StatusNotFound, "not_found"},
		{series.ErrSeasonNotFound, http.StatusNotFound, "not_found"},
		{series.ErrEpisodeNotFound, http.StatusNotFound, "not_found"},
		{series.ErrExists, http.StatusConflict, "already_exists"},
		{series.ErrPathTaken, http.StatusConflict, "path_taken"},
		{series.ErrHasFiles, http.StatusConflict, "has_files"},
		{series.ErrInvalid, http.StatusBadRequest, "validation"},
	}
	for _, t := range table {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return nil
}

func toSeriesStats(st series.Stats) gen.SeriesStats {
	return gen.SeriesStats{SeasonCount: st.Seasons, EpisodeCount: st.Episodes, EpisodeFileCount: st.EpisodeFiles,
		MonitoredEpisodeCount: st.MonitoredEpisodes, MissingCount: st.Missing, SizeOnDisk: st.SizeOnDisk,
		NextAiring: optTime(st.NextAiring), PreviousAiring: optTime(st.PreviousAiring)}
}

func toSeries(it series.ListItem) gen.Series {
	v := it.Series
	return gen.Series{ID: v.ID, ItemRef: v.Ref().String(), TmdbId: v.TMDbID, TvdbId: v.TVDbID, ImdbId: v.IMDbID, TvmazeId: v.TVMazeID,
		MetadataSource: gen.SeriesMetadataSource(v.Source), Title: v.Title, OriginalTitle: v.OriginalTitle, OriginalLanguage: v.OriginalLanguage,
		AlternateTitles: v.AlternateTitles, Year: v.Year, Overview: v.Overview, Status: gen.SeriesStatus(v.Status), Network: v.Network,
		TimeZone: v.TimeZone, AirTime: v.AirTime, Runtime: v.Runtime, Genres: v.Genres, Certification: v.Certification, Rating: v.Rating,
		Votes: v.Votes, FirstAired: v.FirstAired, LastAired: v.LastAired, SeriesType: gen.SeriesType(v.SeriesType), SeasonFolder: v.SeasonFolder,
		MonitorNewSeasons: gen.MonitorNewSeasons(v.MonitorNewSeasons), Poster: v.Poster, Backdrop: v.Backdrop, Monitored: v.Monitored,
		QualityProfileId: v.QualityProfileID, RootFolderId: optI64(v.RootFolderID), Path: v.Path, Tags: v.Tags, AddedAt: v.AddedAt,
		UpdatedAt: v.UpdatedAt, RefreshedAt: optTime(v.RefreshedAt), Stats: toSeriesStats(it.Stats), Downloading: it.Downloading}
}

func toSeason(sv series.SeasonView) gen.SeasonInfo {
	return gen.SeasonInfo{ID: sv.ID, ItemRef: sv.Ref().String(), Number: sv.Number, Monitored: sv.Monitored, Title: sv.Title,
		Overview: sv.Overview, Poster: sv.Poster, AirDate: sv.AirDate, Stats: toSeriesStats(sv.Stats)}
}

func optIntPtr(p *int) gen.OptInt {
	if p == nil {
		return gen.OptInt{}
	}
	return gen.NewOptInt(*p)
}

func toEpisode(ev series.EpisodeView) gen.Episode {
	e := ev.Episode
	w := ev.Wanted
	if w == "" {
		w = "none"
	}
	out := gen.Episode{ID: e.ID, ItemRef: e.Ref().String(), SeriesId: e.SeriesID, SeasonId: e.SeasonID, SeasonNumber: e.Season,
		EpisodeNumber: e.Number, AbsoluteNumber: e.Absolute, Title: e.Title, Overview: e.Overview, AirDate: e.AirDate,
		AirDateUtc: optTime(e.AirDateUTC), Runtime: e.Runtime, Monitored: e.Monitored, Aired: ev.Aired, Removed: e.Removed,
		SceneSeason: optIntPtr(e.SceneSeason), SceneEpisode: optIntPtr(e.SceneEpisode), SceneAbsolute: optIntPtr(e.SceneAbsolute),
		Wanted: gen.EpisodeWanted(w), CutoffMet: ev.CutoffMet}
	if f := e.File; f != nil {
		mi, _ := convert[gen.MediaInfo](f.MediaInfo)
		out.File = gen.NewOptEpisodeFile(gen.EpisodeFile{ID: f.ID, Path: f.Path, SizeBytes: f.Size, Quality: toQuality(f.Quality),
			Revision: max(f.Revision.Version, 1), CustomFormats: f.CustomFormats, Score: f.Score, ReleaseGroup: f.ReleaseGroup,
			Languages: f.Languages, MediaInfo: mi, SceneName: f.SceneName, DownloadId: optI64(f.DownloadID), AddedAt: f.AddedAt})
	}
	if d := ev.Download; d != nil {
		out.Download = gen.NewOptMovieDownload(gen.MovieDownload{ID: d.ID, Title: d.Title, Progress: d.Progress, State: d.State, Status: d.Status})
	}
	return out
}

func toSeriesLookup(r metadata.SeriesSummary, source string, id *int64) gen.SeriesLookup {
	return gen.SeriesLookup{TmdbId: r.TMDbID, TvdbId: r.TVDbID, Source: gen.SeriesLookupSource(source), Title: r.Title,
		OriginalTitle: r.OriginalTitle, Year: r.Year, FirstAired: r.FirstAired, Overview: r.Overview, Poster: r.Poster, Backdrop: r.Backdrop,
		Network: r.Network, Status: r.Status, Rating: r.Rating, SeriesId: optI64(id)}
}

func (h *Handler) seriesOut(ctx context.Context, id int64) (*gen.Series, error) {
	it, err := h.Series.Item(ctx, id)
	if err != nil {
		return nil, err
	}
	out := toSeries(it)
	return &out, nil
}

// ListSeries implements GET /series.
func (h *Handler) ListSeries(ctx context.Context, p gen.ListSeriesParams) (*gen.SeriesList, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	f := series.ListFilter{Status: string(p.Status.Or("")), Missing: p.Missing.Or(false), ProfileID: p.ProfileId.Or(0), Tag: p.Tag.Or(""),
		Search: p.Q.Or(""), Sort: string(p.Sort.Or(gen.ListSeriesSortTitle)), Desc: p.Desc.Or(false), Offset: p.Offset.Or(0), Limit: p.Limit.Or(100)}
	if v, ok := p.Monitored.Get(); ok {
		f.Monitored = &v
	}
	items, total, err := h.Series.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := &gen.SeriesList{Total: total, Items: make([]gen.Series, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, toSeries(it))
	}
	return out, nil
}

func seriesAddInput(rootID, profileID int64, typ gen.OptSeriesType, folder gen.OptBool, mon gen.OptMonitorOption, newSeasons gen.OptMonitorNewSeasons,
	monitored gen.OptBool, tags []string, search gen.OptBool) series.AddInput {
	return series.AddInput{RootFolderID: rootID, QualityProfileID: profileID, SeriesType: string(typ.Or(gen.SeriesTypeStandard)),
		SeasonFolder: folder.Or(true), Monitor: string(mon.Or(gen.MonitorOptionAll)), MonitorNewSeasons: string(newSeasons.Or(gen.MonitorNewSeasonsAll)),
		Monitored: monitored.Or(true), Tags: tags, SearchOnAdd: search.Or(false)}
}

// AddSeries implements POST /series.
func (h *Handler) AddSeries(ctx context.Context, req *gen.AddSeriesRequest) (*gen.Series, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	in := seriesAddInput(req.RootFolderId, req.QualityProfileId, req.SeriesType, req.SeasonFolder, req.Monitor, req.MonitorNewSeasons,
		req.Monitored, req.Tags, req.SearchOnAdd)
	in.TMDbID, in.TVDbID, in.Path = req.TmdbId.Or(0), req.TvdbId.Or(0), req.Path.Or("")
	v, err := h.Series.Add(ctx, in)
	if err != nil {
		return nil, err
	}
	return h.seriesOut(ctx, v.ID)
}

// LookupSeries implements GET /series/lookup.
func (h *Handler) LookupSeries(ctx context.Context, p gen.LookupSeriesParams) ([]gen.SeriesLookup, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Series.Lookup(ctx, p.Term)
	if err != nil {
		return nil, err
	}
	out := make([]gen.SeriesLookup, 0, len(list))
	titles := make([]streamero.Title, 0, len(list))
	for _, r := range list {
		out = append(out, toSeriesLookup(r.SeriesSummary, r.Source, r.SeriesID))
		titles = append(titles, streamero.Title{Kind: "series", TMDbID: r.TMDbID, TVDbID: r.TVDbID})
	}
	for i, link := range h.streameroLinks(ctx, titles) {
		out[i].StreameroUrl = gen.NewOptString(link)
	}
	return out, nil
}

func (h *Handler) enqueueSeries(ctx context.Context, id int64, reason string) (int, error) {
	refs, err := h.Series.SearchTargets(ctx, id)
	if err != nil {
		return 0, err
	}
	for _, r := range refs {
		if err := h.Wanted.Enqueue(ctx, r, reason); err != nil {
			return 0, err
		}
	}
	return len(refs), nil
}

// SearchSeriesBulk implements POST /series/search.
func (h *Handler) SearchSeriesBulk(ctx context.Context, req *gen.SeriesIdsRequest) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	n := 0
	for _, id := range req.SeriesIds {
		c, err := h.enqueueSeries(ctx, id, "manual")
		if errors.Is(err, series.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		n += c
	}
	return &gen.QueuedCount{Count: n}, nil
}

func toSeriesRename(list []series.RenameItem) []gen.SeriesRenameItem {
	out := make([]gen.SeriesRenameItem, 0, len(list))
	for _, r := range list {
		out = append(out, gen.SeriesRenameItem{SeriesId: r.SeriesID, Title: r.Title, From: r.From, To: r.To})
	}
	return out
}

// RenameSeries implements POST /series/rename.
func (h *Handler) RenameSeries(ctx context.Context, req *gen.SeriesIdsRequest) ([]gen.SeriesRenameItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	done, err := h.Series.Rename(ctx, req.SeriesIds)
	return toSeriesRename(done), err
}

// PreviewRenameSeries implements POST /series/rename/preview.
func (h *Handler) PreviewRenameSeries(ctx context.Context, req *gen.SeriesIdsRequest) ([]gen.SeriesRenameItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	plan, err := h.Series.RenamePreview(ctx, req.SeriesIds)
	if err != nil {
		return nil, err
	}
	return toSeriesRename(plan), nil
}

// ScanExistingSeries implements GET /series/import/scan.
func (h *Handler) ScanExistingSeries(ctx context.Context, p gen.ScanExistingSeriesParams) ([]gen.ExistingSeriesFolder, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Series.ScanRootFolder(ctx, p.RootFolderId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ExistingSeriesFolder, 0, len(list))
	for _, f := range list {
		g := gen.ExistingSeriesFolder{Path: f.Path, Name: f.Name, Title: f.Title, Year: f.Year, Videos: f.Videos,
			Candidates: make([]gen.SeriesLookup, 0, len(f.Candidates)), Error: optStr(f.Error)}
		if f.Match != nil {
			g.Match = gen.NewOptSeriesLookup(toSeriesLookup(*f.Match, sourceOf(f.Match), nil))
		}
		for _, c := range f.Candidates {
			g.Candidates = append(g.Candidates, toSeriesLookup(c, sourceOf(&c), nil))
		}
		out = append(out, g)
	}
	return out, nil
}

func sourceOf(s *metadata.SeriesSummary) string {
	if s.TMDbID == 0 && s.TVDbID > 0 {
		return "tvdb"
	}
	return "tmdb"
}

// ImportExistingSeries implements POST /series/import/existing.
func (h *Handler) ImportExistingSeries(ctx context.Context, req *gen.ImportExistingSeriesRequest) ([]gen.ImportExistingSeriesResult, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list := make([]series.ExistingImport, 0, len(req.Items))
	for _, it := range req.Items {
		in := seriesAddInput(it.RootFolderId, it.QualityProfileId, it.SeriesType, it.SeasonFolder, it.Monitor, it.MonitorNewSeasons,
			it.Monitored, it.Tags, it.SearchOnAdd)
		in.TMDbID, in.TVDbID = it.TmdbId.Or(0), it.TvdbId.Or(0)
		list = append(list, series.ExistingImport{Path: it.Path, AddInput: in})
	}
	res := h.Series.ImportExisting(ctx, list)
	out := make([]gen.ImportExistingSeriesResult, 0, len(res))
	for i, r := range res {
		g := gen.ImportExistingSeriesResult{Path: list[i].Path, EpisodeFiles: r.Found}
		if r.Err != nil {
			g.Error = gen.NewOptError(errBody(r.Err))
		}
		if r.Series.ID > 0 {
			if s, err := h.seriesOut(ctx, r.Series.ID); err == nil {
				g.Series = gen.NewOptSeries(*s)
			}
		}
		out = append(out, g)
	}
	return out, nil
}

// PreviewManualImportSeries implements GET /series/import/manual.
func (h *Handler) PreviewManualImportSeries(ctx context.Context, p gen.PreviewManualImportSeriesParams) ([]gen.SeriesImportCandidate, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Series.PreviewImport(ctx, p.Path, p.SeriesId.Or(0))
	if err != nil {
		return nil, err
	}
	out := make([]gen.SeriesImportCandidate, 0, len(list))
	for _, c := range list {
		g := gen.SeriesImportCandidate{Path: c.Path, RelativePath: c.Rel, SizeBytes: c.Size, Sidecars: c.Sidecars,
			Episodes: make([]gen.EpisodeBrief, 0, len(c.Episodes)), ReleaseGroup: optStr(c.Release.Group)}
		if g.Sidecars == nil {
			g.Sidecars = []string{}
		}
		if c.SeriesID > 0 {
			g.SeriesId, g.SeriesTitle = gen.NewOptInt64(c.SeriesID), gen.NewOptString(c.SeriesTitle)
		}
		for _, e := range c.Episodes {
			g.Episodes = append(g.Episodes, gen.EpisodeBrief{ID: e.ID, SeasonNumber: e.Season, EpisodeNumber: e.Number, Title: e.Title})
		}
		if c.Quality.ID != "" {
			g.Quality = gen.NewOptQuality(toQuality(c.Quality.ID))
		}
		if c.Rejection != nil {
			g.Rejection = gen.NewOptError(gen.Error{Code: c.Rejection.Code, Message: c.Rejection.Message})
		}
		out = append(out, g)
	}
	return out, nil
}

// ManualImportSeries implements POST /series/import/manual.
func (h *Handler) ManualImportSeries(ctx context.Context, req *gen.SeriesManualImportRequest) ([]gen.SeriesManualImportResult, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	reqs := make([]series.ImportRequest, 0, len(req.Items))
	for _, it := range req.Items {
		reqs = append(reqs, series.ImportRequest{SeriesID: it.SeriesId, Path: it.Path, EpisodeIDs: it.EpisodeIds, Mode: string(it.Mode.Or("")),
			Sidecars: it.Sidecars, Single: len(req.Items) == 1})
	}
	res, errs := h.Series.ImportFiles(ctx, reqs)
	out := make([]gen.SeriesManualImportResult, 0, len(res))
	for i, r := range res {
		g := gen.SeriesManualImportResult{Path: reqs[i].Path, SeriesId: reqs[i].SeriesID, EpisodeIds: r.EpisodeIDs}
		if g.EpisodeIds == nil {
			g.EpisodeIds = []int64{}
		}
		if errs[i] != nil {
			g.Error = gen.NewOptError(errBody(errs[i]))
		} else {
			g.ImportedPath, g.Mode, g.Upgraded = gen.NewOptString(r.Path), gen.NewOptString(r.Mode), gen.NewOptBool(r.Upgraded)
			g.Recycled = r.Recycled
		}
		out = append(out, g)
	}
	return out, nil
}

// GetSeries implements GET /series/{id}.
func (h *Handler) GetSeries(ctx context.Context, p gen.GetSeriesParams) (*gen.SeriesDetail, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	s, err := h.seriesOut(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	seasons, err := h.Series.SeasonViews(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	list := make([]gen.SeasonInfo, 0, len(seasons))
	for _, sv := range seasons {
		list = append(list, toSeason(sv))
	}
	// SeriesDetail is Series plus seasons: merge the two JSON objects.
	raw, err := s.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	sl, err := convert[json.RawMessage](list)
	if err != nil {
		return nil, err
	}
	fields["seasons"] = sl
	merged, _ := json.Marshal(fields)
	var out gen.SeriesDetail
	if err := out.UnmarshalJSON(merged); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSeries implements PATCH /series/{id}.
func (h *Handler) UpdateSeries(ctx context.Context, req *gen.SeriesUpdate, p gen.UpdateSeriesParams) (*gen.Series, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	var u series.Update
	if v, ok := req.Monitored.Get(); ok {
		u.Monitored = &v
	}
	if v, ok := req.QualityProfileId.Get(); ok {
		u.QualityProfileID = &v
	}
	if v, ok := req.SeriesType.Get(); ok {
		s := string(v)
		u.SeriesType = &s
	}
	if v, ok := req.SeasonFolder.Get(); ok {
		u.SeasonFolder = &v
	}
	if v, ok := req.MonitorNewSeasons.Get(); ok {
		s := string(v)
		u.MonitorNewSeasons = &s
	}
	if v, ok := req.Monitor.Get(); ok {
		s := string(v)
		u.Monitor = &s
	}
	if req.Tags != nil {
		t := req.Tags
		u.Tags = &t
	}
	if v, ok := req.RootFolderId.Get(); ok {
		u.RootFolderID = &v
	}
	if v, ok := req.Path.Get(); ok {
		u.Path = &v
	}
	if _, err := h.Series.Edit(ctx, p.ID, u); err != nil {
		return nil, err
	}
	if u.Monitor != nil || (u.Monitored != nil && *u.Monitored) || u.QualityProfileID != nil {
		_, _ = h.enqueueDue(ctx, p.ID)
	}
	return h.seriesOut(ctx, p.ID)
}

// enqueueDue makes the series' wanted items due for the next scheduled search.
func (h *Handler) enqueueDue(ctx context.Context, id int64) (int, error) {
	refs, err := h.Series.SearchTargets(ctx, id)
	if err != nil {
		return 0, err
	}
	for _, r := range refs {
		h.Wanted.ResetBackoff(ctx, r)
	}
	return len(refs), nil
}

// DeleteSeries implements DELETE /series/{id}.
func (h *Handler) DeleteSeries(ctx context.Context, p gen.DeleteSeriesParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Series.Delete(ctx, p.ID, series.DeleteOptions{DeleteFiles: p.DeleteFiles.Or(false)})
}

// ListSeriesEpisodes implements GET /series/{id}/episodes.
func (h *Handler) ListSeriesEpisodes(ctx context.Context, p gen.ListSeriesEpisodesParams) ([]gen.Episode, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Series.EpisodeViews(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Episode, 0, len(list))
	for _, ev := range list {
		out = append(out, toEpisode(ev))
	}
	return out, nil
}

// RefreshSeries implements POST /series/{id}/refresh.
func (h *Handler) RefreshSeries(ctx context.Context, p gen.RefreshSeriesParams) (*gen.Series, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, _, err := h.Series.Refresh(ctx, p.ID); err != nil {
		return nil, err
	}
	return h.seriesOut(ctx, p.ID)
}

// SearchSeries implements POST /series/{id}/search.
func (h *Handler) SearchSeries(ctx context.Context, p gen.SearchSeriesParams) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	n, err := h.enqueueSeries(ctx, p.ID, "manual")
	if err != nil {
		return nil, err
	}
	return &gen.QueuedCount{Count: n}, nil
}

// UpdateSeason implements PATCH /series/{id}/seasons/{number}.
func (h *Handler) UpdateSeason(ctx context.Context, req *gen.SeasonUpdate, p gen.UpdateSeasonParams) (*gen.SeasonInfo, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, err := h.Series.SetSeasonMonitored(ctx, p.ID, p.Number, req.Monitored); err != nil {
		return nil, err
	}
	seasons, err := h.Series.SeasonViews(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, sv := range seasons {
		if sv.Number == p.Number {
			out := toSeason(sv)
			return &out, nil
		}
	}
	return nil, series.ErrSeasonNotFound
}

func (h *Handler) seasonRef(ctx context.Context, seriesID int64, number int) (library.Ref, error) {
	sn, err := h.Series.SeasonByNumber(ctx, seriesID, number)
	if err != nil {
		return library.Ref{}, err
	}
	return sn.Ref(), nil
}

// SearchSeason implements POST /series/{id}/seasons/{number}/search.
func (h *Handler) SearchSeason(ctx context.Context, p gen.SearchSeasonParams) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := h.seasonRef(ctx, p.ID, p.Number)
	if err != nil {
		return nil, err
	}
	if err := h.Wanted.Enqueue(ctx, ref, "manual"); err != nil {
		return nil, err
	}
	return &gen.QueuedCount{Count: 1}, nil
}

// interactive runs an interactive search for any ref and converts the report.
func (h *Handler) interactive(ctx context.Context, ref library.Ref, userID int64) (*gen.InteractiveSearch, error) {
	ev, err := h.Wanted.Interactive(ctx, ref, userID)
	if err != nil {
		return nil, err
	}
	rep := ev.Report
	out := &gen.InteractiveSearch{Kind: gen.MediaKind(rep.Kind), ProfileName: rep.ProfileName, Accepted: rep.Accepted, Pending: rep.Pending,
		Rejected: rep.Rejected, Decisions: make([]gen.InteractiveDecision, 0, len(rep.Decisions)),
		DurationMs: ev.Search.Duration.Milliseconds(), Indexers: make([]gen.SearchIndexerResult, 0, len(ev.Search.Indexers))}
	for i := range rep.Decisions {
		d := rep.Decisions[i]
		r, _ := ev.Release(&d)
		d.Candidate.DownloadURL = "" // links stay on the server (D-016)
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

func (h *Handler) manualGrab(ctx context.Context, ref library.Ref, req *gen.ManualGrabRequest) (*gen.QueueItem, error) {
	g := wanted.ManualGrab{IndexerID: req.IndexerId, DownloadToken: req.DownloadToken.Or(""), MagnetURL: req.MagnetUrl.Or(""),
		Title: req.Title, Protocol: string(req.Protocol), GUID: req.GUID.Or("")}
	if t, ok := req.PublishDate.Get(); ok {
		g.PublishedAt = &t
	}
	if g.DownloadToken == "" && g.MagnetURL == "" {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: "downloadToken or magnetUrl is required"}
	}
	d, err := h.Wanted.Grab(ctx, ref, g)
	if err != nil {
		return nil, err
	}
	out := toQueueItem(downloads.QueueItem{Download: d})
	h.linkItems(ctx, []*gen.QueueItem{&out})
	return &out, nil
}

// InteractiveSearchSeason implements GET /series/{id}/seasons/{number}/releases.
func (h *Handler) InteractiveSearchSeason(ctx context.Context, p gen.InteractiveSearchSeasonParams) (*gen.InteractiveSearch, error) {
	pr, err := manager(ctx)
	if err != nil {
		return nil, err
	}
	ref, err := h.seasonRef(ctx, p.ID, p.Number)
	if err != nil {
		return nil, err
	}
	return h.interactive(ctx, ref, pr.User.ID)
}

// GrabSeasonRelease implements POST /series/{id}/seasons/{number}/releases.
func (h *Handler) GrabSeasonRelease(ctx context.Context, req *gen.ManualGrabRequest, p gen.GrabSeasonReleaseParams) (*gen.QueueItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := h.seasonRef(ctx, p.ID, p.Number)
	if err != nil {
		return nil, err
	}
	return h.manualGrab(ctx, ref, req)
}

// ListWantedEpisodes implements GET /episodes/wanted.
func (h *Handler) ListWantedEpisodes(ctx context.Context, p gen.ListWantedEpisodesParams) (*gen.WantedEpisodeList, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, total, err := h.Series.WantedEpisodes(ctx, string(p.Kind.Or(gen.ListWantedEpisodesKindMissing)), p.Offset.Or(0), p.Limit.Or(100))
	if err != nil {
		return nil, err
	}
	out := &gen.WantedEpisodeList{Total: total, Items: make([]gen.WantedEpisodeItem, 0, len(list))}
	for _, w := range list {
		item, err := convert[gen.WantedEpisodeItem](toEpisode(w.EpisodeView))
		if err != nil {
			return nil, err
		}
		item.SeriesTitle, item.Poster, item.SeriesType = w.SeriesTitle, w.Poster, gen.SeriesType(w.SeriesType)
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// MonitorEpisodes implements POST /episodes/monitor.
func (h *Handler) MonitorEpisodes(ctx context.Context, req *gen.MonitorEpisodesRequest) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	n, err := h.Series.SetEpisodesMonitored(ctx, req.EpisodeIds, req.Monitored)
	if err != nil {
		return nil, err
	}
	return &gen.QueuedCount{Count: n}, nil
}

// SearchEpisodes implements POST /episodes/search.
func (h *Handler) SearchEpisodes(ctx context.Context, req *gen.EpisodeIdsRequest) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	n := 0
	for _, id := range req.EpisodeIds {
		if _, err := h.Series.Episode(ctx, id); err != nil {
			continue
		}
		if err := h.Wanted.Enqueue(ctx, library.EpisodeRef(id), "manual"); err != nil {
			return nil, err
		}
		n++
	}
	return &gen.QueuedCount{Count: n}, nil
}

func (h *Handler) episodeOut(ctx context.Context, id int64) (*gen.Episode, error) {
	e, err := h.Series.Episode(ctx, id)
	if err != nil {
		return nil, err
	}
	list, err := h.Series.EpisodeViews(ctx, e.SeriesID)
	if err != nil {
		return nil, err
	}
	for _, ev := range list {
		if ev.ID == id {
			out := toEpisode(ev)
			return &out, nil
		}
	}
	return nil, series.ErrEpisodeNotFound
}

// GetEpisode implements GET /episodes/{id}.
func (h *Handler) GetEpisode(ctx context.Context, p gen.GetEpisodeParams) (*gen.Episode, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	return h.episodeOut(ctx, p.ID)
}

// UpdateEpisode implements PATCH /episodes/{id}.
func (h *Handler) UpdateEpisode(ctx context.Context, req *gen.EpisodeUpdate, p gen.UpdateEpisodeParams) (*gen.Episode, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	e, err := h.Series.Episode(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if v, ok := req.Monitored.Get(); ok {
		if _, err := h.Series.SetEpisodesMonitored(ctx, []int64{p.ID}, v); err != nil {
			return nil, err
		}
	}
	if req.ClearScene.Or(false) || req.SceneSeason.Set || req.SceneEpisode.Set || req.SceneAbsolute.Set {
		m := series.SceneMapping{Season: e.SceneSeason, Episode: e.SceneEpisode, Absolute: e.SceneAbsolute}
		if req.ClearScene.Or(false) {
			m = series.SceneMapping{}
		}
		set := func(o gen.OptInt, dst **int) {
			if v, ok := o.Get(); ok {
				*dst = &v
			}
		}
		set(req.SceneSeason, &m.Season)
		set(req.SceneEpisode, &m.Episode)
		set(req.SceneAbsolute, &m.Absolute)
		if _, err := h.Series.SetSceneMapping(ctx, p.ID, m); err != nil {
			return nil, err
		}
	}
	return h.episodeOut(ctx, p.ID)
}

// InteractiveSearchEpisode implements GET /episodes/{id}/releases.
func (h *Handler) InteractiveSearchEpisode(ctx context.Context, p gen.InteractiveSearchEpisodeParams) (*gen.InteractiveSearch, error) {
	pr, err := manager(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.Series.Episode(ctx, p.ID); err != nil {
		return nil, err
	}
	return h.interactive(ctx, library.EpisodeRef(p.ID), pr.User.ID)
}

// GrabEpisodeRelease implements POST /episodes/{id}/releases.
func (h *Handler) GrabEpisodeRelease(ctx context.Context, req *gen.ManualGrabRequest, p gen.GrabEpisodeReleaseParams) (*gen.QueueItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, err := h.Series.Episode(ctx, p.ID); err != nil {
		return nil, err
	}
	return h.manualGrab(ctx, library.EpisodeRef(p.ID), req)
}

func (h *Handler) seriesNaming(ctx context.Context) (*gen.SeriesNamingSettings, error) {
	var (
		out gen.SeriesNamingSettings
		err error
	)
	str := func(k settings.Key[string], dst *string) {
		if err == nil {
			*dst, err = settings.Get(ctx, h.Settings, k)
		}
	}
	str(series.SeriesFolderFormat, &out.SeriesFolderFormat)
	str(series.SeasonFolderFormat, &out.SeasonFolderFormat)
	str(series.SpecialsFolderFormat, &out.SpecialsFolderFormat)
	str(series.StandardEpisodeFormat, &out.StandardEpisodeFormat)
	str(series.DailyEpisodeFormat, &out.DailyEpisodeFormat)
	str(series.AnimeEpisodeFormat, &out.AnimeEpisodeFormat)
	var style, colon string
	str(series.MultiEpisodeStyle, &style)
	str(library.ColonReplacement, &colon)
	if err != nil {
		return nil, err
	}
	out.MultiEpisodeStyle, out.ColonReplacement = gen.MultiEpisodeStyle(style), gen.NewOptColonReplacement(gen.ColonReplacement(colon))
	if out.RenameEpisodes, err = settings.Get(ctx, h.Settings, series.RenameEpisodes); err != nil {
		return nil, err
	}
	if out.SeasonPackThreshold, err = settings.Get(ctx, h.Settings, series.SeasonPackThreshold); err != nil {
		return nil, err
	}
	if out.SearchOnAddDefault, err = settings.Get(ctx, h.Settings, series.SearchOnAddDefault); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSeriesNaming implements GET /settings/mediamanagement/series.
func (h *Handler) GetSeriesNaming(ctx context.Context) (*gen.SeriesNamingSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.seriesNaming(ctx)
}

type namingField struct {
	name, tmpl string
	tokens     []naming.Token
}

func seriesFields(req *gen.SeriesNamingSettings) []namingField {
	return []namingField{
		{"seriesFolderFormat", req.SeriesFolderFormat, naming.SeriesFolderTokens},
		{"seasonFolderFormat", req.SeasonFolderFormat, naming.SeasonFolderTokens},
		{"specialsFolderFormat", req.SpecialsFolderFormat, naming.SeasonFolderTokens},
		{"standardEpisodeFormat", req.StandardEpisodeFormat, naming.EpisodeTokens},
		{"dailyEpisodeFormat", req.DailyEpisodeFormat, naming.EpisodeTokens},
		{"animeEpisodeFormat", req.AnimeEpisodeFormat, naming.EpisodeTokens},
	}
}

// specialsOK lets the specials folder be plain text ("Specials").
func validateField(f namingField) error {
	if f.name == "specialsFolderFormat" && !strings.ContainsAny(f.tmpl, "{}") && strings.TrimSpace(f.tmpl) != "" && !strings.ContainsAny(f.tmpl, `/\`) {
		return nil
	}
	return naming.Validate(f.tmpl, f.tokens)
}

// UpdateSeriesNaming implements PUT /settings/mediamanagement/series.
func (h *Handler) UpdateSeriesNaming(ctx context.Context, req *gen.SeriesNamingSettings) (*gen.SeriesNamingSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	for _, f := range seriesFields(req) {
		if err := validateField(f); err != nil {
			return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_template", Message: f.name + ": " + err.Error()}
		}
	}
	err := h.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, f := range []func() error{
			func() error { return settings.Set(ctx, h.Settings, tx, series.RenameEpisodes, req.RenameEpisodes) },
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.SeriesFolderFormat, req.SeriesFolderFormat)
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.SeasonFolderFormat, req.SeasonFolderFormat)
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.SpecialsFolderFormat, req.SpecialsFolderFormat)
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.StandardEpisodeFormat, req.StandardEpisodeFormat)
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.DailyEpisodeFormat, req.DailyEpisodeFormat)
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.AnimeEpisodeFormat, req.AnimeEpisodeFormat)
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.MultiEpisodeStyle, string(req.MultiEpisodeStyle))
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.SeasonPackThreshold, req.SeasonPackThreshold)
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, series.SearchOnAddDefault, req.SearchOnAddDefault)
			},
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
	return h.seriesNaming(ctx)
}

// PreviewSeriesNaming implements POST /settings/mediamanagement/series/preview.
func (h *Handler) PreviewSeriesNaming(ctx context.Context, req *gen.SeriesNamingSettings) (*gen.SeriesNamingPreview, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	opts := naming.Options{Colon: string(req.ColonReplacement.Or(gen.ColonReplacementDash))}
	style := string(req.MultiEpisodeStyle)
	out := &gen.SeriesNamingPreview{Errors: gen.SeriesNamingPreviewErrors{}, Tokens: make([]gen.NamingToken, 0, len(naming.EpisodeTokens))}
	for _, t := range naming.EpisodeTokens {
		out.Tokens = append(out.Tokens, gen.NamingToken{Name: t.Name, Example: t.Example})
	}
	std := naming.SeriesInfo{Title: "The Expanse", Year: 2015, TVDbID: 280619, TMDbID: 63639, IMDbID: "tt3230854"}
	file := func(name string) *naming.FileInfo {
		r := parser.Parse(name)
		return &naming.FileInfo{Release: &r, Quality: quality.Identify(&r, quality.Series), CustomFormats: []string{"x265"}}
	}
	stdFile := file("The.Expanse.S01E01.1080p.AMZN.WEB-DL.DDP5.1.H.265-GROUP")
	render := func(field, tmpl string, v naming.Values, ext bool) string {
		for _, f := range seriesFields(req) {
			if f.name == field {
				if err := validateField(f); err != nil {
					out.Errors[field] = err.Error()
					return ""
				}
			}
		}
		s, err := naming.Render(tmpl, v, opts)
		if err != nil {
			out.Errors[field] = err.Error()
			return ""
		}
		if ext {
			return naming.WithExtension(s, ".mkv")
		}
		return s
	}
	out.SeriesFolder = render("seriesFolderFormat", req.SeriesFolderFormat, naming.SeriesValues(std), false)
	out.SeasonFolder = render("seasonFolderFormat", req.SeasonFolderFormat, naming.SeasonValues(std, 1), false)
	out.SpecialsFolder = render("specialsFolderFormat", req.SpecialsFolderFormat, naming.SeasonValues(std, 0), false)
	ep1 := naming.EpisodeInfo{Season: 1, Number: 1, Absolute: 1, AirDate: "2015-12-14", Title: "Dulcinea"}
	ep2 := naming.EpisodeInfo{Season: 1, Number: 2, Absolute: 2, AirDate: "2015-12-15", Title: "The Big Empty"}
	out.Standard = render("standardEpisodeFormat", req.StandardEpisodeFormat, naming.EpisodeValues(std, []naming.EpisodeInfo{ep1}, stdFile, style), true)
	out.MultiEpisode = render("standardEpisodeFormat", req.StandardEpisodeFormat,
		naming.EpisodeValues(std, []naming.EpisodeInfo{ep1, ep2}, stdFile, style), true)
	daily := naming.SeriesInfo{Title: "The Daily Show", Year: 1996}
	out.Daily = render("dailyEpisodeFormat", req.DailyEpisodeFormat, naming.EpisodeValues(daily,
		[]naming.EpisodeInfo{{Season: 2024, Number: 52, AirDate: "2024-03-14", Title: "Guest Night"}},
		file("The.Daily.Show.2024.03.14.720p.WEB.h264-GROUP"), style), true)
	anime := naming.SeriesInfo{Title: "Frieren", Year: 2023}
	out.Anime = render("animeEpisodeFormat", req.AnimeEpisodeFormat, naming.EpisodeValues(anime,
		[]naming.EpisodeInfo{{Season: 1, Number: 5, Absolute: 5, Title: "Phantoms of the Dead"}},
		file("[SubsPlease] Frieren - 05 (1080p) [ABCD1234]"), style), true)
	if out.Errors == nil {
		out.Errors = gen.SeriesNamingPreviewErrors{}
	}
	return out, nil
}

// TVDBValidator checks a TVDB key (and PIN) before it is stored.
type TVDBValidator interface {
	ValidateKey(ctx context.Context, key, pin string) error
}
