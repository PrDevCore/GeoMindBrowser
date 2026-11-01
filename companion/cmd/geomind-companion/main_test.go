package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"geomind/companion/internal/ipc"
)

var proxyPortPattern = regexp.MustCompile(`SOCKS5 127\.0\.0\.1:([0-9]{1,5})`)

func clearExitEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GEOMIND_EXIT_REGION",
		"GEOMIND_EXIT_TIMEZONE",
		"GEOMIND_EXIT_LOCALE",
		"GEOMIND_EXIT_DOH_URL",
		"GEOMIND_POLICY",
		"GEOMIND_KNOWN_EXITS",
		"GEOMIND_EXIT_ID",
		"GEOMIND_WG_PRIVATE_KEY",
		"GEOMIND_WG_PEER_PUBLIC_KEY",
		"GEOMIND_WG_ENDPOINT",
		"GEOMIND_WG_LOCAL_ADDRS",
		"GEOMIND_WG_ALLOWED_IPS",
		"GEOMIND_WG_DOH",
		"GEOMIND_WG_MTU",
		"GEOMIND_WG_KEEPALIVE",
	} {
		t.Setenv(key, "")
	}
}

func frame(t *testing.T, payload string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := ipc.WriteMessage(&buf, []byte(payload)); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	return buf.Bytes()
}

func readResponses(t *testing.T, out *bytes.Buffer) []ipc.Envelope {
	t.Helper()
	var responses []ipc.Envelope
	for {
		raw, err := ipc.ReadMessage(out)
		if errors.Is(err, io.EOF) {
			return responses
		}
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		var env ipc.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		responses = append(responses, env)
	}
}

