package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileDictMethods(t *testing.T) {
	t.Run("pop removes and returns value", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"v: int = d.pop(\"a\")\n" +
			"print(str(v) + \" \" + str(len(d)))\n"
		require.Equal(t, "1 1\n", run(t, src))
	})

	t.Run("pop raises KeyError for missing key", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"try:\n" +
			"    d.pop(\"x\")\n" +
			"except KeyError:\n" +
			"    print(\"caught\")\n"
		require.Equal(t, "caught\n", run(t, src))
	})

	t.Run("update merges another dict", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"d.update({\"b\": 3, \"c\": 4})\n" +
			"print(str(d[\"a\"]) + \" \" + str(d[\"b\"]) + \" \" + str(d[\"c\"]))\n"
		require.Equal(t, "1 3 4\n", run(t, src))
	})

	t.Run("setdefault returns existing value", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"v: int = d.setdefault(\"a\", 99)\n" +
			"print(str(v) + \" \" + str(d[\"a\"]))\n"
		require.Equal(t, "1 1\n", run(t, src))
	})

	t.Run("setdefault inserts default when missing", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1}\n" +
			"v: int = d.setdefault(\"b\", 42)\n" +
			"print(str(v) + \" \" + str(d[\"b\"]))\n"
		require.Equal(t, "42 42\n", run(t, src))
	})

	t.Run("clear empties the dict", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"d.clear()\n" +
			"print(str(len(d)))\n"
		require.Equal(t, "0\n", run(t, src))
	})

	t.Run("copy returns independent dict", func(t *testing.T) {
		src := "d: dict[str, int] = {\"a\": 1, \"b\": 2}\n" +
			"e: dict[str, int] = d.copy()\n" +
			"e[\"c\"] = 3\n" +
			"print(str(len(d)) + \" \" + str(len(e)))\n"
		require.Equal(t, "2 3\n", run(t, src))
	})

	t.Run("pop with int keys", func(t *testing.T) {
		src := "d: dict[int, str] = {1: \"one\", 2: \"two\"}\n" +
			"v: str = d.pop(1)\n" +
			"print(v + \" \" + str(len(d)))\n"
		require.Equal(t, "one 1\n", run(t, src))
	})
}
