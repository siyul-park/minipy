# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises items() rendered deterministically by sorting on the key first
# (dict/tuple iteration and comparison order is otherwise unspecified in
# minipy; see docs/spec/02-types.md#iteration-order).
d: dict[str, int] = {"banana": 2, "apple": 1, "cherry": 3}
pairs: list[str] = []
for k in sorted(d.keys()):
    pairs.append(f"{k}={d[k]}")
print(pairs)
print(len(pairs))
