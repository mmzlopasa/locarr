package discover

import (
	"context"
	"errors"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/requests"
)

// SeasonState is one season of a series page.
type SeasonState struct {
	Number       int
	Title        string
	EpisodeCount int
	AirDate      string
	Poster       string
	Status       Status
	Status4K     Status
}

// Title is a title page.
type Title struct {
	Item          Item
	OriginalTitle string
	Tagline       string
	Runtime       int
	Genres        []string
	Certification string
	ReleaseDate   string
	Studio        string
	Network       string
	SeriesStatus  string
	TrailerURL    string
	Cast          []metadata.Credit
	Seasons       []SeasonState
	Albums        []Item
	// Requests are the open requests for it (own ones unless p manages requests).
	Requests []requests.Request
	// LibraryRef is the library item ("" = not in the library).
	LibraryRef string
}

func trailer(key string) string {
	if key == "" {
		return ""
	}
	return "https://www.youtube.com/watch?v=" + key
}

func (s *Service) titleRequests(ctx context.Context, p auth.Principal, f requests.Filter) ([]requests.Request, error) {
	f.Limit = 50
	if !requests.CanManage(p) {
		f.UserID = p.User.ID
	}
	list, _, err := s.Requests.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := []requests.Request{}
	for _, r := range list {
		if r.Status.Open() {
			out = append(out, r)
		}
	}
	s.Requests.WithProgress(ctx, out)
	return out, nil
}

// Movie returns a movie's page.
func (s *Service) Movie(ctx context.Context, p auth.Principal, id int64) (Title, error) {
	m, err := s.Movies.Movie(ctx, id)
	if err != nil {
		return Title{}, err
	}
	ex, err := s.TMDb.Extras(ctx, metadata.MediaMovie, id)
	if err != nil && !errors.Is(err, metadata.ErrNotFound) {
		return Title{}, err
	}
	t := Title{Item: Item{MediaType: Movie, TMDbID: m.TMDbID, Title: m.Title, Year: m.Year, Date: m.ReleaseDate, Overview: m.Overview,
		Poster: m.Poster, Backdrop: m.Backdrop, Rating: m.Rating}, OriginalTitle: m.OriginalTitle, Tagline: ex.Tagline, Runtime: m.Runtime,
		Genres: m.Genres, Certification: m.Certification, ReleaseDate: m.ReleaseDate, Studio: m.Studio, TrailerURL: trailer(ex.TrailerKey),
		Cast: ex.Cast}
	items := []Item{t.Item}
	if err := s.Annotate(ctx, p, items); err != nil {
		return t, err
	}
	t.Item = items[0]
	if st, ok, err := s.Library.Lookup(ctx, requests.KindMovie, id, ""); err == nil && ok {
		t.LibraryRef = st.Ref
	}
	t.Requests, err = s.titleRequests(ctx, p, requests.Filter{Kind: requests.KindMovie, TMDbID: id})
	return t, err
}

