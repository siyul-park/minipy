# Derived from CPython Lib/test/test_builtin.py and Lib/test/test_sort.py
# (PSF License, docs/reference/SOURCES.md).
#
# Host builtins that rebuild a list must retain their reference-typed
# elements. A list literal passed inline is still an unrooted temporary, so
# these calls previously crashed the VM for str elements, while the same call
# through a named variable succeeded.
#
# reversed() is never printed directly: CPython returns a lazy iterator whose
# repr embeds a memory address, so its contents are compared instead.
print(sorted(["b", "a", "c"]))
print(sorted(["b", "a", "c"])[0])
print(sorted("d c".split(" ")))
print(len(sorted(["b", "a"])))
print([x for x in reversed(["a", "b", "c"])])

named: list[str] = ["b", "a", "c"]
print(sorted(named))
print([x for x in reversed(named)])

nested: list[str] = sorted(["y", "x"])
print(sorted(nested))
print(sorted([2, 1]))
print([x for x in reversed([2.0, 1.0])])
