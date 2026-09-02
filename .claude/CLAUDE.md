# CLAUDE.md

@../AGENTS.md

## Claude Code Overlay

The imported `AGENTS.md` is the repository workflow contract.
`docs/coding-patterns.md` is accepted RFC 0001 and the normative coding and
compiler-architecture standard. This overlay contains Claude-specific execution
reminders only; shared rules MUST NOT be copied here.

## Required Claude flow

1. Follow the imported workflow and RFC reading index before editing.
2. For multi-file, uncertain, architectural, or public-API work, explore and map
   ownership before planning edits.
3. Establish a runnable baseline and narrow verification target.
4. Keep a visible plan for multi-step work and complete ownership units in order.
5. Re-read touched files, inspect the complete diff, and run the imported
   Completion Gate before reporting done.
6. Report verification evidence, documentation updates, breaking API impact,
   deliberate RFC deviations, and rejected simplifications.

## Compiler review reminders

When compiler or runtime code changes, explicitly verify:

- reusable configuration contains no per-compilation mutable state;
- parser, checker, lowerer, optimizer, and verifier boundaries remain visible;
- checker and lowerer agree on types, narrowing, specializations, closures,
  exceptions, patterns, native calls, and containers;
- native symbols retain a coherent checker/emitter/runtime contract;
- malformed user source returns stable diagnostics rather than panic;
- no pre-optimization table is written back over an optimized program, and
  every returned program is verified;
- tests observe public behavior or a protected returned program representation,
  never private implementation state;
- owning specs and compatibility/status documents match current behavior.
