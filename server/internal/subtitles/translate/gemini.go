package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// Google Gemini API (POST {base}/v1beta/models/{model}:generateContent with
// the x-goog-api-key header).
const (
	GeminiURL          = "https://generativelanguage.googleapis.com"
	GeminiDefaultModel = "gemini-3.5-flash-lite"
)

// GeminiModels are suggested in the settings.
var GeminiModels = []string{"gemini-3.5-flash-lite", "gemini-3.8-flash"}

type gemini struct{ cfg Config }

// NewGemini returns the Gemini translator.
func NewGemini(cfg Config) Translator {
	cfg.BaseURL = trimURL(cfg.BaseURL, GeminiURL)
	if cfg.Model == "" {
		cfg.Model = GeminiDefaultModel
	}
	return &llm{name: Gemini, c: &gemini{cfg: cfg}, limits: Limits{MaxTexts: 40, MaxChars: 6000}}
}

func (g *gemini) complete(ctx context.Context, system, user string, maxTokens int) (completion, error) {
	if g.cfg.APIKey == "" {
		return completion{}, providers.ErrMissingKey
	}
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": system}}},
		"contents":          []map[string]any{{"role": "user", "parts": []map[string]string{{"text": user}}}},
		"generationConfig": map[string]any{
			"temperature":      0.2,
			"maxOutputTokens":  maxTokens + 4096,
			"responseMimeType": "application/json",
		},
	}
	hdr := http.Header{}
	hdr.Set("x-goog-api-key", g.cfg.APIKey)
	res, err := postJSON(ctx, g.cfg, g.cfg.BaseURL+"/v1beta/models/"+url.PathEscape(g.cfg.Model)+":generateContent", hdr, body)
	if err != nil {
		return completion{}, err
	}
	if res.Status == http.StatusBadRequest && len(res.Body) > 0 && json.Valid(res.Body) {
		// An invalid key is a 400 with API_KEY_INVALID at Google.
		var e struct {
			Error struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Details []struct {
					Reason string `json:"reason"`
				} `json:"details"`
			} `json:"error"`
		}
		if json.Unmarshal(res.Body, &e) == nil {
			for _, d := range e.Error.Details {
				if d.Reason == "API_KEY_INVALID" {
					return completion{}, fmt.Errorf("%w: %s", providers.ErrAuth, e.Error.Message)
				}
			}
		}
	}
	if err := classify(res); err != nil {
		return completion{}, err
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		UsageMetadata struct {
			PromptTokenCount     int64 `json:"promptTokenCount"`
			CandidatesTokenCount int64 `json:"candidatesTokenCount"`
			ThoughtsTokenCount   int64 `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return completion{}, fmt.Errorf("%w: %w", providers.ErrBadResponse, err)
	}
	c := completion{Usage: Usage{InputTokens: out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount + out.UsageMetadata.ThoughtsTokenCount, Requests: 1}}
	if len(out.Candidates) == 0 {
		c.Refused = true
		return c, nil
	}
	cand := out.Candidates[0]
	switch cand.FinishReason {
	case "MAX_TOKENS":
		c.Truncated = true
	case "SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "RECITATION":
		c.Refused = true
	}
	for _, p := range cand.Content.Parts {
		if !p.Thought {
			c.Text += p.Text
		}
	}
	return c, nil
}
