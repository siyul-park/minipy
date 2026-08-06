# Derived from CPython Lib/test/test_slice.py (PSF License, docs/reference/SOURCES.md).
# Exercises contiguous slice assignment where the replacement length matches
# the slice length.
xs: list[int] = [1, 2, 3, 4, 5]
xs[1:3] = [99, 98]
print(xs)
xs[0:1] = [-1]
print(xs)
xs[:] = [7, 7, 7, 7, 7]
print(xs)
ys: list[int] = [1, 2, 3, 4, 5]
ys[1:4] = [10, 20, 30]
print(ys)
