#!/usr/bin/env python3
"""Capture the README's example screenshots from the demo server.

Two things the viewport-sized grabs got wrong, both fixed here:

1. **The pages scroll.** Their root is a fixed, 100dvh-tall container with an
   inner scroller, so a screenshot at the device's own size chops off whatever
   is below the fold — the phone shot was missing the whole file-browser
   section. Rendering against a deliberately tall viewport lets that 100dvh
   container grow, so the full content lays out and nothing is cut.
2. **Trailing background** left below short content is trimmed, so each image
   ends exactly where the content does.

Run the demo server first:
    CABLEDROP_SHOTS=1 go test -run TestScreenshotServer -timeout 1h ./internal/serve/
"""
import pathlib
import struct
import subprocess
import sys

BASE = "http://127.0.0.1:18766"
CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
OUT = pathlib.Path(__file__).resolve().parent.parent / "docs"
SCALE = 2

# page -> (path, css width, capture height, trim trailing background?)
#
# The panel is a fixed-size window whose root is a 100dvh container, so it has
# no natural content height — it stretches to whatever viewport it is given.
# Its example shot must therefore be its real window size, untrimmed.
# The phone page is a scrolling document, so it is captured tall and trimmed.
PAGES = {
    "panel": ("/panel", 380, 540, False),
    "phone": ("/", 390, 2600, True),
}


def shoot(url: str, w: int, h: int, out: pathlib.Path, dark: bool) -> None:
    cmd = [
        CHROME, "--headless", "--disable-gpu", "--hide-scrollbars",
        f"--force-device-scale-factor={SCALE}",
        f"--screenshot={out}", f"--window-size={w},{h}",
        "--virtual-time-budget=8000",
    ]
    if dark:
        # flips prefers-color-scheme; the pages key their dark palette off it
        cmd.append("--force-dark-mode")
        cmd.append("--disable-features=WebContentsForceDark")
    cmd.append(url)
    subprocess.run(cmd, check=True, capture_output=True)


def to_bmp(png: pathlib.Path, bmp: pathlib.Path) -> None:
    subprocess.run(["sips", "-s", "format", "bmp", str(png), "--out", str(bmp)],
                   check=True, capture_output=True)


def read_bmp(path: pathlib.Path):
    data = path.read_bytes()
    off = struct.unpack_from("<I", data, 10)[0]
    w, h = struct.unpack_from("<ii", data, 18)
    bpp = struct.unpack_from("<H", data, 28)[0]
    stride = ((w * bpp // 8) + 3) // 4 * 4
    px = bpp // 8
    top_down = h < 0
    return data, off, w, abs(h), stride, px, top_down


def content_height(png: pathlib.Path, bmp: pathlib.Path) -> int:
    """Rows until the last one that differs from the page background.

    The background is taken as the image's *modal* colour, not a corner sample:
    the top-left pixel can easily land inside a card, which made every row look
    like content and defeated the trim entirely.
    """
    to_bmp(png, bmp)
    data, off, w, h, stride, px, top_down = read_bmp(bmp)

    def row(y):
        return off + (y if top_down else h - 1 - y) * stride

    counts: dict[tuple[int, int, int], int] = {}
    for y in range(0, h, 4):
        base = row(y)
        for x in range(0, w, 4):
            i = base + x * px
            key = (data[i + 2], data[i + 1], data[i])  # RGB, quantised
            counts[key] = counts.get(key, 0) + 1
    bg = max(counts, key=counts.get)

    last = 0
    for y in range(h - 1, -1, -1):
        base = row(y)
        for x in range(0, w, 2):
            i = base + x * px
            if (abs(data[i + 2] - bg[0]) + abs(data[i + 1] - bg[1])
                    + abs(data[i] - bg[2])) > 18:
                last = y
                break
        if last:
            break
    print(f"    (bg={'#%02x%02x%02x' % bg}, content ends at row {last})")
    return min(last + 16, h)  # small bottom margin


def main() -> None:
    for name, (path, w, h, trim) in PAGES.items():
        for theme, dark in (("light", False), ("dark", True)):
            raw = pathlib.Path(f"/tmp/preview/_raw-{name}-{theme}.png")
            bmp = pathlib.Path(f"/tmp/preview/_raw-{name}-{theme}.bmp")
            shoot(BASE + path, w, h, raw, dark)
            ch = content_height(raw, bmp) if trim else h * SCALE
            final = OUT / f"{name}-{theme}.png"
            subprocess.run(["sips", "-c", str(ch), str(w * SCALE),
                            "--cropOffset", "0", "0", str(raw),
                            "--out", str(final)], check=True, capture_output=True)
            print(f"  {final.name}: {w * SCALE}x{ch}px")


if __name__ == "__main__":
    sys.exit(main())
