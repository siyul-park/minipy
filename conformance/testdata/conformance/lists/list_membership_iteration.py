# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises membership testing and enumerate()/zip() iteration.
xs: list[int] = [10, 20, 30]
print(20 in xs)
print(99 in xs)
print(99 not in xs)

for i, v in enumerate(xs):
    print(f"{i}:{v}")

ys: list[str] = ["a", "b", "c"]
for j, w in enumerate(ys):
    print(f"{j}:{w}")

for a, b in zip(xs, ys):
    print(f"{a}-{b}")
