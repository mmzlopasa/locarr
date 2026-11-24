package subtitles

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers/opensubtitles"
)

// ProviderReport is how one provider answered a search.
type ProviderReport struct {
	Provider string `json:"provider"`
	Results  int    `json:"results"`
	Error    string `json:"error,omitempty"`
	Code     string `json:"code,omitempty"`
	Skipped  string `json:"skipped,omitempty"`
}

// Outcome is the result of an automatic search for one item.
type Outcome struct {
	Ref        string           `json:"ref"`
	Searched   []ProviderReport `json:"providers"`
	Downloaded []Subtitle       `json:"downloaded"`
	Missing    []string         `json:"missing"`
	Message    string           `json:"message"`
}

type searchArgs struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
	Probe  bool   `json:"probe,omitempty"`
}

// collect asks every usable provider for langs and scores the results for
// the wants. It also returns the provider reports.
func (s *Service) collect(ctx context.Context, t *Target, wants []Want) ([]Scored, []ProviderReport, error) {
	ps, skipped, err := s.usable(ctx)
	if err != nil {
		return nil, nil, err
	}
	var reports []ProviderReport
	for name, why := range skipped {
		reports = append(reports, ProviderReport{Provider: name, Skipped: why})
	}
	langSet := map[string]bool{}
	var langs []string
	for _, w := range wants {
		if !langSet[w.Language] {
			langSet[w.Language] = true
			langs = append(langs, w.Language)
		}
	}
	hash := ""
	for _, p := range ps {
		if p.Name == opensubtitles.Name {
			hash, _ = opensubtitles.Hash(t.Path)
		}
	}
	q := t.Query(langs, hash)
	file := t.Release()
	var out []Scored
	for _, p := range ps {
		info, _ := implementation(p.Name)
		if len(info.Languages) > 0 && !overlaps(info.Languages, langs) {
			reports = append(reports, ProviderReport{Provider: p.Name, Skipped: "offers none of the wanted languages"})
			continue
		}
		c, err := s.client(p)
		if err != nil {
			reports = append(reports, ProviderReport{Provider: p.Name, Error: err.Error(), Code: "config"})
			continue
		}
		res, err := c.Search(ctx, q)
		if err != nil {
			if ctx.Err() != nil {
				return out, reports, ctx.Err()
			}
			s.recordFailure(ctx, p, err)
			reports = append(reports, ProviderReport{Provider: p.Name, Error: err.Error(), Code: ErrorCode(err)})
			continue
		}
		s.recordSuccess(ctx, p.Name, nil)
		reports = append(reports, ProviderReport{Provider: p.Name, Results: len(res)})
		for _, r := range res {
			for _, w := range wants {
				if r.Language == w.Language {
					out = append(out, Score(t, file, w, r))
				}
			}
		}
	}
	Rank(out)
	return out, reports, nil
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// seen returns the subtitles already downloaded for an item and the
// blacklisted ones, as provider:id keys.
func (s *Service) seen(ctx context.Context, ref string) (downloaded, blacklisted map[string]bool, err error) {
	downloaded, blacklisted = map[string]bool{}, map[string]bool{}
	for q, m := range map[string]map[string]bool{
		`SELECT provider, provider_id FROM subtitle_history WHERE item_ref = ?`:   downloaded,
		`SELECT provider, provider_id FROM subtitle_blacklist WHERE item_ref = ?`: blacklisted,
	} {
		rows, err := s.DB.R.QueryContext(ctx, q, ref)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var p, id string
			if err := rows.Scan(&p, &id); err != nil {
				_ = rows.Close()
				return nil, nil, err
			}
			m[p+":"+id] = true
		}
		_ = rows.Close()
	}
	return downloaded, blacklisted, nil
}

func resultKey(r providers.Result) string { return r.Provider + ":" + r.ID }

func (s *Service) minScore(ctx context.Context, kind string) int {
	k := MinScoreMovies
	if kind == providers.KindEpisode {
		k = MinScoreEpisodes
	}
	v, _ := settings.Get(ctx, s.Settings, k)
	return v
}

