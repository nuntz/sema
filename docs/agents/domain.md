# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root: the glossary. It defines domain terms (Feed, Connector, Tag, Item, …) and the synonyms to avoid.
- **`docs/adr/`**: read ADRs that touch the area you're about to work in.
- **`AGENTS.md`**: older decisions (ranking, read state, the Dock, Send) are recorded as prose in its project-structure section. Read the ones relevant to your area; new decisions go in `docs/adr/`, not `AGENTS.md`.

If `docs/adr/` doesn't exist yet, **proceed silently**. Don't flag its absence; don't create it upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates ADRs lazily when decisions actually get resolved.

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

If your output contradicts an existing ADR or a decision recorded in `AGENTS.md`, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders), but worth reopening because…_
