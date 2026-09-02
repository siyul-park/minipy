# for-over-range lowers to an index loop, not to an iterator object.
total: int = 0
for i in range(5):
    total += i
print(str(total))
