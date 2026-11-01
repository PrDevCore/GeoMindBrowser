package session

import (
	"context"
	"errors"
	"sync"
)

var (
	errFakeCreate = errors.New("fake runtime create failure")
	errFakeDelete = errors.New("fake runtime delete failure")
	errFakeURL    = errors.New("fake streaming url failure")
	errFakeRevoke = errors.New("fake streaming revoke failure")
)

// FakeRuntime is an in-memory Runtime for tests and for running the control
// plane skeleton without a cluster.
type FakeRuntime struct {
	mu sync.Mutex

	objects        map[string]RuntimeSession
	specs          map[string]Spec
	createErr      error
	deleteErr      error
	deleteFailures int
	createCalls    int
	deleteCalls    map[string]int
	deleted        map[string]int
}

// NewFakeRuntime returns an empty fake runtime.
func NewFakeRuntime() *FakeRuntime {
	return &FakeRuntime{
		objects:     make(map[string]RuntimeSession),
		specs:       make(map[string]Spec),
		deleteCalls: make(map[string]int),
		deleted:     make(map[string]int),
	}
}

// Create records a ready session. A non-nil error can be injected with
// FailNextCreate.
func (f *FakeRuntime) Create(ctx context.Context, spec Spec) (RuntimeSession, error) {
	if err := ctx.Err(); err != nil {
		return RuntimeSession{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if f.createErr != nil {
		err := f.createErr
		f.createErr = nil
		return RuntimeSession{}, err
	}
	if spec.ID == "" {
		return RuntimeSession{}, errors.New("fake runtime: empty id")
	}
	if _, exists := f.objects[spec.ID]; exists {
		return RuntimeSession{}, errors.New("fake runtime: duplicate id")
	}
	obj := RuntimeSession{ID: spec.ID, State: StateReady}
	f.objects[spec.ID] = obj
	f.specs[spec.ID] = spec
	return obj, nil
}

// Get returns a live session or ErrRuntimeNotFound.
func (f *FakeRuntime) Get(ctx context.Context, id string) (RuntimeSession, error) {
	if err := ctx.Err(); err != nil {
		return RuntimeSession{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[id]
	if !ok {
		return RuntimeSession{}, ErrRuntimeNotFound
	}
	return obj, nil
}

// Delete removes a session. Delete is idempotent except that a missing object
// reports ErrRuntimeNotFound, matching the Runtime contract.
func (f *FakeRuntime) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls[id]++
	if f.deleteFailures > 0 {
		f.deleteFailures--
		if f.deleteErr != nil {
			return f.deleteErr
		}
		return errFakeDelete
	}
	if _, ok := f.objects[id]; !ok {
		return ErrRuntimeNotFound
	}
	delete(f.objects, id)
	f.deleted[id]++
	return nil
}

// FailNextCreate makes the next Create call fail.
func (f *FakeRuntime) FailNextCreate(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		err = errFakeCreate
	}
	f.createErr = err
}

// FailDeletes makes the next n Delete calls fail without removing the object.
func (f *FakeRuntime) FailDeletes(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteFailures = n
}

// SetDeleteError replaces the error returned while delete failures remain.
func (f *FakeRuntime) SetDeleteError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteErr = err
}

// Drop removes an object as if the runtime lost it, so reconciliation can be
// tested.
func (f *FakeRuntime) Drop(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, id)
}

// Has reports whether the runtime still holds id.
func (f *FakeRuntime) Has(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[id]
	return ok
}

// Spec returns the spec the runtime received for id.
func (f *FakeRuntime) Spec(id string) (Spec, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	spec, ok := f.specs[id]
	return spec, ok
}

// CreateCalls reports how many Create calls were made.
func (f *FakeRuntime) CreateCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalls
}

// DeleteCalls reports how many Delete calls were made for id.
func (f *FakeRuntime) DeleteCalls(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleteCalls[id]
}

// Deleted reports how many successful deletes were observed for id.
func (f *FakeRuntime) Deleted(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleted[id]
}

// FakeStreaming is an in-memory Streaming implementation for tests.
type FakeStreaming struct {
	mu sync.Mutex

	urls      map[string]string
	owners    map[string]string
	revoked   map[string]int
	urlErr    error
	revokeErr error
}

// NewFakeStreaming returns an empty fake provider.
func NewFakeStreaming() *FakeStreaming {
	return &FakeStreaming{
		urls:    make(map[string]string),
		owners:  make(map[string]string),
		revoked: make(map[string]int),
	}
}

// URL mints a placeholder stream URL. A non-nil error can be injected with
// FailNextURL.
func (f *FakeStreaming) URL(sessionID, userID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.urlErr != nil {
		err := f.urlErr
		f.urlErr = nil
		return "", err
	}
	if !validID(sessionID) || userID == "" {
		return "", errors.New("fake streaming: invalid session or user")
	}
	url := "https://stream.invalid/s/" + sessionID
	f.urls[sessionID] = url
	f.owners[sessionID] = userID
	return url, nil
}

// Revoke records that a stream URL was revoked. A non-nil error can be injected
// with FailNextRevoke.
func (f *FakeStreaming) Revoke(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revokeErr != nil {
		err := f.revokeErr
		f.revokeErr = nil
		return err
	}
	f.revoked[sessionID]++
	return nil
}

// FailNextURL makes the next URL call fail.
func (f *FakeStreaming) FailNextURL(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		err = errFakeURL
	}
	f.urlErr = err
}

// FailNextRevoke makes the next Revoke call fail.
func (f *FakeStreaming) FailNextRevoke(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		err = errFakeRevoke
	}
	f.revokeErr = err
}

// Revoked reports whether sessionID was revoked.
func (f *FakeStreaming) Revoked(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoked[sessionID] > 0
}

// RevokeCalls reports how many successful revocations were recorded.
func (f *FakeStreaming) RevokeCalls(sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoked[sessionID]
}

// Owner reports the user a stream URL was minted for.
func (f *FakeStreaming) Owner(sessionID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.owners[sessionID]
}

// URLFor returns the minted stream URL for sessionID.
func (f *FakeStreaming) URLFor(sessionID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.urls[sessionID]
}
