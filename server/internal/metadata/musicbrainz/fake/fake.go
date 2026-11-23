// Package fake is a small MusicBrainz web service (ws/2, JSON) and Cover Art
// Archive for tests and the dev stack, written from their public
// documentation. It serves a fixed discography:
//
//	GET {prefix}/ws/2/artist?query=…                 search
//	GET {prefix}/ws/2/artist/{mbid}?inc=aliases+genres
//	GET {prefix}/ws/2/release-group?artist={mbid}    browse
//	GET {prefix}/ws/2/release-group/{mbid}?inc=artist-credits
//	GET {prefix}/ws/2/release-group?query=…          search (title words; artist:"…" narrows)
//	GET {prefix}/ws/2/release?artist=|release-group= browse (media, release groups)
//	GET {prefix}/ws/2/release/{mbid}?inc=recordings… lookup with tracks
//	GET {prefix}/coverart/release-group/{mbid}/front-{250|500|1200}  (redirects like the real one)
//	GET {prefix}/coverart/release/{mbid}/front-{size}
//
// The prefix is anything before /ws/2/ or /coverart/ (the dev fakes mount it at /musicbrainz).
package fake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/library/tags/tagtest"
)

// Artist ids of the catalog.
const (
	ArtistOpen  = "5a8e07d5-d932-4484-a7f7-e700793a9c94" // "Open Source Artist"
	ArtistOther = "5a8e07d5-d932-4484-a7f7-e700793a9c95" // "Open Source Artist" (jazz trio): the disambiguation case
)

// Release group and release ids of the catalog.
const (
	GroupFreeAlbum  = "8c1e5a3e-0f4d-4a9c-8c43-0000000000a1"
	GroupFreeEP     = "8c1e5a3e-0f4d-4a9c-8c43-0000000000a2"
	GroupLive       = "8c1e5a3e-0f4d-4a9c-8c43-0000000000a3"
	GroupSingle     = "8c1e5a3e-0f4d-4a9c-8c43-0000000000a4"
	GroupRemixes    = "8c1e5a3e-0f4d-4a9c-8c43-0000000000a5"
	GroupNextAlbum  = "8c1e5a3e-0f4d-4a9c-8c43-0000000000a6"
	GroupTrioAlbum  = "8c1e5a3e-0f4d-4a9c-8c43-0000000000b1"
	ReleaseStandard = "9d2f6b4f-1a5e-4bad-9d54-0000000000a1" // Free Album, digital, 5 tracks
	ReleaseDeluxe   = "9d2f6b4f-1a5e-4bad-9d54-0000000000a2" // Free Album, 2 CDs, 5 + 3 tracks
)

// Artist is a catalog artist.
type Artist struct {
	ID, Name, SortName, Disambiguation, Type, Country string
	Aliases                                           []string
	Genres                                            []string
}

// Group is a catalog release group.
type Group struct {
	ID, Artist, Title, Primary, Date string
	Secondary                        []string
}

// Track is a catalog track.
type Track struct {
	ID, Recording, Title string
	LengthMs             int
}

// Medium is a catalog medium.
type Medium struct {
	Format string
	Tracks []Track
}

// Release is a catalog release.
type Release struct {
	ID, Group, Title, Status, Date, Country, Disambiguation, Barcode, Label string
	Media                                                                   []Medium
}

func tracks(prefix string, titles ...string) []Track {
	out := make([]Track, 0, len(titles))
	for i, t := range titles {
		out = append(out, Track{ID: fmt.Sprintf("7e3a9c1d-2b4f-4e6a-8c9d-%s%04d", prefix, i+1),
			Recording: fmt.Sprintf("6f4b8d2e-3c5a-4f7b-9dae-%s%04d", prefix, i+1), Title: t, LengthMs: 150_000 + 17_000*(i+1)})
	}
	return out
}

