package compiler

import (
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/builtins"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/parser"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/typing"

	vmtypes "github.com/siyul-park/minivm/types"
)

type searchEntry struct {
	fsys fs.FS
	dir  string
}

type moduleInfo struct {
	name      string
	path      string
	ast       *ast.Module
	isPackage bool
	bindings  map[string]binding
	future    map[string]bool
	exports   []string
	all       []string
	allSeen   bool
	allStatic bool
	parent    string
	native    bool
	fsys      fs.FS
	dir       string
	checked   bool
	emitted   bool
	loading   bool
}

type binding struct {
	module string
	symbol string
}

type loader struct {
	reg     *module.Registry
	finders []finder
	dist    *distIndex
	paths   []searchEntry
	modules map[string]*moduleInfo
	stack   []string
	errs    token.ErrorList
}

// moduleSpec is a located, not-yet-loaded module description (importlib
// ModuleSpec analog).
type moduleSpec struct {
	name      string
	origin    string
	fsys      fs.FS
	dir       string
	isPackage bool
	parent    string
	builtin   bool
}

// finder locates a spec for a fully-qualified module name. parent is the
// already-loaded parent package for submodule resolution, or nil for a
// top-level import (importlib MetaPathFinder analog).
type finder interface {
	findSpec(ld *loader, name string, parent *moduleInfo) (*moduleSpec, bool)
}

// builtinFinder resolves native modules from the registry: CPython's
// BuiltinImporter analog, top-level only and highest precedence.
type builtinFinder struct{}

// pathFinder resolves source modules on the search roots, preferring
// __init__.py packages over plain modules: CPython's PathFinder + FileFinder
// with a SourceFileLoader.
type pathFinder struct{}

// nativeRuntime holds the runtime values of native module symbols, bound to the
// program's output writer.
type nativeRuntime struct {
	modules map[string]map[string]vmtypes.Value
	out     io.Writer
}

func newLoader(reg *module.Registry, paths []searchEntry) *loader {
	if reg == nil {
		reg = defaultRegistry()
	}
	ld := &loader{
		reg:     reg,
		paths:   append([]searchEntry(nil), paths...),
		modules: map[string]*moduleInfo{},
	}
	ld.dist = newDistIndex(ld.paths)
	// Finder chain, the importlib sys.meta_path analog: native modules resolve
	// first (CPython's BuiltinImporter), then source modules on the search roots
	// (PathFinder), so native modules win over same-named files.
	ld.finders = []finder{builtinFinder{}, pathFinder{}}
	return ld
}

func newNativeRuntime(reg *module.Registry, out io.Writer) *nativeRuntime {
	rt := &nativeRuntime{out: out}
	rt.modules = reg.Values(rt)
	return rt
}

func (ld *loader) loadEntry(mod *ast.Module) (*moduleInfo, map[string]*moduleInfo) {
	entry := &moduleInfo{
		name:     "__main__",
		path:     "<stdin>",
		ast:      mod,
		bindings: map[string]binding{},
	}
	ld.modules[entry.name] = entry
	ld.scan(entry)
	return entry, ld.modules
}

func (ld *loader) loadModule(name string, pos token.Pos) *moduleInfo {
	name = strings.Trim(name, ".")
	if name == "" {
		return nil
	}
	if m := ld.modules[name]; m != nil {
		if m.loading {
			ld.errs.Add(pos, token.ImportError, "circular import: %s", ld.cycle(name))
		}
		return m
	}
	var parent *moduleInfo
	if i := strings.LastIndex(name, "."); i >= 0 {
		parentName := name[:i]
		parent = ld.loadModule(parentName, pos)
		if parent == nil {
			return nil
		}
		if !parent.isPackage {
			ld.errs.Add(pos, token.ModuleNotFound, "no module named %q; %q is not a package", name, parentName)
			return nil
		}
	}
	sp, ok := ld.findSpec(name, parent)
	if !ok {
		ld.errs.Add(pos, token.ModuleNotFound, "no module named %q", name)
		return nil
	}
	return ld.loadSpec(sp, pos)
}

