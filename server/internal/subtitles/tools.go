package subtitles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/charset"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
	subsync "github.com/mmzlopasa/locarr/server/internal/subtitles/sync"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/tracks"
)

// maxSubtitleFile bounds subtitle files Locarr reads.
const maxSubtitleFile = 16 << 20

// load reads and parses a subtitle file.
func (s *Service) load(sub Subtitle) (*subfmt.Doc, error) {
	p, err := lfs.MustExist(s.DataRoots, sub.Path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxSubtitleFile {
		return nil, errors.New("the subtitle file is too large")
	}
	b, err := os.ReadFile(p) // #nosec G304 -- checked with SafePath
	if err != nil {
		return nil, err
	}
	text, _, err := charset.Decode(b, sub.Language)
	if err != nil {
		return nil, err
	}
	return subfmt.Parse(text)
}

// PreviewCue is a line of a preview.
type PreviewCue struct {
	Start time.Duration `json:"startMs"`
	End   time.Duration `json:"endMs"`
	Text  string        `json:"text"`
	// Original is the source line of a machine translation ("" otherwise).
	Original string `json:"original,omitempty"`
}

// Preview returns the first n cues and the total count.
func (s *Service) Preview(ctx context.Context, id int64, n int) ([]PreviewCue, int, error) {
	sub, err := s.Get(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	d, err := s.load(sub)
	if err != nil {
		return nil, 0, err
	}
	cues := d.Sorted()
	orig := translationOriginal(sub)
	out := []PreviewCue{}
	for i, c := range cues {
		if i >= n {
			break
		}
		out = append(out, PreviewCue{Start: c.Start, End: c.End, Text: c.Text, Original: orig[c.Start.Milliseconds()]})
	}
	return out, len(cues), nil
}

// rewrite saves a changed document over the subtitle; the previous version
// goes to the recycle bin.
func (s *Service) rewrite(ctx context.Context, sub Subtitle, d *subfmt.Doc, reason string) error {
	format := sub.Format
	if format != subfmt.ASS && format != subfmt.SSA && format != subfmt.VTT {
		format = subfmt.SRT
	}
	text, err := d.Write(format)
	if err != nil {
		return err
	}
	if _, err := s.writeAtomic(ctx, sub.Path, []byte(text), sub.ItemRef, reason); err != nil {
		return err
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE subtitles SET encoding = 'utf-8', updated_at = ? WHERE id = ?`, db.Millis(s.now()), sub.ID)
	return err
}

// Shift moves every line of a subtitle by off.
func (s *Service) Shift(ctx context.Context, id int64, off time.Duration) (Subtitle, error) {
	if off == 0 || off > time.Hour || off < -time.Hour {
		return Subtitle{}, invalid("the shift must be between -1 h and +1 h and not zero")
	}
	sub, err := s.Get(ctx, id)
	if err != nil {
		return sub, err
	}
	d, err := s.load(sub)
	if err != nil {
		return sub, err
	}
	d.Shift(off)
	if err := s.rewrite(ctx, sub, d, "before a manual shift"); err != nil {
		return sub, err
	}
	sign := "+"
	if off < 0 {
		sign = ""
	}
	s.record(ctx, sub.ItemRef, TypeEdited, fmt.Sprintf("shifted %s by %s%d ms", filepath.Base(sub.Path), sign, off.Milliseconds()),
		map[string]any{"path": sub.Path, "tool": "shift", "offsetMs": off.Milliseconds()})
	return s.Get(ctx, id)
}

// ParseFPS reads a frame rate ("23.976", "25").
func ParseFPS(v string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 10 || f > 120 {
		return 0, invalid("frame rate %q must be a number between 10 and 120", v)
	}
	return f, nil
}

// ConvertFPS re-times a subtitle made for frame rate from to play at to
// (times are multiplied by from/to).
func (s *Service) ConvertFPS(ctx context.Context, id int64, from, to float64) (Subtitle, error) {
	if from == to {
		return Subtitle{}, invalid("the frame rates are the same")
	}
	sub, err := s.Get(ctx, id)
	if err != nil {
		return sub, err
	}
	d, err := s.load(sub)
	if err != nil {
		return sub, err
	}
	d.Scale(from / to)
	if err := s.rewrite(ctx, sub, d, "before a frame rate conversion"); err != nil {
		return sub, err
	}
	s.record(ctx, sub.ItemRef, TypeEdited, fmt.Sprintf("converted %s from %g to %g fps", filepath.Base(sub.Path), from, to),
		map[string]any{"path": sub.Path, "tool": "fps", "from": from, "to": to})
	return s.Get(ctx, id)
}

// Delete moves a subtitle file to the recycle bin and forgets it; a
// downloaded one can be blacklisted for its item.
func (s *Service) Delete(ctx context.Context, id int64, blacklist bool) error {
	sub, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	recycled := ""
	if _, err := os.Stat(sub.Path); err == nil {
		e, err := s.Recycle.Recycle(ctx, sub.Path, sub.ItemRef, "subtitle deleted")
		if err != nil {
			return err
		}
		recycled = e.RecycledPath
	}
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ?`, id); err != nil {
		return err
	}
	blacklist = blacklist && sub.Provider != "" && !sub.Translated()
	if blacklist {
		if _, err := s.DB.W.ExecContext(ctx, `INSERT OR IGNORE INTO subtitle_blacklist (item_ref, provider, provider_id, language, release, at)
			VALUES (?, ?, ?, ?, ?, ?)`, sub.ItemRef, sub.Provider, sub.ProviderID, sub.Language, sub.Release, db.Millis(s.now())); err != nil {
			return err
		}
	}
	msg := "moved subtitle " + filepath.Base(sub.Path) + " to the recycle bin"
	if blacklist {
		msg += " and blacklisted it"
	}
	s.record(ctx, sub.ItemRef, TypeDeleted, msg, map[string]any{"path": sub.Path, "recycled": recycled, "blacklisted": blacklist,
		"provider": sub.Provider, "language": sub.Language})
	if ref, err := library.ParseRef(sub.ItemRef); err == nil {
		if t, err := s.Target(ctx, ref); err == nil {
			_ = s.UpdateState(ctx, &t)
		}
	}
	return nil
}

// BlacklistEntry is a subtitle rejected for an item.
type BlacklistEntry struct {
	ID         int64     `json:"id"`
	ItemRef    string    `json:"itemRef"`
	Provider   string    `json:"provider"`
	ProviderID string    `json:"providerId"`
	Language   string    `json:"language"`
	Release    string    `json:"release"`
	At         time.Time `json:"at"`
}

// Blacklisted returns an item's blacklist.
func (s *Service) Blacklisted(ctx context.Context, ref string) ([]BlacklistEntry, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, item_ref, provider, provider_id, language, release, at FROM subtitle_blacklist WHERE item_ref = ? ORDER BY at DESC`, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BlacklistEntry{}
	for rows.Next() {
		var (
			e  BlacklistEntry
			at int64
		)
		if err := rows.Scan(&e.ID, &e.ItemRef, &e.Provider, &e.ProviderID, &e.Language, &e.Release, &at); err != nil {
			return nil, err
		}
		e.At = db.Time(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Unblacklist removes a blacklist entry.
func (s *Service) Unblacklist(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM subtitle_blacklist WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Sync references.
const (
	RefAudio = "audio"
)

type syncArgs struct {
	SubtitleID int64  `json:"subtitleId"`
	Reference  string `json:"reference"` // audio, audio:N, track:N (embedded text track), subtitle:ID
	Auto       bool   `json:"auto,omitempty"`
}

// QueueSync queues a re-timing of a subtitle against a reference.
func (s *Service) QueueSync(ctx context.Context, id int64, reference string, auto bool) (Subtitle, error) {
	sub, err := s.Get(ctx, id)
	if err != nil {
		return sub, err
	}
	if reference == "" {
		reference = RefAudio
	}
	kind, _, _ := strings.Cut(reference, ":")
	switch kind {
	case "audio", "track":
		if !s.SyncAvailable() {
			return sub, ErrSyncUnavailable
		}
	case "subtitle":
	default:
		return sub, invalid("reference must be audio, track:N or subtitle:ID")
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE subtitles SET sync_state = 'running', sync_message = '' WHERE id = ?`, id); err != nil {
		return sub, err
	}
	if _, _, err := s.Jobs.Enqueue(ctx, TaskSync, syncArgs{SubtitleID: id, Reference: reference, Auto: auto},
		jobs.EnqueueOptions{UniqueKey: "subtitles-sync:" + strconv.FormatInt(id, 10)}); err != nil {
		return sub, err
	}
	return s.Get(ctx, id)
}

func (s *Service) maybeAutoSync(ctx context.Context, sub Subtitle, results []Scored) {
	on, _ := settings.Get(ctx, s.Settings, AutoSync)
	if !on || !s.SyncAvailable() {
		return
	}
	for _, r := range results {
		if r.Provider == sub.Provider && r.ID == sub.ProviderID && r.HashMatch {
			return // made for this file
		}
	}
	if _, err := s.QueueSync(ctx, sub.ID, RefAudio, true); err != nil {
		s.log().Warn("queue automatic sync", "subtitle", sub.ID, "err", err)
	}
}

// RunSync re-times a subtitle (the sync job).
func (s *Service) RunSync(ctx context.Context, a syncArgs) error {
	sub, err := s.Get(ctx, a.SubtitleID)
	if err != nil {
		return jobs.Permanent(err)
	}
	fail := func(state, msg string) error {
		_, _ = s.DB.W.ExecContext(context.WithoutCancel(ctx), `UPDATE subtitles SET sync_state = ?, sync_message = ?, sync_at = ? WHERE id = ?`,
			state, msg, db.Millis(s.now()), sub.ID)
		s.record(ctx, sub.ItemRef, TypeSynced, "could not re-time "+filepath.Base(sub.Path)+": "+msg,
			map[string]any{"path": sub.Path, "state": state, "reference": a.Reference, "message": msg})
		return nil
	}
	ref, err := library.ParseRef(sub.ItemRef)
	if err != nil {
		return fail("failed", err.Error())
	}
	t, err := s.Target(ctx, ref)
	if err != nil {
		return fail("failed", err.Error())
	}
	video, err := lfs.MustExist(s.DataRoots, t.Path)
	if err != nil {
		return fail("failed", "the video file is missing")
	}
	d, err := s.load(sub)
	if err != nil {
		return fail("failed", err.Error())
	}
	kind, arg, _ := strings.Cut(a.Reference, ":")
	n, _ := strconv.Atoi(arg)
	var signal []float64
	switch kind {
	case "audio":
		if !s.SyncAvailable() {
			return fail("unavailable", ErrSyncUnavailable.Error())
		}
		if arg == "" {
			n = s.audioTrackFor(ctx, t.FileID, sub.Language)
		}
		signal, err = s.FFmpeg.SpeechOf(ctx, video, n)
	case "track":
		var cues []subfmt.Cue
		if cues, err = s.FFmpeg.TextTrack(ctx, video, n); err == nil {
			signal = subsync.CueSignal(cues, 1, 0)
		}
	case "subtitle":
		id, _ := strconv.ParseInt(arg, 10, 64)
		var other Subtitle
		if other, err = s.Get(ctx, id); err == nil {
			if other.MediaFileID != sub.MediaFileID || other.ID == sub.ID {
				err = errors.New("the reference subtitle belongs to another file")
			} else {
				var od *subfmt.Doc
				if od, err = s.load(other); err == nil {
					signal = subsync.CueSignal(od.Cues, 1, 0)
				}
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fail("failed", err.Error())
	}
	res, err := subsync.Align(signal, d.Cues)
	if err != nil {
		return fail("failed", err.Error())
	}
	if !res.Confident {
		return fail("not_confident", fmt.Sprintf("no reliable match (score %.1f, needs %.0f); the subtitle was left as it was", res.Score, subsync.Threshold))
	}
	if res.Offset == 0 && res.Scale == 1 {
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE subtitles SET sync_state = 'done', sync_message = 'already in sync', sync_offset_ms = 0, sync_scale = 1,
			sync_at = ? WHERE id = ?`, db.Millis(s.now()), sub.ID)
		s.record(ctx, sub.ItemRef, TypeSynced, filepath.Base(sub.Path)+" is already in sync with the "+refName(kind),
			map[string]any{"path": sub.Path, "state": "done", "offsetMs": 0, "scale": 1, "score": res.Score, "reference": a.Reference})
		return nil
	}
	subsync.Apply(d, res)
	if err := s.rewrite(ctx, sub, d, "before re-timing"); err != nil {
		return fail("failed", err.Error())
	}
	msg := fmt.Sprintf("re-timed against the %s: %+d ms", refName(kind), res.Offset.Milliseconds())
	if res.Scale != 1 {
		msg += fmt.Sprintf(", frame rate ×%.5f", res.Scale)
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE subtitles SET sync_state = 'done', sync_message = ?, sync_offset_ms = ?, sync_scale = ?, sync_at = ? WHERE id = ?`,
		msg, res.Offset.Milliseconds(), res.Scale, db.Millis(s.now()), sub.ID)
	s.record(ctx, sub.ItemRef, TypeSynced, filepath.Base(sub.Path)+" "+msg, map[string]any{"path": sub.Path, "state": "done", "offsetMs": res.Offset.Milliseconds(),
		"scale": res.Scale, "score": res.Score, "matched": res.Matched, "before": res.Before, "reference": a.Reference, "auto": a.Auto})
	return err
}

func refName(kind string) string {
	switch kind {
	case "track":
		return "embedded subtitle track"
	case "subtitle":
		return "other subtitle"
	}
	return "audio"
}

// audioTrackFor picks the audio track to listen to: the default one, else the first.
func (s *Service) audioTrackFor(ctx context.Context, fileID int64, _ string) int {
	trs, _, _, err := s.Tracks(ctx, fileID)
	if err != nil {
		return 0
	}
	for _, t := range trs {
		if t.Kind == tracks.Audio && t.Default {
			return t.Index
		}
	}
	return 0
}
