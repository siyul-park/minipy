# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises ZeroDivisionError raised by real division, floor division, and
# modulo operations.
try:
    x: int = 1 // 0
except ZeroDivisionError:
    print("floordiv caught")

try:
    y: int = 1 % 0
except ZeroDivisionError:
    print("mod caught")

try:
    z: float = 1.0 / 0.0
except ZeroDivisionError:
    print("truediv caught")

def safe_div(a: int, b: int) -> int:
    try:
        return a // b
    except ZeroDivisionError:
        return 0

print(safe_div(10, 2))
print(safe_div(10, 0))
