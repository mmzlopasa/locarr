// Package sitefake serves recorded, trimmed answers of the built-in public
// sites, for the definition tests, the integration tests and the end-to-end
// fakes (locarr-fakes serves it under /sites). Each site lives under /<id>/:
// point a built-in indexer's address at http://<fake>/sites/<id>.
//
// The recordings were made from the sites' public pages and APIs and keep
// only titles and listing metadata.
package sitefake

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

//go:embed testdata
var files embed.FS

// Handler serves the recordings.
type Handler struct {
	// Requests counts every request served.
	Requests atomic.Int64
}

// New returns a Handler.
func New() *Handler { return &Handler{} }

// File returns one recording (for tests).
func File(site, name string) []byte {
	b, err := files.ReadFile("testdata/" + site + "/" + name)
	if err != nil {
		panic(err)
	}
	return b
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Requests.Add(1)
	site, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	rest = "/" + rest
	q := r.URL.Query()
	var name string
	switch site {
	case "yts":
		switch term := q.Get("query_term"); {
		case term == "tt0111161" || strings.Contains(strings.ToLower(term), "shawshank"):
			name = "imdb.json"
		case term == "":
			name = "latest.json"
		default:
			name = "empty.json"
		}
	case "eztv":
		switch id := q.Get("imdb_id"); id {
		case "944947", "0944947":
			name = "imdb.json"
		case "":
			name = "latest.json"
		default:
			name = `{"imdb_id":"` + id + `","torrents_count":0,"limit":100,"page":1}`
		}
	case "thepiratebay":
		switch {
		case strings.HasPrefix(rest, "/precompiled/"):
			name = "recent.json"
		case q.Get("q") == "tt0111161":
			name = "imdb.json"
		case strings.Contains(strings.ToLower(q.Get("q")), "ubuntu"):
			name = "search.json"
		default:
			name = "none.json"
		}
	case "subsplease":
		switch {
		case q.Get("f") == "latest":
			name = "latest.json"
		case strings.Contains(strings.ToLower(q.Get("s")), "frieren"):
			name = "search.json"
		default:
			name = "none.json"
		}
	case "knaben":
		var body struct {
			Query string `json:"query"`
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		_ = json.Unmarshal(raw, &body)
		switch {
		case r.Method != http.MethodPost || body.Query == "":
			name = "latest.json"
		case strings.Contains(strings.ToLower(body.Query), "ubuntu"):
			name = "search.json"
		default:
			name = "none.json"
		}
	case "nyaa":
		if t := strings.ToLower(q.Get("q")); t == "" || strings.Contains(t, "frieren") {
			name = "search.xml"
		} else {
			name = "empty.xml"
		}
	case "tokyotosho":
		name = "rss.xml"
	case "limetorrents":
		switch {
		case rest == "/rss/":
			name = "rss.xml"
		case strings.Contains(strings.ToLower(rest), "ubuntu"):
			name = "search.html"
		default:
			name = "empty.html"
		}
	case "1337x":
		switch {
		case strings.HasPrefix(rest, "/torrent/"):
			name = "details.html"
		case strings.Contains(strings.ToLower(rest), "ubuntu"):
			name = "search.html"
		default:
			name = "empty.html"
		}
	case "bitsearch":
		if strings.Contains(strings.ToLower(q.Get("q")), "ubuntu") {
			name = "search.html"
		} else {
			name = "empty.html"
		}
	default:
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(name, "{") {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, name)
		return
	}
	b, err := files.ReadFile("testdata/" + site + "/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".json"):
		w.Header().Set("Content-Type", "application/json")
	case strings.HasSuffix(name, ".xml"):
		w.Header().Set("Content-Type", "application/xml")
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, _ = w.Write(b)
}
