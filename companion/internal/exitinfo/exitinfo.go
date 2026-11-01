// Package exitinfo validates the exit metadata that the companion receives
// from the environment. It never reads or transmits browsing data.
package exitinfo

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	regionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]{1,8})?$`)
	localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

// Info is the exit metadata used for status and locale consistency.
type Info struct {
	Region   string
	Timezone string
	Locale   string
	DoHURL   string
}

// FromEnv reads and validates GEOMIND_EXIT_* values. Empty values are allowed;
// a present but invalid value is an error.
func FromEnv(getenv func(string) string) (Info, error) {
	info := Info{
		Region:   strings.TrimSpace(getenv("GEOMIND_EXIT_REGION")),
		Timezone: strings.TrimSpace(getenv("GEOMIND_EXIT_TIMEZONE")),
		Locale:   strings.TrimSpace(getenv("GEOMIND_EXIT_LOCALE")),
		DoHURL:   strings.TrimSpace(getenv("GEOMIND_EXIT_DOH_URL")),
	}
	if info.Region != "" {
		info.Region = strings.ToLower(info.Region)
		if !regionPattern.MatchString(info.Region) {
			return Info{}, fmt.Errorf("invalid exit region format")
		}
	}
	if info.Timezone != "" {
		if info.Timezone == "Local" {
			return Info{}, fmt.Errorf("exit timezone must be an IANA name")
		}
		if _, err := time.LoadLocation(info.Timezone); err != nil {
			return Info{}, fmt.Errorf("invalid exit timezone: %w", err)
		}
	}
	if info.Locale != "" && !localePattern.MatchString(info.Locale) {
		return Info{}, fmt.Errorf("invalid exit locale format")
	}
	if info.DoHURL != "" {
		if err := validateDoHURL(info.DoHURL); err != nil {
			return Info{}, err
		}
	}
	return info, nil
}

// validateDoHURL requires HTTPS and an IP-literal host so resolving the
// resolver itself cannot go to the local DNS resolver.
func validateDoHURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid DoH URL: %w", err)
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
	if net.ParseIP(u.Hostname()) == nil {
		return fmt.Errorf("DoH URL host must be an IP literal")
	}
	return nil
}
