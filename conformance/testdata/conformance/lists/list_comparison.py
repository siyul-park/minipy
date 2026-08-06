# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises list equality and lexicographic ordering comparisons.
a: list[int] = [1, 2, 3]
b: list[int] = [1, 2, 3]
c: list[int] = [1, 2, 4]
d: list[int] = [1, 2]
print(a == b)
print(a == c)
print(a != c)
print(a < c)
print(c > a)
print(d < a)
print(a <= b)
print(a >= b)
print([1, 2, 3] == [1, 2, 3])
