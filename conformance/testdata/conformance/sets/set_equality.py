# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises set equality: structural and order-independent.
a: set[int] = {1, 2, 3}
b: set[int] = {3, 2, 1}
c: set[int] = {1, 2, 4}
print(a == b)
print(a == c)
print(a != c)

words: set[str] = {"one", "two", "three"}
same_words: set[str] = {"three", "two", "one"}
print(words == same_words)
