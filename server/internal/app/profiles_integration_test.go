package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type apiProfile struct {
	ID    int64
	Name  string
	Kind  string
	Items []struct {
		Quality   string   `json:"quality,omitempty"`
		Name      string   `json:"name,omitempty"`
		Qualities []string `json:"qualities,omitempty"`
		Allowed   bool     `json:"allowed"`
	}
	Cutoff          string
	UpgradesAllowed bool
	FormatScores    []map[string]any
	Languages       []string
}

func TestQualityProfilesAPI(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()

	var quals []map[string]any
	e.expect(e.do("GET", "/api/v1/qualities", nil), 200).json(&quals)
	if len(quals) < 40 {
		t.Fatalf("%d qualities", len(quals))
	}
	var profiles []apiProfile
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	if len(profiles) != 12 { // the defaults and the open profile of migration 00018
		t.Fatalf("%d seeded profiles", len(profiles))
	}
	var hd apiProfile
	for _, p := range profiles {
		if p.Kind == "movies" && p.Name == "HD-1080p" {
			hd = p
		}
	}
	if hd.ID == 0 || hd.Cutoff != "group:WEB 1080p" || len(hd.Items) < 30 {
		t.Fatalf("HD-1080p: %+v", hd)
	}

	body := map[string]any{"name": "Remux", "kind": "movies", "cutoff": "remux-2160p", "upgradesAllowed": true,
		"items":     []map[string]any{{"quality": "remux-2160p", "allowed": true}, {"quality": "remux-1080p", "allowed": true}},
		"languages": []string{"original", "hr"}}
	var created apiProfile
	e.expect(e.do("POST", "/api/v1/qualityprofiles", body), 201).json(&created)
	if created.ID == 0 || len(created.Items) < 30 || created.Items[0].Quality != "remux-2160p" {
		t.Fatalf("%+v", created)
	}
	if r := e.expect(e.do("POST", "/api/v1/qualityprofiles", body), 409); r.code() != "name_taken" {
		t.Fatal(r.code())
	}
	body["cutoff"] = "cam"
	body["name"] = "Bad"
	if r := e.expect(e.do("POST", "/api/v1/qualityprofiles", body), 400); r.code() != "validation" || !strings.Contains(string(r.body), "cutoff") {
		t.Fatal(string(r.body))
	}
	body["cutoff"] = "remux-1080p"
	body["name"] = "Remux 1080"
	e.expect(e.do("PUT", "/api/v1/qualityprofiles/"+itoa(created.ID), body), 200)
	e.expect(e.do("GET", "/api/v1/qualityprofiles/"+itoa(created.ID), nil), 200).json(&created)
	if created.Name != "Remux 1080" || created.Cutoff != "remux-1080p" {
		t.Fatalf("%+v", created)
	}
	e.expect(e.do("DELETE", "/api/v1/qualityprofiles/"+itoa(created.ID), nil), 204)
	e.expect(e.do("GET", "/api/v1/qualityprofiles/"+itoa(created.ID), nil), 404)

	// definitions
	var defs []map[string]any
	e.expect(e.do("GET", "/api/v1/qualitydefinitions", nil), 200).json(&defs)
	e.expect(e.do("PUT", "/api/v1/qualitydefinitions", map[string]any{"definitions": []map[string]any{
		{"kind": "movies", "quality": "bluray-1080p", "min": 5, "preferred": 10, "max": 20}}}), 200)
	if r := e.expect(e.do("PUT", "/api/v1/qualitydefinitions", map[string]any{"definitions": []map[string]any{
		{"kind": "movies", "quality": "bluray-1080p", "min": 50, "preferred": 10, "max": 20}}}), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}
	e.expect(e.do("POST", "/api/v1/qualitydefinitions/reset", map[string]any{"kind": "movies"}), 200)

	// a requester may read but not change
	e.expect(e.do("POST", "/api/v1/users", map[string]any{"username": "viewer", "password": "viewer-password", "role": "requester"}), 201)
	admin := e.token
	e.token = e.login("viewer", "viewer-password")
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200)
	e.expect(e.do("DELETE", "/api/v1/qualityprofiles/"+itoa(hd.ID), nil), 403)
	e.expect(e.do("PUT", "/api/v1/qualitydefinitions", map[string]any{"definitions": []any{}}), 403)
	e.token = admin
}

