# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises "in"/"not in" over list[tuple[...]] and list[list[...]], where
# CPython compares elements structurally (by value via ==) rather than by
# identity.
pts: list[tuple[int, int]] = [(0, 0), (1, 1)]
a: tuple[int, int] = (1, 1)
print(a in pts)
print(a == pts[1])
b: tuple[int, int] = (5, 5)
print(b in pts)
print(b not in pts)

lls: list[list[int]] = [[1, 2], [3, 4]]
q: list[int] = [3, 4]
print(q in lls)
print([9, 9] in lls)
