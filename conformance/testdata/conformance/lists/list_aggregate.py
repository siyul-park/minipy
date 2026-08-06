# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises min, max, sum, sorted, and reversed applied to a named list.
xs: list[int] = [4, 8, 1, 9, 3]
print(f"{min(xs)}")
print(f"{max(xs)}")
print(f"{sum(xs)}")
print(sorted(xs))
print([v for v in reversed(xs)])
print(xs)
print(f"{min(3, 7)}")
print(f"{max(3, 7, 1)}")
