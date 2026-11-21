package parser

// feat is what a token stands for.
type feat uint8

const (
	fNone feat = iota
	fCont      // continuation of a multi-token feature
	fSep       // "-", "~", "/"
	fYear
	fEpisode // index into the episode matches
	fResolution
	fSource
	fCodec
	fBitDepth
	fHDR
	fAudio
	fChannels
	fAtmos
	fStreaming
	fLanguage
	fMulti
	fDual
	fSubs
	fHardsub
	fEdition
	f3D
	fRevision
	fFlag
	fComplete
	fMusicBitrate
	fMusicSource
	fSampleRate
	fDisc
	fExtra // ignored noise words (e.g. "rip")
)

// def describes a dictionary entry.
type def struct {
	f   feat
	val string
	n   int
	// primary features end the title.
	primary bool
	// ambiguous words only count in capitals (or after the title).
	ambiguous bool
	// upper means the word only counts when written in capitals.
	upper bool
}

// single maps a lower-cased token (apostrophes removed) to its meaning.
var single = map[string]def{
	// sources
	"remux": {f: fSource, val: string(SourceRemux), primary: true}, "bdremux": {f: fSource, val: string(SourceRemux), primary: true},
	"bd-remux": {f: fSource, val: string(SourceRemux), primary: true},
	"bluray":   {f: fSource, val: string(SourceBluRay), primary: true}, "blu-ray": {f: fSource, val: string(SourceBluRay), primary: true},
	"bdrip": {f: fSource, val: string(SourceBDRip), primary: true}, "brrip": {f: fSource, val: string(SourceBRRip), primary: true},
	"bd":   {f: fSource, val: string(SourceBluRay), primary: true, ambiguous: true},
	"bdmv": {f: fSource, val: string(SourceBRDisk), primary: true}, "bdiso": {f: fSource, val: string(SourceBRDisk), primary: true},
	"bd25": {f: fSource, val: string(SourceBRDisk), primary: true}, "bd50": {f: fSource, val: string(SourceBRDisk), primary: true},
	"bd66": {f: fSource, val: string(SourceBRDisk), primary: true}, "bd100": {f: fSource, val: string(SourceBRDisk), primary: true},
	"hddvd": {f: fSource, val: string(SourceBluRay), primary: true}, "hd-dvd": {f: fSource, val: string(SourceBluRay), primary: true},
	"uhdbd":  {f: fSource, val: "uhd-bluray", primary: true},
	"web-dl": {f: fSource, val: string(SourceWEBDL), primary: true}, "webdl": {f: fSource, val: string(SourceWEBDL), primary: true},
	"web-hd": {f: fSource, val: string(SourceWEBDL), primary: true}, "webhd": {f: fSource, val: string(SourceWEBDL), primary: true},
	"web":    {f: fSource, val: string(SourceWEB), primary: true, ambiguous: true},
	"webrip": {f: fSource, val: string(SourceWEBRip), primary: true}, "web-rip": {f: fSource, val: string(SourceWEBRip), primary: true},
	"webcap": {f: fSource, val: string(SourceWEBRip), primary: true}, "hdrip": {f: fSource, val: string(SourceHDRip), primary: true},
	"hdtv": {f: fSource, val: string(SourceHDTV), primary: true}, "hdtvrip": {f: fSource, val: string(SourceHDTV), primary: true},
	"ahdtv": {f: fSource, val: string(SourceHDTV), primary: true}, "uhdtv": {f: fSource, val: string(SourceHDTV), primary: true},
	"pdtv": {f: fSource, val: string(SourcePDTV), primary: true}, "sdtv": {f: fSource, val: string(SourceSDTV), primary: true},
	"tvrip": {f: fSource, val: string(SourceSDTV), primary: true}, "dsr": {f: fSource, val: string(SourceSDTV), primary: true, upper: true},
	"dsrip": {f: fSource, val: string(SourceSDTV), primary: true}, "satrip": {f: fSource, val: string(SourceSDTV), primary: true},
	"dthrip": {f: fSource, val: string(SourceSDTV), primary: true}, "dvbrip": {f: fSource, val: string(SourceSDTV), primary: true},
	"ppvrip": {f: fSource, val: string(SourceHDTV), primary: true},
	"dvdrip": {f: fSource, val: string(SourceDVD), primary: true}, "dvd": {f: fSource, val: string(SourceDVD), primary: true, ambiguous: true},
	"dvd5": {f: fSource, val: string(SourceDVDR), primary: true}, "dvd9": {f: fSource, val: string(SourceDVDR), primary: true},
	"dvdr": {f: fSource, val: string(SourceDVDR), primary: true}, "dvd-r": {f: fSource, val: string(SourceDVDR), primary: true},
	"dvd-5": {f: fSource, val: string(SourceDVDR), primary: true}, "dvd-9": {f: fSource, val: string(SourceDVDR), primary: true},
	"dvdscr": {f: fSource, val: string(SourceScreener), primary: true}, "dvd-scr": {f: fSource, val: string(SourceScreener), primary: true},
	"screener": {f: fSource, val: string(SourceScreener), primary: true, ambiguous: true}, "scr": {f: fSource, val: string(SourceScreener), primary: true, ambiguous: true},
	"bdscr": {f: fSource, val: string(SourceScreener), primary: true}, "webscr": {f: fSource, val: string(SourceScreener), primary: true},
	"r5":  {f: fSource, val: string(SourceRegional), primary: true, upper: true},
	"cam": {f: fSource, val: string(SourceCam), primary: true, ambiguous: true}, "camrip": {f: fSource, val: string(SourceCam), primary: true},
	"hdcam": {f: fSource, val: string(SourceCam), primary: true}, "cam-rip": {f: fSource, val: string(SourceCam), primary: true},
	"ts": {f: fSource, val: string(SourceTelesync), primary: true, upper: true}, "telesync": {f: fSource, val: string(SourceTelesync), primary: true},
	"hdts": {f: fSource, val: string(SourceTelesync), primary: true}, "hd-ts": {f: fSource, val: string(SourceTelesync), primary: true},
	"pdvd": {f: fSource, val: string(SourceTelesync), primary: true}, "predvdrip": {f: fSource, val: string(SourceTelesync), primary: true},
	"tc": {f: fSource, val: string(SourceTelecine), primary: true, upper: true}, "telecine": {f: fSource, val: string(SourceTelecine), primary: true},
	"hdtc":      {f: fSource, val: string(SourceTelecine), primary: true},
	"workprint": {f: fSource, val: string(SourceWorkprint), primary: true}, "wp": {f: fSource, val: string(SourceWorkprint), primary: true, upper: true},
	"rawhd": {f: fSource, val: string(SourceRawHD), primary: true}, "raw-hd": {f: fSource, val: string(SourceRawHD), primary: true},
	"vhsrip": {f: fSource, val: string(SourceVHS), primary: true}, "vhs": {f: fSource, val: string(SourceVHS), primary: true, upper: true},

	// video codecs
	"x264": {f: fCodec, val: "x264", primary: true}, "h264": {f: fCodec, val: "h264", primary: true}, "h.264": {f: fCodec, val: "h264", primary: true},
	"avc": {f: fCodec, val: "h264", primary: true, ambiguous: true}, "x265": {f: fCodec, val: "x265", primary: true},
	"h265": {f: fCodec, val: "h265", primary: true}, "h.265": {f: fCodec, val: "h265", primary: true},
	"hevc": {f: fCodec, val: "h265", primary: true}, "av1": {f: fCodec, val: "av1", primary: true},
	"vc-1": {f: fCodec, val: "vc1", primary: true}, "vc1": {f: fCodec, val: "vc1", primary: true},
	"mpeg-2": {f: fCodec, val: "mpeg2", primary: true}, "mpeg2": {f: fCodec, val: "mpeg2", primary: true},
	"xvid": {f: fCodec, val: "xvid", primary: true}, "divx": {f: fCodec, val: "divx", primary: true},
	"vp9": {f: fCodec, val: "vp9", primary: true}, "hi10p": {f: fCodec, val: "hi10p", primary: true}, "hi10": {f: fCodec, val: "hi10p", primary: true},

	// HDR
	"hdr10+": {f: fHDR, val: "HDR10+", primary: true}, "hdr10plus": {f: fHDR, val: "HDR10+", primary: true},
	"hdr10p": {f: fHDR, val: "HDR10+", primary: true}, "hdr10": {f: fHDR, val: "HDR10", primary: true},
	"hdr": {f: fHDR, val: "HDR10", primary: true, ambiguous: true}, "dv": {f: fHDR, val: "DV", primary: true, upper: true},
	"dovi": {f: fHDR, val: "DV", primary: true}, "dolbyvision": {f: fHDR, val: "DV", primary: true},
	"hlg": {f: fHDR, val: "HLG", primary: true}, "sdr": {f: fFlag, val: "sdr", upper: true},
	"fel": {f: fFlag, val: "dv-fel", upper: true}, "mel": {f: fFlag, val: "dv-mel", upper: true},

	// audio
	"ddp": {f: fAudio, val: "eac3", primary: true}, "dd+": {f: fAudio, val: "eac3", primary: true},
	"eac3": {f: fAudio, val: "eac3", primary: true}, "e-ac-3": {f: fAudio, val: "eac3", primary: true},
	"ddpa": {f: fAudio, val: "eac3+atmos", primary: true}, "dd": {f: fAudio, val: "ac3", primary: true, upper: true},
	"ac3": {f: fAudio, val: "ac3", primary: true}, "ac-3": {f: fAudio, val: "ac3", primary: true},
	"aac": {f: fAudio, val: "aac", primary: true}, "he-aac": {f: fAudio, val: "aac", primary: true}, "heaac": {f: fAudio, val: "aac", primary: true},
	"lc-aac": {f: fAudio, val: "aac", primary: true}, "aaclc": {f: fAudio, val: "aac", primary: true},
	"dts": {f: fAudio, val: "dts", primary: true}, "dts-hd": {f: fAudio, val: "dts-hd", primary: true},
	"dtshd": {f: fAudio, val: "dts-hd", primary: true}, "dts-hdma": {f: fAudio, val: "dts-hd ma", primary: true},
	"dtshdma": {f: fAudio, val: "dts-hd ma", primary: true}, "dts-ma": {f: fAudio, val: "dts-hd ma", primary: true},
	"dts-x": {f: fAudio, val: "dts:x", primary: true}, "dtsx": {f: fAudio, val: "dts:x", primary: true},
	"dts:x": {f: fAudio, val: "dts:x", primary: true}, "dts-es": {f: fAudio, val: "dts-es", primary: true},
	"dtses": {f: fAudio, val: "dts-es", primary: true}, "truehd": {f: fAudio, val: "truehd", primary: true},
	"flac": {f: fAudio, val: "flac", primary: true}, "opus": {f: fAudio, val: "opus", primary: true, ambiguous: true},
	"lpcm": {f: fAudio, val: "pcm", primary: true}, "pcm": {f: fAudio, val: "pcm", primary: true},
	"mp3": {f: fAudio, val: "mp3", primary: true}, "mp2": {f: fAudio, val: "mp2", primary: true},
	"vorbis": {f: fAudio, val: "vorbis", primary: true, ambiguous: true}, "ogg": {f: fAudio, val: "vorbis", primary: true, ambiguous: true},
	"alac": {f: fAudio, val: "alac", primary: true}, "wav": {f: fAudio, val: "wav", primary: true},
	"ape": {f: fAudio, val: "ape", primary: true, upper: true}, "aiff": {f: fAudio, val: "wav", primary: true},
	"dsd": {f: fAudio, val: "dsd", primary: true, upper: true}, "atmos": {f: fAtmos, primary: true, ambiguous: true},
	"2ch": {f: fChannels, val: "2.0"}, "6ch": {f: fChannels, val: "5.1"}, "8ch": {f: fChannels, val: "7.1"},
	"stereo": {f: fChannels, val: "2.0"}, "mono": {f: fChannels, val: "1.0"},

	// languages handled by the languages package; these are the multi/dual markers
	"multi": {f: fMulti, upper: true}, "multi-audio": {f: fMulti}, "multiaudio": {f: fMulti},
	"multi-lang": {f: fMulti}, "multilang": {f: fMulti}, "multi-language": {f: fMulti},
	"dual": {f: fDual, upper: true}, "dual-audio": {f: fDual}, "dualaudio": {f: fDual},
	"dl":       {f: fDual, upper: true},
	"multisub": {f: fSubs, val: "multi"}, "multisubs": {f: fSubs, val: "multi"}, "multi-sub": {f: fSubs, val: "multi"},
	"multi-subs": {f: fSubs, val: "multi"}, "subbed": {f: fSubs}, "subs": {f: fSubs, upper: true}, "sub": {f: fSubs, upper: true},
	"vostfr": {f: fSubs, val: "fr"}, "vost": {f: fSubs, upper: true}, "subfrench": {f: fSubs, val: "fr"},
	"esub": {f: fSubs, val: "en"}, "esubs": {f: fSubs, val: "en"}, "engsub": {f: fSubs, val: "en"}, "engsubs": {f: fSubs, val: "en"},
	"nlsub": {f: fSubs, val: "nl"}, "nlsubs": {f: fSubs, val: "nl"}, "swesub": {f: fSubs, val: "sv"}, "hrsub": {f: fSubs, val: "hr"},
	"hrsubs": {f: fSubs, val: "hr"}, "titlovi": {f: fSubs, val: "hr"},
	"hc": {f: fHardsub, upper: true}, "hcsub": {f: fHardsub}, "hc-sub": {f: fHardsub}, "hcsubs": {f: fHardsub},
	"hardsub": {f: fHardsub}, "hardsubs": {f: fHardsub}, "hardcoded": {f: fHardsub}, "korsub": {f: fHardsub, val: "ko"},
	"korsubs": {f: fHardsub, val: "ko"},
	"dubbed":  {f: fFlag, val: "dubbed"}, "dub": {f: fFlag, val: "dubbed", upper: true},

	// editions
	"extended": {f: fEdition, val: "Extended"}, "unrated": {f: fEdition, val: "Unrated"},
	"uncut": {f: fEdition, val: "Uncut"}, "theatrical": {f: fEdition, val: "Theatrical"},
	"imax": {f: fEdition, val: "IMAX"}, "remastered": {f: fEdition, val: "Remastered"}, "remaster": {f: fEdition, val: "Remastered"},
	"criterion": {f: fEdition, val: "Criterion"}, "redux": {f: fEdition, val: "Redux"},
	"despecialized": {f: fEdition, val: "Despecialized"}, "restored": {f: fEdition, val: "Restored", upper: true},
	"uncensored": {f: fEdition, val: "Uncensored"}, "dc": {f: fEdition, val: "Director's Cut", upper: true},

	// 3D
	"3d": {f: f3D, primary: true}, "hsbs": {f: f3D, primary: true}, "h-sbs": {f: f3D, primary: true},
	"half-sbs": {f: f3D, primary: true}, "hou": {f: f3D, primary: true, upper: true}, "h-ou": {f: f3D, primary: true},
	"half-ou": {f: f3D, primary: true}, "sbs": {f: f3D, primary: true, upper: true}, "mvc": {f: f3D, primary: true, upper: true},

	// revisions
	"proper": {f: fRevision, val: "proper"}, "repack": {f: fRevision, val: "repack"}, "rerip": {f: fRevision, val: "repack"},
	"real": {f: fRevision, val: "real", upper: true},

	// other flags
	"internal": {f: fFlag, val: "internal"}, "int": {f: fFlag, val: "internal", upper: true},
	"limited": {f: fFlag, val: "limited", upper: true}, "readnfo": {f: fFlag, val: "readnfo"},
	"nfofix": {f: fFlag, val: "nfofix"}, "dirfix": {f: fFlag, val: "dirfix"}, "syncfix": {f: fFlag, val: "syncfix"},
	"subfix": {f: fFlag, val: "subfix"}, "hybrid": {f: fFlag, val: "hybrid"},
	"upscaled": {f: fFlag, val: "upscaled"}, "upscale": {f: fFlag, val: "upscaled"}, "ai-upscale": {f: fFlag, val: "upscaled"},
	"ws": {f: fFlag, val: "widescreen", upper: true}, "fs": {f: fFlag, val: "fullscreen", upper: true},
	"oar": {f: fFlag, val: "oar", upper: true}, "extras": {f: fFlag, val: "extras"},
	"bonus": {f: fFlag, val: "extras", upper: true}, "featurettes": {f: fFlag, val: "extras"},
	"trailer": {f: fFlag, val: "trailer", upper: true}, "docu": {f: fFlag, val: "documentary", upper: true},
	"convert": {f: fFlag, val: "convert", upper: true}, "untouched": {f: fFlag, val: "untouched"},
	"ntsc": {f: fFlag, val: "ntsc", upper: true}, "pal": {f: fFlag, val: "pal", upper: true},
	"complete": {f: fComplete}, "batch": {f: fComplete, val: "batch"},
	"rip": {f: fExtra, upper: true}, "hq": {f: fExtra, upper: true}, "fhd": {f: fResolution, n: 1080, primary: true},

	// music
	"v0": {f: fMusicBitrate, val: "V0"}, "v2": {f: fMusicBitrate, val: "V2"}, "vbr": {f: fMusicBitrate, val: "VBR"},
	"cbr": {f: fMusicBitrate, val: "CBR"}, "lossless": {f: fFlag, val: "lossless"},
	"vinyl": {f: fMusicSource, val: "vinyl"}, "lp": {f: fMusicSource, val: "vinyl", upper: true},
	"cd": {f: fMusicSource, val: "cd", upper: true}, "cdda": {f: fMusicSource, val: "cd"}, "sacd": {f: fMusicSource, val: "sacd"},
	"cassette": {f: fMusicSource, val: "tape"}, "discography": {f: fFlag, val: "discography"},
}

