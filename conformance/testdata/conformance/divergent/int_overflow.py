# Derived from CPython Lib/test/test_int.py (PSF License, docs/reference/SOURCES.md).
# Exercises integer overflow past the signed-64-bit boundary. CPython integers
# are arbitrary precision; minipy integers are signed 64-bit and wrap on
# overflow. See docs/compatibility.md.
# minipy-divergence: minipy int is signed 64-bit and wraps on overflow; CPython int is arbitrary precision.
# minipy-divergence-doc: docs/compatibility.md#types
n: int = 9223372036854775807
n = n + 1
print(str(n))
