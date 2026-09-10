"""The Instant Launcher mark as geometry, shared by the SVG generator, the
OBJ writer and the Blender script.

Coordinates are Blender's (X right-front, Y depth, Z up), in centimetres.
The stack is a 5x5x5 cube of one-unit slabs and gaps by default; the bolt
stands on the front (-Y) face, the l on the right (+X) face, each one slab
deep. Every part is a prism: a flat polygon extruded along one axis, so its
faces meet edge to edge and never overlap on screen.
"""

BASE = {
    "slab_bottom": "#2E5C39",
    "slab_middle": "#3E7A4B",
    "slab_top":    "#5B8DEF",
    "bolt":        "#F5A524",
    "letter_l":    "#F2F5FA",
}


def prism(poly, axis, a0, a1):
    """Faces of a polygon extruded from a0 to a1 along `axis` (0, 1 or 2).

    The polygon's two coordinates are the other two axes in ascending order.
    Returns (main, sides, back): the face at a1 (normal +axis), the side
    faces (outward), and the face at a0 (normal -axis). Each face is a list
    of (x, y, z) tuples wound counter-clockwise seen from outside.
    """
    n = len(poly)
    area = sum(poly[i][0] * poly[(i + 1) % n][1] - poly[(i + 1) % n][0] * poly[i][1] for i in range(n))
    if area < 0:
        poly = poly[::-1]
    others = [i for i in range(3) if i != axis]

    def p3(u, v, a):
        pt = [0.0, 0.0, 0.0]
        pt[others[0]], pt[others[1]], pt[axis] = u, v, a
        return tuple(pt)

    # CCW in (u, v) is outward for the +axis face when (u, v, axis) is a
    # right-handed order, which holds for axis 2 (x,y,z) and axis 0 (y,z,x)
    # but not for axis 1 (x,z,y), so that one flips.
    flip = axis == 1
    main = [p3(u, v, a1) for u, v in poly]
    back = [p3(u, v, a0) for u, v in poly]
    if flip:
        main, back = main[::-1], back[::-1]
    sides = []
    for i in range(n):
        j = (i + 1) % n
        a, b = poly[i], poly[j]
        quad = [p3(*a, a0), p3(*b, a0), p3(*b, a1), p3(*a, a1)]
        sides.append(quad[::-1] if flip else quad)
    return main, sides, back[::-1]


def build(slab=30.0, gap=30.0):
    """Returns the parts in drawing order: (name, material, main, sides, back)."""
    pitch, side = slab + gap, 2 * (slab + gap) + slab
    parts = []
    square = [(0, 0), (150, 0), (150, 150), (0, 150)]
    for name, z0 in (("slab_bottom", 0), ("slab_middle", pitch), ("slab_top", 2 * pitch)):
        parts.append((name, name) + prism(square, 2, z0, z0 + slab))

    # the i: a bolt spanning the full height, its two flat cuts level with
    # the middle slab's edges. Polygon in (x, z), extruded along -Y.
    W = side * 3.6 / 7
    lo, hi = 1 - pitch / side, 1 - (pitch + slab) / side
    shape = [(0.80, 0.0), (0.0, lo), (0.41, lo), (0.23, 1.0), (1.0, hi), (0.59, hi)]
    s0 = (150 - W) / 2
    bolt = [(s0 + x * W, (1 - y) * side) for x, y in shape]
    main, sides, back = prism(bolt, 1, -slab, 0)
    parts.append(("bolt", "bolt", back, sides, main))   # the -Y face is the one we see

    # the l: Minecraft's letter, foot and nub one slab tall, in (y, z),
    # extruded along +X off the right face. It reads along +Y from the
    # front corner.
    u = slab
    s1 = (150 - 3 * u) / 2
    ell = [(s1, 0), (s1 + 3 * u, 0), (s1 + 3 * u, u), (s1 + 2 * u, u), (s1 + 2 * u, side),
           (s1, side), (s1, side - u), (s1 + u, side - u), (s1 + u, u), (s1, u)]
    parts.append(("letter_l", "letter_l") + prism(ell, 0, 150, 150 + u))
    return parts


def normal(pts):
    nx = ny = nz = 0.0
    for i in range(len(pts)):
        (x0, y0, z0), (x1, y1, z1) = pts[i], pts[(i + 1) % len(pts)]
        nx += (y0 - y1) * (z0 + z1)
        ny += (z0 - z1) * (x0 + x1)
        nz += (x0 - x1) * (y0 + y1)
    l = (nx * nx + ny * ny + nz * nz) ** 0.5 or 1.0
    return nx / l, ny / l, nz / l
