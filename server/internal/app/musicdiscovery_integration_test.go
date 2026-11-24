package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	lfmfake "github.com/mmzlopasa/locarr/server/internal/metadata/lastfm/fake"
	lbfake "github.com/mmzlopasa/locarr/server/internal/metadata/listenbrainz/fake"
	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
	spfake "github.com/mmzlopasa/locarr/server/internal/metadata/spotify/fake"
)

type apiSuggestion struct {
	Mbid      string
	Name      string
	Score     int
	SeedCount int `json:"seedCount"`
	Seeds     []struct {
		Mbid     string
		Name     string
		ArtistID int64 `json:"artistId"`
	}
	Providers     []string
	Tags          []string
	Cover         string
	MatchedByName bool `json:"matchedByName"`
	Links         struct{ Musicbrainz, Lastfm, Spotify, Listenbrainz string }
	ArtistID      int64 `json:"artistId"`
	Status        string
	RequestID     int64 `json:"requestId"`
}

type apiSuggestions struct {
	Items     []apiSuggestion
	Total     int
	Tags      []struct{ Name string }
	Providers []struct {
		ID             string
		Enabled        bool
		Configured     bool
		Artists        int
		RelatedArtists string `json:"relatedArtists"`
	}
	Pending    int
	Refreshing bool
	CanAdd     bool `json:"canAdd"`
	CanRequest bool `json:"canRequest"`
}

