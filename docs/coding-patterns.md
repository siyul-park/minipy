# Coding and Compiler Architecture Standard

This document is the normative coding and compiler-architecture specification for
minipy. It defines function shape, naming, APIs, domain ownership, declaration
order, diagnostics, compilation state, concurrency, tests, and documentation.

A violation in a changed file is a blocking defect, not a style preference.
Unchanged code is migrated when it is touched or when a repository-wide
conformance change explicitly includes it.

## 1. Precedence and requirement levels

### 1.1 Precedence

Apply the first rule that resolves the question:

1. A nearby pattern that is more specific, internally consistent, and compliant
   with this standard.
2. This document.
3. General Go convention.

Existing code does not override this standard merely because it is nearby. A
local exception MUST state the invariant that requires it.

### 1.2 Requirement levels

| Term | Meaning |
|---|---|
| MUST, MUST NOT | Required. |
| SHOULD, SHOULD NOT | Default; a deviation requires a reason in the change summary. |
| MAY | Allowed at the author's discretion. |

### 1.3 Reading index

Read SS2 for every change, then only the sections the change touches.

| Change | Sections |
|---|---|
| functions, methods, or helpers | 3, 4 |
| names or public APIs | 4, 5 |
| package or type ownership | 6 |
| compiler phases or runtime bridge | 7 |
| declaration or file layout | 8 |
| errors or diagnostics | 9 |
| concurrency or shared state | 10 |
| tests or benchmarks | 11 |
| documentation, commits, or PRs | 12 |

## 2. Core principles

1. Code MUST read as a behavior specification. A reader identifies what happens,
   which phase owns it, and which invariant it preserves without simulating the
   implementation.
2. Each function MUST hold one abstraction level.
3. Behavior MUST live in the package, phase, and type that own its rule.
4. Related state, validation, mutation, and cleanup MUST stay together.
5. Exported surface MUST be the smallest complete API.
6. Structure MUST be added only when it removes real complexity.
7. Explicit behavior takes precedence over hidden mechanics; readable code takes
   precedence over clever code.
8. Checker and lowerer assumptions MUST remain synchronized.
9. Every phase boundary MUST produce a value whose validity is clear.
10. Every compiled program MUST be verified after all transformations and before
    it is returned.

### 2.1 Simplification gate

Every touched symbol needs a current reason to exist. Review each file, type,
interface, field, function, method, parameter, result, constant, and variable.
Remove, inline, merge, narrow, privatize, or rename anything whose only purpose
is future flexibility, one-call-site convenience, symmetry without behavior, or
shorter code.

Before adding structure, consider a simpler algorithm. Prefer one direct pass,
phase-local state, exact ownership, and data flow matching the compilation
pipeline. Performance claims MUST include benchmark evidence.

Run simplification in passes until another pass finds no safe improvement.
Intentionally rejected simplifications MUST be recorded in the change summary.

## 3. Functions

### 3.1 Abstraction level

A function MUST NOT mix unrelated levels, including:

- compilation orchestration with token, AST, or bytecode mechanics;
- syntax construction with semantic policy;
- type policy with minivm instruction encoding;
- runtime protocol orchestration with reference-counting mechanics.

The main flow MUST remain visible. Mechanics MAY move behind a behavior-level
name when that makes the flow easier to read.

```text
Compile
  parse source modules
  check the module graph
  lower checked modules
  optimize
  verify

lowerFunction
  create frame
  emit body
  close captures
```

Large grammar and node-dispatch switches MAY remain in one function when the
cases are one cohesive catalogue at one abstraction level. Complex cases SHOULD
move to receiver-owned behavior after the dispatch function that introduces
them.

### 3.2 Declaration order

Callers MUST be declared before callees. Reading downward MUST reveal
progressively more detail.

- Symbols at the same abstraction level MUST be adjacent and in call order.
- A callee MUST be as close to its caller as its other callers allow.
- A shared callee MUST follow the last same-level caller that introduces it.
- Lower-level mechanics MUST NOT interrupt a higher-level flow.
- Diagnostic constructors, formatting helpers, and comparable leaves MUST be
  last in their declaration group.

Functional options precede the constructor they configure. `MustNew` precedes
`New` when both exist.

### 3.3 Helper extraction

A helper MAY be extracted only when it:

1. removes real duplication;
2. names reusable compiler, domain, or protocol behavior;
3. separates mechanics that would mix abstraction levels; or
4. is required as a function value by composition.

