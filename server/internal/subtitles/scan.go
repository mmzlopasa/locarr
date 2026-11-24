package subtitles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/importer"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/tracks"
)

// Tracks returns the stored tracks of a media file.
func (s *Service) Tracks(ctx context.Context, fileID int64) ([]tracks.Track, string, bool, error) {
	var (
		probeErr string
		at       int64
	)
	err := s.DB.R.QueryRowContext(ctx, `SELECT probed_at, error FROM media_file_probes WHERE media_file_id = ?`, fileID).Scan(&at, &probeErr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind, idx, codec, language, name, is_default, forced, hearing_impaired, text
		FROM media_file_tracks WHERE media_file_id = ? ORDER BY kind, idx`, fileID)
	if err != nil {
		return nil, "", true, err
	}
	defer rows.Close()
	out := []tracks.Track{}
	for rows.Next() {
		var t tracks.Track
		if err := rows.Scan(&t.Kind, &t.Index, &t.Codec, &t.Language, &t.Name, &t.Default, &t.Forced, &t.HearingImpaired, &t.Text); err != nil {
			return nil, "", true, err
		}
		out = append(out, t)
	}
	return out, probeErr, true, rows.Err()
}

// Probe reads a media file's tracks and stores them.
func (s *Service) Probe(ctx context.Context, fileID int64, path string) ([]tracks.Track, error) {
	list, perr := tracks.Read(path)
	msg := ""
	if perr != nil {
		msg = perr.Error()
	}
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_file_tracks WHERE media_file_id = ?`, fileID); err != nil {
			return err
		}
		for _, t := range list {
			if _, err := tx.ExecContext(ctx, `INSERT INTO media_file_tracks (media_file_id, kind, idx, codec, language, name, is_default, forced,
				hearing_impaired, text) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, fileID, t.Kind, t.Index, t.Codec, t.Language, t.Name, t.Default,
				t.Forced, t.HearingImpaired, t.Text); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO media_file_probes (media_file_id, probed_at, error) VALUES (?, ?, ?)
			ON CONFLICT (media_file_id) DO UPDATE SET probed_at = excluded.probed_at, error = excluded.error`, fileID, db.Millis(s.now()), msg)
		return err
	})
	return list, err
}

// sidecarInfo reads language and flags from a sidecar suffix (".hr.forced.srt").
func sidecarInfo(suffix string) (lang string, forced, hi bool, format string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(strings.ToLower(suffix), "."), ".")
	if len(parts) == 0 {
		return "", false, false, "", false
	}
	format = parts[len(parts)-1]
	if !providers.SubtitleExts["."+format] {
		return "", false, false, "", false
	}
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "forced", "foreign":
			forced = true
		case "hi", "sdh", "cc":
			hi = true
		case "default", TranslatedMarker:
		default:
			if l := languages.FromContainer(p); l != "" && lang == "" {
				lang = l
			} else if c, ok := languages.FromWord(p, false); ok && lang == "" {
				lang = c
			}
		}
	}
	if lang == "" {
		lang = languages.Unknown
	}
	return lang, forced, hi, format, true
}

