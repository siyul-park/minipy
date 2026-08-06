# Derived from CPython Lib/test/test_fstring.py (PSF License, docs/reference/SOURCES.md).
# Exercises richer expressions inside f-string replacement fields.
def square(x: int) -> int:
    return x * x

a: int = 3
b: int = 4
print(f"{square(a) + square(b)}")
print(f"{a if a > b else b}")
print(f"{[v * 2 for v in range(4)]}")
cond: bool = True
print(f"{'yes' if cond else 'no'}")
print(f"{a == b}")
print(f"{(a + b) * 2}")
print(f"debug: {a=} {b=}")