A private helper SHOULD have at least two real callers. A single-use helper MUST
be inlined unless inlining would mix abstraction levels. A surviving single-use
helper MUST name a policy or mechanic, not a sequential step.

A helper MUST NOT exist only to shorten a function, delegate one call, translate
one obvious error, hide one branch, or label setup. Collection and comparison
rules over another package's values belong to that package when it can own the
operation honestly.

### 3.4 Methods and functions

Use a method when behavior belongs to one receiver. Use a package function for
constructors, behavior shared by unrelated types, or behavior with no natural
receiver.

Receiver-owned behavior MUST NOT remain a package helper. A method MUST NOT be
added merely to shorten a call. Constructors are package functions: `NewType`
for exported concrete types and `newType` for private types.

## 4. Naming

### 4.1 General rules

- Prefer one-word names.
- Add a word only when package, receiver, file, or local context cannot preserve
  the exact concept.
- Use one canonical term per concept across packages.
- MUST NOT repeat package, receiver, phase, or representation context.
- MUST NOT abbreviate except for established terms such as `ID`, `URI`, `URL`,
  `ABI`, `VM`, `IR`, `CFG`, `SSA`, and `EOF`.
- MUST NOT use one-letter names except conventional indexes, receivers in small
  scopes, and compact mathematical loops.
- Preserve exact language, protocol, minivm, and standard-library terms.

Receivers such as `c` for checker/compiler and `l` for lowerer are permitted only
where the file has one unambiguous primary receiver. Use a role name when
multiple receiver concepts coexist.

### 4.2 Predicates and transitions

| Form | Use |
|---|---|
| `HasX` | membership or registered-value existence |
| `IsX` | state or type predicate |
| `MatchX` | equality, verifier, or pattern check |
| `CanX` | capability computed from current state |
| direct field name | direct boolean value |

Domain verbs such as `Parse`, `Resolve`, `Check`, `Narrow`, `Lower`, `Optimize`,
`Verify`, `Retain`, and `Release` are reserved for computations or transitions,
not plain collection lookups.

### 4.3 Compiler vocabulary

Use these terms consistently:

- **source**: original text or its source identity;
- **syntax**: tokens and AST shape;
- **checked**: semantic facts validated by the checker;
- **lowered**: minivm-oriented representation or bytecode;
- **program**: final minivm program;
- **diagnostic**: stable user-facing source failure;
- **session**: mutable state for one compilation;
- **compiler**: reusable validated configuration and entry point;
- **module graph**: reachable source and native modules for one compilation.

Do not use generic names such as `Service`, `Manager`, `Context`, `Data`, `Info`,
or `Result` when a precise compiler role exists. `context.Context` retains its
standard name.

## 5. Types and APIs

### 5.1 API surface

Every exported symbol is a maintenance commitment.

- Accept interfaces when callers provide behavior; define them where consumed.
- Return concrete types from constructors.
- Keep public structs small and intentional.
- MUST NOT expose writable aggregate or session fields.
- MUST NOT add aliases, wrappers, pass-through methods, or containers named
  `Request`, `Response`, `Result`, `Data`, or `Info` without a distinct contract.
- Add a type only when it owns an invariant, policy, phase product, state
  transition, or stable capability.

Public APIs MAY change when the current shape violates ownership, leaks mutable
phase state, prevents a valid phase boundary, or creates an incomplete contract.
A breaking change MUST be explicit in the commit/PR, update all repository
callers, update documentation, and either provide a deliberate migration path or
state why a clean break is safer. Public change permission MUST NOT justify
speculative renaming.

### 5.2 Constructors and options

A constructor MUST require only values that have no safe default, cannot be
created internally or supplied later, and are required for primary behavior.
Optional construction-time choices MUST use functional options. Required values
MUST use direct parameters.

A reusable `Compiler` MUST contain validated policy and infrastructure only.
Mutable source, diagnostics, module graph, checker state, lowerer state, and
emission state MUST belong to one compilation session and MUST NOT leak across
calls.

Options MUST be passed through the constructor that owns their contract. Defaults
are applied first, then options. An option reusing an existing public transition
MUST call that transition rather than duplicate validation.

### 5.3 Encapsulation and mutable values

Every exported type is an ownership boundary, including within its package. A
method on one exported type MUST use another exported type's smallest public
constructor, behavior, getter, or setter rather than access its private state.
Encoding and private implementation functions belonging to the type MAY access
that state.

Getters MUST return defensive copies of mutable slices, maps, bytes, keys, AST
buffers, and metadata. A setter is appropriate only when it owns the complete
replacement transition and preserves all invariants.

