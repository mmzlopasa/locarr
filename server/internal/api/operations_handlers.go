package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-faster/jx"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/backup"
	"github.com/mmzlopasa/locarr/server/internal/board"
	"github.com/mmzlopasa/locarr/server/internal/health"
	"github.com/mmzlopasa/locarr/server/internal/history"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/logging"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/stats"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// RestoreConfirmation must be typed to restore a backup.
const RestoreConfirmation = "RESTORE"

func mapOperationsError(err error) *StatusError {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		return &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, backup.ErrNewerSchema):
		return &StatusError{Status: http.StatusConflict, Code: "backup_newer", Message: err.Error()}
	case errors.Is(err, backup.ErrKeyMismatch):
		return &StatusError{Status: http.StatusBadRequest, Code: "backup_key_mismatch", Message: err.Error()}
	case errors.Is(err, backup.ErrInvalid):
		return &StatusError{Status: http.StatusBadRequest, Code: "backup_invalid", Message: err.Error()}
	}
	return nil
}

func reasons(list []timeline.ReasonCount) []gen.ReasonCount {
	out := make([]gen.ReasonCount, 0, len(list))
	for _, r := range list {
		g, err := convert[gen.ReasonCount](r)
		if err != nil {
			g = gen.ReasonCount{Code: r.Code, Count: r.Count, Example: r.Example}
		}
		out = append(out, g)
	}
	return out
}

// GetBoard implements GET /board.
func (h *Handler) GetBoard(ctx context.Context, p gen.GetBoardParams) (*gen.BoardSummary, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	sum, err := h.Board.Summarize(ctx, string(p.Kind.Or("")))
	if err != nil {
		return nil, err
	}
	out := &gen.BoardSummary{Total: sum.Total, Groups: make([]gen.BoardGroup, 0, len(sum.Groups))}
	if !sum.UpdatedAt.IsZero() {
		out.UpdatedAt = gen.NewOptDateTime(sum.UpdatedAt)
	}
	for _, g := range sum.Groups {
		out.Groups = append(out.Groups, gen.BoardGroup{Code: g.Code, Tone: gen.BoardGroupTone(g.Tone), Fix: g.Fix, Count: g.Count, Searches: g.Searches,
			Kinds: gen.BoardKindCounts{Movie: g.Kinds[board.KindMovie], Episode: g.Kinds[board.KindEpisode], Album: g.Kinds[board.KindAlbum],
				Subtitle: g.Kinds[board.KindSubtitle]}, Reasons: reasons(g.Reasons)})
	}
	return out, nil
}

// ListBoardItems implements GET /board/items.
func (h *Handler) ListBoardItems(ctx context.Context, p gen.ListBoardItemsParams) (*gen.BoardItemPage, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, total, err := h.Board.List(ctx, board.ListFilter{Code: p.Code.Or(""), Fix: p.Fix.Or(""), Kind: string(p.Kind.Or("")),
		Query: p.Q.Or(""), Offset: p.Offset.Or(0), Limit: p.Limit.Or(100)})
	if err != nil {
		return nil, err
	}
	out := &gen.BoardItemPage{Total: total, Items: make([]gen.BoardItem, 0, len(list))}
	for _, it := range list {
		g := gen.BoardItem{Key: it.Key, Kind: gen.BoardKind(it.Kind), ItemRef: it.ItemRef, ParentRef: it.ParentRef, Title: it.Title, Code: it.Code,
			Tone: gen.BoardItemTone(it.Tone), Fix: it.Fix, Message: it.Message, Params: gen.BoardItemParams(rawMap(it.Params)),
			Reasons: reasons(it.Reasons), Searches: it.Searches, UpdatedAt: it.UpdatedAt}
		if it.DownloadID != nil {
			g.DownloadId = gen.NewOptInt64(*it.DownloadID)
		}
		if it.QualityProfileID > 0 {
			g.QualityProfileId = gen.NewOptInt64(it.QualityProfileID)
		}
		out.Items = append(out.Items, g)
	}
	return out, nil
}

