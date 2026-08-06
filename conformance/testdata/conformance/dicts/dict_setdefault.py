# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises setdefault() for both a missing and an existing key.
d: dict[str, int] = {"a": 1}
d.setdefault("b", 2)
print(sorted(d.keys()))
print(d["b"])
d.setdefault("a", 999)
print(d["a"])
result: int = d.setdefault("c", 3)
print(result)
print(d["c"])