// Settings with keys checked at the provider, suggestions for the library,
// "not interested", the similar artists of an artist, adding a suggestion,
// and what people who only request see.
func TestMusicDiscoveryAPI(t *testing.T) {
	me := newMusicEnv(t)
	mux := http.NewServeMux()
	mux.Handle("/lastfm/", lfmfake.New())
	mux.Handle("/lb/", lbfake.New())
	mux.Handle("/spotify/", spfake.New())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	md := me.deps.MusicDiscovery
	md.LastFM.BaseURL, md.LastFM.Limiter = srv.URL+"/lastfm/", metadata.NewLimiter(1000, 1000)
	md.LB.APIURL, md.LB.LabsURL, md.LB.Limiter = srv.URL+"/lb", srv.URL+"/lb", metadata.NewLimiter(1000, 1000)
	md.Spotify.APIURL, md.Spotify.AccountsURL = srv.URL+"/spotify/v1", srv.URL+"/spotify"

	// Settings: keys are write-only and checked first.
	if r := me.do("PUT", "/api/v1/settings/musicdiscovery", map[string]any{"lastfmApiKey": "wrong"}); r.code() != "metadata_key_invalid" {
		t.Fatalf("bad key: %d %s", r.StatusCode, r.body)
	}
	if r := me.do("PUT", "/api/v1/settings/musicdiscovery", map[string]any{"spotifyClientId": "only-id"}); r.StatusCode != 400 {
		t.Fatalf("id without secret: %d", r.StatusCode)
	}
	var st struct {
		Lastfm       bool
		RefreshDays  int                       `json:"refreshDays"`
		LastfmApiKey struct{ Configured bool } `json:"lastfmApiKey"`
		Providers    []struct {
			ID             string
			RelatedArtists string `json:"relatedArtists"`
		}
	}
	r := me.expect(me.do("PUT", "/api/v1/settings/musicdiscovery", map[string]any{"lastfmApiKey": lfmfake.Key, "spotifyClientId": spfake.ClientID,
		"spotifyClientSecret": spfake.ClientSecret, "refreshDays": 14}), 200)
	r.json(&st)
	if !st.LastfmApiKey.Configured || st.RefreshDays != 14 || st.Providers[3].RelatedArtists != "restricted" {
		t.Fatalf("settings: %s", r.body)
	}
	if strings.Contains(string(r.body), lfmfake.Key) || strings.Contains(string(r.body), spfake.ClientSecret) {
		t.Fatal("a key came back")
	}
	var test struct {
		Ok             bool
		RelatedArtists string `json:"relatedArtists"`
	}
	me.expect(me.do("POST", "/api/v1/settings/musicdiscovery/test", map[string]any{"provider": "spotify"}), 200).json(&test)
	if !test.Ok || test.RelatedArtists != "restricted" {
		t.Fatalf("spotify test: %+v", test)
	}
	me.expect(me.do("POST", "/api/v1/settings/musicdiscovery/test", map[string]any{"provider": "lastfm"}), 200).json(&test)
	if !test.Ok {
		t.Fatalf("lastfm test: %+v", test)
	}

	// A library artist: suggestions appear once the background refresh ran.
	open := me.addArtist(mbfake.ArtistOpen, false, "none")
	var list apiSuggestions
	deadline := time.Now().Add(20 * time.Second)
	for {
		me.expect(me.do("GET", "/api/v1/music/discover/artists", nil), 200).json(&list)
		if list.Total >= 2 && list.Items[0].Cover != "" && !list.Refreshing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no suggestions: %+v", list)
		}
		time.Sleep(100 * time.Millisecond)
	}
	choir := list.Items[0]
	if choir.Mbid != mbfake.ArtistChoir || choir.Seeds[0].ArtistID != open.ID || choir.Links.Lastfm == "" || choir.Links.Musicbrainz == "" ||
		strings.Join(choir.Providers, ",") != "lastfm,listenbrainz,musicbrainz" || choir.Status != "none" || !list.CanAdd {
		t.Fatalf("choir: %+v", choir)
	}
	for _, p := range list.Providers {
		if p.ID == "spotify" && (p.RelatedArtists != "restricted" || p.Artists != 0) {
			t.Fatalf("spotify state: %+v", p)
		}
	}
	// Filter by tag.
	var tagged apiSuggestions
	me.expect(me.do("GET", "/api/v1/music/discover/artists?tag=choral&provider=lastfm,listenbrainz", nil), 200).json(&tagged)
	if tagged.Total != 1 || tagged.Items[0].Mbid != mbfake.ArtistChoir {
		t.Fatalf("tag filter: %+v", tagged)
	}

	// Not interested, and back.
	me.expect(me.do("POST", "/api/v1/music/discover/exclusions", map[string]any{"mbid": mbfake.ArtistOther}), 200)
	me.expect(me.do("GET", "/api/v1/music/discover/artists", nil), 200).json(&list)
	if list.Total != 1 {
		t.Fatalf("excluded: %+v", list.Items)
	}
	var ex []struct{ Mbid, Name string }
	me.expect(me.do("GET", "/api/v1/music/discover/exclusions", nil), 200).json(&ex)
	if len(ex) != 1 || ex[0].Mbid != mbfake.ArtistOther {
		t.Fatalf("exclusions: %+v", ex)
	}
	me.expect(me.do("DELETE", "/api/v1/music/discover/exclusions/"+mbfake.ArtistOther, nil), 204)
	if r := me.do("POST", "/api/v1/music/discover/exclusions", map[string]any{"mbid": "not-an-id"}); r.StatusCode != 400 {
		t.Fatalf("bad id: %d", r.StatusCode)
	}

	// People who only request see the suggestions with Request, without library ids.
	fan := me.user("fan", "requester", requesterPerms())
	me.as(fan, func() {
		var mine apiSuggestions
		r := me.expect(me.do("GET", "/api/v1/music/discover/artists", nil), 200)
		r.json(&mine)
		if mine.CanAdd || !mine.CanRequest || mine.Total != 2 || mine.Items[0].Seeds[0].ArtistID != 0 || mine.Items[0].ArtistID != 0 {
			t.Fatalf("requester view: %s", r.body)
		}
		for _, path := range []string{"/api/v1/music/discover/exclusions", "/api/v1/settings/musicdiscovery",
			"/api/v1/music/artists/" + itoa(open.ID) + "/similar"} {
			if r := me.do("GET", path, nil); r.StatusCode != 403 {
				t.Fatalf("requester %s: %d", path, r.StatusCode)
			}
		}
		if r := me.do("POST", "/api/v1/music/discover/exclusions", map[string]any{"mbid": mbfake.ArtistChoir}); r.StatusCode != 403 {
			t.Fatalf("requester excluding: %d", r.StatusCode)
		}
		if r := me.do("POST", "/api/v1/music/discover/refresh", nil); r.StatusCode != 403 {
			t.Fatalf("requester refreshing: %d", r.StatusCode)
		}
		var req struct{ ID int64 }
		me.expect(me.do("POST", "/api/v1/requests", map[string]any{"kind": "artist", "mbid": mbfake.ArtistChoir}), 201).json(&req)
		me.expect(me.do("GET", "/api/v1/music/discover/artists", nil), 200).json(&mine)
		if mine.Items[0].Status != "pending" || mine.Items[0].RequestID != req.ID {
			t.Fatalf("requested: %+v", mine.Items[0])
		}
	})
	perms := requesterPerms()
	perms["requestMusic"] = false
	noMusic := me.user("moviefan", "requester", perms)
	me.as(noMusic, func() {
		if r := me.do("GET", "/api/v1/music/discover/artists", nil); r.StatusCode != 403 {
			t.Fatalf("no music permission: %d", r.StatusCode)
		}
	})

	// Adding a suggestion takes it off the list; the artist page lists it among similar artists.
	choirArtist := me.addArtist(mbfake.ArtistChoir, false, "none")
	me.expect(me.do("GET", "/api/v1/music/discover/artists", nil), 200).json(&list)
	for _, it := range list.Items {
		if it.Mbid == mbfake.ArtistChoir {
			t.Fatal("an artist in the library is still suggested")
		}
	}
	var sim apiSuggestions
	me.expect(me.do("GET", "/api/v1/music/artists/"+itoa(open.ID)+"/similar", nil), 200).json(&sim)
	if len(sim.Items) == 0 || sim.Items[0].Mbid != mbfake.ArtistChoir || sim.Items[0].ArtistID != choirArtist.ID {
		t.Fatalf("similar: %+v", sim.Items)
	}
	me.expect(me.do("POST", "/api/v1/music/discover/refresh", map[string]any{"seeds": []string{mbfake.ArtistOpen}}), 202)
}
