# Derived from CPython Lib/test/test_fstring.py (PSF License, docs/reference/SOURCES.md).
# Exercises basic f-string interpolation of names, expressions, and calls.
name: str = "Ada"
age: int = 30
print(f"{name} is {age}")
print(f"{age + 1}")
print(f"{name.upper()}")
print(f"sum={1 + 2 + 3}")
items: list[int] = [1, 2, 3]
print(f"len={len(items)}")
print(f"{'literal text'}")
x: int = 5
y: int = 7
print(f"{x} + {y} = {x + y}")
print(f"nested {f'{x}'}")
