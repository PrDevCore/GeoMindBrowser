# Decision Log

Architecture decisions for GeoMind. Session-level notes live in
`project-state/decisions.md`; this file holds decisions that outlive a session.

Status values: proposed, accepted, superseded.

## ADR-0001: Go for the native companion

- Date: 2026-10-30
- Status: accepted
- Context: AGENTS.md allows Rust or Go. Phase 1 needs a native-messaging host, a userspace
  WireGuard tunnel, a SOCKS5 proxy, and a local model supervisor.
- Decision: Go.
- Why: wireguard-go is the WireGuard project's reference userspace implementation;
  wireguard-go's netstack avoids TUN privileges and OS routes (fewer leak paths); static
  cross-compilation and simple concurrency suit a tunnel supervisor.
- Consequences: confirm the audit status of wireguard-go before Step 4 (rule 7). Go is not
  installed on the dev machine yet.

## ADR-0002: llama.cpp server as the Phase 1 local model

- Date: 2026-10-30
- Status: accepted
- Context: AGENTS.md allows llama.cpp or ONNX Runtime behind a swappable interface.
- Decision: launch `llama-server` as a child process on loopback with a per-launch API key
  and a SHA-256 model check.
- Why: no Python runtime, GGUF models, simple lifecycle, and the `AIProvider` interface
  keeps the runtime swappable.
- Consequences: model licensing and size need approval; verify the server does not log
  prompts.

## ADR-0003: Fail closed with PAC, no OS firewall in Phase 1

- Date: 2026-10-30
- Status: accepted
- Context: A matched host must never fall back to the real IP, including on failure.
- Decision: generate PAC locally with no DIRECT fallback for matched hosts; validate before
  install; reject connections at the proxy unless the tunnel is healthy; do not implement an
  OS firewall kill switch in Phase 1.
- Why: a firewall changes system network settings (needs approval), and proxy-level
  fail-closed meets the Phase 1 acceptance criteria.
- Consequences: only browser traffic that uses the PAC is protected. Documented residual
  risk. Revisit before claiming system-wide protection.

## ADR-0004: Native messaging instead of a loopback control port

- Date: 2026-10-30
- Status: accepted
- Context: The extension needs policy, tunnel status, and AI streaming.
- Decision: use Chrome native messaging with a fixed extension key and a typed, size-bounded
  protocol. No HTTP control port.
- Why: no local TCP surface for pages or other processes; no origin/CORS mistakes.
- Consequences: per-OS host installation scripts; 1 MB host-to-extension message limit, so
  AI output is chunked.

## ADR-0005: Global WebRTC UDP policy

- Date: 2026-10-30
- Status: accepted
- Context: WebRTC must not expose local or real public candidates on routed tabs.
- Decision: set `webRTCIPHandlingPolicy = disable_non_proxied_udp` globally.
- Why: Chrome exposes only a profile-wide policy; per-tab control is not available in MV3.
- Consequences: WebRTC is disabled on non-routed tabs too in Phase 1. Documented.

## ADR-0006: Timezone matching deferred

- Date: 2026-10-30
- Status: accepted for Phase 1 with the plan approval of 2026-10-30
- Context: Stock Chromium cannot override timezone per site without the debugger API
  (detectable) or an OS change (needs approval).
- Decision: accept the gap for Phase 1 and do not claim timezone matching.
- Consequences: this is a documented deviation from the networking invariant. Revisit before
  any claim of full locale matching, and keep it listed in the threat model residual risks.

## ADR-0007: No infrastructure in Phase 1

- Date: 2026-10-30
- Status: accepted
- Context: Real exit nodes and orchestration need approval and are not required by the
  Phase 1 acceptance criteria.
- Decision: test against a local WireGuard peer and a local exit resolver; no Terraform,
  no cloud resources.

## ADR-0008: Policy matching and PAC shape

- Date: 2026-10-30
- Status: accepted
- Context: rules must apply deterministically and fail closed inside a PAC script.
- Decision:
  - Precedence: exact over wildcard, deeper wildcard suffix over shallower, higher priority,
    then rule order.
  - `*.example.com` excludes the apex; covering both needs two rules.
  - A route rule with an unknown exit compiles to block.
  - Block maps to a dead loopback proxy (`SOCKS5 127.0.0.1:9`), never DIRECT.
  - The PAC is a JSON rule table plus a fixed interpreter; only the default returns DIRECT.
  - The PAC lowercases the host and strips one trailing dot.
  - Phase 1 rejects IP literals and allows only `defaultAction: "direct"`.
