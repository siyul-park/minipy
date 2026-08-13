# Derived from CPython Lib/test/test_sort.py (PSF License, docs/reference/SOURCES.md).
# Exercises sorted() key/reverse keywords and preserves the input list.
xs: list[int] = [3, 1, 2]
print(sorted(xs, reverse=True))
print(sorted(xs, key=lambda n: -n))
print(sorted(xs, key=lambda n: n, reverse=True))
print(xs)

words: list[str] = ["bb", "a", "cc", "d"]
print(sorted(words, key=lambda s: len(s), reverse=True))
