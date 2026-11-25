package app

import (
	"fmt"
	"slices"
	"testing"

	afake "github.com/mmzlopasa/locarr/server/internal/adult/meta/fake"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

type apiAdultSettings struct {
	DefaultProfileID int64    `json:"defaultProfileId"`
	OpenProfileID    *int64   `json:"openProfileId"`
	ProtectedTerms   []string `json:"protectedTerms"`
	ProfileUsage     *struct {
		Items, Studios, Performers int
	} `json:"profileUsage"`
}

// TestAdultOpenProfileAndBlocklist: new adult items get the profile that
// allows every quality (migration 00018), existing items can be switched in
// one step, the owner's blocklist starts empty and stays as the owner left
// it, and the protected terms always block (D-109).
func TestAdultOpenProfileAndBlocklist(t *testing.T) {
	ae := newAdultEnv(t)
	me := ae.me()
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true, "stashdbKey": afake.StashKey}), 200)
	ae.grant(me.ID, "admin", true)
	ae.expect(ae.do("POST", "/api/v1/adult/rootfolders", map[string]any{"path": ae.adultRoot}), 201)

	var st apiAdultSettings
	ae.expect(ae.do("GET", "/api/v1/adult/settings", nil), 200).json(&st)
	if st.OpenProfileID == nil || st.DefaultProfileID != *st.OpenProfileID {
		t.Fatalf("settings %+v: the open profile must be the default", st)
	}
	if !slices.Contains(st.ProtectedTerms, "child") || slices.Contains(st.ProtectedTerms, "teen") || slices.Contains(st.ProtectedTerms, "snuff") {
		t.Fatalf("protected terms %v", st.ProtectedTerms)
	}
	openID := *st.OpenProfileID
	var prof struct {
		Name  string
		Items []struct {
			Quality string
			Allowed bool
		}
	}
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/qualityprofiles/%d", openID), nil), 200).json(&prof)
	want := quality.OpenProfile()
	if prof.Name != quality.OpenProfileName || len(prof.Items) != len(want.Items) {
		t.Fatalf("stored profile %+v", prof)
	}
	for i, it := range prof.Items {
		if it.Quality != want.Items[i].Quality || !it.Allowed {
			t.Fatalf("item %d: %+v, want %s allowed", i, it, want.Items[i].Quality)
		}
	}

	// A new item gets the open profile.
	var it apiAdultItem
	ae.expect(ae.do("POST", "/api/v1/adult/items", map[string]any{"source": "stashdb", "id": "sc-2", "search": false}), 201).json(&it)
	profileOf := func() (id int64) {
		_ = ae.deps.DB.R.QueryRow(`SELECT quality_profile_id FROM adult_items WHERE id = ?`, it.ID).Scan(&id)
		return id
	}
	if got := profileOf(); got != openID {
		t.Fatalf("new item profile %d, want %d", got, openID)
	}
	// An item on another profile shows in the usage and is switched in one step.
	ae.expect(ae.do("PATCH", fmt.Sprintf("/api/v1/adult/items/%d", it.ID), map[string]any{"qualityProfileId": ae.profileID}), 200)
	ae.expect(ae.do("GET", "/api/v1/adult/settings", nil), 200).json(&st)
	if st.ProfileUsage == nil || st.ProfileUsage.Items != 1 {
		t.Fatalf("usage %+v", st.ProfileUsage)
	}
	var upd struct{ Updated int64 }
	ae.expect(ae.do("POST", "/api/v1/adult/items/quality-profile", map[string]any{"profileId": openID}), 200).json(&upd)
	if upd.Updated != 1 || profileOf() != openID {
		t.Fatalf("switched %d, profile %d", upd.Updated, profileOf())
	}
	// Usage is adult data: an administrator without view_adult does not get it.
	ae.grant(me.ID, "admin", false)
	st = apiAdultSettings{}
	ae.expect(ae.do("GET", "/api/v1/adult/settings", nil), 200).json(&st)
	if st.ProfileUsage != nil {
		t.Fatalf("usage without view_adult: %+v", st.ProfileUsage)
	}
	if r := ae.do("POST", "/api/v1/adult/items/quality-profile", map[string]any{"profileId": openID}); r.StatusCode != 404 {
		t.Fatalf("switch without view_adult: %d", r.StatusCode)
	}
	ae.grant(me.ID, "admin", true)

	// Blocklist: empty, the owner adds and removes terms, protected ones
	// cannot be added and always apply.
	type term struct{ ID int64 }
	list := func() []term {
		var out []term
		ae.expect(ae.do("GET", "/api/v1/adult/blocklist", nil), 200).json(&out)
		return out
	}
	if l := list(); len(l) != 0 {
		t.Fatalf("seeded: %+v", l)
	}
	for _, s := range []string{"ownterm", "other term"} {
		ae.expect(ae.do("POST", "/api/v1/adult/blocklist", map[string]any{"term": s}), 201)
	}
	if r := ae.expect(ae.do("POST", "/api/v1/adult/blocklist", map[string]any{"term": "Child"}), 409); r.code() != "protected_term" {
		t.Fatal(r.code())
	}
	for _, x := range list() {
		ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/blocklist/%d", x.ID), nil), 204)
	}
	// Turning the feature off and on, and the startup steps, bring nothing back.
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": false}), 200)
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true}), 200)
	if err := ae.deps.DB.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := ae.deps.Quality.Seed(t.Context()); err != nil {
		t.Fatal(err)
	}
	if l := list(); len(l) != 0 {
		t.Fatalf("terms came back: %+v", l)
	}
	if term, ok := ae.deps.Adult.BlockedText(t.Context(), "StudioA.24.01.15.Pre.Teen.Title.1080p"); !ok || (term != "pre teen" && term != "preteen") {
		t.Fatalf("protected term not applied: %q %v", term, ok)
	}
	if _, ok := ae.deps.Adult.BlockedText(t.Context(), "StudioA.24.01.15.Ownterm.Title.1080p"); ok {
		t.Fatal("a removed term still blocks")
	}
}

