package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/music"
	"github.com/mmzlopasa/locarr/server/internal/library/series"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/requests"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Locarr is what the importer writes through: the services of every area
// (never their tables directly) plus read access for the plan.
type Locarr struct {
	DB        *db.DB
	Box       *secrets.Box
	Auth      *auth.Service
	Settings  *settings.Store
	Roots     *rootfolders.Service
	Quality   *quality.Store
	Formats   *customformats.Store
	Downloads *downloads.Service
	Indexers  *indexers.Service
	Movies    *movies.Service
	Series    *series.Service
	Music     *music.Service
	Requests  *requests.Service
	// Subtitles is set once the subtitle area exists (nil = subtitle
	// settings are planned and reported but not applied).
	Subtitles SubtitleTarget
	DataRoots []string
}

// SubtitleTarget is how subtitle settings are imported (the subtitle area
// implements it in the app wiring).
type SubtitleTarget interface {
	// Profiles returns the existing subtitle profiles by lower-cased name.
	Profiles(ctx context.Context) (map[string]int64, error)
	// SaveProfile creates (id 0) or replaces a profile.
	SaveProfile(ctx context.Context, id int64, p SubtitleProfile) (int64, error)
	// Provider reports whether Locarr has a provider of this name (and its own name).
	Provider(name string) (string, bool)
	// EnableProvider turns a provider on with the settings found (empty = keep).
	EnableProvider(ctx context.Context, name string, fields map[string]string) error
	// Assign gives library items a profile.
	Assign(ctx context.Context, itemIDs []int64, profileID int64) (int, error)
}

// Actions of plan items.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionSkip   = "skip"
)

// Conflict resolutions.
const (
	ResolveMerge  = "merge"
	ResolveRename = "rename"
	ResolveSkip   = "skip"
)

// Attention codes: what a person should look at after the import.
const (
	AttnSecret          = "needs_secret"
	AttnPassword        = "needs_password"
	AttnFileMissing     = "file_not_found"
	AttnUnknownCond     = "unknown_condition"
	AttnPath            = "path_problem"
	AttnProfileFallback = "profile_fallback"
	AttnUnsupported     = "unsupported"
	AttnRefresh         = "metadata_pending"
)

// Conflict is a name already used in Locarr by something not imported from
// this source.
type Conflict struct {
	Existing string   `json:"existing"`
	Options  []string `json:"options"`
	Chosen   string   `json:"chosen"`
}

// Item is one line of the plan.
type Item struct {
	Seq       int       `json:"seq"`
	Key       string    `json:"key"`
	SourceID  int64     `json:"sourceId"`
	Category  string    `json:"category"`
	Name      string    `json:"name"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason,omitempty"`
	Message   string    `json:"message,omitempty"`
	Conflict  *Conflict `json:"conflict,omitempty"`
	Attention []string  `json:"attention,omitempty"`
	Status    string    `json:"status"`
	LocalRef  string    `json:"localRef,omitempty"`
	Error     string    `json:"error,omitempty"`

	run   func(ctx context.Context) (string, error)
	batch string
	data  any
	link  *linkInfo
}

func (it *Item) attn(codes ...string) {
	for _, c := range codes {
		if !contains(it.Attention, c) {
			it.Attention = append(it.Attention, c)
		}
	}
}

