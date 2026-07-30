// Package stringmod provides Python's string module as a native module:
// string constants (ascii_lowercase, ascii_uppercase, ascii_letters, digits,
// hexdigits, octdigits, punctuation, whitespace, printable). Each symbol is a
// ConstantSymbol that emits its value inline via the constant pool. Static
// types are preferred; all constants have type str.
package stringmod

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/types"

	vmtypes "github.com/siyul-park/minivm/types"
)

// Name is the module's registered name.
const Name = "string"

// Python string module constant values.
const (
	asciiLowercase = "abcdefghijklmnopqrstuvwxyz"
	asciiUppercase = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	asciiLetters   = asciiLowercase + asciiUppercase
	digits         = "0123456789"
	hexdigits      = "0123456789abcdefABCDEF"
	octdigits      = "01234567"
	punctuation    = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
	whitespace     = " \t\n\r\x0b\x0c"
	printable      = digits + asciiLetters + punctuation + whitespace
)

// New builds the string native module.
func New() *module.NativeModule {
	return module.NewNative(Name,
		constant("ascii_lowercase", asciiLowercase),
		constant("ascii_uppercase", asciiUppercase),
		constant("ascii_letters", asciiLetters),
		constant("digits", digits),
		constant("hexdigits", hexdigits),
		constant("octdigits", octdigits),
		constant("punctuation", punctuation),
		constant("whitespace", whitespace),
		constant("printable", printable),
	)
}

// constant builds a ConstantSymbol that emits a CONST_GET instruction for a
// string value from the constant pool.
func constant(name string, value string) *module.NativeConstant {
	return module.NewConstant(name, types.Str, func(e module.Emitter, _ []ast.Expr) {
		e.ConstGet(vmtypes.String(value))
	})
}
