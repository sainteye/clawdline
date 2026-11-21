#!/usr/bin/env python3
"""Inline the scene script and its authored backdrops into the self-contained entry."""
import base64
from pathlib import Path

here = Path(__file__).resolve().parent
scene = (here / 'scene.js').read_text(encoding='utf-8')
cast = []
for art in sorted((here / 'art').glob('*.webp')):
    data = 'data:image/webp;base64,' + base64.b64encode(art.read_bytes()).decode()
    if art.stem.startswith(('char-', 'enemy-')) or (art.stem[0] == 'f' and art.stem[1:3].isdigit()) or (art.stem[0] == 'g' and art.stem[1].isdigit()):
        cast.append(f'ART[{art.stem!r}] = {data!r};')
        continue
    marker = f'%%ART:{art.stem}%%'
    if marker not in scene:
        raise SystemExit(f'{art.name} is not referenced by scene.js')
    scene = scene.replace(marker, data)
if scene.count('/*CAST_ART*/') != 1:
    raise SystemExit('scene.js needs exactly one cast marker')
scene = scene.replace('/*CAST_ART*/', '\n'.join(cast))
if '%%ART:' in scene:
    raise SystemExit('scene.js references artwork that is missing from art/')
page = (here / 'src.html').read_text(encoding='utf-8')
if page.count('/*SCENE*/') != 1:
    raise SystemExit('src.html needs exactly one scene marker')
(here / 'index.html').write_text(page.replace('/*SCENE*/', scene), encoding='utf-8')
print(f"index.html: {(here / 'index.html').stat().st_size} bytes")
