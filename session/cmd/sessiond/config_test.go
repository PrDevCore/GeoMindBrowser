package main

import (
	"reflect"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

const validToken = "dev-token-0123456789"

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"GEOMIND_SESSION_DEV_TOKEN": validToken,
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Addr != "127.0.0.1:8080" {
		t.Fatalf("Addr = %q, want the loopback default", cfg.Addr)
	}
	if cfg.DevUser != "dev" {
		t.Fatalf("DevUser = %q, want dev", cfg.DevUser)
	}
	if !reflect.DeepEqual(cfg.Regions, []string{"de-test"}) {
		t.Fatalf("Regions = %v, want [de-test]", cfg.Regions)
	}
	if cfg.AllowPublic {
		t.Fatal("AllowPublic defaulted to true")
	}
	if cfg.MaxPerUser != 2 || cfg.MaxTotal != 20 {
		t.Fatalf("caps = %d/%d, want 2/20", cfg.MaxPerUser, cfg.MaxTotal)
	}
	if cfg.TTL != 30*time.Minute || cfg.Idle != 5*time.Minute {
		t.Fatalf("ttl/idle = %s/%s, want 30m/5m", cfg.TTL, cfg.Idle)
	}
	if cfg.UserRate != 30 || cfg.IPRate != 120 {
		t.Fatalf("rates = %d/%d, want 30/120", cfg.UserRate, cfg.IPRate)
	}
	if cfg.SweepInterval != 5*time.Second {
		t.Fatalf("SweepInterval = %s, want 5s", cfg.SweepInterval)
	}
}

func TestLoadConfigRequiresDevToken(t *testing.T) {
	for _, token := range []string{"", "short", "with space 0123456789", "bad\x01token-0123456789"} {
		values := map[string]string{}
		if token != "" {
			values["GEOMIND_SESSION_DEV_TOKEN"] = token
		}
		if _, err := loadConfig(env(values)); err == nil {
			t.Errorf("loadConfig accepted token %q", token)
		}
	}
}

func TestLoadConfigRefusesPublicBind(t *testing.T) {
	values := map[string]string{
		"GEOMIND_SESSION_DEV_TOKEN": validToken,
		"GEOMIND_SESSION_ADDR":      "0.0.0.0:8080",
	}
	if _, err := loadConfig(env(values)); err == nil {
		t.Fatal("loadConfig accepted a public bind without an opt-in")
	}

	for _, addr := range []string{":8080", "0.0.0.0:8080", "[::]:8080"} {
		values := map[string]string{
			"GEOMIND_SESSION_DEV_TOKEN": validToken,
			"GEOMIND_SESSION_ADDR":      addr,
		}
		if _, err := loadConfig(env(values)); err == nil {
			t.Errorf("loadConfig accepted wildcard bind %q", addr)
		}
	}

	values["GEOMIND_SESSION_ALLOW_PUBLIC"] = "1"
	cfg, err := loadConfig(env(values))
	if err != nil {
		t.Fatalf("loadConfig with opt-in: %v", err)
	}
	if !cfg.AllowPublic {
		t.Fatal("AllowPublic = false, want true")
	}

	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		values := map[string]string{
			"GEOMIND_SESSION_DEV_TOKEN": validToken,
			"GEOMIND_SESSION_ADDR":      addr,
		}
		if _, err := loadConfig(env(values)); err != nil {
			t.Errorf("loadConfig(%q): %v", addr, err)
		}
	}

	if _, err := loadConfig(env(map[string]string{
		"GEOMIND_SESSION_DEV_TOKEN": validToken,
		"GEOMIND_SESSION_ADDR":      "not-an-address",
	})); err == nil {
		t.Fatal("loadConfig accepted a malformed address")
	}
}

func TestLoadConfigValidatesLimits(t *testing.T) {
	base := map[string]string{"GEOMIND_SESSION_DEV_TOKEN": validToken}

	cases := []map[string]string{
		{"GEOMIND_SESSION_IDLE": "45m", "GEOMIND_SESSION_TTL": "30m"},
		{"GEOMIND_SESSION_MAX_PER_USER": "0"},
		{"GEOMIND_SESSION_MAX_PER_USER": "101"},
		{"GEOMIND_SESSION_MAX_TOTAL": "many"},
		{"GEOMIND_SESSION_USER_RATE": "-1"},
		{"GEOMIND_SESSION_TTL": "soon"},
	}
	for _, overrides := range cases {
		values := map[string]string{}
		for key, value := range base {
			values[key] = value
		}
		for key, value := range overrides {
			values[key] = value
		}
		if _, err := loadConfig(env(values)); err == nil {
			t.Errorf("loadConfig accepted overrides %v", overrides)
		}
	}
}

func TestLoadConfigParsesLists(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"GEOMIND_SESSION_DEV_TOKEN":    validToken,
		"GEOMIND_SESSION_REGIONS":      " de-test , us-test ,, ",
		"GEOMIND_SESSION_MAX_PER_USER": "1",
		"GEOMIND_SESSION_MAX_TOTAL":    "1",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !reflect.DeepEqual(cfg.Regions, []string{"de-test", "us-test"}) {
		t.Fatalf("Regions = %v, want [de-test us-test]", cfg.Regions)
	}
}

