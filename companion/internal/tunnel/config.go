package tunnel

import (
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultMTU is the WireGuard interface MTU used when none is configured.
	DefaultMTU = 1420
	// DefaultKeepalive keeps NAT mappings alive and refreshes handshakes.
	DefaultKeepalive = 25 * time.Second
	minMTU           = 1280
	maxMTU           = 9000
)

// Config is the in-memory WireGuard configuration. Keys come from the
// environment and are never written to disk or included in errors.
type Config struct {
	PrivateKey     [32]byte
	PeerPublicKey  [32]byte
	Endpoint       netip.AddrPort
	AllowedIPs     []netip.Prefix
	LocalAddresses []netip.Addr
	ListenPort     uint16
	MTU            int
	Keepalive      time.Duration
	StaleAfter     time.Duration
	DoHURL         string
	// RootCAs overrides the system roots for the DoH endpoint. It exists for
	// tests and for private exit certificates; nil means system roots.
	RootCAs *x509.CertPool
}

// FromEnv reads GEOMIND_WG_* values. Keys and the DoH URL are required; a
// missing or invalid value is an error and never echoed back.
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		MTU:       DefaultMTU,
		Keepalive: DefaultKeepalive,
	}
	priv, err := parseKey("GEOMIND_WG_PRIVATE_KEY", getenv("GEOMIND_WG_PRIVATE_KEY"))
	if err != nil {
		return Config{}, err
	}
	cfg.PrivateKey = priv
	peer, err := parseKey("GEOMIND_WG_PEER_PUBLIC_KEY", getenv("GEOMIND_WG_PEER_PUBLIC_KEY"))
	if err != nil {
		return Config{}, err
	}
	cfg.PeerPublicKey = peer
	cfg.Endpoint, err = parseEndpoint(getenv("GEOMIND_WG_ENDPOINT"))
	if err != nil {
		return Config{}, err
	}
	cfg.LocalAddresses, err = parseLocalAddresses(getenv("GEOMIND_WG_LOCAL_ADDRS"))
	if err != nil {
		return Config{}, err
	}
	cfg.AllowedIPs, err = parseAllowedIPs(getenv("GEOMIND_WG_ALLOWED_IPS"))
	if err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(getenv("GEOMIND_WG_MTU")); raw != "" {
		mtu, convErr := strconv.Atoi(raw)
		if convErr != nil {
			return Config{}, fmt.Errorf("GEOMIND_WG_MTU must be an integer")
		}
		cfg.MTU = mtu
	}
	if raw := strings.TrimSpace(getenv("GEOMIND_WG_KEEPALIVE")); raw != "" {
		seconds, convErr := strconv.Atoi(raw)
		if convErr != nil {
			return Config{}, fmt.Errorf("GEOMIND_WG_KEEPALIVE must be an integer number of seconds")
		}
		cfg.Keepalive = time.Duration(seconds) * time.Second
	}
	cfg.DoHURL = strings.TrimSpace(getenv("GEOMIND_WG_DOH"))
	if cfg.DoHURL == "" {
		return Config{}, fmt.Errorf("GEOMIND_WG_DOH is required")
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.PrivateKey == ([32]byte{}) {
		return fmt.Errorf("private key is required")
	}
	if c.PeerPublicKey == ([32]byte{}) {
		return fmt.Errorf("peer public key is required")
	}
	if !c.Endpoint.IsValid() {
		return fmt.Errorf("peer endpoint is required")
	}
	if c.Endpoint.Port() == 0 {
		return fmt.Errorf("peer endpoint port must not be zero")
	}
	if addr := c.Endpoint.Addr(); addr.IsUnspecified() || addr.IsMulticast() {
		return fmt.Errorf("peer endpoint must be a unicast address")
	}
	if len(c.LocalAddresses) == 0 {
		return fmt.Errorf("at least one local address is required")
	}
	for _, addr := range c.LocalAddresses {
		if !addr.IsValid() || addr.IsUnspecified() || addr.IsMulticast() {
			return fmt.Errorf("local address must be a unicast address")
		}
	}
	if len(c.AllowedIPs) == 0 {
		return fmt.Errorf("at least one allowed IP prefix is required")
	}
	for _, prefix := range c.AllowedIPs {
		if !prefix.IsValid() || prefix.Addr().IsMulticast() {
			return fmt.Errorf("allowed IP must be a unicast prefix")
		}
	}
	if c.MTU == 0 {
		c.MTU = DefaultMTU
	}
	if c.MTU < minMTU || c.MTU > maxMTU {
		return fmt.Errorf("MTU must be between %d and %d", minMTU, maxMTU)
	}
	if c.Keepalive == 0 {
		c.Keepalive = DefaultKeepalive
	}
	seconds := int(c.Keepalive / time.Second)
	if seconds < 1 || seconds > 65535 || c.Keepalive%time.Second != 0 {
		return fmt.Errorf("keepalive must be whole seconds between 1 and 65535")
	}
	if c.StaleAfter == 0 {
		c.StaleAfter = DefaultStaleAfter
	}
	if c.StaleAfter <= 0 {
		return fmt.Errorf("stale timeout must be positive")
	}
	if c.DoHURL != "" {
		if err := validateDoHURL(c.DoHURL); err != nil {
			return err
		}
	}
	return nil
}

