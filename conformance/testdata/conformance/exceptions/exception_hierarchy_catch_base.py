# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises catching several distinct exception types through a common base
# class handler.
def raises_value() -> None:
    raise ValueError("v")

def raises_key() -> None:
    d: dict[str, int] = {}
    x: int = d["missing"]

def raises_index() -> None:
    xs: list[int] = [1]
    y: int = xs[5]

for fn_name in ["value", "key", "index"]:
    try:
        if fn_name == "value":
            raises_value()
        elif fn_name == "key":
            raises_key()
        else:
            raises_index()
    except Exception:
        print(f"caught {fn_name}")
