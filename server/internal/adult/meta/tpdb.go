package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

// DefaultTPDBURL is ThePornDB's public API.
const DefaultTPDBURL = "https://api.theporndb.net"

// TPDBClient speaks ThePornDB's public REST API with the user's API key
// (Authorization: Bearer). Sites are studios.
type TPDBClient struct{ h *HTTP }

// NewTPDB returns a client (two requests a second).
func NewTPDB(base string, key func(context.Context) (string, error), cache *metadata.Cache) *TPDBClient {
	if base == "" {
		base = DefaultTPDBURL
	}
	return &TPDBClient{h: newHTTP(base, key, cache, 2)}
}

// Name implements Provider.
func (*TPDBClient) Name() string { return TPDB }

func tpdbAuth(r *http.Request, key string) { r.Header.Set("Authorization", "Bearer "+key) }

func (c *TPDBClient) get(ctx context.Context, path string, q url.Values, ttl time.Duration, key string, out any) error {
	full := path
	if len(q) > 0 {
		full += "?" + q.Encode()
	}
	fetch := func() ([]byte, error) { return c.h.do(ctx, http.MethodGet, full, nil, tpdbAuth, key) }
	var (
		b   []byte
		err error
	)
	if ttl == 0 {
		b, err = fetch()
	} else {
		b, err = c.h.cached(ctx, "tpdb:"+full, ttl, fetch)
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("ThePornDB answer: %w", err)
	}
	return nil
}

// flexID reads an id given as a string or a number.
type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*f = flexID(s)
	return nil
}

type tpdbSite struct {
	ID     flexID `json:"id"`
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Logo   string `json:"logo"`
	Poster string `json:"poster"`
	Parent *struct {
		Name string `json:"name"`
	} `json:"parent"`
}

type tpdbPerformer struct {
	ID         flexID   `json:"id"`
	Name       string   `json:"name"`
	Disambig   string   `json:"disambiguation"`
	Aliases    []string `json:"aliases"`
	Image      string   `json:"image"`
	Thumbnail  string   `json:"thumbnail"`
	DeletedAt  *string  `json:"deleted_at"`
	IsDisputed bool     `json:"is_disputed"`
}

type tpdbScene struct {
	ID          flexID          `json:"id"`
	Title       string          `json:"title"`
	Type        string          `json:"type"`
	Date        string          `json:"date"`
	Description string          `json:"description"`
	Duration    int             `json:"duration"`
	Image       string          `json:"image"`
	Poster      string          `json:"poster"`
	Site        *tpdbSite       `json:"site"`
	Performers  []tpdbPerformer `json:"performers"`
	Tags        []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Hashes []struct {
		Hash string `json:"hash"`
		Type string `json:"type"`
	} `json:"hashes"`
	DeletedAt  *string `json:"deleted_at"`
	IsDisputed bool    `json:"is_disputed"`
}

func (s tpdbSite) studio() Studio {
	id := s.UUID
	if id == "" {
		id = string(s.ID)
	}
	out := Studio{Source: TPDB, ID: id, Name: s.Name, URL: s.URL, Image: s.Logo}
	if out.Image == "" {
		out.Image = s.Poster
	}
	if s.Parent != nil {
		out.Parent = s.Parent.Name
	}
	return out
}

func (p tpdbPerformer) performer() Performer {
	out := Performer{Source: TPDB, ID: string(p.ID), Name: p.Name, Disambiguation: p.Disambig, Aliases: p.Aliases, Image: p.Image}
	if out.Image == "" {
		out.Image = p.Thumbnail
	}
	return out
}

func (s tpdbScene) scene(movie bool) Scene {
	out := Scene{Source: TPDB, ID: string(s.ID), Movie: movie || strings.EqualFold(s.Type, "movie"), Title: s.Title, Duration: s.Duration,
		Overview: s.Description, Image: s.Image, Removed: s.DeletedAt != nil || s.IsDisputed}
	if len(s.Date) >= 10 {
		out.Date = s.Date[:10]
	}
	if out.Image == "" {
		out.Image = s.Poster
	}
	if s.Site != nil {
		st := s.Site.studio()
		out.Studio = &st
	}
	for _, p := range s.Performers {
		if p.DeletedAt == nil && !p.IsDisputed {
			out.Performers = append(out.Performers, p.performer())
		}
	}
	for _, t := range s.Tags {
		out.Tags = append(out.Tags, t.Name)
	}
	for _, h := range s.Hashes {
		if strings.EqualFold(h.Type, "PHASH") && out.PHash == "" {
			out.PHash = h.Hash
		}
	}
	return out
}

func listScenes(list []tpdbScene, movie bool) []Scene {
	out := []Scene{}
	for _, s := range list {
		if sc := s.scene(movie); !sc.Removed {
			out = append(out, sc)
		}
	}
	return out
}

// Validate implements Provider.
func (c *TPDBClient) Validate(ctx context.Context, key string) error {
	var out struct {
		Data []tpdbScene `json:"data"`
	}
	return c.get(ctx, "/scenes", url.Values{"q": {"a"}, "per_page": {"1"}}, 0, key, &out)
}

// tpdbOrder is ThePornDB's orderBy for a search order. Its documented
// order is newest release first (recently_released); relevance is its
// default for a text search. Other orders have no documented value, so the
// page comes back in relevance order and the caller orders it.
func tpdbOrder(sort string) (string, bool) {
	switch sort {
	case "", SortNewest:
		return "recently_released", true
	case SortRelevance:
		return "", true
	}
	return "", false
}

