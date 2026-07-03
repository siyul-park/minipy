package compiler

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minivm/interp"
	"github.com/stretchr/testify/require"
)

func runFS(t *testing.T, src string, fsys fstest.MapFS, opts ...Option) string {
	t.Helper()
	var buf bytes.Buffer
	all := []Option{WithOutput(&buf), WithModules(fsys)}
	all = append(all, opts...)
	prog, err := Compile(strings.NewReader(src), all...)
	require.NoError(t, err)
	vm := interp.New(prog)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	return buf.String()
}

func TestCompileImports(t *testing.T) {
	t.Run("import runs module once and exposes globals and functions", func(t *testing.T) {
		fsys := fstest.MapFS{
			"a.py": {Data: []byte("print(\"load a\")\nx: int = 2\ndef f() -> None:\n    print(\"call f\")\n")},
		}
		src := "print(\"main before\")\nimport a\nprint(str(a.x))\na.f()\nimport a\nprint(\"main after\")\n"
		require.Equal(t, "main before\nload a\n2\ncall f\nmain after\n", runFS(t, src, fsys))
	})

	t.Run("alias and from import bind imported symbols", func(t *testing.T) {
		fsys := fstest.MapFS{
			"a.py": {Data: []byte("x: int = 4\ndef f() -> int:\n    return x + 1\n")},
		}
		src := "import a as b\nfrom a import f as g\nprint(str(b.x))\nprint(str(g()))\n"
		require.Equal(t, "4\n5\n", runFS(t, src, fsys))
	})

	t.Run("package dotted and relative imports work", func(t *testing.T) {
		fsys := fstest.MapFS{
			"pkg/__init__.py": {Data: []byte("from . import sib\nroot: int = 7\n")},
			"pkg/sib.py":      {Data: []byte("v: int = 3\n")},
			"pkg/sub.py":      {Data: []byte("from .sib import v\n")},
		}
		src := "import pkg.sub\nfrom pkg import sib\nprint(str(pkg.root))\nprint(str(pkg.sib.v))\nprint(str(sib.v))\nprint(str(pkg.sub.v))\n"
		require.Equal(t, "7\n3\n3\n3\n", runFS(t, src, fsys))
	})

	t.Run("builtins module and builtin shadowing", func(t *testing.T) {
		fsys := fstest.MapFS{
			"m.py": {Data: []byte("def len(x: int) -> int:\n    return x + 10\n")},
		}
		src := "import builtins\nfrom builtins import print as p\nfrom m import len\np(str(len(5)))\nbuiltins.print(str(builtins.len([1, 2])))\np(str(builtins.isinstance(1, int)))\n"
		require.Equal(t, "15\n2\nTrue\n", runFS(t, src, fsys))
	})

	t.Run("native modules win over filesystem modules", func(t *testing.T) {
		fsys := fstest.MapFS{
			"builtins.py": {Data: []byte("x: int = 1\n")},
			"operator.py": {Data: []byte("x: int = 2\n")},
		}
		src := "import builtins\nimport operator\nbuiltins.print(str(operator.add(2, 3)))\n"
		require.Equal(t, "5\n", runFS(t, src, fsys))
	})

	t.Run("operator native functions use python operator names", func(t *testing.T) {
		src := "import operator\nfrom operator import floordiv as fd\nprint(str(operator.add(2, 3)))\nprint(str(fd(7, 2)))\nprint(str(operator.eq(4, 4)))\nprint(str(operator.not_(False)))\nprint(str(operator.contains([1, 2], 2)))\nprint(str(operator.neg(3)))\n"
		require.Equal(t, "5\n3\nTrue\nTrue\nTrue\n-3\n", runFS(t, src, fstest.MapFS{}))
	})

	t.Run("module path entries and package priority", func(t *testing.T) {
		fsys := fstest.MapFS{
			"one/a.py":            {Data: []byte("x: int = 1\n")},
			"two/a.py":            {Data: []byte("x: int = 2\n")},
			"one/pkg.py":          {Data: []byte("x: int = 10\n")},
			"one/pkg/__init__.py": {Data: []byte("x: int = 11\n")},
		}
		src := "import a\nimport pkg\nprint(str(a.x))\nprint(str(pkg.x))\n"
		require.Equal(t, "1\n11\n", runFS(t, src, fstest.MapFS{}, WithModulePath(fsys, "one", "two")))
	})

	t.Run("module attribute assignment", func(t *testing.T) {
		fsys := fstest.MapFS{"a.py": {Data: []byte("x: int = 1\n")}}
		src := "import a\na.x = 4\na.x += 1\nprint(str(a.x))\n"
		require.Equal(t, "5\n", runFS(t, src, fsys))
	})

	t.Run("imported exception class works in raise and except", func(t *testing.T) {
		fsys := fstest.MapFS{
			"m.py": {Data: []byte("class E(Exception):\n    pass\n\ndef fail() -> None:\n    raise E(\"boom\")\n")},
		}
		src := "import m\ntry:\n    m.fail()\nexcept m.E as e:\n    print(e.message)\n"
		require.Equal(t, "boom\n", runFS(t, src, fsys))
	})

	t.Run("imported class works in annotations isinstance and match", func(t *testing.T) {
		fsys := fstest.MapFS{
			"m.py": {Data: []byte("class P:\n    pass\n")},
		}
		src := "import m\np: m.P = m.P()\nprint(str(isinstance(p, m.P)))\nmatch p:\n    case m.P():\n        print(\"matched\")\n"
		require.Equal(t, "True\nmatched\n", runFS(t, src, fsys))
	})

	t.Run("module function specialization crosses module boundary", func(t *testing.T) {
		fsys := fstest.MapFS{
			"m.py": {Data: []byte("def describe(x: int | str) -> str:\n    if isinstance(x, int):\n        return \"i\" + str(x)\n    return \"s\" + x\n")},
		}
		src := "import m\nprint(m.describe(1))\nprint(m.describe(\"x\"))\n"
		require.Equal(t, "i1\nsx\n", runFS(t, src, fsys))
	})
}

