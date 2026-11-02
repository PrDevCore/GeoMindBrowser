# GeoMind Threat Model

Status: initial draft, 2026-10-30; revised 2026-10-31 for the R1 scope decision (ADR-0015,
approved by user instruction). Scope: Phase 1 (Mode A) in detail; Mode B at design
level. Per AGENTS.md, further changes to this file require human approval.

Companion decisions: Go, wireguard-go, llama.cpp (see docs/decisions.md).

## 1. System summary

Phase 1 is a Chromium MV3 extension plus a native companion process on the user's machine.
The extension applies per-site routing rules and the privacy policy; the companion owns the
userspace WireGuard tunnel, a loopback SOCKS5 proxy, DNS resolution, and the local LLM
sidecar. The AI sidebar runs entirely offline.

```
tab (Chromium)
  |  PAC: matched host -> socks5 127.0.0.1:PORT (no DIRECT fallback)
  |        other hosts -> DIRECT
  v
companion SOCKS5 (127.0.0.1, ephemeral port)
  |  gate: tunnel healthy? else refuse
  v
wireguard-go netstack (userspace, no TUN privileges)
  |  DNS via DoH at exit, inside the tunnel
  v
exit node  --->  Internet
```

Control plane: extension <-> companion over Chrome native messaging only. No TCP control
port, no localhost HTTP control surface. The llama.cpp server binds to 127.0.0.1 on a random
port with a per-launch API key and is reachable only by the companion process.

## 2. Assets

| ID  | Asset | Why it matters | Where it lives |
| --- | --- | --- | --- |
| A1  | Real public IP and coarse geolocation | Re-identifies the user; defeats the product's purpose | OS network stack |
| A2  | DNS query stream | Reveals destinations even under TLS | OS resolver, browser resolver, proxy |
| A3  | Destination metadata (SNI, IP, timing, volume) | Traffic analysis, censorship, correlation | Network path, exit node |
| A4  | Page content, cookies, session tokens, form data | Account takeover, data loss | Browser profile |
| A5  | AI inputs and outputs | Page text and selections may contain PII | Extension side panel, companion, model context |
| A6  | Device resources: files, clipboard, camera, microphone | Local compromise | OS |
| A7  | Integrity of consequential actions (purchases, logins, form submits) | Financial and account harm | Phase 2+ agent |
| A8  | WireGuard keys, exit credentials, session tokens | Tunnel impersonation, traffic redirection | Memory only; never on disk |
| A9  | Exit fleet metadata (location, ownership, liveness) | Availability and truthfulness of geo claims | Config, exit nodes |
| A10 | User trust in the privacy claim | Product viability | Documentation and behavior |

## 3. Adversaries

| ID  | Adversary | Capabilities | Goals |
| --- | --- | --- | --- |
| E1  | ISP / local network | Passive capture, DNS interception, blocking, on-path MITM with certs | Identify user, log destinations, block |
| E2  | Exit operator (malicious or compromised) | Sees all traffic entering the exit; can log, drop, delay, tamper; sees SNI and DNS if it terminates them | Correlate user, inject content, deny service |
| E3  | Target site | JS, fingerprinting, WebRTC, TLS metadata, account linking | Identify user, link sessions, block region evasion |
| E4  | Malicious page code | Arbitrary JS in a tab; can attempt prompt injection into the AI | Exfiltrate data, manipulate AI output, fingerprint |
| E5  | Malicious or compromised extension | Tab access, network APIs, storage | Read pages, exfiltrate, change routing |
| E6  | Cloud AI provider (opt-in, Phase 3+) | Sees prompts sent to it | Train on or retain user content |
| E7  | Compromised dependency or CI | Code execution in build or runtime | Backdoor, key theft |
| E8  | Other local user or process | Same-host access | Read profile, keys, AI context |

E8 is retained for completeness, but it is out of scope for Phase 1 routing integrity and
confidentiality: the OS user boundary is trusted. See ADR-0015 and section 8, item 8.

Out of scope for Phase 1: a compromised OS or kernel, global passive traffic correlation
(needs an adversary seeing both ends), legal coercion of an exit operator beyond the logging
guarantees, and a hostile process running with the user's OS privileges (E8; see section
8.8). The OS user boundary is trusted in Phase 1.

## 4. Trust boundaries

