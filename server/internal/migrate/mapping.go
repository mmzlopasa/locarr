package migrate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/requests"
)

// The mapping rules (D-074). Every rule is a plain table or a small function
// so the preview can say exactly why something became what it became.

// qualityAliases are source quality names that differ from Locarr's ladder
// names (compared without case, spaces or dashes).
var qualityAliases = map[string]string{
	"bluray1080premux": "remux-1080p", "bluray2160premux": "remux-2160p", "remux1080p": "remux-1080p", "remux2160p": "remux-2160p",
	"brdisk": "brdisk", "rawhd": "rawhd", "dvdr": "dvdr", "dvdscr": "screener", "regional": "regional", "unknown": quality.UnknownVideo,
	// Music.
	"flac": "flac", "flac24bit": "flac-24", "alac": "alac", "alac24bit": "alac", "wav": "wav", "mp3320": "mp3-320", "mp3256": "mp3-256",
	"mp3192": "mp3-192", "mp3160": "mp3", "mp3128": "mp3-128", "mp396": "mp3-128", "mp380": "mp3-128", "mp364": "mp3-128",
	"mp356": "mp3-128", "mp348": "mp3-128", "mp340": "mp3-128", "mp332": "mp3-128", "mp324": "mp3-128", "mp316": "mp3-128",
	"mp38": "mp3-128", "mp3vbrv0": "mp3-v0", "mp3vbrv2": "mp3-v2", "mp3vbr": "mp3", "aac320": "aac", "aac256": "aac",
	"aac192": "aac", "aacvbr": "aac", "opus": "opus", "unknownaudio": quality.UnknownMusic,
}

var musicUnknown = map[string]bool{"unknown": true}

func normName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '_' || r == '.' {
			return -1
		}
		return r
	}, strings.ToLower(s))
}

// MapQuality maps a source quality name to a Locarr quality id of the kind
// ("" when Locarr has no such quality).
func MapQuality(name string, k quality.Kind) string {
	n := normName(name)
	if k == quality.Music {
		if musicUnknown[n] {
			return quality.UnknownMusic
		}
		if strings.HasPrefix(n, "oggvorbis") || strings.HasPrefix(n, "vorbis") {
			return "vorbis"
		}
	}
	if id, ok := qualityAliases[n]; ok {
		if x, ok := quality.ByID(id); ok && x.Music == (k == quality.Music) {
			return id
		}
	}
	for _, x := range quality.ForKind(k) {
		if normName(x.Name) == n || normName(x.ID) == n {
			return x.ID
		}
	}
	return ""
}

// MapLanguage maps a source language name ("English", "Portuguese (Brazil)")
// or code to a Locarr language code ("" when unknown).
func MapLanguage(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "", "unknown":
		return ""
	case "any":
		return languages.Any
	case "original":
		return languages.Original
	case "portuguese (brazil)", "brazilian", "pt-br", "pb":
		return "pt-br"
	case "spanish (latino)", "latino", "spanish (latin america)":
		return "es-419"
	}
	if languages.Valid(n) {
		return n
	}
	for _, l := range languages.List() {
		if strings.EqualFold(l.Name, name) || strings.EqualFold(l.Native, name) {
			return l.Code
		}
	}
	return ""
}

// ProfileConversion is a converted quality profile and what got lost.
type ProfileConversion struct {
	Profile  quality.Profile
	Warnings []string
	// FormatScores are by source custom-format id (resolved when applied).
	FormatScores map[int64]int
}

