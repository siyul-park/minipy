# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises chained comparisons, including short-circuiting on the first
# failing link.
print(1 < 2 < 3)
print(1 < 3 < 2)
print(1 < 2 <= 2 < 3)
print(3 > 2 > 1)
print(1 == 1 != 2)
print(1 < 2 == 2.0)

calls: list[int] = []

def probe(n: int) -> int:
    calls.append(n)
    return n

print(probe(1) < probe(2) < probe(0) < probe(3))
print(calls)
