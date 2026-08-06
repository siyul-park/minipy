# Derived from CPython Lib/test/test_float.py (PSF License, docs/reference/SOURCES.md).
# Exercises round() with banker's rounding on ties and with an explicit
# precision argument.
print(round(0.5))
print(round(1.5))
print(round(2.5))
print(round(-0.5))
print(round(-1.5))
print(round(2.4))
print(round(2.6))
print(round(3.14159, 2))
print(round(3.14159, 0))
print(round(3.14159, 4))
print(round(1234.5678, -2))
