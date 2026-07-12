package compiler

import (
	"io"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	minitypes "github.com/siyul-park/minipy/types"

	vmtypes "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestCompileEllipsis(t *testing.T) {
	t.Run("singleton annotation and comparisons", func(t *testing.T) {
		src := `x: EllipsisType = ...
assert x is Ellipsis
assert ... is Ellipsis
assert not (... is not Ellipsis)
assert ... == Ellipsis
assert not (... != Ellipsis)
`
		require.Empty(t, run(t, src))
	})

	t.Run("constant is interned once", func(t *testing.T) {
		prog, err := Compile(strings.NewReader("x = ...\ny = ...\nz = Ellipsis\n"), WithOutput(io.Discard))
		require.NoError(t, err)

		count := 0
		for _, value := range prog.Constants {
			strct, ok := value.(*vmtypes.Struct)
			if ok && strct.Type().Equals(minitypes.Ellipsis.VM()) {
				count++
			}
		}
		require.Equal(t, 1, count)
	})

	t.Run("identity survives calls and returns", func(t *testing.T) {
		src := `def identity(x: EllipsisType) -> EllipsisType:
    return x
assert identity(...) is Ellipsis
assert identity(Ellipsis) == ...
`
		require.Empty(t, run(t, src))
	})

	t.Run("global shadowing", func(t *testing.T) {
		src := `Ellipsis = 0
values: list[int] = [1]
print(str(values[Ellipsis]))
`
		require.Equal(t, "1\n", run(t, src))
	})

	t.Run("local shadowing", func(t *testing.T) {
		src := `def value() -> int:
    Ellipsis = 2
    return Ellipsis
print(str(value()))
`
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("ordering rejected", func(t *testing.T) {
		_, err := Compile(strings.NewReader("assert ... < ...\n"), WithOutput(io.Discard))
		require.Error(t, err)
		code(t, err, token.NotComparable)
	})

	t.Run("subscripts rejected for every receiver", func(t *testing.T) {
		receivers := []struct {
			name string
			src  string
		}{
			{"list", "value: list[int] = [1]\n"},
			{"dict", "value: dict[str, int] = {\"x\": 1}\n"},
			{"tuple", "value: tuple[int] = (1,)\n"},
			{"str", "value: str = \"x\"\n"},
			{"bytes", "value: bytes = b\"x\"\n"},
			{"class", `class Value:
    def __getitem__(self, index: int) -> int:
        return index
value: Value = Value()
`},
		}
		for _, receiver := range receivers {
			for _, index := range []string{"...", "Ellipsis"} {
				errs := checkOnly(t, receiver.src+"result = value["+index+"]\n")
				require.NotEmptyf(t, errs, "receiver=%s index=%s", receiver.name, index)
				code(t, errs, token.UnsupportedFeature)
				require.Containsf(t, errs.Error(), "ellipsis subscript is not supported", "receiver=%s index=%s", receiver.name, index)
			}
		}
	})

	t.Run("type is not constructible", func(t *testing.T) {
		_, err := Compile(strings.NewReader("x = EllipsisType()\n"), WithOutput(io.Discard))
		require.Error(t, err)
		code(t, err, token.UndefinedName)
	})
}
