package indexers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// keyPlaceholder replaces an indexer's API key inside stored download links.
const keyPlaceholder = "{locarr-apikey}"

// eligible returns the indexers a query goes to.
func (s *Service) eligible(ctx context.Context, q Query) ([]Indexer, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, ind := range all {
		if !ind.Enabled {
			continue
		}
		if len(q.IndexerIDs) > 0 && !slices.Contains(q.IndexerIDs, ind.ID) {
			continue
		}
		if q.Type == SearchAdult && len(ind.Categories.Adult) == 0 {
			continue
		}
		switch q.Source {
		case SourceAutomatic:
			if !ind.EnableAutomatic {
				continue
			}
		case SourceRSS:
			if !ind.EnableRSS {
				continue
			}
		default:
			// An explicit choice of indexers overrides the interactive switch.
			if !ind.EnableInteractive && len(q.IndexerIDs) == 0 {
				continue
			}
		}
		out = append(out, ind)
	}
	return out, nil
}

// SearchStream searches every eligible indexer in parallel and calls emit
// (from one goroutine at a time) as each indexer finishes, with its releases
// that no earlier indexer returned. It returns when all are done or ctx ends.
func (s *Service) SearchStream(ctx context.Context, q Query, emit func(Event)) (Result, error) {
	start := time.Now()
	if q.Source == "" {
		q.Source = SourceInteractive
	}
	if q.Type == "" {
		q.Type = SearchAny
	}
	inds, err := s.eligible(ctx, q)
	if err != nil {
		return Result{}, err
	}
	timeout, err := settings.Get(ctx, s.Settings, SearchTimeoutSeconds)
	if err != nil {
		return Result{}, err
	}
	sctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	type answer struct {
		res      IndexerResult
		releases []Release
	}
	answers := make(chan answer, len(inds))
	var wg sync.WaitGroup
	for _, ind := range inds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, rel := s.searchOne(sctx, ind, q)
			answers <- answer{res, rel}
		}()
	}
	go func() {
		wg.Wait()
		close(answers)
	}()

	seen := map[string]bool{}
	out := Result{Releases: []Release{}, Indexers: []IndexerResult{}}
	take := func(a answer) []Release {
		fresh := make([]Release, 0, len(a.releases))
		for _, r := range a.releases {
			// Adult releases only answer adult searches (D-103).
			if !AnswersQuery(q.Type, &r) {
				continue
			}
			if k := r.Key(); !seen[k] {
				seen[k] = true
				fresh = append(fresh, r)
			}
		}
		out.Releases = append(out.Releases, fresh...)
		out.Indexers = append(out.Indexers, a.res)
		return fresh
	}
	if emit != nil {
		for a := range answers {
			emit(Event{Indexer: a.res, Releases: take(a)})
		}
	} else {
		// Without a stream, wait for everyone so that a duplicate is kept from
		// the preferred indexer.
		var all []answer
		for a := range answers {
			all = append(all, a)
		}
		prio := map[int64]int{}
		for _, ind := range inds {
			prio[ind.ID] = ind.Priority
		}
		slices.SortStableFunc(all, func(a, b answer) int {
			if d := prio[a.res.IndexerID] - prio[b.res.IndexerID]; d != 0 {
				return d
			}
			return strings.Compare(a.res.IndexerName, b.res.IndexerName)
		})
		for _, a := range all {
			take(a)
		}
	}
	out.Duration = time.Since(start)
	s.writeLog(ctx, q, start, out)
	return out, nil
}

// Search is SearchStream that waits for every indexer. When several indexers
// return the same release, the one from the preferred indexer is kept.
func (s *Service) Search(ctx context.Context, q Query) (Result, error) {
	return s.SearchStream(ctx, q, nil)
}

