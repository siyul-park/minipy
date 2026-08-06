# Derived from CPython Lib/test/test_operator.py (PSF License, docs/reference/SOURCES.md).
# Exercises the operator module's arithmetic functions.
import operator

print(operator.add(3, 4))
print(operator.sub(10, 4))
print(operator.mul(3, 4))
print(operator.truediv(7, 2))
print(operator.floordiv(7, 2))
print(operator.mod(7, 2))
print(operator.pow(2, 5))
print(operator.neg(5))
print(operator.pos(-5))
print(operator.abs(-9))
