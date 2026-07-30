package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileStringFormat(t *testing.T) {
	t.Run("simple placeholder", func(t *testing.T) {
		src := "print(\"hello {}\".format(\"world\"))\n"
		require.Equal(t, "hello world\n", run(t, src))
	})

	t.Run("multiple auto-numbered placeholders", func(t *testing.T) {
		src := "print(\"{} + {} = {}\".format(1, 2, 3))\n"
		require.Equal(t, "1 + 2 = 3\n", run(t, src))
	})

	t.Run("explicit positional indexes", func(t *testing.T) {
		src := "print(\"{0} and {1}\".format(\"a\", \"b\"))\n"
		require.Equal(t, "a and b\n", run(t, src))
	})

	t.Run("reversed positional indexes", func(t *testing.T) {
		src := "print(\"{1} before {0}\".format(\"a\", \"b\"))\n"
		require.Equal(t, "b before a\n", run(t, src))
	})

	t.Run("no placeholders", func(t *testing.T) {
		src := "print(\"hello\".format())\n"
		require.Equal(t, "hello\n", run(t, src))
	})

	t.Run("escaped braces", func(t *testing.T) {
		src := "print(\"{{0}} is {}\".format(\"ok\"))\n"
		require.Equal(t, "{0} is ok\n", run(t, src))
	})

	t.Run("format with bool", func(t *testing.T) {
		src := "print(\"{} is {}\".format(True, False))\n"
		require.Equal(t, "True is False\n", run(t, src))
	})

	t.Run("format with float", func(t *testing.T) {
		src := "print(\"pi is {}\".format(3.14))\n"
		require.Equal(t, "pi is 3.14\n", run(t, src))
	})

	t.Run("repeated index", func(t *testing.T) {
		src := "print(\"{0}{0}{0}\".format(\"ab\"))\n"
		require.Equal(t, "ababab\n", run(t, src))
	})
}
