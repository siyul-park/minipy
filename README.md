# minipy

**A statically-typed subset of Python that compiles to
[minivm](https://github.com/siyul-park/minivm) bytecode.**

You write Python with type hints. minipy type-checks it ahead of time and emits a
minivm program that runs on minivm's threaded interpreter and ARM64 trace JIT.
minipy is not a Python interpreter and is not dynamically typed — it is a small,
fast, embeddable language that *looks like* Python.

## Highlights

- **Static, AOT.** Every type is known at compile time; no runtime type dispatch.
- **Type hints required at boundaries, inferred locally.** Annotate function
  params/returns, class fields, and module globals; local variables are inferred.
  Untyped boundaries do not compile.
- **`int` is int64** (wraps on overflow) — no arbitrary-precision integers.
- **A real subset.** Every minipy program is valid Python 3.13 source.
- **Direct lowering.** Typed AST → minivm bytecode (no IR); reuses minivm's
  optimizer and JIT.

```python
def fib(n: int) -> int:
    if n < 2:
        return n
    return fib(n - 1) + fib(n - 2)

print(str(fib(20)))   # 6765
```

## Status

Specification stage. The language and its staged implementation plan are
documented; the compiler is not yet built. Start at the overview:

- **[docs/spec/00-overview.md](docs/spec/00-overview.md)** — goals, typing model,
  int64 rule, compilation pipeline.

## Documentation

| Doc | Contents |
|---|---|
| [docs/spec/00-overview.md](docs/spec/00-overview.md) | goals, typing philosophy, pipeline |
| [docs/spec/01-lexical.md](docs/spec/01-lexical.md) | tokens, indentation, literal subset |
| [docs/spec/02-types.md](docs/spec/02-types.md) | type system + Python→minivm mapping |
| [docs/spec/03-grammar.md](docs/spec/03-grammar.md) | the subset grammar, tagged by milestone |
| [docs/spec/04-static-semantics.md](docs/spec/04-static-semantics.md) | typing rules, inference, scoping, errors |
| [docs/spec/05-codegen.md](docs/spec/05-codegen.md) | lowering each construct to minivm opcodes |
| [docs/spec/06-builtins.md](docs/spec/06-builtins.md) | builtins + host-function ABI |
| [docs/roadmap.md](docs/roadmap.md) | milestones M0–M10 |
| [docs/reference/](docs/reference/) | upstream CPython 3.13 grammar/lexical/datamodel |

## License

See [LICENSE](LICENSE). Reference material under `docs/reference/` is from the
CPython documentation, © Python Software Foundation (see
[docs/reference/SOURCES.md](docs/reference/SOURCES.md)).
