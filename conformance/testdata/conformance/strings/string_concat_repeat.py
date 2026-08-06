# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises string concatenation, repetition, and adjacent literal joining.
a: str = "foo"
b: str = "bar"
print(a + b)
print(a * 3)
print(3 * a)
print(a * 0)
adjacent: str = "hello, " "world"
print(adjacent)
c: str = a
c += b
print(c)
print(len(a + b))
