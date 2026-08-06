# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises update() merging and copy() producing independent storage.
d: dict[str, int] = {"a": 1, "b": 2}
d.update({"b": 20, "c": 3})
print(sorted(d.keys()))
print(d["a"])
print(d["b"])
print(d["c"])

original: dict[str, int] = {"x": 1}
duplicate: dict[str, int] = original.copy()
duplicate["x"] = 99
duplicate["y"] = 2
print(original["x"])
print(len(original))
print(len(duplicate))
