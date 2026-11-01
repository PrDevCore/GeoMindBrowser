// Package session owns the lifecycle of remote browser sessions. It holds the
// state machine, the per-user and global caps, and the teardown rules; it never
// stores page content.
package session

import "fmt"

// State is the lifecycle state of a remote session.
type State string

// Lifecycle states. A session is only valid while it is not terminal; only
// ready and active sessions expose a stream URL.
const (
	StateRequested    State = "requested"
	StateProvisioning State = "provisioning"
	StateReady        State = "ready"
	StateActive       State = "active"
	StateTerminating  State = "terminating"
	StateGone         State = "gone"
	StateFailed       State = "failed"
)

// Terminal reports whether no further work is possible in this state.
func (s State) Terminal() bool {
	return s == StateGone || s == StateFailed
}

var transitions = map[State]map[State]bool{
	StateRequested:    {StateProvisioning: true, StateFailed: true, StateTerminating: true},
	StateProvisioning: {StateReady: true, StateActive: true, StateFailed: true, StateTerminating: true},
	StateReady:        {StateActive: true, StateTerminating: true, StateFailed: true},
	StateActive:       {StateTerminating: true, StateFailed: true},
	StateTerminating:  {StateGone: true},
	StateFailed:       {StateTerminating: true},
	StateGone:         {},
}

func canTransition(from, to State) bool {
	return transitions[from][to]
}

func checkTransition(from, to State) error {
	if !canTransition(from, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}
