package requests

import "fmt"

// Action moves a request from one status to another.
type Action string

// Actions.
const (
	ActApprove   Action = "approve"   // a manager (or auto-approval) said yes
	ActDecline   Action = "decline"   // a manager said no
	ActCancel    Action = "cancel"    // the requester withdrew it
	ActProcess   Action = "process"   // the library has it and is getting it
	ActPartial   Action = "partial"   // some of it is in the library
	ActAvailable Action = "available" // all of it is in the library
	ActFail      Action = "fail"      // the library gave up for now
	ActRetry     Action = "retry"     // a manager asked to search again
)

// transitions is the state machine (D-054):
//
//	pending ─approve→ approved ─process→ processing ─partial→ partial ─available→ available
//	pending ─decline/cancel→ declined/cancelled
//	approved/processing ─fail→ failed ─retry→ processing (and it may still arrive: failed → partial/available)
var transitions = map[Action]map[Status]Status{
	ActApprove: {StatusPending: StatusApproved},
	ActDecline: {StatusPending: StatusDeclined, StatusApproved: StatusDeclined, StatusProcessing: StatusDeclined,
		StatusPartial: StatusDeclined, StatusFailed: StatusDeclined},
	ActCancel:  {StatusPending: StatusCancelled},
	ActProcess: {StatusApproved: StatusProcessing, StatusProcessing: StatusProcessing, StatusFailed: StatusProcessing},
	ActPartial: {StatusApproved: StatusPartial, StatusProcessing: StatusPartial, StatusPartial: StatusPartial,
		StatusFailed: StatusPartial, StatusAvailable: StatusPartial},
	ActAvailable: {StatusApproved: StatusAvailable, StatusProcessing: StatusAvailable, StatusPartial: StatusAvailable,
		StatusFailed: StatusAvailable, StatusAvailable: StatusAvailable},
	ActFail:  {StatusApproved: StatusFailed, StatusProcessing: StatusFailed, StatusFailed: StatusFailed},
	ActRetry: {StatusFailed: StatusProcessing},
}

// Next returns the status after a, or ErrNotPending when a does not apply.
func Next(from Status, a Action) (Status, error) {
	if to, ok := transitions[a][from]; ok {
		return to, nil
	}
	return from, fmt.Errorf("%w: cannot %s a request that is %s", ErrNotPending, a, from)
}

// Final reports a status nothing more happens to.
func (s Status) Final() bool {
	return s == StatusDeclined || s == StatusCancelled || s == StatusAvailable
}

// Open reports a status that still blocks a duplicate request.
func (s Status) Open() bool { return s != StatusDeclined && s != StatusCancelled }

// Tracked reports a status the library sync looks at.
func (s Status) Tracked() bool {
	return s == StatusApproved || s == StatusProcessing || s == StatusPartial || s == StatusFailed || s == StatusAvailable
}

// actionFor maps a computed library status to the action that reaches it.
func actionFor(s Status) Action {
	switch s {
	case StatusAvailable:
		return ActAvailable
	case StatusPartial:
		return ActPartial
	case StatusFailed:
		return ActFail
	}
	return ActProcess
}
