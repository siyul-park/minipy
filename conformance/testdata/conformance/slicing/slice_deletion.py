# Derived from CPython Lib/test/test_slice.py (PSF License, docs/reference/SOURCES.md).
# Exercises del on a single list index and on a contiguous slice.
xs: list[int] = [1, 2, 3, 4, 5]
del xs[0]
print(xs)
ys: list[int] = [1, 2, 3, 4, 5]
del ys[1:3]
print(ys)
zs: list[int] = [1, 2, 3, 4, 5]
del zs[:2]
print(zs)
ws: list[int] = [1, 2, 3, 4, 5]
del ws[2:]
print(ws)
