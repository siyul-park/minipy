# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises get() with a present key and get() with a default for a missing key.
d: dict[str, int] = {"a": 1, "b": 2}
print(d.get("a"))
print(d.get("a", -1))
print(d.get("zz", -1))
print(d.get("zz", 0))
count: int = d.get("missing", 0) + 1
print(count)
