package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/fsops"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/library/recycle"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// mapLibraryError maps errors of the library packages (nil = not one of them).
func mapLibraryError(err error) *StatusError {
	if ie, ok := movies.IsImportError(err); ok {
		return &StatusError{Status: http.StatusUnprocessableEntity, Code: ie.Code, Message: ie.Message}
	}
	table := []struct {
		err    error
		status int
		code   string
	}{
		{movies.ErrNotFound, http.StatusNotFound, "not_found"},
		{movies.ErrListNotFound, http.StatusNotFound, "not_found"},
		{movies.ErrExists, http.StatusConflict, "already_exists"},
		{movies.ErrPathTaken, http.StatusConflict, "path_taken"},
		{movies.ErrHasFile, http.StatusConflict, "has_file"},
		{movies.ErrInvalid, http.StatusBadRequest, "validation"},
		{metadata.ErrNoKey, http.StatusConflict, "metadata_key_missing"},
		{metadata.ErrInvalidKey, http.StatusBadGateway, "metadata_key_invalid"},
		{metadata.ErrUnavailable, http.StatusBadGateway, "metadata_unavailable"},
		{metadata.ErrNotFound, http.StatusNotFound, "not_found"},
		{metadata.ErrBadImage, http.StatusBadRequest, "validation"},
		{recycle.ErrNotFound, http.StatusNotFound, "not_found"},
		{recycle.ErrRestoreTaken, http.StatusConflict, "restore_conflict"},
		{recycle.ErrInsideBin, http.StatusBadRequest, "validation"},
		{library.ErrBadRef, http.StatusBadRequest, "validation"},
		{naming.ErrTemplate, http.StatusBadRequest, "invalid_template"},
		{fsops.ErrExists, http.StatusConflict, "destination_exists"},
		{fsops.ErrVerify, http.StatusInternalServerError, "transfer_verify_failed"},
		{wanted.ErrUnknownKind, http.StatusBadRequest, "validation"},
	}
	for _, t := range table {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return nil
}

func optI64(p *int64) gen.OptInt64 {
	if p == nil {
		return gen.OptInt64{}
	}
	return gen.NewOptInt64(*p)
}

func toQuality(id string) gen.Quality {
	q, ok := quality.ByID(id)
	if !ok {
		q = quality.ByIDMust(quality.UnknownVideo)
	}
	out, _ := convert[gen.Quality](q)
	return out
}

func toMovie(it movies.ListItem) gen.Movie {
	m := it.Movie
	out := gen.Movie{ID: m.ID, ItemRef: gen.NewOptString(m.Ref().String()), TmdbId: m.TMDbID, ImdbId: m.IMDbID, Title: m.Title,
		OriginalTitle: m.OriginalTitle, OriginalLanguage: m.OriginalLanguage, AlternateTitles: m.AlternateTitles, Year: m.Year,
		Overview: m.Overview, Runtime: m.Runtime, Status: m.Status, Studio: m.Studio, Genres: m.Genres, Certification: m.Certification,
		Rating: m.Rating, Votes: m.Votes, InCinemas: m.InCinemas, DigitalRelease: m.DigitalRelease, PhysicalRelease: m.PhysicalRelease,
		CollectionId: m.CollectionID, CollectionName: m.CollectionName, Poster: m.Poster, Backdrop: m.Backdrop, Monitored: m.Monitored,
		QualityProfileId: m.QualityProfileID, RootFolderId: optI64(m.RootFolderID), Path: m.Path,
		MinimumAvailability: gen.MinimumAvailability(m.MinimumAvailability), Tags: m.Tags, AddedByListId: optI64(m.AddedByListID),
		AddedAt: m.AddedAt, UpdatedAt: m.UpdatedAt, RefreshedAt: optTime(m.RefreshedAt), Available: it.Available,
		AvailableFrom: optTime(it.AvailableFrom), CutoffMet: it.CutoffMet, LibraryStatus: gen.MovieStatus(it.Status())}
	if f := m.File; f != nil {
		mi, _ := convert[gen.MediaInfo](f.MediaInfo)
		out.File = gen.NewOptMovieFile(gen.MovieFile{ID: f.ID, Path: f.Path, SizeBytes: f.Size, Quality: toQuality(f.Quality),
			Revision: max(f.Revision.Version, 1), CustomFormats: f.CustomFormats, Score: f.Score, ReleaseGroup: f.ReleaseGroup,
			Edition: f.Edition, Languages: f.Languages, MediaInfo: mi, SceneName: f.SceneName, DownloadId: optI64(f.DownloadID), AddedAt: f.AddedAt})
	}
	if d := it.Download; d != nil {
		out.Download = gen.NewOptMovieDownload(gen.MovieDownload{ID: d.ID, Title: d.Title, Progress: d.Progress, State: d.State, Status: d.Status})
	}
	return out
}

func toLookup(r metadata.MovieSummary, id *int64) gen.MovieLookup {
	return gen.MovieLookup{TmdbId: r.TMDbID, Title: r.Title, OriginalTitle: r.OriginalTitle, Year: r.Year, ReleaseDate: r.ReleaseDate,
		Overview: r.Overview, Poster: r.Poster, Backdrop: r.Backdrop, Popularity: r.Popularity, Rating: r.Rating, MovieId: optI64(id)}
}

func (h *Handler) movieOut(ctx context.Context, id int64) (*gen.Movie, error) {
	it, err := h.Movies.Item(ctx, id)
	if err != nil {
		return nil, err
	}
	out := toMovie(it)
	return &out, nil
}

// ListMovies implements GET /movies.
func (h *Handler) ListMovies(ctx context.Context, p gen.ListMoviesParams) (*gen.MovieList, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	f := movies.ListFilter{Status: string(p.Status.Or("")), ProfileID: p.ProfileId.Or(0), Tag: p.Tag.Or(""), Search: p.Q.Or(""),
		Sort: string(p.Sort.Or(gen.ListMoviesSortTitle)), Desc: p.Desc.Or(false), Offset: p.Offset.Or(0), Limit: p.Limit.Or(100)}
	if v, ok := p.Monitored.Get(); ok {
		f.Monitored = &v
	}
	items, total, err := h.Movies.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := &gen.MovieList{Total: total, Items: make([]gen.Movie, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, toMovie(it))
	}
	return out, nil
}

func addInput(o gen.AddOptions) movies.AddInput {
	return movies.AddInput{RootFolderID: o.RootFolderId, QualityProfileID: o.QualityProfileId,
		MinimumAvailability: library.Availability(o.MinimumAvailability.Or(gen.MinimumAvailabilityReleased)), Monitored: o.Monitored.Or(true),
		Tags: o.Tags, SearchOnAdd: o.SearchOnAdd.Or(false)}
}

// AddMovie implements POST /movies.
func (h *Handler) AddMovie(ctx context.Context, req *gen.AddMovieRequest) (*gen.Movie, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	in := addInput(gen.AddOptions{RootFolderId: req.RootFolderId, QualityProfileId: req.QualityProfileId, MinimumAvailability: req.MinimumAvailability,
		Monitored: req.Monitored, Tags: req.Tags, SearchOnAdd: req.SearchOnAdd})
	in.TMDbID, in.Path = req.TmdbId, req.Path.Or("")
	m, err := h.Movies.Add(ctx, in)
	if err != nil {
		return nil, err
	}
	return h.movieOut(ctx, m.ID)
}

// LookupMovies implements GET /movies/lookup.
func (h *Handler) LookupMovies(ctx context.Context, p gen.LookupMoviesParams) ([]gen.MovieLookup, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Movies.Lookup(ctx, p.Term)
	if err != nil {
		return nil, err
	}
	out := make([]gen.MovieLookup, 0, len(list))
	titles := make([]streamero.Title, 0, len(list))
	for _, r := range list {
		out = append(out, toLookup(r.MovieSummary, r.MovieID))
		titles = append(titles, streamero.Title{Kind: "movie", TMDbID: r.TMDbID})
	}
	for i, link := range h.streameroLinks(ctx, titles) {
		out[i].StreameroUrl = gen.NewOptString(link)
	}
	return out, nil
}

// SearchMovies implements POST /movies/search.
func (h *Handler) SearchMovies(ctx context.Context, req *gen.MovieIdsRequest) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	n := 0
	for _, id := range req.MovieIds {
		if _, err := h.Movies.Get(ctx, id); err != nil {
			continue
		}
		if err := h.Wanted.Enqueue(ctx, library.MovieRef(id), "manual"); err != nil {
			return nil, err
		}
		n++
	}
	return &gen.QueuedCount{Count: n}, nil
}

