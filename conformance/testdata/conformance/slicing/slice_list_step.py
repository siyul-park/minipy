# Derived from CPython Lib/test/test_slice.py (PSF License, docs/reference/SOURCES.md).
# Exercises slicing with an explicit step, including negative step reversal.
xs: list[int] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]
print(xs[::2])
print(xs[1::2])
print(xs[::3])
print(xs[::-1])
print(xs[::-2])
print(xs[8:2:-2])
print(xs[2:8:2])
