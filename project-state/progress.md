# Progress

Updated: 2026-11-01

## Phase 1 steps

| Step | Status | Notes |
| --- | --- | --- |
| 0. Plan, threat model, skeleton | done | Approved 2026-10-30 |
| 1. Policy core + PAC generator | done | Go + TypeScript, shared fixtures, golden PAC |
| 2. Companion skeleton + native messaging | done | IPC codec, dispatch, handlers, extension service worker, installers |
| 3. Proxy, supervisor, fail-closed gate | done | Loopback SOCKS5, health gate, supervisor, fake-tunnel tests |
| 4. WireGuard tunnel | done | wireguard-go + netstack, DoH through the tunnel, two-peer tests |
| 5. Extension routing + leak suite | done (Linux capture pending) | PAC install, privacy policy, badge, polling, hot reload, locale headers, browser fail-closed tests; Linux netns packet capture not run |
| 6. Offline AI sidebar | blocked | Needs llama.cpp and model approval |
| 7. Hardening, docs, full suite, security review | blocked | Full suite green; R2, R3, R5-R8 fixed 2026-11-01; R1 scoped out (ADR-0015); R4 decided with geolocation blocked (ADR-0019), timezone/language documented as out of scope; AC2/AC3 incomplete |

## Phase 2 (Mode B) steps

Plan: `docs/plans/phase-2.md`, approved 2026-11-01 with Kasm Workspaces + managed Kubernetes as
the target stack.

| Step | Status | Notes |
| --- | --- | --- |
| 1. Control plane skeleton | done (fake runtime) | Go API, state machine, caps, rate limits, idempotent teardown; no cluster workload |
| 2. Kubernetes runtime | done (fake-client tested, no cluster) | client-go Pod/Secret/NetworkPolicy builders, readiness wait, idempotent fail-closed teardown, manifest security assertions |
| 3. Session image | not started | needs Docker/Linux; not available on this host |
| 4. Streaming integration | not started | KasmVNC/Guacamole adapter, short-lived URLs, revocation |
| 5. Terraform and CI | not started | `plan` only; user applies |
| 6. Staging deploy | blocked | needs user-provided cluster, domain, IdP, credentials |
| 7. Client integration | not started | extension entry point and E2E |

## Phase 2 step 1: what was built, 2026-11-01

- `session/` is a separate Go module (`geomind/session`); it shares no code with the
  companion. Step 1 used only the standard library; step 2 added client-go for the
  Kubernetes runtime.
- `session/internal/session`: lifecycle state machine (`requested -> provisioning -> ready ->
  active -> terminating -> gone`, plus `failed`), an in-memory `Manager` with per-user and
  global concurrent caps, TTL and idle expiry, runtime reconciliation, idempotent teardown
  with retry, ownership checks, and stream-URL minting through a `Streaming` interface.
  `FakeRuntime`/`FakeStreaming` exercise the whole path without Docker or a cluster.
- `session/internal/auth`: `Verifier` interface and a `DevVerifier` that hashes tokens and
  compares them in constant time. Hosted builds must swap in an OIDC verifier.
- `session/internal/ratelimit`: bounded fixed-window limiter, fail-closed when the key table
  is full.
- `session/internal/api`: `POST /v1/sessions`, `GET /v1/sessions/{id}`,
  `DELETE /v1/sessions/{id}`, `GET /healthz`; bearer auth, per-user create limit, per-IP
  limit, 4 KiB JSON body cap with unknown-field rejection, `Cache-Control: no-store` and
  `nosniff` on every response, and stable error codes. Handlers never return or log page
  content; ownership failures are indistinguishable from unknown IDs.
- `session/cmd/sessiond`: env-only configuration; refuses a non-loopback bind without an
  explicit opt-in, requires a dev token of at least 16 characters, enforces caps and rate
  limits, and runs the sweeper and graceful shutdown. It uses the fake runtime and says so.
- CI: a `session` job runs gofmt, vet, golangci-lint 2.15.0, and `go test -race ./...`.

Not implemented in step 1: Kubernetes, a real Chromium image, a streaming provider, Terraform,
OIDC, and the extension entry point. The API currently returns placeholder stream URLs.

## Phase 2 step 1: verification, 2026-11-01

```
$ gofmt -l . && go vet ./...
(no output)

$ go test -count=1 ./...
ok      geomind/session/cmd/sessiond        1.515s
ok      geomind/session/internal/api        2.421s
ok      geomind/session/internal/auth       1.167s
ok      geomind/session/internal/ratelimit  1.193s
ok      geomind/session/internal/session    1.592s

$ golangci-lint run ./...
0 issues.

$ go test -count=1 -v ./... | grep -c -- '--- PASS'
43
```

Live smoke test against the built binary on loopback port 18080:

```
GET /healthz                                    -> 200 {"status":"ok"}, Cache-Control: no-store
POST /v1/sessions (no token)                    -> 401 unauthenticated, WWW-Authenticate
POST /v1/sessions (Bearer dev token)            -> 201, Location, state ready, streamUrl,
                                                   expiresAt 2026-11-01T13:27:18Z
GET /v1/sessions/{id}                           -> 200, same session
DELETE /v1/sessions/{id}                        -> 204
GET /v1/sessions/{id} after delete              -> 404 not_found
DELETE /v1/sessions/{id} again                  -> 204
```

`go test -race` did not run on this host (the Windows race detector needs cgo). The new CI
`session` job runs it on Linux; CI has still never executed.

`GET` is observation only and does not extend the idle timer; the `Touch` path exists but has
no HTTP route yet. Until the streaming adapter (step 4) calls it, a session is reaped five
minutes after creation even if it is being polled. The TTL and idle values are configurable.

## Phase 2 step 2: what was built, 2026-11-01

- Dependency: `k8s.io/client-go` / `k8s.io/api` / `k8s.io/apimachinery` v0.37.2, pinned in
  `session/go.mod` (approved with the step, ADR-0017). No other Kubernetes libraries.
- `session/internal/kube/kube.go`: `Runtime` implementing `session.Runtime` with client-go.
  Object names are `gms-` + 40 hex characters of SHA-256(session ID), so they are DNS-safe,
  deterministic, and do not expose the ID; the full ID, region, TTL, and idle values are
  annotations. Create writes Secret, NetworkPolicy, Pod (tolerating `AlreadyExists` so a retry
  converges) and then polls Pod readiness within `ReadyTimeout`. Get maps Pod state onto the
  control-plane states. Delete removes the Pod and waits for it to disappear, then removes the
  NetworkPolicy and Secret; if the Pod is still terminating or its delete fails, policy and
  secret stay and an error is returned so `Sweep` retries.
