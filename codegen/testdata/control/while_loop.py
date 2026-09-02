# A counted while loop, the hot shape the interpreter's fusion targets.
i: int = 0
total: int = 0
while i < 5:
    total += i
    i += 1
print(str(total))
