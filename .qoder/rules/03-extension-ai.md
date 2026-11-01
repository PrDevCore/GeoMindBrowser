# Extension and AI invariants

- No passive page reads, no background scraping. Reads happen only on user invocation.
- Page content goes in a clearly delimited data channel, separate from system and user
  instructions. It may inform a plan but never issue instructions.
- Cloud model calls are opt-in and run local PII redaction first.
- Agent mode runs only inside Mode B containers. Purchases, logins, and form submissions go
  through an approval gate with a plain-language preview.
- The model sits behind a swappable interface.
- Every feature that ingests page content ships with a prompt-injection test fixture.
