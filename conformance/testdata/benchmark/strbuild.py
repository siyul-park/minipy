# String building (concatenate many small numeral tokens, then run them
# through several str methods): exercises repeated string concatenation and
# str method calls (`upper`, `lower`, `replace`, `strip`, `count`, `find`,
# `startswith`, `endswith`). 45000 tokens runs in ~1.7s under CPython 3.13.
#
# Algorithm note: two patterns were tried here first and both hit minipy
# bugs, so this file avoids them. (1) `str.split(sep)` on a string built from
# many variable-length tokens segfaults minipy once the input is large
# enough (reproducible from as few as a few hundred tokens); this file never
# calls `split`. (2) Indexing every character of the final (very long)
# string in a `while idx < len(s): ... s[idx] ...` loop is not a bug but is
# quadratic here (string indexing is not O(1)), so per-character checksum
# work is done incrementally on each small token as it is generated, before
# concatenation, instead of by re-indexing the large joined string
# afterward. See the benchmark report for both repros.
#
# Annotation note: digits is not self-recursive, but its parameter is
# annotated: an unannotated parameter with no default infers to `Any`, and
# `Any` does not support the arithmetic this function needs.
def digits(n: int) -> str:
    if n == 0:
        return "0"
    v = n
    s = ""
    while v > 0:
        d = v % 10
        s = chr(48 + d) + s
        v = v // 10
    return s


def main():
    n = 45000
    big = ""
    token_checksum = 0
    i = 0
    while i < n:
        tok = digits(i * 2654435761 % 99999)
        j = 0
        while j < len(tok):
            token_checksum = token_checksum + ord(tok[j]) * (j + 1)
            j = j + 1
        token_checksum = token_checksum % 1000000007
        big = big + tok + " "
        i = i + 1

    upper = big.upper()
    lower = upper.lower()
    replaced = lower.replace("5", "#")
    trimmed = replaced.strip(" ")

    checksum = token_checksum
    checksum = checksum + len(big) + len(trimmed)
    checksum = checksum + replaced.count("#") * 7
    checksum = checksum + trimmed.find("999")
    if trimmed.startswith("0"):
        checksum = checksum + 1
    if trimmed.endswith("9"):
        checksum = checksum + 2

    print(str(checksum))


main()
