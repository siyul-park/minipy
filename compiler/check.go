package compiler

import (
	"sort"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/builtins"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/parser"
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
	constIdx      int // VM constant index of this (specialized) function body
	mod           *moduleInfo
}

// specialization is one monomorphic instantiation of a specializable function:
// a clone whose parameters are bound to a concrete argument tuple, with its own
// per-node type table so the same body lowers differently per instantiation.
type specialization struct {
	key      string
	params   []types.Type
	info     *function
	types    map[ast.Expr]types.Type
	calls    map[*ast.CallExpr]*specialization
	args     map[*ast.CallExpr][]ast.Expr
	emitted  bool
	emitting bool
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

// checker resolves names and types for a module, producing a per-expression
// type table and a global symbol table consumed by the compiler.
type checker struct {
	errs       token.ErrorList
	types      map[ast.Expr]types.Type
	globals    map[string]*global
	functions  map[string]*function
	classes    map[string]*class
	aliases    map[string]*alias
	aliasDecls map[*ast.AnnAssign]bool
	lambdas    map[*ast.LambdaExpr]*function
	genExprs   map[*ast.GeneratorExp]*function
	order      []string
	classOrder []string
	loops      int // enclosing-loop depth, for break/continue validation
	excepts    int // enclosing-except depth, for bare raise validation
	current    *function
	// narrowed overlays flow-sensitive types onto bindings inside a guarded
	// region (isinstance / is-None). nameType consults it first so a use sees
	// the narrowed member type, not the declared union.
	narrowed map[string]types.Type
	// temps overlays short-lived names such as comprehension targets without
	// declaring them in module or function scope.
	temps map[string]types.Type
	// callSpec maps a call site to the specialization it links to; specActive
	// guards against unbounded re-instantiation of recursive specializations.
	callSpec   map[*ast.CallExpr]*specialization
	callArgs   map[*ast.CallExpr][]ast.Expr
	specActive map[string]bool
	loader     *loader
	reg        *module.Registry
	modules    map[string]*moduleInfo
	mod        *moduleInfo
	attrSym    map[*ast.Attribute]string
	attrMod    map[*ast.Attribute]string
	attrNative map[*ast.Attribute]module.Symbol
	// lenDunder marks len() call sites whose argument is a class instance, so
	// the compiler lowers them to a direct obj.__len__() call instead of the
	// native len builtin.
	lenDunder map[*ast.CallExpr]bool
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

func newChecker(loaders ...*loader) *checker {
	var ld *loader
	if len(loaders) > 0 {
		ld = loaders[0]
	}
	if ld == nil {
		ld = newLoader(nil, nil)
	}
	c := &checker{
		types:      map[ast.Expr]types.Type{},
		globals:    map[string]*global{},
		functions:  map[string]*function{},
		classes:    map[string]*class{},
		aliases:    map[string]*alias{},
		aliasDecls: map[*ast.AnnAssign]bool{},
		lambdas:    map[*ast.LambdaExpr]*function{},
		genExprs:   map[*ast.GeneratorExp]*function{},
		narrowed:   map[string]types.Type{},
		temps:      map[string]types.Type{},
		callSpec:   map[*ast.CallExpr]*specialization{},
		callArgs:   map[*ast.CallExpr][]ast.Expr{},
		specActive: map[string]bool{},
		loader:     ld,
		reg:        ld.reg,
		modules:    ld.modules,
		attrSym:    map[*ast.Attribute]string{},
		attrMod:    map[*ast.Attribute]string{},
		attrNative: map[*ast.Attribute]module.Symbol{},
		lenDunder:  map[*ast.CallExpr]bool{},
	}
	c.declareBuiltinExceptions()
	return c
}

// checker adapts to module.Checker so native modules can drive type-checking
// without depending on compiler internals.
var _ module.Checker = (*checker)(nil)

// Check type-checks a sub-expression and returns its type.
func (c *checker) Check(e ast.Expr) types.Type { return c.expr(e) }

// Type returns the already-recorded type of an expression.
func (c *checker) Type(e ast.Expr) types.Type { return c.types[e] }

// SetType records the resolved type of an expression.
func (c *checker) SetType(e ast.Expr, t types.Type) { c.types[e] = t }

// ResolveType interprets an expression as a type annotation.
func (c *checker) ResolveType(e ast.Expr) types.Type { return c.resolveType(e) }

// Error reports a static error.
func (c *checker) Error(pos token.Pos, code token.Code, format string, args ...any) {
	c.errs.Add(pos, code, format, args...)
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

// check walks every top-level statement, accumulating diagnostics.
func (c *checker) checkProgram(entry *moduleInfo) {
	c.checkModule(entry)
	c.computeClassIntervals()
}

func (c *checker) check(mod *ast.Module) {
	entry, _ := c.loader.loadEntry(mod)
	c.checkProgram(entry)
}

func (c *checker) checkModule(m *moduleInfo) {
	if m == nil || m.checked {
		return
	}
	m.checked = true
	prev := c.mod
	c.mod = m
	// Alias state is module-scoped; save and restore it so a recursively
	// checked module (e.g. an imported native module) cannot resolve another
	// module's aliases under the wrong context.
	prevAliases := c.aliases
	c.aliases = map[string]*alias{}
	defer func() {
		c.aliases = prevAliases
		c.mod = prev
	}()
	c.checkFutureImports(m)
	c.declareClasses(m.ast.Body)
	c.declareFuncs(m.ast.Body)
	c.collectAliases(m.ast.Body)
	c.checkBlock(m.ast.Body)
	c.resolveAliases()
	c.computeExports(m)
}

func (c *checker) checkFutureImports(m *moduleInfo) {
	if m.future == nil {
		m.future = map[string]bool{}
	}
	inPrefix := true
	docstringAllowed := true
	for _, s := range m.ast.Body {
		if docstringAllowed {
			if expr, ok := s.(*ast.ExprStmt); ok {
				if _, ok := expr.X.(*ast.StrLit); ok {
					docstringAllowed = false
					continue
				}
			}
		}
		docstringAllowed = false
		future, ok := s.(*ast.ImportFrom)
		if ok && future.Level == 0 && future.Module == "__future__" {
			if !inPrefix {
				c.errs.Add(future.Pos(), token.SyntaxError, "from __future__ imports must occur at beginning of file")
				continue
			}
			for _, name := range future.Names {
				if name.As != "" {
					c.errs.Add(name.Pos(), token.SyntaxError, "future feature %q cannot be imported as an alias", name.Name)
					continue
				}
				switch name.Name {
				case "annotations":
					m.future[name.Name] = true
				default:
					c.errs.Add(name.Pos(), token.SyntaxError, "unknown __future__ feature %q", name.Name)
				}
			}
			continue
		}
		inPrefix = false
	}
}

func (c *checker) computeExports(m *moduleInfo) {
	if m.native {
		return
	}
	if m.allSeen {
		if m.allStatic {
			m.exports = uniqueStrings(m.all)
		}
		return
	}
	names := map[string]bool{}
	for name := range m.bindings {
		if !strings.HasPrefix(name, "_") {
			names[name] = true
		}
	}
	for key := range c.globals {
		if name, ok := localNameForModule(m.name, key); ok && !strings.HasPrefix(name, "_") {
			names[name] = true
		}
	}
	for key := range c.functions {
		if name, ok := localNameForModule(m.name, key); ok && !strings.HasPrefix(name, "_") {
			names[name] = true
		}
	}
	for key := range c.classes {
		if name, ok := localNameForModule(m.name, key); ok && !strings.HasPrefix(name, "_") {
			names[name] = true
		}
	}
	m.exports = make([]string, 0, len(names))
	for name := range names {
		m.exports = append(m.exports, name)
	}
	sort.Strings(m.exports)
}

func localNameForModule(moduleName, key string) (string, bool) {
	if moduleName == "__main__" {
		if strings.Contains(key, ".") {
			return "", false
		}
		return key, true
	}
	prefix := moduleName + "."
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(key, prefix)
	return name, !strings.Contains(name, ".")
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (c *checker) key(name string) string {
	if c.mod == nil || c.mod.name == "__main__" {
		return name
	}
	return c.mod.name + "." + name
}

func moduleKey(module, symbol string) string {
	if module == "__main__" {
		return symbol
	}
	return module + "." + symbol
}

func (c *checker) resolveName(name string) resolvedName {
	if c.mod != nil {
		if b, ok := c.mod.bindings[name]; ok {
			if b.symbol == "" {
				return resolvedName{module: b.module, kind: "module"}
			}
			if sym, ok := c.reg.Symbol(b.module, b.symbol); ok {
				return resolvedName{key: b.module + "." + b.symbol, native: sym, kind: "native"}
			}
			key := moduleKey(b.module, b.symbol)
			switch {
			case c.functions[key] != nil:
				return resolvedName{key: key, kind: "function"}
			case c.classes[key] != nil:
				return resolvedName{key: key, kind: "class"}
			case c.globals[key] != nil:
				return resolvedName{key: key, kind: "global"}
			}
			return resolvedName{key: key}
		}
	}
	key := c.key(name)
	switch {
	case c.functions[key] != nil:
		return resolvedName{key: key, kind: "function"}
	case c.classes[key] != nil:
		return resolvedName{key: key, kind: "class"}
	case c.globals[key] != nil:
		return resolvedName{key: key, kind: "global"}
	case c.reg.Has(name):
		return resolvedName{module: name, kind: "module"}
	case c.classes["builtins."+name] != nil:
		return resolvedName{key: "builtins." + name, kind: "class"}
	}
	if sym, ok := c.reg.FallbackSymbol(name); ok {
		return resolvedName{key: c.reg.FallbackName() + "." + name, native: sym, kind: "native"}
	}
	return resolvedName{key: key}
}

func (c *checker) resolveModuleAttr(moduleName, name string) resolvedName {
	if sym, ok := c.reg.Symbol(moduleName, name); ok {
		return resolvedName{key: moduleName + "." + name, native: sym, kind: "native"}
	}
	if m := c.modules[moduleName]; m != nil {
		if b, ok := m.bindings[name]; ok {
			if b.symbol == "" {
				return resolvedName{module: b.module, kind: "module"}
			}
			if sym, ok := c.reg.Symbol(b.module, b.symbol); ok {
				return resolvedName{key: b.module + "." + b.symbol, native: sym, kind: "native"}
			}
			return c.resolveModuleAttr(b.module, b.symbol)
		}
	}
	key := moduleKey(moduleName, name)
	switch {
	case c.functions[key] != nil:
		return resolvedName{key: key, kind: "function"}
	case c.classes[key] != nil:
		return resolvedName{key: key, kind: "class"}
	case c.globals[key] != nil:
		return resolvedName{key: key, kind: "global"}
	case c.modules[key] != nil:
		return resolvedName{module: key, kind: "module"}
	}
	return resolvedName{key: key}
}

// checkBlock checks a statement sequence (a module body or a compound block).
// When an `if` whose body always returns/raises and has no else precedes other
// statements, the negative narrowing of its condition applies to the rest of
// the block (e.g. `if isinstance(x, int): return ...` narrows x for what
// follows).
func (c *checker) checkBlock(body []ast.Stmt) {
	for i, s := range body {
		c.stmt(s)
		iff, ok := s.(*ast.If)
		if !ok || len(iff.Orelse) != 0 || !blockReturns(iff.Body) {
			continue
		}
		if known, truth := c.truth(iff.Cond); known {
			if truth {
				return
			}
			continue
		}
		if _, neg := c.narrowings(iff.Cond); len(neg) > 0 {
			rest := body[i+1:]
			c.withNarrow(neg, func() { c.checkBlock(rest) })
			return
		}
	}
}

// withNarrow runs body with the given bindings narrowed, restoring the previous
// overlay afterward.
func (c *checker) withNarrow(m map[string]types.Type, body func()) {
	if len(m) == 0 {
		body()
		return
	}
	type saved struct {
		t  types.Type
		ok bool
	}
	old := make(map[string]saved, len(m))
	for k, v := range m {
		prev, ok := c.narrowed[k]
		old[k] = saved{prev, ok}
		c.narrowed[k] = v
	}
	body()
	for k, s := range old {
		if s.ok {
			c.narrowed[k] = s.t
		} else {
			delete(c.narrowed, k)
		}
	}
}

// narrowings extracts the type refinements a condition implies for its true
// (pos) and false (neg) branches. It recognizes isinstance(NAME, T) and
// NAME is/is not None, and only narrows bindings whose type is a union or Any.
func (c *checker) narrowings(cond ast.Expr) (pos, neg map[string]types.Type) {
	pos = map[string]types.Type{}
	neg = map[string]types.Type{}
	switch e := cond.(type) {
	case *ast.CallExpr:
		name, ok := e.Fn.(*ast.Name)
		if !ok || name.Name != "isinstance" || len(e.Args) != 2 {
			return
		}
		target, ok := e.Args[0].(*ast.Name)
		if !ok {
			return
		}
		cur := c.currentType(target.Name)
		if !narrowable(cur) {
			return
		}
		t := c.resolveType(e.Args[1])
		if t == types.Invalid {
			return
		}
		pos[target.Name] = t
		if w := types.Without(cur, t); w != types.Invalid {
			neg[target.Name] = w
		}
	case *ast.Compare:
		if len(e.Ops) != 1 {
			return
		}
		target, ok := e.X.(*ast.Name)
		if !ok {
			return
		}
		if _, ok := e.Comparators[0].(*ast.NoneLit); !ok {
			return
		}
		cur := c.currentType(target.Name)
		if !narrowable(cur) {
			return
		}
		switch e.Ops[0] {
		case token.IS:
			pos[target.Name] = types.None
			if w := types.Without(cur, types.None); w != types.Invalid {
				neg[target.Name] = w
			}
		case token.ISNOT:
			if w := types.Without(cur, types.None); w != types.Invalid {
				pos[target.Name] = w
			}
			neg[target.Name] = types.None
		}
	}
	return
}

// truth returns statically known results for pure guards once a
// specialization has bound a narrowed value to one concrete type.
func (c *checker) truth(cond ast.Expr) (known bool, truth bool) {
	return fold(cond, c.currentType, c.resolveType)
}

func fold(cond ast.Expr, current func(string) types.Type, typeOf func(ast.Expr) types.Type) (known bool, truth bool) {
	switch e := cond.(type) {
	case *ast.CallExpr:
		name, ok := e.Fn.(*ast.Name)
		if !ok || name.Name != "isinstance" || len(e.Args) != 2 {
			return false, false
		}
		target, ok := e.Args[0].(*ast.Name)
		if !ok {
			return false, false
		}
		cur := current(target.Name)
		if !concrete(cur) && !types.Equal(cur, types.None) {
			return false, false
		}
		t := typeOf(e.Args[1])
		if t == nil || t == types.Invalid {
			return false, false
		}
		return true, types.AssignableTo(cur, t)
	case *ast.Compare:
		if len(e.Ops) != 1 {
			return false, false
		}
		target, ok := e.X.(*ast.Name)
		if !ok {
			return false, false
		}
		if _, ok := e.Comparators[0].(*ast.NoneLit); !ok {
			return false, false
		}
		cur := current(target.Name)
		if !concrete(cur) && !types.Equal(cur, types.None) {
			return false, false
		}
		isNone := types.Equal(cur, types.None)
		switch e.Ops[0] {
		case token.IS:
			return true, isNone
		case token.ISNOT:
			return true, !isNone
		}
	}
	return false, false
}

// currentType returns the binding's current (possibly narrowed) type without
// emitting diagnostics, or Invalid if the name is unknown here.
func (c *checker) currentType(name string) types.Type {
	if t, ok := c.temps[name]; ok {
		return t
	}
	if t, ok := c.narrowed[name]; ok {
		return t
	}
	if c.current != nil && !c.current.globals[name] {
		if l, ok := c.current.locals[name]; ok {
			return l.typ
		}
		if cap, ok := c.current.captures[name]; ok {
			return cap.typ
		}
	}
	if g, ok := c.globals[c.resolveName(name).key]; ok {
		return g.typ
	}
	return types.Invalid
}

// narrowable reports whether a binding of type t benefits from flow narrowing.
func narrowable(t types.Type) bool {
	if _, ok := t.(*types.Union); ok {
		return true
	}
	return types.IsAny(t)
}

func (c *checker) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.AnnAssign:
		c.annAssign(n)
	case *ast.Assign:
		c.assign(n)
	case *ast.AugAssign:
		c.augAssign(n)
	case *ast.ExprStmt:
		c.expr(n.X)
	case *ast.If:
		c.ifStmt(n)
	case *ast.While:
		c.whileStmt(n)
	case *ast.For:
		c.forStmt(n)
	case *ast.Function:
		c.functionStmt(n)
	case *ast.Class:
		c.classStmt(n)
	case *ast.Import:
		c.importStmt(n)
	case *ast.ImportFrom:
		c.importFromStmt(n)
	case *ast.TypeAlias:
		c.aliases[c.key(n.Name.Name)] = &alias{expr: n.Value, pos: n.Name.Pos()}
	case *ast.Global:
		if c.current == nil {
			c.errs.Add(n.Pos(), token.SyntaxError, "'global' outside function")
			return
		}
		for _, name := range n.Names {
			c.current.globals[name] = true
		}
	case *ast.Nonlocal:
		if c.current == nil {
			c.errs.Add(n.Pos(), token.SyntaxError, "'nonlocal' outside function")
			return
		}
		for _, name := range n.Names {
			if c.findEnclosingLocal(name) == nil {
				c.errs.Add(n.Pos(), token.NoBindingForNonlocal, "no binding for nonlocal %q found", name)
				continue
			}
			c.current.nonlocal[name] = true
		}
	case *ast.Return:
		c.returnStmt(n)
	case *ast.Yield:
		c.yieldStmt(n)
	case *ast.Break:
		if c.loops == 0 {
			c.errs.Add(n.Pos(), token.SyntaxError, "'break' outside loop")
		}
	case *ast.Continue:
		if c.loops == 0 {
			c.errs.Add(n.Pos(), token.SyntaxError, "'continue' outside loop")
		}
	case *ast.Pass:
		// no-op
	case *ast.Delete:
		c.deleteStmt(n)
	case *ast.Assert:
		c.assertStmt(n)
	case *ast.Match:
		c.matchStmt(n)
	case *ast.Try:
		c.tryStmt(n)
	case *ast.Raise:
		c.raiseStmt(n)
	case *ast.With:
		c.withStmt(n)
	}
}

func (c *checker) importStmt(n *ast.Import) {
	if c.current != nil {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "import is only supported at module top level")
		return
	}
	for _, a := range n.Names {
		m := c.loader.loadModule(a.Name, a.Pos())
		c.checkModule(m)
		if m == nil {
			continue
		}
		local := a.As
		target := a.Name
		if local == "" {
			local = strings.Split(a.Name, ".")[0]
			target = local
		}
		c.mod.bindings[local] = binding{module: target}
	}
}

func (c *checker) importFromStmt(n *ast.ImportFrom) {
	if c.current != nil {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "from import is only supported at module top level")
		return
	}
	if n.Level == 0 && n.Module == "__future__" {
		return
	}
	if len(n.Names) == 1 && n.Names[0].Name == "*" {
		c.importStar(n)
		return
	}
	base := c.loader.resolveFrom(c.mod, n)
	if base == "" {
		return
	}
	m := c.loader.loadModule(base, n.Pos())
	c.checkModule(m)
	if m == nil {
		return
	}
	for _, a := range n.Names {
		local := a.As
		if local == "" {
			local = a.Name
		}
		res := c.resolveModuleAttr(base, a.Name)
		switch res.kind {
		case "function", "class", "global", "native":
			c.mod.bindings[local] = binding{module: base, symbol: a.Name}
		case "module":
			c.mod.bindings[local] = binding{module: res.module}
		default:
			errMark := len(c.loader.errs)
			sub := c.loader.loadModule(base+"."+a.Name, a.Pos())
			if sub == nil {
				c.loader.errs = c.loader.errs[:errMark]
			}
			c.checkModule(sub)
			if sub != nil {
				c.mod.bindings[local] = binding{module: sub.name}
				continue
			}
			c.errs.Add(a.Pos(), token.ImportError, "cannot import name %q from %q", a.Name, base)
		}
	}
}

