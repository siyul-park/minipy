# minipy — Builtins & Host ABI

minipy ships a small, typed builtin namespace. Each builtin is either **lowered
inline** to opcodes or **bound to a minivm host function**. There is no Python
`builtins` module object and no runtime name lookup — builtins are resolved at
compile time.

## Binding strategies

1. **Inline** — the call is replaced by opcodes (no function call overhead).
   Example: `len(lst)` → `ARRAY_LEN`.
2. **Host function** — bound to a Go `interp.HostFunction` placed in the constant
   pool; emitted as `CONST_GET <i>; CALL`. Used for I/O and runtime helpers.

Builtins are statically typed; calling one with the wrong type/arity is a compile
error (`TypeMismatch`/`ArityMismatch`), not a runtime `TypeError`.

## Core builtins (M0–M3)

| builtin | signature | binding |
|---|---|---|
| `print` | `(*vals) -> None` (v1: `(str) -> None` + per-type overloads) | host |
| `len` | `(str) -> int`, `(list[T]) -> int`, `(dict[K,V]) -> int` | inline (`STRING_LEN`/`ARRAY_LEN`/`MAP_LEN`) |
| `range` | `(int) \| (int,int) \| (int,int,int) -> range` | inline (drives `for` desugar; no object) |
| `int` | `(float) -> int`, `(bool) -> int`, `(str) -> int` | inline conv (`F64_TO_I64_S`) / host (parse) |
| `float` | `(int) -> float`, `(str) -> float` | inline (`I64_TO_F64_S`) / host (parse) |
| `str` | `(int\|float\|bool) -> str` | host (format) |
| `bool` | `(int\|float\|str) -> bool` | inline (`!= 0` / nonempty) |
| `abs` | `(int) -> int`, `(float) -> float` | inline (branch / `F64_ABS`) |
| `min` / `max` | `(int,int)->int`, `(float,float)->float` | inline (compare + `SELECT` / `F64_MIN`/`MAX`) |

`print` is the canonical host function. Its Go shape (per minivm
[host-integration](https://github.com/siyul-park/minivm/blob/main/docs/host-integration.md)):

```go
print := interp.NewHostFunction(
    &types.FunctionType{Params: []types.Type{types.TypeString}, Returns: nil},
    func(vm *interp.Interpreter, args []types.Boxed) ([]types.Boxed, error) {
        s, _ := vm.Load(args[0].Ref()).(types.String)
        fmt.Println(string(s))   // host policy decides the sink
        return nil, nil
    },
)
```

`range` does not create a runtime object in v1; it only configures the `for`
loop's bounds at compile time ([`05-codegen.md`](05-codegen.md#for-range-and-iterables)).
A first-class lazy `range` object is deferred to M6 (it is naturally a generator).

## Later builtins (by milestone)

| milestone | adds |
|---|---|
| M3 | list methods `append/pop`, dict `get/keys/values/items`, str `upper/lower/split/join/find`, `enumerate`, `zip` |
| M5 | `isinstance` (limited, for class hierarchy), `@dataclass`, `@staticmethod` |
| M6 | `iter`, `next`, lazy `range` object |
| M7 | exception classes `Exception`, `ValueError`, `KeyError`, `IndexError`, … |
| M8 | a curated typed stdlib subset (`math`, `random`, …) exposed as host modules |

## Host-function ABI (for embedders)

minipy programs run inside a host Go program that calls minivm `interp.New`. The
host provides the implementations behind builtin/stdlib host functions. Contract:

- A host function has a `*types.FunctionType` (params/returns) that **must match**
  the type minipy assigns to the builtin; minipy emits calls assuming this ABI.
- Arguments arrive as typed `[]types.Boxed` (no reflection). Primitives are inline
  (`args[i].I64()`, `.F64()`, `.Bool()`); refs (`str`, `list`, …) are loaded with
  `vm.Load(args[i].Ref())`.
- Returns are `[]types.Boxed`; a `-> None` builtin returns `nil`.
- Resource and policy limits (heap, fuel, I/O sinks) are the host's to set via
  minivm options — minipy does not bypass them.

A minipy compilation therefore produces both a `program.Program` and a manifest of
required host functions (name → `FunctionType`), so an embedder knows exactly what
to register. Standard builtins ship as a default registry the CLI installs
automatically.

## Out of scope

`eval`, `exec`, `compile`, `globals`, `locals`, `vars`, `dir`, `getattr`,
`setattr`, `hasattr`, `__import__`, `open` (until M8 stdlib policy), `input`
(host-policy dependent), and any builtin returning an untyped/dynamic value in the
static core. Reflective/dynamic builtins, if ever added, belong to the M9 dynamic
mode.
