package compat

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/discover"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/requests"
)

// Media status codes of the Seerr API (mediaInfo.status, media.status).
const (
	mediaUnknown    = 1
	mediaPending    = 2
	mediaProcessing = 3
	mediaPartial    = 4
	mediaAvailable  = 5
)

// Request status codes of the Seerr API.
const (
	requestPending  = 1
	requestApproved = 2
	requestDeclined = 3
)

// mediaStatus maps a Discover badge to a media status code; it is the same
// state Locarr's own Discover page shows.
func mediaStatus(s discover.Status) int {
	switch s {
	case discover.Available:
		return mediaAvailable
	case discover.Partial:
		return mediaPartial
	case discover.Processing, discover.InLibrary:
		return mediaProcessing
	case discover.Pending:
		return mediaPending
	}
	return mediaUnknown
}

// requestStatus maps a request's state to a request status code.
func requestStatus(s requests.Status) int {
	switch s {
	case requests.StatusPending:
		return requestPending
	case requests.StatusDeclined, requests.StatusCancelled:
		return requestDeclined
	}
	return requestApproved
}

type mediaInfo struct {
	TMDbID int64 `json:"tmdbId"`
	Status int   `json:"status"`
}

type result struct {
	ID           int64      `json:"id"`
	MediaType    string     `json:"mediaType"`
	Title        string     `json:"title,omitempty"`
	Name         string     `json:"name,omitempty"`
	ReleaseDate  string     `json:"releaseDate,omitempty"`
	FirstAirDate string     `json:"firstAirDate,omitempty"`
	Overview     string     `json:"overview,omitempty"`
	PosterPath   string     `json:"posterPath,omitempty"`
	ProfilePath  string     `json:"profilePath,omitempty"`
	BackdropPath string     `json:"backdropPath,omitempty"`
	VoteAverage  float64    `json:"voteAverage"`
	MediaInfo    *mediaInfo `json:"mediaInfo,omitempty"`
}

type page struct {
	Page         int      `json:"page"`
	TotalPages   int      `json:"totalPages"`
	TotalResults int      `json:"totalResults"`
	Results      []result `json:"results"`
}

// tmdbPath turns a stored TMDb image file name into the path form the API uses.
func tmdbPath(file string) string {
	file = strings.TrimPrefix(file, "/")
	if file == "" || strings.ContainsAny(file, "/?#") {
		return ""
	}
	return "/" + file
}

func toResult(it discover.Item) (result, bool) {
	r := result{ID: it.TMDbID, Overview: it.Overview, VoteAverage: it.Rating}
	switch it.MediaType {
	case discover.Movie:
		r.MediaType, r.Title, r.ReleaseDate = "movie", it.Title, it.Date
		r.PosterPath, r.BackdropPath = tmdbPath(it.Poster), tmdbPath(it.Backdrop)
	case discover.Series:
		r.MediaType, r.Name, r.FirstAirDate = "tv", it.Title, it.Date
		r.PosterPath, r.BackdropPath = tmdbPath(it.Poster), tmdbPath(it.Backdrop)
	case discover.Person:
		r.MediaType, r.Name, r.ProfilePath = "person", it.Title, tmdbPath(it.Poster)
		return r, it.TMDbID > 0
	default:
		return r, false
	}
	if code := mediaStatus(it.Status); code != mediaUnknown {
		r.MediaInfo = &mediaInfo{TMDbID: it.TMDbID, Status: code}
	}
	return r, it.TMDbID > 0
}

func toPage(pg discover.Page) page {
	out := page{Page: max(pg.Page, 1), TotalPages: max(pg.TotalPages, 1), Results: []result{}}
	for _, it := range pg.Items {
		if r, ok := toResult(it); ok {
			out.Results = append(out.Results, r)
		}
	}
	// TMDb pages hold 20 results; the exact total is not kept.
	out.TotalResults = (out.TotalPages-1)*20 + len(out.Results)
	return out
}

func pageParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return min(n, 500)
}

// metadataError answers a failure of the metadata provider.
func (h *Handler) metadataError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, metadata.ErrNoKey), errors.Is(err, metadata.ErrInvalidKey):
		writeError(w, http.StatusServiceUnavailable, "metadata_key_missing", "Locarr has no working TMDb key (Settings → Metadata)")
	case errors.Is(err, metadata.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "TMDb does not know this title")
	default:
		h.log().Warn("compat discover", "err", err)
		writeError(w, http.StatusBadGateway, "metadata_unavailable", "TMDb could not be reached")
	}
}

// GET /api/v1/search?query=&page=&language=
func (h *Handler) seerrSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	n := pageParam(r)
	if q == "" || n > 1 {
		// Locarr searches return one page.
		writeJSON(w, http.StatusOK, page{Page: n, TotalPages: 1, Results: []result{}})
		return
	}
	if len(q) > 200 {
		writeError(w, http.StatusBadRequest, "validation", "query is too long")
		return
	}
	ctx := metadata.WithLanguage(r.Context(), r.URL.Query().Get("language"))
	pg, err := h.Discover.Search(ctx, principal(ctx), q, discover.SearchTMDb)
	if err != nil {
		h.metadataError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPage(pg))
}

