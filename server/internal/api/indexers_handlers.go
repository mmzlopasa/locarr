package api

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/jx"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

func optInt(p *int) gen.OptInt {
	if p == nil {
		return gen.OptInt{}
	}
	return gen.NewOptInt(*p)
}

func ptrInt(o gen.OptInt) *int {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}

func toCats(c indexers.Categories) gen.IndexerCategories {
	nz := func(l []int) []int {
		if l == nil {
			return []int{}
		}
		return l
	}
	return gen.IndexerCategories{Movies: nz(c.Movies), Tv: nz(c.TV), Music: nz(c.Music), Books: nz(c.Books)}
}

var searchTypeNames = map[znab.SearchType]gen.SearchType{
	znab.SearchGeneric: gen.SearchTypeSearch, znab.SearchTV: gen.SearchTypeTv, znab.SearchMovie: gen.SearchTypeMovie,
	znab.SearchMusic: gen.SearchTypeMusic, znab.SearchBook: gen.SearchTypeBook,
}

func toCaps(c *znab.Caps) gen.IndexerCaps {
	out := gen.IndexerCaps{ServerTitle: optStr(c.ServerTitle), Searching: []gen.IndexerSearchCap{}, Categories: []gen.IndexerCategory{}}
	if c.LimitMax > 0 {
		out.LimitMax = gen.NewOptInt(c.LimitMax)
	}
	if c.LimitDefault > 0 {
		out.LimitDefault = gen.NewOptInt(c.LimitDefault)
	}
	for _, s := range c.Searching {
		p := s.Params
		if p == nil {
			p = []string{}
		}
		out.Searching = append(out.Searching, gen.IndexerSearchCap{Type: searchTypeNames[s.Type], Available: s.Available, Params: p})
	}
	var conv func(c znab.Category) gen.IndexerCategory
	conv = func(c znab.Category) gen.IndexerCategory {
		g := gen.IndexerCategory{ID: c.ID, Name: c.Name}
		for _, s := range c.Subcat {
			g.Subcategories = append(g.Subcategories, conv(s))
		}
		return g
	}
	for _, cat := range c.Categories {
		out.Categories = append(out.Categories, conv(cat))
	}
	return out
}

func toIndexer(i indexers.Indexer, now time.Time, defs func(string) (*native.Definition, bool)) gen.Indexer {
	out := gen.Indexer{
		ID: i.ID, Name: i.Name, Kind: gen.IndexerKind(i.Kind), Protocol: gen.Protocol(i.Protocol()), BaseUrl: i.BaseURL,
		ApiPath: i.APIPath, ApiKey: gen.SecretState{Configured: i.HasAPIKey}, Enabled: i.Enabled, EnableRss: i.EnableRSS,
		EnableAutomaticSearch: i.EnableAutomatic, EnableInteractiveSearch: i.EnableInteractive, Priority: i.Priority,
		Tags: i.Tags, Categories: toCats(i.Categories), MinimumSeeders: i.MinimumSeeders,
		SeedTimeMinutes: optInt(i.SeedTimeMinutes), RequestLimit: optInt(i.RequestLimit),
		RequestPeriodSeconds: optInt(i.RequestPeriodSec), GrabLimitPerDay: optInt(i.GrabLimitPerDay),
		TimeoutSeconds: i.TimeoutSeconds, ProxyUrl: optStr(i.ProxyURL), ProxyUsername: optStr(i.ProxyUsername),
		ProxyPassword: gen.SecretState{Configured: i.HasProxyPassword}, FlaresolverrUrl: optStr(i.FlareSolverrURL),
		CapsUpdatedAt: optTime(i.CapsUpdatedAt), CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt,
		Health: gen.IndexerHealth{Status: gen.IndexerHealthStatus(i.HealthStatus()), Failures: i.Health.Failures,
			LastError: optStr(i.Health.LastError), LastErrorCode: optStr(i.Health.LastErrorCode),
			LastErrorAt: optTime(i.Health.LastErrorAt), LastSuccessAt: optTime(i.Health.LastSuccessAt)},
	}
	if i.InBackoff(now) {
		out.Health.RetryAt = optTime(i.Health.RetryAt)
	}
	if i.SeedRatio != nil {
		out.SeedRatio = gen.NewOptFloat64(*i.SeedRatio)
	}
	if i.SourceID != nil {
		out.Source = gen.NewOptIndexerSourceRef(gen.IndexerSourceRef{ID: *i.SourceID, Name: i.SourceName, Ref: i.SourceRef})
	}
	if i.Caps != nil {
		out.Caps = gen.NewOptIndexerCaps(toCaps(i.Caps))
	}
	if i.Kind == indexers.Native {
		out.Health.ParseFailures = gen.NewOptInt(i.Health.ParseFailures)
		out.Health.SelfTestAt = optTime(i.Health.SelfTestAt)
		if i.Health.SelfTestOK != nil {
			out.Health.SelfTestOk = gen.NewOptBool(*i.Health.SelfTestOK)
		}
		ref := gen.IndexerDefinitionRef{ID: i.Definition, Name: i.Definition, Missing: true}
		if def, ok := defs(i.Definition); ok {
			ref = gen.IndexerDefinitionRef{ID: def.ID, Name: def.Name, Version: def.Version, Builtin: def.Builtin, Overrides: def.Overrides}
		}
		out.Definition = gen.NewOptIndexerDefinitionRef(ref)
		opts := gen.IndexerOptions{}
		for k, v := range i.Options {
			opts[k] = v
		}
		out.Options = gen.NewOptIndexerOptions(opts)
	}
	return out
}

