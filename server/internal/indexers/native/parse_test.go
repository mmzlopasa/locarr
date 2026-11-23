package native

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/znab"
)

func sz(f float64, shift int) int64 { return int64(math.Round(f * float64(int64(1)<<shift))) }

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{
		"6.6 GiB": sz(6.6, 30), "848.96 MB": sz(848.96, 20), "1,234 KB": 1234 << 10, "890199081": 890199081,
		"10.99 MB": sz(10.99, 20), "1.1 GB": sz(1.1, 30), "33.66MB": sz(33.66, 20),
		"2,5 GB": sz(2.5, 30), "": 0, "n/a": 0,
	} {
		if got := ParseSize(in); got != want {
			t.Errorf("%q: %d, want %d", in, got, want)
		}
	}
}

func TestParseDate(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"Sat, 12 Sep 2026 15:45:00 -0000": time.Date(2026, 9, 12, 15, 45, 0, 0, time.UTC),
		"14 Nov 2026 11:40:20 +0200":      time.Date(2026, 11, 14, 9, 40, 20, 0, time.UTC),
		"1446344423":                      time.Unix(1446344423, 0).UTC(),
		"2026-11-23T11:59:13+00:00":       time.Date(2026, 11, 23, 11, 59, 13, 0, time.UTC),
		"9 hours ago":                     now.Add(-9 * time.Hour),
		"1 Year+":                         now.Add(-365 * 24 * time.Hour),
		"Yesterday":                       now.Add(-24 * time.Hour),
		"Nov. 27th '25":                   time.Date(2025, 11, 27, 0, 0, 0, 0, time.UTC),
		"9am Nov. 14th":                   time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC),
		"Dec. 30th":                       time.Date(2025, 12, 30, 0, 0, 0, 0, time.UTC),
		"10:12pm":                         time.Date(2026, 11, 23, 22, 12, 0, 0, time.UTC),
		"Fri, 29 May 2026 17:05:35 +0000": time.Date(2026, 5, 29, 17, 5, 35, 0, time.UTC),
	} {
		if got := ParseDate(in, nil, now); !got.Equal(want) {
			t.Errorf("%q: %s, want %s", in, got, want)
		}
	}
	if got := ParseDate("4/18/2019", []string{"1/2/2006"}, now); got.Year() != 2019 || got.Day() != 18 {
		t.Errorf("layout: %s", got)
	}
	if !ParseDate("whenever", nil, now).IsZero() {
		t.Error("junk parsed")
	}
}

