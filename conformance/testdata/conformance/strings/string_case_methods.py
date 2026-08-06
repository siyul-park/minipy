# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises case-transforming string methods.
s: str = "Hello World"
print(s.upper())
print(s.lower())
print("hello world".capitalize())
print("hello world".title())
print(s.swapcase())
print("ALREADY UPPER".upper())
print("already lower".lower())
