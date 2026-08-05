# Derived from CPython Lib/test/test_class.py (PSF License, docs/reference/SOURCES.md).
# Exercises class definitions, __init__, methods, and single inheritance.
class Point:
    x: float
    y: float

    def __init__(self, x: float, y: float) -> None:
        self.x = x
        self.y = y

    def norm(self) -> float:
        return (self.x * self.x + self.y * self.y) ** 0.5

class Point3D(Point):
    z: float

    def __init__(self, x: float, y: float, z: float) -> None:
        self.x = x
        self.y = y
        self.z = z

    def norm(self) -> float:
        return (self.x * self.x + self.y * self.y + self.z * self.z) ** 0.5

p: Point = Point(3.0, 4.0)
print(f"{p.norm()}")

q: Point3D = Point3D(1.0, 2.0, 2.0)
print(f"{q.norm()}")
q.x = 0.0
print(f"{q.x}")
