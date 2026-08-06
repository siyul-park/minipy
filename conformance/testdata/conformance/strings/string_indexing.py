# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises indexing, iteration, len(), and IndexError on out-of-range access.
s: str = "hello"
print(len(s))
print(s[0])
print(s[4])
for ch in s:
    print(ch)
try:
    print(s[10])
except IndexError:
    print("index error")
print(len(""))
