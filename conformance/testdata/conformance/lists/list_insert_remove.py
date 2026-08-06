# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises insert and remove.
xs: list[int] = [1, 2, 3]
xs.insert(0, 0)
print(xs)
xs.insert(2, 99)
print(xs)
xs.insert(len(xs), 100)
print(xs)
xs.remove(99)
print(xs)
xs.remove(0)
print(xs)
xs.insert(-1, 77)
print(xs)