func TestImportErrors(t *testing.T) {
	t.Run("missing module", func(t *testing.T) {
		_, err := Compile(strings.NewReader("import nope\n"), WithModules(fstest.MapFS{}))
		require.Error(t, err)
		code(t, err, token.ModuleNotFound)
	})

	t.Run("circular import", func(t *testing.T) {
		fsys := fstest.MapFS{
			"a.py": {Data: []byte("import b\n")},
			"b.py": {Data: []byte("import a\n")},
		}
		_, err := Compile(strings.NewReader("import a\n"), WithModules(fsys))
		require.Error(t, err)
		code(t, err, token.ImportError)
		require.Contains(t, err.Error(), "circular import: a -> b -> a")
	})

	t.Run("star import rejected", func(t *testing.T) {
		fsys := fstest.MapFS{"a.py": {Data: []byte("x: int = 1\n")}}
		_, err := Compile(strings.NewReader("from a import *\n"), WithModules(fsys))
		require.Error(t, err)
		code(t, err, token.UnsupportedFeature)
	})

	t.Run("relative import in entry rejected", func(t *testing.T) {
		_, err := Compile(strings.NewReader("from . import a\n"), WithModules(fstest.MapFS{}))
		require.Error(t, err)
		code(t, err, token.ImportError)
	})

	t.Run("not a package", func(t *testing.T) {
		fsys := fstest.MapFS{"a.py": {Data: []byte("x: int = 1\n")}}
		_, err := Compile(strings.NewReader("import a.b\n"), WithModules(fsys))
		require.Error(t, err)
		code(t, err, token.ModuleNotFound)
		require.Contains(t, err.Error(), `"a" is not a package`)
	})

	t.Run("from import missing name", func(t *testing.T) {
		fsys := fstest.MapFS{"a.py": {Data: []byte("x: int = 1\n")}}
		_, err := Compile(strings.NewReader("from a import y\n"), WithModules(fsys))
		require.Error(t, err)
		code(t, err, token.ImportError)
		require.NotContains(t, err.Error(), "ModuleNotFoundError")
	})

	t.Run("relative import beyond top level", func(t *testing.T) {
		fsys := fstest.MapFS{"pkg/__init__.py": {Data: []byte("from .. import nope\n")}}
		_, err := Compile(strings.NewReader("import pkg\n"), WithModules(fsys))
		require.Error(t, err)
		code(t, err, token.ImportError)
	})

	t.Run("nested import rejected", func(t *testing.T) {
		fsys := fstest.MapFS{"a.py": {Data: []byte("x: int = 1\n")}}
		_, err := Compile(strings.NewReader("def f() -> None:\n    import a\n"), WithModules(fsys))
		require.Error(t, err)
		code(t, err, token.UnsupportedFeature)
	})
}
