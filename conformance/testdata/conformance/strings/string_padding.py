# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises center(), ljust(), rjust(), and zfill().
print(f"'{'hi'.center(10)}'")
print(f"'{'hi'.center(10, '*')}'")
print(f"'{'hi'.ljust(6)}'")
print(f"'{'hi'.ljust(6, '-')}'")
print(f"'{'hi'.rjust(6)}'")
print(f"'{'hi'.rjust(6, '-')}'")
print("42".zfill(5))
print("-42".zfill(5))
print("123456".zfill(3))
print(f"'{'wide'.center(3)}'")