- Why: no per-rule code generation, easy to validate, and one golden artifact shared by the
  Go and TypeScript tests.
- Consequences: no IP or CIDR rules yet; an invalid PAC is rejected instead of installed;
  PAC behavior assumes Chrome passes a canonical ASCII host.

## ADR-0009: wireguard-go with netstack and StdNetBind

- Date: 2026-10-30
- Status: accepted
- Context: Phase 1 needs a userspace tunnel with no OS route or TUN device, so a routed
  host has no path to the real network. The library is a network and crypto dependency,
  which needs an explicit approval (AGENTS.md rule 7).
- Decision:
  - Use `golang.zx2c4.com/wireguard` (`device`, `conn`, `tun/netstack`) configured through
    the UAPI `IpcSet`/`IpcGet` protocol. Only `golang.org/x/net` is a direct extra module;
    gVisor and wintun arrive as transitive requirements.
  - The device is created with `conn.NewStdNetBind()`. The Windows ring bind stops
    receiving after one UDP ICMP port-unreachable, which breaks reconnects.
  - All dialing, including DoH, goes through `netstack.Net`; the tunnel has no host-network
    path, and `DialContext` refuses non-TCP networks and unhealthy states.
  - Keys come from `GEOMIND_WG_*` env vars, live only in memory, and are never logged or
    written to disk. Errors never echo key material.
  - The wireguard-go logger is replaced with a no-op: its default writes to stdout and
    would corrupt the native messaging stream.
  - `Healthy()` is the supervisor's view of handshake freshness, refreshed from
    `last_handshake_time_sec`, not from the device's `Up()` state.
- Why: the reference implementation, no custom crypto, no TUN privileges, and no possible
  OS-level fallback path.
- Audit status: the WireGuard protocol has formal analysis; there is no confirmed
  standalone third-party audit of the Go implementation. The user approved this dependency
  on 2026-10-30 for Phase 1, with the gap recorded for the Phase 1 security review.
- Consequences: gVisor and wintun are vendored and built even though wintun is unused at
  runtime; dependency updates must be reviewed manually; the Linux netns capture suite is
  the only proof that no packet leaves outside the tunnel.

## ADR-0010: Extension routing install and browser test harness

- Date: 2026-10-30
- Status: accepted
- Context: a PAC that Chrome cannot parse falls back to DIRECT, so the install step is the
  fail-closed boundary. Browser behavior cannot be proven by unit tests alone.
- Decision:
  - The extension validates the companion PAC before install: fixed header, strict JSON rule
    table, exactly one loopback SOCKS5 route target, dead block proxy, and the exact
    interpreter body. It mirrors the Go validator but does not regenerate the PAC.
  - A valid PAC is installed as `pac_script` with `mandatory: true` in regular scope.
  - A rejected or unavailable PAC never clears an already installed PAC. The last valid
    PAC keeps routing matched hosts to the companion proxy, which fails closed. The badge
    (`!`) and session status surface the degraded state.
  - The badge is the only routing UI in this step; there is no popup or options page.
  - Browser tests use Playwright 1.64.1 with its pinned Chromium in a persistent, headful
    profile, a real companion native host, and a local origin server.
- Why: one validation path before install, no silent DIRECT fallback, and an automated
  browser-level proof of fail-closed routing without network namespaces.
- Consequences: Playwright and Chromium are dev-only network dependencies (approved
  2026-10-30); the browser suite needs a display or xvfb on Linux; header rules and
  `Accept-Language` remain unimplemented; DNS/WebRTC packet capture still requires the
  Linux netns suite.

## ADR-0011: Policy hardening and CI lint parity

- Date: 2026-10-30
- Status: accepted
- Context: a full-repo analysis found four gaps. Go accepted numeric IPv4 forms the
  extension rejects, so a policy could load on one side and fail on the other. Go accepted
  a missing `rules` field, accepted unbounded priority, and the policy `Store` had no
  synchronization ahead of hot reload. CI ran neither the extension typecheck nor the
  declared golangci-lint configuration.
