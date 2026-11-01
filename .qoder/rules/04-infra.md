# Infrastructure rules

- Run `terraform plan` only. Never `apply`; I do that after review.
- Exit nodes: RAM-only, no logs, minimal attack surface, published location and ownership.
- Session containers: one ephemeral pod per session, no persistent disk, full teardown on
  close, resource and egress limits, no shared state between sessions.
- Rate limiting and abuse handling are required features.
- No secrets in code or state files; document the secrets flow instead.
