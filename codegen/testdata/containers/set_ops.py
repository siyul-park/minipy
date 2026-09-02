# A set is a map from element to bool; membership is a direct map lookup.
s: set[int] = {1, 2, 3}
s.add(4)
print(str(2 in s))
print(str(len(s)))
