# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises non-string keys and del on a dict item.
by_id: dict[int, str] = {1: "one", 2: "two", 3: "three"}
print(by_id[1])
print(by_id[2])
print(len(by_id))
del by_id[2]
print(len(by_id))
print(2 in by_id)
print(sorted(by_id.keys()))

flags: dict[bool, str] = {True: "yes", False: "no"}
print(flags[True])
print(flags[False])
