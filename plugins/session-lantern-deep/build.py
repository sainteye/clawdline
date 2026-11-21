#!/usr/bin/env python3
"""Inline the scene script and its authored backdrops into the self-contained entry."""
import base64
from pathlib import Path

here = Path(__file__).resolve().parent
scene = (here / 'scene.js').read_text(encoding='utf-8')
for art in sorted((here / 'art').glob('*.webp')):
    marker = f'%%ART:{art.stem}%%'
    if marker not in scene:
        raise SystemExit(f'{art.name} is not referenced by scene.js')
    scene = scene.replace(marker, 'data:image/webp;base64,' + base64.b64encode(art.read_bytes()).decode())
if '%%ART:' in scene:
    raise SystemExit('scene.js references artwork that is missing from art/')
page = (here / 'src.html').read_text(encoding='utf-8')
if page.count('/*SCENE*/') != 1:
    raise SystemExit('src.html needs exactly one scene marker')
(here / 'index.html').write_text(page.replace('/*SCENE*/', scene), encoding='utf-8')
print(f"index.html: {(here / 'index.html').stat().st_size} bytes")
