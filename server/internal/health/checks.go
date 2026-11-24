package health

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Thresholds that are not settings.
const (
	// WALWarnBytes: a write-ahead log this large means checkpoints are not keeping up.
	WALWarnBytes = 512 << 20
	// DatabaseInfoBytes: a database this large is worth knowing about.
	DatabaseInfoBytes = 4 << 30
	// SkewWarn: clocks further apart than this break TOTP codes and release dates.
	SkewWarn = 2 * time.Minute
	// categoryCheckEvery limits how often clients are asked for their categories.
	categoryCheckEvery = time.Hour
)

// Deps are what the standard checks read.
type Deps struct {
	DB        *db.DB
	Settings  *settings.Store
	Indexers  *indexers.Service
	Counts    func(ctx context.Context, kindCategory string) (timeline.Counts, error)
	Downloads *downloads.Service
	Roots     *rootfolders.Service
	Metadata  *metadata.Health
	// TMDbConfigured reports whether a TMDb key is set.
	TMDbConfigured func(ctx context.Context) bool
	Version        *VersionChecker
	Now            func() time.Time
}

func (d *Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func gb(n int) int64 { return int64(n) << 30 }

// RegisterStandard adds every built-in check.
func (s *Service) RegisterStandard(d *Deps) {
	cats := &categoryCache{at: map[int64]time.Time{}, missing: map[int64][]string{}}
	s.Register("indexers", d.checkIndexers)
	s.Register("downloadclients", func(ctx context.Context) ([]Issue, error) { return d.checkClients(ctx, cats) })
	s.Register("paths", d.checkPaths)
	s.Register("rootfolders", d.checkRootFolders)
	s.Register("recyclebin", d.checkRecycleBin)
	s.Register("imports", d.checkImports)
	s.Register("metadata", d.checkMetadata)
	s.Register("subtitles", d.checkSubtitles)
	s.Register("database", d.checkDatabase)
	if d.Version != nil {
		s.Register("version", d.Version.Issues)
	}
}

var kindCategories = map[string]string{rootfolders.KindMovies: "movies", rootfolders.KindSeries: "tv", rootfolders.KindMusic: "music"}

func (d *Deps) checkIndexers(ctx context.Context) ([]Issue, error) {
	list, err := d.Indexers.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Issue
	enabled := 0
	for _, ix := range list {
		if ix.Enabled {
			enabled++
		}
		key := "indexer:" + strconv.FormatInt(ix.ID, 10)
		if ix.HealthStatus() != indexers.HealthFailing {
			// A built-in site whose weekly self-test failed although searches
			// have not (yet) failed.
			if ix.Enabled && ix.Kind == indexers.Native && ix.Health.SelfTestOK != nil && !*ix.Health.SelfTestOK {
				out = append(out, Issue{Key: key, Severity: SeverityWarning, Code: "indexer_selftest_failed",
					Message: fmt.Sprintf("the weekly test of built-in indexer %s failed", ix.Name), Params: map[string]any{"name": ix.Name},
					Docs: "indexer-builtin", FixRoute: "/indexers"})
			}
			continue
		}
		p := map[string]any{"name": ix.Name, "error": ix.Health.LastError}
		if ix.Health.RetryAt != nil {
			p["retryAt"] = ix.Health.RetryAt.UTC()
		}
		code, docs := "indexer_failing", "indexer-failing"
		switch ix.Health.LastErrorCode {
		case "challenge": // a Cloudflare challenge: FlareSolverr helps
			code, docs = "indexer_challenge", "indexer-builtin"
		case "layout": // a built-in site's pages no longer match its definition
			code, docs = "indexer_layout", "indexer-builtin"
			p["parseFailures"] = ix.Health.ParseFailures
		}
		out = append(out, Issue{Key: key, Severity: SeverityWarning, Code: code,
			Message: fmt.Sprintf("indexer %s is failing: %s", ix.Name, ix.Health.LastError), Params: p, Docs: docs, FixRoute: "/indexers"})
	}
	kinds, err := d.libraryKinds(ctx)
	if err != nil {
		return nil, err
	}
	if len(kinds) > 0 && enabled == 0 {
		return append(out, Issue{Key: "indexers:none", Severity: SeverityError, Code: "indexers_none",
			Message: "no indexer is enabled: Locarr cannot find anything", Docs: "no-indexers", FixRoute: "/indexers"}), nil
	}
	for _, k := range kinds {
		c, err := d.Counts(ctx, kindCategories[k])
		if err != nil {
			return nil, err
		}
		if c.Indexers == 0 {
			out = append(out, Issue{Key: "indexers:" + k, Severity: SeverityWarning, Code: "indexer_no_kind", Params: map[string]any{"kind": k},
				Message: "no enabled indexer is used for automatic searches of " + k, Docs: "no-indexers", FixRoute: "/indexers"})
		}
	}
	return out, nil
}

// libraryKinds are the media kinds that have a root folder.
func (d *Deps) libraryKinds(ctx context.Context) ([]string, error) {
	rows, err := d.DB.R.QueryContext(ctx, `SELECT DISTINCT kind FROM root_folders ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

type categoryCache struct {
	mu      sync.Mutex
	at      map[int64]time.Time
	missing map[int64][]string
}

func (d *Deps) checkClients(ctx context.Context, cats *categoryCache) ([]Issue, error) {
	clients, err := d.Downloads.Clients(ctx)
	if err != nil {
		return nil, err
	}
	snaps := d.Downloads.Snapshots()
	minFree, _ := settings.Get(ctx, d.Settings, settings.MinFreeSpaceGB)
	kinds, err := d.libraryKinds(ctx)
	if err != nil {
		return nil, err
	}
	var out []Issue
	enabled := 0
	for _, c := range clients {
		if !c.Enabled {
			continue
		}
		enabled++
		id := strconv.FormatInt(c.ID, 10)
		sn, ok := snaps[c.ID]
		if !ok {
			continue // not polled yet
		}
		if !sn.OK {
			out = append(out, Issue{Key: "client:" + id, Severity: SeverityError, Code: "client_unreachable",
				Params:  map[string]any{"name": c.Name, "error": sn.Error, "errorCode": sn.ErrorCode},
				Message: fmt.Sprintf("download client %s is not reachable: %s", c.Name, sn.Error), Docs: "client-unreachable", FixRoute: "/downloaders"})
			continue
		}
		if sn.FreeSpace != nil && minFree > 0 && *sn.FreeSpace < gb(minFree) {
			out = append(out, Issue{Key: "client:" + id + ":space", Severity: SeverityWarning, Code: "client_low_space",
				Params:  map[string]any{"name": c.Name, "freeBytes": *sn.FreeSpace, "minBytes": gb(minFree)},
				Message: fmt.Sprintf("download client %s has only %d MB free", c.Name, *sn.FreeSpace>>20), Docs: "download-disk-space", FixRoute: "/downloaders"})
		}
		cats.mu.Lock()
		stale := d.now().Sub(cats.at[c.ID]) > categoryCheckEvery
		cats.mu.Unlock()
		if stale {
			res, err := d.Downloads.TestSaved(ctx, c.ID)
			if err == nil && res.OK {
				cats.mu.Lock()
				cats.at[c.ID], cats.missing[c.ID] = d.now(), res.MissingCategories
				cats.mu.Unlock()
			}
		}
		cats.mu.Lock()
		missing := cats.missing[c.ID]
		cats.mu.Unlock()
		for _, m := range missing {
			out = append(out, Issue{Key: "client:" + id + ":category:" + m, Severity: SeverityWarning, Code: "client_category_missing",
				Params:  map[string]any{"name": c.Name, "category": m},
				Message: fmt.Sprintf("download client %s has no category %q", c.Name, m), Docs: "client-category", FixRoute: "/downloaders"})
		}
	}
	if enabled == 0 && len(kinds) > 0 {
		out = append(out, Issue{Key: "clients:none", Severity: SeverityError, Code: "clients_none",
			Message: "no download client is enabled: Locarr cannot download anything", Docs: "no-clients", FixRoute: "/downloaders"})
	}
	return out, nil
}

// Adult downloads appear only as "download <id>" (health warnings reach shared channels; D-101).
func (d *Deps) checkPaths(ctx context.Context) ([]Issue, error) {
	var out []Issue
	rows, err := d.DB.R.QueryContext(ctx, `SELECT id, CASE WHEN COALESCE(json_extract(item_ref, '$.ref'), '') LIKE 'scene:%' OR COALESCE(json_extract(item_ref, '$.ref'), '') LIKE 'adult_movie:%' THEN 'download ' || id ELSE title END, client_name, content_path, save_path, message FROM downloads
		WHERE status = 'active' AND progress >= 1 AND message LIKE '%not visible%'`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id                               int64
			title, client, content, save, ms string
		)
		if err := rows.Scan(&id, &title, &client, &content, &save, &ms); err != nil {
			_ = rows.Close()
			return nil, err
		}
		p := content
		if p == "" {
			p = save
		}
		out = append(out, Issue{Key: "download:" + strconv.FormatInt(id, 10) + ":path", Severity: SeverityError, Code: "path_not_visible",
			Params:  map[string]any{"title": title, "client": client, "path": p, "downloadId": id},
			Message: fmt.Sprintf("%s finished in %s but Locarr cannot see %s", title, client, p), Docs: "remote-path-mapping", FixRoute: "/downloaders"})
	}
	_ = rows.Close()
	maps, err := d.Downloads.Mappings(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range maps {
		if st, err := os.Stat(m.LocalPath); err == nil && st.IsDir() {
			continue
		}
		out = append(out, Issue{Key: "mapping:" + strconv.FormatInt(m.ID, 10), Severity: SeverityWarning, Code: "mapping_local_missing",
			Params:  map[string]any{"host": m.Host, "remotePath": m.RemotePath, "localPath": m.LocalPath},
			Message: fmt.Sprintf("remote path mapping %s → %s: the local folder does not exist", m.RemotePath, m.LocalPath),
			Docs:    "remote-path-mapping", FixRoute: "/downloaders"})
	}
	return out, nil
}

func (d *Deps) checkRootFolders(ctx context.Context) ([]Issue, error) {
	list, err := d.Roots.List(ctx)
	if err != nil {
		return nil, err
	}
	minFree, _ := settings.Get(ctx, d.Settings, settings.MinFreeSpaceGB)
	var out []Issue
	for _, f := range list {
		id := strconv.FormatInt(f.ID, 10)
		p := map[string]any{"path": f.Path, "kind": f.Kind}
		switch {
		case !f.Accessible:
			out = append(out, Issue{Key: "rootfolder:" + id, Severity: SeverityError, Code: "rootfolder_missing", Params: p,
				Message: "root folder " + f.Path + " is missing or cannot be read", Docs: "root-folders", FixRoute: "/settings/media"})
		case !f.Writable:
			if f.WriteProblem != nil {
				p["reason"], p["detail"] = f.WriteProblem.Reason, f.WriteProblem.Detail
			}
			out = append(out, Issue{Key: "rootfolder:" + id, Severity: SeverityError, Code: "rootfolder_unwritable", Params: p,
				Message: "Locarr cannot write to root folder " + f.Path, Docs: "permissions", FixRoute: "/settings/media"})
		case f.Space != nil && minFree > 0 && f.Space.Free < gb(minFree):
			p["freeBytes"], p["minBytes"] = f.Space.Free, gb(minFree)
			out = append(out, Issue{Key: "rootfolder:" + id + ":space", Severity: SeverityWarning, Code: "rootfolder_low_space", Params: p,
				Message: fmt.Sprintf("root folder %s has only %d MB free", f.Path, f.Space.Free>>20), Docs: "disk-space", FixRoute: "/settings/media"})
		}
	}
	if len(list) == 0 {
		out = append(out, Issue{Key: "rootfolders:none", Severity: SeverityInfo, Code: "rootfolders_none",
			Message: "no root folder yet: add the folders for movies, series and music", Docs: "root-folders", FixRoute: "/settings/media"})
	}
	return out, nil
}

func (d *Deps) checkRecycleBin(ctx context.Context) ([]Issue, error) {
	limit, _ := settings.Get(ctx, d.Settings, settings.RecycleBinWarnGB)
	if limit <= 0 {
		return nil, nil
	}
	var size, n int64
	if err := d.DB.R.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0), COUNT(*) FROM recycle_bin`).Scan(&size, &n); err != nil {
		return nil, err
	}
	if size < gb(limit) {
		return nil, nil
	}
	return []Issue{{Key: "recyclebin:size", Severity: SeverityWarning, Code: "recycle_bin_large",
		Params:  map[string]any{"sizeBytes": size, "limitBytes": gb(limit), "entries": n},
		Message: fmt.Sprintf("the recycle bin holds %d GB", size>>30), Docs: "recycle-bin", FixRoute: "/settings/mediamanagement"}}, nil
}

func (d *Deps) checkImports(ctx context.Context) ([]Issue, error) {
	mins, _ := settings.Get(ctx, d.Settings, settings.StuckImportMinutes)
	if mins <= 0 {
		return nil, nil
	}
	rows, err := d.DB.R.QueryContext(ctx, `SELECT id, CASE WHEN COALESCE(json_extract(item_ref, '$.ref'), '') LIKE 'scene:%' OR COALESCE(json_extract(item_ref, '$.ref'), '') LIKE 'adult_movie:%' THEN 'download ' || id ELSE title END, completed_at, message FROM downloads WHERE status = 'completed' AND completed_at < ?
		ORDER BY completed_at LIMIT 50`, db.Millis(d.now().Add(-time.Duration(mins)*time.Minute)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var (
			id, at     int64
			title, msg string
		)
		if err := rows.Scan(&id, &title, &at, &msg); err != nil {
			return nil, err
		}
		waited := int(d.now().Sub(db.Time(at)).Minutes())
		out = append(out, Issue{Key: "download:" + strconv.FormatInt(id, 10) + ":import", Severity: SeverityWarning, Code: "import_stuck",
			Params:  map[string]any{"title": title, "minutes": waited, "detail": msg, "downloadId": id},
			Message: fmt.Sprintf("%s finished %d minutes ago and is not imported: %s", title, waited, msg), Docs: "stuck-imports",
			FixRoute: "/activity/board"})
	}
	return out, rows.Err()
}

func (d *Deps) checkMetadata(ctx context.Context) ([]Issue, error) {
	var out []Issue
	if d.TMDbConfigured != nil && !d.TMDbConfigured(ctx) {
		out = append(out, Issue{Key: "metadata:tmdb:key", Severity: SeverityWarning, Code: "metadata_no_tmdb_key",
			Message: "no TMDb API key is set: movies and series cannot be added", Docs: "metadata-providers", FixRoute: "/settings/metadata"})
	}
	for _, st := range d.Metadata.Snapshot() {
		p := map[string]any{"provider": st.Provider, "error": st.Error}
		switch st.State {
		case metadata.StateInvalidKey:
			out = append(out, Issue{Key: "metadata:" + st.Provider, Severity: SeverityError, Code: "metadata_invalid_key", Params: p,
				Message: st.Provider + " refuses the API key", Docs: "metadata-providers", FixRoute: "/settings/metadata"})
		case metadata.StateRateLimited:
			out = append(out, Issue{Key: "metadata:" + st.Provider, Severity: SeverityWarning, Code: "metadata_rate_limited", Params: p,
				Message: st.Provider + " is rate limiting Locarr", Docs: "metadata-providers", FixRoute: "/settings/metadata"})
		case metadata.StateUnavailable:
			out = append(out, Issue{Key: "metadata:" + st.Provider, Severity: SeverityWarning, Code: "metadata_unavailable", Params: p,
				Message: st.Provider + " is not reachable: " + st.Error, Docs: "metadata-providers", FixRoute: "/settings/metadata"})
		}
	}
	return out, nil
}

func (d *Deps) checkSubtitles(ctx context.Context) ([]Issue, error) {
	now := db.Millis(d.now())
	rows, err := d.DB.R.QueryContext(ctx, `SELECT name, quota_remaining, quota_reset_at, retry_at, failures, last_error FROM subtitle_providers
		WHERE enabled = 1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var (
			name, lastErr     string
			quota, reset, rtr *int64
			failures          int
		)
		if err := rows.Scan(&name, &quota, &reset, &rtr, &failures, &lastErr); err != nil {
			return nil, err
		}
		switch {
		case quota != nil && *quota <= 0 && (reset == nil || *reset > now):
			p := map[string]any{"provider": name}
			if reset != nil {
				p["resetAt"] = db.Time(*reset)
			}
			out = append(out, Issue{Key: "subtitles:" + name, Severity: SeverityWarning, Code: "subtitle_provider_quota", Params: p,
				Message: "subtitle provider " + name + " has used up its download quota", Docs: "subtitle-providers", FixRoute: "/settings/subtitles"})
		case failures > 0 && rtr != nil && *rtr > now:
			out = append(out, Issue{Key: "subtitles:" + name, Severity: SeverityWarning, Code: "subtitle_provider_failing",
				Params:  map[string]any{"provider": name, "error": lastErr, "retryAt": db.Time(*rtr)},
				Message: "subtitle provider " + name + " is failing: " + lastErr, Docs: "subtitle-providers", FixRoute: "/settings/subtitles"})
		}
	}
	return out, rows.Err()
}

func (d *Deps) checkDatabase(_ context.Context) ([]Issue, error) {
	var out []Issue
	if st, err := os.Stat(d.DB.Path + "-wal"); err == nil && st.Size() > WALWarnBytes {
		out = append(out, Issue{Key: "database:wal", Severity: SeverityWarning, Code: "database_wal_large",
			Params:  map[string]any{"walBytes": st.Size()},
			Message: fmt.Sprintf("the database write-ahead log is %d MB", st.Size()>>20), Docs: "database"})
	}
	if size := d.DB.Size(); size > DatabaseInfoBytes {
		out = append(out, Issue{Key: "database:size", Severity: SeverityInfo, Code: "database_large", Params: map[string]any{"sizeBytes": size},
			Message: fmt.Sprintf("the database is %d MB", size>>20), Docs: "database", FixRoute: "/settings/system"})
	}
	return out, nil
}

// StreameroIssue is the issue for a failing Streamero connection.
func StreameroIssue(reason string) Issue {
	return Issue{Check: "streamero", Severity: SeverityWarning, Code: "streamero_unreachable", Params: map[string]any{"error": reason},
		Message: "Streamero could not be reached: " + reason, Docs: "streamero", FixRoute: "/settings/mediaserver"}
}

// BackupIssue is the issue for a failing scheduled backup.
func BackupIssue(reason string) Issue {
	return Issue{Check: "backups", Severity: SeverityWarning, Code: "backup_failed", Params: map[string]any{"error": reason},
		Message: "the scheduled backup failed: " + reason, Docs: "backups", FixRoute: "/system/backups"}
}

// trimVersion drops a leading "v".
func trimVersion(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
