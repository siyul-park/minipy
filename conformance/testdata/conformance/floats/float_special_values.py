# Derived from CPython Lib/test/test_float.py (PSF License, docs/reference/SOURCES.md).
# Exercises infinity and NaN via float() parsing and the math module predicates.
import math

pos_inf: float = float("inf")
neg_inf: float = float("-inf")
nan: float = float("nan")
print(pos_inf)
print(neg_inf)
print(math.isinf(pos_inf))
print(math.isinf(neg_inf))
print(math.isnan(nan))
print(nan != nan)
print(nan == nan)
print(pos_inf > 1000000.0)
print(neg_inf < -1000000.0)
