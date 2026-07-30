package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileStringMethods(t *testing.T) {
	t.Run("strip no args", func(t *testing.T) {
		src := `print("  hello  ".strip())`
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("strip with chars", func(t *testing.T) {
		src := `print("xxhelloxx".strip("x"))`
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("lstrip no args", func(t *testing.T) {
		src := `print("  hello  ".lstrip())`
		require.Equal(t, "hello  \n", run(t, src))
	})

	t.Run("lstrip with chars", func(t *testing.T) {
		src := `print("xxhello".lstrip("x"))`
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("rstrip no args", func(t *testing.T) {
		src := `print("  hello  ".rstrip())`
		require.Equal(t, "  hello\n", run(t, src))
	})

	t.Run("rstrip with chars", func(t *testing.T) {
		src := `print("helloxx".rstrip("x"))`
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("startswith true", func(t *testing.T) {
		src := `print(str("hello".startswith("he")))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("startswith false", func(t *testing.T) {
		src := `print(str("hello".startswith("lo")))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("endswith true", func(t *testing.T) {
		src := `print(str("hello".endswith("lo")))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("endswith false", func(t *testing.T) {
		src := `print(str("hello".endswith("he")))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("replace all", func(t *testing.T) {
		src := `print("hello".replace("l", "r"))`
		require.Equal(t, "herro\n", run(t, src))
	})

	t.Run("replace with count", func(t *testing.T) {
		src := `print("hello".replace("l", "r", 1))`
		require.Equal(t, "herlo\n", run(t, src))
	})

	t.Run("count", func(t *testing.T) {
		src := `print(str("hello".count("l")))`
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("isdigit true", func(t *testing.T) {
		src := `print(str("123".isdigit()))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isdigit false", func(t *testing.T) {
		src := `print(str("12a".isdigit()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("isdigit empty", func(t *testing.T) {
		src := `print(str("".isdigit()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("isalpha true", func(t *testing.T) {
		src := `print(str("hello".isalpha()))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isalpha false", func(t *testing.T) {
		src := `print(str("hello1".isalpha()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("isalnum true", func(t *testing.T) {
		src := `print(str("hello123".isalnum()))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isalnum false", func(t *testing.T) {
		src := `print(str("hello 123".isalnum()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("isspace true", func(t *testing.T) {
		src := `print(str("   ".isspace()))`
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isspace false", func(t *testing.T) {
		src := `print(str(" a ".isspace()))`
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("capitalize", func(t *testing.T) {
		src := `print("hello WORLD".capitalize())`
		require.Equal(t, "Hello world\n", run(t, src))
	})

	t.Run("title", func(t *testing.T) {
		src := `print("hello world".title())`
		require.Equal(t, "Hello World\n", run(t, src))
	})

	t.Run("swapcase", func(t *testing.T) {
		src := `print("Hello World".swapcase())`
		require.Equal(t, "hELLO wORLD\n", run(t, src))
	})

	t.Run("center", func(t *testing.T) {
		src := `print("hi".center(10))`
		require.Equal(t, "    hi    \n", run(t, src))
	})

	t.Run("center with fill", func(t *testing.T) {
		src := `print("hi".center(10, "*"))`
		require.Equal(t, "****hi****\n", run(t, src))
	})

	t.Run("ljust", func(t *testing.T) {
		src := `print("hi".ljust(10))`
		require.Equal(t, "hi        \n", run(t, src))
	})

	t.Run("ljust with fill", func(t *testing.T) {
		src := `print("hi".ljust(10, "*"))`
		require.Equal(t, "hi********\n", run(t, src))
	})

	t.Run("rjust", func(t *testing.T) {
		src := `print("hi".rjust(10))`
		require.Equal(t, "        hi\n", run(t, src))
	})

	t.Run("rjust with fill", func(t *testing.T) {
		src := `print("hi".rjust(10, "*"))`
		require.Equal(t, "********hi\n", run(t, src))
	})

	t.Run("zfill", func(t *testing.T) {
		src := `print("42".zfill(5))`
		require.Equal(t, "00042\n", run(t, src))
	})

	t.Run("zfill with sign", func(t *testing.T) {
		src := `print("-42".zfill(5))`
		require.Equal(t, "-0042\n", run(t, src))
	})

	t.Run("encode", func(t *testing.T) {
		src := `print(str(len("hello".encode())))`
		require.Equal(t, "5\n", run(t, src))
	})
}