func TestHashesAndMagnets(t *testing.T) {
	const hex = "a017ac9bf02de9e36f1f9177bdb60612186b0b0d"
	for in, want := range map[string]string{
		"A017AC9BF02DE9E36F1F9177BDB60612186B0B0D":                                 hex,
		"magnet:?xt=urn:btih:" + hex + "&dn=x":                                     hex,
		"http://itorrents.net/torrent/" + strings.ToUpper(hex) + ".torrent?title=": hex,
		"magnet:?xt=urn:btih:VLJ3XGIWPFSZPFC3WRVIL2UCEQFHAW2B&dn=x":                "aad3bb9916796597945bb46a85ea82240a705b41",
		"": "", "not a hash": "",
	} {
		if got := NormalizeHash(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	m := Magnet(hex, "Big Buck Bunny", []string{"udp://a:1/announce", " "})
	if m != "magnet:?xt=urn:btih:"+hex+"&dn=Big+Buck+Bunny&tr=udp%3A%2F%2Fa%3A1%2Fannounce" {
		t.Fatal(m)
	}
	for in, want := range map[string]string{"tt0111161": "tt0111161", "111161": "tt0111161", "0944947": "tt0944947", "": "", "tt0": ""} {
		if got := ParseIMDB(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestTemplatesAndRequests(t *testing.T) {
	def, err := Parse([]byte(`id: tt
name: T
version: 1
links: [https://t.example/base]
modes: {search: [q, imdbid], tv: [q, season, ep]}
categories:
  - {id: "m", cat: 2000, desc: Movies}
  - {id: "hd", cat: 2040, desc: HD}
  - {id: "tv", cat: 5000, desc: TV}
  - {id: "an", cat: 5070, desc: Anime}
search:
  requests:
    - if: [imdbid]
      path: '/imdb/{{ .IMDBID }}'
    - if: [keywords]
      path: '/s/{{ .Keywords | dashed | pathescape }}?c={{ .Categories | join "," }}&p={{ .Page }}'
  response: json
  rows: r
  fields:
    title: {select: t}
    details: {select: d}
test: {query: x}
`))
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Def: def}
	q := c.translate(znab.Request{Type: znab.SearchTV, Query: "The Show", Season: "1", Episode: "2", Categories: []int{5000}})
	if q.Keywords != "The Show S01E02" || strings.Join(q.Categories, ",") != "tv,an" || q.Mode != "tv" {
		t.Fatalf("%+v", q)
	}
	req, ok := pickRequest(def.compiled.search, q)
	if !ok || !strings.HasPrefix(req.r.Path, "/s/") {
		t.Fatalf("%+v", req)
	}
	var b strings.Builder
	q.Page = 1
	if err := req.path.Execute(&b, q); err != nil || b.String() != "/s/The-Show-S01E02?c=tv,an&p=1" {
		t.Fatalf("%q %v", b.String(), err)
	}
	q = c.translate(znab.Request{IMDBID: "111161"})
	if req, _ := pickRequest(def.compiled.search, q); !strings.HasPrefix(req.r.Path, "/imdb/") || q.IMDBID != "tt0111161" {
		t.Fatalf("%+v", q)
	}
	if q := c.translate(znab.Request{}); q.Mode != "rss" {
		t.Fatal(q.Mode)
	}
	if q := c.translate(znab.Request{Type: znab.SearchTV, Query: "News", Season: "2026", Episode: "03/15"}); q.Keywords != "News 2026 03 15" {
		t.Fatal(q.Keywords)
	}
	if got := c.newznabFor("hd"); len(got) != 2 || got[0] != 2040 || got[1] != 2000 {
		t.Fatal(got)
	}
	if got := c.resolve("/torrent/1/"); got != "https://t.example/base/torrent/1/" {
		t.Fatal(got)
	}
	if got := c.resolve("https://elsewhere.example/x"); got != "https://elsewhere.example/x" || c.onSite(got) {
		t.Fatal(got)
	}
}

func TestDefinitionValidation(t *testing.T) {
	for name, src := range map[string]string{
		"unknown key":   "id: a1\nname: A\nversion: 1\nlinks: [https://a.example]\nmodes: {search: [q]}\nimpl: yts\ntest: {query: x}\nsurprise: 1\n",
		"bad link":      "id: a1\nname: A\nversion: 1\nlinks: [ftp://a]\nmodes: {search: [q]}\nimpl: yts\ntest: {query: x}\n",
		"bad mode":      "id: a1\nname: A\nversion: 1\nlinks: [https://a.example]\nmodes: {search: [q], films: [q]}\nimpl: yts\ntest: {query: x}\n",
		"unknown impl":  "id: a1\nname: A\nversion: 1\nlinks: [https://a.example]\nmodes: {search: [q]}\nimpl: nope\ntest: {query: x}\n",
		"no search":     "id: a1\nname: A\nversion: 1\nlinks: [https://a.example]\nmodes: {search: [q]}\ntest: {query: x}\n",
		"bad selector":  "id: a1\nname: A\nversion: 1\nlinks: [https://a.example]\nmodes: {search: [q]}\nsearch: {requests: [{path: /}], response: html, rows: 'tr[', fields: {title: {select: a}, details: {select: a, attr: href}}}\ntest: {query: x}\n",
		"bad template":  "id: a1\nname: A\nversion: 1\nlinks: [https://a.example]\nmodes: {search: [q]}\nsearch: {requests: [{path: '/{{ .Nope '}], response: json, fields: {title: {select: a}, details: {select: b}}}\ntest: {query: x}\n",
		"no link field": "id: a1\nname: A\nversion: 1\nlinks: [https://a.example]\nmodes: {search: [q]}\nsearch: {requests: [{path: /}], response: json, fields: {title: {select: a}}}\ntest: {query: x}\n",
		"no test":       "id: a1\nname: A\nversion: 1\nlinks: [https://a.example]\nmodes: {search: [q]}\nimpl: yts\n",
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
