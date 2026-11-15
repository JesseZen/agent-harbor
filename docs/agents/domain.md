# Domain documentation

This is a single-context repository. Its existing shared glossary is `docs/glossary.md`; retain that canonical location rather than duplicate it at the root.

Before investigating domain terminology or designing behavior, read `docs/glossary.md`, then any relevant decisions under `docs/adr/`. If ADRs do not exist, continue without scaffolding empty decisions. If this repository later adopts a glossary map, follow the map for the relevant context.

Use the glossary's terms in specifications, tickets, tests and implementation. A client, model, conversation, terminal and connection intent are distinct concepts. Record genuine terminology gaps through domain modeling rather than silently inventing synonyms.

Surface conflicts with an existing ADR before overriding the recorded decision. Product behavior remains governed by `docs/spec.md` and the relevant integration documents; the glossary is not a substitute for those contracts.
