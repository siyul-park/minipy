# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises pop() with and without a default, and pop()'s KeyError.
d: dict[str, int] = {"a": 1, "b": 2, "c": 3}
removed: int = d.pop("b")
print(removed)
print(len(d))
print(sorted(d.keys()))
print(d.pop("zz", -1))
print(len(d))
try:
    d.pop("zz")
except KeyError:
    print("keyerror")
