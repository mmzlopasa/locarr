// Package fake serves stand-ins for the applications Locarr imports from,
// written from their public API documentation: Sonarr and Radarr (API v3),
// Lidarr and Prowlarr (API v1), Bazarr, Overseerr/Seerr, the qBittorrent
// WebUI API v2 and the SABnzbd API. Only the read endpoints the importer uses
// exist; every request that is not a GET (except qBittorrent's sign-in) is
// counted in Mutations so tests can prove nothing was changed.
package fake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Server is one fake application.
type Server struct {
	Kind     string
	APIKey   string
	Username string
	Password string
	Version  string
	Lib      *Library
	// Mutations counts requests that would change something.
	Mutations atomic.Int64
	Hits      atomic.Int64
	// Prefix is the URL base the server is mounted under ("" or "/sonarr").
	Prefix string
}

// New returns a fake of kind serving lib.
func New(kind, apiKey string, lib *Library) *Server {
	s := &Server{Kind: kind, APIKey: apiKey, Lib: lib}
	s.Version = map[string]string{"sonarr": "4.0.11.2680", "radarr": "5.14.0.9383", "lidarr": "2.8.2.4493", "prowlarr": "1.28.2.4885",
		"bazarr": "1.4.5", "overseerr": "1.33.2", "seerr": "2.7.3", "qbittorrent": "v5.0.2", "sabnzbd": "4.4.1"}[kind]
	return s
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Hits.Add(1)
	p := strings.TrimPrefix(r.URL.Path, s.Prefix)
	if s.Kind == "qbittorrent" && p == "/api/v2/auth/login" && r.Method == http.MethodPost {
		_ = r.ParseForm()
		if r.PostForm.Get("username") == s.Username && r.PostForm.Get("password") == s.Password {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fake-session", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
			return
		}
		_, _ = w.Write([]byte("Fails."))
		return
	}
	if r.Method != http.MethodGet {
		s.Mutations.Add(1)
		http.Error(w, "read-only fake", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r, p) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var v any
	var ok bool
	switch s.Kind {
	case "sonarr", "radarr", "lidarr", "prowlarr":
		v, ok = s.arr(r, p)
	case "bazarr":
		v, ok = s.bazarr(r, p)
	case "overseerr", "seerr":
		v, ok = s.seerr(r, p)
	case "qbittorrent":
		if p == "/api/v2/app/version" || p == "/api/v2/app/webapiVersion" {
			_, _ = w.Write([]byte(map[bool]string{true: s.Version, false: "2.11.2"}[p == "/api/v2/app/version"]))
			return
		}
		v, ok = s.qbit(p)
	case "sabnzbd":
		v, ok = s.sab(r)
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, v)
}

func (s *Server) authorized(r *http.Request, p string) bool {
	switch s.Kind {
	case "qbittorrent":
		if s.Username == "" && s.Password == "" {
			return true
		}
		c, err := r.Cookie("SID")
		return err == nil && c.Value == "fake-session"
	case "sabnzbd":
		return r.URL.Query().Get("mode") == "version" || r.URL.Query().Get("apikey") == s.APIKey
	case "bazarr":
		return r.Header.Get("X-API-KEY") == s.APIKey
	case "overseerr", "seerr":
		return p == "/api/v1/status" || r.Header.Get("X-Api-Key") == s.APIKey
	}
	return r.Header.Get("X-Api-Key") == s.APIKey || r.URL.Query().Get("apikey") == s.APIKey
}

func field(name string, value any) map[string]any {
	return map[string]any{"name": name, "value": value}
}

func qid(r *http.Request, k string) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get(k), 10, 64)
	return n
}

