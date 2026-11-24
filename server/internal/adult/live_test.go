//go:build live

package adult

// The live adult search check asks the real built-in sites that carry adult
// content for a few scenes, with the same search texts and scene matching as
// an automatic search (D-108). It is not part of CI. The scenes are not kept
// in the repository; give them in a JSON file:
//
//	[{"studio": "…", "date": "2020-08-19", "title": "…", "performers": ["…"]}]
//
//	LOCARR_LIVE_SCENES=/path/scenes.json go test -tags live -run TestLiveAdult -v ./internal/adult/
//
// LOCARR_LIVE_BROWSER and LOCARR_LIVE_BROWSER_TOKEN let sites behind a
// browser check go through the companion browser, as in the indexers' live
// test.

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/indexers/native"
	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

type liveScene struct {
	Studio     string   `json:"studio"`
	Date       string   `json:"date"`
	Title      string   `json:"title"`
	Performers []string `json:"performers"`
}

func TestLiveAdult(t *testing.T) {
	path := os.Getenv("LOCARR_LIVE_SCENES")
	if path == "" {
		t.Skip("set LOCARR_LIVE_SCENES to a JSON file of scenes")
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- a file named by the person running the test
	if err != nil {
		t.Fatal(err)
	}
	var scenes []liveScene
	if err := json.Unmarshal(raw, &scenes); err != nil {
		t.Fatal(err)
	}
	defs, err := native.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	var br *httpx.Browser
	if u := os.Getenv("LOCARR_LIVE_BROWSER"); u != "" {
		br = &httpx.Browser{ControlPolicy: httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"),
			netip.MustParsePrefix("172.16.0.0/12")}},
			Config: func(context.Context) (httpx.BrowserConfig, error) {
				return httpx.BrowserConfig{URL: u, Token: os.Getenv("LOCARR_LIVE_BROWSER_TOKEN")}, nil
			}}
	}
	var profile quality.Profile
	for _, p := range quality.DefaultProfiles() {
		if p.Kind == quality.Movies && p.Name == "Any" {
			profile, profile.ID = p, 1
		}
	}
	clients := map[string]*native.Client{}
	for _, d := range defs {
		if !d.HasAdult() {
			continue
		}
		timeout := 30 * time.Second
		if d.TimeoutSeconds > 0 {
			timeout = time.Duration(d.TimeoutSeconds) * time.Second
		}
		hc, err := httpx.New(httpx.Options{Policy: httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}},
			Timeout: timeout, Browser: br, Challenge: d.Challenge})
		if err != nil {
			t.Fatal(err)
		}
		clients[d.ID] = &native.Client{Def: d, HTTP: hc, Trackers: native.DefaultTrackers}
	}
	for _, sc := range scenes {
		terms := searchTerms(true, sc.Studio, sc.Date, sc.Title, 0, sc.Performers)
		dctx := decision.Context{Profile: profile, Definitions: quality.DefaultDefinitions(), Mode: decision.ModeInteractive, Now: time.Now(),
			Wanted: decision.Wanted{Kind: quality.Movies, Title: sc.Title, RuntimeMinutes: 30, Adult: &decision.Adult{Scene: true,
				Studios: []string{sc.Studio}, Date: sc.Date, Title: sc.Title, Performers: sc.Performers}}}
		t.Logf("scene %s %s: terms %q", sc.Studio, sc.Date, terms)
		for _, d := range defs {
			c := clients[d.ID]
			if c == nil {
				continue
			}
			results, matches, first, note := 0, 0, "", ""
			for i, term := range terms {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				page, err := c.Search(ctx, znab.Request{Type: znab.SearchGeneric, Query: term, Categories: []int{6000}})
				cancel()
				if err != nil {
					note = string(znab.KindOf(err)) + ": " + err.Error()
					break
				}
				for _, it := range page.Items {
					// As the indexer service: an adult search takes adult or uncategorised releases.
					if len(it.Categories) > 0 && !slices.ContainsFunc(it.Categories, native.IsAdultCategory) {
						continue
					}
					results++
					size := it.Size
					if size == 0 {
						size = 500 << 20
					}
					dec := decision.EvaluateOne(context.Background(), &dctx, decision.Candidate{GUID: it.GUID, Title: it.Title, Size: size,
						Protocol: decision.Torrent, IndexerID: 1, IndexerName: d.Name, PublishDate: it.PublishDate, Seeders: it.Seeders})
					if !slices.ContainsFunc(dec.Rejections, func(r decision.Rejection) bool {
						return r.Code == decision.CodeWrongScene || r.Code == decision.CodeWrongYear
					}) {
						matches++
						if first == "" {
							first = it.Title
						}
					}
				}
				if matches > 0 {
					note = "found with term " + strconv.Itoa(i+1)
					break
				}
			}
			t.Logf("  %-14s results %3d, right scene %2d %s; first: %q", d.ID, results, matches, note, first)
		}
	}
}