// Catalog.
var (
	Artists = []Artist{
		{ID: ArtistOpen, Name: "Open Source Artist", SortName: "Open Source Artist", Disambiguation: "electronic duo", Type: "Group",
			Country: "XW", Aliases: []string{"OSA", "Otvoreni Izvor"}, Genres: []string{"electronic", "ambient"}},
		{ID: ArtistOther, Name: "Open Source Artist", SortName: "Open Source Artist", Disambiguation: "jazz trio from Zagreb", Type: "Group",
			Country: "HR", Genres: []string{"jazz"}},
	}
	Groups = []Group{
		{ID: GroupFreeAlbum, Artist: ArtistOpen, Title: "Free Album", Primary: "Album", Date: "2020-03-14"},
		{ID: GroupFreeEP, Artist: ArtistOpen, Title: "Free EP", Primary: "EP", Date: "2021-06-01"},
		{ID: GroupLive, Artist: ArtistOpen, Title: "Live at the Commons", Primary: "Album", Secondary: []string{"Live"}, Date: "2022-09-09"},
		{ID: GroupSingle, Artist: ArtistOpen, Title: "Free Single", Primary: "Single", Date: "2019-01-10"},
		{ID: GroupRemixes, Artist: ArtistOpen, Title: "Free Remixes", Primary: "Album", Secondary: []string{"Compilation", "Remix"}, Date: "2023-02-02"},
		{ID: GroupNextAlbum, Artist: ArtistOpen, Title: "Next Album", Primary: "Album", Date: "2030-01-01"},
		{ID: GroupTrioAlbum, Artist: ArtistOther, Title: "Trio Sessions", Primary: "Album", Date: "2015-05-05"},
	}
	freeAlbum = tracks("0000000a", "Opening", "Second Light", "Žuta Kuća", "Night Bus", "Closing")
	Releases  = []Release{
		{ID: ReleaseStandard, Group: GroupFreeAlbum, Title: "Free Album", Status: "Official", Date: "2020-03-14", Country: "XW",
			Barcode: "0000000000001", Label: "Commons Records", Media: []Medium{{Format: "Digital Media", Tracks: freeAlbum}}},
		{ID: ReleaseDeluxe, Group: GroupFreeAlbum, Title: "Free Album", Status: "Official", Date: "2020-11-20", Country: "GB",
			Disambiguation: "deluxe edition", Label: "Commons Records",
			Media: []Medium{{Format: "CD", Tracks: freeAlbum}, {Format: "CD", Tracks: tracks("0000000b", "Bonus One", "Bonus Two", "Bonus Three")}}},
		{ID: "9d2f6b4f-1a5e-4bad-9d54-0000000000a3", Group: GroupFreeEP, Title: "Free EP", Status: "Official", Date: "2021-06-01", Country: "XW",
			Media: []Medium{{Format: "Digital Media", Tracks: tracks("0000000c", "EP One", "EP Two", "EP Three")}}},
		{ID: "9d2f6b4f-1a5e-4bad-9d54-0000000000a4", Group: GroupLive, Title: "Live at the Commons", Status: "Official", Date: "2022-09-09",
			Country: "XW", Media: []Medium{{Format: "Digital Media", Tracks: tracks("0000000d", "Intro (live)", "Second Light (live)", "Night Bus (live)", "Encore")}}},
		{ID: "9d2f6b4f-1a5e-4bad-9d54-0000000000a5", Group: GroupSingle, Title: "Free Single", Status: "Official", Date: "2019-01-10", Country: "XW",
			Media: []Medium{{Format: "Digital Media", Tracks: tracks("0000000e", "Free Single")}}},
		{ID: "9d2f6b4f-1a5e-4bad-9d54-0000000000a6", Group: GroupRemixes, Title: "Free Remixes", Status: "Official", Date: "2023-02-02", Country: "XW",
			Media: []Medium{{Format: "Digital Media", Tracks: tracks("0000000f", "Opening (remix)", "Night Bus (remix)")}}},
		{ID: "9d2f6b4f-1a5e-4bad-9d54-0000000000a7", Group: GroupRemixes, Title: "Free Remixes", Status: "Bootleg", Date: "2023-03-03", Country: "DE",
			Media: []Medium{{Format: "CD", Tracks: tracks("00000010", "Opening (remix)", "Night Bus (remix)", "Unknown")}}},
		{ID: "9d2f6b4f-1a5e-4bad-9d54-0000000000a8", Group: GroupNextAlbum, Title: "Next Album", Status: "Official", Date: "2030-01-01", Country: "XW",
			Media: []Medium{{Format: "Digital Media", Tracks: tracks("00000011", "Future One", "Future Two")}}},
		{ID: "9d2f6b4f-1a5e-4bad-9d54-0000000000b1", Group: GroupTrioAlbum, Title: "Trio Sessions", Status: "Official", Date: "2015-05-05", Country: "HR",
			Media: []Medium{{Format: "CD", Tracks: tracks("00000012", "Standard", "Blue")}}},
	}
)

