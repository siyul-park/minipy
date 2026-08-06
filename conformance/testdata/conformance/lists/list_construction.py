# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises list literal construction, indexing, and len.
xs: list[int] = [10, 20, 30, 40, 50]
print(f"{len(xs)}")
print(f"{xs[0]} {xs[4]}")

empty: list[int] = []
print(f"{len(empty)}")

single: list[str] = ["only"]
print(f"{len(single)} {single[0]}")

nested: list[list[int]] = [[1, 2], [3, 4], [5, 6]]
print(f"{nested[0][1]} {nested[2][0]}")
print(f"{len(nested)}")
