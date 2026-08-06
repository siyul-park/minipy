# Derived from CPython Lib/test/test_funcattrs.py (PSF License, docs/reference/SOURCES.md).
# Exercises function definitions, recursion, and default/keyword parameters.
from typing import Callable

def factorial(n: int) -> int:
    if n <= 1:
        return 1
    return n * factorial(n - 1)

def mix(a: int, b: int = 2, c: int = 3) -> int:
    return a * 100 + b * 10 + c

print(f"5! = {factorial(5)}")
print(f"{mix(1)} {mix(1, 4)} {mix(1, 4, 9)}")

def apply_twice(f: Callable[[int], int], x: int) -> int:
    return f(f(x))

def increment(x: int) -> int:
    return x + 1

print(f"{apply_twice(increment, 10)}")
