package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/adult"
	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// errAdultHidden answers every adult endpoint the caller may not see: the
// same 404 as an unknown path, so nothing tells that the feature exists.
var errAdultHidden = &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: "no such endpoint"}

func mapAdultError(err error) *StatusError {
	var ie *adult.ImportError
	if errors.As(err, &ie) {
		return &StatusError{Status: http.StatusConflict, Code: ie.Code, Message: ie.Msg}
	}
	table := []struct {
		err    error
		status int
		code   string
	}{
		{adult.ErrHidden, http.StatusNotFound, "not_found"},
		{adult.ErrNotFound, http.StatusNotFound, "not_found"},
		{adult.ErrConfirm, http.StatusBadRequest, "adult_confirm_required"},
		{adult.ErrExists, http.StatusConflict, "already_exists"},
		{adult.ErrRemoved, http.StatusUnprocessableEntity, "adult_entry_removed"},
		{adult.ErrBlocked, http.StatusUnprocessableEntity, "blocked_tag"},
		{adult.ErrNoRoot, http.StatusBadRequest, "adult_no_root_folder"},
		{adult.ErrNoProfile, http.StatusBadRequest, "validation"},
		{adult.ErrUnknownSource, http.StatusBadRequest, "validation"},
		{adult.ErrBadTerm, http.StatusBadRequest, "validation"},
		{meta.ErrNoKey, http.StatusBadRequest, "metadata_key_missing"},
		{meta.ErrBadKey, http.StatusBadRequest, "metadata_key_invalid"},
		{meta.ErrNotFound, http.StatusNotFound, "not_found"},
		{meta.ErrRemoved, http.StatusUnprocessableEntity, "adult_entry_removed"},
	}
	for _, t := range table {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return nil
}

// adultAdmin: administrators only (settings and blocklist work while the
// feature is off); everyone else gets 404.
func (h *Handler) adultAdmin(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	// API keys reach these only when created with "include adult".
	if !p.IsAdmin() || h.Adult == nil || (p.APIKeyID != 0 && !p.IncludeAdult) {
		return p, errAdultHidden
	}
	return p, nil
}

// adultUser: the feature is on and the caller holds view_adult (and an API
// key was created with "include adult"); otherwise 404.
func (h *Handler) adultUser(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if !h.Adult.Visible(ctx, p) {
		return p, errAdultHidden
	}
	return p, nil
}

// adultManager: a permitted user who may also change the library.
func (h *Handler) adultManager(ctx context.Context) (auth.Principal, error) {
	p, err := h.adultUser(ctx)
	if err != nil {
		return p, err
	}
	if !p.Can(auth.PermManageLibrary) {
		return p, errForbidden
	}
	return p, nil
}

func (h *Handler) adultSettings(ctx context.Context) (*gen.AdultSettings, error) {
	s := h.Settings
	out := &gen.AdultSettings{Tokens: naming.TokenNames(adult.Tokens)}
	var err error
	get := func(f func() error) {
		if err == nil {
			err = f()
		}
	}
	get(func() (e error) { out.Enabled, e = settings.Get(ctx, s, adult.Enabled); return })
	get(func() (e error) {
		v, e := settings.Get(ctx, s, adult.ConfirmedAt)
		out.ConfirmedAt = optStr(v)
		return e
	})
	get(func() (e error) { out.StashdbKeySet, e = s.Configured(ctx, adult.StashDBKey); return })
	get(func() (e error) { out.TpdbKeySet, e = s.Configured(ctx, adult.TPDBKey); return })
	get(func() (e error) {
		v, e := settings.Get(ctx, s, adult.DefaultSource)
		out.DefaultSource = gen.AdultSettingsDefaultSource(v)
		return e
	})
	get(func() (e error) { out.DefaultProfileId, e = settings.Get(ctx, s, adult.DefaultProfile); return })
	get(func() (e error) { out.DefaultRootFolderId, e = settings.Get(ctx, s, adult.DefaultRoot); return })
	get(func() (e error) { out.SceneFolder, e = settings.Get(ctx, s, adult.SceneFolder); return })
	get(func() (e error) { out.SceneFile, e = settings.Get(ctx, s, adult.SceneFile); return })
	get(func() (e error) { out.MovieFolder, e = settings.Get(ctx, s, adult.MovieFolder); return })
	get(func() (e error) { out.MovieFile, e = settings.Get(ctx, s, adult.MovieFile); return })
	get(func() (e error) { out.StatsAggregate, e = settings.Get(ctx, s, adult.StatsAggregate); return })
	get(func() (e error) { out.NotifyStreamero, e = settings.Get(ctx, s, adult.NotifyStreamero); return })
	return out, err
}

// GetAdultSettings implements GET /adult/settings.
func (h *Handler) GetAdultSettings(ctx context.Context) (*gen.AdultSettings, error) {
	if _, err := h.adultAdmin(ctx); err != nil {
		return nil, err
	}
	return h.adultSettings(ctx)
}

// UpdateAdultSettings implements PUT /adult/settings.
func (h *Handler) UpdateAdultSettings(ctx context.Context, req *gen.AdultSettingsUpdate) (*gen.AdultSettings, error) {
	p, err := h.adultAdmin(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range []struct {
		v      gen.OptString
		key    settings.SecretKey
		source string
	}{{req.StashdbKey, adult.StashDBKey, meta.StashDB}, {req.TpdbKey, adult.TPDBKey, meta.TPDB}} {
		v, ok := k.v.Get()
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if v != "" {
			prov, err := h.Adult.Provider(k.source)
			if err != nil {
				return nil, err
			}
			if err := prov.Validate(ctx, v); err != nil {
				return nil, err
			}
		}
		if err := h.Settings.SetSecret(ctx, k.key, v); err != nil {
			return nil, err
		}
	}
	for _, t := range []struct {
		v   gen.OptString
		key settings.Key[string]
	}{{req.SceneFolder, adult.SceneFolder}, {req.SceneFile, adult.SceneFile}, {req.MovieFolder, adult.MovieFolder}, {req.MovieFile, adult.MovieFile}} {
		if v, ok := t.v.Get(); ok {
			if err := naming.Validate(v, adult.Tokens); err != nil {
				return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_template", Message: err.Error()}
			}
			if err := settings.Set(ctx, h.Settings, h.DB.W, t.key, v); err != nil {
				return nil, err
			}
		}
	}
	if v, ok := req.DefaultSource.Get(); ok {
		if err := settings.Set(ctx, h.Settings, h.DB.W, adult.DefaultSource, string(v)); err != nil {
			return nil, err
		}
	}
	for _, t := range []struct {
		v   gen.OptInt64
		key settings.Key[int64]
	}{{req.DefaultProfileId, adult.DefaultProfile}, {req.DefaultRootFolderId, adult.DefaultRoot}} {
		if v, ok := t.v.Get(); ok {
			if err := settings.Set(ctx, h.Settings, h.DB.W, t.key, v); err != nil {
				return nil, err
			}
		}
	}
	for _, t := range []struct {
		v   gen.OptBool
		key settings.Key[bool]
	}{{req.StatsAggregate, adult.StatsAggregate}, {req.NotifyStreamero, adult.NotifyStreamero}} {
		if v, ok := t.v.Get(); ok {
			if err := settings.Set(ctx, h.Settings, h.DB.W, t.key, v); err != nil {
				return nil, err
			}
		}
	}
	if on, ok := req.Enabled.Get(); ok {
		if err := h.Adult.Enable(ctx, p.User.ID, on, req.ConfirmAdults.Or(false)); err != nil {
			return nil, err
		}
	}
	return h.adultSettings(ctx)
}

// ListAdultBlocklist implements GET /adult/blocklist.
func (h *Handler) ListAdultBlocklist(ctx context.Context) ([]gen.AdultBlockTerm, error) {
	if _, err := h.adultAdmin(ctx); err != nil {
		return nil, err
	}
	if err := h.Adult.SeedBlocklist(ctx); err != nil {
		return nil, err
	}
	list, err := h.Adult.Blocklist(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AdultBlockTerm, 0, len(list))
	for _, t := range list {
		out = append(out, gen.AdultBlockTerm{ID: t.ID, Term: t.Term, Builtin: t.Builtin})
	}
	return out, nil
}

// AddAdultBlocklistTerm implements POST /adult/blocklist.
func (h *Handler) AddAdultBlocklistTerm(ctx context.Context, req *gen.AddAdultBlocklistTermReq) (*gen.AdultBlockTerm, error) {
	if _, err := h.adultAdmin(ctx); err != nil {
		return nil, err
	}
	t, err := h.Adult.AddTerm(ctx, req.Term)
	if err != nil {
		return nil, err
	}
	return &gen.AdultBlockTerm{ID: t.ID, Term: t.Term, Builtin: t.Builtin}, nil
}

// DeleteAdultBlocklistTerm implements DELETE /adult/blocklist/{id}.
func (h *Handler) DeleteAdultBlocklistTerm(ctx context.Context, p gen.DeleteAdultBlocklistTermParams) error {
	if _, err := h.adultAdmin(ctx); err != nil {
		return err
	}
	return h.Adult.RemoveTerm(ctx, p.ID)
}

// GetAdultPreferences implements GET /adult/preferences.
func (h *Handler) GetAdultPreferences(ctx context.Context) (*gen.AdultPreferences, error) {
	p, err := h.adultUser(ctx)
	if err != nil {
		return nil, err
	}
	blur, err := settings.Get(ctx, h.Settings, adult.BlurKey(p.User.ID))
	return &gen.AdultPreferences{Blur: blur}, err
}

// UpdateAdultPreferences implements PUT /adult/preferences.
func (h *Handler) UpdateAdultPreferences(ctx context.Context, req *gen.AdultPreferences) (*gen.AdultPreferences, error) {
	p, err := h.adultUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := settings.Set(ctx, h.Settings, h.DB.W, adult.BlurKey(p.User.ID), req.Blur); err != nil {
		return nil, err
	}
	return &gen.AdultPreferences{Blur: req.Blur}, nil
}

// ListAdultRootFolders implements GET /adult/rootfolders.
func (h *Handler) ListAdultRootFolders(ctx context.Context) ([]gen.AdultRootFolder, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.Adult.RootFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AdultRootFolder, 0, len(list))
	for _, r := range list {
		out = append(out, gen.AdultRootFolder{ID: r.ID, Path: r.Path})
	}
	return out, nil
}

// AddAdultRootFolder implements POST /adult/rootfolders.
func (h *Handler) AddAdultRootFolder(ctx context.Context, req *gen.AddAdultRootFolderReq) (*gen.AdultRootFolder, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	r, err := h.Adult.AddRootFolder(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	return &gen.AdultRootFolder{ID: r.ID, Path: r.Path}, nil
}

// DeleteAdultRootFolder implements DELETE /adult/rootfolders/{id}.
func (h *Handler) DeleteAdultRootFolder(ctx context.Context, p gen.DeleteAdultRootFolderParams) error {
	if _, err := h.adultManager(ctx); err != nil {
		return err
	}
	return h.Adult.DeleteRootFolder(ctx, p.ID)
}

// LookupAdult implements GET /adult/lookup.
func (h *Handler) LookupAdult(ctx context.Context, p gen.LookupAdultParams) ([]gen.AdultLookupResult, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.Adult.Lookup(ctx, string(p.Source), string(p.Type), p.Term)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AdultLookupResult, 0, len(list))
	for _, r := range list {
		out = append(out, gen.AdultLookupResult{Source: gen.AdultLookupResultSource(r.Source), ID: r.ID, Type: gen.AdultLookupResultType(r.Type),
			Name: r.Name, Date: optStr(r.Date), Studio: optStr(r.Studio), Performers: r.Performers, InLibrary: gen.NewOptBool(r.InLibrary)})
	}
	return out, nil
}

func addOptions(req *gen.AdultAddRequest) adult.AddOptions {
	return adult.AddOptions{Source: string(req.Source), ID: req.ID, QualityProfileID: req.QualityProfileId.Or(0), RootFolderID: req.RootFolderId.Or(0),
		Monitored: req.Monitored.Or(true), Search: req.Search.Or(true)}
}

func optID(p *int64) gen.OptInt64 {
	if p == nil {
		return gen.OptInt64{}
	}
	return gen.NewOptInt64(*p)
}

func toAdultStudio(s adult.Studio) gen.AdultStudio {
	return gen.AdultStudio{ID: s.ID, Name: s.Name, StashdbId: optStr(s.StashDBID), TpdbId: optStr(s.TPDBID), URL: optStr(s.URL), Image: optStr(s.Image),
		Monitored: s.Monitored, MonitorSince: optStr(s.MonitorSince), QualityProfileId: s.QualityProfileID, RootFolderId: optID(s.RootFolderID),
		Path: s.Path, AddedAt: s.AddedAt, RefreshedAt: optTime(s.RefreshedAt), Scenes: s.Scenes, Files: s.Files}
}

func toAdultPerformer(p adult.Performer) gen.AdultPerformer {
	out := gen.AdultPerformer{ID: p.ID, Name: p.Name, Disambiguation: optStr(p.Disambiguation), StashdbId: optStr(p.StashDBID), TpdbId: optStr(p.TPDBID),
		Image: optStr(p.Image), Monitored: p.Monitored, MonitorSince: optStr(p.MonitorSince), AddedAt: p.AddedAt, RefreshedAt: optTime(p.RefreshedAt),
		Items: p.Items}
	if p.QualityProfileID > 0 {
		out.QualityProfileId = gen.NewOptInt64(p.QualityProfileID)
	}
	return out
}

func toAdultItem(it adult.Item) gen.AdultItem {
	out := gen.AdultItem{ID: it.ID, Kind: gen.AdultItemKind(it.Kind), Ref: it.Ref().String(), StudioId: optID(it.StudioID), Studio: it.Studio,
		Title: it.Title, ReleaseDate: optStr(it.ReleaseDate), Overview: optStr(it.Overview), StashdbId: optStr(it.StashDBID), TpdbId: optStr(it.TPDBID),
		Image: optStr(it.Image), Monitored: it.Monitored, QualityProfileId: it.QualityProfileID, Path: it.Path, AddedAt: it.AddedAt,
		SourceTags: it.SourceTags, Performers: make([]gen.AdultItemPerformersItem, 0, len(it.Performers))}
	if it.Year > 0 {
		out.Year = gen.NewOptInt(it.Year)
	}
	if it.Duration > 0 {
		out.Duration = gen.NewOptInt(it.Duration)
	}
	for _, p := range it.Performers {
		out.Performers = append(out.Performers, gen.AdultItemPerformersItem{ID: p.ID, Name: p.Name})
	}
	if f := it.File; f != nil {
		out.File = gen.NewOptAdultFile(gen.AdultFile{ID: f.ID, Path: f.Path, Size: f.Size, Quality: f.Quality, ReleaseGroup: optStr(f.ReleaseGroup),
			SceneName: optStr(f.SceneName), AddedAt: gen.NewOptDateTime(f.AddedAt)})
	}
	return out
}

// ListAdultStudios implements GET /adult/studios.
func (h *Handler) ListAdultStudios(ctx context.Context) ([]gen.AdultStudio, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.Adult.Studios(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AdultStudio, 0, len(list))
	for _, s := range list {
		out = append(out, toAdultStudio(s))
	}
	return out, nil
}

// AddAdultStudio implements POST /adult/studios.
func (h *Handler) AddAdultStudio(ctx context.Context, req *gen.AdultAddRequest) (*gen.AdultStudio, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	s, err := h.Adult.AddStudio(ctx, addOptions(req))
	if err != nil {
		return nil, err
	}
	if req.Search.Or(true) {
		h.searchStudio(ctx, s.ID)
	}
	out := toAdultStudio(s)
	return &out, nil
}

func (h *Handler) searchStudio(ctx context.Context, id int64) {
	items, _, err := h.Adult.Items(ctx, adult.ItemFilter{StudioID: id, Missing: true, Limit: -1})
	if err != nil {
		return
	}
	for _, it := range items {
		_ = h.Wanted.Enqueue(ctx, it.Ref(), "added")
	}
}

// GetAdultStudio implements GET /adult/studios/{id}.
func (h *Handler) GetAdultStudio(ctx context.Context, p gen.GetAdultStudioParams) (*gen.AdultStudio, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	s, err := h.Adult.Studio(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := toAdultStudio(s)
	return &out, nil
}

func edit(req *gen.AdultEditRequest) adult.Edit {
	var e adult.Edit
	if v, ok := req.Monitored.Get(); ok {
		e.Monitored = &v
	}
	if v, ok := req.QualityProfileId.Get(); ok {
		e.QualityProfileID = &v
	}
	return e
}

// UpdateAdultStudio implements PATCH /adult/studios/{id}.
func (h *Handler) UpdateAdultStudio(ctx context.Context, req *gen.AdultEditRequest, p gen.UpdateAdultStudioParams) (*gen.AdultStudio, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	s, err := h.Adult.EditStudio(ctx, p.ID, edit(req))
	if err != nil {
		return nil, err
	}
	out := toAdultStudio(s)
	return &out, nil
}

// DeleteAdultStudio implements DELETE /adult/studios/{id}.
func (h *Handler) DeleteAdultStudio(ctx context.Context, p gen.DeleteAdultStudioParams) error {
	if _, err := h.adultManager(ctx); err != nil {
		return err
	}
	return h.Adult.DeleteStudio(ctx, p.ID, p.DeleteFiles.Or(false))
}

// RefreshAdultStudio implements POST /adult/studios/{id}/refresh.
func (h *Handler) RefreshAdultStudio(ctx context.Context, p gen.RefreshAdultStudioParams) (*gen.QueuedCount, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	n, err := h.Adult.RefreshStudio(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	h.searchStudio(ctx, p.ID)
	return &gen.QueuedCount{Count: n}, nil
}

// ListAdultPerformers implements GET /adult/performers.
func (h *Handler) ListAdultPerformers(ctx context.Context) ([]gen.AdultPerformer, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.Adult.Performers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.AdultPerformer, 0, len(list))
	for _, p := range list {
		out = append(out, toAdultPerformer(p))
	}
	return out, nil
}

// AddAdultPerformer implements POST /adult/performers.
func (h *Handler) AddAdultPerformer(ctx context.Context, req *gen.AdultAddRequest) (*gen.AdultPerformer, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	p, err := h.Adult.AddPerformer(ctx, addOptions(req))
	if err != nil {
		return nil, err
	}
	out := toAdultPerformer(p)
	return &out, nil
}

// GetAdultPerformer implements GET /adult/performers/{id}.
func (h *Handler) GetAdultPerformer(ctx context.Context, p gen.GetAdultPerformerParams) (*gen.AdultPerformer, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	pf, err := h.Adult.Performer(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := toAdultPerformer(pf)
	return &out, nil
}

// UpdateAdultPerformer implements PATCH /adult/performers/{id}.
func (h *Handler) UpdateAdultPerformer(ctx context.Context, req *gen.AdultEditRequest, p gen.UpdateAdultPerformerParams) (*gen.AdultPerformer, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	pf, err := h.Adult.EditPerformer(ctx, p.ID, edit(req))
	if err != nil {
		return nil, err
	}
	if pf.Monitored {
		if _, err := h.Adult.RefreshPerformer(ctx, pf.ID); err != nil {
			h.Log.Warn("refresh performer", "err", err)
		}
	}
	out := toAdultPerformer(pf)
	return &out, nil
}

// DeleteAdultPerformer implements DELETE /adult/performers/{id}.
func (h *Handler) DeleteAdultPerformer(ctx context.Context, p gen.DeleteAdultPerformerParams) error {
	if _, err := h.adultManager(ctx); err != nil {
		return err
	}
	return h.Adult.DeletePerformer(ctx, p.ID)
}

// ListAdultItems implements GET /adult/items.
func (h *Handler) ListAdultItems(ctx context.Context, p gen.ListAdultItemsParams) (*gen.AdultItemPage, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	f := adult.ItemFilter{Kind: string(p.Kind.Or("")), StudioID: p.StudioId.Or(0), PerformerID: p.PerformerId.Or(0), Term: p.Term.Or(""),
		Missing: p.Missing.Or(false), Limit: p.Limit.Or(100), Offset: p.Offset.Or(0)}
	list, total, err := h.Adult.Items(ctx, f)
	if err != nil {
		return nil, err
	}
	out := &gen.AdultItemPage{Total: total, Items: make([]gen.AdultItem, 0, len(list))}
	for _, it := range list {
		out.Items = append(out.Items, toAdultItem(it))
	}
	return out, nil
}

// AddAdultItem implements POST /adult/items.
func (h *Handler) AddAdultItem(ctx context.Context, req *gen.AdultAddRequest) (*gen.AdultItem, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	it, err := h.Adult.AddItem(ctx, req.Kind.Or("") == gen.AdultAddRequestKindMovie, addOptions(req))
	if err != nil {
		return nil, err
	}
	out := toAdultItem(it)
	return &out, nil
}

// GetAdultItem implements GET /adult/items/{id}.
func (h *Handler) GetAdultItem(ctx context.Context, p gen.GetAdultItemParams) (*gen.AdultItem, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	it, err := h.Adult.Item(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := toAdultItem(it)
	return &out, nil
}

// UpdateAdultItem implements PATCH /adult/items/{id}.
func (h *Handler) UpdateAdultItem(ctx context.Context, req *gen.AdultEditRequest, p gen.UpdateAdultItemParams) (*gen.AdultItem, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	it, err := h.Adult.EditItem(ctx, p.ID, edit(req))
	if err != nil {
		return nil, err
	}
	h.Wanted.ResetBackoff(ctx, it.Ref())
	out := toAdultItem(it)
	return &out, nil
}

// DeleteAdultItem implements DELETE /adult/items/{id}.
func (h *Handler) DeleteAdultItem(ctx context.Context, p gen.DeleteAdultItemParams) error {
	if _, err := h.adultManager(ctx); err != nil {
		return err
	}
	return h.Adult.DeleteItem(ctx, p.ID, p.DeleteFiles.Or(false))
}

func (h *Handler) adultRef(ctx context.Context, id int64) (library.Ref, error) {
	it, err := h.Adult.Item(ctx, id)
	if err != nil {
		return library.Ref{}, err
	}
	return it.Ref(), nil
}

// SearchAdultItem implements POST /adult/items/{id}/search.
func (h *Handler) SearchAdultItem(ctx context.Context, p gen.SearchAdultItemParams) (*gen.QueuedCount, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	ref, err := h.adultRef(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if err := h.Wanted.RetryNow(ctx, ref); err != nil {
		return nil, err
	}
	return &gen.QueuedCount{Count: 1}, nil
}

// InteractiveSearchAdultItem implements GET /adult/items/{id}/releases.
func (h *Handler) InteractiveSearchAdultItem(ctx context.Context, p gen.InteractiveSearchAdultItemParams) (*gen.InteractiveSearch, error) {
	pr, err := h.adultManager(ctx)
	if err != nil {
		return nil, err
	}
	ref, err := h.adultRef(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	ev, err := h.Wanted.Interactive(ctx, ref, pr.User.ID)
	if err != nil {
		return nil, err
	}
	return h.toInteractive(ev)
}

// GrabAdultRelease implements POST /adult/items/{id}/releases.
func (h *Handler) GrabAdultRelease(ctx context.Context, req *gen.ManualGrabRequest, p gen.GrabAdultReleaseParams) (*gen.QueueItem, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	ref, err := h.adultRef(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if term, blocked := h.Adult.BlockedText(ctx, req.Title); blocked {
		return nil, &StatusError{Status: http.StatusUnprocessableEntity, Code: "blocked_tag", Message: "the release name contains the blocked term " + term}
	}
	g := wanted.ManualGrab{IndexerID: req.IndexerId, DownloadToken: req.DownloadToken.Or(""), MagnetURL: req.MagnetUrl.Or(""),
		Title: req.Title, Protocol: string(req.Protocol), GUID: req.GUID.Or("")}
	if t, ok := req.PublishDate.Get(); ok {
		g.PublishedAt = &t
	}
	if g.DownloadToken == "" && g.MagnetURL == "" {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: "downloadToken or magnetUrl is required"}
	}
	d, err := h.Wanted.Grab(ctx, ref, g)
	if err != nil {
		return nil, err
	}
	out := toQueueItem(downloads.QueueItem{Download: d})
	return &out, nil
}

// GetAdultItemTimeline implements GET /adult/items/{id}/timeline.
func (h *Handler) GetAdultItemTimeline(ctx context.Context, p gen.GetAdultItemTimelineParams) (*gen.Timeline, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	it, ex, events, err := h.Adult.Explain(ctx, p.ID, 500)
	if err != nil {
		return nil, err
	}
	ge, err := toExplanation(ex)
	if err != nil {
		return nil, err
	}
	out := &gen.Timeline{Item: it.Ref().String(), Title: optStr(it.Title), Explanation: ge, Events: make([]gen.TimelineEvent, 0, len(events))}
	for _, e := range events {
		out.Events = append(out.Events, gen.TimelineEvent{ID: e.ID, At: e.At, Type: e.Type, Source: gen.TimelineEventSource(e.Source),
			Message: e.Message, Data: rawJSON(e.Data), DownloadId: optI64(e.DownloadID)})
	}
	return out, nil
}

// RenameAdultItem implements POST /adult/items/{id}/rename.
func (h *Handler) RenameAdultItem(ctx context.Context, p gen.RenameAdultItemParams) (*gen.AdultRename, error) {
	if _, err := h.adultManager(ctx); err != nil {
		return nil, err
	}
	r, err := h.Adult.Rename(ctx, p.ID, p.Apply.Or(false))
	if err != nil {
		return nil, err
	}
	return &gen.AdultRename{ItemId: r.ItemID, From: r.From, To: r.To}, nil
}

// GetAdultCalendar implements GET /adult/calendar.
func (h *Handler) GetAdultCalendar(ctx context.Context, p gen.GetAdultCalendarParams) ([]gen.AdultItem, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	list, _, err := h.Adult.Items(ctx, adult.ItemFilter{From: day(p.Start), To: day(p.End), Limit: 500})
	if err != nil {
		return nil, err
	}
	out := make([]gen.AdultItem, 0, len(list))
	for _, it := range list {
		out = append(out, toAdultItem(it))
	}
	return out, nil
}

// GetAdultImage implements GET /adult/images/{key}: permitted users only,
// never cached by shared caches.
func (h *Handler) GetAdultImage(ctx context.Context, p gen.GetAdultImageParams) (*gen.GetAdultImageOKHeaders, error) {
	if _, err := h.adultUser(ctx); err != nil {
		return nil, err
	}
	path, err := h.Adult.ImagePath(p.Key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) // #nosec G304 -- inside the private image folder, key validated
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	_, _ = f.Seek(0, 0)
	return &gen.GetAdultImageOKHeaders{CacheControl: gen.NewOptString("private, no-store"), ContentType: http.DetectContentType(buf[:n]),
		Response: gen.GetAdultImageOK{Data: f}}, nil
}

// hidesAdult reports a caller who may not see adult content (shared lists
// leave adult entries out for them).
func (h *Handler) hidesAdult(ctx context.Context) bool {
	p, err := principal(ctx)
	return err != nil || !h.Adult.Visible(ctx, p)
}

// isAdultDownload reports a download grabbed for an adult item.
func isAdultDownload(itemRef []byte) bool { return library.IsAdult(library.RefFromJSON(itemRef).Kind) }
