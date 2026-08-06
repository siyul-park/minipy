# Derived from CPython Lib/test/test_fstring.py and test_format.py (PSF License,
# docs/reference/SOURCES.md).
# Exercises f-string format specs: width, alignment, sign, precision, and
# numeric presentation types.
n: int = 42
print(f"{n:6d}")
print(f"{n:<6d}|")
print(f"{n:>6d}|")
print(f"{n:^6d}|")
print(f"{n:06d}")
print(f"{n:+d}")
print(f"{-n:+d}")
pi: float = 3.14159
print(f"{pi:.2f}")
print(f"{pi:10.3f}")
print(f"{255:x}")
print(f"{255:X}")
print(f"{8:o}")
print(f"{5:b}")
print(f"{0.5:.0%}")
width: int = 8
precision: int = 3
print(f"{pi:{width}.{precision}f}")
