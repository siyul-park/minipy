# Derived from CPython Lib/test/test_int.py (PSF License, docs/reference/SOURCES.md).
# Exercises values near the signed-64-bit boundary that minipy represents
# without overflow (see docs/compatibility.md for the wraparound divergence
# case covering values that actually exceed the boundary).
big: int = 9223372036854775807
print(big)
print(big - 1)
print(big > 0)
small: int = -9223372036854775807
print(small)
print(small < 0)
print(big + small)