- Decision:
  - Go rejects a host whose final label is a decimal or `0x`-hex number, plus canonical IP
    literals, matching WHATWG URL IPv4 parsing. A browser resolves such a host as an
    address, not a name, so `010.0.0.1`, `2130706433`, `0x7f000001`, `0x7f.0.0.1`,
    `999.999.999.999`, and `example.123` are all invalid patterns in both languages.
  - Rule priority is bounded to ±1,000,000 in Go, matching the schema and TypeScript.
  - `decodeDoc` rejects a missing or null `rules` field.
  - `Store` uses a `sync.RWMutex`: `Load` swaps the compiled policy under the write lock
    and `Match`/`PAC` read under a read lock. `knownExits` is immutable after `New`.
  - Shared invalid fixtures cover all of the above, so Go and TypeScript must both reject
    them. A concurrent reload/match test runs under `-race` in CI.
  - CI runs `npm run typecheck` and golangci-lint v2.15.0, installed with
    `go install ...@v2.15.0` so the version is pinned in the workflow.
- Why: divergent host validation is a fail-open-adjacent risk, and an unsynchronized store
  would become a data race the moment reload is wired. Pinning the linter makes local and
  CI results match (2.15.0).
- Consequences: domains ending in a numeric label are rejected in Phase 1 even though no
  such public suffix is expected; the Go race detector still needs a C toolchain, so local
  Windows runs skip `-race` while Linux CI enforces it.

## ADR-0012: Live tunnel status polling

- Date: 2026-10-30
- Status: accepted
- Context: the badge reflected tunnel state only at connect time. If the tunnel dropped
  later, the PAC stayed installed and routed tabs failed closed, but the user saw no signal.
- Decision:
  - While the native port is connected, the service worker requests `tunnel.status` every
    5 seconds. Overlapping requests are suppressed; the timer stops on disconnect, suspend,
    protocol mismatch, or a connect error.
  - Status payloads are strictly parsed (`up`, `down`, `starting`; string-or-null exit and
    region; non-empty `since`). A malformed reply leaves the last known state in place.
  - Badge precedence: routing error or unavailable wins; otherwise a down tunnel shows a
    red `!`, a starting tunnel an amber `…`, and only routing active plus tunnel up clears
    the badge.
  - Polling never touches `chrome.proxy.settings`. The installed PAC remains the routing
    switch, so a failed poll cannot open a DIRECT fallback.
- Why: surface the degraded state without weakening the fail-closed data plane, and keep
  the decision logic pure and testable outside the service worker.
- Consequences: about 12 native messages per minute per browser; status can be up to one
  poll interval stale, and a hung (not disconnected) companion keeps its last state until a
  request fails; the no-WireGuard browser harness now legitimately shows `tunnel down`.

## ADR-0013: Policy hot reload over the native messaging stream

- Date: 2026-10-30
- Status: accepted
- Context: the companion loaded the policy once at startup, so any rule edit required
  restarting the browser or the host. The policy `Store` was already lock-protected
  (ADR-0011), but nothing reloaded it, and the extension had no way to learn about a change
  except by reconnecting.
- Decision:
  - The companion watches the policy file's modification time and size once per second.
    A changed file is reloaded through `Store.Load`; a document that fails validation leaves
    the previous policy in effect and is retried on later ticks.
  - A successful reload emits `policy.changed` with an empty payload. The envelope uses the
    reserved id `event`; requests with that id are rejected as malformed, so a response and
    an event can never be confused.
  - All frames (responses and events) are written through one mutex-protected `Sender`
    (`ServeWithSender`), because the watcher and the request loop are different goroutines.
    A failed stream stays failed rather than writing into a broken pipe.
  - `Store.Snapshot` returns the current document under the read lock. `hello` and
    `policy.get` now read it, so a reload is visible without a reconnect. A published
    document is never mutated in place.
  - The extension refetches the policy on `policy.changed`, reinstalls the validated PAC,
    and updates status and badge. Refreshes are serialized with a dirty flag; a failed
    refresh keeps the old PAC installed and does not disconnect.
  - Session status writes are chained so the 5-second tunnel poll and a policy refresh cannot
    land out of order.
- Why: rule edits should not require restarting the browser, and the reload path must be at
  least as fail-closed as startup: invalid input must never replace a working PAC.
