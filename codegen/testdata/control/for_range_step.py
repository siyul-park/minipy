# A negative literal step picks the loop's comparison at compile time, which is
# why only a literal step takes the counter loop. A computed step keeps the
# iterator, whose direction is only known at run time.
total: int = 0
for i in range(10, 0, -3):
    total += i
step: int = 2
for j in range(0, 6, step):
    total += j
print(str(total))
