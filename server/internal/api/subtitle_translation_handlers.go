package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/subtitles"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/translate"
)

func mapTranslationError(err error) *StatusError {
	var ce *subtitles.CapError
	switch {
	case errors.As(err, &ce):
		return &StatusError{Status: http.StatusTooManyRequests, Code: "translation_cap", Message: err.Error()}
	case errors.Is(err, subtitles.ErrNoTranslator):
		return &StatusError{Status: http.StatusConflict, Code: "translation_unavailable", Message: err.Error()}
	case errors.Is(err, subtitles.ErrNoSource):
		return &StatusError{Status: http.StatusConflict, Code: "translation_no_source", Message: err.Error()}
	case errors.Is(err, subtitles.ErrNothingToTrans):
		return &StatusError{Status: http.StatusConflict, Code: "translation_nothing_missing", Message: err.Error()}
	case errors.Is(err, subtitles.ErrHumanSubtitle):
		return &StatusError{Status: http.StatusConflict, Code: "translation_file_exists", Message: err.Error()}
	case errors.Is(err, translate.ErrUnsupported):
		return &StatusError{Status: http.StatusBadGateway, Code: "translation_unsupported_language", Message: err.Error()}
	case errors.Is(err, translate.ErrCountMismatch):
		return &StatusError{Status: http.StatusBadGateway, Code: "translation_line_mismatch", Message: err.Error()}
	}
	return nil
}

func optInt64(p *int64) gen.OptInt64 {
	if p == nil {
		return gen.OptInt64{}
	}
	return gen.NewOptInt64(*p)
}

func toSubtitleTranslator(t subtitles.TranslatorState) gen.SubtitleTranslator {
	info, _ := subtitles.TranslatorInfoOf(t.Name)
	out := gen.SubtitleTranslator{Name: t.Name, Enabled: t.Enabled, NeedsKey: info.NeedsKey, NeedsUrl: info.NeedsURL, Local: info.Local,
		Unit: gen.SubtitleTranslatorUnit(info.Unit), DefaultModel: info.DefaultModel, Models: append([]string{}, info.Models...), Website: info.Website,
		BaseUrl: t.BaseURL, ApiKey: gen.SecretState{Configured: t.APIKeySet}, Model: t.Model, Formality: t.Formality, GlossaryId: t.GlossaryID,
		MonthlyCap: t.MonthlyCap, Failures: t.Failures, RetryAt: optTime(t.RetryAt), LastError: t.LastError, LastErrorCode: t.LastErrorCode,
		LastErrorAt: optTime(t.LastErrorAt), LastSuccessAt: optTime(t.LastSuccessAt), QuotaUsed: optInt64(t.QuotaUsed), QuotaLimit: optInt64(t.QuotaLimit),
		QuotaCheckedAt: optTime(t.QuotaCheckedAt), Fields: []gen.SubtitleTranslatorFieldsItem{},
		Month: gen.SubtitleTranslationUsage{Month: t.Month.Month, Characters: t.Month.Characters, InputTokens: t.Month.InputTokens,
			OutputTokens: t.Month.OutputTokens, Requests: t.Month.Requests, Lines: t.Month.Lines, Subtitles: t.Month.Subtitles}}
	for _, f := range info.Fields {
		out.Fields = append(out.Fields, gen.SubtitleTranslatorFieldsItem(f))
	}
	return out
}

// ListSubtitleTranslators implements GET /subtitles/translators.
func (h *Handler) ListSubtitleTranslators(ctx context.Context) ([]gen.SubtitleTranslator, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Subtitles.TranslatorStates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.SubtitleTranslator, 0, len(list))
	for _, t := range list {
		out = append(out, toSubtitleTranslator(t))
	}
	return out, nil
}

// UpdateSubtitleTranslator implements PUT /subtitles/translators/{name}.
func (h *Handler) UpdateSubtitleTranslator(ctx context.Context, req *gen.SubtitleTranslatorInput, p gen.UpdateSubtitleTranslatorParams) (*gen.SubtitleTranslator, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	in := subtitles.TranslatorInput{BaseURL: optStrPtr(req.BaseUrl), APIKey: optStrPtr(req.ApiKey), Model: optStrPtr(req.Model),
		GlossaryID: optStrPtr(req.GlossaryId)}
	if v, ok := req.Enabled.Get(); ok {
		in.Enabled = &v
	}
	if v, ok := req.Formality.Get(); ok {
		f := string(v)
		in.Formality = &f
	}
	if v, ok := req.MonthlyCap.Get(); ok {
		in.MonthlyCap = &v
	}
	t, err := h.Subtitles.UpdateTranslator(ctx, string(p.Name), in)
	if err != nil {
		return nil, err
	}
	out := toSubtitleTranslator(t)
	return &out, nil
}

