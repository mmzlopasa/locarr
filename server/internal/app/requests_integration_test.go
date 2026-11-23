package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	mbfake "github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz/fake"
)

// runRequests runs the request sync in the background (Serve does it in
// runBackground).
func runRequests(t *testing.T, e *env) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.deps.Requests.Run(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })
}

// as runs f with another bearer token and puts the old one back.
func (e *env) as(token string, f func()) {
	old := e.token
	e.token = token
	defer func() { e.token = old }()
	f()
}

// user creates a user with permissions (nil = the role's) and signs it in.
func (e *env) user(name, role string, perms map[string]bool) string {
	e.t.Helper()
	body := map[string]any{"username": name, "password": name + "-password", "role": role}
	if perms != nil {
		body["permissions"] = perms
	}
	e.expect(e.do("POST", "/api/v1/users", body), 201)
	return e.login(name, name+"-password")
}

func requesterPerms(extra ...string) map[string]bool {
	p := map[string]bool{"requestMovies": true, "requestSeries": true, "requestMusic": true, "autoApprove": false, "request4k": false,
		"manageLibrary": false, "manageRequests": false}
	for _, k := range extra {
		p[k] = true
	}
	return p
}

type apiRequest struct {
	ID          int64
	Kind        string
	Status      string
	Title       string
	Is4k        bool
	LibraryRef  string `json:"libraryRef"`
	CanCancel   bool   `json:"canCancel"`
	AutoApprove bool   `json:"autoApproved"`
	Seasons     []struct {
		Number int
		Status string
	}
	Explanation *struct{ Code, Message string }
	RequestedBy struct{ Username string } `json:"requestedBy"`
}

func (e *env) request(id int64) apiRequest {
	e.t.Helper()
	var r apiRequest
	e.expect(e.do("GET", fmt.Sprintf("/api/v1/requests/%d", id), nil), 200).json(&r)
	return r
}

type apiNotifications struct {
	Items []struct {
		ID        int64
		Type      string
		Title     string
		Message   string
		RequestId int64 `json:"requestId"`
		IssueId   int64 `json:"issueId"`
		Read      bool
	}
	Unread   int
	LatestId int64 `json:"latestId"`
}

func (e *env) notifications() apiNotifications {
	e.t.Helper()
	var n apiNotifications
	e.expect(e.do("GET", "/api/v1/notifications", nil), 200).json(&n)
	return n
}

func (n apiNotifications) has(typ string) bool {
	for _, it := range n.Items {
		if it.Type == typ {
			return true
		}
	}
	return false
}

func (n apiNotifications) types() string {
	var out []string
	for _, it := range n.Items {
		out = append(out, it.Type)
	}
	return strings.Join(out, " ")
}

func (e *env) requestSettings(mod func(s map[string]any)) {
	e.t.Helper()
	var s map[string]any
	e.expect(e.do("GET", "/api/v1/settings/requests", nil), 200).json(&s)
	mod(s)
	e.expect(e.do("PUT", "/api/v1/settings/requests", s), 200)
}

func kindDefaults(profile, root int64) map[string]any {
	return map[string]any{"qualityProfileId": profile, "rootFolderId": root, "qualityProfile4kId": 0, "rootFolder4kId": 0, "tags": []string{}}
}

type apiDiscoverItem struct {
	MediaType string `json:"mediaType"`
	TmdbId    int64  `json:"tmdbId"`
	Mbid      string
	Title     string
	Status    string
	Status4k  string `json:"status4k"`
	RequestId int64  `json:"requestId"`
}

