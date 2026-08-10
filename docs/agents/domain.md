# Domain Docs

How engineering skills should consume this repository's domain documentation.

## Before exploring, read these

- `CONTEXT.md` at the repository root.
- Relevant decisions under `docs/adr/`.

If these paths do not exist, proceed silently. Do not create placeholders. The `domain-modeling` skill, reached through `grill-with-docs`, creates them lazily when terminology or durable architectural decisions are resolved.

## File structure

This is a single-context repository:

```text
/
├── CONTEXT.md
├── docs/
│   └── adr/
└── src/
```

## Use the glossary's vocabulary

When output names a domain concept—in an issue title, proposal, hypothesis, test, API, or code identifier—use the canonical term from `CONTEXT.md`. If a required concept is missing, reconsider whether new language is needed or record the gap for `domain-modeling`.

## Flag ADR conflicts

If proposed work conflicts with an existing ADR, surface the conflict explicitly instead of silently overriding the decision.
