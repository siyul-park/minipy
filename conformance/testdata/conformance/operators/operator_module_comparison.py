# Derived from CPython Lib/test/test_operator.py (PSF License, docs/reference/SOURCES.md).
# Exercises the operator module's comparison, truth, and containment functions.
import operator

print(operator.eq(3, 3))
print(operator.ne(3, 4))
print(operator.lt(3, 4))
print(operator.le(3, 3))
print(operator.gt(4, 3))
print(operator.ge(4, 4))
print(operator.not_(True))
print(operator.not_(False))
print(operator.truth(1))
print(operator.truth(0))
print(operator.contains([1, 2, 3], 2))
print(operator.contains([1, 2, 3], 9))
