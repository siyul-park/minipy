# Derived from CPython Lib/test/test_listcomps.py (PSF License, docs/reference/SOURCES.md).
# Exercises list/dict comprehensions with filters and nested loops over ints.
squares: list[int] = [i * i for i in range(6)]
print(f"{squares[0]} {squares[3]} {squares[5]}")

evens: list[int] = [i for i in range(10) if i % 2 == 0]
print(f"{len(evens)}")

pairs: list[int] = [i * j for i in range(3) for j in range(3)]
print(f"{len(pairs)} {pairs[8]}")

squared_by_key: dict[str, int] = {str(i): i * i for i in range(4)}
print(f"{squared_by_key.get('3')}")

total: int = 0
for v in (i * i for i in range(5) if i > 1):
    total = total + v
print(f"{total}")
