"""Builds the Instant Launcher mark as a Blender scene.

Run inside Blender (Scripting tab, or from a terminal):

    blender --background --python packaging/mark/instant-launcher-blender.py
    flatpak run org.blender.Blender --background --python "$PWD/packaging/mark/instant-launcher-blender.py"

It writes instant-launcher.blend next to this script and a test render
instant-launcher-render.png. The geometry is the icon's, in centimetres:
a 5x5x5 cube: three 150x150x30 slabs with 30 between them; a lightning-bolt i on the front face whose
flat cuts sit level with the middle slab; a Minecraft-font l on the right
face, foot and nub a slab face tall. Both letters protrude one slab depth.
"""
import math
import os
import sys
import bpy

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import il_model

S = 0.01  # cm -> m

# Proportions. The icon has 36-thick slabs 88 apart (212 tall in all). Set
# SLAB = GAP = 30 for a true 5x5x5 cube: three slabs and two gaps of one unit
# each, five units wide.
SLAB = float(os.environ.get("IL_SLAB", 30))
GAP = float(os.environ.get("IL_GAP", 30))
PITCH = SLAB + GAP
SIDE = 2 * PITCH + SLAB          # height of the stack

PALETTE = {"Slab bottom": il_model.BASE["slab_bottom"], "Slab middle": il_model.BASE["slab_middle"],
           "Slab top": il_model.BASE["slab_top"], "Bolt": il_model.BASE["bolt"], "Letter l": il_model.BASE["letter_l"]}
BACKGROUND = "#1E2128"


def srgb_to_linear(c):
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def rgba(hexcol):
    r, g, b = (int(hexcol[i:i + 2], 16) / 255 for i in (1, 3, 5))
    return (srgb_to_linear(r), srgb_to_linear(g), srgb_to_linear(b), 1.0)


def material(name):
    mat = bpy.data.materials.get(name) or bpy.data.materials.new(name)
    mat.use_nodes = True
    bsdf = mat.node_tree.nodes.get("Principled BSDF")
    bsdf.inputs["Base Color"].default_value = rgba(PALETTE[name])
    bsdf.inputs["Roughness"].default_value = 0.6
    return mat


def mesh_object(name, mat_name, faces, collection):
    """One object from a list of faces given as (x, y, z) tuples in cm."""
    verts, index, polys = [], {}, []
    for face in faces:
        poly = []
        for p in face:
            if p not in index:
                index[p] = len(verts)
                verts.append(tuple(c * S for c in p))
            poly.append(index[p])
        polys.append(poly)
    me = bpy.data.meshes.new(name)
    me.from_pydata(verts, [], polys)
    me.update()
    me.materials.append(material(mat_name))
    ob = bpy.data.objects.new(name, me)
    collection.objects.link(ob)
    return ob


def clear_scene():
    for ob in list(bpy.data.objects):
        bpy.data.objects.remove(ob, do_unlink=True)


NAMES = {"slab_bottom": "Slab bottom", "slab_middle": "Slab middle", "slab_top": "Slab top",
         "bolt": "Bolt", "letter_l": "Letter l"}


def build():
    clear_scene()
    scene = bpy.context.scene
    col = bpy.data.collections.get("Instant Launcher") or bpy.data.collections.new("Instant Launcher")
    if col.name not in scene.collection.children:
        scene.collection.children.link(col)

    for name, mat, main, sides, back in il_model.build(SLAB, GAP):
        mesh_object(NAMES[name], NAMES[mat], [main, back] + sides, col)

    # camera: orthographic, the 2:1 pixel-isometric angle (30 degrees elevation)
    cam_data = bpy.data.cameras.new("Icon camera")
    cam_data.type = "ORTHO"
    cam_data.ortho_scale = 3.4
    cam = bpy.data.objects.new("Icon camera", cam_data)
    col.objects.link(cam)
    centre = (0.75 + 0.18, 0.75 - 0.18, SIDE * S / 2)
    direction = (math.sqrt(0.5) * math.cos(math.radians(30)), -math.sqrt(0.5) * math.cos(math.radians(30)), math.sin(math.radians(30)))
    cam.location = tuple(c + 10 * d for c, d in zip(centre, direction))
    cam.rotation_euler = (math.radians(60), 0.0, math.radians(45))
    scene.camera = cam

    # light: three shadowless suns, one per visible face direction, so the
    # faces shade like the icon (top brightest, front mid, right darkest)
    # without any cast shadows muddying the letters.
    for name, rot, energy in (("Sun top", (0, 0, 0), 3.0),
                              ("Sun front", (math.radians(90), 0, 0), 2.2),
                              ("Sun right", (0, math.radians(90), 0), 1.3)):
        data = bpy.data.lights.new(name, type="SUN")
        data.energy = energy
        data.use_shadow = False
        ob = bpy.data.objects.new(name, data)
        col.objects.link(ob)
        ob.rotation_euler = rot

    # world and render
    world = scene.world or bpy.data.worlds.new("World")
    scene.world = world
    world.use_nodes = True
    bg = world.node_tree.nodes.get("Background")
    bg.inputs["Color"].default_value = rgba(BACKGROUND)
    bg.inputs["Strength"].default_value = 0.35
    scene.view_settings.view_transform = "Standard"   # icon-true colours, no filmic curve
    scene.render.resolution_x = scene.render.resolution_y = 1024
    scene.render.film_transparent = False
    try:
        scene.render.engine = "BLENDER_EEVEE_NEXT"
    except TypeError:
        scene.render.engine = "BLENDER_EEVEE"


if __name__ == "__main__":
    build()
    here = os.path.dirname(os.path.abspath(__file__)) if "__file__" in globals() else os.getcwd()
    suffix = os.environ.get("IL_SUFFIX", "")
    blend = os.path.join(here, f"instant-launcher{suffix}.blend")
    bpy.ops.wm.save_as_mainfile(filepath=blend)
    bpy.context.scene.render.filepath = os.path.join(here, f"instant-launcher{suffix}-render.png")
    bpy.ops.render.render(write_still=True)
    print("saved", blend)
