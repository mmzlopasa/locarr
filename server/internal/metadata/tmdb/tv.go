package tmdb

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

var _ metadata.SeriesProvider = (*Client)(nil)

// Name implements metadata.SeriesProvider.
func (c *Client) Name() string { return providerName }

type tvResult struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	OriginalName  string   `json:"original_name"`
	FirstAirDate  string   `json:"first_air_date"`
	Overview      string   `json:"overview"`
	PosterPath    string   `json:"poster_path"`
	BackdropPath  string   `json:"backdrop_path"`
	Popularity    float64  `json:"popularity"`
	VoteAverage   float64  `json:"vote_average"`
	OriginCountry []string `json:"origin_country"`
}

func (t tvResult) summary() metadata.SeriesSummary {
	return metadata.SeriesSummary{TMDbID: t.ID, Title: t.Name, OriginalTitle: t.OriginalName, Year: year(t.FirstAirDate),
		FirstAired: t.FirstAirDate, Overview: t.Overview, Poster: file(t.PosterPath), Backdrop: file(t.BackdropPath),
		Popularity: t.Popularity, Rating: t.VoteAverage}
}

// SearchSeries implements metadata.SeriesProvider.
func (c *Client) SearchSeries(ctx context.Context, query string, y int) ([]metadata.SeriesSummary, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}, "language": {c.lang(ctx)}, "page": {"1"}}
	if y > 0 {
		q.Set("first_air_date_year", strconv.Itoa(y))
	}
	var res struct {
		Results []tvResult `json:"results"`
	}
	if err := c.get(ctx, "/3/search/tv", q, metadata.TTLSearch, &res); err != nil {
		return nil, err
	}
	out := make([]metadata.SeriesSummary, 0, len(res.Results))
	for _, r := range res.Results {
		out = append(out, r.summary())
	}
	return out, nil
}

