package compiler

import (
	"strconv"
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func TestRandomModule(t *testing.T) {
	t.Run("random returns float in range", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: float = random.random()\nprint(str(x))\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		f, err := strconv.ParseFloat(out, 64)
		require.NoError(t, err)
		require.True(t, f >= 0.0 && f < 1.0, "expected [0,1), got %f", f)
	})

	t.Run("randint returns int in range", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: int = random.randint(1, 10)\nprint(str(x))\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		n, err := strconv.ParseInt(out, 10, 64)
		require.NoError(t, err)
		require.True(t, n >= 1 && n <= 10, "expected [1,10], got %d", n)
	})

	t.Run("randrange one arg", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: int = random.randrange(5)\nprint(str(x))\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		n, err := strconv.ParseInt(out, 10, 64)
		require.NoError(t, err)
		require.True(t, n >= 0 && n < 5, "expected [0,5), got %d", n)
	})

	t.Run("randrange two args", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: int = random.randrange(3, 8)\nprint(str(x))\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		n, err := strconv.ParseInt(out, 10, 64)
		require.NoError(t, err)
		require.True(t, n >= 3 && n < 8, "expected [3,8), got %d", n)
	})

	t.Run("uniform returns float in range", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: float = random.uniform(1.0, 5.0)\nprint(str(x))\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		f, err := strconv.ParseFloat(out, 64)
		require.NoError(t, err)
		require.True(t, f >= 1.0 && f <= 5.0, "expected [1,5], got %f", f)
	})

	t.Run("uniform with int promotion", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: float = random.uniform(0, 10)\nprint(str(x))\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		f, err := strconv.ParseFloat(out, 64)
		require.NoError(t, err)
		require.True(t, f >= 0.0 && f <= 10.0, "expected [0,10], got %f", f)
	})

	t.Run("choice picks element from list", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nxs: list[int] = [10, 20, 30, 40, 50]\nx: int = random.choice(xs)\nprint(str(x))\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		n, err := strconv.ParseInt(out, 10, 64)
		require.NoError(t, err)
		require.Contains(t, []int64{10, 20, 30, 40, 50}, n)
	})

	t.Run("shuffle modifies list", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nxs: list[int] = [1, 2, 3, 4, 5]\nrandom.shuffle(xs)\nprint(str(len(xs)))\n"
		out := run(t, src)
		require.Equal(t, "5\n", out)
	})

	t.Run("seed deterministic output", func(t *testing.T) {
		src := "import random\nrandom.seed(123)\na: int = random.randint(0, 100)\nrandom.seed(123)\nb: int = random.randint(0, 100)\nprint(str(a == b))\n"
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("from import works", func(t *testing.T) {
		src := "from random import random, seed\nseed(42)\ny: float = random()\nprint(str(y))\n"
		out := run(t, src)
		out = strings.TrimSpace(out)
		f, err := strconv.ParseFloat(out, 64)
		require.NoError(t, err)
		require.True(t, f >= 0.0 && f < 1.0, "expected [0,1), got %f", f)
	})
}

func TestRandomModuleErrors(t *testing.T) {
	t.Run("random with arguments", func(t *testing.T) {
		src := "import random\nrandom.random(1)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.ArityMismatch)
	})

	t.Run("randint with wrong type", func(t *testing.T) {
		src := "import random\nrandom.randint(1.0, 2.0)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("randint with wrong arity", func(t *testing.T) {
		src := "import random\nrandom.randint(1)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.ArityMismatch)
	})

	t.Run("randrange with wrong type", func(t *testing.T) {
		src := "import random\nrandom.randrange(1.0)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("uniform with string argument", func(t *testing.T) {
		src := "import random\nrandom.uniform(\"a\", \"b\")\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("choice with non-list argument", func(t *testing.T) {
		src := "import random\nrandom.choice(42)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("shuffle with non-list argument", func(t *testing.T) {
		src := "import random\nrandom.shuffle(42)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("seed with wrong type", func(t *testing.T) {
		src := "import random\nrandom.seed(1.0)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})
}

func TestRandomModuleRuntimeErrors(t *testing.T) {
	t.Run("randrange with zero stop", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: int = random.randrange(0)\nprint(str(x))\n"
		err := runError(t, src)
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty range")
	})

	t.Run("randrange with inverted range", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: int = random.randrange(5, 3)\nprint(str(x))\n"
		err := runError(t, src)
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty range")
	})

	t.Run("randint with inverted range", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nx: int = random.randint(5, 3)\nprint(str(x))\n"
		err := runError(t, src)
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty range")
	})

	t.Run("choice with empty list", func(t *testing.T) {
		src := "import random\nrandom.seed(42)\nxs: list[int] = []\nx: int = random.choice(xs)\nprint(str(x))\n"
		err := runError(t, src)
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty sequence")
	})
}
