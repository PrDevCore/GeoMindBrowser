// Package api exposes the session control plane over HTTP. It never returns
// page content and never logs request destinations.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"geomind/session/internal/auth"
	"geomind/session/internal/ratelimit"
	"geomind/session/internal/session"
)

const defaultMaxBody = 4096

// Config wires the HTTP API to its dependencies.
type Config struct {
	Manager       *session.Manager
	Verifier      auth.Verifier
	CreateLimiter *ratelimit.Limiter
	IPLimiter     *ratelimit.Limiter
	MaxBodyBytes  int64
	Logger        *slog.Logger
}

// Server is the HTTP surface of the control plane.
type Server struct {
	manager       *session.Manager
	verifier      auth.Verifier
	createLimiter *ratelimit.Limiter
	ipLimiter     *ratelimit.Limiter
	maxBody       int64
	log           *slog.Logger
}

// New validates cfg and returns a Server.
func New(cfg Config) (*Server, error) {
	if cfg.Manager == nil {
		return nil, errors.New("api: manager is required")
	}
	if cfg.Verifier == nil {
		return nil, errors.New("api: verifier is required")
	}
	if cfg.CreateLimiter == nil || cfg.IPLimiter == nil {
		return nil, errors.New("api: rate limiters are required")
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultMaxBody
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{
		manager:       cfg.Manager,
		verifier:      cfg.Verifier,
		createLimiter: cfg.CreateLimiter,
		ipLimiter:     cfg.IPLimiter,
		maxBody:       cfg.MaxBodyBytes,
		log:           cfg.Logger,
	}, nil
}

// Handler returns the routed, authenticated, rate-limited handler.
func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("POST /v1/sessions", s.create)
	api.HandleFunc("GET /v1/sessions/{id}", s.get)
	api.HandleFunc("DELETE /v1/sessions/{id}", s.delete)

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", s.health)
	root.Handle("/v1/", s.limitIP(s.authenticate(api)))
	return s.headers(root)
}

type createRequest struct {
	Region string `json:"region"`
}

type sessionView struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	Region    string `json:"region"`
	StreamURL string `json:"streamUrl,omitempty"`
	ExpiresAt string `json:"expiresAt"`
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(r.Context())
	if !ok {
		s.unauthorized(w)
		return
	}
	if allowed, retry := s.createLimiter.Allow(principal.ID); !allowed {
		writeRetry(w, retry)
		return
	}
	var req createRequest
	if err := s.decode(w, r, &req); err != nil {
		s.badRequest(w, err)
		return
	}
	record, err := s.manager.Create(r.Context(), principal.ID, session.Spec{Region: req.Region})
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Location", "/v1/sessions/"+record.ID)
	writeJSON(w, http.StatusCreated, newView(record))
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(r.Context())
	if !ok {
		s.unauthorized(w)
		return
	}
	record, err := s.manager.Get(principal.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newView(record))
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(r.Context())
	if !ok {
		s.unauthorized(w)
		return
	}
	err := s.manager.Delete(r.Context(), principal.ID, r.PathValue("id"))
	switch {
	case err == nil, errors.Is(err, session.ErrNotFound):
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, session.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found", "session not found")
	default:
		s.fail(w, err)
	}
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("api: trailing data after JSON body")
	}
	return nil
}

func (s *Server) badRequest(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrInvalidRegion), errors.Is(err, session.ErrInvalidSpec):
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid session request")
	case errors.Is(err, session.ErrNotFound), errors.Is(err, session.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found", "session not found")
	case errors.Is(err, session.ErrCapacity):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "capacity", "session capacity reached")
	case errors.Is(err, session.ErrUnavailable),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		writeError(w, http.StatusServiceUnavailable, "unavailable", "service unavailable")
	default:
		s.log.Error("session request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			s.unauthorized(w)
			return
		}
		token, found := strings.CutPrefix(values[0], "Bearer ")
		if !found || token == "" || strings.ContainsAny(token, " \t") {
			s.unauthorized(w)
			return
		}
		principal, err := s.verifier.Verify(r.Context(), token)
		if err != nil {
			s.unauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	})
}

func (s *Server) limitIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowed, retry := s.ipLimiter.Allow(clientIP(r)); !allowed {
			writeRetry(w, retry)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Cache-Control", "no-store")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="geomind-session"`)
	writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
}

type principalKey struct{}

func principalFrom(ctx context.Context) (auth.Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(auth.Principal)
	return principal, ok
}

func newView(record session.Record) sessionView {
	view := sessionView{
		ID:        record.ID,
		State:     string(record.State),
		Region:    record.Region,
		ExpiresAt: record.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if (record.State == session.StateReady || record.State == session.StateActive) && record.StreamURL != "" {
		view.StreamURL = record.StreamURL
	}
	return view
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "unknown"
	}
	return host
}

func writeRetry(w http.ResponseWriter, retry time.Duration) {
	seconds := int(math.Ceil(retry.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