// ConvertProfile turns a source quality profile into a Locarr one of kind k.
// Items are reversed (the applications list worst first), qualities Locarr
// does not have are left out, and a cutoff that no longer exists becomes the
// best allowed item.
func ConvertProfile(p QualityProfile, k quality.Kind, langs []string) ProfileConversion {
	conv := ProfileConversion{Profile: quality.Profile{Name: strings.TrimSpace(p.Name), Kind: k, UpgradesAllowed: p.UpgradeAllowed,
		MinFormatScore: p.MinFormatScore, CutoffFormatScore: p.CutoffFormatScore, FormatScores: []quality.FormatScore{}},
		FormatScores: map[int64]int{}}
	seen := map[string]bool{}
	cutoffKey := ""
	var dropped []string
	member := func(q *QualityRef) string {
		if q == nil {
			return ""
		}
		id := MapQuality(q.Name, k)
		if id == "" {
			dropped = append(dropped, q.Name)
			return ""
		}
		if seen[id] {
			return ""
		}
		seen[id] = true
		return id
	}
	for i := len(p.Items) - 1; i >= 0; i-- {
		it := p.Items[i]
		if len(it.Items) == 0 && it.Quality != nil {
			id := member(it.Quality)
			if id == "" {
				continue
			}
			conv.Profile.Items = append(conv.Profile.Items, quality.Item{Quality: id, Allowed: it.Allowed})
			if it.Quality.ID == p.Cutoff {
				cutoffKey = id
			}
			continue
		}
		var members []string
		for j := len(it.Items) - 1; j >= 0; j-- {
			if id := member(it.Items[j].Quality); id != "" {
				members = append(members, id)
			}
		}
		if len(members) == 0 {
			continue
		}
		var x quality.Item
		if len(members) == 1 {
			x = quality.Item{Quality: members[0], Allowed: it.Allowed}
		} else {
			name := strings.TrimSpace(it.Name)
			if name == "" {
				name = fmt.Sprintf("Group %d", it.ID)
			}
			x = quality.Item{Name: name, Qualities: members, Allowed: it.Allowed}
		}
		conv.Profile.Items = append(conv.Profile.Items, x)
		if it.ID == p.Cutoff && it.ID != 0 {
			cutoffKey = x.Key()
		}
	}
	if len(dropped) > 0 {
		sort.Strings(dropped)
		conv.Warnings = append(conv.Warnings, "qualities Locarr does not have were left out: "+strings.Join(dropped, ", "))
	}
	conv.Profile.Cutoff = cutoffKey
	if pos := conv.Profile.CutoffPosition(); pos < 0 || !conv.Profile.Items[pos].Allowed {
		if best := conv.Profile.BestAllowed(); best >= 0 {
			conv.Profile.Cutoff = conv.Profile.Items[best].Key()
			if cutoffKey != "" || p.Cutoff != 0 {
				conv.Warnings = append(conv.Warnings, "the cutoff was not an allowed quality here; it is now "+quality.ItemName(conv.Profile.Items[best]))
			}
		}
	}
	for _, fs := range p.FormatScores {
		if fs.Score != 0 {
			conv.FormatScores[fs.FormatID] = fs.Score
		}
	}
	conv.Profile.Languages = []string{}
	for _, l := range langs {
		code := MapLanguage(l)
		switch code {
		case "":
			conv.Warnings = append(conv.Warnings, "language "+l+" is unknown to Locarr and was left out")
		case languages.Any:
		default:
			conv.Profile.Languages = appendUnique(conv.Profile.Languages, code)
		}
	}
	return conv
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// MapAvailability maps Radarr's minimum availability.
func MapAvailability(v string) library.Availability {
	switch strings.ToLower(v) {
	case "announced", "tba", "predb":
		return library.Announced
	case "incinemas", "in_cinemas":
		return library.InCinemas
	}
	return library.Released
}

// MapSeriesStatus maps Sonarr's series status.
func MapSeriesStatus(v string) string {
	switch strings.ToLower(v) {
	case "ended", "deleted":
		return "ended"
	case "upcoming":
		return "upcoming"
	}
	return "continuing"
}

// MapSeriesType maps Sonarr's series type.
func MapSeriesType(v string) string {
	switch strings.ToLower(v) {
	case "daily":
		return "daily"
	case "anime":
		return "anime"
	}
	return "standard"
}

// MapMonitorNew maps "monitor new items" (all or none; Lidarr also new).
func MapMonitorNew(v string, music bool) string {
	switch strings.ToLower(v) {
	case "none":
		return "none"
	case "new":
		if music {
			return "future"
		}
	}
	return "all"
}

// Request application permission bits, as their public API documents them.
const (
	seerrAdmin             = 2
	seerrManageSettings    = 4
	seerrManageUsers       = 8
	seerrManageRequests    = 16
	seerrRequest           = 32
	seerrAutoApprove       = 128
	seerrAutoApproveMovie  = 256
	seerrAutoApproveTV     = 512
	seerrRequest4K         = 1024
	seerrRequest4KMovie    = 2048
	seerrRequest4KTV       = 4096
	seerrAutoApprove4K     = 32768
	seerrAutoApprove4KMov  = 65536
	seerrAutoApprove4KTV   = 131072
	seerrRequestMovie      = 262144
	seerrRequestTV         = 524288
	seerrManageIssues      = 1048576
	seerrPermissionUnknown = 0
)

// PermissionRule is one row of the permission table shown in the docs and
// the preview.
type PermissionRule struct {
	Source string
	Locarr string
}

// PermissionTable documents MapPermissions.
var PermissionTable = []PermissionRule{
	{"Admin", "role admin (everything)"},
	{"Manage settings / Manage users", "role manager + manage library"},
	{"Manage requests / Manage issues", "role manager + manage requests"},
	{"Request (all)", "request movies, series and music"},
	{"Request movies / Request series", "request movies / request series"},
	{"Request 4K (any)", "request 4K"},
	{"Auto-approve (all)", "auto-approve every kind"},
	{"Auto-approve movies / series", "auto-approve movies / series"},
	{"Auto-approve 4K (any)", "auto-approve 4K"},
	{"anything else (vote, watchlist, …)", "not imported (Locarr has no equivalent)"},
}

// MapPermissions turns request application permission bits into a Locarr
// role and permissions (see PermissionTable).
func MapPermissions(bits int64) (auth.Role, auth.Permissions) {
	has := func(b int64) bool { return bits&b != 0 }
	if has(seerrAdmin) {
		return auth.RoleAdmin, auth.DefaultPermissions(auth.RoleAdmin)
	}
	role := auth.RoleRequester
	var p auth.Permissions
	if has(seerrManageSettings) || has(seerrManageUsers) {
		role = auth.RoleManager
		p |= auth.PermManageLibrary
	}
	if has(seerrManageRequests) || has(seerrManageIssues) {
		role = auth.RoleManager
		p |= auth.PermManageRequests
	}
	if has(seerrRequest) {
		p |= auth.PermRequestMovies | auth.PermRequestSeries | auth.PermRequestMusic
	}
	if has(seerrRequestMovie) {
		p |= auth.PermRequestMovies
	}
	if has(seerrRequestTV) {
		p |= auth.PermRequestSeries
	}
	if has(seerrRequest4K) || has(seerrRequest4KMovie) || has(seerrRequest4KTV) {
		p |= auth.PermRequest4K
	}
	if has(seerrAutoApprove) {
		p |= auth.PermAutoApprove | auth.PermAutoApproveMovies | auth.PermAutoApproveSeries | auth.PermAutoApproveMusic
	}
	if has(seerrAutoApproveMovie) {
		p |= auth.PermAutoApproveMovies
	}
	if has(seerrAutoApproveTV) {
		p |= auth.PermAutoApproveSeries
	}
	if has(seerrAutoApprove4K) || has(seerrAutoApprove4KMov) || has(seerrAutoApprove4KTV) {
		p |= auth.PermAutoApprove4K
	}
	return role, p
}

// Request application states (request status and media status).
const (
	reqPending  = 1
	reqApproved = 2
	reqDeclined = 3
	reqFailed   = 4
	reqDone     = 5 // "completed" in newer versions

	mediaProcessing = 3
	mediaPartial    = 4
	mediaAvailable  = 5
)

// MapRequestStatus maps a request's own status and its media status to a
// Locarr request status.
func MapRequestStatus(status, media int) requests.Status {
	switch status {
	case reqPending:
		return requests.StatusPending
	case reqDeclined:
		return requests.StatusDeclined
	case reqFailed:
		return requests.StatusFailed
	case reqDone:
		return requests.StatusAvailable
	case reqApproved:
		switch media {
		case mediaAvailable:
			return requests.StatusAvailable
		case mediaPartial:
			return requests.StatusPartial
		case mediaProcessing:
			return requests.StatusProcessing
		}
		return requests.StatusApproved
	}
	return requests.StatusPending
}

// MapIssueType maps the issue type number.
func MapIssueType(t int) string {
	switch t {
	case 1:
		return "video"
	case 2:
		return "audio"
	case 3:
		return "subtitles"
	}
	return "other"
}

// MapClientImplementation maps a source download client implementation.
func MapClientImplementation(impl string) string {
	switch strings.ToLower(impl) {
	case "qbittorrent":
		return "qbittorrent"
	case "transmission", "vuze":
		return "transmission"
	case "deluge":
		return "deluge"
	case "rtorrent":
		return "rtorrent"
	case "aria2":
		return "aria2"
	case "sabnzbd":
		return "sabnzbd"
	case "nzbget":
		return "nzbget"
	}
	return ""
}

// Naming tokens of the other applications (inside braces, without the
// prefix and suffix) and the Locarr token each becomes.
var namingTokens = map[string]string{
	// Movies.
	"movie title": "Title", "movie cleantitle": "CleanTitle", "movie titlethe": "TitleThe", "movie titlefirstcharacter": "TitleFirstChar",
	"movie originaltitle": "OriginalTitle", "movie collection": "Collection", "release year": "Year", "edition tags": "Edition",
	"quality full": "QualityFull", "quality title": "Quality", "mediainfo videocodec": "VideoCodec", "mediainfo simple": "VideoCodec",
	"mediainfo videodynamicrange": "HDR", "mediainfo videodynamicrangetype": "HDR", "mediainfo videobitdepth": "VideoBitDepth",
	"release group": "ReleaseGroup", "custom formats": "CustomFormats", "tmdbid": "TmdbId", "imdbid": "ImdbId",
	// Series.
	"series title": "SeriesTitle", "series titleyear": "SeriesTitleYear", "series cleantitle": "SeriesCleanTitle",
	"series titlethe": "SeriesTitleThe", "series titlefirstcharacter": "SeriesTitleFirstChar", "series cleantitleyear": "SeriesTitleYear",
	"season:00": "Season", "season:0": "SeasonNumber", "season": "SeasonNumber", "episode:00": "Episode", "episode:0": "EpisodeNumber",
	"episode": "EpisodeNumber", "episode title": "EpisodeTitle", "episode cleantitle": "EpisodeCleanTitle", "air-date": "AirDate",
	"absolute:000": "Absolute", "absolute:00": "Absolute", "absolute": "Absolute", "tvdbid": "TvdbId", "year": "Year",
	// Music.
	"artist name": "ArtistName", "artist cleanname": "ArtistCleanName", "artist namethe": "ArtistNameThe",
	"artist namefirstcharacter": "ArtistNameFirstChar", "artist disambiguation": "ArtistDisambiguation", "album title": "AlbumTitle",
	"album cleantitle": "AlbumCleanTitle", "album titlethe": "AlbumTitleThe", "album type": "AlbumType",
	"album disambiguation": "AlbumDisambiguation", "track:00": "TrackNumber", "track:0": "TrackNumberPlain",
	"track title": "TrackTitle", "track cleantitle": "TrackCleanTitle", "medium:00": "DiscNumber", "medium:0": "DiscNumber",
	"medium": "DiscNumber", "medium name": "DiscTitle", "track artistname": "TrackArtistName",
}

var tokenRe = regexp.MustCompile(`\{([^{}]*)\}`)

// TranslateNaming rewrites a template of the other applications into
// Locarr's syntax; it returns the tokens it did not know.
func TranslateNaming(tmpl string) (string, []string) {
	var unknown []string
	out := tokenRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		inner := m[1 : len(m)-1]
		// The longest known token inside the braces; what is around it is
		// the prefix and suffix (as both syntaxes allow).
		low := strings.ToLower(inner)
		best, at := "", -1
		for tok := range namingTokens {
			if i := strings.Index(low, tok); i >= 0 && len(tok) > len(best) {
				best, at = tok, i
			}
		}
		if best == "" {
			unknown = append(unknown, inner)
			return m
		}
		prefix, suffix := inner[:at], inner[at+len(best):]
		if strings.IndexFunc(prefix+suffix, isWordRune) >= 0 {
			unknown = append(unknown, inner)
			return m
		}
		return "{" + prefix + namingTokens[best] + suffix + "}"
	})
	return out, unknown
}

