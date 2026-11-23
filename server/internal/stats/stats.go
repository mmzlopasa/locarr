// Package stats computes the numbers for monitoring dashboards (/api/stats,
// D-085) and renders them as Prometheus metrics (/metrics). The database part
// is cached for 15 seconds, so dashboards may poll often.
package stats

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// CacheFor is how long computed stats are reused.
const CacheFor = 15 * time.Second

// RootFolder is one root folder's space.
type RootFolder struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Accessible bool   `json:"accessible"`
	FreeBytes  *int64 `json:"freeBytes,omitempty"`
	TotalBytes *int64 `json:"totalBytes,omitempty"`
}

// Stats are the dashboard numbers (field names are part of the API).
type Stats struct {
	Version            string       `json:"version"`
	Commit             string       `json:"commit"`
	GeneratedAt        time.Time    `json:"generatedAt"`
	Movies             int          `json:"movies"`
	MoviesHave         int          `json:"moviesHave"`
	MoviesMissing      int          `json:"moviesMissing"`
	Series             int          `json:"series"`
	Episodes           int          `json:"episodes"`
	EpisodesHave       int          `json:"episodesHave"`
	EpisodesMissing    int          `json:"episodesMissing"`
	Artists            int          `json:"artists"`
	Albums             int          `json:"albums"`
	AlbumsHave         int          `json:"albumsHave"`
	AlbumsMissing      int          `json:"albumsMissing"`
	QueueSize          int          `json:"queueSize"`
	ActiveDownloads    int          `json:"activeDownloads"`
	DownloadSpeedBytes int64        `json:"downloadSpeedBytes"`
	WantedMovies       int          `json:"wantedMovies"`
	WantedEpisodes     int          `json:"wantedEpisodes"`
	WantedAlbums       int          `json:"wantedAlbums"`
	FailedImports      int          `json:"failedImports"`
	HealthErrors       int          `json:"healthErrors"`
	HealthWarnings     int          `json:"healthWarnings"`
	HealthInfo         int          `json:"healthInfo"`
	Indexers           int          `json:"indexers"`
	IndexersHealthy    int          `json:"indexersHealthy"`
	IndexersFailing    int          `json:"indexersFailing"`
	IndexersDisabled   int          `json:"indexersDisabled"`
	RequestsPending    int          `json:"requestsPending"`
	SubtitlesMissing   int          `json:"subtitlesMissing"`
	LastRSSSyncAt      *time.Time   `json:"lastRssSyncAt,omitempty"`
	RootFolders        []RootFolder `json:"rootFolders"`
}

