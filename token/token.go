// Package token defines the lexical tokens of minipy source — a strict subset
// of CPython 3.13 (docs/spec/01-lexical.md). Every reserved Python keyword is
// recognized so a program can never use one as a name; keywords outside the
// implemented grammar are rejected later by the parser.
package token

import "strconv"

// Type classifies a lexical token.
type Type int

// Pos is a 1-based source position (line and column in runes).
type Pos struct {
	Line   int
	Column int
}

// Token is a single lexical unit: its kind, the source text it was scanned
// from, and the position of its first rune.
type Token struct {
	Type    Type
	Literal string
	Pos     Pos
}

const (
	ILLEGAL Type = iota
	EOF          // ENDMARKER

	NEWLINE
	INDENT
	DEDENT

	NAME
	INT
	FLOAT
	STRING
	FSTRING

	// Keywords — the full Python reserved set (docs/spec/01-lexical.md).
	keywordBeg
	TRUE
	FALSE
	NONE
	AND
	OR
	NOT
	IF
	ELIF
	ELSE
	WHILE
	FOR
	IN
	NOTIN
	ISNOT
	BREAK
	CONTINUE
	PASS
	DEF
	RETURN
	CLASS
	LAMBDA
	GLOBAL
	NONLOCAL
	YIELD
	TRY
	EXCEPT
	FINALLY
	RAISE
	WITH
	AS
	IMPORT
	FROM
	IS
	DEL
	ASSERT
	ASYNC
	AWAIT
	keywordEnd

	// Operators and delimiters.
	PLUS          // +
	MINUS         // -
	STAR          // *
	DOUBLESTAR    // **
	SLASH         // /
	DOUBLESLASH   // //
	PERCENT       // %
	LSHIFT        // <<
	RSHIFT        // >>
	AMP           // &
	PIPE          // |
	CARET         // ^
	TILDE         // ~
	AT            // @ (lexed, rejected at parse)
	LT            // <
	GT            // >
	LE            // <=
	GE            // >=
	EQ            // ==
	NE            // !=
	ASSIGN        // =
	PLUSEQ        // +=
	MINUSEQ       // -=
	STAREQ        // *=
	SLASHEQ       // /=
	DOUBLESLASHEQ // //=
	PERCENTEQ     // %=
	AMPEQ         // &=
	PIPEEQ        // |=
	CARETEQ       // ^=
	LSHIFTEQ      // <<=
	RSHIFTEQ      // >>=
	DOUBLESTAREQ  // **=
	WALRUS        // := (lexed, rejected at parse)
	ARROW         // ->
	LPAREN        // (
	RPAREN        // )
	LBRACKET      // [
	RBRACKET      // ]
	LBRACE        // {
	RBRACE        // }
	COMMA         // ,
	COLON         // :
	DOT           // .
	SEMICOLON     // ;
)

var names = map[Type]string{
	ILLEGAL: "ILLEGAL",
	EOF:     "EOF",
	NEWLINE: "NEWLINE",
	INDENT:  "INDENT",
	DEDENT:  "DEDENT",
	NAME:    "NAME",
	INT:     "INT",
	FLOAT:   "FLOAT",
	STRING:  "STRING",
	FSTRING: "FSTRING",

	TRUE:     "True",
	FALSE:    "False",
	NONE:     "None",
	AND:      "and",
	OR:       "or",
	NOT:      "not",
	IF:       "if",
	ELIF:     "elif",
	ELSE:     "else",
	WHILE:    "while",
	FOR:      "for",
	IN:       "in",
	NOTIN:    "not in",
	ISNOT:    "is not",
	BREAK:    "break",
	CONTINUE: "continue",
	PASS:     "pass",
	DEF:      "def",
	RETURN:   "return",
	CLASS:    "class",
	LAMBDA:   "lambda",
	GLOBAL:   "global",
	NONLOCAL: "nonlocal",
	YIELD:    "yield",
	TRY:      "try",
	EXCEPT:   "except",
	FINALLY:  "finally",
	RAISE:    "raise",
	WITH:     "with",
	AS:       "as",
	IMPORT:   "import",
	FROM:     "from",
	IS:       "is",
	DEL:      "del",
	ASSERT:   "assert",
	ASYNC:    "async",
	AWAIT:    "await",

	PLUS:          "+",
	MINUS:         "-",
	STAR:          "*",
	DOUBLESTAR:    "**",
	SLASH:         "/",
	DOUBLESLASH:   "//",
	PERCENT:       "%",
	LSHIFT:        "<<",
	RSHIFT:        ">>",
	AMP:           "&",
	PIPE:          "|",
	CARET:         "^",
	TILDE:         "~",
	AT:            "@",
	LT:            "<",
	GT:            ">",
	LE:            "<=",
	GE:            ">=",
	EQ:            "==",
	NE:            "!=",
	ASSIGN:        "=",
	PLUSEQ:        "+=",
	MINUSEQ:       "-=",
	STAREQ:        "*=",
	SLASHEQ:       "/=",
	DOUBLESLASHEQ: "//=",
	PERCENTEQ:     "%=",
	AMPEQ:         "&=",
	PIPEEQ:        "|=",
	CARETEQ:       "^=",
	LSHIFTEQ:      "<<=",
	RSHIFTEQ:      ">>=",
	DOUBLESTAREQ:  "**=",
	WALRUS:        ":=",
	ARROW:         "->",
	LPAREN:        "(",
	RPAREN:        ")",
	LBRACKET:      "[",
	RBRACKET:      "]",
	LBRACE:        "{",
	RBRACE:        "}",
	COMMA:         ",",
	COLON:         ":",
	DOT:           ".",
	SEMICOLON:     ";",
}

var keywords = func() map[string]Type {
	m := make(map[string]Type, keywordEnd-keywordBeg)
	for t := keywordBeg + 1; t < keywordEnd; t++ {
		m[names[t]] = t
	}
	return m
}()

// Lookup returns the keyword Type for an identifier, or NAME if it is not a
// reserved word.
func Lookup(ident string) Type {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return NAME
}

// IsKeyword reports whether t is a reserved keyword.
func (t Type) IsKeyword() bool {
	return keywordBeg < t && t < keywordEnd
}

// String returns the mnemonic for a token type.
func (t Type) String() string {
	if s, ok := names[t]; ok {
		return s
	}
	return "Type(" + strconv.Itoa(int(t)) + ")"
}

// String renders a position as "line:col".
func (p Pos) String() string {
	return strconv.Itoa(p.Line) + ":" + strconv.Itoa(p.Column)
}

// String renders a token for diagnostics.
func (t Token) String() string {
	if t.Literal != "" && t.Literal != t.Type.String() {
		return t.Type.String() + "(" + t.Literal + ")"
	}
	return t.Type.String()
}
