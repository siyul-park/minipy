# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises key insertion, overwrite, and length changes.
d: dict[str, int] = {"a": 1}
print(len(d))
d["b"] = 2
print(len(d))
d["a"] = 100
print(d["a"])
print(len(d))
d["c"] = 3
d["c"] = 30
print(d["c"])
print(sorted(d.keys()))
