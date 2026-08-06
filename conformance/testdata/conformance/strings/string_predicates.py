# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises isdigit(), isalpha(), isalnum(), and isspace().
print("12345".isdigit())
print("123a5".isdigit())
print("".isdigit())
print("abcXYZ".isalpha())
print("abc123".isalpha())
print("abc123".isalnum())
print("abc 123".isalnum())
print("   ".isspace())
print(" a ".isspace())
print("\t\n".isspace())
