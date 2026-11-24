package httpx

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
)

// ErrChallenge is returned when a site's browser check could not be passed.
var ErrChallenge = errors.New("blocked by a browser check")

// IsChallenge reports whether r is a browser check page (Cloudflare,
// DDoS-Guard or a similar "checking your browser" page) instead of the
// site's content.
func IsChallenge(r *Response) bool { return isChallenge(r) }

// checkTitles are titles of pages that check the browser before the content.
var checkTitles = [][]byte{
	[]byte("<title>just a moment"), []byte("<title>checking your browser"), []byte("<title>ddos-guard"),
	[]byte("<title>one moment, please"), []byte("<title>please wait while we"), []byte("<title>ddos protection"),
	[]byte("<title>security check in progress"), []byte("<title>verifying you are human"),
}

// checkMarkers appear only on browser check pages.
var checkMarkers = [][]byte{
	[]byte("_cf_chl_opt"), []byte("cf-chl-"), []byte("cf_chl_"), []byte("/cdn-cgi/challenge-platform/h/"),
	[]byte("check.ddos-guard.net"), []byte("/.well-known/ddos-guard/"), []byte("sucuri_cloudproxy_js"),
	[]byte("enable javascript and cookies to continue"), []byte("checking if the site connection is secure"),
}

// isChallenge recognises a browser check page. A check answers 403, 429 or
// 503 (some waiting rooms answer 200 with a script and no content), names
// itself in the title or carries the check's own script markers. Pages that
// merely load a bot-detection script among their content are not checks.
func isChallenge(r *Response) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(r.Header.Get("Cf-Mitigated"), "challenge") {
		return true
	}
	b := bytes.ToLower(r.Body[:min(len(r.Body), 64<<10)])
	title := false
	for _, t := range checkTitles {
		if bytes.Contains(b, t) {
			title = true
			break
		}
	}
	marker := false
	for _, m := range checkMarkers {
		if bytes.Contains(b, m) {
			marker = true
			break
		}
	}
	switch r.Status {
	case http.StatusForbidden, http.StatusServiceUnavailable, http.StatusTooManyRequests:
		server := strings.ToLower(r.Header.Get("Server"))
		vendor := strings.Contains(server, "cloudflare") || strings.Contains(server, "ddos-guard")
		return title || marker && (vendor || len(r.Body) < 256<<10)
	case http.StatusOK:
		// A small page that is nothing but a check.
		return title && marker && len(r.Body) < 64<<10
	}
	return false
}
