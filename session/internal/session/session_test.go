package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{at: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func newTestManager(t *testing.T) (*Manager, *FakeRuntime, *FakeStreaming, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	runtime := NewFakeRuntime()
	streaming := NewFakeStreaming()
	manager, err := New(Options{
		Runtime:     runtime,
		Streaming:   streaming,
		Regions:     []string{"de-test", "us-test"},
		MaxPerUser:  2,
		MaxTotal:    5,
		DefaultTTL:  30 * time.Minute,
		DefaultIdle: 5 * time.Minute,
		CallTimeout: time.Second,
		Now:         clock.Now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return manager, runtime, streaming, clock
}

func TestCreateProvisionsReadySession(t *testing.T) {
	manager, runtime, streaming, _ := newTestManager(t)

	record, err := manager.Create(context.Background(), "alice", Spec{Region: "de-test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if record.State != StateReady {
		t.Fatalf("state = %q, want %q", record.State, StateReady)
	}
	if record.ID == "" || !runtime.Has(record.ID) {
		t.Fatalf("runtime does not hold session %q", record.ID)
	}
	if record.StreamURL != streaming.URLFor(record.ID) || record.StreamURL == "" {
		t.Fatalf("stream URL = %q, want the minted URL", record.StreamURL)
	}
	if got := streaming.Owner(record.ID); got != "alice" {
		t.Fatalf("stream owner = %q, want alice", got)
	}
	if got := record.ExpiresAt.Sub(record.CreatedAt); got != 30*time.Minute {
		t.Fatalf("ttl = %s, want 30m", got)
	}

	spec, ok := runtime.Spec(record.ID)
	if !ok {
		t.Fatal("runtime did not record the spec")
	}
	if spec.TTL != 30*time.Minute || spec.Idle != 5*time.Minute {
		t.Fatalf("spec ttl/idle = %s/%s, want 30m/5m", spec.TTL, spec.Idle)
	}
	if spec.CPU != "500m" || spec.Memory != "512Mi" || spec.Egress != "session-proxy" {
		t.Fatalf("spec resources = %q/%q/%q", spec.CPU, spec.Memory, spec.Egress)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	manager, runtime, _, _ := newTestManager(t)
	ctx := context.Background()

	if _, err := manager.Create(ctx, "", Spec{Region: "de-test"}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("empty owner error = %v, want ErrInvalidSpec", err)
	}
	if _, err := manager.Create(ctx, "alice", Spec{Region: "xx-test"}); !errors.Is(err, ErrInvalidRegion) {
		t.Fatalf("unknown region error = %v, want ErrInvalidRegion", err)
	}
	if _, err := manager.Create(ctx, "alice", Spec{Region: "DE-TEST"}); !errors.Is(err, ErrInvalidRegion) {
		t.Fatalf("case-mismatched region error = %v, want ErrInvalidRegion", err)
	}
	if _, err := manager.Create(ctx, "alice", Spec{Region: "de-test", CPU: "a\x00b"}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("control characters error = %v, want ErrInvalidSpec", err)
	}
	if calls := runtime.CreateCalls(); calls != 0 {
		t.Fatalf("runtime Create calls = %d, want 0", calls)
	}
}

func TestCreateClampsSpec(t *testing.T) {
	manager, _, _, _ := newTestManager(t)

	record, err := manager.Create(context.Background(), "alice", Spec{
		Region: "de-test",
		TTL:    48 * time.Hour,
		Idle:   2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := record.ExpiresAt.Sub(record.CreatedAt); got != time.Hour {
		t.Fatalf("ttl = %s, want the 1h maximum", got)
	}
	if record.IdleTimeout != 30*time.Minute {
		t.Fatalf("idle = %s, want the 30m maximum", record.IdleTimeout)
	}
}

func TestIdleClampsToTTL(t *testing.T) {
	manager, _, _, _ := newTestManager(t)

	record, err := manager.Create(context.Background(), "alice", Spec{
		Region: "de-test",
		TTL:    10 * time.Minute,
		Idle:   20 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if record.IdleTimeout != 10*time.Minute {
		t.Fatalf("idle = %s, want it clamped to the 10m ttl", record.IdleTimeout)
	}
}

func TestCreateEnforcesCaps(t *testing.T) {
	manager, runtime, _, _ := newTestManager(t)
	ctx := context.Background()

	first, err := manager.Create(ctx, "alice", Spec{Region: "de-test"})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := manager.Create(ctx, "alice", Spec{Region: "de-test"}); err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if _, err := manager.Create(ctx, "alice", Spec{Region: "de-test"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("third Create error = %v, want ErrCapacity", err)
	}

	for _, owner := range []string{"bob", "carol", "dave"} {
		if _, err := manager.Create(ctx, owner, Spec{Region: "de-test"}); err != nil {
			t.Fatalf("Create for %s: %v", owner, err)
		}
	}
	if _, err := manager.Create(ctx, "erin", Spec{Region: "de-test"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("global cap error = %v, want ErrCapacity", err)
	}

	if err := manager.Delete(ctx, "alice", first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := manager.Create(ctx, "alice", Spec{Region: "de-test"}); err != nil {
		t.Fatalf("Create after freeing capacity: %v", err)
	}
	if calls := runtime.CreateCalls(); calls != 6 {
		t.Fatalf("runtime Create calls = %d, want 6", calls)
	}
}

func TestCreateFailureDoesNotLeakCapacity(t *testing.T) {
	manager, runtime, _, _ := newTestManager(t)
	manager.opts.MaxTotal = 1
	ctx := context.Background()

	runtime.FailNextCreate(nil)
	if _, err := manager.Create(ctx, "alice", Spec{Region: "de-test"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Create error = %v, want ErrUnavailable", err)
	}
	if _, err := manager.Create(ctx, "alice", Spec{Region: "de-test"}); err != nil {
		t.Fatalf("Create after failure: %v", err)
	}
}

func TestStreamingFailureTearsDownRuntime(t *testing.T) {
	runtime := NewFakeRuntime()
	streaming := NewFakeStreaming()
	manager, err := New(Options{
		Runtime:     runtime,
		Streaming:   streaming,
		Regions:     []string{"de-test"},
		CallTimeout: time.Second,
		NewID:       func() (string, error) { return "ses_fixed00001", nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	streaming.FailNextURL(nil)

	if _, err := manager.Create(context.Background(), "alice", Spec{Region: "de-test"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Create error = %v, want ErrUnavailable", err)
	}
	if runtime.Has("ses_fixed00001") {
		t.Fatal("runtime object was not torn down")
	}
	if calls := runtime.DeleteCalls("ses_fixed00001"); calls != 1 {
		t.Fatalf("runtime Delete calls = %d, want 1", calls)
	}
}

func TestTTLExpiryTearsDown(t *testing.T) {
	manager, runtime, streaming, clock := newTestManager(t)
	ctx := context.Background()

	record, err := manager.Create(ctx, "alice", Spec{Region: "de-test", TTL: 10 * time.Minute, Idle: 10 * time.Minute})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	clock.Advance(9 * time.Minute)
	if handled := manager.Sweep(ctx); handled != 0 {
		t.Fatalf("Sweep handled = %d before expiry, want 0", handled)
	}
	clock.Advance(2 * time.Minute)
	if handled := manager.Sweep(ctx); handled != 1 {
		t.Fatalf("Sweep handled = %d at expiry, want 1", handled)
	}
	if _, err := manager.Get("alice", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after expiry error = %v, want ErrNotFound", err)
	}
	if runtime.Has(record.ID) {
		t.Fatal("runtime object survived ttl expiry")
	}
	if !streaming.Revoked(record.ID) {
		t.Fatal("stream URL was not revoked")
	}
}

func TestIdleExpiryAndTouch(t *testing.T) {
	manager, _, _, clock := newTestManager(t)
	ctx := context.Background()

	record, err := manager.Create(ctx, "alice", Spec{Region: "de-test", Idle: time.Minute})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	clock.Advance(45 * time.Second)
	if err := manager.Touch("alice", record.ID); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	active, err := manager.Get("alice", record.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if active.State != StateActive {
		t.Fatalf("state after Touch = %q, want active", active.State)
	}
	clock.Advance(45 * time.Second)
	manager.Sweep(ctx)
	if _, err := manager.Get("alice", record.ID); err != nil {
		t.Fatalf("session was reaped despite activity: %v", err)
	}
	clock.Advance(20 * time.Second)
	manager.Sweep(ctx)
	if _, err := manager.Get("alice", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after idle expiry error = %v, want ErrNotFound", err)
	}
}

func TestOwnershipIsEnforced(t *testing.T) {
	manager, runtime, _, _ := newTestManager(t)
	ctx := context.Background()

	record, err := manager.Create(ctx, "alice", Spec{Region: "de-test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := manager.Touch("bob", record.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Touch error = %v, want ErrForbidden", err)
	}
	if _, err := manager.Get("bob", record.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Get error = %v, want ErrForbidden", err)
	}
	if err := manager.Delete(ctx, "bob", record.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Delete error = %v, want ErrForbidden", err)
	}
	if !runtime.Has(record.ID) {
		t.Fatal("another user's Delete removed the runtime object")
	}
	if _, err := manager.Get("alice", record.ID); err != nil {
		t.Fatalf("owner lost the session: %v", err)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	manager, runtime, streaming, _ := newTestManager(t)
	ctx := context.Background()

	record, err := manager.Create(ctx, "alice", Spec{Region: "de-test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := manager.Delete(ctx, "alice", record.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if runtime.Has(record.ID) {
		t.Fatal("runtime object survived Delete")
	}
	if !streaming.Revoked(record.ID) {
		t.Fatal("stream URL was not revoked")
	}
	if _, err := manager.Get("alice", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrNotFound", err)
	}
	if err := manager.Delete(ctx, "alice", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete error = %v, want ErrNotFound", err)
	}
}

func TestTeardownRetriesUntilGone(t *testing.T) {
	manager, runtime, _, _ := newTestManager(t)
	ctx := context.Background()

	record, err := manager.Create(ctx, "alice", Spec{Region: "de-test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	runtime.FailDeletes(2)
	if err := manager.Delete(ctx, "alice", record.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Delete error = %v, want ErrUnavailable", err)
	}
	record, err = manager.Get("alice", record.ID)
	if err != nil {
		t.Fatalf("Get after failed teardown: %v", err)
	}
	if record.State != StateTerminating {
		t.Fatalf("state = %q, want terminating", record.State)
	}
	manager.Sweep(ctx)
	if record, err = manager.Get("alice", record.ID); err != nil {
		t.Fatalf("session disappeared before the retry succeeded: %v", err)
	}
	if record.State != StateTerminating {
		t.Fatalf("state after failed retry = %q, want terminating", record.State)
	}
	manager.Sweep(ctx)
	if _, err := manager.Get("alice", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after retried teardown error = %v, want ErrNotFound", err)
	}
	if runtime.Has(record.ID) {
		t.Fatal("runtime object still present")
	}
}

func TestSweepReconcilesExternalRemoval(t *testing.T) {
	manager, runtime, streaming, _ := newTestManager(t)
	ctx := context.Background()

	record, err := manager.Create(ctx, "alice", Spec{Region: "de-test"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	runtime.Drop(record.ID)
	manager.Sweep(ctx)
	if _, err := manager.Get("alice", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after external removal error = %v, want ErrNotFound", err)
	}
	if streaming.RevokeCalls(record.ID) != 1 {
		t.Fatalf("revoke calls = %d, want 1", streaming.RevokeCalls(record.ID))
	}
}

func TestConcurrentCreatesRespectCap(t *testing.T) {
	manager, _, _, _ := newTestManager(t)
	manager.opts.MaxPerUser = 3
	manager.opts.MaxTotal = 100

	var wg sync.WaitGroup
	var mu sync.Mutex
	created, capped, other := 0, 0, 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.Create(context.Background(), "alice", Spec{Region: "de-test"})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case errors.Is(err, ErrCapacity):
				capped++
			default:
				other++
			}
		}()
	}
	wg.Wait()

	if created != 3 {
		t.Fatalf("created = %d, want 3", created)
	}
	if capped != 17 {
		t.Fatalf("capped = %d, want 17", capped)
	}
	if other != 0 {
		t.Fatalf("unexpected errors = %d", other)
	}
}

func TestStateTransitions(t *testing.T) {
	allowed := []struct{ from, to State }{
		{StateRequested, StateProvisioning},
		{StateProvisioning, StateReady},
		{StateReady, StateActive},
		{StateActive, StateTerminating},
		{StateTerminating, StateGone},
		{StateProvisioning, StateFailed},
	}
	for _, tc := range allowed {
		if !canTransition(tc.from, tc.to) {
			t.Errorf("transition %s -> %s should be allowed", tc.from, tc.to)
		}
	}
	rejected := []struct{ from, to State }{
		{StateGone, StateReady},
		{StateActive, StateReady},
		{StateReady, StateProvisioning},
		{StateRequested, StateReady},
		{StateFailed, StateActive},
	}
	for _, tc := range rejected {
		if canTransition(tc.from, tc.to) {
			t.Errorf("transition %s -> %s should be rejected", tc.from, tc.to)
		}
		if err := checkTransition(tc.from, tc.to); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("checkTransition(%s, %s) = %v, want ErrInvalidTransition", tc.from, tc.to, err)
		}
	}
}

func TestNewValidatesOptions(t *testing.T) {
	valid := Options{
		Runtime:   NewFakeRuntime(),
		Streaming: NewFakeStreaming(),
		Regions:   []string{"de-test"},
	}
	if _, err := New(Options{Streaming: valid.Streaming, Regions: valid.Regions}); err == nil {
		t.Fatal("New accepted a nil runtime")
	}
	if _, err := New(Options{Runtime: valid.Runtime, Regions: valid.Regions}); err == nil {
		t.Fatal("New accepted a nil streaming provider")
	}
	if _, err := New(Options{Runtime: valid.Runtime, Streaming: valid.Streaming}); err == nil {
		t.Fatal("New accepted an empty region list")
	}
}
