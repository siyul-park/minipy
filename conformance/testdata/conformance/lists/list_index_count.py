# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises index() and count(), including a missing-value ValueError.
xs: list[int] = [1, 2, 3, 2, 1, 2]
print(f"{xs.index(2)}")
print(f"{xs.count(2)}")
print(f"{xs.count(9)}")
try:
    xs.index(9)
except ValueError:
    print("not found")