- `session/internal/kube/manifests.go`: builders for the three objects.
  - Secret: opaque, 32 random bytes under `session-token`, no owner reference.
  - Pod: non-root 10001, `RuntimeDefault` seccomp, read-only root FS, drop ALL, no privilege
    escalation, no service-account token, service links off, no host namespaces,
    `RestartPolicy: Never`, zero grace, TTL as `ActiveDeadlineSeconds`, sized `emptyDir` for
    `/tmp` and `/home/geomind`, probe on the named `vnc` port (6901), token from the secret.
  - NetworkPolicy: selects the session Pod, denies by default, ingress only from
    `streaming-edge` Pods in the streaming namespace on `vnc`, egress only to the configured
    proxy component on TCP. No DNS egress rule: routed names resolve at the exit, which keeps
    the Phase 1 DNS invariant inside the cluster.
- `session.Options.CreateTimeout`: provisioning budget separate from the 20s per-call
  timeout; defaults to 2m, or the Kubernetes ready timeout plus 30s. The HTTP write deadline
  now follows it so a slow readiness wait cannot be cut off by the 15s default.
- `session/cmd/sessiond`: `GEOMIND_SESSION_RUNTIME=fake|kube` (default fake). The kube path
  loads a kubeconfig path or in-cluster credentials and refuses to start without an image or
  with an invalid port/timeout combination. The fake path is unchanged and remains the
  default on this host.

Not implemented in step 2: no object has been applied to a real cluster; there is no image
listening on 6901 yet (step 3), no RBAC manifests or Terraform (step 5), no streaming adapter
(step 4), and no OIDC. Readiness is a TCP probe, so step 3 must confirm the KasmVNC listener.
Image references are tags, not digests; pinning is follow-up hardening.

## Phase 2 step 2: verification, 2026-11-01

```
$ gofmt -l . && go vet ./...
(no output)

$ go test -count=1 ./...
ok      geomind/session/cmd/sessiond        0.785s
ok      geomind/session/internal/api        1.185s
ok      geomind/session/internal/auth       0.309s
ok      geomind/session/internal/kube       1.866s
ok      geomind/session/internal/ratelimit  0.318s
ok      geomind/session/internal/session    0.391s

$ golangci-lint run ./...
0 issues.

$ go test -count=1 -v ./... | grep -c -- '--- PASS'
113

$ go test -count=1 -v ./internal/kube | grep -c -- '--- PASS'
61
```

The kube tests use the client-go fake clientset and a goroutine that flips Pod status:
create-and-wait, retry convergence, failed Pod, Pod disappearance, readiness timeout, context
cancellation, invalid spec (no API calls), secret-create failure, state mapping, delete order
asserted as pods -> networkpolicies -> secrets, repeat delete, missing objects, policy retained
while a Pod is terminating, policy retained when the Pod delete fails, and cleanup when the
Pod is already gone. Manifest tests assert the full security context, the absence of host
namespaces/service-account token/hostPath volumes, resource request=limit, the token secret
reference, the probe port, and that the NetworkPolicy has no DNS egress or empty peer.

Smoke tests against the built binary:

```
$ GEOMIND_SESSION_DEV_TOKEN=... ./bin/sessiond.exe
level=INFO msg="session control plane listening" addr=127.0.0.1:18081 regions=[de-test]
runtime=fake maxPerUser=2 maxTotal=20
POST /v1/sessions (no token) -> 401
POST /v1/sessions (Bearer)    -> 201 {"state":"ready",...}

$ GEOMIND_SESSION_RUNTIME=kube ./bin/sessiond.exe
sessiond: GEOMIND_SESSION_KUBE_IMAGE is required when GEOMIND_SESSION_RUNTIME=kube
```

`go test -race` still has not run locally (no cgo on this host); the CI `session` job runs it
on Linux, and CI has never executed. No cluster, kubectl, or Docker exists here, so
client-go against a real API server and the NetworkPolicy behavior are unverified.

## Step 1: what was built

- `companion/internal/policy`: strict document decoding (unknown fields rejected), rule and
  host validation, IDNA to punycode, precedence, unknown-exit to block, PAC generation and
  structural validation.
- `extension/src/lib/policy.ts`: mirror used for previews and PAC tests, same validation and
  precedence.
- `shared/fixtures/policy`: five valid scenarios and fourteen invalid policies, consumed by
  both languages; `golden/basic.pac` must match both generators byte for byte.
- PAC: JSON rule table plus a fixed interpreter, no DIRECT return except the default, block
  maps to `SOCKS5 127.0.0.1:9`, host is lowercased and one trailing dot is stripped.

## Verification log: Step 1, 2026-10-30

Commands actually run locally, with real output.

```
$ go version
go version go1.27.3 windows/amd64

$ gofmt -l . && go vet ./...
(no output)

$ go test -v ./...
--- PASS: TestValidFixtures (basic, block, idn, precedence, unknown-exit)
--- PASS: TestInvalidFixtures (14 subtests, all rejected)
--- PASS: TestMatchRequiresLoadedPolicy
--- PASS: TestPACGolden
--- PASS: TestPACHasNoDirectFallbackForMatchedHosts
--- PASS: TestValidatePACRejectsTampering (4 subtests)
--- PASS: TestPACProxyAddressValidation
--- PASS: TestNormalizeHost
--- PASS: TestCompileMarksUnknownExitAsBlock
--- PASS: TestLoadKeepsPreviousPolicyOnError
--- PASS: TestLoadRejectsTrailingData
ok      geomind/companion/internal/policy

$ golangci-lint run ./...
0 issues.

$ npm run typecheck
(no output, exit 0)

$ npm run lint
(no output, exit 0)

$ npm test
Test Files  2 passed (2)
     Tests  30 passed (30)
```

## Step 2: what was built

- `companion/internal/ipc`: 4-byte length-prefixed native messaging codec (1 MiB cap),
  strict envelope and payload validation, typed dispatch, chunk-safe error responses.
- `companion/internal/exitinfo`: region, IANA timezone, locale, and DoH URL from env, with
  the DoH endpoint required to be an IP literal.
- `companion/cmd/geomind-companion`: flags, policy load, fake `down` tunnel status, stdin/
  stdout protocol only.
