#!/usr/bin/env python3
"""Draws the iOS app icon (1024 px PNG, no text) from the web brand mark
(web/public/favicon.svg: an L stroke and a dot in mint on near-black).

iOS rounds the corners itself, so the icon is a full square without transparency.
Pure Python (no Pillow): anti-aliasing from exact distances to the shapes.

  scripts/ios-icon.py [out.png]
"""
import math
import pathlib
import struct
import sys
import zlib

SIZE = 1024
ROOT = pathlib.Path(__file__).resolve().parent.parent
OUT = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "ios/Locarr/Resources/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png"

# Geometry of favicon.svg (viewBox 32), scaled to 84 % around the centre so the mark keeps
# clear of the rounded corners iOS applies.
S = SIZE / 32 * 0.84
DX = DY = SIZE / 2 - 16 * S
STROKE = 3.5 * S / 2
SEGMENTS = [((10 * S + DX, 7 * S + DY), (10 * S + DX, 25 * S + DY)), ((10 * S + DX, 25 * S + DY), (22 * S + DX, 25 * S + DY))]
DOT = (21 * S + DX, 11 * S + DY, 3 * S)

MINT = (0x4F, 0xD1, 0xA5)
TOP = (0x17, 0x1C, 0x23)
BOTTOM = (0x0B, 0x0E, 0x12)


def seg_dist(px, py, a, b):
    ax, ay = a
    bx, by = b
    vx, vy = bx - ax, by - ay
    t = max(0.0, min(1.0, ((px - ax) * vx + (py - ay) * vy) / (vx * vx + vy * vy)))
    cx, cy = ax + t * vx, ay + t * vy
    return math.hypot(px - cx, py - cy)


def coverage(d):
    """Signed distance (negative inside) to pixel coverage with a 1 px soft edge."""
    return max(0.0, min(1.0, 0.5 - d))


def mix(a, b, t):
    return tuple(round(x + (y - x) * t) for x, y in zip(a, b))


def pixel(x, y):
    px, py = x + 0.5, y + 0.5
    # Background: a quiet vertical gradient with a faint mint glow behind the mark.
    bg = mix(TOP, BOTTOM, py / SIZE)
    glow = max(0.0, 1 - math.hypot(px - SIZE * 0.45, py - SIZE * 0.55) / (SIZE * 0.62)) ** 2 * 0.10
    bg = mix(bg, MINT, glow)
    d = min(seg_dist(px, py, a, b) for a, b in SEGMENTS) - STROKE
    d = min(d, math.hypot(px - DOT[0], py - DOT[1]) - DOT[2])
    return mix(bg, MINT, coverage(d))


def png(width, height, rows):
    def chunk(tag, data):
        c = tag + data
        return struct.pack(">I", len(data)) + c + struct.pack(">I", zlib.crc32(c) & 0xFFFFFFFF)
    raw = b"".join(b"\x00" + bytes(v for p in row for v in p) for row in rows)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def main():
    rows = [[pixel(x, y) for x in range(SIZE)] for y in range(SIZE)]
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_bytes(png(SIZE, SIZE, rows))
    print(f"wrote {OUT}")


if __name__ == "__main__":
    main()
