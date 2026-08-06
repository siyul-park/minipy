package operator_test

import (
	"io"
	"testing"

	"github.com/siyul-park/minipy/ast"
	"github.com/siyul-park/minipy/module"
	"github.com/siyul-park/minipy/operator"
	"github.com/siyul-park/minipy/token"
	"github.com/siyul-park/minipy/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	vmtypes "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// stubChecker satisfies module.Checker for the type-level rules, counting errors.
type stubChecker struct{ errs int }

func (c *stubChecker) Check(ast.Expr) types.Type                     { return types.Invalid }
func (c *stubChecker) CheckWithHint(ast.Expr, types.Type) types.Type { return types.Invalid }
func (c *stubChecker) SetType(ast.Expr, types.Type)                  {}
func (c *stubChecker) ResolveType(ast.Expr) types.Type               { return types.Invalid }
func (c *stubChecker) Error(token.Pos, token.Code, string, ...any) {
	c.errs++
}

// stubEmitter records both emitted opcodes and host-function calls, so a test
// can assert a comparison lowered through a real host function (containers,
// dynamic dispatch) and not only through inline opcodes.
type stubEmitter struct {
	ops       []instr.Opcode
	hostCalls []*interp.HostFunction
}

type stubRuntime struct{}

func (stubRuntime) Out() io.Writer { return io.Discard }

func (e *stubEmitter) Emit(op instr.Opcode, _ ...uint64) { e.ops = append(e.ops, op) }
func (*stubEmitter) Expr(ast.Expr)                       {}
func (*stubEmitter) Type(ast.Expr) types.Type            { return types.Invalid }
func (*stubEmitter) TypeIndex(types.Type) uint64         { return 0 }
func (*stubEmitter) ConstGet(vmtypes.Value)              {}
func (e *stubEmitter) CallHost(fn *interp.HostFunction) {
	e.hostCalls = append(e.hostCalls, fn)
}
func (*stubEmitter) CallHostVoid(*interp.HostFunction)        {}
func (*stubEmitter) Host(string, string) *interp.HostFunction { return nil }
func (*stubEmitter) Runtime() module.Runtime                  { return stubRuntime{} }
func (*stubEmitter) Label() instr.Label                       { return 0 }
func (*stubEmitter) Bind(instr.Label)                         {}
func (*stubEmitter) Br(instr.Label)                           {}
func (*stubEmitter) BrIf(instr.Label)                         {}
func (*stubEmitter) Tmp(vmtypes.Type) int                     { return 0 }

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
		{"mixed add", types.Int, types.Float, token.PLUS, types.Float, false},
		{"mixed add reverse", types.Float, types.Int, token.PLUS, types.Float, false},
		{"mixed sub", types.Int, types.Float, token.MINUS, types.Float, false},
		{"mixed sub reverse", types.Float, types.Int, token.MINUS, types.Float, false},
		{"mixed mul", types.Int, types.Float, token.STAR, types.Float, false},
		{"mixed mul reverse", types.Float, types.Int, token.STAR, types.Float, false},
		{"mixed truediv", types.Int, types.Float, token.SLASH, types.Float, false},
		{"mixed truediv reverse", types.Float, types.Int, token.SLASH, types.Float, false},
		{"mixed floordiv", types.Int, types.Float, token.DOUBLESLASH, types.Float, false},
		{"mixed floordiv reverse", types.Float, types.Int, token.DOUBLESLASH, types.Float, false},
		{"mixed mod", types.Int, types.Float, token.PERCENT, types.Float, false},
		{"mixed mod reverse", types.Float, types.Int, token.PERCENT, types.Float, false},
		{"mixed pow", types.Int, types.Float, token.DOUBLESTAR, types.Float, false},
		{"mixed pow reverse", types.Float, types.Int, token.DOUBLESTAR, types.Float, false},
		{"str concat", types.Str, types.Str, token.PLUS, types.Str, false},
		{"str repeat", types.Str, types.Int, token.STAR, types.Str, false},
		{"bytes concat", types.Bytes, types.Bytes, token.PLUS, types.Bytes, false},
		{"bytes plus str mismatch", types.Bytes, types.Str, token.PLUS, types.Invalid, true},
		{"bytes star unsupported", types.Bytes, types.Int, token.STAR, types.Invalid, true},
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
		{"int float eq", token.EQ, types.Int, types.Float, false},
		{"int float lt", token.LT, types.Int, types.Float, false},
		{"float int ge", token.GE, types.Float, types.Int, false},
		{"in list", token.IN, types.Int, types.NewList(types.Int), false},
		{"in non-container", token.IN, types.Int, types.Int, true},
		{"bytes eq", token.EQ, types.Bytes, types.Bytes, false},
		{"bytes ne", token.NE, types.Bytes, types.Bytes, false},
		{"bytes lt rejected", token.LT, types.Bytes, types.Bytes, true},
		{"bytes le rejected", token.LE, types.Bytes, types.Bytes, true},
		{"bytes in bytes needle wrong type", token.IN, types.Str, types.Bytes, true},
		{"int in bytes", token.IN, types.Int, types.Bytes, false},
		{"ellipsis is", token.IS, types.Ellipsis, types.Ellipsis, false},
		{"ellipsis is not", token.ISNOT, types.Ellipsis, types.Ellipsis, false},
		{"ellipsis eq", token.EQ, types.Ellipsis, types.Ellipsis, false},
		{"ellipsis ne", token.NE, types.Ellipsis, types.Ellipsis, false},
		{"ellipsis ordering rejected", token.LT, types.Ellipsis, types.Ellipsis, true},
		{"ellipsis cross-type equality rejected", token.EQ, types.Ellipsis, types.Int, true},
		{"ellipsis cross-type identity rejected", token.IS, types.Ellipsis, types.Int, true},
		{"list eq", token.EQ, types.NewList(types.Int), types.NewList(types.Int), false},
		{"list ne", token.NE, types.NewList(types.Int), types.NewList(types.Int), false},
		{"list lt orderable elem", token.LT, types.NewList(types.Int), types.NewList(types.Int), false},
		{"list lt str elem", token.LT, types.NewList(types.Str), types.NewList(types.Str), false},
		{"list lt non-orderable elem rejected", token.LT, types.NewList(types.NewList(types.Int)), types.NewList(types.NewList(types.Int)), true},
		{"list eq non-orderable elem still allowed", token.EQ, types.NewList(types.NewList(types.Int)), types.NewList(types.NewList(types.Int)), false},
		{"tuple eq", token.EQ, types.NewTuple(types.Int, types.Str), types.NewTuple(types.Int, types.Str), false},
		{"tuple lt orderable fields", token.LT, types.NewTuple(types.Int, types.Str), types.NewTuple(types.Int, types.Str), false},
		{"tuple lt non-orderable field rejected", token.LT, types.NewTuple(types.Int, types.NewList(types.Int)), types.NewTuple(types.Int, types.NewList(types.Int)), true},
		{"dict eq", token.EQ, types.NewDict(types.Str, types.Int), types.NewDict(types.Str, types.Int), false},
		{"dict lt rejected", token.LT, types.NewDict(types.Str, types.Int), types.NewDict(types.Str, types.Int), true},
		{"set eq", token.EQ, types.NewSet(types.Int), types.NewSet(types.Int), false},
		{"set le rejected", token.LE, types.NewSet(types.Int), types.NewSet(types.Int), true},
		{"class eq", token.EQ, types.NewClass("P", nil), types.NewClass("P", nil), false},
		{"class lt rejected", token.LT, types.NewClass("P", nil), types.NewClass("P", nil), true},
		{"iterator eq", token.EQ, types.NewIterator(types.Int), types.NewIterator(types.Int), false},
		{"iterator gt rejected", token.GT, types.NewIterator(types.Int), types.NewIterator(types.Int), true},
		{"callable eq", token.EQ, types.NewCallable([]types.Type{types.Int}, types.Bool), types.NewCallable([]types.Type{types.Int}, types.Bool), false},
		{"callable ge rejected", token.GE, types.NewCallable([]types.Type{types.Int}, types.Bool), types.NewCallable([]types.Type{types.Int}, types.Bool), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &stubChecker{}
			operator.Comparable(c, tt.op, tt.left, tt.right, token.Pos{})
			require.Equal(t, tt.wantErr, c.errs > 0)
		})
	}
}

