# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises finally running on the normal path, the exception path, and a
# return-from-try path.
def normal() -> None:
    try:
        print("body")
    finally:
        print("finally-normal")

def raises_and_catches() -> None:
    try:
        raise ValueError("x")
    except ValueError:
        print("caught")
    finally:
        print("finally-exception")

def returns_from_try() -> int:
    try:
        return 7
    finally:
        print("finally-return")

normal()
raises_and_catches()
print(returns_from_try())
