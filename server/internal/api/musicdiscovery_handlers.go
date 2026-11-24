package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/discover"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/lastfm"
	"github.com/mmzlopasa/locarr/server/internal/metadata/spotify"
	"github.com/mmzlopasa/locarr/server/internal/musicdiscovery"
	"github.com/mmzlopasa/locarr/server/internal/requests"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// radioheadMBID is a well-known artist the provider tests ask about when the library has none.
const radioheadMBID = "a74b1b7f-71a5-4011-9441-d0b5e4122711"

func (h *Handler) discovery() (*musicdiscovery.Service, error) {
	if h.MusicDiscovery == nil {
		return nil, &StatusError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "music discovery is not running"}
	}
	return h.MusicDiscovery, nil
}

// discoveryReader: library managers, and people who may request music (who
// get the list without library ids).
func discoveryReader(ctx context.Context) (auth.Principal, bool, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, false, err
	}
	if p.Can(auth.PermManageLibrary) {
		return p, true, nil
	}
	if requests.CanRequest(p, requests.KindArtist, false) {
		return p, false, nil
	}
	return p, false, errForbidden
}

func (h *Handler) providerStates(ctx context.Context, d *musicdiscovery.Service) ([]gen.MusicDiscoveryProvider, error) {
	list, err := d.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.MusicDiscoveryProvider, 0, len(list))
	for _, st := range list {
		g := gen.MusicDiscoveryProvider{ID: gen.MusicDiscoveryProviderId(st.ID), Enabled: st.Enabled, Configured: st.Configured,
			Artists: st.Artists, Failed: st.Failed, LastError: optStr(st.LastError), LastAt: optTime(st.LastAt)}
		if st.ID == musicdiscovery.Spotify {
			g.RelatedArtists = gen.NewOptMusicDiscoveryProviderRelatedArtists(gen.MusicDiscoveryProviderRelatedArtists(d.SpotifyRelated()))
		}
		out = append(out, g)
	}
	return out, nil
}

func (h *Handler) suggestionList(ctx context.Context, p auth.Principal, manager bool, d *musicdiscovery.Service, res musicdiscovery.Result,
	seeds []string) (*gen.ArtistSuggestionList, error) {
	out := &gen.ArtistSuggestionList{Items: make([]gen.ArtistSuggestion, 0, len(res.Items)), Total: res.Total,
		Tags: make([]gen.ArtistSuggestionTag, 0, len(res.Tags)), Refreshing: d.Running(), CanAdd: manager && h.Music != nil,
		CanRequest: requests.CanRequest(p, requests.KindArtist, false)}
	for _, t := range res.Tags {
		out.Tags = append(out.Tags, gen.ArtistSuggestionTag{Name: t.Name, Count: t.Count})
	}
	items := make([]discover.Item, 0, len(res.Items))
	for _, sg := range res.Items {
		items = append(items, discover.Item{MediaType: discover.Artist, MBID: sg.MBID, ArtistMBID: sg.MBID, Title: sg.Name})
	}
	if h.Discover != nil {
		if err := h.Discover.Annotate(ctx, p, items); err != nil {
			return nil, err
		}
	}
	for i, sg := range res.Items {
		g := gen.ArtistSuggestion{Mbid: sg.MBID, Name: sg.Name, Disambiguation: sg.Disambiguation, Score: sg.Score, SeedCount: sg.SeedCount,
			Seeds: make([]gen.ArtistSuggestionSeed, 0, len(sg.Seeds)), Providers: make([]gen.MusicDiscoveryProviderId, 0, len(sg.Providers)),
			Tags: sg.Tags, Cover: sg.Cover, MatchedByName: sg.Confidence < 0.8,
			Links: gen.ArtistSuggestionLinks{Musicbrainz: "https://musicbrainz.org/artist/" + sg.MBID,
				Listenbrainz: optStr("https://listenbrainz.org/artist/" + sg.MBID + "/")},
			Status: gen.DiscoverStatus(items[i].Status), RequestId: optPos(items[i].RequestID)}
		if g.Status == "" {
			g.Status = gen.DiscoverStatusNone
		}
		for _, s := range sg.Seeds {
			seed := gen.ArtistSuggestionSeed{Mbid: s.MBID, Name: s.Name}
			if manager {
				seed.ArtistId = optPos(s.ArtistID)
			}
			g.Seeds = append(g.Seeds, seed)
		}
		lfm := false
		for _, pr := range sg.Providers {
			g.Providers = append(g.Providers, gen.MusicDiscoveryProviderId(pr))
			lfm = lfm || pr == musicdiscovery.LastFM
		}
		switch {
		case sg.LastFMURL != "":
			g.Links.Lastfm = optStr(sg.LastFMURL)
		case lfm:
			g.Links.Lastfm = optStr(lastfm.ArtistURL(sg.Name))
		}
		g.Links.Spotify = optStr(sg.SpotifyURL)
		if manager {
			g.ArtistId = optPos(sg.ArtistID)
		}
		out.Items = append(out.Items, g)
	}
	providers, err := h.providerStates(ctx, d)
	if err != nil {
		return nil, err
	}
	out.Providers = providers
	last, err := d.LastRefresh(ctx)
	if err != nil {
		return nil, err
	}
	out.LastRefreshAt = optTime(last)
	// Artists nobody has asked about yet (new in the library) are asked in the background.
	all, err := d.LibrarySeeds(ctx)
	if err != nil {
		return nil, err
	}
	if len(seeds) > 0 {
		want := map[string]bool{}
		for _, s := range seeds {
			want[strings.ToLower(s)] = true
		}
		kept := all[:0]
		for _, s := range all {
			if want[s.MBID] {
				kept = append(kept, s)
			}
		}
		all = kept
	}
	active, err := d.Active(ctx)
	if err != nil {
		return nil, err
	}
	if out.Pending, err = d.Pending(ctx, all, active); err != nil {
		return nil, err
	}
	if out.Pending > 0 && !out.Refreshing {
		if err := d.RequestRefresh(ctx, nil); err != nil && h.Log != nil {
			h.Log.Warn("queue music discovery refresh", "err", err)
		}
	}
	return out, nil
}

