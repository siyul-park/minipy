# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises lexicographic string comparison operators.
print("apple" < "banana")
print("banana" < "apple")
print("apple" <= "apple")
print("Apple" < "apple")
print("abc" > "ab")
print("abc" == "abc")
print("abc" != "abd")
print("" < "a")
print("abc" >= "abc")
