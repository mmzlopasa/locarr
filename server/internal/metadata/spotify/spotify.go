// Package spotify reads artists from the Spotify Web API with the client
// credentials flow, written from Spotify's public documentation. Since
// November 2024 Spotify no longer serves "related artists" (or
// recommendations) to new apps and apps in development mode; the client
// tries the endpoint and reports ErrRestricted when Spotify refuses it, and
// otherwise offers what every app gets: artist search with genres and the
// artist's page. Search answers at most 10 artists per request.
package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// Default addresses.
const (
	DefaultAPIURL      = "https://api.spotify.com/v1"
	DefaultAccountsURL = "https://accounts.spotify.com"
)

const (
	maxBody = 4 << 20
	timeout = 20 * time.Second
)

// ErrRestricted is returned when Spotify does not give this app related artists.
var ErrRestricted = errors.New("related artists are not available to this Spotify app (Spotify restricted them in November 2024)")

// Credentials are the app's client id and secret.
type Credentials struct{ ID, Secret string }

// Client talks to Spotify.
type Client struct {
	APIURL      string
	AccountsURL string
	// Creds returns the configured credentials (empty = not configured).
	Creds   func(ctx context.Context) Credentials
	Limiter *metadata.Limiter
	HTTP    *http.Client

	mu      sync.Mutex
	token   string
	tokenID string
	expires time.Time
}

// New returns a client (empty URLs = the public services).
func New(apiURL, accountsURL string, creds func(ctx context.Context) Credentials) *Client {
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	if accountsURL == "" {
		accountsURL = DefaultAccountsURL
	}
	return &Client{APIURL: strings.TrimRight(apiURL, "/"), AccountsURL: strings.TrimRight(accountsURL, "/"), Creds: creds,
		Limiter: metadata.NewLimiter(5, 5), HTTP: &http.Client{Timeout: timeout, CheckRedirect: metadata.SameHost}}
}

// Artist is a Spotify artist.
type Artist struct {
	ID     string
	Name   string
	Genres []string
}

// URL is the artist's page on Spotify.
func (a Artist) URL() string { return ArtistURL(a.ID) }

// ArtistURL is an artist's page on Spotify.
func ArtistURL(id string) string { return "https://open.spotify.com/artist/" + id }

func read(res *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	_ = res.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("%w: answer too large", metadata.ErrUnavailable)
	}
	return body, nil
}

// fetchToken runs the client credentials flow.
func (c *Client) fetchToken(ctx context.Context, cr Credentials) (string, time.Duration, error) {
	if cr.ID == "" || cr.Secret == "" {
		return "", 0, metadata.ErrNoKey
	}
	if err := c.Limiter.Wait(ctx); err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.AccountsURL+"/api/token",
		strings.NewReader(url.Values{"grant_type": {"client_credentials"}}.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.SetBasicAuth(cr.ID, cr.Secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Locarr/"+version.Version)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
	}
	body, err := read(res)
	if err != nil {
		return "", 0, err
	}
	switch {
	case res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnauthorized:
		return "", 0, fmt.Errorf("%w: Spotify refused the client id or secret", metadata.ErrInvalidKey)
	case res.StatusCode >= 300:
		return "", 0, fmt.Errorf("%w: token HTTP %d", metadata.ErrUnavailable, res.StatusCode)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", 0, fmt.Errorf("%w: bad token answer", metadata.ErrUnavailable)
	}
	return tok.AccessToken, time.Duration(max(tok.ExpiresIn, 60)) * time.Second, nil
}

func (c *Client) accessToken(ctx context.Context, cr Credentials, renew bool) (string, error) {
	c.mu.Lock()
	if !renew && c.token != "" && c.tokenID == cr.ID+":"+cr.Secret && time.Now().Before(c.expires) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	t, ttl, err := c.fetchToken(ctx, cr)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.token, c.tokenID, c.expires = t, cr.ID+":"+cr.Secret, time.Now().Add(ttl-30*time.Second)
	c.mu.Unlock()
	return t, nil
}

func (c *Client) creds(ctx context.Context) Credentials {
	if c.Creds == nil {
		return Credentials{}
	}
	cr := c.Creds(ctx)
	return Credentials{ID: strings.TrimSpace(cr.ID), Secret: strings.TrimSpace(cr.Secret)}
}

// get calls the API with a bearer token (renewed once on 401).
func (c *Client) get(ctx context.Context, cr Credentials, path string, out any) error {
	renew := false
	for attempt := 0; attempt < 3; attempt++ {
		tok, err := c.accessToken(ctx, cr, renew)
		if err != nil {
			return err
		}
		if err := c.Limiter.Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Locarr/"+version.Version)
		res, err := c.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
		}
		body, err := read(res)
		if err != nil {
			return err
		}
		switch {
		case res.StatusCode == http.StatusUnauthorized && !renew:
			renew = true
			continue
		case res.StatusCode == http.StatusTooManyRequests:
			wait := 5 * time.Second
			if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s >= 0 && s < 300 {
				wait = time.Duration(s+1) * time.Second
			}
			c.Limiter.Pause(time.Now().Add(wait))
			continue
		case res.StatusCode == http.StatusForbidden:
			return ErrRestricted
		case res.StatusCode == http.StatusNotFound:
			return metadata.ErrNotFound
		case res.StatusCode >= 300:
			return fmt.Errorf("%w: HTTP %d", metadata.ErrUnavailable, res.StatusCode)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("%w: bad answer: %w", metadata.ErrUnavailable, err)
		}
		return nil
	}
	return fmt.Errorf("%w: %w", metadata.ErrUnavailable, metadata.ErrRateLimited)
}

