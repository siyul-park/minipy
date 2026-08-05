# Derived from CPython Lib/test/test_math.py (PSF License, docs/reference/SOURCES.md).
# Exercises the math module subset minipy implements.
import math

print(f"{math.sqrt(16.0)}")
print(f"{math.pi:.4f}")
print(f"{math.fabs(-5.5)}")
print(str(math.isnan(math.nan)))
print(str(math.isinf(math.inf)))
print(f"{math.pow(2.0, 8.0)}")
print(f"{math.degrees(math.pi):.1f}")
print(f"{math.radians(180.0):.4f}")
