//go:build streamero

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// streameroClient calls a real Streamero's own API.
type streameroClient struct {
	t     *testing.T
	base  string
	token string
}

func (c *streameroClient) call(method, path string, body, out any) int {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+"/api/v1"+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		c.t.Logf("Streamero %s %s: %d %s", method, path, res.StatusCode, raw)
		return res.StatusCode
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("Streamero %s %s: %v: %s", method, path, err, raw)
		}
	}
	return res.StatusCode
}

func (c *streameroClient) must(method, path string, body, out any) {
	c.t.Helper()
	if st := c.call(method, path, body, out); st >= 300 {
		c.t.Fatalf("Streamero %s %s: HTTP %d", method, path, st)
	}
}

type streameroTitle struct {
	TmdbID    int    `json:"tmdbId"`
	MediaType string `json:"mediaType"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	PosterURL string `json:"posterUrl"`
}

type streameroDiscover struct {
	Configured bool             `json:"configured"`
	CanRequest bool             `json:"canRequest"`
	Titles     []streameroTitle `json:"titles"`
}

func (d streameroDiscover) find(id int) *streameroTitle {
	for i := range d.Titles {
		if d.Titles[i].TmdbID == id {
			return &d.Titles[i]
		}
	}
	return nil
}

// A real Streamero uses Locarr as its Seerr, Radarr and Sonarr: discover,
// search, request and upcoming all go through the compatible routes.
func TestRealStreameroDiscover(t *testing.T) {
	stURL, host := os.Getenv("LOCARR_TEST_STREAMERO_URL"), os.Getenv("LOCARR_TEST_LOCARR_HOST")
	if stURL == "" || host == "" {
		t.Skip("LOCARR_TEST_STREAMERO_URL / LOCARR_TEST_LOCARR_HOST not set (scripts/test-streamero-compat.sh)")
	}
	se := newSeriesEnv(t)
	runRequests(t, se.env)
	soon := time.Now().UTC().AddDate(0, 0, 20).Format("2006-01-02")
	for i := range se.tmdb.Movies {
		if se.tmdb.Movies[i].ID == 900002 {
			se.tmdb.Movies[i].Digital = soon
		}
	}
	se.requestSettings(func(s map[string]any) {
		s["movie"] = kindDefaults(se.profileID, se.rootID)
		s["series"] = kindDefaults(se.tvProfile, se.tvRoot)
	})
	key := se.apiKey(se.token, false)

	// Locarr on an address the Streamero container can reach.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: se.srv.Config.Handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	locarr := fmt.Sprintf("http://%s:%d", host, ln.Addr().(*net.TCPAddr).Port)

	st := &streameroClient{t: t, base: strings.TrimRight(stURL, "/")}
	var signedIn struct{ Token string }
	st.must("POST", "/setup", map[string]any{"setupCode": os.Getenv("LOCARR_TEST_STREAMERO_SETUP"), "username": "admin",
		"password": "compat-admin-password-1"}, &signedIn)
	st.token = signedIn.Token
	st.must("PUT", "/settings/requests", map[string]any{"usersCanRequest": true,
		"seerrUrl": locarr + "/compat/seerr", "seerrApiKey": key,
		"radarrUrl": locarr + "/compat/radarr", "radarrApiKey": key,
		"sonarrUrl": locarr + "/compat/sonarr", "sonarrApiKey": key}, nil)
	var checks []struct {
		App     string `json:"app"`
		Ok      bool   `json:"ok"`
		Version string `json:"version"`
		Error   string `json:"error"`
	}
	st.must("POST", "/settings/requests/test", nil, &checks)
	if len(checks) != 3 {
		t.Fatalf("checks: %+v", checks)
	}
	for _, c := range checks {
		if !c.Ok || c.Version == "" {
			t.Fatalf("%s: %+v", c.App, c)
		}
	}

	var d streameroDiscover
	st.must("GET", "/discover", nil, &d)
	if !d.Configured || !d.CanRequest || len(d.Titles) == 0 {
		t.Fatalf("trending: %+v", d)
	}
	trending := len(d.Titles)
	st.must("GET", "/discover?q=Sintel", nil, &d)
	s := d.find(45745)
	if s == nil || s.MediaType != "movie" || s.Title != "Sintel" || s.Status != "none" || !strings.HasPrefix(s.PosterURL, "https://image.tmdb.org/") {
		t.Fatalf("search: %+v", d.Titles)
	}

	var res struct{ Status string }
	st.must("POST", "/discover/request", map[string]any{"tmdbId": 45745, "mediaType": "movie"}, &res)
	if res.Status != "processing" {
		t.Fatalf("movie request: %+v", res)
	}
	st.must("POST", "/discover/request", map[string]any{"tmdbId": 900002, "mediaType": "movie"}, &res)
	st.must("POST", "/discover/request", map[string]any{"tmdbId": 800424, "mediaType": "tv"}, &res)
	if res.Status != "processing" {
		t.Fatalf("series request: %+v", res)
	}

	// What reached Locarr.
	var reqs struct {
		Items []struct {
			Kind          string
			Title         string
			Status        string
			AllSeasons    bool                      `json:"allSeasons"`
			FutureSeasons bool                      `json:"futureSeasons"`
			RequestedBy   struct{ Username string } `json:"requestedBy"`
		}
	}
	se.expect(se.do("GET", "/api/v1/requests?status=all", nil), 200).json(&reqs)
	if len(reqs.Items) != 3 {
		t.Fatalf("Locarr requests: %+v", reqs.Items)
	}
	for _, r := range reqs.Items {
		if r.RequestedBy.Username != "admin" || r.Status == "pending" {
			t.Fatalf("request: %+v", r)
		}
		if r.Kind == "series" && (!r.AllSeasons || !r.FutureSeasons) {
			t.Fatalf("series request: %+v", r)
		}
	}

	st.must("GET", "/discover?q=Sintel", nil, &d)
	if s := d.find(45745); s == nil || s.Status != "processing" {
		t.Fatalf("badge: %+v", s)
	}

	var upcoming []struct {
		Kind        string `json:"kind"`
		Title       string `json:"title"`
		SeriesTitle string `json:"seriesTitle"`
		Season      int    `json:"season"`
		Episode     int    `json:"episode"`
		Date        time.Time
		PosterURL   string `json:"posterUrl"`
	}
	st.must("GET", "/upcoming?days=30", nil, &upcoming)
	var movie, eps int
	for _, u := range upcoming {
		switch {
		case u.Kind == "movie" && u.Title == "Kino Platno" && u.Date.Format("2006-01-02") == soon:
			movie++
		case u.Kind == "episode" && u.SeriesTitle == "Elephants Dream" && u.Season == 1 && u.PosterURL != "":
			eps++
		}
	}
	if movie != 1 || eps != 2 {
		t.Fatalf("upcoming: %+v", upcoming)
	}
	t.Logf("Streamero showed %d trending titles, requested 3 titles and listed %d upcoming releases through Locarr", trending, len(upcoming))
}
