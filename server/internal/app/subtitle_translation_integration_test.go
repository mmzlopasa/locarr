package app

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
	trfake "github.com/mmzlopasa/locarr/server/internal/subtitles/translate/fake"
)

type apiTranslator struct {
	Name          string
	Enabled       bool
	MonthlyCap    int64
	LastErrorCode string
	QuotaUsed     *int64
	ApiKey        struct{ Configured bool }
	Month         struct{ Characters, InputTokens, OutputTokens, Lines, Subtitles int64 }
}

func (se *subEnv) translator(name string) apiTranslator {
	se.t.Helper()
	var list []apiTranslator
	se.expect(se.do("GET", "/api/v1/subtitles/translators", nil), 200).json(&list)
	for _, t := range list {
		if t.Name == name {
			return t
		}
	}
	se.t.Fatalf("no translator %s", name)
	return apiTranslator{}
}

func (se *subEnv) translatedSub(ref, lang string) (path, provider, from string, ok bool) {
	var it struct {
		Subtitles []struct {
			Path, Language, Source, Provider, TranslatedFrom string
		}
	}
	se.expect(se.do("GET", "/api/v1/subtitles/items/"+ref, nil), 200).json(&it)
	for _, s := range it.Subtitles {
		if s.Language == lang && s.Source == "translated" {
			return s.Path, s.Provider, s.TranslatedFrom, true
		}
	}
	return "", "", "", false
}

