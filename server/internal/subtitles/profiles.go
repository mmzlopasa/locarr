package subtitles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// Hearing-impaired preferences of a profile language.
const (
	HIInclude = "include" // normal preferred, HI accepted
	HIPrefer  = "prefer"
	HIExclude = "exclude"
	HIOnly    = "only"
)

// NoProfile as an item override means "no subtitles for this item".
const NoProfile = -1

// Want is one entry of a profile: a language and how.
type Want struct {
	Language        string `json:"language"`
	Forced          bool   `json:"forced"`
	HearingImpaired string `json:"hearingImpaired"`
	// Translator is the translation service for this language ("" = the
	// default for the language, "none" = never translate it).
	Translator string `json:"translator,omitempty"`
}

// Key is the stable name of a wanted subtitle ("hr", "hr:forced", "en:hi").
func (w Want) Key() string {
	k := w.Language
	if w.Forced {
		k += ":forced"
	}
	if w.HearingImpaired == HIOnly {
		k += ":hi"
	}
	return k
}

// Profile is a subtitle language profile.
type Profile struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Languages    []Want `json:"languages"`
	SkipEmbedded bool   `json:"skipEmbedded"`
	Cutoff       bool   `json:"cutoff"`
	// TranslateAfterHours: translate a missing language this long after the
	// file arrived when no subtitle was found (0 = never automatically).
	TranslateAfterHours int       `json:"translateAfterHours"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// ProfileInput is a new or changed profile.
type ProfileInput struct {
	Name                string
	Languages           []Want
	SkipEmbedded        bool
	Cutoff              bool
	TranslateAfterHours int
}

// ValidationError is input Locarr refuses.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

func (in *ProfileInput) check() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return invalid("a profile needs a name of at most 100 characters")
	}
	if len(in.Languages) == 0 || len(in.Languages) > 20 {
		return invalid("a profile needs between 1 and 20 languages")
	}
	if in.TranslateAfterHours < 0 || in.TranslateAfterHours > 24*365 {
		return invalid("translateAfterHours must be between 0 and 8760")
	}
	seen := map[string]bool{}
	for i := range in.Languages {
		w := &in.Languages[i]
		w.Language = strings.ToLower(strings.TrimSpace(w.Language))
		if _, ok := languages.ByCode(w.Language); !ok {
			return invalid("unknown language %q", w.Language)
		}
		switch w.HearingImpaired {
		case "":
			w.HearingImpaired = HIInclude
		case HIInclude, HIPrefer, HIExclude, HIOnly:
		default:
			return invalid("hearingImpaired must be include, prefer, exclude or only")
		}
		switch w.Translator {
		case "", TranslatorNone:
		default:
			if !knownTranslator(w.Translator) {
				return invalid("unknown translation service %q", w.Translator)
			}
		}
		if seen[w.Key()] {
			return invalid("%s is in the profile twice", w.Key())
		}
		seen[w.Key()] = true
	}
	return nil
}

const profileCols = `id, name, languages, skip_embedded, cutoff, translate_after_hours, created_at, updated_at`

func scanProfile(row interface{ Scan(...any) error }) (Profile, error) {
	var (
		p          Profile
		langs      string
		cr, up     int64
		skip, stop bool
	)
	if err := row.Scan(&p.ID, &p.Name, &langs, &skip, &stop, &p.TranslateAfterHours, &cr, &up); err != nil {
		return p, err
	}
	p.SkipEmbedded, p.Cutoff, p.CreatedAt, p.UpdatedAt = skip, stop, db.Time(cr), db.Time(up)
	if err := json.Unmarshal([]byte(langs), &p.Languages); err != nil {
		return p, err
	}
	return p, nil
}

// Profiles lists the profiles by name.
func (s *Service) Profiles(ctx context.Context) ([]Profile, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+profileCols+` FROM subtitle_profiles ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Profile returns one profile.
func (s *Service) Profile(ctx context.Context, id int64) (Profile, error) {
	p, err := scanProfile(s.DB.R.QueryRowContext(ctx, `SELECT `+profileCols+` FROM subtitle_profiles WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// ErrDuplicateName is a profile name that is taken.
var ErrDuplicateName = errors.New("a profile with this name already exists")

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateProfile adds a profile.
func (s *Service) CreateProfile(ctx context.Context, in ProfileInput) (Profile, error) {
	if err := in.check(); err != nil {
		return Profile{}, err
	}
	b, _ := json.Marshal(in.Languages)
	now := db.Millis(s.now())
	var id int64
	err := s.DB.W.QueryRowContext(ctx, `INSERT INTO subtitle_profiles (name, languages, skip_embedded, cutoff, translate_after_hours, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, in.Name, string(b), in.SkipEmbedded, in.Cutoff, in.TranslateAfterHours, now, now).Scan(&id)
	if isUnique(err) {
		return Profile{}, ErrDuplicateName
	}
	if err != nil {
		return Profile{}, err
	}
	return s.Profile(ctx, id)
}

// UpdateProfile changes a profile and refreshes what items using it miss.
func (s *Service) UpdateProfile(ctx context.Context, id int64, in ProfileInput) (Profile, error) {
	if err := in.check(); err != nil {
		return Profile{}, err
	}
	b, _ := json.Marshal(in.Languages)
	res, err := s.DB.W.ExecContext(ctx, `UPDATE subtitle_profiles SET name = ?, languages = ?, skip_embedded = ?, cutoff = ?, translate_after_hours = ?,
		updated_at = ? WHERE id = ?`, in.Name, string(b), in.SkipEmbedded, in.Cutoff, in.TranslateAfterHours, db.Millis(s.now()), id)
	if isUnique(err) {
		return Profile{}, ErrDuplicateName
	}
	if err != nil {
		return Profile{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Profile{}, ErrNotFound
	}
	s.queueRefresh(ctx)
	return s.Profile(ctx, id)
}

// DeleteProfile removes a profile; items that used it fall back to the default.
func (s *Service) DeleteProfile(ctx context.Context, id int64) error {
	for _, k := range []settings.Key[int64]{DefaultProfileMovies, DefaultProfileSeries} {
		if v, _ := settings.Get(ctx, s.Settings, k); v == id {
			return ErrProfileInUse
		}
	}
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM subtitle_profiles WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM subtitle_item_profiles WHERE profile_id = ?`, id)
		return err
	})
	if err == nil {
		s.queueRefresh(ctx)
	}
	return err
}

// ItemProfile is the profile an item uses and why.
type ItemProfile struct {
	Profile *Profile `json:"profile"`
	// Source: default (the kind's default), item (set on the item), none (subtitles off).
	Source string `json:"source"`
	// Override is the item's own setting: 0 = default, -1 = none, else a profile id.
	Override int64 `json:"override"`
}

// EffectiveProfile returns the profile for a library item (media_items id)
// of a kind ("movie" or "series").
func (s *Service) EffectiveProfile(ctx context.Context, itemID int64, kind string) (ItemProfile, error) {
	var override int64
	err := s.DB.R.QueryRowContext(ctx, `SELECT profile_id FROM subtitle_item_profiles WHERE item_id = ?`, itemID).Scan(&override)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ItemProfile{}, err
	}
	out := ItemProfile{Override: override}
	id := override
	switch {
	case override == NoProfile:
		out.Source = "none"
		return out, nil
	case override > 0:
		out.Source = "item"
	default:
		out.Source = "default"
		k := DefaultProfileMovies
		if kind == "series" || kind == "episode" {
			k = DefaultProfileSeries
		}
		if id, err = settings.Get(ctx, s.Settings, k); err != nil {
			return out, err
		}
	}
	if id <= 0 {
		out.Source = "none"
		return out, nil
	}
	p, err := s.Profile(ctx, id)
	if errors.Is(err, ErrNotFound) {
		out.Source = "none"
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Profile = &p
	return out, nil
}

// AssignProfile sets the override of library items (media_items ids of
// movies or series): 0 = the default, -1 = none, else a profile id.
func (s *Service) AssignProfile(ctx context.Context, itemIDs []int64, profileID int64) (int, error) {
	if profileID > 0 {
		if _, err := s.Profile(ctx, profileID); err != nil {
			return 0, err
		}
	} else if profileID != 0 && profileID != NoProfile {
		return 0, invalid("profileId must be a profile, 0 (default) or -1 (none)")
	}
	n := 0
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, id := range itemIDs {
			var kind string
			if err := tx.QueryRowContext(ctx, `SELECT kind FROM media_items WHERE id = ?`, id).Scan(&kind); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			if kind != "movie" && kind != "series" {
				continue
			}
			var err error
			if profileID == 0 {
				_, err = tx.ExecContext(ctx, `DELETE FROM subtitle_item_profiles WHERE item_id = ?`, id)
			} else {
				_, err = tx.ExecContext(ctx, `INSERT INTO subtitle_item_profiles (item_id, profile_id) VALUES (?, ?)
					ON CONFLICT (item_id) DO UPDATE SET profile_id = excluded.profile_id`, id, profileID)
			}
			if err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(itemIDs) > 50 {
		s.queueRefresh(ctx) // a large library is refreshed in the background
		return n, nil
	}
	for _, id := range itemIDs {
		s.refreshItem(ctx, id)
	}
	return n, nil
}

// ItemIDs lists the library items of a kind (movie or series).
func (s *Service) ItemIDs(ctx context.Context, kind string) ([]int64, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM media_items WHERE kind = ? ORDER BY id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
