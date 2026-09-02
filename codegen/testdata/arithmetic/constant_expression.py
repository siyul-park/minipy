# Literal-only arithmetic. Nothing folds it at the default level; the optimizer
# levels are what fold it, which is why this case exists.
print(str(2 + 3 * 4))