// BoardAction implements POST /board/actions.
func (h *Handler) BoardAction(ctx context.Context, req *gen.BoardActionRequest) (*gen.BoardActionResult, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	if req.Action == gen.BoardActionRequestActionChangeProfile {
		id, ok := req.QualityProfileId.Get()
		if !ok {
			return nil, &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: "qualityProfileId is required"}
		}
		if _, err := h.Quality.Profile(ctx, id); err != nil {
			return nil, err
		}
	}
	done, failed := h.Board.Apply(ctx, h.BoardActions, string(req.Action), req.Keys, req.QualityProfileId.Or(0))
	out := &gen.BoardActionResult{Done: done, Failed: make([]gen.BoardActionFailure, 0, len(failed))}
	for _, f := range failed {
		msg := f.Err.Error()
		if se := mapError(f.Err); se != nil {
			msg = se.Message
		}
		out.Failed = append(out.Failed, gen.BoardActionFailure{Key: f.Key, Code: f.Code, Message: msg})
	}
	return out, nil
}

// RefreshBoard implements POST /board/refresh.
func (h *Handler) RefreshBoard(ctx context.Context) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	_, _, err := h.Jobs.Enqueue(ctx, "board-rebuild", nil, jobsUnique("board-rebuild"))
	return err
}

// ListHistory implements GET /history.
func (h *Handler) ListHistory(ctx context.Context, p gen.ListHistoryParams) (*gen.HistoryPage, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, more, err := h.History.List(ctx, history.Filter{Category: string(p.Category.Or("")), Kind: string(p.Kind.Or("")), Query: p.Q.Or(""),
		Offset: p.Offset.Or(0), Limit: p.Limit.Or(50)})
	if err != nil {
		return nil, err
	}
	keep, err := settings.Get(ctx, h.Settings, settings.HistoryDays)
	if err != nil {
		return nil, err
	}
	out := &gen.HistoryPage{HasMore: more, Items: make([]gen.HistoryEntry, 0, len(list)), RetentionDays: gen.NewOptInt(keep)}
	for _, e := range list {
		g := gen.HistoryEntry{ID: e.ID, At: e.At, Category: gen.HistoryCategory(e.Category), Type: e.Type, ItemRef: e.ItemRef, Title: e.Title,
			Message: e.Message, Release: optStr(e.Release), Indexer: optStr(e.Indexer), Client: optStr(e.Client),
			ItemTitle: gen.NewOptString(e.ItemTitle), ItemGone: gen.NewOptBool(e.ItemGone), Data: jx.Raw(e.Data)}
		switch e.Kind {
		case "movie", "series", "music":
			g.Kind = gen.NewOptHistoryEntryKind(gen.HistoryEntryKind(e.Kind))
		}
		if e.DownloadID != nil {
			g.DownloadId = gen.NewOptInt64(*e.DownloadID)
		}
		out.Items = append(out.Items, g)
	}
	return out, nil
}

// DeleteHistoryEntry implements DELETE /history/{id}.
func (h *Handler) DeleteHistoryEntry(ctx context.Context, p gen.DeleteHistoryEntryParams) error {
	if _, err := manager(ctx); err != nil {
		return err
	}
	if item, ok := p.Item.Get(); ok {
		ref, err := h.timelineRef(ctx, item)
		if err != nil {
			return err
		}
		return h.History.DeleteItemEntry(ctx, ref, p.ID)
	}
	return h.History.Delete(ctx, p.ID)
}

// timelineRef checks an item ref for deleting timeline rows (D-109): adult
// refs only for callers who may see adult content (D-103), otherwise the
// same 404 as an unknown entry.
func (h *Handler) timelineRef(ctx context.Context, item string) (string, error) {
	ref, err := library.ParseRef(item)
	if err != nil {
		return "", history.ErrNotFound
	}
	if library.IsAdult(ref.Kind) {
		if h.Adult == nil {
			return "", history.ErrNotFound
		}
		if _, err := h.adultManager(ctx); err != nil {
			return "", history.ErrNotFound
		}
	}
	return ref.String(), nil
}

// ClearHistory implements POST /history/clear.
func (h *Handler) ClearHistory(ctx context.Context, req *gen.HistoryClearRequest) (*gen.HistoryClearResult, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	var before time.Time
	if d, ok := req.OlderThanDays.Get(); ok {
		before = time.Now().Add(-time.Duration(d) * 24 * time.Hour)
	}
	if item, ok := req.Item.Get(); ok {
		ref, err := h.timelineRef(ctx, item)
		if err != nil {
			return nil, err
		}
		f := history.ItemFilter{Ref: ref, IDs: req.Ids, Before: before}
		if req.DryRun.Or(false) {
			n, err := h.History.CountItem(ctx, f)
			return &gen.HistoryClearResult{Count: n}, err
		}
		n, err := h.History.ClearItem(ctx, f)
		if err != nil {
			return nil, err
		}
		return &gen.HistoryClearResult{Count: n, Deleted: true}, nil
	}
	f := history.Filter{Category: string(req.Category.Or("")), Kind: string(req.Kind.Or("")), Query: req.Q.Or(""), Before: before}
	if req.DryRun.Or(false) {
		n, err := h.History.Count(ctx, f)
		return &gen.HistoryClearResult{Count: n}, err
	}
	n, err := h.History.Clear(ctx, f)
	if err != nil {
		return nil, err
	}
	return &gen.HistoryClearResult{Count: n, Deleted: true}, nil
}

