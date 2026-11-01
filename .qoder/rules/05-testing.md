# Testing

Required suites: DNS and WebRTC leak tests, kill switch (kill tunnel mid-load), routing-rule
matching, locale/timezone consistency, session teardown leaves nothing behind, prompt
injection fixtures against the agent. Run the full suite and linter after every meaningful
change and paste real output. Only touch product code to make it testable, and flag it.
