package musicdiscovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/metadata/spotify"
)

// Seed is a library artist suggestions start from.
type Seed struct {
	MBID     string
	Name     string
	ArtistID int64
}

// Candidate is one provider's similar artist, mapped to MusicBrainz.
type Candidate struct {
	MBID           string
	Name           string
	Disambiguation string
	// Score is the provider's similarity scaled to 0..1.
	Score float64
	// Confidence is how sure the MusicBrainz id is (1 = the provider gave it).
	Confidence float64
	LastFMURL  string
	SpotifyID  string
}

// relationScore is how much a MusicBrainz relationship suggests a listener
// of one artist likes the other.
func relationScore(rel string) float64 {
	switch rel {
	case "collaboration", "member of band", "subgroup":
		return 0.7
	case "founder", "conductor position", "vocal supporting musician", "instrumental supporting musician", "supporting musician":
		return 0.45
	case "is person", "named after", "artist rename", "married", "sibling", "parent", "involved with":
		return 0 // the same person, or family: not a suggestion
	}
	return 0.3
}

// similar asks provider p for artists similar to seed.
func (s *Service) similar(ctx context.Context, p string, seed Seed) ([]Candidate, error) {
	var out []Candidate
	switch p {
	case LastFM:
		list, err := s.LastFM.Similar(ctx, seed.MBID, seed.Name, PerSeed)
		if err != nil {
			return nil, err
		}
		for _, a := range list {
			c := Candidate{MBID: a.MBID, Name: a.Name, Score: a.Match, Confidence: 0.95, LastFMURL: a.URL}
			if c.MBID == "" {
				c.MBID, c.Confidence, c.Disambiguation = s.resolve(ctx, a.Name)
			}
			out = append(out, c)
		}
	case ListenBrainz:
		list, err := s.LB.Similar(ctx, seed.MBID)
		if err != nil {
			return nil, err
		}
		top := 0.0
		for _, a := range list {
			top = max(top, a.Score)
		}
		for i, a := range list {
			if i == PerSeed {
				break
			}
			score := 0.0
			if top > 0 {
				score = a.Score / top
			}
			out = append(out, Candidate{MBID: a.MBID, Name: a.Name, Disambiguation: a.Comment, Score: score, Confidence: 1})
		}
	case MusicBrainz:
		list, err := s.MB.ArtistRelations(ctx, seed.MBID)
		if err != nil {
			return nil, err
		}
		best := map[string]Candidate{}
		for _, r := range list {
			sc := relationScore(r.Relation)
			if sc <= 0 {
				continue
			}
			if b, ok := best[r.MBID]; !ok || sc > b.Score {
				best[r.MBID] = Candidate{MBID: r.MBID, Name: r.Name, Disambiguation: r.Disambiguation, Score: sc, Confidence: 1}
			}
		}
		for _, c := range best {
			out = append(out, c)
		}
	case Spotify:
		if s.SpotifyRelated() == "restricted" && s.now().Sub(time.UnixMilli(s.spotifyCheckedAt.Load())) < 24*time.Hour {
			return nil, spotify.ErrRestricted
		}
		id, err := s.spotifyID(ctx, seed.Name)
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, metadata.ErrNotFound
		}
		list, err := s.Spotify.Related(ctx, id)
		if errors.Is(err, spotify.ErrRestricted) {
			s.SetSpotifyRelated(false)
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		s.SetSpotifyRelated(true)
		for i, a := range list {
			c := Candidate{Name: a.Name, SpotifyID: a.ID, Score: 1 - 0.6*float64(i)/float64(max(len(list), 1))}
			if r, err := s.MB.ArtistByURL(ctx, spotify.ArtistURL(a.ID)); err == nil {
				c.MBID, c.Confidence, c.Disambiguation = r.MBID, 1, r.Disambiguation
			} else {
				c.MBID, c.Confidence, c.Disambiguation = s.resolve(ctx, a.Name)
			}
			out = append(out, c)
		}
	default:
		return nil, fmt.Errorf("unknown provider %q", p)
	}
	kept := out[:0]
	for _, c := range out {
		c.MBID = strings.ToLower(c.MBID)
		if c.MBID == "" || c.MBID == seed.MBID || c.Confidence < MinConfidence {
			continue
		}
		kept = append(kept, c)
		if len(kept) == PerSeed {
			break
		}
	}
	return kept, nil
}

// spotifyID finds the Spotify artist of the same name.
func (s *Service) spotifyID(ctx context.Context, name string) (string, error) {
	list, err := s.Spotify.SearchArtists(ctx, name)
	if err != nil {
		return "", err
	}
	want := Normalize(name)
	for _, a := range list {
		if Normalize(a.Name) == want {
			return a.ID, nil
		}
	}
	return "", nil
}

type resolution struct {
	MBID           string  `json:"mbid"`
	Confidence     float64 `json:"confidence"`
	Disambiguation string  `json:"disambiguation"`
}

// resolve finds the MusicBrainz artist of a name: one artist of exactly
// that name is a good match (0.9); several of the same name take the best
// MusicBrainz search score, and how far ahead it is decides the confidence
// (0.6 or 0.5 — shown as "matched by name"); anything else is no match.
// Answers are cached for 30 days.
func (s *Service) resolve(ctx context.Context, name string) (string, float64, string) {
	key := metadata.CacheKey("discovery-resolve", Normalize(name))
	if b, ok := s.Cache.Get(ctx, key); ok {
		var r resolution
		if json.Unmarshal(b, &r) == nil {
			return r.MBID, r.Confidence, r.Disambiguation
		}
	}
	list, err := s.MB.SearchArtists(ctx, name)
	if err != nil {
		return "", 0, ""
	}
	r := resolveFrom(name, list)
	if b, err := json.Marshal(r); err == nil {
		s.Cache.Put(ctx, key, b, 30*24*time.Hour)
	}
	return r.MBID, r.Confidence, r.Disambiguation
}

func resolveFrom(name string, list []metadata.ArtistSummary) resolution {
	want := Normalize(name)
	var exact []metadata.ArtistSummary
	for _, a := range list {
		if Normalize(a.Name) == want {
			exact = append(exact, a)
		}
	}
	switch {
	case len(exact) == 0:
		return resolution{}
	case len(exact) == 1 && exact[0].Score >= 90:
		return resolution{MBID: exact[0].MBID, Confidence: 0.9, Disambiguation: exact[0].Disambiguation}
	case len(exact) == 1:
		return resolution{MBID: exact[0].MBID, Confidence: 0.6, Disambiguation: exact[0].Disambiguation}
	}
	sort.SliceStable(exact, func(i, j int) bool { return exact[i].Score > exact[j].Score })
	best, second := exact[0], exact[1]
	conf := 0.5
	if best.Score-second.Score >= 10 {
		conf = 0.6
	}
	return resolution{MBID: best.MBID, Confidence: conf, Disambiguation: best.Disambiguation}
}
