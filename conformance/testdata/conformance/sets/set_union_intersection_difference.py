# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises set union, intersection, difference, and symmetric difference
# through both operators and methods.
a: set[int] = {1, 2, 3, 4}
b: set[int] = {3, 4, 5, 6}
print(sorted([v for v in a.union(b)]))
print(sorted([v for v in a.intersection(b)]))
print(sorted([v for v in a.difference(b)]))
print(sorted([v for v in b.difference(a)]))
print(sorted([v for v in (a | b)]))
print(sorted([v for v in (a & b)]))
print(sorted([v for v in (a - b)]))
print(sorted([v for v in (a ^ b)]))
print(len(a.union(b)))
print(len(a.intersection(b)))
