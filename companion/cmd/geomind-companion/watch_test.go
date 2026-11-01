package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"geomind/companion/internal/ipc"
	"geomind/companion/internal/policy"
)

const watchTestInterval = 5 * time.Millisecond

type eventWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *eventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// completeFrames decodes every whole frame written so far. A frame caught mid
// write is simply not counted yet; the next poll re-reads from the start.
func (w *eventWriter) completeFrames() []ipc.Envelope {
	w.mu.Lock()
	buf := append([]byte(nil), w.buf...)
	w.mu.Unlock()

	var frames []ipc.Envelope
	r := bytes.NewReader(buf)
	for {
		raw, err := ipc.ReadMessage(r)
		if err != nil {
			return frames
		}
		var env ipc.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return frames
		}
		frames = append(frames, env)
	}
}

func writePolicyAt(t *testing.T, path, id, host, exit string) {
	t.Helper()
	body := `{"version":1,"defaultAction":"direct","rules":[{"id":"` + id +
		`","host":"` + host + `","action":"route","exit":"` + exit + `"}]}`
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func startWatcher(t *testing.T, path string, store *policy.Store, writer *eventWriter) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchPolicy(ctx, path, store, ipc.NewSender(writer), watchTestInterval, nil)
	}()
	return func() {
		cancel()
		<-done
	}
}

func TestWatchPolicyReloadsChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	store := policy.New([]string{"de"})
	writer := &eventWriter{}
	stop := startWatcher(t, path, store, writer)
	defer stop()

	writePolicyAt(t, path, "old", "old.example", "de")
	waitFor(t, "initial policy load", func() bool {
		decision, err := store.Match("old.example")
		return err == nil && decision.RuleID == "old"
	})
	waitFor(t, "initial policy event", func() bool {
		return len(writer.completeFrames()) == 1
	})

	writePolicyAt(t, path, "new", "new.example", "de")
	waitFor(t, "reloaded policy", func() bool {
		decision, err := store.Match("new.example")
		return err == nil && decision.RuleID == "new"
	})
	waitFor(t, "reload event", func() bool {
		return len(writer.completeFrames()) == 2
	})

	for _, env := range writer.completeFrames() {
		if env.ID != ipc.EventID || env.Type != "policy.changed" {
			t.Fatalf("event = %+v", env)
		}
		if len(env.Payload) != 0 && string(env.Payload) != "{}" {
			t.Fatalf("event payload = %s", env.Payload)
		}
	}
}

func TestWatchPolicyKeepsOldPolicyOnInvalidReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	store := policy.New([]string{"de"})
	writer := &eventWriter{}
	stop := startWatcher(t, path, store, writer)
	defer stop()

	writePolicyAt(t, path, "old", "old.example", "de")
	waitFor(t, "initial policy load", func() bool {
		decision, err := store.Match("old.example")
		return err == nil && decision.RuleID == "old"
	})

	if err := os.WriteFile(path+".tmp", []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
	// Several watch ticks pass; the broken file must not replace the policy.
	time.Sleep(20 * watchTestInterval)

	decision, err := store.Match("old.example")
	if err != nil || decision.RuleID != "old" {
		t.Fatalf("old policy was replaced: decision=%+v err=%v", decision, err)
	}
	if frames := writer.completeFrames(); len(frames) != 1 {
		t.Fatalf("events = %d, want 1", len(frames))
	}

	writePolicyAt(t, path, "new", "new.example", "de")
	waitFor(t, "policy after invalid file was fixed", func() bool {
		decision, err := store.Match("new.example")
		return err == nil && decision.RuleID == "new"
	})
	waitFor(t, "recovery event", func() bool {
		return len(writer.completeFrames()) == 2
	})
}

func TestWatchPolicyIgnoresUnchangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	store := policy.New([]string{"de"})
	writer := &eventWriter{}
	stop := startWatcher(t, path, store, writer)
	defer stop()

	writePolicyAt(t, path, "old", "old.example", "de")
	waitFor(t, "initial policy event", func() bool {
		return len(writer.completeFrames()) == 1
	})
	time.Sleep(20 * watchTestInterval)
	if frames := writer.completeFrames(); len(frames) != 1 {
		t.Fatalf("events = %d after an unchanged file", len(frames))
	}
}
