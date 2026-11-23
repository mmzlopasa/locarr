// Package streamero talks to Streamero, the media server (its native API under
// /api/v1, from its public OpenAPI description): it asks for library scans
// after imports, keeps an index of the titles Streamero already has and checks
// Streamero accounts for "Sign in with Streamero" (D-062 … D-064).
package streamero

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

// Errors returned by the client.
var (
	ErrUnreachable  = errors.New("Streamero cannot be reached")
	ErrAuth         = errors.New("Streamero refused the API key")
	ErrNotAdmin     = errors.New("the API key does not belong to a Streamero administrator")
	ErrNotStreamero = errors.New("the address does not answer like Streamero")
	ErrBadURL       = errors.New("the Streamero address must be an http:// or https:// URL")
	// Sign-in answers.
	ErrInvalidCredentials = errors.New("wrong Streamero username or password")
	ErrTOTPRequired       = errors.New("Streamero asks for a two-factor code")
	ErrInvalidTOTP        = errors.New("wrong Streamero two-factor code")
	ErrLocked             = errors.New("the Streamero account is locked for now")
)

// APIError is an unexpected answer.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("Streamero answered HTTP %d (%s): %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("Streamero answered HTTP %d", e.Status)
}

// noAuth marks a call sent without credentials.
const noAuth = "\x00none"

// Client is a Streamero API client for one address.
type Client struct {
	base string // ".../api/v1"
	key  string
	hc   *httpx.Client
}

// NormalizeURL checks and cleans a Streamero address (no trailing slash, no /api/v1).
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrBadURL
	}
	p := strings.TrimRight(u.Path, "/")
	p = strings.TrimSuffix(p, "/api/v1")
	u.Path, u.RawPath = p, ""
	return strings.TrimRight(u.String(), "/"), nil
}

// New returns a client for baseURL (already normalised) with an API key ("" for
// unauthenticated calls such as sign-in). policy is the outbound address policy.
func New(baseURL, apiKey string, policy httpx.Policy) (*Client, error) {
	hc, err := httpx.New(httpx.Options{Policy: policy, Timeout: httpx.DefaultTimeout, MaxBytes: 8 << 20, Secrets: []string{apiKey}})
	if err != nil {
		return nil, err
	}
	return &Client{base: baseURL + "/api/v1", key: apiKey, hc: hc}, nil
}

func (c *Client) do(ctx context.Context, method, path, token string, body, out any, secrets ...string) (int, error) {
	hdr := http.Header{"Accept": {"application/json"}}
	switch token {
	case "":
		token = c.key
	case noAuth:
		token = ""
	}
	if token != "" {
		hdr.Set("Authorization", "Bearer "+token)
	}
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return 0, err
		}
		hdr.Set("Content-Type", "application/json")
	}
	res, err := c.hc.Do(ctx, method, c.base+path, hdr, raw)
	if err != nil {
		if errors.Is(err, httpx.ErrBlocked) {
			return 0, err
		}
		msg := err.Error()
		for _, s := range append(secrets, c.key, token) {
			if s != "" {
				msg = strings.ReplaceAll(msg, s, "***")
			}
		}
		return 0, fmt.Errorf("%w: %s", ErrUnreachable, msg)
	}
	if res.Status >= 300 {
		var e struct{ Code, Message string }
		_ = json.Unmarshal(res.Body, &e)
		return res.Status, &APIError{Status: res.Status, Code: e.Code, Message: e.Message}
	}
	if out != nil && len(res.Body) > 0 {
		if err := json.Unmarshal(res.Body, out); err != nil {
			return res.Status, fmt.Errorf("%w: %v", ErrNotStreamero, err)
		}
	}
	return res.Status, nil
}

// SystemInfo is Streamero's public server information.
type SystemInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Product    string `json:"product"`
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	APIVersion string `json:"apiVersion"`
}

// Info returns the public server information (no key needed).
func (c *Client) Info(ctx context.Context) (SystemInfo, error) {
	var si SystemInfo
	if _, err := c.do(ctx, http.MethodGet, "/system/info", noAuth, nil, &si); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return si, ErrNotStreamero
		}
		return si, err
	}
	if !strings.EqualFold(si.Product, "Streamero") {
		return si, ErrNotStreamero
	}
	return si, nil
}

