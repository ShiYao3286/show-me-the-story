"""Generate the Windows icon set from docs/show-me-the-story.webp.

Called by CI before building the Windows exe; go-winres needs one PNG per size.

Usage: python tools/make_icon.py
Requires: Pillow (pip install pillow)
"""
import os
import sys

from PIL import Image

# Repo root = parent of tools/
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, "docs", "show-me-the-story.webp")
OUTDIR = os.path.join(ROOT, "build", "windows")
SIZES = (256, 128, 64, 48, 32, 24, 16)
# The logo has transparency; composite onto the app's dark canvas so the icon
# reads correctly in Explorer (which does not blend alpha onto the app theme).
BG = (22, 22, 22, 255)  # design token base-200


def main() -> int:
    if not os.path.exists(SRC):
        print(f"error: logo not found: {SRC}", file=sys.stderr)
        return 1
    os.makedirs(OUTDIR, exist_ok=True)

    img = Image.open(SRC).convert("RGBA")
    canvas = Image.new("RGBA", img.size, BG)
    canvas.alpha_composite(img)

    # Centre-crop to a square so non-square sources still yield square icons.
    w, h = canvas.size
    side = min(w, h)
    left, top = (w - side) // 2, (h - side) // 2
    square = canvas.crop((left, top, left + side, top + side))

    for size in SIZES:
        path = os.path.join(OUTDIR, f"icon_{size}.png")
        square.resize((size, size), Image.LANCZOS).save(path, format="PNG", optimize=True)
        print(f"wrote {path} ({os.path.getsize(path)} bytes)")

    # Multi-image .ico for the NSIS wizard (embedded per-size PNGs).
    square.resize((256, 256), Image.LANCZOS).save(
        os.path.join(OUTDIR, "icon.ico"),
        format="ICO",
        sizes=[(s, s) for s in SIZES],
    )
    print("wrote build/windows/icon.ico")
    return 0


if __name__ == "__main__":
    sys.exit(main())