- `extension/manifest.json`: MV3, fixed 2048-bit key (ID `kbhjhoinfglmngepoiheidpjicmpenoi`),
  permissions exactly `nativeMessaging` and `storage`, no host permissions or content scripts.
- `extension/src/background`: native messaging client with timeouts and disconnect handling;
  service worker bootstrap that fetches policy and tunnel status.
- `scripts/install-native-host.{sh,ps1}` plus `shared/schema/ipc.schema.json`.

## Step 2: verification, 2026-10-30

```
$ go test ./...
ok      geomind/companion/cmd/geomind-companion
ok      geomind/companion/internal/exitinfo
ok      geomind/companion/internal/ipc
ok      geomind/companion/internal/policy

$ golangci-lint run ./...
0 issues.

$ npm run typecheck && npm run lint
(no output, exit 0)

$ npm test
Test Files  4 passed (4)
     Tests  51 passed (51)
```

Handshake against the built binary:

```
{"id":"h1","type":"hello.result","payload":{"protocolVersion":1,"policyVersion":0,
"tunnel":{"state":"down","exit":null,"region":null,"since":"2026-10-30T15:07:34Z"}}}
```

## Step 3: what was built

- `companion/internal/tunnel/supervisor.go`: thread-safe handshake-freshness supervisor;
  anything other than a fresh `up` state is unhealthy, so the proxy fails closed.
- `companion/internal/proxy`: loopback-only (`tcp4 127.0.0.1`) SOCKS5 CONNECT, no auth,
  hostname passed through unresolved, health checked at accept and after the handshake,
  in-flight connections closed on `Close`, no retry path.
- `companion/internal/proxy/nodial_test.go`: AST guard that rejects `net.Dial*`,
  `net.Lookup*`, `net.Resolve*`, `net.Dialer`, and `net.Resolver` in the package.
- `tests/leak/companion/run.sh`: runs the fake-tunnel fail-closed checks without privileges.

## Step 3: verification, 2026-10-30

```
$ gofmt -l ./internal/proxy ./internal/tunnel
(no output)

$ go vet ./...
(no output)

$ go test ./...
ok      geomind/companion/cmd/geomind-companion
ok      geomind/companion/internal/exitinfo
ok      geomind/companion/internal/ipc
ok      geomind/companion/internal/policy
ok      geomind/companion/internal/proxy
ok      geomind/companion/internal/tunnel

$ golangci-lint run ./...
0 issues.

$ sh tests/leak/companion/run.sh
PASS: TestPackageHasNoDirectDialPath, TestRefusesWhenUnhealthyAtAccept,
TestRefusesAfterHandshakeWhenTunnelDrops, TestForwardsDomainUnresolvedAndRelays,
TestTargetAddresses, TestRejectsUnsupportedAuth, TestRejectsNonConnectCommand,
TestRejectsZeroPort, TestRejectsAmbiguousDomain, TestKillMidTransferStopsTraffic,
TestCloseFailsInFlightConnections, TestNewServerRejectsNonLoopbackListener,
TestNewServerRejectsNilTunnelAndClosesListener, TestListenBindsLoopbackEphemeralPort
PASS: all five supervisor tests
```

The kill test asserts three things with the fake tunnel: the client sees the cut transfer
fail, a new connection is refused while unhealthy, and no additional `DialContext` call is
recorded. Packet capture of "no direct attempt" needs a real tunnel and runs in Step 4/5;
the AST guard is the static proof that this package has no direct path at all.

## Step 4: what was built

- `companion/internal/tunnel/config.go`: `Config` plus strict `FromEnv` parsing of
  `GEOMIND_WG_*` (keys as 64-hex, endpoint as `IP:port` only, local addresses, allowed
  prefixes, DoH URL, MTU 1280-9000, keepalive in whole seconds). Errors never echo key
  material.
- `companion/internal/tunnel/device.go`: wireguard-go device over gVisor `netstack`, no TUN
  device and no host-network path. Configured through UAPI `IpcSet`; handshake freshness is
  read from `IpcGet` and fed to the Step 3 supervisor, so `Healthy()` means "fresh handshake",
  not "device started". `DialContext` accepts TCP only, refuses when unhealthy, dials IP
  literals directly and otherwise resolves via DoH.
- `companion/internal/tunnel/doh.go`: DNS-over-HTTPS through the netstack, A and AAAA,
  endpoint must be an IP literal, TLS >= 1.2 verified against the configured roots, redirects
  rejected, response ID/question/rcode validated, 64 KiB cap.
- Device logging is discarded: the wireguard-go default logs to stdout, which would corrupt
  the native messaging stream. `conn.NewStdNetBind()` is used because the Windows ring bind
  stops receiving after a UDP ICMP port-unreachable.
- `companion/testdata/wg.env.example`: placeholder env, no real keys. `.env.example` gained
  the same section.
- `tests/leak/wg/run.sh`: Linux/root netns harness that runs the tunnel tests under
  `unshare -n` and asserts with tcpdump that only loopback UDP exists (no DNS port 53, no TCP
  SYN, no ICMP, no non-loopback address).

## Step 4: verification, 2026-10-30

```
$ gofmt -l cmd internal
(no output)

$ go vet ./...
(no output)

$ go test -count=1 ./...
ok      geomind/companion/cmd/geomind-companion
ok      geomind/companion/internal/exitinfo
ok      geomind/companion/internal/ipc
ok      geomind/companion/internal/policy
ok      geomind/companion/internal/proxy
ok      geomind/companion/internal/tunnel   8.103s

$ golangci-lint run ./...
0 issues.

$ go test -count=1 -run 'TestTwoPeersCarryTrafficAndDoH|TestKillPeerStopsTrafficAndGoesUnhealthy|TestHandshakeFailureStaysUnhealthy' -v ./internal/tunnel
=== RUN   TestTwoPeersCarryTrafficAndDoH
--- PASS: TestTwoPeersCarryTrafficAndDoH (1.03s)
=== RUN   TestKillPeerStopsTrafficAndGoesUnhealthy
--- PASS: TestKillPeerStopsTrafficAndGoesUnhealthy (5.03s)
=== RUN   TestHandshakeFailureStaysUnhealthy
--- PASS: TestHandshakeFailureStaysUnhealthy (1.50s)
ok      geomind/companion/internal/tunnel   7.793s

$ npm run typecheck && npm run lint && npm test
Test Files  4 passed (4)
     Tests  51 passed (51)
```

