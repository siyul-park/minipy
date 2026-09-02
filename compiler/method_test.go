package compiler

import (
	"bytes"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
	"github.com/stretchr/testify/require"
)

// TestBuiltinMethodCatalogue pins the property the catalogue exists for: a
// builtin method has both a checker rule and an emitter, or it does not exist.
// Before the catalogue the two halves were separate switches in separate files
// dispatching on different axes, so one could be added without the other.
func TestBuiltinMethodCatalogue(t *testing.T) {
	catalogues := map[string]map[string]builtinMethod{
		"list": listMethods,
		"dict": dictMethods,
		"set":  setMethods,
		"str":  strMethods,
	}

	t.Run("every entry owns both halves of its contract", func(t *testing.T) {
		for receiver, methods := range catalogues {
			require.NotEmpty(t, methods, receiver)
			for name, method := range methods {
				require.Equalf(t, name, method.name, "%s.%s is indexed under another name", receiver, name)
				require.NotNilf(t, method.check, "%s.%s has no checker rule", receiver, name)
				require.NotNilf(t, method.emit, "%s.%s has no emitter", receiver, name)
			}
		}
	})

	t.Run("a receiver resolves to its own catalogue", func(t *testing.T) {
		require.Len(t, builtinMethods(types.NewList(types.Int)), len(listMethods))
		require.Len(t, builtinMethods(types.NewDict(types.Str, types.Int)), len(dictMethods))
		require.Len(t, builtinMethods(types.NewSet(types.Int)), len(setMethods))
		require.Len(t, builtinMethods(types.Str), len(strMethods))
		require.Nil(t, builtinMethods(types.Int))
	})

	t.Run("a name shared by several receivers keeps one entry per receiver", func(t *testing.T) {
		// pop, copy, clear, count and remove exist on more than one receiver.
		// The lowerer used to re-discriminate the receiver inside one case for
		// each; now each catalogue owns its own.
		for _, name := range []string{"pop", "copy", "clear"} {
			list, listOK := lookupBuiltinMethod(types.NewList(types.Int), name)
			dict, dictOK := lookupBuiltinMethod(types.NewDict(types.Str, types.Int), name)
			require.Truef(t, listOK && dictOK, "list and dict both define %s", name)
			require.NotSamef(t, &list, &dict, "%s must not be one shared entry", name)
		}
	})
}

func TestCompileBuiltinMethodDiagnostics(t *testing.T) {
	t.Run("a wrong argument count names the method and the count", func(t *testing.T) {
		_, err := Compile(strings.NewReader("xs: list[int] = []\nxs.append(1, 2)\n"), WithOutput(&bytes.Buffer{}))
		require.Error(t, err)
		code(t, err, token.ArityMismatch)
		require.Contains(t, err.Error(), "list.append takes exactly 1 argument (2 given)")
	})

	t.Run("a wrong argument type names both types", func(t *testing.T) {
		_, err := Compile(strings.NewReader("xs: list[int] = []\nxs.append(\"x\")\n"), WithOutput(&bytes.Buffer{}))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
		require.Contains(t, err.Error(), "list.append expects int, got str")
	})

	t.Run("an unknown method reports the receiver it was called on", func(t *testing.T) {
		_, err := Compile(strings.NewReader("xs: list[int] = []\nxs.nope()\n"), WithOutput(&bytes.Buffer{}))
		require.Error(t, err)
		code(t, err, token.UnsupportedFeature)
		require.Contains(t, err.Error(), "method nope on list[int] is not supported")
	})

	t.Run("a method of another receiver is unknown here", func(t *testing.T) {
		// `add` belongs to set, `upper` to str: reaching either through a list
		// receiver must not find the other catalogue's entry.
		_, err := Compile(strings.NewReader("xs: list[int] = []\nxs.add(1)\n"), WithOutput(&bytes.Buffer{}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "method add on list[int] is not supported")

		_, err = Compile(strings.NewReader("xs: list[int] = []\nxs.upper()\n"), WithOutput(&bytes.Buffer{}))
		require.Error(t, err)
		require.Contains(t, err.Error(), "method upper on list[int] is not supported")
	})

	t.Run("arguments of an unsupported method are still checked", func(t *testing.T) {
		_, err := Compile(strings.NewReader("n: int = 1\nn.nope(undefined_name)\n"), WithOutput(&bytes.Buffer{}))
		require.Error(t, err)
		code(t, err, token.UndefinedName)
	})
}