// FindScenes implements Provider.
func (c *TPDBClient) FindScenes(ctx context.Context, term string, movie bool, o Find) (ScenePage, error) {
	var out struct {
		Data []tpdbScene `json:"data"`
		Meta struct {
			CurrentPage int `json:"current_page"`
			LastPage    int `json:"last_page"`
		} `json:"meta"`
	}
	path := "/scenes"
	if movie {
		path = "/movies"
	}
	page := max(o.Page, 1)
	q := url.Values{"q": {term}, "per_page": {strconv.Itoa(PageSize)}, "page": {strconv.Itoa(page)}}
	order, ordered := tpdbOrder(o.Sort)
	if order != "" {
		q.Set("orderBy", order)
	}
	if err := c.get(ctx, path, q, time.Hour, "", &out); err != nil {
		return ScenePage{}, err
	}
	more := len(out.Data) >= PageSize
	if out.Meta.LastPage > 0 {
		more = page < out.Meta.LastPage
	}
	return ScenePage{Scenes: listScenes(out.Data, movie), More: more, Ordered: ordered}, nil
}

// SearchScenes implements Provider (relevance, first page).
func (c *TPDBClient) SearchScenes(ctx context.Context, term string) ([]Scene, error) {
	p, err := c.FindScenes(ctx, term, false, Find{Sort: SortRelevance})
	return p.Scenes, err
}

// SearchMovies implements Provider (relevance, first page).
func (c *TPDBClient) SearchMovies(ctx context.Context, term string) ([]Scene, error) {
	p, err := c.FindScenes(ctx, term, true, Find{Sort: SortRelevance})
	return p.Scenes, err
}

// SearchPerformers implements Provider.
func (c *TPDBClient) SearchPerformers(ctx context.Context, term string) ([]Performer, error) {
	var out struct {
		Data []tpdbPerformer `json:"data"`
	}
	if err := c.get(ctx, "/performers", url.Values{"q": {term}, "per_page": {"25"}}, time.Hour, "", &out); err != nil {
		return nil, err
	}
	res := []Performer{}
	for _, p := range out.Data {
		if p.DeletedAt == nil && !p.IsDisputed {
			res = append(res, p.performer())
		}
	}
	return res, nil
}

// SearchStudios implements Provider.
func (c *TPDBClient) SearchStudios(ctx context.Context, term string) ([]Studio, error) {
	var out struct {
		Data []tpdbSite `json:"data"`
	}
	if err := c.get(ctx, "/sites", url.Values{"q": {term}, "per_page": {"25"}}, time.Hour, "", &out); err != nil {
		return nil, err
	}
	res := []Studio{}
	for _, s := range out.Data {
		res = append(res, s.studio())
	}
	return res, nil
}

func (c *TPDBClient) one(ctx context.Context, path string, movie bool) (Scene, error) {
	var out struct {
		Data *tpdbScene `json:"data"`
	}
	if err := c.get(ctx, path, nil, 12*time.Hour, "", &out); err != nil {
		return Scene{}, err
	}
	if out.Data == nil {
		return Scene{}, ErrNotFound
	}
	return out.Data.scene(movie), nil
}

// Scene implements Provider.
func (c *TPDBClient) Scene(ctx context.Context, id string) (Scene, error) {
	return c.one(ctx, "/scenes/"+url.PathEscape(id), false)
}

// Movie implements Provider.
func (c *TPDBClient) Movie(ctx context.Context, id string) (Scene, error) {
	return c.one(ctx, "/movies/"+url.PathEscape(id), true)
}

// Studio implements Provider.
func (c *TPDBClient) Studio(ctx context.Context, id string) (Studio, error) {
	var out struct {
		Data *tpdbSite `json:"data"`
	}
	if err := c.get(ctx, "/sites/"+url.PathEscape(id), nil, 12*time.Hour, "", &out); err != nil {
		return Studio{}, err
	}
	if out.Data == nil {
		return Studio{}, ErrNotFound
	}
	return out.Data.studio(), nil
}

// Performer implements Provider.
func (c *TPDBClient) Performer(ctx context.Context, id string) (Performer, error) {
	var out struct {
		Data *tpdbPerformer `json:"data"`
	}
	if err := c.get(ctx, "/performers/"+url.PathEscape(id), nil, 12*time.Hour, "", &out); err != nil {
		return Performer{}, err
	}
	if out.Data == nil {
		return Performer{}, ErrNotFound
	}
	if out.Data.DeletedAt != nil || out.Data.IsDisputed {
		return Performer{}, ErrRemoved
	}
	return out.Data.performer(), nil
}

func (c *TPDBClient) scenesBy(ctx context.Context, param, id string, page int) ([]Scene, error) {
	var out struct {
		Data []tpdbScene `json:"data"`
	}
	q := url.Values{param: {id}, "page": {strconv.Itoa(max(page, 1))}, "per_page": {"50"}, "orderBy": {"recently_released"}}
	if err := c.get(ctx, "/scenes", q, time.Hour, "", &out); err != nil {
		return nil, err
	}
	return listScenes(out.Data, false), nil
}

// StudioScenes implements Provider.
func (c *TPDBClient) StudioScenes(ctx context.Context, id string, page int) ([]Scene, error) {
	return c.scenesBy(ctx, "site_id", id, page)
}

// PerformerScenes implements Provider.
func (c *TPDBClient) PerformerScenes(ctx context.Context, id string, page int) ([]Scene, error) {
	return c.scenesBy(ctx, "performer_id", id, page)
}

var _ Provider = (*TPDBClient)(nil)
