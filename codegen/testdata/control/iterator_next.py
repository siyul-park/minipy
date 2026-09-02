# An explicit iterator advanced with next(). The exhausted branch ends in
# `unreachable`, which is a block terminator: minivm's DCE pass removes it, so
# the block falls through into the continuation with a shallower stack and the
# program stops verifying. See TestCodegenVerifiesAtEveryOptimizationLevel.
xs: list[int] = [1, 2]
it = iter(xs)
print(str(next(it)))
print(str(next(it)))
