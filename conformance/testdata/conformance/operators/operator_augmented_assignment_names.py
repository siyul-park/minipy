# Derived from CPython Lib/test/test_augassign.py (PSF License, docs/reference/SOURCES.md).
# Exercises augmented assignment operators applied directly to a name binding.
n: int = 10
n += 5
print(n)
n -= 3
print(n)
n *= 2
print(n)
n //= 4
print(n)
n **= 2
print(n)
n %= 5
print(n)

x: float = 2.0
x += 1.5
print(x)
x *= 2.0
print(x)
x /= 4.0
print(x)
