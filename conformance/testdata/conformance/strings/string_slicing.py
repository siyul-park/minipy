# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises string slicing, including negative indices and step.
s: str = "0123456789"
print(s[2:5])
print(s[:3])
print(s[7:])
print(s[:])
print(s[::2])
print(s[1::2])
print(s[::-1])
print(s[-3:])
print(s[:-3])
print(s[2:8:2])
print(s[-1])
print(s[-5:-2])