// The translation flow: a movie with only an English subtitle → "translate
// now" with DeepL writes Movie.hr.translated.srt with the English timing →
// the monthly cap stops a second run → a human Croatian subtitle on disk
// replaces the translation (recycle bin) → the scheduled job translates
// with Anthropic once the profile's wait is over.
func TestSubtitleTranslationFlow(t *testing.T) {
	le := newLibraryEnv(t)
	f := trfake.New()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	svc := le.deps.Subtitles
	svc.URLs = map[string]string{"translate:deepl": srv.URL + "/deepl", "translate:anthropic": srv.URL + "/anthropic"}
	svc.TranslateSleep = func(context.Context, time.Duration) error { return nil }
	se := &subEnv{libraryEnv: le}

	// Permissions: managers see the services and can translate, only administrators configure them.
	manager := le.user("vesna", "manager", nil)
	le.as(manager, func() {
		se.expect(se.do("GET", "/api/v1/subtitles/translators", nil), 200)
		se.expect(se.do("PUT", "/api/v1/subtitles/translators/deepl", map[string]any{"enabled": true}), 403)
		se.expect(se.do("POST", "/api/v1/subtitles/translators/deepl/test", nil), 403)
		se.expect(se.do("GET", "/api/v1/subtitles/translation", nil), 403)
	})

	// DeepL needs its key; the key is never returned; Test translates a sample.
	se.expect(se.do("PUT", "/api/v1/subtitles/translators/deepl", map[string]any{"enabled": true}), 400)
	se.expect(se.do("PUT", "/api/v1/subtitles/translators/deepl", map[string]any{"enabled": true, "apiKey": trfake.DeepLFreeKey}), 200)
	raw := se.expect(se.do("GET", "/api/v1/subtitles/translators", nil), 200)
	if strings.Contains(string(raw.body), trfake.DeepLFreeKey) {
		t.Fatal("the DeepL key leaked")
	}
	var test struct{ Output, To string }
	se.expect(se.do("POST", "/api/v1/subtitles/translators/deepl/test", map[string]any{"to": "hr"}), 200).json(&test)
	if test.Output != "[hr] Where are you going? <i>Wait for me!</i>" {
		t.Fatalf("test: %+v", test)
	}
	if tr := se.translator("deepl"); tr.QuotaUsed == nil || !tr.ApiKey.Configured || tr.Month.Characters == 0 {
		t.Fatalf("deepl state: %+v", tr)
	}

	pid := se.profile("Hrvatski", map[string]any{"language": "hr", "forced": false, "hearingImpaired": "include"})
	se.expect(se.do("PUT", "/api/v1/subtitles/settings", map[string]any{"defaultProfileMovies": pid}), 200)

	m := le.addMovie(10378, true)
	ref := fmt.Sprintf("movie:%d", m.ID)
	defer se.dump(ref)
	waitFor(t, "the grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.client.finish(t)
	le.poll()
	waitFor(t, "the import", func() bool { return le.movie(m.ID).File != nil })
	video := le.movie(m.ID).File.Path
	base := strings.TrimSuffix(video, ".mkv")
	waitFor(t, "the English sidecar", func() bool {
		_, err := os.Stat(base + ".en.srt")
		return err == nil
	})
	if it := se.item(ref); it.status("hr") != "missing" {
		t.Fatalf("hr should be missing: %+v", it.Wants)
	}

	// Translate now (as a manager).
	le.as(manager, func() {
		se.expect(se.do("POST", "/api/v1/subtitles/items/"+ref+"/translate", map[string]any{}), 202)
	})
	waitFor(t, "the translation", func() bool { return se.item(ref).status("hr") == "translated" })
	path, provider, from, _ := se.translatedSub(ref, "hr")
	if path != base+".hr.translated.srt" || provider != "deepl" || from != "en" {
		t.Fatalf("translation %s from %s with %s", path, from, provider)
	}
	src, _ := os.ReadFile(base + ".en.srt")
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sd, _ := subfmt.Parse(string(src))
	od, err := subfmt.Parse(string(out))
	if err != nil || len(sd.Cues) != len(od.Cues) {
		t.Fatalf("cues %d → %d (%v)", len(sd.Cues), len(od.Cues), err)
	}
	for i := range sd.Cues {
		if sd.Cues[i].Start != od.Cues[i].Start || sd.Cues[i].End != od.Cues[i].End {
			t.Fatalf("cue %d timing changed", i)
		}
		if !strings.HasPrefix(od.Cues[i].Text, "[hr] ") && !strings.Contains(od.Cues[i].Text, "[hr] ") {
			t.Fatalf("cue %d not translated: %q", i, od.Cues[i].Text)
		}
	}
	tl := le.timeline(m.ID)
	found := false
	for _, ev := range tl.Events {
		if ev.Type == "subtitle_translated" && strings.Contains(ev.Message, "translated hr from en with DeepL, ") && strings.Contains(ev.Message, " lines, ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("timeline: %s", tl.types())
	}
	if it := se.item(ref); it.Explanation.Code != "complete_translated" {
		t.Fatalf("explanation %+v", it.Explanation)
	}
	var prev struct {
		Lines []struct{ Text, Original string }
	}
	id := int64(0)
	for _, s := range se.item(ref).Subtitles {
		if s.Source == "translated" {
			id = s.ID
		}
	}
	se.expect(se.do("GET", fmt.Sprintf("/api/v1/subtitles/%d/preview?lines=3", id), nil), 200).json(&prev)
	if len(prev.Lines) == 0 || prev.Lines[0].Original == "" || !strings.Contains(prev.Lines[0].Text, prev.Lines[0].Original[:3]) {
		t.Fatalf("preview: %+v", prev.Lines)
	}
	if tr := se.translator("deepl"); tr.Month.Subtitles != 1 || tr.Month.Lines == 0 {
		t.Fatalf("usage: %+v", tr.Month)
	}

	// The monthly cap stops the next translation before anything is sent.
	se.expect(se.do("DELETE", fmt.Sprintf("/api/v1/subtitles/%d", id), nil), 204)
	se.expect(se.do("PUT", "/api/v1/subtitles/translators/deepl", map[string]any{"monthlyCap": 10}), 200)
	sent := f.Count("deepl")
	se.expect(se.do("POST", "/api/v1/subtitles/items/"+ref+"/translate", map[string]any{"keys": []string{"hr"}}), 202)
	waitFor(t, "the cap", func() bool { return strings.Contains(le.timeline(m.ID).types(), "subtitle_translation_failed") })
	if f.Count("deepl") != sent {
		t.Fatal("a request was sent over the cap")
	}
	if tr := se.translator("deepl"); tr.LastErrorCode != "cap" {
		t.Fatalf("cap not recorded: %+v", tr)
	}

	// The scheduled translation with Anthropic for Croatian, once the profile's wait is over.
	se.expect(se.do("PUT", "/api/v1/subtitles/translators/anthropic", map[string]any{"enabled": true, "apiKey": trfake.AnthropicKey}), 200)
	se.expect(se.do("PUT", "/api/v1/subtitles/translation", map[string]any{"languageTranslators": map[string]string{"hr": "anthropic"}}), 200)
	se.expect(se.do("PUT", fmt.Sprintf("/api/v1/subtitles/profiles/%d", pid), map[string]any{"name": "Hrvatski", "translateAfterHours": 2,
		"languages": []map[string]any{{"language": "hr", "forced": false, "hearingImpaired": "include"}}}), 200)
	waitFor(t, "the state refresh", func() bool {
		var at *int64
		_ = le.deps.DB.R.QueryRow(`SELECT translate_at FROM subtitle_state WHERE item_ref = ?`, ref).Scan(&at)
		return at != nil
	})
	_, _ = le.deps.DB.W.Exec(`UPDATE subtitle_state SET searches = 1 WHERE item_ref = ?`, ref)
	if n, err := svc.TranslateDue(context.Background()); err != nil || n != 0 {
		t.Fatalf("translated before the wait: %d %v", n, err)
	}
	svc.Now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	if n, err := svc.TranslateDue(context.Background()); err != nil || n != 1 {
		t.Fatalf("scheduled translation: %d %v", n, err)
	}
	svc.Now = nil
	if _, provider, _, ok := se.translatedSub(ref, "hr"); !ok || provider != "anthropic" {
		t.Fatalf("scheduled translation by %s", provider)
	}
	if f.Count("anthropic") == 0 {
		t.Fatal("anthropic was not asked")
	}

	// A human Croatian subtitle found on disk replaces the translation.
	human := base + ".hr.srt"
	if err := os.WriteFile(human, []byte("1\n00:00:01,000 --> 00:00:02,000\nLjudski prijevod.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	se.expect(se.do("POST", "/api/v1/subtitles/scan", nil), 202)
	waitFor(t, "the human subtitle", func() bool { return se.item(ref).status("hr") == "present" })
	if _, err := os.Stat(base + ".hr.translated.srt"); !os.IsNotExist(err) {
		t.Fatalf("the translation is still there: %v", err)
	}
	if _, _, _, ok := se.translatedSub(ref, "hr"); ok {
		t.Fatal("the translation is still recorded")
	}
	// Never over a human subtitle: translating hr now has nothing to do.
	if r := se.do("POST", "/api/v1/subtitles/items/"+ref+"/translate", map[string]any{"keys": []string{"hr"}}); r.StatusCode != 202 {
		t.Fatalf("translate with nothing missing: %d", r.StatusCode)
	}
	b, _ := os.ReadFile(human)
	if !strings.Contains(string(b), "Ljudski") {
		t.Fatal("the human subtitle was changed")
	}
}
