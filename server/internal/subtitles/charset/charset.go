// Package charset finds the text encoding of a subtitle file and converts
// it to UTF-8. Subtitles come in UTF-8 (with or without BOM), UTF-16, and
// the old single-byte code pages of their language: Windows-1250 /
// ISO-8859-2 for Croatian, Bosnian, Serbian Latin, Slovenian, Czech,
// Polish…; Windows-1251 / ISO-8859-5 / KOI8-R for Cyrillic; Windows-1252 /
// ISO-8859-1/15 for western languages, and a few more. The expected
// language decides between code pages that would all decode without error.
package charset

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

// Encoding names returned by Detect.
const (
	UTF8    = "utf-8"
	UTF16LE = "utf-16le"
	UTF16BE = "utf-16be"
)

// ErrBinary is returned for data that is not text in any known encoding.
var ErrBinary = errors.New("not a text file")

type candidate struct {
	name string
	enc  encoding.Encoding
}

var (
	cp1250 = candidate{"windows-1250", charmap.Windows1250}
	iso2   = candidate{"iso-8859-2", charmap.ISO8859_2}
	cp1251 = candidate{"windows-1251", charmap.Windows1251}
	iso5   = candidate{"iso-8859-5", charmap.ISO8859_5}
	koi8r  = candidate{"koi8-r", charmap.KOI8R}
	cp1252 = candidate{"windows-1252", charmap.Windows1252}
	iso1   = candidate{"iso-8859-1", charmap.ISO8859_1}
	iso15  = candidate{"iso-8859-15", charmap.ISO8859_15}
	cp1253 = candidate{"windows-1253", charmap.Windows1253}
	iso7   = candidate{"iso-8859-7", charmap.ISO8859_7}
	cp1254 = candidate{"windows-1254", charmap.Windows1254}
	iso9   = candidate{"iso-8859-9", charmap.ISO8859_9}
	cp1255 = candidate{"windows-1255", charmap.Windows1255}
	cp1256 = candidate{"windows-1256", charmap.Windows1256}
	cp1257 = candidate{"windows-1257", charmap.Windows1257}
	iso13  = candidate{"iso-8859-13", charmap.ISO8859_13}
)

// letters are the non-ASCII letters a language uses; a decoding that
// produces them scores, anything else counts against it.
var letters = map[string]string{
	"hr": "čćđšžČĆĐŠŽ", "bs": "čćđšžČĆĐŠŽ", "sr": "čćđšžČĆĐŠŽабвгдђежзијклљмнњопрстћуфхцчџшАБВГДЂЕЖЗИЈКЛЉМНЊОПРСТЋУФХЦЧЏШ",
	"sl": "čšžČŠŽ", "cs": "áčďéěíňóřšťúůýžÁČĎÉĚÍŇÓŘŠŤÚŮÝŽ", "sk": "áäčďéíĺľňóôŕšťúýžÁÄČĎÉÍĹĽŇÓÔŔŠŤÚÝŽ",
	"pl": "ąćęłńóśźżĄĆĘŁŃÓŚŹŻ", "hu": "áéíóöőúüűÁÉÍÓÖŐÚÜŰ", "ro": "ăâîșțşţĂÂÎȘȚŞŢ", "sq": "çëÇË",
	"ru": "абвгдежзийклмнопрстуфхцчшщъыьэюяёАБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯЁ",
	"uk": "абвгґдеєжзиіїйклмнопрстуфхцчшщьюяАБВГҐДЕЄЖЗИІЇЙКЛМНОПРСТУФХЦЧШЩЬЮЯ",
	"bg": "абвгдежзийклмнопрстуфхцчшщъьюяАБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЬЮЯ",
	"mk": "абвгдѓежзѕијклљмнњопрстќуфхцчџшАБВГДЃЕЖЗЅИЈКЛЉМНЊОПРСТЌУФХЦЧЏШ",
	"de": "äöüßÄÖÜ", "fr": "àâæçéèêëîïôœùûüÿÀÂÆÇÉÈÊËÎÏÔŒÙÛÜŸ", "es": "áéíñóúü¿¡ÁÉÍÑÓÚÜ", "pt": "ãõáâàçéêíóôúÃÕÁÂÀÇÉÊÍÓÔÚ",
	"it": "àèéìíòóùÀÈÉÌÒÙ", "nl": "éëïöüÉËÏÖÜ", "sv": "åäöÅÄÖ", "no": "æøåÆØÅ", "da": "æøåÆØÅ", "fi": "äöåÄÖÅ",
	"is": "áðéíóúýþæöÁÐÉÍÓÚÝÞÆÖ", "ca": "àçèéíïòóúüÀÇÈÉÍÏÒÓÚÜ", "et": "äöõüšžÄÖÕÜŠŽ",
	"el": "αβγδεζηθικλμνξοπρστυφχψωάέήίόύώΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩς", "tr": "çğıöşüÇĞİÖŞÜ",
	"he": "אבגדהוזחטיכלמנסעפצקרשתךםןףץ", "ar": "ابتثجحخدذرزسشصضطظعغفقكلمنهوي", "fa": "ابپتثجچحخدذرزژسشصضطظعغفقکگلمنوهی",
	"lt": "ąčęėįšųūžĄČĘĖĮŠŲŪŽ", "lv": "āčēģīķļņšūžĀČĒĢĪĶĻŅŠŪŽ",
}

