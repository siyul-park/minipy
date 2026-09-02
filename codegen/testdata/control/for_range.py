# for-over-range lowers to a counter loop: no iterator is allocated and no
# coroutine protocol runs per step. The test sits at the bottom so the compare
# feeds its branch directly, which is one fused handler in the interpreter.
total: int = 0
for i in range(5):
    total += i
print(str(total))
