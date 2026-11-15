#!/usr/bin/env python3
"""Pack a self-contained View Plugin v1 directory, without installing dependencies."""
import argparse
import json
from pathlib import Path
import zipfile


def pack(source: Path, output: Path):
    manifest_bytes = (source / 'manifest.json').read_bytes()
    manifest = json.loads(manifest_bytes)
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
