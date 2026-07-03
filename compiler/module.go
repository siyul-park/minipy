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
	paths   []searchEntry
	modules map[string]*moduleInfo
	stack   []string
	errs    token.ErrorList
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
	for _, mod := range reg.Modules() {
		names := mod.Names()
		bindings := make(map[string]binding, len(names))
		for _, symbol := range names {
			bindings[symbol] = binding{module: mod.Name(), symbol: symbol}
		}
		ld.modules[mod.Name()] = &moduleInfo{
			name:     mod.Name(),
			path:     "<" + mod.Name() + ">",
			ast:      &ast.Module{},
			native:   true,
			bindings: bindings,
		}
	}
	return ld
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
	m := ld.findModule(name, pos)
	if m == nil {
		return nil
	}
	ld.modules[name] = m
	ld.stack = append(ld.stack, name)
	m.loading = true
	src, err := fs.ReadFile(m.fsys, m.path)
	if err != nil {
		ld.errs.Add(pos, token.ModuleNotFound, "no module named %q", name)
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

func (ld *loader) findModule(name string, pos token.Pos) *moduleInfo {
	parts := strings.Split(name, ".")
	if len(parts) == 1 {
		for _, entry := range ld.paths {
			if m := ld.findChild(entry.fsys, cleanDir(entry.dir), "", parts[0], name); m != nil {
				return m
			}
		}
		ld.errs.Add(pos, token.ModuleNotFound, "no module named %q", name)
		return nil
	}
	parentName := strings.Join(parts[:len(parts)-1], ".")
	parent := ld.loadModule(parentName, pos)
	if parent == nil {
		return nil
	}
	if !parent.isPackage {
		ld.errs.Add(pos, token.ModuleNotFound, "no module named %q; %q is not a package", name, parentName)
		return nil
	}
	child := parts[len(parts)-1]
	if m := ld.findChild(parent.fsys, parent.dir, parentName, child, name); m != nil {
		return m
	}
	ld.errs.Add(pos, token.ModuleNotFound, "no module named %q", name)
	return nil
}

func (ld *loader) findChild(fsys fs.FS, dir, parent, child, name string) *moduleInfo {
	pkgInit := path.Join(dir, child, "__init__.py")
	if readable(fsys, pkgInit) {
		return &moduleInfo{
			name:      name,
			path:      pkgInit,
			isPackage: true,
			parent:    parent,
			fsys:      fsys,
			dir:       path.Join(dir, child),
			bindings:  map[string]binding{},
		}
	}
	file := path.Join(dir, child+".py")
	if readable(fsys, file) {
		return &moduleInfo{
			name:     name,
			path:     file,
			parent:   parent,
			fsys:     fsys,
			dir:      dir,
			bindings: map[string]binding{},
		}
	}
	return nil
}

func readable(fsys fs.FS, file string) bool {
	if _, err := fs.Stat(fsys, file); err != nil {
		return false
	}
	return true
}

func (ld *loader) scan(m *moduleInfo) {
	if m == nil || m.ast == nil {
		return
	}
	for _, s := range m.ast.Body {
		switch n := s.(type) {
		case *ast.Import:
			for _, a := range n.Names {
				ld.loadModule(a.Name, a.Pos())
			}
		case *ast.ImportFrom:
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

func (ld *loader) resolveFrom(m *moduleInfo, n *ast.ImportFrom) string {
	if n.Level == 0 {
		return n.Module
	}
	if m == nil || m.name == "__main__" {
		ld.errs.Add(n.Pos(), token.ImportError, "attempted relative import with no known parent package")
		return ""
	}
	anchor := m.name
	if !m.isPackage {
		anchor = m.parent
	}
	if anchor == "" {
		ld.errs.Add(n.Pos(), token.ImportError, "attempted relative import beyond top-level package")
		return ""
	}
	parts := strings.Split(anchor, ".")
	up := n.Level - 1
	if up > len(parts)-1 {
		ld.errs.Add(n.Pos(), token.ImportError, "attempted relative import beyond top-level package")
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

func cleanDir(dir string) string {
	if dir == "" || dir == "." {
		return "."
	}
	return path.Clean(dir)
}

// defaultRegistry is the built-in native module set: builtins (the fallback for
// unqualified names) and operator.
func defaultRegistry() *module.Registry {
	return module.NewRegistry(
		[]module.Module{builtins.New(), operator.New()},
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

// nativeRuntime holds the runtime values of native module symbols, bound to the
// program's output writer.
type nativeRuntime struct {
	modules map[string]map[string]vmtypes.Value
	out     io.Writer
}

func newNativeRuntime(reg *module.Registry, out io.Writer) *nativeRuntime {
	rt := &nativeRuntime{out: out}
	rt.modules = reg.Values(rt)
	return rt
}

func (rt *nativeRuntime) Value(moduleName, symbol string) vmtypes.Value {
	if symbols := rt.modules[moduleName]; symbols != nil {
		return symbols[symbol]
	}
	return nil
}
