// Package lexer turns minipy source into a token stream, including the
// INDENT/DEDENT/NEWLINE/ENDMARKER structure tokens (docs/spec/01-lexical.md).
// minipy is stricter than CPython about whitespace: tabs in leading indentation
// are rejected outright.
//
// The lexer reads runes incrementally from an io.Reader and yields one token per
// Next call. Lex is a convenience that drains Next into a slice.
package lexer

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/siyul-park/minipy/token"
)

// Lexer scans source read from an io.Reader into tokens, one Next at a time.
type Lexer struct {
	r   *bufio.Reader
	buf []rune // runes read so far; indexed by pos
	pos int

	readEOF bool
	started bool
	done    bool

	line int
	col  int

	indents     []int
	parens      int
	atLineStart bool
	lineHasTok  bool

	pending []token.Token
	errs    token.ErrorList
}

// eofRune marks "no more input" from the rune accessors.
const eofRune = rune(-1)

// bom is the UTF-8 byte-order mark, skipped if it leads the input.
const bom = '\uFEFF'

var stringPrefixes = map[string]struct{}{
	"r": {}, "R": {}, "f": {}, "F": {}, "b": {}, "B": {}, "u": {}, "U": {},
}

// New returns a Lexer over r.
func New(r io.Reader) *Lexer {
	return &Lexer{
		r:           bufio.NewReader(r),
		line:        1,
		col:         1,
		indents:     []int{0},
		atLineStart: true,
	}
}

// Lex scans r and returns every token through ENDMARKER. A non-nil error is a
// token.ErrorList holding every lexical diagnostic found.
func Lex(r io.Reader) ([]token.Token, error) {
	l := New(r)
	var toks []token.Token
	for {
		tok := l.Next()
		toks = append(toks, tok)
		if tok.Type == token.EOF {
			return toks, l.Err()
		}
	}
}

// Next returns the next token, ending with a single ENDMARKER (EOF) token.
func (l *Lexer) Next() token.Token {
	for len(l.pending) == 0 && !l.done {
		l.step()
	}
	if len(l.pending) == 0 {
		return token.Token{Type: token.EOF, Pos: l.here()}
	}
	tok := l.pending[0]
	l.pending = l.pending[1:]
	return tok
}

// Err returns the accumulated lexical diagnostics, or nil if there were none.
func (l *Lexer) Err() error {
	return l.errs.Err()
}

// step advances the scanner by one unit of work, queuing zero or more tokens.
func (l *Lexer) step() {
	if !l.started {
		l.started = true
		if l.cur() == bom {
			l.pos++
		}
	}
	if l.eof() {
		l.finish()
		return
	}
	if l.atLineStart && l.parens == 0 {
		if l.scanIndent() {
			return
		}
		l.atLineStart = false
		l.lineHasTok = false
		return
	}

	c := l.cur()
	switch {
	case c == ' ' || c == '\f' || c == '\t':
		l.pos++
		l.col++
	case c == '\\':
		if r := l.at(1); r == '\n' || r == '\r' {
			l.pos++
			l.col++
			l.consumeNewline()
		} else {
			l.errs.Add(l.here(), token.LexError, "unexpected character %q", string(c))
			l.pos++
			l.col++
		}
	case c == '#':
		l.skipComment()
	case c == '\n' || c == '\r':
		l.consumeNewline()
		if l.parens == 0 {
			if l.lineHasTok {
				l.add(token.NEWLINE, "", l.here())
			}
			l.atLineStart = true
			l.lineHasTok = false
		}
	case isNameStart(c):
		l.scanNameOrString()
		l.lineHasTok = true
	case isDigit(c) || (c == '.' && isDigit(l.at(1))):
		l.scanNumber()
		l.lineHasTok = true
	case c == '\'' || c == '"':
		l.scanString(false, false, false)
		l.lineHasTok = true
	default:
		l.scanOperator()
		l.lineHasTok = true
	}
}

// finish emits the trailing NEWLINE, any open DEDENTs, and ENDMARKER.
func (l *Lexer) finish() {
	if l.lineHasTok {
		l.add(token.NEWLINE, "", l.here())
		l.lineHasTok = false
	}
	for len(l.indents) > 1 {
		l.indents = l.indents[:len(l.indents)-1]
		l.add(token.DEDENT, "", l.here())
	}
	l.add(token.EOF, "", l.here())
	l.done = true
}

