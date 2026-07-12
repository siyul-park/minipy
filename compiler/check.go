package compiler

import (
	"sort"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
)

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
	checked   map[*moduleInfo]bool
}

func newChecker(ld *loader) *checker {
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
		checked:    map[*moduleInfo]bool{},
	}
	c.declareBuiltinExceptions()
	return c
}

// checker adapts to module.Checker so native modules can drive type-checking
// without depending on compiler internals.
var _ module.Checker = (*checker)(nil)

// Check type-checks a sub-expression and returns its type.
func (c *checker) Check(e ast.Expr) types.Type { return c.expr(e) }

// SetType records the resolved type of an expression.
func (c *checker) SetType(e ast.Expr, t types.Type) { c.types[e] = t }

// ResolveType interprets an expression as a type annotation.
func (c *checker) ResolveType(e ast.Expr) types.Type { return c.resolveType(e) }

// Error reports a static error.
func (c *checker) Error(pos token.Pos, code token.Code, format string, args ...any) {
	c.errs.Add(pos, code, format, args...)
}

// check walks every top-level statement, accumulating diagnostics.
func (c *checker) checkProgram(entry *moduleInfo) {
	c.checkModule(entry)
	c.computeClassIntervals()
}

func (c *checker) check(mod *ast.Module) {
	entry := c.loader.loadEntry(mod)
	c.checkProgram(entry)
}

func (c *checker) checkModule(m *moduleInfo) {
	if m == nil || c.checked[m] {
		return
	}
	c.checked[m] = true
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
