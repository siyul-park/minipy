package compiler

import (
	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

// global is a module-level binding: its declared type, VM global slot, and
// whether it has been assigned a value yet.
type global struct {
	typ   types.Type
	index int
	init  bool
}

type local struct {
	typ   types.Type
	index int
	init  bool
	boxed bool
}

type parameter struct {
	name         string
	typ          types.Type
	defaultValue ast.Expr
	kind         ast.ParamKind
	vararg       bool
	kwarg        bool
}

type function struct {
	name        string
	params      []parameter
	paramIndex  map[string]int
	result      types.Type
	inferResult bool         // return type is inferred from the body (no annotation)
	returns     []types.Type // return expression types collected while inferring
	generator   bool
	slot        *global
	local       *local
	locals      map[string]*local
	order       []string
	parent      *function
	children    map[string]*function
	captures    map[string]*capture
	capOrder    []string
	globals     map[string]bool
	nonlocal    map[string]bool

	// specialization: a polymorphic function (union/Any parameter) is
	// monomorphized per concrete call-site argument tuple when its body
	// type-checks under that tuple. The union/Any body still compiles to the
	// global slot as the fallback.
	specializable bool
	decorated     bool // has at least one decorator; disables specialization so calls cannot bypass the wrapper
	body          []ast.Stmt
	astParams     []*ast.Param
	instances     []*specialization
	mod           *moduleInfo
}

// specialization is one monomorphic instantiation of a specializable function:
// a clone whose parameters are bound to a concrete argument tuple, with its own
// per-node type table so the same body lowers differently per instantiation.
type specialization struct {
	key    string
	params []types.Type
	info   *function
	types  map[ast.Expr]types.Type
	calls  map[*ast.CallExpr]*specialization
	args   map[*ast.CallExpr][]ast.Expr
}

type classField struct {
	name  string
	typ   types.Type
	index int
	value ast.Expr
	pos   token.Pos
}

type class struct {
	name       string
	typ        *types.Class
	fields     []classField
	fieldIndex map[string]int
	methods    map[string]*function
	methodBody map[string][]ast.Stmt
	base       *class
	classID    int
	low        int
	high       int
	dataclass  bool
}

type capture struct {
	name  string
	typ   types.Type
	index int
	src   *local
	boxed bool
}

type initState struct {
	locals  map[string]bool
	globals map[string]bool
}

type resolvedName struct {
	key    string
	module string
	native module.Symbol
	kind   string
}

// maxSpecializations caps monomorphic instantiations per function; past it,
// calls fall back to the single union/Any-typed body.
const maxSpecializations = 8

func newFunction(name string) *function {
	return &function{
		name:       name,
		paramIndex: map[string]int{},
		locals:     map[string]*local{},
		children:   map[string]*function{},
		captures:   map[string]*capture{},
		globals:    map[string]bool{},
		nonlocal:   map[string]bool{},
	}
}

func (f *function) addParam(p parameter) {
	if f.paramIndex == nil {
		f.paramIndex = map[string]int{}
	}
	f.paramIndex[p.name] = len(f.params)
	f.params = append(f.params, p)
}

func (f *function) setParams(params []parameter) {
	f.params = params
	f.paramIndex = make(map[string]int, len(params))
	for i, p := range params {
		f.paramIndex[p.name] = i
	}
}

func (f *function) paramPosition(name string) (int, bool) {
	if f.paramIndex != nil {
		i, ok := f.paramIndex[name]
		return i, ok
	}
	for i, p := range f.params {
		if p.name == name {
			return i, true
		}
	}
	return 0, false
}
