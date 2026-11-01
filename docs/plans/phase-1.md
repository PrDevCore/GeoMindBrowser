# Phase 1 Plan: MV3 extension, Go companion, offline AI sidebar

Status: **approved 2026-10-30**. Steps 1-5 are implemented; all locally runnable suites pass.
Step 5's Linux packet-capture half and the AC2 proof are still missing. Policy hot reload,
live tunnel status, and locale header rules landed 2026-10-30 (ADR-0012, ADR-0013,
ADR-0014). The independent security review returned **FAIL**; R1 (same-user process takes
over the stale loopback port) was closed as an explicit Phase 1 scope decision (ADR-0015).
R2, R3, and R5-R8 were fixed 2026-11-01 with tests; R4 was decided the same day (ADR-0019:
geolocation blocked on routed origins, timezone/language parity accepted for Phase 1).
Phase 1 is still not complete: the Linux capture half of step 5 is missing. Step 6 needs
llama.cpp and model approval.
Verification is recorded in `project-state/progress.md`.

Decisions taken 2026-10-30: Go for the companion, llama.cpp for local inference. See
docs/decisions.md (ADR-0001, ADR-0002). Everything else below is a proposal for approval.

## 1. Goal

Ship a Phase 1 client that does three things and nothing else:

1. Applies per-site geo-routing rules (`*.example.com -> Germany`) through a userspace
   WireGuard tunnel, failing closed when the tunnel is down.
2. Proves no DNS or WebRTC leak on routed traffic in an automated test.
3. Provides an AI sidebar that works fully offline and reads a page only when the user
   invokes it.

## 2. Non-goals (Phase 1)

- Mode B remote browser sessions, MASQUE, Kasm/Guacamole.
- Real exit infrastructure, Terraform, Nomad/Kubernetes. Phase 1 tests use a local test WG
  peer only.
- Cloud AI and PII redaction (interface reserved, not implemented).
- Agent mode, purchases, logins, form submission.
- A Chromium fork.
- An OS-level firewall kill switch (requires changing system network settings; approval
  needed and it is not in the acceptance criteria).
- Full fingerprint normalization beyond header and locale consistency.
- Timezone override (see open questions; this is a known gap against the networking
  invariant).
- Resistance to a hostile process running as the same OS user (E8). Phase 1 trusts the OS
  user boundary; see ADR-0015.

## 3. Architecture decisions

| Area | Decision | Why |
| --- | --- | --- |
| Companion | Go | wireguard-go is the reference userspace implementation; netstack avoids TUN privileges; static cross-compiled binaries; native-messaging host is small |
| Tunnel | wireguard-go with `tun/netstack` | Userspace TCP/IP (gVisor netstack) means no root, no system TUN, and no OS route that could leak; dialing only succeeds through the tunnel |
| DNS | SOCKS5 hostname pass-through, resolved by a DoH client dialing through the netstack | The local resolver is never called for matched hosts |
| Routing | Chrome `proxy` API with a locally generated PAC script | Only supported MV3 way to route per host; PAC with no DIRECT fallback gives fail-closed behavior |
| Control plane | Native messaging | No loopback TCP control port that a web page or other local process could reach |
| Extension identity | Fixed `key` in manifest.json | Native messaging host manifests need a stable extension ID |
| AI | llama.cpp `llama-server` child process, loopback, per-launch API key | Swappable behind `AIProvider`; no Python runtime; offline |
| Page reads | `activeTab` + `scripting.executeScript` on user gesture | No declared host permissions, no content scripts, no passive reads |
| AI on routed tabs | Disabled in Phase 1 | AGENTS.md constraint 3: geo-isolated tabs excluded by default |

## 4. Acceptance criteria and how they are proven

| # | Criterion (from kickoff) | Proof |
| --- | --- | --- |
| AC1 | `*.example.com -> Germany` style rules apply correctly | Shared policy fixtures pass in Go and TypeScript; PAC unit tests; browser routing test observes the tunnel peer |
| AC2 | A WebRTC/DNS leak test shows the exit region, not the real one | Linux netns suite with tcpdump: no local DNS queries, no host/public ICE candidates, no direct packets to origin |
| AC3 | AI sidebar works fully offline and reads a page only when invoked | Offline netns (no default route) summarizing a local page; capture shows loopback only; test asserts no page read before the click |