// searchOne runs a query against one indexer, honouring backoff, request
// limits and the cache.
func (s *Service) searchOne(ctx context.Context, ind Indexer, q Query) (IndexerResult, []Release) {
	start := time.Now()
	res := IndexerResult{IndexerID: ind.ID, IndexerName: ind.Name}
	done := func(st Status, err error) (IndexerResult, []Release) {
		res.Status = st
		res.Duration = time.Since(start)
		if err != nil {
			res.ErrorCode = string(znab.KindOf(err))
			if errors.Is(err, context.DeadlineExceeded) {
				res.ErrorCode = string(znab.KindUnreachable)
			}
			res.Error = errorText(err)
		}
		return res, nil
	}
	now := s.now()
	if ind.InBackoff(now) {
		return done(StatusBackoff, fmt.Errorf("skipped after %d failures until %s", ind.Health.Failures,
			ind.Health.RetryAt.Format(time.RFC3339)))
	}
	c, err := s.client(ctx, ind)
	if err != nil {
		return done(StatusError, err)
	}
	caps := ind.Caps
	if caps == nil || ind.CapsUpdatedAt == nil || now.Sub(*ind.CapsUpdatedAt) > capsMaxAge {
		if ok, _ := s.allowRequest(ind); !ok {
			return done(StatusRateLimited, errors.New("request limit reached"))
		}
		fresh, err := c.Caps(ctx)
		switch {
		case err == nil:
			caps = fresh
			if err := s.saveCaps(context.WithoutCancel(ctx), ind.ID, fresh); err != nil {
				s.Log.Warn("save indexer caps", "indexer", ind.Name, "err", err)
			}
		case caps == nil:
			if countsAsFailure(ctx, err) {
				s.recordFailure(ctx, ind, err)
			}
			return done(StatusError, err)
		}
	}
	req, ok := buildRequest(ind, caps, q)
	if !ok {
		return done(StatusUnsupported, &znab.Error{Kind: znab.KindUnsupported, Message: "the indexer cannot search by these terms"})
	}
	ttl, err := settings.Get(ctx, s.Settings, SearchCacheSeconds)
	if err != nil {
		return done(StatusError, err)
	}
	key := strconv.FormatInt(ind.ID, 10) + "|" + ind.UpdatedAt.String() + "|" + req.Values().Encode()
	if q.Source != SourceRSS {
		if cached, ok := s.cache.get(key, time.Duration(ttl)*time.Second, now); ok {
			res.Cached = true
			res.Count = len(cached)
			res.Status = StatusOK
			res.Duration = time.Since(start)
			return res, cached
		}
	}
	if ok, wait := s.allowRequest(ind); !ok {
		return done(StatusRateLimited, fmt.Errorf("request limit reached; next request in %s", wait.Round(time.Second)))
	}
	page, err := c.Search(ctx, req)
	if err != nil {
		if countsAsFailure(ctx, err) {
			s.recordFailure(ctx, ind, err)
		}
		switch znab.KindOf(err) {
		case znab.KindUnsupported:
			return done(StatusUnsupported, err)
		case znab.KindRateLimit:
			return done(StatusRateLimited, err)
		}
		return done(StatusError, err)
	}
	if page.Dropped > 0 {
		s.recordParseFailures(context.WithoutCancel(ctx), ind, page.Dropped)
	}
	if ind.Health.Failures > 0 || ind.Health.LastSuccessAt == nil || now.Sub(*ind.Health.LastSuccessAt) > time.Minute {
		s.recordSuccess(context.WithoutCancel(ctx), ind.ID)
	}
	releases := make([]Release, 0, len(page.Items))
	for _, it := range page.Items {
		releases = append(releases, toRelease(ind, it, c.secret()))
	}
	if q.Source != SourceRSS {
		s.cache.put(key, releases, time.Duration(ttl)*time.Second, now)
	}
	res.Status = StatusOK
	res.Count = len(releases)
	res.Duration = time.Since(start)
	return res, releases
}

var searchFunc = map[SearchType]znab.SearchType{
	SearchAny: znab.SearchGeneric, SearchTV: znab.SearchTV, SearchMovie: znab.SearchMovie,
	SearchMusic: znab.SearchMusic, SearchBook: znab.SearchBook, SearchAdult: znab.SearchGeneric,
}

