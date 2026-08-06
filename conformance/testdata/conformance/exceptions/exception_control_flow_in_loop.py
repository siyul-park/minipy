# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises catching exceptions inside a loop body without aborting the loop.
numerators: list[int] = [10, 20, 30, 40]
denominators: list[int] = [2, 0, 5, 0]
results: list[int] = []
for i in range(len(numerators)):
    try:
        results.append(numerators[i] // denominators[i])
    except ZeroDivisionError:
        results.append(-1)
print(results)

successes: int = 0
failures: int = 0
for d in denominators:
    try:
        r: int = 100 // d
        successes = successes + 1
    except ZeroDivisionError:
        failures = failures + 1
print(f"{successes} {failures}")