## 5. File layout

Directories are created empty in the skeleton. Files appear only in the step that owns them.

```
extension/
  manifest.json                 step 2
  src/background/               service worker: policy sync, PAC, privacy policy
  src/sidebar/                  side panel UI
  src/content/                  (only if activeTab injection needs helpers)
  src/lib/                      shared TS: policy client, IPC, prompt envelope
  test/unit/                    vitest unit tests
  test/fixtures/                injection HTML fixtures
  public/                       icons
companion/
  go.mod                        step 1
  cmd/geomind-companion/        main, flags, wiring
  internal/ipc/                 native messaging codec + typed dispatch
  internal/policy/              load, validate, match, PAC generation
  internal/tunnel/              WireGuard device + supervisor
  internal/proxy/               SOCKS5 server + health gate
  internal/ai/                  llama-server lifecycle, prompt assembly, streaming
  internal/exitinfo/            exit region, timezone, locale metadata
  testdata/                     test certs/config (no secrets)
shared/
  schema/                       IPC and policy schemas (JSON Schema)
  fixtures/policy/              cross-language policy test vectors
tests/
  leak/                         netns harness: DNS, WebRTC, kill switch, restore
  injection/                    prompt injection end-to-end fixtures
  offline-ai/                   no-uplink AI test
scripts/                        native host installer (sh, ps1)
docs/
  threat-model.md
  decisions.md
  plans/phase-1.md
project-state/
```

No `infra/` in Phase 1; real infrastructure needs approval and a later phase.

## 6. Interfaces

### 6.1 Policy document

```json
{
  "version": 1,
  "defaultAction": "direct",
  "rules": [
    { "id": "de-example", "host": "*.example.com", "action": "route", "exit": "de-test" },
    { "id": "exact", "host": "shop.example.org", "action": "route", "exit": "de-test" },
    { "id": "blocked", "host": "*.blocked.test", "action": "block" }
  ]
}
```

Matching semantics, all of which get fixtures:

- Hosts are lowercased, a trailing dot is stripped, and IDNs are compared in punycode.
- Exact patterns match one host only.
- `*.example.com` matches one or more labels below the apex (`a.example.com`,
  `a.b.example.com`) and does **not** match the apex `example.com`. A user who wants both
  adds two rules.
- No regex, no paths, no ports, no CIDR in Phase 1. Ports and paths are a later feature.
- Precedence: exact beats wildcard; longer wildcard suffix beats shorter; explicit
  `priority` (higher wins) breaks remaining ties; then rule order. Deterministic and
  documented in fixtures.
- A `route` rule whose exit is unknown or unhealthy is `block`, never `direct`.
- The default action applies only to hosts with no matching rule.

### 6.2 Extension <-> companion IPC (native messaging)

Envelope: `{ "id": string, "type": string, "payload": object }`. One request, one or more
responses. Chrome limits host-to-extension messages to 1 MB, so AI output is chunked.

| Type | Direction | Payload |
| --- | --- | --- |
| `hello` | ext -> comp | `{ extensionVersion }` |
| `hello.result` | comp -> ext | `{ protocolVersion, policyVersion, tunnel: TunnelStatus }` |
| `policy.get` | ext -> comp | `{}` |
| `policy.result` | comp -> ext | `{ policy: PolicyDoc, pac: string }` |
| `policy.changed` | comp -> ext | `{ policyVersion }` (no browsing data) |
| `tunnel.status` | ext -> comp | `{}` |
| `tunnel.status.result` | comp -> ext | `TunnelStatus` |
| `ai.summarize` | ext -> comp | `{ requestId, mode: "page" \| "selection", text, trust: "untrusted", region }` |
| `ai.chunk` | comp -> ext | `{ requestId, delta }` |
| `ai.done` | comp -> ext | `{ requestId }` |
| `ai.error` | comp -> ext | `{ requestId, code, message }` |

`TunnelStatus`: `{ state: "up" | "down" | "starting", exit: string \| null, region: string \| null, since: string }`.
Message size is capped at 256 KB outgoing (text is truncated with an explicit marker) and
validated on both sides. Unknown types are rejected, never ignored.

