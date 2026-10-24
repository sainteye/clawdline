#!/bin/bash
# Real disposable HFS+ images and HTTP processes; no real app or signing keys.
set -euo pipefail
portable=no
case "${1:-}" in
  --portable) portable=yes ;;
  '') ;;
  *) echo "usage: $0 [--portable]" >&2; exit 2 ;;
esac
if [ "$(uname -s)" != Darwin ] && [ "$portable" != yes ]; then
  echo 'SKIP DMG integration requires macOS (no release acceptance)'
  exit 0
fi
cd "$(dirname "$0")/../.."
smoke_script=${DMG_SMOKE_SCRIPT:-tools/release/dmg-install-smoke.sh}
work=$(mktemp -d "${TMPDIR:-/tmp}/dmg-harness-test.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
export DMG_TEST_WORK="$work"
export DMG_TEST_PYTHON="$(command -v python3)"
python3 - <<'PY'
import os
from pathlib import Path
import plistlib
root = Path(os.environ['DMG_TEST_WORK'])
app = root / 'source/Clawdline Next.app'
macos = app / 'Contents/MacOS'
resources = app / 'Contents/Resources'
macos.mkdir(parents=True)
resources.mkdir()
(app / 'Contents/Info.plist').write_bytes(plistlib.dumps({
    'CFBundleExecutable': 'Clawdline Next', 'CFBundleShortVersionString': '2.1.2',
    'LSMinimumSystemVersion': '13.0'}))
(resources / 'shell-revision').write_text('a' * 64 + '\n')
(root / 'source/Applications').symlink_to('/Applications')
# An executable fixture speaks the actual daemon version and HTTP interfaces.
daemon = macos / 'clawdline'
daemon.write_text('#!' + os.environ['DMG_TEST_PYTHON'] + '''
import http.server, json, os, sys
from pathlib import Path
commit = '1234567890abcdef1234567890abcdef12345678'
if sys.argv[1:] == ['version', '--json']:
    print(json.dumps({'version':'v2.1.2','commit':commit,'os':'darwin','arch':'arm64'}))
    sys.exit(0)
assert sys.argv[1:] == ['serve']
(Path(os.environ['CLAWDLINE_NEXT_DIR']) / 'local-token').write_text('fixture-local-credential')
behavior_file = Path(__file__).parent.parent / 'Resources/behavior'
behavior = behavior_file.read_text() if behavior_file.exists() else ''
if behavior == 'exit':
    sys.exit(4)
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/BUILD.json' and self.headers.get('Authorization') != 'Bearer fixture-local-credential':
            self.send_response(401)
            self.end_headers()
            return
        bodies = {'/v1/health':json.dumps({'ok': True, 'served_by':'clawdline-go'}),
                  '/':'<html>synthetic console</html>',
                  '/BUILD.json':json.dumps({'version':'v2.1.2', 'stamp':'wrong' if behavior == 'stamp' else commit})}
        self.send_response(503 if behavior == 'http' else (200 if self.path in bodies else 404))
        self.end_headers()
        self.wfile.write(bodies.get(self.path, '').encode())
http.server.HTTPServer(('127.0.0.1',int(os.environ['CLAWDLINE_NEXT_PORT'])),Handler).serve_forever()
''')
daemon.chmod(0o755)
shell = macos / 'Clawdline Next'
shell.write_text('''#!/bin/bash
set -eu
"$(dirname "$0")/clawdline" serve &
child=$!
trap 'kill "$child"; wait "$child" || true; exit 0' TERM INT
wait "$child"
''')
shell.chmod(0o755)
PY
if [ "$portable" = yes ]; then
  # Only platform integrations are doubled. Bundle processes, HTTP, version
  # comparison, refusal paths and cleanup execute the production harness body.
  cat >"$work/platform-double.py" <<'PY'
import os, pathlib, platform, runpy, shutil, subprocess, sys, types
from unittest.mock import patch
source = pathlib.Path(sys.argv[1]).read_text().split("<<'PY'\n", 1)[1].rsplit('\nPY', 1)[0]
sys.argv = ['dmg-smoke'] + sys.argv[2:]
real_run, real_popen, real_statvfs = subprocess.run, subprocess.Popen, os.statvfs
shell_pid = None
def platform_run(argv, **kwargs):
    if argv[0] == 'ps':
        return subprocess.CompletedProcess(argv, 0, b'' if '-axo' in argv else str(shell_pid).encode(), b'')
    if argv[0] == 'hdiutil':
        image = pathlib.Path(argv[-1])
        if argv[1] == 'verify':
            return subprocess.CompletedProcess(argv, 1 if image.read_text() == 'broken' else 0, b'', b'bad image')
        if argv[1] == 'attach':
            import plistlib
            mount = pathlib.Path(argv[argv.index('-mountpoint') + 1])
            fixture = image.parent / ('empty' if image.stem == 'missing' else 'source-' + image.stem if image.stem in ('stamp','exit','http','shortcut') else 'source')
            shutil.copytree(fixture, mount, dirs_exist_ok=True, symlinks=True)
            return subprocess.CompletedProcess(argv, 0, plistlib.dumps({'system-entities':[{'mount-point':str(mount)}]}), b'')
        return subprocess.CompletedProcess(argv, 0, b'', b'')
    if argv[0] == 'codesign':
        return subprocess.CompletedProcess(argv, 1, b'', b'unsigned fixture')
    if argv[0] == 'ditto':
        shutil.copytree(argv[1], argv[2], symlinks=True)
        return subprocess.CompletedProcess(argv, 0, b'', b'')
    if argv[0] == 'sandbox-exec':
        argv = argv[3:]
    return real_run(argv, **kwargs)
def platform_popen(argv, **kwargs):
    global shell_pid
    if argv[0] == 'sandbox-exec':
        argv = argv[3:]
    process = real_popen(argv, **kwargs)
    if argv[0].endswith('/Clawdline Next'):
        shell_pid = process.pid
    return process
real_which = shutil.which
def which(name):
    return '/platform-double/' + name if name in ('hdiutil','codesign','xcrun','spctl','ditto','sandbox-exec') else real_which(name)
with patch.object(platform, 'system', return_value='Darwin'), patch.object(platform, 'machine', return_value='arm64'), \
     patch.object(platform, 'mac_ver', return_value=('13.0','','')), patch.object(shutil, 'which', side_effect=which), \
     patch.object(subprocess, 'run', side_effect=platform_run), patch.object(subprocess, 'Popen', side_effect=platform_popen), \
     patch.object(os, 'statvfs', return_value=types.SimpleNamespace(f_flag=os.ST_RDONLY)):
    exec(compile(source, 'dmg-install-smoke.sh', 'exec'), {'__name__':'__main__'})
PY
  printf good >"$work/good.dmg"
else
  hdiutil create -srcfolder "$work/source" -volname ClawdlineTest -format UDZO "$work/good.dmg"
fi
mkdir "$work/empty"
ln -s /Applications "$work/empty/Applications"
if [ "$portable" = yes ]; then
  printf missing >"$work/missing.dmg"
else
  hdiutil create -srcfolder "$work/empty" -volname ClawdlineEmpty -format UDZO "$work/missing.dmg"
fi
printf broken >"$work/broken.dmg"
for behavior in stamp exit http shortcut; do
  cp -R "$work/source" "$work/source-$behavior"
  if [ "$behavior" = shortcut ]; then
    rm "$work/source-$behavior/Applications"
    ln -s /tmp "$work/source-$behavior/Applications"
  else
    printf '%s' "$behavior" >"$work/source-$behavior/Clawdline Next.app/Contents/Resources/behavior"
  fi
  if [ "$portable" = yes ]; then
    printf '%s' "$behavior" >"$work/$behavior.dmg"
  else
    hdiutil create -srcfolder "$work/source-$behavior" -volname "Clawdline-$behavior" -format UDZO "$work/$behavior.dmg"
  fi
done
commit=1234567890abcdef1234567890abcdef12345678
run_smoke() {
  local command=(bash "$smoke_script")
  if [ "$portable" = yes ]; then command=(python3 "$work/platform-double.py" "$smoke_script"); fi
  ${command[@]+"${command[@]}"} --dmg "$1" --version "$2" --commit "$3" \
    --work-dir "$work" --evidence-dir "$work/$4" --mode "$5" --timeout 2
}
expect_refusal() {
  local name=$1 pattern=$2
  shift 2
  if "$@" >"$work/$name.log" 2>&1; then
    echo "FAIL $name was accepted" >&2; exit 1
  fi
  if ! grep -Eq "$pattern" "$work/$name.log"; then
    cat "$work/$name.log" >&2; echo "FAIL $name refused for another reason" >&2; exit 1
  fi
  echo "PASS $name rejected for expected reason"
}
run_smoke "$work/good.dmg" v2.1.2 "$commit" good fixture
expect_refusal missing-app 'missing Clawdline Next.app' run_smoke "$work/missing.dmg" v2.1.2 "$commit" missing fixture
expect_refusal corrupt-image 'hdiutil failed' run_smoke "$work/broken.dmg" v2.1.2 "$commit" broken fixture
expect_refusal wrong-version 'wrong bundle version' run_smoke "$work/good.dmg" v9.9.9 "$commit" version fixture
expect_refusal wrong-commit 'wrong daemon commit' run_smoke "$work/good.dmg" v2.1.2 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa commit fixture
expect_refusal unsigned-release 'codesign failed' run_smoke "$work/good.dmg" v2.1.2 "$commit" unsigned release
expect_refusal wrong-console 'wrong console commit' run_smoke "$work/stamp.dmg" v2.1.2 "$commit" console fixture
expect_refusal failed-http 'HTTP Error 503' run_smoke "$work/http.dmg" v2.1.2 "$commit" http fixture
expect_refusal failed-shell 'bundle shell exited before readiness' run_smoke "$work/exit.dmg" v2.1.2 "$commit" exit fixture
expect_refusal wrong-shortcut 'missing Applications shortcut' run_smoke "$work/shortcut.dmg" v2.1.2 "$commit" shortcut fixture
python3 - <<'PY'
import json, os
from pathlib import Path
root = Path(os.environ['DMG_TEST_WORK'])
for name in ('good', 'missing', 'broken', 'version', 'commit', 'unsigned', 'console', 'http', 'exit', 'shortcut'):
    result = json.loads((root / name / 'result.json').read_text())
    assert result['cleanup'], (name, result)
assert not list(root.glob('dmg-smoke-*')), 'owned state/mount directories leaked'
print('PASS every arm cleaned its owned mount and state')
PY
echo 'PASS DMG harness: 1 positive, 9 negative controls; no release acceptance'
