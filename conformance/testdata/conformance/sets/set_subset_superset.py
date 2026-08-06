# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises issubset() and issuperset().
small: set[int] = {2, 3}
big: set[int] = {1, 2, 3, 4, 5}
print(small.issubset(big))
print(big.issuperset(small))
print(big.issubset(small))
print(small.issuperset(big))
same: set[int] = {2, 3}
print(small.issubset(same))
print(small.issuperset(same))
