# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises a custom Exception subclass that declares its own field and a
# custom __init__ that assigns it, both on direct construction and after a
# raise/except-as round trip.
class MyError(Exception):
    msg: str
    code: int = 7
    def __init__(self, m: str) -> None:
        self.msg = m

e: MyError = MyError("x")
print(e.msg)
print(e.code)

def fail() -> None:
    raise MyError("boom")

try:
    fail()
except MyError as caught:
    print(caught.msg)
