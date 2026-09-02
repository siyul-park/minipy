# A generator expression synthesizes a lazily resumed function and yields an
# iterator handle without running the body.
total: int = 0
for value in (i * 2 for i in range(5)):
    total += value
print(str(total))
