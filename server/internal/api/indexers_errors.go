package api

import (
	"errors"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

// mapIndexerError maps indexer errors (nil when err is not one).
func mapIndexerError(err error) *StatusError {
	var (
		inv *indexers.InvalidError
		src *indexers.SourceError
		ze  *znab.Error
	)
	switch {
	case errors.As(err, &inv):
		return &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: inv.Error()}
	case errors.As(err, &src):
		return &StatusError{Status: http.StatusBadGateway, Code: "source_error", Message: src.Error()}
	case errors.As(err, &ze):
		return &StatusError{Status: http.StatusBadGateway, Code: "indexer_" + string(ze.Kind), Message: ze.Error()}
	}
	for _, t := range []struct {
		err    error
		status int
		code   string
	}{
		{indexers.ErrNotFound, http.StatusNotFound, "not_found"},
		{indexers.ErrSourceMissing, http.StatusNotFound, "not_found"},
		{indexers.ErrNameTaken, http.StatusConflict, "name_taken"},
		{indexers.ErrGrabLimit, http.StatusTooManyRequests, "grab_limit"},
		{indexers.ErrBadToken, http.StatusBadRequest, "bad_token"},
		{indexers.ErrNoDownload, http.StatusNotFound, "no_download"},
	} {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return nil
}
