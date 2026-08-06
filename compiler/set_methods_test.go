package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileSetMethods(t *testing.T) {
	t.Run("add inserts element", func(t *testing.T) {
		src := "s: set[int] = {1, 2}\n" +
			"s.add(3)\n" +
			"print(str(len(s)))\n"
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("add duplicate is no-op", func(t *testing.T) {
		src := "s: set[int] = {1, 2, 3}\n" +
			"s.add(2)\n" +
			"print(str(len(s)))\n"
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("remove existing element", func(t *testing.T) {
		src := "s: set[int] = {1, 2, 3}\n" +
			"s.remove(2)\n" +
			"print(str(len(s)))\n"
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("remove raises KeyError for missing element", func(t *testing.T) {
		src := "s: set[int] = {1, 2}\n" +
			"try:\n" +
			"    s.remove(9)\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("discard existing element", func(t *testing.T) {
		src := "s: set[int] = {1, 2, 3}\n" +
			"s.discard(2)\n" +
			"print(str(len(s)))\n"
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("discard missing element is silent", func(t *testing.T) {
		src := "s: set[int] = {1, 2}\n" +
			"s.discard(9)\n" +
			"print(str(len(s)))\n"
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("pop removes an element", func(t *testing.T) {
		src := "s: set[int] = {42}\n" +
			"v: int = s.pop()\n" +
			"print(str(v) + \" \" + str(len(s)))\n"
		require.Equal(t, "42 0\n", run(t, src))
	})

	t.Run("pop raises KeyError on empty set", func(t *testing.T) {
		src := "s: set[int] = {1}\n" +
			"s.pop()\n" +
			"try:\n" +
			"    s.pop()\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("clear empties the set", func(t *testing.T) {
		src := "s: set[int] = {1, 2, 3}\n" +
			"s.clear()\n" +
			"print(str(len(s)))\n"
		require.Equal(t, "0\n", run(t, src))
	})

	t.Run("union returns combined set", func(t *testing.T) {
		src := "a: set[int] = {1, 2}\n" +
			"b: set[int] = {2, 3}\n" +
			"c: set[int] = a.union(b)\n" +
			"print(str(len(c)))\n"
		require.Equal(t, "3\n", run(t, src))
	})

	t.Run("intersection returns common elements", func(t *testing.T) {
		src := "a: set[int] = {1, 2, 3}\n" +
			"b: set[int] = {2, 3, 4}\n" +
			"c: set[int] = a.intersection(b)\n" +
			"print(str(len(c)))\n"
		require.Equal(t, "2\n", run(t, src))
	})

	t.Run("difference returns elements not in other", func(t *testing.T) {
		src := "a: set[int] = {1, 2, 3}\n" +
			"b: set[int] = {2, 3, 4}\n" +
			"c: set[int] = a.difference(b)\n" +
			"print(str(len(c)))\n"
		require.Equal(t, "1\n", run(t, src))
	})

	t.Run("issubset true", func(t *testing.T) {
		src := "a: set[int] = {1, 2}\n" +
			"b: set[int] = {1, 2, 3}\n" +
			"print(str(a.issubset(b)))\n"
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("issubset false", func(t *testing.T) {
		src := "a: set[int] = {1, 2, 4}\n" +
			"b: set[int] = {1, 2, 3}\n" +
			"print(str(a.issubset(b)))\n"
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("issuperset true", func(t *testing.T) {
		src := "a: set[int] = {1, 2, 3}\n" +
			"b: set[int] = {1, 2}\n" +
			"print(str(a.issuperset(b)))\n"
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("issuperset false", func(t *testing.T) {
		src := "a: set[int] = {1, 2, 3}\n" +
			"b: set[int] = {1, 4}\n" +
			"print(str(a.issuperset(b)))\n"
		require.Equal(t, "False\n", run(t, src))
	})

	t.Run("copy returns independent set", func(t *testing.T) {
		src := "s: set[int] = {1, 2, 3}\n" +
			"t: set[int] = s.copy()\n" +
			"t.add(4)\n" +
			"print(str(len(s)) + \" \" + str(len(t)))\n"
		require.Equal(t, "3 4\n", run(t, src))
	})

	t.Run("set with str elements", func(t *testing.T) {
		src := "s: set[str] = {\"a\", \"b\"}\n" +
			"s.add(\"c\")\n" +
			"s.discard(\"a\")\n" +
			"print(str(len(s)))\n"
		require.Equal(t, "2\n", run(t, src))
	})

	// A set[str] literal passed inline is an unrooted temporary: these
	// builders copy borrowed entries into a fresh map without retaining
	// them, so releasing the temporary source set(s) used to free the
	// entries out from under the result.
	t.Run("union of temporary str set literals", func(t *testing.T) {
		got := run(t, `print(str(len({"a", "b"}.union({"b", "c"}))))`)
		require.Equal(t, "3\n", got)
	})

	t.Run("intersection of temporary str set literals", func(t *testing.T) {
		got := run(t, `print(str(len({"a", "b"}.intersection({"b", "c"}))))`)
		require.Equal(t, "1\n", got)
	})

	t.Run("difference of temporary str set literals", func(t *testing.T) {
		got := run(t, `print(str(len({"a", "b"}.difference({"b", "c"}))))`)
		require.Equal(t, "1\n", got)
	})

	t.Run("copy of temporary str set literal", func(t *testing.T) {
		got := run(t, `print(str(len({"a", "b"}.copy())))`)
		require.Equal(t, "2\n", got)
	})
}
