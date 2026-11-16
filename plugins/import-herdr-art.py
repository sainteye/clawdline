#!/usr/bin/env python3
"""Vendor pinned, MIT-licensed Herdr artwork from two local source checkouts.

No network or upstream code execution. cwebp -lossless -exact preserves pixels;
the inventory records original and vendored file hashes for every asset.
"""
import argparse
import concurrent.futures
import hashlib
import json
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parent


def digest(data):
    return hashlib.sha256(data).hexdigest()


def convert(job):
    src, dst, relative = job
    dst.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(['cwebp', '-quiet', '-lossless', '-exact', str(src), '-o', str(dst)], check=True)
    return {'source': relative, 'asset': dst.name,
            'sourceSha256': digest(src.read_bytes()), 'sha256': digest(dst.read_bytes())}


def vendor(source, slug):
    target = ROOT / slug
    (target / 'upstream').mkdir(parents=True, exist_ok=True)
    if slug == 'session-codecraft':
        mod = source / 'mods/world-of-codecraft'
        names = ['main.js', 'place.js', 'monde-data.js', 'terre-data.js', 'lieux.js', 'heros-data.js', 'kit.js', 'ambiance.js']
        heroes = ['dwarf-artificer', 'elf-mage', 'human-warrior', 'orc-mage', 'undead-rogue', 'dwarf-priest']
        images = list((mod / 'art/monde').glob('*.png'))
        for hero in heroes:
            images += list((mod / 'art/heros' / hero).rglob('*.png'))
        fonts = ['CinzelDecorative-Bold.ttf', 'OFL-Cinzel.txt']
    else:
        mod = source / 'mods/kaiju'
        names = ['main.js', 'city.js', 'state.js', 'manifest.js']
        images = []
        for theme in ['day', 'night']:
            for path in (mod / 'assets' / theme).rglob('*.png'):
                if 'avatar-' not in path.name and 'head-' not in path.name and 'face-' not in path.name:
                    images.append(path)
        fonts = []
    for name in ['LICENSE', 'RECIPE.md']:
        shutil.copyfile(source / name, target / 'upstream' / name)
    for name in names:
        shutil.copyfile(mod / name, target / 'upstream' / name)
    for name in fonts:
        shutil.copyfile(mod / 'fonts' / name, target / 'upstream' / name)
        if name.startswith('OFL-'):
            notice = target / 'upstream' / name
            notice.write_text('\n'.join(line.rstrip() for line in notice.read_text().splitlines()) + '\n')
    jobs = [(p, target / 'art' / p.relative_to(mod).with_suffix('.webp'), str(p.relative_to(source))) for p in images]
    with concurrent.futures.ThreadPoolExecutor() as pool:
        inventory = list(pool.map(convert, jobs))
    for item, (_, dst, _) in zip(inventory, jobs):
        item['asset'] = str(dst.relative_to(target))
    (target / 'art-inventory.json').write_text(json.dumps(inventory, indent=2) + '\n')
    print(slug, len(inventory), 'images', sum(p.stat().st_size for _, p, _ in jobs), 'bytes', flush=True)


if __name__ == '__main__':
    args = argparse.ArgumentParser(description=__doc__)
    args.add_argument('world', type=Path)
    args.add_argument('kaiju', type=Path)
    parsed = args.parse_args()
    vendor(parsed.world, 'session-codecraft')
    vendor(parsed.kaiju, 'session-kaiju')
