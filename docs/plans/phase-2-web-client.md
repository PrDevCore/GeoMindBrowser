# Phase 2 addendum: Mode B web client (`web-client/`)

Status: **draft 2026-11-02. Not approved, not implemented.** Implementation starts only
after this plan and the approval items in section 8 are accepted.

## 1. What is being built, and what is not

The user asked for "the browser as a web app". The only architecture in which a web page
*is* the browser is Mode B: the remote container renders the page, and the user's browser
receives pixels and forwards input. The real IP never contacts a target site, and the
fail-closed rule maps cleanly: stream down, view stops, no local fallback.

This addendum builds the **launcher and lifecycle surface** for that: one small web app that
authenticates to the existing control plane, starts one session per explicit click, follows
its state, opens the provider's stream view, and guarantees teardown.

It explicitly does **not** build:

- A pixel/input client. KasmVNC already ships one and owns DTLS/TLS and the codec. Writing
  our own VNC/WebRTC decoder would be custom codec code and is rejected (AGENTS.md 7).
- Any Mode A capability. A page cannot hold a tunnel, set a proxy, or stop WebRTC and DNS
  leaks; the extension plus companion stay the only Mode A surface.
- Any hosted claim. There is no Docker, no cluster, no domain, and no IdP on this host, so
  `streamUrl` here is produced by the fake provider in `session/internal/session/fake.go`.

## 2. Existing contract this consumes

From [session/internal/api/api.go:73-83](../../session/internal/api/api.go) and
[docs/plans/phase-2.md](phase-2.md) section 6:

- `POST /v1/sessions` `{"region": string}` -> `201` `sessionView`
- `GET /v1/sessions/{id}` -> `200` `sessionView`
- `DELETE /v1/sessions/{id}` -> `204`, idempotent
- `GET /healthz` -> `200` (unauthenticated)
- `sessionView` = `{ id, state, region, streamUrl?, expiresAt }`; `streamUrl` is present only
  in `ready`/`active` ([api.go:256-267](../../session/internal/api/api.go)).