// Server is the fake.
type Server struct {
	mu sync.Mutex
	// MinInterval answers 503 to requests closer together than this (like
	// MusicBrainz does to clients over the limit); 0 = never.
	MinInterval time.Duration
	// Requests are the arrival times of ws/2 requests; UserAgents what they sent.
	Requests   []time.Time
	UserAgents []string
	// Violations counts requests refused for coming too fast.
	Violations int
	// Extra artists, groups and releases (tests add to the catalog).
	ExtraArtists  []Artist
	ExtraGroups   []Group
	ExtraReleases []Release
}

// New returns a fake.
func New() *Server { return &Server{} }

func (s *Server) artists() []Artist {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(append([]Artist{}, Artists...), s.ExtraArtists...)
}

func (s *Server) groups() []Group {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(append([]Group{}, Groups...), s.ExtraGroups...)
}

func (s *Server) releases() []Release {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(append([]Release{}, Releases...), s.ExtraReleases...)
}

// Count is the number of ws/2 requests so far.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Requests)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func errorJSON(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg, "help": "For usage, please see: https://musicbrainz.org/development/mmd"})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if i := strings.Index(p, "/coverart/"); i >= 0 {
		s.cover(w, r, p[i+len("/coverart/"):])
		return
	}
	i := strings.Index(p, "/ws/2/")
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	now := time.Now()
	s.mu.Lock()
	tooFast := s.MinInterval > 0 && len(s.Requests) > 0 && now.Sub(s.Requests[len(s.Requests)-1]) < s.MinInterval
	s.Requests = append(s.Requests, now)
	s.UserAgents = append(s.UserAgents, r.Header.Get("User-Agent"))
	if tooFast {
		s.Violations++
	}
	s.mu.Unlock()
	if tooFast {
		w.Header().Set("Retry-After", "1")
		errorJSON(w, http.StatusServiceUnavailable, "Your requests are exceeding the allowable rate limit.")
		return
	}
	if r.Header.Get("User-Agent") == "" {
		errorJSON(w, http.StatusForbidden, "a User-Agent is required")
		return
	}
	if f := r.URL.Query().Get("fmt"); f != "json" {
		errorJSON(w, http.StatusBadRequest, "only fmt=json is served by the fake")
		return
	}
	rest := strings.Trim(p[i+len("/ws/2/"):], "/")
	entity, id, _ := strings.Cut(rest, "/")
	q := r.URL.Query()
	switch {
	case entity == "artist" && id == "":
		s.searchArtists(w, q.Get("query"))
	case entity == "artist":
		s.artist(w, strings.ToLower(id))
	case entity == "release-group" && id == "" && q.Get("query") != "":
		s.searchGroups(w, q.Get("query"))
	case entity == "release-group" && id != "":
		g, ok := s.group(strings.ToLower(id))
		if !ok {
			errorJSON(w, http.StatusNotFound, "Not Found")
			return
		}
		a := s.artistOf(g.Artist)
		out := groupJSON(g)
		out["artist-credit"] = []map[string]any{{"name": a.Name, "artist": map[string]any{"id": a.ID, "name": a.Name}}}
		writeJSON(w, out)
	case entity == "release-group" && id == "":
		s.browseGroups(w, strings.ToLower(q.Get("artist")), q)
	case entity == "release" && id == "":
		s.browseReleases(w, q)
	case entity == "release":
		s.release(w, strings.ToLower(id))
	default:
		errorJSON(w, http.StatusBadRequest, "unsupported request")
	}
}

