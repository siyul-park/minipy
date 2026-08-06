# Derived from CPython Lib/test/test_list.py (PSF License, docs/reference/SOURCES.md).
# Exercises pop() with no argument, a positive index, and a negative index.
xs: list[int] = [10, 20, 30, 40, 50]
last: int = xs.pop()
print(f"{last} {xs}")
first: int = xs.pop(0)
print(f"{first} {xs}")
mid: int = xs.pop(1)
print(f"{mid} {xs}")
neg: int = xs.pop(-1)
print(f"{neg} {xs}")