func (c *checker) importStar(n *ast.ImportFrom) {
	base := c.loader.resolveFrom(c.mod, n)
	if base == "" {
		return
	}
	m := c.loader.loadModule(base, n.Pos())
	c.checkModule(m)
	if m == nil {
		return
	}
	if m.allSeen && !m.allStatic {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "from-import * requires a static __all__ in %q", base)
		return
	}
	for _, name := range m.exports {
		if c.localBindingExists(name) {
			c.errs.Add(n.Names[0].Pos(), token.ImportError, "from-import * from %q conflicts with local name %q", base, name)
			continue
		}
		res := c.resolveModuleAttr(base, name)
		switch res.kind {
		case "function", "class", "global", "native":
			c.mod.bindings[name] = binding{module: base, symbol: name}
		case "module":
			c.mod.bindings[name] = binding{module: res.module}
		default:
			c.errs.Add(n.Names[0].Pos(), token.ImportError, "cannot import name %q from %q", name, base)
		}
	}
}

func (c *checker) localBindingExists(name string) bool {
	if _, ok := c.mod.bindings[name]; ok {
		return true
	}
	key := c.key(name)
	if _, ok := c.globals[key]; ok {
		return true
	}
	if _, ok := c.functions[key]; ok {
		return true
	}
	if _, ok := c.classes[key]; ok {
		return true
	}
	return false
}

func (c *checker) tryStmt(n *ast.Try) {
	state := c.snapshotInits()
	c.checkBlock(n.Body)
	for _, h := range n.Handlers {
		if h.Star {
			c.errs.Add(h.Pos(), token.UnsupportedFeature, "except* is not supported yet")
		}
		ht := c.handlerType(h)
		if h.Name != "" {
			c.bindCapture(h.Name, ht, h.Pos())
		}
		c.excepts++
		c.checkBlock(h.Body)
		c.excepts--
	}
	c.checkBlock(n.Orelse)
	c.restoreInits(state)
	c.checkBlock(n.Finalbody)
}

func (c *checker) handlerType(h *ast.ExceptHandler) types.Type {
	if h.Type == nil {
		return c.classes["BaseException"].typ
	}
	info := c.exceptionClass(h.Type)
	if info == nil {
		return types.Invalid
	}
	return info.typ
}

func (c *checker) raiseStmt(n *ast.Raise) {
	if n.Cause != nil {
		c.expr(n.Cause)
	}
	if n.Exc == nil {
		if c.excepts == 0 {
			c.errs.Add(n.Pos(), token.SyntaxError, "bare raise outside except")
		}
		return
	}
	t := c.expr(n.Exc)
	if t == types.Invalid {
		return
	}
	cls, ok := t.(*types.Class)
	if !ok || !c.isException(cls.Name) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "raise requires an Exception instance, got %s", t)
	}
}

func (c *checker) withStmt(n *ast.With) {
	if n.Async {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "async with is parse-only until scheduler support lands")
	}
	for _, item := range n.Items {
		receiver := c.expr(item.Context)
		cls, ok := receiver.(*types.Class)
		if !ok {
			if receiver != types.Invalid {
				c.errs.Add(item.Pos(), token.UnsupportedFeature, "with requires a context manager, got %s", receiver)
			}
			continue
		}
		info := c.classes[cls.Name]
		enter := method(info, "__enter__")
		exit := method(info, "__exit__")
		if enter == nil || exit == nil {
			c.errs.Add(item.Pos(), token.UnsupportedFeature, "%s is not a context manager", cls.Name)
			continue
		}
		if len(enter.params) != 1 {
			c.errs.Add(item.Pos(), token.ArityMismatch, "%s.__enter__() takes no arguments", cls.Name)
		}
		if len(exit.params) != 1 {
			c.errs.Add(item.Pos(), token.ArityMismatch, "%s.__exit__() takes no arguments", cls.Name)
		}
		if item.OptionalVars != nil {
			name, ok := item.OptionalVars.(*ast.Name)
			if !ok {
				c.errs.Add(item.OptionalVars.Pos(), token.SyntaxError, "with target must be a name")
			} else if c.current != nil {
				l := c.declareLocal(name.Name, enter.result, name.Pos())
				l.init = true
				c.types[name] = l.typ
			} else {
				g := c.declare(name.Name, enter.result, name.Pos())
				g.init = true
				c.types[name] = g.typ
			}
		}
	}
	c.checkBlock(n.Body)
}

func (c *checker) snapshotInits() initState {
	locals := map[string]bool{}
	if c.current != nil {
		for name, l := range c.current.locals {
			locals[name] = l.init
		}
	}
	globals := map[string]bool{}
	for name, g := range c.globals {
		globals[name] = g.init
	}
	return initState{locals: locals, globals: globals}
}

func (c *checker) restoreInits(state initState) {
	if c.current != nil {
		for name, l := range c.current.locals {
			l.init = state.locals[name]
		}
	}
	for name, g := range c.globals {
		g.init = state.globals[name]
	}
}

func (c *checker) mergeInits(left, right initState) {
	if c.current != nil {
		for name, l := range c.current.locals {
			l.init = left.locals[name] && right.locals[name]
		}
	}
	for name, g := range c.globals {
		g.init = left.globals[name] && right.globals[name]
	}
}

// deleteStmt checks each `del` target. A deleted Name becomes
// definitely-unassigned so a later read reuses UseBeforeDefinition; subscript and
// attribute targets are checked as deletable lvalues.
func (c *checker) deleteStmt(n *ast.Delete) {
	for _, target := range n.Targets {
		switch t := target.(type) {
		case *ast.Name:
			c.deleteName(t)
		case *ast.Subscript:
			receiver := c.expr(t.X)
			if slice, ok := t.Index.(*ast.Slice); ok {
				c.listSliceMutation(t, slice, receiver, nil)
				continue
			}
			index := c.expr(t.Index)
			switch receiver.(type) {
			case *types.List, *types.Dict:
				c.indexResultType(t, receiver, index)
			default:
				if receiver != types.Invalid {
					c.errs.Add(t.Pos(), token.UnsupportedFeature, "cannot delete an item from %s", receiver)
				}
			}
		case *ast.Attribute:
			c.fieldType(t, c.expr(t.X))
		default:
			c.errs.Add(target.Pos(), token.SyntaxError, "cannot delete this expression")
		}
	}
}

func (c *checker) deleteName(n *ast.Name) {
	if c.current != nil {
		if c.current.globals[n.Name] {
			c.deleteGlobalName(n)
			return
		}
		if l, ok := c.current.locals[n.Name]; ok {
			if !l.init {
				c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
			}
			l.init = false
			c.types[n] = l.typ
			return
		}
		if cap := c.capture(n); cap != nil {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "cannot delete captured name %q", n.Name)
			return
		}
	}
	c.deleteGlobalName(n)
}

func (c *checker) deleteGlobalName(n *ast.Name) {
	g, ok := c.globals[c.resolveName(n.Name).key]
	if !ok {
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", n.Name)
		return
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
	}
	g.init = false
	c.types[n] = g.typ
}

// assertStmt checks the test types as bool and the optional message is printable.
func (c *checker) assertStmt(n *ast.Assert) {
	c.condition(n.Test)
	if n.Msg != nil {
		mt := c.expr(n.Msg)
		if mt != types.Invalid && !types.Printable(mt) {
			c.errs.Add(n.Msg.Pos(), token.TypeMismatch, "assert message must be printable, got %s", mt)
		}
	}
}

// matchStmt checks the subject, then every case pattern (declaring captures),
// guard, and body.
func (c *checker) matchStmt(n *ast.Match) {
	subjT := c.expr(n.Subject)
	state := c.snapshotInits()
	for _, cs := range n.Cases {
		c.restoreInits(state)
		c.checkPattern(cs.Pattern, subjT)
		if cs.Guard != nil {
			c.condition(cs.Guard)
		}
		c.checkBlock(cs.Body)
	}
	c.restoreInits(state)
}

// bindCapture declares a pattern capture variable in the current scope and marks
// it initialized; re-binding the same name must keep a consistent type.
func (c *checker) bindCapture(name string, t types.Type, pos token.Pos) {
	if name == "" || name == "_" {
		return
	}
	if c.current != nil {
		if l, ok := c.current.locals[name]; ok {
			if t != types.Invalid && l.typ != types.Invalid && !types.Equal(l.typ, t) {
				c.errs.Add(pos, token.PatternError, "capture %q binds inconsistent types %s and %s", name, l.typ, t)
			}
			l.init = true
			return
		}
		l := c.declareLocal(name, t, pos)
		l.init = true
		return
	}
	if g, ok := c.globals[c.resolveName(name).key]; ok {
		if t != types.Invalid && g.typ != types.Invalid && !types.Equal(g.typ, t) {
			c.errs.Add(pos, token.PatternError, "capture %q binds inconsistent types %s and %s", name, g.typ, t)
		}
		g.init = true
		return
	}
	g := c.declare(name, t, pos)
	g.init = true
}

// checkPattern validates a pattern against the subject type and declares any
// capture variables it introduces.
func (c *checker) checkPattern(p ast.Pattern, subjT types.Type) {
	switch pat := p.(type) {
	case *ast.WildcardPattern:
		// matches anything, binds nothing
	case *ast.CapturePattern:
		c.bindCapture(pat.Name, subjT, pat.Pos())
	case *ast.StarPattern:
		c.bindCapture(pat.Name, types.NewList(subjT), pat.Pos())
	case *ast.AsPattern:
		c.checkPattern(pat.Pattern, subjT)
		c.bindCapture(pat.Name, subjT, pat.Pos())
	case *ast.OrPattern:
		for _, alt := range pat.Alts {
			c.checkPattern(alt, subjT)
		}
	case *ast.ValuePattern:
		value := c.expr(pat.Value)
		if value != types.Invalid && subjT != types.Invalid && !types.Equal(value, subjT) {
			c.errs.Add(pat.Pos(), token.PatternError, "pattern value %s is not comparable to subject %s", value, subjT)
		}
	case *ast.SequencePattern:
		c.checkSequencePattern(pat, subjT)
	case *ast.MappingPattern:
		c.checkMappingPattern(pat, subjT)
	case *ast.ClassPattern:
		c.checkClassPattern(pat, subjT)
	}
}

func (c *checker) checkSequencePattern(pat *ast.SequencePattern, subjT types.Type) {
	switch s := subjT.(type) {
	case *types.List:
		for _, e := range pat.Elems {
			if star, ok := e.(*ast.StarPattern); ok {
				c.bindCapture(star.Name, types.NewList(s.Elem), star.Pos())
				continue
			}
			c.checkPattern(e, s.Elem)
		}
	case *types.Tuple:
		if pat.Star >= 0 {
			prefix := pat.Star
			suffix := len(pat.Elems) - pat.Star - 1
			if len(s.Elems) < prefix+suffix {
				c.errs.Add(pat.Pos(), token.PatternError, "sequence pattern expects at least %d elements, %s has %d", prefix+suffix, subjT, len(s.Elems))
				return
			}
			for i := 0; i < prefix; i++ {
				c.checkPattern(pat.Elems[i], s.Elems[i])
			}
			for j := 0; j < suffix; j++ {
				c.checkPattern(pat.Elems[prefix+1+j], s.Elems[len(s.Elems)-suffix+j])
			}
			// The captured rest must be homogeneous so it binds as list[T].
			star := pat.Elems[prefix].(*ast.StarPattern)
			elemType := homogeneous(s.Elems[prefix : len(s.Elems)-suffix])
			if elemType == types.Invalid {
				c.errs.Add(star.Pos(), token.TypeMismatch, "starred tuple rest must have homogeneous type")
				return
			}
			c.bindCapture(star.Name, types.NewList(elemType), star.Pos())
			return
		}
		if len(pat.Elems) != len(s.Elems) {
			c.errs.Add(pat.Pos(), token.PatternError, "sequence pattern expects %d elements, %s has %d", len(pat.Elems), subjT, len(s.Elems))
			return
		}
		for i, e := range pat.Elems {
			c.checkPattern(e, s.Elems[i])
		}
	default:
		if subjT != types.Invalid {
			c.errs.Add(pat.Pos(), token.PatternError, "sequence pattern requires a list or tuple subject, got %s", subjT)
		}
	}
}

func (c *checker) checkMappingPattern(pat *ast.MappingPattern, subjT types.Type) {
	d, ok := subjT.(*types.Dict)
	if !ok {
		if subjT != types.Invalid {
			c.errs.Add(pat.Pos(), token.PatternError, "mapping pattern requires a dict subject, got %s", subjT)
		}
		return
	}
	for i, key := range pat.Keys {
		kt := c.expr(key)
		if kt != types.Invalid && !types.AssignableTo(kt, d.Key) {
			c.errs.Add(key.Pos(), token.PatternError, "mapping key %s does not match dict key %s", kt, d.Key)
		}
		c.checkPattern(pat.Values[i], d.Value)
	}
	if pat.Rest != "" {
		c.bindCapture(pat.Rest, types.NewDict(d.Key, d.Value), pat.Pos())
	}
}

func (c *checker) checkClassPattern(pat *ast.ClassPattern, subjT types.Type) {
	display := ""
	key := ""
	if name, ok := pat.Class.(*ast.Name); ok {
		display = name.Name
		key = c.resolveName(name.Name).key
	} else if attr, ok := pat.Class.(*ast.Attribute); ok {
		display = attr.Name
		if mod, ok := c.moduleExpr(attr.X); ok {
			res := c.resolveModuleAttr(mod, attr.Name)
			key = res.key
			c.attrSym[attr] = key
		}
	} else {
		c.errs.Add(pat.Pos(), token.UnsupportedFeature, "class pattern target is not supported")
		return
	}
	info := c.classes[key]
	if info == nil {
		c.errs.Add(pat.Pos(), token.UndefinedName, "class %q is not defined", display)
		return
	}
	if subjT != types.Invalid && !types.Equal(subjT, info.typ) {
		c.errs.Add(pat.Pos(), token.PatternError, "class pattern %s does not match subject %s", display, subjT)
	}
	for i, sub := range pat.Args {
		if i < len(info.fields) {
			c.checkPattern(sub, info.fields[i].typ)
		} else {
			c.errs.Add(sub.Pos(), token.PatternError, "class %s has no positional field %d", display, i)
		}
	}
	for i, kw := range pat.KwNames {
		idx, ok := info.fieldIndex[kw]
		if !ok {
			c.errs.Add(pat.Kw[i].Pos(), token.UndefinedName, "field %q is not defined on %s", kw, display)
			continue
		}
		c.checkPattern(pat.Kw[i], info.fields[idx].typ)
	}
}

func (c *checker) ifStmt(n *ast.If) {
	c.condition(n.Cond)
	if known, truth := c.truth(n.Cond); known {
		if truth {
			c.checkBlock(n.Body)
		} else {
			c.checkBlock(n.Orelse)
		}
		return
	}
	pos, neg := c.narrowings(n.Cond)
	state := c.snapshotInits()
	c.withNarrow(pos, func() { c.checkBlock(n.Body) })
	left := c.snapshotInits()
	c.restoreInits(state)
	c.withNarrow(neg, func() { c.checkBlock(n.Orelse) })
	right := c.snapshotInits()
	c.mergeInits(left, right)
}