// toIndexer converts with the handler's site definitions.
func (h *Handler) toIndexer(i indexers.Indexer, now time.Time) gen.Indexer {
	return toIndexer(i, now, h.Indexers.Definition)
}

func (h *Handler) fromInput(g *gen.IndexerInput) indexers.Input {
	in := indexers.DefaultInput()
	isNative := indexers.Kind(g.Kind) == indexers.Native
	if isNative {
		if def, ok := h.Indexers.Definition(g.Definition.Or("")); ok {
			in = h.Indexers.NativeInput(def)
		}
		in.Definition = g.Definition.Or("")
		in.Options = map[string]string{}
		if o, ok := g.Options.Get(); ok {
			for k, v := range o {
				in.Options[k] = v
			}
		}
	}
	in.Name, in.Kind = g.Name, indexers.Kind(g.Kind)
	if v := g.BaseUrl.Or(""); v != "" || !isNative {
		in.BaseURL = v
	}
	in.APIPath = g.ApiPath.Or("/api")
	if v, ok := g.ApiKey.Get(); ok {
		in.APIKey = &v
	}
	in.Enabled = g.Enabled.Or(true)
	in.EnableRSS = g.EnableRss.Or(in.EnableRSS)
	in.EnableAutomatic = g.EnableAutomaticSearch.Or(true)
	in.EnableInteractive = g.EnableInteractiveSearch.Or(true)
	in.Priority = g.Priority.Or(25)
	in.Tags = g.Tags
	if c, ok := g.Categories.Get(); ok {
		in.Categories = &indexers.Categories{Movies: c.Movies, TV: c.Tv, Music: c.Music, Books: c.Books}
	}
	in.MinimumSeeders = g.MinimumSeeders.Or(1)
	if v, ok := g.SeedRatio.Get(); ok {
		in.SeedRatio = &v
	}
	in.SeedTimeMinutes, in.GrabLimitPerDay = ptrInt(g.SeedTimeMinutes), ptrInt(g.GrabLimitPerDay)
	if g.RequestLimit.Set || g.RequestPeriodSeconds.Set || !isNative {
		in.RequestLimit, in.RequestPeriodSec = ptrInt(g.RequestLimit), ptrInt(g.RequestPeriodSeconds)
	}
	// The API fills its default timeout (30 s) in; a slow site keeps its own.
	if t := g.TimeoutSeconds.Or(0); t > 0 && (!isNative || t != 30) {
		in.TimeoutSeconds = t
	}
	in.ProxyURL, in.ProxyUsername, in.FlareSolverrURL = g.ProxyUrl.Or(""), g.ProxyUsername.Or(""), g.FlaresolverrUrl.Or("")
	if v, ok := g.ProxyPassword.Get(); ok {
		in.ProxyPassword = &v
	}
	return in
}

