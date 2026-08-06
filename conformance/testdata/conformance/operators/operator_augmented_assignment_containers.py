# Derived from CPython Lib/test/test_augassign.py (PSF License, docs/reference/SOURCES.md).
# Exercises augmented assignment on list elements, dict values, and object
# attributes.
xs: list[int] = [1, 2, 3]
xs[0] += 100
xs[1] *= 10
print(xs)

d: dict[str, int] = {"a": 1, "b": 2}
d["a"] += 9
d["b"] //= 2
print(d["a"])
print(d["b"])

class Counter:
    value: int
    def __init__(self) -> None:
        self.value = 0

c: Counter = Counter()
c.value += 5
c.value += 5
c.value -= 3
print(c.value)

s: str = "foo"
s += "bar"
s *= 2
print(s)
