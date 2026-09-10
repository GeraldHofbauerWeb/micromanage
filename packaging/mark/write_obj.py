"""Writes the mark from il_model.py as OBJ + MTL (metres, Z up)."""
import sys
from il_model import BASE, build

slab = float(sys.argv[1]) if len(sys.argv) > 1 else 30
gap = float(sys.argv[2]) if len(sys.argv) > 2 else 30
out = sys.argv[3] if len(sys.argv) > 3 else "instant-launcher"
S = 0.01

def rgb(h): return tuple(int(h[i:i + 2], 16) / 255 for i in (1, 3, 5))

with open(out + ".mtl", "w") as m:
    m.write("# Instant Launcher mark - materials\n")
    for name, col in BASE.items():
        m.write("\nnewmtl %s\nKd %.4f %.4f %.4f\nKa 0 0 0\nKs 0.05 0.05 0.05\nNs 10\nd 1\nillum 2\n" % ((name,) + rgb(col)))

verts, index = [], {}
def vid(p):
    if p not in index:
        verts.append(p); index[p] = len(verts)
    return index[p]

lines = []
for name, mat, main, sides, back in build(slab, gap):
    lines.append(f"\no {name}\nusemtl {mat}")
    for face in [main, back] + sides:
        lines.append("f " + " ".join(str(vid(p)) for p in face))
with open(out + ".obj", "w") as o:
    o.write("# Instant Launcher mark: a cube of three instance slabs, a lightning-bolt i and a Minecraft-font l.\n")
    o.write(f"# Units: metres, Z up (import with Y forward / Z up). Slab {slab:g} cm, gap {gap:g} cm.\n")
    o.write(f"mtllib {out}.mtl\n")
    for x, y, z in verts:
        o.write(f"v {x * S:.4f} {y * S:.4f} {z * S:.4f}\n")
    o.write("\n".join(lines) + "\n")
print("wrote", out + ".obj", len(verts), "vertices")
