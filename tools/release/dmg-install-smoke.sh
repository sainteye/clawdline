#!/bin/bash
# Repeatable, isolated macOS DMG acceptance. No setup, launchd, or UI scripting.
set -euo pipefail
exec python3 - "$@" <<'PY'
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import plistlib
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def run(*args):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
    with (evidence / 'commands.jsonl').open('a') as commands:
        commands.write(json.dumps({'argv': args, 'exit': result.returncode,
            'stdout': result.stdout.decode(errors='replace'),
            'stderr': result.stderr.decode(errors='replace')}) + '\n')
    if result.returncode:
        raise RuntimeError(f'{args[0]} failed ({result.returncode}): '
                           + result.stderr.decode(errors='replace')[-4000:])
    return result.stdout


parser = argparse.ArgumentParser(description='Copy and launch a DMG in a disposable Applications directory.')
parser.add_argument('--dmg', required=True)
parser.add_argument('--version', required=True, help='Expected release version, e.g. v2.1.2')
parser.add_argument('--commit', required=True, help='Expected full console and daemon commit')
parser.add_argument('--work-dir', required=True, help='Existing task-owned temporary parent')
parser.add_argument('--evidence-dir', required=True, help='New directory; logs survive cleanup')
parser.add_argument('--mode', choices=('release', 'fixture'), default='release',
                    help='fixture skips Apple trust checks and is never release acceptance')
parser.add_argument('--timeout', type=float, default=60, help='Readiness deadline in seconds')
args = parser.parse_args()
evidence = Path(args.evidence_dir).resolve()
require(not evidence.exists(), 'evidence directory must be new')
evidence.mkdir(parents=True)
report = {'mode': args.mode, 'accepted': False, 'checks': [], 'cleanup': False}
root = None
mounted = False
shell = None
daemon_pid = None
log = None
exit_code = 1


def check(name):
    report['checks'].append(name)
    print('PASS ' + name, flush=True)


def interrupted(signum, frame):
    raise RuntimeError(f'interrupted by signal {signum}')


