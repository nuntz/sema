# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root: the glossary. It defines domain terms (Feed, Connector, Tag, Item, …) and the synonyms to avoid.
- **`docs/adr/`**: read ADRs that touch the area you're about to work in.

New decisions go in `docs/adr/` as numbered ADRs, created by the `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) when decisions get resolved. `AGENTS.md` keeps only a one-line guardrail that points at an ADR when its rule must hold on every change.

## File structure

Single-context repo:

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-<decision>.md
│   └── 0002-<decision>.md
├── cmd/
├── internal/
└── web/
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms listed under _Avoid_.

If the concept you need isn't in the glossary yet, that's a signal: either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`, which adds terms to `CONTEXT.md`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders), but worth reopening because…_
