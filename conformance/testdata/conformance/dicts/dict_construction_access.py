# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises dict literal construction, subscript access, and len().
d: dict[str, int] = {"a": 1, "b": 2, "c": 3}
print(d["a"])
print(d["b"])
print(d["c"])
print(len(d))

empty: dict[str, int] = {}
print(len(empty))

single: dict[str, str] = {"only": "value"}
print(single["only"])
print(len(single))
