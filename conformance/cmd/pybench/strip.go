package main

import "strings"

// stripAnnotations mechanically removes PEP 526 variable annotations
// (`name: type` and `name: type = value` statements, including class field
// declarations) from source, producing a copy gpython 3.4 can parse. gpython
// accepts PEP 3107 function parameter and return annotations
// (`def f(x: int) -> int:`) unmodified, so only bare statement-level
// annotations need rewriting: `name: type = value` becomes `name = value`,
// and a bare `name: type` declaration with no value is dropped entirely
// (gpython does not need it; attributes are created dynamically by
// `self.x = ...` in `__init__`).
//
// Only lines that are not continuing a bracketed expression from a prior
// line (paren/bracket/brace depth zero at line start) are considered, so a
// dict literal or multi-line call spanning lines is never misread as an
// annotation. A candidate line is one whose first token, at that zero depth,
// is a bare identifier immediately followed by `:` — compound-statement
// headers (`if x:`, `for i in range(n):`, `else:`, ...) never match this
// shape, since their first identifier is followed by more tokens or by a
// keyword, not directly by `:`.
func stripAnnotations(source string) string {
	lines := strings.Split(source, "\n")
	out := make([]string, 0, len(lines))
	depth := 0
	for _, line := range lines {
		if depth == 0 {
			if rewritten, drop := stripAnnotationLine(line); !drop {
				out = append(out, rewritten)
			}
			depth += bracketDelta(line)
			continue
		}
		out = append(out, line)
		depth += bracketDelta(line)
	}
	return strings.Join(out, "\n")
}

// bracketDelta returns the net change in paren/bracket/brace nesting depth
// contributed by line, ignoring bracket characters inside string literals.
func bracketDelta(line string) int {
	delta := 0
	inString := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inString != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == inString {
				inString = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inString = c
		case '(', '[', '{':
			delta++
		case ')', ']', '}':
			delta--
		case '#':
			return delta
		}
	}
	return delta
}

// stripAnnotationLine rewrites a single statement-level annotation line.
// drop reports whether the line (a bare declaration with no value) should be
// omitted entirely; rewritten is only meaningful when drop is false.
func stripAnnotationLine(line string) (rewritten string, drop bool) {
	indent, body := splitIndent(line)
	name, rest, ok := matchAnnotationHead(body)
	if !ok {
		return line, false
	}
	eq, ok := topLevelAssign(rest)
	if !ok {
		return "", true
	}
	return indent + name + " = " + strings.TrimSpace(rest[eq+1:]), false
}

// splitIndent separates line's leading whitespace from the rest.
func splitIndent(line string) (indent, body string) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i], line[i:]
}

// matchAnnotationHead reports whether body begins with `identifier:` (no
// space between the identifier and the colon is required, but none is
// forbidden either) followed by at least one more character, returning the
// identifier and the text after the colon.
func matchAnnotationHead(body string) (name, rest string, ok bool) {
	i := 0
	for i < len(body) && isIdentByte(body[i], i == 0) {
		i++
	}
	if i == 0 || i >= len(body) {
		return "", "", false
	}
	name = body[:i]
	j := i
	for j < len(body) && (body[j] == ' ' || body[j] == '\t') {
		j++
	}
	if j >= len(body) || body[j] != ':' {
		return "", "", false
	}
	rest = strings.TrimSpace(body[j+1:])
	if rest == "" {
		return "", "", false
	}
	return name, rest, true
}

func isIdentByte(c byte, first bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		return true
	case c >= '0' && c <= '9':
		return !first
	default:
		return false
	}
}

// topLevelAssign finds the byte offset of the `=` that separates a type
// expression from its default value in rest, skipping brackets, string
// literals, and the two-character comparison operators (`==`, `!=`, `<=`,
// `>=`) that are not assignment. It reports ok=false when rest carries no
// value (a bare declaration).
func topLevelAssign(rest string) (int, bool) {
	depth := 0
	inString := byte(0)
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if inString != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == inString {
				inString = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inString = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth != 0 {
				continue
			}
			if i > 0 && strings.ContainsRune("=!<>", rune(rest[i-1])) {
				continue
			}
			if i+1 < len(rest) && rest[i+1] == '=' {
				continue
			}
			return i, true
		}
	}
	return 0, false
}
