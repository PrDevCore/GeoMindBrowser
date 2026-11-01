# Workflow

- Work in small steps: one concern per change, one logical commit.
- Before each phase, write docs/plans/phase-N.md (files, interfaces, risks, test plan,
  acceptance criteria) and stop for my approval before implementing.
- Keep state in /project-state/: brief.md, active-context.md, progress.md, decisions.md.
  Read them at the start of every session; update them at the end. Create them if missing.
- Implement only the step I name. Do not start the next step, add unrequested features, or
  refactor unrelated code.
- If blocked, uncertain, or a step conflicts with a hard constraint in AGENTS.md: stop and
  ask. Do not guess and continue.
- Prefer the smallest change that meets the acceptance criteria.