func (c *checker) whileStmt(n *ast.While) {
	c.condition(n.Cond)
	c.loops++
	c.checkBlock(n.Body)
	c.loops--
	c.checkBlock(n.Orelse)
}

// forStmt checks `for TARGET in ITERABLE`. The target is auto-declared to the
// iterable element type; its body runs inside a loop for break/continue.
func (c *checker) forStmt(n *ast.For) {
	if n.Async {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "async for is parse-only until scheduler support lands")
	}
	target := forTargetName(n.Target)
	iter := c.expr(n.Iter)
	elem := iterableElem(iter)
	if elem == types.Invalid {
		c.errs.Add(n.Iter.Pos(), token.NotIterable, "%s is not iterable", iter)
	}
	if tupleTarget, ok := n.Target.(*ast.TupleLit); ok {
		c.bindForTupleTarget(tupleTarget, elem)
	} else if c.current != nil {
		l := c.declareLocal(target.Name, elem, target.Pos())
		l.init = true
		c.types[target] = l.typ
	} else {
		g := c.declare(target.Name, elem, target.Pos())
		g.init = true
		c.types[target] = g.typ
	}
	c.loops++
	c.checkBlock(n.Body)
	c.loops--
	c.checkBlock(n.Orelse)
}

func iterableElem(t types.Type) types.Type {
	switch x := t.(type) {
	case *types.List:
		return x.Elem
	case *types.Dict:
		return x.Key
	case *types.Set:
		return x.Elem
	case *types.Iterator:
		return x.Elem
	default:
		if types.Equal(t, types.Str) {
			return types.Str
		}
		if types.Equal(t, types.Bytes) {
			return types.Int
		}
		return types.Invalid
	}
}

func (c *checker) bindForTupleTarget(target *ast.TupleLit, elem types.Type) {
	tuple, ok := elem.(*types.Tuple)
	if !ok {
		if elem != types.Invalid {
			c.errs.Add(target.Pos(), token.TypeMismatch, "tuple target cannot unpack %s", elem)
		}
		return
	}
	if len(tuple.Elems) != len(target.Elems) {
		c.errs.Add(target.Pos(), token.ArityMismatch, "for target needs %d values, got %d", len(target.Elems), len(tuple.Elems))
		return
	}
	for i, e := range target.Elems {
		name, ok := e.(*ast.Name)
		if !ok {
			c.errs.Add(e.Pos(), token.SyntaxError, "for tuple target must be a name")
			continue
		}
		if c.current != nil {
			l := c.declareLocal(name.Name, tuple.Elems[i], name.Pos())
			l.init = true
			c.types[name] = l.typ
			continue
		}
		g := c.declare(name.Name, tuple.Elems[i], name.Pos())
		g.init = true
		c.types[name] = g.typ
	}
}

func forTargetName(e ast.Expr) *ast.Name {
	if name, ok := e.(*ast.Name); ok {
		return name
	}
	return &ast.Name{Base: ast.Base{Position: e.Pos()}, Name: ""}
}

// condition checks that a control-flow test types as bool (no truthiness
// coercion, docs/spec/02-types.md).
func (c *checker) condition(e ast.Expr) {
	t := c.expr(e)
	if !types.Equal(t, types.Bool) && t != types.Invalid {
		c.errs.Add(e.Pos(), token.TypeMismatch, "condition must be bool, got %s", t)
	}
}

func (c *checker) annAssign(n *ast.AnnAssign) {
	t := c.resolveType(n.Ann)
	if types.Equal(t, types.TypeAlias) {
		c.typeAliasAssign(n)
		return
	}
	var g *global
	var l *local
	if c.current != nil {
		l = c.declareLocal(n.Target.Name, t, n.Pos())
	} else {
		g = c.declare(n.Target.Name, t, n.Pos())
	}
	if n.Value != nil {
		value := c.exprWithHint(n.Value, t)
		if t != types.Invalid && value != types.Invalid && !types.AssignableTo(value, t) {
			c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", value, t, n.Target.Name)
		}
		if l != nil {
			l.init = true
		} else {
			g.init = true
		}
	}
}

func (c *checker) typeAliasAssign(n *ast.AnnAssign) {
	c.aliasDecls[n] = true
	if n.Value == nil {
		c.errs.Add(n.Pos(), token.MissingAnnotation, "type alias %q needs a value", n.Target.Name)
		return
	}
	c.aliases[c.key(n.Target.Name)] = &alias{expr: n.Value, pos: n.Target.Pos()}
}

// alias is a module-level type-alias declaration: its RHS, source position, and
// (once resolved) the concrete type it names.
type alias struct {
	expr      ast.Expr
	pos       token.Pos
	typ       types.Type
	resolving bool
}

// collectAliases records every type-alias RHS before any type is resolved, so
// aliases may reference one another regardless of declaration order. The
// legacy `Name: TypeAlias = expr` form is handled later by typeAliasAssign,
// which recognizes the alias via the resolved annotation type (so a shadowed
// `TypeAlias` name is not mistaken for it).
func (c *checker) collectAliases(body []ast.Stmt) {
	for _, s := range body {
		if n, ok := s.(*ast.TypeAlias); ok {
			c.aliases[c.key(n.Name.Name)] = &alias{expr: n.Value, pos: n.Name.Pos()}
		}
	}
}

// resolveAliases resolves every collected alias. Running it as a dedicated pass
// ensures unused aliases still report cycles or undefined targets.
func (c *checker) resolveAliases() {
	for key := range c.aliases {
		c.resolveAlias(key)
	}
}

// resolveAlias resolves a single alias, caching the result and guarding against
// recursive cycles.
func (c *checker) resolveAlias(key string) types.Type {
	a := c.aliases[key]
	if a == nil {
		return types.Invalid
	}
	if a.resolving {
		c.errs.Add(a.pos, token.CyclicAlias, "recursive type alias %q", key)
		return types.Invalid
	}
	if a.typ != nil {
		return a.typ
	}
	a.resolving = true
	defer func() { a.resolving = false }()
	a.typ = c.resolveType(a.expr)
	return a.typ
}

func (c *checker) assign(n *ast.Assign) {
	name, ok := n.Target.(*ast.Name)
	if !ok {
		c.assignTarget(n.Target, n.Value, n.Pos())
		return
	}
	if c.current == nil {
		if _, isFunc := c.functions[c.key(name.Name)]; isFunc {
			c.errs.Add(n.Pos(), token.TypeMismatch, "cannot assign to function %q", name.Name)
			c.expr(n.Value)
			return
		}
	}
	value := c.expr(n.Value)
	if c.current != nil {
		switch {
		case c.current.globals[name.Name]:
		case c.current.nonlocal[name.Name]:
			cap := c.capture(name)
			if cap == nil {
				return
			}
			if cap.typ != types.Invalid && value != types.Invalid && !types.AssignableTo(value, cap.typ) {
				c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", value, cap.typ, name.Name)
			}
			cap.boxed = true
			cap.src.boxed = true
			c.types[name] = cap.typ
			return
		default:
			l, declared := c.current.locals[name.Name]
			if !declared {
				l = c.declareLocal(name.Name, value, n.Pos())
			}
			if l.typ != types.Invalid && value != types.Invalid && !types.AssignableTo(value, l.typ) {
				c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", value, l.typ, name.Name)
			}
			l.init = true
			c.types[name] = l.typ
			return
		}
	}
	g, declared := c.globals[c.key(name.Name)]
	if !declared {
		// Whole-program inference: an unannotated global takes the type of its
		// first assignment instead of requiring an annotation.
		g = c.declare(name.Name, value, n.Pos())
		g.init = true
		c.types[name] = value
		return
	}
	if g.typ != types.Invalid && value != types.Invalid && !types.AssignableTo(value, g.typ) {
		c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", value, g.typ, name.Name)
	}
	g.init = true
	c.types[name] = g.typ
}

func (c *checker) assignTarget(target ast.Expr, value ast.Expr, pos token.Pos) {
	switch t := target.(type) {
	case *ast.Subscript:
		receiver := c.expr(t.X)
		if slice, ok := t.Index.(*ast.Slice); ok {
			c.listSliceMutation(t, slice, receiver, value)
			return
		}
		if cls, ok := receiver.(*types.Class); ok {
			c.classSetItem(t, cls, value)
			return
		}
		if types.Equal(receiver, types.Bytes) {
			c.errs.Add(t.Pos(), token.NotIndexable, "bytes does not support item assignment")
			c.expr(t.Index)
			c.expr(value)
			return
		}
		index := c.expr(t.Index)
		valueType := c.expr(value)
		elem := c.indexResultType(t, receiver, index)
		if elem != types.Invalid && valueType != types.Invalid && !types.AssignableTo(valueType, elem) {
			c.errs.Add(value.Pos(), token.TypeMismatch, "cannot assign %s to indexed %s", valueType, elem)
		}
	case *ast.Attribute:
		receiver := c.expr(t.X)
		field := c.fieldType(t, receiver)
		valueType := c.expr(value)
		if field != types.Invalid && valueType != types.Invalid && !types.AssignableTo(valueType, field) {
			c.errs.Add(value.Pos(), token.TypeMismatch, "cannot assign %s to field %s", valueType, field)
		}
	case *ast.TupleLit:
		c.unpackAssign(t, c.expr(value), value.Pos())
	default:
		c.errs.Add(pos, token.SyntaxError, "cannot assign to this expression")
		c.expr(value)
	}
}

// classSetItem resolves obj[index] = value for a class instance via
// __setitem__, validating the index and value against the method's declared
// parameters and requiring a None result.
func (c *checker) classSetItem(t *ast.Subscript, cls *types.Class, value ast.Expr) {
	index := c.expr(t.Index)
	valueType := c.expr(value)
	info := c.classes[cls.Name]
	if info == nil {
		return
	}
	m := method(info, "__setitem__")
	if m == nil || len(m.params) != 3 {
		c.errs.Add(t.Pos(), token.NotIndexable, "%s does not support item assignment", cls)
		return
	}
	idxParam := m.params[1].typ
	if index != types.Invalid && idxParam != types.Invalid && !types.AssignableTo(index, idxParam) {
		c.errs.Add(t.Index.Pos(), token.TypeMismatch, "%s.__setitem__ index must be %s, got %s", info.name, idxParam, index)
	}
	valParam := m.params[2].typ
	if valueType != types.Invalid && valParam != types.Invalid && !types.AssignableTo(valueType, valParam) {
		c.errs.Add(value.Pos(), token.TypeMismatch, "%s.__setitem__ value must be %s, got %s", info.name, valParam, valueType)
	}
	if m.result != types.Invalid && !types.Equal(m.result, types.None) {
		c.errs.Add(t.Pos(), token.TypeMismatch, "%s.__setitem__ must return None, got %s", info.name, m.result)
	}
}

func (c *checker) listSliceMutation(target *ast.Subscript, slice *ast.Slice, receiver types.Type, value ast.Expr) {
	c.checkSliceBounds(slice)
	if !supportedMutationSliceStep(slice.Step) {
		c.errs.Add(slice.Step.Pos(), token.UnsupportedFeature, "extended slice assignment is not supported")
	}
	list, ok := receiver.(*types.List)
	if !ok {
		if types.Equal(receiver, types.Bytes) {
			c.errs.Add(target.Pos(), token.NotIndexable, "bytes is immutable and does not support slice assignment")
		} else if receiver != types.Invalid {
			c.errs.Add(target.Pos(), token.UnsupportedFeature, "cannot mutate a slice of %s", receiver)
		}
		if value != nil {
			c.expr(value)
		}
		return
	}
	if value == nil {
		return
	}
	valueType := c.exprWithHint(value, receiver)
	if valueType != types.Invalid && !types.Equal(valueType, receiver) {
		c.errs.Add(value.Pos(), token.TypeMismatch, "slice assignment expects list[%s], got %s", list.Elem, valueType)
	}
}

func supportedMutationSliceStep(step ast.Expr) bool {
	if step == nil {
		return true
	}
	if lit, ok := step.(*ast.IntLit); ok {
		return lit.Value == 1
	}
	return false
}

func (c *checker) unpackAssign(target *ast.TupleLit, value types.Type, pos token.Pos) {
	var elems []types.Type
	var listElem types.Type
	switch t := value.(type) {
	case *types.Tuple:
		elems = t.Elems
	case *types.List:
		listElem = t.Elem
		elems = make([]types.Type, len(target.Elems))
		for i := range elems {
			elems[i] = t.Elem
		}
	default:
		if value != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot unpack %s", value)
		}
		return
	}
	star := tupleStarIndex(target)
	if star == -2 {
		c.errs.Add(target.Pos(), token.SyntaxError, "multiple starred targets in assignment")
		return
	}
	if star < 0 && len(elems) != len(target.Elems) {
		c.errs.Add(pos, token.ArityMismatch, "unpack needs %d values, got %d", len(target.Elems), len(elems))
		return
	}
	if star >= 0 && len(elems) < len(target.Elems)-1 {
		c.errs.Add(pos, token.ArityMismatch, "not enough values to unpack")
		return
	}
	for i, elem := range target.Elems {
		if star, ok := elem.(*ast.Starred); ok {
			name, ok := star.X.(*ast.Name)
			if !ok {
				c.errs.Add(star.Pos(), token.SyntaxError, "starred assignment target must be a name")
				continue
			}
			elemType := listElem
			if elemType == nil {
				rest := elems[i : len(elems)-(len(target.Elems)-i-1)]
				elemType = homogeneous(rest)
				if elemType == types.Invalid {
					c.errs.Add(star.Pos(), token.TypeMismatch, "starred tuple rest must have homogeneous type")
					continue
				}
			}
			c.bindUnpackedName(name, types.NewList(elemType))
			continue
		}
		name, ok := elem.(*ast.Name)
		if !ok {
			c.errs.Add(elem.Pos(), token.SyntaxError, "tuple unpack target must be a name")
			continue
		}
		srcIdx := i
		if star >= 0 && i > star {
			srcIdx = len(elems) - (len(target.Elems) - i)
		}
		c.bindUnpackedName(name, elems[srcIdx])
	}
}

func tupleStarIndex(target *ast.TupleLit) int {
	star := -1
	for i, elem := range target.Elems {
		if _, ok := elem.(*ast.Starred); ok {
			if star >= 0 {
				return -2
			}
			star = i
		}
	}
	return star
}

func homogeneous(ts []types.Type) types.Type {
	if len(ts) == 0 {
		return types.Any
	}
	first := ts[0]
	for _, t := range ts[1:] {
		if !types.Equal(first, t) {
			return types.Invalid
		}
	}
	return first
}

func (c *checker) bindUnpackedName(name *ast.Name, t types.Type) {
	if c.current != nil {
		l, declared := c.current.locals[name.Name]
		if !declared {
			l = c.declareLocal(name.Name, t, name.Pos())
		}
		if !types.AssignableTo(t, l.typ) {
			c.errs.Add(name.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", t, l.typ, name.Name)
		}
		l.init = true
		c.types[name] = l.typ
		return
	}
	g, declared := c.globals[c.key(name.Name)]
	if !declared {
		g = c.declare(name.Name, t, name.Pos())
		g.init = true
		c.types[name] = g.typ
		return
	}
	if !types.AssignableTo(t, g.typ) {
		c.errs.Add(name.Pos(), token.TypeMismatch, "cannot assign %s to %s %q", t, g.typ, name.Name)
	}
	g.init = true
	c.types[name] = g.typ
}

func (c *checker) augAssign(n *ast.AugAssign) {
	name, ok := n.Target.(*ast.Name)
	if !ok {
		attr, ok := n.Target.(*ast.Attribute)
		if !ok {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "augmented assignment target is not supported")
			c.expr(n.Value)
			return
		}
		receiver := c.expr(attr.X)
		field := c.fieldType(attr, receiver)
		value := c.expr(n.Value)
		result := c.binaryType(field, n.Op, value, n.Pos())
		if result != types.Invalid && field != types.Invalid && !types.AssignableTo(result, field) {
			c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to field %s", result, field)
		}
		return
	}
	if c.current != nil {
		switch {
		case c.current.globals[name.Name]:
		case c.current.nonlocal[name.Name]:
			cap := c.capture(name)
			if cap == nil {
				c.expr(n.Value)
				return
			}
			c.types[name] = cap.typ
			value := c.expr(n.Value)
			result := c.binaryType(cap.typ, n.Op, value, n.Pos())
			if result != types.Invalid && cap.typ != types.Invalid && !types.AssignableTo(result, cap.typ) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", result, cap.typ, name.Name)
			}
			cap.boxed = true
			cap.src.boxed = true
			return
		default:
			l, declared := c.current.locals[name.Name]
			if !declared {
				c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
				c.expr(n.Value)
				return
			}
			if !l.init {
				c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", name.Name)
			}
			c.types[name] = l.typ
			value := c.expr(n.Value)
			result := c.binaryType(l.typ, n.Op, value, n.Pos())
			if result != types.Invalid && l.typ != types.Invalid && !types.AssignableTo(result, l.typ) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", result, l.typ, name.Name)
			}
			l.init = true
			return
		}
	}
	g, declared := c.globals[c.key(name.Name)]
	if !declared {
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
		c.expr(n.Value)
		return
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", name.Name)
	}
	c.types[name] = g.typ
	value := c.expr(n.Value)
	result := c.binaryType(g.typ, n.Op, value, n.Pos())
	if result != types.Invalid && g.typ != types.Invalid && !types.AssignableTo(result, g.typ) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "result %s is not assignable to %s %q", result, g.typ, name.Name)
	}
	g.init = true
}

