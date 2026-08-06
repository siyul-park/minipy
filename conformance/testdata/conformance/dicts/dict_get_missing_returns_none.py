# Derived from CPython Lib/test/test_dict.py (PSF License, docs/reference/SOURCES.md).
# Exercises the single-argument get() form on a missing key: CPython returns
# None rather than a value-type zero value, and `is None` narrows the result.
d: dict[str, int] = {"a": 1}
print(d.get("zz"))
print(d.get("a"))
print(d.get("zz") is None)
print(d.get("a") is None)

v = d.get("zz")
if v is None:
    print("none-branch")
else:
    print(str(v))
