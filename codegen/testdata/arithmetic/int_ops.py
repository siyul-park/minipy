# Integer arithmetic on statically typed locals: the shape every numeric
# expression lowers through, with no boxing and no host call.
a: int = 7
b: int = 3
print(str(a + b))
print(str(a - b))
print(str(a * b))
print(str(a // b))
print(str(a % b))
