package naming

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Music tokens.
var (
	// ArtistTokens name an artist's folder.
	ArtistTokens = []Token{{"ArtistName", "The Beatles"}, {"ArtistCleanName", "The Beatles"}, {"ArtistNameThe", "Beatles, The"},
		{"ArtistNameFirstChar", "B"}, {"ArtistDisambiguation", "UK rock band"}, {"ArtistMbId", "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"}}
	// AlbumTokens name an album's folder (artist tokens work too).
	AlbumTokens = append(append([]Token{}, ArtistTokens...), []Token{{"AlbumTitle", "Abbey Road"}, {"AlbumCleanTitle", "Abbey Road"},
		{"AlbumTitleThe", "Abbey Road"}, {"AlbumType", "Album"}, {"AlbumDisambiguation", "deluxe"}, {"ReleaseYear", "1969"},
		{"ReleaseDate", "1969-09-26"}, {"OriginalYear", "1969"}, {"EditionYear", "2019"}, {"ReleaseCountry", "GB"}, {"ReleaseFormat", "CD"},
		{"ReleaseDisambiguation", "2019 remaster"}, {"RecordLabel", "Apple Records"}, {"DiscCount", "1"}, {"TrackCount", "17"},
		{"Quality", "FLAC 24bit"}, {"AudioFormat", "FLAC"}, {"AudioBitDepth", "24bit"}, {"AudioSampleRate", "96kHz"},
		{"ReleaseGroup", "GROUP"}, {"AlbumMbId", "9162580e-5df4-32de-80cc-f45a8d8a9b1d"}}...)
	// DiscTokens name a disc folder (album tokens work too).
	DiscTokens = append(append([]Token{}, AlbumTokens...), Token{"DiscNumber", "1"}, Token{"DiscTitle", "Bonus Disc"})
	// TrackTokens name a track file.
	TrackTokens = append(append([]Token{}, DiscTokens...), []Token{{"TrackNumber", "01"}, {"TrackNumberPlain", "1"}, {"TrackTitle", "Come Together"},
		{"TrackCleanTitle", "Come Together"}, {"TrackArtistName", "The Beatles"}, {"AudioBitrate", "320kbps"}, {"Extension", "flac"}}...)
)

// ArtistInfo is what an artist contributes to names.
type ArtistInfo struct {
	Name           string
	Disambiguation string
	MBID           string
}

// AlbumInfo is what an album and its chosen release contribute.
type AlbumInfo struct {
	Title          string
	Type           string // album, ep, single, …
	Disambiguation string
	ReleaseDate    string // first release of the album
	MBID           string
	// Release (edition).
	EditionDate           string
	Country               string
	Format                string
	ReleaseDisambiguation string
	Label                 string
	Discs                 int
	Tracks                int
}

// TrackInfo is one track and its file.
type TrackInfo struct {
	Disc      int
	DiscTitle string
	Number    int
	Title     string
	Artist    string
	// File facts.
	Quality      string // display name ("FLAC 24bit")
	Format       string // flac, mp3, …
	BitDepth     int
	SampleRate   int
	Bitrate      int // kbit/s
	ReleaseGroup string
	Extension    string
}

func firstChar(s string) string {
	for _, r := range CleanTitle(TitleThe(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return ""
}

var albumTypeNames = map[string]string{"album": "Album", "ep": "EP", "single": "Single", "broadcast": "Broadcast", "other": "Other"}

var formatNames = map[string]string{"flac": "FLAC", "mp3": "MP3", "aac": "AAC", "alac": "ALAC", "vorbis": "OGG", "opus": "Opus", "wav": "WAV"}

// MusicValues returns token values (album, track may be nil for the artist
// and album folders).
func MusicValues(a ArtistInfo, al *AlbumInfo, t *TrackInfo) Values {
	v := Values{"ArtistName": a.Name, "ArtistCleanName": CleanTitle(a.Name), "ArtistNameThe": TitleThe(a.Name),
		"ArtistNameFirstChar": firstChar(a.Name), "ArtistDisambiguation": a.Disambiguation, "ArtistMbId": a.MBID}
	if al == nil {
		return v
	}
	v["AlbumTitle"], v["AlbumCleanTitle"], v["AlbumTitleThe"] = al.Title, CleanTitle(al.Title), TitleThe(al.Title)
	v["AlbumType"] = albumTypeNames[strings.ToLower(al.Type)]
	if v["AlbumType"] == "" && al.Type != "" {
		v["AlbumType"] = al.Type
	}
	v["AlbumDisambiguation"], v["AlbumMbId"] = al.Disambiguation, al.MBID
	if len(al.ReleaseDate) >= 4 {
		v["ReleaseYear"], v["OriginalYear"], v["ReleaseDate"] = al.ReleaseDate[:4], al.ReleaseDate[:4], al.ReleaseDate
	}
	if v["ReleaseYear"] == "" && len(al.EditionDate) >= 4 {
		v["ReleaseYear"] = al.EditionDate[:4]
	}
	if len(al.EditionDate) >= 4 {
		v["EditionYear"] = al.EditionDate[:4]
	}
	v["ReleaseCountry"], v["ReleaseFormat"], v["ReleaseDisambiguation"], v["RecordLabel"] = al.Country, al.Format, al.ReleaseDisambiguation, al.Label
	if al.Discs > 0 {
		v["DiscCount"] = strconv.Itoa(al.Discs)
	}
	if al.Tracks > 0 {
		v["TrackCount"] = strconv.Itoa(al.Tracks)
	}
	if t == nil {
		return v
	}
	v["DiscNumber"] = strconv.Itoa(max(t.Disc, 1))
	v["DiscTitle"] = t.DiscTitle
	if t.Number > 0 {
		v["TrackNumber"] = fmt.Sprintf("%02d", t.Number)
		v["TrackNumberPlain"] = strconv.Itoa(t.Number)
	}
	v["TrackTitle"], v["TrackCleanTitle"], v["TrackArtistName"] = t.Title, CleanTitle(t.Title), t.Artist
	if v["TrackArtistName"] == "" {
		v["TrackArtistName"] = a.Name
	}
	v["Quality"] = t.Quality
	v["AudioFormat"] = formatNames[t.Format]
	if t.BitDepth > 0 {
		v["AudioBitDepth"] = strconv.Itoa(t.BitDepth) + "bit"
	}
	if t.SampleRate > 0 {
		khz := float64(t.SampleRate) / 1000
		v["AudioSampleRate"] = strings.TrimSuffix(strings.TrimRight(strconv.FormatFloat(khz, 'f', 1, 64), "0"), ".") + "kHz"
	}
	if t.Bitrate > 0 {
		v["AudioBitrate"] = strconv.Itoa(t.Bitrate) + "kbps"
	}
	v["ReleaseGroup"] = t.ReleaseGroup
	v["Extension"] = strings.TrimPrefix(strings.ToLower(t.Extension), ".")
	return v
}