// declare registers a new global or returns the existing one, reporting a type
// change on redeclaration.
func (c *checker) declare(name string, t types.Type, pos token.Pos) *global {
	key := c.key(name)
	if g, ok := c.globals[key]; ok {
		if _, isFunc := c.functions[key]; isFunc && t != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare function %q", name)
			return g
		}
		if _, isClass := c.classes[key]; isClass && t != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare class %q", name)
			return g
		}
		if t != types.Invalid && g.typ != types.Invalid && !types.Equal(g.typ, t) {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare %q from %s to %s", name, g.typ, t)
		}
		return g
	}
	if _, isClass := c.classes[key]; isClass && t != types.Invalid {
		c.errs.Add(pos, token.TypeMismatch, "cannot redeclare class %q", name)
	}
	g := &global{typ: t, index: len(c.order)}
	c.globals[key] = g
	c.order = append(c.order, key)
	return g
}

func (c *checker) declareLocal(name string, t types.Type, pos token.Pos) *local {
	if l, ok := c.current.locals[name]; ok {
		if t != types.Invalid && l.typ != types.Invalid && !types.Equal(l.typ, t) {
			c.errs.Add(pos, token.TypeMismatch, "cannot redeclare %q from %s to %s", name, l.typ, t)
		}
		return l
	}
	l := &local{typ: t, index: len(c.current.params) + len(c.current.order)}
	c.current.locals[name] = l
	c.current.order = append(c.current.order, name)
	return l
}

func (c *checker) declareFuncLocal(info *function, pos token.Pos) {
	if info.local != nil {
		return
	}
	info.local = c.declareLocal(info.name, types.NewCallable(srcTypes(info.params), info.result), pos)
}

func (c *checker) declareFuncs(body []ast.Stmt) {
	for _, s := range body {
		f, ok := s.(*ast.Function)
		if !ok {
			continue
		}
		key := c.key(f.Name.Name)
		if _, exists := c.functions[key]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q", f.Name.Name)
			continue
		}
		if _, exists := c.classes[key]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare class %q as a function", f.Name.Name)
			continue
		}
		if _, exists := c.globals[key]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare %q as a function", f.Name.Name)
			continue
		}
		info := newFunction(key)
		info.mod = c.mod
		info.generator = containsYield(f.Body)
		if f.Returns == nil {
			info.inferResult = true
			info.result = types.None // refined from collected returns after the body
		} else {
			info.result = c.resolveType(f.Returns)
		}
		for _, p := range f.Params {
			info.addParam(c.makeParam(p))
		}
		info.body = f.Body
		info.astParams = f.Params
		info.decorated = len(f.DecoratorExprs) > 0
		info.specializable = specializable(info)
		info.slot = c.declare(f.Name.Name, types.Invalid, f.Pos())
		c.functions[key] = info
	}
}

func (c *checker) declareClasses(body []ast.Stmt) {
	for _, s := range body {
		cls, ok := s.(*ast.Class)
		if !ok {
			continue
		}
		name := cls.Name.Name
		key := c.key(name)
		if _, exists := c.classes[key]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare class %q", name)
			continue
		}
		if _, exists := c.globals[key]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare %q as a class", name)
			continue
		}
		if _, exists := c.functions[key]; exists {
			c.errs.Add(cls.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q as a class", name)
			continue
		}
		c.classes[key] = &class{
			name:       key,
			typ:        types.NewClass(key, nil),
			fieldIndex: map[string]int{},
			methods:    map[string]*function{},
			methodBody: map[string][]ast.Stmt{},
		}
		c.classOrder = append(c.classOrder, key)
	}
}

// declareBuiltinExceptions seeds the class table with the builtin exception
// hierarchy exported by the builtins module, so exception identity lives in the
// builtins module rather than being hardcoded here.
func (c *checker) declareBuiltinExceptions() {
	fields := []classField{
		{name: "__classid", typ: types.Int, index: 0},
		{name: "message", typ: types.Str, index: 1},
	}
	excs := builtins.Exceptions()
	for _, exc := range excs {
		info := &class{
			name:       exc.Name,
			typ:        types.NewClass(exc.Name, nil),
			fields:     append([]classField(nil), fields...),
			fieldIndex: map[string]int{"__classid": 0, "message": 1},
			methods:    map[string]*function{},
			methodBody: map[string][]ast.Stmt{},
		}
		c.classes["builtins."+exc.Name] = info
		c.classes[exc.Name] = info
		c.classOrder = append(c.classOrder, "builtins."+exc.Name)
	}
	for _, exc := range excs {
		if exc.Base != "" {
			c.classes["builtins."+exc.Name].base = c.classes["builtins."+exc.Base]
		}
	}
	for _, exc := range excs {
		c.classes["builtins."+exc.Name].typ.Fields = classTypeFields(c.classes["builtins."+exc.Name].fields)
	}
}

func (c *checker) computeClassIntervals() {
	children := map[string][]*class{}
	for _, name := range c.classOrder {
		info := c.classes[name]
		if info == nil || info.base == nil {
			continue
		}
		children[info.base.name] = append(children[info.base.name], info)
	}
	next := 1
	var dfs func(*class)
	dfs = func(info *class) {
		info.classID = next
		info.low = next
		next++
		for _, child := range children[info.name] {
			dfs(child)
		}
		info.high = next - 1
	}
	if root := c.classes["BaseException"]; root != nil {
		dfs(root)
	}
}

func (c *checker) classStmt(n *ast.Class) {
	info := c.classes[c.key(n.Name.Name)]
	if info == nil {
		return
	}
	c.classDecorators(info, n.DecoratorExprs)
	if len(n.Bases) > 1 {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "multiple base classes are not supported yet (tracked by #16)")
	}
	for _, kw := range n.Keywords {
		switch {
		case kw.Name == "":
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "dynamic class keywords (**kwargs) are not supported")
		case kw.Name == "metaclass":
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "metaclass is not supported yet (tracked by #22)")
		default:
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "unknown class keyword %q", kw.Name)
		}
	}
	if n.BaseClass != nil {
		base := c.classes[c.resolveName(n.BaseClass.Name).key]
		if base == nil {
			c.errs.Add(n.BaseClass.Pos(), token.UnsupportedType, "unknown base class %q", n.BaseClass.Name)
		} else if base == info {
			c.errs.Add(n.BaseClass.Pos(), token.TypeMismatch, "class %q cannot inherit from itself", info.name)
		} else {
			info.base = base
			info.fields = append(info.fields, base.fields...)
			for name, idx := range base.fieldIndex {
				info.fieldIndex[name] = idx
			}
		}
	}
	for _, s := range n.Body {
		switch member := s.(type) {
		case *ast.AnnAssign:
			c.classField(info, member)
		case *ast.Function:
			c.classMethod(info, member)
		case *ast.Pass:
			// no-op
		default:
			c.errs.Add(member.Pos(), token.SyntaxError, "class body supports only fields and methods")
		}
	}
	c.checkDataclassDefaults(info)
	info.typ.Fields = classTypeFields(info.fields)
	for _, s := range n.Body {
		if f, ok := s.(*ast.Function); ok {
			if method := info.methods[f.Name.Name]; method != nil {
				c.checkFunctionBody(f.Body, f.Params, method, f.Pos())
			}
		}
	}
}

// classDecorators supports only @dataclass and @dataclass(), both of which
// enable the existing dataclass constructor path with identical behavior.
// Dataclass options, any other class decorator, and metaclasses are diagnosed
// distinctly and tracked by follow-up issues (#32 and #22) rather than
// collapsed into one generic diagnostic.
func (c *checker) classDecorators(info *class, decorators []ast.Expr) {
	for _, dec := range decorators {
		if name, ok := dec.(*ast.Name); ok && name.Name == "dataclass" {
			info.dataclass = true
			continue
		}
		call, ok := dec.(*ast.CallExpr)
		if !ok {
			c.errs.Add(dec.Pos(), token.UnsupportedFeature, "class decorators other than @dataclass are not supported yet (tracked by #22)")
			continue
		}
		name, ok := call.Fn.(*ast.Name)
		if !ok || name.Name != "dataclass" {
			c.errs.Add(dec.Pos(), token.UnsupportedFeature, "class decorators other than @dataclass are not supported yet (tracked by #22)")
			continue
		}
		if len(call.Args) > 0 || len(call.Keywords) > 0 || len(call.StarArgs) > 0 || call.Kwargs != nil {
			c.errs.Add(dec.Pos(), token.UnsupportedFeature, "dataclass options are not supported yet (tracked by #32)")
			continue
		}
		info.dataclass = true
	}
}

func (c *checker) exceptionClass(e ast.Expr) *class {
	display := ""
	key := ""
	if name, ok := e.(*ast.Name); ok {
		display = name.Name
		key = c.resolveName(name.Name).key
	} else if attr, ok := e.(*ast.Attribute); ok {
		display = attr.Name
		if mod, ok := c.moduleExpr(attr.X); ok {
			res := c.resolveModuleAttr(mod, attr.Name)
			key = res.key
			c.attrSym[attr] = key
		}
	} else {
		c.errs.Add(e.Pos(), token.UnsupportedFeature, "exception type must be a class name")
		return nil
	}
	info := c.classes[key]
	if info == nil {
		if _, ok := types.Resolve(display); ok {
			c.errs.Add(e.Pos(), token.TypeMismatch, "except type must inherit from Exception, got %s", display)
			return nil
		}
		c.errs.Add(e.Pos(), token.UndefinedName, "class %q is not defined", display)
		return nil
	}
	if !c.isException(info.name) {
		c.errs.Add(e.Pos(), token.TypeMismatch, "except type must inherit from Exception, got %s", info.name)
		return nil
	}
	return info
}

func (c *checker) isException(name string) bool {
	return isException(c.classes[name])
}

func method(info *class, name string) *function {
	for info != nil {
		if method := info.methods[name]; method != nil {
			return method
		}
		info = info.base
	}
	return nil
}

func (c *checker) classField(info *class, n *ast.AnnAssign) {
	name := n.Target.Name
	if _, exists := info.fieldIndex[name]; exists {
		c.errs.Add(n.Target.Pos(), token.TypeMismatch, "cannot redeclare field %q", name)
	}
	t := c.resolveType(n.Ann)
	if recursiveByValue(t, info.typ) {
		c.errs.Add(n.Ann.Pos(), token.UnsupportedType, "field %q embeds %s by value; use an optional or reference-like type", name, info.name)
		t = types.Invalid
	}
	field := classField{name: name, typ: t, index: len(info.fields), value: n.Value, pos: n.Target.Pos()}
	info.fieldIndex[name] = field.index
	info.fields = append(info.fields, field)
	if n.Value != nil {
		value := c.exprWithHint(n.Value, t)
		if t != types.Invalid && value != types.Invalid && !types.AssignableTo(value, t) {
			c.errs.Add(n.Value.Pos(), token.TypeMismatch, "cannot assign %s to field %s %q", value, t, name)
		}
	}
}

func (c *checker) classMethod(info *class, n *ast.Function) {
	c.checkParamFeatures(n.Params)
	if _, exists := info.methods[n.Name.Name]; exists {
		c.errs.Add(n.Name.Pos(), token.TypeMismatch, "cannot redeclare method %q", n.Name.Name)
		return
	}
	if len(n.Params) == 0 || n.Params[0].Name.Name != "self" {
		c.errs.Add(n.Pos(), token.MissingAnnotation, "method %q needs self parameter", n.Name.Name)
		return
	}
	params := make([]parameter, 0, len(n.Params))
	for i, p := range n.Params {
		pt := types.Type(info.typ)
		if i > 0 {
			pt = c.paramType(p)
		} else if p.Ann != nil {
			pt = c.resolveType(p.Ann)
			if pt != types.Invalid && !types.Equal(pt, info.typ) {
				c.errs.Add(p.Ann.Pos(), token.TypeMismatch, "self must be %s, got %s", info.typ, pt)
			}
		}
		params = append(params, c.makeParamWithType(p, pt))
	}
	inferResult := n.Returns == nil && n.Name.Name != "__init__"
	result := types.None
	if n.Returns != nil {
		result = c.resolveType(n.Returns)
		if n.Name.Name == "__init__" && result != types.Invalid && !types.Equal(result, types.None) {
			c.errs.Add(n.Returns.Pos(), token.TypeMismatch, "__init__ must return None, got %s", result)
		}
	}
	method := newFunction(info.name + "." + n.Name.Name)
	method.mod = c.mod
	method.setParams(params)
	method.result = result
	method.inferResult = inferResult
	method.generator = containsYield(n.Body)
	info.methods[n.Name.Name] = method
	info.methodBody[n.Name.Name] = n.Body
	c.checkSpecialMethod(info, n, method)
}

// checkSpecialMethod eagerly validates the signatures of the restricted set of
// special methods minipy statically dispatches (__len__, __getitem__,
// __setitem__). Parameter counts are checked here; return types are checked
// only when annotated, because unannotated results are inferred from the body
// after class checking and are re-validated at each use site.
func (c *checker) checkSpecialMethod(info *class, n *ast.Function, method *function) {
	switch n.Name.Name {
	case "__len__":
		if len(method.params) != 1 {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s.__len__ must take only self", info.name)
		}
		if n.Returns != nil && method.result != types.Invalid && !types.Equal(method.result, types.Int) {
			c.errs.Add(n.Returns.Pos(), token.TypeMismatch, "%s.__len__ must return int, got %s", info.name, method.result)
		}
	case "__getitem__":
		if len(method.params) != 2 {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s.__getitem__ must take self and an index", info.name)
		}
	case "__setitem__":
		if len(method.params) != 3 {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s.__setitem__ must take self, an index, and a value", info.name)
		}
		if n.Returns != nil && method.result != types.Invalid && !types.Equal(method.result, types.None) {
			c.errs.Add(n.Returns.Pos(), token.TypeMismatch, "%s.__setitem__ must return None, got %s", info.name, method.result)
		}
	}
}

func (c *checker) checkDataclassDefaults(info *class) {
	if !info.dataclass {
		return
	}
	seenDefault := false
	for _, field := range info.fields {
		if field.value != nil {
			seenDefault = true
			continue
		}
		if seenDefault {
			c.errs.Add(field.pos, token.TypeMismatch, "non-default field %q follows default field", field.name)
		}
	}
}

func (c *checker) makeParam(p *ast.Param) parameter {
	t := c.paramType(p)
	// A *args parameter binds the collected surplus as list[T]; **kwargs binds
	// it as dict[str, T]. The annotation gives the element/value type T.
	if p.Vararg {
		t = types.NewList(t)
	} else if p.Kwarg {
		t = types.NewDict(types.Str, t)
	}
	return c.makeParamWithType(p, t)
}

func (c *checker) makeParamWithType(p *ast.Param, t types.Type) parameter {
	return parameter{name: p.Name.Name, typ: t, defaultValue: p.Default, kind: p.Kind, vararg: p.Vararg, kwarg: p.Kwarg}
}

func classTypeFields(fields []classField) []types.Field {
	out := make([]types.Field, len(fields))
	for i, f := range fields {
		out[i] = types.Field{Name: f.name, Type: f.typ}
	}
	return out
}

func (c *checker) nestedFuncs(info *function, body []ast.Stmt) {
	for _, s := range body {
		f, ok := s.(*ast.Function)
		if !ok {
			continue
		}
		if _, exists := info.children[f.Name.Name]; exists {
			c.errs.Add(f.Name.Pos(), token.TypeMismatch, "cannot redeclare function %q", f.Name.Name)
			continue
		}
		child := newFunction(f.Name.Name)
		child.mod = c.mod
		child.generator = containsYield(f.Body)
		child.parent = info
		c.checkParamFeatures(f.Params)
		if f.Returns == nil {
			child.inferResult = true
			child.result = types.None
		} else {
			child.result = c.resolveType(f.Returns)
		}
		for _, p := range f.Params {
			child.addParam(c.makeParam(p))
		}
		info.children[f.Name.Name] = child
		c.declareFuncLocal(child, f.Pos())
	}
}

func srcTypes(params []parameter) []types.Type {
	out := make([]types.Type, len(params))
	for i, p := range params {
		out[i] = p.typ
	}
	return out
}

// paramType resolves a parameter's declared type, defaulting an unannotated
// parameter to Any so whole-program inference can compile it as a dynamic slot.
func (c *checker) paramType(p *ast.Param) types.Type {
	if p.Ann == nil {
		return types.Any
	}
	return c.resolveType(p.Ann)
}