1. **Page <-> extension.** Page content is hostile. It never shares a channel with
   instructions; it is passed as delimited data (AGENTS.md constraint 4).
2. **Extension <-> companion.** Native messaging, caller identity is the browser, messages
   are typed and size-bounded.
3. **Companion <-> exit.** WireGuard, authenticated and encrypted; exit is trusted for
   availability and for not logging, and untrusted for content confidentiality only to the
   extent TLS protects it.
4. **Companion <-> local model.** Loopback only, per-launch API key, no outbound network.
5. **Local machine <-> Internet for non-routed traffic.** DIRECT, outside the product's
   protection. The extension only claims protection for matched hosts.
6. **OS user <-> GeoMind processes.** The user's OS account is trusted. A hostile process
   running as the same user is out of scope for Phase 1 routing and confidentiality (E8,
   ADR-0015).

## 5. Threats and mitigations, Mode A

| ID | Threat | Mitigation | Residual risk | Test |
| --- | --- | --- | --- | --- |
| T1 | Matched host falls back to DIRECT when the tunnel or proxy is down | PAC returns only the SOCKS proxy for matched hosts, never a DIRECT fallback; proxy refuses connections unless the tunnel is healthy; supervisor marks unhealthy on handshake staleness | A Chromium bug or a policy-sync failure could leave a stale PAC; a same-user process that binds the dead proxy port is outside Phase 1 (E8, section 8, item 8). PAC is regenerated locally and validated before install | kill switch: kill tunnel mid-request (tests/leak) |
| T2 | DNS leaks to the local resolver | SOCKS5 receives the hostname and resolves inside the tunnel via DoH at the exit; no local resolver call in the proxy path; DNS mode is "remote only" | Chromium may pre-resolve for some features; covered by the capture test | DNS leak: local resolver logs must stay empty |
| T3 | WebRTC exposes host or public candidates | `webRTCIPHandlingPolicy = disable_non_proxied_udp` (global); no STUN through the proxy; no UDP egress from netstack except the tunnel | Breaks WebRTC on all tabs, including non-routed ones, in Phase 1 | WebRTC fixture with a capturing STUN server |
| T4 | QUIC/HTTP3 bypasses the proxy | QUIC is disabled while a proxy is configured; test asserts no UDP/443 to origin IPs | Chromium version differences | capture test on a QUIC-capable origin |
| T5 | Tab restore or browser restart reconnects using the real IP | Routing state is derived from policy, not from per-tab memory; PAC is installed before tabs restore; proxy gate is independent of tab lifecycle | Restore ordering differs across Chrome versions | restart-and-restore test |
| T6 | IPv6 bypass | All matched traffic goes to the proxy regardless of family; proxy dials through the tunnel only; no direct IPv6 socket | Non-matched traffic is DIRECT by design | dual-stack capture test |
| T7 | Extension compromise reads pages | No host permissions by default; page access via `activeTab` only after a user gesture; optional host permission only for sites with header rules; minimal permissions | A compromised extension still sees pages the user invokes the AI on | manifest review |
| T8 | Policy/exit metadata reveals sites | Policy is stored locally, never transmitted; no per-site logs; AI context never includes the policy | Local profile is readable by other local users unless disk encryption is used (E8; out of scope for Phase 1, ADR-0015) | code review, no-network test |
| T9 | Malicious page injects instructions into the AI | Page text is delimited data with an explicit untrusted marker; system prompt states data cannot issue instructions; Phase 1 AI has no tools, no actions, no network; sidebar renders text, not remote markdown/images | Model may still be swayed within its answer text | injection fixtures |
| T10 | Page exfiltrates via AI-rendered content | Sidebar renders plain text; no remote images, no auto-loaded links | User can click a link manually | UI review, fixture with image URL |
| T11 | Traffic analysis by ISP | TLS to exit; no plaintext DNS | ISP still sees connection to an exit IP and volume/timing | design |
| T12 | Exit operator reads content | TLS end-to-end to the site; exit sees SNI, IPs, timing | Exit can log destinations and inject at the TLS layer only with a trusted cert | design, operator policy |
| T13 | Companion or model leaks context to disk | No conversation persistence; llama.cpp server without prompt logging; model path read-only; crash dumps disabled | OS swap and pagefile can hold memory | review, offline test |
| T14 | Secrets in repo or logs | Env vars only, `.env.example` with empty values, CI secret hygiene, keys generated in memory | Test keys could be pasted by mistake | CI hygiene job |
| T15 | Local model runtime calls home | llama-server launched with no outbound route (loopback bind); offline test runs with no default route | Upstream build flags could change | no-network test |
| T16 | Fingerprint mismatch (locale/timezone/geolocation) | Accept-Language and locale set per matched region; geolocation blocked on routed origins; timezone and `Intl` overrides are a known gap (see section 8, ADR-0019) | A determined site can still detect a timezone or language mismatch | locale table test, geolocation unit and Playwright tests, documented gap |
| T17 | Session/tab interference | Phase 1 has one profile and one proxy; tabs share the profile by design | No isolation between tabs in Phase 1 | documented |

