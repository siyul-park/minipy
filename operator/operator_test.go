package operator_test

import (
	"testing"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/stretchr/testify/require"
)

// stubChecker satisfies module.Checker for the type-level rules, counting errors.
type stubChecker struct{ errs int }

func (c *stubChecker) Check(ast.Expr) types.Type       { return types.Invalid }
func (c *stubChecker) Type(ast.Expr) types.Type        { return types.Invalid }
func (c *stubChecker) SetType(ast.Expr, types.Type)    {}
func (c *stubChecker) ResolveType(ast.Expr) types.Type { return types.Invalid }
func (c *stubChecker) Error(token.Pos, token.Code, string, ...any) {
	c.errs++
}

func TestBinaryType(t *testing.T) {
	tests := []struct {
		name        string
		left, right types.Type
		op          token.Type
		want        types.Type
		wantErr     bool
	}{
		{"int add", types.Int, types.Int, token.PLUS, types.Int, false},
		{"float add", types.Float, types.Float, token.PLUS, types.Float, false},
		{"mixed add", types.Int, types.Float, token.PLUS, types.Invalid, true},
		{"str concat", types.Str, types.Str, token.PLUS, types.Str, false},
		{"str repeat", types.Str, types.Int, token.STAR, types.Str, false},
		{"true div", types.Int, types.Int, token.SLASH, types.Float, false},
		{"bitand", types.Int, types.Int, token.AMP, types.Int, false},
		{"bitand float", types.Float, types.Float, token.AMP, types.Invalid, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &stubChecker{}
			got := operator.BinaryType(c, tt.left, tt.op, tt.right, token.Pos{})
			require.Truef(t, types.Equal(got, tt.want), "got %s, want %s", got, tt.want)
			require.Equal(t, tt.wantErr, c.errs > 0)
		})
	}
}

func TestComparable(t *testing.T) {
	tests := []struct {
		name        string
		op          token.Type
		left, right types.Type
		wantErr     bool
	}{
		{"eq int", token.EQ, types.Int, types.Int, false},
		{"eq mismatch", token.EQ, types.Int, types.Str, true},
		{"in list", token.IN, types.Int, types.NewList(types.Int), false},
		{"in non-container", token.IN, types.Int, types.Int, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &stubChecker{}
			operator.Comparable(c, tt.op, tt.left, tt.right, token.Pos{})
			require.Equal(t, tt.wantErr, c.errs > 0)
		})
	}
}

func TestContainsType(t *testing.T) {
	require.True(t, operator.ContainsType(types.Int, types.NewList(types.Int)))
	require.False(t, operator.ContainsType(types.Int, types.Int))
}

func TestOperatorNames(t *testing.T) {
	name, ok := operator.BinaryName(token.PLUS)
	require.True(t, ok)
	require.Equal(t, "add", name)

	name, ok = operator.CompareName(token.EQ)
	require.True(t, ok)
	require.Equal(t, "eq", name)

	name, ok = operator.UnaryName(token.MINUS)
	require.True(t, ok)
	require.Equal(t, "neg", name)

	require.Equal(t, "contains", operator.ContainsName())
	require.Equal(t, "not_", operator.NotName())

	_, ok = operator.BinaryName(token.EQ)
	require.False(t, ok)
}

func TestNewModuleSymbols(t *testing.T) {
	m := operator.New()
	require.Equal(t, "operator", m.Name())
	want := []string{"add", "sub", "mul", "truediv", "floordiv", "mod", "pow",
		"and_", "or_", "xor", "lshift", "rshift",
		"eq", "ne", "lt", "le", "gt", "ge",
		"neg", "pos", "invert", "contains", "not_", "abs", "truth"}
	for _, name := range want {
		_, ok := m.Symbol(name)
		require.Truef(t, ok, "missing symbol %q", name)
	}
}
