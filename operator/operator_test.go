package operator_test

import (
	"testing"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"
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
			if !types.Equal(got, tt.want) {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
			if (c.errs > 0) != tt.wantErr {
				t.Fatalf("errs=%d, wantErr=%v", c.errs, tt.wantErr)
			}
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
			if (c.errs > 0) != tt.wantErr {
				t.Fatalf("errs=%d, wantErr=%v", c.errs, tt.wantErr)
			}
		})
	}
}

func TestContainsType(t *testing.T) {
	if !operator.ContainsType(types.Int, types.NewList(types.Int)) {
		t.Error("int in list[int] should be allowed")
	}
	if operator.ContainsType(types.Int, types.Int) {
		t.Error("int is not a container")
	}
}

func TestOperatorNames(t *testing.T) {
	if name, ok := operator.BinaryName(token.PLUS); !ok || name != "add" {
		t.Errorf("BinaryName(PLUS) = %q, %v", name, ok)
	}
	if name, ok := operator.CompareName(token.EQ); !ok || name != "eq" {
		t.Errorf("CompareName(EQ) = %q, %v", name, ok)
	}
	if name, ok := operator.UnaryName(token.MINUS); !ok || name != "neg" {
		t.Errorf("UnaryName(MINUS) = %q, %v", name, ok)
	}
	if operator.ContainsName() != "contains" || operator.NotName() != "not_" {
		t.Error("contains/not_ name mismatch")
	}
	if _, ok := operator.BinaryName(token.EQ); ok {
		t.Error("EQ is not a binary operator name")
	}
}

func TestNewModuleSymbols(t *testing.T) {
	m := operator.New()
	if m.Name() != "operator" {
		t.Fatalf("module name = %q", m.Name())
	}
	want := []string{"add", "sub", "mul", "truediv", "floordiv", "mod", "pow",
		"and_", "or_", "xor", "lshift", "rshift",
		"eq", "ne", "lt", "le", "gt", "ge",
		"neg", "pos", "invert", "contains", "not_", "abs", "truth"}
	for _, name := range want {
		if _, ok := m.Symbol(name); !ok {
			t.Errorf("missing symbol %q", name)
		}
	}
}