// ListArtistSuggestions implements GET /music/discover/artists.
func (h *Handler) ListArtistSuggestions(ctx context.Context, params gen.ListArtistSuggestionsParams) (*gen.ArtistSuggestionList, error) {
	p, manager, err := discoveryReader(ctx)
	if err != nil {
		return nil, err
	}
	d, err := h.discovery()
	if err != nil {
		return nil, err
	}
	q := musicdiscovery.Query{Seeds: params.Seed, Tag: params.Tag.Or(""), MinScore: params.MinScore.Or(0), Offset: params.Offset.Or(0),
		Limit: params.Limit.Or(40)}
	for _, pr := range params.Provider {
		q.Providers = append(q.Providers, string(pr))
	}
	res, err := d.Suggest(ctx, q)
	if err != nil {
		return nil, err
	}
	return h.suggestionList(ctx, p, manager, d, res, params.Seed)
}

// GetSimilarArtists implements GET /music/artists/{id}/similar.
func (h *Handler) GetSimilarArtists(ctx context.Context, params gen.GetSimilarArtistsParams) (*gen.ArtistSuggestionList, error) {
	p, err := manager(ctx)
	if err != nil {
		return nil, err
	}
	m, err := h.musicService()
	if err != nil {
		return nil, err
	}
	d, err := h.discovery()
	if err != nil {
		return nil, err
	}
	a, err := m.Artist(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	res, err := d.Suggest(ctx, musicdiscovery.Query{Seeds: []string{a.MBID}, IncludeLibrary: true, Limit: params.Limit.Or(12)})
	if err != nil {
		return nil, err
	}
	return h.suggestionList(ctx, p, true, d, res, []string{a.MBID})
}

// RefreshArtistSuggestions implements POST /music/discover/refresh.
func (h *Handler) RefreshArtistSuggestions(ctx context.Context, req gen.OptRefreshArtistSuggestionsReq) (*gen.QueuedCount, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	d, err := h.discovery()
	if err != nil {
		return nil, err
	}
	var seeds []string
	if r, ok := req.Get(); ok {
		seeds = r.Seeds
	}
	if err := d.RequestRefresh(ctx, seeds); err != nil {
		return nil, err
	}
	return &gen.QueuedCount{Count: 1}, nil
}

func toExclusion(e musicdiscovery.Exclusion) gen.ArtistExclusion {
	return gen.ArtistExclusion{Mbid: e.MBID, Name: e.Name, CreatedAt: e.CreatedAt}
}

// ListArtistExclusions implements GET /music/discover/exclusions.
func (h *Handler) ListArtistExclusions(ctx context.Context) ([]gen.ArtistExclusion, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	d, err := h.discovery()
	if err != nil {
		return nil, err
	}
	list, err := d.Exclusions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ArtistExclusion, 0, len(list))
	for _, e := range list {
		out = append(out, toExclusion(e))
	}
	return out, nil
}

