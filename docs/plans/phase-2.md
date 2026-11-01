# Phase 2 Plan: Mode B remote browser sessions (hosted)

Status: **approved 2026-11-01; steps 1 and 2 implemented and locally verified. Steps 3-7
not started.**

Mode B is listed as Phase 3+ in `docs/threat-model.md`; this plan pulls the hosted-session
slice forward at the user's request. Per `docs/threat-model.md` and `.qoder/rules/01-workflow.md`,
implementation starts only after this plan and the open decisions below are approved.

## 1. What "hosting Mode B" means

Mode B is not a hosting layer for the Phase 1 client. It is a new service:

- A control plane that creates one ephemeral Chromium container per user request.
- A streaming edge that delivers pixels and input to the user's browser.
- An egress path so the remote browser leaves from the chosen exit, not the user's IP.
- Identity, TLS, rate limiting, abuse handling, and full teardown.

Hosting requires a Linux container host or cluster, a domain with TLS, an identity provider,
and cloud credentials. **This Windows dev host has none of these** (no Docker, no WSL distro,
no kubectl, no Terraform, no cloud account). Local unit tests can run here; integration and
hosting cannot. A staging target must be provided before any hosted acceptance test.

Content-confidentiality note that must appear in user docs: in Mode B, TLS terminates inside
the session container, so the exit operator can see page content. Mode A does not have this
property. Mode B is stronger for IP isolation and weaker for content confidentiality.

## 2. Goals

- One ephemeral Chromium session per explicit user action, torn down completely on close,
  idle timeout, or hard TTL.
- Stream rendered pixels and input over an encrypted, authenticated channel using a vetted
  provider (no custom crypto, no custom codec).
- Egress from the session only through the approved proxy/exit path; no direct host network.
- No persistent disk, no shared volumes, no cross-session state.
- Per-session and per-user resource caps, concurrent-session caps, and rate limits.
- Operational data limited to content-free counters and lifecycle events; no URLs, page
  content, pixels, or per-user browsing logs (AGENTS.md hard constraint 2).

## 3. Non-goals for this phase

- Agent actions, purchases, logins, or form submission automation.
- Cloud AI.
- New exit infrastructure beyond the single approved region for the first slice.
- Chromium forks.
- Billing and public self-service signup.
- Real-IP guarantees for a hostile same-user process (Phase 1 boundary, ADR-0015).

## 4. Proposed stack (requires approval)

Defaults come from AGENTS.md. Recommended choices, with the alternatives:

| Concern | Recommended | Alternative | Why |
| --- | --- | --- | --- |
| Streaming | Kasm Workspaces with KasmVNC | Apache Guacamole (RDP/VNC) | Container-native ephemeral browser sessions; provider owns pixel/input transport and DTLS/TLS; Guacamole is lighter and Apache-2.0 but needs a desktop image plus guacd |
| Orchestration | Managed Kubernetes | Nomad | One Pod per session, NetworkPolicy, seccomp, read-only root FS, ephemeral emptyDir, native teardown |
| Control plane | Go | Rust | Same language and operational model as the companion; shares policy/validation conventions |
| Infra | Terraform, `plan` only | — | Repo rule: `apply` is the user's action after review |
| Identity | Existing OIDC provider | Short-lived tokens from a minimal auth service | No custom crypto; standard libraries only |
| TLS edge | Managed ingress / load balancer | Envoy + cert-manager | Standard TLS termination; no hand-rolled crypto |

If Kasm licensing or operational weight is unacceptable, Guacamole is the fallback and the
container image includes an X server plus Chromium. That choice changes the image and session
model, so it is an approval item, not an implementation detail.

## 5. Architecture

```
extension / user browser
        |  HTTPS (OIDC bearer token)
        v
control plane (Go API, no page content)
        |  Kubernetes API
        v
per-session Pod: Chromium + streaming agent (non-root, read-only FS, no PVC)
        |  egress policy -> session proxy -> exit -> site
        v
streaming edge (Kasm/Guacamole) --HTTPS/DTLS--> user browser
```

- The extension adds one explicit "Open remote session" action. No page content is read and
  no passive session is created. The Phase 1 PAC/proxy path is untouched.
