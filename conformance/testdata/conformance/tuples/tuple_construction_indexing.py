# Derived from CPython Lib/test/test_tuple.py (PSF License, docs/reference/SOURCES.md).
# Exercises heterogeneous tuple construction, constant indexing, and len().
t: tuple[int, str, float] = (1, "a", 2.5)
print(t[0])
print(t[1])
print(t[2])
print(len(t))

single: tuple[int] = (5,)
print(single[0])
print(len(single))

triple: tuple[int, int, int] = (10, 20, 30)
print(triple[0])
print(triple[1])
print(triple[2])