// A requester finds a movie, asks for it; an administrator approves; the
// movie is searched, downloaded and imported; the requester sees it
// available and is told. Then a reported problem gets a replacement search.
func TestRequestMovieEndToEnd(t *testing.T) {
	le := newLibraryEnv(t)
	runRequests(t, le.env)
	le.requestSettings(func(s map[string]any) { s["movie"] = kindDefaults(le.profileID, le.rootID) })
	fam := le.user("fam", "requester", nil)

	// Discover: the home rows, search, the title page.
	var home struct {
		Rows []struct {
			Key   string
			Items []apiDiscoverItem
		}
		MetadataReady bool `json:"metadataReady"`
	}
	le.as(fam, func() { le.expect(le.do("GET", "/api/v1/discover", nil), 200).json(&home) })
	if !home.MetadataReady || len(home.Rows) == 0 || home.Rows[0].Key != "trending" {
		t.Fatalf("home: %+v", home)
	}
	var search struct{ Items []apiDiscoverItem }
	le.as(fam, func() { le.expect(le.do("GET", "/api/v1/discover/search?q=tears", nil), 200).json(&search) })
	if len(search.Items) != 1 || search.Items[0].TmdbId != 133701 || search.Items[0].Status != "none" {
		t.Fatalf("search: %+v", search.Items)
	}
	var page struct {
		Item       apiDiscoverItem
		Cast       []struct{ Name string }
		TrailerUrl string `json:"trailerUrl"`
		LibraryRef string `json:"libraryRef"`
		Ability    struct {
			CanRequest     bool `json:"canRequest"`
			CanRequest4k   bool `json:"canRequest4k"`
			CanReportIssue bool `json:"canReportIssue"`
		}
	}
	le.as(fam, func() { le.expect(le.do("GET", "/api/v1/discover/movies/133701", nil), 200).json(&page) })
	if page.Item.Title != "Tears of Steel" || len(page.Cast) == 0 || !strings.HasPrefix(page.TrailerUrl, "https://www.youtube.com/watch?v=") ||
		!page.Ability.CanRequest || page.Ability.CanRequest4k || page.Ability.CanReportIssue {
		t.Fatalf("title page: %+v", page)
	}

	// Request it; asking again is refused.
	var req apiRequest
	le.as(fam, func() {
		le.expect(le.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 133701, "comment": "for the weekend"}), 201).json(&req)
		if r := le.expect(le.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 133701}), 409); r.code() != "already_requested" {
			t.Fatalf("duplicate: %s", r.code())
		}
		if r := le.expect(le.do("POST", fmt.Sprintf("/api/v1/requests/%d/approve", req.ID), nil), 403); r.code() != "forbidden" {
			t.Fatalf("requester approving: %s", r.code())
		}
	})
	if req.Status != "pending" || !req.CanCancel || req.LibraryRef != "" {
		t.Fatalf("created: %+v", req)
	}
	if n := le.notifications(); !n.has("request.created") {
		t.Fatalf("admin was not told: %s", n.types())
	}
	le.as(fam, func() {
		le.expect(le.do("GET", "/api/v1/discover/search?q=tears", nil), 200).json(&search)
		if search.Items[0].Status != "pending" || search.Items[0].RequestId != req.ID {
			t.Fatalf("badge while pending: %+v", search.Items[0])
		}
	})

	// The administrator approves: the movie is added and searched.
	var approved apiRequest
	le.expect(le.do("POST", fmt.Sprintf("/api/v1/requests/%d/approve", req.ID), nil), 200).json(&approved)
	if approved.Status != "processing" || !strings.HasPrefix(approved.LibraryRef, "movie:") {
		t.Fatalf("approved: %+v", approved)
	}
	var movieID int64
	_, _ = fmt.Sscanf(approved.LibraryRef, "movie:%d", &movieID)
	waitFor(t, "the grab", func() bool { return le.downloadCount(movieID) == 1 })
	le.poll()
	le.client.finish(t)
	le.poll()
	waitFor(t, "the request to become available", func() bool {
		var r apiRequest
		le.as(fam, func() { r = le.request(req.ID) })
		return r.Status == "available"
	})
	le.as(fam, func() {
		n := le.notifications()
		if !n.has("request.approved") || !n.has("request.available") || n.Unread < 2 {
			t.Fatalf("requester notifications: %s (%d unread)", n.types(), n.Unread)
		}
		r := le.request(req.ID)
		if r.LibraryRef != "" {
			t.Fatal("a requester must not see library refs")
		}
		le.expect(le.do("GET", "/api/v1/discover/movies/133701", nil), 200).json(&page)
		if page.Item.Status != "available" || !page.Ability.CanReportIssue || page.LibraryRef != "" {
			t.Fatalf("title page after import: %+v", page)
		}
		// Polling with since: nothing new.
		var poll apiNotifications
		le.expect(le.do("GET", fmt.Sprintf("/api/v1/notifications?since=%d", n.LatestId), nil), 200).json(&poll)
		if len(poll.Items) != 0 {
			t.Fatalf("since: %+v", poll.Items)
		}
		var unread struct{ Unread int }
		le.expect(le.do("POST", "/api/v1/notifications/read", map[string]any{"all": true}), 200).json(&unread)
		if unread.Unread != 0 {
			t.Fatal("mark all read")
		}
		if r := le.expect(le.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 133701}), 409); r.code() != "already_available" {
			t.Fatalf("available title: %s", r.code())
		}
	})
	tl := le.timeline(movieID)
	if !strings.Contains(tl.types(), "requested") || !strings.Contains(tl.types(), "request_approved") {
		t.Fatalf("timeline: %s", tl.types())
	}

	// A problem: the administrator resolves it with a replacement search.
	var issue struct {
		ID     int64
		Status string
	}
	le.as(fam, func() {
		le.expect(le.do("POST", "/api/v1/issues", map[string]any{"kind": "movie", "tmdbId": 133701, "type": "audio",
			"message": "no sound after 5 minutes"}), 201).json(&issue)
		if r := le.expect(le.do("POST", fmt.Sprintf("/api/v1/issues/%d/resolve", issue.ID), map[string]any{}), 403); r.code() != "forbidden" {
			t.Fatal("requester resolving")
		}
		if r := le.expect(le.do("POST", "/api/v1/issues", map[string]any{"kind": "movie", "tmdbId": 10378, "type": "video", "message": "x"}), 409); r.code() != "not_available" {
			t.Fatalf("issue for a title that is not there: %s", r.code())
		}
	})
	if n := le.notifications(); !n.has("issue.created") {
		t.Fatalf("admin not told about the issue: %s", n.types())
	}
	file := le.movie(movieID).File.Path
	le.expect(le.do("POST", fmt.Sprintf("/api/v1/issues/%d/resolve", issue.ID), map[string]any{"searchReplacement": true,
		"comment": "looking for another release"}), 200).json(&issue)
	if issue.Status != "resolved" {
		t.Fatalf("issue: %+v", issue)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("the old file is still in the library")
	}
	if m := le.movie(movieID); m.File != nil {
		t.Fatalf("movie still has a file: %+v", m.File)
	}
	var bin struct{ Entries []struct{ OriginalPath string } }
	le.expect(le.do("GET", "/api/v1/recyclebin", nil), 200).json(&bin)
	if len(bin.Entries) != 1 || bin.Entries[0].OriginalPath != file {
		t.Fatalf("recycle bin: %+v", bin)
	}
	var blocked []struct{ Title, Reason string }
	le.expect(le.do("GET", "/api/v1/queue/blocklist", nil), 200).json(&blocked)
	if len(blocked) != 1 || blocked[0].Title != "Tears.of.Steel.2012.720p.WEBRip.x264-LOCARR" || blocked[0].Reason != "issue" {
		t.Fatalf("blocklist: %+v", blocked)
	}
	le.as(fam, func() {
		if n := le.notifications(); !n.has("issue.resolved") {
			t.Fatalf("reporter not told: %s", n.types())
		}
		var comments []struct{ Body string }
		le.expect(le.do("GET", fmt.Sprintf("/api/v1/issues/%d/comments", issue.ID), nil), 200).json(&comments)
		if len(comments) != 1 {
			t.Fatalf("comments: %+v", comments)
		}
		le.expect(le.do("POST", fmt.Sprintf("/api/v1/issues/%d/reopen", issue.ID), nil), 200).json(&issue)
		if issue.Status != "open" {
			t.Fatal("reopen")
		}
	})
}

