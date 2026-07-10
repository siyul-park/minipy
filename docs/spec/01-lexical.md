# Lexical Structure

Tokenization, indentation, literal scanning, and lexical diagnostics for minipy
source text.

## When to Read

Read this when changing the lexer, adding or removing token kinds, changing
literal syntax, or diagnosing whitespace and indentation behavior.

For syntax built from these tokens, read `03-grammar.md`. For unsupported syntax
that still tokenizes successfully, read `04-static-semantics.md`.

## Source of Truth

| Concern | Source |
|---|---|
| token kinds and spelling | `token/token.go` |
| scanner behavior | `lexer/lexer.go` |
| parser use of tokens | `parser/parser.go` |
| grammar built from tokens | `docs/spec/03-grammar.md` |
| diagnostics | `token/error.go` |

## Summary

The lexer is an indentation-aware, rune-based scanner over an `io.Reader`. It
emits one token at a time and accumulates lexical diagnostics instead of failing
on the first malformed token.

minipy follows Python-like lexical rules where they fit the implemented subset,
but it is stricter about some whitespace and literal forms.

## Structure Tokens

The token stream includes:

- `NEWLINE` for logical line endings outside parentheses, brackets, and braces.
- `INDENT` and `DEDENT` for indentation changes at the start of logical lines.
- `EOF`, rendered as the Python `ENDMARKER` concept in docs.

Blank lines and comment-only lines do not emit `NEWLINE`. A final `NEWLINE` is
emitted at end-of-file if the last physical line contained tokens, followed by
open `DEDENT`s and a single `EOF`.

Tabs in leading indentation are rejected. Spaces are counted one column at a time;
form feed is skipped in indentation measurement.

## Comments and Whitespace

- `#` starts a comment outside strings and runs to the physical line ending.
- Spaces, tabs, and form feed inside a logical line separate tokens.
- A backslash followed immediately by a physical newline joins the physical lines.
- A bare backslash elsewhere is a lexical error.
- Newlines inside `()`, `[]`, and `{}` do not emit `NEWLINE`.

## Tokens

The actual token kinds are defined in `token/token.go`. Literals are split into
`INT`, `FLOAT`, `STRING`, `FSTRING`, and `BYTES`; f-strings are represented by one
`FSTRING` token whose literal text is parsed later by the parser.

Identifiers use Unicode letters, Unicode digits after the first rune, and `_`.
The lexer recognizes the full Python reserved keyword set so a reserved word can
never be used as an identifier. Soft keywords such as `match`, `case`, and `type`
are lexed as `NAME` and interpreted by the parser only in statement positions
where they introduce a supported form.

## Keywords

Reserved keyword tokens are:

```text
True False None
and or not if elif else while for in break continue pass
def return class lambda global nonlocal yield try except finally raise
with as import from is del assert async await
```

The composite comparison spellings `not in` and `is not` are represented by
comparison operator tokens after parsing their two-token source forms.

## Operators and Delimiters

The lexer recognizes:

```text
+ - * ** / // % << >> & | ^ ~ @
< > <= >= == != = += -= *= /= //= %= &= |= ^= <<= >>= **=
:= -> ( ) [ ] { } , : . ;
```

Support is phase-specific:

- `:=` is parsed as a named expression when the target is a name.
- `@` is tokenized and can appear in expression syntax, but matrix-multiply
  semantics are not implemented by the checker/operator layer.
- `->` is accepted only in function return annotations.

## Numeric Literals

Supported numeric literals are bounded to minipy runtime types:

- decimal, binary (`0b`), octal (`0o`), and hexadecimal (`0x`) integer literals
- underscores in digit sequences
- decimal floating-point literals with optional exponent

Integers must fit in signed 64 bits. Floats parse as IEEE-754 `float64`.
Imaginary literals (`1j`) are rejected because minipy has no complex type.

## String Literals

The lexer supports:

- single-quoted, double-quoted, and triple-quoted strings
- raw strings with `r`/`R`
- f-strings with `f`/`F`
- bytes literals with `b`/`B`, and raw bytes with any of `br`, `Br`, `bR`, `BR`,
  `rb`, `Rb`, `rB`, `RB`
- adjacent plain string concatenation in the parser; adjacent bytes literals
  concatenate the same way but a `STRING` and a `BYTES` literal may not be
  adjacent (`token.SyntaxError`, "cannot mix bytes and nonbytes literals")

`u`/`U` is recognized only to diagnose that the unicode prefix form is
unsupported (`token.UnsupportedFeature`). It still scans the following string
so parsing can continue. `u`/`U` cannot combine with `r`, `f`, or `b`; `f` and
`b` cannot combine with each other. Any other prefix letter combination, or a
prefix longer than two letters, is not recognized as a string prefix.

A bytes literal (`b"..."`) is scanned like a string but produces a `BYTES`
token instead of `STRING`:

- every directly written character must be ASCII; a non-ASCII character
  written literally (not via an escape) is a `token.LexError`
- in non-raw bytes literals, `\xhh` decodes to a single raw byte (not a UTF-8
  encoded rune); `\uhhhh` and `\Uhhhhhhhh` are **not** Unicode escapes in bytes
  mode — they are left undecoded (backslash and letter kept literally), same
  as any other unrecognized escape
- raw bytes literals (`rb"..."`/`br"..."` and case variants) preserve
  backslashes exactly like raw strings

Escapes decoded by the lexer in (non-raw) string and bytes literals are:

```text
\n \t \r \\ \' \" \0 \a \b \f \v \xhh \uhhhh \Uhhhhhhhh
```

`\uhhhh` and `\Uhhhhhhhh` apply only outside bytes mode, as noted above.
Unknown escapes are preserved as a backslash plus the escaped character, matching
Python's lenient behavior for ordinary strings. Named Unicode escapes
(`\N{...}`) are not implemented.

## F-strings

An f-string is lexed as a single decoded `FSTRING` token. The parser later splits
it into literal and replacement-field parts.

Supported replacement-field features:

- `{expr}`
- debug text such as `{x=}` and `{x = }`
- conversions `!s`, `!r`, and `!a`
- format specs, including one level of nested replacement fields inside the spec

Unsupported or invalid f-string constructs are reported as syntax or unsupported
feature diagnostics during parsing/checking.

## Encoding

The lexer skips a leading UTF-8 byte-order mark (`U+FEFF`) if present. It does not
implement Python source encoding cookies; input is expected to be UTF-8 text as
supplied by the caller.

## Related Docs

- `docs/README.md` — documentation map and ownership guide.
- `docs/spec/03-grammar.md` — grammar built from lexer tokens.
- `docs/spec/04-static-semantics.md` — checker diagnostics for unsupported forms.
- `docs/compatibility.md` — user-facing lexical and syntax compatibility status.
