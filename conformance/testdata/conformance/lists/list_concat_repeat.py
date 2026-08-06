# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises list concatenation and repetition.
a: list[int] = [1, 2, 3]
b: list[int] = [4, 5]
print(a + b)
print(b + a)
print(a * 3)
print(3 * a)
print(a * 0)
c: list[int] = a + b
c = c + [6]
print(c)
print(len(a * 4))