// ExcludeArtist implements POST /music/discover/exclusions.
func (h *Handler) ExcludeArtist(ctx context.Context, req *gen.ExcludeArtistReq) (*gen.ArtistExclusion, error) {
	p, err := manager(ctx)
	if err != nil {
		return nil, err
	}
	d, err := h.discovery()
	if err != nil {
		return nil, err
	}
	e, err := d.Exclude(ctx, req.Mbid, req.Name.Or(""), p.User.ID)
	if errors.Is(err, musicdiscovery.ErrInvalidMBID) {
		return nil, statusErr(http.StatusBadRequest, "validation", err)
	}
	if err != nil {
		return nil, err
	}
	out := toExclusion(e)
	return &out, nil
}

// RemoveArtistExclusion implements DELETE /music/discover/exclusions/{mbid}.
func (h *Handler) RemoveArtistExclusion(ctx context.Context, params gen.RemoveArtistExclusionParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	d, err := h.discovery()
	if err != nil {
		return err
	}
	return d.Include(ctx, params.Mbid)
}

func (h *Handler) musicDiscoverySettings(ctx context.Context, d *musicdiscovery.Service) (*gen.MusicDiscoverySettings, error) {
	cfg, err := d.Config(ctx)
	if err != nil {
		return nil, err
	}
	state := func(k settings.SecretKey) gen.SecretState {
		ok, _ := h.Settings.Configured(ctx, k)
		return gen.SecretState{Configured: ok}
	}
	providers, err := h.providerStates(ctx, d)
	if err != nil {
		return nil, err
	}
	return &gen.MusicDiscoverySettings{Lastfm: cfg.LastFM, Listenbrainz: cfg.ListenBrainz, Musicbrainz: cfg.MusicBrainz, Spotify: cfg.Spotify,
		LastfmApiKey: state(musicdiscovery.LastFMKey), SpotifyClientId: state(musicdiscovery.SpotifyID),
		SpotifyClientSecret: state(musicdiscovery.SpotifySecret), RefreshDays: cfg.RefreshDays, Providers: providers}, nil
}

// GetMusicDiscoverySettings implements GET /settings/musicdiscovery.
func (h *Handler) GetMusicDiscoverySettings(ctx context.Context) (*gen.MusicDiscoverySettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	d, err := h.discovery()
	if err != nil {
		return nil, err
	}
	return h.musicDiscoverySettings(ctx, d)
}

