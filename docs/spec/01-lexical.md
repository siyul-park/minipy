# Lexical Structure

The lexer is an indentation-aware, rune-based scanner over an `io.Reader`. It
emits one token at a time and accumulates lexical diagnostics instead of failing
on the first malformed token.

minipy follows Python-like lexical rules where they fit the implemented subset,
but it is stricter about some whitespace and literal forms.

## Structure tokens

The token stream includes:

- `NEWLINE` for logical line endings outside parentheses, brackets, and braces.
- `INDENT` and `DEDENT` for indentation changes at the start of logical lines.
- `EOF`, rendered as the Python `ENDMARKER` concept in docs.

Blank lines and comment-only lines do not emit `NEWLINE`. A final `NEWLINE` is
emitted at end-of-file if the last physical line contained tokens, followed by
open `DEDENT`s and a single `EOF`.

Tabs in leading indentation are rejected. Spaces are counted one column at a
time; form feed is skipped in indentation measurement.

## Comments and whitespace

- `#` starts a comment outside strings and runs to the physical line ending.
- Spaces, tabs, and form feed inside a logical line separate tokens.
- A backslash followed immediately by a physical newline joins the physical lines.
- A bare backslash elsewhere is a lexical error.
- Newlines inside `()`, `[]`, and `{}` do not emit `NEWLINE`.

## Tokens

The actual token kinds are defined in `token/token.go`. Literals are split into
`INT`, `FLOAT`, `STRING`, and `FSTRING`; f-strings are represented by one `FSTRING`
token whose literal text is parsed later by the parser.

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

## Operators and delimiters

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

## Numeric literals

Supported numeric literals are bounded to minipy runtime types:

- decimal, binary (`0b`), octal (`0o`), and hexadecimal (`0x`) integer literals
- underscores in digit sequences
- decimal floating-point literals with optional exponent

Integers must fit in signed 64 bits. Floats parse as IEEE-754 `float64`.
Imaginary literals (`1j`) are rejected because minipy has no complex type.

## String literals

The lexer supports:

- single-quoted, double-quoted, and triple-quoted strings
- raw strings with `r`/`R`
- f-strings with `f`/`F`
- adjacent plain string concatenation in the parser

`b`/`B` and `u`/`U` prefixes are recognized only to diagnose that bytes/unicode
prefix forms are unsupported. They still scan the following string so parsing can
continue.

Escapes decoded by the lexer are:

```text
\n \t \r \\ \' \" \0 \a \b \f \v \xhh \uhhhh \Uhhhhhhhh
```

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

The lexer skips a leading UTF-8 byte-order mark (`U+FEFF`) if present. It does
not implement Python source encoding cookies; input is expected to be UTF-8 text
as supplied by the caller.
