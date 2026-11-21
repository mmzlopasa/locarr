package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// The internal types use the API's JSON field names, so conversion between
// them and the generated types goes through JSON.
func convert[T any](in any) (T, error) {
	var out T
	// Generated types marshal through pointer methods: pass an addressable copy.
	if rv := reflect.ValueOf(in); rv.IsValid() && rv.Kind() != reflect.Pointer {
		p := reflect.New(rv.Type())
		p.Elem().Set(rv)
		in = p.Interface()
	}
	b, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

// mapProfileError maps the errors of the profile packages.
func mapProfileError(err error) *StatusError {
	var qi *quality.InvalidError
	var ci *customformats.InvalidError
	switch {
	case errors.As(err, &qi):
		return &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: qi.Reason}
	case errors.As(err, &ci):
		return &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: ci.Reason}
	case errors.Is(err, quality.ErrNotFound), errors.Is(err, customformats.ErrNotFound):
		return &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, quality.ErrNameTaken), errors.Is(err, customformats.ErrNameTaken):
		return &StatusError{Status: http.StatusConflict, Code: "name_taken", Message: err.Error()}
	case errors.Is(err, quality.ErrDefault):
		return &StatusError{Status: http.StatusConflict, Code: "default_profile", Message: err.Error()}
	}
	return nil
}

func requireUser(ctx context.Context) error {
	_, err := principal(ctx)
	return err
}

// --- qualities and definitions ---

// ListQualities implements GET /qualities.
func (h *Handler) ListQualities(ctx context.Context) ([]gen.Quality, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	return convert[[]gen.Quality](quality.All())
}

// ListQualityDefinitions implements GET /qualitydefinitions.
func (h *Handler) ListQualityDefinitions(ctx context.Context) ([]gen.QualityDefinition, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	defs, err := h.Quality.Definitions(ctx)
	if err != nil {
		return nil, err
	}
	return convert[[]gen.QualityDefinition](defs)
}

// UpdateQualityDefinitions implements PUT /qualitydefinitions.
func (h *Handler) UpdateQualityDefinitions(ctx context.Context, req *gen.UpdateQualityDefinitionsRequest) ([]gen.QualityDefinition, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	defs, err := convert[[]quality.Definition](req.Definitions)
	if err != nil {
		return nil, err
	}
	if err := h.Quality.UpdateDefinitions(ctx, defs); err != nil {
		return nil, err
	}
	return h.ListQualityDefinitions(ctx)
}

// ResetQualityDefinitions implements POST /qualitydefinitions/reset.
func (h *Handler) ResetQualityDefinitions(ctx context.Context, req *gen.ResetQualityDefinitionsRequest) ([]gen.QualityDefinition, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if err := h.Quality.ResetDefinitions(ctx, quality.Kind(req.Kind.Or(""))); err != nil {
		return nil, err
	}
	return h.ListQualityDefinitions(ctx)
}

// --- quality profiles ---

// ListQualityProfiles implements GET /qualityprofiles.
func (h *Handler) ListQualityProfiles(ctx context.Context) ([]gen.QualityProfile, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.Quality.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	return convert[[]gen.QualityProfile](list)
}