// findSpec walks the finder chain, returning the first located spec (importlib
// sys.meta_path semantics).
func (ld *loader) findSpec(name string, parent *moduleInfo) (*moduleSpec, bool) {
	for _, f := range ld.finders {
		if sp, ok := f.findSpec(ld, name, parent); ok {
			return sp, true
		}
	}
	return nil, false
}

// loadSpec realizes a located spec into a moduleInfo, dispatching to the builtin
// or source loader.
func (ld *loader) loadSpec(sp *moduleSpec, pos token.Pos) *moduleInfo {
	if sp.builtin {
		return ld.loadBuiltin(sp.name)
	}
	m := &moduleInfo{
		name:      sp.name,
		path:      sp.origin,
		isPackage: sp.isPackage,
		parent:    sp.parent,
		fsys:      sp.fsys,
		dir:       sp.dir,
		bindings:  map[string]binding{},
	}
	ld.modules[sp.name] = m
	ld.stack = append(ld.stack, sp.name)
	m.loading = true
	src, err := fs.ReadFile(sp.fsys, sp.origin)
	if err != nil {
		ld.errs.Add(pos, token.ModuleNotFound, "no module named %q", sp.name)
		ld.stack = ld.stack[:len(ld.stack)-1]
		m.loading = false
		return nil
	}
	parsed, parseErr := parser.Parse(strings.NewReader(string(src)))
	m.ast = parsed
	if parseErr != nil {
		if list, ok := parseErr.(token.ErrorList); ok {
			ld.errs = append(ld.errs, list...)
		} else {
			ld.errs.Add(pos, token.SyntaxError, "%s", parseErr)
		}
	}
	ld.scan(m)
	m.loading = false
	ld.stack = ld.stack[:len(ld.stack)-1]
	return m
}

// loadBuiltin realizes a native module into a synthetic moduleInfo whose
// bindings expose each registry symbol under its own name.
func (ld *loader) loadBuiltin(name string) *moduleInfo {
	mod, ok := ld.reg.Module(name)
	if !ok {
		return nil
	}
	names := mod.Names()
	bindings := make(map[string]binding, len(names))
	for _, symbol := range names {
		bindings[symbol] = binding{module: name, symbol: symbol}
	}
	m := &moduleInfo{
		name:     name,
		path:     "<" + name + ">",
		ast:      &ast.Module{},
		native:   true,
		bindings: bindings,
		exports:  names,
	}
	ld.modules[name] = m
	return m
}

// distribution returns the installed distribution providing a top-level import
// name, or false if none is installed on the search roots.
func (ld *loader) distribution(importName string) (*distribution, bool) {
	return ld.dist.distribution(importName)
}

func (ld *loader) scan(m *moduleInfo) {
	if m == nil || m.ast == nil {
		return
	}
	m.all, m.allSeen, m.allStatic = staticAll(m.ast.Body)
	for _, s := range m.ast.Body {
		switch n := s.(type) {
		case *ast.Import:
			for _, a := range n.Names {
				ld.loadModule(a.Name, a.Pos())
			}
		case *ast.ImportFrom:
			if n.Level == 0 && n.Module == "__future__" {
				continue
			}
			base := ld.resolveFrom(m, n)
			if base == "" {
				continue
			}
			if base != m.name {
				ld.loadModule(base, n.Pos())
			}
		}
	}
}

// relativeBase resolves a relative import's parent anchor to the module name to
// load, or "" when the import is illegal (relative import from __main__, beyond
// the top-level package, or with no parent). Level-0 imports return Module
// unchanged. The checker adds the diagnostic; the lowerer only needs the result.
func relativeBase(mod *moduleInfo, n *ast.ImportFrom) string {
	if n.Level == 0 {
		return n.Module
	}
	if mod == nil || mod.name == "__main__" {
		return ""
	}
	anchor := mod.name
	if !mod.isPackage {
		anchor = mod.parent
	}
	parts := strings.Split(anchor, ".")
	up := n.Level - 1
	if up > len(parts)-1 {
		return ""
	}
	base := strings.Join(parts[:len(parts)-up], ".")
	if n.Module != "" {
		if base == "" {
			return n.Module
		}
		return base + "." + n.Module
	}
	return base
}