// User is a Streamero account.
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	IsAdmin     bool   `json:"isAdmin"`
	Disabled    bool   `json:"disabled"`
}

// Me returns the owner of the API key.
func (c *Client) Me(ctx context.Context) (User, error) {
	var u User
	_, err := c.do(ctx, http.MethodGet, "/auth/me", "", nil, &u)
	return u, authErr(err)
}

// Users lists Streamero's accounts (admin key).
func (c *Client) Users(ctx context.Context) ([]User, error) {
	var out []User
	_, err := c.do(ctx, http.MethodGet, "/users", "", nil, &out)
	return out, authErr(err)
}

func authErr(err error) error {
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Status {
		case http.StatusUnauthorized:
			return ErrAuth
		case http.StatusForbidden:
			return ErrNotAdmin
		}
	}
	return err
}

// Library is a Streamero library.
type Library struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Paths     []string `json:"paths"`
	ItemCount int      `json:"itemCount"`
}

// Libraries lists the libraries.
func (c *Client) Libraries(ctx context.Context) ([]Library, error) {
	var out []Library
	_, err := c.do(ctx, http.MethodGet, "/libraries", "", nil, &out)
	return out, authErr(err)
}

// Scan asks Streamero to scan one library (it answers 202 at once).
func (c *Client) Scan(ctx context.Context, libraryID string) error {
	_, err := c.do(ctx, http.MethodPost, "/libraries/"+url.PathEscape(libraryID)+"/scan", "", nil, nil)
	return authErr(err)
}

// Item is a title in a library listing.
type Item struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Year  int    `json:"year"`
}

// Items returns a page of a library's top-level items.
func (c *Client) Items(ctx context.Context, libraryID string, offset, limit int) ([]Item, int, error) {
	var page struct {
		Items []Item `json:"items"`
		Total int    `json:"total"`
	}
	q := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}, "sort": {"added"}}
	_, err := c.do(ctx, http.MethodGet, "/libraries/"+url.PathEscape(libraryID)+"/items?"+q.Encode(), "", nil, &page)
	return page.Items, page.Total, authErr(err)
}

// ProviderIDs returns an item's provider ids (tmdb, imdb, tvdb, …).
func (c *Client) ProviderIDs(ctx context.Context, itemID string) (map[string]string, error) {
	var d struct {
		ProviderIDs map[string]string `json:"providerIds"`
	}
	_, err := c.do(ctx, http.MethodGet, "/items/"+url.PathEscape(itemID), "", nil, &d)
	return d.ProviderIDs, authErr(err)
}

// Login checks a username and password (and a two-factor code) and returns the
// session token Streamero created and the account. The caller must end the
// session with Logout at once; the password is never kept.
func (c *Client) Login(ctx context.Context, username, password, totp string) (string, User, error) {
	body := map[string]any{"username": username, "password": password,
		"device": map[string]string{"name": "Locarr sign-in check", "client": "Locarr"}}
	if totp = strings.TrimSpace(totp); totp != "" {
		body["totpCode"] = totp
	}
	var res struct {
		Token string `json:"token"`
		User  User   `json:"user"`
	}
	_, err := c.do(ctx, http.MethodPost, "/auth/login", noAuth, body, &res, password)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) {
			switch {
			case ae.Code == "totp_required":
				return "", User{}, ErrTOTPRequired
			case ae.Code == "invalid_totp":
				return "", User{}, ErrInvalidTOTP
			case ae.Status == http.StatusTooManyRequests:
				return "", User{}, ErrLocked
			case ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden:
				return "", User{}, ErrInvalidCredentials
			}
		}
		return "", User{}, err
	}
	if res.Token == "" || res.User.ID == "" {
		return "", User{}, ErrNotStreamero
	}
	return res.Token, res.User, nil
}

// Logout ends a session created by Login.
func (c *Client) Logout(ctx context.Context, token string) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/logout", token, nil, nil)
	return err
}
