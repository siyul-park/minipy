# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises union(), intersection(), and difference() (the & | - operators
# are not supported on sets in minipy, so the method forms are used).
a: set[int] = {1, 2, 3, 4}
b: set[int] = {3, 4, 5, 6}
print(sorted([v for v in a.union(b)]))
print(sorted([v for v in a.intersection(b)]))
print(sorted([v for v in a.difference(b)]))
print(sorted([v for v in b.difference(a)]))
print(len(a.union(b)))
print(len(a.intersection(b)))
