package subtitles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/charset"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
)

// Subtitle is a subtitle file Locarr knows next to a media file.
type Subtitle struct {
	ID              int64      `json:"id"`
	MediaFileID     int64      `json:"mediaFileId"`
	ItemRef         string     `json:"itemRef"`
	Path            string     `json:"path"`
	Language        string     `json:"language"`
	Forced          bool       `json:"forced"`
	HearingImpaired bool       `json:"hearingImpaired"`
	Format          string     `json:"format"`
	Source          string     `json:"source"`
	Provider        string     `json:"provider"`
	ProviderID      string     `json:"providerId"`
	Release         string     `json:"release"`
	Score           *int       `json:"score,omitempty"`
	Encoding        string     `json:"encoding"`
	SyncState       string     `json:"syncState"`
	SyncMessage     string     `json:"syncMessage"`
	SyncOffsetMs    int64      `json:"syncOffsetMs"`
	SyncScale       float64    `json:"syncScale"`
	SyncAt          *time.Time `json:"syncAt,omitempty"`
	AddedAt         time.Time  `json:"addedAt"`
}

// Want returns the profile key the subtitle satisfies.
func (s Subtitle) Want() Want {
	w := Want{Language: s.Language, Forced: s.Forced, HearingImpaired: HIInclude}
	if s.HearingImpaired {
		w.HearingImpaired = HIOnly
	}
	return w
}

const subCols = `id, media_file_id, item_ref, path, language, forced, hearing_impaired, format, source, provider, provider_id, release, score,
	encoding, sync_state, sync_message, sync_offset_ms, sync_scale, sync_at, added_at`

func scanSub(row interface{ Scan(...any) error }) (Subtitle, error) {
	var (
		s      Subtitle
		score  sql.NullInt64
		syncAt sql.NullInt64
		added  int64
	)
	err := row.Scan(&s.ID, &s.MediaFileID, &s.ItemRef, &s.Path, &s.Language, &s.Forced, &s.HearingImpaired, &s.Format, &s.Source, &s.Provider,
		&s.ProviderID, &s.Release, &score, &s.Encoding, &s.SyncState, &s.SyncMessage, &s.SyncOffsetMs, &s.SyncScale, &syncAt, &added)
	if score.Valid {
		v := int(score.Int64)
		s.Score = &v
	}
	s.SyncAt, s.AddedAt = db.NullTime(syncAt), db.Time(added)
	return s, err
}