signal.signal(signal.SIGTERM, interrupted)
signal.signal(signal.SIGINT, interrupted)
try:
    require(platform.system() == 'Darwin', 'DMG acceptance requires macOS')
    require(platform.machine() == 'arm64', 'DMG acceptance requires Apple Silicon')
    require(int(platform.mac_ver()[0].split('.')[0]) >= 13, 'macOS 13 or newer is required')
    for tool in ('hdiutil', 'codesign', 'xcrun', 'spctl', 'ditto', 'sandbox-exec', 'lsof'):
        require(shutil.which(tool), 'missing tool: ' + tool)
    require(args.timeout > 0, 'timeout must be positive')
    require(len(args.commit) == 40 and all(c in '0123456789abcdef' for c in args.commit),
            'commit must be a full lowercase Git SHA')
    dmg = Path(args.dmg).resolve(strict=True)
    parent = Path(args.work_dir).resolve(strict=True)
    root = Path(tempfile.mkdtemp(prefix='dmg-smoke-', dir=parent))
    mount = root / 'volume'
    mount.mkdir()
    digest = hashlib.sha256()
    with dmg.open('rb') as image:
        for block in iter(lambda: image.read(1024 * 1024), b''):
            digest.update(block)
    report['sha256'] = digest.hexdigest()
    # Pin subprocess output locale: plist parsing never depends on translated text.
    os.environ['LC_ALL'] = 'C'
    (evidence / 'verify.txt').write_bytes(run('hdiutil', 'verify', str(dmg)))
    check('image integrity')
    attached = run('hdiutil', 'attach', '-readonly', '-nobrowse', '-noautoopen',
                   '-plist', '-mountpoint', str(mount), str(dmg))
    mounted = True
    (evidence / 'attach.plist').write_bytes(attached)
    entities = plistlib.loads(attached)['system-entities']
    require(any(Path(e.get('mount-point', '/')) == mount for e in entities),
            'image did not mount at the owned mountpoint')
    # Ask the mounted filesystem, not just whether -readonly was requested.
    require(bool(os.statvfs(mount).f_flag & os.ST_RDONLY), 'image is not mounted read-only')
    check('read-only mount')
    app = mount / 'Clawdline Next.app'
    require(app.is_dir() and not app.is_symlink(), 'missing Clawdline Next.app')
    link = mount / 'Applications'
    require(link.is_symlink() and os.readlink(link) == '/Applications',
            'missing Applications shortcut to /Applications')
    info = plistlib.loads((app / 'Contents/Info.plist').read_bytes())
    require(info.get('CFBundleShortVersionString', '').lstrip('v') == args.version.lstrip('v'),
            'wrong bundle version')
    require(info.get('LSMinimumSystemVersion') == '13.0', 'bundle must declare macOS 13.0')
    require(info.get('CFBundleExecutable') == 'Clawdline Next', 'unexpected shell executable')
    check('bundle layout and version')
    if args.mode == 'release':
        (evidence / 'dmg-signature.txt').write_bytes(run('codesign', '--verify', '--strict', str(dmg)))
        (evidence / 'app-signature.txt').write_bytes(run('codesign', '--verify', '--deep', '--strict', str(app)))
        for name, target in (('dmg', dmg), ('app', app)):
            (evidence / (name + '-ticket.txt')).write_bytes(run('xcrun', 'stapler', 'validate', str(target)))
        (evidence / 'gatekeeper.txt').write_bytes(run('spctl', '--assess', '--type', 'execute', '--verbose=4', str(app)))
        (evidence / 'dmg-gatekeeper.txt').write_bytes(run('spctl', '--assess', '--type', 'open', '--context', 'context:primary-signature', '--verbose=4', str(dmg)))
        check('Apple signatures, stapled tickets and Gatekeeper')
    else:
        check('fixture only: Apple trust NOT assessed')
    applications = root / 'Applications'
    applications.mkdir()
    installed = applications / app.name
    run('ditto', str(app), str(installed))
    check('copy to disposable Applications')
    state = root / 'state'
    home = root / 'home'
    temporary = root / 'tmp'
    for directory in (state, home, temporary, home / '.claude', home / '.codex'):
        directory.mkdir(parents=True, exist_ok=True)
    # Explicitly disable real terminal discovery, independently of empty HOME.
    (state / 'config.json').write_text(json.dumps({'iterm_integration': False,
        'iterm_scan': False, 'session_auto_discovery': False, 'hotkey': ''}))
    with socket.socket() as reservation:
        reservation.bind(('127.0.0.1', 0))
        port = reservation.getsockname()[1]
    require(port != 7727, 'refusing production port')
    report['port'] = port
    # TMUX_TMPDIR isolates a system tmux as well as the bundled private socket.
    env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'HOME': str(home),
        'CFFIXED_USER_HOME': str(home), 'XDG_CONFIG_HOME': str(home / '.config'),
        'CLAUDE_CONFIG_DIR': str(home / '.claude'), 'CODEX_HOME': str(home / '.codex'),
        'TMPDIR': str(temporary) + '/', 'TMUX_TMPDIR': str(temporary),
        'CLAWDLINE_NEXT_DIR': str(state), 'CLAWDLINE_NEXT_PORT': str(port),
        'CLAWDLINE_NEXT_HOST': '127.0.0.1', 'LC_ALL': 'C'}
    original_home = str(Path.home().resolve())
    # HOME is insufficient for Foundation's OS-account home resolution. Deny the
    # original home except this test's owned tree. No personal transcript reads.
    profile = root / 'isolation.sb'
    profile.write_text('(version 1)\n(allow default)\n'
        '(deny file-read* file-write* (require-all (subpath ' + json.dumps(original_home)
        + ') (require-not (subpath ' + json.dumps(str(root)) + '))))\n'
        '(deny process-exec (literal "/usr/bin/osascript"))\n')
    binary = installed / 'Contents/MacOS/clawdline'
    sandbox = ['sandbox-exec', '-f', str(profile)]
    version = json.loads(subprocess.check_output(sandbox + [str(binary), 'version', '--json'], env=env, timeout=30))
    require(version.get('version', '').lstrip('v') == args.version.lstrip('v'), 'wrong daemon version')
    require(version.get('commit') == args.commit and version.get('arch') == 'arm64'
            and version.get('os') == 'darwin', 'wrong daemon commit or platform')
    (evidence / 'version.json').write_text(json.dumps(version, indent=2) + '\n')
    revision = (installed / 'Contents/Resources/shell-revision').read_text().strip()
    require(len(revision) == 64 and all(c in '0123456789abcdef' for c in revision), 'missing shell revision')
    report['shell_revision'] = revision
    check('bundled daemon identity')
    log = (evidence / 'launch.log').open('wb')
    shell = subprocess.Popen(sandbox + [str(installed / 'Contents/MacOS/Clawdline Next')],
                             env=env, cwd=root, stdout=log, stderr=subprocess.STDOUT)
    report['shell_pid'] = shell.pid
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def fetch(path):
        request = urllib.request.Request(f'http://127.0.0.1:{port}' + path)
        if path == '/BUILD.json':
            # Read only the credential created inside this disposable state.
            # It stays in process memory and is never included in evidence.
            token = (state / 'local-token').read_text().strip()
            require(bool(token), 'isolated daemon has no local credential')
            request.add_header('Authorization', 'Bearer ' + token)
        with opener.open(request, timeout=1) as response:
            require(response.status == 200, path + ' did not answer 200')
            return response.read()

    deadline = time.monotonic() + args.timeout
    last = ''
    while time.monotonic() < deadline:
        require(shell.poll() is None, 'bundle shell exited before readiness; see launch.log')
        try:
            health = json.loads(fetch('/v1/health'))
            require(health.get('ok') is True and health.get('served_by') == 'clawdline-go', 'unhealthy or foreign daemon')
            page = fetch('/')
            require(b'<html' in page.lower(), 'console response is not HTML')
            stamp = json.loads(fetch('/BUILD.json'))
            require(stamp.get('stamp') == args.commit, 'wrong console commit')
            require(stamp.get('version', '').lstrip('v') == args.version.lstrip('v'), 'wrong console version')
            break
        except Exception as error:
            last = str(error)
            # Poll the readiness condition with a deadline; never a fixed startup sleep.
            import select
            select.select([], [], [], min(0.1, max(0, deadline - time.monotonic())))
    else:
        raise RuntimeError('readiness deadline: ' + last)
    # Record and stop only the listener actually launched by this shell.
    listener = run('lsof', '-nP', '-t', f'-iTCP:{port}', '-sTCP:LISTEN').decode().split()
    require(len(set(listener)) == 1, 'expected exactly one listener')
    daemon_pid = int(listener[0])
    ppid = int(run('ps', '-o', 'ppid=', '-p', str(daemon_pid)).strip())
    require(ppid == shell.pid, 'listener is not the bundle shell child')
    report['daemon_pid'] = daemon_pid
    (evidence / 'health.json').write_text(json.dumps(health, indent=2) + '\n')
    (evidence / 'BUILD.json').write_text(json.dumps(stamp, indent=2) + '\n')
    check('shell-owned daemon, health, console HTTP 200 and matching BUILD.json')
    report['accepted'] = True
    exit_code = 0