// scanIndent measures the leading whitespace of a logical line and queues
// INDENT/DEDENT against the indent stack. It reports blank == true for a blank
// or comment-only line (which produces no structure tokens).
func (l *Lexer) scanIndent() (blank bool) {
	width := 0
	for {
		switch l.cur() {
		case ' ':
			width++
			l.pos++
			l.col++
		case '\f':
			l.pos++
			l.col++
		case '\t':
			l.errs.Add(l.here(), token.LexError, "tab in indentation; minipy requires spaces")
			l.pos++
			l.col++
		default:
			goto measured
		}
	}
measured:
	if c := l.cur(); c == eofRune || c == '\n' || c == '\r' || c == '#' {
		if c == '#' {
			l.skipComment()
		}
		if r := l.cur(); r == '\n' || r == '\r' {
			l.consumeNewline()
		}
		return true
	}

	top := l.indents[len(l.indents)-1]
	switch {
	case width > top:
		l.indents = append(l.indents, width)
		l.add(token.INDENT, "", l.here())
	case width < top:
		for len(l.indents) > 1 && width < l.indents[len(l.indents)-1] {
			l.indents = l.indents[:len(l.indents)-1]
			l.add(token.DEDENT, "", l.here())
		}
		if l.indents[len(l.indents)-1] != width {
			l.errs.Add(l.here(), token.LexError, "unindent does not match any outer indentation level")
		}
	}
	return false
}

// scanNameOrString reads an identifier/keyword, or a prefixed string when the
// identifier is a string prefix immediately followed by a quote.
func (l *Lexer) scanNameOrString() {
	pos := l.here()
	start := l.pos
	for isNameContinue(l.cur()) {
		l.pos++
		l.col++
	}
	word := string(l.buf[start:l.pos])

	if _, ok := stringPrefixes[word]; ok && (l.cur() == '\'' || l.cur() == '"') {
		raw := word == "r" || word == "R"
		fstr := word == "f" || word == "F"
		bytes := word == "b" || word == "B" || word == "u" || word == "U"
		l.scanStringAt(pos, raw, fstr, bytes)
		return
	}
	l.add(token.Lookup(word), word, pos)
}

// scanNumber reads an int or float literal (docs/spec/01-lexical.md#numeric).
func (l *Lexer) scanNumber() {
	pos := l.here()
	start := l.pos

	if l.cur() == '0' {
		switch l.at(1) {
		case 'x', 'X', 'o', 'O', 'b', 'B':
			l.pos += 2
			l.col += 2
			for isHexDigit(l.cur()) || l.cur() == '_' {
				l.pos++
				l.col++
			}
			l.finishInt(start, l.pos, pos)
			return
		}
	}

	isFloat := false
	for isDigit(l.cur()) || l.cur() == '_' {
		l.pos++
		l.col++
	}
	if l.cur() == '.' {
		isFloat = true
		l.pos++
		l.col++
		for isDigit(l.cur()) || l.cur() == '_' {
			l.pos++
			l.col++
		}
	}
	if l.cur() == 'e' || l.cur() == 'E' {
		sp, sc := l.pos, l.col
		l.pos++
		l.col++
		if l.cur() == '+' || l.cur() == '-' {
			l.pos++
			l.col++
		}
		if isDigit(l.cur()) {
			isFloat = true
			for isDigit(l.cur()) || l.cur() == '_' {
				l.pos++
				l.col++
			}
		} else {
			l.pos, l.col = sp, sc
		}
	}

	end := l.pos
	if l.cur() == 'j' || l.cur() == 'J' {
		l.errs.Add(pos, token.LexError, "imaginary literals are not supported (no complex)")
		l.pos++
		l.col++
	}

	if isFloat {
		l.finishFloat(start, end, pos)
	} else {
		l.finishInt(start, end, pos)
	}
}

// scanString reads a string whose opening quote is at the cursor.
func (l *Lexer) scanString(raw, fstr, bytes bool) {
	l.scanStringAt(l.here(), raw, fstr, bytes)
}

// scanStringAt reads a string literal whose prefix (if any) started at pos and
// whose opening quote is at the cursor. Escapes are decoded unless raw.
func (l *Lexer) scanStringAt(pos token.Pos, raw, fstr, bytes bool) {
	if bytes {
		l.errs.Add(pos, token.UnsupportedFeature, "bytes/unicode string prefixes are not supported yet")
	}
	q := l.cur()
	triple := l.at(1) == q && l.at(2) == q
	if triple {
		l.pos += 3
		l.col += 3
	} else {
		l.pos++
		l.col++
	}

	var sb strings.Builder
	for {
		c := l.cur()
		if c == eofRune {
			l.errs.Add(pos, token.LexError, "unterminated string literal")
			break
		}
		if !triple && (c == '\n' || c == '\r') {
			l.errs.Add(pos, token.LexError, "unterminated string literal")
			break
		}
		if c == q {
			if !triple {
				l.pos++
				l.col++
				break
			}
			if l.at(1) == q && l.at(2) == q {
				l.pos += 3
				l.col += 3
				break
			}
			sb.WriteRune(c)
			l.pos++
			l.col++
			continue
		}
		if c == '\n' || c == '\r' {
			sb.WriteByte('\n')
			l.consumeNewline()
			continue
		}
		if c == '\\' {
			if raw {
				sb.WriteRune(c)
				l.pos++
				l.col++
				if l.cur() != eofRune {
					sb.WriteRune(l.cur())
					l.pos++
					l.col++
				}
				continue
			}
			l.pos++
			l.col++
			l.decodeEscape(&sb, pos)
			continue
		}
		sb.WriteRune(c)
		l.pos++
		l.col++
	}
	if fstr {
		l.add(token.FSTRING, sb.String(), pos)
		return
	}
	l.add(token.STRING, sb.String(), pos)
}