The two-peer tests run real WireGuard handshakes over loopback: traffic reaches the peer's
netstack, a hostname is resolved only by the exit-side DoH test server (hit count asserted),
killing the peer stops the transfer and flips the supervisor to unhealthy, and an endpoint
with no listener leaves `Healthy() == false` with dials returning `ErrUnhealthy`.

`tests/leak/wg/run.sh` was not run on this Windows machine; it is root/Linux only. Packet
capture of the full tunnel path therefore remains unproven until CI or a Linux VM runs it.

## Step 5: what was built

- `cmd/geomind-companion`: starts the WireGuard device when any `GEOMIND_WG_*` value is
  present, otherwise a closed tunnel that refuses every dial. Either way it starts a
  loopback SOCKS5 proxy and generates the PAC with that proxy's real port. Tunnel status is
  read live from the device supervisor. A partial WireGuard configuration is a startup
  error, never a silent fallback.
- Native-host deployment values (`GEOMIND_POLICY`, `GEOMIND_KNOWN_EXITS`, `GEOMIND_EXIT_ID`)
  are flag defaults, because Chrome starts the host without arguments.
- `extension/src/background/pac.ts`: validates the companion PAC (header, strict rule JSON,
  one loopback route target, dead block proxy, exact interpreter) and installs it as a
  mandatory `pac_script`. It never clears a previously installed PAC.
- `extension/src/background/privacy.ts`: applies `disable_non_proxied_udp` and re-applies it
  if another extension or the user changes it.
- `extension/src/background/service-worker.ts`: fetches the policy, installs routing, writes
  routing/privacy state to session storage, and shows a badge when routing is unavailable.
- `manifest.json`: adds `proxy`, `privacy`, and a badge-only `action`.
- `tests/leak/browser/routing.spec.ts`: Playwright loads the built extension in a persistent
  Chromium with a real native host, then proves WebRTC policy, PAC installation from the
  companion, fail-closed routing to a local origin, and a successful direct control request.
- `tests/leak/run.sh`: runs the companion, WireGuard, and browser stages.

## Step 5: verification, 2026-10-30

```
$ gofmt -l cmd internal
(no output)

$ go vet ./...
(no output)

$ go test -count=1 ./...
ok      geomind/companion/cmd/geomind-companion   0.681s
ok      geomind/companion/internal/exitinfo
ok      geomind/companion/internal/ipc
ok      geomind/companion/internal/policy
ok      geomind/companion/internal/proxy
ok      geomind/companion/internal/tunnel   8.121s

$ golangci-lint run ./...
0 issues.

$ npm run typecheck && npm run lint
(no output, exit 0)

$ npm test
Test Files  5 passed (5)
     Tests  57 passed (57)

$ npx playwright test
Running 3 tests using 1 worker
  ok 1 disables non-proxied WebRTC UDP (458ms)
  ok 2 installs the companion PAC and reports routing active (241ms)
  ok 3 fails closed for a routed host and still loads a direct host (500ms)
  3 passed (11.3s)

$ sh tests/leak/companion/run.sh
PASS (proxy, AST guard, and supervisor tests)
```

The browser test is end to end: Playwright registers a native messaging host pointing at
the built companion, the companion serves a policy with a real ephemeral proxy port, the
extension validates and installs the PAC, a request to the routed host fails with zero hits
on the local origin, and an unmatched host loads through DIRECT.

`tests/leak/wg/run.sh` and the full `tests/leak/run.sh` were not run: the WireGuard stage
needs root and Linux. The browser stage is headful and used the Windows desktop.

## Triage of analysis findings, 2026-10-30

- Go host validation rejects a final label that is a decimal or `0x`-hex number, matching
  WHATWG URL IPv4 parsing. `010.0.0.1`, `2130706433`, `0x7f000001`, `0x7f.0.0.1`,
  `999.999.999.999`, and `example.123` are now rejected by both Go and TypeScript.
- Rule priority is bounded to ±1,000,000 in Go, matching the schema and TypeScript.
- `decodeDoc` rejects a missing or null `rules` field.
- `Store` now uses `sync.RWMutex`; `Load` compiles outside the lock and swaps under the
  write lock, while `Match` and `PAC` read under a read lock.
- Shared invalid fixtures grew from 14 to 26 cases, covering IP forms, priority bounds,
  and the `rules` field. Both languages consume the same file.
- New `TestStoreConcurrentMatchAndReload` exercises match, PAC generation, and reload
  concurrently.
