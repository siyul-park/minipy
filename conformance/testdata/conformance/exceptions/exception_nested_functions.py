# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises an exception raised several call frames deep propagating up to a
# handler in an outer caller.
def innermost() -> None:
    raise RuntimeError("deep failure")

def middle() -> None:
    innermost()

def outer() -> str:
    try:
        middle()
        return "no error"
    except RuntimeError:
        return "caught at outer"

print(outer())

def level3() -> int:
    raise ValueError("from level3")

def level2() -> int:
    return level3()

def level1() -> int:
    return level2()

try:
    level1()
except ValueError:
    print("caught at top level")
