# Derived from CPython Lib/test/test_grammar.py (PSF License, docs/reference/SOURCES.md).
# Exercises and/or/not, including short-circuit evaluation observed via a
# side-effecting helper.
calls: list[str] = []

def record(name: str, value: bool) -> bool:
    calls.append(name)
    return value

print(record("a", True) and record("b", False))
print(calls)
calls.clear()

print(record("c", False) and record("d", True))
print(calls)
calls.clear()

print(record("e", True) or record("f", True))
print(calls)
calls.clear()

print(record("g", False) or record("h", True))
print(calls)

print(not True)
print(not False)
print(not (1 == 1))
