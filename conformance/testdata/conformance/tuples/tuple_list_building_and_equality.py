# Derived from CPython Lib/test/test_tuple.py (PSF License, docs/reference/SOURCES.md).
# Exercises building a list of tuples incrementally and reading it back by
# index and by unpacking.
pts: list[tuple[int, int]] = []
pts.append((0, 0))
pts.append((1, 1))
pts.append((2, 4))
print(len(pts))
print(pts[0])
print(pts[2])

total_x: int = 0
total_y: int = 0
for px, py in pts:
    total_x = total_x + px
    total_y = total_y + py
print(total_x)
print(total_y)

def make_point(x: int, y: int) -> tuple[int, int]:
    return x, y

a: tuple[int, int] = make_point(3, 4)
b: tuple[int, int] = make_point(3, 4)
print(a == b)
print(a == pts[1])
