package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	afake "github.com/mmzlopasa/locarr/server/internal/adult/meta/fake"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native/sitefake"
)

// TestAdultBuiltinIndexers runs adult search through the built-in public
// sites (recorded answers, synthetic names, D-108): adult-only sites are
// offered only while the library is on and are opted in when added; sites
// without adult categories are not offered and never asked; an adult scene
// search finds the scene on the general and adult-only sites; normal
// searches never return an adult release or ask an adult-only site.
func TestAdultBuiltinIndexers(t *testing.T) {
	ae := newAdultEnv(t)
	if _, err := ae.deps.DB.W.Exec(`DELETE FROM indexers`); err != nil { // only the built-in sites below
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.StripPrefix("/sites", sitefake.New()))
	t.Cleanup(srv.Close)

	type definition struct {
		ID    string
		Adult bool
	}
	catalog := func() []definition {
		var c struct{ Definitions []definition }
		ae.expect(ae.do("GET", "/api/v1/indexers/definitions", nil), 200).json(&c)
		return c.Definitions
	}
	for _, d := range catalog() {
		if d.Adult || d.ID == "sukebei" || d.ID == "xxxclub" {
			t.Fatalf("adult-only site %s offered while the adult library is off", d.ID)
		}
	}

	// Enable the library, grant the administrator, add the studio's scenes.
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true,
		"stashdbKey": afake.StashKey, "tpdbKey": afake.TPDBKey, "defaultProfileId": ae.profileID}), 200)
	admin := ae.me()
	ae.expect(ae.do("PATCH", "/api/v1/users/"+itoa(admin.ID), map[string]any{"permissions": map[string]bool{"requestMovies": true,
		"requestSeries": true, "requestMusic": true, "autoApprove": true, "request4k": true, "manageLibrary": true, "manageRequests": true,
		"viewAdult": true}}), 200)
	adultOnly := 0
	for _, d := range catalog() {
		if d.Adult {
			adultOnly++
		}
	}
	if adultOnly != 2 {
		t.Fatalf("%d adult-only sites in the catalog", adultOnly)
	}
	ae.expect(ae.do("POST", "/api/v1/adult/rootfolders", map[string]any{"path": ae.adultRoot}), 201)
	ae.expect(ae.do("POST", "/api/v1/adult/studios", map[string]any{"source": "stashdb", "id": lookupID(t, ae, "studio"), "monitored": true,
		"search": false}), 201)
	var page struct{ Items []apiAdultItem }
	ae.expect(ae.do("GET", "/api/v1/adult/items?kind=scene", nil), 200).json(&page)
	var scene apiAdultItem
	for _, it := range page.Items {
		if it.Title == "Scene Title" {
			scene = it
		}
	}
	if scene.ID == 0 {
		t.Fatal("no scene")
	}

	// Add the sites as the one-click "Use" does.
	type indexer struct {
		ID           int64
		Name         string
		AdultCapable bool
		Categories   struct{ Movies, Tv, Adult []int }
	}
	sites := []string{"thepiratebay", "knaben", "bitsearch", "eztv", "yts", "sukebei", "xxxclub"}
	byName := map[string]indexer{}
	for _, s := range sites {
		var ind indexer
		ae.expect(ae.do("POST", "/api/v1/indexers", map[string]any{"name": s, "kind": "native", "definition": s,
			"baseUrl": srv.URL + "/sites/" + s}), 201).json(&ind)
		byName[s] = ind
	}
	for _, s := range sites {
		ind := byName[s]
		wantCapable := s != "eztv" && s != "yts"
		if ind.AdultCapable != wantCapable {
			t.Errorf("%s: adultCapable %v", s, ind.AdultCapable)
		}
		switch s {
		case "sukebei", "xxxclub":
			if len(ind.Categories.Adult) == 0 || len(ind.Categories.Movies) > 0 || len(ind.Categories.Tv) > 0 {
				t.Errorf("%s: an adult-only site starts opted in to adult only: %+v", s, ind.Categories)
			}
		default:
			if len(ind.Categories.Adult) > 0 {
				t.Errorf("%s: opted in to adult by default", s)
			}
		}
	}
	// The owner ticks every general site, including two without adult categories.
	if _, err := ae.deps.DB.W.Exec(`UPDATE indexers SET categories = json_set(categories, '$.adult', json('[6000]'))`); err != nil {
		t.Fatal(err)
	}

	// Normal searches: no adult release, no adult-only site asked.
	type searchResult struct {
		Releases []struct {
			Title       string
			IndexerName string
			Categories  []int
		}
		Indexers []struct{ IndexerName, Status string }
	}
	for _, q := range []string{"/api/v1/search?q=Scene%20Title&type=movie", "/api/v1/search?q=StudioA%2024%2001%2015",
		"/api/v1/search?q=StudioA%2024%2001%2015&type=tv"} {
		var sr searchResult
		ae.expect(ae.do("GET", q, nil), 200).json(&sr)
		for _, r := range sr.Releases {
			if strings.Contains(r.Title, "StudioA") || strings.Contains(r.Title, "Performer") ||
				slices.ContainsFunc(r.Categories, func(c int) bool { return c >= 6000 && c < 7000 }) {
				t.Errorf("%s: adult release %q from %s", q, r.Title, r.IndexerName)
			}
		}
		for _, i := range sr.Indexers {
			if i.IndexerName == "sukebei" || i.IndexerName == "xxxclub" {
				t.Errorf("%s asked the adult-only site %s", q, i.IndexerName)
			}
		}
	}

	// Interactive adult search: the scene from every adult-capable site; the
	// sites without adult categories are not asked at all.
	var is struct {
		Accepted  int
		Decisions []struct {
			Decision struct {
				Candidate struct {
					Title       string
					IndexerName string
				}
				Status     string
				Rejections []struct{ Code string }
			}
		}
		Indexers []struct {
			IndexerName, Status string
			Count               int
		}
	}
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d/releases", scene.ID), nil), 200).json(&is)
	asked := map[string]int{}
	for _, i := range is.Indexers {
		if i.Status != "ok" {
			t.Errorf("%s: %s", i.IndexerName, i.Status)
		}
		asked[i.IndexerName] = i.Count
	}
	for _, s := range []string{"thepiratebay", "knaben", "bitsearch", "sukebei", "xxxclub"} {
		if asked[s] == 0 {
			t.Errorf("%s returned nothing for the scene: %v", s, asked)
		}
	}
	if _, ok := asked["eztv"]; ok {
		t.Error("EZTV was asked for an adult search")
	}
	if _, ok := asked["yts"]; ok {
		t.Error("YTS was asked for an adult search")
	}
	if is.Accepted == 0 {
		t.Fatalf("nothing accepted: %+v", is.Decisions)
	}
	for _, d := range is.Decisions {
		c := d.Decision.Candidate
		// Another scene of the same studio must be rejected as the wrong scene.
		if strings.Contains(c.Title, "Other.Scene") && d.Decision.Status == "accepted" {
			t.Errorf("wrong scene accepted: %s", c.Title)
		}
	}

	// Automatic search grabs it.
	ae.expect(ae.do("PATCH", fmt.Sprintf("/api/v1/adult/items/%d", scene.ID), map[string]any{"monitored": true}), 200)
	ae.expect(ae.do("POST", fmt.Sprintf("/api/v1/adult/items/%d/search", scene.ID), nil), 202)
	waitFor(t, "the adult grab", func() bool { return ae.adultDownloads() == 1 })
}
