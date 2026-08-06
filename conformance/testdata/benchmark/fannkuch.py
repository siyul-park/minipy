# Fannkuch (pancake-flip permutation search): exercises recursion, in-place
# list swaps, and tight integer loops. n=9, run twice, takes ~1.5s under
# CPython 3.13.
#
# Algorithm note: the classic fannkuch-redux benchmark generates permutations
# iteratively with a single reusable count/perm1 array pair (a "next
# permutation" state machine living in one big while-loop). That formulation
# was tried here first and triggers a minipy correctness bug: a list element
# read late in a long-lived while loop observes a stale value after roughly
# 50-90 loop iterations of interleaved reads and writes to the same list,
# corrupting the permutation (confirmed reproducible independent of
# optimization level; see the benchmark report for the isolated repro and
# both outputs). This file instead generates permutations recursively
# (Heap's algorithm), swapping into a shared array across call frames rather
# than mutating it repeatedly inside one loop body, which does not trigger
# the bug. Checksum/max-flips values therefore differ from the canonical
# fannkuch-redux checksum (permutation order differs), but that is fine here:
# nothing outside this file depends on matching the upstream ordering, only
# on minipy, CPython, pypy3, and gpython agreeing with each other.
#
# Annotation note: permute is self-recursive, so it needs the explicit
# `-> tuple[int, int, int]` return annotation for the same reason fib.py
# needs `-> int`. count_flips and fannkuch are not self-recursive, but their
# `list[int]` parameters still need annotations: an unannotated parameter
# with no default infers to `Any`, and `Any` does not support the indexing
# and arithmetic these functions need.
#
# -O3 note: main() calls `fannkuch(9)` with a literal, not a `n = 9` local
# passed through. Passing the same value via a local variable instead of a
# literal made the `-O 3` GVN/CSE pass take minutes instead of seconds on
# this program (a compile-time blowup, not a runtime one); the literal call
# does not trigger it. See the benchmark report.
def count_flips(perm: list[int]) -> int:
    a = perm.copy()
    flips = 0
    k = a[0]
    while k != 0:
        i = 0
        j = k
        while i < j:
            t = a[i]
            a[i] = a[j]
            a[j] = t
            i = i + 1
            j = j - 1
        flips = flips + 1
        k = a[0]
    return flips


def permute(a: list[int], k: int, permcount: int, checksum: int, maxflips: int) -> tuple[int, int, int]:
    if k == 1:
        flips = count_flips(a)
        if flips > maxflips:
            maxflips = flips
        if permcount % 2 == 0:
            checksum = checksum + flips
        else:
            checksum = checksum - flips
        return (permcount + 1, checksum, maxflips)
    i = 0
    while i < k:
        permcount, checksum, maxflips = permute(a, k - 1, permcount, checksum, maxflips)
        if k % 2 == 0:
            tmp = a[i]
            a[i] = a[k - 1]
            a[k - 1] = tmp
        else:
            tmp = a[0]
            a[0] = a[k - 1]
            a[k - 1] = tmp
        i = i + 1
    return (permcount, checksum, maxflips)


def fannkuch(n: int) -> int:
    a = [0] * n
    i = 0
    while i < n:
        a[i] = i
        i = i + 1
    permcount, checksum, maxflips = permute(a, n, 0, 0, 0)
    return checksum * 1000 + maxflips


def main():
    total = 0
    rep = 0
    while rep < 2:
        total = total + fannkuch(9)
        rep = rep + 1
    print(str(total))


main()
