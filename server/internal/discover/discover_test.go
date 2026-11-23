package discover

import (
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/requests"
)

func TestCombine(t *testing.T) {
	cases := []struct {
		lib  Status
		req  Status
		want Status
	}{
		{"", "", None},
		{"", Pending, Pending},
		{"", Processing, Processing},
		{Processing, "", InLibrary},    // added by an administrator, nobody asked
		{Processing, Pending, Pending}, // in the library without files, someone asked
		{Partial, Pending, Partial},    // what is here wins
		{Available, Pending, Available},
		{Available, "", Available},
	}
	for _, c := range cases {
		if got := combine(libState{status: c.lib}, c.req); got != c.want {
			t.Errorf("library %q, request %q: %s, want %s", c.lib, c.req, got, c.want)
		}
	}
	for st, want := range map[requests.Status]Status{requests.StatusPending: Pending, requests.StatusApproved: Processing,
		requests.StatusProcessing: Processing, requests.StatusFailed: Processing, requests.StatusPartial: Partial,
		requests.StatusAvailable: Available} {
		if got := requestStatus(st); got != want {
			t.Errorf("%s: %s, want %s", st, got, want)
		}
	}
}
