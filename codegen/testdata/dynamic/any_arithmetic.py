# An Any-typed value keeps the dynamic reference path: every operation is a
# host call rather than a numeric opcode.
def twice(value: Any) -> Any:
    return value + value

print(str(twice(21)))