func sourceStats(list []history.SourceStats) []gen.SourceStats {
	out := make([]gen.SourceStats, 0, len(list))
	for _, s := range list {
		g := gen.SourceStats{Name: s.Name, Grabs: s.Grabs, Imported: s.Imported, Failed: s.Failed, InProgress: s.InProgress}
		if s.ID != nil {
			g.ID = gen.NewOptInt64(*s.ID)
		}
		if s.SuccessRate != nil {
			g.SuccessRate = gen.NewOptFloat64(*s.SuccessRate)
		}
		if s.AvgImport != nil {
			g.AvgImportSeconds = gen.NewOptInt64(int64(s.AvgImport.Seconds()))
		}
		out = append(out, g)
	}
	return out
}

// GetActivityStats implements GET /activity/stats.
func (h *Handler) GetActivityStats(ctx context.Context, p gen.GetActivityStatsParams) (*gen.ActivityStats, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	days := p.Days.Or(30)
	ix, cl, err := h.History.Stats(ctx, time.Now().AddDate(0, 0, -days))
	if err != nil {
		return nil, err
	}
	return &gen.ActivityStats{Days: days, Indexers: sourceStats(ix), Clients: sourceStats(cl)}, nil
}

// ClearBlocklist implements DELETE /queue/blocklist.
func (h *Handler) ClearBlocklist(ctx context.Context) (*gen.BlocklistCleared, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	res, err := h.DB.W.ExecContext(ctx, `DELETE FROM blocklist`)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	return &gen.BlocklistCleared{Count: int(n)}, nil
}

func healthReport(rep health.Report, ran bool) *gen.HealthReport {
	out := &gen.HealthReport{Issues: make([]gen.HealthIssue, 0, len(rep.Issues))}
	if ran {
		out.CheckedAt = gen.NewOptDateTime(rep.CheckedAt)
	}
	for _, is := range rep.Issues {
		out.Issues = append(out.Issues, gen.HealthIssue{Key: is.Key, Check: is.Check, Severity: gen.HealthSeverity(is.Severity), Code: is.Code,
			Message: is.Message, Params: gen.HealthIssueParams(rawMap(is.Params)), Docs: is.Docs, FixRoute: optStr(is.FixRoute), Since: is.Since})
	}
	return out
}

// GetHealth implements GET /system/health (runs the checks the first time).
func (h *Handler) GetHealth(ctx context.Context) (*gen.HealthReport, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	rep, ran := h.Health.Last()
	if !ran {
		rep, ran = h.Health.Run(ctx), true
	}
	return healthReport(rep, ran), nil
}

// RunHealthChecks implements POST /system/health/run.
func (h *Handler) RunHealthChecks(ctx context.Context) (*gen.HealthReport, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return healthReport(h.Health.Run(ctx), true), nil
}

