#!/usr/bin/env python3
"""Build two self-contained browser ports from vendored original artwork."""
import base64
import json
from pathlib import Path
import re
import subprocess
import struct

ROOT = Path(__file__).resolve().parent


def read_constant(path, name):
    text = path.read_text()
    value = re.search(r'(?:export const ' + name + r'\s*=|export default)\s*([\s\S]+)', text)[1]
    decoder = json.JSONDecoder()
    return decoder.raw_decode(value.strip())[0]


def build(slug, kind, en, zh, cover_source):
    path = ROOT / slug
    art = {str(p.relative_to(path / 'art')): 'data:image/webp;base64,' + base64.b64encode(p.read_bytes()).decode()
           for p in sorted((path / 'art').rglob('*.webp'))}
    meta = {'kind': kind}
    if kind == 'world':
        meta['heroes'] = read_constant(path / 'upstream/heros-data.js', 'HEROES')
        meta['fronts'] = read_constant(path / 'upstream/monde-data.js', 'FRONTS')
        font = base64.b64encode((path / 'upstream/CinzelDecorative-Bold.ttf').read_bytes()).decode()
        font_css = "@font-face{font-family:Codecraft;src:url(data:font/ttf;base64," + font + ")}"
    else:
        meta['city'] = read_constant(path / 'upstream/manifest.js', '')
        font_css = ''
    licenses = (path / 'upstream/LICENSE').read_text()
    if kind == 'world':
        licenses += '\n' + (path / 'upstream/OFL-Cinzel.txt').read_text()
    # License text is inert HTML text; arbitrary source content cannot terminate a script.
    from html import escape
    content = (ROOT / 'herdr-scene.html').read_text().replace('<!-- FONT -->', '<style>' + font_css + '</style>')
    content = content.replace('<!-- LICENSE -->', escape(licenses))
    content = content.replace('<!-- DATA -->', '<script>const ART=' + json.dumps(art) + ';const META=' + json.dumps(meta) + ';</script>')
    content = content.replace('<!-- LOGIC -->', '<script>\n' + (ROOT / 'herdr-scene.js').read_text() + '\n</script>')
    (path / 'index.html').write_text(content)
    cover = path / 'cover.png'
    subprocess.run(['ffmpeg', '-y', '-hide_banner', '-loglevel', 'error', '-i', str(cover_source),
                    '-filter_complex', 'scale=76:76:force_original_aspect_ratio=decrease:flags=neighbor,pad=96:96:(ow-iw)/2:(oh-ih)/2:color=0x211a20,split[a][b];[a]palettegen=max_colors=24[p];[b][p]paletteuse=dither=none,format=rgba',
                    '-frames:v', '1', '-update', '1', str(cover)], check=True)
    # The package profile accepts only the critical PNG chunks.
    raw = cover.read_bytes()
    chunks, at = [raw[:8]], 8
    while at < len(raw):
        end = at + 12 + struct.unpack('>I', raw[at:at+4])[0]
        if raw[at+4:at+8] in [b'IHDR', b'IDAT', b'IEND']:
            chunks.append(raw[at:end])
        at = end
    cover.write_bytes(b''.join(chunks))
    manifest = {'manifestVersion': 1, 'id': 'com.clawdline.' + slug, 'version': '0.2.0', 'hostApi': 1,
                'name': {'en': en, 'zh-Hant': zh}, 'entry': 'index.html',
                'capabilities': ['sessions.read', 'sessions.open'],
                'cover': 'data:image/png;base64,' + base64.b64encode(cover.read_bytes()).decode()}
    (path / 'manifest.json').write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n')
    subprocess.run(['python3', str(ROOT.parent / 'tools/pack-view-plugin.py'), str(path),
                    str(ROOT.parent / 'web/console/public/view-plugins' / (slug + '.zip'))], check=True)


if __name__ == '__main__':
    build('session-codecraft', 'world', 'World of Codecraft', '奇幻村莊',
          ROOT / 'session-codecraft/art/art/heros/elf-mage/stand.webp')
    build('session-kaiju', 'kaiju', 'Kaiju City', '怪獸城市',
          ROOT / 'session-kaiju/art/assets/day/monsters/lizard/idle.webp')