- Bearer token, single `Authorization` header, IP and per-user rate limits, `Cache-Control:
  no-store`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` on control-plane
  responses ([api.go:233-242](../../session/internal/api/api.go)).
- Errors are `{ error, message }` with `invalid_request`, `not_found`, `capacity`,
  `rate_limited`, `unauthenticated`, `unavailable`, `internal`.

`X-Frame-Options: DENY` on the control plane does not by itself decide embedding: the stream
origin is separate and would need its own `frame-ancestors` allow-list. See approval item (b).

## 3. Files

```
web-client/
  package.json          name, license Apache-2.0, author Pr Dev Core Tech, dev-only deps
  tsconfig.json         strict, exactOptionalPropertyTypes, same shape as extension/
  vite.config.ts        dev proxy /v1 -> http://127.0.0.1:8080; build to dist/
  index.html            CSP without unsafe-inline; referrer no-referrer
  src/main.ts           wiring only
  src/api.ts            typed control-plane client; no retry loops, no token storage
  src/session.ts        client-side mirror of the state machine; terminal states are terminal
  src/stream.ts         open the provider view, watch for loss, drive teardown
  src/teardown.ts       close/beacon path: DELETE on tab close, idle, error, or click
  src/ui.ts             plain-language status, disabled affordances while provisioning
  test/*.test.ts        vitest units with fetch stubbed
```

No product code outside `web-client/` changes, except the CI job and the docs in section 7.
`extension/` and `companion/` are untouched, so every Phase 1 invariant keeps its current
coverage.

## 4. Interfaces

```ts
type SessionView = {
  id: string;
  state: "requested" | "provisioning" | "ready" | "active" | "terminating" | "gone" | "failed";
  region: string;
  streamUrl?: string;
  expiresAt: string;
};

interface ControlPlane {
  health(): Promise<boolean>;
  create(region: string): Promise<SessionView>;
  get(id: string): Promise<SessionView>;
  remove(id: string): Promise<void>;
}
```

The token enters once, from a `#`-free in-page field, lives in a module-scoped variable, and
is never written to `localStorage`, `sessionStorage`, IndexedDB, cookies, the URL, or the
console. No analytics, no beacons other than the teardown `DELETE`, no third-party origins
in the page. This is hard constraint 2 applied to a new surface, so it is enforced by tests
rather than by intent.

## 5. Behaviour and failure paths

- One click, one session. The Start control is disabled from the moment `create()` is called
  until the session reaches a terminal state, so a double click cannot burn two Pods.
- Poll `GET` every 2s while `requested`/`provisioning`, with a client deadline matching the
  control plane's provisioning budget; on deadline, call `DELETE` and report "stopped".
- `ready`/`active` reveals exactly one "Open session view" affordance pointing at
  `streamUrl`. `expiresAt` is displayed as a countdown; at expiry the UI goes to "ended" and
  stops polling.
- **Fail closed on transport loss.** If polling errors twice, returns `401`/`404`, or reports
  `failed`/`terminating`, the client calls `DELETE` once and shows a stopped state. It never
  reopens the stream locally and never suggests browsing without the session.
- `429`/`capacity` shows the server `Retry-After` and does not auto-retry.
- Page unload fires the teardown `DELETE` (best-effort fetch with `keepalive`); a missed
  beacon is still bounded by the control plane's TTL and idle reaper.
- `Stop now` is always available and is the same `DELETE` path.

## 6. Risks

- **Nothing end-to-end is provable here.** The stream view, egress path, exit IP, and Pod
  isolation all require step 3 (image) and step 6 (staging). Acceptance criteria in
  `docs/plans/phase-2.md` section 10 stay open.
- **New leak surface in the launcher itself.** `streamUrl` is a bearer-equivalent for the
  session view: the page must not put it in `href` history text, the console, or any anchor
  without `rel="noreferrer"`, and must not send a referrer to the stream host. Tests assert
  the anchor attributes.
- **Iframe embedding needs a policy decision.** Embedding the provider view in-page requires
  `frame-ancestors` on the stream origin and a clickjacking review; the control plane already
  sends `X-Frame-Options: DENY` for its own responses. New-tab is the lower-risk default.
- **Operator visibility.** Whoever hosts this page sees the mapping user -> session -> time.
  That is already true of the control plane, but it must be written into the threat model
  before hosting (approval item c).
- **Phase 1 debt.** AC2 (Linux netns capture) is still failing CI and unproven, and the
  browser suite has no CI coverage. This addendum does not touch either, and does not change
  the recommendation that AC2 closes first.

## 7. Tests and CI

Unit (vitest, `fetch` stubbed): state transitions including `failed` from every state;
teardown exactly once on error, on expiry, on stop click, and on unload; Start disabled
during provisioning; `429` maps to the `Retry-After` message with no retry; `401`/`404`
stops polling; no-storage guard (stubbing `localStorage`/`sessionStorage`/`indexedDB` throws
if touched); token never appears in a URL or an anchor `href`; `streamUrl` anchors carry
`rel="noreferrer"`.

Local run (no cluster): `GEOMIND_SESSION_RUNTIME=fake` plus a dev token
(`session/internal/auth.DevVerifier`) starts `sessiond` on `127.0.0.1:8080`, `vite` proxies
`/v1`, and the whole lifecycle is clickable against the fake provider. This verifies the
control path only.

CI: one new `web-client` job mirroring `extension` (checkout, `setup-node` with
`cache-dependency-path: web-client/package-lock.json`, `npm ci --ignore-scripts`, typecheck,
lint, test). No change to the failing leak job.

## 8. Approval items before any of this is written

a. **New dev-only dependencies** in `web-client/`: `vite`, `typescript`, `vitest`. No
   network or crypto code; version pinning and lockfile committed. AGENTS.md requires your
   sign-off for new dependencies.
b. **Stream presentation**: new tab (recommended for this slice) or embedded iframe, which
   additionally requires a `frame-ancestors` decision for the stream origin.
c. **`docs/threat-model.md` edit** to record the hosted launcher as an operator-visibility
   surface and the `streamUrl` bearer property. Required approval per AGENTS.md.
d. **Scope confirmation**: this is the launcher, not a pixel client, and it makes no hosted
   Mode B claim until steps 3, 5, and 6 exist.
