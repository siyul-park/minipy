# minipy — Lexical Structure

The minipy lexer is a strict subset of CPython 3.13's
([reference](../reference/python-lexical.md)). Same tokens, same indentation
rules; fewer literal forms. A minipy source file is UTF-8.

## Lines, comments, joining

- Source is UTF-8 (no encoding-declaration cookie support; a leading UTF-8 BOM is
  ignored). Physical line endings `LF`, `CR LF`, `CR` are all accepted.
- `#` begins a comment to end of line.
- **Explicit** line joining with trailing `\` and **implicit** joining inside
  `() [] {}` work exactly as in Python.
- Blank/whitespace-only lines produce no `NEWLINE`.

## Indentation (INDENT / DEDENT)

Identical to Python: leading whitespace drives an indent stack that emits
`INDENT`/`DEDENT` tokens (algorithm in [reference](../reference/python-lexical.md#indentation--indent--dedent)).

minipy is **stricter** about whitespace:

- **Spaces only.** A tab in leading indentation is a `LexError` (no tab→space
  expansion, no `TabError` ambiguity). Recommended unit: 4 spaces.
- Mixed tab/space indentation never occurs because tabs are rejected outright.

## Tokens

```text
NAME NUMBER STRING FSTRING_START FSTRING_MIDDLE FSTRING_END
NEWLINE INDENT DEDENT ENDMARKER
<keyword>  <operator>  <delimiter>
```

### Identifiers

`xid_start xid_continue*`, NFKC-normalized, case-sensitive — same as Python.
Dunder names (`__x__`) are allowed only where the spec defines them (e.g.
`__init__`); other dunders are reserved and rejected (`UnsupportedFeature`).

### Keywords

minipy reserves the **same** keyword set as Python (so a program never
accidentally uses one as a name), but several keywords are **not yet accepted by
the grammar** and produce `UnsupportedFeature` until their milestone:

| Status | Keywords |
|---|---|
| Supported (by milestone) | `False True None and or not if elif else while for in break continue pass def return` (M0–M2); `class` (M5); `lambda nonlocal global` (M4); `yield` (M6); `try except finally raise with as` (M7); `import from` (M8) |
| Reserved, **rejected** | `async await` (no async core); `del assert` (deferred); `is` (identity — deferred, see note) |

Soft keywords `match`/`case`/`type` are **not** part of minipy (no structural
pattern matching, no `type` aliases via statement; use annotations).

> `is` / `is not`: identity comparison is deferred. Use `x is None` only once M7
> defines `None`-identity; until then `== None` is rejected in favor of a future
> `is None` form. (Tracked in [`../roadmap.md`](../roadmap.md).)

### Operators

All Python operators are lexed. `@` (matrix multiply) and `:=` (walrus) are
**lexed but rejected** by the grammar (`UnsupportedFeature`).

```text
+   -   *   **  /   //  %
<<  >>  &   |   ^   ~
<   >   <=  >=  ==  !=
```

### Delimiters

```text
(  )  [  ]  {  }  ,  :  .  ;  =  ->
+=  -=  *=  /=  //=  %=  &=  |=  ^=  >>=  <<=  **=
```

`;` (multiple simple statements on one line) is lexed; the grammar accepts it
(M0) but the style guide discourages it.

## Literals

### Numeric

- **Integer** — decimal, `0x`/`0o`/`0b`, underscores allowed. Value must fit
  signed 64-bit at compile time, else `IntOverflow`. (`int` = int64.)
- **Float** — point/exponent forms with underscores → minivm `f64`.
- **Imaginary** (`j`) — **rejected** (no `complex`).

```python
0          255    0xFF    0o17    0b1010    1_000_000     # int
3.14       1e10   .5      2.       6.022_140_76e23        # float
3.14j                                                     # ERROR: no complex
```

### Boolean / None

`True`, `False` → `bool`; `None` → `NoneType`. (Atoms, not literals lexically, but
listed here for completeness.)

### Strings

- Quote forms: `'…' "…" '''…''' """…"""`.
- Prefixes: plain, `r`/`R` (raw), `f`/`F` (f-string). **`b`/`B` (bytes) and
  `u`/`U` are rejected** in M0–M2; `bytes` arrives later (see roadmap).
- Standard escape sequences (`\n \t \\ \" \' \xhh \uXXXX \UXXXXXXXX \N{…}`).
- **Adjacent string concatenation** (`"a" "b"` → `"ab"`) is supported at compile
  time.
- A `str` is a sequence of Unicode codepoints, mapped to minivm `String`
  (UTF-32, interned, immutable) — see [`02-types.md`](02-types.md).

### f-strings

Supported subset (M3): `f"{expr}"`, `f"{expr!r}"`/`!s`/`!a`, and `:format_spec`.
The embedded `expr` must be a supported minipy expression of a type with a known
string conversion. `{expr=}` debug form and nested replacement fields beyond one
level are deferred.

## What the lexer rejects (summary)

| Form | Result |
|---|---|
| tab in indentation | `LexError` |
| imaginary literal `…j` | `LexError` |
| bytes/`u` string prefix (pre-roadmap) | `UnsupportedFeature` |
| integer literal not fitting int64 | `IntOverflow` |
| `@`, `:=` | lexed, rejected at parse |