- The control plane is stateless; session state lives in the cluster object and a short-lived
  token store. No database of visited sites.
- Each session gets a random identifier, a per-session secret, a hard TTL (default 30 min),
  and an idle timeout (default 5 min).
- Teardown deletes the Pod, its emptyDir, the per-session secret, and the streaming
  connection record. Teardown is idempotent and retried until observed gone.
- Network policy: default-deny pod-to-pod; egress only to the session proxy; DNS only through
  the proxy; no host network, no hostPath.

## 6. Interfaces

Control plane HTTP API (versioned, JSON):

- `POST /v1/sessions` body `{ "region": "de-test" }` -> `201 { "id", "streamUrl", "expiresAt" }`
  - authenticated, rate limited, concurrent-session capped.
  - `streamUrl` is short-lived and bound to the session and user.
- `GET /v1/sessions/{id}` -> lifecycle state, no page data.
- `DELETE /v1/sessions/{id}` -> idempotent teardown.
- `GET /healthz` -> no secrets, no session data.

Session state machine: `requested -> provisioning -> ready -> active -> terminating -> gone`,
plus `failed`. Only `ready`/`active` expose a stream URL. Any transition failure moves to
`terminating`; every state has a timeout.

Runtime interface (swappable, no Kubernetes types in handlers):

```go
type Spec struct {
    Region    string
    TTL       time.Duration
    Idle      time.Duration
    CPU       string
    Memory    string
    Egress    string
}

type Session struct {
    ID        string
    State     State
    StreamURL string
    ExpiresAt time.Time
}

type Runtime interface {
    Create(ctx context.Context, spec Spec) (Session, error)
    Get(ctx context.Context, id string) (Session, error)
    Delete(ctx context.Context, id string) error
}
```

Implementations: `kube` (staging/production) and `fake` (unit tests, no Docker). The fake
runtime exercises the full state machine and teardown logic on this Windows host.

Step 1 refinement (2026-11-01): the control plane assigns `Spec.ID` before provisioning so
caps, ownership, and cancellation are accounted for before a workload exists; `Runtime.Create`
must echo that ID and return `ready` or `active`. The implemented result type is
`RuntimeSession`. The fake provider is in `session/internal/session/fake.go`.

Step 2 implementation (2026-11-01): the Kubernetes runtime lives in `session/internal/kube`
(client-go 0.37.2, approved with this step). It creates one Secret, one NetworkPolicy, and one
Pod per session, named `gms-` plus a truncated SHA-256 of the session ID so the name is
DNS-safe and does not leak the ID; the full ID is an annotation. It waits for Pod readiness
within a configurable budget and deletes the Pod first, then the NetworkPolicy, then the
Secret, so egress is never unconstrained for a running Pod. `sessiond` selects the runtime
with `GEOMIND_SESSION_RUNTIME=fake|kube`; the fake default keeps the control plane runnable
on this host. Manifests are asserted in unit tests; nothing has been applied to a cluster.
The NetworkPolicy has no DNS egress rule on purpose: the session resolves names at the exit
through the proxy, matching the Phase 1 DNS invariant.

Streaming provider interface: `Streaming.URL(sessionID, userID) (string, error)` and
`Streaming.Revoke(sessionID)`; Kasm/Guacamole implementation behind it.

## 7. Security invariants and tests

| Invariant | Test |
| --- | --- |
| One Pod per session, no shared volumes, no PVC | Manifest assertion + integration: session A cannot read B's volume or Pod IP |
| Non-root, read-only root FS, seccomp, no hostPath, no host network | Pod spec assertion in unit test; admission check in staging |
| Default-deny pod-to-pod; egress only via proxy | Integration: direct dial fails, proxy dial succeeds |
| Stream URL requires auth and expires | Unit: missing/expired/wrong-user token rejected; integration: expired URL rejected |
| Clipboard off, uploads off, downloads gated | Provider config test + staging manual check |
| Full teardown on close, idle, TTL | Integration: object, secret, and emptyDir gone; repeat delete succeeds |
| Rate limit and concurrency cap | Unit + integration: N+1 requests rejected with 429 |
| No browsing data in logs | Log assertion: no URL/page/query fields; only session IDs and states |
| No custom crypto | Dependency review: TLS/DTLS and tokens from standard libraries |
| No cross-user session access | Integration: user B cannot GET/DELETE user A's session |

