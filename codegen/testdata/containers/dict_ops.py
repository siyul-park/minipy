# A str-keyed dict is a content-keyed map; a keyed read raises rather than
# returning a zero value, so it goes through a host function.
d: dict[str, int] = {"a": 1, "b": 2}
d["c"] = 3
print(str(d["a"]))
print(str(len(d)))
