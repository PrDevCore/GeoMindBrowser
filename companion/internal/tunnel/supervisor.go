// Package tunnel owns the WireGuard device and its health supervisor.
package tunnel

import (
	"sync"
	"time"
)

// State is the tunnel state exposed to the rest of the companion.
type State string

// Tunnel states. Anything other than StateUp is treated as unhealthy.
const (
	StateDown     State = "down"
	StateStarting State = "starting"
	StateUp       State = "up"
)

// DefaultStaleAfter is the handshake age after which the tunnel is unhealthy.
const DefaultStaleAfter = 3 * time.Minute

// Status is a snapshot of the supervisor state.
type Status struct {
	State  State
	Exit   string
	Region string
	Since  time.Time
}

// Supervisor tracks WireGuard handshake freshness. A tunnel that has not
// completed a handshake within staleAfter is unhealthy, so callers fail closed.
type Supervisor struct {
	mu            sync.RWMutex
	staleAfter    time.Duration
	now           func() time.Time
	state         State
	exit          string
	region        string
	since         time.Time
	lastHandshake time.Time
}

// NewSupervisor returns a supervisor in the down state. A non-positive
// staleAfter uses DefaultStaleAfter; a nil clock uses time.Now.
func NewSupervisor(staleAfter time.Duration, now func() time.Time) *Supervisor {
	if staleAfter <= 0 {
		staleAfter = DefaultStaleAfter
	}
	if now == nil {
		now = time.Now
	}
	return &Supervisor{
		staleAfter: staleAfter,
		now:        now,
		state:      StateDown,
		since:      now(),
	}
}

// MarkStarting records that a tunnel is being established. It is not healthy.
func (s *Supervisor) MarkStarting(exit, region string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateStarting
	s.exit = exit
	s.region = region
	s.since = s.now()
	s.lastHandshake = time.Time{}
}

// MarkHandshake records a completed handshake. The tunnel becomes healthy
// until staleAfter elapses without another handshake.
func (s *Supervisor) MarkHandshake(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at.IsZero() {
		at = s.now()
	}
	if s.state != StateUp {
		s.since = s.now()
	}
	s.state = StateUp
	s.lastHandshake = at
}

// MarkDown records that the tunnel is down and must not carry traffic.
func (s *Supervisor) MarkDown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateDown
	s.since = s.now()
	s.lastHandshake = time.Time{}
}

// Healthy reports whether the tunnel may carry traffic right now.
func (s *Supervisor) Healthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.effectiveStateLocked() == StateUp
}

// Status returns a consistent snapshot. A stale tunnel reports down.
func (s *Supervisor) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Status{
		State:  s.effectiveStateLocked(),
		Exit:   s.exit,
		Region: s.region,
		Since:  s.since,
	}
}

func (s *Supervisor) effectiveStateLocked() State {
	if s.state != StateUp {
		return s.state
	}
	if s.now().Sub(s.lastHandshake) > s.staleAfter {
		return StateDown
	}
	return StateUp
}
