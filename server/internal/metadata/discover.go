package metadata

import "context"

// Discover media types.
const (
	MediaMovie  = "movie"
	MediaSeries = "series"
	MediaPerson = "person"
)

// DiscoverResult is one title (or person) in a discover list or search.
type DiscoverResult struct {
	MediaType     string  `json:"mediaType"` // movie, series, person
	TMDbID        int64   `json:"tmdbId"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"originalTitle"`
	Year          int     `json:"year"`
	Date          string  `json:"date"` // release or first air date
	Overview      string  `json:"overview"`
	Poster        string  `json:"poster"` // a person's profile picture
	Backdrop      string  `json:"backdrop"`
	Rating        float64 `json:"rating"`
	Popularity    float64 `json:"popularity"`
	Department    string  `json:"department"` // people: Acting, Directing, …
}

// DiscoverPage is one page of a list.
type DiscoverPage struct {
	Items      []DiscoverResult `json:"items"`
	Page       int              `json:"page"`
	TotalPages int              `json:"totalPages"`
}

// Genre is a genre (or a studio or network) to browse by.
type Genre struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Credit is a cast member.
type Credit struct {
	PersonID  int64  `json:"personId"`
	Name      string `json:"name"`
	Character string `json:"character"`
	Profile   string `json:"profile"`
}

// TitleExtras are what a title page shows beyond the library record: the
// tagline, the cast and a trailer.
type TitleExtras struct {
	Tagline string   `json:"tagline"`
	Cast    []Credit `json:"cast"`
	// TrailerKey is a YouTube video id ("" = none).
	TrailerKey string `json:"trailerKey"`
}

// Person is someone in front of or behind the camera.
type Person struct {
	TMDbID     int64            `json:"tmdbId"`
	Name       string           `json:"name"`
	Biography  string           `json:"biography"`
	Profile    string           `json:"profile"`
	Birthday   string           `json:"birthday"`
	Department string           `json:"department"`
	KnownFor   []DiscoverResult `json:"knownFor"`
}

// Discover list keys a provider answers (the API's DiscoverListKey, without
// recently_added, which comes from the library).
const (
	ListTrending       = "trending"
	ListTrendingMovies = "trending_movies"
	ListTrendingSeries = "trending_series"
	ListPopularMovies  = "popular_movies"
	ListPopularSeries  = "popular_series"
	ListUpcomingMovies = "upcoming_movies"
	ListNowPlaying     = "now_playing"
	ListTopRatedMovies = "top_rated_movies"
	ListTopRatedSeries = "top_rated_series"
	ListAiringSeries   = "airing_series"
	ListGenreMovies    = "genre_movies"
	ListGenreSeries    = "genre_series"
	ListStudio         = "studio"
	ListNetwork        = "network"
)

// DiscoverProvider answers the request pages (P8): lists to browse, search
// across kinds, cast and trailers, and people.
type DiscoverProvider interface {
	// DiscoverList returns a page of a list; id is the genre, studio or network for
	// the lists that need one.
	DiscoverList(ctx context.Context, list string, id int64, page int) (DiscoverPage, error)
	// Genres returns the movie or series genres (mediaType movie or series).
	Genres(ctx context.Context, mediaType string) ([]Genre, error)
	// SearchAll searches movies, series and people (mediaType "" = all three).
	SearchAll(ctx context.Context, query, mediaType string) (DiscoverPage, error)
	// Extras returns the cast and trailer of a movie or series.
	Extras(ctx context.Context, mediaType string, id int64) (TitleExtras, error)
	Person(ctx context.Context, id int64) (Person, error)
}

type languageKey struct{}

// WithLanguage asks the provider to answer one call in another language than
// the configured one (a client that sends its own, such as the compatible
// request API). An invalid tag is ignored.
func WithLanguage(ctx context.Context, lang string) context.Context {
	if !validLanguage(lang) {
		return ctx
	}
	return context.WithValue(ctx, languageKey{}, lang)
}

// LanguageFrom returns the language set with WithLanguage ("" = none).
func LanguageFrom(ctx context.Context) string {
	l, _ := ctx.Value(languageKey{}).(string)
	return l
}

// validLanguage accepts "hr" and "hr-HR" style tags.
func validLanguage(l string) bool {
	if len(l) != 2 && len(l) != 5 {
		return false
	}
	for i, c := range l {
		switch {
		case i < 2 && c >= 'a' && c <= 'z':
		case i == 2 && c == '-':
		case i > 2 && c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return true
}
