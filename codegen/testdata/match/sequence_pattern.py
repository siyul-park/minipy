# match lowers to a linear decision tree over one evaluated subject slot.
def classify(xs: list[int]) -> str:
    match xs:
        case []:
            return "empty"
        case [x]:
            return "one"
        case _:
            return "many"

print(classify([]))
print(classify([1]))
print(classify([1, 2]))