// ScanDisk records the subtitle files next to a target's video and drops
// rows whose files are gone. Returns what was added.
func (s *Service) ScanDisk(ctx context.Context, t *Target) ([]Subtitle, error) {
	dir := filepath.Dir(t.Path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	known, err := s.FileSubtitles(ctx, t.FileID)
	if err != nil {
		return nil, err
	}
	byPath := map[string]bool{}
	base := strings.TrimSuffix(t.Path, filepath.Ext(t.Path)) + "."
	for _, k := range known {
		// Gone, or named after a previous file (the video was upgraded and renamed): forget it.
		if _, err := os.Stat(k.Path); errors.Is(err, os.ErrNotExist) || !strings.HasPrefix(strings.ToLower(k.Path), strings.ToLower(base)) {
			if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ?`, k.ID); err != nil {
				return nil, err
			}
			continue
		}
		byPath[k.Path] = true
	}
	vbase := strings.ToLower(strings.TrimSuffix(filepath.Base(t.Path), filepath.Ext(t.Path))) + "."
	marker, _ := settings.Get(ctx, s.Settings, TranslationSuffix)
	var added []Subtitle
	now := db.Millis(s.now())
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(strings.ToLower(e.Name()), vbase) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if byPath[p] || strings.HasSuffix(e.Name(), ".locarr-new") {
			continue
		}
		suffix := importer.SidecarSuffix(t.Path, p)
		lang, forced, hi, format, ok := sidecarInfo(suffix)
		if !ok {
			continue
		}
		if format == subfmt.VTT || format == subfmt.SRT || format == subfmt.ASS || format == subfmt.SSA {
			source := SourceDisk
			if translatedName(suffix, marker) {
				source = SourceTranslated
			}
			var id int64
			err := s.DB.W.QueryRowContext(ctx, `INSERT INTO subtitles (media_file_id, item_ref, path, language, forced, hearing_impaired, format,
				source, added_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (path) DO UPDATE SET media_file_id = excluded.media_file_id, item_ref = excluded.item_ref RETURNING id`,
				t.FileID, t.Ref.String(), p, lang, forced, hi, format, source, now, now).Scan(&id)
			if err != nil {
				return added, err
			}
			if sub, err := s.Get(ctx, id); err == nil {
				added = append(added, sub)
			}
		}
	}
	s.retireTranslations(ctx, t)
	return added, nil
}

// translatedName reports a sidecar suffix that marks a machine translation
// (".hr.translated.srt", or the configured marker).
func translatedName(suffix, marker string) bool {
	for _, p := range strings.Split(strings.ToLower(suffix), ".") {
		if p == TranslatedMarker || marker != "" && p == strings.ToLower(marker) {
			return true
		}
	}
	return false
}

// satisfies reports whether a present subtitle (file or embedded track) covers a want.
func satisfies(w Want, lang string, forced, hi bool) bool {
	if lang != w.Language || forced != w.Forced {
		return false
	}
	switch w.HearingImpaired {
	case HIOnly:
		return hi
	case HIExclude:
		return !hi
	}
	return true
}

// WantStatus is how one profile entry stands for a target.
type WantStatus struct {
	Want
	Key string `json:"key"`
	// Status: present (a file), embedded (a track in the video), missing, skipped (cutoff reached).
	Status     string `json:"status"`
	SubtitleID int64  `json:"subtitleId,omitempty"`
	TrackIndex *int   `json:"trackIndex,omitempty"`
}

// Status computes what a target has and misses.
func (s *Service) Status(ctx context.Context, t *Target) (ItemProfile, []WantStatus, error) {
	kind := "movie"
	if t.Kind == providers.KindEpisode {
		kind = "series"
	}
	ip, err := s.EffectiveProfile(ctx, t.ItemID, kind)
	if err != nil || ip.Profile == nil {
		return ip, []WantStatus{}, err
	}
	subs, err := s.FileSubtitles(ctx, t.FileID)
	if err != nil {
		return ip, nil, err
	}
	trs, _, _, err := s.Tracks(ctx, t.FileID)
	if err != nil {
		return ip, nil, err
	}
	out := make([]WantStatus, 0, len(ip.Profile.Languages))
	reached := false
	for _, w := range ip.Profile.Languages {
		st := WantStatus{Want: w, Key: w.Key(), Status: "missing"}
		for _, sub := range subs {
			if !sub.Translated() && satisfies(w, sub.Language, sub.Forced, sub.HearingImpaired) {
				st.Status, st.SubtitleID = "present", sub.ID
				break
			}
		}
		if st.Status == "missing" && ip.Profile.SkipEmbedded {
			for _, tr := range trs {
				if tr.Kind == tracks.Subtitle && satisfies(w, tr.Language, tr.Forced, tr.HearingImpaired) {
					idx := tr.Index
					st.Status, st.TrackIndex = "embedded", &idx
					break
				}
			}
		}
		if st.Status == "missing" {
			// A machine translation covers it until a human subtitle is found.
			for _, sub := range subs {
				if sub.Translated() && satisfies(w, sub.Language, sub.Forced, sub.HearingImpaired) {
					st.Status, st.SubtitleID = "translated", sub.ID
					break
				}
			}
		}
		if st.Status == "missing" && reached {
			st.Status = "skipped"
		}
		if st.Status != "missing" && st.Status != "skipped" && ip.Profile.Cutoff {
			reached = true
		}
		out = append(out, st)
	}
	if reached {
		for i := range out {
			if out[i].Status == "missing" {
				out[i].Status = "skipped"
			}
		}
	}
	return ip, out, nil
}

// missingWants returns the wanted entries that are missing.
func missingWants(st []WantStatus) []Want {
	var out []Want
	for _, w := range st {
		if w.Status == "missing" {
			out = append(out, w.Want)
		}
	}
	return out
}

// UpdateState recomputes a target's row in subtitle_state.
func (s *Service) UpdateState(ctx context.Context, t *Target) error {
	ip, st, err := s.Status(ctx, t)
	if err != nil {
		return err
	}
	keys := []string{}
	translated := false
	for _, w := range st {
		switch w.Status {
		case "missing":
			keys = append(keys, w.Key)
		case "translated":
			translated = true
		}
	}
	// Translation: once the profile's wait after the file arrived is over.
	var translateAt any
	if ip.Profile != nil && ip.Profile.TranslateAfterHours > 0 && len(translatableKeys(ip.Profile, keys)) > 0 {
		translateAt = t.AddedAt + int64(ip.Profile.TranslateAfterHours)*3_600_000
	}
	upgradable := translated // keep looking for a human subtitle
	if days, _ := settings.Get(ctx, s.Settings, UpgradeDays); days > 0 && s.now().Sub(db.Time(t.AddedAt)) < time.Duration(days)*24*time.Hour {
		subs, err := s.FileSubtitles(ctx, t.FileID)
		if err != nil {
			return err
		}
		for _, sub := range subs {
			if sub.Source == "downloaded" && sub.Score != nil && *sub.Score < PointsHash {
				upgradable = true
			}
		}
	}
	b, _ := json.Marshal(keys)
	now := db.Millis(s.now())
	_, err = s.DB.W.ExecContext(ctx, `INSERT INTO subtitle_state (item_ref, item_id, entity_id, kind, media_file_id, missing, missing_count, upgradable,
		next_search_at, file_added_at, updated_at, translate_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_ref) DO UPDATE SET item_id = excluded.item_id, media_file_id = excluded.media_file_id, missing = excluded.missing,
		missing_count = excluded.missing_count, upgradable = excluded.upgradable, file_added_at = excluded.file_added_at,
		updated_at = excluded.updated_at, translate_at = excluded.translate_at,
		translate_retry_at = CASE WHEN subtitle_state.media_file_id <> excluded.media_file_id THEN NULL ELSE subtitle_state.translate_retry_at END,
		translate_attempts = CASE WHEN subtitle_state.media_file_id <> excluded.media_file_id THEN 0 ELSE subtitle_state.translate_attempts END,
		-- A new file starts its searches over.
		searches = CASE WHEN subtitle_state.media_file_id <> excluded.media_file_id THEN 0 ELSE subtitle_state.searches END,
		next_search_at = CASE WHEN subtitle_state.media_file_id <> excluded.media_file_id THEN excluded.next_search_at
			ELSE COALESCE(subtitle_state.next_search_at, excluded.next_search_at) END`,
		t.Ref.String(), t.ItemID, t.Ref.ID, t.Kind, t.FileID, string(b), len(keys), upgradable, now, t.AddedAt, now, translateAt)
	return err
}

// RefreshTarget probes (when needed), scans the disk and updates the state of a target.
func (s *Service) RefreshTarget(ctx context.Context, t *Target, probe bool) ([]Subtitle, error) {
	if _, _, probed, _ := s.Tracks(ctx, t.FileID); probe || !probed {
		if _, err := s.Probe(ctx, t.FileID, t.Path); err != nil {
			return nil, err
		}
	}
	added, err := s.ScanDisk(ctx, t)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return added, err
	}
	for _, a := range added {
		s.record(ctx, t.Ref.String(), TypeFound, "found subtitle "+filepath.Base(a.Path)+" on disk",
			map[string]any{"path": a.Path, "language": a.Language, "forced": a.Forced, "hearingImpaired": a.HearingImpaired})
	}
	return added, s.UpdateState(ctx, t)
}

// refreshItem updates the state of every target of a library item.
func (s *Service) refreshItem(ctx context.Context, itemID int64) {
	ts, err := s.ItemTargets(ctx, itemID)
	if err != nil {
		s.log().Warn("subtitle state", "item", itemID, "err", err)
		return
	}
	for i := range ts {
		if err := s.UpdateState(ctx, &ts[i]); err != nil {
			s.log().Warn("subtitle state", "item", ts[i].Ref.String(), "err", err)
		}
	}
}

// scanArgs are the arguments of the scan job.
type scanArgs struct {
	// StateOnly recomputes what is missing without reading the disk.
	StateOnly bool `json:"stateOnly,omitempty"`
}

// QueueStateRefresh recomputes what every item misses in the background.
func (s *Service) QueueStateRefresh(ctx context.Context) { s.queueRefresh(ctx) }

func (s *Service) queueRefresh(ctx context.Context) {
	if s.Jobs == nil {
		return
	}
	if _, _, err := s.Jobs.Enqueue(ctx, TaskScan, scanArgs{StateOnly: true}, jobs.EnqueueOptions{UniqueKey: "subtitles-state"}); err != nil {
		s.log().Warn("queue subtitle state refresh", "err", err)
	}
}

// ScanAll refreshes every movie and episode with a file (existing libraries):
// reads tracks of files not read yet, records subtitles found on disk and
// recomputes what is missing. Orphaned state rows are dropped.
func (s *Service) ScanAll(ctx context.Context, stateOnly bool) (int, error) {
	n := 0
	err := s.AllTargets(ctx, func(t Target) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var err error
		if stateOnly {
			err = s.UpdateState(ctx, &t)
		} else {
			_, err = s.RefreshTarget(ctx, &t, false)
		}
		if err != nil {
			s.log().Warn("subtitle scan", "item", t.Ref.String(), "err", err)
		}
		n++
		return nil
	})
	if err != nil {
		return n, err
	}
	// Rows of removed files go with them (foreign key); an episode that now has another file is refreshed above.
	_, err = s.DB.W.ExecContext(ctx, `DELETE FROM subtitle_state WHERE kind = 'episode'
		AND NOT EXISTS (SELECT 1 FROM episodes e WHERE e.id = subtitle_state.entity_id AND e.file_id = subtitle_state.media_file_id)`)
	return n, err
}

// Imported is called after a movie or episode file was imported.
func (s *Service) Imported(ctx context.Context, refs []library.Ref) {
	for _, ref := range refs {
		if _, _, err := s.Jobs.Enqueue(ctx, TaskSearchItem, searchArgs{Ref: ref.String(), Reason: "import", Probe: true},
			jobs.EnqueueOptions{UniqueKey: "subtitles-search:" + ref.String()}); err != nil {
			s.log().Warn("queue subtitle search", "item", ref.String(), "err", err)
		}
	}
}
