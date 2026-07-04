# minipy — Code Generation (lowering to minivm)

The compiler walks the typed AST and produces a minivm `program.Program`. It emits
`instr.Instruction` sequences with **symbolic labels**; a final pass resolves
labels to the signed-16-bit relative offsets minivm branches expect
(`target = instruction_start + width + operand`). minivm's optimizer
(`optimize.NewOptimizer(optimize.O1)`) runs afterward.

Opcode names and semantics below are from minivm
[`docs/instruction-set.md`](https://github.com/siyul-park/minivm/blob/main/docs/instruction-set.md).
Stack effect notation: `a b → c` means pops `a` then `b`, pushes `c`.

## Program shape

- The module body becomes the **entry function** (`fp == 1`). Top-level `def`s and
  `class`es become `*Function`/struct-type constants in the program constant pool.
- Module globals → VM globals (`GLOBAL_*`, u16 index).
- Strings, function templates, and out-of-inline-range constants live in the
  constant pool (`CONST_GET`, u16 index).

## Values & literals

| minipy | emit |
|---|---|
| `int` literal `n` | `I64_CONST n` |
| `float` literal `x` | `F64_CONST x` |
| `True` / `False` | `I32_CONST 1` / `I32_CONST 0` |
| `None` | `REF_NULL` |
| `str` literal | `CONST_GET <pool index of String>` |

## Variables

| access | local | global | upvalue |
|---|---|---|---|
| read | `LOCAL_GET i` | `GLOBAL_GET i` | `UPVAL_GET i` |
| write | `LOCAL_SET i` | `GLOBAL_SET i` | `UPVAL_SET i` |
| write-and-keep | `LOCAL_TEE i` | `GLOBAL_TEE i` | — |
| delete (M9) | store `Zero(kind)` then `LOCAL_SET i` | store `Zero(kind)` then `GLOBAL_SET i` | — |

minivm has no slot-delete opcode, so `del NAME` stores the slot's minivm
uninitialized value (`types.Zero(kind)`: `REF_NULL` for ref kinds, the typed zero
const for scalars) — see [`del`](#del).

A mutable captured variable is boxed: `REF_NEW` at definition, `REF_GET`/`REF_SET`
through the upvalue cell (see [closures](#closures-m4)).

## Arithmetic & comparison

`int` (i64) and `float` (f64) pick the matching opcode family. Operands are
pushed left then right; the binary op pops both.

| op | `int` | `float` |
|---|---|---|
| `+ - *` | `I64_ADD/SUB/MUL` | `F64_ADD/SUB/MUL` |
| `/` | (int→float) `I64_TO_F64_S` both, then `F64_DIV` | `F64_DIV` |
| `//` | `I64_DIV_S` | `F64_DIV` then `F64_FLOOR` |
| `%` | `I64_REM_S` | `F64` floor-mod sequence |
| `**` | exponent-by-squaring loop / host `pow` | host `pow`→`F64` |
| `& \| ^ << >>` | `I64_AND/OR/XOR/SHL/SHR_S` | — (int only) |
| unary `-` | `I64_CONST 0` `SWAP` `I64_SUB` | `F64_NEG` |
| unary `~` | `I64_CONST -1` `I64_XOR` | — |
| `== != < <= > >=` | `I64_EQ/NE/LT_S/LE_S/GT_S/GE_S` → i1 | `F64_EQ/NE/LT/LE/GT/GE` → i1 |

`int / int` always yields `float` (matches Python true division): convert both
operands with `I64_TO_F64_S`, then `F64_DIV`. Overflow on `+ - * <<` is **not**
checked — i64 wraps, by design. `// %` by zero traps (`I64_DIV_S`/`REM_S`
→ `ErrDivideByZero`).

`str` `==`/`<`… use `STRING_EQ`/`STRING_LT`/… ; `str + str` is `STRING_CONCAT`.
Chained comparisons (`a < b < c`) evaluate each operand once and short-circuit on
the first false comparison. Middle operands are saved to a temporary slot only
long enough to become the left operand of the next comparison.

## Boolean & short-circuit

`a and b` / `a or b` short-circuit via branches (operands are `bool`=i1; comparison
opcodes and `*.eqz` push runtime kind `i1`, which shares the i32 slot):

```text
# a and b
<a>                 # i1 on stack
DUP
BR_IF  L_eval_b     # if a != 0, evaluate b
BR     L_end        # else result is a (false)
L_eval_b:
DROP
<b>
L_end:
```

`not a` → `I32_EQZ`. `a if c else b` lowers `<c>; BR_IF Lt; <b>; BR Le; Lt: <a>; Le:`
(or `SELECT` when both arms are side-effect-free single values).

## Control flow

### if / elif / else

```text
<cond>            # i1
I32_EQZ           # invert: jump when false
BR_IF L_else
<then-block>
BR L_end
L_else:
<else-block>      # or next elif chain
L_end:
```

### while

```text
L_top:
<cond>
I32_EQZ
BR_IF L_end
<body>            # break → BR L_end ; continue → BR L_top
BR L_top
L_end:
```

`break`/`continue` emit `BR` to the loop's `L_end`/`L_top` labels (a label stack
tracks the enclosing loop). `while…else` runs the `else` block at `L_end` only
when no `break` fired (separate `L_else_done` label).

### for (range and iterables)

`for i in range(a, b, s)` desugars to an integer `while` loop (no allocation):

```text
i = a
L_top:
<i < b>  (or i > b when s < 0)   # I64_LT_S
I32_EQZ ; BR_IF L_end
<body>
i = i + s
BR L_top
L_end:
```

`for x in <iterable>` (list/dict/generator, M3/M6) desugars to the iterator
protocol: obtain an iterator, then loop `RESUME`/`CORO_DONE`/`CORO_VALUE`
(see [generators](#generators-m6)). Over a `list[T]`, the simple form uses
`ARRAY_LEN` + an index loop with `ARRAY_GET`. Over a `dict` or `set`, lowering
uses `MAP_ITER` and the same iterator loop, avoiding key-array materialization.
Comprehensions use the same choice: array loop for lists, iterator loop for
iterators/dicts/sets/strings.

## Functions & calls (M2)

A `def` builds a `*Function` via minivm's `FunctionBuilder` with the param/local
slot layout from [`04-static-semantics.md`](04-static-semantics.md), then is stored
as a constant. minivm's `CALL` convention: push args, push the funcref last, then
`CALL`.

```text
# r = f(x, y)
<x>
<y>
CONST_GET <f>
CALL
LOCAL_SET <r>
```

- `RETURN` returns the top of stack (or pushes `REF_NULL` for a `-> None` fall-off).
- **Tail calls:** a `return f(...)` in tail position emits `RETURN_CALL` (args + fn,
  reuses the frame) so self/mutual recursion runs in constant frame depth.
- Default arguments are materialized at the call site by the compiler (it knows
  which args are omitted) — no runtime default machinery.
- **Variadic parameters.** A `*args: T` parameter is a normal VM parameter of type
  `list[T]`; a `**kwargs: T` parameter is a normal VM parameter of type
  `dict[str, T]`. At the call site the binder collects surplus positional arguments
  into a synthetic `list[T]` display and surplus keyword arguments into a synthetic
  `dict[str, T]` display, so the existing `ARRAY_NEW_DEFAULT`/`MAP_NEW` lowering
  builds the aggregate — no runtime argument-binding machinery. Fixed-arity calls
  are unaffected and still lower directly to `CALL`. Static `*tuple` call arguments
  expand to individual arguments at compile time; dynamic `*list`/`**dict` unpacking
  at the call site is not yet lowered.

## Strings & containers (M3)

| operation | emit |
|---|---|
| `list[T]` display `[a,b,c]` | push elems, `ARRAY_NEW <elemtype>` count, or `ARRAY_NEW_DEFAULT`+`ARRAY_SET` |
| `lst[i]` read / write | `ARRAY_GET` / `ARRAY_SET` (traps `ErrIndexOutOfRange`) |
| `len(lst)` | `ARRAY_LEN` |
| `dict` display `{k:v}` | push k/v pairs, `MAP_NEW <type>` count |
| `d[k]` read | `MAP_GET` (missing → zero value) or `MAP_LOOKUP` for membership |
| `d[k] = v` | `MAP_SET` |
| `k in d` | `MAP_LOOKUP` → use the `ok` flag |
| `len(d)` | `MAP_LEN` |
| `tuple` `(a,b)` | `STRUCT_NEW <type>` over fields; `t[const]` → `STRUCT_GET index` |
| `s1 + s2` | `STRING_CONCAT` ; `len(s)` → `STRING_LEN` |

Index/key types are statically known, so the compiler selects the right specialized
map/array type up front.

## Closures (M4)

For a nested function capturing `N` names: emit the captured upvalues, then the
function template, then `CLOSURE_NEW` (pops template + `N` upvals → closure).
Inside the closure, captured names use `UPVAL_GET/SET i`. A captured variable that
the inner function **writes** is boxed into a heap cell (`REF_NEW`) so both scopes
share mutations; read-only captures pass by value.

```text
# make_adder(n) returns lambda x: x + n
<n as upvalue>          # boxed cell if mutated, else value
CONST_GET <inner fn>
CLOSURE_NEW
```

## Classes (M5)

- A class type is a `StructType` (fields = annotated members, by index).
- Instantiation `C(args)`: `STRUCT_NEW_DEFAULT` (or build fields) then call
  `__init__` with the new struct as `self`.
- `obj.field` → `STRUCT_GET <index>`; `obj.field = v` → `STRUCT_SET <index>`.
- `obj.method(args)` → push `obj` + args, `CONST_GET <method fn>`, `CALL`.

## Generators (M6)

A `def` containing `yield` is a coroutine-function in minivm: `CALL` returns a
`Coroutine` handle. `yield e` emits `<e>; YIELD`. Iterating:

```text
<call gen()> → coro handle
L_top:
coro ; CORO_DONE ; BR_IF L_end
coro ; CORO_VALUE → current item (release handle path per minivm)
<loop body uses item>
coro ; <in> ; RESUME
BR L_top
L_end:
```

(Exact handle lifetime follows minivm coroutine semantics; `RESUME` delivers the
send-value, `CORO_DONE`/`CORO_VALUE` test/extract.)

## Exceptions & context managers

minipy uses minivm's built-in exception machinery: per-function handler tables
declared with `program.Builder.Try(start, end, catch, depth)`, `THROW` for guest
raises, and `ERROR_NEW` for error payload construction. Runtime traps and
host-function Go errors are catchable through the same handler path.

- Exception instances are structs with `__classid: int` and `message: str` as the
  leading fields. Built-in exception classes and user subclasses of `Exception`
  receive class ids from a DFS interval over the inheritance tree, so
  `except T` is two integer comparisons: `T.low <= classid <= T.high`.
- `raise E(...)` constructs the exception struct, wraps it with `ERROR_NEW`, then
  emits `THROW`. Bare `raise` inside an exception handler rethrows the active
  `types.Error`.
- `try/except/finally` lowers to minivm handler-table entries around protected
  regions. Catch blocks receive the thrown value on the operand stack at the
  handler target. `ERROR_GET` yields the guest-raised struct payload; if the
  payload is null, a single host function maps the VM trap to `ZeroDivisionError`,
  `IndexError`, `TypeError`, or `RuntimeError` and creates the same struct shape.
  Guest exception matching after that point is pure bytecode.
- `finally` blocks are emitted on every exit edge (normal, exception, return,
  break, continue) and rethrow with `THROW` when required.
- `with x as y:` desugars to `y = x.__enter__(); try: <body> finally: x.__exit__()`.

The compiler uses direct edge duplication for `finally` code and keeps minivm's
native handler-table and `THROW` path as the only unwinding mechanism.

## Statement completeness & pattern matching (M9)

### `del`

`del NAME` resolves the name to its storage class and stores the slot's minivm
uninitialized value — `types.Zero(kind)`, i.e. `REF_NULL` for ref-kind slots and
the typed zero const for scalars (minivm has no `LOCAL_DELETE`/`GLOBAL_DELETE`).
The checker marks the binding definitely-unassigned, so a later read follows
normal definite-assignment rules and becomes `UseBeforeDefinition` statically when
provable; the stored zero is only the runtime state on paths the checker cannot
prove unreachable. `del obj.attr` zeroes the struct field in place (`STRUCT_SET`);
`del d[key]` on a dict uses `MAP_DELETE`; `del lst[i]` on a list reuses the
`list.pop(i)` host (remove + left-shift) and drops the result.

### `assert`

```text
<test>
BR_IF L_ok
<message or default "AssertionError">
ERROR_NEW
THROW
L_ok:
```

`assert test, msg` evaluates `msg` only on failure. The false path uses minivm's
exception path (`ERROR_NEW`; `THROW`) so it benefits from the same interpreter/JIT
handling as other exceptions.

### `match` / `case`

Pattern matching lowers to a decision tree:

- literal/value patterns use existing equality/comparison opcodes;
- sequence/mapping/class patterns emit shape tests followed by element/field tests;
- alternatives (`p1 | p2`) branch to shared success/failure labels;
- captures bind with `LOCAL_SET`/`GLOBAL_SET` in the selected case arm only;
- guards run after a pattern succeeds and must leave an i1 bool for `BR_IF`.

The subject is evaluated once into a temp slot; sub-values are extracted into
fresh temp slots for recursive tests. Starred list captures (`[a, *rest]`) reuse an
`ARRAY_SLICE` for the rest; starred tuple captures (`(a, *rest, z)`) build the rest
`list` with `ARRAY_NEW_DEFAULT`/`STRUCT_GET`/`ARRAY_SET` (`emitTupleRestList`) since
tuple arity is static; mapping `**rest` reuses a `dictRest` host that copies the
source dict minus the consumed keys (no mutation). Class patterns need no runtime
`isinstance` — the static subject type already fixes the class — so they only
destructure fields. Current restrictions: a starred tuple rest must be homogeneous
(binds as `list[T]`; heterogeneous rest is a `TypeMismatch`), and dotted class names
(`mod.Class(...)`) are rejected as `UnsupportedFeature`. Lowering uses ordered
`BR_IF` chains in Python case order; `BR_TABLE` for dense scalar cases is a future
optimization.

## Unions, `Any` & specialization (M10)

The M10 layer lowers only the **residual** dynamic slots; anything the inference
pass resolved to a concrete type emits exactly as the static core does. This is
where "minimize types" pays off — runtime tag-checks appear only on slots that
stayed a union or `Any`.

- **Representation.** A concrete slot stays unboxed/native. An `A | B` / `Any`
  slot is a minivm `ref` holding a value boxed with its runtime tag (self-describing
  `Boxed` values; see
  [value-representation](https://github.com/siyul-park/minivm/blob/main/docs/value-representation.md#dynamic-any-values)).
- **Widen `T → A|B` / `T → Any`.** No opcode is emitted: a `ref` slot stores any
  self-describing `Boxed` verbatim, so a concrete value flows into a union/`Any`
  slot (or a `ref` parameter) directly. (`REF_NEW` is reserved for mutable
  closure cells and is *not* used here.)
- **Narrow `A|B → T` / `Any → T`.** Emit `REF_CAST <T>` — a checked cast that traps
  `TypeError` at runtime — **unless** flow analysis already proved the type, in
  which case nothing is emitted. A slot inference resolved to concrete never reaches
  this path.
- **`isinstance(x, T)`** → `REF_TEST <T>` (i32 flag) normalized to i1 via `!= 0`,
  so the `bool` result is uniformly `i1`-kinded. In the true branch the checker
  narrows `x` to `T`, so later uses need no further `REF_CAST`.
- **Union dispatch.** An operation on an un-narrowed union lowers to a tag switch: a
  `REF_TEST` chain (or jump table) selecting the per-member lowering, each arm
  operating on the unboxed member. `Optional[T]` is the two-arm case
  (`REF_IS_NULL` → null arm / `T` arm).
- **Specialization.** An unannotated parameter resolves to `Any` and the function
  is compiled **once** with a dynamic `ref` body; concrete arguments widen into
  the `ref` parameter for free at the call site, and the body recovers concrete
  members with `isinstance`/`REF_CAST`. This is the union-typed-body form.

  > Per-type monomorphization — emitting a separate `*Function` constant
  > (`f$int`, `f$str`, …) so each monomorphic call site links directly with no
  > boxing — is a **deferred** optimization. It would reduce `ref` traffic on hot
  > paths but produces the same results as the single union-typed body above.

```text
# describe(x: int | str): isinstance dispatch
LOCAL_GET <x>
REF_TEST <int>                   # i32
I32_EQZ ; BR_IF L_str
LOCAL_GET <x> ; REF_CAST <int>   # narrowed (elided if flow already proved int)
<... int arm ...>
BR L_end
L_str:
LOCAL_GET <x> ; REF_CAST <str>
<... str arm ...>
L_end:
```

## Worked example (M0)

minipy:

```python
x: int = 6
y: int = 7
print(x * y)
```

emitted minivm (entry function; `print` bound as host fn constant 0):

```text
I64_CONST 6
GLOBAL_SET 0          # x
I64_CONST 7
GLOBAL_SET 1          # y
GLOBAL_GET 0
GLOBAL_GET 1
I64_MUL
CONST_GET 0           # print host function
CALL
```

After `optimize.O1`, `6 * 7` over constants folds to `I64_CONST 42` when the
globals are not observed elsewhere. See [`../roadmap.md`](../roadmap.md) for the
hand-checkable example per milestone.
