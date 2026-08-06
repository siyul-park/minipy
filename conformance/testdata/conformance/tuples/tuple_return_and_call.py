# Derived from CPython Lib/test/test_tuple.py (PSF License, docs/reference/SOURCES.md).
# Exercises functions that return a bare tuple and callers unpacking it.
def pair() -> tuple[int, int]:
    return 3, 4

x, y = pair()
print(x)
print(y)

def minmax(values: list[int]) -> tuple[int, int]:
    return min(values), max(values)

lo, hi = minmax([5, 2, 9, 1, 7])
print(lo)
print(hi)

def divmod_like(a: int, b: int) -> tuple[int, int]:
    return a // b, a % b

q, r = divmod_like(17, 5)
print(q)
print(r)
