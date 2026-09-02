# A module binding is a declared global slot; a function's binding is a frame
# local. The two lower to different opcodes for the same source syntax.
count: int = 1

def bump(step: int) -> int:
    total: int = count + step
    return total

print(str(bump(2)))