// Get returns one subtitle.
func (s *Service) Get(ctx context.Context, id int64) (Subtitle, error) {
	sub, err := scanSub(s.DB.R.QueryRowContext(ctx, `SELECT `+subCols+` FROM subtitles WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sub, ErrNotFound
	}
	return sub, err
}

// FileSubtitles lists the subtitles of a media file.
func (s *Service) FileSubtitles(ctx context.Context, fileID int64) ([]Subtitle, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+subCols+` FROM subtitles WHERE media_file_id = ? ORDER BY language, forced, hearing_impaired, id`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subtitle{}
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// SidecarPath is where a subtitle for video goes: the video's name with
// ".<lang>[.forced|.hi].<ext>" ("Movie (2008).hr.srt", "….hr.forced.srt").
func SidecarPath(video, lang string, forced, hi bool, ext string) string {
	base := strings.TrimSuffix(video, filepath.Ext(video))
	name := base + "." + lang
	switch {
	case forced:
		name += ".forced"
	case hi:
		name += ".hi"
	}
	return name + "." + strings.TrimPrefix(ext, ".")
}

// prepared is a decoded, checked subtitle ready to be written.
type prepared struct {
	Text     string
	Format   string
	Encoding string
	Cues     int
}

// Prepare decodes a downloaded file (any encoding, CRLF), checks that it is
// a usable SRT/ASS/SSA/VTT and converts it to SRT when asked.
func Prepare(data []byte, lang, preferred string) (prepared, error) {
	text, enc, err := charset.Decode(data, lang)
	if err != nil {
		return prepared{}, err
	}
	text = subfmt.Normalize(text)
	doc, err := subfmt.Parse(text)
	if err != nil {
		return prepared{}, err
	}
	if err := doc.Valid(); err != nil {
		return prepared{}, err
	}
	format := doc.Format
	if preferred == subfmt.SRT && format != subfmt.SRT {
		format = subfmt.SRT
		if text, err = doc.Write(subfmt.SRT); err != nil {
			return prepared{}, err
		}
	} else if format == subfmt.SRT {
		// Rewritten: numbered, sorted, LF line endings, no BOM.
		text, _ = doc.Write(subfmt.SRT)
	}
	return prepared{Text: text, Format: format, Encoding: enc, Cues: len(doc.Cues)}, nil
}

// writeAtomic writes data next to dst and renames it into place; an
// existing dst goes to the recycle bin first (never deleted).
func (s *Service) writeAtomic(ctx context.Context, dst string, data []byte, ref, reason string) (recycled string, err error) {
	safe, err := lfs.SafePath(s.DataRoots, dst)
	if err != nil {
		return "", err
	}
	tmp := safe + ".locarr-new"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { // #nosec G306 -- media files are shared with media servers
		return "", err
	}
	if _, err := os.Stat(safe); err == nil {
		e, err := s.Recycle.Recycle(ctx, safe, ref, reason)
		if err != nil {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("could not move the old subtitle to the recycle bin: %w", err)
		}
		recycled = e.RecycledPath
	}
	if err := os.Rename(tmp, safe); err != nil {
		_ = os.Remove(tmp)
		return recycled, err
	}
	return recycled, nil
}

// saveInput describes a subtitle to save for a target.
type saveInput struct {
	Want       Want
	Data       []byte
	Provider   string
	ProviderID string
	Release    string
	Score      int
	// Replace: whatever exists for this language goes to the recycle bin
	// (manual choice); otherwise only a lower-scored download is replaced.
	Replace bool
}

// ErrNotBetter means an existing subtitle is at least as good.
var ErrNotBetter = errors.New("an existing subtitle is at least as good")

func (s *Service) save(ctx context.Context, t *Target, in saveInput) (Subtitle, string, error) {
	preferred, _ := settings.Get(ctx, s.Settings, PreferredFormat)
	p, err := Prepare(in.Data, in.Want.Language, preferred)
	if err != nil {
		return Subtitle{}, "", err
	}
	hi := in.Want.HearingImpaired == HIOnly
	existing, err := s.FileSubtitles(ctx, t.FileID)
	if err != nil {
		return Subtitle{}, "", err
	}
	var old *Subtitle
	for i, e := range existing {
		if e.Language == in.Want.Language && e.Forced == in.Want.Forced && e.HearingImpaired == hi && old == nil {
			old = &existing[i]
		}
	}
	if old != nil && !in.Replace {
		if old.Source == "disk" || old.Score != nil && *old.Score >= in.Score {
			return *old, "", ErrNotBetter
		}
	}
	dst := SidecarPath(t.Path, in.Want.Language, in.Want.Forced, hi, p.Format)
	recycled, err := s.writeAtomic(ctx, dst, []byte(p.Text), t.Ref.String(), "replaced by a better subtitle")
	if err != nil {
		return Subtitle{}, "", err
	}
	// The old file under another name (other format or flag) goes to the recycle bin too.
	if old != nil && old.Path != dst {
		if _, err := os.Stat(old.Path); err == nil {
			if e, err := s.Recycle.Recycle(ctx, old.Path, t.Ref.String(), "replaced by a better subtitle"); err == nil {
				recycled = e.RecycledPath
			}
		}
	}
	now := db.Millis(s.now())
	var id int64
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if old != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ?`, old.ID); err != nil {
				return err
			}
		}
		if err := tx.QueryRowContext(ctx, `INSERT INTO subtitles (media_file_id, item_ref, path, language, forced, hearing_impaired, format, source,
			provider, provider_id, release, score, encoding, added_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'downloaded', ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (path) DO UPDATE SET media_file_id = excluded.media_file_id, item_ref = excluded.item_ref, language = excluded.language,
			forced = excluded.forced, hearing_impaired = excluded.hearing_impaired, format = excluded.format, source = 'downloaded',
			provider = excluded.provider, provider_id = excluded.provider_id, release = excluded.release, score = excluded.score,
			encoding = excluded.encoding, sync_state = '', sync_message = '', sync_offset_ms = 0, sync_scale = 1, sync_at = NULL,
			updated_at = excluded.updated_at RETURNING id`,
			t.FileID, t.Ref.String(), dst, in.Want.Language, in.Want.Forced, hi, p.Format, in.Provider, in.ProviderID, in.Release, in.Score,
			p.Encoding, now, now).Scan(&id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO subtitle_history (item_ref, provider, provider_id, language, score, path, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			t.Ref.String(), in.Provider, in.ProviderID, in.Want.Language, in.Score, dst, now)
		return err
	})
	if err != nil {
		return Subtitle{}, recycled, err
	}
	sub, err := s.Get(ctx, id)
	return sub, recycled, err
}
