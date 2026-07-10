#!/usr/bin/env python3
"""生成 JianVideo 自有的确定性 Bayer DNG delegate 测试样本。"""

import hashlib
import struct
from pathlib import Path

WIDTH = 256
HEIGHT = 256
OUTPUT = Path(__file__).with_name("jianvideo-gradient.dng")
BYTE, ASCII, SHORT, LONG, RATIONAL, SRATIONAL = 1, 2, 3, 4, 5, 10


def encode_values(kind: int, values) -> bytes:
    if kind == BYTE:
        return bytes(values)
    if kind == ASCII:
        return values.encode("ascii") + b"\0"
    if kind == SHORT:
        return struct.pack("<" + "H" * len(values), *values)
    if kind == LONG:
        return struct.pack("<" + "I" * len(values), *values)
    signed = kind == SRATIONAL
    code = "ii" if signed else "II"
    return b"".join(struct.pack("<" + code, numerator, denominator) for numerator, denominator in values)


def entry(tag: int, kind: int, values) -> dict:
    data = encode_values(kind, values)
    count = len(data) if kind in (BYTE, ASCII) else len(values)
    return {"tag": tag, "kind": kind, "count": count, "data": data}


def tiff_entries() -> list[dict]:
    return [
        entry(254, LONG, [0]), entry(256, LONG, [WIDTH]), entry(257, LONG, [HEIGHT]),
        entry(258, SHORT, [16]), entry(259, SHORT, [1]), entry(262, SHORT, [32803]),
        entry(271, ASCII, "JianVideo"), entry(272, ASCII, "Synthetic Gradient DNG"),
        entry(273, LONG, [0]), entry(274, SHORT, [1]), entry(277, SHORT, [1]),
        entry(278, LONG, [HEIGHT]), entry(279, LONG, [WIDTH * HEIGHT * 2]),
        entry(284, SHORT, [1]), entry(305, ASCII, "JianVideo fixture generator"),
        entry(33421, SHORT, [2, 2]), entry(33422, BYTE, [0, 1, 1, 2]),
    ]


def dng_entries() -> list[dict]:
    identity = [(1, 1), (0, 1), (0, 1), (0, 1), (1, 1), (0, 1), (0, 1), (0, 1), (1, 1)]
    return [
        entry(50706, BYTE, [1, 4, 0, 0]), entry(50707, BYTE, [1, 1, 0, 0]),
        entry(50708, ASCII, "JianVideo Synthetic Gradient DNG"),
        entry(50710, BYTE, [0, 1, 2]), entry(50711, SHORT, [1]),
        entry(50713, SHORT, [1, 1]), entry(50714, RATIONAL, [(0, 1)]),
        entry(50717, LONG, [65535]), entry(50719, LONG, [0, 0]),
        entry(50720, LONG, [WIDTH, HEIGHT]), entry(50721, SRATIONAL, identity),
        entry(50728, RATIONAL, [(1, 2), (1, 1), (3, 4)]),
        entry(50730, SRATIONAL, [(0, 1)]), entry(50778, SHORT, [21]),
        entry(50829, LONG, [0, 0, HEIGHT, WIDTH]),
    ]


def raw_pixels() -> bytes:
    gains = (9000, 15000, 15000, 22000)
    values = []
    for y in range(HEIGHT):
        for x in range(WIDTH):
            base = (x * 257 + y * 131) % 48000
            values.append(min(65535, base + gains[(y % 2) * 2 + x % 2]))
    return struct.pack("<" + "H" * len(values), *values)


def build_dng() -> bytes:
    entries = sorted(tiff_entries() + dng_entries(), key=lambda item: item["tag"])
    extra_offset = 8 + 2 + len(entries) * 12 + 4
    extra = bytearray()
    for item in entries:
        if len(item["data"]) > 4:
            item["offset"] = extra_offset + len(extra)
            extra.extend(item["data"])
            extra.extend(b"\0" * (len(extra) % 2))
    pixel_offset = extra_offset + len(extra)
    next(item for item in entries if item["tag"] == 273)["data"] = struct.pack("<I", pixel_offset)
    output = bytearray(b"II*\0\x08\0\0\0" + struct.pack("<H", len(entries)))
    for item in entries:
        output.extend(struct.pack("<HHI", item["tag"], item["kind"], item["count"]))
        output.extend(item["data"].ljust(4, b"\0") if len(item["data"]) <= 4 else struct.pack("<I", item["offset"]))
    output.extend(b"\0\0\0\0" + extra + raw_pixels())
    return bytes(output)


def main() -> None:
    content = build_dng()
    OUTPUT.write_bytes(content)
    print(f"已生成 {OUTPUT.name}：{len(content)} 字节，SHA-256 {hashlib.sha256(content).hexdigest()}")


if __name__ == "__main__":
    main()