except Exception as error:
    report['error'] = str(error)
    print('FAIL ' + str(error), file=sys.stderr)
finally:
    errors = []
    if shell is not None:
        # Discover only direct children with the exact copied daemon executable,
        # before terminating their parent; readiness might have failed earlier.
        owned = []
        if shell.poll() is None:
            try:
                rows = run('ps', '-axo', 'pid=,ppid=,comm=').decode().splitlines()
                for row in rows:
                    fields = row.strip().split(None, 2)
                    if len(fields) == 3 and int(fields[1]) == shell.pid and fields[2] == str(binary):
                        owned.append(int(fields[0]))
            except Exception as error:
                errors.append('cannot inventory owned children: ' + str(error))
        if shell.poll() is None:
            shell.terminate()
            try:
                shell.wait(timeout=15)
            except subprocess.TimeoutExpired:
                shell.kill()
                shell.wait(timeout=5)
        # The shell normally reaps its child. If it failed to do so, verify the
        # exact executable again before signalling any PID captured above.
        for pid in set(owned + ([daemon_pid] if daemon_pid is not None else [])):
            try:
                os.kill(pid, 0)
                executable = run('ps', '-o', 'comm=', '-p', str(pid)).decode().strip()
                if executable == str(binary):
                    os.kill(pid, signal.SIGTERM)
                    errors.append('shell required manual daemon cleanup: ' + str(pid))
                else:
                    errors.append('PID identity changed; not signalled: ' + str(pid))
            except ProcessLookupError:
                pass
            except Exception as error:
                errors.append('cannot verify owned PID cleanup: ' + str(error))
        if log:
            log.close()
    if root is not None:
        for name in ('daemon.log', 'shell.log'):
            source = root / 'state' / name
            if source.is_file():
                shutil.copy2(source, evidence / name)
        if mounted or os.path.ismount(root / 'volume'):
            try:
                run('hdiutil', 'detach', str(root / 'volume'))
            except Exception as error:
                errors.append(str(error))
        if not errors:
            shutil.rmtree(root)
        else:
            report['retained_work_dir'] = str(root)
    report['cleanup'] = not errors
    if errors:
        report['cleanup_errors'] = errors
        report['accepted'] = False
        exit_code = 1
    (evidence / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
sys.exit(exit_code)
PY
