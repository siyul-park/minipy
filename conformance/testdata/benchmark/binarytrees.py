# Binary trees (build-and-checksum many short-lived trees plus one long-lived
# tree, benchmarks-game style): exercises class allocation, recursive
# construction/traversal, and `Node | None` narrowing. min depth 4, max
# depth 13, runs in ~1.0s under CPython 3.13.
#
# Annotation note: item_check and bottom_up_tree are self-recursive, so they
# need explicit return annotations (`-> int` and `-> Node`) for the same
# reason fib.py needs `-> int`. Node's fields (`left`, `right`) are declared
# with the forward-reference string annotation `"Node | None"` because the
# class type does not exist yet at the point its own fields are declared;
# see docs/spec/02-types.md's Annotation Syntax section.
class Node:
    item: int
    left: "Node | None"
    right: "Node | None"

    def __init__(self, item: int, left: "Node | None", right: "Node | None") -> None:
        self.item = item
        self.left = left
        self.right = right


def item_check(t: "Node | None") -> int:
    if t is None:
        return 0
    if t.left is None:
        return t.item
    return t.item + item_check(t.left) - item_check(t.right)


def bottom_up_tree(item: int, depth: int) -> Node:
    if depth > 0:
        return Node(item, bottom_up_tree(2 * item - 1, depth - 1), bottom_up_tree(2 * item, depth - 1))
    return Node(item, None, None)


def main():
    min_depth = 4
    max_depth = 13
    stretch_depth = max_depth + 1

    stretch_tree = bottom_up_tree(0, stretch_depth)
    checksum = item_check(stretch_tree)

    long_lived_tree = bottom_up_tree(0, max_depth)

    depth = min_depth
    while depth <= max_depth:
        iterations = 1
        shift = 0
        while shift < (max_depth - depth + min_depth):
            iterations = iterations * 2
            shift = shift + 1

        acc = 0
        i = 1
        while i <= iterations:
            t1 = bottom_up_tree(i, depth)
            acc = acc + item_check(t1)
            t2 = bottom_up_tree(0 - i, depth)
            acc = acc + item_check(t2)
            i = i + 1
        checksum = checksum + acc
        depth = depth + 2

    checksum = checksum + item_check(long_lived_tree)
    print(str(checksum))


main()
