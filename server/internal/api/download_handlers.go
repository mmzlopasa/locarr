package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-faster/jx"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// mapDownloadsError maps errors of the download packages (nil = not one of them).
func mapDownloadsError(err error) *StatusError {
	table := []struct {
		err    error
		status int
		code   string
	}{
		{downloads.ErrNotFound, http.StatusNotFound, "not_found"},
		{downloads.ErrNameTaken, http.StatusConflict, "name_taken"},
		{downloads.ErrInvalid, http.StatusBadRequest, "validation"},
		{downloads.ErrNoClient, http.StatusConflict, "no_download_client"},
		{downloads.ErrAlreadyQueued, http.StatusConflict, "already_queued"},
		{downloads.ErrFetch, http.StatusBadGateway, "fetch_failed"},
		{downloaders.ErrAuth, http.StatusBadGateway, "auth_failed"},
		{downloaders.ErrTLS, http.StatusBadGateway, "tls_untrusted"},
		{downloaders.ErrBlocked, http.StatusBadGateway, "address_blocked"},
		{downloaders.ErrWrongURL, http.StatusBadGateway, "wrong_url"},
		{downloaders.ErrUnreachable, http.StatusBadGateway, "unreachable"},
		{downloaders.ErrItemNotFound, http.StatusNotFound, "not_found"},
		{downloaders.ErrUnsupported, http.StatusConflict, "unsupported"},
		{downloaders.ErrInvalidRequest, http.StatusBadRequest, "validation"},
		{downloaders.ErrClient, http.StatusBadGateway, "client_error"},
	}
	for _, t := range table {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return nil
}

func manager(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if !p.Can(auth.PermManageLibrary) {
		return p, errForbidden
	}
	return p, nil
}

func toClientStatus(sn downloads.Snapshot, ok bool) gen.DownloadClientStatus {
	if !ok {
		return gen.DownloadClientStatus{}
	}
	st := gen.DownloadClientStatus{Ok: sn.OK, CheckedAt: gen.NewOptDateTime(sn.CheckedAt), ErrorCode: optStr(sn.ErrorCode),
		Error: optStr(sn.Error), Version: optStr(sn.Version)}
	if sn.FreeSpace != nil {
		st.FreeSpaceBytes = gen.NewOptInt64(*sn.FreeSpace)
	}
	return st
}

func toDownloadClient(c downloads.Client, snaps map[int64]downloads.Snapshot) gen.DownloadClient {
	sn, ok := snaps[c.ID]
	return gen.DownloadClient{ID: c.ID, Name: c.Name, Implementation: c.Implementation, Protocol: gen.DownloadProtocol(c.Protocol),
		Enabled: c.Enabled, Priority: c.Priority, Host: c.Host, Port: c.Port, UseTls: c.UseTLS, AllowSelfSigned: c.AllowSelfSigned,
		UrlBase: c.URLBase, Username: c.Username, Password: gen.SecretState{Configured: c.PasswordSet},
		ApiKey: gen.SecretState{Configured: c.APIKeySet}, Directory: c.Directory,
		Categories:     gen.DownloadClientCategories{Movies: c.CategoryMovies, Series: c.CategorySeries, Music: c.CategoryMusic},
		RecentPriority: gen.QueuePriority(c.RecentPriority), OlderPriority: gen.QueuePriority(c.OlderPriority),
		InitialState: gen.DownloadClientInitialState(c.InitialState), RemoveCompleted: c.RemoveCompleted,
		Sequential: c.Sequential, FirstLastPiece: c.FirstLastPiece, Tags: c.Tags, Status: toClientStatus(sn, ok),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func optSecret(o gen.OptString) *string {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}

func fromClientInput(in *gen.DownloadClientInput) downloads.ClientInput {
	cats := in.Categories.Or(gen.DownloadClientCategories{})
	return downloads.ClientInput{Name: in.Name, Implementation: in.Implementation, Enabled: in.Enabled.Or(true),
		Priority: in.Priority.Or(1), Host: in.Host, Port: in.Port.Or(0), UseTLS: in.UseTls.Or(false),
		AllowSelfSigned: in.AllowSelfSigned.Or(false), URLBase: in.UrlBase.Or(""), Username: in.Username.Or(""),
		Password: optSecret(in.Password), APIKey: optSecret(in.ApiKey), Directory: in.Directory.Or(""),
		CategoryMovies: cats.Movies, CategorySeries: cats.Series, CategoryMusic: cats.Music,
		RecentPriority:  downloaders.Priority(in.RecentPriority.Or(gen.QueuePriorityNormal)),
		OlderPriority:   downloaders.Priority(in.OlderPriority.Or(gen.QueuePriorityNormal)),
		InitialState:    string(in.InitialState.Or(gen.DownloadClientInputInitialStateStart)),
		RemoveCompleted: in.RemoveCompleted.Or(false), Sequential: in.Sequential.Or(false),
		FirstLastPiece: in.FirstLastPiece.Or(false), Tags: in.Tags}
}

// ListDownloadClients implements GET /downloadclients.
func (h *Handler) ListDownloadClients(ctx context.Context) ([]gen.DownloadClient, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Downloads.Clients(ctx)
	if err != nil {
		return nil, err
	}
	snaps := h.Downloads.Snapshots()
	out := make([]gen.DownloadClient, 0, len(list))
	for _, c := range list {
		out = append(out, toDownloadClient(c, snaps))
	}
	return out, nil
}

// GetDownloadClient implements GET /downloadclients/{id}.
func (h *Handler) GetDownloadClient(ctx context.Context, params gen.GetDownloadClientParams) (*gen.DownloadClient, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	c, err := h.Downloads.GetClient(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := toDownloadClient(c, h.Downloads.Snapshots())
	return &out, nil
}

// CreateDownloadClient implements POST /downloadclients.
func (h *Handler) CreateDownloadClient(ctx context.Context, req *gen.DownloadClientInput) (*gen.DownloadClient, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	c, err := h.Downloads.CreateClient(ctx, fromClientInput(req))
	if err != nil {
		return nil, err
	}
	h.Downloads.Kick()
	out := toDownloadClient(c, nil)
	return &out, nil
}

// UpdateDownloadClient implements PUT /downloadclients/{id}.
func (h *Handler) UpdateDownloadClient(ctx context.Context, req *gen.DownloadClientInput, params gen.UpdateDownloadClientParams) (*gen.DownloadClient, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	c, err := h.Downloads.UpdateClient(ctx, params.ID, fromClientInput(req))
	if err != nil {
		return nil, err
	}
	h.Downloads.Kick()
	out := toDownloadClient(c, nil)
	return &out, nil
}

// DeleteDownloadClient implements DELETE /downloadclients/{id}.
func (h *Handler) DeleteDownloadClient(ctx context.Context, params gen.DeleteDownloadClientParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Downloads.DeleteClient(ctx, params.ID)
}

// ListDownloadClientImplementations implements GET /downloadclients/implementations.
func (h *Handler) ListDownloadClientImplementations(ctx context.Context) ([]gen.DownloadClientImplementation, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	impls := downloaders.Implementations()
	out := make([]gen.DownloadClientImplementation, 0, len(impls))
	for _, i := range impls {
		fields := make([]gen.DownloadClientField, 0, len(i.Fields))
		for _, f := range i.Fields {
			fields = append(fields, gen.DownloadClientField(f))
		}
		req := make([]gen.DownloadClientField, 0, len(i.Required))
		for _, f := range i.Required {
			req = append(req, gen.DownloadClientField(f))
		}
		c := i.Capabilities
		out = append(out, gen.DownloadClientImplementation{Name: i.Name, DisplayName: i.DisplayName,
			Protocol: gen.DownloadProtocol(i.Protocol), DefaultPort: i.DefaultPort, DefaultUrlBase: i.DefaultURLBase,
			DefaultUsername: i.DefaultUser, Fields: fields, Required: req,
			Capabilities: gen.DownloadClientCapabilities{Categories: c.Categories, CreateCategories: c.CreateCategories,
				DirectoryCategories: c.DirectoryCategories, Tags: c.Tags, SeedLimits: c.SeedLimits, Sequential: c.Sequential,
				FirstLastPiece: c.FirstLastPiece, Priority: c.Priority, SavePath: c.SavePath, FreeSpace: c.FreeSpace,
				DeletesData: c.DeletesData}})
	}
	return out, nil
}

// TestDownloadClient implements POST /downloadclients/test.
func (h *Handler) TestDownloadClient(ctx context.Context, req *gen.DownloadClientTestRequest) (*gen.DownloadClientTestResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	res, err := h.Downloads.Test(ctx, req.ID.Or(0), fromClientInput(&req.Client))
	if err != nil {
		return nil, err
	}
	out := &gen.DownloadClientTestResult{Ok: res.OK, ErrorCode: optStr(res.ErrorCode), Message: optStr(res.Message),
		Version: optStr(res.Version), MissingCategories: res.MissingCategories, CanCreateCategories: res.CanCreate}
	if out.MissingCategories == nil {
		out.MissingCategories = []string{}
	}
	if res.FreeSpace != nil {
		out.FreeSpaceBytes = gen.NewOptInt64(*res.FreeSpace)
	}
	return out, nil
}

// CreateDownloadClientCategories implements POST /downloadclients/{id}/categories.
func (h *Handler) CreateDownloadClientCategories(ctx context.Context, params gen.CreateDownloadClientCategoriesParams) (*gen.CreatedCategories, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	cats, err := h.Downloads.CreateCategories(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return &gen.CreatedCategories{Categories: cats}, nil
}

func (h *Handler) downloadSettings(ctx context.Context) (*gen.DownloadSettings, error) {
	poll, err := settings.Get(ctx, h.Settings, downloads.PollSeconds)
	if err != nil {
		return nil, err
	}
	stalled, err := settings.Get(ctx, h.Settings, downloads.StalledHours)
	if err != nil {
		return nil, err
	}
	return &gen.DownloadSettings{PollSeconds: poll, StalledHours: stalled}, nil
}

// GetDownloadSettings implements GET /downloadclients/settings.
func (h *Handler) GetDownloadSettings(ctx context.Context) (*gen.DownloadSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.downloadSettings(ctx)
}

// UpdateDownloadSettings implements PATCH /downloadclients/settings.
func (h *Handler) UpdateDownloadSettings(ctx context.Context, req *gen.UpdateDownloadSettings) (*gen.DownloadSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if v, ok := req.PollSeconds.Get(); ok {
		if err := settings.Set(ctx, h.Settings, nil, downloads.PollSeconds, v); err != nil {
			return nil, err
		}
	}
	if v, ok := req.StalledHours.Get(); ok {
		if err := settings.Set(ctx, h.Settings, nil, downloads.StalledHours, v); err != nil {
			return nil, err
		}
	}
	h.Downloads.Kick()
	return h.downloadSettings(ctx)
}

func toMapping(m downloads.Mapping) gen.RemotePathMapping {
	return gen.RemotePathMapping{ID: m.ID, Host: m.Host, RemotePath: m.RemotePath, LocalPath: m.LocalPath}
}

// ListRemotePathMappings implements GET /remotepathmappings.
func (h *Handler) ListRemotePathMappings(ctx context.Context) ([]gen.RemotePathMapping, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Downloads.Mappings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.RemotePathMapping, 0, len(list))
	for _, m := range list {
		out = append(out, toMapping(m))
	}
	return out, nil
}

// CreateRemotePathMapping implements POST /remotepathmappings.
func (h *Handler) CreateRemotePathMapping(ctx context.Context, req *gen.RemotePathMappingInput) (*gen.RemotePathMapping, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	m, err := h.Downloads.CreateMapping(ctx, downloads.Mapping{Host: req.Host, RemotePath: req.RemotePath, LocalPath: req.LocalPath})
	if err != nil {
		return nil, err
	}
	out := toMapping(m)
	return &out, nil
}

// UpdateRemotePathMapping implements PUT /remotepathmappings/{id}.
func (h *Handler) UpdateRemotePathMapping(ctx context.Context, req *gen.RemotePathMappingInput, params gen.UpdateRemotePathMappingParams) (*gen.RemotePathMapping, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	m, err := h.Downloads.UpdateMapping(ctx, downloads.Mapping{ID: params.ID, Host: req.Host, RemotePath: req.RemotePath, LocalPath: req.LocalPath})
	if err != nil {
		return nil, err
	}
	out := toMapping(m)
	return &out, nil
}

// DeleteRemotePathMapping implements DELETE /remotepathmappings/{id}.
func (h *Handler) DeleteRemotePathMapping(ctx context.Context, params gen.DeleteRemotePathMappingParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Downloads.DeleteMapping(ctx, params.ID)
}

func rawJSON(b []byte) jx.Raw {
	if len(b) == 0 || !json.Valid(b) {
		return nil
	}
	return jx.Raw(b)
}

func toQueueItem(q downloads.QueueItem) gen.QueueItem {
	d := q.Download
	out := gen.QueueItem{ID: d.ID, Unknown: q.Unknown, ClientName: d.ClientName, ClientItemId: d.ClientItemID,
		Protocol: gen.DownloadProtocol(d.Protocol), Title: d.Title, Indexer: optStr(d.Indexer), MediaKind: optStr(d.MediaKind),
		Category: d.Category, Status: gen.DownloadStatus(d.Status), State: gen.DownloadState(d.State), RawState: d.RawState,
		Progress: d.Progress, SizeBytes: d.Size, DownloadedBytes: d.Downloaded, DownloadSpeed: d.DownloadSpeed,
		UploadSpeed: d.UploadSpeed, Ratio: d.Ratio, SeedingSeconds: int64(d.SeedingTime / time.Second),
		ContentPath: optStr(d.ContentPath), LocalPath: optStr(d.LocalPath), Message: d.Message, AddedAt: d.AddedAt,
		UpdatedAt: d.UpdatedAt, CompletedAt: optTime(d.CompletedAt)}
	if !q.Unknown {
		out.ItemRef = rawJSON(d.ItemRef)
	}
	if !downloaders.State(out.State).Valid() {
		out.State = gen.DownloadStateQueued
	}
	if d.ClientID != nil {
		out.ClientId = gen.NewOptInt64(*d.ClientID)
	}
	if d.ETA != nil {
		out.EtaSeconds = gen.NewOptInt64(int64(*d.ETA / time.Second))
	}
	return out
}

// GetQueue implements GET /queue.
func (h *Handler) GetQueue(ctx context.Context, params gen.GetQueueParams) (*gen.Queue, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	h.Downloads.Watch()
	items, err := h.Downloads.Queue(ctx, params.IncludeUnknown.Or(true))
	if err != nil {
		return nil, err
	}
	clients, err := h.Downloads.Clients(ctx)
	if err != nil {
		return nil, err
	}
	snaps := h.Downloads.Snapshots()
	out := &gen.Queue{Items: make([]gen.QueueItem, 0, len(items)), Clients: make([]gen.QueueClient, 0, len(clients))}
	hide := h.hidesAdult(ctx)
	for _, it := range items {
		if hide && isAdultDownload(it.Download.ItemRef) {
			continue
		}
		out.Items = append(out.Items, toQueueItem(it))
	}
	{
		ptrs := make([]*gen.QueueItem, len(out.Items))
		for i := range out.Items {
			ptrs[i] = &out.Items[i]
		}
		h.linkItems(ctx, ptrs)
	}
	for _, c := range clients {
		sn, ok := snaps[c.ID]
		qc := gen.QueueClient{ID: c.ID, Name: c.Name, Protocol: gen.DownloadProtocol(c.Protocol), Enabled: c.Enabled,
			Status: toClientStatus(sn, ok)}
		if impl, ok := downloaders.Lookup(c.Implementation); ok {
			qc.CanPrioritise = gen.NewOptBool(impl.Capabilities.Priority)
		}
		out.Clients = append(out.Clients, qc)
	}
	return out, nil
}

// RefreshQueue implements POST /queue/refresh.
func (h *Handler) RefreshQueue(ctx context.Context) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	h.Downloads.Watch()
	h.Downloads.Kick()
	return nil
}

// GrabRelease implements POST /queue/grab.
func (h *Handler) GrabRelease(ctx context.Context, req *gen.GrabRequest) (*gen.QueueItem, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	g := downloads.GrabRequest{DownloadURL: req.DownloadUrl.Or(""), DownloadToken: req.DownloadToken.Or(""), Protocol: downloaders.Protocol(req.Protocol), Title: req.Title,
		Indexer: req.Indexer.Or(""), GUID: req.GUID.Or(""), Tags: req.Tags}
	if len(req.ItemRef) > 0 {
		g.ItemRef = json.RawMessage(req.ItemRef)
	}
	if v, ok := req.MediaKind.Get(); ok {
		g.MediaKind = string(v)
	}
	if v, ok := req.IndexerId.Get(); ok {
		g.IndexerID = &v
	}
	if v, ok := req.ClientId.Get(); ok {
		g.ClientID = &v
	}
	if v, ok := req.SeedRatio.Get(); ok {
		g.SeedRatio = &v
	}
	if v, ok := req.SeedTimeMinutes.Get(); ok {
		d := time.Duration(v) * time.Minute
		g.SeedTime = &d
	}
	if v, ok := req.PublishedAt.Get(); ok {
		g.PublishedAt = &v
	}
	d, err := h.Downloads.Grab(ctx, g)
	if err != nil {
		return nil, err
	}
	out := toQueueItem(downloads.QueueItem{Download: d})
	return &out, nil
}

// PauseQueueItem implements POST /queue/{id}/pause.
func (h *Handler) PauseQueueItem(ctx context.Context, params gen.PauseQueueItemParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Downloads.Pause(ctx, params.ID)
}

// ResumeQueueItem implements POST /queue/{id}/resume.
func (h *Handler) ResumeQueueItem(ctx context.Context, params gen.ResumeQueueItemParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Downloads.Resume(ctx, params.ID)
}

// SetQueueItemPriority implements POST /queue/{id}/priority.
func (h *Handler) SetQueueItemPriority(ctx context.Context, req *gen.PriorityRequest, params gen.SetQueueItemPriorityParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Downloads.SetPriority(ctx, params.ID, downloaders.Priority(req.Priority))
}

// RemoveQueueItem implements DELETE /queue/{id}.
func (h *Handler) RemoveQueueItem(ctx context.Context, params gen.RemoveQueueItemParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Downloads.Remove(ctx, params.ID, downloads.RemoveOptions{DeleteData: params.DeleteData.Or(false),
		Blocklist: params.Blocklist.Or(false), Message: params.Reason.Or("")})
}

// ListQueueItemEvents implements GET /queue/{id}/events.
func (h *Handler) ListQueueItemEvents(ctx context.Context, params gen.ListQueueItemEventsParams) ([]gen.DownloadEvent, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if d, err := h.Downloads.Get(ctx, params.ID); err != nil {
		return nil, err
	} else if isAdultDownload(d.ItemRef) && h.hidesAdult(ctx) {
		return nil, errAdultHidden
	}
	evs, err := h.Downloads.Events(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.DownloadEvent, 0, len(evs))
	for _, e := range evs {
		out = append(out, gen.DownloadEvent{ID: e.ID, DownloadId: e.DownloadID, At: e.At, Type: string(e.Type),
			FromState: e.FromState, ToState: e.ToState, Message: e.Message, Data: rawJSON(e.Data)})
	}
	return out, nil
}

func toBlock(e downloads.BlockEntry) gen.BlocklistEntry {
	out := gen.BlocklistEntry{ID: e.ID, Title: e.Title, Protocol: e.Protocol, Indexer: e.Indexer, InfoHash: e.InfoHash,
		GUID: e.GUID, Reason: e.Reason, Message: e.Message, ItemRef: rawJSON(e.ItemRef), CreatedAt: e.CreatedAt}
	if e.IndexerID != nil {
		out.IndexerId = gen.NewOptInt64(*e.IndexerID)
	}
	if e.DownloadID != nil {
		out.DownloadId = gen.NewOptInt64(*e.DownloadID)
	}
	return out
}

// ListBlocklist implements GET /queue/blocklist.
func (h *Handler) ListBlocklist(ctx context.Context, params gen.ListBlocklistParams) ([]gen.BlocklistEntry, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Downloads.Blocklist(ctx, params.Limit.Or(500))
	if err != nil {
		return nil, err
	}
	out := make([]gen.BlocklistEntry, 0, len(list))
	hide := h.hidesAdult(ctx)
	for _, e := range list {
		if hide && isAdultDownload(e.ItemRef) {
			continue
		}
		out = append(out, toBlock(e))
	}
	return out, nil
}

// AddBlocklistEntry implements POST /queue/blocklist.
func (h *Handler) AddBlocklistEntry(ctx context.Context, req *gen.BlocklistInput) (*gen.BlocklistEntry, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	e := downloads.BlockEntry{Title: req.Title, Indexer: req.Indexer.Or(""), InfoHash: req.InfoHash.Or(""), GUID: req.GUID.Or(""),
		Reason: req.Reason.Or(downloads.ReasonManual), Message: req.Message.Or("")}
	if v, ok := req.Protocol.Get(); ok {
		e.Protocol = string(v)
	}
	if v, ok := req.IndexerId.Get(); ok {
		e.IndexerID = &v
	}
	if len(req.ItemRef) > 0 {
		e.ItemRef = json.RawMessage(req.ItemRef)
	}
	added, err := h.Downloads.AddBlock(ctx, e)
	if err != nil {
		return nil, err
	}
	out := toBlock(added)
	return &out, nil
}

// DeleteBlocklistEntry implements DELETE /queue/blocklist/{id}.
func (h *Handler) DeleteBlocklistEntry(ctx context.Context, params gen.DeleteBlocklistEntryParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	return h.Downloads.DeleteBlock(ctx, params.ID)
}
