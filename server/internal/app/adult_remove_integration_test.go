package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	afake "github.com/mmzlopasa/locarr/server/internal/adult/meta/fake"
)

type apiAdultPage struct {
	Items []struct {
		apiAdultItem
		Status string
	}
	Total int
}

func (ae *adultEnv) items(t *testing.T, query string) apiAdultPage {
	t.Helper()
	var p apiAdultPage
	ae.expect(ae.do("GET", "/api/v1/adult/items?"+query, nil), 200).json(&p)
	return p
}

func (ae *adultEnv) sceneID(t *testing.T, title string) int64 {
	t.Helper()
	for _, it := range ae.items(t, "kind=scene").Items {
		if it.Title == title {
			return it.ID
		}
	}
	return 0
}

// TestAdultRemoveExcludeBulk: removing with "not interested" keeps a scene
// from coming back on refresh until the exclusion is undone; bulk monitor,
// unmonitor and remove; studio and performer removal with and without
// their scenes; files stay unless asked; sorted lookups; and none of it is
// reachable without view_adult or manage_library (D-110).
func TestAdultRemoveExcludeBulk(t *testing.T) {
	ae := newAdultEnv(t)
	me := ae.me()
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true, "stashdbKey": afake.StashKey,
		"tpdbKey": afake.TPDBKey, "defaultProfileId": ae.profileID}), 200)
	ae.grant(me.ID, "admin", true)
	ae.expect(ae.do("POST", "/api/v1/adult/rootfolders", map[string]any{"path": ae.adultRoot}), 201)

	// Lookups come newest first by default (asked of the source), oldest on request.
	for _, src := range []string{"stashdb", "tpdb"} {
		for _, order := range []string{"", "newest", "oldest"} {
			var l struct {
				Results []struct{ Name, Date, Image string }
				Page    int
				More    bool
			}
			q := "/api/v1/adult/lookup?source=" + src + "&type=scene&term=scene"
			if order != "" {
				q += "&sort=" + order
			}
			ae.expect(ae.do("GET", q, nil), 200).json(&l)
			if len(l.Results) < 2 || l.Page != 1 || l.More {
				t.Fatalf("%s %s: %+v", src, order, l)
			}
			for i := 1; i < len(l.Results); i++ {
				a, b := l.Results[i-1].Date, l.Results[i].Date
				if (order == "oldest" && a > b) || (order != "oldest" && a < b) {
					t.Fatalf("%s %q order: %+v", src, order, l.Results)
				}
			}
			if l.Results[0].Image == "" {
				t.Fatalf("%s: no thumbnail", src)
			}
		}
	}

	var studio struct{ ID int64 }
	ae.expect(ae.do("POST", "/api/v1/adult/studios", map[string]any{"source": "stashdb", "id": "st-studio-a", "monitored": true, "search": false}), 201).
		json(&studio)
	all := ae.items(t, "kind=scene")
	if all.Total != 3 {
		t.Fatalf("scenes %+v", all)
	}
	for _, it := range all.Items {
		if it.Status != "missing" || it.Image == "" { // older than the follow date: not monitored
			t.Fatalf("status %+v", it)
		}
	}
	refresh := func() { ae.expect(ae.do("POST", fmt.Sprintf("/api/v1/adult/studios/%d/refresh", studio.ID), nil), 200) }

	// Removed without "not interested": the refresh brings it back.
	id := ae.sceneID(t, "Second Scene")
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/items/%d", id), nil), 204)
	refresh()
	if ae.sceneID(t, "Second Scene") == 0 {
		t.Fatal("a plain remove is not remembered")
	}
	// With it: gone for good, listed in the exclusions.
	id = ae.sceneID(t, "Second Scene")
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/items/%d?exclude=true", id), nil), 204)
	refresh()
	if ae.sceneID(t, "Second Scene") != 0 {
		t.Fatal("excluded scene came back")
	}
	var ex []struct {
		ID          int64
		Title, Kind string
		StashdbId   string
		Studio      string
		ReleaseDate string
	}
	ae.expect(ae.do("GET", "/api/v1/adult/exclusions", nil), 200).json(&ex)
	if len(ex) != 1 || ex[0].Title != "Second Scene" || ex[0].StashdbId != "sc-2" || ex[0].Studio != "Studio A" || ex[0].Kind != "scene" {
		t.Fatalf("exclusions %+v", ex)
	}
	// A followed performer does not bring it back either.
	var perf struct{ ID int64 }
	ae.expect(ae.do("POST", "/api/v1/adult/performers", map[string]any{"source": "stashdb", "id": "pf-performer-two", "monitored": true}), 201).json(&perf)
	ae.expect(ae.do("POST", fmt.Sprintf("/api/v1/adult/performers/%d/refresh", perf.ID), nil), 200)
	if ae.sceneID(t, "Second Scene") != 0 {
		t.Fatal("performer refresh brought an excluded scene back")
	}
	// Undo: the next refresh adds it again.
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/exclusions/%d", ex[0].ID), nil), 204)
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/exclusions/%d", ex[0].ID), nil), 404)
	refresh()
	if ae.sceneID(t, "Second Scene") == 0 {
		t.Fatal("undone exclusion still applies")
	}
	// Adding by hand also lifts an exclusion.
	id = ae.sceneID(t, "Second Scene")
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/items/%d?exclude=true", id), nil), 204)
	ae.expect(ae.do("POST", "/api/v1/adult/items", map[string]any{"source": "stashdb", "id": "sc-2", "search": false}), 201)
	ae.expect(ae.do("GET", "/api/v1/adult/exclusions", nil), 200).json(&ex)
	if len(ex) != 0 {
		t.Fatalf("manual add kept the exclusion: %+v", ex)
	}

	// Bulk monitor / unmonitor and the status and monitored filters.
	ids := []int64{ae.sceneID(t, "Scene Title"), ae.sceneID(t, "Tagged Scene")}
	// The one added by hand is monitored already.
	wanted0 := ae.items(t, "kind=scene&status=wanted").Total
	var res struct{ Changed int }
	ae.expect(ae.do("POST", "/api/v1/adult/items/bulk", map[string]any{"ids": ids, "action": "monitor"}), 200).json(&res)
	if res.Changed != 2 {
		t.Fatalf("monitor changed %d", res.Changed)
	}
	if w := ae.items(t, "kind=scene&status=wanted"); w.Total != wanted0+2 {
		t.Fatalf("wanted %+v", w)
	}
	if m := ae.items(t, "kind=scene&monitored=false"); m.Total != ae.items(t, "kind=scene").Total-2-wanted0 {
		t.Fatalf("unmonitored %+v", m)
	}
	ae.expect(ae.do("POST", "/api/v1/adult/items/bulk", map[string]any{"ids": ids, "action": "unmonitor"}), 200).json(&res)
	if res.Changed != 2 || ae.items(t, "kind=scene&status=wanted").Total != wanted0 {
		t.Fatalf("unmonitor %d", res.Changed)
	}
	if r := ae.expect(ae.do("POST", "/api/v1/adult/items/bulk", map[string]any{"ids": ids, "action": "delete"}), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}
	// Sorting by title.
	byTitle := ae.items(t, "kind=scene&sort=title")
	for i := 1; i < len(byTitle.Items); i++ {
		if byTitle.Items[i-1].Title > byTitle.Items[i].Title {
			t.Fatalf("title order %+v", byTitle.Items)
		}
	}

	// A file stays on disk when the item is removed without "delete the file".
	file := filepath.Join(ae.adultRoot, "Studio A", "kept.mkv")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ae.deps.DB.W.Exec(`INSERT INTO adult_files (item_id, path, size, quality, added_at) VALUES (?, ?, 1, 'WEBDL-1080p', 0)`,
		ids[0], file); err != nil {
		t.Fatal(err)
	}
	if st := ae.items(t, "kind=scene&status=downloaded"); st.Total != 1 {
		t.Fatalf("downloaded %+v", st)
	}

	// Permissions: a manager without view_adult and a user with view_adult
	// but without manage_library get nothing / are refused.
	var mgr, viewer apiUser
	ae.expect(ae.do("POST", "/api/v1/users", map[string]any{"username": "mgr2", "password": "manager-pw2", "role": "manager"}), 201).json(&mgr)
	ae.expect(ae.do("POST", "/api/v1/users", map[string]any{"username": "viewer", "password": "viewer-pw12", "role": "manager"}), 201).json(&viewer)
	ae.expect(ae.do("PATCH", "/api/v1/users/"+itoa(viewer.ID), map[string]any{"permissions": map[string]bool{"requestMovies": true,
		"requestSeries": true, "requestMusic": true, "autoApprove": false, "request4k": false, "manageRequests": false,
		"manageLibrary": false, "viewAdult": true}}), 200)
	mgrTok, viewTok := ae.login("mgr2", "manager-pw2"), ae.login("viewer", "viewer-pw12")
	bulk := map[string]any{"ids": ids, "action": "remove"}
	writes := [][2]string{{"POST", "/api/v1/adult/items/bulk"}, {"DELETE", fmt.Sprintf("/api/v1/adult/items/%d?exclude=true", ids[0])},
		{"DELETE", "/api/v1/adult/exclusions/1"}, {"DELETE", fmt.Sprintf("/api/v1/adult/studios/%d?keepScenes=true", studio.ID)},
		{"DELETE", fmt.Sprintf("/api/v1/adult/performers/%d", perf.ID)}, {"POST", fmt.Sprintf("/api/v1/adult/performers/%d/refresh", perf.ID)}}
	for _, w := range writes {
		if r := ae.as(mgrTok, w[0], w[1], bulk); r.StatusCode != 404 {
			t.Fatalf("%s %s without view_adult: %d", w[0], w[1], r.StatusCode)
		}
		if r := ae.as(viewTok, w[0], w[1], bulk); r.StatusCode != 403 {
			t.Fatalf("%s %s without manage_library: %d", w[0], w[1], r.StatusCode)
		}
	}
	if r := ae.as(mgrTok, "GET", "/api/v1/adult/exclusions", nil); r.StatusCode != 404 {
		t.Fatalf("exclusions without view_adult: %d", r.StatusCode)
	}
	ae.expect(ae.as(viewTok, "GET", "/api/v1/adult/exclusions", nil), 200)

	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/items/%d", ids[0]), nil), 204)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the file went without deleteFiles: %v", err)
	}

	// Bulk remove with "not interested".
	before := ae.items(t, "kind=scene").Total
	ae.expect(ae.do("POST", "/api/v1/adult/items/bulk", map[string]any{"ids": []int64{ids[1], 999999}, "action": "remove", "exclude": true}), 200).json(&res)
	if res.Changed != 1 || ae.items(t, "kind=scene").Total != before-1 {
		t.Fatalf("bulk remove %d", res.Changed)
	}
	ae.expect(ae.do("GET", "/api/v1/adult/exclusions", nil), 200).json(&ex)
	if len(ex) != 1 || ex[0].Title != "Tagged Scene" {
		t.Fatalf("exclusions %+v", ex)
	}

	// Performer removed with their scenes; the studio removed keeping its scenes.
	withPerf := ae.items(t, fmt.Sprintf("performerId=%d", perf.ID)).Total
	if withPerf == 0 {
		t.Fatal("performer has no scenes")
	}
	before = ae.items(t, "kind=scene").Total
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/performers/%d?removeScenes=true", perf.ID), nil), 204)
	if got := ae.items(t, "kind=scene").Total; got != before-withPerf {
		t.Fatalf("performer scenes: %d -> %d (%d)", before, got, withPerf)
	}
	refresh() // the scenes removed without "not interested" come back
	before = ae.items(t, "kind=scene").Total
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/studios/%d?keepScenes=true", studio.ID), nil), 204)
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/studios/%d", studio.ID), nil), 404)
	after := ae.items(t, "kind=scene")
	if after.Total != before || before == 0 {
		t.Fatalf("keepScenes: %d -> %d", before, after.Total)
	}
	for _, it := range after.Items {
		if it.Studio != "Studio A" {
			t.Fatalf("studio name lost: %+v", it)
		}
	}
}
