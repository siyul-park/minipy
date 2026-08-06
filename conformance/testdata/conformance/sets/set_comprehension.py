# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises set comprehensions, including deduplication and a filter clause.
squares: set[int] = {v * v for v in range(-3, 4)}
print(sorted([v for v in squares]))
print(len(squares))

evens: set[int] = {v for v in range(20) if v % 2 == 0}
print(sorted([v for v in evens]))

from_words: set[int] = {len(w) for w in ["a", "bb", "cc", "ddd"]}
print(sorted([v for v in from_words]))
