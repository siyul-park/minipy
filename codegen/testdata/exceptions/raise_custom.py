# A user-defined exception is constructed with its own struct layout, not the
# shared two-field base layout.
class MyError(Exception):
    pass

try:
    raise MyError("boom")
except MyError as e:
    print("caught")
