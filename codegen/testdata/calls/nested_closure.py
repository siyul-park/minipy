# An inner function capturing an enclosing local: the local is boxed and the
# closure carries an upvalue.
def outer(base: int) -> int:
    def inner(extra: int) -> int:
        return base + extra
    return inner(5)

print(str(outer(10)))
