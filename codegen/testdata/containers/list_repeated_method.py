# Two calls to the same list method on the same element type. The host-function
# count in the header is what says whether the compiler reuses one host value
# for both call sites or interns a second identical one.
xs: list[int] = [3, 1, 2]
ys: list[int] = [6, 5, 4]
xs.sort()
ys.sort()
print(str(xs[0]) + " " + str(ys[0]))