func (c *checker) resolveType(e ast.Expr) types.Type {
	if lit, ok := e.(*ast.StrLit); ok {
		ann, err := parser.ParseType(lit.Value)
		if err != nil {
			c.errs.Add(e.Pos(), token.UnsupportedType, "invalid string annotation %q: %s", lit.Value, err)
			return types.Invalid
		}
		return c.resolveType(ann)
	}
	if name, ok := e.(*ast.Name); ok {
		key := c.resolveName(name.Name).key
		if _, ok := c.aliases[key]; ok {
			return c.resolveAlias(key)
		}
		if t, ok := c.typingScalar(name); ok {
			return t
		}
		if resolved, known := types.Resolve(name.Name); known {
			return resolved
		}
		if cls, known := c.classes[c.resolveName(name.Name).key]; known {
			return cls.typ
		}
		c.errs.Add(e.Pos(), token.UnsupportedType, "unknown type %q", name.Name)
		return types.Invalid
	}
	if u, ok := e.(*ast.UnionType); ok {
		members := make([]types.Type, len(u.Members))
		for i, m := range u.Members {
			mt := c.resolveType(m)
			if mt == types.Invalid {
				return types.Invalid
			}
			members[i] = mt
		}
		return types.NewUnion(members...)
	}
	if sub, ok := e.(*ast.Subscript); ok {
		base, ok := c.annotationHead(sub.X)
		if !ok {
			c.errs.Add(e.Pos(), token.UnsupportedType, "unsupported type annotation")
			return types.Invalid
		}
		switch base {
		case "Annotated":
			return c.annotatedType(sub)
		case "Literal":
			return c.literalType(sub)
		case "list":
			return types.NewList(c.resolveType(sub.Index))
		case "set":
			elem := c.resolveType(sub.Index)
			if elem != types.Invalid && !hashableKey(elem) {
				c.errs.Add(sub.Index.Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
				return types.Invalid
			}
			return types.NewSet(elem)
		case "dict":
			args, ok := sub.Index.(*ast.TupleLit)
			if !ok || len(args.Elems) != 2 {
				c.errs.Add(e.Pos(), token.UnsupportedType, "dict annotation needs key and value types")
				return types.Invalid
			}
			key := c.resolveType(args.Elems[0])
			if key != types.Invalid && !hashableKey(key) {
				c.errs.Add(args.Elems[0].Pos(), token.UnsupportedType, "dict key type %s is not supported", key)
				return types.Invalid
			}
			return types.NewDict(key, c.resolveType(args.Elems[1]))
		case "tuple":
			if args, ok := sub.Index.(*ast.TupleLit); ok {
				elems := make([]types.Type, len(args.Elems))
				for i, elem := range args.Elems {
					elems[i] = c.resolveType(elem)
				}
				return types.NewTuple(elems...)
			}
			return types.NewTuple(c.resolveType(sub.Index))
		case "Iterator":
			return types.NewIterator(c.resolveType(sub.Index))
		case "Optional":
			elem := c.resolveType(sub.Index)
			if elem == types.Invalid {
				return types.Invalid
			}
			return types.NewUnion(elem, types.None)
		case "Union":
			var members []types.Type
			if args, ok := sub.Index.(*ast.TupleLit); ok {
				members = make([]types.Type, len(args.Elems))
				for i, elem := range args.Elems {
					members[i] = c.resolveType(elem)
				}
			} else {
				members = []types.Type{c.resolveType(sub.Index)}
			}
			for _, m := range members {
				if m == types.Invalid {
					return types.Invalid
				}
			}
			return types.NewUnion(members...)
		case "Callable":
			args, ok := sub.Index.(*ast.TupleLit)
			if !ok || len(args.Elems) != 2 {
				c.errs.Add(e.Pos(), token.UnsupportedType, "Callable annotation needs parameter list and return type")
				return types.Invalid
			}
			paramTuple, ok := args.Elems[0].(*ast.TupleLit)
			if !ok {
				c.errs.Add(args.Elems[0].Pos(), token.UnsupportedType, "Callable parameter list must be bracketed")
				return types.Invalid
			}
			params := make([]types.Type, len(paramTuple.Elems))
			for i, elem := range paramTuple.Elems {
				params[i] = c.resolveType(elem)
			}
			return types.NewCallable(params, c.resolveType(args.Elems[1]))
		default:
			c.errs.Add(e.Pos(), token.UnsupportedType, "unknown generic type %q", base)
			return types.Invalid
		}
	}
	if attr, ok := e.(*ast.Attribute); ok {
		if t, ok := c.typingScalar(attr); ok {
			return t
		}
		if mod, ok := c.moduleExpr(attr.X); ok {
			res := c.resolveModuleAttr(mod, attr.Name)
			if res.kind == "class" {
				c.attrSym[attr] = res.key
				return c.classes[res.key].typ
			}
		}
		c.errs.Add(e.Pos(), token.UnsupportedType, "unsupported type annotation")
		return types.Invalid
	}
	c.errs.Add(e.Pos(), token.UnsupportedType, "unsupported type annotation")
	return types.Invalid
}

func (c *checker) typingScalar(e ast.Expr) (types.Type, bool) {
	name, ok := c.annotationHead(e)
	if !ok {
		return nil, false
	}
	switch name {
	case "Any":
		return types.Any, true
	case "TypeAlias":
		return types.TypeAlias, true
	default:
		return nil, false
	}
}

func (c *checker) annotationHead(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Name:
		res := c.resolveName(x.Name)
		if res.kind == "native" && strings.HasPrefix(res.key, "typing.") {
			return strings.TrimPrefix(res.key, "typing."), true
		}
		switch x.Name {
		case "Annotated", "Literal", "TypeAlias":
			return "", false
		}
		return x.Name, true
	case *ast.Attribute:
		if mod, ok := c.moduleExpr(x.X); ok {
			res := c.resolveModuleAttr(mod, x.Name)
			if res.kind == "native" && strings.HasPrefix(res.key, "typing.") {
				c.attrNative[x] = res.native
				return x.Name, true
			}
			if res.kind == "class" {
				c.attrSym[x] = res.key
				return x.Name, true
			}
		}
		return "", false
	default:
		return "", false
	}
}

func (c *checker) annotatedType(sub *ast.Subscript) types.Type {
	args := typeArgs(sub.Index)
	if len(args) < 2 {
		c.errs.Add(sub.Pos(), token.UnsupportedType, "Annotated annotation needs a base type and metadata")
		return types.Invalid
	}
	base := c.resolveType(args[0])
	for _, meta := range args[1:] {
		if !literalMetadata(meta) {
			c.errs.Add(meta.Pos(), token.UnsupportedType, "Annotated metadata must be a literal")
			return types.Invalid
		}
	}
	return base
}

func (c *checker) literalType(sub *ast.Subscript) types.Type {
	args := typeArgs(sub.Index)
	if len(args) == 0 {
		c.errs.Add(sub.Pos(), token.UnsupportedType, "Literal annotation needs at least one value")
		return types.Invalid
	}
	values := make([]types.LiteralValue, len(args))
	for i, arg := range args {
		value, ok := literalValue(arg)
		if !ok {
			c.errs.Add(arg.Pos(), token.UnsupportedType, "unsupported Literal argument")
			return types.Invalid
		}
		values[i] = value
	}
	return types.NewLiteral(values...)
}

func typeArgs(e ast.Expr) []ast.Expr {
	if tuple, ok := e.(*ast.TupleLit); ok {
		return tuple.Elems
	}
	return []ast.Expr{e}
}

func literalMetadata(e ast.Expr) bool {
	switch e.(type) {
	case *ast.IntLit, *ast.FloatLit, *ast.BoolLit, *ast.StrLit, *ast.NoneLit:
		return true
	default:
		return false
	}
}

func literalValue(e ast.Expr) (types.LiteralValue, bool) {
	switch x := e.(type) {
	case *ast.IntLit:
		return types.IntLiteral(x.Value), true
	case *ast.BoolLit:
		return types.BoolLiteral(x.Value), true
	case *ast.StrLit:
		return types.StrLiteral(x.Value), true
	case *ast.NoneLit:
		return types.NoneLiteral(), true
	case *ast.UnaryExpr:
		if lit, ok := x.X.(*ast.IntLit); ok {
			switch x.Op {
			case token.MINUS:
				return types.IntLiteral(-lit.Value), true
			case token.PLUS:
				return types.IntLiteral(lit.Value), true
			}
		}
	}
	return types.LiteralValue{}, false
}

func recursiveByValue(t types.Type, cls *types.Class) bool {
	if t == nil || cls == nil || t == types.Invalid {
		return false
	}
	switch x := t.(type) {
	case *types.Class:
		return types.Equal(x, cls)
	case *types.List:
		return recursiveByValue(x.Elem, cls)
	case *types.Dict:
		return recursiveByValue(x.Key, cls) || recursiveByValue(x.Value, cls)
	case *types.Set:
		return recursiveByValue(x.Elem, cls)
	case *types.Tuple:
		for _, elem := range x.Elems {
			if recursiveByValue(elem, cls) {
				return true
			}
		}
		return false
	case *types.Union:
		return false
	default:
		return false
	}
}

func (c *checker) moduleExpr(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.Name:
		res := c.resolveName(x.Name)
		if res.kind == "module" {
			return res.module, true
		}
	case *ast.Attribute:
		if mod, ok := c.moduleExpr(x.X); ok {
			res := c.resolveModuleAttr(mod, x.Name)
			if res.kind == "module" {
				c.attrMod[x] = res.module
				return res.module, true
			}
		}
	}
	return "", false
}

func (c *checker) functionStmt(n *ast.Function) {
	if n.Async {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "async def is parse-only until scheduler support lands")
	}
	c.checkParamFeatures(n.Params)
	if c.current != nil {
		info := c.current.children[n.Name.Name]
		if info == nil {
			return
		}
		info.local.init = true
		c.checkFunctionBody(n.Body, n.Params, info, n.Pos())
		c.checkFunctionDecorators(n, info, &info.local.init)
		return
	}
	info := c.functions[c.key(n.Name.Name)]
	if info == nil {
		return
	}
	info.slot.init = true
	c.checkFunctionBody(n.Body, n.Params, info, n.Pos())
	c.checkFunctionDecorators(n, info, &info.slot.init)
}

// checkFunctionDecorators type-checks a function's decorator expressions in
// source order and requires each to evaluate to exactly Callable[[F], F],
// where F is the function's own (possibly inferred) signature. The binding is
// temporarily marked uninitialized while decorators are checked, so a
// decorator referring to the function it decorates is diagnosed as
// use-before-definition (Python evaluates decorators before the name binds).
func (c *checker) checkFunctionDecorators(n *ast.Function, info *function, init *bool) {
	if len(n.DecoratorExprs) == 0 {
		return
	}
	f := types.NewCallable(srcTypes(info.params), info.result)
	want := types.NewCallable([]types.Type{f}, f)
	*init = false
	for _, dec := range n.DecoratorExprs {
		got := c.decoratorExprType(dec)
		if got != types.Invalid && !types.Equal(got, want) {
			c.errs.Add(dec.Pos(), token.TypeMismatch, "decorator must be %s, got %s", want, got)
		}
	}
	*init = true
}

// decoratorExprType type-checks one decorator expression restricted to the
// statically resolvable subset accepted by this issue: a bare name, a
// module-qualified attribute, or a call of either. Other AST shapes (arbitrary
// PEP 614 decorator expressions) are rejected without evaluation.
func (c *checker) decoratorExprType(e ast.Expr) types.Type {
	if !c.decoratorShapeOK(e) {
		c.errs.Add(e.Pos(), token.UnsupportedFeature, "unsupported decorator expression")
		return types.Invalid
	}
	return c.expr(e)
}

func (c *checker) decoratorShapeOK(e ast.Expr) bool {
	switch n := e.(type) {
	case *ast.Name:
		return true
	case *ast.Attribute:
		_, ok := c.moduleExpr(n.X)
		return ok
	case *ast.CallExpr:
		return c.decoratorShapeOK(n.Fn)
	default:
		return false
	}
}

func (c *checker) checkParamFeatures(params []*ast.Param) {
	varargs := 0
	for _, p := range params {
		if p.Default != nil {
			dt := c.exprWithHint(p.Default, c.paramType(p))
			pt := c.paramType(p)
			if dt != types.Invalid && pt != types.Invalid && !types.AssignableTo(dt, pt) {
				c.errs.Add(p.Default.Pos(), token.TypeMismatch, "default for %q must be %s, got %s", p.Name.Name, pt, dt)
			}
		}
		if p.Vararg {
			varargs++
			if varargs > 1 {
				c.errs.Add(p.Pos(), token.SyntaxError, "multiple *args parameters are not allowed")
			}
		}
	}
}

func (c *checker) checkFunctionBody(body []ast.Stmt, params []*ast.Param, info *function, pos token.Pos) {
	prev := c.current
	prevMod := c.mod
	c.current = info
	if info.mod != nil {
		c.mod = info.mod
	}
	c.nestedFuncs(info, body)
	for i, p := range info.params {
		if _, exists := info.locals[p.name]; exists {
			c.errs.Add(params[i].Name.Pos(), token.TypeMismatch, "duplicate parameter %q", p.name)
			continue
		}
		info.locals[p.name] = &local{typ: p.typ, index: i, init: true}
	}
	if info.generator {
		if _, ok := info.result.(*types.Iterator); !ok && info.result != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "generator function %q must return Iterator[T], got %s", info.name, info.result)
		}
	}
	c.checkBlock(body)
	if info.inferResult {
		// Infer the return type as the join of every return expression's type;
		// a body with no value-returns is None.
		if len(info.returns) == 0 {
			info.result = types.None
		} else {
			result := info.returns[0]
			for _, next := range info.returns[1:] {
				result = types.Join(result, next)
			}
			info.result = result
		}
	}
	if !info.generator && !types.Equal(info.result, types.None) && !blockReturns(body) {
		c.errs.Add(pos, token.TypeMismatch, "function %q may fall through without returning %s", info.name, info.result)
	}
	c.current = prev
	c.mod = prevMod
}

func (c *checker) returnStmt(n *ast.Return) {
	if c.current == nil {
		c.errs.Add(n.Pos(), token.SyntaxError, "'return' outside function")
		if n.Value != nil {
			c.expr(n.Value)
		}
		return
	}
	if c.current.generator {
		if n.Value != nil {
			c.errs.Add(n.Pos(), token.TypeMismatch, "generator function cannot return a value")
			c.expr(n.Value)
		}
		return
	}
	result := types.Type(types.None)
	if n.Value != nil {
		if c.current.inferResult {
			result = c.expr(n.Value)
		} else {
			result = c.exprWithHint(n.Value, c.current.result)
		}
	}
	if c.current.inferResult {
		// Return type is being inferred: collect this branch's type instead of
		// checking against a fixed annotation.
		c.current.returns = append(c.current.returns, result)
		return
	}
	if c.current.result != types.Invalid && result != types.Invalid && !types.AssignableTo(result, c.current.result) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "cannot return %s from function returning %s", result, c.current.result)
	}
}

func (c *checker) yieldStmt(n *ast.Yield) {
	c.checkYield(n.Value, n.From, n.Pos())
}

func (c *checker) checkYield(value ast.Expr, from bool, pos token.Pos) {
	if c.current == nil {
		c.errs.Add(pos, token.SyntaxError, "'yield' outside function")
		if value != nil {
			c.expr(value)
		}
		return
	}
	iter, ok := c.current.result.(*types.Iterator)
	if !ok {
		if c.current.result != types.Invalid {
			c.errs.Add(pos, token.TypeMismatch, "yield in function returning %s; expected Iterator[T]", c.current.result)
		}
		if value != nil {
			c.expr(value)
		}
		return
	}
	if from {
		if value == nil {
			c.errs.Add(pos, token.SyntaxError, "'yield from' needs an iterable")
			return
		}
		vt := c.expr(value)
		if vt != types.Invalid && !c.iterableType(vt) {
			c.errs.Add(pos, token.TypeMismatch, "'yield from' needs an iterable, got %s", vt)
			return
		}
		et := types.None
		if vt != types.Invalid {
			et = iterableElem(vt)
		}
		if et != types.Invalid && iter.Elem != types.Invalid && !types.AssignableTo(et, iter.Elem) {
			c.errs.Add(pos, token.TypeMismatch, "cannot yield from %s into generator yielding %s", vt, iter.Elem)
		}
		return
	}
	yt := types.None
	if value != nil {
		yt = c.exprWithHint(value, iter.Elem)
	}
	if yt != types.Invalid && iter.Elem != types.Invalid && !types.AssignableTo(yt, iter.Elem) {
		c.errs.Add(pos, token.TypeMismatch, "cannot yield %s from generator yielding %s", yt, iter.Elem)
	}
}

func (c *checker) iterableType(t types.Type) bool {
	switch t.(type) {
	case *types.Iterator, *types.Dict, *types.Set, *types.List:
		return true
	default:
		return types.Equal(t, types.Str)
	}
}

// synthGenExpr builds a hidden generator function whose body is equivalent to
// `for target in iter: if cond: yield elem`, so a generator expression lowers
// to a lazily resumed coroutine instead of an eagerly built list.
func (c *checker) synthGenExpr(n *ast.GeneratorExp, elem types.Type) *function {
	info := newFunction("<genexpr>")
	info.result = types.NewIterator(elem)
	info.inferResult = false
	info.generator = true
	info.parent = c.current
	info.mod = c.mod
	info.body = c.synthGenBody(n.Clauses, n.Elem)
	c.checkFunctionBody(info.body, nil, info, n.Pos())
	return info
}

func (c *checker) synthGenBody(clauses []*ast.Comprehension, elem ast.Expr) []ast.Stmt {
	inner := []ast.Stmt{&ast.Yield{Base: ast.Base{Position: elem.Pos()}, Value: elem}}
	for i := len(clauses) - 1; i >= 0; i-- {
		clause := clauses[i]
		body := inner
		for j := len(clause.Ifs) - 1; j >= 0; j-- {
			body = []ast.Stmt{&ast.If{Base: ast.Base{Position: clause.Ifs[j].Pos()}, Cond: clause.Ifs[j], Body: body}}
		}
		inner = []ast.Stmt{&ast.For{Base: ast.Base{Position: clause.Pos()}, Target: clause.Target, Iter: clause.Iter, Body: body}}
	}
	return inner
}

func blockReturns(body []ast.Stmt) bool {
	for _, s := range body {
		switch n := s.(type) {
		case *ast.Return:
			return true
		case *ast.If:
			if len(n.Orelse) > 0 && blockReturns(n.Body) && blockReturns(n.Orelse) {
				return true
			}
		case *ast.Try:
			if blockReturns(n.Finalbody) {
				return true
			}
			if len(n.Handlers) == 0 && blockReturns(n.Body) {
				return true
			}
			if len(n.Handlers) > 0 && blockTerminates(n.Body) {
				all := true
				for _, h := range n.Handlers {
					all = all && blockReturns(h.Body)
				}
				if all {
					return true
				}
			}
		case *ast.With:
			if blockReturns(n.Body) {
				return true
			}
		}
	}
	return false
}