func (it *Item) note(format string, a ...any) {
	m := fmt.Sprintf(format, a...)
	if it.Message == "" {
		it.Message = m
	} else {
		it.Message += "; " + m
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

type linkInfo struct {
	sourceKey, entity, external, hash string
}

// SourceOptions say what to import from one source.
type SourceOptions struct {
	ID       int64 `json:"id"`
	Settings bool  `json:"settings"`
	Library  bool  `json:"library"`
	Users    bool  `json:"users"`
	History  bool  `json:"history"`
	// ProwlarrMode: "source" keeps Prowlarr as an indexer source (synced),
	// "direct" adds each of its indexers on its own.
	ProwlarrMode string `json:"prowlarrMode,omitempty"`
}

// Options are the choices of a run.
type Options struct {
	Sources      []SourceOptions   `json:"sources"`
	PathMappings []PathMapping     `json:"pathMappings"`
	Resolutions  map[string]string `json:"resolutions"`
	HistoryDays  int               `json:"historyDays"`
	// ActorID is the administrator running the import (owner of requests
	// whose user was not imported).
	ActorID int64 `json:"actorId"`
}

func (o *Options) source(id int64) SourceOptions {
	for _, s := range o.Sources {
		if s.ID == id {
			return s
		}
	}
	return SourceOptions{ID: id}
}

// Counts sums a plan up by category and action.
type Counts map[string]map[string]int

// Plan is the result of planning.
type Plan struct {
	Items    []*Item  `json:"items"`
	Warnings []string `json:"warnings"`
}

// Counts returns per-category action counts.
func (p *Plan) Counts() Counts {
	c := Counts{}
	for _, it := range p.Items {
		if c[it.Category] == nil {
			c[it.Category] = map[string]int{}
		}
		c[it.Category][it.Action]++
	}
	return c
}

// planner builds a plan from snapshots and the current Locarr state.
type planner struct {
	ctx   context.Context
	l     *Locarr
	opts  Options
	snaps []*Snapshot
	tr    Translator
	plan  *Plan
	links map[string]link

	mu  sync.Mutex
	ids map[string]int64

	// Local state read once.
	roots         []rootfolders.Folder
	plannedRoots  map[string]string // local path → kind
	profiles      []quality.Profile
	formats       []customformats.Format
	formatsByName map[string]plannedName
	metaProfiles  []music.MetadataProfile
	users         []auth.User
	indexerList   []indexers.Indexer
	clientList    []downloads.Client
	prowlarrHosts map[string]int64
	plannedNames  map[string]plannedName
	feeds         []feed
	clients       []*plannedClient
	// secrets are the sources' own API keys or passwords (by source id).
	secrets map[int64]string
	local   *localIndex
	// refresh lists the library items created (their metadata refresh runs later).
	refresh []string
}

type plannedName struct {
	hash   string
	key    string
	source string
}

type link struct {
	localRef string
	hash     string
}

func (p *planner) setID(k string, id int64) {
	p.mu.Lock()
	p.ids[k] = id
	p.mu.Unlock()
}

func (p *planner) id(k string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ids[k]
}

func idKey(sourceID int64, entity string, ext any) string {
	return fmt.Sprintf("%d:%s:%v", sourceID, entity, ext)
}

func hashOf(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// refID reads the id of a local ref ("qualityprofile:12" → 12).
func refID(ref string) int64 {
	i := strings.LastIndex(ref, ":")
	if i < 0 {
		return 0
	}
	n, _ := strconv.ParseInt(ref[i+1:], 10, 64)
	return n
}

func (p *planner) add(it *Item) *Item {
	it.Seq = len(p.plan.Items) + 1
	if it.Status == "" {
		it.Status = "planned"
	}
	p.plan.Items = append(p.plan.Items, it)
	return it
}

func (p *planner) skip(s *Snapshot, category, ext, name, reason, msg string, attention ...string) *Item {
	it := &Item{Key: idKey(s.SourceID, category, ext), SourceID: s.SourceID, Category: category, Name: name, Action: ActionSkip,
		Reason: reason, Message: msg}
	it.attn(attention...)
	return p.add(it)
}

func (p *planner) loadLinks() error {
	p.links = map[string]link{}
	keys := map[string]bool{}
	for _, s := range p.snaps {
		keys[s.Key] = true
	}
	rows, err := p.l.DB.R.QueryContext(p.ctx, `SELECT source_key, entity, external_id, local_ref, hash FROM migrate_links`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sk, ent, ext, ref, h string
		if err := rows.Scan(&sk, &ent, &ext, &ref, &h); err != nil {
			return err
		}
		if keys[sk] {
			p.links[sk+"|"+ent+"|"+ext] = link{localRef: ref, hash: h}
		}
	}
	return rows.Err()
}

func (p *planner) linkOf(s *Snapshot, entity, ext string) (link, bool) {
	l, ok := p.links[s.Key+"|"+entity+"|"+ext]
	return l, ok
}

// decision is how a named setting is handled.
type decision struct {
	action   string
	targetID int64
	name     string
}

// decideNamed handles a setting that has a name: unchanged or updated when
// it was imported before (by link), a conflict when the name is taken by
// something else, otherwise created. exists says whether a local id still
// exists; byName finds a local one by name.
func (p *planner) decideNamed(it *Item, s *Snapshot, entity, ext, name, hash string, exists func(int64) bool,
	byName func(string) (int64, bool)) decision {
	it.link = &linkInfo{sourceKey: s.Key, entity: entity, external: ext, hash: hash}
	if l, ok := p.linkOf(s, entity, ext); ok {
		if id := refID(l.localRef); id > 0 && exists(id) {
			if l.hash == hash {
				it.Action, it.Reason = ActionSkip, "unchanged"
				it.LocalRef = l.localRef
				return decision{action: ActionSkip, targetID: id, name: name}
			}
			it.Action, it.Reason = ActionUpdate, "imported_before"
			return decision{action: ActionUpdate, targetID: id, name: name}
		}
	}
	// The same name planned from another source in this run.
	pk := entity + "|" + strings.ToLower(name)
	if prev, ok := p.plannedNames[pk]; ok && prev.key != it.Key {
		if prev.hash == hash {
			it.Action, it.Reason = ActionSkip, "duplicate"
			it.note("the same as the one from %s", prev.source)
			return decision{action: ActionSkip, name: name, targetID: -1}
		}
		return p.conflict(it, s, name, prev.source+" ("+name+")", 0, ResolveRename)
	}
	if id, ok := byName(name); ok {
		d := p.conflict(it, s, name, name, id, ResolveMerge)
		if d.action != ActionSkip {
			p.plannedNames[pk] = plannedName{hash: hash, key: it.Key, source: s.Name}
		}
		return d
	}
	p.plannedNames[pk] = plannedName{hash: hash, key: it.Key, source: s.Name}
	it.Action = ActionCreate
	return decision{action: ActionCreate, name: name}
}

func (p *planner) conflict(it *Item, s *Snapshot, name, existing string, id int64, def string) decision {
	opts := []string{ResolveMerge, ResolveRename, ResolveSkip}
	if id == 0 {
		opts = []string{ResolveRename, ResolveSkip}
	}
	chosen := def
	if c, ok := p.opts.Resolutions[it.Key]; ok && contains(opts, c) {
		chosen = c
	}
	it.Conflict = &Conflict{Existing: existing, Options: opts, Chosen: chosen}
	switch chosen {
	case ResolveMerge:
		it.Action, it.Reason = ActionUpdate, "name_exists"
		return decision{action: ActionUpdate, targetID: id, name: name}
	case ResolveRename:
		newName := fmt.Sprintf("%s (%s)", name, s.Name)
		it.Action, it.Reason, it.Name = ActionCreate, "renamed", newName
		return decision{action: ActionCreate, name: newName}
	}
	it.Action, it.Reason = ActionSkip, "conflict_skipped"
	return decision{action: ActionSkip, name: name, targetID: id}
}

func (p *planner) loadLocal() error {
	var err error
	ctx := p.ctx
	if p.roots, err = p.l.Roots.List(ctx); err != nil {
		return err
	}
	if p.profiles, err = p.l.Quality.Profiles(ctx); err != nil {
		return err
	}
	if p.formats, err = p.l.Formats.List(ctx); err != nil {
		return err
	}
	if p.l.Music != nil {
		if p.metaProfiles, err = p.l.Music.Profiles(ctx); err != nil {
			return err
		}
	}
	if p.users, err = p.l.Auth.Users(ctx); err != nil {
		return err
	}
	if p.indexerList, err = p.l.Indexers.List(ctx); err != nil {
		return err
	}
	if p.clientList, err = p.l.Downloads.Clients(ctx); err != nil {
		return err
	}
	return nil
}

// Build plans the import of snaps.
func Build(ctx context.Context, l *Locarr, opts Options, snaps []*Snapshot, secrets map[int64]string) (*Plan, *planner, error) {
	if secrets == nil {
		secrets = map[int64]string{}
	}
	p := &planner{ctx: ctx, l: l, opts: opts, snaps: snaps, plan: &Plan{}, ids: map[string]int64{}, secrets: secrets,
		tr: Translator{Mappings: opts.PathMappings, DataRoots: l.DataRoots}, plannedRoots: map[string]string{},
		plannedNames: map[string]plannedName{}, prowlarrHosts: map[string]int64{}}
	if err := p.loadLinks(); err != nil {
		return nil, nil, err
	}
	if err := p.loadLocal(); err != nil {
		return nil, nil, err
	}
	sort.SliceStable(p.snaps, func(i, j int) bool { return kindOrder(p.snaps[i].Kind) < kindOrder(p.snaps[j].Kind) })
	for _, s := range p.snaps {
		p.plan.Warnings = append(p.plan.Warnings, prefixed(s.Name, s.Warnings)...)
		if s.Kind == Prowlarr {
			host, port, _, _ := hostPort(s.URL)
			p.prowlarrHosts[strings.ToLower(host)+":"+strconv.Itoa(port)] = s.SourceID
		}
	}
	steps := []func(*Snapshot, SourceOptions){
		p.planRootFolders, p.planQualityDefinitions, p.planCustomFormats, p.planQualityProfiles, p.planMetadataProfiles,
		p.planDelayProfiles, p.planNaming, p.planProwlarr, p.planIndexers, p.planReleaseProfiles, p.planDownloadClients,
		p.planPathMappings, p.planImportLists, p.planExclusions,
	}
	for _, step := range steps {
		for _, s := range p.snaps {
			step(s, opts.source(s.SourceID))
		}
	}
	p.planMovies()
	p.planSeries()
	p.planArtists()
	for _, s := range p.snaps {
		so := opts.source(s.SourceID)
		p.planUsers(s, so)
	}
	for _, s := range p.snaps {
		so := opts.source(s.SourceID)
		p.planRequests(s, so)
		p.planIssues(s, so)
	}
	for _, s := range p.snaps {
		p.planSubtitles(s, opts.source(s.SourceID))
	}
	for _, s := range p.snaps {
		so := opts.source(s.SourceID)
		p.planBlocklist(s, so)
		p.planHistory(s, so)
	}
	return p.plan, p, nil
}

func prefixed(name string, list []string) []string {
	out := make([]string, 0, len(list))
	for _, w := range list {
		out = append(out, name+": "+w)
	}
	return out
}

// kindOrder: Prowlarr before the *arrs (its indexers win), download clients
// read as sources before the *arrs' client settings.
func kindOrder(k Kind) int {
	return map[Kind]int{Prowlarr: 0, QBittorrent: 1, SABnzbd: 2, Radarr: 3, Sonarr: 4, Lidarr: 5, Bazarr: 6, Overseerr: 7, Seerr: 8}[k]
}

func isArr(k Kind) bool { return k == Sonarr || k == Radarr || k == Lidarr }

// ---- root folders ----

func (p *planner) rootKind(k Kind) string {
	switch k {
	case Sonarr:
		return rootfolders.KindSeries
	case Lidarr:
		return rootfolders.KindMusic
	}
	return rootfolders.KindMovies
}

func (p *planner) planRootFolders(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !(so.Settings || so.Library) {
		return
	}
	kind := p.rootKind(s.Kind)
	for _, r := range s.RootFolders {
		local, ok := p.tr.Translate(s.SourceID, r.Path)
		it := &Item{Key: idKey(s.SourceID, "rootfolder", r.ID), SourceID: s.SourceID, Category: "rootfolder", Name: r.Path}
		switch {
		case !ok:
			it.Action, it.Reason = ActionSkip, "outside_data_roots"
			it.note("%s is outside the folders Locarr may use; add a path mapping", local)
			it.attn(AttnPath)
		case !dirExists(local):
			it.Action, it.Reason = ActionSkip, "path_not_found"
			it.note("%s does not exist here; add a path mapping", local)
			it.attn(AttnPath)
		default:
			it.Name = r.Path + " → " + local
			found := false
			for _, f := range p.roots {
				if f.Path == local {
					found = true
					if f.Kind != kind {
						it.Action, it.Reason = ActionSkip, "kind_mismatch"
						it.note("Locarr already uses %s for %s", local, f.Kind)
						it.attn(AttnPath)
					} else {
						it.Action, it.Reason, it.LocalRef = ActionSkip, "exists", fmt.Sprintf("rootfolder:%d", f.ID)
					}
				}
			}
			if !found {
				if k, planned := p.plannedRoots[local]; planned {
					it.Action, it.Reason = ActionSkip, "duplicate"
					if k != kind {
						it.Reason = "kind_mismatch"
						it.attn(AttnPath)
					}
				} else {
					p.plannedRoots[local] = kind
					it.Action = ActionCreate
					path := local
					it.run = func(ctx context.Context) (string, error) {
						f, err := p.l.Roots.Add(ctx, path, kind)
						if err != nil {
							return "", err
						}
						return fmt.Sprintf("rootfolder:%d", f.ID), nil
					}
				}
			}
		}
		p.add(it)
	}
}

// rootFor finds (at apply time) the root folder of kind containing path.
func (p *planner) rootFor(ctx context.Context, path, kind string) (rootfolders.Folder, bool) {
	list, err := p.l.Roots.List(ctx)
	if err != nil {
		return rootfolders.Folder{}, false
	}
	return pickRoot(list, path, kind)
}

func pickRoot(list []rootfolders.Folder, path, kind string) (rootfolders.Folder, bool) {
	var best rootfolders.Folder
	for _, f := range list {
		if f.Kind == kind && strings.HasPrefix(path, strings.TrimRight(f.Path, "/")+"/") && len(f.Path) > len(best.Path) {
			best = f
		}
	}
	return best, best.ID > 0
}

// rootPlanned reports at plan time whether a root folder will contain path.
func (p *planner) rootPlanned(path, kind string) bool {
	if _, ok := pickRoot(p.roots, path, kind); ok {
		return true
	}
	for r, k := range p.plannedRoots {
		if k == kind && strings.HasPrefix(path, strings.TrimRight(r, "/")+"/") {
			return true
		}
	}
	return false
}

// ---- quality definitions ----

func (p *planner) planQualityDefinitions(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.Settings || len(s.QualityDefinitions) == 0 {
		return
	}
	k := kindOf(s.Kind)
	var defs []quality.Definition
	var unknown []string
	for _, d := range s.QualityDefinitions {
		id := MapQuality(d.Quality, k)
		if id == "" {
			unknown = append(unknown, d.Quality)
			continue
		}
		def := quality.Definition{Kind: k, Quality: id, Min: d.Min}
		if d.Max != nil && *d.Max > 0 && *d.Max < 2000 {
			def.Max = *d.Max
		}
		if d.Preferred != nil && *d.Preferred > 0 && *d.Preferred < 2000 {
			def.Preferred = *d.Preferred
		}
		if def.Max > 0 && def.Preferred > def.Max {
			def.Preferred = def.Max
		}
		if def.Preferred > 0 && def.Preferred < def.Min {
			def.Preferred = 0
		}
		if def.Validate() != nil {
			unknown = append(unknown, d.Quality)
			continue
		}
		defs = append(defs, def)
	}
	it := &Item{Key: idKey(s.SourceID, "qualitydefinitions", k), SourceID: s.SourceID, Category: "qualitydefinitions",
		Name: fmt.Sprintf("Quality sizes (%s, %d)", k, len(defs))}
	if len(unknown) > 0 {
		it.note("not taken: %s", strings.Join(unknown, ", "))
	}
	h := hashOf(defs)
	d := p.decideNamed(it, s, "qualitydefinitions", string(k), it.Name, h, func(int64) bool { return true },
		func(string) (int64, bool) { return 0, false })
	if d.action == ActionCreate {
		it.Action = ActionUpdate // sizes always exist; they are replaced
	}
	if it.Action != ActionSkip {
		it.run = func(ctx context.Context) (string, error) {
			return "qualitydefinitions:1", p.l.Quality.UpdateDefinitions(ctx, defs)
		}
	}
	p.add(it)
}

// ---- custom formats ----

func (p *planner) planCustomFormats(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) {
		return
	}
	flavor := customformats.FlavorMovies
	if s.Kind == Sonarr {
		flavor = customformats.FlavorSeries
	}
	for _, cf := range s.CustomFormats {
		key := idKey(s.SourceID, "customformat", cf.ID)
		it := &Item{Key: key, SourceID: s.SourceID, Category: "customformat", Name: cf.Name}
		res, err := customformats.Import(cf.Raw, flavor)
		if err != nil || len(res.Formats) != 1 {
			it.Action, it.Reason = ActionSkip, "invalid"
			if err != nil {
				it.note("%v", err)
			}
			p.add(it)
			continue
		}
		f := res.Formats[0]
		if len(res.Warnings) > 0 {
			it.attn(AttnUnknownCond)
			it.note("%s", strings.Join(res.Warnings, "; "))
		}
		if !so.Settings {
			// Not importing settings: map to an existing format of the same name only.
			for _, lf := range p.formats {
				if strings.EqualFold(lf.Name, f.Name) {
					p.setID(key, lf.ID)
				}
			}
			continue
		}
		h := hashOf(f.Specs)
		d := p.decideNamed(it, s, "customformat", strconv.FormatInt(cf.ID, 10), f.Name, h, p.formatExists, p.formatByName)
		switch {
		case d.targetID > 0:
			p.setID(key, d.targetID)
		case d.action == ActionSkip && it.Reason == "duplicate":
			if prev, ok := p.plannedNames["customformat|"+strings.ToLower(f.Name)]; ok {
				prevKey := prev.key
				it.run = func(context.Context) (string, error) { p.setID(key, p.id(prevKey)); return "", nil }
			}
		}
		if d.action != ActionSkip {
			f.Name = d.name
			target := d.targetID
			it.run = func(ctx context.Context) (string, error) {
				f.ID = target
				saved, err := p.l.Formats.Save(ctx, f)
				if err != nil {
					return "", err
				}
				p.setID(key, saved.ID)
				return fmt.Sprintf("customformat:%d", saved.ID), nil
			}
		}
		p.add(it)
	}
}

func (p *planner) formatExists(id int64) bool {
	for _, f := range p.formats {
		if f.ID == id {
			return true
		}
	}
	return false
}

func (p *planner) formatByName(name string) (int64, bool) {
	for _, f := range p.formats {
		if strings.EqualFold(f.Name, name) {
			return f.ID, true
		}
	}
	return 0, false
}

// ---- quality profiles ----

// profileLanguages are the languages a Sonarr v3 profile is used with (the
// language profile its series use most) or a Radarr profile's language.
func profileLanguages(s *Snapshot, qp QualityProfile) []string {
	if qp.Language != "" {
		return []string{qp.Language}
	}
	if len(s.LanguageProfiles) == 0 {
		return nil
	}
	uses := map[int64]int{}
	for _, v := range s.Series {
		if v.QualityProfileID == qp.ID {
			uses[v.LanguageProfileID]++
		}
	}
	best, n := int64(0), 0
	for id, c := range uses {
		if c > n || (c == n && id < best) {
			best, n = id, c
		}
	}
	for _, lp := range s.LanguageProfiles {
		if lp.ID == best {
			return lp.Languages
		}
	}
	return nil
}

func (p *planner) planQualityProfiles(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) {
		return
	}
	k := kindOf(s.Kind)
	for _, qp := range s.QualityProfiles {
		key := idKey(s.SourceID, "qualityprofile", qp.ID)
		conv := ConvertProfile(qp, k, profileLanguages(s, qp))
		it := &Item{Key: key, SourceID: s.SourceID, Category: "qualityprofile", Name: qp.Name}
		for _, w := range conv.Warnings {
			it.note("%s", w)
		}
		if !so.Settings {
			if id, ok := p.profileByName(k)(conv.Profile.Name); ok {
				p.setID(key, id)
			}
			continue
		}
		check := conv.Profile
		if err := check.Validate(); err != nil {
			it.Action, it.Reason = ActionSkip, "invalid"
			it.note("%v", err)
			p.add(it)
			continue
		}
		h := hashOf([]any{conv.Profile, conv.FormatScores})
		d := p.decideNamed(it, s, "qualityprofile", strconv.FormatInt(qp.ID, 10), conv.Profile.Name, h, p.profileExists,
			p.profileByName(k))
		if d.targetID > 0 {
			p.setID(key, d.targetID)
		}
		if d.action != ActionSkip {
			prof := conv.Profile
			prof.Name = d.name
			target := d.targetID
			scores := conv.FormatScores
			sid := s.SourceID
			it.run = func(ctx context.Context) (string, error) {
				prof.FormatScores = []quality.FormatScore{}
				ids := make([]int64, 0, len(scores))
				for src := range scores {
					ids = append(ids, src)
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
				seen := map[int64]bool{}
				for _, src := range ids {
					if local := p.id(idKey(sid, "customformat", src)); local > 0 && !seen[local] {
						seen[local] = true
						prof.FormatScores = append(prof.FormatScores, quality.FormatScore{FormatID: local, Score: scores[src]})
					}
				}
				var saved quality.Profile
				var err error
				if target > 0 {
					prof.ID = target
					saved, err = p.l.Quality.UpdateProfile(ctx, prof)
				} else {
					saved, err = p.l.Quality.CreateProfile(ctx, prof)
				}
				if err != nil {
					return "", err
				}
				p.setID(key, saved.ID)
				return fmt.Sprintf("qualityprofile:%d", saved.ID), nil
			}
		}
		p.add(it)
	}
}

func (p *planner) profileExists(id int64) bool {
	for _, x := range p.profiles {
		if x.ID == id {
			return true
		}
	}
	return false
}

func (p *planner) profileByName(k quality.Kind) func(string) (int64, bool) {
	return func(name string) (int64, bool) {
		for _, x := range p.profiles {
			if x.Kind == k && strings.EqualFold(x.Name, name) {
				return x.ID, true
			}
		}
		return 0, false
	}
}

// profileFor resolves (at apply time) the local profile for a source
// profile, falling back to the first profile of the kind.
func (p *planner) profileFor(ctx context.Context, sourceID, srcID int64, k quality.Kind) (int64, bool) {
	if id := p.id(idKey(sourceID, "qualityprofile", srcID)); id > 0 {
		return id, false
	}
	list, err := p.l.Quality.Profiles(ctx)
	if err == nil {
		for _, x := range list {
			if x.Kind == k {
				return x.ID, true
			}
		}
	}
	return 0, true
}

// profileKnown reports at plan time whether a source profile maps to a local one.
func (p *planner) profileKnown(sourceID, srcID int64) bool {
	if p.id(idKey(sourceID, "qualityprofile", srcID)) > 0 {
		return true
	}
	for _, it := range p.plan.Items {
		if it.Key == idKey(sourceID, "qualityprofile", srcID) && it.Action == ActionCreate {
			return true
		}
	}
	return false
}

// ---- metadata profiles (music) ----

func (p *planner) planMetadataProfiles(s *Snapshot, so SourceOptions) {
	if s.Kind != Lidarr || p.l.Music == nil {
		return
	}
	for _, mp := range s.MetadataProfiles {
		key := idKey(s.SourceID, "metadataprofile", mp.ID)
		prof := music.MetadataProfile{Name: mp.Name}
		for _, x := range mp.Primary {
			prof.PrimaryTypes = append(prof.PrimaryTypes, MapMetadataType(x))
		}
		for _, x := range mp.Secondary {
			prof.SecondaryTypes = append(prof.SecondaryTypes, MapMetadataType(x))
		}
		for _, x := range mp.Statuses {
			prof.ReleaseStatuses = append(prof.ReleaseStatuses, MapMetadataType(x))
		}
		byName := func(name string) (int64, bool) {
			for _, x := range p.metaProfiles {
				if strings.EqualFold(x.Name, name) {
					return x.ID, true
				}
			}
			return 0, false
		}
		if !so.Settings {
			if id, ok := byName(mp.Name); ok {
				p.setID(key, id)
			}
			continue
		}
		it := &Item{Key: key, SourceID: s.SourceID, Category: "metadataprofile", Name: mp.Name}
		check := prof
		if err := check.Validate(); err != nil {
			it.Action, it.Reason = ActionSkip, "invalid"
			it.note("%v", err)
			p.add(it)
			continue
		}
		exists := func(id int64) bool {
			for _, x := range p.metaProfiles {
				if x.ID == id {
					return true
				}
			}
			return false
		}
		d := p.decideNamed(it, s, "metadataprofile", strconv.FormatInt(mp.ID, 10), mp.Name, hashOf(check), exists, byName)
		if d.targetID > 0 {
			p.setID(key, d.targetID)
		}
		if d.action != ActionSkip {
			prof.Name, prof.ID = d.name, d.targetID
			it.run = func(ctx context.Context) (string, error) {
				saved, err := p.l.Music.SaveProfile(ctx, prof)
				if err != nil {
					return "", err
				}
				p.setID(key, saved.ID)
				return fmt.Sprintf("metadataprofile:%d", saved.ID), nil
			}
		}
		p.add(it)
	}
}

// ---- delay profiles ----

func (p *planner) tagNames(s *Snapshot, ids []int64) []string {
	out := []string{}
	for _, id := range ids {
		if n, ok := s.Tags[id]; ok {
			out = append(out, n)
		}
	}
	return quality.NormTags(out)
}

func (p *planner) planDelayProfiles(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.Settings {
		return
	}
	local, _ := p.l.Quality.DelayProfiles(p.ctx)
	for _, dp := range s.DelayProfiles {
		tags := p.tagNames(s, dp.Tags)
		isDefault := len(dp.Tags) == 0
		d := quality.DelayProfile{Name: fmt.Sprintf("%s: %s", s.Name, strings.Join(tags, ", ")), Enabled: true,
			PreferredProtocol: strings.ToLower(dp.PreferredProtocol), UsenetEnabled: dp.EnableUsenet, TorrentEnabled: dp.EnableTorrent,
			UsenetDelay: dp.UsenetDelay, TorrentDelay: dp.TorrentDelay, BypassIfHighestQuality: dp.BypassIfHighestQuality,
			BypassIfAboveScore: dp.BypassIfAboveScore, MinimumScore: dp.MinimumScore, Tags: tags}
		if d.PreferredProtocol != quality.Usenet {
			d.PreferredProtocol = quality.Torrent
		}
		name := d.Name
		if isDefault {
			name = "Default delay profile"
		}
		key := idKey(s.SourceID, "delayprofile", dp.ID)
		it := &Item{Key: key, SourceID: s.SourceID, Category: "delayprofile", Name: name}
		h := hashOf([]any{d.PreferredProtocol, d.UsenetEnabled, d.TorrentEnabled, d.UsenetDelay, d.TorrentDelay, d.BypassIfHighestQuality,
			d.BypassIfAboveScore, d.MinimumScore, d.Tags})
		exists := func(id int64) bool {
			for _, x := range local {
				if x.ID == id {
					return true
				}
			}
			return false
		}
		byName := func(n string) (int64, bool) {
			for _, x := range local {
				if isDefault && x.Default {
					return x.ID, true
				}
				if !isDefault && !x.Default && quality.TagsMatch(x.Tags, tags) && len(x.Tags) == len(tags) && strings.Join(x.Tags, ",") == strings.Join(tags, ",") {
					return x.ID, true
				}
			}
			return 0, false
		}
		dec := p.decideNamed(it, s, "delayprofile", strconv.FormatInt(dp.ID, 10), name, h, exists, byName)
		if dec.action != ActionSkip {
			target := dec.targetID
			if dec.action == ActionCreate && isDefault {
				// A second default (another source) cannot exist: it updates the default.
				target, _ = byName(name)
			}
			it.run = func(ctx context.Context) (string, error) {
				d.ID = target
				if target > 0 {
					if cur, err := p.l.Quality.DelayProfile(ctx, target); err == nil && cur.Default {
						d.Name, d.Tags = cur.Name, []string{}
					}
				}
				saved, err := p.l.Quality.SaveDelayProfile(ctx, d)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("delayprofile:%d", saved.ID), nil
			}
		}
		p.add(it)
	}
}

// ---- release profiles ----

func (p *planner) planReleaseProfiles(s *Snapshot, so SourceOptions) {
	if !isArr(s.Kind) || !so.Settings {
		return
	}
	local, _ := p.l.Quality.ReleaseProfiles(p.ctx)
	for i, rp := range s.ReleaseProfiles {
		name := strings.TrimSpace(rp.Name)
		if name == "" {
			name = fmt.Sprintf("%s release profile %d", s.Name, i+1)
		}
		key := idKey(s.SourceID, "releaseprofile", rp.ID)
		it := &Item{Key: key, SourceID: s.SourceID, Category: "releaseprofile", Name: name}
		if rp.Preferred > 0 {
			it.note("%d preferred terms were not taken (use custom formats with scores instead)", rp.Preferred)
		}
		if len(rp.Required) == 0 && len(rp.Ignored) == 0 {
			it.Action, it.Reason = ActionSkip, "empty"
			p.add(it)
			continue
		}
		r := quality.ReleaseProfile{Name: name, Enabled: rp.Enabled, Required: rp.Required, Ignored: rp.Ignored, Tags: p.tagNames(s, rp.Tags)}
		exists := func(id int64) bool {
			for _, x := range local {
				if x.ID == id {
					return true
				}
			}
			return false
		}
		byName := func(n string) (int64, bool) {
			for _, x := range local {
				if strings.EqualFold(x.Name, n) {
					return x.ID, true
				}
			}
			return 0, false
		}
		dec := p.decideNamed(it, s, "releaseprofile", strconv.FormatInt(rp.ID, 10), name, hashOf(r), exists, byName)
		if dec.action != ActionSkip {
			r.Name = dec.name
			target, sid, srcIx := dec.targetID, s.SourceID, rp.IndexerID
			it.run = func(ctx context.Context) (string, error) {
				r.ID = target
				if srcIx > 0 {
					r.IndexerID = p.id(idKey(sid, "indexer", srcIx))
				}
				saved, err := p.l.Quality.SaveReleaseProfile(ctx, r)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("releaseprofile:%d", saved.ID), nil
			}
		}
		p.add(it)
	}
}

func dirExists(p string) bool {
	st, err := statDir(p)
	return err == nil && st
}
