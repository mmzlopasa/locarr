// Package api implements the generated ogen server interface (api/openapi.yaml).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"

	"github.com/mmzlopasa/locarr/server/internal/adult"
	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/backup"
	"github.com/mmzlopasa/locarr/server/internal/board"
	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/discover"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/health"
	"github.com/mmzlopasa/locarr/server/internal/history"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/recycle"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/logging"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/migrate"
	"github.com/mmzlopasa/locarr/server/internal/musicdiscovery"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/notify/delivery"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/requests"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/stats"
	"github.com/mmzlopasa/locarr/server/internal/subtitles"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// SessionCookie is the web client's HttpOnly session cookie.
const SessionCookie = "locarr_session"

// Handler implements gen.Handler.
type Handler struct {
	DB          *db.DB
	Auth        *auth.Service
	Settings    *settings.Store
	Jobs        *jobs.Queue
	RootFolders *rootfolders.Service
	Quality     *quality.Store
	Formats     *customformats.Store
	Downloads   *downloads.Service
	Indexers    *indexers.Service
	Movies      *movies.Service
	// Adult is the optional adult library (D-103); nil-safe when off.
	Adult     *adult.Service
	Series    *series.Service
	Music     *music.Service
	Subtitles *subtitles.Service
	// CoverArt caches Cover Art Archive images; LinkExisting queues linking
	// an existing artist folder's files (both set by the app).
	CoverArt     *metadata.ImageCache
	LinkExisting func(ctx context.Context, artistID int64)
	Wanted       *wanted.Service
	// Requests (P8): requests and issues, discover pages, notifications.
	Requests *requests.Service
	Discover *discover.Service
	// MusicDiscovery suggests artists similar to the library's.
	MusicDiscovery *musicdiscovery.Service
	Hub            *notify.Hub
	// Streamero (P9) and the outward notification channels.
	Streamero *streamero.Service
	Channels  *delivery.Service
	// Migrate imports an existing setup (P10).
	Migrate  *migrate.Service
	Timeline *timeline.Store
	Recycle  *recycle.Bin
	Images   *metadata.ImageCache
	// TMDb validates keys before they are stored; MetaCache is cleared when the language changes.
	TMDb metadata.MovieProvider
	// TVDB validates a TVDB key before it is stored (nil = TVDB not available).
	TVDB      TVDBValidator
	MetaCache *metadata.Cache
	// Operations (P11).
	Board        *board.Service
	BoardActions board.Actions
	History      *history.Store
	Health       *health.Service
	Backups      *backup.Service
	Stats        *stats.Service
	Logs         *logging.Sinks
	// MaxSchema is the newest schema version this build knows (restores refuse newer ones).
	MaxSchema int64
	// Restart restarts the services in-process (set by the app; nil = restore unavailable).
	Restart   func()
	ConfigDir string
	DataRoots []string
	StartedAt time.Time
	Log       *slog.Logger
}

var _ gen.Handler = (*Handler)(nil)

// StatusError is an error with an HTTP status and a stable code.
type StatusError struct {
	Status  int
	Code    string
	Message string
}

func (e *StatusError) Error() string { return e.Message }

func statusErr(status int, code string, err error) *StatusError {
	return &StatusError{Status: status, Code: code, Message: err.Error()}
}

var (
	errForbidden = &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: "you are not allowed to do this"}
	errReadOnly  = &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: "this API key is read-only"}
)

