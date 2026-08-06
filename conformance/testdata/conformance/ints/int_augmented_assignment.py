# Derived from CPython Lib/test/test_augassign.py (PSF License, docs/reference/SOURCES.md).
# Exercises augmented assignment operators on an int binding.
n: int = 5
n += 1
print(n)
n -= 2
print(n)
n *= 3
print(n)
n //= 2
print(n)
n **= 2
print(n)
n %= 5
print(n)
n <<= 2
print(n)
n >>= 1
print(n)
n &= 6
print(n)
n |= 1
print(n)
n ^= 3
print(n)
