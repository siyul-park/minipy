# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises strip/lstrip/rstrip with and without an explicit character set.
padded: str = "   spaced out   "
print(f"'{padded.strip()}'")
print(f"'{padded.lstrip()}'")
print(f"'{padded.rstrip()}'")
print(f"'{'aaahelloaaa'.strip('a')}'")
print(f"'{'xxhelloxx'.lstrip('x')}'")
print(f"'{'xxhelloxx'.rstrip('x')}'")
print(f"'{'no-padding'.strip()}'")