- Consequences: one `stat` per second; a reload that keeps the same version number is still
  applied (the extension treats each fetched document as new); the options page and status
  reflect the new rule count within a second. Rules still come from a local file; there is
  no editing UI that writes policies.

## ADR-0014: Accept-Language header rules and the options page

- Date: 2026-10-30
- Status: accepted
- Context: networking invariants require Accept-Language to match the exit country; ADR-0010
  recorded header rules as unimplemented. Chrome can only modify request headers through
  `declarativeNetRequest`, which requires host access for the affected URLs.
- Decision:
  - Add `declarativeNetRequestWithHostAccess` and declare `optional_host_permissions`
    `*://*/*`. No permission is requested until the user presses Enable on the options page;
    the request contains only the origins of routed sites, never the full pattern. No
    `<all_urls>`, no host permissions at install time.
  - `planHeaderRules(policy, locale)` produces one `modifyHeaders` rule per routed pattern,
    setting only `accept-language` to the validated exit locale. Exact rules use an anchored
    `regexFilter` for the host and optional port; wildcard rules require at least one label
    below the suffix, mirroring policy matching. Block rules and the default action are
    ignored, and there are no rules when the exit reports no locale.
  - Rules are installed only for origins the user granted; a revoked permission silently
    drops that host's rule instead of widening access. Disabling removes only rules in the
    extension's own id range and removes the optional origins.
  - The service worker stores the last valid policy in session storage and re-plans rules
    after connect, policy reload, tunnel locale change, or a change to the enabled flag.
  - The options page shows routing state, exit locale, routed sites, and the header toggle.
    Rules are still edited in the policy file; the page manages permission, not policy text.
- Why: locale consistency without granting broad access, and with the same fail-closed
  posture: a missing permission or locale means no header change, never a direct request.
- Consequences: only top-level navigations are guaranteed to be modified; a subresource
  whose initiator is a different, unpermitted site is not (Chrome requires initiator host
  access). The header is visible to the target site and is not a fingerprint defense by
  itself. Actual header application needs a manual browser check because the permission
  prompt is native UI that the automated suite cannot accept; the suite instead proves the
  locale reaches the extension, that no rules exist before a grant, and the plan/install
  logic with a stubbed API.

## ADR-0015: Same-user local processes are out of scope for Phase 1 routing integrity

- Date: 2026-10-31
- Status: accepted (user decision, 2026-10-31)
- Context: The independent review found R1. After the companion exits, the installed PAC
  still points at its ephemeral loopback port. A process running as the same OS user can bind
  that port and speak SOCKS5, so a routed request can leave from the real IP. PAC cannot
  authenticate its SOCKS5 peer, Chrome starts the native host as its child, and Phase 1 has
  no reconnect timer or proxy-drift detection.
- Decision:
  - Phase 1's fail-closed guarantee covers tunnel failure, proxy refusal, companion exit,
    policy-sync failure, browser restart, and service-worker restart. It does not cover a
    hostile process running as the same OS user.
  - E8 (other local user or process) is out of scope for Phase 1 routing integrity and
    confidentiality. The trusted-OS-user assumption is explicit: code with the user's
    privileges can already read the profile, keys, and AI context, and can control the
    browser.
  - The extension still keeps the last valid PAC installed on companion loss and never
    replaces it with DIRECT. That remains the fail-closed behavior for process and network
    failures.
  - Required follow-up hardening (does not close R1): reconnect with backoff while the
    service worker is alive; detect proxy-setting drift and surface it; surface privacy
    policy errors; keep the badge truthful. These get tests when implemented.
  - Reopen this decision before any claim of resistance to local malware. Closing R1
    requires OS-level isolation (separate user, firewall, or an authenticated data plane)
    outside Phase 1.
- Why: a persistent companion daemon narrows the window but does not close it, because the
  same user can kill it and rebind the port; SOCKS5 peer authentication is not expressible in
  a PAC; and same-user code can read the keys and profile regardless. The honest boundary is
  the OS user.
- Alternatives rejected: persistent per-user daemon (narrows but does not close the window,
  adds lifecycle and update surface); authenticated loopback proxy (not expressible through
  PAC); OS firewall or kill switch (changes system network settings, requires approval,
  deferred).
