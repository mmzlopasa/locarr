// Package discover is what people who request titles browse (P8): lists
// from TMDb (trending, popular, upcoming, now playing, top rated, by genre,
// studio and network), search across movies, series, people and music,
// title pages, and everything marked with its state in the library and in
// requests (D-061).
package discover

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
	"github.com/mmzlopasa/locarr/server/internal/requests"
)

// Status is a title's state for the person looking at it.
type Status string

// Statuses (DiscoverStatus in the API).
const (
	None       Status = "none"
	Pending    Status = "pending"
	Processing Status = "processing"
	Partial    Status = "partial"
	Available  Status = "available"
	InLibrary  Status = "in_library"
)

// Media types.
const (
	Movie  = "movie"
	Series = "series"
	Person = "person"
	Artist = "artist"
	Album  = "album"
)

// RecentlyAdded is the list built from the library.
const RecentlyAdded = "recently_added"

// Item is a title (or person) with its state.
type Item struct {
	MediaType  string
	TMDbID     int64
	MBID       string
	ArtistMBID string
	Title      string
	Subtitle   string
	Year       int
	Date       string
	Overview   string
	Poster     string
	Backdrop   string
	Rating     float64
	Status     Status
	Status4K   Status
	// RequestID is the caller's open request for it (0 = none).
	RequestID int64
	// StreameroURL links to the title in Streamero when it has it.
	StreameroURL string
}

// Page is a page of items.
type Page struct {
	Items      []Item
	Page       int
	TotalPages int
}

// Row is one row of the home page.
type Row struct {
	Key   string
	Items []Item
}

// Service builds the discover pages.
type Service struct {
	DB       *db.DB
	TMDb     metadata.DiscoverProvider
	Movies   metadata.MovieProvider
	TV       metadata.SeriesProvider
	MB       *musicbrainz.Client
	Requests *requests.Service
	Library  requests.Library
	// Streamero marks titles Streamero already has (nil = none).
	Streamero LinkSource
}

// LinkSource returns links to titles a media server has, by position.
type LinkSource interface {
	Links(ctx context.Context, titles []streamero.Title) (map[int]string, error)
}

// HomeLists are the provider rows of the home page, in order.
var HomeLists = []string{metadata.ListTrending, metadata.ListPopularMovies, metadata.ListPopularSeries, metadata.ListUpcomingMovies,
	metadata.ListNowPlaying, metadata.ListTopRatedMovies, metadata.ListTopRatedSeries}

// Studios and Networks are offered to browse by (TMDb company and network ids).
var (
	Studios = []metadata.Genre{{ID: 420, Name: "Marvel Studios"}, {ID: 3, Name: "Pixar"}, {ID: 2, Name: "Walt Disney Pictures"},
		{ID: 174, Name: "Warner Bros. Pictures"}, {ID: 33, Name: "Universal Pictures"}, {ID: 4, Name: "Paramount"},
		{ID: 41077, Name: "A24"}, {ID: 10342, Name: "Studio Ghibli"}, {ID: 1632, Name: "Lionsgate"}}
	Networks = []metadata.Genre{{ID: 213, Name: "Netflix"}, {ID: 49, Name: "HBO"}, {ID: 2552, Name: "Apple TV+"}, {ID: 1024, Name: "Prime Video"},
		{ID: 2739, Name: "Disney+"}, {ID: 453, Name: "Hulu"}, {ID: 174, Name: "AMC"}, {ID: 88, Name: "FX"}, {ID: 4, Name: "BBC One"}}
)

// Home returns the home rows; ready is false when TMDb has no key yet.
func (s *Service) Home(ctx context.Context, p auth.Principal) ([]Row, bool, error) {
	rows := make([]Row, len(HomeLists))
	errs := make([]error, len(HomeLists))
	var wg sync.WaitGroup
	for i, key := range HomeLists {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pg, err := s.TMDb.DiscoverList(ctx, key, 0, 1)
			rows[i], errs[i] = Row{Key: key, Items: fromResults(pg.Items)}, err
		}()
	}
	wg.Wait()
	ready := true
	var out []Row
	for i, err := range errs {
		switch {
		case errors.Is(err, metadata.ErrNoKey), errors.Is(err, metadata.ErrInvalidKey):
			ready = false
		case err != nil:
			// One list failing (TMDb hiccup) leaves the others.
		case len(rows[i].Items) > 0:
			out = append(out, rows[i])
		}
	}
	recent, err := s.recent(ctx, 20)
	if err != nil {
		return nil, ready, err
	}
	if len(recent) > 0 {
		out = append([]Row{{Key: RecentlyAdded, Items: recent}}, out...)
	}
	var all []Item
	for _, r := range out {
		all = append(all, r.Items...)
	}
	if err := s.Annotate(ctx, p, all); err != nil {
		return nil, ready, err
	}
	// Annotate filled the copies; put them back by position.
	n := 0
	for i := range out {
		for j := range out[i].Items {
			out[i].Items[j] = all[n]
			n++
		}
	}
	return out, ready, nil
}