### 6.3 Go interfaces

```go
type Decision struct {
    Action string // "direct", "route", "block"
    Exit   string
}

type PolicyStore interface {
    Load(path string) (PolicyDoc, error) // validate; reject unknown fields
    Match(host string) Decision
    PAC(proxyAddr string) (string, error) // generated, validated
}

type Tunnel interface {
    Start(ctx context.Context, cfg TunnelConfig) error
    Stop(ctx context.Context) error
    Healthy() bool
    DialContext(ctx context.Context, network, addr string) (net.Conn, error)
    Resolver() DNSResolver // DoH through the tunnel only
}

type Proxy interface {
    Addr() string
    Serve(ctx context.Context) error
    Close() error
}

type AIProvider interface {
    Summarize(ctx context.Context, req SummarizeRequest, emit func(delta string) error) error
    Close() error
}

type SummarizeRequest struct {
    RequestID string
    Mode      string // "page" or "selection"
    Text      string // untrusted data, never instructions
    Region    string
}
```

The proxy depends on `Tunnel`, not on a concrete WireGuard type, so the kill-switch tests
can use a fake tunnel without weakening production code.

### 6.4 Prompt envelope

The prompt is assembled as: fixed system policy, then a delimited block:

```
<untrusted_page_content>
...page text, escaped so it cannot close the delimiter...
</untrusted_page_content>
```

The system policy states that the block is data, that it cannot change instructions, and
that the assistant has no tools. The sidebar renders plain text with links disabled.

## 7. Routing, privacy, and kill-switch design

- **PAC generation.** For each rule the PAC emits an exact comparison or `shExpMatch`, in
  precedence order. Matched hosts return `SOCKS5 127.0.0.1:<port>`. There is **no**
  `DIRECT` fallback for matched hosts. `block` returns a dead loopback proxy
  (`SOCKS5 127.0.0.1:9`) so the request fails rather than escaping.
- **PAC validation.** The companion validates the PAC by parsing it and checking that every
  `route` rule maps to the proxy and no matched branch can return `DIRECT`. The extension
  refuses to install an invalid PAC. Because an unparseable PAC makes Chrome go DIRECT, the
  failure mode is "routing disabled and surfaced", not "silently direct"; the UI must show
  this state.
- **Proxy gate.** SOCKS5 accepts a connection only if `Tunnel.Healthy()`. Dialing uses only
  `Tunnel.DialContext`. No direct `net.Dial` exists in the proxy package (enforced by a
  lint rule and a test that greps the package).
- **DNS.** SOCKS5 receives the hostname and the DoH client resolves it through the netstack.
  The DoH endpoint is configured as an IP, not a hostname, to avoid bootstrap resolution.
- **WebRTC.** `chrome.privacy.network.webRTCIPHandlingPolicy =
  "disable_non_proxied_udp"`, applied at startup and re-applied on change. This is global
  and breaks WebRTC on non-routed tabs too; documented.
- **QUIC.** Verify Chromium disables QUIC under a proxy; test asserts no UDP/443 to origin.
- **Restore.** PAC is a persisted browser setting. The service worker re-fetches policy and
  revalidates before enabling routing; tabs restored before that stay on the last valid PAC.
- **Headers/locale.** `declarativeNetRequest` modifies `Accept-Language` for hosts with a
  region rule. This needs host access, so those hosts become optional host permissions the
  user grants when adding a rule. No `<all_urls>`.

## 8. Steps

Each step is one Quest, reviewed and approved separately. "Files" lists what the step may
create or change; it must not touch unrelated code.

### Step 1: Policy core and PAC generator

Status: done 2026-10-30 (see project-state/progress.md).

- Files: `companion/internal/policy/**`, `companion/go.mod`, `extension/src/lib/policy.ts`,
  `extension/package.json`, `extension/tsconfig.json`, `extension/eslint.config.mjs`,
  `shared/fixtures/policy/**`, `shared/schema/policy.schema.json`.
- Build: parser with strict validation, matcher, PAC generator and validator; TypeScript
  mirror of the matcher for PAC tests and UI previews.