// A user who may auto-approve series asks for one season: no approval, the
// season pack arrives, the season and the request are available.
func TestRequestSeasonAutoApproved(t *testing.T) {
	se := newSeriesEnv(t)
	runRequests(t, se.env)
	se.requestSettings(func(s map[string]any) { s["series"] = kindDefaults(se.tvProfile, se.tvRoot) })
	tok := se.user("kid", "requester", requesterPerms("autoApproveSeries"))

	var page struct {
		Seasons []struct {
			Number int
			Status string
		}
	}
	se.as(tok, func() {
		se.expect(se.do("GET", "/api/v1/discover/series/800434", nil), 200).json(&page)
		if len(page.Seasons) != 1 || page.Seasons[0].Number != 1 || page.Seasons[0].Status != "none" {
			t.Fatalf("seasons: %+v", page.Seasons)
		}
		if r := se.expect(se.do("POST", "/api/v1/requests", map[string]any{"kind": "series", "tmdbId": 800434, "seasons": []int{7}}), 400); r.code() != "validation" {
			t.Fatalf("unknown season: %s", r.code())
		}
	})
	var req apiRequest
	se.as(tok, func() {
		se.expect(se.do("POST", "/api/v1/requests", map[string]any{"kind": "series", "tmdbId": 800434, "seasons": []int{1}}), 201).json(&req)
	})
	if req.Status != "processing" || !req.AutoApprove || len(req.Seasons) != 1 {
		t.Fatalf("auto-approved: %+v", req)
	}
	r := se.request(req.ID)
	var seriesID int64
	_, _ = fmt.Sscanf(r.LibraryRef, "series:%d", &seriesID)
	if seriesID == 0 {
		t.Fatalf("library ref %q", r.LibraryRef)
	}
	// Only the requested season is monitored; specials are not.
	if ep := se.episode(seriesID, 0, 1); ep.Monitored {
		t.Fatal("specials monitored")
	}
	if ep := se.episode(seriesID, 1, 1); !ep.Monitored {
		t.Fatal("season 1 not monitored")
	}
	waitFor(t, "the season pack", func() bool {
		var n int
		_ = se.deps.DB.R.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&n)
		return n > 0
	})
	se.poll()
	se.as(tok, func() {
		if got := se.request(req.ID); got.Status != "processing" {
			t.Fatalf("while downloading: %+v", got)
		}
	})
	se.client.finish(t)
	se.poll()
	waitFor(t, "the season to be available", func() bool { return se.request(req.ID).Status == "available" })
	got := se.request(req.ID)
	if got.Seasons[0].Status != "available" {
		t.Fatalf("season status: %+v", got.Seasons)
	}
	se.as(tok, func() {
		if n := se.notifications(); !n.has("request.available") || n.has("request.approved") {
			t.Fatalf("notifications: %s", n.types())
		}
		se.expect(se.do("GET", "/api/v1/discover/series/800434", nil), 200).json(&page)
		if page.Seasons[0].Status != "available" {
			t.Fatalf("season badge: %+v", page.Seasons)
		}
		if r := se.expect(se.do("POST", "/api/v1/requests", map[string]any{"kind": "series", "tmdbId": 800434, "allSeasons": true}), 409); r.code() != "already_requested" {
			t.Fatalf("all seasons again: %s", r.code())
		}
	})
}

