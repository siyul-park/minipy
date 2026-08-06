# Derived from CPython Lib/test/test_slice.py (PSF License, docs/reference/SOURCES.md).
# Exercises slice bound clamping: out-of-range and inverted bounds never raise,
# they just produce an empty or clamped result.
xs: list[int] = [0, 1, 2, 3, 4]
print(xs[100:200])
print(xs[-100:3])
print(xs[3:-100])
print(xs[5:2])
print(xs[-100:100])
print(xs[2:1000])
