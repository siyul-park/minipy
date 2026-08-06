# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises dict equality (structural, order-independent) and nested containers.
a: dict[str, int] = {"x": 1, "y": 2}
b: dict[str, int] = {"y": 2, "x": 1}
c: dict[str, int] = {"x": 1, "y": 3}
print(a == b)
print(a == c)
print(a != c)

nested: dict[str, list[int]] = {"evens": [2, 4, 6], "odds": [1, 3, 5]}
print(nested["evens"])
nested["evens"].append(8)
print(nested["evens"])
print(len(nested["odds"]))

counts: dict[str, int] = {}
for ch in "banana":
    counts[ch] = counts.get(ch, 0) + 1
pairs: list[str] = []
for k in sorted(counts.keys()):
    pairs.append(f"{k}:{counts[k]}")
print(pairs)
