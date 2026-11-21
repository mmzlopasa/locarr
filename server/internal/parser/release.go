// Package parser turns a release name (or a file path) into structured facts:
// title, year, seasons and episodes, quality, audio, languages, release group
// and so on. It is written from publicly known scene and P2P naming
// conventions and checked against the golden files in testdata/.
//
// The parser never fails: anything it cannot classify is reported in
// Release.Unparsed and lowers Release.Confidence.
package parser

// Kind is the parser's guess of what a release contains.
type Kind string

// Kinds.
const (
	KindUnknown Kind = "unknown"
	KindMovie   Kind = "movie"
	KindSeries  Kind = "series"
	KindMusic   Kind = "music"
)

// Source is where the video came from.
type Source string

// Video sources, roughly from worst to best.
const (
	SourceUnknown   Source = ""
	SourceWorkprint Source = "workprint"
	SourceCam       Source = "cam"
	SourceTelesync  Source = "telesync"
	SourceTelecine  Source = "telecine"
	SourceScreener  Source = "screener"
	SourceRegional  Source = "regional"
	SourceVHS       Source = "vhs"
	SourceSDTV      Source = "sdtv"
	SourcePDTV      Source = "pdtv"
	SourceHDTV      Source = "hdtv"
	SourceDVD       Source = "dvd"
	SourceDVDR      Source = "dvdr"
	SourceHDRip     Source = "hdrip"
	SourceWEB       Source = "web"
	SourceWEBRip    Source = "webrip"
	SourceWEBDL     Source = "webdl"
	SourceBDRip     Source = "bdrip"
	SourceBRRip     Source = "brrip"
	SourceBluRay    Source = "bluray"
	SourceRemux     Source = "remux"
	SourceBRDisk    Source = "brdisk"
	SourceRawHD     Source = "rawhd"
)

// Video describes the picture.
type Video struct {
	Source     Source `json:"source,omitempty"`
	Resolution int    `json:"resolution,omitempty"` // lines: 480, 720, 1080, 2160, …
	Interlaced bool   `json:"interlaced,omitempty"`
	// UHD is set for "UHD BluRay" style names.
	UHD bool `json:"uhd,omitempty"`
	// Codec is the normalised codec: h264, h265, av1, vc1, mpeg2, xvid, divx, vp9.
	Codec string `json:"codec,omitempty"`
	// Encoder is x264 or x265 when the name says the video was re-encoded.
	Encoder  string   `json:"encoder,omitempty"`
	BitDepth int      `json:"bitDepth,omitempty"`
	HDR      []string `json:"hdr,omitempty"` // HDR10, HDR10+, DV, HLG
	// DolbyVisionProfile is a hint ("5", "7", "8") from the other tags.
	DolbyVisionProfile string `json:"dolbyVisionProfile,omitempty"`
}

// Audio describes the sound.
type Audio struct {
	// Codecs are normalised: aac, ac3, eac3, truehd, dts, dts-es, dts-hd hra,
	// dts-hd ma, dts:x, flac, opus, pcm, mp3, mp2, vorbis, alac, wav, ape.
	Codecs   []string `json:"codecs,omitempty"`
	Channels string   `json:"channels,omitempty"` // 1.0, 2.0, 5.1, 7.1
	Atmos    bool     `json:"atmos,omitempty"`
	// Bitrate is a music bitrate: "320", "256", "V0", "V2", …
	Bitrate       string  `json:"bitrate,omitempty"`
	BitDepth      int     `json:"bitDepth,omitempty"` // music: 16 or 24
	SampleRateKHz float64 `json:"sampleRateKHz,omitempty"`
}

// Revision tells repeated releases of the same thing apart.
type Revision struct {
	// Version is 1 for a first release; PROPER, REPACK and v2 raise it.
	Version int  `json:"version"`
	Real    int  `json:"real,omitempty"`
	Proper  bool `json:"proper,omitempty"`
	Repack  bool `json:"repack,omitempty"`
}

// Music holds what a music release name says.
type Music struct {
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	// Source is cd, web, vinyl, sacd, dvd or tape.
	Source string `json:"source,omitempty"`
	Discs  int    `json:"discs,omitempty"`
	Disc   int    `json:"disc,omitempty"`
}

// Release is everything the parser learned from a name.
type Release struct {
	Input           string   `json:"input"`
	Kind            Kind     `json:"kind"`
	Title           string   `json:"title"`
	AlternateTitles []string `json:"alternateTitles,omitempty"`
	Year            int      `json:"year,omitempty"`

	// Series.
	Seasons          []int  `json:"seasons,omitempty"`
	Episodes         []int  `json:"episodes,omitempty"`
	AbsoluteEpisodes []int  `json:"absoluteEpisodes,omitempty"`
	AirDate          string `json:"airDate,omitempty"` // YYYY-MM-DD for daily shows
	EpisodeTitle     string `json:"episodeTitle,omitempty"`
	FullSeason       bool   `json:"fullSeason,omitempty"`
	MultiSeason      bool   `json:"multiSeason,omitempty"`
	CompleteSeries   bool   `json:"completeSeries,omitempty"`
	Special          bool   `json:"special,omitempty"`
	Anime            bool   `json:"anime,omitempty"`

	// Movies.
	Editions []string `json:"editions,omitempty"`
	ThreeD   bool     `json:"threeD,omitempty"`

	Music *Music `json:"music,omitempty"`

	Video            Video  `json:"video"`
	Audio            Audio  `json:"audio"`
	StreamingService string `json:"streamingService,omitempty"`

	// Languages are spoken languages named in the release (ISO 639-1).
	Languages         []string `json:"languages,omitempty"`
	MultiAudio        bool     `json:"multiAudio,omitempty"`
	DualAudio         bool     `json:"dualAudio,omitempty"`
	SubtitleLanguages []string `json:"subtitleLanguages,omitempty"`
	MultiSubs         bool     `json:"multiSubs,omitempty"`
	HardcodedSubs     bool     `json:"hardcodedSubs,omitempty"`

	Group    string   `json:"group,omitempty"`
	Hash     string   `json:"hash,omitempty"`
	Revision Revision `json:"revision"`
	// Flags are other tags: internal, limited, hybrid, readnfo, dubbed, …
	Flags []string `json:"flags,omitempty"`

	Extension string `json:"extension,omitempty"`
	Sample    bool   `json:"sample,omitempty"`
	// Obfuscated names (random or hashed) carry no information.
	Obfuscated bool `json:"obfuscated,omitempty"`

	// Confidence is 0..1: how much of the name the parser understood.
	Confidence float64 `json:"confidence"`
	// Unparsed lists the parts after the title the parser could not classify.
	Unparsed []string `json:"unparsed,omitempty"`
}

// HasFlag reports whether the release carries flag f.
func (r *Release) HasFlag(f string) bool {
	for _, x := range r.Flags {
		if x == f {
			return true
		}
	}
	return false
}

// IsSeasonPack reports a release with whole seasons and no single episodes.
func (r *Release) IsSeasonPack() bool {
	return (r.FullSeason || r.MultiSeason || r.CompleteSeries) && len(r.Episodes) == 0
}

func (r *Release) addFlag(f string) {
	if !r.HasFlag(f) {
		r.Flags = append(r.Flags, f)
	}
}

func addUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func addInt(list []int, v int) []int {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
