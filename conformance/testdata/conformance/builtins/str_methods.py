# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises common string methods.
s: str = "  Hello, World  "
print(s.strip())
print(s.strip().upper())
print(s.strip().lower())
print(str(s.strip().startswith("Hello")))
print(str(s.strip().endswith("World")))
print(s.strip().replace("World", "minipy"))
print("-".join(["a", "b", "c"]))
print(str("a,b,c".split(",")))
print(str(len("hello")))
print(str("hello".find("l")))
print("hello"[1:4])