// An album request goes through approval to a FLAC import.
func TestRequestAlbumEndToEnd(t *testing.T) {
	me := newMusicEnv(t)
	runRequests(t, me.env)
	me.requestSettings(func(s map[string]any) { s["music"] = kindDefaults(me.lossless, me.rootID) })
	fan := me.user("fan", "requester", nil)

	var artist struct {
		Item   apiDiscoverItem
		Albums []apiDiscoverItem
	}
	var search struct{ Items []apiDiscoverItem }
	me.as(fan, func() {
		me.expect(me.do("GET", "/api/v1/discover/search?type=music&q=free%20album", nil), 200).json(&search)
		found := false
		for _, it := range search.Items {
			found = found || (it.MediaType == "album" && it.Mbid == mbfake.GroupFreeAlbum)
		}
		if !found {
			t.Fatalf("music search: %+v", search.Items)
		}
		me.expect(me.do("GET", "/api/v1/discover/artists/"+mbfake.ArtistOpen, nil), 200).json(&artist)
	})
	if artist.Item.Title != "Open Source Artist" || len(artist.Albums) == 0 {
		t.Fatalf("artist page: %+v", artist)
	}
	var req apiRequest
	me.as(fan, func() {
		me.expect(me.do("POST", "/api/v1/requests", map[string]any{"kind": "album", "mbid": mbfake.GroupFreeAlbum}), 201).json(&req)
	})
	if req.Status != "pending" || req.Title != "Free Album" {
		t.Fatalf("album request: %+v", req)
	}
	me.expect(me.do("POST", fmt.Sprintf("/api/v1/requests/%d/approve", req.ID), nil), 200).json(&req)
	if req.Status != "processing" || !strings.HasPrefix(req.LibraryRef, "album:") {
		t.Fatalf("approved: %+v", req)
	}
	var albumID int64
	_, _ = fmt.Sscanf(req.LibraryRef, "album:%d", &albumID)
	waitFor(t, "album grab", func() bool { return me.downloadsFor(albumID) == 1 })
	// Only the requested album is monitored.
	if got := me.client.added; len(got) != 1 || !strings.Contains(got[0], "[FLAC]") {
		t.Fatalf("grabbed %v", got)
	}
	me.client.finish(t)
	me.poll()
	waitFor(t, "the album to be available", func() bool { return me.request(req.ID).Status == "available" })
	me.as(fan, func() {
		if n := me.notifications(); !n.has("request.available") {
			t.Fatalf("notifications: %s", n.types())
		}
		me.expect(me.do("GET", "/api/v1/discover/artists/"+mbfake.ArtistOpen, nil), 200).json(&artist)
		for _, al := range artist.Albums {
			if al.Mbid == mbfake.GroupFreeAlbum && al.Status != "available" {
				t.Fatalf("album badge: %+v", al)
			}
		}
	})
}

