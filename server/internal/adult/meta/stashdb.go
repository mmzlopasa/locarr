package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

// DefaultStashDBURL is the GraphQL endpoint of the community database.
const DefaultStashDBURL = "https://stashdb.org/graphql"

// StashDBClient speaks the public StashDB GraphQL API with the user's
// API key (header ApiKey). StashDB has scenes, performers and studios; it
// has no separate movies.
type StashDBClient struct{ h *HTTP }

// NewStashDB returns a client (two requests a second).
func NewStashDB(endpoint string, key func(context.Context) (string, error), cache *metadata.Cache) *StashDBClient {
	if endpoint == "" {
		endpoint = DefaultStashDBURL
	}
	return &StashDBClient{h: newHTTP(endpoint, key, cache, 2)}
}

// Name implements Provider.
func (*StashDBClient) Name() string { return StashDB }

func stashAuth(r *http.Request, key string) { r.Header.Set("ApiKey", key) }

type gqlError struct {
	Message string `json:"message"`
}

func (c *StashDBClient) query(ctx context.Context, cacheKey string, ttl time.Duration, key, q string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	fetch := func() ([]byte, error) {
		b, err := c.h.do(ctx, http.MethodPost, "", body, stashAuth, key)
		if err != nil {
			return nil, err
		}
		var env struct {
			Data   json.RawMessage `json:"data"`
			Errors []gqlError      `json:"errors"`
		}
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, fmt.Errorf("StashDB answer: %w", err)
		}
		if len(env.Errors) > 0 {
			if len(env.Data) == 0 || string(env.Data) == "null" {
				return nil, fmt.Errorf("StashDB: %s", env.Errors[0].Message)
			}
		}
		return env.Data, nil
	}
	var (
		data []byte
		err  error
	)
	if cacheKey == "" {
		data, err = fetch()
	} else {
		data, err = c.h.cached(ctx, "stashdb:"+cacheKey, ttl, fetch)
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

const stashSceneFields = `id title details release_date date duration deleted
	images { url width height }
	studio { id name }
	performers { performer { id name disambiguation deleted images { url } } }
	tags { name }
	fingerprints { hash algorithm }`

type stashScene struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Details     string `json:"details"`
	ReleaseDate string `json:"release_date"`
	Date        string `json:"date"`
	Duration    int    `json:"duration"`
	Deleted     bool   `json:"deleted"`
	Images      []struct {
		URL string `json:"url"`
	} `json:"images"`
	Studio *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"studio"`
	Performers []struct {
		Performer stashPerformer `json:"performer"`
	} `json:"performers"`
	Tags []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Fingerprints []struct {
		Hash      string `json:"hash"`
		Algorithm string `json:"algorithm"`
	} `json:"fingerprints"`
}

type stashPerformer struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Disambiguation string   `json:"disambiguation"`
	Aliases        []string `json:"aliases"`
	Deleted        bool     `json:"deleted"`
	Images         []struct {
		URL string `json:"url"`
	} `json:"images"`
}

type stashStudio struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
	URLs    []struct {
		URL string `json:"url"`
	} `json:"urls"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	Parent *struct {
		Name string `json:"name"`
	} `json:"parent"`
}

func (s stashScene) scene() Scene {
	out := Scene{Source: StashDB, ID: s.ID, Title: s.Title, Date: s.ReleaseDate, Duration: s.Duration, Overview: s.Details, Removed: s.Deleted}
	if out.Date == "" {
		out.Date = s.Date
	}
	if len(out.Date) > 10 {
		out.Date = out.Date[:10]
	}
	if len(s.Images) > 0 {
		out.Image = s.Images[0].URL
	}
	if s.Studio != nil {
		out.Studio = &Studio{Source: StashDB, ID: s.Studio.ID, Name: s.Studio.Name}
	}
	for _, p := range s.Performers {
		if !p.Performer.Deleted {
			out.Performers = append(out.Performers, p.Performer.performer())
		}
	}
	for _, t := range s.Tags {
		out.Tags = append(out.Tags, t.Name)
	}
	for _, f := range s.Fingerprints {
		if f.Algorithm == "PHASH" && out.PHash == "" {
			out.PHash = f.Hash
		}
	}
	return out
}

