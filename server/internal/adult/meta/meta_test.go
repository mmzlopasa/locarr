package meta_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/adult/meta"
	"github.com/mmzlopasa/locarr/server/internal/adult/meta/fake"
)

func key(k string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return k, nil }
}

func clients(t *testing.T) (*httptest.Server, []meta.Provider) {
	f := fake.New("")
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.SetBase(srv.URL)
	return srv, []meta.Provider{
		meta.NewStashDB(srv.URL+"/graphql", key(fake.StashKey), nil),
		meta.NewTPDB(srv.URL+"/tpdb", key(fake.TPDBKey), nil),
	}
}

func TestProvidersAgainstFakes(t *testing.T) {
	ctx := context.Background()
	_, ps := clients(t)
	for _, p := range ps {
		t.Run(p.Name(), func(t *testing.T) {
			scenes, err := p.SearchScenes(ctx, "scene")
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range scenes {
				if s.Removed || s.Title == "Removed Scene" {
					t.Fatalf("removed entry returned: %+v", s)
				}
			}
			if len(scenes) < 2 {
				t.Fatalf("scenes: %+v", scenes)
			}
			s := scenes[0]
			if s.Studio == nil || s.Studio.Name != "Studio A" || s.Date != "2024-01-15" || len(s.Performers) != 1 || s.Image == "" {
				t.Fatalf("scene: %+v", s)
			}
			if s.PHash == "" {
				t.Error("phash missing")
			}
			one, err := p.Scene(ctx, s.ID)
			if err != nil || one.Title != "Scene Title" {
				t.Fatalf("scene by id: %+v %v", one, err)
			}
			studios, err := p.SearchStudios(ctx, "studio")
			if err != nil || len(studios) != 1 {
				t.Fatalf("studios: %+v %v", studios, err)
			}
			st, err := p.Studio(ctx, studios[0].ID)
			if err != nil || st.Name != "Studio A" {
				t.Fatalf("studio: %+v %v", st, err)
			}
			byStudio, err := p.StudioScenes(ctx, st.ID, 1)
			if err != nil || len(byStudio) != 3 { // Removed Scene is dropped
				t.Fatalf("studio scenes: %d %v", len(byStudio), err)
			}
			perfs, err := p.SearchPerformers(ctx, "performer two")
			if err != nil || len(perfs) != 1 {
				t.Fatalf("performers: %+v %v", perfs, err)
			}
			byPerf, err := p.PerformerScenes(ctx, perfs[0].ID, 1)
			if err != nil || len(byPerf) != 2 {
				t.Fatalf("performer scenes: %d %v", len(byPerf), err)
			}
		})
	}
}

func TestRemovedSceneFlagged(t *testing.T) {
	_, ps := clients(t)
	for _, p := range ps {
		id := map[string]string{meta.StashDB: "sc-3", meta.TPDB: "tp-3"}[p.Name()]
		s, err := p.Scene(context.Background(), id)
		if err != nil || !s.Removed {
			t.Errorf("%s: removed scene %+v %v", p.Name(), s, err)
		}
	}
}

func TestTPDBMovies(t *testing.T) {
	_, ps := clients(t)
	m, err := ps[1].SearchMovies(context.Background(), "movie")
	if err != nil || len(m) != 1 || !m[0].Movie || m[0].Year() != 2023 {
		t.Fatalf("movies: %+v %v", m, err)
	}
}

func TestBadKey(t *testing.T) {
	f := fake.New("")
	srv := httptest.NewServer(f)
	defer srv.Close()
	for _, p := range []meta.Provider{meta.NewStashDB(srv.URL+"/graphql", key("wrong"), nil), meta.NewTPDB(srv.URL+"/tpdb", key("wrong"), nil)} {
		if err := p.Validate(context.Background(), "wrong"); !errors.Is(err, meta.ErrBadKey) {
			t.Errorf("%s: %v", p.Name(), err)
		}
		if _, err := p.SearchScenes(context.Background(), "x"); !meta.IsKeyError(err) {
			t.Errorf("%s search: %v", p.Name(), err)
		}
	}
	if _, err := meta.NewTPDB(srv.URL+"/tpdb", key(""), nil).SearchScenes(context.Background(), "x"); !errors.Is(err, meta.ErrNoKey) {
		t.Errorf("no key: %v", err)
	}
}
