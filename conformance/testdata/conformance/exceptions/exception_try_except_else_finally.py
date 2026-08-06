# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises try/except/else/finally execution order for both the success and
# failure paths.
def safe_divide(a: int, b: int) -> None:
    try:
        result: int = a // b
    except ZeroDivisionError:
        print("except")
    else:
        print(f"else {result}")
    finally:
        print("finally")

safe_divide(10, 2)
safe_divide(10, 0)
