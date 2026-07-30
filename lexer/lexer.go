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
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/siyul-park/minipy/token"
)

// Lexer scans source read from an io.Reader into tokens, one Next at a time.
type Lexer struct {
	reader *bufio.Reader
	runes  []rune // runes read so far; indexed by offset
	offset int

	readErr error
	readEOF bool
	started bool
	done    bool

	line   int
	column int

	indents     []int
	parens      int
	atLineStart bool
	lineHasTok  bool

	pending     []token.Token
	diagnostics token.ErrorList
}

// eofRune marks "no more input" from the rune accessors.
const eofRune = rune(-1)

// bom is the UTF-8 byte-order mark, skipped if it leads the input.
const bom = '\uFEFF'

// stringMode captures which of the (at most two) prefix letters preceded a
// string/bytes literal: raw (r/R), f-string (f/F), bytes (b/B), or the
// unsupported unicode marker (u/U). The zero value means an unprefixed
// string.
type stringMode struct {
	raw       bool
	formatted bool
	bytes     bool
	unicode   bool
}

// classifyPrefix reports the stringMode for a string-prefix identifier (e.g.
// "r", "rb", "FR"), or ok == false if word is not a valid prefix combination.
// An empty word is a valid, unprefixed mode.
func classifyPrefix(word string) (mode stringMode, ok bool) {
	if len(word) > 2 {
		return stringMode{}, false
	}
	for _, r := range word {
		switch r {
		case 'r', 'R':
			if mode.raw {
				return stringMode{}, false
			}
			mode.raw = true
		case 'f', 'F':
			if mode.formatted {
				return stringMode{}, false
			}
			mode.formatted = true
		case 'b', 'B':
			if mode.bytes {
				return stringMode{}, false
			}
			mode.bytes = true
		case 'u', 'U':
			if mode.unicode {
				return stringMode{}, false
			}
			mode.unicode = true
		default:
			return stringMode{}, false
		}
	}
	if mode.formatted && mode.bytes {
		return stringMode{}, false
	}
	if mode.unicode && (mode.raw || mode.formatted || mode.bytes) {
		return stringMode{}, false
	}
	return mode, true
}

// New returns a Lexer over reader.
func New(reader io.Reader) *Lexer {
	return &Lexer{
		reader:      bufio.NewReader(reader),
		line:        1,
		column:      1,
		indents:     []int{0},
		atLineStart: true,
	}
}

