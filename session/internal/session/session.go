package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

// Errors returned by the manager. Handlers map them to HTTP statuses without
// exposing internal detail.
var (
	ErrNotFound          = errors.New("session not found")
	ErrForbidden         = errors.New("session belongs to another user")
	ErrCapacity          = errors.New("session capacity reached")
	ErrInvalidRegion     = errors.New("region not allowed")
	ErrInvalidSpec       = errors.New("invalid session spec")
	ErrUnavailable       = errors.New("session service unavailable")
	ErrInvalidTransition = errors.New("invalid session transition")
)

// ErrRuntimeNotFound reports that the runtime no longer holds a session. The
// control plane treats it as already torn down.
var ErrRuntimeNotFound = errors.New("runtime object not found")

// Spec describes the session a caller wants. ID is chosen by the control plane
// so caps and teardown can be accounted for before provisioning starts.
type Spec struct {
	ID     string
	Region string
	TTL    time.Duration
	Idle   time.Duration
	CPU    string
	Memory string
	Egress string
}

// RuntimeSession is the runtime's view of one provisioned session. StreamURL is
// optional; the control plane mints its own URL through Streaming.
type RuntimeSession struct {
	ID        string
	State     State
	StreamURL string
}

// Runtime provisions and destroys session workloads. Implementations must be
// safe for concurrent use. Create returns either ready or active and must honor
// ctx cancellation. Delete is idempotent and returns ErrRuntimeNotFound when
// the workload is already gone.
type Runtime interface {
	Create(ctx context.Context, spec Spec) (RuntimeSession, error)
	Get(ctx context.Context, id string) (RuntimeSession, error)
	Delete(ctx context.Context, id string) error
}

// Streaming mints short-lived, user-bound stream URLs and revokes them on
// teardown. Both methods must be safe for concurrent use.
type Streaming interface {
	URL(sessionID, userID string) (string, error)
	Revoke(ctx context.Context, sessionID string) error
}

// Record is the control plane's state for one session. It contains no page
// content and no browsing data.
type Record struct {
	ID          string
	Owner       string
	Region      string
	State       State
	StreamURL   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastActive  time.Time
	IdleTimeout time.Duration
}

// Options configures a Manager. Zero values are replaced by defaults.
type Options struct {
	Runtime          Runtime
	Streaming        Streaming
	Regions          []string
	MaxPerUser       int
	MaxTotal         int
	DefaultTTL       time.Duration
	MaxTTL           time.Duration
	DefaultIdle      time.Duration
	MaxIdle          time.Duration
	CPU              string
	Memory           string
	Egress           string
	CallTimeout      time.Duration
	CreateTimeout    time.Duration
	ProvisionTimeout time.Duration
	Now              func() time.Time
	NewID            func() (string, error)
	Logger           *slog.Logger
}

// Manager owns the session state machine. It never stores page content.
type Manager struct {
	opts   Options
	rt     Runtime
	stream Streaming
	log    *slog.Logger

	mu       sync.Mutex
	sessions map[string]*Record
}

