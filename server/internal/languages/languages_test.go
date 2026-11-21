package languages

import "testing"

func TestCatalogue(t *testing.T) {
	seen := map[int]bool{}
	for _, l := range List() {
		if l.Code == "" || l.Name == "" || l.Native == "" {
			t.Errorf("incomplete %+v", l)
		}
		if seen[l.LegacyID] {
			t.Errorf("legacy id %d twice", l.LegacyID)
		}
		seen[l.LegacyID] = true
		if c, ok := FromLegacyID(l.LegacyID); !ok || c != l.Code {
			t.Errorf("legacy id %d -> %s", l.LegacyID, c)
		}
	}
	for _, c := range []string{Any, Original, Unknown, "hr", "sr", "bs", "pt-br"} {
		if !Valid(c) {
			t.Errorf("%s not valid", c)
		}
	}
	if Valid("xx") {
		t.Error("xx valid")
	}
}

func TestFromWord(t *testing.T) {
	cases := []struct {
		word     string
		capitals bool
		want     string
	}{
		{"GERMAN", true, "de"}, {"german", false, "de"}, {"Hrvatski", false, "hr"}, {"HRV", true, "hr"}, {"SRB", true, "sr"},
		{"HR", true, "hr"}, {"hr", false, ""}, {"ITA", true, "it"}, {"cat", false, ""}, {"CAT", true, "ca"}, {"truefrench", false, "fr"},
	}
	for _, c := range cases {
		got, _ := FromWord(c.word, c.capitals)
		if got != c.want {
			t.Errorf("%s: %q want %q", c.word, got, c.want)
		}
	}
}