// buildRequest maps a query onto what the indexer supports. It falls back to
// a plain text search when the specific function is missing, and reports
// false when the indexer cannot express the query at all.
func buildRequest(ind Indexer, caps *znab.Caps, q Query) (znab.Request, bool) {
	fn := searchFunc[q.Type]
	if fn == "" {
		fn = znab.SearchGeneric
	}
	sc := caps.Search(fn)
	if !sc.Available {
		fn, sc = znab.SearchGeneric, caps.Search(znab.SearchGeneric)
	}
	r := znab.Request{Type: fn, Offset: q.Offset}
	cats := q.Categories
	if len(cats) == 0 {
		cats = ind.Categories.For(q.Type)
	}
	r.Categories = searchCategories(cats)

	type param struct {
		name string
		set  func()
		has  bool
	}
	params := []param{
		{"imdbid", func() { r.IMDBID = q.IMDBID }, q.IMDBID != ""},
		{"tmdbid", func() { r.TMDBID = q.TMDBID }, q.TMDBID > 0},
		{"tvdbid", func() { r.TVDBID = q.TVDBID }, q.TVDBID > 0},
		{"tvmazeid", func() { r.TVMazeID = q.TVMazeID }, q.TVMazeID > 0},
		{"season", func() { r.Season = q.Season }, q.Season != ""},
		{"ep", func() { r.Episode = q.Episode }, q.Episode != ""},
		{"artist", func() { r.Artist = q.Artist }, q.Artist != ""},
		{"album", func() { r.Album = q.Album }, q.Album != ""},
		{"author", func() { r.Author = q.Author }, q.Author != ""},
		{"title", func() { r.Title = q.Title }, q.Title != ""},
	}
	// TV searches carry every id the series has; indexers combine ids with
	// "and" and few know them all, so only the strongest supported one goes.
	onlyID := ""
	if q.Type == SearchTV {
		for _, c := range []struct {
			name string
			has  bool
		}{{"tvdbid", q.TVDBID > 0}, {"tvmazeid", q.TVMazeID > 0}, {"imdbid", q.IMDBID != ""}, {"tmdbid", q.TMDBID > 0}} {
			if c.has && sc.Supports(c.name) {
				onlyID = c.name
				break
			}
		}
	}
	ids := false
	var leftover []string // structured terms the indexer cannot take as parameters
	for _, p := range params {
		if !p.has {
			continue
		}
		if onlyID != "" && strings.HasSuffix(p.name, "id") && p.name != onlyID {
			continue
		}
		if sc.Supports(p.name) {
			p.set()
			if strings.HasSuffix(p.name, "id") {
				ids = true
			}
			continue
		}
		switch p.name {
		case "artist":
			leftover = append(leftover, q.Artist)
		case "album":
			leftover = append(leftover, q.Album)
		case "author":
			leftover = append(leftover, q.Author)
		case "title":
			leftover = append(leftover, q.Title)
		}
	}
	term := strings.TrimSpace(strings.Join(append([]string{q.Term}, leftover...), " "))
	if term != "" {
		if !sc.Supports("q") && !ids {
			return r, false
		}
		if sc.Supports("q") {
			r.Query = term
		}
	}
	hasStructured := q.IMDBID != "" || q.TMDBID > 0 || q.TVDBID > 0 || q.TVMazeID > 0
	if hasStructured && !ids && r.Query == "" {
		return r, false // only ids were given and the indexer takes none of them
	}
	limit := q.Limit
	if limit <= 0 {
		limit = caps.LimitDefault
	}
	if caps.LimitMax > 0 && limit > caps.LimitMax {
		limit = caps.LimitMax
	}
	r.Limit = limit
	return r, true
}

// toRelease turns a feed item into a Release, removing the API key from links.
func toRelease(ind Indexer, it znab.Item, apiKey string) Release {
	strip := func(s string) string {
		if apiKey == "" || s == "" {
			return s
		}
		return strings.ReplaceAll(s, apiKey, keyPlaceholder)
	}
	r := Release{
		GUID: strip(it.GUID), Title: it.Title, IndexerID: ind.ID, IndexerName: ind.Name, IndexerPriority: ind.Priority,
		Protocol: ind.Protocol(), Size: it.Size, Files: it.Files, Grabs: it.Grabs, Seeders: it.Seeders,
		Leechers: it.Leechers, Peers: it.Peers, PublishDate: it.PublishDate, Categories: it.Categories,
		DownloadURL: strip(it.Link), InfoHash: it.InfoHash, InfoURL: strip(it.InfoURL), IMDBID: it.IMDBID,
		TMDBID: it.TMDBID, TVDBID: it.TVDBID, TVMazeID: it.TVMazeID, RageID: it.RageID, Season: it.Season,
		Episode: it.Episode, Year: it.Year, Artist: it.Artist, Album: it.Album, Author: it.Author,
		BookTitle: it.BookTitle, Group: it.Group, DownloadVolumeFactor: it.DownloadVolumeFactor,
		UploadVolumeFactor: it.UploadVolumeFactor, MinimumRatio: it.MinimumRatio, Flags: []string{}, Attrs: it.Attrs,
	}
	if ind.Protocol() == Torrent {
		r.MagnetURL = it.MagnetURL
	}
	if r.Categories == nil {
		r.Categories = []int{}
	}
	if it.MinimumSeedTime != nil {
		r.MinimumSeedTime = *it.MinimumSeedTime
	}
	if f := it.DownloadVolumeFactor; f != nil {
		switch {
		case *f == 0:
			r.Flags = append(r.Flags, "freeleech")
		case *f <= 0.5:
			r.Flags = append(r.Flags, "halfleech")
		case *f < 1:
			r.Flags = append(r.Flags, "partialfreeleech")
		}
	}
	if f := it.UploadVolumeFactor; f != nil && *f >= 2 {
		r.Flags = append(r.Flags, "doubleupload")
	}
	for _, t := range it.Tags {
		if !slices.Contains(r.Flags, t) {
			r.Flags = append(r.Flags, t)
		}
	}
	for k, vs := range r.Attrs {
		for i, v := range vs {
			vs[i] = strip(v)
		}
		r.Attrs[k] = vs
	}
	return r
}
