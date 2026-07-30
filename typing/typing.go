// Package typing exposes annotation-only symbols from Python's typing module.
package typing

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

// Name is the module's registered name.
const Name = "typing"

var annotationNames = [...]string{
	"Any",
	"Annotated",
	"Callable",
	"Iterator",
	"Literal",
	"Optional",
	"TypeAlias",
	"Union",
}

// New builds the typing native module. Its symbols are valid only in annotation
// context; value use is rejected by the checker before lowering.
func New() *module.NativeModule {
	symbols := make([]module.Symbol, len(annotationNames))
	for index, name := range annotationNames {
		symbols[index] = annotationSymbol(name)
	}
	return module.NewNative(Name, symbols...)
}

func annotationSymbol(name string) *module.NativeSymbol {
	return module.NewSymbol(name,
		func(c module.Checker, args []ast.Expr, pos token.Pos) types.Type {
			for _, arg := range args {
				c.Check(arg)
			}
			c.Error(pos, token.UnsupportedFeature, "typing.%s is annotation-only", name)
			return types.Invalid
		},
		func(module.Emitter, []ast.Expr) {},
		nil)
}
