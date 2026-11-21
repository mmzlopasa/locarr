// Package languages is the catalogue of audio and subtitle languages Locarr
// understands: ISO 639-1 codes, English and native names, the words that
// release names use for them, and the numeric ids used by the commonly shared
// custom-format JSON.
package languages

import (
	"sort"
	"strings"
)

// Special codes used in profiles.
const (
	// Any accepts every language.
	Any = "any"
	// Original is the title's original language (known from metadata).
	Original = "original"
	// Unknown is a release whose name does not say.
	Unknown = "unknown"
)

// Language is one language.
type Language struct {
	Code   string `json:"code"`   // ISO 639-1 (or a short tag such as "pt-br", "es-419")
	Name   string `json:"name"`   // English name
	Native string `json:"native"` // name in the language itself
	// LegacyID is the numeric id the commonly shared custom-format JSON uses
	// for this language (0 = none).
	LegacyID int `json:"legacyId,omitempty"`
}

// All lists the known languages (sorted by English name when returned by List).
var all = []Language{
	{"en", "English", "English", 1},
	{"fr", "French", "Français", 2},
	{"es", "Spanish", "Español", 3},
	{"de", "German", "Deutsch", 4},
	{"it", "Italian", "Italiano", 5},
	{"da", "Danish", "Dansk", 6},
	{"nl", "Dutch", "Nederlands", 7},
	{"ja", "Japanese", "日本語", 8},
	{"is", "Icelandic", "Íslenska", 9},
	{"zh", "Chinese", "中文", 10},
	{"ru", "Russian", "Русский", 11},
	{"pl", "Polish", "Polski", 12},
	{"vi", "Vietnamese", "Tiếng Việt", 13},
	{"sv", "Swedish", "Svenska", 14},
	{"no", "Norwegian", "Norsk", 15},
	{"fi", "Finnish", "Suomi", 16},
	{"tr", "Turkish", "Türkçe", 17},
	{"pt", "Portuguese", "Português", 18},
	{"nl-be", "Flemish", "Vlaams", 19},
	{"el", "Greek", "Ελληνικά", 20},
	{"ko", "Korean", "한국어", 21},
	{"hu", "Hungarian", "Magyar", 22},
	{"he", "Hebrew", "עברית", 23},
	{"lt", "Lithuanian", "Lietuvių", 24},
	{"cs", "Czech", "Čeština", 25},
	{"hi", "Hindi", "हिन्दी", 26},
	{"ro", "Romanian", "Română", 27},
	{"th", "Thai", "ไทย", 28},
	{"bg", "Bulgarian", "Български", 29},
	{"pt-br", "Portuguese (Brazil)", "Português (Brasil)", 30},
	{"ar", "Arabic", "العربية", 31},
	{"uk", "Ukrainian", "Українська", 32},
	{"fa", "Persian", "فارسی", 33},
	{"bn", "Bengali", "বাংলা", 34},
	{"sk", "Slovak", "Slovenčina", 35},
	{"lv", "Latvian", "Latviešu", 36},
	{"es-419", "Spanish (Latin America)", "Español (Latinoamérica)", 37},
	{"ca", "Catalan", "Català", 38},
	{"hr", "Croatian", "Hrvatski", 39},
	{"sr", "Serbian", "Српски / Srpski", 40},
	{"bs", "Bosnian", "Bosanski", 41},
	{"et", "Estonian", "Eesti", 42},
	{"ta", "Tamil", "தமிழ்", 43},
	{"id", "Indonesian", "Bahasa Indonesia", 44},
	{"te", "Telugu", "తెలుగు", 45},
	{"mk", "Macedonian", "Македонски", 46},
	{"sl", "Slovenian", "Slovenščina", 47},
	{"ms", "Malay", "Bahasa Melayu", 48},
	{"ml", "Malayalam", "മലയാളം", 49},
	{"kn", "Kannada", "ಕನ್ನಡ", 50},
	{"sq", "Albanian", "Shqip", 51},
	{"af", "Afrikaans", "Afrikaans", 52},
	{"mr", "Marathi", "मराठी", 53},
	{"tl", "Tagalog", "Tagalog", 54},
	{"ur", "Urdu", "اردو", 55},
}

var byCode = func() map[string]Language {
	m := make(map[string]Language, len(all))
	for _, l := range all {
		m[l.Code] = l
	}
	return m
}()

var byLegacy = func() map[int]string {
	m := make(map[int]string, len(all)+2)
	for _, l := range all {
		m[l.LegacyID] = l.Code
	}
	m[-1] = Any
	m[-2] = Original
	m[0] = Unknown
	return m
}()

