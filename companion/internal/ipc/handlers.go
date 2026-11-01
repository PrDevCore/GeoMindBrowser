package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"geomind/companion/internal/policy"
)

// Tunnel states exposed to the extension. Anything else is rejected.
const (
	TunnelUp       = "up"
	TunnelDown     = "down"
	TunnelStarting = "starting"
)

// TunnelStatus is the wire representation defined by the phase plan.
type TunnelStatus struct {
	State  string  `json:"state"`
	Exit   *string `json:"exit"`
	Region *string `json:"region"`
	Locale *string `json:"locale"`
	Since  string  `json:"since"`
}

var localePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// HelloResult answers a hello request.
type HelloResult struct {
	ProtocolVersion int          `json:"protocolVersion"`
	PolicyVersion   int          `json:"policyVersion"`
	Tunnel          TunnelStatus `json:"tunnel"`
}

// PolicyResult answers a policy.get request.
type PolicyResult struct {
	Policy policy.Doc `json:"policy"`
	PAC    string     `json:"pac"`
}

// Deps are the runtime dependencies of the Step 2 handlers.
type Deps struct {
	// Policy returns the currently loaded policy, if any.
	Policy func() (policy.Doc, bool)
	// PolicyAndPAC returns the document and its PAC from one snapshot.
	PolicyAndPAC func() (policy.Doc, string, bool, error)
	// Tunnel returns the current fail-closed tunnel status.
	Tunnel func() TunnelStatus
}

// HandlerError is an error with a stable wire code and a safe message.
type HandlerError struct {
	Code    string
	Message string
}

func (e *HandlerError) Error() string { return e.Message }

// Handlers returns the typed dispatch table for the Step 2 request types.
func Handlers(d Deps) map[string]Handler {
	return map[string]Handler{
		"hello":         helloHandler(d),
		"policy.get":    policyHandler(d),
		"tunnel.status": tunnelHandler(d),
	}
}

type helloPayload struct {
	ExtensionVersion string `json:"extensionVersion,omitempty"`
}

func helloHandler(d Deps) Handler {
	return func(_ context.Context, payload json.RawMessage) (Response, error) {
		var req helloPayload
		if err := DecodePayload(payload, &req); err != nil {
			return Response{}, err
		}
		if len(req.ExtensionVersion) > 64 {
			return Response{}, &HandlerError{Code: "bad_payload", Message: "extensionVersion is too long"}
		}
		status, err := status(d)
		if err != nil {
			return Response{}, err
		}
		policyVersion := 0
		if d.Policy != nil {
			if doc, ok := d.Policy(); ok {
				policyVersion = doc.Version
			}
		}
		return Response{
			Type: "hello.result",
			Payload: HelloResult{
				ProtocolVersion: ProtocolVersion,
				PolicyVersion:   policyVersion,
				Tunnel:          status,
			},
		}, nil
	}
}

func policyHandler(d Deps) Handler {
	return func(_ context.Context, payload json.RawMessage) (Response, error) {
		var req struct{}
		if err := DecodePayload(payload, &req); err != nil {
			return Response{}, err
		}
		if d.PolicyAndPAC == nil {
			return Response{}, &HandlerError{Code: "policy_unavailable", Message: "no policy is loaded"}
		}
		doc, pac, ok, err := d.PolicyAndPAC()
		if err != nil {
			return Response{}, &HandlerError{Code: "policy_unavailable", Message: "policy could not be generated"}
		}
		if !ok {
			return Response{}, &HandlerError{Code: "policy_unavailable", Message: "no policy is loaded"}
		}
		if doc.Rules == nil {
			doc.Rules = []policy.Rule{}
		}
		return Response{Type: "policy.result", Payload: PolicyResult{Policy: doc, PAC: pac}}, nil
	}
}

func tunnelHandler(d Deps) Handler {
	return func(_ context.Context, payload json.RawMessage) (Response, error) {
		var req struct{}
		if err := DecodePayload(payload, &req); err != nil {
			return Response{}, err
		}
		status, err := status(d)
		if err != nil {
			return Response{}, err
		}
		return Response{Type: "tunnel.status.result", Payload: status}, nil
	}
}

func status(d Deps) (TunnelStatus, error) {
	if d.Tunnel == nil {
		return TunnelStatus{}, &HandlerError{Code: "tunnel_unavailable", Message: "tunnel status is unavailable"}
	}
	st := d.Tunnel()
	switch st.State {
	case TunnelUp, TunnelDown, TunnelStarting:
	default:
		return TunnelStatus{}, &HandlerError{Code: "tunnel_unavailable", Message: "tunnel status is invalid"}
	}
	if st.Since == "" {
		return TunnelStatus{}, &HandlerError{Code: "tunnel_unavailable", Message: "tunnel status has no timestamp"}
	}
	if _, err := time.Parse(time.RFC3339, st.Since); err != nil {
		return TunnelStatus{}, &HandlerError{Code: "tunnel_unavailable", Message: "tunnel status timestamp is invalid"}
	}
	if st.Locale != nil && !localePattern.MatchString(*st.Locale) {
		return TunnelStatus{}, &HandlerError{Code: "tunnel_unavailable", Message: "tunnel status locale is invalid"}
	}
	return st, nil
}

// NewStatus builds a validated status value.
func NewStatus(state, exit, region string, since time.Time) (TunnelStatus, error) {
	st := TunnelStatus{State: state, Since: since.UTC().Format(time.RFC3339)}
	switch state {
	case TunnelUp, TunnelDown, TunnelStarting:
	default:
		return TunnelStatus{}, fmt.Errorf("invalid tunnel state %q", state)
	}
	if exit != "" {
		st.Exit = &exit
	}
	if region != "" {
		st.Region = &region
	}
	return st, nil
}
