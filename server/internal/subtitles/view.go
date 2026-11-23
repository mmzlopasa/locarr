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
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/tracks"
)

// SearchState is an item's search bookkeeping.
type SearchState struct {
	Searches       int        `json:"searches"`
	LastSearchAt   *time.Time `json:"lastSearchAt,omitempty"`
	NextSearchAt   *time.Time `json:"nextSearchAt,omitempty"`
	LastResult     string     `json:"lastResult"`
	ProvidersAsked int        `json:"providersAsked"`
}

// Explanation answers "why is the subtitle not here?".
type Explanation struct {
	Code    string         `json:"code"`
	Tone    string         `json:"tone"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params"`
}

// ItemView is everything the Subtitles section of an item shows.
type ItemView struct {
	Ref           string         `json:"ref"`
	Title         string         `json:"title"`
	FilePath      string         `json:"filePath"`
	HasFile       bool           `json:"hasFile"`
	Profile       ItemProfile    `json:"profile"`
	Wants         []WantStatus   `json:"wants"`
	Subtitles     []Subtitle     `json:"subtitles"`
	Tracks        []tracks.Track `json:"tracks"`
	ProbeError    string         `json:"probeError"`
	State         SearchState    `json:"state"`
	Explanation   Explanation    `json:"explanation"`
	SyncAvailable bool           `json:"syncAvailable"`
}

func (s *Service) state(ctx context.Context, ref string) (SearchState, error) {
	var (
		st         SearchState
		last, next sql.NullInt64
	)
	err := s.DB.R.QueryRowContext(ctx, `SELECT searches, last_search_at, next_search_at, last_result, providers_asked FROM subtitle_state WHERE item_ref = ?`, ref).
		Scan(&st.Searches, &last, &next, &st.LastResult, &st.ProvidersAsked)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	st.LastSearchAt, st.NextSearchAt = db.NullTime(last), db.NullTime(next)
	return st, err
}

// Item returns the Subtitles section of a movie or episode.
func (s *Service) Item(ctx context.Context, ref library.Ref) (ItemView, error) {
	v := ItemView{Ref: ref.String(), Wants: []WantStatus{}, Subtitles: []Subtitle{}, Tracks: []tracks.Track{}, SyncAvailable: s.SyncAvailable()}
	t, err := s.Target(ctx, ref)
	if errors.Is(err, ErrNoFile) {
		v.Explanation = Explanation{Code: "no_file", Tone: "info", Message: "the item has no file yet; subtitles are searched once it is imported", Params: map[string]any{}}
		itemID, kind := ref.ID, "movie"
		if ref.Kind == library.RefEpisode {
			kind = "series"
			if err := s.DB.R.QueryRowContext(ctx, `SELECT series_id FROM episodes WHERE id = ?`, ref.ID).Scan(&itemID); err != nil {
				return v, err
			}
		}
		v.Profile, err = s.EffectiveProfile(ctx, itemID, kind)
		return v, err
	}
	if err != nil {
		return v, err
	}
	v.Title, v.FilePath, v.HasFile = t.Title, t.Path, true
	if _, _, probed, _ := s.Tracks(ctx, t.FileID); !probed {
		if _, err := s.RefreshTarget(ctx, &t, true); err != nil {
			s.log().Warn("subtitle refresh", "item", ref.String(), "err", err)
		}
	}
	if v.Profile, v.Wants, err = s.Status(ctx, &t); err != nil {
		return v, err
	}
	if v.Subtitles, err = s.FileSubtitles(ctx, t.FileID); err != nil {
		return v, err
	}
	if v.Tracks, v.ProbeError, _, err = s.Tracks(ctx, t.FileID); err != nil {
		return v, err
	}
	if v.State, err = s.state(ctx, ref.String()); err != nil {
		return v, err
	}
	v.Explanation, err = s.explain(ctx, v)
	return v, err
}

