package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"geomind/session/internal/auth"
	"geomind/session/internal/ratelimit"
	"geomind/session/internal/session"
)

const (
	aliceToken = "token-alice"
	bobToken   = "token-bob"
)

type harness struct {
	handler   http.Handler
	manager   *session.Manager
	runtime   *session.FakeRuntime
	streaming *session.FakeStreaming
}

type harnessOptions struct {
	maxPerUser  int
	createLimit int
	ipLimit     int
	maxBody     int64
}

func newHarness(t *testing.T, opts harnessOptions) *harness {
	t.Helper()
	if opts.maxPerUser == 0 {
		opts.maxPerUser = 3
	}
	if opts.createLimit == 0 {
		opts.createLimit = 100
	}
	if opts.ipLimit == 0 {
		opts.ipLimit = 1000
	}

	runtime := session.NewFakeRuntime()
	streaming := session.NewFakeStreaming()
	manager, err := session.New(session.Options{
		Runtime:     runtime,
		Streaming:   streaming,
		Regions:     []string{"de-test", "us-test"},
		MaxPerUser:  opts.maxPerUser,
		MaxTotal:    20,
		CallTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	verifier, err := auth.NewDevVerifier(map[string]string{
		aliceToken: "alice",
		bobToken:   "bob",
	})
	if err != nil {
		t.Fatalf("auth.NewDevVerifier: %v", err)
	}
	server, err := New(Config{
		Manager:       manager,
		Verifier:      verifier,
		CreateLimiter: ratelimit.New(opts.createLimit, time.Minute),
		IPLimiter:     ratelimit.New(opts.ipLimit, time.Minute),
		MaxBodyBytes:  opts.maxBody,
	})
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	return &harness{handler: server.Handler(), manager: manager, runtime: runtime, streaming: streaming}
}

func (h *harness) request(t *testing.T, method, path, authorization, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, reader)
	req.RemoteAddr = "192.0.2.10:4567"
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func (h *harness) call(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	authorization := ""
	if token != "" {
		authorization = "Bearer " + token
	}
	return h.request(t, method, path, authorization, body)
}

func (h *harness) create(t *testing.T, token string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	rec := h.call(t, http.MethodPost, "/v1/sessions", token, `{"region":"de-test"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var view sessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return view.ID, rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestHealthzNeedsNoAuth(t *testing.T) {
	h := newHarness(t, harnessOptions{})

	rec := h.call(t, http.MethodGet, "/healthz", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("body = %q, want a status field", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestCreateRequiresAuthentication(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	body := `{"region":"de-test"}`

	for _, header := range []string{
		"",
		"Bearer",
		"Bearer ",
		"Bearer wrong-token",
		"Basic token-alice",
		"bearer token-alice",
		"Bearer token-alice ",
	} {
		rec := h.request(t, http.MethodPost, "/v1/sessions", header, body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want 401", header, rec.Code)
			continue
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("Authorization %q: missing WWW-Authenticate", header)
		}
		if strings.Contains(rec.Body.String(), "token-alice") {
			t.Errorf("Authorization %q: response echoed the token", header)
		}
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/sessions", strings.NewReader(body))
	req.RemoteAddr = "192.0.2.10:4567"
	req.Header.Add("Authorization", "Bearer "+aliceToken)
	req.Header.Add("Authorization", "Bearer "+aliceToken)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate Authorization headers: status = %d, want 401", rec.Code)
	}

	if calls := h.runtime.CreateCalls(); calls != 0 {
		t.Fatalf("runtime Create calls = %d, want 0", calls)
	}
}

func TestCreateReturnsReadySession(t *testing.T) {
	h := newHarness(t, harnessOptions{})

	id, rec := h.create(t, aliceToken)
	if id == "" {
		t.Fatal("response has no session id")
	}
	if got := rec.Header().Get("Location"); got != "/v1/sessions/"+id {
		t.Fatalf("Location = %q, want /v1/sessions/%s", got, id)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}

	var view sessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.State != string(session.StateReady) {
		t.Fatalf("state = %q, want ready", view.State)
	}
	if view.Region != "de-test" {
		t.Fatalf("region = %q, want de-test", view.Region)
	}
	if view.StreamURL == "" || view.StreamURL != h.streaming.URLFor(id) {
		t.Fatalf("streamUrl = %q, want the minted URL", view.StreamURL)
	}
	if _, err := time.Parse(time.RFC3339, view.ExpiresAt); err != nil {
		t.Fatalf("expiresAt = %q is not RFC3339: %v", view.ExpiresAt, err)
	}
	if strings.Contains(rec.Body.String(), "alice") {
		t.Fatal("response leaked the owner identifier")
	}
}

func TestCreateRejectsInvalidBodies(t *testing.T) {
	h := newHarness(t, harnessOptions{})

	for _, body := range []string{
		"",
		"not json",
		`{"region":""}`,
		`{"region":"xx-test"}`,
		`{"region":"de-test","extra":true}`,
		`{"region":"de-test"}{"region":"de-test"}`,
	} {
		rec := h.call(t, http.MethodPost, "/v1/sessions", aliceToken, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400 (body %s)", body, rec.Code, rec.Body.String())
			continue
		}
		if got := decodeError(t, rec).Error; got != "invalid_request" {
			t.Errorf("body %q: error = %q, want invalid_request", body, got)
		}
	}
	if calls := h.runtime.CreateCalls(); calls != 0 {
		t.Fatalf("runtime Create calls = %d, want 0", calls)
	}
}

func TestCreateRejectsOversizedBody(t *testing.T) {
	h := newHarness(t, harnessOptions{maxBody: 32})

	body := `{"region":"` + strings.Repeat("a", 80) + `"}`
	rec := h.call(t, http.MethodPost, "/v1/sessions", aliceToken, body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body %s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec).Error; got != "request_too_large" {
		t.Fatalf("error = %q, want request_too_large", got)
	}
}

func TestCreateRateLimit(t *testing.T) {
	h := newHarness(t, harnessOptions{createLimit: 1})

	h.create(t, aliceToken)
	rec := h.call(t, http.MethodPost, "/v1/sessions", aliceToken, `{"region":"de-test"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := decodeError(t, rec).Error; got != "rate_limited" {
		t.Fatalf("error = %q, want rate_limited", got)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	if calls := h.runtime.CreateCalls(); calls != 1 {
		t.Fatalf("runtime Create calls = %d, want 1", calls)
	}
}

func TestCreateConcurrencyCap(t *testing.T) {
	h := newHarness(t, harnessOptions{maxPerUser: 1})

	h.create(t, aliceToken)
	rec := h.call(t, http.MethodPost, "/v1/sessions", aliceToken, `{"region":"de-test"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := decodeError(t, rec).Error; got != "capacity" {
		t.Fatalf("error = %q, want capacity", got)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", rec.Header().Get("Retry-After"))
	}
	if _, other := h.create(t, bobToken); other.Code != http.StatusCreated {
		t.Fatalf("another user's create status = %d, want 201", other.Code)
	}
}

func TestGetEnforcesOwnership(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	id, _ := h.create(t, aliceToken)

	rec := h.call(t, http.MethodGet, "/v1/sessions/"+id, aliceToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner GET status = %d, want 200", rec.Code)
	}
	var view sessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.ID != id {
		t.Fatalf("id = %q, want %q", view.ID, id)
	}

	for _, tc := range []struct {
		name  string
		token string
		id    string
	}{
		{"another user", bobToken, id},
		{"unknown id", aliceToken, "ses_doesnotexist"},
	} {
		rec := h.call(t, http.MethodGet, "/v1/sessions/"+tc.id, tc.token, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", tc.name, rec.Code)
			continue
		}
		if got := decodeError(t, rec).Error; got != "not_found" {
			t.Errorf("%s: error = %q, want not_found", tc.name, got)
		}
	}

	if rec := h.call(t, http.MethodGet, "/v1/sessions/"+id, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status = %d, want 401", rec.Code)
	}
}

func TestDeleteEnforcesOwnershipAndIsIdempotent(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	id, _ := h.create(t, aliceToken)

	if rec := h.call(t, http.MethodDelete, "/v1/sessions/"+id, bobToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("other user's DELETE status = %d, want 404", rec.Code)
	}
	if rec := h.call(t, http.MethodGet, "/v1/sessions/"+id, aliceToken, ""); rec.Code != http.StatusOK {
		t.Fatal("other user's DELETE terminated the session")
	}
	if rec := h.call(t, http.MethodDelete, "/v1/sessions/"+id, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated DELETE status = %d, want 401", rec.Code)
	}

	rec := h.call(t, http.MethodDelete, "/v1/sessions/"+id, aliceToken, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("owner DELETE status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("DELETE body = %q, want empty", rec.Body.String())
	}
	if rec := h.call(t, http.MethodGet, "/v1/sessions/"+id, aliceToken, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE status = %d, want 404", rec.Code)
	}
	if rec := h.call(t, http.MethodDelete, "/v1/sessions/"+id, aliceToken, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("repeat DELETE status = %d, want 204", rec.Code)
	}
	if h.runtime.Has(id) {
		t.Fatal("runtime object survived DELETE")
	}
}

func TestIPLimitAppliesBeforeAuth(t *testing.T) {
	h := newHarness(t, harnessOptions{ipLimit: 1})

	h.create(t, aliceToken)
	rec := h.call(t, http.MethodGet, "/v1/sessions/ses_doesnotexist", bobToken, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := decodeError(t, rec).Error; got != "rate_limited" {
		t.Fatalf("error = %q, want rate_limited", got)
	}
	if rec := h.call(t, http.MethodGet, "/healthz", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	rec := h.call(t, http.MethodPut, "/v1/sessions", aliceToken, `{"region":"de-test"}`)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	manager, err := session.New(session.Options{
		Runtime:   session.NewFakeRuntime(),
		Streaming: session.NewFakeStreaming(),
		Regions:   []string{"de-test"},
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	verifier, err := auth.NewDevVerifier(map[string]string{"token-alice": "alice"})
	if err != nil {
		t.Fatalf("auth.NewDevVerifier: %v", err)
	}
	limiter := ratelimit.New(1, time.Minute)

	if _, err := New(Config{Verifier: verifier, CreateLimiter: limiter, IPLimiter: limiter}); err == nil {
		t.Fatal("New accepted a nil manager")
	}
	if _, err := New(Config{Manager: manager, CreateLimiter: limiter, IPLimiter: limiter}); err == nil {
		t.Fatal("New accepted a nil verifier")
	}
	if _, err := New(Config{Manager: manager, Verifier: verifier}); err == nil {
		t.Fatal("New accepted nil rate limiters")
	}
}
