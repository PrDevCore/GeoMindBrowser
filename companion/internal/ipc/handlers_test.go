package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"geomind/companion/internal/policy"
)

func testStatus(t *testing.T) TunnelStatus {
	t.Helper()
	st, err := NewStatus(TunnelDown, "", "de", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func testDeps(t *testing.T, loaded bool) Deps {
	t.Helper()
	doc := policy.Doc{Version: 1, DefaultAction: policy.ActionDirect}
	return Deps{
		Policy: func() (policy.Doc, bool) { return doc, loaded },
		PolicyAndPAC: func() (policy.Doc, string, bool, error) {
			return doc, "// pac", loaded, nil
		},
		Tunnel: func() TunnelStatus { return testStatus(t) },
	}
}

func TestHelloHandlerReportsProtocolAndStatus(t *testing.T) {
	handlers := Handlers(testDeps(t, true))
	resp, err := handlers["hello"](context.Background(), nil)
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	result, ok := resp.Payload.(HelloResult)
	if !ok {
		t.Fatalf("payload type %T", resp.Payload)
	}
	if result.ProtocolVersion != ProtocolVersion || result.PolicyVersion != 1 {
		t.Fatalf("got %+v", result)
	}
	if result.Tunnel.State != TunnelDown || result.Tunnel.Region == nil || *result.Tunnel.Region != "de" {
		t.Fatalf("tunnel = %+v", result.Tunnel)
	}
	if result.Tunnel.Exit != nil {
		t.Fatalf("exit should be null, got %v", *result.Tunnel.Exit)
	}
}

func TestHelloWithoutPolicyReportsVersionZero(t *testing.T) {
	resp, err := Handlers(testDeps(t, false))["hello"](context.Background(), nil)
	if err != nil {
		t.Fatalf("hello: %v", err)
	}
	result := resp.Payload.(HelloResult)
	if result.PolicyVersion != 0 {
		t.Fatalf("policyVersion = %d", result.PolicyVersion)
	}
}

func TestHelloRejectsOversizedVersion(t *testing.T) {
	payload := json.RawMessage(`{"extensionVersion":"` + strings.Repeat("a", 65) + `"}`)
	_, err := Handlers(testDeps(t, false))["hello"](context.Background(), payload)
	var handlerErr *HandlerError
	if !errors.As(err, &handlerErr) || handlerErr.Code != "bad_payload" {
		t.Fatalf("error = %v", err)
	}
}

func TestPolicyHandlerRequiresLoadedPolicy(t *testing.T) {
	_, err := Handlers(testDeps(t, false))["policy.get"](context.Background(), nil)
	var handlerErr *HandlerError
	if !errors.As(err, &handlerErr) || handlerErr.Code != "policy_unavailable" {
		t.Fatalf("error = %v", err)
	}
}

func TestPolicyHandlerReturnsArrayForEmptyRules(t *testing.T) {
	resp, err := Handlers(testDeps(t, true))["policy.get"](context.Background(), nil)
	if err != nil {
		t.Fatalf("policy.get: %v", err)
	}
	result := resp.Payload.(PolicyResult)
	if result.PAC != "// pac" {
		t.Fatalf("pac = %q", result.PAC)
	}
	if result.Policy.Rules == nil {
		t.Fatal("rules must encode as an array, not null")
	}
}

func TestTunnelHandlerRejectsInvalidState(t *testing.T) {
	deps := testDeps(t, false)
	deps.Tunnel = func() TunnelStatus {
		return TunnelStatus{State: "sideways", Since: time.Now().UTC().Format(time.RFC3339)}
	}
	_, err := Handlers(deps)["tunnel.status"](context.Background(), nil)
	var handlerErr *HandlerError
	if !errors.As(err, &handlerErr) || handlerErr.Code != "tunnel_unavailable" {
		t.Fatalf("error = %v", err)
	}
}

func TestTunnelHandlerRejectsMissingTimestamp(t *testing.T) {
	deps := testDeps(t, false)
	deps.Tunnel = func() TunnelStatus { return TunnelStatus{State: TunnelDown} }
	if _, err := Handlers(deps)["tunnel.status"](context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestTunnelHandlerRejectsInvalidLocale(t *testing.T) {
	deps := testDeps(t, false)
	deps.Tunnel = func() TunnelStatus {
		st := testStatus(t)
		bad := "de_DE"
		st.Locale = &bad
		return st
	}
	_, err := Handlers(deps)["tunnel.status"](context.Background(), nil)
	var handlerErr *HandlerError
	if !errors.As(err, &handlerErr) || handlerErr.Code != "tunnel_unavailable" {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodePayloadRejectsUnknownFields(t *testing.T) {
	handler := Handlers(testDeps(t, false))["hello"]
	if _, err := handler(context.Background(), json.RawMessage(`{"extensionVersion":"1","extra":true}`)); !errors.Is(err, ErrMalformedMessage) {
		t.Fatalf("error = %v", err)
	}
}

func TestServeReturnsTypedHelloPayload(t *testing.T) {
	responses := serveFrames(t, Handlers(testDeps(t, true)), []byte(`{"id":"h1","type":"hello","payload":{"extensionVersion":"0.1.0"}}`))
	if len(responses) != 1 {
		t.Fatalf("responses = %d", len(responses))
	}
	var result HelloResult
	if err := json.Unmarshal(responses[0].Payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Fatalf("got %+v", result)
	}
}

func TestNewStatusValidatesState(t *testing.T) {
	if _, err := NewStatus("weird", "", "", time.Now()); err == nil {
		t.Fatal("expected error")
	}
}
