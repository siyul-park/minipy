# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises list slicing: start/stop/step, negative indices, and reversal.
xs: list[int] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]
print(xs[2:5])
print(xs[:3])
print(xs[7:])
print(xs[:])
print(xs[::2])
print(xs[1::2])
print(xs[::-1])
print(xs[-3:])
print(xs[:-3])
print(xs[2:8:2])
print(xs[8:2:-2])
