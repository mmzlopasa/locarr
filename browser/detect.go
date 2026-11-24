package main

import "strings"

// Page is what the browser sees of the current document while it waits.
type Page struct {
	Status int    // HTTP status of the last main document
	Server string // its Server header (lower-case)
	// Mitigated is its cf-mitigated header (lower-case).
	Mitigated string
	Title     string
	Text      string // the visible text
	HTML      string // the document's markup (possibly truncated)
	// Widget is true when the page holds an interactive check widget
	// (a CAPTCHA frame or container).
	Widget bool
}

// State is the verdict about a page.
type State int

// Page states.
const (
	StateContent   State = iota // real content: done
	StateChallenge              // an automatic browser check is running
	StateHuman                  // the check asks for a person (a CAPTCHA)
	StateBlocked                // the site refuses this client outright
)

func (s State) String() string {
	return [...]string{"content", "challenge", "human", "blocked"}[s]
}

// challengeTitles are page titles of browser checks that pass on their own.
var challengeTitles = []string{
	"just a moment", "checking your browser", "ddos-guard", "ddos protection", "one moment, please",
	"please wait while we", "verifying you are human", "security check in progress", "browser check",
}

// challengeMarkers are strings in the markup that only browser check pages carry.
var challengeMarkers = []string{
	"_cf_chl_opt", "cf-chl-", "cf_chl_", "/cdn-cgi/challenge-platform/h/", "challenges.cloudflare.com/cdn-cgi",
	"check.ddos-guard.net", "/.well-known/ddos-guard/", "ddos-guard/js-challenge",
	"sucuri_cloudproxy_js", "enable javascript and cookies to continue",
	"checking if the site connection is secure", "this process is automatic",
}

// humanMarkers are interactive check widgets: a person has to solve them.
var humanMarkers = []string{
	"hcaptcha.com/captcha", "class=\"h-captcha\"", "class=\"g-recaptcha\"", "google.com/recaptcha",
	"recaptcha/api.js", "hcaptcha.com/1/api.js", "verify you are human by completing",
	"complete the security check to access", "please solve this captcha", "i'm not a robot",
}

// blockedMarkers mean the site refuses the client; waiting does not help.
var blockedMarkers = []string{
	"sorry, you have been blocked", "error code: 1020", "access denied | ", "you do not have access to",
	"your ip has been banned", "error 1006", "error 1015",
}

// Classify looks at a page the way a person would: is this still a check
// screen, a screen that wants a person, a refusal, or the site itself.
func Classify(p Page) State {
	title := strings.ToLower(strings.TrimSpace(p.Title))
	html := strings.ToLower(p.HTML)
	if len(html) > 256<<10 {
		html = html[:256<<10]
	}
	html += "\n" + strings.ToLower(p.Text)
	check := p.Mitigated == "challenge"
	for _, t := range challengeTitles {
		if strings.Contains(title, t) {
			check = true
			break
		}
	}
	if !check {
		for _, m := range challengeMarkers {
			if strings.Contains(html, m) {
				check = true
				break
			}
		}
	}
	human := p.Widget
	if !human {
		for _, m := range humanMarkers {
			if strings.Contains(html, m) {
				human = true
				break
			}
		}
	}
	errorStatus := p.Status == 403 || p.Status == 503 || p.Status == 429
	if errorStatus && !check {
		for _, m := range blockedMarkers {
			if strings.Contains(html, m) || strings.Contains(title, m) {
				return StateBlocked
			}
		}
	}
	switch {
	case check && human:
		return StateHuman
	case check:
		return StateChallenge
	case human && errorStatus:
		// A CAPTCHA on an error page without the usual check markers.
		return StateHuman
	}
	return StateContent
}
