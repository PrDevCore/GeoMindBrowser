# GeoMind Browser

Privacy-first AI browser with geo-routed access.

Status: **Phase 1 in progress.** Steps 1-5 are implemented and the local Go, TypeScript, and
Playwright suites pass: policy core and PAC generator, companion native-messaging IPC, the
loopback SOCKS5 proxy with its fail-closed health gate and tunnel supervisor, the userspace
WireGuard tunnel (wireguard-go + netstack) with DoH at the exit, and extension routing with
live status, policy hot reload, proxy-drift repair, and permission-gated locale headers. The
Linux DNS/WebRTC packet capture (AC2) and the offline AI sidebar (Step 6) are still open. The
independent security review's findings R2, R3, and R5-R8 are fixed with tests; R4 is decided
(ADR-0019): geolocation is blocked on routed origins, and timezone/language parity is
documented as out of Phase 1 scope because no MV3 API can change it without a visible
debugger attach. R1 (a same-user process takes over the stale loopback port) is scoped out
for Phase 1 under ADR-0015.

## What Phase 1 builds

- A Chromium MV3 extension with per-site routing rules and an offline AI side panel.
- A Go companion app that owns a userspace WireGuard tunnel (wireguard-go + netstack), a
  loopback-only SOCKS5 proxy with a fail-closed gate, DoH at the exit, and a llama.cpp
  sidecar.
- A Linux network-namespace leak suite for DNS and WebRTC (the capture is still pending) plus
  browser tests that a dropped tunnel stops routed traffic.

Mode B (remote browser sessions), cloud AI, agent actions, and real exit infrastructure are
later phases.

## Phase 2 (Mode B) status

`docs/plans/phase-2.md` is approved with Kasm Workspaces + managed Kubernetes as the target.
Steps 1 and 2 are implemented locally. The `session/` Go module exposes the session API,
lifecycle state machine, ownership checks, TTL/idle expiry, concurrency caps, rate limits, and
idempotent teardown. Its default runtime is an in-memory fake; a client-go runtime builds one
Secret, NetworkPolicy, and Pod per session (non-root, read-only root FS, no DNS egress,
fail-closed teardown) and is verified with the fake clientset only. No object has been applied
to a cluster, and there is no Chromium image, streaming provider, OIDC, or Terraform yet. The
fake stream URLs are placeholders, and nothing is hosted.

Step 8, approved and implemented 2026-11-02 (ADR-0020), is `web-client/`: a framework-free
TypeScript + Vite launcher for Mode B. It authenticates to `/v1`, starts one session per
click, follows the state machine, opens the provider's stream view in a new tab through an
HTTPS-only noreferrer link, and guarantees teardown on stop, expiry, error, and tab close. It
holds the bearer token only for the page lifetime and stores nothing. It is a launcher, not a
pixel client: KasmVNC owns DTLS and codecs, and the real stream view still needs steps 3 to 6.

Run it locally against the fake control plane (a ≥16-character dev token is required):

```sh
(cd session && GEOMIND_SESSION_RUNTIME=fake \
  GEOMIND_SESSION_DEV_TOKEN=local-dev-token-00000000 \
  go run ./cmd/sessiond) &
npm install                         # repo root, for the Playwright end-to-end
(cd web-client && npm install && npm run dev) &   # proxies /v1 and /healthz to 127.0.0.1:8080
(cd web-client && npm test)         # from web-client/
npm run test:web                    # from the repo root: end-to-end, headful, needs Go
```

`npm run test:web` builds `sessiond`, spawns it in fake mode, builds and serves the bundle
with `vite preview`, and drives the whole lifecycle in Chromium; it is not part of CI.

## workspace-demo/

`workspace-demo/` is a React + Express mock of a team workspace - browser panel, sprint board,
shared documents, team chat and a VPN console - imported 2026-11-04 under
[ADR-0021](docs/decisions.md). It is a design reference, not a client: nothing routes traffic,
the VPN console and its metrics are simulated, there is no AI feature, and no CI job builds it.
Run `npm install && npm run dev` inside that directory; `npm run typecheck`, `npm test` and
`npm run build` are the checks it is expected to keep passing.

## Hard rules

Read [AGENTS.md](AGENTS.md) first. The short version:

- Fail closed. A matched host never falls back to the real IP on tunnel, proxy, or process
  failure. Phase 1 trusts the OS user boundary; a hostile same-user process is out of scope
  (ADR-0015).
- No telemetry, analytics, or persistent browsing logs.
- The AI reads a page only when the user invokes it; geo-routed tabs are excluded.
- Page text is data, never instructions.
- No custom crypto, no secrets in the repo, no abuse-oriented features.

## Layout

| Path | Contents |
| --- | --- |
| `extension/` | MV3 extension: service worker, side panel, policy client |
| `companion/` | Go native companion: policy, SOCKS5 proxy, WireGuard tunnel, AI sidecar |
| `workspace-demo/` | React + Express workspace mock: simulated VPN console, design reference only |
| `shared/` | Policy and IPC schemas, cross-language test fixtures |
| `tests/` | Linux netns leak suite, prompt-injection fixtures, offline AI test |
| `docs/` | [Threat model](docs/threat-model.md), [decisions](docs/decisions.md), [Phase 1 plan](docs/plans/phase-1.md) |
| `project-state/` | Session brief, active context, progress, decision log |
| `.qoder/rules/` | Workflow, networking, extension/AI, infra, testing, security-review rules |

## Prerequisites

- Go 1.27+ (companion and Go tests). Step 1 used a user-local Go 1.27.3 install.
- Node 24 (extension tooling). Installed, with `extension/package-lock.json` committed.
- Playwright 1.64.1 with Chromium 156 for the browser routing test: run `npm install` and
  `npx playwright install chromium` at the repo root. The suite runs headful, so it needs a
  desktop session or xvfb.
- llama.cpp `llama-server` plus a GGUF model, path and hash via env. Not installed yet.
- Linux with root for the network-namespace leak suite. The dev machine is Windows, so that
  suite runs in CI or a Linux VM.

Toolchain and dependency installation needs approval; see the dependency table in the
Phase 1 plan.

## Why there is no local control port

The extension talks to the companion over Chrome native messaging. The companion does not
listen on TCP for control; its only listener is the loopback SOCKS5 data-plane proxy, which
refuses traffic whenever the tunnel is unhealthy. The AI server, when added, binds to
loopback with a per-launch key.

## Contributing

Work one approved step at a time. Every step needs tests, a linter run with real output, and
updated docs and state. Stop and ask when a change conflicts with a hard rule.

## License

Apache License 2.0; see [LICENSE](LICENSE) and [NOTICE](NOTICE). Third-party dependencies
retain their own licenses.
