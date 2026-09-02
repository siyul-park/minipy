# A union parameter called with one concrete type per call site produces
# monomorphic clones, so the type test disappears from the specialized bodies.
def render(value: int | str) -> str:
    if isinstance(value, int):
        return str(value)
    return value

print(render(1))
print(render("x"))
