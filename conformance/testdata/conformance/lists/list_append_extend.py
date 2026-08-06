# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises append, extend, and += mutation.
xs: list[int] = [1, 2]
xs.append(3)
print(xs)
xs.extend([4, 5])
print(xs)
xs += [6]
print(xs)
xs.extend(xs)
print(xs)
print(len(xs))
