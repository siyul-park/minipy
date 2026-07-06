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

- `docs/coding-style.md` Principles and Symbol and Algorithm Review were applied: every touched symbol has a reason, simpler algorithms were considered, and another simplification pass found no safe improvement.
- Declaration order follows `docs/coding-style.md`: callers before callees, with the allowed exception that functional options may sit immediately above the constructor or function they configure.
- Private package functions used by one type became methods on that type, unless they are constructors, shared by multiple types, or clearer inline.
- Single-use helpers stayed inline unless extraction names real behavior or removes real duplication.
- Struct fields follow the semantic layers in `docs/coding-style.md`.
- Tests assert public behavior, use one top-level test per public symbol, keep source/diagnostic/runtime expectations visible, and use `require`.
- Documentation, workflow, language behavior, compatibility, or convention changes updated the owning docs.

If any checklist item is intentionally not applied, record the reason in the final summary.