// GetStats implements GET /system/stats (and /api/stats).
func (h *Handler) GetStats(ctx context.Context) (*gen.Stats, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	st, err := h.Stats.Get(ctx)
	if err != nil {
		return nil, err
	}
	out := &gen.Stats{Version: st.Version, Commit: st.Commit, GeneratedAt: st.GeneratedAt, Movies: st.Movies, MoviesHave: st.MoviesHave,
		MoviesMissing: st.MoviesMissing, Series: st.Series, Episodes: st.Episodes, EpisodesHave: st.EpisodesHave, EpisodesMissing: st.EpisodesMissing,
		Artists: st.Artists, Albums: st.Albums, AlbumsHave: st.AlbumsHave, AlbumsMissing: st.AlbumsMissing, QueueSize: st.QueueSize,
		ActiveDownloads: st.ActiveDownloads, DownloadSpeedBytes: st.DownloadSpeedBytes, WantedMovies: st.WantedMovies,
		WantedEpisodes: st.WantedEpisodes, WantedAlbums: st.WantedAlbums, FailedImports: st.FailedImports, HealthErrors: st.HealthErrors,
		HealthWarnings: st.HealthWarnings, HealthInfo: st.HealthInfo, Indexers: st.Indexers, IndexersHealthy: st.IndexersHealthy,
		IndexersFailing: st.IndexersFailing, IndexersDisabled: st.IndexersDisabled, RequestsPending: st.RequestsPending,
		SubtitlesMissing: st.SubtitlesMissing, RootFolders: make([]gen.StatsRootFolder, 0, len(st.RootFolders))}
	if st.LastRSSSyncAt != nil {
		out.LastRssSyncAt = gen.NewOptDateTime(*st.LastRSSSyncAt)
	}
	if st.AdultItems != nil {
		out.AdultItems = gen.NewOptInt(*st.AdultItems)
	}
	for _, r := range st.RootFolders {
		g := gen.StatsRootFolder{Path: r.Path, Kind: gen.MediaKind(r.Kind), Accessible: r.Accessible}
		if r.FreeBytes != nil {
			g.FreeBytes = gen.NewOptInt64(*r.FreeBytes)
		}
		if r.TotalBytes != nil {
			g.TotalBytes = gen.NewOptInt64(*r.TotalBytes)
		}
		out.RootFolders = append(out.RootFolders, g)
	}
	return out, nil
}

