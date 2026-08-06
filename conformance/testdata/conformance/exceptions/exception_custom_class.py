# Derived from CPython Lib/test/test_exceptions.py (PSF License, docs/reference/SOURCES.md).
# Exercises defining, raising, and catching a custom exception subclass.
class ConfigError(Exception):
    pass

class ParseError(ConfigError):
    pass

def load(kind: str) -> str:
    try:
        if kind == "config":
            raise ConfigError("bad config")
        if kind == "parse":
            raise ParseError("bad parse")
        return "loaded"
    except ParseError:
        return "parse failed"
    except ConfigError:
        return "config failed"

print(load("config"))
print(load("parse"))
print(load("ok"))

try:
    raise ParseError("nested under config")
except ConfigError:
    print("caught parse error as config error")
