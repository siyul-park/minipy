# Derived from CPython Lib/test/test_float.py (PSF License, docs/reference/SOURCES.md).
# Exercises augmented assignment on a float binding and mixed int/float
# arithmetic promotion.
x: float = 1.0
x += 0.5
print(x)
x *= 4.0
print(x)
x -= 1.0
print(x)
x /= 2.0
print(x)

a: int = 3
b: float = 2.0
print(a + b)
print(a / b)
print(a * b)
c: float = a + b
print(c)
