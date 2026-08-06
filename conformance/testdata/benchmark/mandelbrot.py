# Mandelbrot set membership (escape-time count summed over a pixel grid):
# exercises float arithmetic in a tight per-pixel loop with an early-return
# escape check. 750x750 grid, max 100 iterations per point, runs in ~1.9s
# under CPython 3.13.
#
# Annotation note: escape_count is not self-recursive, but its parameters
# are still annotated: an unannotated parameter with no default infers to
# `Any`, and `Any` does not support the float arithmetic these functions
# need.
def escape_count(cr: float, ci: float, max_iter: int) -> int:
    zr = 0.0
    zi = 0.0
    i = 0
    while i < max_iter:
        zr2 = zr * zr
        zi2 = zi * zi
        if zr2 + zi2 > 4.0:
            return i
        new_zr = zr2 - zi2 + cr
        new_zi = 2.0 * zr * zi + ci
        zr = new_zr
        zi = new_zi
        i = i + 1
    return max_iter


def main():
    width = 750
    height = 750
    max_iter = 100
    x_min = -2.0
    x_max = 1.0
    y_min = -1.5
    y_max = 1.5

    total = 0
    py = 0
    while py < height:
        cy = y_min + (y_max - y_min) * float(py) / float(height - 1)
        px = 0
        while px < width:
            cx = x_min + (x_max - x_min) * float(px) / float(width - 1)
            total = total + escape_count(cx, cy, max_iter)
            px = px + 1
        py = py + 1

    print(str(total))


main()
