# Derived from CPython Lib/test/test_operator.py (PSF License, docs/reference/SOURCES.md).
# Exercises the operator module's bitwise functions.
import operator

print(operator.and_(5, 3))
print(operator.or_(5, 2))
print(operator.xor(5, 1))
print(operator.invert(5))
print(operator.invert(0))
print(operator.lshift(1, 4))
print(operator.rshift(256, 4))
