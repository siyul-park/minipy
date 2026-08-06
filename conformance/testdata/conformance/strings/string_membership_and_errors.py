# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises membership testing and index/value error handling.
s: str = "the quick brown fox"
print("quick" in s)
print("slow" in s)
print("slow" not in s)
print("" in s)
try:
    n: int = int("")
except ValueError:
    print("empty int caught")
try:
    v: int = int("12.5")
except ValueError:
    print("float-string int caught")
words: list[str] = s.split(" ")
print(len(words))
print("fox" in words)
