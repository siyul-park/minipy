# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises reverse() and copy(), including that copy() is independent storage.
xs: list[int] = [1, 2, 3, 4]
xs.reverse()
print(xs)

ys: list[int] = xs.copy()
ys.append(99)
print(xs)
print(ys)

ys[0] = -1
print(xs[0])
print(ys[0])

print([v for v in reversed(xs)])