// phrase is a fixed sequence of (normalised, lower-case) tokens.
type phrase struct {
	words []string
	d     def
}

var phraseList = []phrase{
	{[]string{"web", "dl"}, def{f: fSource, val: string(SourceWEBDL), primary: true}},
	{[]string{"web", "rip"}, def{f: fSource, val: string(SourceWEBRip), primary: true}},
	{[]string{"blu", "ray"}, def{f: fSource, val: string(SourceBluRay), primary: true}},
	{[]string{"uhd", "bluray"}, def{f: fSource, val: "uhd-bluray", primary: true}},
	{[]string{"uhd", "blu-ray"}, def{f: fSource, val: "uhd-bluray", primary: true}},
	{[]string{"uhd", "blu", "ray"}, def{f: fSource, val: "uhd-bluray", primary: true}},
	{[]string{"uhd", "bdremux"}, def{f: fSource, val: "uhd-remux", primary: true}},
	{[]string{"uhd", "remux"}, def{f: fSource, val: "uhd-remux", primary: true}},
	{[]string{"complete", "bluray"}, def{f: fSource, val: string(SourceBRDisk), primary: true}},
	{[]string{"complete", "blu-ray"}, def{f: fSource, val: string(SourceBRDisk), primary: true}},
	{[]string{"complete", "uhd", "bluray"}, def{f: fSource, val: string(SourceBRDisk), primary: true}},
	{[]string{"complete", "uhd", "blu-ray"}, def{f: fSource, val: string(SourceBRDisk), primary: true}},
	{[]string{"hd", "dvd"}, def{f: fSource, val: string(SourceBluRay), primary: true}},
	{[]string{"dvd", "rip"}, def{f: fSource, val: string(SourceDVD), primary: true}},
	{[]string{"raw", "hd"}, def{f: fSource, val: string(SourceRawHD), primary: true}},
	{[]string{"hd", "tv"}, def{f: fSource, val: string(SourceHDTV), primary: true}},
	{[]string{"dvd", "scr"}, def{f: fSource, val: string(SourceScreener), primary: true}},
	{[]string{"hd", "cam"}, def{f: fSource, val: string(SourceCam), primary: true}},
	{[]string{"hd", "ts"}, def{f: fSource, val: string(SourceTelesync), primary: true}},
	{[]string{"hd", "rip"}, def{f: fSource, val: string(SourceHDRip), primary: true}},
	{[]string{"tv", "rip"}, def{f: fSource, val: string(SourceSDTV), primary: true}},
	{[]string{"h", "264"}, def{f: fCodec, val: "h264", primary: true}},
	{[]string{"h", "265"}, def{f: fCodec, val: "h265", primary: true}},
	{[]string{"x", "264"}, def{f: fCodec, val: "x264", primary: true}},
	{[]string{"x", "265"}, def{f: fCodec, val: "x265", primary: true}},
	{[]string{"dts-hd", "ma"}, def{f: fAudio, val: "dts-hd ma", primary: true}},
	{[]string{"dts", "hd", "ma"}, def{f: fAudio, val: "dts-hd ma", primary: true}},
	{[]string{"dts", "ma"}, def{f: fAudio, val: "dts-hd ma", primary: true}},
	{[]string{"dts-hd", "hra"}, def{f: fAudio, val: "dts-hd hra", primary: true}},
	{[]string{"dts-hd", "hr"}, def{f: fAudio, val: "dts-hd hra", primary: true}},
	{[]string{"dts", "hd", "hra"}, def{f: fAudio, val: "dts-hd hra", primary: true}},
	{[]string{"dts", "x"}, def{f: fAudio, val: "dts:x", primary: true}},
	{[]string{"dts", "es"}, def{f: fAudio, val: "dts-es", primary: true}},
	{[]string{"true", "hd"}, def{f: fAudio, val: "truehd", primary: true}},
	{[]string{"dolby", "digital", "plus"}, def{f: fAudio, val: "eac3", primary: true}},
	{[]string{"dolby", "digital"}, def{f: fAudio, val: "ac3", primary: true}},
	{[]string{"dolby", "atmos"}, def{f: fAtmos, primary: true}},
	{[]string{"dolby", "vision"}, def{f: fHDR, val: "DV", primary: true}},
	{[]string{"hdr10", "plus"}, def{f: fHDR, val: "HDR10+", primary: true}},
	{[]string{"hdr", "10+"}, def{f: fHDR, val: "HDR10+", primary: true}},
	{[]string{"hdr", "10"}, def{f: fHDR, val: "HDR10", primary: true}},
	{[]string{"dual", "audio"}, def{f: fDual}},
	{[]string{"multi", "audio"}, def{f: fMulti}},
	{[]string{"multi", "subs"}, def{f: fSubs, val: "multi"}},
	{[]string{"multi", "sub"}, def{f: fSubs, val: "multi"}},
	{[]string{"multiple", "subtitle"}, def{f: fSubs, val: "multi"}},
	{[]string{"multiple", "subtitles"}, def{f: fSubs, val: "multi"}},
	{[]string{"hard", "coded"}, def{f: fHardsub}},
	{[]string{"hc", "subs"}, def{f: fHardsub}},
	{[]string{"read", "nfo"}, def{f: fFlag, val: "readnfo"}},
	{[]string{"directors", "cut"}, def{f: fEdition, val: "Director's Cut"}},
	{[]string{"director", "cut"}, def{f: fEdition, val: "Director's Cut"}},
	{[]string{"extended", "cut"}, def{f: fEdition, val: "Extended"}},
	{[]string{"extended", "edition"}, def{f: fEdition, val: "Extended"}},
	{[]string{"extended", "version"}, def{f: fEdition, val: "Extended"}},
	{[]string{"theatrical", "cut"}, def{f: fEdition, val: "Theatrical"}},
	{[]string{"theatrical", "edition"}, def{f: fEdition, val: "Theatrical"}},
	{[]string{"unrated", "cut"}, def{f: fEdition, val: "Unrated"}},
	{[]string{"unrated", "edition"}, def{f: fEdition, val: "Unrated"}},
	{[]string{"imax", "enhanced"}, def{f: fEdition, val: "IMAX"}},
	{[]string{"imax", "edition"}, def{f: fEdition, val: "IMAX"}},
	{[]string{"criterion", "collection"}, def{f: fEdition, val: "Criterion"}},
	{[]string{"criterion", "edition"}, def{f: fEdition, val: "Criterion"}},
	{[]string{"special", "edition"}, def{f: fEdition, val: "Special Edition"}},
	{[]string{"collectors", "edition"}, def{f: fEdition, val: "Collector's Edition"}},
	{[]string{"ultimate", "edition"}, def{f: fEdition, val: "Ultimate"}},
	{[]string{"ultimate", "cut"}, def{f: fEdition, val: "Ultimate"}},
	{[]string{"final", "cut"}, def{f: fEdition, val: "Final Cut"}},
	{[]string{"anniversary", "edition"}, def{f: fEdition, val: "Anniversary"}},
	{[]string{"open", "matte"}, def{f: fEdition, val: "Open Matte"}},
	{[]string{"rogue", "cut"}, def{f: fEdition, val: "Rogue Cut"}},
	{[]string{"black", "and", "chrome"}, def{f: fEdition, val: "Black and Chrome"}},
	{[]string{"black", "&", "chrome"}, def{f: fEdition, val: "Black and Chrome"}},
	{[]string{"4k", "remaster"}, def{f: fEdition, val: "Remastered"}},
	{[]string{"4k", "remastered"}, def{f: fEdition, val: "Remastered"}},
	{[]string{"deluxe", "edition"}, def{f: fEdition, val: "Deluxe Edition"}},
	{[]string{"limited", "edition"}, def{f: fEdition, val: "Limited Edition"}},
	{[]string{"assembly", "cut"}, def{f: fEdition, val: "Assembly Cut"}},
	{[]string{"producers", "cut"}, def{f: fEdition, val: "Producer's Cut"}},
	{[]string{"half", "sbs"}, def{f: f3D, primary: true}},
	{[]string{"half", "ou"}, def{f: f3D, primary: true}},
	{[]string{"complete", "series"}, def{f: fComplete, val: "series"}},
	{[]string{"the", "complete", "series"}, def{f: fComplete, val: "series"}},
	{[]string{"complete", "seasons"}, def{f: fComplete, val: "series"}},
	{[]string{"all", "seasons"}, def{f: fComplete, val: "series"}},
	{[]string{"web", "flac"}, def{f: fAudio, val: "flac", primary: true}},
	{[]string{"10", "bit"}, def{f: fBitDepth, n: 10, primary: true}},
	{[]string{"8", "bit"}, def{f: fBitDepth, n: 8, primary: true}},
	{[]string{"12", "bit"}, def{f: fBitDepth, n: 12, primary: true}},
	{[]string{"16", "bit"}, def{f: fBitDepth, n: 16, primary: true}},
	{[]string{"24", "bit"}, def{f: fBitDepth, n: 24, primary: true}},
}

