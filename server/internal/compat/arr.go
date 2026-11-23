package compat

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// tmdbImageBase serves the stored TMDb image files.
const tmdbImageBase = "https://image.tmdb.org/t/p/w500/"

type image struct {
	CoverType string `json:"coverType"`
	RemoteURL string `json:"remoteUrl"`
}

// posterImages returns the poster as a public image address, or none.
func posterImages(poster string) []image {
	switch {
	case strings.HasPrefix(poster, "https://"):
		return []image{{CoverType: "poster", RemoteURL: poster}}
	case tmdbPath(poster) != "":
		return []image{{CoverType: "poster", RemoteURL: tmdbImageBase + strings.TrimPrefix(poster, "/")}}
	}
	return []image{}
}

// calendarRange reads start and end (RFC 3339 or a date); without them it is
// today and the next two days. Longer ranges than 400 days are refused.
func calendarRange(r *http.Request) (time.Time, time.Time, bool) {
	parse := func(s string) (time.Time, bool) {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC(), true
		}
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t.UTC(), true
		}
		return time.Time{}, false
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	from, to := today, today.AddDate(0, 0, 2)
	if s := r.URL.Query().Get("start"); s != "" {
		t, ok := parse(s)
		if !ok {
			return from, to, false
		}
		from = t
	}
	if s := r.URL.Query().Get("end"); s != "" {
		t, ok := parse(s)
		if !ok {
			return from, to, false
		}
		to = t
	}
	return from, to, !to.Before(from) && to.Sub(from) <= 400*24*time.Hour
}

func boolParam(r *http.Request, name string) bool {
	v, _ := strconv.ParseBool(r.URL.Query().Get(name))
	return v
}

func day(t time.Time) string { return t.Format("2006-01-02") }

// dateTime turns a stored YYYY-MM-DD into midnight UTC (nil = none).
func dateTime(d string) *time.Time {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return nil
	}
	return &t
}

// movie is a library movie as the Radarr API describes it.
type movie struct {
	ID              int64      `json:"id"`
	Title           string     `json:"title"`
	Year            int        `json:"year"`
	TitleSlug       string     `json:"titleSlug"`
	TMDbID          int64      `json:"tmdbId"`
	IMDbID          string     `json:"imdbId,omitempty"`
	Overview        string     `json:"overview"`
	InCinemas       *time.Time `json:"inCinemas,omitempty"`
	DigitalRelease  *time.Time `json:"digitalRelease,omitempty"`
	PhysicalRelease *time.Time `json:"physicalRelease,omitempty"`
	HasFile         bool       `json:"hasFile"`
	Monitored       bool       `json:"monitored"`
	Images          []image    `json:"images"`
}

const movieColumns = `i.id, i.title, i.year, m.tmdb_id, m.imdb_id, m.overview, m.in_cinemas, m.digital_release, m.physical_release,
	EXISTS (SELECT 1 FROM media_files f WHERE f.item_id = i.id), i.monitored, m.poster`

func scanMovies(rows *sql.Rows) ([]movie, error) {
	defer rows.Close()
	out := []movie{}
	for rows.Next() {
		var (
			m                  movie
			cinemas, dig, phys string
			poster             string
		)
		if err := rows.Scan(&m.ID, &m.Title, &m.Year, &m.TMDbID, &m.IMDbID, &m.Overview, &cinemas, &dig, &phys, &m.HasFile, &m.Monitored,
			&poster); err != nil {
			return nil, err
		}
		m.TitleSlug = strconv.FormatInt(m.ID, 10)
		m.InCinemas, m.DigitalRelease, m.PhysicalRelease = dateTime(cinemas), dateTime(dig), dateTime(phys)
		m.Images = posterImages(poster)
		out = append(out, m)
	}
	return out, rows.Err()
}

