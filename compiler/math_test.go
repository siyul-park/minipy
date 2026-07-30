package compiler

import (
	"strings"
	"testing"

	"github.com/siyul-park/minipy/token"
	"github.com/stretchr/testify/require"
)

func TestMathModule(t *testing.T) {
	t.Run("sqrt", func(t *testing.T) {
		src := "import math\nprint(str(math.sqrt(4.0)))\n"
		require.Equal(t, "2.0\n", run(t, src))
	})

	t.Run("sqrt with int promotion", func(t *testing.T) {
		src := "import math\nprint(str(math.sqrt(9)))\n"
		require.Equal(t, "3.0\n", run(t, src))
	})

	t.Run("pi constant", func(t *testing.T) {
		src := "import math\nprint(str(math.pi))\n"
		require.Equal(t, "3.141592653589793\n", run(t, src))
	})

	t.Run("pi assigned to variable", func(t *testing.T) {
		src := "import math\nx: float = math.pi\nprint(str(x))\n"
		require.Equal(t, "3.141592653589793\n", run(t, src))
	})

	t.Run("e constant", func(t *testing.T) {
		src := "import math\nprint(str(math.e))\n"
		require.Equal(t, "2.718281828459045\n", run(t, src))
	})

	t.Run("tau constant", func(t *testing.T) {
		src := "import math\nprint(str(math.tau))\n"
		require.Equal(t, "6.283185307179586\n", run(t, src))
	})

	t.Run("inf constant", func(t *testing.T) {
		src := "import math\nprint(str(math.inf))\n"
		require.Equal(t, "+Inf\n", run(t, src))
	})

	t.Run("nan constant", func(t *testing.T) {
		src := "import math\nprint(str(math.isnan(math.nan)))\n"
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("factorial", func(t *testing.T) {
		src := "from math import factorial\nprint(str(factorial(5)))\n"
		require.Equal(t, "120\n", run(t, src))
	})

	t.Run("gcd", func(t *testing.T) {
		src := "from math import gcd\nprint(str(gcd(12, 8)))\n"
		require.Equal(t, "4\n", run(t, src))
	})

	t.Run("ceil", func(t *testing.T) {
		src := "import math\nprint(str(math.ceil(2.3)))\n"
		require.Equal(t, "3.0\n", run(t, src))
	})

	t.Run("floor", func(t *testing.T) {
		src := "import math\nprint(str(math.floor(2.7)))\n"
		require.Equal(t, "2.0\n", run(t, src))
	})

	t.Run("fabs", func(t *testing.T) {
		src := "import math\nprint(str(math.fabs(-3.5)))\n"
		require.Equal(t, "3.5\n", run(t, src))
	})

	t.Run("isnan", func(t *testing.T) {
		src := "import math\nprint(str(math.isnan(math.nan)))\n"
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isinf", func(t *testing.T) {
		src := "import math\nprint(str(math.isinf(math.inf)))\n"
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("isfinite", func(t *testing.T) {
		src := "import math\nprint(str(math.isfinite(1.0)))\n"
		require.Equal(t, "True\n", run(t, src))
	})

	t.Run("pow", func(t *testing.T) {
		src := "import math\nprint(str(math.pow(2.0, 10.0)))\n"
		require.Equal(t, "1024.0\n", run(t, src))
	})

	t.Run("degrees", func(t *testing.T) {
		src := "import math\nprint(str(math.degrees(math.pi)))\n"
		require.Equal(t, "180.0\n", run(t, src))
	})

	t.Run("radians", func(t *testing.T) {
		src := "import math\nprint(str(math.radians(180.0)))\n"
		require.Equal(t, "3.141592653589793\n", run(t, src))
	})

	t.Run("from import constant", func(t *testing.T) {
		src := "from math import pi\nprint(str(pi))\n"
		require.Equal(t, "3.141592653589793\n", run(t, src))
	})
}

func TestMathModuleErrors(t *testing.T) {
	t.Run("sqrt with string argument", func(t *testing.T) {
		src := "import math\nmath.sqrt(\"x\")\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("factorial with float argument", func(t *testing.T) {
		src := "from math import factorial\nfactorial(3.0)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})

	t.Run("gcd with float argument", func(t *testing.T) {
		src := "from math import gcd\ngcd(1.0, 2.0)\n"
		_, err := Compile(strings.NewReader(src))
		require.Error(t, err)
		code(t, err, token.TypeMismatch)
	})
}

func TestMathModuleRuntimeErrors(t *testing.T) {
	t.Run("factorial of negative number", func(t *testing.T) {
		src := "from math import factorial\nprint(str(factorial(-1)))\n"
		err := runError(t, src)
		require.Error(t, err)
		require.Contains(t, err.Error(), "factorial")
	})
}
