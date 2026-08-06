# Spectral norm (power iteration on the implicit Hilbert-like matrix
# A(i,j) = 1/((i+j)(i+j+1)/2 + i+1)): exercises float division, math.sqrt,
# and nested O(n^2) index loops. n=400, 10 power-iteration rounds, runs in
# ~1.8s under CPython 3.13.
#
# Annotation note: none of these functions are self-recursive, but every
# parameter is annotated: an unannotated parameter with no default infers to
# `Any`, and `Any` does not support the float arithmetic and list indexing
# these functions need.
import math


def eval_a(i: int, j: int) -> float:
    return 1.0 / float((i + j) * (i + j + 1) // 2 + i + 1)


def eval_a_times_u(n: int, u: list[float], out: list[float]) -> None:
    i = 0
    while i < n:
        s = 0.0
        j = 0
        while j < n:
            s = s + eval_a(i, j) * u[j]
            j = j + 1
        out[i] = s
        i = i + 1


def eval_at_times_u(n: int, u: list[float], out: list[float]) -> None:
    i = 0
    while i < n:
        s = 0.0
        j = 0
        while j < n:
            s = s + eval_a(j, i) * u[j]
            j = j + 1
        out[i] = s
        i = i + 1


def eval_ata_times_u(n: int, u: list[float], out: list[float], tmp: list[float]) -> None:
    eval_a_times_u(n, u, tmp)
    eval_at_times_u(n, tmp, out)


def main():
    n = 400
    u = [1.0] * n
    v = [0.0] * n
    tmp = [0.0] * n

    it = 0
    while it < 10:
        eval_ata_times_u(n, u, v, tmp)
        eval_ata_times_u(n, v, u, tmp)
        it = it + 1

    vbv = 0.0
    vv = 0.0
    i = 0
    while i < n:
        vbv = vbv + u[i] * v[i]
        vv = vv + v[i] * v[i]
        i = i + 1

    result = math.sqrt(vbv / vv)
    print(f"{result:.9f}")


main()
