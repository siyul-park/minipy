# A direct call to a known function: the callee is a function constant loaded
# from a global slot, not a dynamic lookup.
def add(a: int, b: int) -> int:
    return a + b

print(str(add(1, 2)))
