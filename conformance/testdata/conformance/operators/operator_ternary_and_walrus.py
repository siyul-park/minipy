# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises conditional expressions and the walrus (named expression) operator.
n: int = 7
print("even" if n % 2 == 0 else "odd")
print(1 if True else 2)

values: list[int] = [1, 2, 3, 4, 5]
big: list[int] = [v for v in values if v > 2]
print(big)

if (total := sum(values)) > 10:
    print(f"big total {total}")
else:
    print(f"small total {total}")

data: list[int] = [4, 9, 16]
roots: list[float] = [r for v in data if (r := v ** 0.5) > 0]
print(roots)
