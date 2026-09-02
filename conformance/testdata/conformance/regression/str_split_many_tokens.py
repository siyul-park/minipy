# Derived from docs/benchmarks.md, "Algorithm changes forced by real minipy bugs" (finding 5).
# Regression: split a string built from many variable-length tokens.
# docs/benchmarks.md records `s.split(" ")` segfaulting the VM from a few
# hundred such tokens, and the benchmark corpus never calls split for that
# reason.
built: str = ""
i: int = 0
while i < 600:
    built = built + str(i * 37 % 100000) + " "
    i += 1

tokens: list[str] = built.split(" ")
print(str(len(tokens)))
print(tokens[0])
print(tokens[1])
print(tokens[599])
