# Derived from docs/benchmarks.md, "Algorithm changes forced by real minipy bugs" (finding 1).
# Regression: a long-lived `while` loop that rewrites the same list in place,
# reading elements it wrote in an earlier iteration. docs/benchmarks.md records
# a stale read after 50-90 iterations of this shape, which corrupted the
# fannkuch-redux permutation state; the benchmark corpus routes around it with
# a recursive formulation, so nothing executed the shape itself.
n: int = 6
perm1: list[int] = []
count: list[int] = []
i: int = 0
while i < n:
    perm1.append(i)
    count.append(0)
    i += 1

perm: list[int] = []
i = 0
while i < n:
    perm.append(0)
    i += 1

maxflips: int = 0
checksum: int = 0
permcount: int = 0
r: int = n

while True:
    while r != 1:
        count[r - 1] = r
        r -= 1
    i = 0
    while i < n:
        perm[i] = perm1[i]
        i += 1
    flips: int = 0
    k: int = perm[0]
    while k != 0:
        j: int = 0
        while j < (k + 1) // 2:
            t: int = perm[j]
            perm[j] = perm[k - j]
            perm[k - j] = t
            j += 1
        flips += 1
        k = perm[0]
    if flips > maxflips:
        maxflips = flips
    if permcount % 2 == 0:
        checksum += flips
    else:
        checksum -= flips
    permcount += 1

    done: bool = False
    while True:
        if r == n:
            done = True
            break
        p0: int = perm1[0]
        i = 0
        while i < r:
            perm1[i] = perm1[i + 1]
            i += 1
        perm1[r] = p0
        count[r] -= 1
        if count[r] > 0:
            break
        r += 1
    if done:
        break

print(str(permcount))
print(str(checksum))
print(str(maxflips))
