package indexers

import (
	"context"
	"net/url"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Settings of the companion browser.
var (
	// BrowserURL replaces the browser's address (empty: LOCARR_BROWSER_URL or
	// the compose service name).
	BrowserURL = settings.Key[string]{Name: "indexers.browserUrl", Default: ""}
	// BrowserToken replaces LOCARR_BROWSER_TOKEN.
	BrowserToken = settings.SecretKey{Name: "indexers.browserToken"}
)

// BrowserConfig is the browser's current address and token.
func (s *Service) BrowserConfig(ctx context.Context) (httpx.BrowserConfig, error) {
	cfg := httpx.BrowserConfig{URL: httpx.DefaultBrowserURL, Token: s.BrowserEnv.Token}
	if s.BrowserEnv.URL != "" {
		cfg.URL = s.BrowserEnv.URL
	}
	u, err := settings.Get(ctx, s.Settings, BrowserURL)
	if err != nil {
		return cfg, err
	}
	if u != "" {
		cfg.URL = u
	}
	tok, ok, err := s.Settings.Secret(ctx, BrowserToken)
	if err != nil {
		return cfg, err
	}
	if ok {
		cfg.Token = tok
	}
	pol, err := s.browserPolicy(ctx)
	if err != nil {
		return cfg, err
	}
	cfg.Policy = &pol
	return cfg, nil
}

// browserPolicy reaches the browser: the indexers' allowed hosts apply (for a
// browser on the host itself), private networks are always allowed (it lives
// on the compose network).
func (s *Service) browserPolicy(ctx context.Context) (httpx.Policy, error) {
	pol, err := s.policy(ctx)
	pol.BlockPrivate = false
	return pol, err
}

// ValidateBrowserURL checks an administrator-entered browser address.
func (s *Service) ValidateBrowserURL(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	pol, err := s.browserPolicy(ctx)
	if err != nil {
		return err
	}
	if err := pol.CheckURL(raw); err != nil {
		return invalid("browserUrl", err.Error())
	}
	return nil
}

// BrowserStatus asks the companion browser how it is.
func (s *Service) BrowserStatus(ctx context.Context) httpx.BrowserStatus { return s.Browser.Status(ctx) }

// BrowserTest has the browser pass its built-in check page, or (with rawURL)
// open that page with the indexers' address policy.
func (s *Service) BrowserTest(ctx context.Context, rawURL, variant string) (httpx.TestResult, error) {
	if strings.TrimSpace(rawURL) == "" {
		return s.Browser.SelfTest(ctx, variant)
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return httpx.TestResult{}, err
	}
	return s.Browser.TestURL(ctx, pol, strings.TrimSpace(rawURL))
}

// ChallengeIssue explains why an indexer's browser check failed: the browser
// is missing, failed, or the site wants a person. ok is false when nothing
// is known.
func (s *Service) ChallengeIssue(ind Indexer) (reason, message string, ok bool) {
	base := ind.BaseURL
	if base == "" && ind.Kind == Native {
		if d, found := s.Definition(ind.Definition); found {
			base = d.DefaultURL()
		}
	}
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		if r, m, found := s.Browser.HostIssue(u.Host); found {
			return r, m, true
		}
	}
	if reach, known := s.Browser.Reachable(); known && !reach {
		return httpx.ReasonMissing, "the built-in browser is not running or not reachable", true
	}
	return "", "", false
}
