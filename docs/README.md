# Documentation

Documentation map and ownership guide for minipy contributors and agents.

## When to Read

Read this before editing documentation, adding a language feature, or looking for
the owner of a topic.

Use this file as the documentation entry point. It should summarize where to go,
not duplicate the full content of each document.

## Source of Truth

| Concern | Source |
|---|---|
| project purpose and quick start | `README.md` |
| compiler and language overview | `docs/spec/00-overview.md` |
| lexical rules | `docs/spec/01-lexical.md` |
| source type system | `docs/spec/02-types.md` |
| accepted syntax | `docs/spec/03-grammar.md` |
| checker behavior and diagnostics | `docs/spec/04-static-semantics.md` |
| lowering and runtime representation | `docs/spec/05-codegen.md` |
| builtins and native modules | `docs/spec/06-builtins.md` |
| Python compatibility status | `docs/compatibility.md` |
| completed work and remaining gaps | `docs/roadmap.md` |
| contributor coding patterns | `docs/coding-style.md` |

## Reading Paths

### New contributor

1. `README.md` for the project summary and commands.
2. `docs/spec/00-overview.md` for the compiler pipeline and package roles.
3. `docs/coding-style.md` before making a code or documentation change.

### Language feature change

1. Update the owning spec file.
2. Update `docs/compatibility.md` if user-facing Python compatibility changes.
3. Update `docs/roadmap.md` if the change completes, defers, or reclassifies work.
4. Update `README.md` only when the public summary or command flow changes.

### Compiler behavior change

1. `docs/spec/04-static-semantics.md` for checker behavior.
2. `docs/spec/05-codegen.md` for lowering behavior.
3. `docs/spec/06-builtins.md` for native symbol behavior.
4. Tests near the package that owns the changed behavior.

## Document Roles

- Spec files are authoritative and should use precise implementation language.
- Compatibility is a user-facing matrix; keep rows short and link back to the
  owning spec when detail would become repetitive.
- Roadmap is status-oriented; do not put normative language rules there.
- Coding style governs contributor decisions; it should reference minivm shared
  patterns and add only minipy-specific rules.
- README is for orientation, not a full language manual.

## Quality Checklist

Before merging documentation changes, check that the docs are:

- **Consistent** — terms such as `checker`, `lowerer`, `source type`, `native
  module`, `parse-only`, and `diagnostic` mean the same thing everywhere.
- **Scoped** — each topic has one owner, and other documents summarize instead of
  copying full rules.
- **Current** — shipped behavior is described directly, and unsupported behavior
  is labeled as parse-only, restricted, planned, or out of scope.
- **Readable** — headings answer the reader's likely questions in order, examples
  are small, and long tables use short notes.
- **Reviewable** — docs-only changes avoid unrelated code edits and explain what
  was validated against implementation.

## Maintenance Notes

When adding a new document, add it to this index and to `README.md` only if it is
useful to users outside contributor workflows.

When moving ownership of a topic, update this file and remove duplicate detailed
rules from the old location.

## Related Docs

- `README.md` — public project entry point.
- `docs/coding-style.md` — code and documentation contribution patterns.
- `docs/spec/00-overview.md` — compiler architecture and execution model.
- `docs/compatibility.md` — feature support matrix.
- `docs/roadmap.md` — planned and deferred work.