// UpdateMusicDiscoverySettings implements PUT /settings/musicdiscovery.
func (h *Handler) UpdateMusicDiscoverySettings(ctx context.Context, req *gen.MusicDiscoverySettingsInput) (*gen.MusicDiscoverySettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	d, err := h.discovery()
	if err != nil {
		return nil, err
	}
	if key, ok := req.LastfmApiKey.Get(); ok {
		key = strings.TrimSpace(key)
		if key != "" && d.LastFM != nil {
			if err := d.LastFM.Validate(ctx, key); err != nil {
				return nil, err
			}
		}
		if err := h.Settings.SetSecret(ctx, musicdiscovery.LastFMKey, key); err != nil {
			return nil, err
		}
	}
	id, idSet := req.SpotifyClientId.Get()
	secret, secretSet := req.SpotifyClientSecret.Get()
	id, secret = strings.TrimSpace(id), strings.TrimSpace(secret)
	switch {
	case idSet && id == "":
		for _, k := range []settings.SecretKey{musicdiscovery.SpotifyID, musicdiscovery.SpotifySecret} {
			if err := h.Settings.SetSecret(ctx, k, ""); err != nil {
				return nil, err
			}
		}
		d.ResetSpotify()
	case idSet || secretSet:
		if id == "" || secret == "" {
			return nil, &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: "set the Spotify client id and secret together"}
		}
		if d.Spotify != nil {
			caps, err := d.Spotify.Validate(ctx, spotify.Credentials{ID: id, Secret: secret})
			if err != nil {
				return nil, err
			}
			d.SetSpotifyRelated(caps.Related)
		}
		if err := h.Settings.SetSecret(ctx, musicdiscovery.SpotifyID, id); err != nil {
			return nil, err
		}
		if err := h.Settings.SetSecret(ctx, musicdiscovery.SpotifySecret, secret); err != nil {
			return nil, err
		}
	}
	cfg, err := d.Config(ctx)
	if err != nil {
		return nil, err
	}
	set := func(o gen.OptBool, dst *bool) {
		if v, ok := o.Get(); ok {
			*dst = v
		}
	}
	set(req.Lastfm, &cfg.LastFM)
	set(req.Listenbrainz, &cfg.ListenBrainz)
	set(req.Musicbrainz, &cfg.MusicBrainz)
	set(req.Spotify, &cfg.Spotify)
	if v, ok := req.RefreshDays.Get(); ok {
		cfg.RefreshDays = v
	}
	if err := settings.Set(ctx, h.Settings, nil, musicdiscovery.ConfigKey, cfg); err != nil {
		return nil, err
	}
	d.Invalidate()
	return h.musicDiscoverySettings(ctx, d)
}

// TestMusicDiscoveryProvider implements POST /settings/musicdiscovery/test.
func (h *Handler) TestMusicDiscoveryProvider(ctx context.Context, req *gen.TestMusicDiscoveryProviderReq) (*gen.MusicDiscoveryTestResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	d, err := h.discovery()
	if err != nil {
		return nil, err
	}
	probe := radioheadMBID
	if seeds, err := d.LibrarySeeds(ctx); err == nil && len(seeds) > 0 {
		probe = seeds[0].MBID
	}
	out := &gen.MusicDiscoveryTestResult{Ok: true}
	switch req.Provider {
	case gen.MusicDiscoveryProviderIdLastfm:
		key := d.LastFMKeyValue(ctx)
		if key == "" {
			err = metadata.ErrNoKey
		} else if d.LastFM != nil {
			err = d.LastFM.Validate(ctx, key)
		}
	case gen.MusicDiscoveryProviderIdSpotify:
		cr := d.SpotifyCreds(ctx)
		if cr.ID == "" || cr.Secret == "" {
			err = metadata.ErrNoKey
		} else if d.Spotify != nil {
			var caps spotify.Capabilities
			caps, err = d.Spotify.Validate(ctx, cr)
			if err == nil {
				d.SetSpotifyRelated(caps.Related)
			}
		}
		out.RelatedArtists = gen.NewOptMusicDiscoveryTestResultRelatedArtists(gen.MusicDiscoveryTestResultRelatedArtists(d.SpotifyRelated()))
	case gen.MusicDiscoveryProviderIdListenbrainz:
		if d.LB != nil {
			_, err = d.LB.Similar(ctx, probe)
		}
	case gen.MusicDiscoveryProviderIdMusicbrainz:
		if d.MB != nil {
			_, err = d.MB.ArtistRelations(ctx, probe)
		}
	}
	if errors.Is(err, metadata.ErrNotFound) {
		err = nil // reachable, it just does not know the artist
	}
	if err != nil {
		out.Ok, out.Message = false, err.Error()
		switch {
		case errors.Is(err, metadata.ErrNoKey):
			out.Code = optStr("metadata_key_missing")
		case errors.Is(err, metadata.ErrInvalidKey):
			out.Code = optStr("metadata_key_invalid")
		default:
			out.Code = optStr("metadata_unavailable")
		}
		return out, nil
	}
	out.Message = "ok"
	return out, nil
}