- Consequences: R1 is not a Phase 1 acceptance blocker, but it is a documented residual risk
  and must not be described as "no real-IP fallback under all conditions." R2 (proxy drift)
  remains open and is a Phase 1 blocker. Threat model T1, section 7, section 8, section 9,
  and the plan invariant map are updated; user-facing claims must state the trusted-OS-user
  boundary.

## ADR-0016: Phase 2 remote-session stack and control-plane shape

- Date: 2026-11-01
- Status: accepted for the stack and step 1 (user decision, 2026-11-01). Identity provider,
  integration target, and the threat-model status change remain open.
- Context: The user asked to complete Mode B and host it. `docs/plans/phase-2.md` was written
  and approved before implementation, per `.qoder/rules/01-workflow.md`. This development host
  has no Docker, Kubernetes, Terraform, or cloud credentials, so only the control plane can be
  built and tested locally.
- Decision:
  - Streaming: Kasm Workspaces with KasmVNC. Apache Guacamole is the fallback if Kasm
    licensing or operational weight is unacceptable.
  - Orchestration: managed Kubernetes with one ephemeral Pod per session. Nomad is the
    fallback.
  - Control plane: Go, in a separate stdlib-only module `session/`. HTTP handlers contain no
    Kubernetes types; the runtime is behind an interface.
  - Infrastructure: Terraform with `plan` only in CI; the user applies.
  - Identity: OIDC from an existing provider. The local `DevVerifier` is not a hosted
    authentication mechanism and must be replaced before any deployment.
  - The control plane assigns the session ID, enforces per-user and global caps plus request
    rate limits, and owns TTL, idle expiry, reconciliation, and idempotent teardown. Stream
    URLs are minted by the streaming provider and revoked on teardown.
  - No persistent disk, no shared volumes, no cross-session state, and no page content in the
    control plane or its logs.
- Why: the stack matches the AGENTS.md defaults, keeps provider-owned crypto and streaming,
  and lets the control plane be verified on Windows with a fake runtime before any cluster
  exists. Assigning the ID in the control plane makes caps, ownership, and cancellation
  accounting correct before a workload is created.
- Alternatives rejected: Guacamole as the first choice (heavier image and desktop stack for
  this slice, kept as fallback); Nomad (viable, but Kubernetes has native NetworkPolicy,
  ephemeral storage, and per-Pod teardown); a home-grown auth service (custom security code);
  starting with Kubernetes code (untestable on this host and premature before the API shape is
  settled).
- Consequences: step 1 is a real API over a fake runtime, not a hosted product. The session
  image, streaming integration, Terraform, staging deploy, and extension entry point remain
  steps 2-7. Mode B terminates TLS inside the session container, so the exit operator can see
  page content; user-facing docs must say so before any hosted release. Moving Mode B from
  design-level Phase 3+ to an active phase requires approval to edit `docs/threat-model.md`.

## ADR-0017: Kubernetes session runtime contract

- Date: 2026-11-01
- Status: accepted (step 2 of `docs/plans/phase-2.md`, requested by the user 2026-11-01).
- Context: Step 1 put a `session.Runtime` interface in front of provisioning with a fake
  implementation. Step 2 needs a real implementation, and the user asked to start it. This
  also approves the client-go dependency (a networking dependency under the AGENTS.md
  approval rule). There is still no cluster, kubectl, or Docker on this host, so only
  fake-client and manifest tests can run locally.
- Decision:
  - Dependency: `k8s.io/client-go`, `k8s.io/api`, and `k8s.io/apimachinery` v0.37.2, pinned
    in `session/go.mod`. Nothing else from the Kubernetes ecosystem is added.
  - Objects: one Secret (32 random bytes, key `session-token`), one NetworkPolicy, and one
    Pod per session, in one namespace. No Service, PVC, or ServiceAccount.
  - Names: `gms-` plus the first 40 hex characters of SHA-256 of the session ID. The name is
    DNS-1123 safe, deterministic, and does not expose the ID; the full ID, region, TTL, and
    idle timeout are annotations.
  - Pod: non-root (10001), `RuntimeDefault` seccomp, read-only root filesystem, all
    capabilities dropped, no privilege escalation, no service-account token, no service
    links, no host namespaces, `RestartPolicy: Never`, zero termination grace, TTL as
    `ActiveDeadlineSeconds`, and only `emptyDir` volumes with size limits for `/tmp` and the
    session home.
  - NetworkPolicy: default-deny with ingress only from `streaming-edge` Pods on the named
    `vnc` port and egress only to the configured proxy component on TCP. There is no DNS
    egress rule; routed names resolve at the exit through the proxy, matching the Phase 1 DNS
    invariant.
  - Create is ordered Secret, NetworkPolicy, Pod, then wait for readiness. Existing objects
    are tolerated so a retried create converges instead of failing.
  - Delete removes the Pod first and waits for it to disappear, then removes the
    NetworkPolicy and Secret. If the Pod is still terminating or its delete fails, the policy
    and secret are retained and an error is returned so `Sweep` retries; egress is never
    unconstrained for a running Pod.
  - The control plane gets a separate `CreateTimeout` (default 2m, or the Kubernetes ready
    timeout plus 30s) so a slow Pod start is not cut off by the 20s per-call timeout. The
    HTTP write deadline follows it.
