package main

import (
	"os"
	"path/filepath"
	"testing"

	"geomind/session/internal/session"
)

func TestBuildRuntimeDefaultsToFake(t *testing.T) {
	rt, err := buildRuntime(config{Runtime: runtimeFake})
	if err != nil {
		t.Fatalf("buildRuntime: %v", err)
	}
	if _, ok := rt.(*session.FakeRuntime); !ok {
		t.Fatalf("runtime = %T, want the fake runtime", rt)
	}
}

func TestBuildRuntimeRejectsUnknownRuntime(t *testing.T) {
	if _, err := buildRuntime(config{Runtime: "nomad"}); err == nil {
		t.Fatal("buildRuntime accepted an unknown runtime")
	}
}

func TestKubeRESTConfigReadsKubeconfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	content := `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: test-token
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	cfg, err := kubeRESTConfig(path)
	if err != nil {
		t.Fatalf("kubeRESTConfig: %v", err)
	}
	if cfg.Host != "https://127.0.0.1:6443" {
		t.Errorf("host = %q", cfg.Host)
	}
	if cfg.BearerToken != "test-token" {
		t.Error("bearer token was not loaded from the kubeconfig")
	}
}

func TestKubeRESTConfigRejectsMissingFile(t *testing.T) {
	if _, err := kubeRESTConfig(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("kubeRESTConfig accepted a missing file")
	}
}

func TestSafePort(t *testing.T) {
	if port, err := safePort("port", 1080); err != nil || port != 1080 {
		t.Errorf("safePort(1080) = %d, %v", port, err)
	}
	for _, value := range []int{0, -1, 65536} {
		if _, err := safePort("port", value); err == nil {
			t.Errorf("safePort(%d) was accepted", value)
		}
	}
}
