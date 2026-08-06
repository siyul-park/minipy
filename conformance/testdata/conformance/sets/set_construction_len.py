# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises set literal construction, deduplication, and len(). Sets are
# unordered in minipy, so members are collected into a sorted list before
# printing (see docs/spec/02-types.md#iteration-order).
s: set[int] = {1, 2, 3, 2, 1}
print(len(s))
print(sorted([v for v in s]))

single: set[str] = {"only"}
print(single)
print(len(single))
