# Derived from CPython Lib/test/test_slice.py (PSF License, docs/reference/SOURCES.md).
# Exercises string slicing: bounds, negative indices, and step, mirroring
# list slicing behavior.
s: str = "0123456789"
print(s[2:5])
print(s[:4])
print(s[6:])
print(s[:])
print(s[-3:])
print(s[:-3])
print(s[::2])
print(s[::-1])
print(s[100:200])
print(s[-100:3])
