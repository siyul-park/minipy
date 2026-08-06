# Derived from CPython Lib/test/test_string.py (PSF License, docs/reference/SOURCES.md).
# Exercises the string module's constant character-class strings.
import string

print(string.ascii_lowercase)
print(string.ascii_uppercase)
print(string.digits)
print(string.ascii_letters)
print(len(string.punctuation))
print("a" in string.ascii_lowercase)
print("5" in string.digits)
