package migrate

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

func statDir(p string) (bool, error) {
	st, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	return st.IsDir(), nil
}

// masked reports a secret the source did not reveal.
func masked(v string) bool {
	return v != "" && strings.Trim(v, "*") == ""
}

func str(f map[string]any, k string) string {
	switch v := f[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func num(f map[string]any, k string) float64 {
	switch v := f[k].(type) {
	case float64:
		return v
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	}
	return 0
}

func boolean(f map[string]any, k string) bool {
	v, _ := f[k].(bool)
	return v
}

func ints(f map[string]any, k string) []int {
	var out []int
	if list, ok := f[k].([]any); ok {
		for _, x := range list {
			if n, ok := x.(float64); ok {
				out = append(out, int(n))
			}
		}
	}
	return out
}

// ---- naming ----

type namingSetting struct {
	tokens []naming.Token
	set    func(ctx context.Context, s *settings.Store, v string) error
}

func stringSetting(k settings.Key[string]) func(context.Context, *settings.Store, string) error {
	return func(ctx context.Context, s *settings.Store, v string) error { return settings.Set(ctx, s, nil, k, v) }
}

func (p *planner) planNaming(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.Settings || len(s.Naming) == 0 {
		return
	}
	var fields map[string]namingSetting
	var rename func(context.Context, bool) error
	renameKey := ""
	switch s.Kind {
	case Radarr:
		fields = map[string]namingSetting{
			"standardMovieFormat": {tokens: naming.MovieTokens, set: stringSetting(library.MovieFileFormat)},
			"movieFolderFormat":   {tokens: naming.MovieTokens, set: stringSetting(library.MovieFolderFormat)},
		}
		renameKey = "renameMovies"
		rename = func(ctx context.Context, on bool) error {
			return settings.Set(ctx, p.l.Settings, nil, library.RenameMovies, on)
		}
	case Sonarr:
		fields = map[string]namingSetting{
			"standardEpisodeFormat": {tokens: naming.EpisodeTokens, set: stringSetting(series.StandardEpisodeFormat)},
			"dailyEpisodeFormat":    {tokens: naming.EpisodeTokens, set: stringSetting(series.DailyEpisodeFormat)},
			"animeEpisodeFormat":    {tokens: naming.EpisodeTokens, set: stringSetting(series.AnimeEpisodeFormat)},
			"seriesFolderFormat":    {tokens: naming.SeriesFolderTokens, set: stringSetting(series.SeriesFolderFormat)},
			"seasonFolderFormat":    {tokens: naming.SeasonFolderTokens, set: stringSetting(series.SeasonFolderFormat)},
			"specialsFolderFormat":  {tokens: naming.SeasonFolderTokens, set: stringSetting(series.SpecialsFolderFormat)},
		}
		renameKey = "renameEpisodes"
		rename = func(ctx context.Context, on bool) error {
			return settings.Set(ctx, p.l.Settings, nil, series.RenameEpisodes, on)
		}
	case Lidarr:
		fields = map[string]namingSetting{
			"standardTrackFormat":  {tokens: naming.TrackTokens, set: stringSetting(music.TrackFormat)},
			"multiDiscTrackFormat": {tokens: naming.TrackTokens, set: stringSetting(music.MultiDiscTrackFormat)},
			"artistFolderFormat":   {tokens: naming.ArtistTokens, set: stringSetting(music.ArtistFolderFormat)},
		}
		renameKey = "renameTracks"
		rename = func(ctx context.Context, on bool) error {
			return settings.Set(ctx, p.l.Settings, nil, music.RenameTracks, on)
		}
	}
	type change struct {
		set func(context.Context, *settings.Store, string) error
		v   string
	}
	var changes []change
	var notes []string
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	applied := map[string]string{}
	for _, k := range names {
		raw := str(s.Naming, k)
		if raw == "" {
			continue
		}
		f := fields[k]
		tmpl, unknown := TranslateNaming(raw)
		if len(unknown) > 0 {
			notes = append(notes, fmt.Sprintf("%s kept Locarr's default (unknown tokens: %s)", k, strings.Join(unknown, ", ")))
			continue
		}
		if err := naming.Validate(tmpl, f.tokens); err != nil {
			notes = append(notes, fmt.Sprintf("%s kept Locarr's default (%v)", k, err))
			continue
		}
		changes = append(changes, change{f.set, tmpl})
		applied[k] = tmpl
	}
	colon := MapColon(s.Naming["colonReplacementFormat"])
	style := ""
	if s.Kind == Sonarr {
		style = MapMultiEpisodeStyle(s.Naming["multiEpisodeStyle"])
	}
	_, hasRename := s.Naming[renameKey]
	renameOn := boolean(s.Naming, renameKey)
	it := &Item{Key: idKey(s.SourceID, "naming", s.Kind), SourceID: s.SourceID, Category: "naming", Name: s.Name + " naming"}
	if len(notes) > 0 {
		it.note("%s", strings.Join(notes, "; "))
	}
	if len(changes) == 0 && colon == "" && style == "" && !hasRename {
		it.Action, it.Reason = ActionSkip, "nothing_to_take"
		p.add(it)
		return
	}
	h := hashOf([]any{applied, colon, style, renameOn, hasRename})
	d := p.decideNamed(it, s, "naming", string(s.Kind), it.Name, h, func(int64) bool { return true },
		func(string) (int64, bool) { return 0, false })
	if d.action == ActionCreate {
		it.Action = ActionUpdate
	}
	if it.Action != ActionSkip {
		it.run = func(ctx context.Context) (string, error) {
			for _, c := range changes {
				if err := c.set(ctx, p.l.Settings, c.v); err != nil {
					return "", err
				}
			}
			if colon != "" {
				if err := settings.Set(ctx, p.l.Settings, nil, library.ColonReplacement, colon); err != nil {
					return "", err
				}
			}
			if style != "" {
				if err := settings.Set(ctx, p.l.Settings, nil, series.MultiEpisodeStyle, style); err != nil {
					return "", err
				}
			}
			if hasRename {
				if err := rename(ctx, renameOn); err != nil {
					return "", err
				}
			}
			return "settings:naming", nil
		}
	}
	p.add(it)
}

// ---- Prowlarr ----

func (p *planner) prowlarrConn(s *Snapshot) (string, bool) {
	key, ok := p.secrets[s.SourceID]
	return key, ok && key != ""
}

func (p *planner) planProwlarr(s *Snapshot, so SourceOptions) {
	if s.Kind != Prowlarr || !so.Settings {
		return
	}
	mode := so.ProwlarrMode
	if mode != "direct" {
		mode = "source"
	}
	apiKey, _ := p.prowlarrConn(s)
	if mode == "source" {
		it := &Item{Key: idKey(s.SourceID, "indexersource", "prowlarr"), SourceID: s.SourceID, Category: "indexersource",
			Name: fmt.Sprintf("%s as indexer source (%d indexers)", s.Name, len(s.Indexers))}
		existing, _ := p.l.Indexers.Sources(p.ctx)
		var found int64
		for _, src := range existing {
			if src.Kind == indexers.Prowlarr && strings.EqualFold(strings.TrimRight(src.URL, "/"), strings.TrimRight(s.URL, "/")) {
				found = src.ID
			}
		}
		if found > 0 {
			it.Action, it.Reason, it.LocalRef = ActionSkip, "exists", fmt.Sprintf("indexersource:%d", found)
			it.note("already a source; it keeps its indexers in sync by itself")
		} else {
			it.Action = ActionCreate
			u := s.URL
			it.run = func(ctx context.Context) (string, error) {
				res, err := p.l.Indexers.CreateSource(ctx, indexers.SourceInput{Kind: indexers.Prowlarr, Name: s.Name, URL: u, APIKey: apiKey})
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("indexersource:%d", res.Source.ID), nil
			}
		}
		p.add(it)
		return
	}
	for _, ix := range s.Indexers {
		p.planOneIndexer(s, ix, apiKey)
	}
}

// ---- indexers ----

// categorySplit sorts Newznab category ids into Locarr's groups.
func categorySplit(cats []int) indexers.Categories {
	var c indexers.Categories
	for _, id := range cats {
		switch {
		case id >= 2000 && id < 3000:
			c.Movies = append(c.Movies, id)
		case id >= 5000 && id < 6000:
			c.TV = append(c.TV, id)
		case id >= 3000 && id < 4000:
			c.Music = append(c.Music, id)
		case id >= 7000 && id < 8000:
			c.Books = append(c.Books, id)
		}
	}
	return c
}

func mergeInts(a, b []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, x := range append(append([]int{}, a...), b...) {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Ints(out)
	return out
}

// planOneIndexer plans one Prowlarr indexer added on its own: generic
// Torznab/Newznab indexers point at their own site, the others at
// Prowlarr's feed for them.
func (p *planner) planOneIndexer(s *Snapshot, ix Indexer, prowlarrKey string) {
	key := idKey(s.SourceID, "indexer", ix.ID)
	it := &Item{Key: key, SourceID: s.SourceID, Category: "indexer", Name: ix.Name}
	kind := indexers.Torznab
	if strings.EqualFold(ix.Protocol, "usenet") {
		kind = indexers.Newznab
	}
	in := indexers.DefaultInput()
	in.Name, in.Kind, in.Enabled, in.Priority, in.Tags = ix.Name, kind, ix.Enable, clamp(ix.Priority, 1, 50, 25), p.tagNames(s, ix.Tags)
	cats := categorySplit(ix.Categories)
	in.Categories = &cats
	generic := strings.EqualFold(ix.Implementation, "Torznab") || strings.EqualFold(ix.Implementation, "Newznab")
	if generic && str(ix.Fields, "baseUrl") != "" {
		in.BaseURL = strings.TrimRight(str(ix.Fields, "baseUrl"), "/")
		in.APIPath = firstNonEmpty(str(ix.Fields, "apiPath"), "/api")
		if k := str(ix.Fields, "apiKey"); masked(k) {
			in.Enabled = false
			it.attn(AttnSecret)
			it.note("the API key is hidden by Prowlarr: enter it and enable the indexer")
		} else if k != "" {
			in.APIKey = &k
		}
	} else {
		in.BaseURL = strings.TrimRight(s.URL, "/") + "/" + strconv.FormatInt(ix.ID, 10)
		in.APIPath = "/api"
		k := prowlarrKey
		in.APIKey = &k
		it.note("searched through Prowlarr")
	}
	p.finishIndexer(s, it, in, strconv.FormatInt(ix.ID, 10))
}

func clamp(v, lo, hi, def int) int {
	if v < lo || v > hi {
		if v == 0 {
			return def
		}
		return max(lo, min(v, hi))
	}
	return v
}

func (p *planner) finishIndexer(s *Snapshot, it *Item, in indexers.Input, ext string) {
	exists := func(id int64) bool {
		for _, x := range p.indexerList {
			if x.ID == id {
				return true
			}
		}
		return false
	}
	byName := func(n string) (int64, bool) {
		for _, x := range p.indexerList {
			if strings.EqualFold(x.Name, n) {
				return x.ID, true
			}
		}
		return 0, false
	}
	h := hashOf([]any{in.Name, in.Kind, in.BaseURL, in.APIPath, in.Enabled, in.Priority, in.Tags, in.Categories, in.EnableRSS,
		in.EnableAutomatic, in.EnableInteractive, in.MinimumSeeders, in.APIKey != nil})
	d := p.decideNamed(it, s, "indexer", ext, in.Name, h, exists, byName)
	key := it.Key
	if d.targetID > 0 {
		p.setID(key, d.targetID)
	}
	if d.action != ActionSkip {
		in.Name = d.name
		target := d.targetID
		it.run = func(ctx context.Context) (string, error) {
			var saved indexers.Indexer
			var err error
			if target > 0 {
				saved, err = p.l.Indexers.Update(ctx, target, in)
			} else {
				saved, err = p.l.Indexers.Create(ctx, in)
			}
			if err != nil {
				return "", err
			}
			p.setID(key, saved.ID)
			return fmt.Sprintf("indexer:%d", saved.ID), nil
		}
	}
	p.add(it)
}

// planIndexers plans the *arr indexers: the same feed in several apps
// becomes one indexer (categories merged); feeds served by a Prowlarr being
// imported are left to it.
func (p *planner) planIndexers(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.Settings {
		return
	}
	for _, ix := range s.Indexers {
		key := idKey(s.SourceID, "indexer", ix.ID)
		it := &Item{Key: key, SourceID: s.SourceID, Category: "indexer", Name: ix.Name}
		impl := strings.ToLower(ix.Implementation)
		if impl != "torznab" && impl != "newznab" {
			it.Action, it.Reason = ActionSkip, "unsupported_implementation"
			it.note("%s indexers are not supported; add them through Prowlarr or Jackett", ix.Implementation)
			it.attn(AttnUnsupported)
			p.add(it)
			continue
		}
		base := strings.TrimRight(str(ix.Fields, "baseUrl"), "/")
		if u, err := url.Parse(base); err == nil {
			port, _ := strconv.Atoi(u.Port())
			if port == 0 {
				port = 80
				if u.Scheme == "https" {
					port = 443
				}
			}
			if pid, ok := p.prowlarrHosts[strings.ToLower(u.Hostname())+":"+strconv.Itoa(port)]; ok {
				it.Action, it.Reason = ActionSkip, "provided_by_prowlarr"
				it.note("comes from Prowlarr, which is imported too")
				_ = pid
				p.add(it)
				continue
			}
		}
		kind := indexers.Torznab
		if impl == "newznab" {
			kind = indexers.Newznab
		}
		in := indexers.DefaultInput()
		in.Name, in.Kind, in.BaseURL, in.APIPath = ix.Name, kind, base, firstNonEmpty(str(ix.Fields, "apiPath"), "/api")
		in.Enabled = ix.EnableRSS || ix.EnableAutomatic || ix.EnableInteractive
		in.EnableRSS, in.EnableAutomatic, in.EnableInteractive = ix.EnableRSS, ix.EnableAutomatic, ix.EnableInteractive
		in.Priority, in.Tags = clamp(ix.Priority, 1, 50, 25), p.tagNames(s, ix.Tags)
		if n := int(num(ix.Fields, "minimumSeeders")); n > 0 {
			in.MinimumSeeders = n
		}
		if r := num(ix.Fields, "seedCriteria.seedRatio"); r > 0 {
			in.SeedRatio = &r
		}
		if t := int(num(ix.Fields, "seedCriteria.seedTime")); t > 0 {
			in.SeedTimeMinutes = &t
		}
		var cats indexers.Categories
		list := mergeInts(ints(ix.Fields, "categories"), ints(ix.Fields, "animeCategories"))
		switch s.Kind {
		case Radarr:
			cats.Movies = list
		case Sonarr:
			cats.TV = list
		case Lidarr:
			cats.Music = list
		}
		in.Categories = &cats
		if k := str(ix.Fields, "apiKey"); masked(k) {
			in.Enabled = false
			it.attn(AttnSecret)
			it.note("the API key is hidden by %s: enter it and enable the indexer", s.Kind.Label())
		} else if k != "" {
			in.APIKey = &k
		}
		// The same feed from an earlier source: one indexer with both categories.
		if prev := p.sameFeed(base, in.APIPath); prev != nil {
			it.Action, it.Reason = ActionUpdate, "merged"
			it.note("the same indexer as %s from another application: its categories are added", prev.Name)
			prevKey := prev.Key
			add := cats
			it.run = func(ctx context.Context) (string, error) {
				id := p.id(prevKey)
				if id == 0 {
					return "", nil
				}
				cur, err := p.l.Indexers.Get(ctx, id)
				if err != nil {
					return "", err
				}
				c := cur.Categories
				c.Movies, c.TV, c.Music = mergeInts(c.Movies, add.Movies), mergeInts(c.TV, add.TV), mergeInts(c.Music, add.Music)
				upd := inputOf(cur)
				upd.Categories = &c
				if _, err := p.l.Indexers.Update(ctx, id, upd); err != nil {
					return "", err
				}
				p.setID(key, id)
				return fmt.Sprintf("indexer:%d", id), nil
			}
			p.mergedUnchanged(it, s, "indexer", strconv.FormatInt(ix.ID, 10), hashOf(add))
			p.add(it)
			continue
		}
		p.feeds = append(p.feeds, feed{base: base, apiPath: in.APIPath, item: it})
		p.finishIndexer(s, it, in, strconv.FormatInt(ix.ID, 10))
	}
}

type feed struct {
	base, apiPath string
	item          *Item
}

func (p *planner) sameFeed(base, apiPath string) *Item {
	for _, f := range p.feeds {
		if strings.EqualFold(f.base, base) && f.apiPath == apiPath && (f.item.Action != ActionSkip || f.item.Reason == "unchanged") {
			return f.item
		}
	}
	return nil
}

// inputOf turns a stored indexer back into an update (secrets unchanged).
func inputOf(x indexers.Indexer) indexers.Input {
	c := x.Categories
	return indexers.Input{Name: x.Name, Kind: x.Kind, BaseURL: x.BaseURL, APIPath: x.APIPath, Enabled: x.Enabled, EnableRSS: x.EnableRSS,
		EnableAutomatic: x.EnableAutomatic, EnableInteractive: x.EnableInteractive, Priority: x.Priority, Tags: x.Tags, Categories: &c,
		MinimumSeeders: x.MinimumSeeders, SeedRatio: x.SeedRatio, SeedTimeMinutes: x.SeedTimeMinutes, RequestLimit: x.RequestLimit,
		RequestPeriodSec: x.RequestPeriodSec, GrabLimitPerDay: x.GrabLimitPerDay, TimeoutSeconds: x.TimeoutSeconds,
		ProxyURL: x.ProxyURL, ProxyUsername: x.ProxyUsername, FlareSolverrURL: x.FlareSolverrURL}
}

// ---- download clients ----

type plannedClient struct {
	impl, host string
	port       int
	item       *Item
	input      *downloads.ClientInput
}

func (p *planner) findPlannedClient(impl, host string, port int) *plannedClient {
	for _, c := range p.clients {
		if c.impl == impl && sameEndpoint(c.host, c.port, host, port) {
			return c
		}
	}
	return nil
}

// planDownloadClients plans the clients read as sources (qBittorrent,
// SABnzbd, with the credentials the person entered) and the *arr clients
// (secrets are hidden by the applications: a matching source supplies them,
// else the client needs them entered). The same client in several
// applications becomes one client with each application's category.
func (p *planner) planDownloadClients(s *Snapshot, so SourceOptions) {
	if !so.Settings {
		return
	}
	if (s.Kind == QBittorrent || s.Kind == SABnzbd) && s.Client != nil {
		c := s.Client
		in := downloads.ClientInput{Name: s.Name, Implementation: c.Implementation, Enabled: true, Priority: 1, Host: c.Host, Port: c.Port,
			UseTLS: c.UseTLS, URLBase: c.URLBase, Username: c.Username}
		secret := p.secrets[s.SourceID]
		if s.Kind == QBittorrent {
			in.Password = &secret
		} else {
			in.APIKey = &secret
		}
		for _, cat := range c.Categories {
			switch categoryKind(cat.Name) {
			case quality.Movies:
				if in.CategoryMovies == "" {
					in.CategoryMovies = cat.Name
				}
			case quality.Series:
				if in.CategorySeries == "" {
					in.CategorySeries = cat.Name
				}
			case quality.Music:
				if in.CategoryMusic == "" {
					in.CategoryMusic = cat.Name
				}
			}
		}
		it := &Item{Key: idKey(s.SourceID, "downloadclient", "self"), SourceID: s.SourceID, Category: "downloadclient",
			Name: fmt.Sprintf("%s (%s:%d)", s.Name, c.Host, c.Port)}
		pc := &plannedClient{impl: c.Implementation, host: c.Host, port: c.Port, item: it, input: &in}
		p.clients = append(p.clients, pc)
		p.finishClient(s, it, pc, "self")
		return
	}
	if !isArr(s.Kind) {
		return
	}
	for _, dc := range s.DownloadClients {
		key := idKey(s.SourceID, "downloadclient", dc.ID)
		it := &Item{Key: key, SourceID: s.SourceID, Category: "downloadclient", Name: dc.Name}
		impl := MapClientImplementation(dc.Implementation)
		if impl == "" {
			it.Action, it.Reason = ActionSkip, "unsupported_implementation"
			it.note("%s is not a client Locarr supports", dc.Implementation)
			it.attn(AttnUnsupported)
			p.add(it)
			continue
		}
		f := dc.Fields
		host, port := str(f, "host"), int(num(f, "port"))
		cat := firstNonEmpty(str(f, "movieCategory"), str(f, "tvCategory"), str(f, "musicCategory"), str(f, "category"))
		if prev := p.findPlannedClient(impl, host, port); prev != nil && prev.item.Action == ActionSkip && prev.item.Reason != "unchanged" &&
			p.id(prev.item.Key) == 0 {
			it.Action, it.Reason = ActionSkip, prev.item.Reason
			it.note("the same client as %s, which is not added", prev.item.Name)
			it.attn(prev.item.Attention...)
			p.add(it)
			continue
		}
		if prev := p.findPlannedClient(impl, host, port); prev != nil {
			// One client, one category per application (set on the client the
			// earlier application created).
			it.Action, it.Reason = ActionUpdate, "merged"
			it.note("the same client as %s: its %s category is %q", prev.item.Name, kindOf(s.Kind), cat)
			prevKey, kind := prev.item.Key, kindOf(s.Kind)
			it.run = func(ctx context.Context) (string, error) {
				id := p.id(prevKey)
				if id == 0 {
					return "", fmt.Errorf("the client it merges into was not added")
				}
				cur, err := p.l.Downloads.GetClient(ctx, id)
				if err != nil {
					return "", err
				}
				upd := clientInputOf(cur)
				setCategory(&upd, kind, cat)
				if _, err := p.l.Downloads.UpdateClient(ctx, id, upd); err != nil {
					return "", err
				}
				p.setID(key, id)
				return fmt.Sprintf("downloadclient:%d", id), nil
			}
			p.mergedUnchanged(it, s, "downloadclient", strconv.FormatInt(dc.ID, 10), hashOf([]any{kind, cat}))
			p.add(it)
			continue
		}
		in := downloads.ClientInput{Name: dc.Name, Implementation: impl, Enabled: dc.Enable, Priority: clamp(dc.Priority, 1, 50, 1),
			Host: host, Port: port, UseTLS: boolean(f, "useSsl"), URLBase: str(f, "urlBase"), Username: str(f, "username"),
			Tags: p.tagNames(s, dc.Tags), Directory: firstNonEmpty(str(f, "movieDirectory"), str(f, "tvDirectory"), str(f, "musicDirectory"))}
		setCategory(&in, kindOf(s.Kind), cat)
		if pw := str(f, "password"); pw != "" {
			if masked(pw) {
				it.attn(AttnSecret)
			} else {
				in.Password = &pw
			}
		}
		if k := str(f, "apiKey"); k != "" {
			if masked(k) {
				it.attn(AttnSecret)
			} else {
				in.APIKey = &k
			}
		}
		if contains(it.Attention, AttnSecret) {
			it.note("the password or API key is hidden by %s: enter it in Download clients", s.Kind.Label())
			in.Enabled = false
		}
		pc := &plannedClient{impl: impl, host: host, port: port, item: it, input: &in}
		p.clients = append(p.clients, pc)
		p.finishClient(s, it, pc, strconv.FormatInt(dc.ID, 10))
	}
}

func setCategory(in *downloads.ClientInput, k quality.Kind, cat string) {
	switch k {
	case quality.Movies:
		in.CategoryMovies = cat
	case quality.Series:
		in.CategorySeries = cat
	case quality.Music:
		in.CategoryMusic = cat
	}
}

func (p *planner) finishClient(s *Snapshot, it *Item, pc *plannedClient, ext string) {
	in := pc.input
	exists := func(id int64) bool {
		for _, x := range p.clientList {
			if x.ID == id {
				return true
			}
		}
		return false
	}
	// The same client is the same address, whatever it is called.
	byName := func(string) (int64, bool) {
		for _, x := range p.clientList {
			if x.Implementation == in.Implementation && sameEndpoint(x.Host, x.Port, in.Host, in.Port) {
				return x.ID, true
			}
		}
		return 0, false
	}
	h := hashOf([]any{in.Name, in.Implementation, in.Host, in.Port, in.UseTLS, in.URLBase, in.Username, in.CategoryMovies,
		in.CategorySeries, in.CategoryMusic, in.Password != nil, in.APIKey != nil, in.Enabled})
	d := p.decideNamed(it, s, "downloadclient", ext, in.Name, h, exists, byName)
	key := it.Key
	if d.targetID > 0 {
		p.setID(key, d.targetID)
	}
	if d.action == ActionSkip {
		p.add(it)
		return
	}
	impl, required := clientRequires(in.Implementation)
	missing := (required == "password" && in.Password == nil) || (required == "apiKey" && in.APIKey == nil)
	if missing && d.targetID == 0 {
		it.Action, it.Reason = ActionSkip, "needs_secret"
		it.note("%s cannot be added without its %s: add it in Download clients", impl, required)
		it.attn(AttnSecret)
		p.add(it)
		return
	}
	in.Name = d.name
	if d.action == ActionCreate && p.clientNameTaken(in.Name, it) {
		in.Name = fmt.Sprintf("%s (%s)", in.Name, s.Name)
		it.Name = in.Name
		it.note("another client already has its name")
	}
	target := d.targetID
	it.run = func(ctx context.Context) (string, error) {
		// A credential from a client read as a source fills hidden ones.
		if sc := p.sourceCredential(in); sc != nil {
			if in.Password == nil && sc.Password != nil {
				in.Password = sc.Password
			}
			if in.APIKey == nil && sc.APIKey != nil {
				in.APIKey = sc.APIKey
			}
		}
		var saved downloads.Client
		var err error
		if target > 0 {
			cur, gerr := p.l.Downloads.GetClient(ctx, target)
			if gerr != nil {
				return "", gerr
			}
			upd := *in
			upd.Enabled = upd.Enabled || cur.Enabled
			if upd.CategoryMovies == "" {
				upd.CategoryMovies = cur.CategoryMovies
			}
			if upd.CategorySeries == "" {
				upd.CategorySeries = cur.CategorySeries
			}
			if upd.CategoryMusic == "" {
				upd.CategoryMusic = cur.CategoryMusic
			}
			saved, err = p.l.Downloads.UpdateClient(ctx, target, upd)
		} else {
			saved, err = p.l.Downloads.CreateClient(ctx, *in)
		}
		if err != nil {
			return "", err
		}
		p.setID(key, saved.ID)
		return fmt.Sprintf("downloadclient:%d", saved.ID), nil
	}
	p.add(it)
}

func clientRequires(impl string) (string, string) {
	switch impl {
	case "sabnzbd":
		return "SABnzbd", "apiKey"
	case "deluge":
		return "Deluge", "password"
	}
	return impl, ""
}

// sourceCredential is the input of a client read as a source at the same address.
func (p *planner) sourceCredential(in *downloads.ClientInput) *downloads.ClientInput {
	for _, c := range p.clients {
		if c.input != in && strings.HasSuffix(c.item.Key, ":downloadclient:self") && c.impl == in.Implementation &&
			sameEndpoint(c.host, c.port, in.Host, in.Port) {
			return c.input
		}
	}
	return nil
}

// ---- remote path mappings ----

func (p *planner) planPathMappings(s *Snapshot, so SourceOptions) {
	if !so.Settings {
		return
	}
	existing, _ := p.l.Downloads.Mappings(p.ctx)
	add := func(ext, host, remote, sourceLocal, why string) {
		key := idKey(s.SourceID, "pathmapping", ext)
		it := &Item{Key: key, SourceID: s.SourceID, Category: "pathmapping", Name: fmt.Sprintf("%s: %s", host, remote)}
		local, ok := p.tr.Translate(s.SourceID, sourceLocal)
		if !ok {
			it.Action, it.Reason = ActionSkip, "outside_data_roots"
			it.note("%s is outside the folders Locarr may use; add a path mapping first", local)
			it.attn(AttnPath)
			p.add(it)
			return
		}
		it.Name += " → " + local
		if why != "" {
			it.note("%s", why)
		}
		for _, m := range existing {
			if strings.EqualFold(m.Host, host) && strings.TrimRight(m.RemotePath, `/\`) == strings.TrimRight(remote, `/\`) {
				it.Action, it.Reason, it.LocalRef = ActionSkip, "exists", fmt.Sprintf("pathmapping:%d", m.ID)
				p.add(it)
				return
			}
		}
		for _, x := range p.plan.Items {
			if x.Category == "pathmapping" && x.Action == ActionCreate && strings.HasPrefix(x.Name, host+": "+remote+" →") {
				it.Action, it.Reason = ActionSkip, "duplicate"
				p.add(it)
				return
			}
		}
		it.Action = ActionCreate
		m := downloads.Mapping{Host: host, RemotePath: remote, LocalPath: local}
		it.run = func(ctx context.Context) (string, error) {
			saved, err := p.l.Downloads.CreateMapping(ctx, m)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("pathmapping:%d", saved.ID), nil
		}
		p.add(it)
	}
	if isArr(s.Kind) {
		for i, m := range s.PathMappings {
			add(strconv.Itoa(i), m.Host, m.RemotePath, m.LocalPath, "")
		}
	}
	// Hints from a client read as a source: a category folder the client
	// sees under another name than Locarr does.
	if s.Client != nil {
		for _, cat := range s.Client.Categories {
			if cat.SavePath == "" {
				continue
			}
			raw := normSourcePath(cat.SavePath)
			local, ok := p.tr.Translate(s.SourceID, raw)
			if !ok || local == raw {
				continue
			}
			add("cat:"+cat.Name, s.Client.Host, raw, raw, fmt.Sprintf("category %s saves to %s as the client sees it", cat.Name, raw))
		}
	}
}

// ---- import lists, collections ----

func (p *planner) planImportLists(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.Settings {
		return
	}
	if s.Kind != Radarr {
		for _, l := range s.ImportLists {
			p.skip(s, "importlist", strconv.FormatInt(l.ID, 10), l.Name, "unsupported_kind",
				"import lists for "+string(kindOf(s.Kind))+" are not available in Locarr yet", AttnUnsupported)
		}
		return
	}
	existing, _ := p.l.Movies.Lists(p.ctx)
	type want struct {
		ext, name, kind, listID, root string
		profile                       int64
		avail                         string
		monitored, search, enabled    bool
		tags                          []int64
	}
	var wants []want
	for _, l := range s.ImportLists {
		w := want{ext: strconv.FormatInt(l.ID, 10), name: l.Name, root: l.RootFolderPath, profile: l.QualityProfileID,
			avail: l.MinimumAvailability, monitored: l.Monitored, search: l.SearchOnAdd, enabled: l.Enabled, tags: l.Tags}
		switch strings.ToLower(l.Implementation) {
		case "tmdblistimport":
			w.kind, w.listID = movies.ListTMDbList, str(l.Fields, "listId")
		case "tmdbcollectionimport":
			w.kind, w.listID = movies.ListTMDbCollection, str(l.Fields, "collectionId")
		default:
			p.skip(s, "importlist", w.ext, l.Name, "unsupported_implementation", l.Implementation+" lists are not supported (TMDb lists and collections are)",
				AttnUnsupported)
			continue
		}
		wants = append(wants, w)
	}
	for _, c := range s.Collections {
		if c.Monitored {
			wants = append(wants, want{ext: "collection:" + strconv.FormatInt(c.TMDbID, 10), name: c.Title + " (collection)",
				kind: movies.ListTMDbCollection, listID: strconv.FormatInt(c.TMDbID, 10), root: c.RootFolderPath, profile: c.QualityProfileID,
				avail: c.MinimumAvailability, monitored: true, search: c.SearchOnAdd, enabled: true, tags: c.Tags})
		}
	}
	for _, w := range wants {
		it := &Item{Key: idKey(s.SourceID, "importlist", w.ext), SourceID: s.SourceID, Category: "importlist", Name: w.name}
		local, ok := p.tr.Translate(s.SourceID, w.root)
		if !ok || !p.rootPlannedExact(local, "movies") {
			it.Action, it.Reason = ActionSkip, "no_root_folder"
			it.note("its folder %s is not a Locarr root folder", w.root)
			it.attn(AttnPath)
			p.add(it)
			continue
		}
		l := movies.ImportList{Name: w.name, Kind: w.kind, ListID: w.listID, Enabled: w.enabled, MinimumAvailability: MapAvailability(w.avail),
			Monitored: w.monitored, SearchOnAdd: w.search, Tags: p.tagNames(s, w.tags)}
		exists := func(id int64) bool {
			for _, x := range existing {
				if x.ID == id {
					return true
				}
			}
			return false
		}
		byName := func(string) (int64, bool) {
			for _, x := range existing {
				if x.Kind == w.kind && x.ListID == w.listID {
					return x.ID, true
				}
			}
			return 0, false
		}
		d := p.decideNamed(it, s, "importlist", w.ext, w.name, hashOf([]any{l, w.root, w.profile}), exists, byName)
		if d.action != ActionSkip {
			l.Name = d.name
			target, sid, prof := d.targetID, s.SourceID, w.profile
			it.run = func(ctx context.Context) (string, error) {
				root, ok := p.rootFor(ctx, local+"/x", "movies")
				if !ok {
					return "", fmt.Errorf("no root folder for %s", local)
				}
				l.ID, l.RootFolderID = target, root.ID
				l.QualityProfileID, _ = p.profileFor(ctx, sid, prof, quality.Movies)
				saved, err := p.l.Movies.SaveList(ctx, l)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("importlist:%d", saved.ID), nil
			}
		}
		p.add(it)
	}
}

// rootPlannedExact reports whether local is (or will be) a root folder of kind.
func (p *planner) rootPlannedExact(local, kind string) bool {
	for _, f := range p.roots {
		if f.Path == local && f.Kind == kind {
			return true
		}
	}
	return p.plannedRoots[local] == kind || p.rootPlanned(local+"/x", kind)
}

// ---- exclusions ----

func (p *planner) planExclusions(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.Settings || len(s.Exclusions) == 0 {
		return
	}
	it := &Item{Key: idKey(s.SourceID, "exclusions", "all"), SourceID: s.SourceID, Category: "exclusions",
		Name: fmt.Sprintf("%d list exclusions", len(s.Exclusions))}
	if s.Kind != Radarr {
		it.Action, it.Reason = ActionSkip, "unsupported_kind"
		it.note("exclusions for %s are not available in Locarr yet", kindOf(s.Kind))
		p.add(it)
		return
	}
	list := append([]Exclusion(nil), s.Exclusions...)
	d := p.decideNamed(it, s, "exclusions", "all", it.Name, hashOf(list), func(int64) bool { return true },
		func(string) (int64, bool) { return 0, false })
	if d.action != ActionSkip {
		it.run = func(ctx context.Context) (string, error) {
			n := 0
			for _, e := range list {
				added, err := p.l.Movies.AddExclusion(ctx, movies.Exclusion{TMDbID: e.TMDbID, Title: e.Title, Year: e.Year})
				if err != nil {
					return "", err
				}
				if added {
					n++
				}
			}
			return fmt.Sprintf("exclusions:%d", n), nil
		}
	}
	p.add(it)
}

// clientInputOf turns a stored client back into an update (secrets unchanged).
func clientInputOf(c downloads.Client) downloads.ClientInput {
	return downloads.ClientInput{Name: c.Name, Implementation: c.Implementation, Enabled: c.Enabled, Priority: c.Priority, Host: c.Host,
		Port: c.Port, UseTLS: c.UseTLS, AllowSelfSigned: c.AllowSelfSigned, URLBase: c.URLBase, Username: c.Username, Directory: c.Directory,
		CategoryMovies: c.CategoryMovies, CategorySeries: c.CategorySeries, CategoryMusic: c.CategoryMusic, RecentPriority: c.RecentPriority,
		OlderPriority: c.OlderPriority, InitialState: c.InitialState, RemoveCompleted: c.RemoveCompleted, Sequential: c.Sequential,
		FirstLastPiece: c.FirstLastPiece, Tags: c.Tags}
}

func (p *planner) clientNameTaken(name string, self *Item) bool {
	for _, x := range p.clientList {
		if strings.EqualFold(x.Name, name) {
			return true
		}
	}
	for _, c := range p.clients {
		if c.item != self && c.item.Action == ActionCreate && strings.EqualFold(c.input.Name, name) {
			return true
		}
	}
	return false
}