Abuse handling is in scope: per-IP and per-user request limits, concurrent-session cap,
session TTL, CPU/memory/egress caps, and an operator kill switch. No residential-proxy or
ban-evasion features.

## 8. Client integration

- Add one explicit entry point (options page or a new extension page) that calls the control
  plane after a user click and opens `streamUrl` in a new tab.
- No content script, no page read, no persistent identifier in the extension.
- The session token is held in memory for the tab lifetime; no storage of visited sites.
- Phase 1 routing stays active; Mode B tabs are remote and do not use the local PAC.

## 9. Delivery steps

Each step ends with tests, lint, and an updated `project-state/progress.md`.

1. **Control plane skeleton (local, testable here).** Go module `session/` with the API,
   state machine, fake runtime, fake streaming provider, auth middleware stub, rate limiter,
   and unit/integration tests. No Kubernetes dependency.
2. **Kubernetes runtime.** Client-go implementation, Pod/Secret/NetworkPolicy builders,
   idempotent teardown, fake-client tests. No cluster required.
3. **Session image.** Chromium + KasmVNC (or Guacamole agent), non-root, read-only root FS,
   seccomp profile, egress configuration, image test that runs locally only where Docker
   exists (CI/Linux, not this host).
4. **Streaming integration.** Provider adapter, short-lived URLs, revocation, clipboard and
   download defaults.
5. **Terraform and CI.** Cluster, registry, ingress, DNS, secrets flow documented (no secrets
   in state or repo); `terraform fmt`/`validate`/`plan` in CI only, never `apply`.
6. **Staging deploy (user action).** User applies Terraform and provides domain, IdP, and
   credentials; we run integration and abuse tests against staging.
7. **Client integration and E2E.** Extension entry point, cross-session isolation, teardown
   timing, and leak checks.

## 10. Acceptance criteria

- A user with a valid token can start one session and load a page through the remote browser.
- Closing the tab, idle timeout, and TTL each remove the Pod, secret, and storage; a repeat
  delete is a no-op.
- User B cannot reach user A's session or Pod.
- Direct egress from the Pod fails; proxied egress succeeds.
- The Pod runs non-root with a read-only root FS and no host mounts.
- The stream endpoint rejects missing, expired, and wrong-user tokens.
- Rate and concurrency limits return a clear error without creating a session.
- Logs contain no URLs, page content, pixels, or per-user browsing history.
- `go test` and `golangci-lint` pass; integration tests pass on the approved Linux target;
  `terraform plan` is reviewed and applied by the user.

## 11. Risks

- **No local runtime:** this host cannot build or run the image or cluster; all container and
  hosting verification needs the approved Linux target. No hosted claim can be made before
  that.
- **Provider choice:** Kasm licensing/weight vs Guacamole integration effort; changing later
  changes the image and streaming adapter.
- **Cost and abuse:** always-on streaming infrastructure is expensive; caps and a kill switch
  are mandatory from step 1.
- **Identity:** OIDC setup and token binding are prerequisites; a home-grown auth service
  would be custom security code and is rejected.
- **Content exposure:** the exit/stream operator sees page content; user-facing docs must say
  so before any hosted release.
- **Phase 1 debt:** AC2 (Linux netns capture) and R4 (timezone/geolocation) are still open.
  Starting Phase 2 before closing them splits verification effort; closing AC2 first is
  recommended.

## 12. Open decisions for approval

1. **Start Phase 2 now, or close AC2/R4 first?** Started 2026-11-01 at the user's request;
   AC2 and R4 remain open Phase 1 debt.
2. **Streaming:** Kasm Workspaces (approved 2026-11-01); Guacamole remains the fallback.
3. **Orchestration and hosting:** managed Kubernetes (approved 2026-11-01). Cloud, region,
   budget, domain, and DNS are still open.
4. **Identity:** which OIDC provider; who owns the tenant. Open.
5. **Integration target:** CI runner with Docker, a dedicated Linux VM, or a managed
   staging cluster. Open.
6. **Threat-model update:** Mode B moves from design-level Phase 3+ to an active phase; this
   requires approval to edit `docs/threat-model.md`. Open.