// Quotas, cancelling, declining, bulk approval, comments and invites.
func TestRequestRulesQuotasAndInvites(t *testing.T) {
	le := newLibraryEnv(t)
	le.requestSettings(func(s map[string]any) {
		s["movie"] = kindDefaults(le.profileID, le.rootID)
		s["quotas"] = map[string]any{"movie": map[string]any{"limit": 2, "days": 7}, "series": map[string]any{"limit": 0, "days": 7},
			"music": map[string]any{"limit": 0, "days": 7}}
	})
	tok := le.user("ana", "requester", nil)
	ids := map[int64]int64{}
	le.as(tok, func() {
		for _, tmdb := range []int64{10378, 45745} {
			var r apiRequest
			le.expect(le.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": tmdb}), 201).json(&r)
			ids[tmdb] = r.ID
		}
		if r := le.expect(le.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 133701}), 429); r.code() != "quota_exceeded" {
			t.Fatalf("quota: %s", r.code())
		}
		var sum struct {
			Pending int
			Quotas  struct {
				Movie struct {
					Limit, Used, Remaining int
					NextSlotAt             string `json:"nextSlotAt"`
				}
				Series struct{ Unlimited bool }
			}
		}
		le.expect(le.do("GET", "/api/v1/requests/summary", nil), 200).json(&sum)
		if sum.Pending != 2 || sum.Quotas.Movie.Limit != 2 || sum.Quotas.Movie.Remaining != 0 || sum.Quotas.Movie.NextSlotAt == "" ||
			!sum.Quotas.Series.Unlimited {
			t.Fatalf("summary: %+v", sum)
		}
		// Cancelling an own pending request frees nothing of the quota window but stops it.
		var c apiRequest
		le.expect(le.do("POST", fmt.Sprintf("/api/v1/requests/%d/cancel", ids[45745]), nil), 200).json(&c)
		if c.Status != "cancelled" {
			t.Fatalf("cancel: %+v", c)
		}
		if r := le.expect(le.do("POST", fmt.Sprintf("/api/v1/requests/%d/cancel", ids[45745]), nil), 409); r.code() != "not_pending" {
			t.Fatalf("cancel twice: %s", r.code())
		}
		// A cancelled request does not count.
		le.expect(le.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 900002, "comment": "please"}), 201)
	})
	// An own rule for her beats the default.
	var uid int64
	_ = le.deps.DB.R.QueryRow(`SELECT id FROM users WHERE username = 'ana'`).Scan(&uid)
	var quotas struct {
		Movie struct {
			Limit  int
			Custom bool
		}
	}
	le.expect(le.do("PUT", fmt.Sprintf("/api/v1/users/%d/quotas", uid), map[string]any{"movie": map[string]any{"limit": 10, "days": 7}}), 200).json(&quotas)
	if quotas.Movie.Limit != 10 || !quotas.Movie.Custom {
		t.Fatalf("override: %+v", quotas)
	}
	le.as(tok, func() {
		le.expect(le.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 133701}), 201)
	})

	// Decline with a reason; bulk approval reports what it could not do.
	le.expect(le.do("POST", fmt.Sprintf("/api/v1/requests/%d/decline", ids[10378]), map[string]any{"reason": "we have it on disc"}), 200)
	var all struct {
		Items []apiRequest
		Total int
	}
	le.expect(le.do("GET", "/api/v1/requests?status=pending", nil), 200).json(&all)
	if all.Total != 2 {
		t.Fatalf("pending: %+v", all)
	}
	var bulk struct {
		Done   int
		Errors []struct {
			ID   int64
			Code string
		}
	}
	le.expect(le.do("POST", "/api/v1/requests/approve", map[string]any{"ids": []int64{all.Items[0].ID, ids[10378]}}), 200).json(&bulk)
	if bulk.Done != 1 || len(bulk.Errors) != 1 || bulk.Errors[0].ID != ids[10378] || bulk.Errors[0].Code != "not_pending" {
		t.Fatalf("bulk: %+v", bulk)
	}
	le.as(tok, func() {
		n := le.notifications()
		if !n.has("request.declined") || !n.has("request.approved") {
			t.Fatalf("requester notifications: %s", n.types())
		}
		for _, it := range n.Items {
			if it.Type == "request.declined" && it.Message != "we have it on disc" {
				t.Fatalf("decline reason: %+v", it)
			}
		}
		le.expect(le.do("POST", fmt.Sprintf("/api/v1/requests/%d/comments", all.Items[1].ID), map[string]any{"body": "any news?"}), 201)
		// Another user's request is invisible.
		var mine struct{ Total int }
		le.expect(le.do("GET", "/api/v1/requests?mine=false", nil), 200).json(&mine)
		if mine.Total != 4 {
			t.Fatalf("own requests: %d", mine.Total)
		}
	})
	if n := le.notifications(); !n.has("request.comment") {
		t.Fatalf("admin not told about the comment: %s", n.types())
	}
	other := le.user("ivo", "requester", nil)
	le.as(other, func() {
		le.expect(le.do("GET", fmt.Sprintf("/api/v1/requests/%d", ids[10378]), nil), 404)
		le.expect(le.do("GET", fmt.Sprintf("/api/v1/requests/%d/comments", ids[10378]), nil), 404)
		var list struct{ Total int }
		le.expect(le.do("GET", "/api/v1/requests", nil), 200).json(&list)
		if list.Total != 0 {
			t.Fatal("a requester sees someone else's requests")
		}
	})

	// Invites: one use, quotas given to the new account; revoked ones stop working.
	var inv struct {
		Invite struct{ ID int64 }
		Code   string
	}
	le.expect(le.do("POST", "/api/v1/invites", map[string]any{"label": "Family", "role": "requester", "maxUses": 1, "expiresInDays": 3,
		"quotas": map[string]any{"movie": map[string]any{"limit": 1, "days": 30}}}), 201).json(&inv)
	if inv.Code == "" {
		t.Fatal("no code")
	}
	noAuth := map[string]string{"Authorization": ""}
	var check struct{ Role, InstanceName string }
	le.expect(le.req("POST", "/api/v1/auth/invite", map[string]any{"code": inv.Code}, noAuth), 200).json(&check)
	if check.Role != "requester" || check.InstanceName == "" {
		t.Fatalf("check: %+v", check)
	}
	var signed struct {
		Token string
		User  struct {
			Username, Role string
		}
	}
	le.expect(le.req("POST", "/api/v1/auth/signup", map[string]any{"code": inv.Code, "username": "nana", "password": "nana-password-1"}, noAuth), 200).json(&signed)
	if signed.Token == "" || signed.User.Role != "requester" {
		t.Fatalf("sign-up: %+v", signed)
	}
	if r := le.expect(le.req("POST", "/api/v1/auth/signup", map[string]any{"code": inv.Code, "username": "nana2", "password": "nana-password-2"}, noAuth), 403); r.code() != "invite_invalid" {
		t.Fatalf("used invite: %s", r.code())
	}
	le.as(signed.Token, func() {
		var sum struct {
			Quotas struct {
				Movie struct {
					Limit  int
					Custom bool
				}
			}
		}
		le.expect(le.do("GET", "/api/v1/requests/summary", nil), 200).json(&sum)
		if sum.Quotas.Movie.Limit != 1 || !sum.Quotas.Movie.Custom {
			t.Fatalf("invite quotas: %+v", sum.Quotas)
		}
	})
	var inv2 struct {
		Invite struct{ ID int64 }
		Code   string
	}
	le.expect(le.do("POST", "/api/v1/invites", map[string]any{"role": "requester"}), 201).json(&inv2)
	le.expect(le.do("DELETE", fmt.Sprintf("/api/v1/invites/%d", inv2.Invite.ID), nil), 204)
	le.expect(le.req("POST", "/api/v1/auth/invite", map[string]any{"code": inv2.Code}, noAuth), 403)
	var invites []struct {
		ID      int64
		Uses    int
		Expired bool
		Revoked bool
	}
	le.expect(le.do("GET", "/api/v1/invites", nil), 200).json(&invites)
	if len(invites) != 2 || !invites[0].Revoked || invites[1].Uses != 1 || !invites[1].Expired {
		t.Fatalf("invites: %+v", invites)
	}
	var providers []struct{ ID, Kind string }
	le.expect(le.req("GET", "/api/v1/auth/providers", nil, noAuth), 200).json(&providers)
	if len(providers) != 1 || providers[0].ID != "local" || providers[0].Kind != "password" {
		t.Fatalf("providers: %+v", providers)
	}
}

