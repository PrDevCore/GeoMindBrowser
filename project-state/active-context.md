# Active Context

Updated: 2026-11-02

## Where we are

Phase 1, Steps 1-5 are implemented and all locally runnable suites pass, but the independent
security review returned **FAIL** (2026-10-31). Step 5's Linux netns packet capture is still
outstanding, and Step 6 is blocked on approval for llama.cpp and a GGUF model. The plan was
approved 2026-10-30; the WireGuard dependency was approved on 2026-10-30 (ADR-0009) and
Playwright with its pinned Chromium was approved on 2026-10-30 (ADR-0010).

Step 5 landed live tunnel status polling (ADR-0012), policy hot reload (ADR-0013), exit
locale over IPC, per-site `Accept-Language` header rules with a minimal options page
(ADR-0014), and the locale parity fixture table. The review (R1-R9 in
`project-state/progress.md`) found a critical stale-port takeover path after companion loss
and a high proxy-drift detection gap. R1 is now an explicit Phase 1 scope decision
(ADR-0015, 2026-10-31): the OS user boundary is trusted and same-user processes are out of
scope for routing integrity. On 2026-11-01 R2, R3, and R5-R8 were fixed with tests (PAC
read-back and drift repair, visible WebRTC failures, race fixes, atomic `policy.get`,
options grant cleanup). R4 was decided on 2026-11-01 (ADR-0019): geolocation is blocked on
every routed origin through `contentSettings.location`, and timezone/`Intl`/language parity
is documented as out of Phase 1 scope. Phase 1's tunnel capture now runs green in CI
(run 41508832667); AC2 still needs IPv6, QUIC, and browser-level WebRTC capture cases, and
Step 6 is open.

Phase 2 (Mode B, hosted remote browser) started on 2026-11-01 after the user approved
`docs/plans/phase-2.md` and the Kasm + managed Kubernetes target. Step 1 is done: the
`session/` Go module implements the control-plane API, lifecycle state machine, ownership
checks, TTL/idle expiry, concurrency caps, rate limits, and idempotent teardown against an
in-memory fake runtime. Step 2 is also done: a client-go runtime (v0.37.2) builds one Secret,
one NetworkPolicy, and one Pod per session, waits for readiness, and tears down in a
fail-closed order, verified with the fake clientset and manifest assertions. No object has
been applied to a real cluster; there is no image, streaming provider, OIDC, or Terraform yet,
and nothing is hosted.

Phase 2 step 8 landed on 2026-11-02 (ADR-0020): `web-client/`, a framework-free TypeScript +
Vite launcher for Mode B. It speaks only to the `/v1` contract, mirrors the server state
machine, keeps the bearer token in a page-lifetime closure, opens the provider view as an
`https`-only noreferrer link, and tears the session down on stop, expiry, error, and
`pagehide`. It is not a pixel client — KasmVNC owns DTLS and codecs. Verified locally with 45
vitest cases, a typecheck, a production build, and a headful Playwright run (`npm run test:web`)
against a real `sessiond` in fake-runtime mode served over `vite preview`; a `web-client` CI job
runs install, typecheck, test, and build. Nothing about it is hosted.

Step 1 delivered: the Go policy package (parse, validate, match, PAC generation and
validation), the TypeScript policy mirror, shared fixtures, and a golden PAC checked by both
languages.

Step 2 delivered: native messaging codec and typed dispatch (`hello`, `policy.get`,
`tunnel.status`), exit metadata from env, the companion binary, MV3 manifest with a fixed
key, the background native-messaging client and service-worker bootstrap, and the native host
installers for POSIX and Windows.

Step 3 delivered: the handshake-freshness supervisor and the loopback SOCKS5 proxy with a
fail-closed health gate, plus an AST guard that the proxy package contains no direct dial or
local resolver path.

Step 4 delivered: the userspace WireGuard device over gVisor netstack with UAPI
configuration, env-only keys held in memory, a DoH resolver that dials only through the
netstack, and supervisor-backed health derived from handshake freshness. Two real peers
complete handshakes in tests over loopback.

