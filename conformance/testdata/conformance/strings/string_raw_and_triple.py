# Derived from CPython Lib/test/test_str.py (PSF License, docs/reference/SOURCES.md).
# Exercises raw string literals, triple-quoted strings, and escape sequences.
raw: str = r"no\nescape\there"
print(raw)
triple: str = """line one
line two
line three"""
print(triple)
escaped: str = "tab\tnewline\nbackslash\\quote\""
print(escaped)
single: str = 'single-quoted with "double" inside'
print(single)
print(len(triple.split("\n")))
