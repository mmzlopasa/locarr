//go:build live

// Live checks against the real services, for the owner with real keys:
//
//	LOCARR_LIVE_DEEPL_KEY=… LOCARR_LIVE_OPENAI_KEY=… LOCARR_LIVE_ANTHROPIC_KEY=… LOCARR_LIVE_GEMINI_KEY=… \
//	LOCARR_LIVE_LIBRETRANSLATE_URL=http://… LOCARR_LIVE_COMPATIBLE_URL=http://ollama:11434/v1 LOCARR_LIVE_COMPATIBLE_MODEL=llama3.1 \
//	go test -tags live ./internal/subtitles/translate/ -run Live -v
//
// Each translates three lines into Croatian (a few hundred characters or tokens).
package translate_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/translate"
)

func TestLiveTranslators(t *testing.T) {
	hc, err := httpx.New(httpx.Options{Timeout: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func() (translate.Translator, bool){
		"deepl": func() (translate.Translator, bool) {
			k := os.Getenv("LOCARR_LIVE_DEEPL_KEY")
			return translate.NewDeepL(translate.Config{APIKey: k, HTTP: hc}), k != ""
		},
		"openai": func() (translate.Translator, bool) {
			k := os.Getenv("LOCARR_LIVE_OPENAI_KEY")
			return translate.NewOpenAI(translate.Config{APIKey: k, HTTP: hc}), k != ""
		},
		"anthropic": func() (translate.Translator, bool) {
			k := os.Getenv("LOCARR_LIVE_ANTHROPIC_KEY")
			return translate.NewAnthropic(translate.Config{APIKey: k, HTTP: hc}), k != ""
		},
		"gemini": func() (translate.Translator, bool) {
			k := os.Getenv("LOCARR_LIVE_GEMINI_KEY")
			return translate.NewGemini(translate.Config{APIKey: k, HTTP: hc}), k != ""
		},
		"libretranslate": func() (translate.Translator, bool) {
			u := os.Getenv("LOCARR_LIVE_LIBRETRANSLATE_URL")
			return translate.NewLibreTranslate(translate.Config{BaseURL: u, APIKey: os.Getenv("LOCARR_LIVE_LIBRETRANSLATE_KEY"), HTTP: hc}), u != ""
		},
		"openai-compatible": func() (translate.Translator, bool) {
			u := os.Getenv("LOCARR_LIVE_COMPATIBLE_URL")
			return translate.NewOpenAICompatible(translate.Config{BaseURL: u, Model: os.Getenv("LOCARR_LIVE_COMPATIBLE_MODEL"), HTTP: hc}), u != ""
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			tr, ok := mk()
			if !ok {
				t.Skip("no key or address")
			}
			out, u, err := tr.Translate(context.Background(), translate.Request{Texts: []string{"Where are you going?", "<i>Wait for me!</i>",
				"- I told you already, I can't stay."}, From: "en", To: "hr", Title: "Sintel (2010)"})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%q, usage %+v", out, u)
			if len(out) != 3 || !translate.SameTags("<i>Wait for me!</i>", out[1]) {
				t.Fatalf("bad answer %q", out)
			}
		})
	}
}