func blockTerminates(body []ast.Stmt) bool {
	for _, s := range body {
		switch n := s.(type) {
		case *ast.Return, *ast.Raise:
			return true
		case *ast.If:
			if len(n.Orelse) > 0 && blockTerminates(n.Body) && blockTerminates(n.Orelse) {
				return true
			}
		case *ast.Try:
			if blockReturns(n.Finalbody) || blockReturns(n.Body) {
				return true
			}
		case *ast.With:
			if blockTerminates(n.Body) {
				return true
			}
		}
	}
	return false
}

func containsYield(body []ast.Stmt) bool {
	for _, s := range body {
		if stmtHasYield(s) {
			return true
		}
	}
	return false
}

func stmtHasYield(s ast.Stmt) bool {
	switch n := s.(type) {
	case *ast.Yield:
		return true
	case *ast.ExprStmt:
		return exprHasYield(n.X)
	case *ast.Return:
		return n.Value != nil && exprHasYield(n.Value)
	case *ast.AnnAssign:
		return n.Value != nil && exprHasYield(n.Value)
	case *ast.Assign:
		return exprHasYield(n.Value) || exprHasYield(n.Target)
	case *ast.AugAssign:
		return exprHasYield(n.Value) || exprHasYield(n.Target)
	case *ast.Assert:
		return (n.Test != nil && exprHasYield(n.Test)) || (n.Msg != nil && exprHasYield(n.Msg))
	case *ast.Raise:
		return (n.Exc != nil && exprHasYield(n.Exc)) || (n.Cause != nil && exprHasYield(n.Cause))
	case *ast.If:
		return exprHasYield(n.Cond) || containsYield(n.Body) || containsYield(n.Orelse)
	case *ast.While:
		return exprHasYield(n.Cond) || containsYield(n.Body) || containsYield(n.Orelse)
	case *ast.For:
		return exprHasYield(n.Iter) || exprHasYield(n.Target) || containsYield(n.Body) || containsYield(n.Orelse)
	case *ast.Match:
		if n.Subject != nil && exprHasYield(n.Subject) {
			return true
		}
		for _, c := range n.Cases {
			if containsYield(c.Body) {
				return true
			}
		}
	case *ast.Try:
		if containsYield(n.Body) || containsYield(n.Orelse) || containsYield(n.Finalbody) {
			return true
		}
		for _, h := range n.Handlers {
			if containsYield(h.Body) {
				return true
			}
		}
	case *ast.With:
		for _, item := range n.Items {
			if exprHasYield(item.Context) {
				return true
			}
		}
		return containsYield(n.Body)
	case *ast.Function, *ast.Class:
		return false
	}
	return false
}

func exprHasYield(e ast.Expr) bool {
	if e == nil {
		return false
	}
	switch n := e.(type) {
	case *ast.YieldExpr:
		return true
	case *ast.UnaryExpr:
		return exprHasYield(n.X)
	case *ast.BinaryExpr:
		return exprHasYield(n.X) || exprHasYield(n.Y)
	case *ast.BoolOp:
		return exprHasYield(n.X) || exprHasYield(n.Y)
	case *ast.Compare:
		if exprHasYield(n.X) {
			return true
		}
		for _, c := range n.Comparators {
			if exprHasYield(c) {
				return true
			}
		}
	case *ast.CallExpr:
		if exprHasYield(n.Fn) {
			return true
		}
		for _, a := range n.Args {
			if exprHasYield(a) {
				return true
			}
		}
	case *ast.IfExp:
		return exprHasYield(n.Body) || exprHasYield(n.Cond) || exprHasYield(n.Orelse)
	case *ast.NamedExpr:
		return exprHasYield(n.Value)
	case *ast.LambdaExpr:
		return false
	case *ast.AwaitExpr:
		return exprHasYield(n.X)
	case *ast.Starred:
		return exprHasYield(n.X)
	case *ast.Attribute:
		return exprHasYield(n.X)
	case *ast.Subscript:
		return exprHasYield(n.X) || exprHasYield(n.Index)
	case *ast.Slice:
		return exprHasYield(n.Lower) || exprHasYield(n.Upper) || exprHasYield(n.Step)
	case *ast.TupleLit:
		for _, e := range n.Elems {
			if exprHasYield(e) {
				return true
			}
		}
	case *ast.ListLit:
		for _, e := range n.Elems {
			if exprHasYield(e) {
				return true
			}
		}
	case *ast.DictLit:
		for i, k := range n.Keys {
			if exprHasYield(k) || exprHasYield(n.Values[i]) {
				return true
			}
		}
	case *ast.SetLit:
		for _, e := range n.Elems {
			if exprHasYield(e) {
				return true
			}
		}
	case *ast.GeneratorExp:
		if exprHasYield(n.Elem) {
			return true
		}
		return compHasYield(n.Clauses)
	case *ast.ListComp:
		if exprHasYield(n.Elem) {
			return true
		}
		return compHasYield(n.Clauses)
	case *ast.SetComp:
		if exprHasYield(n.Elem) {
			return true
		}
		return compHasYield(n.Clauses)
	case *ast.DictComp:
		if exprHasYield(n.Key) || exprHasYield(n.Value) {
			return true
		}
		return compHasYield(n.Clauses)
	case *ast.FString:
		for _, p := range n.Parts {
			if f, ok := p.(*ast.FStringExpr); ok && exprHasYield(f.Expr) {
				return true
			}
		}
	}
	return false
}

func compHasYield(clauses []*ast.Comprehension) bool {
	for _, cl := range clauses {
		if exprHasYield(cl.Iter) {
			return true
		}
		for _, f := range cl.Ifs {
			if exprHasYield(f) {
				return true
			}
		}
	}
	return false
}

// specializable reports whether a function may be monomorphized: it has at
// least one union/Any parameter (so distinct concrete call types are possible)
// and is not a generator. Whether a given instantiation actually type-checks is
// decided at the call site, which rolls back and falls back to the union/Any
// body on any error.
func specializable(info *function) bool {
	if info.generator || info.decorated {
		return false
	}
	for _, p := range info.params {
		if _, ok := p.typ.(*types.Union); ok {
			return true
		}
		if types.IsAny(p.typ) {
			return true
		}
	}
	return false
}

// specialize returns the monomorphic instantiation of function for the given concrete
// argument tuple, creating it on first use. It returns nil — falling back to the
// single union/Any-typed body — when the function is not specializable, an
// argument is not concrete, the per-function cap is reached, the instantiation
// recurses, or re-checking the reachable body under the concrete types produces
// any error.
func (c *checker) specialize(info *function, argTypes []types.Type) *specialization {
	if !info.specializable {
		return nil
	}
	params := make([]parameter, len(info.params))
	signature := make([]types.Type, len(info.params))
	for i, p := range info.params {
		arg := argTypes[i]
		if _, isU := p.typ.(*types.Union); isU || types.IsAny(p.typ) {
			if !concrete(arg) {
				return nil
			}
			params[i] = p
			params[i].typ = arg
		} else {
			if arg == types.Invalid || !types.AssignableTo(arg, p.typ) {
				return nil
			}
			params[i] = p
		}
		signature[i] = params[i].typ
	}

	key := specKey(signature)
	for _, s := range info.instances {
		if s.key == key {
			return s
		}
	}
	if len(info.instances) >= maxSpecializations {
		return nil
	}
	guard := info.name + "#" + key
	if c.specActive[guard] {
		return nil
	}

	clone := newFunction(info.name + "$" + key)
	clone.mod = info.mod
	clone.setParams(params)
	clone.inferResult = info.inferResult
	clone.result = info.result
	clone.generator = info.generator
	clone.body = info.body
	clone.astParams = info.astParams
	if clone.inferResult {
		clone.result = types.None
	}

	errMark := len(c.errs)
	savedTypes, savedNarrow, savedCalls, savedArgs := c.types, c.narrowed, c.callSpec, c.callArgs
	c.types = map[ast.Expr]types.Type{}
	c.narrowed = map[string]types.Type{}
	c.callSpec = map[*ast.CallExpr]*specialization{}
	c.callArgs = map[*ast.CallExpr][]ast.Expr{}
	c.specActive[guard] = true
	c.checkFunctionBody(clone.body, clone.astParams, clone, token.Pos{})
	delete(c.specActive, guard)
	itypes, icalls, iargs := c.types, c.callSpec, c.callArgs
	c.types, c.narrowed, c.callSpec, c.callArgs = savedTypes, savedNarrow, savedCalls, savedArgs

	if len(c.errs) > errMark {
		c.errs = c.errs[:errMark] // discard: this tuple cannot specialize
		return nil
	}
	spec := &specialization{key: key, params: signature, info: clone, types: itypes, calls: icalls, args: iargs}
	info.instances = append(info.instances, spec)
	return spec
}

// concrete reports whether a type is a single non-dynamic type that a
// specialization can bind a parameter to.
func concrete(t types.Type) bool {
	if t == nil || t == types.Invalid || types.IsAny(t) {
		return false
	}
	switch t.(type) {
	case *types.Union, *types.TypeVar:
		return false
	}
	return true
}

// specKey is the canonical signature key for a concrete parameter tuple.
func specKey(ts []types.Type) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = t.String()
	}
	return strings.Join(parts, ",")
}

// expr types an expression, records the result, and returns it.
func (c *checker) expr(e ast.Expr) types.Type {
	return c.exprWithHint(e, nil)
}

func (c *checker) exprWithHint(e ast.Expr, hint types.Type) types.Type {
	if value, ok := literalValue(e); ok {
		if lit := literalHint(hint, value); lit != nil {
			t := types.NewLiteral(value)
			c.types[e] = t
			return t
		}
	}
	if lit, ok := hint.(*types.Literal); ok {
		if lit.Base != nil {
			hint = lit.Base
		}
	}
	t := c.typeOf(e, hint)
	c.types[e] = t
	return t
}

func literalHint(hint types.Type, value types.LiteralValue) *types.Literal {
	switch t := hint.(type) {
	case *types.Literal:
		if t.Contains(value) {
			return t
		}
	case *types.Union:
		for _, member := range t.Members {
			if lit := literalHint(member, value); lit != nil {
				return lit
			}
		}
	}
	return nil
}

func (c *checker) typeOf(e ast.Expr, hint types.Type) types.Type {
	switch n := e.(type) {
	case *ast.IntLit:
		return types.Int
	case *ast.FloatLit:
		return types.Float
	case *ast.BoolLit:
		return types.Bool
	case *ast.StrLit:
		return types.Str
	case *ast.BytesLit:
		return types.Bytes
	case *ast.NoneLit:
		return types.None
	case *ast.Name:
		return c.nameType(n)
	case *ast.UnaryExpr:
		return c.unaryType(n)
	case *ast.BinaryExpr:
		return c.binary(n)
	case *ast.BoolOp:
		return c.boolOpType(n)
	case *ast.Compare:
		return c.compareType(n)
	case *ast.CallExpr:
		return c.callType(n)
	case *ast.IfExp:
		return c.ifExpType(n)
	case *ast.ListLit:
		return c.listType(n, hint)
	case *ast.DictLit:
		return c.dictType(n, hint)
	case *ast.TupleLit:
		elems := make([]types.Type, len(n.Elems))
		for i, elem := range n.Elems {
			elems[i] = c.expr(elem)
		}
		return types.NewTuple(elems...)
	case *ast.LambdaExpr:
		return c.lambda(n, hint)
	case *ast.SetLit:
		return c.setType(n, hint)
	case *ast.ListComp:
		elem := c.compElem(n.Clauses, n.Elem)
		return types.NewList(elem)
	case *ast.DictComp:
		var key, value types.Type
		c.compClauses(n.Clauses, func() {
			key = c.expr(n.Key)
			value = c.expr(n.Value)
		})
		if !hashableKey(key) {
			c.errs.Add(n.Key.Pos(), token.UnsupportedType, "dict key type %s is not supported", key)
			return types.Invalid
		}
		return types.NewDict(key, value)
	case *ast.SetComp:
		elem := c.compElem(n.Clauses, n.Elem)
		if !hashableKey(elem) {
			c.errs.Add(n.Elem.Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
			return types.Invalid
		}
		return types.NewSet(elem)
	case *ast.Subscript:
		receiver := c.expr(n.X)
		if slice, ok := n.Index.(*ast.Slice); ok {
			return c.sliceResultType(slice, receiver)
		}
		index := c.expr(n.Index)
		return c.indexResultType(n, receiver, index)
	case *ast.Slice:
		c.checkSliceBounds(n)
		return types.Invalid
	case *ast.Starred:
		c.expr(n.X)
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "starred unpacking is not supported yet")
		return types.Invalid
	case *ast.NamedExpr:
		c.assign(&ast.Assign{Base: n.Base, Target: n.Target, Value: n.Value})
		return c.currentType(n.Target.Name)
	case *ast.AwaitExpr:
		c.expr(n.X)
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "await is parse-only until scheduler support lands")
		return types.Invalid
	case *ast.YieldExpr:
		if c.current != nil {
			c.current.generator = true
		}
		c.checkYield(n.Value, n.From, n.Pos())
		return types.None
	case *ast.GeneratorExp:
		elem := c.compElem(n.Clauses, n.Elem)
		t := types.NewIterator(elem)
		if c.genExprs[n] == nil {
			c.genExprs[n] = c.synthGenExpr(n, elem)
		}
		return t
	case *ast.Attribute:
		return c.fieldType(n, c.expr(n.X))
	case *ast.FString:
		c.fstring(n)
		return types.Str
	default:
		return types.Invalid
	}
}

func (c *checker) listType(n *ast.ListLit, hint types.Type) types.Type {
	if len(n.Elems) == 0 {
		if list, ok := hint.(*types.List); ok {
			return list
		}
		c.errs.Add(n.Pos(), token.UnsupportedType, "empty list needs list[T] annotation")
		return types.Invalid
	}
	elem := c.listElemType(n.Elems[0])
	for _, e := range n.Elems[1:] {
		et := c.listElemType(e)
		if elem != types.Invalid && et != types.Invalid && !types.Equal(elem, et) {
			c.errs.Add(e.Pos(), token.TypeMismatch, "list elements must have same type: %s and %s", elem, et)
			return types.Invalid
		}
	}
	return types.NewList(elem)
}

func (c *checker) listElemType(e ast.Expr) types.Type {
	if star, ok := e.(*ast.Starred); ok {
		t := c.expr(star.X)
		switch x := t.(type) {
		case *types.List:
			return x.Elem
		case *types.Tuple:
			return homogeneous(x.Elems)
		default:
			if t != types.Invalid {
				c.errs.Add(star.Pos(), token.TypeMismatch, "starred list element must be list or tuple, got %s", t)
			}
			return types.Invalid
		}
	}
	return c.expr(e)
}

func (c *checker) dictType(n *ast.DictLit, hint types.Type) types.Type {
	if len(n.Keys) == 0 {
		if dt, ok := hint.(*types.Dict); ok {
			return dt
		}
		c.errs.Add(n.Pos(), token.UnsupportedType, "empty dict needs dict[K, V] annotation")
		return types.Invalid
	}
	for i, keyExpr := range n.Keys {
		if star, ok := keyExpr.(*ast.Starred); ok || n.Values[i] == nil {
			if ok {
				c.expr(star.X)
			} else {
				c.errs.Add(keyExpr.Pos(), token.UnsupportedFeature, "dict unpacking is not supported yet")
				return types.Invalid
			}
		}
	}
	key, value := c.dictEntryType(n.Keys[0], n.Values[0])
	for i := 1; i < len(n.Keys); i++ {
		k, v := c.dictEntryType(n.Keys[i], n.Values[i])
		if key != types.Invalid && k != types.Invalid && !types.Equal(key, k) {
			c.errs.Add(n.Keys[i].Pos(), token.TypeMismatch, "dict keys must have same type: %s and %s", key, k)
			return types.Invalid
		}
		if value != types.Invalid && v != types.Invalid && !types.Equal(value, v) {
			c.errs.Add(n.Values[i].Pos(), token.TypeMismatch, "dict values must have same type: %s and %s", value, v)
			return types.Invalid
		}
	}
	if !hashableKey(key) {
		c.errs.Add(n.Keys[0].Pos(), token.UnsupportedType, "dict key type %s is not supported", key)
		return types.Invalid
	}
	return types.NewDict(key, value)
}

func (c *checker) dictEntryType(key, value ast.Expr) (types.Type, types.Type) {
	if star, ok := key.(*ast.Starred); ok && value == nil {
		t := c.expr(star.X)
		if d, ok := t.(*types.Dict); ok {
			return d.Key, d.Value
		}
		if t != types.Invalid {
			c.errs.Add(star.Pos(), token.TypeMismatch, "dict unpacking requires dict, got %s", t)
		}
		return types.Invalid, types.Invalid
	}
	return c.expr(key), c.expr(value)
}

func hashableKey(t types.Type) bool {
	t = types.Erase(t)
	return types.Equal(t, types.Int) || types.Equal(t, types.Float) || types.Equal(t, types.Bool) || types.Equal(t, types.Str)
}

