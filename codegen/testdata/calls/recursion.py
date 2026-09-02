# Recursion is what makes scratch temporaries frame locals rather than globals.
def fib(n: int) -> int:
    if n < 2:
        return n
    return fib(n - 1) + fib(n - 2)

print(str(fib(10)))