- CI runs `npm run typecheck` and golangci-lint v2.15.0 (installed with
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`).

```
$ go vet ./...
(no output)

$ go test -count=1 ./...
ok      geomind/companion/cmd/geomind-companion   0.527s
ok      geomind/companion/internal/exitinfo       1.288s
ok      geomind/companion/internal/ipc            2.098s
ok      geomind/companion/internal/policy         1.736s
ok      geomind/companion/internal/proxy          1.729s
ok      geomind/companion/internal/tunnel         7.927s

$ golangci-lint run ./...
0 issues.

$ npm run typecheck && npm run lint
(no output, exit 0)

$ npm test
Test Files  5 passed (5)
     Tests  67 passed (67)

$ npx playwright test
Running 3 tests using 1 worker
  ok 1 disables non-proxied WebRTC UDP (200ms)
  ok 2 installs the companion PAC and reports routing active (931ms)
  ok 3 fails closed for a routed host and still loads a direct host (402ms)
  3 passed (10.6s)

$ sh tests/leak/companion/run.sh
PASS (proxy, AST guard, and supervisor tests)
```

`go test -race` was not run locally: this Windows host has Go but no C toolchain (cgo is
required for the Windows race detector). Linux CI runs `go test -race ./...`.

## Live tunnel status polling, 2026-10-30

- `extension/src/background/status.ts`: pure tunnel-status parser (strict state, exit,
  region, and `since` checks) and badge decision.
- The service worker polls `tunnel.status` every 5 seconds while connected, suppresses
  overlapping requests, refreshes `companionStatus`, and stops the timer on disconnect,
  suspend, protocol mismatch, or a connect error.
- Badge precedence: routing error or unavailable wins; otherwise a down tunnel is a red
  `!`, a starting tunnel an amber `…`, and only routing active plus tunnel up clears it.
- A failed poll keeps the last known state and never touches `chrome.proxy.settings`; the
  installed PAC remains the routing switch, so polling cannot open a DIRECT fallback.
- Tests: seven unit tests for parsing and badge mapping; the Playwright harness now asserts
  the `tunnel down` badge for the no-WireGuard companion and that stored status advances on
  the poll interval.

```
$ npm run typecheck && npm run lint
(no output, exit 0)

$ npm test
Test Files  6 passed (6)
     Tests  74 passed (74)

$ npx playwright test
Running 4 tests using 1 worker
  ok 1 disables non-proxied WebRTC UDP (278ms)
  ok 2 installs the companion PAC and reports routing active (261ms)
  ok 3 polls tunnel status while connected (5.2s)
  ok 4 fails closed for a routed host and still loads a direct host (356ms)
  4 passed (15.5s)
```

## Step 5 completion: hot reload, locale headers, options page, 2026-10-30

- Policy hot reload (ADR-0013): the companion watches the policy file's mtime and size once a
  second, reloads through the existing locked `Store`, and emits an unsolicited
  `policy.changed` event under the reserved id `"event"`. All native-message writes now go
  through a mutex-guarded `Sender` with a failure latch, so a response and an event cannot
  interleave. An invalid reload keeps the previous policy and retries on the next tick. The
  extension serializes refreshes (in-flight plus dirty flags), never replaces a valid PAC
  with a failed fetch, and re-syncs header rules after a successful refresh.
- Locale over IPC: `tunnel.status` carries `locale` from the exit metadata, validated by the
  same pattern on both sides. `shared/fixtures/locale/cases.json` is consumed by Go and
  TypeScript so the accepted and rejected sets cannot drift.
- Header rules (ADR-0014): `accept-language` is set with
  `declarativeNetRequestWithHostAccess` for routed origins only, and only after the user
  grants the optional per-origin permission. Exact hosts get an anchored regex, wildcards
  require at least one extra label. Nothing is requested at install time.
- Options page: static `options.html` shows routing state, exit locale, routed hosts, and
  whether headers are on; Enable requests only the routed origins synchronously in the click
  handler, Disable removes origins, storage, and dynamic rules.
- Tests: watcher reload, invalid-reload recovery, no spurious events, concurrent sender
  serialization, reserved-id rejection; 7 header-plan tests and 5 DNR-install tests; 20 locale
  parity tests; two new Playwright cases (edit-and-reload without reconnect, and the options
  page showing locale/hosts with rules still off before any grant).

```
$ gofmt -l . && go vet ./...
(no output)

$ go test -count=1 ./...
ok      geomind/companion/cmd/geomind-companion
ok      geomind/companion/internal/exitinfo
ok      geomind/companion/internal/ipc
ok      geomind/companion/internal/policy
ok      geomind/companion/internal/proxy
ok      geomind/companion/internal/tunnel

$ go test -count=5 ./cmd/geomind-companion/
ok      geomind/companion/cmd/geomind-companion

$ golangci-lint run ./...
0 issues.

$ npm run typecheck && npm run lint
(no output, exit 0)

$ npm test
Test Files  9 passed (9)
     Tests  108 passed (108)

$ npm run build
(no output; dist/options/options.js emitted)

$ npx playwright test
6 passed (20.7s)

$ sh tests/leak/companion/run.sh
PASS (proxy, AST guard, and supervisor tests)
```

The hot-reload browser test is end to end: editing the policy file changes the reported
`ruleCount` and the installed PAC while the connection stays up, and an invalid edit leaves
routing active with the old PAC. The header test asserts that no dynamic rule exists before
the user grants a permission, so locale headers are off by default.

## Independent security review, 2026-10-31

Read-only review against `docs/threat-model.md` and `.qoder/rules/06-security-review.md`,
performed separately from the implementation. All locally runnable suites were green
(`go test` six packages, `golangci-lint` 0 issues, typecheck/lint clean, 108 extension tests,
build ok, companion leak script PASS, Playwright 6 passed in 20.2 s). The verdict was
**FAIL**: R1 (critical) and R2 (high) were real-IP leak paths. R1 was subsequently closed as
an explicit Phase 1 scope decision (ADR-0015, 2026-10-31); R2 remains open, so the phase gate
is still FAIL.

| # | Severity | Finding | Status |
| --- | --- | --- | --- |
| R1 | Critical | A stale PAC keeps pointing at the companion's ephemeral loopback port after the companion dies. A same-user process can bind that port and run a permissive SOCKS5 server; routed hosts then leave from the real IP. The extension has no reconnect timer, so the window lasts until the service worker wakes or the browser restarts. | scoped out for Phase 1 (ADR-0015); hardening pending |
| R2 | High | Proxy drift is never detected. `installPac` only writes settings; nothing reads `chrome.proxy.settings.get` or `levelOfControl`, unlike the WebRTC watcher. Another extension or an enterprise policy can take control while the badge still says routing active. | fixed 2026-11-01: install read-back, 5 s guard, onChange re-assert, badge on unrecoverable drift; Playwright regression test |
| R3 | Medium | `privacyStatus: error` is written to session storage but never surfaced in the badge or options page, so a failed WebRTC policy is invisible. | fixed 2026-11-01: badge precedence + options line, unit and Playwright coverage |
| R4 | Medium | The locale/timezone/geolocation invariant is unmet. `Accept-Language` is opt-in per origin, `navigator.language`/`Intl`/timezone/geolocation are unchanged, and a site can compare them. Timezone is an accepted gap (ADR-0006); geolocation is not documented as one. | decided and partly fixed 2026-11-01 (ADR-0019): geolocation is blocked on routed origins; timezone/language parity is documented as out of Phase 1 scope and belongs to Mode B |
| R5 | Medium | Header sync records `applied*` from the globals *after* the `installHeaderRules` await. A policy or toggle change during the await can install the old plan while recording the new state, suppressing the retry. | fixed 2026-11-01 |
| R6 | Low | `policy.get` reads the document and PAC under separate read locks, so a reload between them can return an old document with a new PAC (mismatch only, not fail-open). | fixed 2026-11-01: `SnapshotWithPAC` + concurrency test |
| R7 | Low | The startup `syncPolicy` call does not set `policyInFlight`, so a `policy.changed` event during startup can race it; the older install can land last. | fixed 2026-11-01 |
| R8 | Low | Options Disable is unavailable when `headersEnabled` is false even if the origin permission is still held, and removing a rule from the policy leaves its old origin permission granted. | fixed 2026-11-01: grant tracking, disable while held, stale-grant pruning |
| R9 | Low | The loopback SOCKS5 proxy has no authentication, so any local process can use the exit for arbitrary destinations (same local-adversary boundary as R1). | scoped out with E8 (ADR-0015) |

Reproduction for R1: load a routed policy and note the PAC port; kill the companion; bind
`127.0.0.1:<port>` with a SOCKS5 server that dials CONNECT targets directly; open a routed
host before the service worker reconnects. Chrome sends `CONNECT host:443` to that process.
The reproduction remains valid, but on 2026-10-31 the user decided this adversary is outside
Phase 1 (ADR-0015); the OS user boundary is trusted and the limitation is recorded in
threat-model section 8.8. It is a known limitation, not a Phase 1 gate.

Review answers, condensed: (1) matched hosts never get DIRECT in the PAC and the proxy gate
is sound, but R2/R3 leak within the Phase 1 boundary and R1 leaks outside it by ADR-0015;
DNS/QUIC/IPv6 are unproven; (2) no AI ships
in Phase 1 and there is no page-read permission, content script, or externally connectable
surface, so page text has no path to act; (3) one profile, one PAC, one global WebRTC policy
and global DNR rules, so cross-region identity re-linking is possible (T17); no cross-tab
observation path was found; (4) no telemetry or browsing logs; status and policy are
memory-backed session storage, `headersEnabled` is a local boolean, the PAC and DNR rules
persist routed patterns and locale as configuration, companion stderr has no key material,
and wireguard-go logging is discarded; (5) no custom crypto; wireguard-go + gVisor are
vetted but unaudited (ADR-0009), TLS is stdlib, the SOCKS5/DoH/PAC code is custom and
loopback-only/structural.

Unverified: the Linux netns DNS/WebRTC/QUIC/IPv6 capture and a real-tunnel kill-mid-download
never ran; `go test -race` needs cgo and did not run locally; CI has never executed; Chrome's
SOCKS5 DNS behavior, QUIC under PAC, and two-extension proxy precedence are unconfirmed; and
real header application after a permission grant has no test (the Playwright case only proves
that no rule exists before the grant).

## R1 scope decision, 2026-10-31

- **Decision (ADR-0015):** Phase 1's fail-closed guarantee covers tunnel failure, proxy
  refusal, companion exit, policy-sync failure, browser restart, and service-worker restart.
  It does not cover a hostile process running as the same OS user. E8 is out of scope for
  Phase 1 routing integrity and confidentiality; the OS user boundary is trusted.
- **Why:** a PAC cannot authenticate its SOCKS5 peer, Chrome starts the companion as its
  child, and a persistent companion daemon would narrow the window without closing it (the
  same user can kill it and rebind the port). Same-user code can already read the profile,
  keys, and AI context. The honest boundary is the OS user.
- **What remains:** R2 (proxy drift) is still a Phase 1 blocker. Reconnect with backoff,
  proxy-drift detection, privacy-error surfacing, and a truthful badge are follow-up
  hardening; they reduce accidental exposure but do not close R1.
- **Claim boundary:** do not describe Phase 1 as "no real-IP fallback under all conditions."
  Say "no fallback on tunnel, proxy, or process failure under the Phase 1 threat model,
  which trusts the OS user boundary."
- **Updated:** threat model sections 3, 4, 5 (T1), 7, 8 (item 8), 9, and 10; plan non-goals,
  risks, invariant map, and open questions; `project-state/active-context.md`.

## Review remediation R2, R3, R5-R8, 2026-11-01

- **R2 (proxy drift).** `installPac` now reads back `chrome.proxy.settings.get` and requires
  `levelOfControl == controlled_by_this_extension`, `mode == pac_script`, byte-identical PAC
  data, and `mandatory == true`; otherwise it fails with `proxy_override`. The service worker
  keeps the last valid PAC in session storage, reparses it after a worker restart, re-checks
  every 5 s, and re-asserts it on `chrome.proxy.settings.onChange`. All PAC writes queue
  through one chain with a generation token, so a repair cannot resurrect a PAC that a policy
  refresh already replaced. If repair fails, routing is reported as
  `proxy settings changed` and the badge turns red; a repaired drift restores the previous
  truthful status (including a disconnect that happened meanwhile).
- **R3 (silent WebRTC failure).** `badgeFor` takes the privacy state and a failure outranks
  every routing state. The options page shows WebRTC protection state.
- **R5/R7 (races).** Header `applied*` markers are recorded only when the document, locale,
  and toggle still match what was installed. The startup policy sync now sets
  `policyInFlight` and honors a `policy.changed` event that arrived during it.
- **R6 (atomic policy.get).** `Store.SnapshotWithPAC` returns the document and PAC under one
  read lock; `policy.get` uses it.
- **R8 (options cleanup).** Granted origins are tracked, Disable is available while a grant is
  held even if the feature toggle is off, and grants for sites no longer routed are removed
  (skipped when the policy is not loaded, so a transient empty state cannot revoke them).

Verification, commands actually run:

```
$ gofmt -l . && go vet ./...
(no output)

$ go test -count=1 ./...
ok      geomind/companion/cmd/geomind-companion   0.836s
ok      geomind/companion/internal/exitinfo       3.358s
ok      geomind/companion/internal/ipc            4.083s
ok      geomind/companion/internal/policy         3.879s
ok      geomind/companion/internal/proxy          4.029s
ok      geomind/companion/internal/tunnel         8.637s

$ go test -count=1 -run TestSnapshotWithPACIsAtomic -v ./internal/policy
--- PASS: TestSnapshotWithPACIsAtomic (0.30s)

$ golangci-lint run ./...
0 issues.

$ npm run typecheck && npm run lint
(no output, exit 0)

$ npm test
Test Files  9 passed (9)
     Tests  113 passed (113)

$ npm run build
(no output; dist emitted)

$ npx playwright test
7 passed (20.6s)

$ sh tests/leak/companion/run.sh
PASS
```

New tests: `pacSatisfied` and install read-back cases, the badge privacy precedence, a
Playwright case that sets the proxy to `direct` and asserts the PAC is restored with routing
still active, `TestSnapshotWithPACIsAtomic`, and the options page assertion that WebRTC
protection is reported active.

Still open after remediation: R4's timezone/language half (documented as out of Phase 1
scope, not fixable without the debugger API), the threat-model wording update for R4, AC2
(Linux netns capture), AC3/Step 6, reconnect with backoff, `go test -race` (CI only), and
reading the first CI run. The unrecoverable-drift path is unit-tested at install time; the guard's own
unrecoverable branch has no end-to-end test because a persistent foreign controller cannot be
simulated in the Playwright profile.

## R4 remediation, 2026-11-01

- `extension/src/background/geolocation.ts` plans one `block` rule per routed policy pattern
  (exact hosts and wildcards) through `chrome.contentSettings.location` and applies all of
  them, reporting the first failure after the rest were attempted. Blocks are monotonic: a
  policy change only adds them, so a reload never opens a window.
- The service worker applies the plan whenever the policy document changes, stores
  `geolocationStatus` in session storage, and the options page shows how many routed sites are
  blocked next to the WebRTC status.
- The manifest adds `contentSettings`, used only for `location`. `docs/decisions.md` ADR-0019
  records why timezone and `navigator.language`/`Intl` are not spoofed (no MV3 API; the
  debugger route is visible and itself a fingerprint) and that parity is a Mode B property.
- Verification: `npm test` 119 passed (5 new geolocation cases), `npm run typecheck` and
  `npm run lint` clean, `npx playwright test` 8 passed including a new case that reads the
  location setting back in Chromium (`block` for the routed host, `ask` for a direct host)
  and checks the options-page line.
- `docs/threat-model.md` T16 and section 8.1 now record the decision (geolocation blocked,
  timezone/language accepted for Phase 1, parity is a Mode B property).

## What works

Policy parsing/matching and PAC generation (Go + TS with shared fixtures), native messaging
handshake and typed dispatch, extension bootstrap against the companion, tunnel status
supervisor, a loopback SOCKS5 proxy that refuses and tears down traffic whenever the tunnel
is unhealthy, a userspace WireGuard tunnel with DoH resolution at the exit, and extension
routing that installs only a validated PAC, applies the WebRTC policy, fails closed in a
real browser when the proxy is unavailable, and polls tunnel status so the badge reflects a
later tunnel drop. Policies
reload without a restart, the exit locale reaches the extension over IPC, and an explicit
per-site grant installs the matching `Accept-Language` header rule for routed origins (the
install is unit-tested; real header application after a grant is still unverified). Proxy
settings are read back and watched, so a competing controller is re-asserted or surfaced as a
routing error, and a failed WebRTC policy is visible in the badge and options page. The
Phase 2 control-plane skeleton also runs locally (`session/cmd/sessiond`): it creates,
reports, expires, and tears down fake remote sessions behind bearer auth, per-user/per-IP
rate limits, and concurrency caps. The opt-in Kubernetes runtime builds a Secret,
NetworkPolicy, and Pod per session and is verified against the client-go fake clientset; no
object has been applied to a cluster.

## What is stubbed or not started

- No defense against a hostile process running as the same OS user (E8); Phase 1 trusts the
  OS user boundary (ADR-0015). Reconnect with backoff remains follow-up hardening; proxy
  drift detection and repair now ship.
- No native Linux netns DNS/WebRTC/QUIC/IPv6 capture yet; only the code-level fail-closed
  and browser routing proofs exist.
- Header rules cover top-level and script-initiated requests from the granted origin. A
  third-party subresource initiated by an ungranted site is not rewritten, which is
  documented as a Phase 1 limitation (ADR-0014).
- No AI (Step 6 blocked on llama.cpp and a GGUF model).
- No real Mode B runtime is deployed. `sessiond` defaults to an in-memory fake; the Kubernetes
  runtime exists but has never talked to an API server. There is no Chromium image, no
  streaming provider, no OIDC, no Terraform, no RBAC manifests, and no extension entry point.
  The stream URLs it returns are placeholders and cannot be opened.

## Risks and gaps

- Runtime PAC validation is structural (rule table, one DIRECT default). Behaviour is checked
  by evaluating the PAC in Node during tests; Go has no JS engine, so a generator bug that
  preserves the structure but changes semantics would be caught only by the TS tests.
- PAC matching relies on Chrome passing a canonical ASCII host. The script lowercases and
  strips a trailing dot, but cannot punycode.
- The TS matcher duplicates the Go logic; shared fixtures and the golden PAC are the parity
  guard.
- IP literals, CIDR, ports, and paths are rejected in Phase 1; `defaultAction` must be
  direct. A host is also rejected when its final label is numeric (for example
  `example.123`), because a browser resolves that as an IPv4 address, not a name.
- `go test -race` was not run locally (the Windows race detector needs a C toolchain). CI is
  configured to run it.
- CI has still not executed on GitHub.
- Tunnel status can be up to one poll interval (5 seconds) stale, and a hung (not
  disconnected) companion keeps its last known state until a request fails. The PAC still
  fails closed either way.
- wireguard-go has no confirmed standalone third-party audit. The user approved the
  dependency on 2026-10-30 (ADR-0009); the Phase 1 security review must still judge it.
- gVisor and wintun are transitive dependencies. wintun is compiled but unused because the
  device runs in userspace with netstack.
- The netns packet-capture harness has not run. Until it does, "no packet leaves outside the
  tunnel" rests on code structure (netstack-only dialing, the proxy AST guard) and on the
  browser fail-closed test, not on an observed capture. In particular, whether Chrome
  resolves a SOCKS5 hostname locally is unproven; the browser test intentionally maps the
  test host through `--host-resolver-rules`, so it does not answer that question.
- The extension validates the PAC with a TypeScript mirror. A semantic drift between the Go
  validator and the TS validator would let a malformed PAC through only if both accept it;
  the shared golden PAC is the parity guard.
- A stale PAC keeps pointing at the old loopback port after the companion dies. The review
  shows that a same-user process which binds it and speaks SOCKS5 receives routed traffic.
  This is accepted as an out-of-scope adversary for Phase 1 (ADR-0015), not as a fixed
  issue; reconnect with backoff remains planned hardening. Proxy-drift detection and repair
  landed 2026-11-01: recoverable drift is browser-tested, while the guard's unrecoverable
  branch is only covered by the install-time read-back unit tests.
- Policy hot reload has up to one second of latency, and a reload failure is silent apart
  from the absence of an event; the extension keeps the previous policy, which is the
  fail-safe direction but means a persistently broken file is only visible in the companion
  stderr.
- Header rules are installed as dynamic DNR rules, but Chrome only applies them while the
  origin's optional permission is held. A user revoking the permission outside the options
  page leaves the rules present and inert; the options page re-syncs on the next open.
- Playwright and Chromium are dev-only and pinned in `package.json`; the browser suite needs
  a display (headful) and `npm install` plus `npx playwright install chromium`.
- `go test -race` was not run locally (the Windows race detector needs a C toolchain). CI ran
  it on GitHub on 2026-11-01 and the companion and session jobs passed with it.
- Mode B terminates TLS inside the session container, so the exit operator can see page
  content. That limitation must be stated in user-facing docs before any hosted release; it
  is the main reason Mode A remains the content-confidentiality path.
- The Kubernetes runtime is fake-client tested only. client-go pagination, admission
  webhooks, NetworkPolicy enforcement, Pod eviction, and API server error shapes are
  unverified until a real cluster runs. The delete path depends on a Pod disappearing after
  a zero-grace delete; a stuck finalizer makes teardown retry and keeps the policy in place.
- The session NetworkPolicy allows no DNS egress by design. The step 3 image must send every
  outbound name through the SOCKS5 proxy with remote resolution; if it falls back to local
  DNS, browsing fails (fail-closed, but unusable) instead of leaking.
- The control plane has no RBAC manifests yet. `sessiond` needs get/create/delete on pods,
  secrets, and networkpolicies in the session namespace, and get on pods for status; applying
  a broader role would exceed the runtime's needs.
- Session images are referenced by tag. Tag mutation is a supply-chain risk; pinning by
  digest is follow-up hardening. The readiness probe is a TCP connect to port 6901 and has
  not been checked against the real KasmVNC listener.

## App icon, 2026-11-01

- Hero mark: a shield (privacy) fused with a globe (geo-routing) and a three-node
  constellation (local AI), mint-to-violet on deep navy. Master at `design/icon-master.png`.
- `extension/icons/icon-{16,32,48,128}.png`, wired into `manifest.json` under `icons` and
  `action.default_icon`. The 16px size uses a simplified source (`design/icon-small.svg`):
  the globe grid and constellation collapse into noise below 24px, so 16 keeps the shield and
  a single node.
- `extension/test/unit/manifest.test.ts` now asserts every declared icon exists and is a PNG
  of the declared size. Extension tests 114 passed, lint clean, Playwright 7 passed with the
  icons loaded.

## CI leak-suite remediation, 2026-11-01

- Run `41333277008` (commit `d2809b9`): hygiene 7s, extension 36s, companion 2m17s, and
  session 4m37s passed; the leak suite failed after 51s on `Run network-namespace leak
  suite`. Log bodies require a GitHub sign-in the CLI does not have, so the cause is
  UNCONFIRMED. The single annotation is "Process completed with exit code 1."
- Most likely cause: the job ran `sudo -E bash tests/leak/run.sh` and sudo's `secure_path`
  drops the `setup-go` toolchain directory, so the first stage failed on a missing `go`.
  A cold root-owned module cache and a genuine capture failure are the other candidates;
  the 51s duration and the companion job passing the same tests argue against the last.
- `.github/workflows/ci.yml` now runs the leak suite as three named steps: `Companion
  fail-closed checks` (unsudoed), `Network-namespace WireGuard capture`
  (`sudo env PATH="$PATH" LEAK_REQUIRE_NETNS=1 bash tests/leak/wg/run.sh`), and `Browser PAC
  and fail-closed checks`. A failure names its stage.
- `tests/leak/wg/run.sh` gains a `blocked()` helper: with `LEAK_REQUIRE_NETNS=1` every skip
  (non-Linux, non-root, missing `unshare`/`ip`/`tcpdump`/`go`) exits 1 and prints the PATH it
  searched. Local and Windows runs keep skipping with exit 0. Without this, AC2 could only
  ever have been reported green by a run that never captured anything.
- The three Go jobs set `cache-dependency-path` (`companion/go.sum`, `session/go.sum`), which
  removes the `Restore cache failed: Dependencies file is not found` warnings.
- Verified locally: `sh -n` on all three leak scripts; `LEAK_REQUIRE_NETNS=1 sh
  tests/leak/wg/run.sh` exits 1 where the plain run exits 0; companion vet + golangci-lint
  (0 issues) + `go test ./...`; session `gofmt`/vet/`go test ./...`; extension typecheck +
  119 unit tests; `tests/leak/companion/run.sh` green. The netns capture itself did not run
  here (Windows), so it remains unproven until the next CI attempt.

## CI run 41443658102 (commit `d726ee9`), 2026-11-02

- Hygiene, extension, companion, and session passed again. `Companion fail-closed checks`
  passed, which confirms the `sudo` PATH theory for that stage. `Network-namespace WireGuard
  capture` still failed with only "Process completed with exit code 1"; the `Browser PAC and
  fail-closed checks` step was skipped as expected after the failure.
- The job log endpoint returns 403 anonymously, so the cause is still unconfirmed. Two
  candidates remain: `tcpdump`/`unshare` unavailable to root on the runner (now a hard failure
  instead of a silent skip, which is the intended behaviour of `LEAK_REQUIRE_NETNS=1`), or a
  genuine capture/test failure inside the namespace.
- The leak job now adds an `Ensure capture tools` step (installs `tcpdump` only when root
  cannot find it) and the capture step writes the script output to `$RUNNER_TEMP` and mirrors
  the diagnostic lines into `::error::` annotations, because annotations are readable without
  a sign-in and logs are not. Verified: the annotation pipeline emits the exit status, the
  `skip:` line with the PATH root saw, and the `FAIL:` line, and still exits 1; the workflow
  parses with 6 leak steps.
- AC2 remains unproven. Nothing about the tunnel invariant changes until that step goes green.

## Mode B web client plan, 2026-11-02 (awaiting approval)

- The user asked to "build browser as a web app" and chose the Mode B streamed-session
  reading, with TypeScript plus Vite and no framework. Drafted
  `docs/plans/phase-2-web-client.md`: a launcher and lifecycle app for the existing `/v1`
  control plane, plus a pointer from `docs/plans/phase-2.md` as proposed step 8.
- No code written. Four approval items are open: dev-only dependencies (`vite`, `typescript`,
  `vitest`), stream presentation (new tab vs `frame-ancestors` iframe decision), a
  `docs/threat-model.md` edit for the hosted launcher and the bearer property of `streamUrl`,
  and confirmation that this is a launcher rather than a pixel client.
- Recorded plainly in the plan: it proves nothing hosted. The stream view, egress path, exit
  IP, and Pod isolation still require steps 3, 5, and 6, and CI's leak job is still failing.