// TestSubtitleTranslator implements POST /subtitles/translators/{name}/test.
func (h *Handler) TestSubtitleTranslator(ctx context.Context, req gen.OptTestSubtitleTranslatorReq, p gen.TestSubtitleTranslatorParams) (*gen.SubtitleTranslatorTest, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	r := req.Or(gen.TestSubtitleTranslatorReq{})
	res, t, err := h.Subtitles.TestTranslator(ctx, string(p.Name), r.Text.Or(""), r.From.Or(""), r.To.Or(""))
	if err != nil {
		return nil, err
	}
	return &gen.SubtitleTranslatorTest{Input: res.Input, Output: res.Output, From: res.From, To: res.To, DurationMs: res.DurationMs,
		Characters: res.Usage.Characters, InputTokens: res.Usage.InputTokens, OutputTokens: res.Usage.OutputTokens, Translator: toSubtitleTranslator(t)}, nil
}

// ResetSubtitleTranslatorHealth implements POST /subtitles/translators/{name}/health/reset.
func (h *Handler) ResetSubtitleTranslatorHealth(ctx context.Context, p gen.ResetSubtitleTranslatorHealthParams) (*gen.SubtitleTranslator, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	t, err := h.Subtitles.ResetTranslatorHealth(ctx, string(p.Name))
	if err != nil {
		return nil, err
	}
	out := toSubtitleTranslator(t)
	return &out, nil
}

func toTranslationSettings(s subtitles.TranslationSettings) *gen.SubtitleTranslationSettings {
	return &gen.SubtitleTranslationSettings{DefaultTranslator: s.DefaultTranslator, LanguageTranslators: gen.SubtitleTranslationSettingsLanguageTranslators(s.LanguageTranslators),
		Suffix: s.Suffix, SourceLanguages: s.SourceLanguages, AutoSync: s.AutoSync, PerHour: s.PerHour}
}

// GetSubtitleTranslationSettings implements GET /subtitles/translation.
func (h *Handler) GetSubtitleTranslationSettings(ctx context.Context) (*gen.SubtitleTranslationSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	s, err := h.Subtitles.GetTranslationSettings(ctx)
	if err != nil {
		return nil, err
	}
	return toTranslationSettings(s), nil
}

// UpdateSubtitleTranslationSettings implements PUT /subtitles/translation.
func (h *Handler) UpdateSubtitleTranslationSettings(ctx context.Context, req *gen.SubtitleTranslationSettingsInput) (*gen.SubtitleTranslationSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	in := subtitles.TranslationSettingsInput{DefaultTranslator: optStrPtr(req.DefaultTranslator), Suffix: optStrPtr(req.Suffix), SourceLanguages: req.SourceLanguages}
	if m, ok := req.LanguageTranslators.Get(); ok {
		in.LanguageTranslators = map[string]string(m)
		if in.LanguageTranslators == nil {
			in.LanguageTranslators = map[string]string{}
		}
	}
	if v, ok := req.AutoSync.Get(); ok {
		in.AutoSync = &v
	}
	if v, ok := req.PerHour.Get(); ok {
		in.PerHour = &v
	}
	s, err := h.Subtitles.UpdateTranslationSettings(ctx, in)
	if err != nil {
		return nil, err
	}
	return toTranslationSettings(s), nil
}

// TranslateItemSubtitles implements POST /subtitles/items/{ref}/translate.
func (h *Handler) TranslateItemSubtitles(ctx context.Context, req gen.OptTranslateItemSubtitlesReq, p gen.TranslateItemSubtitlesParams) (*gen.SubtitleCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := subRef(p.Ref)
	if err != nil {
		return nil, err
	}
	r := req.Or(gen.TranslateItemSubtitlesReq{})
	if _, err := h.Subtitles.Target(ctx, ref); err != nil {
		return nil, err
	}
	n, err := h.Subtitles.QueueTranslate(ctx, []library.Ref{ref}, r.Keys, r.Translator.Or(""), "manual")
	if err != nil {
		return nil, err
	}
	return &gen.SubtitleCount{Count: n}, nil
}

// TranslateWantedSubtitles implements POST /subtitles/wanted/translate.
func (h *Handler) TranslateWantedSubtitles(ctx context.Context, req *gen.TranslateWantedSubtitlesReq) (*gen.SubtitleCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	var refs []library.Ref
	if req.All.Or(false) {
		var err error
		if refs, err = h.Subtitles.WantedRefs(ctx, string(req.Kind.Or(""))); err != nil {
			return nil, err
		}
	}
	for _, r := range req.Refs {
		ref, err := subRef(r)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	n, err := h.Subtitles.QueueTranslate(ctx, refs, nil, req.Translator.Or(""), "manual")
	if err != nil {
		return nil, err
	}
	return &gen.SubtitleCount{Count: n}, nil
}
