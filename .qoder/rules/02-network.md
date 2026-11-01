# Networking invariants

- Tunnel down means geo-routed traffic stops. Every change here needs a test that kills the
  tunnel mid-request and asserts zero packets leave from the real IP.
- DNS resolves at the exit node, never locally, for routed tabs (DNS-over-HTTPS).
- WebRTC must not expose local or real public candidates on routed tabs.
- Timezone, locale, Accept-Language, and geolocation match the exit country consistently.
- Normalize fingerprints; do not randomize (noise is itself a signal).
- Userspace WireGuard and audited libraries only. Never modify system network settings
  without asking.
- State which invariant each change affects and which test covers it.
