# N-Queens (backtracking, count all solutions): exercises recursion depth,
# list-of-bool state, and boolean logic. n=12 runs in ~1s under CPython 3.13.
#
# Annotation note: solve is self-recursive, so it needs the explicit `-> int`
# return annotation for the same reason as fib.py.
def solve(row: int, n: int, cols: list[bool], diag1: list[bool], diag2: list[bool]) -> int:
    if row == n:
        return 1
    count: int = 0
    col: int = 0
    while col < n:
        d1: int = row - col + n - 1
        d2: int = row + col
        if not cols[col] and not diag1[d1] and not diag2[d2]:
            cols[col] = True
            diag1[d1] = True
            diag2[d2] = True
            count = count + solve(row + 1, n, cols, diag1, diag2)
            cols[col] = False
            diag1[d1] = False
            diag2[d2] = False
        col = col + 1
    return count


def main():
    n: int = 12
    cols: list[bool] = [False for i in range(n)]
    diag1: list[bool] = [False for i in range(2 * n - 1)]
    diag2: list[bool] = [False for i in range(2 * n - 1)]
    print(str(solve(0, n, cols, diag1, diag2)))


main()