// New validates op and returns a Manager.
func New(opts Options) (*Manager, error) {
	if opts.Runtime == nil {
		return nil, errors.New("session: runtime is required")
	}
	if opts.Streaming == nil {
		return nil, errors.New("session: streaming provider is required")
	}
	if len(opts.Regions) == 0 {
		return nil, errors.New("session: at least one region is required")
	}
	if opts.MaxPerUser <= 0 {
		opts.MaxPerUser = 2
	}
	if opts.MaxTotal <= 0 {
		opts.MaxTotal = 20
	}
	if opts.MaxTotal < opts.MaxPerUser {
		opts.MaxTotal = opts.MaxPerUser
	}
	if opts.DefaultTTL <= 0 {
		opts.DefaultTTL = 30 * time.Minute
	}
	if opts.MaxTTL <= 0 {
		opts.MaxTTL = time.Hour
	}
	if opts.DefaultTTL > opts.MaxTTL {
		opts.DefaultTTL = opts.MaxTTL
	}
	if opts.DefaultIdle <= 0 {
		opts.DefaultIdle = 5 * time.Minute
	}
	if opts.MaxIdle <= 0 {
		opts.MaxIdle = 30 * time.Minute
	}
	if opts.DefaultIdle > opts.MaxIdle {
		opts.DefaultIdle = opts.MaxIdle
	}
	if opts.CPU == "" {
		opts.CPU = "500m"
	}
	if opts.Memory == "" {
		opts.Memory = "512Mi"
	}
	if opts.Egress == "" {
		opts.Egress = "session-proxy"
	}
	if opts.CallTimeout <= 0 {
		opts.CallTimeout = 20 * time.Second
	}
	if opts.CreateTimeout <= 0 {
		opts.CreateTimeout = 2 * time.Minute
	}
	if opts.ProvisionTimeout <= 0 {
		opts.ProvisionTimeout = 2 * time.Minute
	}
	if opts.ProvisionTimeout < opts.CreateTimeout {
		opts.ProvisionTimeout = opts.CreateTimeout
	}
	if opts.ProvisionTimeout < opts.CallTimeout {
		opts.ProvisionTimeout = opts.CallTimeout
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NewID == nil {
		opts.NewID = randomID
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Manager{
		opts:     opts,
		rt:       opts.Runtime,
		stream:   opts.Streaming,
		log:      opts.Logger,
		sessions: make(map[string]*Record),
	}, nil
}

// Create provisions a session for owner. It enforces region allowlists,
// per-user and global caps, and returns a ready session with a stream URL.
func (m *Manager) Create(ctx context.Context, owner string, spec Spec) (Record, error) {
	owner = strings.TrimSpace(owner)
	if !validOwner(owner) {
		return Record{}, ErrInvalidSpec
	}
	var err error
	spec, err = m.normalizeSpec(spec)
	if err != nil {
		return Record{}, err
	}
	id, err := m.opts.NewID()
	if err != nil {
		return Record{}, fmt.Errorf("%w: id generation failed", ErrUnavailable)
	}
	if !validID(id) {
		return Record{}, fmt.Errorf("%w: generated id is invalid", ErrUnavailable)
	}
	spec.ID = id

	now := m.now()
	rec := &Record{
		ID:          id,
		Owner:       owner,
		Region:      spec.Region,
		State:       StateRequested,
		CreatedAt:   now,
		ExpiresAt:   now.Add(spec.TTL),
		LastActive:  now,
		IdleTimeout: spec.Idle,
	}

	m.mu.Lock()
	if m.countLocked(owner) >= m.opts.MaxPerUser || len(m.sessions) >= m.opts.MaxTotal {
		m.mu.Unlock()
		return Record{}, ErrCapacity
	}
	if _, exists := m.sessions[id]; exists {
		m.mu.Unlock()
		return Record{}, fmt.Errorf("%w: duplicate id", ErrUnavailable)
	}
	m.sessions[id] = rec
	if err := transitionLocked(rec, StateProvisioning); err != nil {
		delete(m.sessions, id)
		m.mu.Unlock()
		return Record{}, err
	}
	m.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, m.opts.CreateTimeout)
	res, err := m.rt.Create(callCtx, spec)
	cancel()
	if err != nil {
		m.abandon(id)
		m.deleteRuntime(id)
		return Record{}, runtimeFailure(err)
	}
	if err := ctx.Err(); err != nil {
		m.abandon(id)
		m.deleteRuntime(id)
		return Record{}, fmt.Errorf("%w: request cancelled during provisioning", ErrUnavailable)
	}
	if res.ID != id {
		m.abandon(id)
		m.deleteRuntime(id)
		return Record{}, fmt.Errorf("%w: runtime returned a different id", ErrUnavailable)
	}
	if res.State != StateReady && res.State != StateActive {
		m.abandon(id)
		m.deleteRuntime(id)
		return Record{}, fmt.Errorf("%w: runtime returned state %q", ErrUnavailable, res.State)
	}
	url, err := m.stream.URL(id, owner)
	if err != nil {
		m.abandon(id)
		m.deleteRuntime(id)
		return Record{}, fmt.Errorf("%w: streaming unavailable", ErrUnavailable)
	}

	m.mu.Lock()
	cur, ok := m.sessions[id]
	if !ok || cur.State != StateProvisioning {
		m.mu.Unlock()
		m.deleteRuntime(id)
		return Record{}, fmt.Errorf("%w: session was cancelled during provisioning", ErrUnavailable)
	}
	if err := transitionLocked(cur, res.State); err != nil {
		m.mu.Unlock()
		m.deleteRuntime(id)
		return Record{}, err
	}
	cur.StreamURL = url
	out := *cur
	m.mu.Unlock()
	return out, nil
}

// Get returns the current record for owner. A record owned by another user is
// reported as ErrForbidden; handlers surface both that and ErrNotFound as 404.
func (m *Manager) Get(owner, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	if rec.Owner != owner {
		return Record{}, ErrForbidden
	}
	return *rec, nil
}

// Touch marks user activity on a live session. It moves a ready session to
// active so idle timeouts do not reap a session that is being streamed.
func (m *Manager) Touch(owner, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	if rec.Owner != owner {
		return ErrForbidden
	}
	if rec.State != StateReady && rec.State != StateActive {
		return nil
	}
	if err := transitionLocked(rec, StateActive); err != nil {
		return err
	}
	rec.LastActive = m.now()
	return nil
}

// Delete tears a session down. Deleting a session that is already gone returns
// ErrNotFound, which the HTTP layer treats as an idempotent success. A session
// owned by another user is never touched.
func (m *Manager) Delete(ctx context.Context, owner, id string) error {
	m.mu.Lock()
	rec, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	if rec.Owner != owner {
		m.mu.Unlock()
		return ErrForbidden
	}
	state := rec.State
	m.mu.Unlock()

	if state.Terminal() {
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		return nil
	}
	return m.teardown(ctx, id)
}

// Sweep expires sessions by TTL and idle time, reconciles runtime state, and
// retries failed teardowns. It returns the number of sessions it tore down.
func (m *Manager) Sweep(ctx context.Context) int {
	now := m.now()
	var reconcile, teardown []string

	m.mu.Lock()
	for id, rec := range m.sessions {
		switch rec.State {
		case StateTerminating:
			teardown = append(teardown, id)
		case StateRequested, StateProvisioning:
			if now.Sub(rec.CreatedAt) >= m.opts.ProvisionTimeout {
				if transitionLocked(rec, StateTerminating) == nil {
					teardown = append(teardown, id)
				}
			}
		case StateReady, StateActive:
			expired := !rec.ExpiresAt.IsZero() && !now.Before(rec.ExpiresAt)
			idle := rec.IdleTimeout > 0 && now.Sub(rec.LastActive) >= rec.IdleTimeout
			if expired || idle {
				if transitionLocked(rec, StateTerminating) == nil {
					teardown = append(teardown, id)
				}
				continue
			}
			reconcile = append(reconcile, id)
		}
	}
	m.mu.Unlock()

	for _, id := range reconcile {
		m.reconcile(ctx, id)
	}
	for _, id := range teardown {
		if err := m.teardown(ctx, id); err != nil {
			m.log.Warn("session teardown failed", "session", id, "err", err)
		}
	}
	return len(teardown)
}

// RunSweeper runs Sweep on a fixed interval until ctx is done.
func (m *Manager) RunSweeper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Sweep(ctx)
		}
	}
}