func TestCustomFormatsAndParseAPI(t *testing.T) {
	e := newEnv(t, nil)
	e.setup()
	shared, err := os.ReadFile("../customformats/testdata/shared-formats.json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Formats []struct {
			ID          int64
			Name        string
			Unsupported []string
		}
		Warnings []string
		Stored   bool
	}
	e.expect(e.do("POST", "/api/v1/customformats/import", map[string]any{"json": string(shared), "dryRun": true}), 200).json(&res)
	if res.Stored || len(res.Formats) != 8 || len(res.Warnings) != 1 {
		t.Fatalf("dry run %+v", res)
	}
	var list []map[string]any
	e.expect(e.do("GET", "/api/v1/customformats", nil), 200).json(&list)
	if len(list) != 0 {
		t.Fatal("dry run stored formats")
	}
	e.expect(e.do("POST", "/api/v1/customformats/import", map[string]any{"json": string(shared)}), 200).json(&res)
	if !res.Stored || len(res.Formats) != 8 {
		t.Fatalf("%+v", res)
	}
	if r := e.expect(e.do("POST", "/api/v1/customformats/import", map[string]any{"json": string(shared)}), 409); r.code() != "name_taken" {
		t.Fatal(r.code())
	}
	e.expect(e.do("POST", "/api/v1/customformats/import", map[string]any{"json": string(shared), "replace": true}), 200)
	if r := e.expect(e.do("POST", "/api/v1/customformats/import", map[string]any{"json": "{nope"}), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}
	ids := map[string]int64{}
	for _, f := range res.Formats {
		ids[f.Name] = f.ID
	}
	var exported map[string]any
	e.expect(e.do("GET", "/api/v1/customformats/"+itoa(ids["DV (WEBDL)"])+"/export", nil), 200).json(&exported)
	if exported["name"] != "DV (WEBDL)" || !strings.Contains(mustJSON(exported), `"value":7`) {
		t.Fatalf("export %v", exported)
	}
	var schema struct{ Implementations []map[string]any }
	e.expect(e.do("GET", "/api/v1/customformats/schema", nil), 200).json(&schema)
	if len(schema.Implementations) < 10 {
		t.Fatal(schema)
	}
	// own format through the condition builder
	var own map[string]any
	e.expect(e.do("POST", "/api/v1/customformats", map[string]any{"name": "Atmos", "specifications": []map[string]any{
		{"name": "atmos", "implementation": "AudioCodecSpecification", "required": true, "fields": map[string]any{"value": "atmos"}}}}), 201).json(&own)
	if r := e.expect(e.do("POST", "/api/v1/customformats", map[string]any{"name": "Broken", "specifications": []map[string]any{
		{"implementation": "ReleaseTitleSpecification", "fields": map[string]any{"value": "("}}}}), 400); r.code() != "validation" {
		t.Fatal(r.code())
	}

	// score the formats in HD-1080p and parse
	var profiles []apiProfile
	e.expect(e.do("GET", "/api/v1/qualityprofiles", nil), 200).json(&profiles)
	var hd apiProfile
	for _, p := range profiles {
		if p.Kind == "movies" && p.Name == "HD-1080p" {
			hd = p
		}
	}
	upd := map[string]any{"name": hd.Name, "kind": hd.Kind, "items": hd.Items, "cutoff": hd.Cutoff, "upgradesAllowed": true,
		"formatScores":   []map[string]any{{"formatId": ids["x265 (HD)"], "score": -10000}, {"formatId": int64(own["id"].(float64)), "score": 25}},
		"minFormatScore": 0}
	e.expect(e.do("PUT", "/api/v1/qualityprofiles/"+itoa(hd.ID), upd), 200)

	var pr struct {
		Release struct {
			Title string
			Year  int
			Video struct{ Source string }
		}
		Quality       struct{ ID string }
		Score         int
		CustomFormats []struct {
			Name    string
			Matched bool
			Score   int
		}
		Decision *struct {
			Status     string
			Rejections []struct{ Code, Message string }
		}
	}
	e.expect(e.do("POST", "/api/v1/parse", map[string]any{"title": "Dune.Part.Two.2024.1080p.BluRay.TrueHD.7.1.Atmos.x265-GRP"}), 200).json(&pr)
	if pr.Release.Title != "Dune Part Two" || pr.Release.Year != 2024 || pr.Quality.ID != "bluray-1080p" || pr.Decision != nil {
		t.Fatalf("%+v", pr)
	}
	matched := 0
	for _, f := range pr.CustomFormats {
		if f.Matched {
			matched++
		}
	}
	if matched != 2 {
		t.Fatalf("matched %+v", pr.CustomFormats)
	}
	e.expect(e.do("POST", "/api/v1/parse", map[string]any{"title": "Dune.Part.Two.2024.1080p.BluRay.TrueHD.7.1.Atmos.x265-GRP",
		"profileId": hd.ID, "size": 30 << 30, "wanted": map[string]any{"title": "Dune: Part Two", "year": 2024, "runtimeMinutes": 166}}), 200).json(&pr)
	if pr.Score != -9975 || pr.Decision == nil || pr.Decision.Status != "rejected" {
		t.Fatalf("%+v", pr)
	}
	codes := []string{}
	for _, r := range pr.Decision.Rejections {
		codes = append(codes, r.Code)
	}
	if strings.Join(codes, ",") != "custom_format_score_too_low,size_too_large" {
		t.Fatalf("codes %v", pr.Decision.Rejections)
	}

	// release, delay profiles and the evaluate endpoint
	var rp map[string]any
	e.expect(e.do("POST", "/api/v1/releaseprofiles", map[string]any{"name": "No CAM words", "enabled": true, "required": []string{},
		"ignored": []string{"HDCAM", "/\\bTS\\b/"}}), 201).json(&rp)
	e.expect(e.do("POST", "/api/v1/releaseprofiles", map[string]any{"name": "Empty", "enabled": true, "required": []string{}, "ignored": []string{}}), 400)
	var delays []map[string]any
	e.expect(e.do("GET", "/api/v1/delayprofiles", nil), 200).json(&delays)
	if len(delays) != 1 || delays[0]["default"] != true {
		t.Fatal(delays)
	}
	if r := e.expect(e.do("DELETE", "/api/v1/delayprofiles/"+itoa(int64(delays[0]["id"].(float64))), nil), 409); r.code() != "default_profile" {
		t.Fatal(r.code())
	}
	def := delays[0]
	def["torrentDelay"] = 60
	e.expect(e.do("PUT", "/api/v1/delayprofiles/"+itoa(int64(def["id"].(float64))), def), 200)
	var a, b map[string]any
	e.expect(e.do("POST", "/api/v1/delayprofiles", map[string]any{"name": "Anime", "enabled": true, "preferredProtocol": "torrent",
		"usenetEnabled": false, "torrentEnabled": true, "usenetDelay": 0, "torrentDelay": 0, "tags": []string{"anime"}}), 201).json(&a)
	e.expect(e.do("POST", "/api/v1/delayprofiles", map[string]any{"name": "4K", "enabled": true, "preferredProtocol": "usenet",
		"usenetEnabled": true, "torrentEnabled": true, "usenetDelay": 0, "torrentDelay": 0, "tags": []string{"4k"}}), 201).json(&b)
	e.expect(e.do("PUT", "/api/v1/delayprofiles/order", map[string]any{"ids": []int64{int64(b["id"].(float64)), int64(a["id"].(float64))}}), 200).json(&delays)
	if delays[0]["name"] != "4K" || delays[2]["default"] != true {
		t.Fatal(delays)
	}

	var rep struct {
		Accepted, Pending, Rejected int
		BestGUID                    string `json:"bestGuid"`
		Decisions                   []struct {
			Status     string
			Rank       int
			Candidate  struct{ GUID string }
			Rejections []struct{ Code string }
		}
	}
	e.expect(e.do("POST", "/api/v1/decisions/evaluate", map[string]any{"kind": "movies", "profileId": hd.ID,
		"wanted": map[string]any{"title": "Dune Part Two", "year": 2024, "runtimeMinutes": 166},
		"candidates": []map[string]any{
			{"guid": "a", "title": "Dune.Part.Two.2024.1080p.WEB-DL.DDP5.1.H.264-GRP", "size": 6 << 30, "protocol": "usenet", "publishDate": "2024-05-01T00:00:00Z"},
			{"guid": "b", "title": "Dune.Part.Two.2024.1080p.BluRay.x264-GRP", "size": 12 << 30, "protocol": "torrent", "seeders": 40, "publishDate": "2024-05-01T00:00:00Z"},
			{"guid": "c", "title": "Dune.Part.Two.2024.HDCAM.x264-GRP", "size": 2 << 30, "protocol": "torrent", "seeders": 400},
			{"guid": "d", "title": "Dune.Part.Two.2024.1080p.BluRay.x264-NEW", "size": 12 << 30, "protocol": "torrent", "seeders": 5,
				"publishDate": "2099-01-01T00:00:00Z"},
		}}), 200).json(&rep)
	if rep.Accepted != 2 || rep.Pending != 1 || rep.Rejected != 1 || rep.BestGUID != "b" || rep.Decisions[0].Rank != 1 {
		t.Fatalf("%+v", rep)
	}
	e.expect(e.do("POST", "/api/v1/decisions/evaluate", map[string]any{"kind": "series", "profileId": hd.ID, "candidates": []any{}}), 400)

	var langs []map[string]any
	e.expect(e.do("GET", "/api/v1/languages", nil), 200).json(&langs)
	if len(langs) < 50 {
		t.Fatal(len(langs))
	}
	e.expect(e.do("DELETE", "/api/v1/customformats/"+itoa(ids["Big"]), nil), 204)
	e.expect(e.do("DELETE", "/api/v1/customformats/"+itoa(ids["Big"]), nil), 404)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
