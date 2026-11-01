package ratelimit

import (
	"testing"
	"time"
)

func TestAllowWithinWindow(t *testing.T) {
	limiter := New(3, time.Minute)
	for i := 0; i < 3; i++ {
		if allowed, _ := limiter.Allow("user"); !allowed {
			t.Fatalf("request %d was denied", i+1)
		}
	}
	allowed, retry := limiter.Allow("user")
	if allowed {
		t.Fatal("request over the limit was allowed")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry after = %s, want a positive duration within the window", retry)
	}
}

func TestWindowResets(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	limiter := New(1, time.Minute)
	limiter.now = func() time.Time { return now }

	if allowed, _ := limiter.Allow("user"); !allowed {
		t.Fatal("first request was denied")
	}
	if allowed, _ := limiter.Allow("user"); allowed {
		t.Fatal("second request in the same window was allowed")
	}
	now = now.Add(time.Minute)
	if allowed, _ := limiter.Allow("user"); !allowed {
		t.Fatal("request after the window was denied")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	limiter := New(1, time.Minute)
	if allowed, _ := limiter.Allow("alice"); !allowed {
		t.Fatal("alice was denied")
	}
	if allowed, _ := limiter.Allow("bob"); !allowed {
		t.Fatal("bob inherited alice's bucket")
	}
}

func TestKeyBoundIsFailClosed(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	limiter := New(1, time.Minute)
	limiter.maxKeys = 2
	limiter.now = func() time.Time { return now }

	if allowed, _ := limiter.Allow("a"); !allowed {
		t.Fatal("first key was denied")
	}
	if allowed, _ := limiter.Allow("b"); !allowed {
		t.Fatal("second key was denied")
	}
	if allowed, _ := limiter.Allow("c"); allowed {
		t.Fatal("new key was admitted past the key bound")
	}

	now = now.Add(2 * time.Minute)
	if allowed, _ := limiter.Allow("c"); !allowed {
		t.Fatal("new key was denied after expired buckets were pruned")
	}
}

func TestNonPositiveLimitDeniesEverything(t *testing.T) {
	limiter := New(0, time.Minute)
	if allowed, retry := limiter.Allow("user"); allowed || retry <= 0 {
		t.Fatalf("Allow = %v/%s, want denial with a retry hint", allowed, retry)
	}
}