func (ld *loader) resolveFrom(m *moduleInfo, n *ast.ImportFrom) string {
	if n.Level == 0 {
		return n.Module
	}
	if m == nil || m.name == "__main__" {
		ld.errs.Add(n.Pos(), token.ImportError, "attempted relative import with no known parent package")
		return ""
	}
	base := relativeBase(m, n)
	if base == "" {
		ld.errs.Add(n.Pos(), token.ImportError, "attempted relative import beyond top-level package")
	}
	return base
}

func (ld *loader) cycle(name string) string {
	start := 0
	for i, n := range ld.stack {
		if n == name {
			start = i
			break
		}
	}
	parts := append([]string(nil), ld.stack[start:]...)
	parts = append(parts, name)
	return strings.Join(parts, " -> ")
}

func (builtinFinder) findSpec(ld *loader, name string, parent *moduleInfo) (*moduleSpec, bool) {
	if parent != nil || !ld.reg.Has(name) {
		return nil, false
	}
	return &moduleSpec{name: name, origin: "<" + name + ">", builtin: true}, true
}

func (pathFinder) findSpec(ld *loader, name string, parent *moduleInfo) (*moduleSpec, bool) {
	child := name
	if i := strings.LastIndex(name, "."); i >= 0 {
		child = name[i+1:]
	}
	if parent != nil {
		if sp := findOnPath(parent.fsys, parent.dir, name, child); sp != nil {
			sp.parent = parent.name
			return sp, true
		}
		return nil, false
	}
	for _, entry := range ld.paths {
		if sp := findOnPath(entry.fsys, cleanDir(entry.dir), name, child); sp != nil {
			return sp, true
		}
	}
	return nil, false
}

func (rt *nativeRuntime) Value(moduleName, symbol string) vmtypes.Value {
	if symbols := rt.modules[moduleName]; symbols != nil {
		return symbols[symbol]
	}
	return nil
}

func findOnPath(fsys fs.FS, dir, name, child string) *moduleSpec {
	pkgInit := path.Join(dir, child, "__init__.py")
	if readable(fsys, pkgInit) {
		return &moduleSpec{
			name:      name,
			origin:    pkgInit,
			fsys:      fsys,
			dir:       path.Join(dir, child),
			isPackage: true,
		}
	}
	file := path.Join(dir, child+".py")
	if readable(fsys, file) {
		return &moduleSpec{name: name, origin: file, fsys: fsys, dir: dir}
	}
	return nil
}

func readable(fsys fs.FS, file string) bool {
	if _, err := fs.Stat(fsys, file); err != nil {
		return false
	}
	return true
}

func staticAll(body []ast.Stmt) ([]string, bool, bool) {
	for _, s := range body {
		assign, ok := s.(*ast.Assign)
		if !ok {
			continue
		}
		name, ok := assign.Target.(*ast.Name)
		if !ok || name.Name != "__all__" {
			continue
		}
		names, ok := stringSequence(assign.Value)
		return names, true, ok
	}
	return nil, false, false
}

func stringSequence(e ast.Expr) ([]string, bool) {
	var elems []ast.Expr
	switch n := e.(type) {
	case *ast.ListLit:
		elems = n.Elems
	case *ast.TupleLit:
		elems = n.Elems
	default:
		return nil, false
	}
	names := make([]string, len(elems))
	for i, elem := range elems {
		lit, ok := elem.(*ast.StrLit)
		if !ok {
			return nil, false
		}
		names[i] = lit.Value
	}
	return names, true
}

func cleanDir(dir string) string {
	if dir == "" || dir == "." {
		return "."
	}
	return path.Clean(dir)
}

// defaultRegistry is the built-in native module set: builtins (the fallback for
// unqualified names), operator, and annotation-only typing.
func defaultRegistry() *module.Registry {
	return module.NewRegistry(
		[]module.Module{builtins.New(), operator.New(), typing.New()},
		module.WithFallback(builtins.Name),
	)
}

// nativeDisplay strips the builtins. prefix from a qualified native symbol key
// for diagnostics, so print() reads as print() rather than builtins.print().
func nativeDisplay(key string) string {
	if strings.HasPrefix(key, builtins.Name+".") {
		return strings.TrimPrefix(key, builtins.Name+".")
	}
	return key
}
