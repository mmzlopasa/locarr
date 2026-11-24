package logging

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// urlInText finds http(s) URLs inside free text (error messages).
var urlInText = regexp.MustCompile(`https?://[^\s"'<>\x60]+`)

// storedSecretParams are query parameters whose values never go into stored
// messages: credentials and the opaque download tokens indexers put in links.
var storedSecretParams = map[string]bool{
	"apikey": true, "api_key": true, "jackett_apikey": true, "key": true, "token": true, "access_token": true, "passkey": true,
	"pass": true, "password": true, "auth": true, "authkey": true, "sig": true, "signature": true, "secret": true, "session": true,
	"sid": true, "jwt": true, "rsskey": true, "torrent_pass": true, "link": true, "uid": true, "user": true,
}

// maxStoredParam is the longest query value kept as it is; longer ones are
// tokens more often than not.
const maxStoredParam = 32

// ScrubURL masks one URL for storage: user info goes, secret parameters and
// long values become [redacted], the fragment is dropped. Scheme, host, path
// and short harmless parameters stay, so the message still says where.
func ScrubURL(raw string) string {
	// Trailing punctuation belongs to the sentence, not the URL.
	trail := ""
	for len(raw) > 0 && strings.ContainsRune(".,;:)]}", rune(raw[len(raw)-1])) {
		trail = raw[len(raw)-1:] + trail
		raw = raw[:len(raw)-1]
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Scrub(raw) + trail
	}
	if u.User != nil {
		u.User = url.User("redacted")
	}
	u.Fragment, u.RawFragment = "", ""
	if u.RawQuery != "" {
		parts := strings.Split(u.RawQuery, "&")
		for i, p := range parts {
			k, v, _ := strings.Cut(p, "=")
			name, _ := url.QueryUnescape(k)
			if storedSecretParams[strings.ToLower(name)] || len(v) > maxStoredParam {
				if v != "" {
					parts[i] = k + "=" + Redacted
				}
			}
		}
		u.RawQuery = strings.Join(parts, "&")
	}
	return Scrub(u.String()) + trail
}

// ScrubStored masks a message before it is stored where people read it
// later (history, timelines, download events): every URL goes through
// ScrubURL and the rest through Scrub.
func ScrubStored(s string) string {
	if s == "" {
		return s
	}
	if strings.Contains(s, "://") {
		s = urlInText.ReplaceAllStringFunc(s, ScrubURL)
	}
	return Scrub(s)
}

// ScrubStoredJSON applies ScrubStored to every string inside a JSON document
// (event data). Invalid JSON is returned as it is.
func ScrubStoredJSON(raw []byte) []byte {
	if len(raw) == 0 || !strings.Contains(string(raw), "://") && !strings.Contains(strings.ToLower(string(raw)), "key=") &&
		!strings.Contains(strings.ToLower(string(raw)), "token") {
		return raw
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if dec.Decode(&v) != nil {
		return raw
	}
	out, err := json.Marshal(scrubValue(v))
	if err != nil {
		return raw
	}
	return out
}

func scrubValue(v any) any {
	switch x := v.(type) {
	case string:
		return ScrubStored(x)
	case []any:
		for i := range x {
			x[i] = scrubValue(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = scrubValue(x[k])
		}
		return x
	}
	return v
}