// Series returns a series' page with each season's state.
func (s *Service) Series(ctx context.Context, p auth.Principal, id int64) (Title, error) {
	v, err := s.TV.Series(ctx, id)
	if err != nil {
		return Title{}, err
	}
	ex, err := s.TMDb.Extras(ctx, metadata.MediaSeries, id)
	if err != nil && !errors.Is(err, metadata.ErrNotFound) {
		return Title{}, err
	}
	t := Title{Item: Item{MediaType: Series, TMDbID: id, Title: v.Title, Year: v.Year, Date: v.FirstAired, Overview: v.Overview,
		Poster: v.Poster, Backdrop: v.Backdrop, Rating: v.Rating}, OriginalTitle: v.OriginalTitle, Tagline: ex.Tagline, Runtime: v.Runtime,
		Genres: v.Genres, Certification: v.Certification, ReleaseDate: v.FirstAired, Network: v.Network, SeriesStatus: v.Status,
		TrailerURL: trailer(ex.TrailerKey), Cast: ex.Cast}
	items := []Item{t.Item}
	if err := s.Annotate(ctx, p, items); err != nil {
		return t, err
	}
	t.Item = items[0]
	lib, inLib, err := s.Library.Lookup(ctx, requests.KindSeries, id, "")
	if err != nil {
		return t, err
	}
	if inLib {
		t.LibraryRef = lib.Ref
	}
	// Seasons asked for on each track (everyone's: a season someone else
	// asked for is "requested" for everybody).
	all, _, err := s.Requests.List(ctx, requests.Filter{Kind: requests.KindSeries, TMDbID: id, Limit: 200})
	if err != nil {
		return t, err
	}
	asked := map[bool]map[int]Status{false: {}, true: {}}
	for _, r := range all {
		if !r.Status.Open() || (r.ItemRef != "" && r.Status == requests.StatusAvailable && !r.Is4K) {
			continue
		}
		for _, sn := range r.Seasons {
			st := requestStatus(sn.Status)
			if r.Status == requests.StatusPending {
				st = Pending
			}
			if cur, ok := asked[r.Is4K][sn.Number]; !ok || cur == Processing {
				asked[r.Is4K][sn.Number] = st
			}
		}
	}
	for _, sn := range v.Seasons {
		if sn.Number == 0 {
			continue
		}
		ss := SeasonState{Number: sn.Number, Title: sn.Title, EpisodeCount: len(sn.Episodes), AirDate: sn.AirDate, Poster: sn.Poster,
			Status: None, Status4K: None}
		libSt := libState{}
		if inLib {
			if st, ok := lib.Seasons[sn.Number]; ok {
				switch st {
				case requests.StatusAvailable:
					libSt.status = Available
				case requests.StatusPartial:
					libSt.status = Partial
				default:
					libSt.status = Processing
				}
			}
		}
		ss.Status = combine(libSt, asked[false][sn.Number])
		if st, ok := asked[true][sn.Number]; ok {
			ss.Status4K = st
		}
		t.Seasons = append(t.Seasons, ss)
	}
	t.Requests, err = s.titleRequests(ctx, p, requests.Filter{Kind: requests.KindSeries, TMDbID: id})
	return t, err
}

// Person returns a person and what they are known for.
func (s *Service) Person(ctx context.Context, p auth.Principal, id int64) (metadata.Person, []Item, error) {
	per, err := s.TMDb.Person(ctx, id)
	if err != nil {
		return per, nil, err
	}
	items := fromResults(per.KnownFor)
	return per, items, s.Annotate(ctx, p, items)
}

// Artist returns an artist page with their albums and EPs.
func (s *Service) Artist(ctx context.Context, p auth.Principal, mbid string) (Title, error) {
	if s.MB == nil {
		return Title{}, metadata.ErrUnavailable
	}
	mbid = strings.ToLower(mbid)
	a, err := s.MB.Artist(ctx, mbid)
	if err != nil {
		return Title{}, err
	}
	groups, err := s.MB.ReleaseGroups(ctx, mbid)
	if err != nil {
		return Title{}, err
	}
	t := Title{Item: Item{MediaType: Artist, MBID: a.MBID, ArtistMBID: a.MBID, Title: a.Name, Subtitle: a.Disambiguation}, Genres: a.Genres,
		Albums: []Item{}}
	for _, g := range groups {
		if (g.PrimaryType == "album" || g.PrimaryType == "ep") && len(g.SecondaryTypes) == 0 {
			t.Albums = append(t.Albums, fromAlbum(g, a.MBID, a.Name))
		}
	}
	sortAlbums(t.Albums)
	all := append([]Item{t.Item}, t.Albums...)
	if err := s.Annotate(ctx, p, all); err != nil {
		return t, err
	}
	t.Item, t.Albums = all[0], all[1:]
	if len(t.Albums) > 0 {
		t.Item.Poster = t.Albums[0].Poster
	}
	if st, ok, err := s.Library.Lookup(ctx, requests.KindArtist, 0, mbid); err == nil && ok {
		t.LibraryRef = st.Ref
	}
	t.Requests, err = s.titleRequests(ctx, p, requests.Filter{MBID: mbid})
	return t, err
}
