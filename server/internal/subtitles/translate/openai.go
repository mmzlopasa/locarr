package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// OpenAI addresses and models (platform.openai.com / developers.openai.com docs).
const (
	OpenAIURL          = "https://api.openai.com/v1"
	OpenAIDefaultModel = "gpt-6-luna"
)

// OpenAIModels are suggested in the settings (any model id can be typed).
var OpenAIModels = []string{"gpt-6-luna", "gpt-6.1-terra", "gpt-6.2-sol"}

// chat is a Chat Completions client (POST {base}/chat/completions with a
// Bearer key): OpenAI itself, or any server that speaks the same API.
type chat struct {
	cfg        Config
	compatible bool
}

// NewOpenAI returns the OpenAI translator.
func NewOpenAI(cfg Config) Translator {
	cfg.BaseURL = trimURL(cfg.BaseURL, OpenAIURL)
	if cfg.Model == "" {
		cfg.Model = OpenAIDefaultModel
	}
	return &llm{name: OpenAI, c: &chat{cfg: cfg}, limits: Limits{MaxTexts: 40, MaxChars: 6000}}
}

// NewOpenAICompatible returns a translator for an OpenAI-compatible server
// (Ollama, LM Studio, vLLM, …): base URL such as http://ollama:11434/v1,
// an optional key and a model name. Local models get smaller batches.
func NewOpenAICompatible(cfg Config) Translator {
	cfg.BaseURL = trimURL(cfg.BaseURL, "")
	return &llm{name: OpenAICompatible, c: &chat{cfg: cfg, compatible: true}, limits: Limits{MaxTexts: 20, MaxChars: 3000}}
}

func (c *chat) complete(ctx context.Context, system, user string, maxTokens int) (completion, error) {
	if c.cfg.BaseURL == "" {
		return completion{}, fmt.Errorf("%w: no server address", providers.ErrMissingKey)
	}
	if c.cfg.Model == "" {
		return completion{}, fmt.Errorf("%w: no model", providers.ErrMissingKey)
	}
	if !c.compatible && c.cfg.APIKey == "" {
		return completion{}, providers.ErrMissingKey
	}
	body := map[string]any{
		"model": c.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":     0.2,
		"response_format": map[string]string{"type": "json_object"},
	}
	optional := []string{"temperature", "response_format"}
	if c.compatible {
		body["max_tokens"] = maxTokens
	} else {
		body["max_completion_tokens"] = maxTokens
		// Reasoning is not needed to translate a subtitle line.
		body["reasoning_effort"] = "none"
		optional = append(optional, "reasoning_effort")
	}
	hdr := http.Header{}
	if c.cfg.APIKey != "" {
		hdr.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	res, err := postJSON(ctx, c.cfg, c.cfg.BaseURL+"/chat/completions", hdr, body, optional...)
	if err != nil {
		return completion{}, err
	}
	if err := classify(res); err != nil {
		return completion{}, err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(res.Body, &out); err != nil || len(out.Choices) == 0 {
		return completion{}, fmt.Errorf("%w: no completion", providers.ErrBadResponse)
	}
	ch := out.Choices[0]
	return completion{Text: ch.Message.Content, Truncated: ch.FinishReason == "length", Refused: ch.Message.Refusal != "" || ch.FinishReason == "content_filter",
		Usage: Usage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens, Requests: 1}}, nil
}