// Metrics serves Prometheus metrics at /metrics when a metrics token is set
// (Authorization: Bearer <token>); otherwise it does not exist.
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	token, ok, err := h.Settings.Secret(r.Context(), settings.MetricsToken)
	if err != nil || !ok || token == "" {
		WriteError(w, http.StatusNotFound, "not_found", "metrics are off (Settings → Monitoring)")
		return
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="locarr metrics"`)
		WriteError(w, http.StatusUnauthorized, "unauthorized", "wrong or missing metrics token")
		return
	}
	st, err := h.Stats.Get(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = stats.WritePrometheus(w, st)
}

func toLogEntry(e logging.Entry) gen.LogEntry {
	lvl := e.Level
	switch lvl {
	case "debug", "info", "warn", "error":
	default:
		lvl = "info"
	}
	f := gen.LogEntryFields{}
	for k, v := range e.Fields {
		if b, err := json.Marshal(v); err == nil {
			f[k] = jx.Raw(b)
		}
	}
	return gen.LogEntry{Seq: e.Seq, Time: e.Time, Level: gen.LogLevel(lvl), Message: e.Message, Fields: f}
}

func (h *Handler) ring() *logging.Ring {
	if h.Logs == nil {
		return nil
	}
	return h.Logs.Ring
}

// ListLogs implements GET /system/logs.
func (h *Handler) ListLogs(ctx context.Context, p gen.ListLogsParams) ([]gen.LogEntry, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	out := []gen.LogEntry{}
	r := h.ring()
	if r == nil {
		return out, nil
	}
	for _, e := range r.Entries(logging.Filter{MinLevel: string(p.Level.Or("")), Query: p.Q.Or("")}, p.Limit.Or(500)) {
		out = append(out, toLogEntry(e))
	}
	return out, nil
}

// StreamLogs implements GET /system/logs/stream (server-sent events).
func (h *Handler) StreamLogs(ctx context.Context, p gen.StreamLogsParams) (gen.StreamLogsOK, error) {
	if _, err := admin(ctx); err != nil {
		return gen.StreamLogsOK{}, err
	}
	r := h.ring()
	if r == nil {
		return gen.StreamLogsOK{Data: strings.NewReader(": no live log\n\n")}, nil
	}
	f := logging.Filter{MinLevel: string(p.Level.Or(""))}
	ch, stop := r.Subscribe()
	pr, pw := io.Pipe()
	go func() {
		defer stop()
		if _, err := pw.Write([]byte(": live log\n\n")); err != nil {
			return
		}
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = pw.Close()
				return
			case <-ping.C:
				if _, err := pw.Write([]byte(": ping\n\n")); err != nil {
					return
				}
			case e := <-ch:
				if !f.Match(e) {
					continue
				}
				g := toLogEntry(e)
				enc := &jx.Encoder{}
				g.Encode(enc)
				var b bytes.Buffer
				b.WriteString("event: entry\ndata: ")
				b.Write(enc.Bytes())
				b.WriteString("\n\n")
				if _, err := pw.Write(b.Bytes()); err != nil {
					return
				}
			}
		}
	}()
	return gen.StreamLogsOK{Data: pr}, nil
}

// ListLogFiles implements GET /system/logs/files.
func (h *Handler) ListLogFiles(ctx context.Context) ([]gen.LogFile, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	out := []gen.LogFile{}
	if h.Logs == nil || h.Logs.Dir == "" {
		return out, nil
	}
	files, err := logging.Files(h.Logs.Dir)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		out = append(out, gen.LogFile{Name: f.Name, Size: f.Size, ModifiedAt: f.ModifiedAt})
	}
	return out, nil
}

// DownloadLogFile implements GET /system/logs/files/{name}.
func (h *Handler) DownloadLogFile(ctx context.Context, p gen.DownloadLogFileParams) (*gen.DownloadLogFileOKHeaders, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	notFound := &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: "no such log file"}
	if h.Logs == nil || h.Logs.Dir == "" {
		return nil, notFound
	}
	f, err := logging.OpenFile(h.Logs.Dir, p.Name)
	if err != nil {
		return nil, notFound
	}
	return &gen.DownloadLogFileOKHeaders{ContentDisposition: gen.NewOptString(`attachment; filename="` + p.Name + `"`),
		Response: gen.DownloadLogFileOK{Data: f}}, nil
}

func toBackup(b backup.Info) gen.Backup {
	return gen.Backup{Name: b.Name, Kind: gen.BackupKind(b.Kind), Size: b.Size, CreatedAt: b.CreatedAt, Version: optStr(b.Version)}
}

// ListBackups implements GET /system/backups.
func (h *Handler) ListBackups(ctx context.Context) ([]gen.Backup, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Backups.List()
	if err != nil {
		return nil, err
	}
	out := make([]gen.Backup, 0, len(list))
	for _, b := range list {
		out = append(out, toBackup(b))
	}
	return out, nil
}

// CreateBackup implements POST /system/backups.
func (h *Handler) CreateBackup(ctx context.Context) (*gen.Backup, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	b, err := h.Backups.Create(ctx, backup.KindManual)
	if err != nil {
		return nil, err
	}
	out := toBackup(b)
	return &out, nil
}

// DownloadBackup implements GET /system/backups/{name}.
func (h *Handler) DownloadBackup(ctx context.Context, p gen.DownloadBackupParams) (*gen.DownloadBackupOKHeaders, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	path, err := h.Backups.Path(p.Name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) // #nosec G304 -- name checked by backup.Path
	if err != nil {
		return nil, err
	}
	return &gen.DownloadBackupOKHeaders{ContentDisposition: gen.NewOptString(`attachment; filename="` + p.Name + `"`),
		Response: gen.DownloadBackupOK{Data: f}}, nil
}

// DeleteBackup implements DELETE /system/backups/{name}.
func (h *Handler) DeleteBackup(ctx context.Context, p gen.DeleteBackupParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Backups.Delete(p.Name)
}

func checkConfirm(c string) error {
	if strings.TrimSpace(c) != RestoreConfirmation {
		return &StatusError{Status: http.StatusBadRequest, Code: "confirmation_required", Message: "type RESTORE to confirm the restore"}
	}
	return nil
}

// restore stages a backup from the backup folder, backs up the current
// state first and asks the app to restart.
func (h *Handler) restore(ctx context.Context, name string) (*gen.RestoreStarted, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	if h.Restart == nil {
		return nil, &StatusError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "restore is not available in this mode"}
	}
	keep, err := backup.RestorerSession(ctx, h.DB, p.SessionID)
	if err != nil {
		return nil, err
	}
	if _, err := h.Backups.Stage(ctx, name, h.MaxSchema, keep); err != nil {
		return nil, err
	}
	pre, err := h.Backups.Create(ctx, backup.KindPreRestore)
	if err != nil {
		_ = backup.CancelPending(h.Backups.ConfigDir)
		return nil, err
	}
	h.Log.Warn("restoring a backup", "backup", name, "by", p.User.Username, "pre_restore_backup", pre.Name)
	// After this answer is sent: the restart waits for running requests.
	go func() {
		time.Sleep(200 * time.Millisecond)
		h.Restart()
	}()
	return &gen.RestoreStarted{Restarting: true, PreRestoreBackup: gen.NewOptString(pre.Name)}, nil
}

// RestoreBackup implements POST /system/backups/{name}/restore.
func (h *Handler) RestoreBackup(ctx context.Context, p gen.RestoreBackupParams) (*gen.RestoreStarted, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if err := checkConfirm(p.Confirm); err != nil {
		return nil, err
	}
	return h.restore(ctx, p.Name)
}

// RestoreUploadedBackup implements POST /system/backups/restore.
func (h *Handler) RestoreUploadedBackup(ctx context.Context, req gen.RestoreUploadedBackupReq, p gen.RestoreUploadedBackupParams) (*gen.RestoreStarted, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if err := checkConfirm(p.Confirm); err != nil {
		return nil, err
	}
	name, err := h.Backups.SaveUpload(req.Data)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, &StatusError{Status: http.StatusRequestEntityTooLarge, Code: "request_too_large", Message: "the backup is larger than 2 GiB"}
		}
		return nil, err
	}
	return h.restore(ctx, name)
}

func (h *Handler) systemSettings(ctx context.Context) (*gen.SystemSettings, error) {
	out := &gen.SystemSettings{}
	var err error
	get := func(k settings.Key[int], dst *int) {
		if err == nil {
			*dst, err = settings.Get(ctx, h.Settings, k)
		}
	}
	out.UpdateCheck, err = settings.Get(ctx, h.Settings, settings.UpdateCheck)
	get(settings.MinFreeSpaceGB, &out.MinFreeSpaceGb)
	get(settings.RecycleBinWarnGB, &out.RecycleBinWarnGb)
	get(settings.StuckImportMinutes, &out.StuckImportMinutes)
	get(settings.BackupIntervalHours, &out.BackupIntervalHours)
	get(settings.BackupKeep, &out.BackupKeep)
	get(settings.HistoryDays, &out.HistoryDays)
	get(settings.TimelineDays, &out.TimelineDays)
	get(settings.SearchLogDays, &out.SearchLogDays)
	get(settings.DownloadEventsDays, &out.DownloadEventsDays)
	get(indexers.ReleaseRetentionDays, &out.ReleasesSeenDays)
	get(settings.VacuumIntervalDays, &out.VacuumIntervalDays)
	get(settings.ImageCacheMaxMB, &out.ImageCacheMaxMb)
	if err != nil {
		return nil, err
	}
	out.MetricsTokenSet, err = h.Settings.Configured(ctx, settings.MetricsToken)
	return out, err
}

// GetSystemSettings implements GET /settings/system.
func (h *Handler) GetSystemSettings(ctx context.Context) (*gen.SystemSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.systemSettings(ctx)
}

// UpdateSystemSettings implements PUT /settings/system.
func (h *Handler) UpdateSystemSettings(ctx context.Context, req *gen.SystemSettingsInput) (*gen.SystemSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	err := h.DB.Tx(ctx, func(tx *sqlTx) error {
		if v, ok := req.UpdateCheck.Get(); ok {
			if err := settings.Set(ctx, h.Settings, tx, settings.UpdateCheck, v); err != nil {
				return err
			}
		}
		for _, f := range []struct {
			v OptIntLike
			k settings.Key[int]
		}{
			{req.MinFreeSpaceGb, settings.MinFreeSpaceGB}, {req.RecycleBinWarnGb, settings.RecycleBinWarnGB},
			{req.StuckImportMinutes, settings.StuckImportMinutes}, {req.BackupIntervalHours, settings.BackupIntervalHours},
			{req.BackupKeep, settings.BackupKeep}, {req.HistoryDays, settings.HistoryDays}, {req.TimelineDays, settings.TimelineDays}, {req.SearchLogDays, settings.SearchLogDays},
			{req.DownloadEventsDays, settings.DownloadEventsDays}, {req.ReleasesSeenDays, indexers.ReleaseRetentionDays},
			{req.VacuumIntervalDays, settings.VacuumIntervalDays}, {req.ImageCacheMaxMb, settings.ImageCacheMaxMB},
		} {
			if v, ok := f.v.Get(); ok {
				if err := settings.Set(ctx, h.Settings, tx, f.k, v); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if t, ok := req.MetricsToken.Get(); ok {
		t = strings.TrimSpace(t)
		if t != "" && len(t) < 16 {
			return nil, &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: "the metrics token must be at least 16 characters"}
		}
		if err := h.Settings.SetSecret(ctx, settings.MetricsToken, t); err != nil {
			return nil, err
		}
	}
	return h.systemSettings(ctx)
}

// OptIntLike is gen.OptInt (named so the table above reads well).
type OptIntLike = gen.OptInt

type sqlTx = sql.Tx

func jobsUnique(key string) jobs.EnqueueOptions { return jobs.EnqueueOptions{UniqueKey: key} }
