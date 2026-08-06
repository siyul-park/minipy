# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises is/is not identity checks versus ==/!= equality checks.
x: int | None = None
print(x is None)
print(x is not None)

y: int | None = 5
print(y is None)
print(y == None)

a: list[int] = [1, 2, 3]
b: list[int] = [1, 2, 3]
print(a == b)
print(a is b)
print(a is a)

c: list[int] = a
print(a is c)
c.append(4)
print(a)
