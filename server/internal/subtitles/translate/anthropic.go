package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// Anthropic Messages API (POST /v1/messages with x-api-key and
// anthropic-version headers).
const (
	AnthropicURL          = "https://api.anthropic.com"
	AnthropicVersion      = "2026-10-21"
	AnthropicDefaultModel = "claude-haiku-5-5"
)

// AnthropicModels are offered in the settings.
var AnthropicModels = []string{"claude-haiku-5-5", "claude-sonnet-5-5"}

type messages struct{ cfg Config }

// NewAnthropic returns the Anthropic translator.
func NewAnthropic(cfg Config) Translator {
	cfg.BaseURL = trimURL(cfg.BaseURL, AnthropicURL)
	if cfg.Model == "" {
		cfg.Model = AnthropicDefaultModel
	}
	return &llm{name: Anthropic, c: &messages{cfg: cfg}, limits: Limits{MaxTexts: 40, MaxChars: 6000}}
}

// linesSchema constrains the answer to {"lines": [string, ...]}.
var linesSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"lines": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
	"required":             []string{"lines"},
	"additionalProperties": false,
}

func (m *messages) complete(ctx context.Context, system, user string, maxTokens int) (completion, error) {
	if m.cfg.APIKey == "" {
		return completion{}, providers.ErrMissingKey
	}
	// The current models take no sampling parameters; effort keeps the
	// (always adaptive) thinking short, and structured output fixes the shape.
	body := map[string]any{
		"model":      m.cfg.Model,
		"max_tokens": maxTokens + 4096,
		"system":     system,
		"messages":   []map[string]any{{"role": "user", "content": user}},
		"output_config": map[string]any{
			"effort": "low",
			"format": map[string]any{"type": "json_schema", "schema": linesSchema},
		},
	}
	hdr := http.Header{}
	hdr.Set("x-api-key", m.cfg.APIKey)
	hdr.Set("anthropic-version", AnthropicVersion)
	res, err := postJSON(ctx, m.cfg, m.cfg.BaseURL+"/v1/messages", hdr, body, "output_config")
	if err != nil {
		return completion{}, err
	}
	if err := classify(res); err != nil {
		return completion{}, err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil {
		return completion{}, fmt.Errorf("%w: %w", providers.ErrBadResponse, err)
	}
	c := completion{Usage: Usage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens, Requests: 1},
		Truncated: out.StopReason == "max_tokens", Refused: out.StopReason == "refusal"}
	for _, b := range out.Content {
		if b.Type == "text" {
			c.Text += b.Text
		}
	}
	return c, nil
}
