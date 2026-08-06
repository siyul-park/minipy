# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises "in"/"not in" membership testing against dict keys.
d: dict[str, int] = {"a": 1, "b": 2, "c": 3}
print("a" in d)
print("zz" in d)
print("zz" not in d)
print("b" in d and "c" in d)
found: int = 0
for k in ["a", "x", "b", "y", "c"]:
    if k in d:
        found = found + 1
print(found)
