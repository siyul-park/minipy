# Derived from CPython Lib/test/test_int.py (PSF License, docs/reference/SOURCES.md).
# Exercises chained comparisons and mixed int/float comparisons.
print(1 < 2 < 3)
print(3 < 2 < 1)
print(1 < 2 > 0)
print(1 == 1 == 1)
print(1 <= 1 <= 2)
print(5 == 5.0)
print(5 < 5.5)
print(5 > 4.9)
print(5 != 5.0)
print(3 in [1, 2, 3, 4])
print(9 not in [1, 2, 3, 4])
