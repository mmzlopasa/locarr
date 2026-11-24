package musicdiscovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/lastfm"
)

// ErrRunning is returned while a refresh is already going.
var ErrRunning = errors.New("music discovery is already refreshing")

// Report says what a refresh did.
type Report struct {
	Asked    int
	Failed   int
	Enriched int
}

// Running reports whether a refresh is going.
func (s *Service) Running() bool { return s.running.Load() }

// LibrarySeeds lists the library's artists.
func (s *Service) LibrarySeeds(ctx context.Context) ([]Seed, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT a.mbid, i.title, i.id FROM artists a JOIN media_items i ON i.id = a.item_id ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Seed{}
	for rows.Next() {
		var sd Seed
		if err := rows.Scan(&sd.MBID, &sd.Name, &sd.ArtistID); err != nil {
			return nil, err
		}
		sd.MBID = strings.ToLower(sd.MBID)
		out = append(out, sd)
	}
	return out, rows.Err()
}

type seedState struct {
	fetched time.Time
	failed  bool
}

func (s *Service) seedStates(ctx context.Context) (map[[2]string]seedState, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT seed_mbid, provider, fetched_at, error FROM discovery_seeds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[[2]string]seedState{}
	for rows.Next() {
		var (
			id, p, e string
			at       int64
		)
		if err := rows.Scan(&id, &p, &at, &e); err != nil {
			return nil, err
		}
		out[[2]string{id, p}] = seedState{fetched: db.Time(at), failed: e != ""}
	}
	return out, rows.Err()
}

// stale reports whether a seed's answer from a provider should be asked again.
func stale(st seedState, ok bool, now time.Time, days int) bool {
	if !ok {
		return true
	}
	if st.failed {
		return now.Sub(st.fetched) >= retryFailed
	}
	return now.Sub(st.fetched) >= time.Duration(days)*24*time.Hour
}