// GET /api/v3/calendar?start=&end=&unmonitored=
func (h *Handler) radarrCalendar(w http.ResponseWriter, r *http.Request) {
	from, to, ok := calendarRange(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "start and end must be dates, end after start and at most 400 days later")
		return
	}
	a, b := day(from), day(to)
	q := `SELECT ` + movieColumns + ` FROM movies m JOIN media_items i ON i.id = m.item_id WHERE m.item_id IN (
		SELECT item_id FROM movies WHERE in_cinemas <> '' AND in_cinemas BETWEEN ? AND ?
		UNION SELECT item_id FROM movies WHERE digital_release <> '' AND digital_release BETWEEN ? AND ?
		UNION SELECT item_id FROM movies WHERE physical_release <> '' AND physical_release BETWEEN ? AND ?)`
	if !boolParam(r, "unmonitored") {
		q += ` AND i.monitored = 1`
	}
	rows, err := h.DB.R.QueryContext(r.Context(), q+` ORDER BY i.sort_title, i.id`, a, b, a, b, a, b)
	if err != nil {
		h.serverError(w, err)
		return
	}
	list, err := scanMovies(rows)
	if err != nil {
		h.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/v3/movie[?tmdbId=]
func (h *Handler) radarrMovies(w http.ResponseWriter, r *http.Request) {
	q, args := `SELECT `+movieColumns+` FROM movies m JOIN media_items i ON i.id = m.item_id`, []any{}
	if s := r.URL.Query().Get("tmdbId"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "validation", "tmdbId must be a number")
			return
		}
		q, args = q+` WHERE m.tmdb_id = ?`, append(args, id)
	}
	rows, err := h.DB.R.QueryContext(r.Context(), q+` ORDER BY i.sort_title, i.id`, args...)
	if err != nil {
		h.serverError(w, err)
		return
	}
	list, err := scanMovies(rows)
	if err != nil {
		h.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// series is a library series as the Sonarr API describes it.
type series struct {
	ID        int64   `json:"id"`
	Title     string  `json:"title"`
	Year      int     `json:"year"`
	TitleSlug string  `json:"titleSlug"`
	TVDbID    int64   `json:"tvdbId"`
	TMDbID    int64   `json:"tmdbId"`
	IMDbID    string  `json:"imdbId,omitempty"`
	Network   string  `json:"network,omitempty"`
	Monitored bool    `json:"monitored"`
	Images    []image `json:"images"`
}

type episode struct {
	ID            int64      `json:"id"`
	SeriesID      int64      `json:"seriesId"`
	SeasonNumber  int        `json:"seasonNumber"`
	EpisodeNumber int        `json:"episodeNumber"`
	Title         string     `json:"title"`
	Overview      string     `json:"overview,omitempty"`
	AirDate       string     `json:"airDate,omitempty"`
	AirDateUTC    *time.Time `json:"airDateUtc,omitempty"`
	HasFile       bool       `json:"hasFile"`
	Monitored     bool       `json:"monitored"`
	Series        *series    `json:"series,omitempty"`
}

const seriesColumns = `i.id, i.title, i.year, s.tvdb_id, s.tmdb_id, s.imdb_id, s.network, i.monitored, s.poster`

func scanSeries(row interface{ Scan(...any) error }, extra ...any) (series, error) {
	var (
		s      series
		poster string
	)
	if err := row.Scan(append([]any{&s.ID, &s.Title, &s.Year, &s.TVDbID, &s.TMDbID, &s.IMDbID, &s.Network, &s.Monitored, &poster},
		extra...)...); err != nil {
		return s, err
	}
	s.TitleSlug = strconv.FormatInt(s.ID, 10)
	s.Images = posterImages(poster)
	return s, nil
}

// GET /api/v3/calendar?start=&end=&includeSeries=&unmonitored=
func (h *Handler) sonarrCalendar(w http.ResponseWriter, r *http.Request) {
	from, to, ok := calendarRange(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "start and end must be dates, end after start and at most 400 days later")
		return
	}
	// The local air date can be a day off the UTC instant either way.
	q := `SELECT ` + seriesColumns + `, e.id, e.season_number, e.episode_number, e.title, e.overview, e.air_date, e.air_date_utc,
		e.file_id IS NOT NULL, e.monitored
		FROM episodes e JOIN media_items i ON i.id = e.series_id JOIN series s ON s.item_id = i.id
		WHERE e.air_date <> '' AND e.air_date BETWEEN ? AND ? AND e.removed = 0`
	if !boolParam(r, "unmonitored") {
		q += ` AND e.monitored = 1 AND i.monitored = 1`
	}
	rows, err := h.DB.R.QueryContext(r.Context(), q+` ORDER BY e.air_date, e.air_date_utc, i.sort_title, e.season_number, e.episode_number`,
		day(from.AddDate(0, 0, -1)), day(to.AddDate(0, 0, 1)))
	if err != nil {
		h.serverError(w, err)
		return
	}
	defer rows.Close()
	withSeries := boolParam(r, "includeSeries")
	out := []episode{}
	for rows.Next() {
		var (
			e   episode
			air sql.NullInt64
		)
		s, err := scanSeries(rows, &e.ID, &e.SeasonNumber, &e.EpisodeNumber, &e.Title, &e.Overview, &e.AirDate, &air, &e.HasFile, &e.Monitored)
		if err != nil {
			h.serverError(w, err)
			return
		}
		e.SeriesID = s.ID
		e.AirDateUTC = db.NullTime(air)
		if e.AirDateUTC == nil {
			e.AirDateUTC = dateTime(e.AirDate)
		}
		if e.AirDateUTC == nil || e.AirDateUTC.Before(from) || e.AirDateUTC.After(to) {
			continue
		}
		if withSeries {
			e.Series = &s
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		h.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/v3/series[?tvdbId=]
func (h *Handler) sonarrSeries(w http.ResponseWriter, r *http.Request) {
	q, args := `SELECT `+seriesColumns+` FROM series s JOIN media_items i ON i.id = s.item_id`, []any{}
	if v := r.URL.Query().Get("tvdbId"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "validation", "tvdbId must be a number")
			return
		}
		q, args = q+` WHERE s.tvdb_id = ?`, append(args, id)
	}
	rows, err := h.DB.R.QueryContext(r.Context(), q+` ORDER BY i.sort_title, i.id`, args...)
	if err != nil {
		h.serverError(w, err)
		return
	}
	defer rows.Close()
	out := []series{}
	for rows.Next() {
		s, err := scanSeries(rows)
		if err != nil {
			h.serverError(w, err)
			return
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		h.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
