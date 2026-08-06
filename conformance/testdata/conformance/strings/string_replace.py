# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises replace(), including the optional count argument.
s: str = "banana"
print(s.replace("a", "o"))
print(s.replace("a", "o", 1))
print(s.replace("a", "o", 2))
print(s.replace("nana", "NANA"))
print(s.replace("z", "q"))
print("aaaa".replace("a", "bb"))
