package adult

import (
	"context"
	"slices"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/wanted"
)

// Source plugs scenes (or movies) into the wanted orchestration. While the
// feature is off it wants nothing and matches nothing.
type Source struct {
	S     *Service
	Movie bool
	// Gallery: image sets (D-113).
	Gallery bool
}

var (
	_ wanted.Source      = Source{}
	_ wanted.AdultSource = Source{}
)

// Adult implements wanted.AdultSource.
func (Source) Adult() bool { return true }

// Kind implements wanted.Source.
func (w Source) Kind() string {
	switch {
	case w.Gallery:
		return library.RefAdultGallery
	case w.Movie:
		return library.RefAdultMovie
	}
	return library.RefScene
}

func (w Source) kind() string {
	switch {
	case w.Gallery:
		return KindGallery
	case w.Movie:
		return KindMovie
	}
	return KindScene
}

// cutoffMet reports a file that needs no upgrade.
func cutoffMet(p *quality.Profile, f *File) bool {
	if f == nil || p == nil {
		return false
	}
	if f.ImageCount > 0 || f.Quality == GalleryQuality {
		return true // a gallery has no upgrades (D-113)
	}
	pos, _ := p.Position(f.Quality)
	if !p.UpgradesAllowed {
		return true
	}
	if pos < 0 {
		return false
	}
	if cut := p.CutoffPosition(); cut >= 0 && pos > cut {
		return false
	}
	return p.CutoffFormatScore <= 0 || f.Score >= p.CutoffFormatScore
}

// sceneDate writes 2024-01-15 as "24 01 15": scene names carry 24.01.15,
// and indexers split words on the dots.
func sceneDate(d string) string {
	if len(d) != 10 {
		return ""
	}
	return d[2:4] + " " + d[5:7] + " " + d[8:10]
}

func (w Source) toItem(ctx context.Context, it *Item, p *quality.Profile) wanted.Item {
	today := w.S.now().UTC().Format("2006-01-02")
	out := wanted.Item{Ref: it.Ref(), Title: it.Title, Monitored: it.Monitored, Available: it.ReleaseDate == "" || it.ReleaseDate <= today,
		HasFile: it.File != nil, CutoffMet: cutoffMet(p, it.File), ProfileID: it.QualityProfileID, MediaKind: downloads.KindAdult,
		BlockedTerms: w.S.OwnTerms(ctx)}
	var perfs []string
	for _, pf := range it.Performers {
		perfs = append(perfs, pf.Name)
	}
	runtime := it.Duration / 60
	if runtime == 0 {
		runtime = 30
		if it.Kind == KindMovie {
			runtime = 100
		}
	}
	out.Wanted = decision.Wanted{Kind: quality.Movies, Title: it.Title, Year: it.Year, RuntimeMinutes: runtime,
		Adult: &decision.Adult{Scene: it.Kind == KindScene, Studios: []string{it.Studio}, Date: it.ReleaseDate, Title: it.Title, Year: it.Year,
			Performers: perfs}}
	if it.Kind == KindGallery {
		a := out.Wanted.Adult
		a.Scene, a.Gallery = false, true
		if it.Studio == "" {
			a.Studios = nil
		}
		minMB, _ := settings.Get(ctx, w.S.Settings, GalleryMinMB)
		maxMB, _ := settings.Get(ctx, w.S.Settings, GalleryMaxMB)
		a.MinSize, a.MaxSize = int64(max(minMB, 0))<<20, int64(max(maxMB, 0))<<20
		out.CutoffMet = it.File != nil
	} else if it.File != nil {
		out.Wanted.Current = &decision.Current{Quality: it.File.Quality, Revision: it.File.Revision, Score: it.File.Score}
	}
	terms := searchTerms(it.Kind != KindMovie, it.Studio, it.ReleaseDate, it.Title, it.Year, perfs)
	if it.Kind == KindGallery {
		// Public sites name sets "Studio.24.01.15.Performer.Title.XXX.IMAGESET";
		// asking for the word first finds them among the videos.
		terms = append([]string{terms[0] + " imageset"}, terms...)
	}
	out.Query = indexers.Query{Type: indexers.SearchAdult, Term: terms[0]}
	out.AltTerms = terms[1:]
	return out
}

// maxTitleWords keeps "performer + title" searches short: sites match every
// word, and long titles are often cut in release names.
const maxTitleWords = 6

// searchTerms lists the texts to search for, best first (D-108). Public sites
// name a scene in several ways: "Studio.20.08.19.Performer.Title",
// "[Studio] Performer - Title (2020)", "Performer - Title - Studio". So a
// scene is searched as studio + date ("Studio Name 20 08 19"), the studio
// written together + dotted date ("StudioName 20.08.19"), the first
// performer + the title words, and the title alone. Movies are searched by
// title + year, then title. Repeats are dropped by the caller.
func searchTerms(scene bool, studio, date, title string, year int, performers []string) []string {
	clean := naming.CleanTitle(title)
	if !scene {
		if year > 0 {
			return []string{clean + " " + yearStr(year), clean}
		}
		return []string{clean}
	}
	var out []string
	if d := sceneDate(date); d != "" && studio != "" {
		st := naming.CleanTitle(studio)
		out = append(out, st+" "+d, strings.ReplaceAll(st, " ", "")+" "+strings.ReplaceAll(d, " ", "."))
	}
	if core := titleWords(title, performers); len(performers) > 0 && core != "" {
		out = append(out, naming.CleanTitle(performers[0])+" "+core)
	}
	if clean != "" {
		out = append(out, clean)
	}
	if len(out) == 0 {
		out = append(out, naming.CleanTitle(studio))
	}
	return out
}

