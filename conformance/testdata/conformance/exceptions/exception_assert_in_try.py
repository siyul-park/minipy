# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises a failing assert caught by its own except clause, by a base
# Exception clause, and propagating out of a function into the caller's try.
try:
    assert 1 == 2
except AssertionError:
    print("caught bare")

try:
    assert 1 == 2, "boom"
except AssertionError:
    print("caught with message")

try:
    assert 1 == 2, "also boom"
except Exception:
    print("caught as base")

def check(n: int) -> None:
    assert n > 0, "must be positive"

try:
    check(-5)
except AssertionError:
    print("caught from function")

try:
    assert True
    print("passed silently")
except AssertionError:
    print("unreachable")
