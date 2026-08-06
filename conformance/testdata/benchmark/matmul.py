# Dense matrix multiply (naive triple loop, flat row-major arrays): exercises
# float multiply-accumulate over index-computed offsets. 200x200 matrices run
# in ~2.3s under CPython 3.13.
#
# Algorithm note: the natural `i, k, j` loop order accumulates directly into
# `out[i*n+j]` across the k loop (read-modify-write the same list slot many
# times per cell while other cells are also being written in between). That
# formulation was tried here first and triggers a minipy correctness bug: for
# n as small as 6 (216 total multiply-adds), exactly one output cell comes
# out numerically wrong (confirmed against CPython 3.13; see the benchmark
# report for the isolated repro and both outputs). This file instead uses
# `i, j, k` order, accumulating each cell into a fresh local `s` and writing
# `out[i*n+j]` exactly once, which does not trigger the bug and is the
# standard formulation anyway.
#
# -O3 note: even in this safe `i, j, k` form, `-O 3` (GVN/CSE) miscompiles
# the `a[i*n+k]` / `b[k*n+j]` index arithmetic and crashes with "index out of
# range" at runtime, reproducible from n as small as 10. `-O 0` runs and
# matches CPython exactly. The benchmark runner reports this as a real
# correctness FAIL for minipy -O3 on this program rather than skipping it;
# see the benchmark report.
#
# Annotation note: matmul is not self-recursive, but its `list[float]`
# parameters are still annotated: an unannotated parameter with no default
# infers to `Any`, and `Any` does not support the arithmetic and indexing
# this function needs.
def matmul(n: int, a: list[float], b: list[float], out: list[float]) -> None:
    i = 0
    while i < n:
        j = 0
        while j < n:
            s = 0.0
            k = 0
            while k < n:
                s = s + a[i * n + k] * b[k * n + j]
                k = k + 1
            out[i * n + j] = s
            j = j + 1
        i = i + 1


def main():
    n = 200
    a = [0.0] * (n * n)
    b = [0.0] * (n * n)
    out = [0.0] * (n * n)

    i = 0
    while i < n:
        j = 0
        while j < n:
            a[i * n + j] = float((i * 7 + j * 3) % 13) - 6.0
            b[i * n + j] = float((i * 5 + j * 11) % 17) - 8.0
            j = j + 1
        i = i + 1

    matmul(n, a, b, out)

    checksum = 0.0
    idx = 0
    while idx < n * n:
        checksum = checksum + out[idx]
        idx = idx + 1

    print(f"{checksum:.6f}")


main()
