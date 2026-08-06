# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises copy() producing independent storage.
original: set[int] = {1, 2, 3}
duplicate: set[int] = original.copy()
duplicate.add(99)
print(len(original))
print(len(duplicate))
print(99 in original)
print(99 in duplicate)