func unescape(q string) string {
	var b strings.Builder
	esc := false
	for _, r := range q {
		if r == '\\' && !esc {
			esc = true
			continue
		}
		esc = false
		b.WriteRune(r)
	}
	return b.String()
}

func artistJSON(a Artist, score int) map[string]any {
	aliases := []map[string]any{}
	for _, al := range a.Aliases {
		aliases = append(aliases, map[string]any{"name": al, "sort-name": al, "type": "Artist name", "locale": nil, "primary": nil})
	}
	genres := []map[string]any{}
	for _, g := range a.Genres {
		genres = append(genres, map[string]any{"name": g, "count": 3})
	}
	out := map[string]any{"id": a.ID, "name": a.Name, "sort-name": a.SortName, "disambiguation": a.Disambiguation, "type": a.Type,
		"country": a.Country, "life-span": map[string]any{"begin": "2010", "ended": false}, "aliases": aliases, "genres": genres}
	if score > 0 {
		out["score"] = score
	}
	return out
}

func (s *Server) searchArtists(w http.ResponseWriter, query string) {
	term := strings.ToLower(strings.TrimSpace(unescape(query)))
	list := []map[string]any{}
	for _, a := range s.artists() {
		names := append([]string{a.Name}, a.Aliases...)
		score := 0
		for _, n := range names {
			n = strings.ToLower(n)
			switch {
			case n == term:
				score = max(score, 100)
			case term != "" && strings.Contains(n, term):
				score = max(score, 80)
			}
		}
		if score > 0 {
			if a.ID == ArtistOther {
				score -= 10
			}
			list = append(list, artistJSON(a, score))
		}
	}
	writeJSON(w, map[string]any{"created": time.Now().UTC().Format(time.RFC3339), "count": len(list), "offset": 0, "artists": list})
}

func (s *Server) artist(w http.ResponseWriter, id string) {
	for _, a := range s.artists() {
		if a.ID == id {
			writeJSON(w, artistJSON(a, 0))
			return
		}
	}
	errorJSON(w, http.StatusNotFound, "Not Found")
}

func groupJSON(g Group) map[string]any {
	sec := g.Secondary
	if sec == nil {
		sec = []string{}
	}
	return map[string]any{"id": g.ID, "title": g.Title, "primary-type": g.Primary, "secondary-types": sec, "first-release-date": g.Date,
		"disambiguation": ""}
}

func page[T any](list []T, q map[string][]string) ([]T, int) {
	get := func(k string) int {
		if v, ok := q[k]; ok && len(v) > 0 {
			n, _ := strconv.Atoi(v[0])
			return n
		}
		return 0
	}
	off, limit := get("offset"), get("limit")
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	total := len(list)
	if off >= total {
		return nil, total
	}
	return list[off:min(total, off+limit)], total
}

func (s *Server) browseGroups(w http.ResponseWriter, artist string, q map[string][]string) {
	var list []map[string]any
	for _, g := range s.groups() {
		if g.Artist == artist {
			list = append(list, groupJSON(g))
		}
	}
	items, total := page(list, q)
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, map[string]any{"release-group-count": total, "release-group-offset": 0, "release-groups": items})
}

func (s *Server) group(id string) (Group, bool) {
	for _, g := range s.groups() {
		if g.ID == id {
			return g, true
		}
	}
	return Group{}, false
}

func (s *Server) artistOf(id string) Artist {
	for _, a := range s.artists() {
		if a.ID == id {
			return a
		}
	}
	return Artist{}
}

