#!/usr/bin/env python3
"""Собирает icon.ico для hotpipe-gui.

Генератор хранится в проекте (как в wsldrive): значок — двоичный файл, и без
исходника его нельзя ни поправить, ни воспроизвести. ICO собирается вручную —
PIL и ImageMagick на сборочной машине нет.

Рисунок: тёмная скруглённая плашка, поперёк — оранжевая «горячая» труба, на
концах — два зелёных узла: туннель между двумя машинами.

    python3 make-icon.py
"""

import os
import struct

BG = (0x1F, 0x2A, 0x37)    # тёмно-синий, как у Flowbite gray-800
PIPE = (0xF9, 0x73, 0x16)  # оранжевый — труба
NODE = (0x22, 0xC5, 0x5E)  # зелёный — концы туннеля


def pixel(x, y, size):
    """Цвет и прозрачность точки. Возвращает (b, g, r, a)."""
    u, v = (x + 0.5) / size, (y + 0.5) / size

    r = 0.16
    cx = min(max(u, r), 1 - r)
    cy = min(max(v, r), 1 - r)
    if (u - cx) ** 2 + (v - cy) ** 2 > r * r:
        return (0, 0, 0, 0)

    c = BG
    if 0.40 <= v <= 0.60 and 0.22 <= u <= 0.78:
        c = PIPE
    for nx in (0.24, 0.76):
        if (u - nx) ** 2 + (v - 0.5) ** 2 <= 0.15 ** 2:
            c = NODE
    return (c[2], c[1], c[0], 255)


def image(size):
    """Одно изображение внутри ICO: заголовок, точки снизу вверх, маска."""
    xor = bytearray()
    for y in range(size - 1, -1, -1):
        for x in range(size):
            xor += bytes(pixel(x, y, size))
    # Маска прозрачности при 32 битах не используется, но формат её требует.
    row = ((size + 31) // 32) * 4
    and_mask = bytes(row * size)
    header = struct.pack(
        "<IiiHHIIiiII",
        40, size, size * 2, 1, 32, 0, len(xor) + len(and_mask), 0, 0, 0, 0,
    )
    return header + bytes(xor) + and_mask


def main():
    sizes = [16, 32, 48, 64]
    images = [image(s) for s in sizes]
    out = bytearray(struct.pack("<HHH", 0, 1, len(sizes)))
    offset = 6 + 16 * len(sizes)
    for s, img in zip(sizes, images):
        out += struct.pack("<BBBBHHII", s, s, 0, 0, 1, 32, len(img), offset)
        offset += len(img)
    for img in images:
        out += img
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "icon.ico")
    with open(path, "wb") as f:
        f.write(bytes(out))
    print(f"icon.ico собран: {len(out)} байт, размеры {sizes}")


if __name__ == "__main__":
    main()