// List returns every known language sorted by English name.
func List() []Language {
	out := append([]Language(nil), all...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ByCode returns the language with the given code.
func ByCode(code string) (Language, bool) {
	l, ok := byCode[strings.ToLower(code)]
	return l, ok
}

// Valid reports whether code is a known language or one of the special codes.
func Valid(code string) bool {
	switch code {
	case Any, Original, Unknown:
		return true
	}
	_, ok := byCode[code]
	return ok
}

// FromLegacyID maps a numeric id from shared custom-format JSON to a code.
func FromLegacyID(id int) (string, bool) {
	c, ok := byLegacy[id]
	return c, ok
}

// LegacyID maps a code back to the shared numeric id.
func LegacyID(code string) (int, bool) {
	switch code {
	case Any:
		return -1, true
	case Original:
		return -2, true
	case Unknown:
		return 0, true
	}
	l, ok := byCode[code]
	return l.LegacyID, ok
}

// Name returns the English name of code (the code itself when unknown).
func Name(code string) string {
	if l, ok := byCode[code]; ok {
		return l.Name
	}
	return code
}

// Words release names use for languages. Strong words are unambiguous in any
// case; weak words (two- and three-letter codes) only count when written in
// capitals, since they are also ordinary words or title fragments.
var strong = map[string]string{
	"english": "en", "eng": "en",
	"french": "fr", "truefrench": "fr", "francais": "fr", "vff": "fr", "vfq": "fr", "vfi": "fr", "vf2": "fr",
	"spanish": "es", "espanol": "es", "castellano": "es", "spa": "es",
	"latino": "es-419", "latin": "es-419",
	"german": "de", "deutsch": "de", "ger": "de", "deu": "de",
	"italian": "it", "italiano": "it", "ita": "it",
	"danish": "da", "dansk": "da",
	"dutch": "nl", "nederlands": "nl", "nld": "nl",
	"flemish": "nl-be", "vlaams": "nl-be",
	"japanese": "ja", "jap": "ja", "jpn": "ja",
	"icelandic": "is",
	"chinese":   "zh", "mandarin": "zh", "cantonese": "zh", "chi": "zh", "chs": "zh", "cht": "zh", "zho": "zh",
	"russian": "ru", "rus": "ru",
	"polish": "pl", "polski": "pl", "pldub": "pl", "plsub": "pl",
	"vietnamese": "vi",
	"swedish":    "sv", "svenska": "sv", "swe": "sv",
	"norwegian": "no", "norsk": "no",
	"finnish": "fi", "suomi": "fi",
	"turkish":    "tr",
	"portuguese": "pt", "portugues": "pt",
	"brazilian": "pt-br", "dublado": "pt-br", "ptbr": "pt-br", "pt-br": "pt-br",
	"greek":  "el",
	"korean": "ko", "kor": "ko",
	"hungarian": "hu", "magyar": "hu", "hun": "hu",
	"hebrew":     "he",
	"lithuanian": "lt",
	"czech":      "cs", "cze": "cs",
	"hindi":     "hi",
	"romanian":  "ro",
	"thai":      "th",
	"bulgarian": "bg",
	"arabic":    "ar",
	"ukrainian": "uk", "ukr": "uk",
	"persian": "fa", "farsi": "fa",
	"bengali":  "bn",
	"slovak":   "sk",
	"latvian":  "lv",
	"catalan":  "ca",
	"croatian": "hr", "hrvatski": "hr", "cro": "hr", "hrv": "hr",
	"serbian": "sr", "srpski": "sr", "srp": "sr", "srb": "sr",
	"bosnian": "bs", "bosanski": "bs", "bos": "bs",
	"estonian":   "et",
	"tamil":      "ta",
	"indonesian": "id",
	"telugu":     "te",
	"macedonian": "mk",
	"slovenian":  "sl", "slovene": "sl",
	"malay":     "ms",
	"malayalam": "ml",
	"kannada":   "kn",
	"albanian":  "sq",
	"marathi":   "mr",
	"tagalog":   "tl", "filipino": "tl",
	"urdu": "ur",
}

// weak words: only in capitals.
var weak = map[string]string{
	"EN": "en", "FR": "fr", "VF": "fr", "ES": "es", "DE": "de", "IT": "it", "NL": "nl", "JP": "ja",
	"RU": "ru", "PL": "pl", "SE": "sv", "NO": "no", "FI": "fi", "TR": "tr", "PT": "pt", "BR": "pt-br",
	"KR": "ko", "HU": "hu", "CZ": "cs", "HR": "hr", "SR": "sr", "BA": "bs", "SI": "sl", "SK": "sk",
	"UA": "uk", "GR": "el", "IL": "he", "CN": "zh", "DK": "da", "LT": "lt", "LV": "lv", "BG": "bg",
	"RO": "ro", "MK": "mk", "ID": "id", "IN": "hi", "TH": "th", "VN": "vi",
	"ALB": "sq",
	"ARA": "ar",
	"BEN": "bn",
	"BUL": "bg",
	"CAT": "ca",
	"CES": "cs",
	"DAN": "da",
	"DUT": "nl",
	"ELL": "el",
	"EST": "et",
	"FIN": "fi",
	"GRE": "el",
	"HEB": "he",
	"HIN": "hi",
	"IND": "id",
	"KAN": "kn",
	"LAV": "lv",
	"LIT": "lt",
	"MAC": "mk",
	"MAL": "ml",
	"MAY": "ms",
	"MKD": "mk",
	"NOR": "no",
	"PER": "fa",
	"POL": "pl",
	"POR": "pt",
	"RON": "ro",
	"RUM": "ro",
	"SLK": "sk",
	"SLO": "sk",
	"SLV": "sl",
	"TAM": "ta",
	"TEL": "te",
	"THA": "th",
	"TUR": "tr",
	"VIE": "vi",
}

// FromWord returns the language a release-name word stands for. capitals says
// whether the word was written in capitals in the name.
func FromWord(word string, capitals bool) (string, bool) {
	if c, ok := strong[strings.ToLower(word)]; ok {
		return c, true
	}
	if capitals {
		if c, ok := weak[strings.ToUpper(word)]; ok {
			return c, true
		}
	}
	return "", false
}
