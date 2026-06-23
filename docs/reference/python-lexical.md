# Python Lexical Analysis (reference)

> Upstream: <https://docs.python.org/3.13/reference/lexical_analysis.html> · captured 2026-06-23.
> Full Python. minipy's lexer subset is in [`../spec/01-lexical.md`](../spec/01-lexical.md).

## Line structure

- **Logical line** — terminated by the `NEWLINE` token; built from one or more
  physical lines via line joining.
- **Physical line** — characters ended by `LF`, `CR LF`, or `CR` (all equivalent).
- **Comments** — `#` outside a string literal to end of physical line; signify a
  logical-line end unless implicit joining applies.
- **Encoding** — default UTF-8; optional `coding[=:]\s*([-\w.]+)` declaration on
  line 1 or 2; UTF-8 BOM ignored.
- **Explicit line joining** — a physical line ending in `\` (not in string/comment)
  joins with the next; the `\` and newline are removed.
- **Implicit line joining** — expressions inside `() [] {}` span lines without `\`;
  may carry comments; no `NEWLINE` emitted between continuation lines.
- **Blank lines** — only whitespace ± comment: ignored, no `NEWLINE` token.

## Indentation → INDENT / DEDENT

- Leading whitespace of a logical line sets its indentation level and groups
  statements into blocks.
- Tabs expand to the next multiple of 8 (1–8 spaces, left to right, Unix rule).
- Mixing tabs and spaces such that meaning depends on tab width raises `TabError`.
- Token algorithm:
  1. Indent stack starts `[0]` (bottom never popped); strictly increasing.
  2. At each logical line start, compare indentation to stack top:
     - **equal** → no token;
     - **larger** → push, emit `INDENT`;
     - **smaller** → must equal some stack value; pop larger values, emitting one
       `DEDENT` per pop.
  3. At EOF, emit a `DEDENT` for each remaining value > 0.
- Whitespace between tokens (space/tab/formfeed) is interchangeable except at line
  start and inside strings; required only when removing it would merge tokens.

## Identifiers and keywords

```text
identifier   ::= xid_start xid_continue*
id_start     ::= Lu | Ll | Lt | Lm | Lo | Nl | "_" | Other_ID_Start
id_continue  ::= id_start | Mn | Mc | Nd | Pc | Other_ID_Continue
```

- Unlimited length, case-sensitive, normalized to NFKC.

**Reserved keywords:**

```text
False  await   else     import   pass
None   break   except   in       raise
True   class   finally  is       return
and    continue for     lambda   try
as     def     from     nonlocal while
assert del     global   not      with
async  elif    if       or       yield
```

**Soft keywords:** `match`, `case`, `_`, `type` (context-dependent, parser-level).

**Reserved identifier classes:** `_*` (not `import *`), `_` (wildcard/last result),
`__*__` (dunder, system), `__*` (class-private name mangling).

## Numeric literals

```text
integer    ::= decinteger | bininteger | octinteger | hexinteger
decinteger ::= nonzerodigit (["_"] digit)* | "0"+ (["_"] "0")*
bininteger ::= "0" ("b"|"B") (["_"] bindigit)+
octinteger ::= "0" ("o"|"O") (["_"] octdigit)+
hexinteger ::= "0" ("x"|"X") (["_"] hexdigit)+

floatnumber   ::= pointfloat | exponentfloat
pointfloat    ::= [digitpart] fraction | digitpart "."
exponentfloat ::= (digitpart | pointfloat) exponent
digitpart     ::= digit (["_"] digit)*
fraction      ::= "." digitpart
exponent      ::= ("e"|"E") ["+"|"-"] digitpart

imagnumber ::= (floatnumber | digitpart) ("j"|"J")
```

- Underscores group digits (3.6+). No leading zeros in nonzero decimals.
- Integers are unbounded in CPython (arbitrary precision). **minipy restricts
  `int` to int64** — see `../spec/02-types.md`.
- `imagnumber` (complex) is **out of scope** for minipy.

## String / bytes literals

```text
stringprefix ::= "r"|"u"|"R"|"U"|"f"|"F"|"fr"|"Fr"|"fR"|"FR"|"rf"|"rF"|"Rf"|"RF"
bytesprefix  ::= "b"|"B"|"br"|"Br"|"bR"|"BR"|"rb"|"rB"|"Rb"|"RB"
shortstring  ::= "'" shortstringitem* "'" | '"' shortstringitem* '"'
longstring   ::= "'''" longstringitem* "'''" | '"""' longstringitem* '"""'
```

- Quote forms: `'…'`, `"…"`, `'''…'''`, `"""…"""`.
- Adjacent literals concatenate at compile time (`"a" "b"` → `"ab"`).
- Escapes (strings & bytes): `\\ \' \" \a \b \f \n \r \t \v \ooo \xhh` and line
  continuation `\<newline>`. String-only: `\N{name} \uxxxx \Uxxxxxxxx`.
- Raw (`r`) keeps backslashes literal.

### f-strings

```text
f_string          ::= (literal_char | "{{" | "}}" | replacement_field)*
replacement_field ::= "{" f_expression ["="] ["!" conversion] [":" format_spec] "}"
conversion        ::= "s" | "r" | "a"
```

- Expressions evaluated at runtime, left to right; `{{`/`}}` are literal braces.

## Operators

```text
+   -   *   **  /   //  %   @
<<  >>  &   |   ^   ~   :=
<   >   <=  >=  ==  !=
```

## Delimiters

```text
(  )  [  ]  {  }  ,  :  !  .  ;  @  =  ->
+=  -=  *=  /=  //=  %=  @=  &=  |=  ^=  >>=  <<=  **=
```

- `$ ? ` and backtick are errors outside strings/comments.