Note on T1: the fail-closed property depends on the PAC never offering a fallback for a
matched host. The PAC generator must be tested directly; the kill-switch test is the
end-to-end proof.

## 6. Mode B (design level, Phase 3+)

Mode B runs Chromium in an ephemeral container at the exit and streams pixels/input to the
user. This changes which adversary sees what:

- The site sees the exit IP, not the user (protects A1 from E3).
- The **exit operator sees page content in plaintext** because TLS terminates inside the
  container. Mode B is therefore weaker than Mode A for content confidentiality (A4, A5)
  and stronger for IP isolation. This must be stated in user-facing docs; it is the main
  reason to keep both modes.
- The stream provider sees rendered pixels and input events.
- The **launcher is hosted infrastructure** (Phase 2 step 8, `web-client/`). Whoever serves
  it sees that a user started a session, at what time, from what IP, and for what region,
  and serves the page that receives the bearer token. The launcher itself stores no browsing
  data: it writes nothing to storage or cookies, keeps the token in a module closure only for
  the page lifetime, and never places it in a URL (enforced by `web-client/test/privacy.test.ts`).
  Self-hosting the launcher is the mitigation for users who will not accept that visibility.
- The **stream URL is bearer-equivalent**. Anyone who can read it can present the session
  view, so it is treated as a secret: HTTPS only, `referrerpolicy="no-referrer"` and
  `rel="noreferrer noopener"` on the only anchor that uses it, and never logged by the
  control plane.

