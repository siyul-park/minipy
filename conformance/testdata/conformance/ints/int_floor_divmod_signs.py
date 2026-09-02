# Derived from CPython Lib/test/test_int.py (PSF License, docs/reference/SOURCES.md).
# Exercises floor division and divisor-signed remainder across every combination
# of operand signs, which is what separates Python's // and % from a truncating
# machine division.
vals: list[int] = [7, -7, 0, 1, -1, 100, -100, 9, -9]
divs: list[int] = [2, -2, 3, -3, 7, -7, 1, -1, 10, -10]
out: str = ""
for a in vals:
    for b in divs:
        out = out + str(a % b) + ":" + str(a // b) + " "
print(out)