func writePolicy(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	data := `{
		"version": 1,
		"defaultAction": "direct",
		"rules": [
			{"id": "de", "host": "*.example.com", "action": "route", "exit": "de-test"}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunServesHandshakePolicyAndStatus(t *testing.T) {
	clearExitEnv(t)
	var input bytes.Buffer
	input.Write(frame(t, `{"id":"1","type":"hello","payload":{"extensionVersion":"0.1.0"}}`))
	input.Write(frame(t, `{"id":"2","type":"policy.get"}`))
	input.Write(frame(t, `{"id":"3","type":"tunnel.status"}`))

	var out, errOut bytes.Buffer
	err := run(
		[]string{"-policy", writePolicy(t), "-known-exits", "de-test", "-proxy-addr", "127.0.0.1:0"},
		&input, &out, &errOut,
	)
	if err != nil {
		t.Fatalf("run: %v (stderr %q)", err, errOut.String())
	}
	responses := readResponses(t, &out)
	if len(responses) != 3 {
		t.Fatalf("responses = %d", len(responses))
	}
	if responses[0].Type != "hello.result" || responses[1].Type != "policy.result" || responses[2].Type != "tunnel.status.result" {
		t.Fatalf("types = %q, %q, %q", responses[0].Type, responses[1].Type, responses[2].Type)
	}

	var hello ipc.HelloResult
	if err := json.Unmarshal(responses[0].Payload, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.ProtocolVersion != ipc.ProtocolVersion || hello.PolicyVersion != 1 {
		t.Fatalf("hello = %+v", hello)
	}
	if hello.Tunnel.State != ipc.TunnelDown {
		t.Fatalf("state = %q", hello.Tunnel.State)
	}

	var policyResult ipc.PolicyResult
	if err := json.Unmarshal(responses[1].Payload, &policyResult); err != nil {
		t.Fatal(err)
	}
	match := proxyPortPattern.FindStringSubmatch(policyResult.PAC)
	if match == nil {
		t.Fatalf("PAC has no loopback proxy: %q", policyResult.PAC)
	}
	port, err := strconv.Atoi(match[1])
	if err != nil || port < 1 || port == 9 {
		t.Fatalf("PAC proxy port = %q", match[1])
	}
	if len(policyResult.Policy.Rules) != 1 || policyResult.Policy.Rules[0].ID != "de" {
		t.Fatalf("policy = %+v", policyResult.Policy)
	}
}

func TestRunReportsExitLocale(t *testing.T) {
	clearExitEnv(t)
	t.Setenv("GEOMIND_EXIT_REGION", "de")
	t.Setenv("GEOMIND_EXIT_LOCALE", "de-DE")

	var input bytes.Buffer
	input.Write(frame(t, `{"id":"1","type":"hello","payload":{"extensionVersion":"0.1.0"}}`))
	var out, errOut bytes.Buffer
	if err := run([]string{"-proxy-addr", "127.0.0.1:0"}, &input, &out, &errOut); err != nil {
		t.Fatalf("run: %v (stderr %q)", err, errOut.String())
	}
	responses := readResponses(t, &out)
	if len(responses) != 1 {
		t.Fatalf("responses = %d", len(responses))
	}
	var hello ipc.HelloResult
	if err := json.Unmarshal(responses[0].Payload, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Tunnel.Locale == nil || *hello.Tunnel.Locale != "de-DE" {
		t.Fatalf("locale = %v", hello.Tunnel.Locale)
	}
	if hello.Tunnel.Region == nil || *hello.Tunnel.Region != "de" {
		t.Fatalf("region = %v", hello.Tunnel.Region)
	}
}

func TestRuntimeStartsFailClosedProxyWithoutTunnel(t *testing.T) {
	clearExitEnv(t)
	ctx := context.Background()
	rt, err := startRuntime(ctx, runtimeOptions{
		ProxyAddr: "127.0.0.1:0",
		Region:    "de",
		Getenv:    os.Getenv,
	})
	if err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	defer rt.Close()
	go rt.Serve(ctx)

	status := rt.Status()
	if status.State != ipc.TunnelDown {
		t.Fatalf("state = %q", status.State)
	}
	if status.Region == nil || *status.Region != "de" {
		t.Fatalf("region = %v", status.Region)
	}
	assertProxyRefusesWhileDown(t, rt.ProxyAddr())
}

// assertProxyRefusesWhileDown proves the PAC target is live but closed: the
// listener accepts and immediately drops the connection while the tunnel is
// down, so a matched host cannot reach the origin.
func assertProxyRefusesWhileDown(t *testing.T, addr string) {
	t.Helper()
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(context.Background(), "tcp4", addr)
	if err != nil {
		t.Fatalf("PAC proxy address %s is not listening: %v", addr, err)
	}
	defer func() {
		_ = conn.Close()
	}()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("proxy accepted a connection while the tunnel was down")
	}
}

func TestRunRejectsPartialTunnelConfig(t *testing.T) {
	clearExitEnv(t)
	t.Setenv("GEOMIND_WG_PRIVATE_KEY", strings.Repeat("ab", 32))
	var out, errOut bytes.Buffer
	if err := run(nil, bytes.NewReader(nil), &out, &errOut); err == nil {
		t.Fatal("expected error for a partial WireGuard configuration")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay clean, got %q", out.String())
	}
}

func TestRunWithoutPolicyReportsUnavailable(t *testing.T) {
	clearExitEnv(t)
	input := bytes.NewBuffer(frame(t, `{"id":"1","type":"policy.get"}`))
	var out, errOut bytes.Buffer
	if err := run(nil, input, &out, &errOut); err != nil {
		t.Fatalf("run: %v", err)
	}
	responses := readResponses(t, &out)
	if len(responses) != 1 || responses[0].Type != "error" {
		t.Fatalf("responses = %+v", responses)
	}
	var payload ipc.ErrorPayload
	if err := json.Unmarshal(responses[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "policy_unavailable" {
		t.Fatalf("code = %q", payload.Code)
	}
}

func TestRunRejectsInvalidPolicy(t *testing.T) {
	clearExitEnv(t)
	var out, errOut bytes.Buffer
	err := run([]string{"-policy", filepath.Join(t.TempDir(), "missing.json")}, bytes.NewReader(nil), &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.Len() != 0 {
		t.Fatalf("stdout must stay clean, got %q", out.String())
	}
}

func TestRunRejectsInvalidExitEnvironment(t *testing.T) {
	clearExitEnv(t)
	t.Setenv("GEOMIND_EXIT_TIMEZONE", "Mars/Base")
	var out, errOut bytes.Buffer
	if err := run(nil, bytes.NewReader(nil), &out, &errOut); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunHelpIsNotAnError(t *testing.T) {
	clearExitEnv(t)
	var out, errOut bytes.Buffer
	if err := run([]string{"-h"}, bytes.NewReader(nil), &out, &errOut); err != nil {
		t.Fatalf("run -h: %v", err)
	}
}

func TestRunRejectsUnknownFlag(t *testing.T) {
	clearExitEnv(t)
	var out, errOut bytes.Buffer
	if err := run([]string{"-nope"}, bytes.NewReader(nil), &out, &errOut); err == nil {
		t.Fatal("expected error")
	}
}