func iso(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (s *Server) appName() string {
	return map[string]string{"sonarr": "Sonarr", "radarr": "Radarr", "lidarr": "Lidarr", "prowlarr": "Prowlarr"}[s.Kind]
}

func qualityRef(id int, name string) map[string]any { return map[string]any{"id": id, "name": name} }

func (s *Server) arr(r *http.Request, p string) (any, bool) {
	base := "/api/v3"
	if s.Kind == "lidarr" || s.Kind == "prowlarr" {
		base = "/api/v1"
	}
	if !strings.HasPrefix(p, base+"/") {
		return nil, false
	}
	rest := strings.TrimPrefix(p, base)
	lib := s.Lib
	switch rest {
	case "/system/status":
		return map[string]any{"appName": s.appName(), "version": s.Version, "isDocker": true, "urlBase": s.Prefix}, true
	case "/tag":
		out := []any{}
		for _, id := range sortedKeys(lib.Tags) {
			out = append(out, map[string]any{"id": id, "label": lib.Tags[id]})
		}
		return out, true
	case "/qualityprofile":
		return lib.qualityProfiles(s.Kind), true
	case "/qualitydefinition":
		return lib.qualityDefinitions(s.Kind), true
	case "/customformat":
		if s.Kind == "lidarr" {
			return nil, false
		}
		return lib.customFormats(s.Kind), true
	case "/languageprofile":
		if s.Kind == "sonarr" && strings.HasPrefix(s.Version, "3") {
			return []any{map[string]any{"id": 1, "name": "English", "languages": []any{
				map[string]any{"language": qualityRef(1, "English"), "allowed": true},
				map[string]any{"language": qualityRef(39, "Croatian"), "allowed": true},
				map[string]any{"language": qualityRef(2, "French"), "allowed": false}}}}, true
		}
		return nil, false
	case "/releaseprofile":
		return []any{map[string]any{"id": 1, "name": "No x265 cams", "enabled": true, "required": []string{},
			"ignored": []string{"CAM", "TELESYNC"}, "indexerId": 0, "tags": []int{}}}, true
	case "/delayprofile":
		return []any{
			map[string]any{"id": 1, "enableUsenet": true, "enableTorrent": true, "preferredProtocol": "usenet", "usenetDelay": 0,
				"torrentDelay": 60, "bypassIfHighestQuality": true, "bypassIfAboveCustomFormatScore": false, "minimumCustomFormatScore": 0,
				"order": 2147483647, "tags": []int{}},
			map[string]any{"id": 2, "enableUsenet": false, "enableTorrent": true, "preferredProtocol": "torrent", "usenetDelay": 0,
				"torrentDelay": 120, "bypassIfHighestQuality": false, "order": 1, "tags": []int{2}}}, true
	case "/rootfolder":
		out := []any{}
		for i, rf := range lib.roots(s.Kind) {
			out = append(out, map[string]any{"id": i + 1, "path": rf, "accessible": true, "freeSpace": 1 << 40})
		}
		return out, true
	case "/config/naming":
		return lib.naming(s.Kind), true
	case "/indexer":
		if s.Kind == "prowlarr" {
			return lib.prowlarrIndexers(), true
		}
		return lib.arrIndexers(s.Kind), true
	case "/applications":
		if s.Kind != "prowlarr" {
			return nil, false
		}
		return []any{map[string]any{"id": 1, "name": "Sonarr", "implementation": "Sonarr", "syncLevel": "fullSync",
			"fields": []any{field("prowlarrUrl", "http://prowlarr:9696"), field("baseUrl", "http://sonarr:8989"), field("apiKey", "********")}}}, true
	case "/downloadclient":
		return lib.arrClients(s.Kind), true
	case "/remotepathmapping":
		return []any{map[string]any{"id": 1, "host": "qbittorrent", "remotePath": "/downloads/", "localPath": "/data/downloads/"}}, true
	case "/importlist":
		if s.Kind == "radarr" {
			return []any{
				map[string]any{"id": 1, "name": "Blender films", "implementation": "TMDbListImport", "enabled": true, "enableAuto": true,
					"rootFolderPath": lib.MovieRoot, "qualityProfileId": 1, "minimumAvailability": "released", "monitor": "movieOnly",
					"searchOnAdd": false, "tags": []int{}, "fields": []any{field("listId", "8250")}},
				map[string]any{"id": 2, "name": "Trakt trending", "implementation": "TraktListImport", "enabled": true,
					"rootFolderPath": lib.MovieRoot, "qualityProfileId": 1, "tags": []int{}, "fields": []any{field("listName", "trending")}}}, true
		}
		return []any{}, true
	case "/exclusions", "/importlistexclusion":
		switch s.Kind {
		case "radarr":
			if rest == "/importlistexclusion" {
				return nil, false
			}
			return []any{map[string]any{"id": 1, "tmdbId": 99999, "movieTitle": "Excluded Film", "movieYear": 2001}}, true
		case "sonarr":
			return []any{map[string]any{"id": 1, "tvdbId": 88888, "title": "Excluded Show"}}, true
		case "lidarr":
			return []any{map[string]any{"id": 1, "foreignId": "00000000-0000-0000-0000-00000000dead", "artistName": "Excluded"}}, true
		}
	case "/collection":
		if s.Kind == "radarr" {
			return []any{map[string]any{"id": 1, "title": "Open Movie Collection", "tmdbId": 777001, "monitored": true,
				"qualityProfileId": 1, "rootFolderPath": lib.MovieRoot, "minimumAvailability": "released", "searchOnAdd": false, "tags": []int{}}}, true
		}
	case "/movie":
		if s.Kind == "radarr" {
			return lib.radarrMovies(), true
		}
	case "/series":
		if s.Kind == "sonarr" {
			return lib.sonarrSeries(), true
		}
	case "/episode":
		if s.Kind == "sonarr" {
			return lib.sonarrEpisodes(qid(r, "seriesId")), true
		}
	case "/episodefile":
		if s.Kind == "sonarr" {
			return lib.sonarrFiles(qid(r, "seriesId")), true
		}
	case "/artist":
		if s.Kind == "lidarr" {
			return lib.lidarrArtists(), true
		}
	case "/album":
		if s.Kind == "lidarr" {
			return lib.lidarrAlbums(), true
		}
	case "/trackfile":
		if s.Kind == "lidarr" {
			return lib.lidarrTrackFiles(qid(r, "artistId")), true
		}
	case "/track":
		if s.Kind == "lidarr" {
			return lib.lidarrTracks(qid(r, "artistId")), true
		}
	case "/metadataprofile":
		if s.Kind == "lidarr" {
			return []any{map[string]any{"id": 1, "name": "Standard",
				"primaryAlbumTypes": []any{map[string]any{"albumType": qualityRef(0, "Album"), "allowed": true},
					map[string]any{"albumType": qualityRef(1, "EP"), "allowed": true}, map[string]any{"albumType": qualityRef(2, "Single"), "allowed": false}},
				"secondaryAlbumTypes": []any{map[string]any{"albumType": qualityRef(0, "Studio"), "allowed": true},
					map[string]any{"albumType": qualityRef(3, "Live"), "allowed": true}},
				"releaseStatuses": []any{map[string]any{"releaseStatus": qualityRef(0, "Official"), "allowed": true}}}}, true
		}
	case "/blocklist":
		return paged(r, lib.blocklist(s.Kind)), true
	case "/history":
		return paged(r, lib.history(s.Kind)), true
	}
	return nil, false
}

func paged(r *http.Request, all []any) map[string]any {
	page, size := int(qid(r, "page")), int(qid(r, "pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}
	from := min((page-1)*size, len(all))
	to := min(from+size, len(all))
	return map[string]any{"page": page, "pageSize": size, "sortKey": "date", "sortDirection": "descending", "totalRecords": len(all),
		"records": all[from:to]}
}

func sortedKeys(m map[int64]string) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *Server) bazarr(r *http.Request, p string) (any, bool) {
	lib := s.Lib
	switch p {
	case "/api/system/status":
		return map[string]any{"data": map[string]any{"bazarr_version": s.Version, "python_version": "3.12.7"}}, true
	case "/api/system/languages/profiles":
		return []any{
			map[string]any{"profileId": 1, "name": "Croatian + English", "cutoff": nil, "items": []any{
				map[string]any{"id": 1, "language": "hr", "forced": "False", "hi": "False", "audio_exclude": "False"},
				map[string]any{"id": 2, "language": "en", "forced": "False", "hi": "True", "audio_exclude": "False"}},
				"mustContain": []string{}, "mustNotContain": []string{}, "originalFormat": false},
			map[string]any{"profileId": 2, "name": "Forced English", "cutoff": 65535, "items": []any{
				map[string]any{"id": 1, "language": "en", "forced": "True", "hi": "False"}}}}, true
	case "/api/system/settings":
		return map[string]any{
			"general":          map[string]any{"enabled_providers": []string{"opensubtitlescom", "subdl", "podnapisi", "addic7ed"}},
			"opensubtitlescom": map[string]any{"username": "subs-user", "password": "********", "use_hash": true},
			"subdl":            map[string]any{"api_key": "subdl-key-123"},
			"addic7ed":         map[string]any{"username": "x", "password": "y"},
		}, true
	case "/api/movies":
		out := []any{}
		for i, m := range lib.Movies {
			if i%2 == 0 {
				out = append(out, map[string]any{"radarrId": m.ID, "title": m.Title, "profileId": 1, "imdbId": m.IMDb})
			}
		}
		return map[string]any{"data": out, "total": len(out)}, true
	case "/api/series":
		out := []any{}
		for _, v := range lib.Series {
			out = append(out, map[string]any{"sonarrSeriesId": v.ID, "title": v.Title, "profileId": 2, "tvdbId": v.TVDB})
		}
		return map[string]any{"data": out, "total": len(out)}, true
	}
	return nil, false
}

func (s *Server) seerr(r *http.Request, p string) (any, bool) {
	lib := s.Lib
	switch {
	case p == "/api/v1/status":
		return map[string]any{"version": s.Version, "commitTag": "local", "updateAvailable": false}, true
	case p == "/api/v1/settings/main":
		return map[string]any{"applicationTitle": "Requests", "defaultPermissions": 32,
			"defaultQuotas": map[string]any{"movie": map[string]any{"quotaLimit": 5, "quotaDays": 7}, "tv": map[string]any{"quotaLimit": 2, "quotaDays": 7}}}, true
	case p == "/api/v1/user":
		return seerrPage(r, lib.seerrUsers()), true
	case p == "/api/v1/request":
		return seerrPage(r, lib.seerrRequests()), true
	case p == "/api/v1/issue":
		return seerrPage(r, lib.seerrIssues()), true
	case strings.HasPrefix(p, "/api/v1/movie/"):
		id, _ := strconv.ParseInt(path.Base(p), 10, 64)
		for _, m := range lib.Movies {
			if m.TMDb == id {
				return map[string]any{"id": id, "title": m.Title, "releaseDate": fmt.Sprintf("%d-05-01", m.Year)}, true
			}
		}
		return map[string]any{"id": id, "title": fmt.Sprintf("Requested Film %d", id), "releaseDate": "2024-01-01"}, true
	case strings.HasPrefix(p, "/api/v1/tv/"):
		id, _ := strconv.ParseInt(path.Base(p), 10, 64)
		for _, v := range lib.Series {
			if v.TMDb == id {
				return map[string]any{"id": id, "name": v.Title, "firstAirDate": fmt.Sprintf("%d-01-01", v.Year)}, true
			}
		}
		return map[string]any{"id": id, "name": fmt.Sprintf("Requested Show %d", id), "firstAirDate": "2023-01-01"}, true
	}
	return nil, false
}

func seerrPage(r *http.Request, all []any) map[string]any {
	take, skip := int(qid(r, "take")), int(qid(r, "skip"))
	if take <= 0 {
		take = 10
	}
	from := min(skip, len(all))
	to := min(from+take, len(all))
	pages := (len(all) + take - 1) / take
	return map[string]any{"pageInfo": map[string]any{"pages": pages, "pageSize": take, "results": len(all), "page": skip/take + 1},
		"results": all[from:to]}
}

func (s *Server) qbit(p string) (any, bool) {
	switch p {
	case "/api/v2/app/preferences":
		return map[string]any{"save_path": "/downloads/", "temp_path_enabled": false, "web_ui_port": 8080}, true
	case "/api/v2/torrents/categories":
		return map[string]any{
			"radarr":    map[string]any{"name": "radarr", "savePath": "/downloads/movies"},
			"tv-sonarr": map[string]any{"name": "tv-sonarr", "savePath": ""},
			"music":     map[string]any{"name": "music", "savePath": "/downloads/music"},
		}, true
	case "/api/v2/torrents/tags":
		return []string{}, true
	}
	return nil, false
}

func (s *Server) sab(r *http.Request) (any, bool) {
	switch r.URL.Query().Get("mode") {
	case "version":
		return map[string]any{"version": s.Version}, true
	case "get_config":
		return map[string]any{"config": map[string]any{
			"misc": map[string]any{"complete_dir": "/downloads/complete", "download_dir": "/downloads/incomplete", "port": "8080", "api_key": "********"},
			"categories": []any{
				map[string]any{"name": "*", "dir": ""},
				map[string]any{"name": "movies", "dir": "movies"},
				map[string]any{"name": "tv", "dir": "/downloads/complete/tv"},
			}}}, true
	}
	return nil, false
}
