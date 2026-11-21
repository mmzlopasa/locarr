// Package library is the shared core of the media library: item references,
// the base item model, availability rules and media-management settings.
// Movies (P4), series (P5) and music (P6) build on it; the importer, the
// renamer (naming), the recycle bin, the timeline and the wanted
// orchestration are shared too.
package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Ref kinds. A ref names one thing the timeline, searches and downloads talk
// about: a movie, and later a series, an episode, an artist or an album.
const (
	RefMovie   = "movie"
	RefSeries  = "series"
	RefSeason  = "season"
	RefEpisode = "episode"
	RefArtist  = "artist"
	RefAlbum   = "album"
)

// ErrBadRef is returned for a malformed item reference.
var ErrBadRef = errors.New("invalid item reference")

// Ref identifies one library item: "movie:42". The same string is stored in
// search_log.item_ref, timeline_events.item_ref and (as {"ref": "movie:42"})
// in downloads.item_ref.
type Ref struct {
	Kind string
	ID   int64
}

// MovieRef is the ref of a movie.
func MovieRef(id int64) Ref { return Ref{Kind: RefMovie, ID: id} }

// SeriesRef is the ref of a series.
func SeriesRef(id int64) Ref { return Ref{Kind: RefSeries, ID: id} }

// SeasonRef is the ref of a season.
func SeasonRef(id int64) Ref { return Ref{Kind: RefSeason, ID: id} }

// EpisodeRef is the ref of an episode.
func EpisodeRef(id int64) Ref { return Ref{Kind: RefEpisode, ID: id} }

func (r Ref) String() string { return r.Kind + ":" + strconv.FormatInt(r.ID, 10) }

// IsZero reports an empty ref.
func (r Ref) IsZero() bool { return r.Kind == "" && r.ID == 0 }

func validKind(k string) bool {
	switch k {
	case RefMovie, RefSeries, RefSeason, RefEpisode, RefArtist, RefAlbum:
		return true
	}
	return false
}

// ParseRef parses "kind:id".
func ParseRef(s string) (Ref, error) {
	kind, id, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || !validKind(kind) {
		return Ref{}, fmt.Errorf("%w: %q", ErrBadRef, s)
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return Ref{}, fmt.Errorf("%w: %q", ErrBadRef, s)
	}
	return Ref{Kind: kind, ID: n}, nil
}

// refJSON is the shape of downloads.item_ref. Covers lists the other items
// one download is for: the episodes of a season pack or a multi-episode
// release (D-038). A trigger copies ref and covers into download_refs.
type refJSON struct {
	Ref    string   `json:"ref"`
	Covers []string `json:"covers,omitempty"`
}

// JSON is the ref as the opaque item_ref a download carries.
func (r Ref) JSON() json.RawMessage {
	b, _ := json.Marshal(refJSON{Ref: r.String()})
	return b
}

// JSONWith is the item_ref of a download for r that also covers other items.
func (r Ref) JSONWith(covers []Ref) json.RawMessage {
	v := refJSON{Ref: r.String()}
	for _, c := range covers {
		if c != r {
			v.Covers = append(v.Covers, c.String())
		}
	}
	b, _ := json.Marshal(v)
	return b
}

// CoversFromJSON returns the items a download's item_ref covers besides its ref.
func CoversFromJSON(raw json.RawMessage) []Ref {
	var v refJSON
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []Ref
	for _, c := range v.Covers {
		if r, err := ParseRef(c); err == nil {
			out = append(out, r)
		}
	}
	return out
}

// RefFromJSON reads a download's item_ref (zero Ref when it names no item).
func RefFromJSON(raw json.RawMessage) Ref {
	var v refJSON
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return Ref{}
	}
	r, err := ParseRef(v.Ref)
	if err != nil {
		return Ref{}
	}
	return r
}
