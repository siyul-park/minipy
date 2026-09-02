# Derived from docs/benchmarks.md, "Algorithm changes forced by real minipy bugs" (finding 3).
# Regression: read-modify-write of one list slot repeatedly across an inner
# loop, with other slots written in between. docs/benchmarks.md records this
# `i, k, j` matrix-multiply order producing one numerically wrong cell from n as
# small as 6; the benchmark corpus uses the `i, j, k` order instead, so the
# shape itself went untested.
n: int = 8
a: list[int] = []
b: list[int] = []
i: int = 0
while i < n * n:
    a.append(i % 7)
    b.append(i % 5)
    i += 1

out: list[int] = []
i = 0
while i < n * n:
    out.append(0)
    i += 1

i = 0
while i < n:
    k: int = 0
    while k < n:
        aik: int = a[i * n + k]
        j: int = 0
        while j < n:
            out[i * n + j] = out[i * n + j] + aik * b[k * n + j]
            j += 1
        k += 1
    i += 1

rendered: str = ""
i = 0
while i < n * n:
    rendered = rendered + str(out[i]) + ","
    i += 1
print(rendered)