- Why: deterministic names make apply and delete idempotent without a lookup index; hashing
  keeps the session ID out of object names; the delete order preserves the fail-closed
  property; no DNS egress keeps the Phase 1 invariant inside the cluster.
- Consequences: `sessiond` needs RBAC for pods, secrets, and networkpolicies
  (get/create/delete) in the session namespace, and read access to Pods for status; this is
  not yet written as manifests or Terraform. The Pod will not become ready until step 3
  provides an image that listens on 6901. Readiness is a TCP probe, so step 3 must confirm the
  KasmVNC listener. Image references are tags; pinning by digest is follow-up hardening.
  Integration against a real cluster remains unverified on this host.

## ADR-0018: Apache-2.0 repository license

- Date: 2026-11-01
- Status: accepted (user decision, 2026-11-01).
- Context: The repository had no license file and no license metadata, which under default
  copyright means all rights reserved and blocks contributions and redistribution. The user
  asked for Apache-2.0.
- Decision:
  - Original GeoMind code, documentation, and design assets are licensed under the Apache
    License, Version 2.0, in `LICENSE` (verbatim text, placeholder appendix).
  - `NOTICE` carries the copyright line (`Copyright 2026 Pr Dev Core Tech`) and states that
    third-party dependencies keep their own licenses.
  - `package.json` and `extension/package.json` declare `"license": "Apache-2.0"` and
    `"author": "Pr Dev Core Tech"`. Go modules have no license field; `LICENSE` at the
    repository root covers them.
  - Third-party code is not relicensed. client-go and the Kubernetes libraries are
    Apache-2.0, wireguard-go and llama.cpp are MIT; their notices and license files stay
    intact.
- Why: Apache-2.0 grants an explicit patent license, which MIT does not, and is compatible
  with the Kubernetes/client-go dependency set. It is a common, review-friendly choice for
  infrastructure code.
- Consequences: redistributors must include `LICENSE`/`NOTICE` and mark modified files.
  Contributions are inbound under section 5 with no CLA; a CLA or DCO can be added later if
  outside contributions begin. Apache-2.0 does not grant trademark rights, so the GeoMind name
  and icon remain reserved. The copyright holder is written as "Pr Dev Core Tech", the name
  the user supplied on 2026-11-01; update `NOTICE` (not the license text) if that changes.

## ADR-0019: Timezone and geolocation parity (review finding R4)

- Date: 2026-11-01
- Status: accepted (user decision, 2026-11-01: "proceed with the recommended").
- Context: The security review's R4 finding observed that the invariant "timezone, locale,
  Accept-Language, and geolocation match the exit country" is unmet. `Accept-Language` is
  already matched per origin with explicit consent (ADR-0014), and ADR-0006 accepted the
  timezone gap for Phase 1, but geolocation was neither matched nor documented. Chrome can
  answer a location request from the OS provider (Wi-Fi and network beacons), which does not
  pass through the proxy, so a routed tab could read the real coordinates while the page
  itself arrives from the exit.