func (s *Server) releaseJSON(r Release, withTracks bool) map[string]any {
	g, _ := s.group(r.Group)
	a := s.artistOf(g.Artist)
	credit := []map[string]any{{"name": a.Name, "joinphrase": "", "artist": map[string]any{"id": a.ID, "name": a.Name}}}
	media := []map[string]any{}
	for i, m := range r.Media {
		md := map[string]any{"position": i + 1, "format": m.Format, "title": "", "track-count": len(m.Tracks), "track-offset": 0}
		if withTracks {
			ts := []map[string]any{}
			for j, t := range m.Tracks {
				ts = append(ts, map[string]any{"id": t.ID, "number": strconv.Itoa(j + 1), "position": j + 1, "title": t.Title, "length": t.LengthMs,
					"recording": map[string]any{"id": t.Recording, "title": t.Title, "length": t.LengthMs}, "artist-credit": credit})
			}
			md["tracks"] = ts
		}
		media = append(media, md)
	}
	out := map[string]any{"id": r.ID, "title": r.Title, "status": r.Status, "date": r.Date, "country": r.Country, "disambiguation": r.Disambiguation,
		"barcode": r.Barcode, "media": media, "release-group": groupJSON(g), "artist-credit": credit,
		"label-info": []map[string]any{{"catalog-number": "CR-" + r.ID[len(r.ID)-3:], "label": map[string]any{"name": r.Label}}}}
	return out
}

func (s *Server) browseReleases(w http.ResponseWriter, q map[string][]string) {
	first := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return strings.ToLower(v[0])
		}
		return ""
	}
	artist, group := first("artist"), first("release-group")
	var list []map[string]any
	for _, r := range s.releases() {
		g, _ := s.group(r.Group)
		if (artist != "" && g.Artist == artist) || (group != "" && r.Group == group) {
			list = append(list, s.releaseJSON(r, false))
		}
	}
	items, total := page(list, q)
	if items == nil {
		items = []map[string]any{}
	}
	writeJSON(w, map[string]any{"release-count": total, "release-offset": 0, "releases": items})
}

func (s *Server) release(w http.ResponseWriter, id string) {
	for _, r := range s.releases() {
		if r.ID == id {
			writeJSON(w, s.releaseJSON(r, true))
			return
		}
	}
	errorJSON(w, http.StatusNotFound, "Not Found")
}

// Cover returns the fake cover JPEG for an id (a small coloured square).
func Cover(id string) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	h := byte(len(id)) // #nosec G115 -- a hash seed
	for _, c := range id {
		h = h*31 + byte(c&0xFF) // #nosec G115 -- masked
	}
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			img.Set(x, y, color.RGBA{R: h, G: byte(x * 16), B: byte(y * 16), A: 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 80})
	return b.Bytes()
}

func (s *Server) cover(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "files":
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(Cover(strings.TrimSuffix(parts[1], ".jpg")))
	case len(parts) == 3 && (parts[0] == "release-group" || parts[0] == "release") && strings.HasPrefix(parts[2], "front"):
		id := strings.ToLower(parts[1])
		ok := false
		if parts[0] == "release-group" {
			_, ok = s.group(id)
		} else {
			for _, rel := range s.releases() {
				ok = ok || rel.ID == id
			}
		}
		if !ok || id == GroupNextAlbum {
			http.NotFound(w, r)
			return
		}
		// The real archive redirects to its file storage.
		prefix := r.URL.Path[:strings.Index(r.URL.Path, "/coverart/")]
		http.Redirect(w, r, prefix+"/coverart/files/"+id+".jpg", http.StatusTemporaryRedirect) // #nosec G710 -- same host, id checked against the catalog
	default:
		http.NotFound(w, r)
	}
}

// File is one file of a fake download.
type File struct {
	Path string // relative, "/" separated
	Data []byte
}

