package charset

import (
	"os"
	"strings"
	"testing"
)

func TestDecodeFixtures(t *testing.T) {
	cases := []struct {
		file, lang, enc, want string
	}{
		{"hr-cp1250.srt", "hr", "windows-1250", "Što će Đuro reći? Čaša je puna, žute ruže."},
		{"hr-iso8859-2.srt", "hr", "iso-8859-2", "Ćevapčići i šljivovica za sve!"},
		{"hr-utf8-bom.srt", "hr", UTF8, "Ćevapčići"},
		{"hr-utf16le.srt", "hr", UTF16LE, "Đuro"},
		{"hr-utf16be-nobom.srt", "hr", UTF16BE, "žute ruže"},
		{"ru-cp1251.srt", "ru", "windows-1251", "Привет, как дела? Всё хорошо."},
		{"sr-cyrl-cp1251.srt", "sr", "windows-1251", "Љубав и њежност"},
		{"de-cp1252.srt", "de", "windows-1252", "Grüße aus München, schöne Straße."},
		// Bosnian uses the same code page as Croatian.
		{"hr-cp1250.srt", "bs", "windows-1250", "Čaša"},
	}
	for _, c := range cases {
		b, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		got, enc, err := Decode(b, c.lang)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if enc != c.enc || !strings.Contains(got, c.want) {
			t.Errorf("%s (%s): got %s %q, want %s containing %q", c.file, c.lang, enc, got, c.enc, c.want)
		}
		if strings.HasPrefix(got, "\ufeff") {
			t.Errorf("%s: BOM kept", c.file)
		}
	}
}

func TestDecodeBinary(t *testing.T) {
	if _, _, err := Decode([]byte{0x00, 0x01, 0x02, 0xff, 0x00, 0x9a, 0x13}, "hr"); err == nil {
		t.Fatal("binary data decoded")
	}
}
