package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const minDevTokenLen = 16

type config struct {
	Addr          string
	DevToken      string
	DevUser       string
	Regions       []string
	AllowPublic   bool
	MaxPerUser    int
	MaxTotal      int
	TTL           time.Duration
	Idle          time.Duration
	UserRate      int
	IPRate        int
	SweepInterval time.Duration
	CreateTimeout time.Duration

	Runtime          string
	KubeNamespace    string
	KubeImage        string
	KubeConfig       string
	KubeProxyNS      string
	KubeEdgeNS       string
	KubeProxyPort    int
	KubeReadyTimeout time.Duration
	KubePollInterval time.Duration
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		Addr:          envOr(getenv, "GEOMIND_SESSION_ADDR", "127.0.0.1:8080"),
		DevUser:       envOr(getenv, "GEOMIND_SESSION_DEV_USER", "dev"),
		SweepInterval: 5 * time.Second,
		Runtime:       strings.ToLower(envOr(getenv, "GEOMIND_SESSION_RUNTIME", runtimeFake)),
	}

	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return config{}, fmt.Errorf("GEOMIND_SESSION_ADDR: %w", err)
	}
	cfg.AllowPublic = truthy(getenv("GEOMIND_SESSION_ALLOW_PUBLIC"))
	if !loopbackHost(host) && !cfg.AllowPublic {
		return config{}, errors.New("GEOMIND_SESSION_ADDR is not a loopback address; set GEOMIND_SESSION_ALLOW_PUBLIC=1 only behind TLS")
	}

	cfg.DevToken = strings.TrimSpace(getenv("GEOMIND_SESSION_DEV_TOKEN"))
	if len(cfg.DevToken) < minDevTokenLen {
		return config{}, fmt.Errorf("GEOMIND_SESSION_DEV_TOKEN is required and must be at least %d characters", minDevTokenLen)
	}
	if !printableASCII(cfg.DevToken) || strings.ContainsAny(cfg.DevToken, " \t") {
		return config{}, errors.New("GEOMIND_SESSION_DEV_TOKEN must be printable ASCII without spaces")
	}
	if !printableASCII(cfg.DevUser) || len(cfg.DevUser) > 256 {
		return config{}, errors.New("GEOMIND_SESSION_DEV_USER must be printable ASCII of at most 256 characters")
	}

	cfg.Regions = splitList(getenv("GEOMIND_SESSION_REGIONS"))
	if len(cfg.Regions) == 0 {
		cfg.Regions = []string{"de-test"}
	}

	if cfg.MaxPerUser, err = intEnv(getenv, "GEOMIND_SESSION_MAX_PER_USER", 2, 1, 100); err != nil {
		return config{}, err
	}
	if cfg.MaxTotal, err = intEnv(getenv, "GEOMIND_SESSION_MAX_TOTAL", 20, 1, 1000); err != nil {
		return config{}, err
	}
	if cfg.UserRate, err = intEnv(getenv, "GEOMIND_SESSION_USER_RATE", 30, 1, 10000); err != nil {
		return config{}, err
	}
	if cfg.IPRate, err = intEnv(getenv, "GEOMIND_SESSION_IP_RATE", 120, 1, 100000); err != nil {
		return config{}, err
	}
	if cfg.TTL, err = durationEnv(getenv, "GEOMIND_SESSION_TTL", 30*time.Minute, time.Minute, 24*time.Hour); err != nil {
		return config{}, err
	}
	if cfg.Idle, err = durationEnv(getenv, "GEOMIND_SESSION_IDLE", 5*time.Minute, time.Minute, 24*time.Hour); err != nil {
		return config{}, err
	}
	if cfg.Idle > cfg.TTL {
		return config{}, errors.New("GEOMIND_SESSION_IDLE must not exceed GEOMIND_SESSION_TTL")
	}
	if cfg.MaxTotal < cfg.MaxPerUser {
		cfg.MaxTotal = cfg.MaxPerUser
	}
	if cfg.CreateTimeout, err = durationEnv(getenv, "GEOMIND_SESSION_CREATE_TIMEOUT", 2*time.Minute, 5*time.Second, time.Hour); err != nil {
		return config{}, err
	}
	switch cfg.Runtime {
	case runtimeFake:
	case runtimeKube:
		if err := cfg.loadKube(getenv); err != nil {
			return config{}, err
		}
	default:
		return config{}, fmt.Errorf("GEOMIND_SESSION_RUNTIME must be %q or %q", runtimeFake, runtimeKube)
	}
	return cfg, nil
}

const (
	runtimeFake = "fake"
	runtimeKube = "kube"
)

func (cfg *config) loadKube(getenv func(string) string) error {
	cfg.KubeNamespace = envOr(getenv, "GEOMIND_SESSION_KUBE_NAMESPACE", "geomind-sessions")
	cfg.KubeImage = envOr(getenv, "GEOMIND_SESSION_KUBE_IMAGE", "")
	if cfg.KubeImage == "" {
		return errors.New("GEOMIND_SESSION_KUBE_IMAGE is required when GEOMIND_SESSION_RUNTIME=kube")
	}
	cfg.KubeConfig = strings.TrimSpace(getenv("GEOMIND_SESSION_KUBECONFIG"))
	cfg.KubeProxyNS = envOr(getenv, "GEOMIND_SESSION_KUBE_PROXY_NAMESPACE", cfg.KubeNamespace)
	cfg.KubeEdgeNS = envOr(getenv, "GEOMIND_SESSION_KUBE_EDGE_NAMESPACE", cfg.KubeNamespace)

	var err error
	if cfg.KubeProxyPort, err = intEnv(getenv, "GEOMIND_SESSION_KUBE_PROXY_PORT", 1080, 1, 65535); err != nil {
		return err
	}
	if cfg.KubeReadyTimeout, err = durationEnv(getenv, "GEOMIND_SESSION_KUBE_READY_TIMEOUT", 2*time.Minute, time.Second, 30*time.Minute); err != nil {
		return err
	}
	if cfg.KubePollInterval, err = durationEnv(getenv, "GEOMIND_SESSION_KUBE_POLL_INTERVAL", time.Second, 10*time.Millisecond, 30*time.Second); err != nil {
		return err
	}
	if strings.TrimSpace(getenv("GEOMIND_SESSION_CREATE_TIMEOUT")) == "" {
		cfg.CreateTimeout = cfg.KubeReadyTimeout + 30*time.Second
	}
	if cfg.CreateTimeout <= cfg.KubeReadyTimeout {
		return errors.New("GEOMIND_SESSION_CREATE_TIMEOUT must exceed GEOMIND_SESSION_KUBE_READY_TIMEOUT")
	}
	return nil
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envOr(getenv func(string) string, key, fallback string) string {
	if value := strings.TrimSpace(getenv(key)); value != "" {
		return value
	}
	return fallback
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func intEnv(getenv func(string) string, key string, fallback, minValue, maxValue int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minValue, maxValue)
	}
	return value, nil
}

func durationEnv(getenv func(string) string, key string, fallback, minValue, maxValue time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be between %s and %s", key, minValue, maxValue)
	}
	return value, nil
}

func printableASCII(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}