type wireArtist struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Genres []string `json:"genres"`
}

func (a wireArtist) artist() Artist {
	g := a.Genres
	if g == nil {
		g = []string{}
	}
	return Artist{ID: a.ID, Name: a.Name, Genres: g}
}

// SearchArtists finds artists by name (at most 10, Spotify's limit).
func (c *Client) SearchArtists(ctx context.Context, name string) ([]Artist, error) {
	q := url.Values{"q": {name}, "type": {"artist"}, "limit": {"10"}}
	var res struct {
		Artists struct {
			Items []wireArtist `json:"items"`
		} `json:"artists"`
	}
	if err := c.get(ctx, c.creds(ctx), "/search?"+q.Encode(), &res); err != nil {
		return nil, err
	}
	out := make([]Artist, 0, len(res.Artists.Items))
	for _, a := range res.Artists.Items {
		out = append(out, a.artist())
	}
	return out, nil
}

// Related returns Spotify's related artists of an artist (ErrRestricted for
// apps Spotify no longer serves them to).
func (c *Client) Related(ctx context.Context, id string) ([]Artist, error) {
	var res struct {
		Artists []wireArtist `json:"artists"`
	}
	if err := c.get(ctx, c.creds(ctx), "/artists/"+url.PathEscape(id)+"/related-artists", &res); err != nil {
		return nil, err
	}
	out := make([]Artist, 0, len(res.Artists))
	for _, a := range res.Artists {
		out = append(out, a.artist())
	}
	return out, nil
}

// Capabilities is what Spotify gives an app.
type Capabilities struct {
	// Related: the related-artists endpoint answers.
	Related bool
}

// Validate checks credentials without storing them and finds out whether
// related artists are available to the app.
func (c *Client) Validate(ctx context.Context, cr Credentials) (Capabilities, error) {
	var res struct {
		Artists struct {
			Items []wireArtist `json:"items"`
		} `json:"artists"`
	}
	if err := c.get(ctx, cr, "/search?"+url.Values{"q": {"Radiohead"}, "type": {"artist"}, "limit": {"1"}}.Encode(), &res); err != nil {
		return Capabilities{}, err
	}
	if len(res.Artists.Items) == 0 {
		return Capabilities{}, nil
	}
	var rel struct {
		Artists []wireArtist `json:"artists"`
	}
	err := c.get(ctx, cr, "/artists/"+url.PathEscape(res.Artists.Items[0].ID)+"/related-artists", &rel)
	switch {
	case err == nil:
		return Capabilities{Related: true}, nil
	case errors.Is(err, ErrRestricted), errors.Is(err, metadata.ErrNotFound):
		return Capabilities{}, nil
	}
	return Capabilities{}, err
}
