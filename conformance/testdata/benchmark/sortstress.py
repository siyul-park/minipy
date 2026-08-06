# Sort stress (generate-then-sort many mid-sized lists): exercises the
# built-in `list.sort()` plus a deterministic linear-congruential generator
# in place of `random` (not available: no stdlib beyond `math`). 130 rounds
# of 13000 elements runs in ~1.0s under CPython 3.13.
#
# Annotation note: make_list is not self-recursive, but its return is
# annotated (`-> list[int]`): an unannotated return is inferred by joining
# value-return branch types, which works here, but the parameter `n: int`
# still needs its annotation since an unannotated parameter with no default
# infers to `Any`, and `Any` does not support the `[0] * n` list
# multiplication this function needs.
def make_list(n: int, seed: int) -> list[int]:
    xs = [0] * n
    s = seed
    i = 0
    while i < n:
        s = (s * 1103515245 + 12345) % 2147483648
        xs[i] = s % 1000000
        i = i + 1
    return xs


def main():
    n = 13000
    rounds = 130
    checksum = 0
    seed = 1

    r = 0
    while r < rounds:
        xs = make_list(n, seed + r)
        xs.sort()
        i = 0
        while i < n:
            checksum = checksum + xs[i] * (i % 7)
            i = i + 1
        checksum = checksum % 1000000007
        r = r + 1

    print(str(checksum))


main()
