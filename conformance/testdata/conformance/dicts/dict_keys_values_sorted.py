# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises keys()/values(), sorted for deterministic order (dict iteration
# order is unspecified in minipy; see docs/spec/02-types.md#iteration-order).
d: dict[str, int] = {"z": 26, "a": 1, "m": 13}
print(sorted(d.keys()))
print(sorted(d.values()))
total: int = 0
for k in sorted(d.keys()):
    total = total + d[k]
print(total)