func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// MapColon maps the colon replacement setting.
func MapColon(v any) string {
	switch x := v.(type) {
	case float64:
		switch int(x) {
		case 0:
			return library.ColonDelete
		case 1:
			return library.ColonDash
		case 2:
			return library.ColonSpaceDash
		}
	case string:
		switch strings.ToLower(x) {
		case "delete":
			return library.ColonDelete
		case "spacedash", "spacedashspace":
			return library.ColonSpaceDash
		}
		return library.ColonDash
	}
	return ""
}

// MapMultiEpisodeStyle maps Sonarr's multi-episode style number.
func MapMultiEpisodeStyle(v any) string {
	n, ok := v.(float64)
	if !ok {
		return ""
	}
	return map[int]string{0: "extend", 1: "duplicate", 2: "repeat", 3: "scene", 4: "range", 5: "prefixed_range"}[int(n)]
}

// Category heuristics for download client categories read from the client itself.
func categoryKind(name string) quality.Kind {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "radarr") || strings.Contains(n, "movie") || strings.Contains(n, "film"):
		return quality.Movies
	case strings.Contains(n, "sonarr") || strings.Contains(n, "tv") || strings.Contains(n, "series") || strings.Contains(n, "show"):
		return quality.Series
	case strings.Contains(n, "lidarr") || strings.Contains(n, "music") || strings.Contains(n, "audio"):
		return quality.Music
	}
	return ""
}

// kindOf is the media kind an *arr source manages.
func kindOf(k Kind) quality.Kind {
	switch k {
	case Sonarr:
		return quality.Series
	case Lidarr:
		return quality.Music
	}
	return quality.Movies
}

// MapMetadataType normalizes a Lidarr album or status name.
func MapMetadataType(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