// GET /api/v1/discover/trending?page=&language=
func (h *Handler) seerrTrending(w http.ResponseWriter, r *http.Request) {
	ctx := metadata.WithLanguage(r.Context(), r.URL.Query().Get("language"))
	pg, err := h.Discover.List(ctx, principal(ctx), metadata.ListTrending, 0, pageParam(r))
	if err != nil {
		h.metadataError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPage(pg))
}

type requestBody struct {
	MediaType string          `json:"mediaType"`
	MediaID   int64           `json:"mediaId"`
	Seasons   json.RawMessage `json:"seasons"`
	Is4K      bool            `json:"is4k"`
}

type requestMedia struct {
	TMDbID    int64  `json:"tmdbId"`
	MediaType string `json:"mediaType"`
	Status    int    `json:"status"`
	Status4K  int    `json:"status4k"`
}

type requestAnswer struct {
	ID        int64        `json:"id,omitempty"`
	Status    int          `json:"status"`
	Is4K      bool         `json:"is4k"`
	CreatedAt *time.Time   `json:"createdAt,omitempty"`
	Media     requestMedia `json:"media"`
}

// parseSeasons reads "all", a list of numbers or nothing (= all).
func parseSeasons(raw json.RawMessage) (all bool, list []int, err error) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s == `"all"` {
		return true, nil, nil
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return false, nil, fmt.Errorf(`seasons must be "all" or a list of season numbers`)
	}
	return false, list, nil
}

// POST /api/v1/request {mediaType, mediaId, seasons}
func (h *Handler) seerrRequest(w http.ResponseWriter, r *http.Request) {
	var body requestBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "validation", "the body must be JSON")
		return
	}
	in := requests.CreateInput{TMDbID: body.MediaID, Is4K: body.Is4K}
	kind := discover.Movie
	switch body.MediaType {
	case "movie":
		in.Kind = requests.KindMovie
	case "tv":
		kind, in.Kind = discover.Series, requests.KindSeries
		all, list, err := parseSeasons(body.Seasons)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation", err.Error())
			return
		}
		// "all" also follows seasons that are announced later.
		in.AllSeasons, in.FutureSeasons, in.Seasons = all, all, list
	default:
		writeError(w, http.StatusBadRequest, "validation", `mediaType must be "movie" or "tv"`)
		return
	}
	if body.MediaID <= 0 {
		writeError(w, http.StatusBadRequest, "validation", "mediaId (a TMDb id) is required")
		return
	}
	ctx := r.Context()
	p := principal(ctx)
	req, err := h.Requests.Create(ctx, p, in)
	var qe *requests.QuotaError
	switch {
	case err == nil:
	case errors.Is(err, requests.ErrAlreadyRequested), errors.Is(err, requests.ErrAlreadyAvailable),
		errors.Is(err, requests.ErrNothingToRequest):
		// Someone already asked for it (or it is here): answer with where it
		// stands instead of an error, so the client shows that state.
		ans, aerr := h.answer(r, kind, body.MediaID, body.Is4K)
		if aerr != nil {
			h.serverError(w, aerr)
			return
		}
		if ans.Media.Status == mediaPending {
			ans.Status = requestPending
		} else {
			ans.Status = requestApproved
		}
		writeJSON(w, http.StatusOK, ans)
		return
	case errors.As(err, &qe):
		msg := qe.Error()
		if qe.NextSlot != nil {
			msg += "; the next one is possible from " + qe.NextSlot.UTC().Format(time.RFC3339)
		}
		writeError(w, http.StatusTooManyRequests, "quota_exceeded", msg)
		return
	case errors.Is(err, requests.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "the owner of this API key may not request this")
		return
	case errors.Is(err, requests.ErrInvalid):
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	case errors.Is(err, metadata.ErrNoKey), errors.Is(err, metadata.ErrInvalidKey), errors.Is(err, metadata.ErrNotFound):
		h.metadataError(w, err)
		return
	default:
		h.serverError(w, err)
		return
	}
	ans, err := h.answer(r, kind, body.MediaID, body.Is4K)
	if err != nil {
		h.serverError(w, err)
		return
	}
	created := req.CreatedAt
	ans.ID, ans.Status, ans.CreatedAt = req.ID, requestStatus(req.Status), &created
	writeJSON(w, http.StatusCreated, ans)
}

// answer describes a title's media state for the key's owner.
func (h *Handler) answer(r *http.Request, kind string, tmdbID int64, is4K bool) (requestAnswer, error) {
	items := []discover.Item{{MediaType: kind, TMDbID: tmdbID}}
	if err := h.Discover.Annotate(r.Context(), principal(r.Context()), items); err != nil {
		return requestAnswer{}, err
	}
	mt := "movie"
	if kind == discover.Series {
		mt = "tv"
	}
	m := requestMedia{TMDbID: tmdbID, MediaType: mt, Status: mediaStatus(items[0].Status), Status4K: mediaStatus(items[0].Status4K)}
	return requestAnswer{Is4K: is4K, Media: m}, nil
}