Step 5 delivered: the companion starts the device plus a loopback SOCKS5 proxy (a closed
tunnel when WireGuard is not configured) and reports live status; the extension validates
and installs the PAC, applies the WebRTC policy, re-applies it on drift, polls tunnel status
every 5 seconds, and shows a badge when routing is unavailable or the tunnel is down. A
Playwright test loads the built extension with a real native host and proves fail-closed
routing to a local origin in Chromium. Policies reload from disk without a restart, the exit
locale travels over IPC and is covered by shared Go/TS fixtures, and an explicit per-site
grant installs an `Accept-Language` DNR rule for routed origins; nothing is granted at
install time.

Analysis session 2026-10-30: the four findings were fixed and verified (ADR-0011). Go now
rejects numeric IPv4 forms (`010.0.0.1`, `2130706433`, `0x7f000001`, `0x7f.0.0.1`,
`999.999.999.999`, and a numeric final label such as `example.123`), bounds priority to
±1,000,000, and rejects a missing/null `rules` field; the policy `Store` is guarded by an
`RWMutex`; CI now runs `npm run typecheck` and golangci-lint 2.15.0. Shared invalid fixtures
grew from 14 to 26 so both languages must reject the same documents.

## Environment

- Go 1.27.3 at `C:\Users\USER\tools\go\bin`. Not on the system PATH: export it before Go
  commands, or add it permanently.
- golangci-lint 2.15.0 at `C:\Users\USER\go\bin\golangci-lint.exe`.
- Node 24 / npm 11. Extension dev dependencies installed; `extension/package-lock.json` is
  present and should be committed.
- WireGuard modules are in `companion/go.mod`/`go.sum`; no system WireGuard tools or TUN
  driver are needed because the tunnel is userspace.
- Playwright 1.64.1 is a root dev dependency; Chromium 156 is installed under
  `%LOCALAPPDATA%\ms-playwright`. Browser tests run headful and need a display.
- Still missing: Docker, llama.cpp, a GGUF model, kubectl, Terraform, and cloud credentials.
  The netns leak suite needs Linux. The `session` module's Kubernetes runtime is fake-client
  tested only and needs a cluster for integration; its default fake runtime runs here.
- Git: baseline committed and pushed 2026-11-01 to
  https://github.com/PrDevCore/GeoMindBrowser (`8b8df19`, 164 files, authored as
  "Pr Dev Core Tech"). The remote's own LICENSE-only "Initial commit" was merged, not
  overwritten. Windows Credential Manager now holds the GitHub credential.

## Decisions

- Go companion, llama.cpp inference (ADR-0001, ADR-0002).
- Fail-closed PAC plus proxy gate, no OS firewall in Phase 1 (ADR-0003).
- Native messaging only, no loopback control port (ADR-0004).
- Global WebRTC UDP policy (ADR-0005).
- Timezone gap accepted for Phase 1 (ADR-0006).
- Policy precedence and PAC shape (ADR-0008).
- wireguard-go + netstack with `StdNetBind`, env-only keys (ADR-0009, user-approved; no
  confirmed standalone audit, recorded for the Phase 1 security review).
- Validate PAC before install; never clear a valid PAC on failure; badge-only UI; Playwright
  browser harness (ADR-0010).
- Poll tunnel status every 5s; badge prefers routing errors, then tunnel down/starting; a
  failed poll keeps the last state and never touches the PAC (ADR-0012).
- Watch the policy file (mtime + size, 1s), emit `policy.changed` under a reserved event id,
  serialize writes with a latched sender, and keep the old policy on a failed reload
  (ADR-0013).
- Set `accept-language` with `declarativeNetRequestWithHostAccess` only for routed origins
  and only after an explicit per-origin grant; options page owns grant/revoke (ADR-0014).
- Trust the OS user boundary for Phase 1. Fail-closed covers tunnel, proxy, companion, policy,
  and service-worker failures, not a hostile same-user process; E8 is out of scope
  (ADR-0015). Proxy-drift detection and repair shipped 2026-11-01; reconnect with backoff
  remains follow-up hardening.
- Geolocation fails closed on routed origins (`contentSettings.location` = block, monotonic,
  applied on policy load and change); timezone and `Intl` stay host-derived and parity is a
  Mode B property. The manifest now requests `contentSettings` for this one content type
  (ADR-0019).
