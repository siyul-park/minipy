# Derived from CPython Lib/test/test_listcomps.py (PSF License, docs/reference/SOURCES.md).
# Exercises nested and filtered list comprehensions over ints.
matrix: list[list[int]] = [[1, 2, 3], [4, 5, 6], [7, 8, 9]]
flat: list[int] = [v for row in matrix for v in row]
print(flat)

transposed: list[list[int]] = [[row[i] for row in matrix] for i in range(3)]
print(transposed)

evens_squared: list[int] = [v * v for v in range(20) if v % 2 == 0]
print(evens_squared)

nested_filtered: list[int] = [v for row in matrix for v in row if v % 3 == 0]
print(nested_filtered)
