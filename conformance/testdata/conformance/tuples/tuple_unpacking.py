# Derived from CPython Lib/test/test_tuple.py (PSF License, docs/reference/SOURCES.md).
# Exercises tuple unpacking assignment, the swap idiom, and starred unpack.
a: int
b: str
a, b = 1, "x"
print(a)
print(b)

p: int = 1
q: int = 2
p, q = q, p
print(p)
print(q)

first: int
rest: list[int]
first, *rest = [1, 2, 3, 4]
print(first)
print(rest)

mid: list[int]
lead: int
tail: int
lead, *mid, tail = [1, 2, 3, 4]
print(lead)
print(mid)
print(tail)
