#!/usr/bin/env python3
"""Derive every DBHelm brand asset from the single source logo.

The source (assets/brand/source/dbhelm-logo-source.png) is flat blue on an
opaque black background and carries an AI-generator watermark in the bottom
right corner. This script keys the black out to transparency, erases the
watermark, and writes the derived files:

  assets/brand/dbhelm-logo.png            wheel + wordmark, transparent
  assets/brand/dbhelm-mark.png            wheel only, transparent
  desktop/build/appicon.png               1024px app icon (mark on dark tile)
  desktop/build/windows/icon.ico          multi-size Windows icon from the same tile
  extension/media/icon.png                256px extension icon (mark on dark tile)
  extension/media/logo-full.png           wheel + wordmark, transparent
  extension/media/activitybar-icon-128.png  alpha-only silhouette for the Activity Bar

Requires Pillow. Run from the repo root: python3 scripts/brand/make_assets.py
"""
from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[2]
SRC = ROOT / "assets/brand/source/dbhelm-logo-source.png"

# Geometry of the 2048x2048 source (measured on the image).
WATERMARK_BOX = (1692, 1700, 1900, 1950)   # generator sparkle, bottom right
WHEEL_CENTER = (984, 979)                  # centre of the ship's wheel
WHEEL_RING_RADIUS = 575                    # outer edge of the rim (excludes handles)
MARK_BOTTOM = 1622                         # wheel handles end above the wordmark
NOISE_FLOOR = 12                           # source background noise peaks at ~4/255
TILE_BG = (11, 15, 20, 255)                # near-black, matches the source's own ground


def key_out_black(img: Image.Image) -> tuple[Image.Image, tuple[int, int, int]]:
    """Return (transparent RGBA, ink colour). Black -> alpha 0, ink -> alpha 255."""
    rgb = img.convert("RGB")
    px = list(rgb.getdata())
    bright = [p for p in px if sum(p) > 3 * 120]
    bright.sort(key=sum)
    ink = bright[len(bright) // 2]  # median of the bright pixels = the flat blue
    ink_peak = max(ink)
    out = Image.new("RGBA", rgb.size)
    data = []
    for r, g, b in px:
        v = max(r, g, b)
        a = 0 if v <= NOISE_FLOOR else min(255, round((v - NOISE_FLOOR) / (ink_peak - NOISE_FLOOR) * 255))
        data.append((*ink, a))
    out.putdata(data)
    return out, ink


def erase(img: Image.Image, box: tuple[int, int, int, int]) -> None:
    ImageDraw.Draw(img).rectangle(box, fill=(0, 0, 0, 0))


def tight_crop(img: Image.Image, pad: int) -> Image.Image:
    bbox = img.getchannel("A").getbbox()
    x0, y0, x1, y1 = bbox
    canvas = Image.new("RGBA", (x1 - x0 + 2 * pad, y1 - y0 + 2 * pad), (0, 0, 0, 0))
    canvas.alpha_composite(img.crop(bbox), (pad, pad))
    return canvas


def square(img: Image.Image, size: int) -> Image.Image:
    side = max(img.size)
    canvas = Image.new("RGBA", (side, side), (0, 0, 0, 0))
    canvas.alpha_composite(img, ((side - img.width) // 2, (side - img.height) // 2))
    return canvas.resize((size, size), Image.LANCZOS)


def on_tile(mark: Image.Image, size: int, inset: float, radius: float) -> Image.Image:
    tile = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    ImageDraw.Draw(tile).rounded_rectangle((0, 0, size - 1, size - 1), int(size * radius), fill=TILE_BG)
    inner = int(size * (1 - 2 * inset))
    m = square(mark, inner)
    tile.alpha_composite(m, ((size - inner) // 2, (size - inner) // 2))
    return tile


def silhouette(size: int) -> Image.Image:
    """Alpha-only Activity Bar glyph, drawn from geometry.

    The photographic mark has spokes and a double rim that turn to mush at
    24px, so the Activity Bar gets a simplified drawing of the same idea:
    rim, four spoke stubs, and the database cylinder with its up arrow cut out.
    VS Code masks it to a single colour using only the alpha channel.
    """
    S = 1024
    ink, cut = 255, 0
    g = Image.new("L", (S, S), 0)
    d = ImageDraw.Draw(g)
    cx = cy = S // 2
    d.ellipse((cx - 440, cy - 440, cx + 440, cy + 440), fill=ink)
    d.ellipse((cx - 385, cy - 385, cx + 385, cy + 385), fill=cut)
    for dx, dy in ((0, -1), (0, 1), (-1, 0), (1, 0)):  # spoke stubs
        x0, y0 = cx + dx * 340, cy + dy * 340
        d.rounded_rectangle((x0 - 26, y0 - 26, x0 + 26, y0 + 26), 12, fill=ink)
    l, r = cx - 190, cx + 190
    d.rectangle((l, 360, r, 700), fill=ink)
    d.ellipse((l, 300, r, 420), fill=ink)
    d.ellipse((l, 640, r, 760), fill=ink)
    for c in (360, 475, 590):  # gaps between the three discs
        d.arc((l, c - 60, r, c + 60), 0, 180, fill=cut, width=24)
    d.polygon([(cx, 318), (cx + 52, 376), (cx - 52, 376)], fill=cut)  # arrow head
    d.rectangle((cx - 17, 376, cx + 17, 412), fill=cut)               # arrow stem
    g = g.resize((size, size), Image.LANCZOS)
    out = Image.new("RGBA", (size, size), (255, 255, 255, 0))
    out.putalpha(g)
    return out


def main() -> None:
    src = Image.open(SRC).convert("RGBA")
    keyed, ink = key_out_black(src)
    print("ink colour", "#%02x%02x%02x" % ink)
    erase(keyed, WATERMARK_BOX)

    logo = tight_crop(keyed, 40)
    logo.save(ROOT / "assets/brand/dbhelm-logo.png", optimize=True)

    mark_src = keyed.copy()
    erase(mark_src, (0, MARK_BOTTOM, mark_src.width, mark_src.height))
    mark = tight_crop(mark_src, 40)
    mark_sq = square(mark, 1024)
    mark_sq.save(ROOT / "assets/brand/dbhelm-mark.png", optimize=True)

    app = on_tile(mark, 1024, 0.13, 0.22)
    app.save(ROOT / "desktop/build/appicon.png", optimize=True)
    app.save(ROOT / "desktop/build/windows/icon.ico", sizes=[(s, s) for s in (16, 24, 32, 48, 64, 128, 256)])

    ext = ROOT / "extension/media"
    ext.mkdir(parents=True, exist_ok=True)
    on_tile(mark, 256, 0.13, 0.22).save(ext / "icon.png", optimize=True)
    logo.resize((logo.width // 2, logo.height // 2), Image.LANCZOS).save(ext / "logo-full.png", optimize=True)
    silhouette(128).save(ext / "activitybar-icon-128.png", optimize=True)
    print("done")


if __name__ == "__main__":
    main()
