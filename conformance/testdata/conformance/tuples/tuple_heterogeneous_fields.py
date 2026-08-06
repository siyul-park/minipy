# Derived from CPython Lib/test/test_tuple.py (PSF License, docs/reference/SOURCES.md).
# Exercises heterogeneous tuples carrying mixed field types, built and
# consumed across function boundaries.
def describe(name: str, age: int, height: float) -> tuple[str, int, float]:
    return name, age, height

person: tuple[str, int, float] = describe("Ada", 30, 1.65)
print(person[0])
print(person[1])
print(person[2])

def summarize(records: list[tuple[str, int]]) -> int:
    total: int = 0
    for name, count in records:
        total = total + count
    return total

data: list[tuple[str, int]] = [("a", 1), ("b", 2), ("c", 3)]
print(summarize(data))

flag_pair: tuple[bool, int] = (True, 42)
print(flag_pair[0])
print(flag_pair[1])
