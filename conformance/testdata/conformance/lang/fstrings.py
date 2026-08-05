# Derived from CPython Lib/test/test_fstring.py (PSF License, docs/reference/SOURCES.md).
# Exercises f-string interpolation, format specs, and conversions.
name: str = "world"
count: int = 3
print(f"hello {name}, count={count}")
print(f"{count:03d}")
pi: float = 3.14159
print(f"{pi:.2f}")
print(f"{name!r}")
width: int = 6
precision: int = 2
value: float = 12.3456
print(f"{value:{width}.{precision}f}")
