// Package listenbrainz reads similar artists (the public labs dataset
// similar-artists) and an artist's most listened release groups (the
// popularity API) from ListenBrainz, written from their public
// documentation. Both need no key; ListenBrainz asks clients for one request
// per second and a User-Agent, and answers 429 with X-RateLimit-Reset-In when
// a client goes over.
package listenbrainz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// Default addresses.
const (
	DefaultAPIURL  = "https://api.listenbrainz.org"
	DefaultLabsURL = "https://labs.api.listenbrainz.org"
	// Algorithm is the similar-artists dataset used (listening sessions over
	// about 20 years, up to 100 artists per artist).
	Algorithm = "session_based_days_7500_session_300_contribution_5_threshold_10_limit_100_filter_True_skip_30"
)

const (
	maxBody = 8 << 20
	timeout = 30 * time.Second
)

// Client talks to ListenBrainz.
type Client struct {
	APIURL  string
	LabsURL string
	// Contact goes into the User-Agent (the MusicBrainz contact setting).
	Contact func(ctx context.Context) string
	Limiter *metadata.Limiter
	HTTP    *http.Client
}

// New returns a client (empty URLs = the public services).
func New(apiURL, labsURL string) *Client {
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	if labsURL == "" {
		labsURL = DefaultLabsURL
	}
	return &Client{APIURL: strings.TrimRight(apiURL, "/"), LabsURL: strings.TrimRight(labsURL, "/"), Limiter: metadata.NewLimiter(1, 1),
		HTTP: &http.Client{Timeout: timeout, CheckRedirect: metadata.SameHost}}
}

// Artist is a similar artist.
type Artist struct {
	MBID    string
	Name    string
	Comment string
	// Score is the dataset's raw score (only comparable within one answer).
	Score float64
}

func (c *Client) fetch(ctx context.Context, u string, out any) error {
	ua := "Locarr/" + version.Version
	if c.Contact != nil {
		if v := strings.TrimSpace(c.Contact(ctx)); v != "" {
			ua += " ( " + v + " )"
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.Limiter.Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", ua)
		res, err := c.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
		_ = res.Body.Close()
		if err != nil {
			return fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
		}
		if len(body) > maxBody {
			return fmt.Errorf("%w: answer too large", metadata.ErrUnavailable)
		}
		switch {
		case res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable:
			wait := 2 * time.Second
			if s, err := strconv.Atoi(res.Header.Get("X-RateLimit-Reset-In")); err == nil && s >= 0 && s < 120 {
				wait = time.Duration(s+1) * time.Second
			}
			c.Limiter.Pause(time.Now().Add(wait))
			continue
		case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusBadRequest:
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

// Similar returns the artists listened to in the same sessions as the
// artist, most similar first.
func (c *Client) Similar(ctx context.Context, mbid string) ([]Artist, error) {
	q := url.Values{"artist_mbids": {strings.ToLower(mbid)}, "algorithm": {Algorithm}}
	var res []struct {
		MBID      string  `json:"artist_mbid"`
		Name      string  `json:"name"`
		Comment   string  `json:"comment"`
		Score     float64 `json:"score"`
		Reference string  `json:"reference_mbid"`
	}
	if err := c.fetch(ctx, c.LabsURL+"/similar-artists/json?"+q.Encode(), &res); err != nil {
		return nil, err
	}
	out := make([]Artist, 0, len(res))
	for _, a := range res {
		if a.MBID == "" || strings.EqualFold(a.MBID, mbid) {
			continue
		}
		out = append(out, Artist{MBID: strings.ToLower(a.MBID), Name: a.Name, Comment: a.Comment, Score: a.Score})
	}
	return out, nil
}

// TopReleaseGroup returns the artist's most listened album (or, without one,
// release group); "" when ListenBrainz has none.
func (c *Client) TopReleaseGroup(ctx context.Context, mbid string) (string, error) {
	var res []struct {
		MBID         string `json:"release_group_mbid"`
		ReleaseGroup struct {
			Type  string `json:"type"`
			CAAID *int64 `json:"caa_id"`
		} `json:"release_group"`
	}
	if err := c.fetch(ctx, c.APIURL+"/1/popularity/top-release-groups-for-artist/"+url.PathEscape(strings.ToLower(mbid)), &res); err != nil {
		return "", err
	}
	first := ""
	for _, g := range res {
		if g.MBID == "" || g.ReleaseGroup.CAAID == nil {
			continue
		}
		if strings.EqualFold(g.ReleaseGroup.Type, "album") {
			return strings.ToLower(g.MBID), nil
		}
		if first == "" {
			first = strings.ToLower(g.MBID)
		}
	}
	return first, nil
}