func TestEmitCompareStack(t *testing.T) {
	tests := []struct {
		name string
		op   token.Type
		want []instr.Opcode
	}{
		{"is", token.IS, []instr.Opcode{instr.REF_EQ}},
		{"is not", token.ISNOT, []instr.Opcode{instr.REF_EQ, instr.I32_EQZ}},
		{"equal", token.EQ, []instr.Opcode{instr.REF_EQ}},
		{"not equal", token.NE, []instr.Opcode{instr.REF_EQ, instr.I32_EQZ}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &stubEmitter{}
			operator.EmitCompareStack(e, tt.op, types.Ellipsis, types.Ellipsis)
			require.Equal(t, tt.want, e.ops)
		})
	}

	t.Run("mixed int float lt", func(t *testing.T) {
		e := &stubEmitter{}
		operator.EmitCompareStack(e, token.LT, types.Int, types.Float)
		require.Equal(t, []instr.Opcode{instr.SWAP, instr.I64_TO_F64_S, instr.SWAP, instr.F64_LT}, e.ops)
	})

	t.Run("mixed float int gt", func(t *testing.T) {
		e := &stubEmitter{}
		operator.EmitCompareStack(e, token.GT, types.Float, types.Int)
		require.Equal(t, []instr.Opcode{instr.I64_TO_F64_S, instr.F64_GT}, e.ops)
	})

	t.Run("list eq calls a host function", func(t *testing.T) {
		e := &stubEmitter{}
		operator.EmitCompareStack(e, token.EQ, types.NewList(types.Int), types.NewList(types.Int))
		require.Len(t, e.hostCalls, 1)
		require.NotNil(t, e.hostCalls[0])
		require.Empty(t, e.ops)
	})

	t.Run("list ne inverts the equality host call", func(t *testing.T) {
		e := &stubEmitter{}
		operator.EmitCompareStack(e, token.NE, types.NewList(types.Int), types.NewList(types.Int))
		require.Len(t, e.hostCalls, 1)
		require.Equal(t, []instr.Opcode{instr.I32_EQZ}, e.ops)
	})

	t.Run("list lt reduces a host comparison against zero", func(t *testing.T) {
		e := &stubEmitter{}
		operator.EmitCompareStack(e, token.LT, types.NewList(types.Int), types.NewList(types.Int))
		require.Len(t, e.hostCalls, 1)
		require.Equal(t, []instr.Opcode{instr.I64_CONST, instr.I64_LT_S}, e.ops)
	})

	t.Run("tuple ge reduces a host comparison against zero", func(t *testing.T) {
		e := &stubEmitter{}
		tuple := types.NewTuple(types.Int, types.Str)
		operator.EmitCompareStack(e, token.GE, tuple, tuple)
		require.Len(t, e.hostCalls, 1)
		require.Equal(t, []instr.Opcode{instr.I64_CONST, instr.I64_GE_S}, e.ops)
	})

	t.Run("dict eq calls a host function", func(t *testing.T) {
		e := &stubEmitter{}
		dict := types.NewDict(types.Str, types.Int)
		operator.EmitCompareStack(e, token.EQ, dict, dict)
		require.Len(t, e.hostCalls, 1)
		require.Empty(t, e.ops)
	})

	t.Run("set ne calls a host function and inverts", func(t *testing.T) {
		e := &stubEmitter{}
		set := types.NewSet(types.Int)
		operator.EmitCompareStack(e, token.NE, set, set)
		require.Len(t, e.hostCalls, 1)
		require.Equal(t, []instr.Opcode{instr.I32_EQZ}, e.ops)
	})

	t.Run("class eq lowers to identity", func(t *testing.T) {
		e := &stubEmitter{}
		class := types.NewClass("P", nil)
		operator.EmitCompareStack(e, token.EQ, class, class)
		require.Equal(t, []instr.Opcode{instr.REF_EQ}, e.ops)
		require.Empty(t, e.hostCalls)
	})

	t.Run("iterator ne lowers to inverted identity", func(t *testing.T) {
		e := &stubEmitter{}
		it := types.NewIterator(types.Int)
		operator.EmitCompareStack(e, token.NE, it, it)
		require.Equal(t, []instr.Opcode{instr.REF_EQ, instr.I32_EQZ}, e.ops)
	})

	t.Run("callable eq lowers to identity", func(t *testing.T) {
		e := &stubEmitter{}
		callable := types.NewCallable([]types.Type{types.Int}, types.Bool)
		operator.EmitCompareStack(e, token.EQ, callable, callable)
		require.Equal(t, []instr.Opcode{instr.REF_EQ}, e.ops)
	})
}

