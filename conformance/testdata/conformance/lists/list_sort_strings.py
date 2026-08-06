# Derived from CPython Lib/test/test_sort.py (PSF License, docs/reference/SOURCES.md).
# Exercises sorting strings, including case sensitivity and duplicates.
words: list[str] = ["banana", "Apple", "cherry", "apple", "Banana"]
words.sort()
print(words)

sorted_words: list[str] = sorted(words)
print(sorted_words)

print(words[0])
print(words[4] == "cherry")
