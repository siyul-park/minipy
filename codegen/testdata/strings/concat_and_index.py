# String concatenation is a VM opcode; indexing needs a host helper because it
# has to normalize a negative index and decode a rune.
s: str = "hello"
print(s + " world")
print(s[0])
print(s[-1])
print(str(len(s)))