// mapError turns known errors into a status and code (nil = internal error).
func mapError(err error) *StatusError {
	var (
		se        *StatusError
		locked    *auth.LockedError
		secErr    *ogenerrors.SecurityError
		decReq    *ogenerrors.DecodeRequestError
		decParams *ogenerrors.DecodeParamsError
		valErr    *validate.Error
		tooLarge  *http.MaxBytesError
	)
	switch {
	case errors.As(err, &se):
		return se
	case errors.As(err, &tooLarge):
		return &StatusError{Status: http.StatusRequestEntityTooLarge, Code: "request_too_large", Message: "request body too large"}
	case errors.As(err, &secErr):
		return &StatusError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "not signed in"}
	case errors.As(err, &decReq), errors.As(err, &decParams), errors.As(err, &valErr):
		return &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: err.Error()}
	case errors.As(err, &locked):
		return &StatusError{Status: http.StatusLocked, Code: "account_locked", Message: err.Error()}
	}
	if se := mapAdultError(err); se != nil {
		return se
	}
	if se := mapOperationsError(err); se != nil {
		return se
	}
	if se := mapProfileError(err); se != nil {
		return se
	}
	if se := mapSeriesError(err); se != nil {
		return se
	}
	if se := mapLibraryError(err); se != nil {
		return se
	}
	if se := mapMusicError(err); se != nil {
		return se
	}
	if se := mapMediaServerError(err); se != nil {
		return se
	}
	if se := mapTranslationError(err); se != nil {
		return se
	}
	if se := mapSubtitlesError(err); se != nil {
		return se
	}
	if se := mapMigrateError(err); se != nil {
		return se
	}
	if se := mapRequestsError(err); se != nil {
		return se
	}
	if se := mapDownloadsError(err); se != nil {
		return se
	}
	if ie := mapIndexerError(err); ie != nil {
		return ie
	}
	table := []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{auth.ErrWrongPassword, http.StatusForbidden, "invalid_credentials"},
		{auth.ErrTOTPRequired, http.StatusUnauthorized, "totp_required"},
		{auth.ErrInvalidTOTP, http.StatusUnauthorized, "invalid_totp"},
		{auth.ErrSetupDone, http.StatusConflict, "setup_done"},
		{auth.ErrBadSetupCode, http.StatusForbidden, "bad_setup_code"},
		{auth.ErrSetupThrottled, http.StatusTooManyRequests, "rate_limited"},
		{auth.ErrUsernameTaken, http.StatusConflict, "username_taken"},
		{auth.ErrInvalidUsername, http.StatusBadRequest, "invalid_username"},
		{auth.ErrWeakPassword, http.StatusBadRequest, "weak_password"},
		{auth.ErrInvalidRole, http.StatusBadRequest, "validation"},
		{auth.ErrInvalidName, http.StatusBadRequest, "validation"},
		{auth.ErrNotFound, http.StatusNotFound, "not_found"},
		{auth.ErrLastAdmin, http.StatusConflict, "last_admin"},
		{auth.ErrSelf, http.StatusConflict, "self_action"},
		{auth.ErrUnauthenticated, http.StatusUnauthorized, "unauthorized"},
		{auth.ErrSessionRequired, http.StatusForbidden, "session_required"},
		{auth.ErrTOTPAlreadyOn, http.StatusConflict, "totp_already_on"},
		{auth.ErrTOTPNotStarted, http.StatusConflict, "totp_not_started"},
		{auth.ErrTOTPNotOn, http.StatusConflict, "totp_not_on"},
		{jobs.ErrNotFound, http.StatusNotFound, "not_found"},
		{jobs.ErrUnknownTask, http.StatusNotFound, "unknown_task"},
		{jobs.ErrNotCancelable, http.StatusConflict, "conflict"},
		{lfs.ErrNotAllowed, http.StatusBadRequest, "path_not_allowed"},
		{lfs.ErrNotFound, http.StatusNotFound, "path_not_found"},
		{rootfolders.ErrExists, http.StatusConflict, "already_exists"},
		{rootfolders.ErrNotFound, http.StatusNotFound, "not_found"},
		{rootfolders.ErrNotDir, http.StatusBadRequest, "path_not_found"},
		{rootfolders.ErrInvalidKind, http.StatusBadRequest, "validation"},
	}
	for _, t := range table {
		if errors.Is(err, t.err) {
			return statusErr(t.status, t.code, t.err)
		}
	}
	return nil
}

// NewError maps handler errors to the common error response.
func (h *Handler) NewError(ctx context.Context, err error) *gen.ErrorStatusCode {
	if se := mapError(err); se != nil {
		return &gen.ErrorStatusCode{StatusCode: se.Status, Response: gen.Error{Code: se.Code, Message: se.Message}}
	}
	slog.ErrorContext(ctx, "request failed", "err", err)
	return &gen.ErrorStatusCode{StatusCode: http.StatusInternalServerError,
		Response: gen.Error{Code: "internal", Message: "internal server error"}}
}

// ErrorHandler renders errors the generated server raises outside handlers
// (decoding, validation, security) in the common format.
func ErrorHandler(ctx context.Context, w http.ResponseWriter, _ *http.Request, err error) {
	status, body := http.StatusInternalServerError, gen.Error{Code: "internal", Message: "internal server error"}
	if se := mapError(err); se != nil {
		status, body = se.Status, gen.Error{Code: se.Code, Message: se.Message}
	} else {
		slog.ErrorContext(ctx, "request failed", "err", err)
	}
	WriteError(w, status, body.Code, body.Message)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: message})
}

// NotFound answers unknown API paths.
func NotFound(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, http.StatusNotFound, "not_found", "no such endpoint")
}

// MethodNotAllowed answers a known path with the wrong method.
func MethodNotAllowed(w http.ResponseWriter, _ *http.Request, allowed string) {
	w.Header().Set("Allow", allowed)
	WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func optTime(t *time.Time) gen.OptDateTime {
	if t == nil {
		return gen.OptDateTime{}
	}
	return gen.NewOptDateTime(*t)
}

func optStr(s string) gen.OptString {
	if s == "" {
		return gen.OptString{}
	}
	return gen.NewOptString(s)
}
