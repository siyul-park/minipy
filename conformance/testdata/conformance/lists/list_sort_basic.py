# Derived from CPython Lib/test/test_sort.py (PSF License, docs/reference/SOURCES.md).
# Exercises in-place ascending sort on ints, including duplicates.
xs: list[int] = [5, 3, 1, 4, 1, 5, 9, 2, 6]
xs.sort()
print(xs)

already: list[int] = [1, 2, 3, 4, 5]
already.sort()
print(already)

single: list[int] = [42]
single.sort()
print(single)

empty: list[int] = []
empty.sort()
print(empty)

descending: list[int] = [9, 8, 7, 6, 5]
descending.sort()
print(descending)
