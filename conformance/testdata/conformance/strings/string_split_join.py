# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises split() with an explicit separator and join().
csv: str = "a,b,,c"
print(csv.split(","))
print(len(csv.split(",")))
print("one two three".split(" "))
print("  a  b\tc ".split())
print("a\nb\n\nc".split())
print("a\x1cb\x1dc\x1ed\x1fe".split())
print("a\u00a0b\u2003c".split())
print("-".join(["x", "y", "z"]))
print(",".join(["only"]))
words: list[str] = "the quick brown fox".split(" ")
print(words)
print(" ".join(words))
