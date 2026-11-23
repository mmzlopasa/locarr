package requests

import (
	"errors"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/auth"
)

func TestStateMachine(t *testing.T) {
	ok := []struct {
		from Status
		act  Action
		to   Status
	}{
		{StatusPending, ActApprove, StatusApproved},
		{StatusPending, ActDecline, StatusDeclined},
		{StatusPending, ActCancel, StatusCancelled},
		{StatusApproved, ActProcess, StatusProcessing},
		{StatusProcessing, ActPartial, StatusPartial},
		{StatusPartial, ActAvailable, StatusAvailable},
		{StatusProcessing, ActFail, StatusFailed},
		{StatusFailed, ActRetry, StatusProcessing},
		{StatusFailed, ActAvailable, StatusAvailable}, // it arrived after all
		{StatusFailed, ActPartial, StatusPartial},
		{StatusProcessing, ActDecline, StatusDeclined},
		{StatusAvailable, ActPartial, StatusPartial}, // a new season was asked for... (future seasons)
	}
	for _, c := range ok {
		got, err := Next(c.from, c.act)
		if err != nil || got != c.to {
			t.Errorf("%s --%s--> %s, %v; want %s", c.from, c.act, got, err, c.to)
		}
	}
	bad := []struct {
		from Status
		act  Action
	}{
		{StatusApproved, ActCancel}, {StatusProcessing, ActCancel}, {StatusDeclined, ActApprove}, {StatusCancelled, ActApprove},
		{StatusAvailable, ActDecline}, {StatusAvailable, ActFail}, {StatusPending, ActAvailable}, {StatusPending, ActProcess},
		{StatusPartial, ActFail}, {StatusProcessing, ActRetry}, {StatusApproved, ActApprove},
	}
	for _, c := range bad {
		if got, err := Next(c.from, c.act); !errors.Is(err, ErrNotPending) || got != c.from {
			t.Errorf("%s --%s--> %s, %v; want refused", c.from, c.act, got, err)
		}
	}
	for _, s := range []Status{StatusDeclined, StatusCancelled} {
		if s.Open() {
			t.Errorf("%s must not block a new request", s)
		}
	}
	if !StatusFailed.Open() || !StatusAvailable.Open() || !StatusPending.Open() {
		t.Error("open statuses")
	}
}

func TestSeriesAvailability(t *testing.T) {
	facts := map[int]SeasonFacts{
		1: {Number: 1, Aired: 10, Files: 10},
		2: {Number: 2, Aired: 8, Files: 3},
		3: {Number: 3, Aired: 0, Files: 0}, // announced, nothing aired
		4: {Number: 4, Aired: 6, Files: 0},
	}
	cases := []struct {
		seasons []int
		want    Status
	}{
		{[]int{1}, StatusAvailable},
		{[]int{1, 2}, StatusPartial},
		{[]int{1, 4}, StatusPartial},
		{[]int{2}, StatusPartial},
		{[]int{4}, StatusProcessing},
		{[]int{3}, StatusProcessing},
		{[]int{1, 3}, StatusPartial}, // season 3 has not aired: not complete yet
		{[]int{9}, StatusProcessing}, // unknown to the library yet
		{nil, StatusProcessing},
	}
	for _, c := range cases {
		got, per := SeriesStatus(c.seasons, facts)
		if got != c.want {
			t.Errorf("seasons %v: %s, want %s (%v)", c.seasons, got, c.want, per)
		}
	}
	_, per := SeriesStatus([]int{1, 2, 3}, facts)
	if per[1] != StatusAvailable || per[2] != StatusPartial || per[3] != StatusProcessing {
		t.Fatalf("per season: %v", per)
	}
	// More files than aired episodes (a file for an episode that airs later): still available.
	if SeasonStatus(SeasonFacts{Aired: 2, Files: 3}) != StatusAvailable {
		t.Fatal("extra files")
	}
	if TrackStatus(5, 5) != StatusAvailable || TrackStatus(2, 5) != StatusPartial || TrackStatus(0, 5) != StatusProcessing ||
		TrackStatus(0, 0) != StatusProcessing || MovieStatus(true) != StatusAvailable || MovieStatus(false) != StatusProcessing {
		t.Fatal("track or movie status")
	}
	if !Stuck("nothing_found") || !Stuck("all_rejected") || Stuck("downloading") || Stuck("not_available_yet") || Stuck("waiting_delay") {
		t.Fatal("stuck codes")
	}
}