- Tests: Go table tests and Vitest over the same fixtures (exact, wildcard, apex exclusion,
  case, trailing dot, punycode, precedence, conflicts, unknown exit, malformed policy).
  PAC assertions: no DIRECT branch for matched hosts, block maps to dead proxy.
- Acceptance: `go test ./...` and `npm test` pass over identical fixtures; PAC snapshot
  contains no matched-host DIRECT fallback.
- Dependencies: Go toolchain; TypeScript, Vitest, ESLint (approval list).

### Step 2: Companion skeleton and native messaging

Status: done 2026-10-30 (see project-state/progress.md).

- Files: `companion/cmd/geomind-companion/**`, `companion/internal/ipc/**`,
  `companion/internal/exitinfo/**`, `extension/manifest.json`, `extension/src/background/**`,
  `scripts/install-native-host.sh`, `scripts/install-native-host.ps1`, `shared/schema/ipc.schema.json`.
- Build: length-prefixed native messaging codec, typed dispatch, `hello`/`policy.get`/
  `tunnel.status` with a fake tunnel, extension service-worker bootstrap, fixed extension
  key, minimal permissions (`nativeMessaging`, `proxy`, `privacy`, `sidePanel`, `scripting`,
  `activeTab`, `storage`).
- Tests: codec round-trip, oversized and malformed messages, unknown type rejection,
  dispatch contract; no listening sockets in the companion (test enumerates listeners).
- Acceptance: host installs and handshake works on the dev OS; `ss`/`lsof` shows no TCP
  listener from the companion in this step.

### Step 3: Proxy, supervisor, fail-closed gate

Status: done 2026-10-30 (see project-state/progress.md).

- Files: `companion/internal/proxy/**`, `companion/internal/tunnel/supervisor.go`,
  `tests/leak/companion/**`.
- Build: SOCKS5 (no auth, loopback only), health gate, supervisor with staleness timeout,
  a `Tunnel` fake for tests.
- Tests: CONNECT refused when unhealthy; in-flight connection fails on `Stop`;
  hostname is passed to the dialer (no local resolve); no `net.Dial` in the package;
  supervisor marks stale; kill-mid-transfer test with curl.
- Acceptance: with the fake tunnel, killing it mid-download produces a client error and no
  retry through a direct path; the capture shows no direct connection attempt.

### Step 4: WireGuard tunnel

Status: done 2026-10-30 (see project-state/progress.md).

- Files: `companion/internal/tunnel/**`, `companion/testdata/**` (test config, no real keys),
  `tests/leak/wg/**`.
- Build: wireguard-go + netstack device behind `Tunnel`; config from env; keys in memory;
  DoH client through netstack; no key persistence, no config file with secrets.
- Tests: two local peers in netns; traffic reaches the peer; DNS query observed at the exit
  resolver and not at the local resolver; handshake failure leaves `Healthy() == false`.
- Acceptance: netns test shows HTTP and DNS only through the tunnel; killing the interface
  mid-request stops traffic.
- Gate: confirmed 2026-10-30. The user approved `golang.zx2c4.com/wireguard`; no standalone
  third-party audit of the Go implementation exists, recorded in ADR-0009.
- Local verification used two in-process peers over loopback instead of netns. The
  `tests/leak/wg/run.sh` capture harness exists but has not run on the Windows dev host.

### Step 5: Extension routing and leak suite

Status: implemented 2026-10-30; Linux netns DNS/WebRTC/QUIC/IPv6 capture and the
restart/tab-restore run are still missing. Review findings R2, R3, and R5-R8 were fixed
2026-11-01; R4 was decided 2026-11-01 (ADR-0019) and R1 is scoped out (ADR-0015). See
project-state/progress.md.

- Files: `extension/src/background/**`, `extension/src/lib/**`, `extension/src/options/**`,
  `extension/options.html`, `extension/options.css`, `extension/manifest.json`,
  `companion/cmd/geomind-companion/**`, `shared/fixtures/locale/**`, `tests/leak/run.sh`,
  `tests/leak/browser/**`, root `package.json`/`playwright.config.ts`.
- Build: install PAC via `chrome.proxy`, WebRTC policy, policy sync, error UI for invalid
  PAC. The companion starts the tunnel and proxy, watches the policy file, and emits
  `policy.changed`. Optional host permissions and `Accept-Language` header rules are
  managed from a minimal options page. `TunnelStatus` carries the exit locale.
