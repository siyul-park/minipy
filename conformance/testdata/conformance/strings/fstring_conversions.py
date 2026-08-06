# Derived from CPython Lib/test/test_fstring.py (PSF License, docs/reference/SOURCES.md).
# Exercises f-string conversion flags !r, !s, and !a.
name: str = "world"
print(f"{name!r}")
print(f"{name!s}")
print(f"{name!a}")
n: int = 42
print(f"{n!r}")
print(f"{n!s}")
print(f"repr={name!r} str={name!s}")
