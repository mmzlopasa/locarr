package main

import "testing"

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Page
		want State
	}{
		{"plain content", Page{Status: 200, Title: "Search results", HTML: "<table class=table-list></table>"}, StateContent},
		{"content with bot script", Page{Status: 200, Title: "Home", HTML: `<script src="/cdn-cgi/challenge-platform/scripts/jsd/main.js"></script>`}, StateContent},
		{"cloudflare js check", Page{Status: 403, Title: "Just a moment...", HTML: `<script>window._cf_chl_opt={}</script>`}, StateChallenge},
		{"cf-mitigated header", Page{Status: 403, Mitigated: "challenge", Title: "x"}, StateChallenge},
		{"ddos-guard", Page{Status: 403, Title: "DDoS-Guard", HTML: "<script src='/.well-known/ddos-guard/js-challenge/index.js'>"}, StateChallenge},
		{"generic check", Page{Status: 503, Title: "One moment, please...", HTML: "<noscript>Enable JavaScript and cookies to continue</noscript>"}, StateChallenge},
		{"turnstile frame", Page{Status: 403, Title: "Just a moment...", Widget: true}, StateHuman},
		{"hcaptcha markup", Page{Status: 403, Title: "Just a moment...", HTML: `<div class="h-captcha" data-sitekey="x"></div>`}, StateHuman},
		{"visible text", Page{Status: 403, Title: "Just a moment...", Text: "Verify you are human by completing the action below."}, StateHuman},
		{"captcha on error page", Page{Status: 403, Title: "Access", HTML: `<script src="https://www.google.com/recaptcha/api.js"></script>`}, StateHuman},
		{"captcha form on a normal page", Page{Status: 200, Title: "Contact", HTML: `<div class="g-recaptcha"></div>`}, StateContent},
		{"blocked", Page{Status: 403, Title: "Attention Required! | Cloudflare", Text: "Sorry, you have been blocked"}, StateBlocked},
	} {
		if got := Classify(tc.p); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}
