package lexer

import (
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func lex(src string) ([]token.Token, error) {
	return Lex(strings.NewReader(src))
}

func hasCode(t *testing.T, err error, code token.Code) {
	t.Helper()
	el, ok := err.(token.ErrorList)
	require.Truef(t, ok, "expected token.ErrorList, got %T", err)
	for _, e := range el {
		if e.Code == code {
			return
		}
	}
	require.Failf(t, "missing diagnostic", "expected diagnostic %s, got %v", code, err)
}

func TestLex(t *testing.T) {
	t.Run("annotated assignment with positions", func(t *testing.T) {
		tokens, err := lex("x: int = 6")
		require.NoError(t, err)

		got := make([]token.Type, len(tokens))
		for i, token := range tokens {
			got[i] = token.Type
		}
		require.Equal(t, []token.Type{
			token.NAME, token.COLON, token.NAME, token.ASSIGN, token.INT,
			token.NEWLINE, token.EOF,
		}, got)
		require.Equal(t, token.Pos{Line: 1, Column: 1}, tokens[0].Pos)
		require.Equal(t, "x", tokens[0].Literal)
		require.Equal(t, token.Pos{Line: 1, Column: 10}, tokens[4].Pos)
	})

	t.Run("keywords distinct from names", func(t *testing.T) {
		tokens, err := lex("if True and not None or x")
		require.NoError(t, err)

		got := make([]token.Type, len(tokens))
		for i, token := range tokens {
			got[i] = token.Type
		}
		require.Equal(t, []token.Type{
			token.IF, token.TRUE, token.AND, token.NOT, token.NONE, token.OR,
			token.NAME, token.NEWLINE, token.EOF,
		}, got)
	})

	t.Run("numeric literals", func(t *testing.T) {
		cases := map[string]struct {
			typ token.Type
			lit string
		}{
			"6":            {token.INT, "6"},
			"0xFF":         {token.INT, "0xFF"},
			"0o17":         {token.INT, "0o17"},
			"0b1010":       {token.INT, "0b1010"},
			"1_000_000":    {token.INT, "1000000"},
			"3.14":         {token.FLOAT, "3.14"},
			"1e10":         {token.FLOAT, "1e10"},
			".5":           {token.FLOAT, ".5"},
			"2.":           {token.FLOAT, "2."},
			"6.022_140e23": {token.FLOAT, "6.022140e23"},
		}
		for src, want := range cases {
			tokens, err := lex(src)
			require.NoErrorf(t, err, "src=%q", src)
			require.Equalf(t, want.typ, tokens[0].Type, "src=%q", src)
			require.Equalf(t, want.lit, tokens[0].Literal, "src=%q", src)
		}
	})

	t.Run("strings decode escapes; raw and triple", func(t *testing.T) {
		plain, err := lex(`"a\nb\t\"c\""`)
		require.NoError(t, err)
		require.Equal(t, token.STRING, plain[0].Type)
		require.Equal(t, "a\nb\t\"c\"", plain[0].Literal)

		raw, err := lex(`r"a\nb"`)
		require.NoError(t, err)
		require.Equal(t, `a\nb`, raw[0].Literal)

		triple, err := lex(`"""ab"""`)
		require.NoError(t, err)
		require.Equal(t, "ab", triple[0].Literal)

		single, err := lex(`'hi'`)
		require.NoError(t, err)
		require.Equal(t, "hi", single[0].Literal)
	})

	t.Run("multi-char operators longest match", func(t *testing.T) {
		tokens, err := lex("** // <<= -> := == != >= <=")
		require.NoError(t, err)

		got := make([]token.Type, 0, len(tokens))
		for _, next := range tokens {
			if next.Type == token.NEWLINE || next.Type == token.EOF {
				continue
			}
			got = append(got, next.Type)
		}
		require.Equal(t, []token.Type{
			token.DOUBLESTAR, token.DOUBLESLASH, token.LSHIFTEQ, token.ARROW,
			token.WALRUS, token.EQ, token.NE, token.GE, token.LE,
		}, got)
	})

	t.Run("indentation emits INDENT and DEDENT", func(t *testing.T) {
		tokens, err := lex("if x:\n    pass\ny\n")
		require.NoError(t, err)

		got := make([]token.Type, len(tokens))
		for i, token := range tokens {
			got[i] = token.Type
		}
		require.Equal(t, []token.Type{
			token.IF, token.NAME, token.COLON, token.NEWLINE,
			token.INDENT, token.PASS, token.NEWLINE,
			token.DEDENT, token.NAME, token.NEWLINE,
			token.EOF,
		}, got)
	})

	t.Run("blank and comment-only lines produce no NEWLINE", func(t *testing.T) {
		tokens, err := lex("x\n\n   \n# comment\ny\n")
		require.NoError(t, err)

		got := make([]token.Type, len(tokens))
		for i, token := range tokens {
			got[i] = token.Type
		}
		require.Equal(t, []token.Type{
			token.NAME, token.NEWLINE, token.NAME, token.NEWLINE, token.EOF,
		}, got)
	})

	t.Run("implicit line joining inside brackets", func(t *testing.T) {
		tokens, err := lex("(\n  1 +\n  2\n)")
		require.NoError(t, err)

		got := make([]token.Type, len(tokens))
		for i, token := range tokens {
			got[i] = token.Type
		}
		require.Equal(t, []token.Type{
			token.LPAREN, token.INT, token.PLUS, token.INT, token.RPAREN,
			token.NEWLINE, token.EOF,
		}, got)
	})

	t.Run("explicit line joining with backslash", func(t *testing.T) {
		tokens, err := lex("1 + \\\n2")
		require.NoError(t, err)

		got := make([]token.Type, len(tokens))
		for i, token := range tokens {
			got[i] = token.Type
		}
		require.Equal(t, []token.Type{
			token.INT, token.PLUS, token.INT, token.NEWLINE, token.EOF,
		}, got)
	})

	t.Run("every operator and delimiter", func(t *testing.T) {
		src := "+ - * ** / // % << >> & | ^ ~ @ < > <= >= == != = " +
			"+= -= *= /= //= %= &= |= ^= <<= >>= **= -> ( ) [ ] { } , : . ;"
		tokens, err := lex(src)
		require.NoError(t, err)

		got := make([]token.Type, 0, len(tokens))
		for _, next := range tokens {
			if next.Type == token.NEWLINE || next.Type == token.EOF {
				continue
			}
			got = append(got, next.Type)
		}
		require.Equal(t, []token.Type{
			token.PLUS, token.MINUS, token.STAR, token.DOUBLESTAR, token.SLASH,
			token.DOUBLESLASH, token.PERCENT, token.LSHIFT, token.RSHIFT,
			token.AMP, token.PIPE, token.CARET, token.TILDE, token.AT,
			token.LT, token.GT, token.LE, token.GE, token.EQ, token.NE, token.ASSIGN,
			token.PLUSEQ, token.MINUSEQ, token.STAREQ, token.SLASHEQ, token.DOUBLESLASHEQ,
			token.PERCENTEQ, token.AMPEQ, token.PIPEEQ, token.CARETEQ,
			token.LSHIFTEQ, token.RSHIFTEQ, token.DOUBLESTAREQ, token.ARROW,
			token.LPAREN, token.RPAREN, token.LBRACKET, token.RBRACKET,
			token.LBRACE, token.RBRACE, token.COMMA, token.COLON, token.DOT, token.SEMICOLON,
		}, got)
	})

	t.Run("escape sequences decode", func(t *testing.T) {
		tokens, err := lex(`"\x41B\a\b\f\v\r\0\q"`)
		require.NoError(t, err)
		require.Equal(t, "AB\a\b\f\v\r\x00\\q", tokens[0].Literal)
	})

	t.Run("bytes literals", func(t *testing.T) {
		t.Run("prefix case variants", func(t *testing.T) {
			for _, prefix := range []string{"b", "B", "br", "Br", "bR", "BR", "rb", "Rb", "rB", "RB"} {
				tokens, err := lex(prefix + `"ab"`)
				require.NoErrorf(t, err, "prefix=%q", prefix)
				require.Equalf(t, token.BYTES, tokens[0].Type, "prefix=%q", prefix)
				require.Equalf(t, "ab", tokens[0].Literal, "prefix=%q", prefix)
			}
		})

		t.Run("single, double, and triple quotes", func(t *testing.T) {
			single, err := lex(`b'ab'`)
			require.NoError(t, err)
			require.Equal(t, "ab", single[0].Literal)

			double, err := lex(`b"ab"`)
			require.NoError(t, err)
			require.Equal(t, "ab", double[0].Literal)

			triple, err := lex("b'''a\"b'''")
			require.NoError(t, err)
			require.Equal(t, "a\"b", triple[0].Literal)
		})

		t.Run("empty bytes literal", func(t *testing.T) {
			tokens, err := lex(`b""`)
			require.NoError(t, err)
			require.Equal(t, token.BYTES, tokens[0].Type)
			require.Equal(t, "", tokens[0].Literal)
		})

		t.Run("byte payloads via hex escape", func(t *testing.T) {
			cases := map[string]byte{
				`b"\x00"`: 0x00,
				`b"\x7f"`: 0x7f,
				`b"\x80"`: 0x80,
				`b"\xff"`: 0xff,
			}
			for src, want := range cases {
				tokens, err := lex(src)
				require.NoErrorf(t, err, "src=%q", src)
				require.Lenf(t, tokens[0].Literal, 1, "src=%q", src)
				require.Equalf(t, want, tokens[0].Literal[0], "src=%q", src)
			}
		})

		t.Run("simple escapes decode", func(t *testing.T) {
			tokens, err := lex(`b"a\nb\t\\c"`)
			require.NoError(t, err)
			require.Equal(t, "a\nb\t\\c", tokens[0].Literal)
		})

		t.Run("invalid hex escape keeps unknown-escape rule for the bad digit", func(t *testing.T) {
			_, err := lex(`b"\xzz"`)
			require.Error(t, err)
		})

		t.Run("truncated hex escape", func(t *testing.T) {
			_, err := lex(`b"\x4"`)
			require.Error(t, err)
			hasCode(t, err, token.LexError)
		})

		t.Run("raw bytes preserve backslashes", func(t *testing.T) {
			tokens, err := lex(`rb"a\nb"`)
			require.NoError(t, err)
			require.Equal(t, `a\nb`, tokens[0].Literal)
		})

		t.Run("direct non-ASCII character rejected", func(t *testing.T) {
			_, err := lex("b\"café\"")
			require.Error(t, err)
			hasCode(t, err, token.LexError)
		})

		t.Run("u and U escapes are not Unicode-decoded", func(t *testing.T) {
			tokens, err := lex(`b"A\U00000041"`)
			require.NoError(t, err)
			require.Equal(t, `A\U00000041`, tokens[0].Literal)
		})

		t.Run("ordinary strings and f-strings unaffected", func(t *testing.T) {
			str, err := lex(`"hi"`)
			require.NoError(t, err)
			require.Equal(t, token.STRING, str[0].Type)
			require.Equal(t, "hi", str[0].Literal)

			fstr, err := lex(`f"x={x}"`)
			require.NoError(t, err)
			require.Equal(t, token.FSTRING, fstr[0].Type)
			require.Equal(t, "x={x}", fstr[0].Literal)
		})
	})
}

func TestLexer_Next(t *testing.T) {
	t.Run("yields tokens one at a time then repeats EOF", func(t *testing.T) {
		l := New(strings.NewReader("x = 1"))
		var got []token.Type
		for {
			next := l.Next()
			got = append(got, next.Type)
			if next.Type == token.EOF {
				break
			}
		}
		require.Equal(t, []token.Type{
			token.NAME, token.ASSIGN, token.INT, token.NEWLINE, token.EOF,
		}, got)
		require.NoError(t, l.Err())
		require.Equal(t, token.EOF, l.Next().Type) // EOF is idempotent
	})

	t.Run("Err surfaces diagnostics after scanning", func(t *testing.T) {
		l := New(strings.NewReader("3j"))
		for l.Next().Type != token.EOF {
		}
		require.Error(t, l.Err())
	})
}

func TestLexErrors(t *testing.T) {
	t.Run("tab in indentation", func(t *testing.T) {
		_, err := lex("if x:\n\tpass")
		require.Error(t, err)
		hasCode(t, err, token.LexError)
		require.Contains(t, err.Error(), "tab")
	})

	t.Run("integer overflow", func(t *testing.T) {
		_, err := lex("99999999999999999999999")
		hasCode(t, err, token.IntOverflow)
	})

	t.Run("imaginary literal rejected", func(t *testing.T) {
		_, err := lex("3j")
		require.Error(t, err)
		require.Contains(t, err.Error(), "complex")
	})

	t.Run("unterminated string", func(t *testing.T) {
		_, err := lex(`"abc`)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unterminated")
	})

	t.Run("unicode prefix unsupported", func(t *testing.T) {
		_, err := lex(`u"x"`)
		hasCode(t, err, token.UnsupportedFeature)
	})

	t.Run("f-string token", func(t *testing.T) {
		tokens, err := lex(`f"x={x}"`)
		require.NoError(t, err)
		require.Equal(t, token.FSTRING, tokens[0].Type)
		require.Equal(t, "x={x}", tokens[0].Literal)
	})

	t.Run("lone bang is illegal", func(t *testing.T) {
		_, err := lex("a ! b")
		hasCode(t, err, token.LexError)
	})

	t.Run("unexpected character", func(t *testing.T) {
		_, err := lex("$")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected")
	})

	t.Run("unindent mismatch", func(t *testing.T) {
		_, err := lex("if a:\n        b\n    c\n")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unindent")
	})

	t.Run("truncated hex escape", func(t *testing.T) {
		_, err := lex(`"\x4"`)
		require.Error(t, err)
	})
}