// decodeEscape consumes one escape sequence (the backslash is already consumed)
// and writes its decoded value to sb.
func (l *Lexer) decodeEscape(sb *strings.Builder, pos token.Pos) {
	c := l.cur()
	if c == eofRune {
		l.errs.Add(pos, token.LexError, "unterminated escape sequence")
		return
	}
	l.pos++
	l.col++
	switch c {
	case 'n':
		sb.WriteByte('\n')
	case 't':
		sb.WriteByte('\t')
	case 'r':
		sb.WriteByte('\r')
	case '\\':
		sb.WriteByte('\\')
	case '\'':
		sb.WriteByte('\'')
	case '"':
		sb.WriteByte('"')
	case '0':
		sb.WriteByte(0)
	case 'a':
		sb.WriteByte(7)
	case 'b':
		sb.WriteByte(8)
	case 'f':
		sb.WriteByte('\f')
	case 'v':
		sb.WriteByte('\v')
	case 'x':
		l.decodeHex(sb, pos, 2)
	case 'u':
		l.decodeHex(sb, pos, 4)
	case 'U':
		l.decodeHex(sb, pos, 8)
	default:
		// Unknown escape: keep the backslash and the character (Python's lenient rule).
		sb.WriteByte('\\')
		sb.WriteRune(c)
	}
}

// decodeHex reads exactly n hex digits and writes the resulting rune.
func (l *Lexer) decodeHex(sb *strings.Builder, pos token.Pos, n int) {
	l.fill(n - 1)
	if l.pos+n > len(l.buf) {
		l.errs.Add(pos, token.LexError, "truncated \\x/\\u/\\U escape")
		return
	}
	digits := string(l.buf[l.pos : l.pos+n])
	v, err := strconv.ParseUint(digits, 16, 32)
	if err != nil {
		l.errs.Add(pos, token.LexError, "invalid hex escape %q", digits)
		return
	}
	l.pos += n
	l.col += n
	sb.WriteRune(rune(v))
}

