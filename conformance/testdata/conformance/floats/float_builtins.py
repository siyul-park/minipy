# Derived from CPython Lib/test/test_float.py (PSF License, docs/reference/SOURCES.md).
# Exercises abs, min, max, and sum on floats.
print(abs(-3.5))
print(abs(3.5))
print(max(1.5, 2.5, 0.5))
print(min(1.5, 2.5, 0.5))
xs: list[float] = [1.5, 2.5, 3.0]
print(sum(xs))
print(max(xs))
print(min(xs))
print(pow(2.0, 0.5))