// FindRelease returns a catalog release.
func FindRelease(id string) (Release, Group, Artist, bool) {
	for _, r := range Releases {
		if r.ID == id {
			for _, g := range Groups {
				if g.ID == r.Group {
					for _, a := range Artists {
						if a.ID == g.Artist {
							return r, g, a, true
						}
					}
				}
			}
		}
	}
	return Release{}, Group{}, Artist{}, false
}

// FileOptions shape a fake download.
type FileOptions struct {
	// IDs adds the MusicBrainz ids to the tags.
	IDs bool
	// Format is "flac" (default), "flac24" or "mp3" (320 kbit/s).
	Format string
	// Skip leaves out this many tracks from the end (an incomplete download).
	Skip int
}

// ReleaseFiles builds a download of a release: tagged audio files, "Disc N"
// folders for several media, and a cover.
func ReleaseFiles(releaseID string, o FileOptions) []File {
	rel, g, a, ok := FindRelease(releaseID)
	if !ok {
		return nil
	}
	var out []File
	total := 0
	for _, m := range rel.Media {
		total += len(m.Tracks)
	}
	n := 0
	for d, m := range rel.Media {
		for i, t := range m.Tracks {
			n++
			if n > total-o.Skip {
				break
			}
			f := tagtest.Fields{"TITLE": t.Title, "ARTIST": a.Name, "ALBUMARTIST": a.Name, "ALBUM": rel.Title, "DATE": rel.Date,
				"TRACKNUMBER": fmt.Sprintf("%d/%d", i+1, len(m.Tracks)), "DISCNUMBER": fmt.Sprintf("%d/%d", d+1, len(rel.Media))}
			if o.IDs {
				f["MUSICBRAINZ_TRACKID"], f["MUSICBRAINZ_RELEASETRACKID"] = t.Recording, t.ID
				f["MUSICBRAINZ_ALBUMID"], f["MUSICBRAINZ_RELEASEGROUPID"], f["MUSICBRAINZ_ARTISTID"] = rel.ID, g.ID, a.ID
			}
			st := tagtest.Stream{Duration: time.Duration(t.LengthMs) * time.Millisecond, Filler: 8192}
			ext := "flac"
			var data []byte
			switch o.Format {
			case "mp3":
				ext = "mp3"
				st.Bitrate = 320
				data = tagtest.MP3(4, f, st)
			case "flac24":
				st.BitDepth = 24
				data = tagtest.FLAC(f, st)
			default:
				data = tagtest.FLAC(f, st)
			}
			name := fmt.Sprintf("%02d. %s - %s.%s", i+1, a.Name, t.Title, ext)
			if len(rel.Media) > 1 {
				name = fmt.Sprintf("Disc %d/%s", d+1, name)
			}
			out = append(out, File{Path: name, Data: data})
		}
	}
	out = append(out, File{Path: "cover.jpg", Data: Cover(rel.ID)})
	return out
}

// searchGroups matches release groups whose title (or artist name) contains
// a word of the query.
func (s *Server) searchGroups(w http.ResponseWriter, query string) {
	q := strings.ToLower(unescape(query))
	for _, junk := range []string{"releasegroup:", "artist:", "\"", " and "} {
		q = strings.ReplaceAll(q, junk, " ")
	}
	words := strings.Fields(q)
	list := []map[string]any{}
	for _, g := range s.groups() {
		a := s.artistOf(g.Artist)
		hay := strings.ToLower(g.Title + " " + a.Name)
		hits := 0
		for _, wd := range words {
			if strings.Contains(hay, wd) {
				hits++
			}
		}
		if len(words) == 0 || hits < len(words) {
			continue
		}
		out := groupJSON(g)
		out["score"] = 100
		out["artist-credit"] = []map[string]any{{"name": a.Name, "artist": map[string]any{"id": a.ID, "name": a.Name}}}
		list = append(list, out)
	}
	writeJSON(w, map[string]any{"created": time.Now().UTC().Format(time.RFC3339), "count": len(list), "offset": 0, "release-groups": list})
}