- Session runtime: client-go 0.37.2 with one Secret, NetworkPolicy, and Pod per session;
  hashed DNS-safe names; readiness wait; Pod deleted before the NetworkPolicy and Secret; no
  DNS egress rule (names resolve at the exit); non-root, read-only root FS, dropped
  capabilities, no service-account token (ADR-0017).
- The Mode B web app is a launcher, not a pixel client (ADR-0020): `/v1` only, no custom
  media code, token in a closure and never in a URL, new-tab stream view, same-origin hosting
  behind a reverse proxy, dependencies limited to `typescript`/`vite`/`vitest`.

## Open decisions

1. OS firewall kill switch in Phase 1 or later.
2. Per-region browser profiles or a shared profile.
3. Approval for llama.cpp plus a GGUF model (Step 6).
4. Where to run the Linux netns capture: CI runner or a Linux VM.
5. Whether OS-level isolation (separate user, firewall, authenticated data plane) is required
   before Phase 2 to bring same-user processes back into scope (ADR-0015 defers it).
6. Which real token the launcher is handed in a deployment. It assumes a token already exists,
   so it inherits the open OIDC broker decision from ADR-0016.
7. Whether to give `web-client` a linter; CI's lint step currently skips because no script
   exists there.

## Immediate next action

CI is green end to end at `5345db9`, which is the first run where the netns tunnel capture
actually executed and passed. Phase 1's remaining AC2 work is now specific: the capture suite
covers the tunnel and DoH path, but not QUIC, not IPv6 (the tcpdump filter is `ip`), and not
browser-level WebRTC inside the namespace. Those need their own capture cases, and the
independent security review still has to be re-run over R2-R8, ADR-0019, and this test-rule
change. Reconnect with backoff remains hardening.

Phase 2 step 2 is implemented, fake-client tested, linted, and smoke-tested; no cluster was
touched. Step 3 (session image) needs Docker or a Linux host, neither of which exists here, and
step 4 (streaming adapter) depends on that image; step 5 adds Terraform and RBAC (plan only).
Step 6 stays blocked until llama.cpp and a GGUF model are approved. Step 8's launcher is built,
locally verified end to end, and gated in CI.

Nothing may be described as hosted or usable until the image, streaming adapter, Terraform,
and staging deploy run against a user-provided cluster, domain, IdP, and credentials.

GitHub CI is now the observable referee, and after three fixes **run 41508832667 (`5345db9`)
is green on all six jobs** — hygiene, extension, web-client, companion, session, and the netns
leak capture, with `go test -race` active:

- **leak** failed at `Network-namespace WireGuard capture` with `FAIL: ICMP appeared on the
  wire`, after `Build tunnel test binary` (unsudoed) and `Ensure capture tools` both passed.
  Two separate bugs: the cold-build-as-root theory was wrong as a cause, and my annotation
  pipeline never ran because the runner executes `run:` with `bash -e`, so the failing `sudo`
  line aborted the step before `status=$?`. Fixed with `|| status=$?` and a bounded annotation
  set. The captured packets, published as CI notices on every run, are
  `IP 127.0.0.1 > 127.0.0.1: ICMP 127.0.0.1 udp port 38741 unreachable` — the kernel's reply to
  our own UDP send to a peer the tests stop. The rule is now loopback-scoped instead of an ICMP
  ban: any non-`127.0.0.1` address, port 53, any TCP, any other protocol still fails.
- **web-client** failed at typecheck because `@types/node` was hoisted from the repo-root
  `node_modules` locally and never declared in `web-client/package.json`. Declared it, and
  reproduced CI's isolation (a directory with no parent `node_modules`): typecheck clean, 45
  tests, build succeeds.

AC2 is now **partially** proven: the tunnel and DoH path are captured in a real Linux namespace
in CI. The QUIC, IPv6, and browser-level WebRTC cases are not covered — the tcpdump filter is
`ip`, so IPv6 is never captured, and this suite runs no browser. Job logs still need a sign-in;
annotations do not.

All of it is pushed: the user asked to run the leak job on GitHub, which required pushing, and
`d726ee9..5345db9` went to `main` as fast-forwards. Future pushes still need approval.
