package languages

import "strings"

// iso6392 maps ISO 639-2 (bibliographic and terminology) codes, which video
// containers use for track languages, to the codes of this catalogue.
var iso6392 = map[string]string{
	"eng": "en", "fre": "fr", "fra": "fr", "spa": "es", "ger": "de", "deu": "de", "ita": "it", "dan": "da",
	"dut": "nl", "nld": "nl", "jpn": "ja", "ice": "is", "isl": "is", "chi": "zh", "zho": "zh", "rus": "ru",
	"pol": "pl", "vie": "vi", "swe": "sv", "nor": "no", "nob": "no", "nno": "no", "fin": "fi", "tur": "tr",
	"por": "pt", "gre": "el", "ell": "el", "kor": "ko", "hun": "hu", "heb": "he", "lit": "lt", "cze": "cs",
	"ces": "cs", "hin": "hi", "rum": "ro", "ron": "ro", "tha": "th", "bul": "bg", "ara": "ar", "ukr": "uk",
	"per": "fa", "fas": "fa", "ben": "bn", "slo": "sk", "slk": "sk", "lav": "lv", "cat": "ca", "hrv": "hr",
	"scr": "hr", "srp": "sr", "scc": "sr", "bos": "bs", "est": "et", "tam": "ta", "ind": "id", "tel": "te",
	"mac": "mk", "mkd": "mk", "slv": "sl", "may": "ms", "msa": "ms", "mal": "ml", "kan": "kn", "alb": "sq",
	"sqi": "sq", "afr": "af", "mar": "mr", "tgl": "tl", "fil": "tl", "urd": "ur",
}

// FromContainer maps a track language as video containers write it (ISO
// 639-2 such as "hrv", ISO 639-1, or a BCP 47 tag such as "pt-BR") to a code
// of this catalogue. Undetermined ("und", "") and unknown values give "".
func FromContainer(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "und" || s == "zxx" || s == "mul" {
		return ""
	}
	if _, ok := byCode[s]; ok {
		return s
	}
	base, region, _ := strings.Cut(strings.ReplaceAll(s, "_", "-"), "-")
	if c, ok := iso6392[base]; ok {
		base = c
	}
	if region != "" {
		if _, ok := byCode[base+"-"+region]; ok {
			return base + "-" + region
		}
	}
	if _, ok := byCode[base]; ok {
		return base
	}
	return ""
}