// ListIndexers implements GET /indexers.
func (h *Handler) ListIndexers(ctx context.Context) ([]gen.Indexer, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Indexers.List(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]gen.Indexer, 0, len(list))
	for _, i := range list {
		out = append(out, h.toIndexer(i, now))
	}
	return out, nil
}

// CreateIndexer implements POST /indexers.
func (h *Handler) CreateIndexer(ctx context.Context, req *gen.IndexerInput) (*gen.Indexer, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	i, err := h.Indexers.Create(ctx, h.fromInput(req))
	if err != nil {
		return nil, err
	}
	out := h.toIndexer(i, time.Now())
	return &out, nil
}

// GetIndexer implements GET /indexers/{id}.
func (h *Handler) GetIndexer(ctx context.Context, params gen.GetIndexerParams) (*gen.Indexer, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	i, err := h.Indexers.Get(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := h.toIndexer(i, time.Now())
	return &out, nil
}

// UpdateIndexer implements PUT /indexers/{id}.
func (h *Handler) UpdateIndexer(ctx context.Context, req *gen.IndexerInput, params gen.UpdateIndexerParams) (*gen.Indexer, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	i, err := h.Indexers.Update(ctx, params.ID, h.fromInput(req))
	if err != nil {
		return nil, err
	}
	out := h.toIndexer(i, time.Now())
	return &out, nil
}

// DeleteIndexer implements DELETE /indexers/{id}.
func (h *Handler) DeleteIndexer(ctx context.Context, params gen.DeleteIndexerParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Indexers.Delete(ctx, params.ID)
}

func toTestResult(r indexers.TestResult) *gen.IndexerTestResult {
	out := &gen.IndexerTestResult{Ok: r.OK, DurationMs: r.Duration.Milliseconds(), ErrorCode: optStr(r.ErrorCode), Error: optStr(r.Error)}
	if r.Caps != nil {
		out.Caps = gen.NewOptIndexerCaps(toCaps(r.Caps))
		out.DefaultCategories = gen.NewOptIndexerCategories(toCats(r.DefaultCategories))
	}
	if r.OK {
		out.ProbeResults = gen.NewOptInt(r.ProbeResults)
	}
	return out
}

// TestIndexer implements POST /indexers/{id}/test.
func (h *Handler) TestIndexer(ctx context.Context, params gen.TestIndexerParams) (*gen.IndexerTestResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	r, err := h.Indexers.Test(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return toTestResult(r), nil
}

// TestIndexerConfig implements POST /indexers/test.
func (h *Handler) TestIndexerConfig(ctx context.Context, req *gen.IndexerTestRequest) (*gen.IndexerTestResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	var existing *int64
	if v, ok := req.IndexerId.Get(); ok {
		existing = &v
	}
	r, err := h.Indexers.TestInput(ctx, h.fromInput(&req.Indexer), existing)
	if err != nil {
		return nil, err
	}
	return toTestResult(r), nil
}

// ResetIndexerHealth implements POST /indexers/{id}/health/reset.
func (h *Handler) ResetIndexerHealth(ctx context.Context, params gen.ResetIndexerHealthParams) (*gen.Indexer, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	i, err := h.Indexers.ResetHealth(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := h.toIndexer(i, time.Now())
	return &out, nil
}

// canGrab reports whether the caller may see download links and magnets.
func canGrab(p auth.Principal) bool { return p.IsAdmin() || p.User.Role == auth.RoleManager }

// DownloadRelease implements GET /indexers/{id}/download.
func (h *Handler) DownloadRelease(ctx context.Context, params gen.DownloadReleaseParams) (*gen.DownloadReleaseOKHeaders, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if !canGrab(p) {
		return nil, errForbidden
	}
	f, err := h.Indexers.FetchToken(ctx, params.ID, params.Token)
	if err != nil {
		return nil, err
	}
	if f.Magnet != "" {
		return nil, &StatusError{Status: http.StatusConflict, Code: "magnet",
			Message: "the indexer answered with a magnet link instead of a file: use the release's magnet link or send it to a download client"}
	}
	disp := mime.FormatMediaType("attachment", map[string]string{"filename": f.Name})
	return &gen.DownloadReleaseOKHeaders{ContentDisposition: optStr(disp),
		Response: gen.DownloadReleaseOK{Data: bytes.NewReader(f.Data)}}, nil
}

// GetIndexerSettings implements GET /indexers/settings.
func (h *Handler) GetIndexerSettings(ctx context.Context) (*gen.IndexerSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.indexerSettings(ctx)
}

func (h *Handler) indexerSettings(ctx context.Context) (*gen.IndexerSettings, error) {
	var out gen.IndexerSettings
	var err error
	get := func(dst *int, k settings.Key[int]) {
		if err == nil {
			*dst, err = settings.Get(ctx, h.Settings, k)
		}
	}
	get(&out.RssSyncIntervalMinutes, indexers.RSSIntervalMinutes)
	get(&out.ReleaseRetentionDays, indexers.ReleaseRetentionDays)
	get(&out.SearchTimeoutSeconds, indexers.SearchTimeoutSeconds)
	get(&out.SearchCacheSeconds, indexers.SearchCacheSeconds)
	if err != nil {
		return nil, err
	}
	if out.AllowedHosts, err = settings.Get(ctx, h.Settings, indexers.AllowedHosts); err != nil {
		return nil, err
	}
	if out.AllowedHosts == nil {
		out.AllowedHosts = []string{}
	}
	if out.BlockPrivateNetworks, err = settings.Get(ctx, h.Settings, indexers.BlockPrivateNetworks); err != nil {
		return nil, err
	}
	if out.PublicTrackers, err = settings.Get(ctx, h.Settings, indexers.PublicTrackers); err != nil {
		return nil, err
	}
	if out.PublicTrackers == nil {
		out.PublicTrackers = []string{}
	}
	if out.UserAgent, err = settings.Get(ctx, h.Settings, indexers.UserAgent); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateIndexerSettings implements PATCH /indexers/settings.
func (h *Handler) UpdateIndexerSettings(ctx context.Context, req *gen.UpdateIndexerSettingsRequest) (*gen.IndexerSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if req.AllowedHosts != nil {
		hosts := []string{}
		for _, h := range req.AllowedHosts {
			if h = strings.TrimSpace(h); h != "" {
				hosts = append(hosts, h)
			}
		}
		if err := indexers.ValidateAllowedHosts(hosts); err != nil {
			return nil, err
		}
		if err := settings.Set(ctx, h.Settings, nil, indexers.AllowedHosts, hosts); err != nil {
			return nil, err
		}
	}
	for _, kv := range []struct {
		v gen.OptInt
		k settings.Key[int]
	}{{req.ReleaseRetentionDays, indexers.ReleaseRetentionDays}, {req.SearchTimeoutSeconds, indexers.SearchTimeoutSeconds},
		{req.SearchCacheSeconds, indexers.SearchCacheSeconds}} {
		if v, ok := kv.v.Get(); ok {
			if err := settings.Set(ctx, h.Settings, nil, kv.k, v); err != nil {
				return nil, err
			}
		}
	}
	if req.PublicTrackers != nil {
		trackers := []string{}
		for _, t := range req.PublicTrackers {
			if t = strings.TrimSpace(t); t != "" {
				if err := indexers.ValidateTracker(t); err != nil {
					return nil, err
				}
				trackers = append(trackers, t)
			}
		}
		if err := settings.Set(ctx, h.Settings, nil, indexers.PublicTrackers, trackers); err != nil {
			return nil, err
		}
	}
	if v, ok := req.UserAgent.Get(); ok {
		if err := settings.Set(ctx, h.Settings, nil, indexers.UserAgent, strings.TrimSpace(v)); err != nil {
			return nil, err
		}
	}
	if v, ok := req.BlockPrivateNetworks.Get(); ok {
		if err := settings.Set(ctx, h.Settings, nil, indexers.BlockPrivateNetworks, v); err != nil {
			return nil, err
		}
	}
	if v, ok := req.RssSyncIntervalMinutes.Get(); ok {
		if err := settings.Set(ctx, h.Settings, nil, indexers.RSSIntervalMinutes, v); err != nil {
			return nil, err
		}
		if err := h.Jobs.Reschedule(ctx, indexers.TaskRSSSync, time.Duration(v)*time.Minute); err != nil {
			return nil, err
		}
	}
	return h.indexerSettings(ctx)
}

func toSource(s indexers.IndexerSource) gen.IndexerSource {
	return gen.IndexerSource{ID: s.ID, Kind: gen.IndexerSourceKind(s.Kind), Name: s.Name, URL: s.URL,
		ApiKey: gen.SecretState{Configured: true}, Exclude: s.Exclude, IndexerCount: s.IndexerCount,
		LastSyncAt: optTime(s.LastSyncAt), LastError: optStr(s.LastError), CreatedAt: s.CreatedAt}
}

func toSync(r indexers.SyncResult) *gen.IndexerSourceSyncResult {
	return &gen.IndexerSourceSyncResult{Source: toSource(r.Source), Added: r.Added, Updated: r.Updated,
		Disabled: r.Disabled, Unchanged: r.Unchanged}
}

// ListIndexerSources implements GET /indexers/sources.
func (h *Handler) ListIndexerSources(ctx context.Context) ([]gen.IndexerSource, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Indexers.Sources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.IndexerSource, 0, len(list))
	for _, s := range list {
		out = append(out, toSource(s))
	}
	return out, nil
}

// CreateIndexerSource implements POST /indexers/sources.
func (h *Handler) CreateIndexerSource(ctx context.Context, req *gen.IndexerSourceInput) (*gen.IndexerSourceSyncResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	r, err := h.Indexers.CreateSource(ctx, indexers.SourceInput{Kind: indexers.SourceKind(req.Kind), Name: req.Name.Or(""),
		URL: req.URL, APIKey: req.ApiKey, Exclude: req.Exclude})
	if err != nil {
		return nil, err
	}
	return toSync(r), nil
}

// PreviewIndexerSource implements POST /indexers/sources/preview.
func (h *Handler) PreviewIndexerSource(ctx context.Context, req *gen.IndexerSourcePreviewRequest) (*gen.IndexerSourcePreview, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	items, err := h.Indexers.PreviewSource(ctx, req.SourceId.Or(0), indexers.SourceInput{Kind: indexers.SourceKind(req.Kind.Or("")),
		URL: req.URL.Or(""), APIKey: req.ApiKey.Or("")})
	if err != nil {
		return nil, err
	}
	out := &gen.IndexerSourcePreview{Items: make([]gen.IndexerSourceItem, 0, len(items))}
	for _, it := range items {
		g := gen.IndexerSourceItem{Ref: it.Ref, Name: it.Name, Kind: gen.IndexerKind(it.Kind), Protocol: gen.Protocol(it.Kind.Protocol()),
			FeedUrl: it.FeedURL(), EnabledOnSource: gen.NewOptBool(it.EnabledOnSource), Action: gen.IndexerSourceAction(it.Action)}
		if it.IndexerID > 0 {
			g.IndexerId = gen.NewOptInt64(it.IndexerID)
		}
		out.Items = append(out.Items, g)
	}
	return out, nil
}

// DeleteIndexerSource implements DELETE /indexers/sources/{id}.
func (h *Handler) DeleteIndexerSource(ctx context.Context, params gen.DeleteIndexerSourceParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Indexers.DeleteSource(ctx, params.ID)
}

// SyncIndexerSource implements POST /indexers/sources/{id}/sync.
func (h *Handler) SyncIndexerSource(ctx context.Context, params gen.SyncIndexerSourceParams) (*gen.IndexerSourceSyncResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	r, err := h.Indexers.SyncSource(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return toSync(r), nil
}

// releaseView converts releases for one caller.
type releaseView struct {
	h    *Handler
	grab bool
}

func (v releaseView) release(r indexers.Release) gen.Release {
	out := gen.Release{GUID: r.GUID, Title: r.Title, IndexerId: r.IndexerID, IndexerName: r.IndexerName,
		IndexerPriority: gen.NewOptInt(r.IndexerPriority), Protocol: gen.Protocol(r.Protocol), Categories: r.Categories,
		Flags: r.Flags, InfoHash: optStr(r.InfoHash), InfoUrl: optStr(r.InfoURL), ImdbId: optStr(r.IMDBID),
		Season: optStr(r.Season), Episode: optStr(r.Episode),
		Files: optInt(r.Files), Grabs: optInt(r.Grabs), Seeders: optInt(r.Seeders), Leechers: optInt(r.Leechers), Peers: optInt(r.Peers)}
	if r.Size > 0 {
		out.Size = gen.NewOptInt64(r.Size)
	}
	if !r.PublishDate.IsZero() {
		out.PublishDate = gen.NewOptDateTime(r.PublishDate)
	}
	for _, x := range []struct {
		v   int64
		dst *gen.OptInt64
	}{{r.TMDBID, &out.TmdbId}, {r.TVDBID, &out.TvdbId}, {r.TVMazeID, &out.TvmazeId}} {
		if x.v > 0 {
			*x.dst = gen.NewOptInt64(x.v)
		}
	}
	for _, x := range []struct {
		v   *float64
		dst *gen.OptFloat64
	}{{r.DownloadVolumeFactor, &out.DownloadVolumeFactor}, {r.UploadVolumeFactor, &out.UploadVolumeFactor},
		{r.MinimumRatio, &out.MinimumRatio}} {
		if x.v != nil {
			*x.dst = gen.NewOptFloat64(*x.v)
		}
	}
	if r.MinimumSeedTime > 0 {
		out.MinimumSeedTimeSeconds = gen.NewOptInt64(int64(r.MinimumSeedTime / time.Second))
	}
	if v.grab {
		out.MagnetUrl = optStr(r.MagnetURL)
		if r.DownloadURL != "" {
			if tok, err := v.h.Indexers.DownloadToken(r); err == nil {
				out.DownloadLink = gen.NewOptString("/api/v1/indexers/" + strconv.FormatInt(r.IndexerID, 10) +
					"/download?token=" + url.QueryEscape(tok))
			}
		}
	}
	return out
}

func toSearchIndexer(r indexers.IndexerResult) gen.SearchIndexerResult {
	return gen.SearchIndexerResult{IndexerId: r.IndexerID, IndexerName: r.IndexerName, Status: gen.SearchIndexerStatus(r.Status),
		Count: r.Count, Cached: r.Cached, DurationMs: r.Duration.Milliseconds(), ErrorCode: optStr(r.ErrorCode), Error: optStr(r.Error)}
}

var searchTypes = map[gen.SearchType]indexers.SearchType{gen.SearchTypeSearch: indexers.SearchAny, gen.SearchTypeTv: indexers.SearchTV,
	gen.SearchTypeMovie: indexers.SearchMovie, gen.SearchTypeMusic: indexers.SearchMusic, gen.SearchTypeBook: indexers.SearchBook}

func toQuery(p gen.SearchParams, userID int64) indexers.Query {
	return indexers.Query{Type: searchTypes[p.Type.Or(gen.SearchTypeSearch)], Term: strings.TrimSpace(p.Q.Or("")),
		Categories: p.Categories, IndexerIDs: p.IndexerIds, IMDBID: p.ImdbId.Or(""), TMDBID: p.TmdbId.Or(0),
		TVDBID: p.TvdbId.Or(0), TVMazeID: p.TvmazeId.Or(0), Season: p.Season.Or(""), Episode: p.Episode.Or(""),
		Artist: p.Artist.Or(""), Album: p.Album.Or(""), Author: p.Author.Or(""), Title: p.Title.Or(""),
		Limit: p.Limit.Or(0), Offset: p.Offset.Or(0), Source: indexers.SourceInteractive, UserID: userID}
}

// Search implements GET /search.
func (h *Handler) Search(ctx context.Context, params gen.SearchParams) (*gen.SearchResponse, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	res, err := h.Indexers.Search(ctx, toQuery(params, p.User.ID))
	if err != nil {
		return nil, err
	}
	v := releaseView{h: h, grab: canGrab(p)}
	out := &gen.SearchResponse{Releases: make([]gen.Release, 0, len(res.Releases)), Indexers: make([]gen.SearchIndexerResult, 0, len(res.Indexers)),
		DurationMs: res.Duration.Milliseconds()}
	for _, r := range res.Releases {
		out.Releases = append(out.Releases, v.release(r))
	}
	for _, r := range res.Indexers {
		out.Indexers = append(out.Indexers, toSearchIndexer(r))
	}
	return out, nil
}

// SearchStream implements GET /search/stream as Server-Sent Events. The
// router flushes every write of this endpoint (see app.flushStream).
func (h *Handler) SearchStream(ctx context.Context, params gen.SearchStreamParams) (gen.SearchStreamOK, error) {
	p, err := principal(ctx)
	if err != nil {
		return gen.SearchStreamOK{}, err
	}
	q := toQuery(gen.SearchParams(params), p.User.ID)
	v := releaseView{h: h, grab: canGrab(p)}
	pr, pw := io.Pipe()
	go func() {
		write := func(event string, e *jx.Encoder) error {
			var b bytes.Buffer
			b.WriteString("event: " + event + "\ndata: ")
			b.Write(e.Bytes())
			b.WriteString("\n\n")
			_, err := pw.Write(b.Bytes())
			return err
		}
		// A comment first, so the client sees the stream open at once.
		if _, err := pw.Write([]byte(": search started\n\n")); err != nil {
			return
		}
		res, err := h.Indexers.SearchStream(ctx, q, func(ev indexers.Event) {
			e := &jx.Encoder{}
			e.ObjStart()
			e.FieldStart("indexer")
			ir := toSearchIndexer(ev.Indexer)
			ir.Encode(e)
			e.FieldStart("releases")
			e.ArrStart()
			for _, r := range ev.Releases {
				g := v.release(r)
				g.Encode(e)
			}
			e.ArrEnd()
			e.ObjEnd()
			_ = write("indexer", e)
		})
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		e := &jx.Encoder{}
		e.ObjStart()
		e.FieldStart("total")
		e.Int(len(res.Releases))
		e.FieldStart("durationMs")
		e.Int64(res.Duration.Milliseconds())
		e.ObjEnd()
		_ = write("done", e)
		_ = pw.Close()
	}()
	go func() {
		<-ctx.Done()
		_ = pr.CloseWithError(ctx.Err())
	}()
	return gen.SearchStreamOK{Data: pr}, nil
}

// ListSearchLog implements GET /search/log.
func (h *Handler) ListSearchLog(ctx context.Context, params gen.ListSearchLogParams) ([]gen.SearchLogEntry, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Indexers.SearchLog(ctx, params.ItemRef.Or(""), params.Limit.Or(50))
	if err != nil {
		return nil, err
	}
	out := make([]gen.SearchLogEntry, 0, len(list))
	hide := h.hidesAdult(ctx)
	for _, e := range list {
		if hide && (library.IsAdultRef(e.ItemRef) || e.Type == indexers.SearchAdult) {
			continue
		}
		g := gen.SearchLogEntry{ID: e.ID, StartedAt: e.StartedAt, DurationMs: e.Duration.Milliseconds(),
			Source: gen.SearchLogEntrySource(e.Source), Type: gen.SearchType(apiSearchType(e.Type)), Query: e.Query,
			ItemRef: optStr(e.ItemRef), ResultCount: e.ResultCount, ErrorCount: e.ErrorCount,
			Indexers: make([]gen.SearchIndexerResult, 0, len(e.Indexers))}
		if e.UserID > 0 {
			g.UserId = gen.NewOptInt64(e.UserID)
		}
		for _, ir := range e.Indexers {
			g.Indexers = append(g.Indexers, toSearchIndexer(ir))
		}
		out = append(out, g)
	}
	return out, nil
}

func apiSearchType(t indexers.SearchType) string {
	if slices.Contains([]indexers.SearchType{indexers.SearchAny, indexers.SearchTV, indexers.SearchMovie, indexers.SearchMusic,
		indexers.SearchBook}, t) {
		return string(t)
	}
	return string(indexers.SearchAny)
}
