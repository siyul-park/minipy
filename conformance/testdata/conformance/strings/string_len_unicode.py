# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises len() on strings with multi-byte UTF-8 codepoints: len() counts
# codepoints, not encoded bytes, and must agree with iteration and indexing,
# which are already codepoint-based.
s: str = "héllo"
print(len(s))
for ch in s:
    print(ch)
print(s[1])
print(s[0:3])

wide: str = "\U0001F389party"
print(len(wide))
print(wide[0])

print(len(""))
print(len("plain"))
