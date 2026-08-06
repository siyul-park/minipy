# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises clear() and reuse of the cleared list.
xs: list[int] = [1, 2, 3]
xs.clear()
print(xs)
print(len(xs))
xs.append(9)
print(xs)
