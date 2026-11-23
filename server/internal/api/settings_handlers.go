package api

import (
	"context"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

func (h *Handler) loadSettings(ctx context.Context) (*gen.Settings, error) {
	name, err := settings.Get(ctx, h.Settings, settings.InstanceName)
	if err != nil {
		return nil, err
	}
	tmdb, err := h.Settings.Configured(ctx, settings.TMDbAPIKey)
	if err != nil {
		return nil, err
	}
	return &gen.Settings{InstanceName: name, TmdbApiKey: gen.SecretState{Configured: tmdb}}, nil
}

// GetSettings implements GET /settings.
func (h *Handler) GetSettings(ctx context.Context) (*gen.Settings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.loadSettings(ctx)
}

// UpdateSettings implements PATCH /settings.
func (h *Handler) UpdateSettings(ctx context.Context, req *gen.UpdateSettingsRequest) (*gen.Settings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if name, ok := req.InstanceName.Get(); ok {
		if err := settings.Set(ctx, h.Settings, nil, settings.InstanceName, strings.TrimSpace(name)); err != nil {
			return nil, err
		}
	}
	if key, ok := req.TmdbApiKey.Get(); ok {
		if k := strings.TrimSpace(key); k != "" && h.TMDb != nil {
			if err := h.TMDb.Validate(ctx, k); err != nil {
				return nil, err
			}
		}
		if err := h.Settings.SetSecret(ctx, settings.TMDbAPIKey, strings.TrimSpace(key)); err != nil {
			return nil, err
		}
	}
	return h.loadSettings(ctx)
}

func toRootFolder(f rootfolders.Folder) gen.RootFolder {
	out := gen.RootFolder{ID: f.ID, Path: f.Path, Kind: gen.MediaKind(f.Kind), Accessible: f.Accessible,
		Writable: f.Writable, CreatedAt: f.CreatedAt}
	if f.Space != nil {
		out.FreeSpaceBytes = gen.NewOptInt64(f.Space.Free)
		out.TotalSpaceBytes = gen.NewOptInt64(f.Space.Total)
	}
	return out
}

// ListRootFolders implements GET /rootfolders.
func (h *Handler) ListRootFolders(ctx context.Context) ([]gen.RootFolder, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.RootFolders.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.RootFolder, 0, len(list))
	for _, f := range list {
		out = append(out, toRootFolder(f))
	}
	return out, nil
}

// CreateRootFolder implements POST /rootfolders.
func (h *Handler) CreateRootFolder(ctx context.Context, req *gen.CreateRootFolderRequest) (*gen.RootFolder, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	add := h.RootFolders.Add
	if req.Create.Or(false) {
		add = h.RootFolders.Create
	}
	f, err := add(ctx, req.Path, string(req.Kind))
	if err != nil {
		return nil, err
	}
	out := toRootFolder(f)
	return &out, nil
}

// DeleteRootFolder implements DELETE /rootfolders/{id}.
func (h *Handler) DeleteRootFolder(ctx context.Context, params gen.DeleteRootFolderParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.RootFolders.Remove(ctx, params.ID)
}

// BrowseFilesystem implements GET /filesystem.
func (h *Handler) BrowseFilesystem(ctx context.Context, params gen.BrowseFilesystemParams) (*gen.FolderListing, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	l, err := h.RootFolders.Browse(params.Path.Or(""))
	if err != nil {
		return nil, err
	}
	out := &gen.FolderListing{Path: optStr(l.Path), Parent: optStr(l.Parent), Entries: make([]gen.FolderEntry, 0, len(l.Entries))}
	for _, e := range l.Entries {
		out.Entries = append(out.Entries, gen.FolderEntry{Name: e.Name, Path: e.Path})
	}
	return out, nil
}
