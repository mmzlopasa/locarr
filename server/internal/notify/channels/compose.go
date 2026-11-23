package channels

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/notify"
)

// headlines are "<prefix>: <title>" per event type and language.
var headlines = map[string]map[notify.Type]string{
	"en": {
		notify.RequestCreated:        "New request",
		notify.RequestApproved:       "Request approved",
		notify.RequestDeclined:       "Request declined",
		notify.RequestAvailable:      "Now available",
		notify.RequestPartial:        "Partly available",
		notify.RequestFailed:         "Request stuck",
		notify.RequestComment:        "New comment on a request",
		notify.IssueCreated:          "Problem reported",
		notify.IssueComment:          "New comment on a problem",
		notify.IssueResolved:         "Problem resolved",
		notify.IssueReopened:         "Problem reopened",
		notify.LibraryGrabbed:        "Grabbed",
		notify.LibraryImported:       "Imported",
		notify.LibraryUpgraded:       "Upgraded",
		notify.LibraryDownloadFailed: "Download failed",
		notify.LibraryImportFailed:   "Import failed",
		notify.HealthWarning:         "Health warning",
		notify.SubtitleDownloaded:    "Subtitle downloaded",
		notify.SubtitleMissing:       "Subtitle still missing",
		notify.Test:                  "Test message from Locarr",
	},
	"hr": {
		notify.RequestCreated:        "Novi zahtjev",
		notify.RequestApproved:       "Zahtjev odobren",
		notify.RequestDeclined:       "Zahtjev odbijen",
		notify.RequestAvailable:      "Sada dostupno",
		notify.RequestPartial:        "Djelomično dostupno",
		notify.RequestFailed:         "Zahtjev je zapeo",
		notify.RequestComment:        "Novi komentar na zahtjev",
		notify.IssueCreated:          "Prijavljen problem",
		notify.IssueComment:          "Novi komentar na problem",
		notify.IssueResolved:         "Problem riješen",
		notify.IssueReopened:         "Problem ponovno otvoren",
		notify.LibraryGrabbed:        "Poslano na preuzimanje",
		notify.LibraryImported:       "Uvezeno",
		notify.LibraryUpgraded:       "Nadograđeno",
		notify.LibraryDownloadFailed: "Preuzimanje nije uspjelo",
		notify.LibraryImportFailed:   "Uvoz nije uspio",
		notify.HealthWarning:         "Upozorenje o stanju sustava",
		notify.SubtitleDownloaded:    "Titl preuzet",
		notify.SubtitleMissing:       "Titl još nedostaje",
		notify.Test:                  "Probna poruka iz Locarra",
	},
}

var words = map[string]map[string]string{
	"en": {"by": "By", "reason": "Reason", "release": "Release", "seasons": "Seasons", "test": "If you can read this, the channel works.",
		"open": "Open in Locarr"},
	"hr": {"by": "Korisnik", "reason": "Razlog", "release": "Izdanje", "seasons": "Sezone", "test": "Ako ovo vidite, kanal radi.",
		"open": "Otvori u Locarru"},
}

func lang(l string) string {
	if l == "hr" {
		return "hr"
	}
	return "en"
}

func text(l, key string) string { return words[lang(l)][key] }

// Compose renders ev for people in lang (en or hr) with links under baseURL.
func Compose(ev notify.Event, l, baseURL string) Message {
	l = lang(l)
	msg := Message{Event: ev, URL: link(ev, baseURL), PosterURL: PosterURL(ev.Poster)}
	prefix, ok := headlines[l][ev.Type]
	if !ok {
		prefix = string(ev.Type)
	}
	title := strings.TrimSpace(ev.Title)
	switch {
	case ev.Type == notify.Test || title == "":
		msg.Headline = prefix
	default:
		msg.Headline = prefix + ": " + title
	}
	if b, _ := ev.Params["is4k"].(bool); b {
		msg.Headline += " (4K)"
	}
	var lines []string
	if ev.Type == notify.Test {
		lines = append(lines, text(l, "test"))
	}
	if s := seasons(ev.Params["seasons"]); s != "" {
		lines = append(lines, text(l, "seasons")+": "+s)
	}
	if m := strings.TrimSpace(ev.Message); m != "" {
		switch ev.Type {
		case notify.RequestDeclined, notify.RequestFailed, notify.LibraryDownloadFailed, notify.LibraryImportFailed:
			lines = append(lines, text(l, "reason")+": "+m)
		case notify.LibraryGrabbed:
			lines = append(lines, text(l, "release")+": "+m)
		default:
			lines = append(lines, m)
		}
	}
	if ev.Actor != nil && ev.Actor.Username != "" {
		lines = append(lines, text(l, "by")+": "+ev.Actor.Username)
	}
	msg.Body = strings.Join(lines, "\n")
	return msg
}

func seasons(v any) string {
	var out []string
	switch s := v.(type) {
	case []int:
		for _, n := range s {
			out = append(out, strconv.Itoa(n))
		}
	case []int64:
		for _, n := range s {
			out = append(out, strconv.FormatInt(n, 10))
		}
	case []any:
		for _, n := range s {
			switch x := n.(type) {
			case float64:
				out = append(out, strconv.FormatFloat(x, 'f', -1, 64))
			case int:
				out = append(out, strconv.Itoa(x))
			case int64:
				out = append(out, strconv.FormatInt(x, 10))
			default:
				out = append(out, fmt.Sprint(x))
			}
		}
	}
	return strings.Join(out, ", ")
}

// link is the page in Locarr an event is about ("" without a base address).
func link(ev notify.Event, baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	switch {
	case ev.Type == notify.Test:
		return base + "/"
	case ev.RequestID > 0:
		return base + "/requests/" + strconv.FormatInt(ev.RequestID, 10)
	case ev.IssueID > 0:
		return base + "/issues/" + strconv.FormatInt(ev.IssueID, 10)
	}
	if kind, id, ok := strings.Cut(ev.ItemRef, ":"); ok && id != "" {
		if _, err := strconv.ParseInt(id, 10, 64); err == nil {
			switch kind {
			case "movie":
				return base + "/movies/" + id
			case "series":
				return base + "/series/" + id
			case "album":
				return base + "/music/albums/" + id
			case "artist":
				return base + "/music/artists/" + id
			}
		}
	}
	for _, t := range notify.LibraryTypes {
		if t == ev.Type {
			if t == notify.HealthWarning {
				return base + "/system/status"
			}
			return base + "/activity/queue"
		}
	}
	return base + "/"
}

// PosterURL turns a stored poster (a TMDb file or a Cover Art name) into a
// public image address ("" = none).
func PosterURL(poster string) string {
	switch {
	case strings.HasPrefix(poster, "/") && len(poster) > 1 && !strings.ContainsAny(poster, "?#") && !strings.Contains(poster[1:], "/"):
		return "https://image.tmdb.org/t/p/w342" + poster
	case strings.HasPrefix(poster, "rg-") && strings.HasSuffix(poster, ".jpg"):
		mbid := strings.TrimSuffix(strings.TrimPrefix(poster, "rg-"), ".jpg")
		if len(mbid) == 36 && strings.Count(mbid, "-") == 4 {
			return "https://coverartarchive.org/release-group/" + mbid + "/front-250"
		}
	}
	return ""
}
