# List indexing and append lower to array opcodes; index normalization is
# emitted inline rather than called out to the host.
xs: list[int] = [1, 2, 3]
xs.append(4)
print(str(xs[0]))
print(str(xs[-1]))
print(str(len(xs)))