func TestQuota(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	made := []time.Time{now.Add(-10 * day), now.Add(-6 * day), now.Add(-2 * day), now.Add(-time.Hour)}
	u := Compute(Rule{Limit: 3, Days: 7}, made, now)
	if u.Used != 3 || u.Remaining != 0 || u.Unlimited || u.NextSlot == nil || !u.NextSlot.Equal(now.Add(day)) {
		t.Fatalf("usage: %+v next %v", u, u.NextSlot)
	}
	u = Compute(Rule{Limit: 5, Days: 7}, made, now)
	if u.Remaining != 2 || u.NextSlot != nil {
		t.Fatalf("usage: %+v", u)
	}
	u = Compute(Rule{Limit: 0, Days: 7}, made, now)
	if !u.Unlimited || u.Used != 3 {
		t.Fatalf("unlimited: %+v", u)
	}
	u = Compute(Rule{Limit: 1, Days: 30}, made, now)
	if u.Used != 4 || u.Remaining != 0 || !u.NextSlot.Equal(made[3].Add(30*day)) {
		t.Fatalf("over the limit: %+v %v", u, u.NextSlot)
	}
	u = Compute(Rule{Limit: 2, Days: 0}, nil, now) // days defaults to 7
	if u.Days != 7 || u.Remaining != 2 {
		t.Fatalf("defaults: %+v", u)
	}
}

func TestPermissions(t *testing.T) {
	user := func(role auth.Role, perms auth.Permissions) auth.Principal {
		return auth.Principal{User: auth.User{ID: 2, Role: role, Permissions: perms}}
	}
	req := user(auth.RoleRequester, auth.DefaultPermissions(auth.RoleRequester))
	if !CanRequest(req, KindMovie, false) || !CanRequest(req, KindAlbum, false) || CanRequest(req, KindMovie, true) {
		t.Fatal("requester")
	}
	if AutoApproves(req, KindMovie, false) || CanManage(req) || QuotaExempt(req) {
		t.Fatal("requester must wait for approval and count against quotas")
	}
	movies := user(auth.RoleRequester, auth.PermRequestMovies|auth.PermAutoApproveMovies|auth.PermRequest4K)
	if !AutoApproves(movies, KindMovie, false) || AutoApproves(movies, KindSeries, false) || AutoApproves(movies, KindMovie, true) {
		t.Fatal("per-kind auto-approval")
	}
	if CanRequest(movies, KindSeries, false) || !CanRequest(movies, KindMovie, true) {
		t.Fatal("per-kind request")
	}
	all := user(auth.RoleRequester, auth.PermRequestMovies|auth.PermAutoApprove|auth.PermRequest4K|auth.PermAutoApprove4K)
	if !AutoApproves(all, KindSeries, false) || !AutoApproves(all, KindMovie, true) {
		t.Fatal("auto-approve everything")
	}
	if CanRequest(all, KindAlbum, true) {
		t.Fatal("no 4K music")
	}
	mgr := user(auth.RoleManager, auth.DefaultPermissions(auth.RoleManager))
	if !CanManage(mgr) || !QuotaExempt(mgr) || !AutoApproves(mgr, KindAlbum, false) {
		t.Fatal("manager")
	}
	admin := user(auth.RoleAdmin, 0)
	if !CanManage(admin) || !CanRequest(admin, KindMovie, true) || !AutoApproves(admin, KindMovie, true) {
		t.Fatal("admin")
	}
}


func TestSettingsDefaults(t *testing.T) {
	var s Settings
	s.Normalise()
	for _, k := range QuotaKinds {
		if r := s.Quota(k); r.Limit != 0 || r.Days != 7 {
			t.Fatalf("%s: %+v", k, r)
		}
	}
	s.Quotas[QuotaMovie] = Rule{Limit: 3, Days: 0}
	if r := s.Quota(QuotaMovie); r.Limit != 3 || r.Days != 7 {
		t.Fatalf("days default: %+v", r)
	}
	if KindArtist.QuotaKind() != QuotaMusic || KindSeries.QuotaKind() != QuotaSeries || KindMovie.QuotaKind() != QuotaMovie {
		t.Fatal("quota kinds")
	}
	r := Request{Kind: KindAlbum, Title: "Free Album", Subtitle: "Open Source Artist", Year: 2020}
	if r.DisplayTitle() != "Open Source Artist - Free Album (2020)" {
		t.Fatal(r.DisplayTitle())
	}
}
