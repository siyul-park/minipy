# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises "in"/"not in" membership testing against a set.
s: set[int] = {10, 20, 30}
print(10 in s)
print(99 in s)
print(99 not in s)
found: int = 0
for v in [10, 15, 20, 25, 30]:
    if v in s:
        found = found + 1
print(found)
