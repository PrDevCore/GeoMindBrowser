// Command sessiond runs the GeoMind remote-session control plane. The default
// runtime is an in-memory fake; the Kubernetes runtime provisions one ephemeral
// Pod per session and is enabled with GEOMIND_SESSION_RUNTIME=kube.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"geomind/session/internal/api"
	"geomind/session/internal/auth"
	"geomind/session/internal/ratelimit"
	"geomind/session/internal/session"
)

func main() {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sessiond:", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "sessiond:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config, stderr io.Writer) error {
	logger := slog.New(slog.NewTextHandler(stderr, nil))

	rt, err := buildRuntime(cfg)
	if err != nil {
		return err
	}

	manager, err := session.New(session.Options{
		Runtime:       rt,
		Streaming:     session.NewFakeStreaming(),
		Regions:       cfg.Regions,
		MaxPerUser:    cfg.MaxPerUser,
		MaxTotal:      cfg.MaxTotal,
		DefaultTTL:    cfg.TTL,
		DefaultIdle:   cfg.Idle,
		CreateTimeout: cfg.CreateTimeout,
		Logger:        logger,
	})
	if err != nil {
		return err
	}

	verifier, err := auth.NewDevVerifier(map[string]string{cfg.DevToken: cfg.DevUser})
	if err != nil {
		return err
	}

	handler, err := api.New(api.Config{
		Manager:       manager,
		Verifier:      verifier,
		CreateLimiter: ratelimit.New(cfg.UserRate, time.Minute),
		IPLimiter:     ratelimit.New(cfg.IPRate, time.Minute),
		Logger:        logger,
	})
	if err != nil {
		return err
	}

	// Create is synchronous and may wait for a Pod to become ready, so the
	// write deadline has to outlast the provisioning budget.
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      cfg.CreateTimeout + 15*time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	go manager.RunSweeper(ctx, cfg.SweepInterval)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("session control plane listening",
		"addr", cfg.Addr,
		"regions", cfg.Regions,
		"runtime", cfg.Runtime,
		"maxPerUser", cfg.MaxPerUser,
		"maxTotal", cfg.MaxTotal,
	)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
