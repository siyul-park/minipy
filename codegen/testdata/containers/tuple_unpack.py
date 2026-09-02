# A tuple is a struct with fixed fields, so unpacking is field reads with no
# host helper.
pair: tuple[int, str] = (1, "x")
first, second = pair
print(str(first) + second)
