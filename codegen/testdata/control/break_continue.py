# break and continue resolve against the loop label stack.
total: int = 0
for i in range(10):
    if i == 3:
        continue
    if i == 6:
        break
    total += i
print(str(total))
