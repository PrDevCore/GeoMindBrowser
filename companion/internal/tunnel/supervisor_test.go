package tunnel

import (
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
}

func TestSupervisorStartsDown(t *testing.T) {
	clock := newClock()
	s := NewSupervisor(time.Minute, clock.now)
	if s.Healthy() {
		t.Fatal("new supervisor must be unhealthy")
	}
	status := s.Status()
	if status.State != StateDown || !status.Since.Equal(clock.now()) {
		t.Fatalf("status = %+v", status)
	}
}

func TestSupervisorHandshakeAndStaleness(t *testing.T) {
	clock := newClock()
	s := NewSupervisor(time.Minute, clock.now)

	s.MarkStarting("de-1", "de")
	if s.Healthy() {
		t.Fatal("starting tunnel must not be healthy")
	}
	if status := s.Status(); status.State != StateStarting || status.Exit != "de-1" || status.Region != "de" {
		t.Fatalf("status = %+v", status)
	}

	s.MarkHandshake(clock.now())
	if !s.Healthy() {
		t.Fatal("fresh handshake must be healthy")
	}
	if status := s.Status(); status.State != StateUp {
		t.Fatalf("status = %+v", status)
	}

	clock.add(time.Minute)
	if !s.Healthy() {
		t.Fatal("tunnel at the staleness boundary is still healthy")
	}
	clock.add(time.Nanosecond)
	if s.Healthy() {
		t.Fatal("stale tunnel must be unhealthy")
	}
	if status := s.Status(); status.State != StateDown {
		t.Fatalf("stale status = %+v", status)
	}

	s.MarkHandshake(clock.now())
	if !s.Healthy() {
		t.Fatal("a new handshake must restore health")
	}
}

func TestSupervisorMarkDown(t *testing.T) {
	clock := newClock()
	s := NewSupervisor(time.Minute, clock.now)
	s.MarkHandshake(clock.now())
	clock.add(time.Second)
	s.MarkDown()
	if s.Healthy() || s.Status().State != StateDown {
		t.Fatal("MarkDown must make the tunnel unhealthy")
	}
	if !s.Status().Since.Equal(clock.now()) {
		t.Fatalf("since = %v", s.Status().Since)
	}
}

func TestSupervisorZeroStaleUsesDefault(t *testing.T) {
	clock := newClock()
	s := NewSupervisor(0, clock.now)
	if s.staleAfter != DefaultStaleAfter {
		t.Fatalf("staleAfter = %v", s.staleAfter)
	}
	s.MarkHandshake(clock.now())
	clock.add(DefaultStaleAfter + time.Second)
	if s.Healthy() {
		t.Fatal("expected stale tunnel")
	}
}

func TestSupervisorConcurrentAccess(_ *testing.T) {
	s := NewSupervisor(time.Minute, nil)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.MarkHandshake(time.Now())
				_ = s.Healthy()
				_ = s.Status()
			}
		}()
	}
	wg.Wait()
}