### 5.4 Values and catalogues

AST nodes MAY be exported data structs because they are the stable syntax
representation; semantic state MUST NOT be added to them. Token, diagnostic,
opcode, builtin, and type catalogues MAY use domain order instead of visibility
order when that order is part of readability or compatibility.

Sets and maps exposed as deterministic compiler output MUST reject duplicate
identities where duplicates are invalid and MUST emit stable ordering at the
boundary where order is observable.

## 6. Domain and package ownership

| Package | Owns |
|---|---|
| `token` | token vocabulary, positions, diagnostic codes, rendering |
| `lexer` | rune scanning, indentation, literal scanning, lexical diagnostics |
| `ast` | data-only syntax nodes and source positions |
| `parser` | grammar recognition, precedence, recovery, AST construction |
| `types` | source type lattice, normalization, comparison, minivm mapping |
| `module` | extension contracts and native/source module registry |
| `builtins` | builtin symbols, exception hierarchy, builtin semantics |
| `operator` | operator typing and lowering semantics |
| `hostabi` | host/minivm value bridge, formatting, iterators, reference protocol |
| `compiler` | module graph, semantic checking, specialization, lowering, passes |
| `typing` | annotation-only native module vocabulary |
| `cmd/minipy` | CLI/REPL input, output, and process-boundary coordination |

Behavior is placed by its dominant rule, not by how many packages it references.
AST nodes stay data-only. Parser code MUST NOT own semantic policy. Checker code
MUST NOT duplicate native symbol or operator rules. Lowerer code MUST NOT accept
forms the checker rejects, and unsupported forms MUST fail before lowering.

A package MUST NOT introduce nested subpackages merely to hide unrelated
responsibilities. Split only when a responsibility has a stable boundary and
independent vocabulary.

## 7. Compiler and runtime architecture

### 7.1 Pipeline

The canonical pipeline is:

```text
source -> tokens -> AST -> checked module graph -> lowered program
       -> ordered transforms -> verified minivm program
```

The public entry point MUST make this order visible. A phase MUST consume the
preceding phase's validated product and MUST NOT reach backward into another
phase's mutable implementation state.

The architecture adopts three proven ideas from production compilers:

1. CPython's separation of syntax, symbol analysis, control-flow-aware emission,
   and assembly: minipy keeps syntax, semantic metadata, lowering, and final
   program construction distinct.
2. CPython's compilation-unit ownership: mutable state for nested scopes belongs
   to an explicit per-compilation/per-scope stack, not a reusable global object.
3. Go's explicit sequence of typed IR, ordered transforms, target lowering, and
   final checks: minipy keeps pass order visible and verifies the final minivm
   program after every transform.

These are design references, not compatibility goals. minipy MUST NOT copy
CPython's dynamic object runtime, GIL, reference-counted object model, or C API,
and MUST NOT add Go's machine-specific SSA/backend pipeline without a current
minivm requirement.

### 7.2 Phase products

- The lexer returns tokens plus recoverable lexical diagnostics.
- The parser returns a non-nil AST when recovery can preserve useful syntax plus
  accumulated diagnostics.
- The checker owns symbol tables, source types, narrowing, captures, class
  layouts, specializations, and semantic diagnostics.
- Checked metadata MUST be immutable to the lowerer after checking completes.
- The lowerer consumes AST plus checked metadata and emits a minivm program.
- Optimizer passes MUST run in one visible order.
- An optimized program is the transform pipeline's whole product. A phase MUST
  NOT write a pre-optimization constant, type, handler, or global table back
  over it; the passes compact and repair those tables against the code they
  rewrite, so a restored copy no longer describes the emitted program.

A general public `Validate` method MUST NOT be exposed when constructors and
phase boundaries can enforce validity directly.

### 7.3 Compiler and session lifetime

`Compiler` is reusable configuration. A private session owns one invocation.
Each `Compile` call MUST create fresh mutable state. A failed call MUST NOT poison
a later call. Concurrent use is permitted only when the implementation contains
no shared mutable session state; otherwise the public contract MUST state that it
is not safe for concurrent use.

Source readers, request contexts, and diagnostics MUST NOT be stored beyond the
compilation that owns them.

### 7.4 Checker/lowerer symmetry

Changes involving types, truthiness, narrowing, specialization, closures,
exceptions, patterns, native calls, or containers MUST review both checker and
lowerer paths. Any intentional asymmetry MUST name the phase invariant that makes
it safe.

