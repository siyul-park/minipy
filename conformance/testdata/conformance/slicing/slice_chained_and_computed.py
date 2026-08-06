# Derived from CPython Lib/test/test_slice.py (PSF License, docs/reference/SOURCES.md).
# Exercises slicing the result of another expression: chained slices, a
# sliced sorted() result, and a slice used inside a function.
xs: list[int] = [5, 3, 1, 4, 2, 9, 8, 7, 6, 0]
top3: list[int] = sorted(xs)[-3:]
print(top3)
bottom3: list[int] = sorted(xs)[:3]
print(bottom3)
print(xs[2:8][1:3])

def middle(values: list[int]) -> list[int]:
    n: int = len(values)
    return values[1:n - 1]

print(middle([1, 2, 3, 4, 5]))
print(middle([1, 2]))

s: str = "abcdefgh"
print(s[1:-1][::2])