- Tests: PAC validation unit tests; locale format fixtures shared by Go and TypeScript;
  header-plan and DNR install unit tests; Playwright Chromium with a real native host proves
  PAC installation, WebRTC policy, fail-closed routing, direct control traffic, live tunnel
  polling, hot reload of an edited policy (including an invalid reload keeping the old PAC),
  and that no header rules exist before a permission grant. Still needs the Linux netns
  DNS/WebRTC/QUIC/IPv6 capture and restart/tab-restore run.
- Acceptance: AC1 is proven in Chromium for the routing path; AC2 still needs the netns
  capture to show zero local DNS queries and no real ICE candidates.
- Dependencies: Playwright 1.64.1 and pinned Chromium 156 (approved 2026-10-30).

### Step 6: Offline AI sidebar

- Files: `extension/src/sidebar/**`, `extension/src/lib/prompt.ts`,
  `companion/internal/ai/**`, `tests/offline-ai/**`, `extension/test/fixtures/injection/**`.
- Build: side panel, explicit invocation button, selection Q&A, chunked streaming, llama-server
  lifecycle (random loopback port, per-launch API key, SHA-256 model check, no prompt
  logging), prompt envelope, plain-text rendering.
- Tests: prompt envelope snapshot; injection fixtures assert structural separation and log
  model responses for review; no page read before invocation; AI disabled on routed tabs;
  offline netns with no default route still summarizes; capture shows loopback only.
- Acceptance: AC3.

### Step 7: Hardening, docs, full suite, security review

- Files: docs, state files, CI adjustments.
- Work: locale/Accept-Language table test, threat-model update (requires approval),
  full suite and linter, independent security-review session per rule 06, Phase 1 report.
- Acceptance: all suites run with real pasted output; findings triaged; docs updated.

## 9. Test plan

### Unit and contract

- Policy fixtures shared between Go and TS (single source of truth).
- PAC generator: no DIRECT for matched hosts; block is a dead proxy; precedence order.
- IPC codec: framing, size limits, malformed input, unknown types.
- Proxy gate: unhealthy tunnel refuses; no direct dial path.
- Prompt envelope: delimiter escaping, page text cannot close the block.

### Integration (Linux network namespaces, root)

The browser runs in a netns whose only route is a veth to a capture host. The test WG peer
and an exit DoH/HTTP server run on the capture host. tcpdump records everything.

| Suite | Assertion |
| --- | --- |
| DNS leak | Local resolver log empty for matched hosts; exit DoH log has the query |
| WebRTC leak | No ICE candidate contains a real or local address; no UDP to the STUN test IP |
| Kill switch | Kill WG mid-download; request errors; no direct SYN, DNS, or QUIC afterwards |
| Tab restore | Kill browser and restore; first request goes to the proxy; no direct attempt |
| QUIC/IPv6 | No UDP/443 or direct IPv6 to the origin |
| Offline AI | With no default route, summarize still works; capture shows loopback only |

These require root and Linux. The dev machine is Windows, so this suite runs in CI or a
Linux VM; it cannot be run locally as configured today.

### Out of scope for automated tests in Phase 1

- Real exit nodes (no infra before approval).
- macOS and Windows packet capture (manual smoke test only).
- Model answer quality (fixtures review, not assertions).

## 10. Risks

| Risk | Severity | Mitigation |
| --- | --- | --- |
| Invalid PAC silently falls back to DIRECT | Critical | Validate before install; block routing and surface an error; dedicated test |
| wireguard-go audit status unclear | High | Confirm audit/version before Step 4; document in decisions |
| Chrome QUIC or preconnect bypasses the proxy | High | Capture tests; disable QUIC where supported; treat as a release blocker |
| Native messaging install differs on Windows/macOS | Medium | Installer scripts per OS; CI covers Linux; manual smoke on Windows |
| Timezone invariant unmet | Medium | Accepted for Phase 1 (ADR-0019); geolocation blocked on routed origins; do not claim timezone matching |
| MV3 service worker suspension during streaming | Medium | Port-based connection keeps the worker alive; reconnect and resume tests |
| Playwright extension loading changes between versions | Medium | Pin Playwright and Chromium; if it breaks, fall back to system Chrome with a policy file |
| Model is large and slow offline | Low | Small quantized model default; model path and hash from env |
| Leak tests flaky in CI | Medium | Privileged netns job, fixed ports, retries only for setup, not assertions |
| Same-user process binds the stale loopback proxy port | High | Out of scope for Phase 1 (ADR-0015); reconnect and proxy-drift detection are follow-up hardening, not a closure; OS-level isolation is a later phase |

