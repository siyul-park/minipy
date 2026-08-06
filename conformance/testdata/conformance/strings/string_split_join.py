# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises split() with an explicit separator and join().
csv: str = "a,b,,c"
print(csv.split(","))
print(len(csv.split(",")))
print("one two three".split(" "))
print("-".join(["x", "y", "z"]))
print(",".join(["only"]))
words: list[str] = "the quick brown fox".split(" ")
print(words)
print(" ".join(words))