// titleWords is the title without performer names and joining words, cut to
// maxTitleWords words.
func titleWords(title string, performers []string) string {
	words := strings.Fields(naming.CleanTitle(title))
	for _, p := range performers {
		name := strings.Fields(naming.CleanTitle(p))
		if len(name) == 0 {
			continue
		}
		for i := 0; i+len(name) <= len(words); {
			if wordsEqual(words[i:i+len(name)], name) {
				words = slices.Delete(words, i, i+len(name))
				continue
			}
			i++
		}
	}
	words = slices.DeleteFunc(words, func(w string) bool {
		switch strings.ToLower(w) {
		case "with", "and", "feat", "ft", "featuring", "starring", "vs":
			return true
		}
		return false
	})
	if len(words) > maxTitleWords {
		words = words[:maxTitleWords]
	}
	return strings.Join(words, " ")
}

func wordsEqual(a, b []string) bool {
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

// Item implements wanted.Source.
func (w Source) Item(ctx context.Context, id int64) (wanted.Item, error) {
	if !w.S.On(ctx) {
		return wanted.Item{}, ErrNotFound
	}
	it, err := w.S.Item(ctx, id)
	if err != nil {
		return wanted.Item{}, err
	}
	if it.Kind != w.kind() {
		return wanted.Item{}, ErrNotFound
	}
	p, err := w.S.Quality.Profile(ctx, it.QualityProfileID)
	if err != nil {
		return wanted.Item{}, err
	}
	p.Normalize()
	return w.toItem(ctx, &it, &p), nil
}

// Wanted implements wanted.Source.
func (w Source) Wanted(ctx context.Context) ([]wanted.Item, error) {
	if !w.S.On(ctx) {
		return nil, nil
	}
	items, _, err := w.S.Items(ctx, ItemFilter{Kind: w.kind(), Limit: -1})
	if err != nil {
		return nil, err
	}
	profiles := map[int64]*quality.Profile{}
	out := []wanted.Item{}
	for i := range items {
		it := &items[i]
		if !it.Monitored {
			continue
		}
		p, ok := profiles[it.QualityProfileID]
		if !ok {
			pp, err := w.S.Quality.Profile(ctx, it.QualityProfileID)
			if err != nil {
				continue
			}
			pp.Normalize()
			p = &pp
			profiles[it.QualityProfileID] = p
		}
		wi := w.toItem(ctx, it, p)
		if !wi.Available || (wi.HasFile && wi.CutoffMet) {
			continue
		}
		out = append(out, wi)
	}
	return out, nil
}

// Matcher implements wanted.Source: scenes by studio and date, movies by
// title (and year ±1).
func (w Source) Matcher(ctx context.Context) (wanted.Matcher, error) {
	if !w.S.On(ctx) {
		return func(*indexers.Release, *parser.Release) []library.Ref { return nil }, nil
	}
	items, _, err := w.S.Items(ctx, ItemFilter{Kind: w.kind(), Limit: -1})
	if err != nil {
		return nil, err
	}
	byKey := map[string][]library.Ref{}
	type movie struct {
		ref   library.Ref
		title string
		year  int
	}
	var movies []movie
	for i := range items {
		it := &items[i]
		if !it.Monitored {
			continue
		}
		if it.Kind != KindMovie {
			k := decision.StudioKey(it.Studio) + "|" + it.ReleaseDate
			byKey[k] = append(byKey[k], it.Ref())
		} else {
			movies = append(movies, movie{it.Ref(), compact(it.Title), it.Year})
		}
	}
	return func(r *indexers.Release, p *parser.Release) []library.Ref {
		if w.Movie {
			name := compact(r.Title)
			var out []library.Ref
			for _, m := range movies {
				if m.title != "" && strings.HasPrefix(name, m.title) && (p.Year == 0 || m.year == 0 || (p.Year >= m.year-1 && p.Year <= m.year+1)) {
					out = append(out, m.ref)
				}
			}
			return out
		}
		sc, ok := parser.ParseScene(r.Title)
		if !ok {
			return nil
		}
		// Image sets go to galleries, videos to scenes.
		g := parser.ParseGallery(r.Title)
		set := (g.Images || slices.Contains(r.Categories, decision.ImageSetCategory)) && !g.Video
		if set != w.Gallery {
			return nil
		}
		return byKey[decision.StudioKey(sc.Studio)+"|"+sc.Date]
	}, nil
}

func compact(s string) string { return strings.ReplaceAll(decision.NormWords(s), " ", "") }