type tvDetails struct {
	tvResult
	OriginalLanguage string    `json:"original_language"`
	Status           string    `json:"status"`
	LastAirDate      string    `json:"last_air_date"`
	EpisodeRunTime   []int     `json:"episode_run_time"`
	VoteCount        int       `json:"vote_count"`
	Genres           []nameObj `json:"genres"`
	Networks         []struct {
		Name          string `json:"name"`
		OriginCountry string `json:"origin_country"`
	} `json:"networks"`
	Seasons []struct {
		SeasonNumber int    `json:"season_number"`
		Name         string `json:"name"`
		Overview     string `json:"overview"`
		PosterPath   string `json:"poster_path"`
		AirDate      string `json:"air_date"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"seasons"`
	ExternalIDs struct {
		IMDbID string `json:"imdb_id"`
		TVDbID int64  `json:"tvdb_id"`
	} `json:"external_ids"`
	AlternativeTitles struct {
		Results []struct {
			Title string `json:"title"`
		} `json:"results"`
	} `json:"alternative_titles"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

type nameObj struct {
	Name string `json:"name"`
}

type seasonDetails struct {
	Episodes []struct {
		ID            int64  `json:"id"`
		EpisodeNumber int    `json:"episode_number"`
		SeasonNumber  int    `json:"season_number"`
		Name          string `json:"name"`
		Overview      string `json:"overview"`
		AirDate       string `json:"air_date"`
		Runtime       int    `json:"runtime"`
	} `json:"episodes"`
}

// Series implements metadata.SeriesProvider: the series and, season by
// season, every episode (TMDb has no absolute numbers; D-040).
func (c *Client) Series(ctx context.Context, id int64) (metadata.Series, error) {
	lang := c.lang(ctx)
	var d tvDetails
	q := url.Values{"language": {lang}, "append_to_response": {"external_ids,alternative_titles,content_ratings"}}
	if err := c.get(ctx, "/3/tv/"+strconv.FormatInt(id, 10), q, metadata.TTLSeries, &d); err != nil {
		return metadata.Series{}, err
	}
	s := metadata.Series{SeriesSummary: d.summary(), Source: providerName, IMDbID: d.ExternalIDs.IMDbID,
		OriginalLanguage: d.OriginalLanguage, LastAired: d.LastAirDate, Votes: d.VoteCount}
	s.TVDbID = d.ExternalIDs.TVDbID
	s.Status = metadata.NormaliseSeriesStatus(d.Status)
	for _, g := range d.Genres {
		s.Genres = append(s.Genres, g.Name)
	}
	if len(d.Networks) > 0 {
		s.Network = d.Networks[0].Name
		s.TimeZone = metadata.CountryZone(d.Networks[0].OriginCountry)
	}
	if s.TimeZone == "" && len(d.OriginCountry) > 0 {
		s.TimeZone = metadata.CountryZone(d.OriginCountry[0])
	}
	if len(d.EpisodeRunTime) > 0 {
		s.Runtime = d.EpisodeRunTime[0]
	}
	seen := map[string]bool{strings.ToLower(s.Title): true}
	for _, t := range d.AlternativeTitles.Results {
		if k := strings.ToLower(t.Title); t.Title != "" && !seen[k] {
			seen[k] = true
			s.AlternateTitles = append(s.AlternateTitles, t.Title)
		}
	}
	if s.OriginalTitle != "" && !seen[strings.ToLower(s.OriginalTitle)] {
		s.AlternateTitles = append(s.AlternateTitles, s.OriginalTitle)
	}
	region := c.region(ctx)
	for _, r := range d.ContentRatings.Results {
		if r.Country == region && r.Rating != "" {
			s.Certification = r.Rating
		}
	}
	for _, sn := range d.Seasons {
		season := metadata.Season{Number: sn.SeasonNumber, Title: sn.Name, Overview: sn.Overview, Poster: file(sn.PosterPath), AirDate: sn.AirDate}
		var sd seasonDetails
		path := "/3/tv/" + strconv.FormatInt(id, 10) + "/season/" + strconv.Itoa(sn.SeasonNumber)
		if err := c.get(ctx, path, url.Values{"language": {lang}}, metadata.TTLSeason, &sd); err != nil {
			return metadata.Series{}, err
		}
		for _, e := range sd.Episodes {
			rt := e.Runtime
			if rt == 0 {
				rt = s.Runtime
			}
			season.Episodes = append(season.Episodes, metadata.Episode{Season: sn.SeasonNumber, Number: e.EpisodeNumber, TMDbID: e.ID,
				Title: e.Name, Overview: e.Overview, AirDate: e.AirDate, Runtime: rt})
		}
		sort.Slice(season.Episodes, func(i, j int) bool { return season.Episodes[i].Number < season.Episodes[j].Number })
		s.Seasons = append(s.Seasons, season)
	}
	sort.Slice(s.Seasons, func(i, j int) bool { return s.Seasons[i].Number < s.Seasons[j].Number })
	return s, nil
}

// FindSeries implements metadata.SeriesProvider (imdb or tvdb → TMDb id).
func (c *Client) FindSeries(ctx context.Context, source, id string) (int64, error) {
	id = strings.TrimSpace(id)
	var ext string
	switch source {
	case "tmdb":
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 {
			return 0, metadata.ErrNotFound
		}
		return n, nil
	case "imdb":
		id = strings.ToLower(id)
		if !strings.HasPrefix(id, "tt") || len(id) < 4 || len(id) > 12 {
			return 0, metadata.ErrNotFound
		}
		ext = "imdb_id"
	case "tvdb":
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return 0, metadata.ErrNotFound
		}
		ext = "tvdb_id"
	default:
		return 0, metadata.ErrNotFound
	}
	var res struct {
		TVResults []tvResult `json:"tv_results"`
	}
	if err := c.get(ctx, "/3/find/"+url.PathEscape(id), url.Values{"external_source": {ext}}, metadata.TTLSeries, &res); err != nil {
		return 0, err
	}
	if len(res.TVResults) == 0 {
		return 0, metadata.ErrNotFound
	}
	return res.TVResults[0].ID, nil
}