func TestLoadConfigValidatesDevUser(t *testing.T) {
	if _, err := loadConfig(env(map[string]string{
		"GEOMIND_SESSION_DEV_TOKEN": validToken,
		"GEOMIND_SESSION_DEV_USER":  "bad\x01user",
	})); err == nil {
		t.Fatal("loadConfig accepted a non-printable dev user")
	}
}

func TestLoadConfigDefaultsToFakeRuntime(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{"GEOMIND_SESSION_DEV_TOKEN": validToken}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Runtime != runtimeFake {
		t.Fatalf("Runtime = %q, want fake", cfg.Runtime)
	}
	if cfg.CreateTimeout != 2*time.Minute {
		t.Fatalf("CreateTimeout = %s, want 2m", cfg.CreateTimeout)
	}
}

func TestLoadConfigKubeRuntime(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"GEOMIND_SESSION_DEV_TOKEN":  validToken,
		"GEOMIND_SESSION_RUNTIME":    "kube",
		"GEOMIND_SESSION_KUBE_IMAGE": "registry.example/geomind-session:test",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.KubeNamespace != "geomind-sessions" {
		t.Errorf("namespace = %q", cfg.KubeNamespace)
	}
	if cfg.KubeProxyNS != cfg.KubeNamespace || cfg.KubeEdgeNS != cfg.KubeNamespace {
		t.Errorf("peer namespaces = %q/%q, want the session namespace", cfg.KubeProxyNS, cfg.KubeEdgeNS)
	}
	if cfg.KubeProxyPort != 1080 {
		t.Errorf("proxy port = %d", cfg.KubeProxyPort)
	}
	if cfg.KubeReadyTimeout != 2*time.Minute {
		t.Errorf("ready timeout = %s", cfg.KubeReadyTimeout)
	}
	if cfg.KubePollInterval != time.Second {
		t.Errorf("poll interval = %s", cfg.KubePollInterval)
	}
	if cfg.CreateTimeout != cfg.KubeReadyTimeout+30*time.Second {
		t.Errorf("create timeout = %s, want ready timeout plus 30s", cfg.CreateTimeout)
	}
}

func TestLoadConfigKubeRuntimeOverrides(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"GEOMIND_SESSION_DEV_TOKEN":            validToken,
		"GEOMIND_SESSION_RUNTIME":              "kube",
		"GEOMIND_SESSION_KUBE_IMAGE":           "registry.example/geomind-session:test",
		"GEOMIND_SESSION_KUBE_NAMESPACE":       "sessions-a",
		"GEOMIND_SESSION_KUBE_PROXY_NAMESPACE": "egress",
		"GEOMIND_SESSION_KUBE_EDGE_NAMESPACE":  "edge",
		"GEOMIND_SESSION_KUBE_PROXY_PORT":      "9050",
		"GEOMIND_SESSION_KUBE_READY_TIMEOUT":   "90s",
		"GEOMIND_SESSION_KUBE_POLL_INTERVAL":   "250ms",
		"GEOMIND_SESSION_CREATE_TIMEOUT":       "2m",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.KubeNamespace != "sessions-a" || cfg.KubeProxyNS != "egress" || cfg.KubeEdgeNS != "edge" {
		t.Errorf("namespaces = %q/%q/%q", cfg.KubeNamespace, cfg.KubeProxyNS, cfg.KubeEdgeNS)
	}
	if cfg.KubeProxyPort != 9050 {
		t.Errorf("proxy port = %d", cfg.KubeProxyPort)
	}
	if cfg.KubeReadyTimeout != 90*time.Second || cfg.KubePollInterval != 250*time.Millisecond {
		t.Errorf("timeouts = %s/%s", cfg.KubeReadyTimeout, cfg.KubePollInterval)
	}
	if cfg.CreateTimeout != 2*time.Minute {
		t.Errorf("create timeout = %s", cfg.CreateTimeout)
	}
}

func TestLoadConfigRejectsBadRuntimeSettings(t *testing.T) {
	base := map[string]string{
		"GEOMIND_SESSION_DEV_TOKEN":  validToken,
		"GEOMIND_SESSION_RUNTIME":    "kube",
		"GEOMIND_SESSION_KUBE_IMAGE": "registry.example/geomind-session:test",
	}
	cases := []map[string]string{
		{"GEOMIND_SESSION_RUNTIME": "nomad"},
		{"GEOMIND_SESSION_KUBE_IMAGE": ""},
		{"GEOMIND_SESSION_KUBE_PROXY_PORT": "0"},
		{"GEOMIND_SESSION_KUBE_PROXY_PORT": "70000"},
		{"GEOMIND_SESSION_KUBE_READY_TIMEOUT": "10ms"},
		{"GEOMIND_SESSION_KUBE_POLL_INTERVAL": "1ms"},
		{"GEOMIND_SESSION_CREATE_TIMEOUT": "30s", "GEOMIND_SESSION_KUBE_READY_TIMEOUT": "1m"},
	}
	for _, overrides := range cases {
		values := map[string]string{}
		for key, value := range base {
			values[key] = value
		}
		for key, value := range overrides {
			values[key] = value
		}
		if _, err := loadConfig(env(values)); err == nil {
			t.Errorf("loadConfig accepted overrides %v", overrides)
		}
	}
}