func (p stashPerformer) performer() Performer {
	out := Performer{Source: StashDB, ID: p.ID, Name: p.Name, Disambiguation: p.Disambiguation, Aliases: p.Aliases}
	if len(p.Images) > 0 {
		out.Image = p.Images[0].URL
	}
	return out
}

func (s stashStudio) studio() Studio {
	out := Studio{Source: StashDB, ID: s.ID, Name: s.Name}
	if len(s.URLs) > 0 {
		out.URL = s.URLs[0].URL
	}
	if len(s.Images) > 0 {
		out.Image = s.Images[0].URL
	}
	if s.Parent != nil {
		out.Parent = s.Parent.Name
	}
	return out
}

// Validate implements Provider (asks who the key belongs to).
func (c *StashDBClient) Validate(ctx context.Context, key string) error {
	var out struct {
		Me *struct {
			ID string `json:"id"`
		} `json:"me"`
	}
	if err := c.query(ctx, "", 0, key, `query { me { id } }`, nil, &out); err != nil {
		return err
	}
	if out.Me == nil {
		return ErrBadKey
	}
	return nil
}

// SearchScenes implements Provider.
func (c *StashDBClient) SearchScenes(ctx context.Context, term string) ([]Scene, error) {
	var out struct {
		SearchScene []stashScene `json:"searchScene"`
	}
	if err := c.query(ctx, "search-scene:"+term, time.Hour, "", `query($term: String!) { searchScene(term: $term, limit: 25) { `+stashSceneFields+` } }`,
		map[string]any{"term": term}, &out); err != nil {
		return nil, err
	}
	return scenes(out.SearchScene), nil
}

func scenes(list []stashScene) []Scene {
	out := []Scene{}
	for _, s := range list {
		if s.Deleted {
			continue
		}
		out = append(out, s.scene())
	}
	return out
}

// FindScenes implements Provider. Relevance is StashDB's text search
// (searchScene, one page); the other orders use queryScenes with its text
// filter and sort (DATE or TITLE, ascending or descending) and its count.
func (c *StashDBClient) FindScenes(ctx context.Context, term string, movie bool, o Find) (ScenePage, error) {
	if movie {
		return ScenePage{Scenes: []Scene{}, Ordered: true}, nil
	}
	if o.Sort == SortRelevance {
		list, err := c.SearchScenes(ctx, term)
		return ScenePage{Scenes: list, Ordered: true}, err
	}
	sort, dir := "DATE", "DESC"
	switch o.Sort {
	case SortOldest:
		dir = "ASC"
	case SortTitle:
		sort, dir = "TITLE", "ASC"
	}
	page := max(o.Page, 1)
	var out struct {
		Q struct {
			Count  int          `json:"count"`
			Scenes []stashScene `json:"scenes"`
		} `json:"queryScenes"`
	}
	in := map[string]any{"text": term, "page": page, "per_page": PageSize, "sort": sort, "direction": dir}
	if err := c.query(ctx, fmt.Sprintf("find-scene:%s:%s:%s:%d", term, sort, dir, page), time.Hour, "",
		`query($input: SceneQueryInput!) { queryScenes(input: $input) { count scenes { `+stashSceneFields+` } } }`,
		map[string]any{"input": in}, &out); err != nil {
		return ScenePage{}, err
	}
	return ScenePage{Scenes: scenes(out.Q.Scenes), More: page*PageSize < out.Q.Count, Ordered: true}, nil
}

// SearchMovies implements Provider: StashDB has no movies.
func (c *StashDBClient) SearchMovies(context.Context, string) ([]Scene, error) { return []Scene{}, nil }

// Movie implements Provider.
func (c *StashDBClient) Movie(context.Context, string) (Scene, error) { return Scene{}, ErrNotFound }

// SearchPerformers implements Provider.
func (c *StashDBClient) SearchPerformers(ctx context.Context, term string) ([]Performer, error) {
	var out struct {
		List []stashPerformer `json:"searchPerformer"`
	}
	if err := c.query(ctx, "search-performer:"+term, time.Hour, "", `query($term: String!) { searchPerformer(term: $term, limit: 25) {
		id name disambiguation aliases deleted images { url } } }`, map[string]any{"term": term}, &out); err != nil {
		return nil, err
	}
	res := []Performer{}
	for _, p := range out.List {
		if !p.Deleted {
			res = append(res, p.performer())
		}
	}
	return res, nil
}

