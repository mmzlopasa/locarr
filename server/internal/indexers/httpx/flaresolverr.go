package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrChallenge is returned when a Cloudflare challenge could not be solved.
var ErrChallenge = errors.New("blocked by a Cloudflare challenge")

// IsChallenge reports whether r is a Cloudflare browser challenge page.
func IsChallenge(r *Response) bool { return isChallenge(r) }

// isChallenge recognises a Cloudflare browser challenge page.
func isChallenge(r *Response) bool {
	if r.Status != http.StatusForbidden && r.Status != http.StatusServiceUnavailable {
		return false
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Server")), "cloudflare") && r.Header.Get("Cf-Mitigated") == "" {
		return false
	}
	b := strings.ToLower(string(r.Body[:min(len(r.Body), 64<<10)]))
	return r.Header.Get("Cf-Mitigated") == "challenge" || strings.Contains(b, "challenge-platform") ||
		strings.Contains(b, "cf-chl") || strings.Contains(b, "just a moment")
}

// solve asks FlareSolverr (its documented `request.get` command) to pass the
// challenge and keeps the clearance cookies and user agent for later requests
// to the same host.
func (c *Client) solve(ctx context.Context, target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	// FlareSolverr only needs the site to pass the challenge, not the API call.
	site := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}).String()
	body, _ := json.Marshal(map[string]any{"cmd": "request.get", "url": site, "maxTimeout": 60000})
	endpoint := strings.TrimRight(c.o.FlareSolverr, "/") + "/v1"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.fsHC.Do(req)
	if err != nil {
		return c.wrap(err, endpoint)
	}
	defer res.Body.Close()
	raw, err := ReadCapped(res.Body, 8<<20)
	if err != nil {
		return c.wrap(err, endpoint)
	}
	var out struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Solution struct {
			Cookies []struct {
				Name    string  `json:"name"`
				Value   string  `json:"value"`
				Expires float64 `json:"expires"`
			} `json:"cookies"`
			UserAgent string `json:"userAgent"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Status != "ok" {
		return fmt.Errorf("%w: FlareSolverr answered %q", ErrChallenge, Scrub(out.Message, c.o.Secrets...))
	}
	cl := &clearance{host: u.Host, userAgent: out.Solution.UserAgent, expires: time.Now().Add(30 * time.Minute)}
	for _, ck := range out.Solution.Cookies {
		cl.cookies = append(cl.cookies, &http.Cookie{Name: ck.Name, Value: ck.Value}) // #nosec G124 -- sent to a server, not set in a browser
	}
	c.mu.Lock()
	c.cf = cl
	c.mu.Unlock()
	return nil
}
