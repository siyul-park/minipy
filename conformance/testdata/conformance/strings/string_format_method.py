# Derived from CPython Lib/test/test_format.py (PSF License, docs/reference/SOURCES.md).
# Exercises str.format() with automatic and explicit positional fields.
print("{} and {}".format("a", "b"))
print("{1} and {0}".format("first", "second"))
print("{0}-{0}-{1}".format("x", "y"))
print("no placeholders".format())
print("{}{}{}".format(1, 2, 3))