// GetQualityProfile implements GET /qualityprofiles/{id}.
func (h *Handler) GetQualityProfile(ctx context.Context, params gen.GetQualityProfileParams) (*gen.QualityProfile, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	p, err := h.Quality.Profile(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return convert[*gen.QualityProfile](p)
}

// CreateQualityProfile implements POST /qualityprofiles.
func (h *Handler) CreateQualityProfile(ctx context.Context, req *gen.QualityProfileInput) (*gen.QualityProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	p, err := convert[quality.Profile](req)
	if err != nil {
		return nil, err
	}
	if p, err = h.Quality.CreateProfile(ctx, p); err != nil {
		return nil, err
	}
	return convert[*gen.QualityProfile](p)
}

// UpdateQualityProfile implements PUT /qualityprofiles/{id}.
func (h *Handler) UpdateQualityProfile(ctx context.Context, req *gen.QualityProfileInput, params gen.UpdateQualityProfileParams) (*gen.QualityProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	p, err := convert[quality.Profile](req)
	if err != nil {
		return nil, err
	}
	p.ID = params.ID
	if p, err = h.Quality.UpdateProfile(ctx, p); err != nil {
		return nil, err
	}
	return convert[*gen.QualityProfile](p)
}

// DeleteQualityProfile implements DELETE /qualityprofiles/{id}.
func (h *Handler) DeleteQualityProfile(ctx context.Context, params gen.DeleteQualityProfileParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Quality.DeleteProfile(ctx, params.ID)
}

// --- custom formats ---

func toCustomFormat(f customformats.Format) (gen.CustomFormat, error) {
	unsupported := []string{}
	if c, err := customformats.Compile(f); err == nil {
		unsupported = append(unsupported, c.Unsupported...)
	}
	return convert[gen.CustomFormat](struct {
		customformats.Format
		Unsupported []string `json:"unsupported"`
	}{f, unsupported})
}

// ListCustomFormats implements GET /customformats.
func (h *Handler) ListCustomFormats(ctx context.Context) ([]gen.CustomFormat, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.Formats.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.CustomFormat, 0, len(list))
	for _, f := range list {
		g, err := toCustomFormat(f)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// GetCustomFormat implements GET /customformats/{id}.
func (h *Handler) GetCustomFormat(ctx context.Context, params gen.GetCustomFormatParams) (*gen.CustomFormat, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	f, err := h.Formats.Get(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	g, err := toCustomFormat(f)
	return &g, err
}

func (h *Handler) saveFormat(ctx context.Context, id int64, req *gen.CustomFormatInput) (*gen.CustomFormat, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	f, err := convert[customformats.Format](req)
	if err != nil {
		return nil, err
	}
	f.ID = id
	if f, err = h.Formats.Save(ctx, f); err != nil {
		return nil, err
	}
	g, err := toCustomFormat(f)
	return &g, err
}

// CreateCustomFormat implements POST /customformats.
func (h *Handler) CreateCustomFormat(ctx context.Context, req *gen.CustomFormatInput) (*gen.CustomFormat, error) {
	return h.saveFormat(ctx, 0, req)
}

// UpdateCustomFormat implements PUT /customformats/{id}.
func (h *Handler) UpdateCustomFormat(ctx context.Context, req *gen.CustomFormatInput, params gen.UpdateCustomFormatParams) (*gen.CustomFormat, error) {
	return h.saveFormat(ctx, params.ID, req)
}

// DeleteCustomFormat implements DELETE /customformats/{id}.
func (h *Handler) DeleteCustomFormat(ctx context.Context, params gen.DeleteCustomFormatParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Formats.Delete(ctx, params.ID)
}

// GetCustomFormatSchema implements GET /customformats/schema.
func (h *Handler) GetCustomFormatSchema(ctx context.Context) (*gen.CustomFormatSchema, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	enum := func(impl string, values []string) gen.CustomFormatImplementation {
		return gen.CustomFormatImplementation{Implementation: impl, ValueType: gen.CustomFormatImplementationValueTypeEnum, Values: values}
	}
	res := make([]string, 0, len(customformats.Resolutions))
	for _, r := range customformats.Resolutions {
		res = append(res, fmt.Sprint(r))
	}
	out := &gen.CustomFormatSchema{}
	for _, impl := range customformats.Implementations {
		var it gen.CustomFormatImplementation
		switch impl {
		case customformats.ReleaseTitle, customformats.ReleaseGroup, customformats.Edition:
			it = gen.CustomFormatImplementation{Implementation: impl, ValueType: gen.CustomFormatImplementationValueTypeRegex}
		case customformats.Source:
			it = enum(impl, customformats.Sources)
		case customformats.QualityModifier:
			it = enum(impl, customformats.Modifiers)
		case customformats.Resolution:
			it = gen.CustomFormatImplementation{Implementation: impl, ValueType: gen.CustomFormatImplementationValueTypeNumber, Values: res}
		case customformats.VideoCodec:
			it = enum(impl, customformats.VideoCodecs)
		case customformats.HDR:
			it = enum(impl, customformats.HDRFormats)
		case customformats.AudioCodec:
			it = enum(impl, customformats.AudioCodecs)
		case customformats.ReleaseType:
			it = enum(impl, customformats.ReleaseTypes)
		case customformats.IndexerFlag:
			it = enum(impl, customformats.IndexerFlags)
		case customformats.Language:
			it = gen.CustomFormatImplementation{Implementation: impl, ValueType: gen.CustomFormatImplementationValueTypeLanguage}
		case customformats.Size, customformats.Year:
			it = gen.CustomFormatImplementation{Implementation: impl, ValueType: gen.CustomFormatImplementationValueTypeRange}
		case customformats.StreamingService:
			it = gen.CustomFormatImplementation{Implementation: impl, ValueType: gen.CustomFormatImplementationValueTypeEnum, Values: streamingNames()}
		}
		if it.Values == nil {
			it.Values = []string{}
		}
		out.Implementations = append(out.Implementations, it)
	}
	return out, nil
}

func streamingNames() []string {
	return []string{"Amazon", "Apple TV+", "BBC iPlayer", "Crave", "Crunchyroll", "Disney+", "Funimation", "HBO", "HBO Max", "HIDIVE",
		"Hulu", "iTunes", "Max", "Netflix", "Paramount+", "Peacock", "SkyShowtime", "Stan", "Starz", "Viaplay"}
}

// ImportCustomFormats implements POST /customformats/import.
func (h *Handler) ImportCustomFormats(ctx context.Context, req *gen.ImportCustomFormatsRequest) (*gen.ImportCustomFormatsResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	res, err := customformats.Import([]byte(req.JSON), customformats.Flavor(req.Flavor.Or(gen.CustomFormatFlavorMovies)))
	if err != nil {
		return nil, err
	}
	formats := res.Formats
	stored := false
	if !req.DryRun.Or(false) {
		if formats, err = h.Formats.SaveAll(ctx, formats, req.Replace.Or(false)); err != nil {
			return nil, err
		}
		stored = true
	}
	out := &gen.ImportCustomFormatsResult{Warnings: append([]string{}, res.Warnings...), Stored: stored, Formats: []gen.CustomFormat{}}
	for _, f := range formats {
		g, err := toCustomFormat(f)
		if err != nil {
			return nil, err
		}
		out.Formats = append(out.Formats, g)
	}
	return out, nil
}

// ExportCustomFormat implements GET /customformats/{id}/export.
func (h *Handler) ExportCustomFormat(ctx context.Context, params gen.ExportCustomFormatParams) (gen.SharedCustomFormat, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	f, err := h.Formats.Get(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return convert[gen.SharedCustomFormat](customformats.Export(f, customformats.Flavor(params.Flavor.Or(gen.CustomFormatFlavorMovies))))
}

// --- release and delay profiles ---

// ListReleaseProfiles implements GET /releaseprofiles.
func (h *Handler) ListReleaseProfiles(ctx context.Context) ([]gen.ReleaseProfile, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.Quality.ReleaseProfiles(ctx)
	if err != nil {
		return nil, err
	}
	return convert[[]gen.ReleaseProfile](list)
}

func (h *Handler) saveReleaseProfile(ctx context.Context, id int64, req *gen.ReleaseProfileInput) (*gen.ReleaseProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	r, err := convert[quality.ReleaseProfile](req)
	if err != nil {
		return nil, err
	}
	r.ID = id
	if r, err = h.Quality.SaveReleaseProfile(ctx, r); err != nil {
		return nil, err
	}
	return convert[*gen.ReleaseProfile](r)
}

// CreateReleaseProfile implements POST /releaseprofiles.
func (h *Handler) CreateReleaseProfile(ctx context.Context, req *gen.ReleaseProfileInput) (*gen.ReleaseProfile, error) {
	return h.saveReleaseProfile(ctx, 0, req)
}

// UpdateReleaseProfile implements PUT /releaseprofiles/{id}.
func (h *Handler) UpdateReleaseProfile(ctx context.Context, req *gen.ReleaseProfileInput, params gen.UpdateReleaseProfileParams) (*gen.ReleaseProfile, error) {
	return h.saveReleaseProfile(ctx, params.ID, req)
}

// DeleteReleaseProfile implements DELETE /releaseprofiles/{id}.
func (h *Handler) DeleteReleaseProfile(ctx context.Context, params gen.DeleteReleaseProfileParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Quality.DeleteReleaseProfile(ctx, params.ID)
}

// ListDelayProfiles implements GET /delayprofiles.
func (h *Handler) ListDelayProfiles(ctx context.Context) ([]gen.DelayProfile, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.Quality.DelayProfiles(ctx)
	if err != nil {
		return nil, err
	}
	return convert[[]gen.DelayProfile](list)
}

func (h *Handler) saveDelayProfile(ctx context.Context, id int64, req *gen.DelayProfileInput) (*gen.DelayProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	d, err := convert[quality.DelayProfile](req)
	if err != nil {
		return nil, err
	}
	d.ID = id
	if d, err = h.Quality.SaveDelayProfile(ctx, d); err != nil {
		return nil, err
	}
	return convert[*gen.DelayProfile](d)
}

// CreateDelayProfile implements POST /delayprofiles.
func (h *Handler) CreateDelayProfile(ctx context.Context, req *gen.DelayProfileInput) (*gen.DelayProfile, error) {
	return h.saveDelayProfile(ctx, 0, req)
}

// UpdateDelayProfile implements PUT /delayprofiles/{id}.
func (h *Handler) UpdateDelayProfile(ctx context.Context, req *gen.DelayProfileInput, params gen.UpdateDelayProfileParams) (*gen.DelayProfile, error) {
	return h.saveDelayProfile(ctx, params.ID, req)
}

// DeleteDelayProfile implements DELETE /delayprofiles/{id}.
func (h *Handler) DeleteDelayProfile(ctx context.Context, params gen.DeleteDelayProfileParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Quality.DeleteDelayProfile(ctx, params.ID)
}

// ReorderDelayProfiles implements PUT /delayprofiles/order.
func (h *Handler) ReorderDelayProfiles(ctx context.Context, req *gen.ReorderRequest) ([]gen.DelayProfile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if err := h.Quality.ReorderDelayProfiles(ctx, req.Ids); err != nil {
		return nil, err
	}
	return h.ListDelayProfiles(ctx)
}

// ListLanguages implements GET /languages.
func (h *Handler) ListLanguages(ctx context.Context) ([]gen.LanguageInfo, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	return convert[[]gen.LanguageInfo](languages.List())
}

// --- parse and decide ---

// decisionContext loads everything the engine needs for a profile.
func (h *Handler) decisionContext(ctx context.Context, p quality.Profile, wanted *gen.OptWantedItem) (decision.Context, error) {
	dc := decision.Context{Profile: p, Now: time.Now(), Mode: decision.ModeInteractive}
	if w, ok := wanted.Get(); ok {
		var err error
		if dc.Wanted, err = convert[decision.Wanted](w); err != nil {
			return dc, err
		}
	}
	dc.Wanted.Kind = p.Kind
	formats, err := h.Formats.List(ctx)
	if err != nil {
		return dc, err
	}
	dc.Formats, _ = customformats.CompileAll(formats)
	if dc.Definitions, err = h.Quality.Definitions(ctx); err != nil {
		return dc, err
	}
	if dc.ReleaseProfiles, err = h.Quality.ReleaseProfiles(ctx); err != nil {
		return dc, err
	}
	if dc.DelayProfiles, err = h.Quality.DelayProfiles(ctx); err != nil {
		return dc, err
	}
	return dc, nil
}

// ParseRelease implements POST /parse.
func (h *Handler) ParseRelease(ctx context.Context, req *gen.ParseRequest) (*gen.ParseResult, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	r := parser.Parse(req.Title)
	kind := quality.Kind(req.Kind.Or(""))
	var prof *quality.Profile
	if id, ok := req.ProfileId.Get(); ok {
		p, err := h.Quality.Profile(ctx, id)
		if err != nil {
			return nil, err
		}
		prof, kind = &p, p.Kind
	}
	if kind == "" {
		switch r.Kind {
		case parser.KindSeries:
			kind = quality.Series
		case parser.KindMusic:
			kind = quality.Music
		default:
			kind = quality.Movies
		}
	}
	q := quality.Identify(&r, kind)
	formats, err := h.Formats.List(ctx)
	if err != nil {
		return nil, err
	}
	compiled, _ := customformats.CompileAll(formats)
	wantedLang := ""
	if w, ok := req.Wanted.Get(); ok {
		wantedLang = w.OriginalLanguage.Or("")
	}
	in := customformats.Input{Title: req.Title, Release: &r, Quality: q, Size: req.Size.Or(0), IndexerFlags: req.IndexerFlags,
		OriginalLanguage: wantedLang}
	out := &gen.ParseResult{CustomFormats: []gen.CustomFormatEvaluation{}}
	for _, c := range compiled {
		matched, results := c.Evaluate(in)
		score := 0
		if prof != nil {
			score = prof.Score(c.ID)
		}
		if matched {
			out.Score += score
		}
		conds, err := convert[[]gen.ConditionResult](results)
		if err != nil {
			return nil, err
		}
		out.CustomFormats = append(out.CustomFormats, gen.CustomFormatEvaluation{ID: c.ID, Name: c.Name, Matched: matched, Score: score, Conditions: conds})
	}
	if out.Release, err = convert[gen.ParsedRelease](r); err != nil {
		return nil, err
	}
	if out.Quality, err = convert[gen.Quality](q); err != nil {
		return nil, err
	}
	if prof != nil {
		dc, err := h.decisionContext(ctx, *prof, &req.Wanted)
		if err != nil {
			return nil, err
		}
		protocol := string(req.Protocol.Or(gen.ProtocolTorrent))
		d := decision.EvaluateOne(ctx, &dc, decision.Candidate{GUID: "parse", Title: req.Title, Size: req.Size.Or(0), Protocol: protocol,
			IndexerFlags: req.IndexerFlags, PublishDate: dc.Now})
		gd, err := convert[gen.Decision](d)
		if err != nil {
			return nil, err
		}
		out.Decision = gen.NewOptDecision(gd)
	}
	return out, nil
}

// EvaluateReleases implements POST /decisions/evaluate.
func (h *Handler) EvaluateReleases(ctx context.Context, req *gen.EvaluateRequest) (*gen.DecisionReport, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	p, err := h.Quality.Profile(ctx, req.ProfileId)
	if err != nil {
		return nil, err
	}
	if quality.Kind(req.Kind) != p.Kind {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: "the profile is for " + string(p.Kind)}
	}
	dc, err := h.decisionContext(ctx, p, &req.Wanted)
	if err != nil {
		return nil, err
	}
	dc.Mode = decision.Mode(req.Mode.Or(gen.EvaluateRequestModeSearch))
	dc.MinimumSeeders = req.MinimumSeeders.Or(0)
	dc.RetentionDays = req.RetentionDays.Or(0)
	dc.MinimumAgeMinutes = req.MinimumAgeMinutes.Or(0)
	cands, err := convert[[]decision.Candidate](req.Candidates)
	if err != nil {
		return nil, err
	}
	for i := range cands {
		if cands[i].Protocol == "" {
			cands[i].Protocol = decision.Torrent
		}
		if cands[i].GUID == "" {
			cands[i].GUID = fmt.Sprintf("candidate-%d", i+1)
		}
	}
	rep := decision.Evaluate(ctx, dc, cands)
	return convert[*gen.DecisionReport](rep)
}
