package main

import (
	"context"
	"os"
	"time"

	"geomind/companion/internal/ipc"
	"geomind/companion/internal/policy"
)

// policyWatchInterval is short enough to feel instant when a rule is saved and
// long enough that polling a small local file is negligible.
const policyWatchInterval = time.Second

type fileStamp struct {
	modTime time.Time
	size    int64
}

// watchPolicy reloads the policy when the file changes and tells the extension
// with a policy.changed event. A reload that fails validation leaves the
// previous policy in effect and is retried until the file becomes valid.
// start is the stamp captured before the initial load; nil means unknown, so
// the first tick loads the file.
func watchPolicy(ctx context.Context, path string, store *policy.Store, sender *ipc.Sender, interval time.Duration, start *fileStamp) {
	var stamp fileStamp
	known := start != nil
	if start != nil {
		stamp = *start
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		next, present := policyStamp(path)
		if !present {
			continue
		}
		if known && next == stamp {
			continue
		}
		if _, err := store.Load(path); err != nil {
			// Keep the old stamp so the next tick retries an invalid file.
			continue
		}
		stamp, known = next, true
		_ = sender.Notify("policy.changed", struct{}{})
	}
}

func policyStamp(path string) (fileStamp, bool) {
	info, err := os.Stat(path) //nolint:gosec // G703: local policy file path from the companion config.
	if err != nil {
		return fileStamp{}, false
	}
	return fileStamp{modTime: info.ModTime(), size: info.Size()}, true
}