// Lex scans reader and returns every token through ENDMARKER. A non-nil error
// contains lexical diagnostics, an operational reader failure, or both.
func Lex(reader io.Reader) ([]token.Token, error) {
	lexer := New(reader)
	var tokens []token.Token
	for {
		next := lexer.Next()
		tokens = append(tokens, next)
		if next.Type == token.EOF {
			return tokens, lexer.Err()
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
	next := l.pending[0]
	l.pending = l.pending[1:]
	return next
}

// Err returns accumulated lexical diagnostics, an operational reader failure,
// or both. Reader errors preserve their identity for errors.Is/errors.As.
func (l *Lexer) Err() error {
	diagnosticErr := l.diagnostics.Err()
	switch {
	case diagnosticErr == nil:
		return l.readErr
	case l.readErr == nil:
		return diagnosticErr
	default:
		return errors.Join(diagnosticErr, l.readErr)
	}
}

// step advances the scanner by one unit of work, queuing zero or more tokens.
func (l *Lexer) step() {
	if !l.started {
		l.started = true
		if l.cur() == bom {
			l.offset++
		}
	}
	if l.cur() == eofRune {
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
		l.offset++
		l.column++
	case c == '\\':
		if r := l.at(1); r == '\n' || r == '\r' {
			l.offset++
			l.column++
			l.consumeNewline()
		} else {
			l.diagnostics.Add(l.here(), token.LexError, "unexpected character %q", string(c))
			l.offset++
			l.column++
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
		l.scanString(l.here(), stringMode{})
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
		case ' ', '\f':
			width++
			l.offset++
			l.column++
		case '\t':
			l.diagnostics.Add(l.here(), token.LexError, "tab in indentation; minipy requires spaces")
			l.offset++
			l.column++
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
			l.diagnostics.Add(l.here(), token.LexError, "unindent does not match any outer indentation level")
		}
	}
	return false
}

// scanNameOrString reads an identifier/keyword, or a prefixed string when the
// identifier is a string prefix immediately followed by a quote.
func (l *Lexer) scanNameOrString() {
	pos := l.here()
	start := l.offset
	for isNameContinue(l.cur()) {
		l.offset++
		l.column++
	}
	word := string(l.runes[start:l.offset])

	if word != "" && (l.cur() == '\'' || l.cur() == '"') {
		if mode, ok := classifyPrefix(word); ok {
			l.scanString(pos, mode)
			return
		}
	}
	l.add(token.Lookup(word), word, pos)
}

// scanNumber reads an int or float literal (docs/spec/01-lexical.md#numeric).
func (l *Lexer) scanNumber() {
	pos := l.here()
	start := l.offset

	if l.cur() == '0' {
		switch l.at(1) {
		case 'x', 'X', 'o', 'O', 'b', 'B':
			l.offset += 2
			l.column += 2
			for isHexDigit(l.cur()) || l.cur() == '_' {
				l.offset++
				l.column++
			}
			l.finishInt(start, l.offset, pos)
			return
		}
	}

	isFloat := false
	for isDigit(l.cur()) || l.cur() == '_' {
		l.offset++
		l.column++
	}
	if l.cur() == '.' {
		isFloat = true
		l.offset++
		l.column++
		for isDigit(l.cur()) || l.cur() == '_' {
			l.offset++
			l.column++
		}
	}
	if l.cur() == 'e' || l.cur() == 'E' {
		sp, sc := l.offset, l.column
		l.offset++
		l.column++
		if l.cur() == '+' || l.cur() == '-' {
			l.offset++
			l.column++
		}
		if isDigit(l.cur()) {
			isFloat = true
			for isDigit(l.cur()) || l.cur() == '_' {
				l.offset++
				l.column++
			}
		} else {
			l.offset, l.column = sp, sc
		}
	}

	end := l.offset
	if l.cur() == 'j' || l.cur() == 'J' {
		l.diagnostics.Add(pos, token.LexError, "imaginary literals are not supported (no complex)")
		l.offset++
		l.column++
	}

	if isFloat {
		l.finishFloat(start, end, pos)
	} else {
		l.finishInt(start, end, pos)
	}
}

// scanString reads a string or bytes literal whose prefix (if any) started at
// pos and whose opening quote is at the cursor. Escapes are decoded unless
// mode.raw. In mode.bytes, directly written non-ASCII characters are
// rejected and \u/\U are left undecoded (bytes has no Unicode escapes).
func (l *Lexer) scanString(pos token.Pos, mode stringMode) {
	if mode.unicode {
		l.diagnostics.Add(pos, token.UnsupportedFeature, "u/U string prefix is not supported")
	}
	q := l.cur()
	triple := l.at(1) == q && l.at(2) == q
	if triple {
		l.offset += 3
		l.column += 3
	} else {
		l.offset++
		l.column++
	}

	var builder strings.Builder
	for {
		c := l.cur()
		if c == eofRune {
			l.diagnostics.Add(pos, token.LexError, "unterminated string literal")
			break
		}
		if !triple && (c == '\n' || c == '\r') {
			l.diagnostics.Add(pos, token.LexError, "unterminated string literal")
			break
		}
		if c == q {
			if !triple {
				l.offset++
				l.column++
				break
			}
			if l.at(1) == q && l.at(2) == q {
				l.offset += 3
				l.column += 3
				break
			}
			builder.WriteRune(c)
			l.offset++
			l.column++
			continue
		}
		if c == '\n' || c == '\r' {
			builder.WriteByte('\n')
			l.consumeNewline()
			continue
		}
		if c == '\\' {
			if mode.raw {
				builder.WriteRune(c)
				l.offset++
				l.column++
				if l.cur() != eofRune {
					l.checkBytesChar(mode, pos, l.cur())
					builder.WriteRune(l.cur())
					l.offset++
					l.column++
				}
				continue
			}
			l.offset++
			l.column++
			l.decodeEscape(&builder, pos, mode)
			continue
		}
		l.checkBytesChar(mode, pos, c)
		builder.WriteRune(c)
		l.offset++
		l.column++
	}
	if mode.formatted {
		l.add(token.FSTRING, builder.String(), pos)
		return
	}
	if mode.bytes {
		l.add(token.BYTES, builder.String(), pos)
		return
	}
	l.add(token.STRING, builder.String(), pos)
}

// checkBytesChar reports a diagnostic when a non-ASCII character is written
// directly (not via an escape) into a bytes literal.
func (l *Lexer) checkBytesChar(mode stringMode, pos token.Pos, c rune) {
	if mode.bytes && c > unicode.MaxASCII {
		l.diagnostics.Add(pos, token.LexError, "bytes literal cannot contain non-ASCII character %q", string(c))
	}
}

// decodeEscape consumes one escape sequence (the backslash is already consumed)
// and writes its decoded value to builder. In mode.bytes, \u and \U are not
// Unicode escapes and fall through to the unknown-escape rule.
func (l *Lexer) decodeEscape(builder *strings.Builder, pos token.Pos, mode stringMode) {
	c := l.cur()
	if c == eofRune {
		l.diagnostics.Add(pos, token.LexError, "unterminated escape sequence")
		return
	}
	l.offset++
	l.column++
	switch c {
	case 'n':
		builder.WriteByte('\n')
	case 't':
		builder.WriteByte('\t')
	case 'r':
		builder.WriteByte('\r')
	case '\\':
		builder.WriteByte('\\')
	case '\'':
		builder.WriteByte('\'')
	case '"':
		builder.WriteByte('"')
	case '0':
		builder.WriteByte(0)
	case 'a':
		builder.WriteByte(7)
	case 'b':
		builder.WriteByte(8)
	case 'f':
		builder.WriteByte('\f')
	case 'v':
		builder.WriteByte('\v')
	case 'x':
		l.decodeHex(builder, pos, 2, mode.bytes)
	case 'u':
		if mode.bytes {
			builder.WriteByte('\\')
			builder.WriteRune(c)
			return
		}
		l.decodeHex(builder, pos, 4, false)
	case 'U':
		if mode.bytes {
			builder.WriteByte('\\')
			builder.WriteRune(c)
			return
		}
		l.decodeHex(builder, pos, 8, false)
	default:
		// Unknown escape: keep the backslash and the character (Python's lenient rule).
		builder.WriteByte('\\')
		builder.WriteRune(c)
	}
}

// decodeHex reads exactly n hex digits and writes the resulting value. When
// asByte is set (a bytes literal's \xNN), the value is written as a single
// raw byte instead of being UTF-8 encoded as a rune.
func (l *Lexer) decodeHex(builder *strings.Builder, pos token.Pos, n int, asByte bool) {
	l.fill(n - 1)
	if l.offset+n > len(l.runes) {
		l.diagnostics.Add(pos, token.LexError, "truncated \\x/\\u/\\U escape")
		return
	}
	digits := string(l.runes[l.offset : l.offset+n])
	v, err := strconv.ParseUint(digits, 16, 32)
	if err != nil {
		l.diagnostics.Add(pos, token.LexError, "invalid hex escape %q", digits)
		return
	}
	l.offset += n
	l.column += n
	if asByte {
		builder.WriteByte(byte(v))
		return
	}
	builder.WriteRune(rune(v))
}

// scanOperator matches the longest operator or delimiter at the cursor.
func (l *Lexer) scanOperator() {
	pos := l.here()
	c := l.cur()
	emit := func(t token.Type, n int) {
		l.fill(n - 1)
		lit := string(l.runes[l.offset : l.offset+n])
		l.offset += n
		l.column += n
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
			l.diagnostics.Add(pos, token.LexError, "unexpected character %q", "!")
			l.offset++
			l.column++
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
		if la(1) == '.' && la(2) == '.' {
			emit(token.ELLIPSIS, 3)
		} else {
			emit(token.DOT, 1)
		}
	case ';':
		emit(token.SEMICOLON, 1)
	default:
		l.diagnostics.Add(pos, token.LexError, "unexpected character %q", string(c))
		l.offset++
		l.column++
	}
}

func (l *Lexer) finishInt(start, end int, pos token.Pos) {
	clean := strings.ReplaceAll(string(l.runes[start:end]), "_", "")
	if _, err := strconv.ParseInt(clean, 0, 64); err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			l.diagnostics.Add(pos, token.IntOverflow, "integer literal %q exceeds int64", clean)
		} else {
			l.diagnostics.Add(pos, token.LexError, "invalid integer literal %q", clean)
		}
	}
	l.add(token.INT, clean, pos)
}