- Decision:
  - Geolocation fails closed on routed origins. The extension sets
    `chrome.contentSettings.location` to `block` for every routed policy pattern (exact
    hosts and wildcards), when the policy loads and whenever it changes.
  - Blocks are monotonic: a policy edit never lifts one, so a reload cannot open a window.
    Chrome drops rules attributed to this extension when it is disabled or removed.
  - The manifest gains the `contentSettings` permission, used only for `location`. The
    options page shows how many routed sites are blocked, next to the WebRTC status.
  - Timezone and `navigator.language`/`Intl` stay host-derived. MV3 has no API to change
    them; only `chrome.debugger` with CDP `Emulation.setTimezoneOverride` could, which needs
    a debugger attached to every routed tab, shows a debugger banner, and is itself a
    distinguishing signal. Rejected for Phase 1.
  - Phase 1 therefore does not promise timezone or language parity. A routed site can still
    compare its timezone and `Intl` data with the exit country. Full parity belongs to Mode B,
    where the session container runs in the exit's timezone and locale and has no real
    location to read.
- Why: Blocking removes the only precise, high-value signal (coordinates) that the proxy
  cannot cover, and does it without faking values. Faking timezone or locale partially would
  add a detectable inconsistency on top of the existing mismatch, and the networking rule
  "normalize fingerprints, do not randomize" rules out layering overrides that only some
  surfaces honour.
- Consequences: Sites that ask for location on a routed tab receive a denial instead of a
  mismatched position; this is visible in the options page. Blocks persist until the
  extension is disabled or removed, including for hosts removed from the policy, which is
  conservative rather than convenient. `docs/threat-model.md` was updated on 2026-11-01
  (T16 and section 8.1) with the user's approval of that wording change.
- Verification: unit tests for pattern planning and per-pattern failure handling
  (`extension/test/unit/geolocation.test.ts`), manifest permission assertion, and a Playwright
  test that reads the setting back in Chromium (`block` for the routed host, `ask` for a
  direct host) plus the options-page status line.

## ADR-0020: Mode B launcher as a static web client

- Date: 2026-11-02
- Status: accepted (user decisions, 2026-11-02: "Mode B streamed session as a web app" and
  "TypeScript + Vite, no framework", then "fix all" for the outstanding CI, end-to-end, and
  documentation work, which includes the `docs/threat-model.md` section 6 wording below).
- Context: The user asked to "build the browser as a web app". Mode B already renders inside
  an ephemeral container and streams pixels, so the only part that can be a web app is the
  session launcher: pick a region, obtain a session, open the stream. Building a WebRTC
  pixel client here would mean custom media-stack code that ADR-0016 assigns to KasmVNC, and
  it cannot be tested without an exit node. `docs/plans/phase-2-web-client.md` was written
  and approved before implementation, per `.qoder/rules/01-workflow.md`.
- Decision:
  - `web-client/` is a static, framework-free TypeScript + Vite app. It talks only to the
    documented `/v1` control-plane contract (`POST`/`GET`/`DELETE /v1/sessions`, `/healthz`)
    and never touches a session container directly.
  - It is a launcher, not a viewer. The stream opens in a new tab through a single
    `https`-only anchor with `rel="noreferrer noopener"` and `referrerpolicy="no-referrer"`.
    KasmVNC owns DTLS, codecs, and input; no custom media code ships here.
  - The bearer token is typed by the user, held in a module closure for the page lifetime,
    cleared from the field after read, and never written to a URL, storage, or console.
    Requests use `credentials: "omit"`, `cache: "no-store"`, and `no-referrer`.
  - Fail closed maps to lifecycle: an unreachable control plane, an illegal state
    transition, a rejected token, or a provisioning budget overrun ends the session with a
    plain-language message and no stream link. `pagehide` sends a keepalive `DELETE`; the
    control-plane TTL reaper is the backstop, not a second client-side retry.
  - Hosting is same-origin behind a reverse proxy (`/v1`, `/healthz`) so the launcher never
    holds a cross-origin API base. The CSP meta omits `frame-ancestors`, which is ignored in
    a meta; the host must send it.
  - Dependencies are `typescript`, `vite`, and `vitest` only, per the user's stack choice.
    No lint tool is configured; `npm run lint --if-present` in CI skips cleanly until one is
    chosen.
- Why: This is the largest slice of "the browser as a web app" that is honest about the
  architecture, testable without infrastructure, and adds no new network or crypto code. The
  alternative (a custom stream client) would be unverifiable here and would duplicate a
  vetted library.
- Consequences: The launcher host sees session metadata and the token-bearing request;
  `docs/threat-model.md` section 6 now records that plus the bearer property of `streamUrl`,
  and adds launcher-host and stream-URL rows to the Mode B threat table. Nothing is hosted,
  so "works when deployed" remains unproven; only the fake-runtime control plane was run. The
  identity provider question from ADR-0016 stays open: `web-client` assumes a token already
  exists, so real deployment needs the OIDC broker before it.
