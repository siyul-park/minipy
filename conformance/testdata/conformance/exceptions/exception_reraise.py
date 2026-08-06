# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises a bare "raise" re-raising the active exception to an outer handler.
def reraiser() -> str:
    try:
        try:
            raise ValueError("x")
        except ValueError:
            print("inner caught")
            raise
    except ValueError:
        return "outer caught"
    return "unreached"

print(reraiser())

def passthrough() -> str:
    try:
        raise RuntimeError("boom")
    except RuntimeError:
        raise
    return "unreached"

try:
    passthrough()
except RuntimeError:
    print("propagated to caller")