func (l *Lexer) finishFloat(start, end int, pos token.Pos) {
	clean := strings.ReplaceAll(string(l.runes[start:end]), "_", "")
	if _, err := strconv.ParseFloat(clean, 64); err != nil {
		l.diagnostics.Add(pos, token.LexError, "invalid float literal %q", clean)
	}
	l.add(token.FLOAT, clean, pos)
}

func (l *Lexer) skipComment() {
	for {
		c := l.cur()
		if c == eofRune || c == '\n' || c == '\r' {
			return
		}
		l.offset++
		l.column++
	}
}

func (l *Lexer) consumeNewline() {
	if l.cur() == '\r' {
		l.offset++
		if l.cur() == '\n' {
			l.offset++
		}
	} else {
		l.offset++
	}
	l.line++
	l.column = 1
}

func (l *Lexer) add(t token.Type, lit string, pos token.Pos) {
	l.pending = append(l.pending, token.Token{Type: t, Literal: lit, Pos: pos})
}

func (l *Lexer) here() token.Pos {
	return token.Pos{Line: l.line, Column: l.column}
}

// fill reads runes until the buffer holds the requested lookahead or the input
// is exhausted.
func (l *Lexer) fill(n int) {
	for !l.readEOF && len(l.runes) <= l.offset+n {
		ch, _, err := l.reader.ReadRune()
		if err != nil {
			l.readEOF = true
			if !errors.Is(err, io.EOF) {
				l.readErr = err
			}
			return
		}
		l.runes = append(l.runes, ch)
	}
}

// at returns the rune k positions ahead of the cursor, or eofRune past the end.
func (l *Lexer) at(k int) rune {
	l.fill(k)
	if l.offset+k < len(l.runes) {
		return l.runes[l.offset+k]
	}
	return eofRune
}

func (l *Lexer) cur() rune { return l.at(0) }

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