// TestItemTimelineClear: deleting single rows, a group and a whole item's
// timeline through the history API (D-109), for managers only, and never an
// adult row for anyone without view_adult (D-103).
func TestItemTimelineClear(t *testing.T) {
	ae := newAdultEnv(t)
	db := ae.deps.DB.W
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	movie := exec(`INSERT INTO media_items (kind, title, sort_title, year, quality_profile_id, path, added_at, updated_at)
		VALUES ('movie', 'Sintel', 'sintel', 2010, ?, '/m/sintel', 1, 1)`, ae.profileID)
	exec(`INSERT INTO movies (item_id, tmdb_id) VALUES (?, 45745)`, movie)
	other := exec(`INSERT INTO media_items (kind, title, sort_title, year, quality_profile_id, path, added_at, updated_at)
		VALUES ('movie', 'Other', 'other', 2011, ?, '/m/other', 1, 1)`, ae.profileID)
	exec(`INSERT INTO movies (item_id, tmdb_id) VALUES (?, 1)`, other)
	ref, otherRef := fmt.Sprintf("movie:%d", movie), fmt.Sprintf("movie:%d", other)
	search := func(r string, at int) int64 {
		return exec(`INSERT INTO search_log (started_at, duration_ms, source, search_type, query, params, item_ref, result_count, error_count)
			VALUES (?, 10, 'automatic', 'movie', 'q', '{}', ?, 2, 0)`, at, r)
	}
	event := func(r, typ string, at int) int64 {
		return exec(`INSERT INTO timeline_events (item_ref, at, type, message) VALUES (?, ?, ?, 'x')`, r, at, typ)
	}
	s1, s2 := search(ref, 1000), search(ref, 1100)
	d1 := event(ref, "decision", 1200)
	g1 := event(ref, "grab_failed", 1300)
	event(ref, "added", 900)
	otherRow := event(otherRef, "decision", 1000)
	count := func(body map[string]any) int64 {
		var r struct{ Count int64 }
		ae.expect(ae.do("POST", "/api/v1/history/clear", body), 200).json(&r)
		return r.Count
	}
	if n := count(map[string]any{"item": ref, "dryRun": true}); n != 5 {
		t.Fatalf("dry run %d, want 5", n)
	}
	// One group: two searches and their decision.
	if n := count(map[string]any{"item": ref, "ids": []string{fmt.Sprintf("s:%d", s1), fmt.Sprintf("s:%d", s2), fmt.Sprintf("t:%d", d1)}}); n != 3 {
		t.Fatalf("group deleted %d, want 3", n)
	}
	// One row; then it is gone, and another item's row is not reachable this way.
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/history/t:%d?item=%s", g1, ref), nil), 204)
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/history/t:%d?item=%s", g1, ref), nil), 404)
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/history/t:%d?item=%s", otherRow, ref), nil), 404)
	// Requesters may not.
	var rq apiUser
	ae.expect(ae.do("POST", "/api/v1/users", map[string]any{"username": "kid", "password": "requester-pw", "role": "requester"}), 201).json(&rq)
	rqTok := ae.login("kid", "requester-pw")
	if r := ae.as(rqTok, "POST", "/api/v1/history/clear", map[string]any{"item": ref}); r.StatusCode != 403 {
		t.Fatalf("requester clear: %d", r.StatusCode)
	}
	if n := count(map[string]any{"item": ref}); n != 1 {
		t.Fatalf("clear %d, want 1", n)
	}
	var left int
	_ = ae.deps.DB.R.QueryRow(`SELECT (SELECT count(*) FROM timeline_events WHERE item_ref = ?) + (SELECT count(*) FROM search_log WHERE item_ref = ?)`,
		ref, ref).Scan(&left)
	if left != 0 {
		t.Fatalf("%d rows left", left)
	}
	_ = ae.deps.DB.R.QueryRow(`SELECT count(*) FROM timeline_events WHERE id = ?`, otherRow).Scan(&left)
	if left != 1 {
		t.Fatal("another item's row was deleted")
	}

	// Adult rows: a manager without view_adult gets the same 404 as for
	// nothing, with or without the feature on, and nothing is deleted.
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true, "stashdbKey": afake.StashKey}), 200)
	var mgr apiUser
	ae.expect(ae.do("POST", "/api/v1/users", map[string]any{"username": "mgr", "password": "manager-pw1", "role": "manager"}), 201).json(&mgr)
	mgrTok := ae.login("mgr", "manager-pw1")
	sceneRef := "scene:1"
	sceneRow := event(sceneRef, "grab_failed", 2000)
	sceneSearch := search(sceneRef, 1990)
	tries := func(tok string) []int {
		return []int{
			ae.as(tok, "DELETE", fmt.Sprintf("/api/v1/history/t:%d?item=%s", sceneRow, sceneRef), nil).StatusCode,
			ae.as(tok, "DELETE", fmt.Sprintf("/api/v1/history/t:%d", sceneRow), nil).StatusCode,
			ae.as(tok, "POST", "/api/v1/history/clear", map[string]any{"item": sceneRef, "dryRun": true}).StatusCode,
			ae.as(tok, "POST", "/api/v1/history/clear", map[string]any{"item": sceneRef}).StatusCode,
		}
	}
	for _, code := range tries(mgrTok) {
		if code != 404 {
			t.Fatalf("manager without view_adult: %v", tries(mgrTok))
		}
	}
	// The administrator without view_adult neither.
	for _, code := range tries(ae.token) {
		if code != 404 {
			t.Fatalf("admin without view_adult: %v", tries(ae.token))
		}
	}
	// The library-wide clear never counts adult rows (the other movie's row
	// is a decision, which the library-wide history does not show).
	if n := count(map[string]any{"dryRun": true}); n != 0 {
		t.Fatalf("library-wide count %d, want 0", n)
	}
	_ = ae.deps.DB.R.QueryRow(`SELECT (SELECT count(*) FROM timeline_events WHERE id = ?) + (SELECT count(*) FROM search_log WHERE id = ?)`,
		sceneRow, sceneSearch).Scan(&left)
	if left != 2 {
		t.Fatalf("adult rows deleted without the permission (%d left)", left)
	}
	// With view_adult the manager may.
	ae.grant(mgr.ID, "manager", true)
	var r struct{ Count int64 }
	ae.expect(ae.as(mgrTok, "POST", "/api/v1/history/clear", map[string]any{"item": sceneRef}), 200).json(&r)
	if r.Count != 2 {
		t.Fatalf("permitted clear %d, want 2", r.Count)
	}
	// Feature off: 404 again even with the permission.
	event(sceneRef, "decision", 3000)
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": false}), 200)
	if code := ae.as(mgrTok, "POST", "/api/v1/history/clear", map[string]any{"item": sceneRef}).StatusCode; code != 404 {
		t.Fatalf("feature off: %d", code)
	}
}
