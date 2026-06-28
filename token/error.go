package token

import (
	"fmt"
	"strings"
)

// Code is a stable diagnostic category from the minipy error catalogue
// (docs/spec/04-static-semantics.md#error-catalogue). It is shared by every
// compilation phase so diagnostics read uniformly.
type Code string

// Error is a single diagnostic with its source position and catalogue code.
type Error struct {
	Pos  Pos
	Code Code
	Msg  string
}

// ErrorList accumulates diagnostics so a phase can report every error it finds
// rather than aborting on the first (docs/spec/04-static-semantics.md).
type ErrorList []*Error

const (
	LexError             Code = "LexError"
	SyntaxError          Code = "SyntaxError"
	UnsupportedFeature   Code = "UnsupportedFeature"
	UnsupportedType      Code = "UnsupportedType"
	IntOverflow          Code = "IntOverflow"
	MissingAnnotation    Code = "MissingAnnotation"
	TypeMismatch         Code = "TypeMismatch"
	UndefinedName        Code = "UndefinedName"
	UseBeforeDefinition  Code = "UseBeforeDefinition"
	ArityMismatch        Code = "ArityMismatch"
	NotComparable        Code = "NotComparable"
	NotIterable          Code = "NotIterable"
	NotIndexable         Code = "NotIndexable"
	NoBindingForNonlocal Code = "NoBindingForNonlocal"
	PatternError         Code = "PatternError"
	InvalidUnionMember   Code = "InvalidUnionMember"
)

// Python maps a catalogue code to the CPython exception name a user would see
// for the same mistake, so minipy diagnostics read consistently with Python.
func (c Code) Python() string {
	switch c {
	case LexError, SyntaxError, UnsupportedFeature, UnsupportedType:
		return "SyntaxError"
	case IntOverflow:
		return "ValueError"
	case MissingAnnotation, TypeMismatch, ArityMismatch, NotComparable, NotIterable, NotIndexable, PatternError, InvalidUnionMember:
		return "TypeError"
	case UndefinedName, UseBeforeDefinition, NoBindingForNonlocal:
		return "NameError"
	default:
		return "Error"
	}
}

// Error renders a single diagnostic in a Python-consistent form:
// "ExceptionName: message (line L, column C)".
func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s (line %d, column %d)", e.Code.Python(), e.Msg, e.Pos.Line, e.Pos.Column)
}

// Add appends a formatted diagnostic to the list.
func (l *ErrorList) Add(pos Pos, code Code, format string, args ...any) {
	*l = append(*l, &Error{Pos: pos, Code: code, Msg: fmt.Sprintf(format, args...)})
}

// Err returns the list as an error, or nil when it is empty.
func (l ErrorList) Err() error {
	if len(l) == 0 {
		return nil
	}
	return l
}

// Error renders every diagnostic, one per line.
func (l ErrorList) Error() string {
	var builder strings.Builder
	for i, e := range l {
		if i > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(e.Error())
	}
	return builder.String()
}
