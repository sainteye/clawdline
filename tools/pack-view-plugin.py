#!/usr/bin/env python3
"""Pack a self-contained View Plugin v1 directory, without installing dependencies."""
import argparse
import base64
import binascii
import json
from pathlib import Path
import struct
import zipfile
import zlib


def validate_cover(manifest):
    """Match the browser cover profile before writing any output archive."""
    policy = json.loads((Path(__file__).resolve().parent.parent / 'docs/plugins/view-plugin-v1.schema.json').read_text())
    cover = manifest.get('cover')
    if not isinstance(cover, str) or not cover.startswith('data:image/png;base64,'):
        raise ValueError('A dedicated embedded PNG cover is required before publication or installation')
    encoded = cover[len('data:image/png;base64,'):]
    try:
        png = base64.b64decode(encoded, validate=True)
        if not encoded or base64.b64encode(png).decode() != encoded:
            raise ValueError('Non-canonical cover encoding')
    except (ValueError, binascii.Error) as error:
        raise ValueError('Invalid cover encoding') from error
    if len(png) < 57 or png[:8] != b'\x89PNG\r\n\x1a\n':
        raise ValueError('Invalid PNG cover')
    at, side, ended, seen_data = 8, 0, False, False
    compressed = []
    while at < len(png):
        if ended or at + 12 > len(png):
            raise ValueError('Invalid cover chunks')
        size = struct.unpack_from('>I', png, at)[0]
        end = at + size + 12
        if end > len(png) or binascii.crc32(png[at + 4:end - 4]) != struct.unpack_from('>I', png, end - 4)[0]:
            raise ValueError('Invalid cover CRC')
        kind = png[at + 4:at + 8]
        if at == 8:
            if kind != b'IHDR' or size != 13:
                raise ValueError('Missing cover header')
            side, height = struct.unpack_from('>II', png, at + 8)
            if not side or side > policy['properties']['cover']['x-maximum-side'] or side != height or png[at + 16:at + 21] != bytes([8, 6, 0, 0, 0]):
                raise ValueError('Cover must be a bounded square, non-interlaced RGBA8 PNG')
        elif kind == b'IDAT':
            seen_data = True
            compressed.append(png[at + 8:end - 4])
        elif kind == b'IEND':
            if size or not seen_data or end != len(png):
                raise ValueError('Invalid cover ending')
            ended = True
        else:
            raise ValueError('Cover animations and ancillary metadata are unsupported')
        at = end
    if not ended:
        raise ValueError('Missing cover ending')
    stride = side * 4 + 1
    expected = side * stride
    decoder = zlib.decompressobj()
    try:
        pixels = decoder.decompress(b''.join(compressed), expected + 1)
    except zlib.error as error:
        raise ValueError('Invalid cover pixels') from error
    if len(pixels) != expected or not decoder.eof or decoder.unused_data or any(pixels[y * stride] > 4 for y in range(side)):
        raise ValueError('Invalid or oversized cover pixels')


def pack(source: Path, output: Path):
    policy = json.loads((Path(__file__).resolve().parent.parent / 'docs/plugins/view-plugin-v1.schema.json').read_text())
    with (source / 'manifest.json').open('rb') as manifest_file:
        manifest_bytes = manifest_file.read(policy['properties']['cover']['maxLength'] + 1)
    if len(manifest_bytes) > policy['properties']['cover']['maxLength']:
        raise ValueError('Manifest including cover exceeds the package profile')
    manifest = json.loads(manifest_bytes)
    validate_cover(manifest)
    entry = manifest['entry']
    if not isinstance(entry, str) or not entry.endswith('.html') or any(p.startswith('.') or not p for p in entry.split('/')) or '\\' in entry:
        raise ValueError('Entry must be a relative HTML path')
    content = (source / entry).read_text(encoding='utf-8')
    if '<!-- CLAWDLINE_SDK -->' in content:
        sdk = Path(__file__).resolve().parent.parent / 'plugins/sdk/view-plugin-v1.js'
        content = content.replace('<!-- CLAWDLINE_SDK -->', '<script>\n' + sdk.read_text(encoding='utf-8') + '\n</script>')
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, 'w', compression=zipfile.ZIP_STORED) as archive:
        for name, data in [('manifest.json', manifest_bytes), (entry, content.encode('utf-8'))]:
            info = zipfile.ZipInfo(name, date_time=(2026, 1, 1, 0, 0, 0))
            info.external_attr = 0o100644 << 16
            archive.writestr(info, data)
    # The browser remains the authority for manifest and protocol acceptance.
    print(f'{output}: {output.stat().st_size} bytes')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source', type=Path)
    parser.add_argument('output', type=Path)
    args = parser.parse_args()
    pack(args.source, args.output)
