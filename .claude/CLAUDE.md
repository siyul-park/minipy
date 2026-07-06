# CLAUDE.md

@../AGENTS.md

## Claude Code Overlay

This file imports the common agent contract from `AGENTS.md`. Keep shared rules there so Claude Code and Codex stay aligned.

Use this file only for Claude-specific execution reminders.

## Required Claude Flow

1. Start from the imported `AGENTS.md` workflow.
2. For multi-file, uncertain, or risky work, explore first, then plan, then edit.
3. Give yourself a runnable verification target before editing whenever possible.
4. Before reporting done, complete the `AGENTS.md` Completion Gate and the Claude checklist below.
5. Show evidence in the final summary: tests run, docs updated, and any intentionally skipped simplification.

## Claude Checklist

Before reporting done, re-read every touched code/test file and verify:

- `docs/coding-patterns.md` §0.7-§0.9 was applied: every touched symbol has a reason, simpler algorithms were considered, and another simplification pass found no safe improvement.
- Declaration order follows §1.3 and §2.4: callers before callees, with the allowed exception that `With*` option functions may sit immediately above the constructor they configure.
- Minipy compiler rules in §9 were applied when touching lexer, parser, checker, lowerer, native modules, or language-support docs.
- Checker and lowerer assumptions stayed synchronized for narrowing, specialization, closures, exceptions, patterns, and native calls.
- Parse-only, rejected, restricted, lowered, planned, and out-of-scope language behavior stayed explicit in the owning docs.
- Diagnostics for malformed user source flow through `token.Error` / `token.ErrorList`, not panic.
- Language behavior changes updated the owning spec plus compatibility or roadmap status when relevant.
- Native symbol changes kept the checker rule, emitter callback, and optional runtime value / host function coherent.

If any checklist item is intentionally not applied, record the reason in the final summary.