// scanOperator matches the longest operator or delimiter at the cursor.
func (l *Lexer) scanOperator() {
	pos := l.here()
	c := l.cur()
	emit := func(t token.Type, n int) {
		l.fill(n - 1)
		lit := string(l.buf[l.pos : l.pos+n])
		l.pos += n
		l.col += n
		l.add(t, lit, pos)
	}
	la := func(k int) rune { return l.at(k) }

	switch c {
	case '+':
		if la(1) == '=' {
			emit(token.PLUSEQ, 2)
		} else {
			emit(token.PLUS, 1)
		}
	case '-':
		switch {
		case la(1) == '=':
			emit(token.MINUSEQ, 2)
		case la(1) == '>':
			emit(token.ARROW, 2)
		default:
			emit(token.MINUS, 1)
		}
	case '*':
		switch {
		case la(1) == '*' && la(2) == '=':
			emit(token.DOUBLESTAREQ, 3)
		case la(1) == '*':
			emit(token.DOUBLESTAR, 2)
		case la(1) == '=':
			emit(token.STAREQ, 2)
		default:
			emit(token.STAR, 1)
		}
	case '/':
		switch {
		case la(1) == '/' && la(2) == '=':
			emit(token.DOUBLESLASHEQ, 3)
		case la(1) == '/':
			emit(token.DOUBLESLASH, 2)
		case la(1) == '=':
			emit(token.SLASHEQ, 2)
		default:
			emit(token.SLASH, 1)
		}
	case '%':
		if la(1) == '=' {
			emit(token.PERCENTEQ, 2)
		} else {
			emit(token.PERCENT, 1)
		}
	case '<':
		switch {
		case la(1) == '<' && la(2) == '=':
			emit(token.LSHIFTEQ, 3)
		case la(1) == '<':
			emit(token.LSHIFT, 2)
		case la(1) == '=':
			emit(token.LE, 2)
		default:
			emit(token.LT, 1)
		}
	case '>':
		switch {
		case la(1) == '>' && la(2) == '=':
			emit(token.RSHIFTEQ, 3)
		case la(1) == '>':
			emit(token.RSHIFT, 2)
		case la(1) == '=':
			emit(token.GE, 2)
		default:
			emit(token.GT, 1)
		}
	case '&':
		if la(1) == '=' {
			emit(token.AMPEQ, 2)
		} else {
			emit(token.AMP, 1)
		}
	case '|':
		if la(1) == '=' {
			emit(token.PIPEEQ, 2)
		} else {
			emit(token.PIPE, 1)
		}
	case '^':
		if la(1) == '=' {
			emit(token.CARETEQ, 2)
		} else {
			emit(token.CARET, 1)
		}
	case '~':
		emit(token.TILDE, 1)
	case '@':
		emit(token.AT, 1)
	case '=':
		if la(1) == '=' {
			emit(token.EQ, 2)
		} else {
			emit(token.ASSIGN, 1)
		}
	case '!':
		if la(1) == '=' {
			emit(token.NE, 2)
		} else {
			l.errs.Add(pos, token.LexError, "unexpected character %q", "!")
			l.pos++
			l.col++
		}
	case ':':
		if la(1) == '=' {
			emit(token.WALRUS, 2)
		} else {
			emit(token.COLON, 1)
		}
	case '(':
		l.parens++
		emit(token.LPAREN, 1)
	case ')':
		if l.parens > 0 {
			l.parens--
		}
		emit(token.RPAREN, 1)
	case '[':
		l.parens++
		emit(token.LBRACKET, 1)
	case ']':
		if l.parens > 0 {
			l.parens--
		}
		emit(token.RBRACKET, 1)
	case '{':
		l.parens++
		emit(token.LBRACE, 1)
	case '}':
		if l.parens > 0 {
			l.parens--
		}
		emit(token.RBRACE, 1)
	case ',':
		emit(token.COMMA, 1)
	case '.':
		emit(token.DOT, 1)
	case ';':
		emit(token.SEMICOLON, 1)
	default:
		l.errs.Add(pos, token.LexError, "unexpected character %q", string(c))
		l.pos++
		l.col++
	}
}

func (l *Lexer) finishInt(start, end int, pos token.Pos) {
	clean := strings.ReplaceAll(string(l.buf[start:end]), "_", "")
	if _, err := strconv.ParseInt(clean, 0, 64); err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			l.errs.Add(pos, token.IntOverflow, "integer literal %q exceeds int64", clean)
		} else {
			l.errs.Add(pos, token.LexError, "invalid integer literal %q", clean)
		}
	}
	l.add(token.INT, clean, pos)
}

func (l *Lexer) finishFloat(start, end int, pos token.Pos) {
	clean := strings.ReplaceAll(string(l.buf[start:end]), "_", "")
	if _, err := strconv.ParseFloat(clean, 64); err != nil {
		l.errs.Add(pos, token.LexError, "invalid float literal %q", clean)
	}
	l.add(token.FLOAT, clean, pos)
}

func (l *Lexer) skipComment() {
	for {
		c := l.cur()
		if c == eofRune || c == '\n' || c == '\r' {
			return
		}
		l.pos++
		l.col++
	}
}

func (l *Lexer) consumeNewline() {
	if l.cur() == '\r' {
		l.pos++
		if l.cur() == '\n' {
			l.pos++
		}
	} else {
		l.pos++
	}
	l.line++
	l.col = 1
}

func (l *Lexer) add(t token.Type, lit string, pos token.Pos) {
	l.pending = append(l.pending, token.Token{Type: t, Literal: lit, Pos: pos})
}

func (l *Lexer) here() token.Pos {
	return token.Pos{Line: l.line, Column: l.col}
}

// fill reads runes from the reader until buf holds at least pos+n+1 runes or the
// input is exhausted.
func (l *Lexer) fill(n int) {
	for !l.readEOF && len(l.buf) <= l.pos+n {
		ch, _, err := l.r.ReadRune()
		if err != nil {
			l.readEOF = true
			if err != io.EOF {
				l.errs.Add(l.here(), token.LexError, "read error: %s", err)
			}
			return
		}
		l.buf = append(l.buf, ch)
	}
}

// at returns the rune k positions ahead of the cursor, or eofRune past the end.
func (l *Lexer) at(k int) rune {
	l.fill(k)
	if l.pos+k < len(l.buf) {
		return l.buf[l.pos+k]
	}
	return eofRune
}

func (l *Lexer) cur() rune { return l.at(0) }

func (l *Lexer) eof() bool { return l.cur() == eofRune }

func isNameStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isNameContinue(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isHexDigit(r rune) bool {
	return isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