const stashStudioFields = `id name deleted urls { url } images { url } parent { name }`

// SearchStudios implements Provider.
func (c *StashDBClient) SearchStudios(ctx context.Context, term string) ([]Studio, error) {
	var out struct {
		List []stashStudio `json:"searchStudio"`
	}
	if err := c.query(ctx, "search-studio:"+term, time.Hour, "", `query($term: String!) { searchStudio(term: $term, limit: 25) { `+stashStudioFields+` } }`,
		map[string]any{"term": term}, &out); err != nil {
		return nil, err
	}
	res := []Studio{}
	for _, s := range out.List {
		if !s.Deleted {
			res = append(res, s.studio())
		}
	}
	return res, nil
}

// Scene implements Provider.
func (c *StashDBClient) Scene(ctx context.Context, id string) (Scene, error) {
	var out struct {
		Scene *stashScene `json:"findScene"`
	}
	if err := c.query(ctx, "scene:"+id, 12*time.Hour, "", `query($id: ID!) { findScene(id: $id) { `+stashSceneFields+` } }`,
		map[string]any{"id": id}, &out); err != nil {
		return Scene{}, err
	}
	if out.Scene == nil {
		return Scene{}, ErrNotFound
	}
	return out.Scene.scene(), nil
}

// Studio implements Provider.
func (c *StashDBClient) Studio(ctx context.Context, id string) (Studio, error) {
	var out struct {
		Studio *stashStudio `json:"findStudio"`
	}
	if err := c.query(ctx, "studio:"+id, 12*time.Hour, "", `query($id: ID!) { findStudio(id: $id) { `+stashStudioFields+` } }`,
		map[string]any{"id": id}, &out); err != nil {
		return Studio{}, err
	}
	if out.Studio == nil {
		return Studio{}, ErrNotFound
	}
	if out.Studio.Deleted {
		return Studio{}, ErrRemoved
	}
	return out.Studio.studio(), nil
}

// Performer implements Provider.
func (c *StashDBClient) Performer(ctx context.Context, id string) (Performer, error) {
	var out struct {
		P *stashPerformer `json:"findPerformer"`
	}
	if err := c.query(ctx, "performer:"+id, 12*time.Hour, "", `query($id: ID!) { findPerformer(id: $id) {
		id name disambiguation aliases deleted images { url } } }`, map[string]any{"id": id}, &out); err != nil {
		return Performer{}, err
	}
	if out.P == nil {
		return Performer{}, ErrNotFound
	}
	if out.P.Deleted {
		return Performer{}, ErrRemoved
	}
	return out.P.performer(), nil
}

func (c *StashDBClient) queryScenes(ctx context.Context, field, id string, page int) ([]Scene, error) {
	var out struct {
		Q struct {
			Scenes []stashScene `json:"scenes"`
		} `json:"queryScenes"`
	}
	in := map[string]any{field: map[string]any{"value": []string{id}, "modifier": "INCLUDES"},
		"page": max(page, 1), "per_page": 50, "sort": "DATE", "direction": "DESC"}
	if err := c.query(ctx, fmt.Sprintf("query-%s:%s:%d", field, id, page), time.Hour, "",
		`query($input: SceneQueryInput!) { queryScenes(input: $input) { scenes { `+stashSceneFields+` } } }`,
		map[string]any{"input": in}, &out); err != nil {
		return nil, err
	}
	return scenes(out.Q.Scenes), nil
}

// StudioScenes implements Provider.
func (c *StashDBClient) StudioScenes(ctx context.Context, id string, page int) ([]Scene, error) {
	return c.queryScenes(ctx, "studios", id, page)
}

// PerformerScenes implements Provider.
func (c *StashDBClient) PerformerScenes(ctx context.Context, id string, page int) ([]Scene, error) {
	return c.queryScenes(ctx, "performers", id, page)
}

var _ Provider = (*StashDBClient)(nil)

// IsKeyError reports a missing or refused key.
func IsKeyError(err error) bool { return errors.Is(err, ErrNoKey) || errors.Is(err, ErrBadKey) }
