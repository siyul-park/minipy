# Derived from CPython Lib/test/test_int.py (PSF License, docs/reference/SOURCES.md).
# Exercises abs, min, max, sum, and round on ints.
print(abs(-5))
print(abs(5))
print(abs(0))
print(max(3, 7, 1, 9, 2))
print(min(3, 7, 1, 9, 2))
print(max(-3, -7, -1))
print(sum([1, 2, 3, 4, 5]))
empty: list[int] = []
print(sum(empty))
print(round(2.5))
print(round(3.5))
print(round(-2.5))
print(round(2.4))
