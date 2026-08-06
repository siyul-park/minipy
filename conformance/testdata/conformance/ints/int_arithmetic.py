# Derived from CPython Lib/test/test_int.py (PSF License, docs/reference/SOURCES.md).
# Exercises basic integer arithmetic and unary operators.
a: int = 17
b: int = 5
print(a + b)
print(a - b)
print(a * b)
print(a // b)
print(a % b)
print(-a)
print(+a)
print(-(-a))
print(a + b * 2 - 3)
print((a + b) * 2)
