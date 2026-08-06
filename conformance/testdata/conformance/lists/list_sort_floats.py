# Derived from CPython Lib/test/test_sort.py (PSF License, docs/reference/SOURCES.md).
# Exercises sorting floats, including negative values and ties.
xs: list[float] = [3.5, -1.2, 0.0, 2.2, -1.2, 100.0, -50.5]
xs.sort()
print(xs)

ys: list[float] = sorted(xs)
print(ys)

zs: list[float] = [1.0, -0.0, 0.0]
zs.sort()
print(zs)
