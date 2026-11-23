package api

import (
	"context"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/discover"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/requests"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

func (h *Handler) instanceName(ctx context.Context) (string, error) {
	return settings.Get(ctx, h.Settings, settings.InstanceName)
}

func toDiscoverItem(it discover.Item) gen.DiscoverItem {
	out := gen.DiscoverItem{MediaType: gen.DiscoverMediaType(it.MediaType), TmdbId: optPos(it.TMDbID), Mbid: optStr(it.MBID),
		ArtistMbid: optStr(it.ArtistMBID), Title: it.Title, Subtitle: optStr(it.Subtitle), Year: it.Year, Date: optStr(it.Date),
		Overview: optStr(it.Overview), Poster: optStr(it.Poster), Backdrop: optStr(it.Backdrop), Status: gen.DiscoverStatus(it.Status),
		Status4k: gen.DiscoverStatus(it.Status4K), RequestId: optPos(it.RequestID), StreameroUrl: optStr(it.StreameroURL)}
	if out.Status == "" {
		out.Status = gen.DiscoverStatusNone
	}
	if out.Status4k == "" {
		out.Status4k = gen.DiscoverStatusNone
	}
	if it.Rating > 0 {
		out.Rating = gen.NewOptFloat64(it.Rating)
	}
	return out
}

func toDiscoverItems(list []discover.Item) []gen.DiscoverItem {
	out := make([]gen.DiscoverItem, 0, len(list))
	for _, it := range list {
		out = append(out, toDiscoverItem(it))
	}
	return out
}

// GetDiscover implements GET /discover.
func (h *Handler) GetDiscover(ctx context.Context) (*gen.DiscoverHome, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	rows, ready, err := h.Discover.Home(ctx, p)
	if err != nil {
		return nil, err
	}
	out := &gen.DiscoverHome{Rows: make([]gen.DiscoverRow, 0, len(rows)), MetadataReady: ready, MusicReady: h.Discover.MB != nil}
	for _, r := range rows {
		out.Rows = append(out.Rows, gen.DiscoverRow{Key: gen.DiscoverListKey(r.Key), Items: toDiscoverItems(r.Items)})
	}
	mine, _, err := h.Requests.List(ctx, requests.Filter{UserID: p.User.ID, Limit: 20})
	if err != nil {
		return nil, err
	}
	h.Requests.WithProgress(ctx, mine)
	out.Requests = toRequests(p, mine)
	return out, nil
}

// GetDiscoverList implements GET /discover/lists/{list}.
func (h *Handler) GetDiscoverList(ctx context.Context, params gen.GetDiscoverListParams) (*gen.DiscoverPage, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	pg, err := h.Discover.List(ctx, p, string(params.List), params.ID.Or(0), params.Page.Or(1))
	if err != nil {
		return nil, err
	}
	return &gen.DiscoverPage{Items: toDiscoverItems(pg.Items), Page: pg.Page, TotalPages: pg.TotalPages}, nil
}

// GetDiscoverGenres implements GET /discover/genres.
func (h *Handler) GetDiscoverGenres(ctx context.Context) (*gen.DiscoverFacets, error) {
	if _, err := principal(ctx); err != nil {
		return nil, err
	}
	f, err := h.Discover.Facets(ctx)
	if err != nil {
		return nil, err
	}
	conv := func(list []metadata.Genre) []gen.Facet {
		out := make([]gen.Facet, 0, len(list))
		for _, g := range list {
			out = append(out, gen.Facet{ID: g.ID, Name: g.Name})
		}
		return out
	}
	return &gen.DiscoverFacets{MovieGenres: conv(f.MovieGenres), SeriesGenres: conv(f.SeriesGenres), Studios: conv(f.Studios),
		Networks: conv(f.Networks)}, nil
}

// DiscoverSearch implements GET /discover/search.
func (h *Handler) DiscoverSearch(ctx context.Context, params gen.DiscoverSearchParams) (*gen.DiscoverPage, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	pg, err := h.Discover.Search(ctx, p, params.Q, string(params.Type.Or(gen.DiscoverSearchTypeAll)))
	if err != nil {
		return nil, err
	}
	return &gen.DiscoverPage{Items: toDiscoverItems(pg.Items), Page: pg.Page, TotalPages: pg.TotalPages}, nil
}

func ability(p auth.Principal, kind requests.Kind, item discover.Item) gen.RequestAbility {
	return gen.RequestAbility{CanRequest: requests.CanRequest(p, kind, false), CanRequest4k: requests.CanRequest(p, kind, true),
		AutoApprove: requests.AutoApproves(p, kind, false), AutoApprove4k: requests.CanRequest(p, kind, true) && requests.AutoApproves(p, kind, true),
		CanSetOptions:  gen.NewOptBool(requests.CanManage(p)),
		CanReportIssue: gen.NewOptBool(kind != requests.KindArtist && (item.Status == discover.Available || item.Status == discover.Partial))}
}

func (h *Handler) toTitle(p auth.Principal, kind requests.Kind, t discover.Title) *gen.TitleDetail {
	out := &gen.TitleDetail{Item: toDiscoverItem(t.Item), OriginalTitle: optStr(t.OriginalTitle), Tagline: optStr(t.Tagline),
		Genres: t.Genres, Certification: optStr(t.Certification), ReleaseDate: optStr(t.ReleaseDate), Studio: optStr(t.Studio),
		Network: optStr(t.Network), SeriesStatus: optStr(t.SeriesStatus), TrailerUrl: optStr(t.TrailerURL), Cast: []gen.CastMember{},
		Seasons: []gen.SeasonAvailability{}, Albums: toDiscoverItems(t.Albums), Requests: toRequests(p, t.Requests),
		Ability: ability(p, kind, t.Item)}
	if out.Genres == nil {
		out.Genres = []string{}
	}
	if t.Runtime > 0 {
		out.Runtime = gen.NewOptInt(t.Runtime)
	}
	for _, c := range t.Cast {
		out.Cast = append(out.Cast, gen.CastMember{PersonId: optPos(c.PersonID), Name: c.Name, Character: optStr(c.Character),
			Profile: optStr(c.Profile)})
	}
	for _, s := range t.Seasons {
		out.Seasons = append(out.Seasons, gen.SeasonAvailability{Number: s.Number, Title: s.Title, EpisodeCount: s.EpisodeCount,
			AirDate: optStr(s.AirDate), Poster: optStr(s.Poster), Status: gen.DiscoverStatus(s.Status), Status4k: gen.DiscoverStatus(s.Status4K)})
	}
	if p.Can(auth.PermManageLibrary) {
		out.LibraryRef = optStr(t.LibraryRef)
	}
	return out
}

// GetDiscoverMovie implements GET /discover/movies/{tmdbId}.
func (h *Handler) GetDiscoverMovie(ctx context.Context, params gen.GetDiscoverMovieParams) (*gen.TitleDetail, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	t, err := h.Discover.Movie(ctx, p, params.TmdbId)
	if err != nil {
		return nil, err
	}
	return h.toTitle(p, requests.KindMovie, t), nil
}

// GetDiscoverSeries implements GET /discover/series/{tmdbId}.
func (h *Handler) GetDiscoverSeries(ctx context.Context, params gen.GetDiscoverSeriesParams) (*gen.TitleDetail, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	t, err := h.Discover.Series(ctx, p, params.TmdbId)
	if err != nil {
		return nil, err
	}
	return h.toTitle(p, requests.KindSeries, t), nil
}

// GetDiscoverArtist implements GET /discover/artists/{mbid}.
func (h *Handler) GetDiscoverArtist(ctx context.Context, params gen.GetDiscoverArtistParams) (*gen.TitleDetail, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	t, err := h.Discover.Artist(ctx, p, params.Mbid)
	if err != nil {
		return nil, err
	}
	out := h.toTitle(p, requests.KindArtist, t)
	return out, nil
}

// GetDiscoverPerson implements GET /discover/people/{tmdbId}.
func (h *Handler) GetDiscoverPerson(ctx context.Context, params gen.GetDiscoverPersonParams) (*gen.PersonDetail, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	per, items, err := h.Discover.Person(ctx, p, params.TmdbId)
	if err != nil {
		return nil, err
	}
	return &gen.PersonDetail{TmdbId: per.TMDbID, Name: per.Name, Biography: optStr(per.Biography), Profile: optStr(per.Profile),
		Birthday: optStr(per.Birthday), Department: optStr(per.Department), KnownFor: toDiscoverItems(items)}, nil
}
