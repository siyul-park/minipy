# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises unary operators and mixed int/float arithmetic promotion.
print(+5)
print(-5)
print(-(-5))
print(~5)
print(~(~5))
print(not True)
print(not not True)

a: int = 5
b: float = 2.5
print(a + b)
print(b + a)
print(a / 2)
print(a // 2)
print(a * b)
c: float = float(a)
print(c)