func (c *checker) indexResultType(n *ast.Subscript, receiver, index types.Type) types.Type {
	switch t := receiver.(type) {
	case *types.List:
		if index != types.Invalid && !types.Equal(index, types.Int) {
			c.errs.Add(n.Index.Pos(), token.TypeMismatch, "list index must be int, got %s", index)
		}
		return t.Elem
	case *types.Dict:
		if index != types.Invalid && !types.AssignableTo(index, t.Key) {
			c.errs.Add(n.Index.Pos(), token.TypeMismatch, "dict key must be %s, got %s", t.Key, index)
		}
		return t.Value
	case *types.Tuple:
		if idx, ok := constTupleIndex(n.Index); ok {
			if idx < 0 || idx >= len(t.Elems) {
				c.errs.Add(n.Index.Pos(), token.IntOverflow, "tuple index %d out of range", idx)
				return types.Invalid
			}
			return t.Elems[idx]
		}
		c.errs.Add(n.Index.Pos(), token.UnsupportedFeature, "tuple index must be a constant int")
		return types.Invalid
	case *types.Class:
		return c.classGetItemType(n, t, index)
	default:
		if types.Equal(receiver, types.Str) {
			if index != types.Invalid && !types.Equal(index, types.Int) {
				c.errs.Add(n.Index.Pos(), token.TypeMismatch, "str index must be int, got %s", index)
			}
			return types.Str
		}
		if types.Equal(receiver, types.Bytes) {
			if index != types.Invalid && !types.Equal(index, types.Int) {
				c.errs.Add(n.Index.Pos(), token.TypeMismatch, "bytes index must be int, got %s", index)
			}
			return types.Int
		}
		if receiver != types.Invalid {
			c.errs.Add(n.Pos(), token.NotIndexable, "%s is not indexable", receiver)
		}
		return types.Invalid
	}
}

// classGetItemType resolves obj[index] for a class instance via __getitem__,
// validating the index against the method's declared index parameter.
func (c *checker) classGetItemType(n *ast.Subscript, cls *types.Class, index types.Type) types.Type {
	info := c.classes[cls.Name]
	if info == nil {
		return types.Invalid
	}
	m := method(info, "__getitem__")
	if m == nil || len(m.params) != 2 {
		c.errs.Add(n.Pos(), token.NotIndexable, "%s is not indexable", cls)
		return types.Invalid
	}
	param := m.params[1].typ
	if index != types.Invalid && param != types.Invalid && !types.AssignableTo(index, param) {
		c.errs.Add(n.Index.Pos(), token.TypeMismatch, "%s.__getitem__ index must be %s, got %s", info.name, param, index)
	}
	return m.result
}

func (c *checker) sliceResultType(n *ast.Slice, receiver types.Type) types.Type {
	c.checkSliceBounds(n)
	switch receiver.(type) {
	case *types.List:
		return receiver
	default:
		if types.Equal(receiver, types.Str) {
			return types.Str
		}
		if types.Equal(receiver, types.Bytes) {
			return types.Bytes
		}
		if receiver != types.Invalid {
			c.errs.Add(n.Pos(), token.NotIndexable, "%s is not sliceable", receiver)
		}
		return types.Invalid
	}
}

func (c *checker) checkSliceBounds(n *ast.Slice) {
	for _, expr := range []ast.Expr{n.Lower, n.Upper, n.Step} {
		if expr == nil {
			continue
		}
		t := c.expr(expr)
		if t != types.Invalid && !types.Equal(t, types.Int) {
			c.errs.Add(expr.Pos(), token.TypeMismatch, "slice bounds must be int, got %s", t)
		}
	}
}

func (c *checker) fieldType(n *ast.Attribute, receiver types.Type) types.Type {
	if mod, ok := receiver.(*types.Module); ok {
		res := c.resolveModuleAttr(mod.Name, n.Name)
		switch res.kind {
		case "module":
			c.attrMod[n] = res.module
			return types.NewModule(res.module)
		case "native":
			c.attrNative[n] = res.native
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "native function %q is not a first-class value", nativeDisplay(res.key))
			return types.Invalid
		case "function":
			info := c.functions[res.key]
			c.attrSym[n] = res.key
			return types.NewCallable(srcTypes(info.params), info.result)
		case "class":
			c.attrSym[n] = res.key
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "class value %q is not supported", n.Name)
			return types.Invalid
		case "global":
			c.attrSym[n] = res.key
			return c.globals[res.key].typ
		}
		c.errs.Add(n.Pos(), token.UndefinedName, "module %q has no attribute %q", mod.Name, n.Name)
		return types.Invalid
	}
	cls, ok := receiver.(*types.Class)
	if !ok {
		if receiver != types.Invalid {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "attribute %s on %s is not supported", n.Name, receiver)
		}
		return types.Invalid
	}
	info := c.classes[cls.Name]
	if info == nil {
		return types.Invalid
	}
	idx, ok := info.fieldIndex[n.Name]
	if !ok {
		c.errs.Add(n.Pos(), token.UndefinedName, "field %q is not defined on %s", n.Name, cls.Name)
		return types.Invalid
	}
	return info.fields[idx].typ
}

func constTupleIndex(e ast.Expr) (int, bool) {
	if lit, ok := e.(*ast.IntLit); ok {
		return int(lit.Value), true
	}
	return 0, false
}

func (c *checker) fstring(n *ast.FString) {
	for _, part := range n.Parts {
		c.fstringPart(part)
	}
}

func (c *checker) fstringPart(part ast.FStringPart) {
	expr, ok := part.(*ast.FStringExpr)
	if !ok {
		return
	}
	t := c.expr(expr.Expr)
	if t != types.Invalid && !types.Printable(t) {
		c.errs.Add(expr.Pos(), token.TypeMismatch, "unsupported type %s in f-string replacement field", t)
	}
	if expr.Conversion != 0 && expr.Conversion != 's' && expr.Conversion != 'r' && expr.Conversion != 'a' {
		c.errs.Add(expr.Pos(), token.UnsupportedFeature, "unsupported f-string conversion !%c", expr.Conversion)
	}
	for _, fp := range expr.Format {
		// Nested replacement fields inside a format spec may not carry their
		// own format spec: only one level of nesting is supported.
		if nested, ok := fp.(*ast.FStringExpr); ok && len(nested.Format) > 0 {
			c.errs.Add(nested.Pos(), token.UnsupportedFeature, "f-string format spec nesting is too deep")
		}
		c.fstringPart(fp)
	}
}

// ifExpType types the conditional expression `body if cond else orelse`: cond
// must be bool and the two arms must share a type (docs/spec/04-static-semantics.md).
func (c *checker) ifExpType(n *ast.IfExp) types.Type {
	c.condition(n.Cond)
	bt := c.expr(n.Body)
	et := c.expr(n.Orelse)
	if bt == types.Invalid || et == types.Invalid {
		return types.Invalid
	}
	if !types.Equal(bt, et) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "conditional expression arms have different types: %s and %s", bt, et)
		return types.Invalid
	}
	return bt
}

func (c *checker) nameType(n *ast.Name) types.Type {
	if t, ok := c.temps[n.Name]; ok {
		return t
	}
	if t, ok := c.narrowed[n.Name]; ok {
		return t
	}
	if c.current != nil {
		if c.current.globals[n.Name] {
			return c.global(n)
		}
		if l, ok := c.current.locals[n.Name]; ok {
			if !l.init {
				c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
			}
			return l.typ
		}
		if cap := c.capture(n); cap != nil {
			return cap.typ
		}
	}
	res := c.resolveName(n.Name)
	switch res.kind {
	case "module":
		return types.NewModule(res.module)
	case "native":
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "native function %q is not a first-class value", nativeDisplay(res.key))
		return types.Invalid
	case "function":
		info := c.functions[res.key]
		if !info.slot.init {
			c.errs.Add(n.Pos(), token.UseBeforeDefinition, "function %q used before definition", n.Name)
		}
		return types.NewCallable(srcTypes(info.params), info.result)
	case "class":
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "class value %q is not supported", n.Name)
		return types.Invalid
	}
	return c.global(n)
}

func (c *checker) global(n *ast.Name) types.Type {
	res := c.resolveName(n.Name)
	if res.kind == "module" {
		c.errs.Add(n.Pos(), token.UnsupportedFeature, "module object is not a first-class value")
		return types.Invalid
	}
	g, ok := c.globals[res.key]
	if !ok {
		c.errs.Add(n.Pos(), token.UndefinedName, "name %q is not defined", n.Name)
		return types.Invalid
	}
	if !g.init {
		c.errs.Add(n.Pos(), token.UseBeforeDefinition, "name %q used before assignment", n.Name)
	}
	return g.typ
}

func (c *checker) findEnclosingLocal(name string) *local {
	for scope := c.current.parent; scope != nil; scope = scope.parent {
		if l, ok := scope.locals[name]; ok {
			return l
		}
	}
	return nil
}

func (c *checker) capture(n *ast.Name) *capture {
	if c.current == nil {
		return nil
	}
	if cap, ok := c.current.captures[n.Name]; ok {
		return cap
	}
	for scope := c.current.parent; scope != nil; scope = scope.parent {
		if l, ok := scope.locals[n.Name]; ok {
			if c.current.parent != scope {
				ensureCapture(c.current.parent, n.Name, l)
			}
			cap := &capture{name: n.Name, typ: l.typ, index: len(c.current.capOrder), src: l, boxed: l.boxed}
			c.current.captures[n.Name] = cap
			c.current.capOrder = append(c.current.capOrder, n.Name)
			return cap
		}
	}
	if c.current.nonlocal[n.Name] {
		c.errs.Add(n.Pos(), token.NoBindingForNonlocal, "no binding for nonlocal %q found", n.Name)
		return nil
	}
	return nil
}

func ensureCapture(current *function, name string, src *local) {
	if current == nil {
		return
	}
	if _, ok := current.locals[name]; ok {
		return
	}
	if _, ok := current.captures[name]; ok {
		return
	}
	if current.parent != nil {
		ensureCapture(current.parent, name, src)
	}
	current.captures[name] = &capture{name: name, typ: src.typ, index: len(current.capOrder), src: src, boxed: src.boxed}
	current.capOrder = append(current.capOrder, name)
}

func (c *checker) lambda(n *ast.LambdaExpr, hint types.Type) types.Type {
	callable, ok := hint.(*types.Callable)
	if !ok {
		c.errs.Add(n.Pos(), token.MissingAnnotation, "lambda parameter types need a Callable context")
		return types.Invalid
	}
	if len(n.Params) != len(callable.Params) {
		c.errs.Add(n.Pos(), token.ArityMismatch, "lambda expects %d parameter types, got %d", len(n.Params), len(callable.Params))
		return types.Invalid
	}
	info := newFunction("<lambda>")
	info.result = callable.Return
	info.parent = c.current
	for i, p := range n.Params {
		info.addParam(parameter{name: p.Name.Name, typ: callable.Params[i]})
		p.Ann = typeExpr(p.Pos(), callable.Params[i])
	}
	prev := c.current
	c.current = info
	for i, p := range info.params {
		info.locals[p.name] = &local{typ: p.typ, index: i, init: true}
	}
	bt := c.exprWithHint(n.Body, callable.Return)
	if bt != types.Invalid && callable.Return != types.Invalid && !types.AssignableTo(bt, callable.Return) {
		c.errs.Add(n.Body.Pos(), token.TypeMismatch, "cannot return %s from lambda returning %s", bt, callable.Return)
	}
	c.current = prev
	c.lambdas[n] = info
	return callable
}

func typeExpr(pos token.Pos, t types.Type) ast.Expr {
	return &ast.Name{Base: ast.Base{Position: pos}, Name: t.String()}
}

func (c *checker) setType(n *ast.SetLit, hint types.Type) types.Type {
	if len(n.Elems) == 0 {
		if st, ok := hint.(*types.Set); ok {
			return st
		}
		c.errs.Add(n.Pos(), token.UnsupportedType, "empty set needs set[T] annotation")
		return types.Invalid
	}
	elem := c.setElemType(n.Elems[0])
	for _, e := range n.Elems[1:] {
		et := c.setElemType(e)
		if elem != types.Invalid && et != types.Invalid && !types.Equal(elem, et) {
			c.errs.Add(e.Pos(), token.TypeMismatch, "set elements must have same type: %s and %s", elem, et)
			return types.Invalid
		}
	}
	if !hashableKey(elem) {
		c.errs.Add(n.Elems[0].Pos(), token.UnsupportedType, "set element type %s is not supported", elem)
		return types.Invalid
	}
	return types.NewSet(elem)
}

func (c *checker) setElemType(e ast.Expr) types.Type {
	if star, ok := e.(*ast.Starred); ok {
		t := c.expr(star.X)
		if s, ok := t.(*types.Set); ok {
			return s.Elem
		}
		if t != types.Invalid {
			c.errs.Add(star.Pos(), token.TypeMismatch, "starred set element must be set, got %s", t)
		}
		return types.Invalid
	}
	return c.expr(e)
}

func (c *checker) compElem(clauses []*ast.Comprehension, elem ast.Expr) types.Type {
	var typ types.Type
	c.compClauses(clauses, func() {
		typ = c.expr(elem)
	})
	return typ
}

func (c *checker) compClauses(clauses []*ast.Comprehension, body func()) {
	var walk func(int)
	walk = func(i int) {
		if i == len(clauses) {
			body()
			return
		}
		clause := clauses[i]
		if clause.Async {
			c.errs.Add(clause.Pos(), token.UnsupportedFeature, "async comprehensions are parse-only until scheduler support lands")
		}
		iter := c.expr(clause.Iter)
		elem := iterableElem(iter)
		if elem == types.Invalid {
			c.errs.Add(clause.Iter.Pos(), token.NotIterable, "%s is not iterable", iter)
		}

		prev, hadPrev := c.temps[clause.Target.Name]
		c.temps[clause.Target.Name] = elem
		c.types[clause.Target] = elem
		defer func() {
			if hadPrev {
				c.temps[clause.Target.Name] = prev
			} else {
				delete(c.temps, clause.Target.Name)
			}
		}()

		for _, ifExpr := range clause.Ifs {
			c.condition(ifExpr)
		}
		walk(i + 1)
	}
	walk(0)
}

// unaryType, binary, binaryType, and compareType delegate to the operator
// module, the single source of operator semantics (docs/spec/04-static-semantics.md).

func (c *checker) unaryType(n *ast.UnaryExpr) types.Type {
	return operator.UnaryType(c, n.Op, n.X)
}

func (c *checker) binary(n *ast.BinaryExpr) types.Type {
	left := c.expr(n.X)
	right := c.expr(n.Y)
	return c.binaryType(left, n.Op, right, n.Pos())
}

func (c *checker) binaryType(left types.Type, op token.Type, right types.Type, pos token.Pos) types.Type {
	return operator.BinaryType(c, left, op, right, pos)
}

func (c *checker) boolOpType(n *ast.BoolOp) types.Type {
	left := c.expr(n.X)
	right := c.expr(n.Y)
	if (!types.Equal(left, types.Bool) && left != types.Invalid) || (!types.Equal(right, types.Bool) && right != types.Invalid) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "'%s' requires bool operands, got %s and %s", n.Op, left, right)
	}
	return types.Bool
}

func (c *checker) compareType(n *ast.Compare) types.Type {
	prev := c.expr(n.X)
	for i, op := range n.Ops {
		right := c.expr(n.Comparators[i])
		operator.Comparable(c, op, prev, right, n.Pos())
		prev = right
	}
	return types.Bool
}

func (c *checker) callType(n *ast.CallExpr) types.Type {
	if c.checkVariadicCallExtras(n) {
		return types.Invalid
	}
	name, ok := n.Fn.(*ast.Name)
	if !ok {
		if attr, ok := n.Fn.(*ast.Attribute); ok {
			return c.methodCallType(n, attr)
		}
		if len(n.Keywords) > 0 {
			for _, kw := range n.Keywords {
				c.expr(kw.Value)
				c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword arguments for dynamic calls are not supported yet")
			}
			return types.Invalid
		}
		fnType := c.expr(n.Fn)
		return c.callableCallType(n, fnType)
	}

	res := c.resolveName(name.Name)
	if res.kind == "class" {
		cls := c.classes[res.key]
		argExprs, argTypes, ok := c.constructorArgs(n, cls)
		if !ok {
			return types.Invalid
		}
		return c.constructorCallType(n, cls, argExprs, argTypes)
	}
	if res.kind == "function" {
		return c.directFunctionCallType(n, c.functions[res.key], name.Name)
	}
	if c.current != nil {
		if l, ok := c.current.locals[name.Name]; ok {
			return c.callableCallType(n, l.typ)
		}
		if cap := c.capture(name); cap != nil {
			return c.callableCallType(n, cap.typ)
		}
	}
	if res.kind == "global" {
		g := c.globals[res.key]
		return c.callableCallType(n, g.typ)
	}
	if res.kind != "native" {
		for _, arg := range n.StarArgs {
			c.expr(arg)
			c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred calls require a known minipy function or method")
		}
		c.errs.Add(name.Pos(), token.UndefinedName, "name %q is not defined", name.Name)
		return types.Invalid
	}
	if name.Name == "len" && len(n.Args) == 1 && len(n.StarArgs) == 0 && len(n.Keywords) == 0 {
		if result, ok := c.classLenCall(n); ok {
			return result
		}
	}
	return c.checkNativeCall(res.native, n)
}

// classLenCall rewrites len(obj) to obj.__len__() when obj is a class instance
// exposing __len__. Built-in containers fall through to the native len builtin.
func (c *checker) classLenCall(n *ast.CallExpr) (types.Type, bool) {
	cls, ok := c.expr(n.Args[0]).(*types.Class)
	if !ok {
		return types.Invalid, false
	}
	info := c.classes[cls.Name]
	if info == nil {
		return types.Invalid, false
	}
	m := method(info, "__len__")
	if m == nil {
		c.errs.Add(n.Pos(), token.TypeMismatch, "len() does not accept these arguments")
		return types.Invalid, true
	}
	if m.result != types.Invalid && !types.Equal(m.result, types.Int) {
		c.errs.Add(n.Pos(), token.TypeMismatch, "%s.__len__ must return int, got %s", info.name, m.result)
	}
	c.lenDunder[n] = true
	return types.Int, true
}

