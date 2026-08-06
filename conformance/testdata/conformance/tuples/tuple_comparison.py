# Derived from CPython Lib/test/test_tuple.py (PSF License, docs/reference/SOURCES.md).
# Exercises tuple equality and lexicographic ordering comparisons.
t1: tuple[int, int] = (1, 2)
t2: tuple[int, int] = (1, 2)
t3: tuple[int, int] = (1, 3)
t4: tuple[int, int] = (0, 9)
print(t1 == t2)
print(t1 == t3)
print(t1 != t3)
print(t1 < t3)
print(t3 > t1)
print(t4 < t1)
print(t1 <= t2)
print(t1 >= t2)