// TestComparableEmitSymmetry is the checker/lowerer symmetry guard for
// comparisons (AGENTS.md Completion Gate item 6, docs/coding-patterns.md
// SS7.4): for every type in the catalogue below and every comparison
// operator, whenever Comparable admits the pair, EmitCompareStack MUST lower
// it without panicking. CmpOpcode documents unsupported input as an
// unreachable checker/lowerer invariant violation; this test is what keeps
// that claim true as new types are added to either side. Before this fix,
// every container type here was admitted by Comparable but panicked in
// CmpOpcode — this test reproduces the whole admitted matrix, not only the
// cases in the bug report.
func TestComparableEmitSymmetry(t *testing.T) {
	catalogue := []types.Type{
		types.Int,
		types.Float,
		types.Bool,
		types.Str,
		types.Bytes,
		types.Ellipsis,
		types.NewList(types.Int),
		types.NewList(types.Str),
		types.NewList(types.Bool),
		types.NewList(types.Float),
		types.NewList(types.NewList(types.Int)),
		types.NewDict(types.Str, types.Int),
		types.NewDict(types.Int, types.NewList(types.Str)),
		types.NewSet(types.Int),
		types.NewTuple(types.Int, types.Str),
		types.NewTuple(types.Int, types.NewList(types.Int)),
		types.NewClass("P", []types.Field{{Name: "x", Type: types.Int}}),
		types.NewIterator(types.Int),
		types.NewCallable([]types.Type{types.Int}, types.Bool),
		types.Any,
		types.NewUnion(types.Int, types.Str),
		types.NewUnion(types.NewList(types.Int), types.Str),
	}
	ops := []token.Type{
		token.EQ, token.NE, token.LT, token.LE, token.GT, token.GE,
		token.IS, token.ISNOT,
	}

	admitted := 0
	for _, left := range catalogue {
		for _, right := range catalogue {
			for _, op := range ops {
				name := left.String() + " " + op.String() + " " + right.String()
				t.Run(name, func(t *testing.T) {
					c := &stubChecker{}
					operator.Comparable(c, op, left, right, token.Pos{})
					if c.errs > 0 {
						return
					}
					admitted++
					e := &stubEmitter{}
					require.NotPanics(t, func() {
						operator.EmitCompareStack(e, op, left, right)
					}, "checker admitted %s but the emitter panicked", name)
					require.True(t, len(e.ops) > 0 || len(e.hostCalls) > 0,
						"checker admitted %s but the emitter produced no instructions", name)
				})
			}
		}
	}
	require.Positive(t, admitted, "catalogue produced no checker-admitted comparisons to guard")
}

func TestContainsType(t *testing.T) {
	require.True(t, operator.ContainsType(types.Int, types.NewList(types.Int)))
	require.False(t, operator.ContainsType(types.Int, types.Int))
	require.True(t, operator.ContainsType(types.Int, types.Bytes))
	require.False(t, operator.ContainsType(types.Str, types.Bytes))
}

func TestNewModuleSymbols(t *testing.T) {
	m := operator.New()
	require.Equal(t, "operator", m.Name())
	want := []string{"add", "sub", "mul", "truediv", "floordiv", "mod", "pow",
		"and_", "or_", "xor", "lshift", "rshift",
		"eq", "ne", "lt", "le", "gt", "ge",
		"neg", "pos", "invert", "contains", "not_", "abs", "truth"}
	require.Equal(t, want, m.Names())
}