// Pending counts the (library artist, provider) pairs never asked yet.
func (s *Service) Pending(ctx context.Context, seeds []Seed, providers []string) (int, error) {
	states, err := s.seedStates(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sd := range seeds {
		for _, p := range providers {
			if _, ok := states[[2]string{sd.MBID, p}]; !ok {
				n++
			}
		}
	}
	return n, nil
}

// Refresh asks every active provider about the library artists whose
// answers are missing or older than the refresh setting (only = just these
// artists, asked again even when fresh), then looks up tags and covers of
// the best suggestions. Providers run side by side, each at its own rate.
func (s *Service) Refresh(ctx context.Context, only []string) (Report, error) {
	if !s.running.CompareAndSwap(false, true) {
		return Report{}, ErrRunning
	}
	defer s.running.Store(false)
	cfg, err := s.Config(ctx)
	if err != nil {
		return Report{}, err
	}
	providers, err := s.Active(ctx)
	if err != nil {
		return Report{}, err
	}
	seeds, err := s.LibrarySeeds(ctx)
	if err != nil {
		return Report{}, err
	}
	force := map[string]bool{}
	for _, id := range only {
		force[strings.ToLower(id)] = true
	}
	if len(force) > 0 {
		kept := seeds[:0]
		for _, sd := range seeds {
			if force[sd.MBID] {
				kept = append(kept, sd)
			}
		}
		seeds = kept
	}
	states, err := s.seedStates(ctx)
	if err != nil {
		return Report{}, err
	}
	var (
		mu  sync.Mutex
		rep Report
		wg  sync.WaitGroup
	)
	for _, p := range providers {
		var todo []Seed
		for _, sd := range seeds {
			st, ok := states[[2]string{sd.MBID, p}]
			if force[sd.MBID] || stale(st, ok, s.now(), cfg.RefreshDays) {
				todo = append(todo, sd)
			}
		}
		// Never asked first, then the oldest answers.
		sort.SliceStable(todo, func(i, j int) bool {
			return states[[2]string{todo[i].MBID, p}].fetched.Before(states[[2]string{todo[j].MBID, p}].fetched)
		})
		wg.Add(1)
		go func() {
			defer wg.Done()
			asked, failed := s.refreshProvider(ctx, p, todo)
			mu.Lock()
			rep.Asked += asked
			rep.Failed += failed
			mu.Unlock()
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return rep, ctx.Err()
	}
	rep.Enriched, err = s.enrich(ctx, EnrichPerRun)
	s.Invalidate()
	return rep, err
}

func (s *Service) refreshProvider(ctx context.Context, p string, seeds []Seed) (asked, failed int) {
	inRow := 0
	for _, sd := range seeds {
		if ctx.Err() != nil {
			return
		}
		list, err := s.similar(ctx, p, sd)
		asked++
		if errors.Is(err, metadata.ErrNotFound) {
			list, err = nil, nil
		}
		if err != nil && ctx.Err() != nil {
			return
		}
		if serr := s.store(ctx, p, sd.MBID, list, err); serr != nil {
			s.log().Warn("store similar artists", "provider", p, "artist", sd.Name, "err", serr)
		}
		if err != nil {
			failed++
			inRow++
			s.log().Info("similar artists lookup failed", "provider", p, "artist", sd.Name, "err", err)
			// A refused key, Spotify refusing the endpoint or a provider that
			// keeps failing: stop asking it in this run.
			if errStop(err) || inRow >= 5 {
				return
			}
			continue
		}
		inRow = 0
		s.Invalidate()
	}
	return
}

// store replaces one seed's answer from one provider.
func (s *Service) store(ctx context.Context, p, seed string, list []Candidate, failure error) error {
	now := db.Millis(s.now())
	msg := ""
	if failure != nil {
		msg = failure.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
	}
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO discovery_seeds (seed_mbid, provider, fetched_at, error) VALUES (?, ?, ?, ?)
			ON CONFLICT (seed_mbid, provider) DO UPDATE SET fetched_at = excluded.fetched_at, error = excluded.error`, seed, p, now, msg); err != nil {
			return err
		}
		if failure != nil {
			return nil // keep the previous answer
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM discovery_similar WHERE seed_mbid = ? AND provider = ?`, seed, p); err != nil {
			return err
		}
		for _, c := range list {
			if _, err := tx.ExecContext(ctx, `INSERT INTO discovery_similar (seed_mbid, provider, target_mbid, score, confidence) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT DO UPDATE SET score = max(score, excluded.score), confidence = max(confidence, excluded.confidence)`,
				seed, p, c.MBID, c.Score, c.Confidence); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO discovery_artists (mbid, name, disambiguation, lastfm_url, spotify_id, updated_at)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (mbid) DO UPDATE SET
					name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE name END,
					disambiguation = CASE WHEN excluded.disambiguation <> '' THEN excluded.disambiguation ELSE disambiguation END,
					lastfm_url = CASE WHEN excluded.lastfm_url <> '' THEN excluded.lastfm_url ELSE lastfm_url END,
					spotify_id = CASE WHEN excluded.spotify_id <> '' THEN excluded.spotify_id ELSE spotify_id END,
					updated_at = excluded.updated_at`,
				c.MBID, c.Name, c.Disambiguation, c.LastFMURL, c.SpotifyID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// enrich looks up tags and a cover for the best suggestions that have none
// or old ones: tags from Last.fm (or MusicBrainz genres without a Last.fm
// key), the cover from the artist's most listened album on ListenBrainz (or
// their first album on MusicBrainz).
func (s *Service) enrich(ctx context.Context, limit int) (int, error) {
	active, err := s.Active(ctx)
	if err != nil {
		return 0, err
	}
	res, err := s.aggregate(ctx, aggKey{providers: strings.Join(active, ",")})
	if err != nil {
		return 0, err
	}
	cutoff := s.now().Add(-enrichAge)
	lfm := s.Configured(ctx, LastFM)
	cfg, err := s.Config(ctx)
	if err != nil {
		return 0, err
	}
	lfm = lfm && cfg.LastFM
	done := 0
	for _, sg := range res.items {
		if done == limit || ctx.Err() != nil {
			break
		}
		if sg.enrichedAt != nil && sg.enrichedAt.After(cutoff) {
			continue
		}
		tags := []string{}
		disamb := ""
		if lfm {
			if list, err := s.LastFM.TopTags(ctx, sg.MBID, sg.Name); err == nil {
				tags = topTags(list)
			}
		}
		if len(tags) == 0 && s.MB != nil {
			if a, err := s.MB.Artist(ctx, sg.MBID); err == nil {
				for i, g := range a.Genres {
					if i == 5 {
						break
					}
					tags = append(tags, strings.ToLower(g))
				}
				disamb = a.Disambiguation
			}
		}
		cover := ""
		if s.LB != nil && cfg.ListenBrainz {
			cover, _ = s.LB.TopReleaseGroup(ctx, sg.MBID)
		}
		if cover == "" && s.MB != nil {
			if groups, err := s.MB.ReleaseGroups(ctx, sg.MBID); err == nil {
				first := ""
				for _, g := range groups {
					if g.PrimaryType == "album" && len(g.SecondaryTypes) == 0 && g.FirstReleaseDate != "" &&
						(first == "" || g.FirstReleaseDate < first) {
						first, cover = g.FirstReleaseDate, g.MBID
					}
				}
			}
		}
		b, _ := json.Marshal(tags)
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE discovery_artists SET tags = ?, cover_group = ?, enriched_at = ?,
			disambiguation = CASE WHEN ? <> '' THEN ? ELSE disambiguation END WHERE mbid = ?`,
			string(b), cover, db.Millis(s.now()), disamb, disamb, sg.MBID); err != nil {
			return done, err
		}
		done++
	}
	if done > 0 {
		s.Invalidate()
	}
	return done, nil
}

// topTags keeps the five most used tags Last.fm gives some weight.
func topTags(list []lastfm.Tag) []string {
	out := []string{}
	for _, t := range list {
		if len(out) == 5 {
			break
		}
		if t.Count >= 10 && t.Name != "seen live" {
			out = append(out, t.Name)
		}
	}
	return out
}
