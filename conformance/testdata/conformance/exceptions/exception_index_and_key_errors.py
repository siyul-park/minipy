# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises IndexError from list access and KeyError from dict access.
xs: list[int] = [1, 2, 3]
try:
    v: int = xs[10]
except IndexError:
    print("index caught")

d: dict[str, int] = {"a": 1}
try:
    w: int = d["missing"]
except KeyError:
    print("key caught")

try:
    del d["missing"]
except KeyError:
    print("del key caught")

def first_or_none(xs: list[int]) -> int:
    try:
        return xs[0]
    except IndexError:
        return -1

print(first_or_none([9, 8]))
empty: list[int] = []
print(first_or_none(empty))