var phrases = func() map[string][]phrase {
	m := map[string][]phrase{}
	for _, p := range phraseList {
		m[p.words[0]] = append(m[p.words[0]], p)
	}
	// Longest first, so "dts hd ma" wins over "dts ma".
	for k, list := range m {
		for i := 1; i < len(list); i++ {
			for j := i; j > 0 && len(list[j].words) > len(list[j-1].words); j-- {
				list[j], list[j-1] = list[j-1], list[j]
			}
		}
		m[k] = list
	}
	return m
}()

// streaming services: tag -> name. Tags in streamingUpper only count in capitals
// (or the exact spelling given), since they are also ordinary words.
var streaming = map[string]string{
	"amzn": "Amazon", "amazon": "Amazon", "dsnp": "Disney+", "dsny": "Disney+", "disney+": "Disney+",
	"atvp": "Apple TV+", "aptv": "Apple TV+", "atv+": "Apple TV+", "hmax": "HBO Max", "hulu": "Hulu",
	"pcok": "Peacock", "pmtp": "Paramount+", "pmnt": "Paramount+", "crav": "Crave", "bcore": "Bravia Core",
	"funi": "Funimation", "hidi": "HIDIVE", "hidive": "HIDIVE", "vudu": "Vudu", "roku": "Roku",
	"all4": "Channel 4", "my5": "Channel 5", "skst": "SkyShowtime", "tvnz": "TVNZ", "dscp": "Discovery+",
	"crkl": "Crackle", "itvx": "ITVX", "viap": "Viaplay", "viaplay": "Viaplay", "mubi": "MUBI",
	"tubi": "Tubi", "unxt": "U-Next", "zee5": "ZEE5", "snxt": "Sun NXT", "htsr": "Hotstar",
	"nflx": "Netflix", "netflix": "Netflix", "joyn": "Joyn", "rtl+": "RTL+", "amcp": "AMC+", "shout": "Shout! Factory",
	"stz": "Starz", "strz": "Starz", "ufc": "UFC Fight Pass", "mgmp": "MGM+", "aubc": "ABC iview", "sbs": "SBS",
	"vrv": "VRV", "viki": "Viki", "adn": "ADN", "wavve": "Wavve", "tving": "TVING",
}

