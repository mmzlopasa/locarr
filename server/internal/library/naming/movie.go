package naming

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// MovieTokens are the tokens of movie folder and file templates.
var MovieTokens = []Token{
	{"Title", "The Matrix"}, {"CleanTitle", "The Matrix"}, {"TitleThe", "Matrix, The"}, {"TitleFirstChar", "M"},
	{"OriginalTitle", "The Matrix"}, {"Year", "1999"}, {"Collection", "The Matrix Collection"},
	{"TmdbId", "603"}, {"ImdbId", "tt0133093"}, {"Edition", "Director's Cut"},
	{"Quality", "Bluray-1080p"}, {"QualityFull", "Bluray-1080p Proper"}, {"Resolution", "1080p"}, {"Source", "BluRay"},
	{"VideoCodec", "x265"}, {"VideoBitDepth", "10bit"}, {"HDR", "DV HDR10"},
	{"AudioCodec", "TrueHD Atmos"}, {"AudioChannels", "7.1"}, {"AudioLanguages", "EN HR"},
	{"CustomFormats", "HDR Remux"}, {"ReleaseGroup", "GROUP"},
}

// MovieInfo is what a movie contributes to its names.
type MovieInfo struct {
	Title         string
	OriginalTitle string
	Year          int
	TMDbID        int64
	IMDbID        string
	Collection    string
}

// FileInfo is what a file contributes (from the release name or the file).
type FileInfo struct {
	Release       *parser.Release
	Quality       quality.Quality
	CustomFormats []string
}

// CleanTitle keeps letters, digits and spaces.
func CleanTitle(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ':
			b.WriteRune(r)
		case r == '&':
			b.WriteString("and")
		case r == '-' || r == '.' || r == '_':
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// TitleThe moves a leading article to the end: "The Matrix" -> "Matrix, The".
func TitleThe(s string) string {
	for _, a := range []string{"The ", "A ", "An "} {
		if strings.HasPrefix(s, a) && len(s) > len(a) {
			return s[len(a):] + ", " + strings.TrimSpace(a)
		}
	}
	return s
}

// SortTitle is the title used for sorting: lower case, without a leading article.
func SortTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(s, a) && len(s) > len(a) {
			return s[len(a):]
		}
	}
	return s
}

var sourceNames = map[string]string{"bluray": "BluRay", "webdl": "WEB-DL", "webrip": "WEBRip", "tv": "HDTV", "dvd": "DVD",
	"cam": "CAM", "telesync": "TS", "telecine": "TC", "workprint": "WORKPRINT"}

var audioNames = map[string]string{"aac": "AAC", "ac3": "DD", "eac3": "DDP", "truehd": "TrueHD", "dts": "DTS", "dts-es": "DTS-ES",
	"dts-hd hra": "DTS-HD HRA", "dts-hd ma": "DTS-HD MA", "dts:x": "DTS-X", "flac": "FLAC", "opus": "Opus", "pcm": "PCM",
	"mp3": "MP3", "mp2": "MP2", "vorbis": "Vorbis"}

// MovieValues returns the token values for a movie file (f may be nil for folders).
func MovieValues(m MovieInfo, f *FileInfo) Values {
	v := Values{"Title": m.Title, "CleanTitle": CleanTitle(m.Title), "TitleThe": TitleThe(m.Title),
		"OriginalTitle": m.OriginalTitle, "Collection": m.Collection, "ImdbId": m.IMDbID}
	if r := []rune(CleanTitle(TitleThe(m.Title))); len(r) > 0 {
		v["TitleFirstChar"] = strings.ToUpper(string(r[0]))
	}
	if m.Year > 0 {
		v["Year"] = strconv.Itoa(m.Year)
	}
	if m.TMDbID > 0 {
		v["TmdbId"] = strconv.FormatInt(m.TMDbID, 10)
	}
	if f == nil {
		return v
	}
	q := f.Quality
	if q.ID != "" && q.ID != quality.UnknownVideo {
		v["Quality"] = q.Name
		v["QualityFull"] = q.Name
	}
	if q.Resolution > 0 {
		v["Resolution"] = strconv.Itoa(q.Resolution) + "p"
	}
	if s, ok := sourceNames[q.Source]; ok {
		v["Source"] = s
	}
	if q.Modifier == "remux" {
		v["Source"] = "Remux"
	}
	v["CustomFormats"] = strings.Join(f.CustomFormats, " ")
	r := f.Release
	if r == nil {
		return v
	}
	if v["QualityFull"] != "" {
		switch {
		case r.Revision.Proper:
			v["QualityFull"] += " Proper"
		case r.Revision.Repack:
			v["QualityFull"] += " Repack"
		}
	}
	v["Edition"] = strings.Join(r.Editions, " ")
	codec := r.Video.Encoder
	if codec == "" {
		codec = strings.ToUpper(r.Video.Codec)
		switch r.Video.Codec {
		case "h264":
			codec = "h264"
		case "h265":
			codec = "h265"
		}
	}
	v["VideoCodec"] = codec
	if r.Video.BitDepth > 0 {
		v["VideoBitDepth"] = strconv.Itoa(r.Video.BitDepth) + "bit"
	}
	v["HDR"] = strings.Join(r.Video.HDR, " ")
	if len(r.Audio.Codecs) > 0 {
		a := audioNames[r.Audio.Codecs[0]]
		if a == "" {
			a = strings.ToUpper(r.Audio.Codecs[0])
		}
		if r.Audio.Atmos {
			a += " Atmos"
		}
		v["AudioCodec"] = a
	}
	v["AudioChannels"] = r.Audio.Channels
	langs := make([]string, 0, len(r.Languages))
	for _, l := range r.Languages {
		langs = append(langs, strings.ToUpper(l))
	}
	v["AudioLanguages"] = strings.Join(langs, " ")
	v["ReleaseGroup"] = r.Group
	return v
}
