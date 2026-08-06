package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompileAssertInTryExcept regression-tests a failing assert inside
// try/except: assertStmt previously threw a bare string payload instead of a
// proper AssertionError instance, so the except handler's classID read
// (STRUCT_GET on a payload with no struct shape) trapped with a type
// mismatch instead of matching. See docs/spec/05-codegen.md,
// "Exception instance construction".
func TestCompileAssertInTryExcept(t *testing.T) {
	t.Run("failing assert without message is caught", func(t *testing.T) {
		src := "try:\n" +
			"    assert 1 == 2\n" +
			"except AssertionError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("failing assert with message is caught and message is readable", func(t *testing.T) {
		src := "try:\n" +
			"    assert 1 == 2, \"boom\"\n" +
			"except AssertionError as e:\n" +
			"    print(e.message)\n"
		require.Equal(t, "boom\n", run(t, src))
	})

	t.Run("failing assert inside a function propagates to the caller's except", func(t *testing.T) {
		src := "def check(n: int) -> None:\n" +
			"    assert n > 0, \"must be positive\"\n" +
			"try:\n" +
			"    check(-5)\n" +
			"except AssertionError as e:\n" +
			"    print(e.message)\n"
		require.Equal(t, "must be positive\n", run(t, src))
	})

	t.Run("passing assert does not raise inside try", func(t *testing.T) {
		src := "try:\n" +
			"    assert True\n" +
			"    print(\"ok\")\n" +
			"except AssertionError:\n" +
			"    print(\"unreachable\")\n"
		require.Equal(t, "ok\n", run(t, src))
	})

	t.Run("non-str printable assert message is coerced to str", func(t *testing.T) {
		src := "try:\n" +
			"    assert 1 == 2, 42\n" +
			"except AssertionError as e:\n" +
			"    print(e.message)\n"
		require.Equal(t, "42\n", run(t, src))
	})

	t.Run("base Exception handler still catches AssertionError", func(t *testing.T) {
		src := "try:\n" +
			"    assert 1 == 2, \"boom\"\n" +
			"except Exception:\n" +
			"    print(\"caught as base\")\n"
		require.Equal(t, "caught as base\n", run(t, src))
	})
}
