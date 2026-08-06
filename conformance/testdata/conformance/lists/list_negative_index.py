# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises negative-index reads, plain writes, augmented writes, and
# chained assignment on list[int] and list[str].
xs: list[int] = [10, 20, 30]
print(xs[-1])
print(xs[-3])
xs[-1] = 99
print(xs)
xs[-2] += 5
print(xs)
a: list[int] = [0, 0, 0]
b: list[int] = a
a[-2] = 7
print(a)
print(b)
ys: list[str] = ["a", "b", "c"]
print(ys[-2])
