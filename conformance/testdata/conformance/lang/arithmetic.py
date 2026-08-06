# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises integer/float arithmetic operator precedence and mixed-type results.
a: int = 7
b: int = 3
print(f"{a + b} {a - b} {a * b}")
print(f"{a // b} {a % b}")
c: float = 2.5
d: float = 4.0
print(f"{c + d} {d / c}")
print(f"{a ** 2}")
print(f"{-a}")
print(f"{(a + b) * 2 - b}")