func parseKey(name, raw string) ([32]byte, error) {
	var key [32]byte
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return key, fmt.Errorf("%s is required", name)
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != len(key) {
		return key, fmt.Errorf("%s must be 64 hex characters", name)
	}
	copy(key[:], decoded)
	if key == ([32]byte{}) {
		return key, fmt.Errorf("%s must not be all zero", name)
	}
	return key, nil
}

func parseEndpoint(raw string) (netip.AddrPort, error) {
	raw = strings.TrimSpace(raw)
	addr, err := netip.ParseAddrPort(raw)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("GEOMIND_WG_ENDPOINT must be an IP:port literal")
	}
	if addr.Port() == 0 {
		return netip.AddrPort{}, fmt.Errorf("GEOMIND_WG_ENDPOINT port must not be zero")
	}
	if addr.Addr().IsUnspecified() || addr.Addr().IsMulticast() {
		return netip.AddrPort{}, fmt.Errorf("GEOMIND_WG_ENDPOINT must be a unicast address")
	}
	return addr, nil
}

func parseLocalAddresses(raw string) ([]netip.Addr, error) {
	items := splitList(raw)
	if len(items) == 0 {
		return nil, fmt.Errorf("GEOMIND_WG_LOCAL_ADDRS is required")
	}
	addrs := make([]netip.Addr, 0, len(items))
	for _, item := range items {
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("GEOMIND_WG_LOCAL_ADDRS must be a comma-separated list of IP addresses")
		}
		if addr.IsUnspecified() || addr.IsMulticast() {
			return nil, fmt.Errorf("GEOMIND_WG_LOCAL_ADDRS must be unicast addresses")
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func parseAllowedIPs(raw string) ([]netip.Prefix, error) {
	items := splitList(raw)
	if len(items) == 0 {
		return nil, fmt.Errorf("GEOMIND_WG_ALLOWED_IPS is required")
	}
	prefixes := make([]netip.Prefix, 0, len(items))
	for _, item := range items {
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("GEOMIND_WG_ALLOWED_IPS must be a comma-separated list of CIDRs")
		}
		prefix = prefix.Masked()
		if prefix.Addr().IsMulticast() {
			return nil, fmt.Errorf("GEOMIND_WG_ALLOWED_IPS must not contain multicast prefixes")
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func splitList(raw string) []string {
	var items []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

// validateDoHURL requires HTTPS and an IP-literal host so the resolver address
// never has to be resolved locally.
func validateDoHURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("DoH URL is invalid")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("DoH URL must use https")
	}
	if u.User != nil {
		return fmt.Errorf("DoH URL must not contain user information")
	}
	if u.Path == "" {
		return fmt.Errorf("DoH URL must have a path")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("DoH URL must not contain a query or fragment")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("DoH URL host must be an IP literal")
	}
	if _, err := netip.ParseAddr(host); err != nil {
		return fmt.Errorf("DoH URL host must be an IP literal")
	}
	if port := u.Port(); port != "" {
		value, convErr := strconv.Atoi(port)
		if convErr != nil || value < 1 || value > 65535 {
			return fmt.Errorf("DoH URL port is invalid")
		}
	}
	return nil
}
