# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# CPython dicts preserve insertion order (guaranteed since 3.7). minipy dicts
# do not: iteration order is unspecified, and print(d) instead renders
# entries sorted by their rendered string for deterministic output, which
# differs from insertion order once a dict has more than one entry.
# minipy-divergence: dict does not preserve insertion order; print renders entries sorted by rendered string for determinism instead of insertion order.
# minipy-divergence-doc: docs/spec/02-types.md#iteration-order
d: dict[str, int] = {"z": 1, "a": 2, "m": 3}
print(d)
