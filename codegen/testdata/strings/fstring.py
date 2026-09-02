# An f-string lowers to string building, with each replacement field converted
# through the printable path for its static type.
name: str = "world"
count: int = 2
print(f"{name} x{count}")
