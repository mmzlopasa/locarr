package tmdb

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

var _ metadata.DiscoverProvider = (*Client)(nil)

// result is a list entry of any kind (movie, tv or person), as TMDb's list,
// trending, discover and multi-search endpoints return them.
type result struct {
	ID            int64   `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	ProfilePath   string  `json:"profile_path"`
	BackdropPath  string  `json:"backdrop_path"`
	VoteAverage   float64 `json:"vote_average"`
	Popularity    float64 `json:"popularity"`
	Department    string  `json:"known_for_department"`
	Adult         bool    `json:"adult"`
}

func (r result) discover(defaultType string) metadata.DiscoverResult {
	typ := r.MediaType
	if typ == "" {
		typ = defaultType
	}
	out := metadata.DiscoverResult{TMDbID: r.ID, Overview: r.Overview, Backdrop: file(r.BackdropPath), Rating: r.VoteAverage,
		Popularity: r.Popularity}
	switch typ {
	case "tv":
		out.MediaType, out.Title, out.OriginalTitle, out.Date = metadata.MediaSeries, r.Name, r.OriginalName, r.FirstAirDate
		out.Poster = file(r.PosterPath)
	case "person":
		out.MediaType, out.Title, out.Poster, out.Department = metadata.MediaPerson, r.Name, file(r.ProfilePath), r.Department
	default:
		out.MediaType, out.Title, out.OriginalTitle, out.Date = metadata.MediaMovie, r.Title, r.OriginalTitle, r.ReleaseDate
		out.Poster = file(r.PosterPath)
	}
	out.Year = year(out.Date)
	return out
}

type resultPage struct {
	Page       int      `json:"page"`
	TotalPages int      `json:"total_pages"`
	Results    []result `json:"results"`
}

func (p resultPage) page(defaultType string) metadata.DiscoverPage {
	out := metadata.DiscoverPage{Items: make([]metadata.DiscoverResult, 0, len(p.Results)), Page: p.Page, TotalPages: p.TotalPages}
	for _, r := range p.Results {
		if r.Adult {
			continue
		}
		switch r.MediaType {
		case "", "movie", "tv", "person":
			out.Items = append(out.Items, r.discover(defaultType))
		}
	}
	if out.Page == 0 {
		out.Page = 1
	}
	if out.TotalPages < out.Page {
		out.TotalPages = out.Page
	}
	return out
}

// listPath maps a discover list to its TMDb endpoint, extra parameters and
// the media type of its entries.
func listPath(list string, id int64) (string, url.Values, string, error) {
	q := url.Values{}
	need := func() error {
		if id <= 0 {
			return fmt.Errorf("%w: list %s needs an id", metadata.ErrNotFound, list)
		}
		return nil
	}
	switch list {
	case metadata.ListTrending:
		return "/3/trending/all/week", q, "", nil
	case metadata.ListTrendingMovies:
		return "/3/trending/movie/week", q, "movie", nil
	case metadata.ListTrendingSeries:
		return "/3/trending/tv/week", q, "tv", nil
	case metadata.ListPopularMovies:
		return "/3/movie/popular", q, "movie", nil
	case metadata.ListPopularSeries:
		return "/3/tv/popular", q, "tv", nil
	case metadata.ListUpcomingMovies:
		return "/3/movie/upcoming", q, "movie", nil
	case metadata.ListNowPlaying:
		return "/3/movie/now_playing", q, "movie", nil
	case metadata.ListTopRatedMovies:
		return "/3/movie/top_rated", q, "movie", nil
	case metadata.ListTopRatedSeries:
		return "/3/tv/top_rated", q, "tv", nil
	case metadata.ListAiringSeries:
		return "/3/tv/on_the_air", q, "tv", nil
	case metadata.ListGenreMovies, metadata.ListStudio:
		if err := need(); err != nil {
			return "", nil, "", err
		}
		q.Set("sort_by", "popularity.desc")
		q.Set("include_adult", "false")
		if list == metadata.ListStudio {
			q.Set("with_companies", strconv.FormatInt(id, 10))
		} else {
			q.Set("with_genres", strconv.FormatInt(id, 10))
		}
		return "/3/discover/movie", q, "movie", nil
	case metadata.ListGenreSeries, metadata.ListNetwork:
		if err := need(); err != nil {
			return "", nil, "", err
		}
		q.Set("sort_by", "popularity.desc")
		if list == metadata.ListNetwork {
			q.Set("with_networks", strconv.FormatInt(id, 10))
		} else {
			q.Set("with_genres", strconv.FormatInt(id, 10))
		}
		return "/3/discover/tv", q, "tv", nil
	}
	return "", nil, "", fmt.Errorf("%w: unknown list %q", metadata.ErrNotFound, list)
}

// DiscoverList implements metadata.DiscoverProvider.
func (c *Client) DiscoverList(ctx context.Context, list string, id int64, page int) (metadata.DiscoverPage, error) {
	path, q, typ, err := listPath(list, id)
	if err != nil {
		return metadata.DiscoverPage{}, err
	}
	if page < 1 {
		page = 1
	}
	q.Set("page", strconv.Itoa(page))
	q.Set("language", c.lang(ctx))
	if r := c.region(ctx); r != "" && (list == metadata.ListUpcomingMovies || list == metadata.ListNowPlaying) {
		q.Set("region", r)
	}
	var res resultPage
	if err := c.get(ctx, path, q, metadata.TTLSearch, &res); err != nil {
		return metadata.DiscoverPage{}, err
	}
	return res.page(typ), nil
}

// Genres implements metadata.DiscoverProvider.
func (c *Client) Genres(ctx context.Context, mediaType string) ([]metadata.Genre, error) {
	path := "/3/genre/movie/list"
	if mediaType == metadata.MediaSeries {
		path = "/3/genre/tv/list"
	}
	var res struct {
		Genres []metadata.Genre `json:"genres"`
	}
	if err := c.get(ctx, path, url.Values{"language": {c.lang(ctx)}}, metadata.TTLConfig, &res); err != nil {
		return nil, err
	}
	sort.Slice(res.Genres, func(i, j int) bool { return res.Genres[i].Name < res.Genres[j].Name })
	return res.Genres, nil
}

// SearchAll implements metadata.DiscoverProvider.
func (c *Client) SearchAll(ctx context.Context, query, mediaType string) (metadata.DiscoverPage, error) {
	path, typ := "/3/search/multi", ""
	switch mediaType {
	case metadata.MediaMovie:
		path, typ = "/3/search/movie", "movie"
	case metadata.MediaSeries:
		path, typ = "/3/search/tv", "tv"
	case metadata.MediaPerson:
		path, typ = "/3/search/person", "person"
	}
	q := url.Values{"query": {strings.TrimSpace(query)}, "include_adult": {"false"}, "language": {c.lang(ctx)}, "page": {"1"}}
	var res resultPage
	if err := c.get(ctx, path, q, metadata.TTLSearch, &res); err != nil {
		return metadata.DiscoverPage{}, err
	}
	return res.page(typ), nil
}

type credits struct {
	Cast []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Character   string `json:"character"`
		ProfilePath string `json:"profile_path"`
		Order       int    `json:"order"`
	} `json:"cast"`
}

type videos struct {
	Results []struct {
		Key      string `json:"key"`
		Site     string `json:"site"`
		Type     string `json:"type"`
		Official bool   `json:"official"`
	} `json:"results"`
}

// maxCast is how many cast members a title page shows.
const maxCast = 20

// Extras implements metadata.DiscoverProvider.
func (c *Client) Extras(ctx context.Context, mediaType string, id int64) (metadata.TitleExtras, error) {
	kind := "movie"
	if mediaType == metadata.MediaSeries {
		kind = "tv"
	}
	var res struct {
		Tagline string  `json:"tagline"`
		Credits credits `json:"credits"`
		Videos  videos  `json:"videos"`
	}
	q := url.Values{"language": {c.lang(ctx)}, "append_to_response": {"credits,videos"}}
	if err := c.get(ctx, "/3/"+kind+"/"+strconv.FormatInt(id, 10), q, metadata.TTLMovie, &res); err != nil {
		return metadata.TitleExtras{}, err
	}
	out := metadata.TitleExtras{Tagline: res.Tagline, Cast: []metadata.Credit{}}
	sort.SliceStable(res.Credits.Cast, func(i, j int) bool { return res.Credits.Cast[i].Order < res.Credits.Cast[j].Order })
	for _, cm := range res.Credits.Cast {
		if len(out.Cast) == maxCast {
			break
		}
		out.Cast = append(out.Cast, metadata.Credit{PersonID: cm.ID, Name: cm.Name, Character: cm.Character, Profile: file(cm.ProfilePath)})
	}
	// An official YouTube trailer first, then any trailer, then a teaser.
	best := 0
	for _, v := range res.Videos.Results {
		if !strings.EqualFold(v.Site, "YouTube") || v.Key == "" {
			continue
		}
		score := 0
		switch {
		case v.Type == "Trailer" && v.Official:
			score = 3
		case v.Type == "Trailer":
			score = 2
		case v.Type == "Teaser":
			score = 1
		}
		if score > best {
			best, out.TrailerKey = score, v.Key
		}
	}
	return out, nil
}

// Person implements metadata.DiscoverProvider.
func (c *Client) Person(ctx context.Context, id int64) (metadata.Person, error) {
	var res struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Biography   string `json:"biography"`
		ProfilePath string `json:"profile_path"`
		Birthday    string `json:"birthday"`
		Department  string `json:"known_for_department"`
		Credits     struct {
			Cast []result `json:"cast"`
			Crew []result `json:"crew"`
		} `json:"combined_credits"`
	}
	q := url.Values{"language": {c.lang(ctx)}, "append_to_response": {"combined_credits"}}
	if err := c.get(ctx, "/3/person/"+strconv.FormatInt(id, 10), q, metadata.TTLMovie, &res); err != nil {
		return metadata.Person{}, err
	}
	out := metadata.Person{TMDbID: res.ID, Name: res.Name, Biography: res.Biography, Profile: file(res.ProfilePath), Birthday: res.Birthday,
		Department: res.Department, KnownFor: []metadata.DiscoverResult{}}
	seen := map[string]bool{}
	all := append(append([]result{}, res.Credits.Cast...), res.Credits.Crew...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Popularity > all[j].Popularity })
	for _, r := range all {
		if r.Adult || (r.MediaType != "movie" && r.MediaType != "tv") {
			continue
		}
		key := r.MediaType + strconv.FormatInt(r.ID, 10)
		if seen[key] {
			continue
		}
		seen[key] = true
		out.KnownFor = append(out.KnownFor, r.discover(""))
		if len(out.KnownFor) == 40 {
			break
		}
	}
	return out, nil
}