## 11. Dependencies and environment to approve

Installed for Step 1: Go 1.27.3 (user-local, checksum-verified), TypeScript 6.0.3,
ESLint 10.14.0, Vitest 5.0.5, and golangci-lint 2.15.0. Since then: the WireGuard modules
(ADR-0009) and Playwright 1.64.1 with Chromium 156 (ADR-0010). Still pending: llama.cpp and
a model, gitleaks, and pinning the GitHub actions by SHA.

| Item | Purpose | Gate |
| --- | --- | --- |
| Go toolchain 1.27.3 (user-local) | Companion, Steps 1-6 | Installed 2026-10-30 |
| `golang.zx2c4.com/wireguard` and `golang.org/x/{crypto,net}` | Tunnel, netstack, DoH | Network and crypto dependency |
| llama.cpp `llama-server` binary | Local inference | Network dependency |
| GGUF model file (path and license TBD) | Inference | License, size, network |
| Playwright + pinned Chromium | Browser leak tests | Network dependency |
| TypeScript, ESLint, Vitest, @types/chrome | Extension build and tests | Dev dependencies |
| golangci-lint | Linter | Tooling |
| gitleaks (optional) | CI secret scan | Tooling |
| GitHub official actions (checkout, setup-node, setup-go) | CI | Already declared; pin by SHA later |

`wireguard-go` is the WireGuard project's reference userspace implementation; the protocol
has been formally analyzed, but I cannot confirm a standalone third-party audit of the Go
implementation. The user approved the dependency on 2026-10-30 with that gap recorded
(ADR-0009); the Phase 1 security review must still assess it.

## 12. Open questions (decision needed)

1. **Timezone — decided 2026-11-01 (ADR-0019).** Option (a): accept the gap for Phase 1,
   block geolocation on routed origins, and treat timezone/language parity as a Mode B
   property. The debugger route (b) and OS timezone changes (c) stay rejected. Revisit only
   before any claim of full locale matching.
2. **OS firewall kill switch.** Proxy-level fail-closed covers browser traffic only.
   Non-browser traffic still uses the real IP. Add a firewall kill switch in Phase 1, or
   defer?
3. **Per-region profiles.** Shared cookies across regions can re-link identities. Defer to
   Phase 2 or require separate profiles now?
4. **OS-level isolation against same-user processes.** Phase 1 accepts the OS user boundary
   (ADR-0015). Is a separate user, firewall, or authenticated data plane required before
   Phase 2?

## 13. Invariant-to-test mapping (networking rule)

| Change area | Invariant | Test |
| --- | --- | --- |
| PAC generator | Fail closed; no DIRECT fallback | PAC unit + browser fail-closed test |
| Proxy gate | Tunnel down means stop | Companion kill-mid-transfer + netns |
| DoH in netstack | DNS at exit only | DNS leak suite (pending Linux capture) |
| WebRTC policy | No local/public candidates | Applied in Chromium; candidate capture pending |
| Header rules | Accept-Language matches region | Plan and install unit tests; post-grant application still unverified (opt-in, granted origins only) |
| Companion loss / stale port | Fail closed against process and network failure | Out of scope for a same-user process (E8, ADR-0015); reconnect and drift detection pending |
| Proxy drift | Fail closed / visible degradation | Fixed 2026-11-01: install read-back, 5s guard, onChange re-assert, badge on unrecoverable drift; Playwright drift test |
| Timezone | Matches exit | **Unmet; open question** |
| AI reads | User invocation only; routed tabs excluded | Offline AI + invocation test |

## 14. Definition of done per step

Matches AGENTS.md: code and tests, tests and linter actually run with pasted output, docs and
state updated, and a short note on what works, what is stubbed, and what is risky.
