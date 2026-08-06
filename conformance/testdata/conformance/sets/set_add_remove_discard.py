# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises add(), remove(), discard(), and remove()'s KeyError on a missing
# element.
s: set[int] = {1, 2, 3}
s.add(4)
print(len(s))
s.add(2)
print(len(s))
s.discard(4)
print(len(s))
s.discard(999)
print(len(s))
s.remove(1)
print(len(s))
print(sorted([v for v in s]))
try:
    s.remove(999)
except KeyError:
    print("keyerror")