var streamingUpper = map[string]string{
	"NF": "Netflix", "MAX": "Max", "HBO": "HBO", "STAN": "Stan", "CR": "Crunchyroll", "NOW": "Now",
	"SHO": "Showtime", "AMC": "AMC", "iT": "iTunes", "iP": "BBC iPlayer", "RED": "YouTube Premium",
	"CC": "Comedy Central", "BBC": "BBC", "NBC": "NBC", "CBS": "CBS", "FOX": "FOX", "ABC": "ABC",
	"DSNY": "Disney+", "SBS": "SBS", "TVING": "TVING", "PLAY": "Play", "HTSR": "Hotstar",
}

// extensions the parser strips (and remembers).
var extensions = map[string]string{
	"mkv": "video", "mp4": "video", "avi": "video", "m4v": "video", "m2ts": "video", "wmv": "video",
	"mov": "video", "webm": "video", "mpg": "video", "mpeg": "video", "iso": "video", "vob": "video",
	"flv": "video", "divx": "video", "ogm": "video", "rmvb": "video", "3gp": "video", "mts": "video",
	"flac": "audio", "mp3": "audio", "m4a": "audio", "aac": "audio", "ogg": "audio", "opus": "audio",
	"wav": "audio", "ape": "audio", "wv": "audio", "dsf": "audio", "dff": "audio", "alac": "audio", "aiff": "audio",
	"srt": "subtitle", "ass": "subtitle", "ssa": "subtitle", "sub": "subtitle", "idx": "subtitle",
	"vtt": "subtitle", "sup": "subtitle",
	"rar": "archive", "zip": "archive", "7z": "archive", "r00": "archive",
	"nfo": "other", "sfv": "other", "nzb": "other", "torrent": "other", "txt": "other", "jpg": "other", "png": "other",
}
