# Brief

GeoMind is a privacy-first browser with geo-routed access and a local-first AI sidebar.

- Mode A: per-site routing through a userspace WireGuard tunnel to geo exit nodes. Matched
  traffic fails closed on tunnel, proxy, or process failure; it never falls back to the real
  IP within the Phase 1 threat model, which trusts the OS user boundary (ADR-0015).
- Mode B (later): ephemeral remote Chromium sessions streamed to the user.
- AI: offline sidebar for summaries and selection Q&A, invoked explicitly, never reading
  pages in the background. Geo-routed tabs are excluded.

Phase 1 (current): MV3 extension + Go companion + llama.cpp sidecar, with automated DNS,
WebRTC, and kill-switch tests.

Governing documents: [AGENTS.md](../AGENTS.md), `.qoder/rules/`, threat model at
[docs/threat-model.md](../docs/threat-model.md), plan at
[docs/plans/phase-1.md](../docs/plans/phase-1.md).

Human approval is required before real infrastructure, Terraform apply, publishing, new
network or crypto dependencies, threat-model changes, and starting the next phase.
