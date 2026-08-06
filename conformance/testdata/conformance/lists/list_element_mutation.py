# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises item assignment, augmented item assignment, and nested mutation.
xs: list[int] = [1, 2, 3]
xs[0] = 100
print(xs)
xs[2] = 300
print(xs)
xs[1] += 50
print(xs)

grid: list[list[int]] = [[0, 0], [0, 0]]
grid[0][1] = 9
grid[1][0] = 8
print(grid)

row: list[int] = grid[0]
row.append(5)
print(grid[0])
