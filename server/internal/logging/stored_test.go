package logging

import (
	"strings"
	"testing"
)

func TestScrubStored(t *testing.T) {
	long := strings.Repeat("Ab9", 40)
	in := "could not grab Iron.Man.2008.720p: request https://pro.example.net/8/download?apikey=redacted&file=Iron.Man.2008&link=" + long +
		": redirect to another host refused"
	got := ScrubStored(in)
	if strings.Contains(got, long) {
		t.Fatalf("link token kept: %s", got)
	}
	for _, want := range []string{"https://pro.example.net/8/download?", "apikey=[redacted]", "file=Iron.Man.2008", "link=[redacted]",
		": redirect to another host refused", "could not grab Iron.Man.2008.720p"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	for in, want := range map[string]string{
		"Get \"https://u:pw@x.test/api?t=search&passkey=abc&q=x\": timeout": "Get \"https://redacted@x.test/api?t=search&passkey=[redacted]&q=x\": timeout",
		"see https://x.test/a/b.":          "see https://x.test/a/b.",
		"no url here":                      "no url here",
		"http://x.test/?token=s3cret#frag": "http://x.test/?token=[redacted]",
	} {
		if got := ScrubStored(in); got != want {
			t.Errorf("ScrubStored(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScrubStoredJSON(t *testing.T) {
	raw := []byte(`{"error":"request https://x.test/dl?link=` + strings.Repeat("z", 60) + `&id=7: refused","downloadId":9007199254740993,"n":[1,"http://y.test/?apikey=k"]}`)
	got := string(ScrubStoredJSON(raw))
	if strings.Contains(got, strings.Repeat("z", 60)) || strings.Contains(got, "apikey=k\"") || !strings.Contains(got, "9007199254740993") {
		t.Fatalf("got %s", got)
	}
	if !strings.Contains(got, `link=[redacted]\u0026id=7`) {
		t.Fatalf("query kept in shape: %s", got)
	}
	if string(ScrubStoredJSON([]byte(`{"a":1}`))) != `{"a":1}` {
		t.Fatal("plain data changed")
	}
}
