# Session Decision Log

Durable architecture decisions live in [docs/decisions.md](../docs/decisions.md). This file
records session-level choices and their reasons.

## 2026-10-30

- Persisted AGENTS.md and the six rule files verbatim so every future session reads the same
  constraints. The workspace was empty.
- Chose Go for the companion and llama.cpp for inference after asking the user (ADR-0001,
  ADR-0002).
- Chose wireguard-go with netstack over a TUN device: no root, no OS route to leak through.
  Audit status still to confirm before Step 4.
- Chose a locally generated PAC with no DIRECT fallback for matched hosts, validated before
  install. An invalid PAC makes Chrome go DIRECT, so validation is a release blocker.
- Chose native messaging as the only control plane; rejected a loopback HTTP control port.
- Chose to disable AI on routed tabs entirely in Phase 1, satisfying "excluded by default".
- Deferred an OS firewall kill switch; the proxy gate covers browser traffic only.
- Accepted the timezone gap for Phase 1 with plan approval (ADR-0006).
- Created no infrastructure directory: Phase 1 tests use a local WireGuard peer (ADR-0007).
- Installed Go 1.27.3 from the official zip after verifying its SHA-256, into
  `C:\Users\USER\tools\go` (user-local, no admin). Installed golangci-lint 2.15.0 and the
  extension dev dependencies (TypeScript, ESLint, Vitest).
- PAC design: a JSON rule table plus one fixed interpreter, rather than per-rule generated
  code. One golden PAC file must match both generators byte for byte.
- Unknown exits compile to block rather than failing the whole policy load, so one bad rule
  cannot silently disable routing for every other rule.
- The PAC strips one trailing dot and lowercases the host; without this, `a.example.com.`
  could miss its rule and go DIRECT.
- Rejected IP literals and `defaultAction: "block"` in Phase 1 to keep matching and PAC
  semantics unambiguous (ADR-0008).

## 2026-10-31

- Closed review finding R1 as an explicit Phase 1 scope decision (ADR-0015): the OS user
  boundary is trusted, so a hostile process running as the same user is out of scope for
  routing integrity and confidentiality. Fail-closed still covers tunnel, proxy, companion,
  policy-sync, browser restart, and service-worker failures.
- Why: PAC cannot authenticate a SOCKS5 peer, the companion exits with Chrome, and a
  persistent daemon would narrow the window without closing it. Same-user code can already
  read the profile, keys, and AI context.
- This is a scope reduction, not a fix. R2 (proxy drift) remains a Phase 1 blocker; reconnect
  with backoff, drift detection, and privacy-error surfacing are follow-up hardening.
- Updated the threat model (E8, T1, trust boundaries, failure table, residual risk 8, open
  questions, checklist), the plan (non-goals, risks, invariant map, open questions), and the
  state files. User-facing claims must state the trusted-OS-user boundary.

## 2026-11-01

- Approved Phase 2 (Mode B) and its stack (ADR-0016): Kasm Workspaces + managed Kubernetes, a
  Go control plane, Terraform plan-only, and OIDC. Started step 1 only: the stdlib-only
  `session/` module with the HTTP API, lifecycle state machine, caps, rate limits, and fake
  runtime.
- Why: this host has no container or cloud runtime, and repo rules require an approved phase
  plan before implementation. Local tests can prove the control plane, not a hosted session.
- Still open: the OIDC provider, the staging target, and the `docs/threat-model.md` change that
  moves Mode B from design-level Phase 3+ to an active phase (requires approval). No hosted or
  usable Mode B claim is allowed until steps 2-7 run.

## 2026-11-01 (step 2)

- Started and finished Phase 2 step 2 at the user's request: the Kubernetes runtime in
  `session/internal/kube` with client-go 0.37.2 (ADR-0017). Approved dependency: it was
  required by the step the user asked for, and it is the only new network dependency.
- Chose deterministic hashed object names (`gms-` + SHA-256 prefix) over embedding the
  session ID, and a fail-closed delete order (Pod, wait, NetworkPolicy, Secret) over deleting
  everything at once. The NetworkPolicy deliberately has no DNS egress; routed names resolve
  at the exit, preserving the Phase 1 DNS invariant.
- Added `session.Options.CreateTimeout` because the 20s per-call timeout would have cut off a
  legitimate 2m readiness wait; the HTTP write deadline follows it.
- Verified with the fake clientset and manifest assertions only. No cluster, kubectl, or
  Docker exists on this host, so nothing was applied, and `go test -race` still has not run
  locally. CI has never executed.
- Next: step 3 (session image) is blocked on Docker/Linux; step 5 (Terraform + RBAC) and
  step 4 (streaming adapter) follow. Phase 1 AC2, R4, baseline commit, and review re-run
  remain open.

## 2026-11-01 (R4: timezone and geolocation)

- Decided ADR-0019 at the user's "proceed with the recommended": geolocation is blocked on
  every routed origin via `contentSettings.location` (monotonic blocks, new
  `contentSettings` permission), and timezone/`Intl`/`navigator.language` parity is accepted
  as out of Phase 1 scope because only `chrome.debugger` could override it. Mode B is the
  path to real parity.
- Threat-model wording for R4 (T16, section 8.1, open question) was updated on the same day
  with the user's approval.

## 2026-11-01 (license)

- Added the Apache-2.0 license at the user's request (ADR-0018): `LICENSE` (verbatim,
  placeholder appendix), `NOTICE` with the copyright line, README license section, and
  `"license": "Apache-2.0"` in the root and extension `package.json`. Go modules are covered
  by the root `LICENSE`; no dependency is relicensed.
- Copyright holder is "Pr Dev Core Tech" (user-specified 2026-11-01, superseding the initial
  "The GeoMind Authors" placeholder). Apache-2.0 does not grant trademark rights.

## 2026-11-02 (Mode B web client)

- Decided ADR-0020 from the user's "build browser as a web app": the web app is the Mode B
  **launcher**, not a pixel client. KasmVNC keeps owning DTLS/codecs/input; the launcher only
  speaks `/v1`, opens the provider view as an `https`-only noreferrer link, and guarantees
  teardown. Stack is TypeScript + Vite + vitest with no framework, chosen by the user; no
  linter is configured for `web-client` yet.
- The user approved the remaining plan items with "fix all" after they were listed: new-tab
  presentation over an iframe (which would need a `frame-ancestors` decision for the stream
  origin), the launcher-not-pixel-client scope, and the `docs/threat-model.md` section 6 edit
  recording the launcher host as an operator-visibility surface and `streamUrl` as
  bearer-equivalent.
- The launcher stays a local proof. Nothing is hosted: the identity provider (step 4) and the
  streaming provider (step 5) do not exist, so `web-client` currently runs only against
  `sessiond` in fake-runtime mode.
