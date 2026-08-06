# Derived from CPython Lib/test/test_set.py (PSF License, docs/reference/SOURCES.md).
# Exercises clear() and pop(). pop() removes an arbitrary element, so only
# the resulting length and membership are checked, not which element came out.
s: set[int] = {1, 2, 3}
removed: int = s.pop()
print(len(s))
print(removed in [1, 2, 3])
print(removed in s)

t: set[int] = {10, 20}
t.clear()
print(len(t))

empty_after_pop: set[int] = {5}
empty_after_pop.pop()
print(len(empty_after_pop))
try:
    empty_after_pop.pop()
except KeyError:
    print("pop from empty set caught")