### 7.5 Native symbols and runtime bridge

A native symbol owns a coherent contract:

1. checker rule;
2. emitter callback;
3. optional runtime value or host function.

Native symbols are compile-time callable names unless the language spec changes.
`hostabi` owns boxed runtime representation and retain/release behavior. Formatting
or convenience code MUST NOT silently change ownership or hide stale-reference
failures unless that fallback is an explicit public contract.

## 8. File layout

### 8.1 Declaration groups

Use this order unless SS8.2 defines a domain-order exception:

1. public types;
2. private types;
3. public constants;
4. private constants;
5. public variables;
6. private variables;
7. `init` functions;
8. public options and functions;
9. constructors;
10. public methods;
11. clone and explicit conversion methods;
12. standard-library, encoding, database, and protocol hooks;
13. private functions and methods.

Within a group, order by meaning and call flow, never alphabetically. A struct and
all methods with that receiver SHOULD remain in one file. Large checker, lowerer,
parser, and catalogue types MAY split by one named concern; the split MUST retain
one obvious entry file and caller-before-callee order within each concern.

### 8.2 Domain-order exceptions

Grammar nodes, tokens, diagnostic codes, type catalogues, opcode maps, and native
symbol tables MAY use grammar, precedence, lifecycle, or protocol order when
visibility grouping would make the domain harder to audit. State the ordering in
a short file comment when it is not obvious.

Generated code follows its generator. No generated code currently exists in this
repository; introducing it requires an explicit generation and verification
workflow.

### 8.3 Method and field order

For stateless capabilities, adapters, and phase coordinators: primary behavior,
then supporting behavior in top-down call order, then lower-level helpers.

For stateful entities and values: multi-field behavior; field groups in struct
order; within a group mutator, predicate/matcher, getter; workflow transitions;
clone/conversions; interface hooks.

Struct fields are ordered as lifecycle/policy, infrastructure, domain data,
runtime state, read-only configuration, synchronization. Separate conceptual
groups with a blank line. Synchronization fields are last. Struct literals MUST
follow declaration order.

## 9. Errors and diagnostics

- An error MUST NOT be ignored.
- Package-created operational errors MUST use stable `ErrXxx` sentinels when
  callers can branch on identity.
- Dependency errors MUST remain unchanged when identity is contractual.
- Translate only outcomes whose meaning is known at the current boundary.
- Preserve cancellation, timeout, conflict, corruption, verifier, and runtime
  failures.
- MUST NOT wrap an error only to repeat the failed operation.
- Use `errors.Is` and `errors.As` only when identity controls behavior.
- Error messages MUST NOT include secrets, opaque host values, private source,
  or unrelated process data.

Malformed source is reported as stable `token.Error` values accumulated in a
`token.ErrorList`. Diagnostic code, source position, and rendered Python-style
class are public behavior. A phase MUST use the most specific code it owns and
MUST NOT panic for user input.

Panic is permitted only for impossible internal invariants and startup/test setup
where returning an error cannot be part of the contract. Public constructors for
caller-supplied configuration SHOULD return errors instead of panic. A `MustNew`
variant MAY provide explicit panic-on-invalid setup.

## 10. Concurrency and state

- `context.Context` MUST be the first parameter of operations that may block,
  perform I/O, or cross a process boundary when cancellation is part of the API.
- Contexts MUST NOT be stored in structs.
- Cancellation and timeout ownership MUST be preserved.
- Shared mutable state MUST have one owner and explicit synchronization.
- Goroutine lifetimes MUST be tied to a compilation, request, or process and
  shut down gracefully.
- Concurrency MUST NOT be introduced without a measured workload and a defined
  deterministic diagnostic/output order.

The current compiler performs one compilation synchronously. Parallel package or
module compilation is a future architecture decision, not a local optimization.

## 11. Tests and benchmarks

### 11.1 Placement and public contract

Unit tests live beside production code as `*_test.go`. Each test file MUST match
the production file that owns the symbol or phase contract. Cross-phase language
regressions MAY live in the compiler integration test file named for the public
entry point.

Tests are executable specifications of public behavior. They MUST construct
exported types through exported constructors/options and observe exported
functions/methods, diagnostics, returned minivm programs, execution results, or
encoded representations. They MUST NOT read/write private fields, call private
methods/functions, or add test-only back doors.

Inspecting bytecode, constant pools, handler tables, or source/minivm type mapping
is permitted when that returned representation is the protected compiler/VM
contract. The assertion MUST name the contract rather than an incidental index
when possible.