// List returns a page of one list.
func (s *Service) List(ctx context.Context, p auth.Principal, key string, id int64, page int) (Page, error) {
	if key == RecentlyAdded {
		items, err := s.recent(ctx, 60)
		if err != nil {
			return Page{}, err
		}
		return Page{Items: items, Page: 1, TotalPages: 1}, s.Annotate(ctx, p, items)
	}
	pg, err := s.TMDb.DiscoverList(ctx, key, id, page)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: fromResults(pg.Items), Page: pg.Page, TotalPages: min(pg.TotalPages, 50)}
	return out, s.Annotate(ctx, p, out.Items)
}

// Facets are genres, studios and networks.
type Facets struct {
	MovieGenres, SeriesGenres, Studios, Networks []metadata.Genre
}

// Facets returns what to browse by.
func (s *Service) Facets(ctx context.Context) (Facets, error) {
	mg, err := s.TMDb.Genres(ctx, metadata.MediaMovie)
	if err != nil {
		return Facets{}, err
	}
	sg, err := s.TMDb.Genres(ctx, metadata.MediaSeries)
	if err != nil {
		return Facets{}, err
	}
	return Facets{MovieGenres: mg, SeriesGenres: sg, Studios: Studios, Networks: Networks}, nil
}

// SearchTMDb searches movies, series and people without music.
const SearchTMDb = "tmdb"

// Search searches; kind is all, movie, series, person, music or SearchTMDb.
func (s *Service) Search(ctx context.Context, p auth.Principal, q, kind string) (Page, error) {
	var items []Item
	switch kind {
	case "music":
		if s.MB == nil {
			break
		}
		artists, err := s.MB.SearchArtists(ctx, q)
		if err != nil {
			return Page{}, err
		}
		items = append(items, fromArtists(artists, 8)...)
		albums, err := s.MB.SearchAlbums(ctx, q)
		if err != nil {
			return Page{}, err
		}
		for _, a := range albums {
			items = append(items, fromAlbum(a.ReleaseGroup, a.ArtistMBID, a.ArtistName))
		}
	default:
		mt := ""
		switch kind {
		case Movie:
			mt = metadata.MediaMovie
		case Series:
			mt = metadata.MediaSeries
		case Person:
			mt = metadata.MediaPerson
		}
		pg, err := s.TMDb.SearchAll(ctx, q, mt)
		if err != nil {
			return Page{}, err
		}
		items = fromResults(pg.Items)
		if kind == "all" || kind == "" {
			if s.MB != nil {
				// Music is a bonus here: MusicBrainz being slow or down does not fail the search.
				if artists, err := s.MB.SearchArtists(ctx, q); err == nil {
					items = append(items, fromArtists(artists, 5)...)
				}
			}
		}
	}
	return Page{Items: items, Page: 1, TotalPages: 1}, s.Annotate(ctx, p, items)
}

func fromResults(list []metadata.DiscoverResult) []Item {
	out := make([]Item, 0, len(list))
	for _, r := range list {
		mt := r.MediaType
		if mt == metadata.MediaSeries {
			mt = Series
		}
		it := Item{MediaType: mt, TMDbID: r.TMDbID, Title: r.Title, Year: r.Year, Date: r.Date, Overview: r.Overview, Poster: r.Poster,
			Backdrop: r.Backdrop, Rating: r.Rating}
		if mt == Person {
			it.Subtitle = r.Department
		}
		out = append(out, it)
	}
	return out
}

func fromArtists(list []metadata.ArtistSummary, n int) []Item {
	var out []Item
	for _, a := range list {
		if len(out) == n {
			break
		}
		out = append(out, Item{MediaType: Artist, MBID: a.MBID, ArtistMBID: a.MBID, Title: a.Name, Subtitle: a.Disambiguation})
	}
	return out
}

func fromAlbum(g metadata.ReleaseGroup, artistMBID, artistName string) Item {
	y := 0
	if len(g.FirstReleaseDate) >= 4 {
		for _, c := range g.FirstReleaseDate[:4] {
			y = y*10 + int(c-'0')
		}
	}
	return Item{MediaType: Album, MBID: g.MBID, ArtistMBID: artistMBID, Title: g.Title, Subtitle: artistName, Year: y,
		Date: g.FirstReleaseDate, Poster: musicbrainz.CoverName(g.MBID)}
}