| Threat | Mitigation |
| --- | --- |
| Session persistence or cross-session leakage | One ephemeral pod per session, no persistent disk, full teardown, no shared volumes, unique credentials per session |
| Session escapes container and reaches other sessions | Network policy denies pod-to-pod, seccomp, non-root, read-only root FS, egress allowlist |
| Clipboard/download exfiltration | Clipboard sync off by default; downloads gated by explicit user action |
| Stream interception | DTLS-SRTP via Kasm/Guacamole; TLS to the streaming endpoint; short-lived session tokens |
| Abuse and resource exhaustion | Rate limits, per-session CPU/memory/egress caps, automatic teardown |
| Exit operator logging | RAM-only nodes, no persistent storage, published ownership, auditable images |
| Launcher host observes session metadata | Documented, self-hostable launcher; `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, no telemetry or persistent logs in the launcher; page content never transits it |
| Stream URL captured or forwarded | HTTPS-only validation, noreferrer anchors, no referrer leakage, revocation on teardown, TTL and idle expiry |

Residual: exit operator can read content; target site sees exit IP; stream provider sees
pixels; container escape is a high-severity but low-likelihood risk; the launcher host learns
that, when, and from where a session was started and holds the token-bearing request path.

## 7. Failure paths that must fail closed

Each of these needs an explicit test; none may end in a request going DIRECT for a matched
host.

| Failure | Required behavior |
| --- | --- |
| WireGuard handshake stale or device stopped | Proxy refuses new connections; in-flight connections fail |
| Proxy process crash | PAC still points at a dead port; connection error, no fallback. A same-user process that binds the port is out of scope (E8, section 8, item 8) |
| Extension service worker restart | PAC is a browser setting and persists; policy is re-fetched and re-validated. Same E8 caveat |
| Native messaging host missing | Routing stays on the last validated PAC; AI unavailable; no DIRECT fallback. Same E8 caveat |
| Companion exits or is killed | PAC stays installed and routed requests fail; no fallback. There is no reconnect timer in Phase 1, so the degraded state lasts until the worker wakes or the browser restarts. Same E8 caveat |
| Policy fetch returns malformed data | Keep last valid policy; surface an error; never emit an empty PAC with DIRECT for matched hosts |
| Browser restart with session restore | PAC installed before tabs restore; restore test |
| Exit DNS resolver unreachable | The request fails; no local resolver fallback |
| Laptop sleep/wake | Supervisor revalidates the tunnel before serving; stale state is unhealthy |
| PAC parse error | Chrome falls back to DIRECT for everything, so the PAC string is validated before install; empty or invalid policy blocks installation and disables routing instead of silently going direct |

The last row is the sharpest edge: an invalid PAC can silently bypass the tunnel. The
extension must validate the generated PAC and, on failure, remove/disable the proxy config
in a way that blocks matched hosts rather than allowing them. This needs a dedicated test.

These guarantees assume no process running as the same OS user binds the loopback proxy port.
That adversary (E8) is out of scope for Phase 1; see section 8, item 8 and ADR-0015.

## 8. Residual risks accepted for Phase 1

1. **Timezone and language mismatch; geolocation blocked.** Stock Chromium does not expose a
   supported per-site timezone override without the debugger API (detectable, with a visible
   debugger banner) or an OS setting (a system change that needs approval). Phase 1 matches
   Accept-Language per granted origin and blocks geolocation on routed origins, but not
   timezone or `navigator.language`/`Intl`. This conflicts with the networking invariant
   "timezone ... match the exit country"; ADR-0019 (2026-11-01) decided to accept the
   timezone/language half for Phase 1 and treat parity as a Mode B property, where the
   session container runs in the exit's timezone and locale. Routes still reach the exit
   through the proxy; only these page-visible values can disagree.
2. **No OS-level kill switch.** Only traffic that goes through the extension's PAC is
   covered. Other apps and browser features (updates, Safe Browsing, variations) still use
   the real IP. A firewall kill switch changes system network settings and needs approval.
3. **Browser background traffic.** Chromium contacts Google services and performs OCSP and
   update checks outside the extension's control. Phase 1 documents this; enterprise policy
   hardening is a later step.
4. **Fingerprint normalization is partial.** Header and locale consistency only; canvas,
   fonts, and hardware characteristics are not normalized in Phase 1.
5. **Exit operator trust.** Destination metadata and timing are visible to the exit. Mode A
   protects content via TLS, not metadata.
6. **Shared browser profile.** All tabs share cookies and storage; no per-region profile
   isolation in Phase 1.
7. **Local model quality.** Small offline models can be wrong; outputs are advisory, not
   authoritative, and Phase 1 AI takes no actions.
8. **Same-user local process (R1).** A process with the user's OS privileges can read the
   profile, kill the companion, bind its ephemeral loopback port, and impersonate the SOCKS5
   proxy. PAC cannot authenticate the peer, and the companion exits with Chrome, so Phase 1
   cannot keep the endpoint alive or prove its identity. The fail-closed guarantee therefore
   covers network, tunnel, proxy, policy-sync, and process failures, but not a hostile
   same-user process. **Why accepted:** same-user code can already read keys and the profile;
   closing this needs OS-level isolation (separate user, firewall, or an authenticated data
   plane). Planned reconnect and proxy-drift detection narrow the window but do not close it.
   Revisit before any claim of local-malware resistance.

## 9. Open questions

- Timezone: settled for Phase 1 (accepted gap, ADR-0019); revisit only if Mode B parity is
  not acceptable for a Phase 1 use case.
- Is a real OS firewall kill switch required for Phase 1 acceptance, or is proxy-level
  fail-closed sufficient?
- Which audited WireGuard implementation do we pin, and has it had an independent audit?
- Do we need per-region profiles before shipping, given shared cookies?
- Phase 1 trusts the OS user boundary (ADR-0015). Is OS-level isolation (separate user,
  firewall, or an authenticated data plane) required before Phase 2, and what must it cover?

## 10. Security review checklist

The review mode in .qoder/rules/06-security-review.md applies this document. Every change
must answer: real IP/DNS/WebRTC leakage on failure paths, page-to-agent injection, tab and
session isolation, logging and persistence, and custom crypto. Reviews must also state the
trusted-OS-user assumption and the E8 boundary (ADR-0015).
