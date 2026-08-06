# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises explicit exception chaining with "raise ... from ...".
def convert(value: str) -> int:
    try:
        return int(value)
    except ValueError as e:
        raise RuntimeError("conversion failed") from e

try:
    convert("not a number")
except RuntimeError:
    print("chained exception caught")

try:
    raise ValueError("outer") from ValueError("inner")
except ValueError:
    print("caught with explicit cause")

def guarded(n: int) -> str:
    try:
        if n == 0:
            raise ZeroDivisionError("manual")
        return str(100 // n)
    except ZeroDivisionError:
        raise RuntimeError("guarded failure") from None

try:
    guarded(0)
except RuntimeError:
    print("caught with suppressed cause")
print(guarded(4))
