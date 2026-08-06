# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises str()/int()/float() conversions in both directions, plus errors.
print(str(42))
print(str(-7))
print(str(3.14))
print(str(True))
print(str(False))
print(str(None))
print(int("42"))
print(int("  42  "))
print(int("-13"))
print(float("3.14"))
print(float("  2.5  "))
try:
    n: int = int("not a number")
except ValueError:
    print("int caught")
try:
    x: float = float("nope")
except ValueError:
    print("float caught")