func toRename(list []movies.RenameItem) []gen.RenameItem {
	out := make([]gen.RenameItem, 0, len(list))
	for _, r := range list {
		out = append(out, gen.RenameItem{MovieId: r.MovieID, Title: r.Title, From: r.From, To: r.To})
	}
	return out
}

// RenameMovies implements POST /movies/rename.
func (h *Handler) RenameMovies(ctx context.Context, req *gen.MovieIdsRequest) ([]gen.RenameItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	done, err := h.Movies.Rename(ctx, req.MovieIds)
	if err != nil {
		return nil, err
	}
	return toRename(done), nil
}

// PreviewRenameMovies implements POST /movies/rename/preview.
func (h *Handler) PreviewRenameMovies(ctx context.Context, req *gen.MovieIdsRequest) ([]gen.RenameItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	plan, err := h.Movies.RenamePreview(ctx, req.MovieIds)
	if err != nil {
		return nil, err
	}
	return toRename(plan), nil
}

// GetMovie implements GET /movies/{id}.
func (h *Handler) GetMovie(ctx context.Context, p gen.GetMovieParams) (*gen.Movie, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	return h.movieOut(ctx, p.ID)
}

// UpdateMovie implements PATCH /movies/{id}.
func (h *Handler) UpdateMovie(ctx context.Context, req *gen.MovieUpdate, p gen.UpdateMovieParams) (*gen.Movie, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	var u movies.Update
	if v, ok := req.Monitored.Get(); ok {
		u.Monitored = &v
	}
	if v, ok := req.QualityProfileId.Get(); ok {
		u.QualityProfileID = &v
	}
	if v, ok := req.MinimumAvailability.Get(); ok {
		a := library.Availability(v)
		u.MinimumAvailability = &a
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
	before, err := h.Movies.Get(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	m, err := h.Movies.Edit(ctx, p.ID, u)
	if err != nil {
		return nil, err
	}
	// A change that can make releases acceptable makes the movie due for a search.
	if m.Monitored && (!before.Monitored || before.QualityProfileID != m.QualityProfileID || before.MinimumAvailability != m.MinimumAvailability) {
		h.Wanted.ResetBackoff(ctx, m.Ref())
	}
	return h.movieOut(ctx, m.ID)
}

// DeleteMovie implements DELETE /movies/{id}.
func (h *Handler) DeleteMovie(ctx context.Context, p gen.DeleteMovieParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Movies.Delete(ctx, p.ID, movies.DeleteOptions{DeleteFiles: p.DeleteFiles.Or(false), Exclude: p.Exclude.Or(false)})
}

// RefreshMovie implements POST /movies/{id}/refresh.
func (h *Handler) RefreshMovie(ctx context.Context, p gen.RefreshMovieParams) (*gen.Movie, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, err := h.Movies.Refresh(ctx, p.ID); err != nil {
		return nil, err
	}
	return h.movieOut(ctx, p.ID)
}

// SearchMovie implements POST /movies/{id}/search.
func (h *Handler) SearchMovie(ctx context.Context, p gen.SearchMovieParams) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if _, err := h.Movies.Get(ctx, p.ID); err != nil {
		return nil, err
	}
	if err := h.Wanted.Enqueue(ctx, library.MovieRef(p.ID), "manual"); err != nil {
		return nil, err
	}
	return &gen.QueuedCount{Count: 1}, nil
}

// InteractiveSearchMovie implements GET /movies/{id}/releases.
func (h *Handler) InteractiveSearchMovie(ctx context.Context, p gen.InteractiveSearchMovieParams) (*gen.InteractiveSearch, error) {
	pr, err := manager(ctx)
	if err != nil {
		return nil, err
	}
	ev, err := h.Wanted.Interactive(ctx, library.MovieRef(p.ID), pr.User.ID)
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

// GrabMovieRelease implements POST /movies/{id}/releases.
func (h *Handler) GrabMovieRelease(ctx context.Context, req *gen.ManualGrabRequest, p gen.GrabMovieReleaseParams) (*gen.QueueItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	g := wanted.ManualGrab{IndexerID: req.IndexerId, DownloadToken: req.DownloadToken.Or(""), MagnetURL: req.MagnetUrl.Or(""),
		Title: req.Title, Protocol: string(req.Protocol), GUID: req.GUID.Or("")}
	if t, ok := req.PublishDate.Get(); ok {
		g.PublishedAt = &t
	}
	if g.DownloadToken == "" && g.MagnetURL == "" {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: "downloadToken or magnetUrl is required"}
	}
	d, err := h.Wanted.Grab(ctx, library.MovieRef(p.ID), g)
	if err != nil {
		return nil, err
	}
	out := toQueueItem(downloads.QueueItem{Download: d})
	h.linkItems(ctx, []*gen.QueueItem{&out})
	return &out, nil
}

// linkItems adds the library item a download belongs to.
func (h *Handler) linkItems(ctx context.Context, items []*gen.QueueItem) {
	cache := map[library.Ref]*gen.QueueItemLink{}
	for _, it := range items {
		ref := library.RefFromJSON([]byte(it.ItemRef))
		if ref.Kind == library.RefAlbum {
			if link := h.albumLink(ctx, ref); link != nil {
				it.Item = gen.NewOptQueueItemLink(*link)
			}
			continue
		}
		if ref.IsZero() {
			continue
		}
		link, ok := cache[ref]
		if !ok {
			switch ref.Kind {
			case library.RefMovie:
				if m, err := h.Movies.Get(ctx, ref.ID); err == nil {
					link = &gen.QueueItemLink{Ref: ref.String(), Kind: ref.Kind, ID: ref.ID, Title: m.Title, Year: gen.NewOptInt(m.Year)}
				}
			case library.RefEpisode, library.RefSeason:
				if h.Series != nil {
					if es, err := h.Series.Explain(ctx, ref); err == nil {
						link = &gen.QueueItemLink{Ref: ref.String(), Kind: ref.Kind, ID: ref.ID, Title: es.Title}
						if n := len(library.CoversFromJSON([]byte(it.ItemRef))); n > 1 {
							link.Title = fmt.Sprintf("%s (%d episodes)", es.Title, n)
						}
					}
				}
			}
			cache[ref] = link
		}
		if link != nil {
			it.Item = gen.NewOptQueueItemLink(*link)
		}
	}
}