// download fetches and saves one scored result; quota and provider errors
// are recorded on the provider.
func (s *Service) download(ctx context.Context, t *Target, sc Scored, replace bool) (Subtitle, string, error) {
	p, err := s.Provider(ctx, sc.Provider)
	if err != nil {
		return Subtitle{}, "", err
	}
	if p.QuotaUsedUp(s.now()) {
		return Subtitle{}, "", &providers.QuotaError{ResetAt: derefTime(p.QuotaResetAt)}
	}
	c, err := s.client(p)
	if err != nil {
		return Subtitle{}, "", err
	}
	f, st, err := c.Download(ctx, sc.Result, t.Query([]string{sc.Language}, ""))
	if err != nil {
		s.recordFailure(ctx, p, err)
		return Subtitle{}, "", err
	}
	s.recordSuccess(ctx, p.Name, &st)
	release := ""
	if len(sc.Releases) > 0 {
		release = sc.Releases[0]
	}
	return s.save(ctx, t, saveInput{Want: sc.Want, Data: f.Data, Provider: sc.Provider, ProviderID: sc.ID, Release: release,
		Score: sc.Score, Replace: replace})
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// Search runs an automatic search for one movie or episode: every missing
// language of its profile (and, within the upgrade window, better versions
// of downloaded ones), the best usable result above the minimum score.
func (s *Service) Search(ctx context.Context, ref library.Ref, reason string, probe bool) (Outcome, error) {
	out := Outcome{Ref: ref.String(), Searched: []ProviderReport{}, Downloaded: []Subtitle{}, Missing: []string{}}
	t, err := s.Target(ctx, ref)
	if err != nil {
		return out, err
	}
	if probe {
		if _, err := s.RefreshTarget(ctx, &t, true); err != nil {
			s.log().Warn("subtitle refresh", "item", ref.String(), "err", err)
		}
	}
	ip, st, err := s.Status(ctx, &t)
	if err != nil {
		return out, err
	}
	if ip.Profile == nil {
		out.Message = "no subtitle profile applies to this item"
		return out, s.UpdateState(ctx, &t)
	}
	wants := missingWants(st)
	// Languages covered only by a machine translation: a human subtitle replaces it.
	translatedKeys := map[string]bool{}
	for _, w := range st {
		if w.Status == "translated" {
			wants = append(wants, w.Want)
			translatedKeys[w.Key] = true
		}
	}
	// Upgrades: downloaded subtitles below a hash match, while the file is new.
	upgradeFrom := map[string]int{}
	if days, _ := settings.Get(ctx, s.Settings, UpgradeDays); days > 0 && s.now().Sub(db.Time(t.AddedAt)) < time.Duration(days)*24*time.Hour {
		subs, _ := s.FileSubtitles(ctx, t.FileID)
		for _, w := range st {
			if w.Status != "present" {
				continue
			}
			for _, sub := range subs {
				if sub.ID == w.SubtitleID && sub.Source == "downloaded" && sub.Score != nil && *sub.Score < PointsHash {
					wants = append(wants, w.Want)
					upgradeFrom[w.Key] = *sub.Score
				}
			}
		}
	}
	if len(wants) == 0 {
		out.Message = "nothing is missing"
		return out, s.UpdateState(ctx, &t)
	}
	results, reports, err := s.collect(ctx, &t, wants)
	out.Searched = reports
	if err != nil {
		return out, err
	}
	downloaded, blacklisted, err := s.seen(ctx, ref.String())
	if err != nil {
		return out, err
	}
	minimum := s.minScore(ctx, t.Kind)
	asked := 0
	for _, r := range reports {
		if r.Skipped == "" {
			asked++
		}
	}
	gotAny := false
	for _, w := range wants {
		if gotAny && ip.Profile.Cutoff && upgradeFrom[w.Key()] == 0 {
			break
		}
		bestSeen := -1
		var saved *Subtitle
		var lastErr error
		for _, sc := range results {
			if sc.Want.Key() != w.Key() || sc.Rejected != "" {
				continue
			}
			bestSeen = max(bestSeen, sc.Score)
			if sc.Score < minimum || blacklisted[resultKey(sc.Result)] || downloaded[resultKey(sc.Result)] {
				continue
			}
			if from, ok := upgradeFrom[w.Key()]; ok && sc.Score <= from {
				break
			}
			sub, recycled, err := s.download(ctx, &t, sc, false)
			if errors.Is(err, ErrNotBetter) {
				break
			}
			if err != nil {
				lastErr = err
				s.record(ctx, ref.String(), TypeFailed, fmt.Sprintf("could not download the %s subtitle from %s: %s", w.Language, sc.Provider, err),
					map[string]any{"language": w.Language, "provider": sc.Provider, "release": first(sc.Releases), "error": err.Error(), "code": ErrorCode(err)})
				continue
			}
			typ, verb := TypeDownloaded, "found"
			if _, ok := upgradeFrom[w.Key()]; ok {
				typ, verb = TypeUpgraded, "upgraded"
			}
			s.record(ctx, ref.String(), typ, fmt.Sprintf("subtitle %s %s on %s, score %d, saved as %s", w.Key(), verb, sc.Provider, sc.Score, filepath.Base(sub.Path)),
				map[string]any{"language": w.Language, "key": w.Key(), "provider": sc.Provider, "score": sc.Score, "path": sub.Path, "release": first(sc.Releases),
					"parts": sc.Parts, "recycled": recycled, "reason": reason, "hashMatch": sc.HashMatch})
			saved = &sub
			break
		}
		switch {
		case saved != nil:
			out.Downloaded = append(out.Downloaded, *saved)
			gotAny = true
			s.maybeAutoSync(ctx, *saved, results)
		case upgradeFrom[w.Key()] > 0 || translatedKeys[w.Key()]:
			// No better version (or no human subtitle yet): nothing to record.
		default:
			out.Missing = append(out.Missing, w.Key())
			data := map[string]any{"language": w.Language, "key": w.Key(), "providers": asked, "reports": reports, "minScore": minimum, "reason": reason}
			msg := fmt.Sprintf("no %s subtitle yet — searched %d providers", languages.Name(w.Language), asked)
			if bestSeen >= 0 {
				data["bestScore"] = bestSeen
				msg += fmt.Sprintf("; the best had score %d (minimum %d)", bestSeen, minimum)
			}
			if lastErr != nil {
				data["error"] = lastErr.Error()
			}
			s.record(ctx, ref.String(), TypeNotFound, msg, data)
		}
	}
	if err := s.UpdateState(ctx, &t); err != nil {
		return out, err
	}
	out.Message = fmt.Sprintf("%d downloaded, %d still missing", len(out.Downloaded), len(out.Missing))
	stillTranslated := false
	for k := range translatedKeys {
		got := false
		for _, d := range out.Downloaded {
			if d.Want().Key() == k || d.Language+":forced" == k && d.Forced {
				got = true
			}
		}
		stillTranslated = stillTranslated || !got
	}
	return out, s.afterSearch(ctx, &t, len(out.Missing) > 0 || stillTranslated, asked, out.Message)
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// SearchBackoff is the wait before searching an item again after n
// fruitless searches: 6 hours doubling, at most a week.
func SearchBackoff(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	return min(6*time.Hour<<min(n-1, 6), 7*24*time.Hour)
}

func (s *Service) afterSearch(ctx context.Context, t *Target, stillMissing bool, asked int, result string) error {
	var searches int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT searches FROM subtitle_state WHERE item_ref = ?`, t.Ref.String()).Scan(&searches)
	searches++
	now := s.now()
	next := now.Add(24 * time.Hour) // upgrade window: daily
	if stillMissing {
		next = now.Add(SearchBackoff(searches))
	}
	_, err := s.DB.W.ExecContext(ctx, `UPDATE subtitle_state SET searches = ?, last_search_at = ?, next_search_at = ?, last_result = ?,
		providers_asked = ? WHERE item_ref = ?`, searches, db.Millis(now), db.Millis(next), result, asked, t.Ref.String())
	return err
}

// ManualResult is a scored result of an interactive search.
type ManualResult struct {
	Scored
	Key         string `json:"key"`
	Downloaded  bool   `json:"downloaded"`
	Blacklisted bool   `json:"blacklisted"`
	MinScore    int    `json:"minScore"`
}

type manualEntry struct {
	at      time.Time
	results map[string]Scored
}

// Interactive searches every usable provider for a target and returns all
// scored results (for its profile's languages, or the given language).
func (s *Service) Interactive(ctx context.Context, ref library.Ref, language string) ([]ManualResult, []ProviderReport, error) {
	t, err := s.Target(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	var wants []Want
	if language != "" {
		if _, ok := languages.ByCode(language); !ok {
			return nil, nil, ErrBadLanguage
		}
		wants = []Want{{Language: language, HearingImpaired: HIInclude}, {Language: language, Forced: true, HearingImpaired: HIInclude}}
	} else {
		ip, _, err := s.Status(ctx, &t)
		if err != nil {
			return nil, nil, err
		}
		if ip.Profile == nil {
			return nil, nil, invalid("the item has no subtitle profile; choose a language")
		}
		wants = ip.Profile.Languages
	}
	results, reports, err := s.collect(ctx, &t, wants)
	if err != nil {
		return nil, reports, err
	}
	downloaded, blacklisted, err := s.seen(ctx, ref.String())
	if err != nil {
		return nil, reports, err
	}
	minimum := s.minScore(ctx, t.Kind)
	entry := manualEntry{at: s.now(), results: map[string]Scored{}}
	out := make([]ManualResult, 0, len(results))
	for i, sc := range results {
		k := fmt.Sprintf("%d:%s:%s", i, resultKey(sc.Result), sc.Want.Key())
		entry.results[k] = sc
		out = append(out, ManualResult{Scored: sc, Key: k, Downloaded: downloaded[resultKey(sc.Result)],
			Blacklisted: blacklisted[resultKey(sc.Result)], MinScore: minimum})
	}
	s.mu.Lock()
	if s.manual == nil {
		s.manual = map[string]manualEntry{}
	}
	for k, e := range s.manual {
		if s.now().Sub(e.at) > time.Hour {
			delete(s.manual, k)
		}
	}
	s.manual[ref.String()] = entry
	s.mu.Unlock()
	return out, reports, nil
}

// DownloadManual saves a result the user picked from an interactive search;
// it replaces what exists for that language (old file to the recycle bin).
func (s *Service) DownloadManual(ctx context.Context, ref library.Ref, key string) (Subtitle, error) {
	s.mu.Lock()
	e, ok := s.manual[ref.String()]
	sc, found := e.results[key]
	s.mu.Unlock()
	if !ok || !found || s.now().Sub(e.at) > time.Hour {
		return Subtitle{}, ErrResultExpired
	}
	t, err := s.Target(ctx, ref)
	if err != nil {
		return Subtitle{}, err
	}
	if sc.Rejected == "language" || sc.Rejected == "other_movie" || sc.Rejected == "other_episode" {
		return Subtitle{}, invalid("this subtitle is for %s", strings.ReplaceAll(sc.Rejected, "_", " "))
	}
	// A manual pick is taken as the variant it really is.
	sc.Want = Want{Language: sc.Language, Forced: sc.Forced, HearingImpaired: HIInclude}
	if sc.HearingImpaired {
		sc.Want.HearingImpaired = HIOnly
	}
	sub, recycled, err := s.download(ctx, &t, sc, true)
	if err != nil {
		s.record(ctx, ref.String(), TypeFailed, fmt.Sprintf("could not download the %s subtitle from %s: %s", sc.Language, sc.Provider, err),
			map[string]any{"language": sc.Language, "provider": sc.Provider, "release": first(sc.Releases), "error": err.Error(), "code": ErrorCode(err)})
		return Subtitle{}, err
	}
	s.record(ctx, ref.String(), TypeDownloaded, fmt.Sprintf("subtitle %s chosen manually from %s, score %d, saved as %s", sc.Want.Key(), sc.Provider, sc.Score,
		filepath.Base(sub.Path)), map[string]any{"language": sc.Language, "key": sc.Want.Key(), "provider": sc.Provider, "score": sc.Score, "path": sub.Path,
		"release": first(sc.Releases), "parts": sc.Parts, "recycled": recycled, "reason": "manual", "hashMatch": sc.HashMatch})
	if err := s.UpdateState(ctx, &t); err != nil {
		return sub, err
	}
	return sub, nil
}

// QueueSearch queues automatic searches for refs.
func (s *Service) QueueSearch(ctx context.Context, refs []library.Ref, reason string) (int, error) {
	n := 0
	for _, r := range refs {
		if r.Kind != library.RefMovie && r.Kind != library.RefEpisode {
			continue
		}
		if _, _, err := s.Jobs.Enqueue(ctx, TaskSearchItem, searchArgs{Ref: r.String(), Reason: reason},
			jobs.EnqueueOptions{UniqueKey: "subtitles-search:" + r.String()}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SearchDue runs the scheduled search: items missing subtitles (or in their
// upgrade window) whose backoff has passed, at most SearchesPerHour per run
// so provider quotas spread over the day.
func (s *Service) SearchDue(ctx context.Context) (int, error) {
	limit, _ := settings.Get(ctx, s.Settings, SearchesPerHour)
	if limit <= 0 {
		return 0, nil
	}
	ps, _, err := s.usable(ctx)
	if err != nil || len(ps) == 0 {
		return 0, err
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT item_ref FROM subtitle_state WHERE (missing_count > 0 OR upgradable = 1)
		AND (next_search_at IS NULL OR next_search_at <= ?) ORDER BY next_search_at LIMIT ?`, db.Millis(s.now()), limit)
	if err != nil {
		return 0, err
	}
	var refs []library.Ref
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if ref, err := library.ParseRef(r); err == nil {
			refs = append(refs, ref)
		}
	}
	_ = rows.Close()
	n := 0
	for _, ref := range refs {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if _, err := s.Search(ctx, ref, "scheduled", false); err != nil {
			s.log().Warn("scheduled subtitle search", "item", ref.String(), "err", err)
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNoFile) {
				_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM subtitle_state WHERE item_ref = ?`, ref.String())
			}
			continue
		}
		n++
	}
	return n, nil
}
