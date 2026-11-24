// Package housekeeping keeps the config folder small (D-086): it prunes old
// timeline events, search log entries, download events and RSS releases by
// their retention settings, compacts the database on a schedule and caps the
// image cache.
package housekeeping

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/history"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// batch is how many rows one delete removes, so writers are never held up long.
const batch = 5000

// Result says what one run removed.
type Result struct {
	Timeline       int64
	History        int64 // download events of the history (grabs, failures)
	SearchLog      int64
	DownloadEvents int64
	ReleasesSeen   int64
	ImageFiles     int
	ImageBytes     int64
	Vacuumed       bool
}

// Service runs housekeeping.
type Service struct {
	DB       *db.DB
	Settings *settings.Store
	// ImageDir is the root of the image caches.
	ImageDir string
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) deleteBatches(ctx context.Context, q string, args ...any) (int64, error) {
	var total int64
	for {
		res, err := s.DB.W.ExecContext(ctx, q, append(args, batch)...)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < batch {
			return total, nil
		}
	}
}

func (s *Service) days(ctx context.Context, k settings.Key[int]) time.Time {
	d, _ := settings.Get(ctx, s.Settings, k)
	if d <= 0 {
		d = k.Default
	}
	return s.now().Add(-time.Duration(d) * 24 * time.Hour)
}

// Prune removes rows older than their retention. The activity history
// (history.TimelineTypes and history.DownloadTypes) has its own retention,
// HistoryDays, where 0 keeps it forever (D-107).
func (s *Service) Prune(ctx context.Context) (Result, error) {
	var r Result
	var err error
	hTypes, dTypes := history.TimelineTypes(), history.DownloadTypes()
	hMarks, hArgs := inList(hTypes)
	dMarks, dArgs := inList(dTypes)
	if r.Timeline, err = s.deleteBatches(ctx, `DELETE FROM timeline_events WHERE id IN
		(SELECT id FROM timeline_events WHERE at < ? AND type NOT IN (`+hMarks+`) LIMIT ?)`, // #nosec G202 -- placeholders only
		append([]any{db.Millis(s.days(ctx, settings.TimelineDays))}, hArgs...)...); err != nil {
		return r, err
	}
	keep, _ := settings.Get(ctx, s.Settings, settings.HistoryDays)
	if keep > 0 {
		cut := db.Millis(s.now().Add(-time.Duration(keep) * 24 * time.Hour))
		n, err := s.deleteBatches(ctx, `DELETE FROM timeline_events WHERE id IN
			(SELECT id FROM timeline_events WHERE at < ? AND type IN (`+hMarks+`) LIMIT ?)`, append([]any{cut}, hArgs...)...) // #nosec G202 -- placeholders only
		r.Timeline += n
		if err != nil {
			return r, err
		}
		if r.History, err = s.deleteBatches(ctx, `DELETE FROM download_events WHERE id IN
			(SELECT id FROM download_events WHERE at < ? AND type IN (`+dMarks+`) LIMIT ?)`, append([]any{cut}, dArgs...)...); err != nil { // #nosec G202 -- placeholders only
			return r, err
		}
	}
	if r.SearchLog, err = s.deleteBatches(ctx, `DELETE FROM search_log WHERE id IN
		(SELECT id FROM search_log WHERE started_at < ? LIMIT ?)`, db.Millis(s.days(ctx, settings.SearchLogDays))); err != nil {
		return r, err
	}
	// Events of downloads that ended long ago; the downloads themselves stay
	// for the statistics, and the history's own events follow HistoryDays.
	cut := db.Millis(s.days(ctx, settings.DownloadEventsDays))
	if r.DownloadEvents, err = s.deleteBatches(ctx, `DELETE FROM download_events WHERE id IN (SELECT e.id FROM download_events e
		JOIN downloads d ON d.id = e.download_id WHERE d.finished_at IS NOT NULL AND d.finished_at < ? AND e.at < ?
		AND e.type NOT IN (`+dMarks+`) LIMIT ?)`, append([]any{cut, cut}, dArgs...)...); err != nil { // #nosec G202 -- placeholders only
		return r, err
	}
	if r.ReleasesSeen, err = s.deleteBatches(ctx, `DELETE FROM releases_seen WHERE id IN
		(SELECT id FROM releases_seen WHERE first_seen_at < ? LIMIT ?)`, db.Millis(s.days(ctx, indexers.ReleaseRetentionDays))); err != nil {
		return r, err
	}
	return r, nil
}

// VacuumIfDue compacts the database when the interval has passed.
func (s *Service) VacuumIfDue(ctx context.Context) (bool, error) {
	every, _ := settings.Get(ctx, s.Settings, settings.VacuumIntervalDays)
	if every <= 0 {
		return false, nil
	}
	last, _ := settings.Get(ctx, s.Settings, settings.LastVacuum)
	if last > 0 && s.now().Sub(db.Time(last)) < time.Duration(every)*24*time.Hour {
		return false, nil
	}
	if _, err := s.DB.W.ExecContext(ctx, `VACUUM`); err != nil {
		return false, err
	}
	_, _ = s.DB.W.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return true, settings.Set(ctx, s.Settings, nil, settings.LastVacuum, db.Millis(s.now()))
}

// CapImages deletes the oldest cached images until the cache is below 90 %
// of its limit.
func (s *Service) CapImages(ctx context.Context) (int, int64, error) {
	if s.ImageDir == "" {
		return 0, 0, nil
	}
	maxMB, _ := settings.Get(ctx, s.Settings, settings.ImageCacheMaxMB)
	if maxMB <= 0 {
		return 0, 0, nil
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var (
		files []file
		total int64
	)
	err := filepath.WalkDir(s.ImageDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, file{p, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	limit := int64(maxMB) << 20
	if total <= limit {
		return 0, 0, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	target := limit * 9 / 10
	n, freed := 0, int64(0)
	for _, f := range files {
		if total <= target || ctx.Err() != nil {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
			freed += f.size
			n++
		}
	}
	return n, freed, nil
}

// Run does everything.
func (s *Service) Run(ctx context.Context) (Result, error) {
	r, err := s.Prune(ctx)
	if err != nil {
		return r, err
	}
	if r.ImageFiles, r.ImageBytes, err = s.CapImages(ctx); err != nil {
		return r, err
	}
	r.Vacuumed, err = s.VacuumIfDue(ctx)
	return r, err
}

func inList(values []string) (string, []any) {
	args := make([]any, len(values))
	marks := make([]string, len(values))
	for i, v := range values {
		args[i], marks[i] = v, "?"
	}
	return strings.Join(marks, ", "), args
}
