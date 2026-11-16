#!/usr/bin/env python3
"""Publish exact official archives and known shipped revisions without executing them."""
import argparse
import hashlib
import json
from pathlib import Path
import zipfile

ROOT = Path(__file__).resolve().parent.parent


def catalog():
    known = json.loads((ROOT / 'plugins/official-package-revisions.json').read_text())
    packages = []
    for identity, previous in sorted(known.items()):
        slug = identity.removeprefix('com.clawdline.')
        path = ROOT / 'web/console/public/view-plugins' / (slug + '.zip')
        raw = path.read_bytes()
        with zipfile.ZipFile(path) as archive:
            manifest = json.loads(archive.read('manifest.json'))
        source = json.loads((ROOT / 'plugins' / slug / 'manifest.json').read_text())
        if manifest != source or manifest['id'] != identity:
            raise ValueError(f'{slug}: archive and author manifest differ')
        digest = hashlib.sha256(raw).hexdigest()
        packages.append(dict(id=identity, version=manifest['version'],
                             url='/view-plugins/' + path.name, sha256=digest, bytes=len(raw),
                             hostApi=manifest['hostApi'], capabilities=manifest['capabilities'],
                             knownDigests=sorted(set(previous + [digest]))))
    return dict(schemaVersion=1, packages=packages)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    output = ROOT / 'web/console/public/view-plugins/catalog.json'
    rendered = json.dumps(catalog(), indent=2) + '\n'
    if args.check:
        expected_ledger = {item['id']: item['knownDigests'] for item in catalog()['packages']}
        saved_ledger = json.loads((ROOT / 'plugins/official-package-revisions.json').read_text())
        if saved_ledger != expected_ledger or not output.exists() or output.read_text() != rendered:
            raise SystemExit('Official view catalog is stale; run python3 tools/build-view-plugin-catalog.py')
        print('Official view catalog matches every shipped archive and author manifest')
    else:
        ledger = {item['id']: item['knownDigests'] for item in catalog()['packages']}
        (ROOT / 'plugins/official-package-revisions.json').write_text(json.dumps(ledger, indent=2) + '\n')
        output.write_text(rendered)
        print(output)
