# Derived from CPython Lib/test/test_slice.py (PSF License, docs/reference/SOURCES.md).
# Exercises slicing with negative start/stop bounds.
xs: list[int] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]
print(xs[-3:])
print(xs[:-3])
print(xs[-7:-3])
print(xs[-100:3])
print(xs[3:-100])
print(xs[-1:-5:-1])