// Service computes Stats.
type Service struct {
	DB       *db.DB
	Indexers *indexers.Service
	Roots    *rootfolders.Service
	// Health returns the current issue counts by severity.
	Health func() (errs, warnings, info int)
	// WantedMovies counts monitored, available movies without a file.
	WantedMovies func(ctx context.Context) (int, error)
	Now          func() time.Time

	mu     sync.Mutex
	cached *Stats
	at     time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Get returns the stats (at most CacheFor old).
func (s *Service) Get(ctx context.Context) (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.now().Sub(s.at) < CacheFor {
		return *s.cached, nil
	}
	st, err := s.compute(ctx)
	if err != nil {
		return Stats{}, err
	}
	s.cached, s.at = &st, s.now()
	return st, nil
}

func (s *Service) compute(ctx context.Context) (Stats, error) {
	now := s.now().UTC()
	st := Stats{Version: version.Version, Commit: version.Commit, GeneratedAt: now, RootFolders: []RootFolder{}}
	today := now.Format("2006-01-02")
	counts := []struct {
		dst *int
		q   string
		arg []any
	}{
		{&st.Movies, `SELECT COUNT(*) FROM media_items WHERE kind = 'movie'`, nil},
		{&st.MoviesHave, `SELECT COUNT(DISTINCT i.id) FROM media_items i JOIN media_files f ON f.item_id = i.id WHERE i.kind = 'movie'`, nil},
		{&st.MoviesMissing, `SELECT COUNT(*) FROM media_items i WHERE i.kind = 'movie' AND i.monitored = 1
			AND NOT EXISTS (SELECT 1 FROM media_files f WHERE f.item_id = i.id)`, nil},
		{&st.Series, `SELECT COUNT(*) FROM media_items WHERE kind = 'series'`, nil},
		{&st.Episodes, `SELECT COUNT(*) FROM episodes WHERE removed = 0`, nil},
		{&st.EpisodesHave, `SELECT COUNT(*) FROM episodes WHERE file_id IS NOT NULL`, nil},
		{&st.EpisodesMissing, `SELECT COUNT(*) FROM episodes e JOIN media_items i ON i.id = e.series_id JOIN seasons sn ON sn.id = e.season_id
			WHERE i.monitored = 1 AND sn.monitored = 1 AND e.monitored = 1 AND e.removed = 0 AND e.file_id IS NULL
			AND e.air_date_utc IS NOT NULL AND e.air_date_utc <= ?`, []any{db.Millis(now)}},
		{&st.Artists, `SELECT COUNT(*) FROM media_items WHERE kind = 'artist'`, nil},
		{&st.Albums, `SELECT COUNT(*) FROM albums`, nil},
		{&st.AlbumsHave, `SELECT COUNT(DISTINCT album_id) FROM album_files`, nil},
		{&st.AlbumsMissing, `SELECT COUNT(*) FROM albums a JOIN media_items i ON i.id = a.artist_id WHERE a.monitored = 1 AND i.monitored = 1
			AND a.release_date <> '' AND a.release_date <= ? AND NOT EXISTS (SELECT 1 FROM album_files f WHERE f.album_id = a.id)`, []any{today}},
		{&st.QueueSize, `SELECT COUNT(*) FROM downloads WHERE status IN ('active', 'completed')`, nil},
		{&st.ActiveDownloads, `SELECT COUNT(*) FROM downloads WHERE status = 'active' AND state = 'downloading'`, nil},
		{&st.FailedImports, `SELECT COUNT(*) FROM downloads d WHERE d.status = 'completed' AND EXISTS (SELECT 1 FROM download_events e
			WHERE e.download_id = d.id AND e.type = 'import_failed')`, nil},
		{&st.RequestsPending, `SELECT COUNT(*) FROM requests WHERE status = 'pending'`, nil},
		{&st.SubtitlesMissing, `SELECT COUNT(*) FROM subtitle_state WHERE missing_count > 0`, nil},
	}
	for _, c := range counts {
		if err := s.DB.R.QueryRowContext(ctx, c.q, c.arg...).Scan(c.dst); err != nil {
			return st, fmt.Errorf("stats: %w", err)
		}
	}
	st.WantedEpisodes, st.WantedAlbums = st.EpisodesMissing, st.AlbumsMissing
	if s.WantedMovies != nil {
		n, err := s.WantedMovies(ctx)
		if err != nil {
			return st, err
		}
		st.WantedMovies = n
	}
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COALESCE(SUM(download_speed), 0) FROM downloads WHERE status = 'active'`).
		Scan(&st.DownloadSpeedBytes); err != nil {
		return st, err
	}
	var rss sql.NullInt64
	if err := s.DB.R.QueryRowContext(ctx, `SELECT MAX(last_run_at) FROM job_schedules WHERE kind = 'rss-sync'`).Scan(&rss); err == nil {
		st.LastRSSSyncAt = db.NullTime(rss)
	}
	if s.Indexers != nil {
		list, err := s.Indexers.List(ctx)
		if err != nil {
			return st, err
		}
		for _, ix := range list {
			st.Indexers++
			switch ix.HealthStatus() {
			case indexers.HealthFailing:
				st.IndexersFailing++
			case indexers.HealthDisabled:
				st.IndexersDisabled++
			default:
				st.IndexersHealthy++
			}
		}
	}
	if s.Health != nil {
		st.HealthErrors, st.HealthWarnings, st.HealthInfo = s.Health()
	}
	if s.Roots != nil {
		roots, err := s.Roots.List(ctx)
		if err != nil {
			return st, err
		}
		for _, f := range roots {
			rf := RootFolder{Path: f.Path, Kind: f.Kind, Accessible: f.Accessible}
			if f.Space != nil {
				free, total := f.Space.Free, f.Space.Total
				rf.FreeBytes, rf.TotalBytes = &free, &total
			}
			st.RootFolders = append(st.RootFolders, rf)
		}
	}
	return st, nil
}

// WritePrometheus writes st in the Prometheus text exposition format.
func WritePrometheus(w io.Writer, st Stats) error {
	var b strings.Builder
	gauge := func(name, help string, v any, labels ...string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
		if len(labels) == 0 {
			fmt.Fprintf(&b, "%s %v\n", name, v)
			return
		}
		fmt.Fprintf(&b, "%s{%s} %v\n", name, strings.Join(labels, ","), v)
	}
	label := func(k, v string) string {
		return k + `="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v) + `"`
	}
	gauge("locarr_info", "Locarr version.", 1, label("version", st.Version), label("commit", st.Commit))
	lib := []struct {
		kind, state string
		v           int
	}{
		{"movie", "total", st.Movies}, {"movie", "have", st.MoviesHave}, {"movie", "missing", st.MoviesMissing},
		{"series", "total", st.Series},
		{"episode", "total", st.Episodes}, {"episode", "have", st.EpisodesHave}, {"episode", "missing", st.EpisodesMissing},
		{"artist", "total", st.Artists},
		{"album", "total", st.Albums}, {"album", "have", st.AlbumsHave}, {"album", "missing", st.AlbumsMissing},
	}
	fmt.Fprintf(&b, "# HELP locarr_library_items Library items by kind and state.\n# TYPE locarr_library_items gauge\n")
	for _, l := range lib {
		fmt.Fprintf(&b, "locarr_library_items{%s,%s} %d\n", label("kind", l.kind), label("state", l.state), l.v)
	}
	fmt.Fprintf(&b, "# HELP locarr_wanted_items Monitored, available items without a file.\n# TYPE locarr_wanted_items gauge\n")
	for _, kv := range [][2]any{{"movie", st.WantedMovies}, {"episode", st.WantedEpisodes}, {"album", st.WantedAlbums}, {"subtitle", st.SubtitlesMissing}} {
		fmt.Fprintf(&b, "locarr_wanted_items{%s} %v\n", label("kind", kv[0].(string)), kv[1])
	}
	gauge("locarr_queue_size", "Downloads still in a client.", st.QueueSize)
	gauge("locarr_downloads_active", "Downloads downloading now.", st.ActiveDownloads)
	gauge("locarr_download_speed_bytes", "Total download speed in bytes per second.", st.DownloadSpeedBytes)
	gauge("locarr_imports_failed", "Finished downloads whose import failed.", st.FailedImports)
	fmt.Fprintf(&b, "# HELP locarr_health_issues Health issues by severity.\n# TYPE locarr_health_issues gauge\n")
	for _, kv := range [][2]any{{"error", st.HealthErrors}, {"warning", st.HealthWarnings}, {"info", st.HealthInfo}} {
		fmt.Fprintf(&b, "locarr_health_issues{%s} %v\n", label("severity", kv[0].(string)), kv[1])
	}
	fmt.Fprintf(&b, "# HELP locarr_indexers Indexers by health.\n# TYPE locarr_indexers gauge\n")
	for _, kv := range [][2]any{{"healthy", st.IndexersHealthy}, {"failing", st.IndexersFailing}, {"disabled", st.IndexersDisabled}} {
		fmt.Fprintf(&b, "locarr_indexers{%s} %v\n", label("health", kv[0].(string)), kv[1])
	}
	gauge("locarr_requests_pending", "Requests waiting for approval.", st.RequestsPending)
	if st.LastRSSSyncAt != nil {
		gauge("locarr_rss_sync_last_timestamp_seconds", "When the last RSS sync ran.", st.LastRSSSyncAt.Unix())
	}
	roots := append([]RootFolder{}, st.RootFolders...)
	sort.Slice(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
	fmt.Fprintf(&b, "# HELP locarr_rootfolder_free_bytes Free space of each root folder.\n# TYPE locarr_rootfolder_free_bytes gauge\n")
	for _, r := range roots {
		if r.FreeBytes != nil {
			fmt.Fprintf(&b, "locarr_rootfolder_free_bytes{%s,%s} %d\n", label("path", r.Path), label("kind", r.Kind), *r.FreeBytes)
		}
	}
	fmt.Fprintf(&b, "# HELP locarr_rootfolder_total_bytes Size of each root folder's filesystem.\n# TYPE locarr_rootfolder_total_bytes gauge\n")
	for _, r := range roots {
		if r.TotalBytes != nil {
			fmt.Fprintf(&b, "locarr_rootfolder_total_bytes{%s,%s} %d\n", label("path", r.Path), label("kind", r.Kind), *r.TotalBytes)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