- Verification: `npm run typecheck` and 45 vitest cases in `web-client/test/` (state machine,
  request shape and headers, error mapping, lifecycle including teardown-once and budget
  timeout, stream-link gating, rendering, and a source scan that forbids `localStorage`,
  `sessionStorage`, `document.cookie`, `indexedDB`, `console`, and any third-party origin in
  `src/`). End to end, `npm run test:web` (`tests/browser/webclient.spec.ts`, Playwright,
  not in CI: it needs Go, a display, and loopback 8080/4173) builds and runs real `sessiond`
  with `GEOMIND_SESSION_RUNTIME=fake`, serves the built bundle over `vite preview`, and
  asserts Start → Streaming with an `https://stream.invalid/s/<id>` link → Stop → the control
  plane returns `404` for that session, a rejected token produces the plain-language failure
  with no session, and the page leaves no storage, cookies, or token text in the DOM. The CI
  `web-client` job runs install, typecheck, test, and build.

## ADR-0021: Import the team-workspace mock as workspace-demo

- Date: 2026-11-04
- Status: accepted (the user supplied `https://github.com/PrDevCore/GeoMindBrowser.git` with
  "COMMIT" and chose "branch in GeoMindBrowser" over a separate repository).
- Context: A React + Express mock of a team workspace (browser panel, sprint board, shared
  documents, team chat, VPN console) existed outside this repository, generated from a Google AI
  Studio scaffold. It carried a server-side Gemini copilot and UI that claimed encryption,
  masked IPs and live gateway metrics. Before import those were removed: the copilot, its modal,
  the four buttons that opened it, the `@google/genai` dependency and every claim of security in
  the copy are gone, and the simulated panels now label themselves as simulations.
- Decision:
  - The app lands as `workspace-demo/`, a self-contained npm package with its own
    `package.json`, lockfile, tsconfig and Vite config. Nothing in the root package, the
    extension or `web-client` changes.
  - It is a design reference, not a shipping component. `workspace-demo/README.md` states that
    no traffic is routed and no AI exists, and the directory is named `demo` rather than `client`
    so it cannot be read as a sibling of `web-client/`.
  - No CI job is added yet. A new job would install React, Vite, Tailwind and esbuild on every
    run of the shared pipeline before anyone has agreed the mock is worth that time; the local
    checks below are recorded instead, and wiring CI is left as an explicit follow-up.
  - The hard constraints hold without editing `docs/threat-model.md`, which needs approval to
    change: there is no AI code left, so constraints 3 and 5 are out of scope by absence, and
    constraint 2 holds because only task, document and chat content plus a `localStorage` theme
    choice are persisted. Browser tab state stays in component state, and nothing is sent
    anywhere except the same-origin `/api/store` routes.
- Why: Keeping the mock in the same repository as the product makes the workspace UX reviewable
  next to the routing rules it would eventually have to obey. The alternative - pushing this
  history onto the remote's `main` - would have needed a force-push that deletes the existing
  product history, so it was rejected outright.
- Consequences: The repository now holds a second, non-default stack (React 19, Vite 8,
  Express 4, Tailwind 4) alongside the MV3 extension and the framework-free launcher. Promoting
  any part of it into a real client needs its own ADR and a dependency approval, because the
  stack defaults in `AGENTS.md` do not cover it. `workspace-demo/data/store.json` is runtime
  state and is gitignored. The demo server listens on `0.0.0.0`, so it is reachable on the
  developer's LAN and must stay a local process, not a hosted service.
- Verification: Run in `workspace-demo/` after the copy: `npm install` (220 packages),
  `npm run typecheck` (clean), `npm test` (6/6 `node:test` cases: seeding, collection allowlist
  and traversal rejection, PUT round-trip, restart without reseeding, body validation, and a 404
  assertion that no `/api/gemini/generate` exists), `npm run build` (282.92 kB JS at 84.01 kB
  gzip, 37.93 kB CSS). A browser pass over all five tabs of the built bundle reported zero
  AI/Gemini strings, zero console errors and a store that still loads; it ran on the identical
  sources immediately before the copy, not inside this checkout.
