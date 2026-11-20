package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
)

func TestMapError(t *testing.T) {
	cases := map[error]struct {
		status int
		code   string
	}{
		auth.ErrInvalidCredentials:                      {401, "invalid_credentials"},
		fmt.Errorf("wrapped: %w", auth.ErrTOTPRequired): {401, "totp_required"},
		auth.ErrWrongPassword:                           {403, "invalid_credentials"},
		auth.ErrLastAdmin:                               {409, "last_admin"},
		&auth.LockedError{Until: time.Now()}:            {423, "account_locked"},
		jobs.ErrUnknownTask:                             {404, "unknown_task"},
		lfs.ErrNotAllowed:                               {400, "path_not_allowed"},
		errForbidden:                                    {403, "forbidden"},
	}
	for err, want := range cases {
		se := mapError(err)
		if se == nil || se.Status != want.status || se.Code != want.code {
			t.Errorf("%v: got %+v, want %+v", err, se, want)
		}
	}
	if mapError(errors.New("boom")) != nil {
		t.Fatal("unknown error mapped")
	}
	h := &Handler{}
	if e := h.NewError(context.Background(), errors.New("db exploded")); e.StatusCode != 500 || strings.Contains(e.Response.Message, "exploded") {
		t.Fatal("internal error leaked")
	}
}

func TestErrorWriters(t *testing.T) {
	rec := httptest.NewRecorder()
	NotFound(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatal(rec.Body.String())
	}
	rec = httptest.NewRecorder()
	MethodNotAllowed(rec, nil, "GET")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET" {
		t.Fatal(rec.Code)
	}
}

func TestSessionCookie(t *testing.T) {
	ctx := WithRequest(context.Background(), RequestInfo{Secure: true})
	c := sessionCookie(ctx, "tok", 60)
	for _, want := range []string{"locarr_session=tok", "HttpOnly", "Secure", "SameSite=Strict", "Max-Age=60"} {
		if !strings.Contains(c, want) {
			t.Fatalf("%q lacks %s", c, want)
		}
	}
}

func TestPermissionsRoundTrip(t *testing.T) {
	p := auth.PermRequestMovies | auth.PermRequest4K | auth.PermManageRequests
	if fromPermissions(toPermissions(p)) != p {
		t.Fatal("round trip")
	}
	u := toUser(auth.User{Role: auth.RoleAdmin})
	if !u.Permissions.ManageLibrary {
		t.Fatal("admin shown without permissions")
	}
}