func fmtWhen(t *time.Time) string {
	if t == nil {
		return "soon"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// explain derives the one answer for an item's subtitles. Rules in order:
// no profile → complete → no provider → every provider unavailable → not
// searched yet → searched and not found.
func (s *Service) explain(ctx context.Context, v ItemView) (Explanation, error) {
	e := Explanation{Params: map[string]any{"searches": v.State.Searches}}
	set := func(code, tone, msg string) (Explanation, error) {
		e.Code, e.Tone, e.Message = code, tone, msg
		return e, nil
	}
	if v.State.LastSearchAt != nil {
		e.Params["lastSearch"] = v.State.LastSearchAt.UTC()
	}
	if v.State.NextSearchAt != nil {
		e.Params["nextSearch"] = v.State.NextSearchAt.UTC()
	}
	if v.Profile.Profile == nil {
		if v.Profile.Source == "none" && v.Profile.Override == NoProfile {
			return set("disabled", "info", "subtitles are turned off for this item")
		}
		return set("no_profile", "warning", "no subtitle profile applies: choose a default profile in Settings → Subtitles or one for this item")
	}
	var missing, names []string
	embedded := 0
	for _, w := range v.Wants {
		switch w.Status {
		case "missing":
			missing = append(missing, w.Key)
			names = append(names, languages.Name(w.Language))
		case "embedded":
			embedded++
		}
	}
	e.Params["missing"] = missing
	if len(missing) == 0 {
		if embedded > 0 {
			e.Params["embedded"] = embedded
			return set("complete_embedded", "success", "every wanted language is present (some as tracks inside the video)")
		}
		return set("complete", "success", "every wanted language is present")
	}
	ps, err := s.Providers(ctx)
	if err != nil {
		return e, err
	}
	now := s.now()
	enabled, usable := 0, 0
	var reasons []string
	for _, p := range ps {
		if !p.Enabled {
			continue
		}
		enabled++
		switch {
		case p.InBackoff(now):
			reasons = append(reasons, fmt.Sprintf("%s: %s (until %s)", p.Name, p.LastError, fmtWhen(p.RetryAt)))
		case p.QuotaUsedUp(now):
			reasons = append(reasons, fmt.Sprintf("%s: download quota used up until %s", p.Name, fmtWhen(p.QuotaResetAt)))
		default:
			usable++
		}
	}
	e.Params["languages"] = strings.Join(names, ", ")
	if enabled == 0 {
		return set("no_providers", "danger", "no subtitle provider is enabled (Settings → Subtitles)")
	}
	if usable == 0 {
		e.Params["detail"] = strings.Join(reasons, "; ")
		return set("providers_unavailable", "danger", "every enabled provider is unavailable right now: "+strings.Join(reasons, "; "))
	}
	if v.State.Searches == 0 {
		return set("not_searched", "info", "missing "+strings.Join(names, ", ")+"; not searched yet, next search "+fmtWhen(v.State.NextSearchAt))
	}
	e.Params["providers"] = v.State.ProvidersAsked
	return set("not_found", "warning", fmt.Sprintf("no %s subtitle yet — searched %d times (%d providers last time); next search %s",
		strings.Join(names, ", "), v.State.Searches, v.State.ProvidersAsked, fmtWhen(v.State.NextSearchAt)))
}

// Explain returns the explanation for an item.
func (s *Service) Explain(ctx context.Context, ref library.Ref) (Explanation, error) {
	v, err := s.Item(ctx, ref)
	return v.Explanation, err
}

// WantedItem is a row of Wanted → Missing subtitles.
type WantedItem struct {
	Ref          string     `json:"ref"`
	Kind         string     `json:"kind"`
	ItemID       int64      `json:"itemId"`
	Title        string     `json:"title"`
	Season       int        `json:"season,omitempty"`
	Episode      int        `json:"episode,omitempty"`
	EpisodeTitle string     `json:"episodeTitle,omitempty"`
	Missing      []string   `json:"missing"`
	Searches     int        `json:"searches"`
	LastSearchAt *time.Time `json:"lastSearchAt,omitempty"`
	NextSearchAt *time.Time `json:"nextSearchAt,omitempty"`
	LastResult   string     `json:"lastResult"`
}

// Wanted lists items missing subtitles, newest files first. kind: movie, episode or "".
func (s *Service) Wanted(ctx context.Context, kind string, offset, limit int) ([]WantedItem, int, error) {
	where := `st.missing_count > 0`
	args := []any{}
	if kind != "" {
		where += ` AND st.kind = ?`
		args = append(args, kind)
	}
	var total int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_state st WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT st.item_ref, st.kind, st.item_id, mi.title, COALESCE(e.season_number, 0), COALESCE(e.episode_number, 0), COALESCE(e.title, ''),
		st.missing, st.searches, st.last_search_at, st.next_search_at, st.last_result
		FROM subtitle_state st JOIN media_items mi ON mi.id = st.item_id
		LEFT JOIN episodes e ON st.kind = 'episode' AND e.id = st.entity_id
		WHERE ` + where + ` ORDER BY st.file_added_at DESC, st.item_ref LIMIT ? OFFSET ?` // #nosec G202 -- fixed fragments
	rows, err := s.DB.R.QueryContext(ctx, q, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []WantedItem{}
	for rows.Next() {
		var (
			w          WantedItem
			missing    string
			last, next sql.NullInt64
		)
		if err := rows.Scan(&w.Ref, &w.Kind, &w.ItemID, &w.Title, &w.Season, &w.Episode, &w.EpisodeTitle, &missing, &w.Searches, &last, &next, &w.LastResult); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal([]byte(missing), &w.Missing)
		w.LastSearchAt, w.NextSearchAt = db.NullTime(last), db.NullTime(next)
		out = append(out, w)
	}
	return out, total, rows.Err()
}

// WantedRefs returns every item missing subtitles (for "search all").
func (s *Service) WantedRefs(ctx context.Context, kind string) ([]library.Ref, error) {
	q := `SELECT item_ref FROM subtitle_state WHERE missing_count > 0`
	args := []any{}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []library.Ref
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		if ref, err := library.ParseRef(r); err == nil {
			out = append(out, ref)
		}
	}
	return out, rows.Err()
}
