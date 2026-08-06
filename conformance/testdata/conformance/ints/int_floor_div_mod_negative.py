# Derived from CPython Lib/test/test_int.py (PSF License, docs/reference/SOURCES.md).
# Exercises floor division and modulo semantics, including negative operands
# (Python floor-divides toward negative infinity; % result has the divisor's sign).
print(7 // 3)
print(-7 // 3)
print(7 // -3)
print(-7 // -3)
print(7 % 3)
print(-7 % 3)
print(7 % -3)
print(-7 % -3)
print(divmod(7, 3))
print(divmod(-7, 3))
print(divmod(7, -3))
