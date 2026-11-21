// Package quality holds the fixed quality ladder, per-quality size limits
// (definitions), quality profiles, release profiles and delay profiles, and
// their storage.
package quality

import (
	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// Kind is a media kind, named like the API's MediaKind.
type Kind string

// Media kinds.
const (
	Movies Kind = "movies"
	Series Kind = "series"
	Music  Kind = "music"
)

// ValidKind reports a known kind.
func ValidKind(k Kind) bool { return k == Movies || k == Series || k == Music }

// Quality is one rung of the ladder.
type Quality struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	Resolution int    `json:"resolution,omitempty"`
	// Modifier is remux, brdisk, rawhd, regional or screener.
	Modifier string `json:"modifier,omitempty"`
	Music    bool   `json:"music,omitempty"`
	// Rank orders the ladder (higher is better).
	Rank int `json:"rank"`
}

// Unknown qualities.
const (
	UnknownVideo = "unknown"
	UnknownMusic = "music-unknown"
)

func q(id, name, source string, res int, mod string) Quality {
	return Quality{ID: id, Name: name, Source: source, Resolution: res, Modifier: mod}
}

// ladder lists every quality from worst to best.
var ladder = func() []Quality {
	l := []Quality{
		q(UnknownVideo, "Unknown", "unknown", 0, ""),
		q("workprint", "WORKPRINT", "workprint", 0, ""),
		q("cam", "CAM", "cam", 0, ""),
		q("telesync", "TELESYNC", "telesync", 0, ""),
		q("telecine", "TELECINE", "telecine", 0, ""),
		q("regional", "REGIONAL", "dvd", 480, "regional"),
		q("screener", "DVDSCR", "dvd", 480, "screener"),
		q("sdtv", "SDTV", "tv", 480, ""),
		q("dvd", "DVD", "dvd", 480, ""),
		q("dvdr", "DVD-R", "dvd", 480, "dvdr"),
		q("webdl-480p", "WEBDL-480p", "webdl", 480, ""),
		q("webrip-480p", "WEBRip-480p", "webrip", 480, ""),
		q("bluray-480p", "Bluray-480p", "bluray", 480, ""),
		q("bluray-576p", "Bluray-576p", "bluray", 576, ""),
		q("hdtv-720p", "HDTV-720p", "tv", 720, ""),
		q("webdl-720p", "WEBDL-720p", "webdl", 720, ""),
		q("webrip-720p", "WEBRip-720p", "webrip", 720, ""),
		q("bluray-720p", "Bluray-720p", "bluray", 720, ""),
		q("hdtv-1080p", "HDTV-1080p", "tv", 1080, ""),
		q("webdl-1080p", "WEBDL-1080p", "webdl", 1080, ""),
		q("webrip-1080p", "WEBRip-1080p", "webrip", 1080, ""),
		q("bluray-1080p", "Bluray-1080p", "bluray", 1080, ""),
		q("remux-1080p", "Remux-1080p", "bluray", 1080, "remux"),
		q("rawhd", "Raw-HD", "tv", 1080, "rawhd"),
		q("hdtv-2160p", "HDTV-2160p", "tv", 2160, ""),
		q("webdl-2160p", "WEBDL-2160p", "webdl", 2160, ""),
		q("webrip-2160p", "WEBRip-2160p", "webrip", 2160, ""),
		q("bluray-2160p", "Bluray-2160p", "bluray", 2160, ""),
		q("remux-2160p", "Remux-2160p", "bluray", 2160, "remux"),
		q("brdisk", "BR-DISK", "bluray", 1080, "brdisk"),
		q("webdl-4320p", "WEBDL-4320p", "webdl", 4320, ""),
	}
	music := []Quality{
		q(UnknownMusic, "Unknown audio", "unknown", 0, ""),
		q("mp3-128", "MP3-128", "mp3", 0, ""),
		q("mp3-192", "MP3-192", "mp3", 0, ""),
		q("mp3", "MP3", "mp3", 0, ""),
		q("mp3-v2", "MP3-VBR-V2", "mp3", 0, ""),
		q("mp3-256", "MP3-256", "mp3", 0, ""),
		q("mp3-v0", "MP3-VBR-V0", "mp3", 0, ""),
		q("mp3-320", "MP3-320", "mp3", 0, ""),
		q("aac", "AAC", "aac", 0, ""),
		q("vorbis", "OGG Vorbis", "vorbis", 0, ""),
		q("opus", "Opus", "opus", 0, ""),
		q("alac", "ALAC", "alac", 0, ""),
		q("flac", "FLAC", "flac", 0, ""),
		q("flac-24", "FLAC 24bit", "flac", 0, ""),
		q("wav", "WAV", "wav", 0, ""),
	}
	for i := range music {
		music[i].Music = true
	}
	l = append(l, music...)
	for i := range l {
		l[i].Rank = i
	}
	return l
}()