// recent returns the titles that got files most recently.
func (s *Service) recent(ctx context.Context, limit int) ([]Item, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind, tmdb_id, mbid, artist_mbid, title, year, poster, at FROM (
		SELECT 'movie' AS kind, m.tmdb_id, '' AS mbid, '' AS artist_mbid, i.title, i.year, m.poster, MAX(f.added_at) AS at
			FROM media_files f JOIN movies m ON m.item_id = f.item_id JOIN media_items i ON i.id = f.item_id GROUP BY f.item_id
		UNION ALL
		SELECT 'series', s.tmdb_id, '', '', i.title, i.year, s.poster, MAX(f.added_at)
			FROM media_files f JOIN series s ON s.item_id = f.item_id JOIN media_items i ON i.id = f.item_id WHERE s.tmdb_id <> 0 GROUP BY f.item_id
		UNION ALL
		SELECT 'album', 0, al.mbid, ar.mbid, al.title, CAST(substr(al.release_date, 1, 4) AS INTEGER), '', MAX(f.added_at)
			FROM album_files af JOIN media_files f ON f.id = af.file_id JOIN albums al ON al.id = af.album_id
			JOIN artists ar ON ar.item_id = al.artist_id GROUP BY af.album_id
	) ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var (
			it Item
			at int64
		)
		if err := rows.Scan(&it.MediaType, &it.TMDbID, &it.MBID, &it.ArtistMBID, &it.Title, &it.Year, &it.Poster, &at); err != nil {
			return nil, err
		}
		if it.MediaType == Album {
			it.Poster = musicbrainz.CoverName(it.MBID)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// --- state ---

type key struct {
	kind string
	id   string
}

func itemKey(it *Item) (key, bool) {
	switch it.MediaType {
	case Movie, Series:
		return key{it.MediaType, i64(it.TMDbID)}, it.TMDbID > 0
	case Album, Artist:
		return key{it.MediaType, strings.ToLower(it.MBID)}, it.MBID != ""
	}
	return key{}, false
}

func i64(n int64) string { return strconv.FormatInt(n, 10) }

type libState struct {
	status Status
}

// Annotate sets Status, Status4K and RequestID on items in place.
func (s *Service) Annotate(ctx context.Context, p auth.Principal, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	ids := map[string][]any{}
	for i := range items {
		items[i].Status, items[i].Status4K = None, None
		if k, ok := itemKey(&items[i]); ok {
			ids[k.kind] = append(ids[k.kind], k.id)
		}
	}
	lib, err := s.libraryStates(ctx, ids)
	if err != nil {
		return err
	}
	reqs, err := s.requestStates(ctx, p, ids)
	if err != nil {
		return err
	}
	for i := range items {
		k, ok := itemKey(&items[i])
		if !ok {
			continue
		}
		rs := reqs[k]
		items[i].Status = combine(lib[k], rs.status)
		items[i].Status4K = combine(libState{}, rs.status4k)
		items[i].RequestID = rs.mine
	}
	if s.Streamero != nil {
		var titles []streamero.Title
		var pos []int
		for i := range items {
			if (items[i].MediaType == Movie || items[i].MediaType == Series) && items[i].TMDbID > 0 {
				titles = append(titles, streamero.Title{Kind: items[i].MediaType, TMDbID: items[i].TMDbID})
				pos = append(pos, i)
			}
		}
		if links, err := s.Streamero.Links(ctx, titles); err == nil {
			for j, link := range links {
				items[pos[j]].StreameroURL = link
			}
		}
	}
	return nil
}

// combine merges the library's and the requests' view: what the library has
// wins, then the request, then "in the library".
func combine(l libState, req Status) Status {
	switch {
	case l.status == Available:
		return Available
	case l.status == Partial:
		return Partial
	case req != "" && req != None:
		return req
	case l.status != "":
		return InLibrary
	}
	return None
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func (s *Service) libraryStates(ctx context.Context, ids map[string][]any) (map[key]libState, error) {
	out := map[key]libState{}
	now := db.Millis(time.Now())
	queries := []struct {
		kind string
		sql  string
	}{
		{Movie, `SELECT m.tmdb_id, (SELECT COUNT(*) FROM media_files f WHERE f.item_id = m.item_id), 1 FROM movies m WHERE m.tmdb_id IN (%s)`},
		{Series, `SELECT s.tmdb_id,
			(SELECT COUNT(*) FROM episodes e WHERE e.series_id = s.item_id AND e.season_number > 0 AND e.removed = 0 AND e.file_id IS NOT NULL),
			(SELECT COUNT(*) FROM episodes e WHERE e.series_id = s.item_id AND e.season_number > 0 AND e.removed = 0
				AND e.air_date_utc IS NOT NULL AND e.air_date_utc <= ` + i64(now) + `)
			FROM series s WHERE s.tmdb_id IN (%s)`},
		{Album, `SELECT a.mbid, (SELECT COUNT(*) FROM album_files f WHERE f.album_id = a.id),
			(SELECT COUNT(*) FROM tracks t WHERE t.release_id = a.release_id) FROM albums a WHERE a.mbid IN (%s)`},
		{Artist, `SELECT ar.mbid, (SELECT COUNT(*) FROM media_files f WHERE f.item_id = ar.item_id), 0 FROM artists ar WHERE ar.mbid IN (%s)`},
	}
	for _, q := range queries {
		list := ids[q.kind]
		if len(list) == 0 {
			continue
		}
		rows, err := s.DB.R.QueryContext(ctx, strings.Replace(q.sql, "%s", placeholders(len(list)), 1), list...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var (
				id          string
				files, want int
			)
			if err := rows.Scan(&id, &files, &want); err != nil {
				_ = rows.Close()
				return nil, err
			}
			st := Processing // in the library, nothing yet
			switch {
			case q.kind == Movie && files > 0:
				st = Available
			case q.kind == Artist && files > 0:
				st = Partial
			case want > 0 && files >= want:
				st = Available
			case files > 0:
				st = Partial
			}
			out[key{q.kind, strings.ToLower(id)}] = libState{status: st}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type reqState struct {
	status, status4k Status
	mine             int64
}

func requestStatus(st requests.Status) Status {
	switch st {
	case requests.StatusPending:
		return Pending
	case requests.StatusPartial:
		return Partial
	case requests.StatusAvailable:
		return Available
	}
	return Processing
}

func (s *Service) requestStates(ctx context.Context, p auth.Principal, ids map[string][]any) (map[key]reqState, error) {
	out := map[key]reqState{}
	var (
		conds []string
		args  []any
	)
	if l := ids[Movie]; len(l) > 0 {
		conds, args = append(conds, `(kind = 'movie' AND tmdb_id IN (`+placeholders(len(l))+`))`), append(args, l...)
	}
	if l := ids[Series]; len(l) > 0 {
		conds, args = append(conds, `(kind = 'series' AND tmdb_id IN (`+placeholders(len(l))+`))`), append(args, l...)
	}
	if l := ids[Album]; len(l) > 0 {
		conds, args = append(conds, `(kind = 'album' AND mbid IN (`+placeholders(len(l))+`))`), append(args, l...)
	}
	if l := ids[Artist]; len(l) > 0 {
		conds, args = append(conds, `(kind = 'artist' AND mbid IN (`+placeholders(len(l))+`))`), append(args, l...)
	}
	if len(conds) == 0 {
		return out, nil
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, kind, tmdb_id, mbid, is_4k, status, requested_by, item_ref FROM requests
		WHERE status NOT IN ('declined', 'cancelled') AND (`+strings.Join(conds, " OR ")+`) ORDER BY id`, args...) // #nosec G202 -- only placeholders are joined
	if err != nil {
		return nil, err
	}
	type row struct {
		id, tmdb, by    int64
		kind, mbid, ref string
		is4k            bool
		st              string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.kind, &r.tmdb, &r.mbid, &r.is4k, &r.st, &r.by, &r.ref); err != nil {
			_ = rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rank := map[Status]int{None: 0, "": 0, Processing: 1, Pending: 2, Partial: 3, Available: 4}
	for _, r := range list {
		k := key{r.kind, strings.ToLower(r.mbid)}
		if r.kind == Movie || r.kind == Series {
			k.id = i64(r.tmdb)
		}
		st := requestStatus(requests.Status(r.st))
		if st == Available && !r.is4k {
			continue // the library's own state says whether it is still there
		}
		if r.ref != "" && st == Available && s.Library != nil && !s.Library.Exists(ctx, r.ref) {
			continue
		}
		cur := out[k]
		if r.is4k {
			if rank[st] > rank[cur.status4k] {
				cur.status4k = st
			}
		} else if rank[st] > rank[cur.status] {
			cur.status = st
		}
		if r.by == p.User.ID && st != Available && cur.mine == 0 {
			cur.mine = r.id
		}
		out[k] = cur
	}
	return out, nil
}

// sortAlbums puts the newest first.
func sortAlbums(items []Item) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Date > items[j].Date })
}