// Every new endpoint and the library ones, as a requester, a manager, an
// administrator and a read-only API key.
func TestRequestPermissions(t *testing.T) {
	le := newLibraryEnv(t)
	le.requestSettings(func(s map[string]any) { s["movie"] = kindDefaults(le.profileID, le.rootID) })
	requester := le.user("req", "requester", nil)
	manager := le.user("mgr", "manager", nil)
	var key struct{ Key string }
	le.expect(le.do("POST", "/api/v1/auth/apikeys", map[string]any{"name": "Dashboard", "readOnly": true}), 201).json(&key)
	var r apiRequest
	le.as(requester, func() {
		le.expect(le.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 10378}), 201).json(&r)
	})
	var uid int64
	_ = le.deps.DB.R.QueryRow(`SELECT id FROM users WHERE username = 'req'`).Scan(&uid)
	reqPath := func(s string) string { return fmt.Sprintf(s, r.ID) }

	type call struct {
		method, path string
		body         any
	}
	calls := map[string]call{
		"discover":        {"GET", "/api/v1/discover", nil},
		"discoverList":    {"GET", "/api/v1/discover/lists/popular_movies", nil},
		"genres":          {"GET", "/api/v1/discover/genres", nil},
		"search":          {"GET", "/api/v1/discover/search?q=bunny", nil},
		"movie":           {"GET", "/api/v1/discover/movies/10378", nil},
		"series":          {"GET", "/api/v1/discover/series/800434", nil},
		"person":          {"GET", "/api/v1/discover/people/5001", nil},
		"requests":        {"GET", "/api/v1/requests", nil},
		"summary":         {"GET", "/api/v1/requests/summary", nil},
		"getRequest":      {"GET", reqPath("/api/v1/requests/%d"), nil},
		"comments":        {"GET", reqPath("/api/v1/requests/%d/comments"), nil},
		"issues":          {"GET", "/api/v1/issues", nil},
		"notifications":   {"GET", "/api/v1/notifications", nil},
		"createRequest":   {"POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 45745}},
		"comment":         {"POST", reqPath("/api/v1/requests/%d/comments"), map[string]any{"body": "hi"}},
		"markRead":        {"POST", "/api/v1/notifications/read", map[string]any{"all": true}},
		"editRequest":     {"PATCH", reqPath("/api/v1/requests/%d"), map[string]any{"tags": []string{"kids"}}},
		"bulkApprove":     {"POST", "/api/v1/requests/approve", map[string]any{"ids": []int64{999}}},
		"retry":           {"POST", reqPath("/api/v1/requests/%d/retry"), nil},
		"resolveIssue":    {"POST", "/api/v1/issues/999/resolve", map[string]any{}},
		"requestSettings": {"GET", "/api/v1/settings/requests", nil},
		"putSettings":     {"PUT", "/api/v1/settings/requests", map[string]any{"movie": kindDefaults(0, 0), "series": kindDefaults(0, 0), "music": kindDefaults(0, 0), "quotas": map[string]any{"movie": map[string]any{"limit": 0, "days": 7}, "series": map[string]any{"limit": 0, "days": 7}, "music": map[string]any{"limit": 0, "days": 7}}}},
		"invites":         {"GET", "/api/v1/invites", nil},
		"createInvite":    {"POST", "/api/v1/invites", map[string]any{"role": "requester"}},
		"userQuotas":      {"GET", fmt.Sprintf("/api/v1/users/%d/quotas", uid), nil},
		"libraryMovies":   {"GET", "/api/v1/movies", nil},
		"libraryMovie":    {"GET", "/api/v1/movies/999999", nil},
		"librarySeries":   {"GET", "/api/v1/series", nil},
		"libraryArtists":  {"GET", "/api/v1/music/artists", nil},
		"calendar":        {"GET", "/api/v1/calendar?start=2026-01-01&end=2026-02-01", nil},
		"wanted":          {"GET", "/api/v1/episodes/wanted", nil},
		"queue":           {"GET", "/api/v1/queue", nil},
		"timeline":        {"GET", "/api/v1/timeline?item=movie:999999", nil},
	}
	// Expected status per caller; "" = 2xx.
	expect := map[string]map[string]int{
		"requester": {"editRequest": 403, "bulkApprove": 403, "retry": 403, "resolveIssue": 403, "requestSettings": 403, "putSettings": 403,
			"invites": 403, "createInvite": 403, "userQuotas": 403, "libraryMovies": 403, "libraryMovie": 403, "librarySeries": 403,
			"libraryArtists": 403, "calendar": 403, "wanted": 403, "queue": 403, "timeline": 403},
		"manager": {"retry": 409, "resolveIssue": 404, "requestSettings": 403, "putSettings": 403, "invites": 403, "createInvite": 403,
			"userQuotas": 403, "libraryMovie": 404, "timeline": 404},
		"readonly": {"createRequest": 403, "comment": 403, "markRead": 403, "editRequest": 403, "bulkApprove": 403, "retry": 403,
			"resolveIssue": 403, "putSettings": 403, "createInvite": 403, "libraryMovie": 404, "timeline": 404},
	}
	tokens := map[string]string{"requester": requester, "manager": manager, "readonly": key.Key}
	for who, tok := range tokens {
		for name, c := range calls {
			want := expect[who][name]
			body := c.body
			if name == "createRequest" {
				// Each caller asks for another title (a duplicate would be refused).
				body = map[string]any{"kind": "movie", "tmdbId": map[string]int64{"requester": 45745, "manager": 133701, "readonly": 900002}[who]}
			}
			var res resp
			le.as(tok, func() { res = le.do(c.method, c.path, body) })
			switch {
			case want == 0 && (res.StatusCode < 200 || res.StatusCode > 299):
				t.Errorf("%s %s (%s %s): %d %s", who, name, c.method, c.path, res.StatusCode, res.body)
			case want != 0 && res.StatusCode != want:
				t.Errorf("%s %s (%s %s): %d, want %d: %s", who, name, c.method, c.path, res.StatusCode, want, res.body)
			}
			// A requester never gets paths, refs or download links.
			if who == "requester" && res.StatusCode < 300 {
				body := string(res.body)
				for _, leak := range []string{`"libraryRef"`, `"path"`, "/download?", `"downloadUrl"`, `"rootFolderId"`, `"qualityProfileId"`} {
					if strings.Contains(body, leak) {
						t.Errorf("requester %s leaks %s: %s", name, leak, body)
					}
				}
			}
		}
	}
	// Disabled anonymous access everywhere except sign-up and providers.
	for name, c := range calls {
		if res := le.req(c.method, c.path, c.body, map[string]string{"Authorization": ""}); res.StatusCode != 401 {
			t.Errorf("anonymous %s: %d", name, res.StatusCode)
		}
	}
	var u struct{ Permissions map[string]bool }
	le.expect(le.do("GET", fmt.Sprintf("/api/v1/users/%d", uid), nil), 200).json(&u)
	raw, _ := json.Marshal(u.Permissions)
	if u.Permissions["autoApproveMovies"] || !strings.Contains(string(raw), "autoApprove4k") {
		t.Fatalf("permissions: %s", raw)
	}
}
