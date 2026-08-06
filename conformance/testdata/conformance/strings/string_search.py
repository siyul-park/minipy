# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises find(), count(), startswith(), endswith(), and membership.
s: str = "the quick brown fox jumps over the lazy dog"
print(s.find("fox"))
print(s.find("cat"))
print(s.count("the"))
print(s.count("o"))
print(s.startswith("the"))
print(s.startswith("fox"))
print(s.endswith("dog"))
print(s.endswith("fox"))
print("quick" in s)
print("cat" not in s)
print("" in s)
