# A chained comparison evaluates its middle operand once into a scratch slot.
a: int = 1
b: int = 2
c: int = 3
print(str(a < b < c))
