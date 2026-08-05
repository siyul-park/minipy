# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises if/elif/else, while/else, and break/continue.
n: int = 7
if n % 2 == 0:
    print("even")
elif n > 5:
    print("big odd")
else:
    print("small odd")

total: int = 0
i: int = 0
while i < 10:
    if i % 2 == 0:
        i = i + 1
        continue
    if i > 7:
        break
    total = total + i
    i = i + 1
print(f"total={total}")

count: int = 0
while count < 3:
    count = count + 1
else:
    print(f"finished at {count}")
