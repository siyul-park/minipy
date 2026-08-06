package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompileExceptionSubclassFields regression-tests a user-defined
// Exception subclass with its own declared field and a custom __init__: the
// lowerer previously always allocated the shared BaseException struct shape
// (classID + message only) regardless of the constructed class, so reading
// an extra field like `msg` indexed past the allocated struct and segfaulted
// the VM. See docs/spec/05-codegen.md, emitExceptionStruct.
func TestCompileExceptionSubclassFields(t *testing.T) {
	t.Run("custom init sets its own field", func(t *testing.T) {
		src := "class MyError(Exception):\n" +
			"    msg: str\n" +
			"    def __init__(self, m: str) -> None:\n" +
			"        self.msg = m\n" +
			"e: MyError = MyError(\"x\")\n" +
			"print(e.msg)\n"
		require.Equal(t, "x\n", run(t, src))
	})

	t.Run("custom init field survives raise and except-as", func(t *testing.T) {
		src := "class MyError(Exception):\n" +
			"    msg: str\n" +
			"    def __init__(self, m: str) -> None:\n" +
			"        self.msg = m\n" +
			"def f() -> None:\n" +
			"    raise MyError(\"boom\")\n" +
			"try:\n" +
			"    f()\n" +
			"except MyError as e:\n" +
			"    print(e.msg)\n"
		require.Equal(t, "boom\n", run(t, src))
	})

	t.Run("declared default field keeps its value alongside a custom init", func(t *testing.T) {
		src := "class MyError(Exception):\n" +
			"    msg: str\n" +
			"    code: int = 7\n" +
			"    def __init__(self, m: str) -> None:\n" +
			"        self.msg = m\n" +
			"e: MyError = MyError(\"hi\")\n" +
			"print(str(e.code))\n"
		require.Equal(t, "7\n", run(t, src))
	})

	t.Run("subclass with no extra fields still catches by base", func(t *testing.T) {
		src := "class Plain(Exception):\n" +
			"    pass\n" +
			"try:\n" +
			"    raise Plain(\"nope\")\n" +
			"except Exception:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})
}