func (m *Manager) normalizeSpec(spec Spec) (Spec, error) {
	if !slices.Contains(m.opts.Regions, spec.Region) {
		return Spec{}, ErrInvalidRegion
	}
	if spec.TTL <= 0 {
		spec.TTL = m.opts.DefaultTTL
	}
	if spec.TTL > m.opts.MaxTTL {
		spec.TTL = m.opts.MaxTTL
	}
	if spec.Idle <= 0 {
		spec.Idle = m.opts.DefaultIdle
	}
	if spec.Idle > m.opts.MaxIdle {
		spec.Idle = m.opts.MaxIdle
	}
	if spec.Idle > spec.TTL {
		spec.Idle = spec.TTL
	}
	if spec.CPU == "" {
		spec.CPU = m.opts.CPU
	}
	if spec.Memory == "" {
		spec.Memory = m.opts.Memory
	}
	if spec.Egress == "" {
		spec.Egress = m.opts.Egress
	}
	for _, field := range []string{spec.CPU, spec.Memory, spec.Egress} {
		if !validField(field) {
			return Spec{}, ErrInvalidSpec
		}
	}
	return spec, nil
}

func (m *Manager) reconcile(ctx context.Context, id string) {
	m.mu.Lock()
	rec, ok := m.sessions[id]
	if !ok || (rec.State != StateReady && rec.State != StateActive) {
		m.mu.Unlock()
		return
	}
	local := rec.State
	m.mu.Unlock()

	callCtx, cancel := m.callContext(ctx)
	res, err := m.rt.Get(callCtx, id)
	cancel()
	if err != nil {
		if !errors.Is(err, ErrRuntimeNotFound) {
			return
		}
		m.mu.Lock()
		if cur, ok := m.sessions[id]; ok && cur.State == local {
			_ = transitionLocked(cur, StateTerminating)
		}
		m.mu.Unlock()
		if err := m.teardown(ctx, id); err != nil {
			m.log.Warn("session teardown failed", "session", id, "err", err)
		}
		return
	}
	if res.State == StateFailed {
		m.mu.Lock()
		if cur, ok := m.sessions[id]; ok && cur.State == local {
			_ = transitionLocked(cur, StateTerminating)
		}
		m.mu.Unlock()
		if err := m.teardown(ctx, id); err != nil {
			m.log.Warn("session teardown failed", "session", id, "err", err)
		}
		return
	}
	if res.State != StateActive {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.sessions[id]; ok && cur.State == StateReady {
		_ = transitionLocked(cur, StateActive)
	}
}

// teardown revokes the stream URL and deletes the workload. It is idempotent
// and leaves the record in terminating on failure so Sweep can retry.
func (m *Manager) teardown(ctx context.Context, id string) error {
	m.mu.Lock()
	rec, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	if rec.State != StateTerminating {
		if err := transitionLocked(rec, StateTerminating); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	m.mu.Unlock()

	base := context.WithoutCancel(ctx)
	callCtx, cancel := context.WithTimeout(base, m.opts.CallTimeout)
	defer cancel()

	if err := m.stream.Revoke(callCtx, id); err != nil {
		m.log.Warn("stream revocation failed", "session", id, "err", err)
	}
	if err := m.rt.Delete(callCtx, id); err != nil && !errors.Is(err, ErrRuntimeNotFound) {
		return fmt.Errorf("%w: teardown failed: %w", ErrUnavailable, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.sessions[id]; ok && cur.State == StateTerminating {
		_ = transitionLocked(cur, StateGone)
		delete(m.sessions, id)
	}
	return nil
}

func (m *Manager) abandon(id string) {
	m.mu.Lock()
	if rec, ok := m.sessions[id]; ok && rec.State != StateTerminating {
		rec.State = StateFailed
	}
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *Manager) deleteRuntime(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), m.opts.CallTimeout)
	defer cancel()
	if err := m.rt.Delete(ctx, id); err != nil && !errors.Is(err, ErrRuntimeNotFound) {
		m.log.Warn("runtime cleanup failed", "session", id, "err", err)
	}
}

func (m *Manager) countLocked(owner string) int {
	count := 0
	for _, rec := range m.sessions {
		if rec.Owner == owner && !rec.State.Terminal() {
			count++
		}
	}
	return count
}

func (m *Manager) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, m.opts.CallTimeout)
}

func (m *Manager) now() time.Time {
	return m.opts.Now()
}

func transitionLocked(rec *Record, to State) error {
	if rec.State == to {
		return nil
	}
	if err := checkTransition(rec.State, to); err != nil {
		return err
	}
	rec.State = to
	if to == StateTerminating || to == StateGone {
		rec.StreamURL = ""
	}
	return nil
}

func runtimeFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: provisioning timed out", ErrUnavailable)
	}
	return fmt.Errorf("%w: provisioning failed: %w", ErrUnavailable, err)
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "ses_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func validID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func validOwner(owner string) bool {
	return len(owner) > 0 && len(owner) <= 256 && printable(owner)
}

func validField(value string) bool {
	return len(value) > 0 && len(value) <= 64 && printable(value)
}

func printable(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}
