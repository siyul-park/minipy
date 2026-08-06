# Recursive Fibonacci (naive double recursion): exercises function-call
# overhead and integer arithmetic. n=36 runs in ~1.5s under CPython 3.13.
#
# Annotation note: fib needs an explicit `-> int` return annotation. minipy
# infers an unannotated return type by joining value-return branch types, and
# a self-recursive call inside the body would need that very type before it is
# known, so unannotated recursive functions fail to type-check.
def fib(n: int) -> int:
    if n < 2:
        return n
    return fib(n - 1) + fib(n - 2)


def main():
    print(str(fib(36)))


main()
