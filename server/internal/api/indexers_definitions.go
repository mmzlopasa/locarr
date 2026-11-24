package api

import (
	"context"
	"path/filepath"
	"slices"
	"sort"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native"
)

// ListIndexerDefinitions implements GET /indexers/definitions.
func (h *Handler) ListIndexerDefinitions(ctx context.Context) (*gen.IndexerCatalog, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.indexerCatalog(ctx)
}

// ReloadIndexerDefinitions implements POST /indexers/definitions/reload.
func (h *Handler) ReloadIndexerDefinitions(ctx context.Context) (*gen.IndexerCatalog, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if err := h.Indexers.ReloadDefinitions(); err != nil {
		return nil, err
	}
	return h.indexerCatalog(ctx)
}

func (h *Handler) indexerCatalog(ctx context.Context) (*gen.IndexerCatalog, error) {
	list, err := h.Indexers.List(ctx)
	if err != nil {
		return nil, err
	}
	using := map[string][]int64{}
	for _, i := range list {
		if i.Kind == indexers.Native {
			using[i.Definition] = append(using[i.Definition], i.ID)
		}
	}
	out := &gen.IndexerCatalog{Definitions: []gen.IndexerDefinition{}, Presets: []gen.UsenetPreset{}, Errors: []gen.IndexerDefinitionError{}}
	if h.Indexers.Natives != nil && h.Indexers.Natives.Dir != "" {
		out.CustomDir = filepath.Clean(h.Indexers.Natives.Dir)
	}
	adultOn := h.Adult != nil && h.Adult.On(ctx)
	for _, d := range h.Indexers.Definitions() {
		// Adult-only sites are offered only while the adult library is on
		// (an indexer already using one keeps it listed).
		if d.Adult && !adultOn && len(using[d.ID]) == 0 {
			continue
		}
		out.Definitions = append(out.Definitions, toDefinition(d, using[d.ID]))
	}
	for _, p := range indexers.UsenetPresets {
		out.Presets = append(out.Presets, gen.UsenetPreset{ID: p.ID, Name: p.Name, BaseUrl: p.BaseURL, ApiPath: p.APIPath, SiteUrl: p.SiteURL})
	}
	for _, e := range h.Indexers.DefinitionErrors() {
		out.Errors = append(out.Errors, gen.IndexerDefinitionError{File: e.File, Message: e.Message})
	}
	return out, nil
}

func toDefinition(d *native.Definition, ids []int64) gen.IndexerDefinition {
	c := &native.Client{Def: d}
	caps, _ := c.Caps(context.Background())
	gc := toCaps(caps)
	g := gen.IndexerDefinition{ID: d.ID, Name: d.Name, Description: d.Description, Version: d.Version, Builtin: d.Builtin,
		Overrides: d.Overrides, Adult: gen.NewOptBool(d.Adult), File: optStr(d.File), Protocol: gen.Protocol(d.Protocol), Language: optStr(d.Language),
		Links: slices.Clone(d.Links), Verified: d.Verified, Challenge: gen.IndexerDefinitionChallenge(d.Challenge), Flaresolverr: gen.IndexerDefinitionFlaresolverr(d.Challenge), //nolint:staticcheck // the old name stays for older apps
		Rss: d.SupportsRSS(), Searching: gc.Searching, Categories: gc.Categories, Settings: []gen.IndexerDefinitionSetting{},
		Aliases: slices.Clone(d.Aliases), IndexerIds: ids}
	if g.Aliases == nil {
		g.Aliases = []string{}
	}
	if g.IndexerIds == nil {
		g.IndexerIds = []int64{}
	}
	if g.Categories == nil {
		g.Categories = []gen.IndexerCategory{}
	}
	if d.RateLimit.Requests > 0 {
		g.RateLimit = gen.NewOptIndexerRateLimit(gen.IndexerRateLimit{Requests: d.RateLimit.Requests, PeriodSeconds: d.RateLimit.PeriodSeconds})
	}
	for _, s := range d.Settings {
		gs := gen.IndexerDefinitionSetting{Name: s.Name, Type: gen.IndexerDefinitionSettingType(s.Type), Label: s.Label,
			Default: s.Default, Options: []gen.IndexerSettingOption{}}
		keys := make([]string, 0, len(s.Options))
		for k := range s.Options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			gs.Options = append(gs.Options, gen.IndexerSettingOption{Value: k, Label: s.Options[k]})
		}
		g.Settings = append(g.Settings, gs)
	}
	return g
}
