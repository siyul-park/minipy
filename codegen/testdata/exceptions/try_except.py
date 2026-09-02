# A try region declares a handler table entry, and a frame holding one opts out
# of scratch-slot pooling.
def safe_div(a: int, b: int) -> int:
    try:
        return a // b
    except ZeroDivisionError:
        return 0

print(str(safe_div(10, 2)))
print(str(safe_div(10, 0)))