// checkNativeCall type-checks a native call, rejecting starred and keyword
// arguments (unsupported for native functions) before delegating to the symbol.
func (c *checker) checkNativeCall(sym module.Symbol, n *ast.CallExpr) types.Type {
	if len(n.StarArgs) > 0 {
		for _, a := range n.StarArgs {
			c.expr(a)
			c.errs.Add(a.Pos(), token.UnsupportedFeature, "starred arguments for native functions are not supported yet")
		}
		return types.Invalid
	}
	if len(n.Keywords) > 0 {
		for _, kw := range n.Keywords {
			c.expr(kw.Value)
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword arguments for native functions are not supported yet")
		}
		return types.Invalid
	}
	return sym.Check(c, n.Args, n.Pos())
}

func (c *checker) directFunctionCallType(n *ast.CallExpr, info *function, display string) types.Type {
	if c.current == nil && !info.slot.init {
		c.errs.Add(n.Fn.Pos(), token.UseBeforeDefinition, "function %q used before definition", display)
		return types.Invalid
	}
	argExprs, argTypes, ok := c.resolveFunctionArgs(n, info)
	if !ok {
		return types.Invalid
	}
	if spec := c.specialize(info, argTypes); spec != nil {
		c.callSpec[n] = spec
		return spec.info.result
	}
	for i, arg := range argTypes {
		pt := info.params[i].typ
		if arg != types.Invalid && pt != types.Invalid && !types.AssignableTo(arg, pt) {
			c.errs.Add(argExprs[i].Pos(), token.TypeMismatch, "%s() argument %d must be %s, got %s", display, i+1, pt, arg)
		}
	}
	return info.result
}

func (c *checker) checkVariadicCallExtras(n *ast.CallExpr) bool {
	unsupported := false
	if n.Kwargs != nil {
		c.expr(n.Kwargs)
		c.errs.Add(n.Kwargs.Pos(), token.UnsupportedFeature, "double-star call arguments are not supported yet")
		unsupported = true
	}
	return unsupported
}

func (c *checker) resolveFunctionArgs(n *ast.CallExpr, info *function) ([]ast.Expr, []types.Type, bool) {
	params := info.params
	varargIdx, kwargIdx := -1, -1
	for i, p := range params {
		if p.vararg {
			varargIdx = i
		}
		if p.kwarg {
			kwargIdx = i
		}
	}

	args := make([]ast.Expr, len(params))
	seen := make([]bool, len(params))

	positionalArgs := append([]ast.Expr(nil), n.Args...)
	for _, star := range n.StarArgs {
		expanded, ok := c.expandStarCallArg(star)
		if !ok {
			return nil, nil, false
		}
		positionalArgs = append(positionalArgs, expanded...)
	}

	// Positional slots are the ordered params that accept a positional argument
	// (everything before *args that is not keyword-only). Surplus positionals
	// spill into *args when present.
	posSlots := make([]int, 0, len(params))
	for i, p := range params {
		if p.vararg || p.kwarg || p.kind == ast.ParamKwOnly {
			continue
		}
		posSlots = append(posSlots, i)
	}
	var extraPositional []ast.Expr
	for k, arg := range positionalArgs {
		if k < len(posSlots) {
			args[posSlots[k]] = arg
			seen[posSlots[k]] = true
			continue
		}
		if varargIdx < 0 {
			c.errs.Add(arg.Pos(), token.ArityMismatch, "%s() takes too many positional arguments", info.name)
			return nil, nil, false
		}
		extraPositional = append(extraPositional, arg)
	}

	var extraKw []*ast.Keyword
	for _, kw := range n.Keywords {
		idx, ok := info.paramPosition(kw.Name)
		if !ok || params[idx].vararg || params[idx].kwarg {
			if kwargIdx >= 0 {
				extraKw = append(extraKw, kw)
				continue
			}
			c.errs.Add(kw.Pos(), token.ArityMismatch, "%s() got an unexpected keyword argument %q", info.name, kw.Name)
			return nil, nil, false
		}
		if params[idx].kind == ast.ParamPosOnly {
			c.errs.Add(kw.Pos(), token.ArityMismatch, "%s() got positional-only argument %q passed as keyword", info.name, kw.Name)
			return nil, nil, false
		}
		if seen[idx] {
			c.errs.Add(kw.Pos(), token.ArityMismatch, "%s() got multiple values for argument %q", info.name, kw.Name)
			return nil, nil, false
		}
		args[idx] = kw.Value
		seen[idx] = true
	}

	// Materialize *args / **kwargs aggregates as synthetic list/dict displays so
	// the existing display lowering builds the VM array/map parameter.
	if varargIdx >= 0 {
		args[varargIdx] = &ast.ListLit{Base: ast.Base{Position: n.Pos()}, Elems: extraPositional}
		seen[varargIdx] = true
	}
	if kwargIdx >= 0 {
		keys := make([]ast.Expr, len(extraKw))
		vals := make([]ast.Expr, len(extraKw))
		for i, kw := range extraKw {
			key := &ast.StrLit{Base: ast.Base{Position: kw.Pos()}, Value: kw.Name}
			c.types[key] = types.Str
			keys[i] = key
			vals[i] = kw.Value
		}
		args[kwargIdx] = &ast.DictLit{Base: ast.Base{Position: n.Pos()}, Keys: keys, Values: vals}
		seen[kwargIdx] = true
	}

	for i, p := range params {
		if seen[i] {
			continue
		}
		if p.defaultValue == nil {
			c.errs.Add(n.Pos(), token.ArityMismatch, "%s() missing required argument %q", info.name, p.name)
			return nil, nil, false
		}
		args[i] = p.defaultValue
	}
	argTypes := make([]types.Type, len(args))
	for i, arg := range args {
		argTypes[i] = c.exprWithHint(arg, params[i].typ)
	}
	c.callArgs[n] = args
	return args, argTypes, true
}

func (c *checker) expandStarCallArg(arg ast.Expr) ([]ast.Expr, bool) {
	t := c.expr(arg)
	tuple, ok := t.(*types.Tuple)
	if !ok {
		if t != types.Invalid {
			c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred calls require a statically-sized tuple, got %s", t)
		}
		return nil, false
	}
	out := make([]ast.Expr, len(tuple.Elems))
	for i := range tuple.Elems {
		idx := &ast.IntLit{Base: ast.Base{Position: arg.Pos()}, Value: int64(i)}
		sub := &ast.Subscript{Base: ast.Base{Position: arg.Pos()}, X: arg, Index: idx}
		c.types[idx] = types.Int
		c.types[sub] = tuple.Elems[i]
		out[i] = sub
	}
	return out, true
}

func (c *checker) constructorArgs(n *ast.CallExpr, cls *class) ([]ast.Expr, []types.Type, bool) {
	params := constructorParamInfos(cls)
	if params == nil {
		if len(n.StarArgs) > 0 {
			for _, arg := range n.StarArgs {
				c.expr(arg)
				c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred constructor arguments are not supported yet")
			}
			return nil, nil, false
		}
		if len(n.Keywords) > 0 {
			for _, kw := range n.Keywords {
				c.expr(kw.Value)
				c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword constructor arguments are not supported yet")
			}
			return nil, nil, false
		}
		argTypes := make([]types.Type, len(n.Args))
		for i, a := range n.Args {
			argTypes[i] = c.expr(a)
		}
		return n.Args, argTypes, true
	}
	temp := newFunction(cls.name)
	temp.setParams(params)
	return c.resolveFunctionArgs(n, temp)
}

func (c *checker) resolveMethodArgs(n *ast.CallExpr, method *function) ([]ast.Expr, []types.Type, bool) {
	trimmed := *method
	if len(method.params) > 0 {
		trimmed.setParams(method.params[1:])
	}
	args, argTypes, ok := c.resolveFunctionArgs(n, &trimmed)
	return args, argTypes, ok
}

func (c *checker) positionalMethodArgs(n *ast.CallExpr) []types.Type {
	if len(n.StarArgs) > 0 {
		for _, arg := range n.StarArgs {
			c.expr(arg)
			c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred arguments for built-in methods are not supported yet")
		}
		return nil
	}
	if len(n.Keywords) > 0 {
		for _, kw := range n.Keywords {
			c.expr(kw.Value)
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword arguments for built-in methods are not supported yet")
		}
		return nil
	}
	args := make([]types.Type, len(n.Args))
	for i, a := range n.Args {
		args[i] = c.expr(a)
	}
	return args
}

func (c *checker) constructorCallType(n *ast.CallExpr, cls *class, argExprs []ast.Expr, argTypes []types.Type) types.Type {
	params, minArgs := constructorParams(cls)
	if len(argTypes) < minArgs || len(argTypes) > len(params) {
		c.errs.Add(n.Pos(), token.ArityMismatch, "%s() takes %d to %d arguments (%d given)", cls.name, minArgs, len(params), len(argTypes))
		return types.Invalid
	}
	for i, arg := range argTypes {
		pt := params[i]
		if arg != types.Invalid && pt != types.Invalid && !types.AssignableTo(arg, pt) {
			c.errs.Add(argExprs[i].Pos(), token.TypeMismatch, "%s() argument %d must be %s, got %s", cls.name, i+1, pt, arg)
		}
	}
	return cls.typ
}

func constructorParams(cls *class) ([]types.Type, int) {
	if isException(cls) {
		return []types.Type{types.Str}, 0
	}
	if init := cls.methods["__init__"]; init != nil {
		params := srcTypes(init.params)
		if len(params) == 0 {
			return nil, 0
		}
		return params[1:], len(params) - 1
	}
	if !cls.dataclass {
		return nil, 0
	}
	params := make([]types.Type, len(cls.fields))
	minArgs := len(cls.fields)
	for i, field := range cls.fields {
		params[i] = field.typ
		if field.value != nil && i < minArgs {
			minArgs = i
		}
	}
	return params, minArgs
}

func constructorParamInfos(cls *class) []parameter {
	if isException(cls) {
		return []parameter{{name: "message", typ: types.Str, defaultValue: &ast.StrLit{}}}
	}
	if init := cls.methods["__init__"]; init != nil {
		if len(init.params) == 0 {
			return nil
		}
		return init.params[1:]
	}
	if !cls.dataclass {
		return nil
	}
	params := make([]parameter, len(cls.fields))
	for i, field := range cls.fields {
		params[i] = parameter{name: field.name, typ: field.typ, defaultValue: field.value}
	}
	return params
}

func isException(info *class) bool {
	for info != nil {
		if info.name == "BaseException" {
			return true
		}
		info = info.base
	}
	return false
}

func (c *checker) callableCallType(n *ast.CallExpr, value types.Type) types.Type {
	callable, ok := value.(*types.Callable)
	if !ok {
		if value != types.Invalid {
			c.errs.Add(n.Pos(), token.TypeMismatch, "%s is not callable", value)
		}
		for _, arg := range n.Args {
			c.expr(arg)
		}
		for _, kw := range n.Keywords {
			c.expr(kw.Value)
		}
		return types.Invalid
	}
	if len(n.Keywords) > 0 {
		for _, kw := range n.Keywords {
			c.expr(kw.Value)
			c.errs.Add(kw.Pos(), token.UnsupportedFeature, "keyword arguments for callable values are not supported yet")
		}
		return types.Invalid
	}
	if len(n.StarArgs) > 0 {
		for _, arg := range n.StarArgs {
			c.expr(arg)
			c.errs.Add(arg.Pos(), token.UnsupportedFeature, "starred arguments for callable values are not supported yet")
		}
		return types.Invalid
	}
	if len(n.Args) != len(callable.Params) {
		c.errs.Add(n.Pos(), token.ArityMismatch, "callable takes exactly %d arguments (%d given)", len(callable.Params), len(n.Args))
		return types.Invalid
	}
	for i, arg := range n.Args {
		got := c.expr(arg)
		if got != types.Invalid && callable.Params[i] != types.Invalid && !types.AssignableTo(got, callable.Params[i]) {
			c.errs.Add(arg.Pos(), token.TypeMismatch, "callable argument %d must be %s, got %s", i+1, callable.Params[i], got)
		}
	}
	return callable.Return
}

func (c *checker) methodCallType(n *ast.CallExpr, attr *ast.Attribute) types.Type {
	receiver := c.expr(attr.X)
	switch t := receiver.(type) {
	case *types.Module:
		res := c.resolveModuleAttr(t.Name, attr.Name)
		switch res.kind {
		case "native":
			c.attrNative[attr] = res.native
			return c.checkNativeCall(res.native, n)
		case "function":
			info := c.functions[res.key]
			c.attrSym[attr] = res.key
			return c.directFunctionCallType(n, info, attr.Name)
		case "class":
			cls := c.classes[res.key]
			c.attrSym[attr] = res.key
			argExprs, argTypes, ok := c.constructorArgs(n, cls)
			if !ok {
				return types.Invalid
			}
			return c.constructorCallType(n, cls, argExprs, argTypes)
		default:
			c.errs.Add(n.Pos(), token.UndefinedName, "module %q has no attribute %q", t.Name, attr.Name)
			for _, arg := range n.Args {
				c.expr(arg)
			}
			return types.Invalid
		}
	case *types.Class:
		info := c.classes[t.Name]
		if info == nil {
			return types.Invalid
		}
		method := method(info, attr.Name)
		if method == nil {
			c.errs.Add(n.Pos(), token.UnsupportedFeature, "method %s on %s is not supported", attr.Name, receiver)
			return types.Invalid
		}
		argExprs, argTypes, ok := c.resolveMethodArgs(n, method)
		if !ok {
			return types.Invalid
		}
		for i, got := range argTypes {
			pt := method.params[i+1].typ
			if got != types.Invalid && pt != types.Invalid && !types.AssignableTo(got, pt) {
				c.errs.Add(argExprs[i].Pos(), token.TypeMismatch, "%s.%s() argument %d must be %s, got %s", info.name, attr.Name, i+1, pt, got)
			}
		}
		return method.result
	case *types.List:
		args := c.positionalMethodArgs(n)
		switch attr.Name {
		case "append":
			if len(args) != 1 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.append takes exactly 1 argument (%d given)", len(args))
				return types.Invalid
			}
			if !types.AssignableTo(args[0], t.Elem) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.append expects %s, got %s", t.Elem, args[0])
				return types.Invalid
			}
			return types.None
		case "pop":
			if len(args) > 1 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.pop takes at most 1 argument (%d given)", len(args))
				return types.Invalid
			}
			if len(args) == 1 && !types.Equal(args[0], types.Int) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.pop index must be int, got %s", args[0])
				return types.Invalid
			}
			return t.Elem
		case "index":
			if len(args) != 1 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.index takes exactly 1 argument (%d given)", len(args))
				return types.Invalid
			}
			if !types.AssignableTo(args[0], t.Elem) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.index expects %s, got %s", t.Elem, args[0])
				return types.Invalid
			}
			return types.Int
		case "insert":
			if len(args) != 2 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.insert takes exactly 2 arguments (%d given)", len(args))
				return types.Invalid
			}
			if !types.Equal(args[0], types.Int) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.insert index must be int, got %s", args[0])
				return types.Invalid
			}
			if !types.AssignableTo(args[1], t.Elem) {
				c.errs.Add(n.Args[1].Pos(), token.TypeMismatch, "list.insert value must be %s, got %s", t.Elem, args[1])
				return types.Invalid
			}
			return types.None
		case "extend":
			if len(args) != 1 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.extend takes exactly 1 argument (%d given)", len(args))
				return types.Invalid
			}
			if !types.Equal(args[0], t) {
				c.errs.Add(n.Args[0].Pos(), token.TypeMismatch, "list.extend expects list[%s], got %s", t.Elem, args[0])
				return types.Invalid
			}
			return types.None
		case "reverse":
			if len(args) != 0 {
				c.errs.Add(n.Pos(), token.ArityMismatch, "list.reverse takes no arguments (%d given)", len(args))
				return types.Invalid
			}
			return types.None
		}
	case *types.Dict:
		args := c.positionalMethodArgs(n)
		switch attr.Name {
		case "get":
			if len(args) < 1 || len(args) > 2 || !types.AssignableTo(args[0], t.Key) || (len(args) == 2 && !types.AssignableTo(args[1], t.Value)) {
				c.errs.Add(n.Pos(), token.TypeMismatch, "dict.get expects key and optional default")
				return types.Invalid
			}
			return t.Value
		case "keys":
			if len(args) == 0 {
				return types.NewList(t.Key)
			}
		case "values":
			if len(args) == 0 {
				return types.NewList(t.Value)
			}
		case "items":
			if len(args) == 0 {
				return types.NewList(types.NewTuple(t.Key, t.Value))
			}
		}
	default:
		args := c.positionalMethodArgs(n)
		if types.Equal(receiver, types.Str) {
			switch attr.Name {
			case "upper", "lower":
				if len(args) == 0 {
					return types.Str
				}
			case "split":
				if len(args) <= 1 && (len(args) == 0 || types.Equal(args[0], types.Str)) {
					return types.NewList(types.Str)
				}
			case "join":
				if len(args) == 1 {
					if list, ok := args[0].(*types.List); ok && types.Equal(list.Elem, types.Str) {
						return types.Str
					}
				}
			case "find":
				if len(args) == 1 && types.Equal(args[0], types.Str) {
					return types.Int
				}
			}
		}
	}
	c.errs.Add(n.Pos(), token.UnsupportedFeature, "method %s on %s is not supported", attr.Name, receiver)
	return types.Invalid
}
