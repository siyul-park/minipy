# Derived from CPython Lib/test/test_slice.py (PSF License, docs/reference/SOURCES.md).
# Exercises basic list slicing: start, stop, and full-copy forms.
xs: list[int] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]
print(xs[2:5])
print(xs[:4])
print(xs[6:])
print(xs[:])
print(xs[2:2])
print(xs[0:10])
