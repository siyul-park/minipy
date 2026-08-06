# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises dict comprehensions, including a filter clause.
squares: dict[int, int] = {i: i * i for i in range(6)}
print(sorted(squares.keys()))
values: list[int] = []
for k in sorted(squares.keys()):
    values.append(squares[k])
print(values)

evens_only: dict[int, int] = {i: i * i for i in range(10) if i % 2 == 0}
print(sorted(evens_only.keys()))

from_words: dict[str, int] = {w: len(w) for w in ["a", "bb", "ccc"]}
lengths: list[int] = []
for w in sorted(from_words.keys()):
    lengths.append(from_words[w])
print(lengths)
