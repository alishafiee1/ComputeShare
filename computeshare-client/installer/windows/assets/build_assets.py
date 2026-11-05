"""Builds the installer images from assets/src/*.jpg (made with an image generator).

Run:  python build_assets.py        (needs Pillow: pip install pillow)
Writes next to this file: welcome-panel*.bmp, finish-panel*.bmp, small-logo*.bmp/png,
csnode.ico, logo-transparent.png, warning.png, restart.png

Why a script: the generator leaves a star mark in a corner, white instead of transparent
background, and sizes that do not match the Inno Setup panels. This fixes all three.
"""
import os
from PIL import Image, ImageDraw, ImageFilter

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = os.path.join(HERE, "src")
PANEL = (164, 314)  # Inno Setup WizardImageFile (96 DPI); 328x628 is the 200% version
NAVY = (20, 33, 61)  # #14213d


def load(name):
    return Image.open(os.path.join(SRC, name)).convert("RGB")


def erase_star(im, box):
    """Paint the generator's corner star with the nearest plain background colour."""
    x0, y0, x1, y1 = box
    sample = im.getpixel((x0 - 20, (y0 + y1) // 2))
    ImageDraw.Draw(im).rectangle(box, fill=sample)
    return im


def logo_transparent():
    """White background -> real transparency (flood fill from the corners + anti-alias fringe)."""
    im = load("logo.jpg")
    w, h = im.size
    # 255 where the pixel is near white, then flood from the corner: only the outside stays marked
    near_white = Image.eval(im.convert("L"), lambda v: 255 if v > 215 else 0)
    ImageDraw.floodfill(near_white, (0, 0), 128)
    outside = near_white.point(lambda v: 255 if v == 128 else 0)
    fringe_zone = outside.filter(ImageFilter.MaxFilter(5))  # grow by 2 px to cover the soft edge
    rgba = Image.new("RGBA", (w, h))
    src_px, out_px = im.load(), rgba.load()
    out_m, zone_m = outside.load(), fringe_zone.load()
    navy_r = 20.0
    for y in range(h):
        for x in range(w):
            r, g, b = src_px[x, y]
            if out_m[x, y] == 255:
                out_px[x, y] = (0, 0, 0, 0)
            elif zone_m[x, y] == 255:  # edge pixel: a mix of navy and white
                a = max(0.0, min(1.0, (255.0 - r) / (255.0 - navy_r)))
                out_px[x, y] = (NAVY[0], NAVY[1], NAVY[2], int(round(a * 255)))
            else:
                out_px[x, y] = (r, g, b, 255)
    bbox = rgba.getchannel("A").point(lambda v: 255 if v > 8 else 0).getbbox()
    rgba = rgba.crop(bbox)
    side = max(rgba.size)
    pad = int(side * 0.04)
    canvas = Image.new("RGBA", (side + 2 * pad, side + 2 * pad), (0, 0, 0, 0))
    canvas.paste(rgba, ((canvas.width - rgba.width) // 2, (canvas.height - rgba.height) // 2))
    return canvas.resize((1024, 1024), Image.LANCZOS)


def panel(name, logo):
    im = load(name)
    erase_star(im, (610, 1225, 730, 1325))
    # panel ratio is narrower than the picture: extend the plain navy bottom instead of cropping the sides
    h = round(im.width * PANEL[1] / PANEL[0])
    bg = im.getpixel((im.width // 2, im.height - 30))
    canvas = Image.new("RGB", (im.width, h), bg)
    canvas.paste(im, (0, 0))
    # the logo sits in the empty lower part
    lw = int(im.width * 0.34)
    lg = logo.resize((lw, lw), Image.LANCZOS)
    # the logo's navy square would show as a faint box on the panel: make its navy match the panel
    px = lg.load()
    for y in range(lg.height):
        for x in range(lg.width):
            r, g, b, a = px[x, y]
            if abs(r - NAVY[0]) + abs(g - NAVY[1]) + abs(b - NAVY[2]) < 45:
                px[x, y] = (bg[0], bg[1], bg[2], a)
    canvas.paste(lg, ((canvas.width - lw) // 2, int(h * 0.70)), lg)
    return canvas


def on_white(logo, size):
    bg = Image.new("RGB", (size, size), (255, 255, 255))
    lg = logo.resize((size, size), Image.LANCZOS)
    bg.paste(lg, (0, 0), lg)
    return bg


def spot(name, out):
    im = load(name)
    erase_star(im, (1235, 640, 1330, 730))
    im.resize((880, round(880 * im.height / im.width)), Image.LANCZOS).save(os.path.join(HERE, out))
    # Inno Setup pages load BMP only
    small = im.resize((220, round(220 * im.height / im.width)), Image.LANCZOS)
    small.save(os.path.join(HERE, out.replace(".png", ".bmp")))


def main():
    logo = logo_transparent()
    logo.save(os.path.join(HERE, "logo-transparent.png"))
    logo.save(os.path.join(HERE, "csnode.ico"), sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
    for name, stem in (("welcome.jpg", "welcome-panel"), ("finish.jpg", "finish-panel")):
        big = panel(name, logo)
        for scale, suffix in ((1, ""), (2, "-200")):
            big.resize((PANEL[0] * scale, PANEL[1] * scale), Image.LANCZOS).save(os.path.join(HERE, f"{stem}{suffix}.bmp"))
    for size, suffix in ((55, ""), (110, "-200")):
        small = on_white(logo, size)
        small.save(os.path.join(HERE, f"small-logo{suffix}.bmp"))
        small.save(os.path.join(HERE, f"small-logo{suffix}.png"))
    spot("warning.jpg", "warning.png")
    spot("restart.jpg", "restart.png")
    print("ok")


if __name__ == "__main__":
    main()