var byID = func() map[string]Quality {
	m := make(map[string]Quality, len(ladder))
	for _, x := range ladder {
		m[x.ID] = x
	}
	return m
}()

// All returns the ladder (worst first).
func All() []Quality { return append([]Quality(nil), ladder...) }

// ForKind returns the qualities that apply to a media kind.
func ForKind(k Kind) []Quality {
	var out []Quality
	for _, x := range ladder {
		if x.Music == (k == Music) {
			out = append(out, x)
		}
	}
	return out
}

// ByID returns a quality.
func ByID(id string) (Quality, bool) {
	x, ok := byID[id]
	return x, ok
}

func tier(res int) int {
	switch {
	case res >= 4320:
		return 4320
	case res >= 2160:
		return 2160
	case res >= 1080:
		return 1080
	case res >= 720:
		return 720
	case res >= 576:
		return 576
	case res > 0:
		return 480
	}
	return 0
}

// Identify maps a parsed release to a quality of the given kind.
func Identify(r *parser.Release, k Kind) Quality {
	if k == Music || (k == "" && r.Kind == parser.KindMusic) {
		return ByIDMust(identifyMusic(r))
	}
	return ByIDMust(identifyVideo(r, k))
}

// ByIDMust returns a quality that is known to exist.
func ByIDMust(id string) Quality {
	if x, ok := byID[id]; ok {
		return x
	}
	return byID[UnknownVideo]
}

func identifyVideo(r *parser.Release, k Kind) string {
	res := tier(r.Video.Resolution)
	if r.Video.UHD && res == 0 {
		res = 2160
	}
	byRes := func(prefix string, fallback string) string {
		switch res {
		case 4320:
			if prefix == "webdl" || prefix == "webrip" {
				return "webdl-4320p"
			}
			return prefix + "-2160p"
		case 2160, 1080, 720:
			return prefix + "-" + itoa(res) + "p"
		}
		return fallback
	}
	switch r.Video.Source {
	case parser.SourceCam:
		return "cam"
	case parser.SourceTelesync:
		return "telesync"
	case parser.SourceTelecine:
		return "telecine"
	case parser.SourceWorkprint:
		return "workprint"
	case parser.SourceScreener:
		return "screener"
	case parser.SourceRegional:
		return "regional"
	case parser.SourceBRDisk:
		return "brdisk"
	case parser.SourceRawHD:
		return "rawhd"
	case parser.SourceRemux:
		switch {
		case res >= 2160:
			return "remux-2160p"
		case res == 720:
			return "bluray-720p"
		}
		return "remux-1080p"
	case parser.SourceBluRay, parser.SourceBDRip, parser.SourceBRRip:
		if res == 576 {
			return "bluray-576p"
		}
		return byRes("bluray", "bluray-480p")
	case parser.SourceWEBDL, parser.SourceWEB:
		return byRes("webdl", "webdl-480p")
	case parser.SourceWEBRip, parser.SourceHDRip:
		return byRes("webrip", "webrip-480p")
	case parser.SourceHDTV, parser.SourcePDTV, parser.SourceSDTV, parser.SourceVHS:
		if res == 4320 {
			return "hdtv-2160p"
		}
		return byRes("hdtv", "sdtv")
	case parser.SourceDVD:
		return "dvd"
	case parser.SourceDVDR:
		return "dvdr"
	}
	// No source in the name: guess from the resolution (TV-style for
	// episodes, WEB-style otherwise).
	if res == 0 {
		return UnknownVideo
	}
	if k == Series || (k == "" && r.Kind == parser.KindSeries) {
		if res == 4320 {
			return "hdtv-2160p"
		}
		return byRes("hdtv", "sdtv")
	}
	return byRes("webdl", "webdl-480p")
}

func identifyMusic(r *parser.Release) string {
	has := func(c string) bool {
		for _, x := range r.Audio.Codecs {
			if x == c {
				return true
			}
		}
		return false
	}
	switch {
	case has("flac") || has("ape"):
		if r.Audio.BitDepth >= 24 {
			return "flac-24"
		}
		return "flac"
	case has("alac"):
		return "alac"
	case has("wav") || has("pcm") || has("dsd"):
		return "wav"
	case has("mp3"):
		switch r.Audio.Bitrate {
		case "320":
			return "mp3-320"
		case "V0":
			return "mp3-v0"
		case "256":
			return "mp3-256"
		case "V2":
			return "mp3-v2"
		case "192", "160":
			return "mp3-192"
		case "128", "96":
			return "mp3-128"
		}
		return "mp3"
	case has("aac"):
		return "aac"
	case has("opus"):
		return "opus"
	case has("vorbis"):
		return "vorbis"
	}
	return UnknownMusic
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