// candidatesFor returns the code pages worth trying for a language, best
// guess first.
func candidatesFor(lang string) []candidate {
	switch lang {
	case "hr", "bs", "sl", "cs", "sk", "pl", "hu", "ro", "sq":
		return []candidate{cp1250, iso2, cp1252}
	case "sr":
		// Serbian is written in Latin (1250) and Cyrillic (1251).
		return []candidate{cp1250, cp1251, iso2, iso5}
	case "ru", "uk", "bg", "mk", "be":
		return []candidate{cp1251, koi8r, iso5}
	case "el":
		return []candidate{cp1253, iso7}
	case "tr":
		return []candidate{cp1254, iso9}
	case "he":
		return []candidate{cp1255}
	case "ar", "fa", "ur":
		return []candidate{cp1256}
	case "lt", "lv", "et":
		return []candidate{cp1257, iso13}
	case "", "unknown":
		return []candidate{cp1252, cp1250, cp1251, iso15, cp1253, cp1254}
	}
	return []candidate{cp1252, iso15, iso1, cp1250}
}

// score rates decoded text for a language: expected letters count up,
// control characters and unusual symbols count down.
func score(text, lang string) int {
	want := letters[lang]
	s := 0
	for _, r := range text {
		switch {
		case r < 0x80:
			if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
				s -= 20
			}
		case want != "" && strings.ContainsRune(want, r):
			s += 3
		case unicode.IsLetter(r):
			if want == "" {
				s++
			} else {
				s -= 2
			}
		case r == '€' || r == '…' || r == '–' || r == '—' || r == '„' || r == '“' || r == '”' || r == '‘' || r == '’' || r == '«' || r == '»' || r == '°' || r == '♪':
		case unicode.IsControl(r) || r == utf8.RuneError:
			s -= 20
		default:
			s -= 3
		}
	}
	return s
}

// utf16Guess finds UTF-16 without a BOM by its zero bytes (Latin text in
// UTF-16 has a zero in every second byte).
func utf16Guess(b []byte) (string, bool) {
	n := min(len(b), 4096) &^ 1
	if n < 8 {
		return "", false
	}
	evenZero, oddZero := 0, 0
	for i := 0; i < n; i += 2 {
		if b[i] == 0 {
			evenZero++
		}
		if b[i+1] == 0 {
			oddZero++
		}
	}
	half := n / 2
	switch {
	case oddZero > half*6/10 && evenZero < half/10:
		return UTF16LE, true
	case evenZero > half*6/10 && oddZero < half/10:
		return UTF16BE, true
	}
	return "", false
}

func decodeUTF16(b []byte, order binary.ByteOrder) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = order.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// Decode returns the text as UTF-8 and the encoding it was in. lang (ISO
// 639-1) is the language the text is expected to be in.
func Decode(b []byte, lang string) (string, string, error) {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		b = b[3:]
		if !utf8.Valid(b) {
			return strings.ToValidUTF8(string(b), "�"), UTF8, nil
		}
		return string(b), UTF8, nil
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return decodeUTF16(b[2:], binary.LittleEndian), UTF16LE, nil
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return decodeUTF16(b[2:], binary.BigEndian), UTF16BE, nil
	}
	if enc, ok := utf16Guess(b); ok {
		if enc == UTF16LE {
			return decodeUTF16(b, binary.LittleEndian), enc, nil
		}
		return decodeUTF16(b, binary.BigEndian), enc, nil
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return "", "", ErrBinary
	}
	if utf8.Valid(b) {
		return string(b), UTF8, nil
	}
	lang = strings.ToLower(lang)
	if i := strings.IndexAny(lang, "-_"); i > 0 {
		lang = lang[:i]
	}
	best, bestName, bestScore := "", "", 0
	for _, c := range candidatesFor(lang) {
		out, err := c.enc.NewDecoder().Bytes(b)
		if err != nil {
			continue
		}
		sc := score(string(out), lang)
		if bestName == "" || sc > bestScore {
			best, bestName, bestScore = string(out), c.name, sc
		}
	}
	if bestName == "" {
		return "", "", ErrBinary
	}
	return best, bestName, nil
}
