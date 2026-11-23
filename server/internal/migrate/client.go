package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

// SourceError is a failure talking to a source, in words for the person.
type SourceError struct {
	Code    string // unreachable, unauthorized, wrong_app, unsupported_version, bad_response
	Message string
}

func (e *SourceError) Error() string { return e.Message }

// Conn is a source to read from: its address and secret (API key, or the
// password for qBittorrent).
type Conn struct {
	Kind     Kind
	URL      string
	APIKey   string
	Username string
	Password string
}

// client reads a source. It only ever sends GET requests; qBittorrent also
// needs its documented sign-in POST, which changes nothing on it.
type client struct {
	conn   Conn
	hc     *httpx.Client
	big    *httpx.Client
	cookie string
}

// limits for one source request.
const (
	requestTimeout = 60 * time.Second
	listTimeout    = 10 * time.Minute
	listMaxBytes   = 1 << 30
)

func newClient(conn Conn, pol httpx.Policy) (*client, error) {
	conn.URL = strings.TrimRight(strings.TrimSpace(conn.URL), "/")
	secrets := []string{conn.APIKey, conn.Password}
	hc, err := httpx.New(httpx.Options{Policy: pol, Timeout: requestTimeout, MaxBytes: 64 << 20, Secrets: secrets})
	if err != nil {
		return nil, err
	}
	big, err := httpx.New(httpx.Options{Policy: pol, Timeout: listTimeout, MaxBytes: listMaxBytes, Secrets: secrets})
	if err != nil {
		return nil, err
	}
	return &client{conn: conn, hc: hc, big: big}, nil
}

func (c *client) headers() http.Header {
	h := http.Header{"Accept": {"application/json"}}
	switch c.conn.Kind {
	case Bazarr:
		h.Set("X-API-KEY", c.conn.APIKey)
	case QBittorrent:
		if c.cookie != "" {
			h.Set("Cookie", c.cookie)
		}
		// qBittorrent checks the Referer/Origin against its own address.
		h.Set("Referer", c.conn.URL)
	case SABnzbd:
	default:
		if c.conn.APIKey != "" {
			h.Set("X-Api-Key", c.conn.APIKey)
		}
	}
	return h
}

func (c *client) url(path string, q url.Values) string {
	u := c.conn.URL + path
	if c.conn.Kind == SABnzbd {
		if q == nil {
			q = url.Values{}
		}
		q.Set("apikey", c.conn.APIKey)
		q.Set("output", "json")
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

func (c *client) fail(err error) error {
	var se *SourceError
	if errors.As(err, &se) {
		return se
	}
	return &SourceError{Code: "unreachable", Message: fmt.Sprintf("%s did not answer: %s", c.conn.Kind.Label(),
		httpx.Scrub(err.Error(), c.conn.APIKey, c.conn.Password))}
}

func (c *client) statusErr(path string, status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &SourceError{Code: "unauthorized", Message: fmt.Sprintf("%s refused the API key or sign-in", c.conn.Kind.Label())}
	case http.StatusNotFound:
		return &SourceError{Code: "not_found", Message: fmt.Sprintf("%s has no %s (HTTP 404)", c.conn.Kind.Label(), path)}
	}
	return &SourceError{Code: "bad_response", Message: fmt.Sprintf("%s answered HTTP %d for %s", c.conn.Kind.Label(), status, path)}
}

// get reads one JSON document.
func (c *client) get(ctx context.Context, path string, q url.Values, v any) error {
	res, err := c.hc.Get(ctx, c.url(path, q), c.headers())
	if err != nil {
		return c.fail(err)
	}
	if res.Status != http.StatusOK {
		return c.statusErr(path, res.Status)
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(res.Body, v); err != nil {
		return &SourceError{Code: "bad_response", Message: fmt.Sprintf("%s sent something that is not the expected JSON for %s", c.conn.Kind.Label(), path)}
	}
	return nil
}

// getText reads a plain-text answer (qBittorrent versions).
func (c *client) getText(ctx context.Context, path string) (string, error) {
	res, err := c.hc.Get(ctx, c.url(path, nil), c.headers())
	if err != nil {
		return "", c.fail(err)
	}
	if res.Status != http.StatusOK {
		return "", c.statusErr(path, res.Status)
	}
	return strings.TrimSpace(string(res.Body)), nil
}

// list streams a JSON array, decoding one element at a time into fn.
func list[T any](ctx context.Context, c *client, path string, q url.Values, fn func(T)) error {
	status, _, err := c.big.Stream(ctx, c.url(path, q), c.headers(), func(r io.Reader) error {
		dec := json.NewDecoder(r)
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); !ok || d != '[' {
			return fmt.Errorf("not a JSON array")
		}
		for dec.More() {
			var v T
			if err := dec.Decode(&v); err != nil {
				return err
			}
			fn(v)
		}
		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "JSON") || strings.Contains(err.Error(), "invalid character") {
			return &SourceError{Code: "bad_response", Message: fmt.Sprintf("%s sent something that is not a JSON list for %s", c.conn.Kind.Label(), path)}
		}
		return c.fail(err)
	}
	if status != http.StatusOK {
		return c.statusErr(path, status)
	}
	return nil
}

// all reads a JSON array.
func all[T any](ctx context.Context, c *client, path string, q url.Values) ([]T, error) {
	out := []T{}
	err := list(ctx, c, path, q, func(v T) { out = append(out, v) })
	return out, err
}

// qbitLogin signs in to qBittorrent (POST /api/v2/auth/login, the WebUI API's
// documented handshake) and keeps the session cookie. Nothing on the client
// changes.
func (c *client) qbitLogin(ctx context.Context) error {
	if c.conn.Username == "" && c.conn.Password == "" {
		return nil // the WebUI may allow clients without sign-in (local network bypass)
	}
	form := url.Values{"username": {c.conn.Username}, "password": {c.conn.Password}}.Encode()
	hdr := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}, "Referer": {c.conn.URL}}
	res, err := c.hc.Do(ctx, http.MethodPost, c.conn.URL+"/api/v2/auth/login", hdr, []byte(form))
	if err != nil {
		return c.fail(err)
	}
	if res.Status == http.StatusForbidden {
		return &SourceError{Code: "unauthorized", Message: "qBittorrent refused the sign-in (too many failed attempts block the address for a while)"}
	}
	if res.Status != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(string(res.Body)), "Ok") {
		return &SourceError{Code: "unauthorized", Message: "qBittorrent refused the user name or password"}
	}
	for _, ck := range (&http.Response{Header: res.Header}).Cookies() {
		if ck.Name == "SID" || strings.HasSuffix(ck.Name, "_SID") {
			c.cookie = ck.Name + "=" + ck.Value
		}
	}
	return nil
}