### 11.2 Test layout

Write one top-level test per exported constructor, function, or method with an
independent contract:

```text
TestCompile
TestNewCompiler
TestCompiler_Compile
```

Cases belong under `t.Run`. Test order MUST match source declaration order. Each
subtest owns its mutable arrange; sibling subtests MUST NOT share an object graph.

Use `require`, not `assert` or direct `t.Fatal`/`t.Error`, except benchmark APIs
where `b.Fatal` is conventional and necessary. Keep source snippets, expected
diagnostics, and runtime behavior visible near assertions.

### 11.3 Fixtures, helpers, and doubles

Duplicated arrange is acceptable. A test helper MAY decode a wire/program value,
implement an exported extension point, or assemble a real object graph when it
does not hide behavior. It MUST NOT sequence the behavior under test, proxy a
real implementation to inject failure, or expose private state.

Prefer real in-memory registries, filesystems, runtimes, and compilers. Reproduce
failures through real public mechanisms: invalid source, missing modules,
duplicate symbols, cancelled contexts, stale references, verifier failures, or
concurrent writes where supported. If no public mechanism reproduces a claimed
failure, delete the private-shape test or improve production API only when the
missing capability is a legitimate contract.

### 11.4 Coverage and benchmarks

Test lexer/parser/checker/lowerer invariants at their owning public boundary and
retain end-to-end compile/run regression coverage. Security and correctness fixes
MUST add regression coverage when requested by the change.

Benchmarks MUST preserve semantics, use representative input, report before/after
results, and avoid setup in the timed region. Performance structure without
benchmark evidence is prohibited.

## 12. Documentation, commits, and review

### 12.1 Documentation ownership

| Change | Owner |
|---|---|
| coding or architecture convention | `docs/coding-patterns.md` |
| agent workflow/routing | `AGENTS.md`, with Claude-only delta in `.claude/CLAUDE.md` |
| syntax | `docs/spec/03-grammar.md` |
| types/checker | `docs/spec/02-types.md`, `docs/spec/04-static-semantics.md` |
| lowering/runtime representation | `docs/spec/05-codegen.md` |
| builtins/operators/native modules | `docs/spec/06-builtins.md` |
| user-visible compatibility | `docs/compatibility.md` |
| completed/deferred work | `docs/roadmap.md` |

Specs describe current behavior. Every construct is classified as lowered,
parse-only, restricted, rejected, planned, or out of scope. Stale milestone
language is prohibited.

### 12.2 Git and PRs

Commits MUST be focused and use `<type>(scope): <summary>` in imperative mood,
72 characters or fewer. Breaking changes use `!` and a `BREAKING CHANGE:` body.
PRs explain what changed, why, tests run, public migration impact, and benchmark
impact where relevant.

### 12.3 Completion checklist

Before completing a change, verify:

- [ ] owning package, phase, and type are clear;
- [ ] names use canonical terms;
- [ ] functions hold one abstraction level;
- [ ] helpers remove real complexity;
- [ ] phase products do not leak mutable implementation state;
- [ ] checker/lowerer assumptions remain synchronized;
- [ ] no pre-optimization table is written back over an optimized program, and
      final verification remains intact;
- [ ] errors preserve operational identity and diagnostics remain stable;
- [ ] mutable values are defensively copied;
- [ ] declarations and tests follow call/source order;
- [ ] relevant tests, `go vet ./...`, and `go test ./...` pass;
- [ ] behavior and compatibility docs are synchronized;
- [ ] unrelated changes are absent from the diff;
- [ ] a final simplification pass found no safe improvement.

## 13. Design references

This document adapts architecture principles from the following production
compiler documentation while retaining minipy's own language and minivm
contracts:

- [CPython internals](https://devguide.python.org/internals/)
- [PEP 339: Design of the CPython Compiler](https://peps.python.org/pep-0339/)
- [Introduction to the Go compiler](https://github.com/golang/go/blob/master/src/cmd/compile/README.md)

The references inform phase separation, compilation-unit state, ordered
transformations, and verification. They are not normative dependencies and do
not make minipy compatible with either implementation.

Content derived from these references was rephrased for compliance with
licensing restrictions.

## 14. Amendment process

Changes to this document require a focused documentation change that states the
problem, the invariant being changed, migration impact, and rejected
alternatives. A code change MUST NOT silently establish a conflicting convention.

`AGENTS.md` routes work to this standard and enforces completion. Tool-specific
files MUST remain overlays and MUST NOT duplicate the normative rules.
