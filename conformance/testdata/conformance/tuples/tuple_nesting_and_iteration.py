# Derived from CPython Lib/test/test_tuple.py (PSF License, docs/reference/SOURCES.md).
# Exercises nested tuples and unpacking a tuple target inside a for loop.
nested: tuple[tuple[int, int], int] = ((1, 2), 3)
print(nested[0][0])
print(nested[0][1])
print(nested[1])

points: list[tuple[int, str]] = [(1, "a"), (2, "b"), (3, "c")]
for i, s in points:
    print(f"{i}:{s}")

total: int = 0
for i, s in points:
    total = total + i
print(total)
