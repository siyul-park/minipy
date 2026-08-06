# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises "in"/"not in" membership across lists, dicts, and strings.
xs: list[int] = [1, 2, 3]
print(2 in xs)
print(9 in xs)
print(9 not in xs)

d: dict[str, int] = {"a": 1, "b": 2}
print("a" in d)
print("z" in d)

s: str = "hello world"
print("world" in s)
print("xyz" in s)
print("xyz" not in s)

print(3 in [1, 2, 3] and "a" in {"a": 1})
