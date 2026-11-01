package exitinfo

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestFromEnvEmpty(t *testing.T) {
	info, err := FromEnv(env(nil))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if info != (Info{}) {
		t.Fatalf("got %+v", info)
	}
}

func TestFromEnvValid(t *testing.T) {
	info, err := FromEnv(env(map[string]string{
		"GEOMIND_EXIT_REGION":   " DE ",
		"GEOMIND_EXIT_TIMEZONE": "Europe/Berlin",
		"GEOMIND_EXIT_LOCALE":   "de-DE",
		"GEOMIND_EXIT_DOH_URL":  "https://9.9.9.9/dns-query",
	}))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if info.Region != "de" || info.Timezone != "Europe/Berlin" || info.Locale != "de-DE" {
		t.Fatalf("got %+v", info)
	}
	if info.DoHURL != "https://9.9.9.9/dns-query" {
		t.Fatalf("DoH = %q", info.DoHURL)
	}
}

func TestFromEnvAcceptsIPv6DoH(t *testing.T) {
	if _, err := FromEnv(env(map[string]string{
		"GEOMIND_EXIT_DOH_URL": "https://[2606:4700:4700::1111]/dns-query",
	})); err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
}

func TestFromEnvRejectsInvalidValues(t *testing.T) {
	cases := map[string]map[string]string{
		"region":   {"GEOMIND_EXIT_REGION": "germany"},
		"timezone": {"GEOMIND_EXIT_TIMEZONE": "Mars/Base"},
		"locale":   {"GEOMIND_EXIT_LOCALE": "de_DE"},
		"doh http": {"GEOMIND_EXIT_DOH_URL": "http://9.9.9.9/dns-query"},
		"doh host": {"GEOMIND_EXIT_DOH_URL": "https://dns.example/dns-query"},
		"doh path": {"GEOMIND_EXIT_DOH_URL": "https://9.9.9.9"},
		"doh user": {"GEOMIND_EXIT_DOH_URL": "https://user@9.9.9.9/dns-query"},
		"doh args": {"GEOMIND_EXIT_DOH_URL": "https://9.9.9.9/dns-query?x=1"},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := FromEnv(env(values)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestFromEnvRejectsLocalTimezone(t *testing.T) {
	_, err := FromEnv(env(map[string]string{"GEOMIND_EXIT_TIMEZONE": "Local"}))
	if err == nil || !strings.Contains(err.Error(), "IANA") {
		t.Fatalf("error = %v", err)
	}
}

func TestFromEnvTrimsLocale(t *testing.T) {
	info, err := FromEnv(env(map[string]string{"GEOMIND_EXIT_LOCALE": " de-DE "}))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if info.Locale != "de-DE" {
		t.Fatalf("locale = %q", info.Locale)
	}
}

func TestLocaleFixtures(t *testing.T) {
	data, err := os.ReadFile("../../../shared/fixtures/locale/cases.json")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var fixtures struct {
		Valid   []string `json:"valid"`
		Invalid []string `json:"invalid"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	if len(fixtures.Valid) == 0 || len(fixtures.Invalid) == 0 {
		t.Fatalf("fixtures are empty: %+v", fixtures)
	}
	for _, locale := range fixtures.Valid {
		if _, err := FromEnv(env(map[string]string{"GEOMIND_EXIT_LOCALE": locale})); err != nil {
			t.Errorf("locale %q: %v", locale, err)
		}
	}
	for _, locale := range fixtures.Invalid {
		if _, err := FromEnv(env(map[string]string{"GEOMIND_EXIT_LOCALE": locale})); err == nil {
			t.Errorf("locale %q was accepted", locale)
		}
	}
}
