# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises dispatch to the first matching except clause among several.
def classify(n: int) -> str:
    try:
        if n == 0:
            raise ValueError("zero")
        if n < 0:
            raise TypeError("negative")
        if n > 100:
            raise KeyError("too big")
        return "fine"
    except ValueError:
        return "value"
    except TypeError:
        return "type"
    except KeyError:
        return "key"

print(classify(0))
print(classify(-5))
print(classify(200))
print(classify(5))
