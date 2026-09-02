# A comprehension is an eager construction loop, not a generator.
xs: list[int] = [i * i for i in range(5)]
print(str(xs[4]))
