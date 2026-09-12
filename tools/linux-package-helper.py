#!/usr/bin/env python3
"""Descriptor-bound and crash-durable primitives for linux-package.sh.

The shell owns policy and OpenSSL invocation. This helper owns operations which cannot be made
safe with pathname-only shell utilities: immutable input snapshots, exact extraction, service
state creation beneath an untrusted service-owned directory, fsync, and journaled link pairs.
"""

import argparse
import base64
import hashlib
import json
import os
import re
import stat
import subprocess
import sys
import tarfile
import tempfile

EXPECTED_MODES = {
    "bin/ClawdlineLinux": 0o755,
    "bin/clawdline-daemon-wrapper": 0o755,
    "lib/systemd/system/clawdline-daemon.service": 0o644,
    "lib/systemd/system/clawdline-tmux.service": 0o644,
    "lib/sysusers.d/clawdline.conf": 0o644,
    "release.env": 0o644,
    "share/clawdline/daemon.json.in": 0o644,
}
MANIFEST_PATH = "share/clawdline/release-manifest.json"
EXPECTED_FILES = set(EXPECTED_MODES) | {MANIFEST_PATH}
DERIVED_MODES = {"verified.env": 0o644, "verified-provenance.json": 0o600}
EXPECTED_DIRECTORIES = {
    "bin", "lib", "lib/systemd", "lib/systemd/system", "lib/sysusers.d",
    "share", "share/clawdline",
}
TARGET_RE = re.compile(r"releases/[0-9]+\.[0-9]+\.[0-9]+(?:[.+-][A-Za-z0-9.-]+)?-[0-9a-f]{16}")


def fail(message):
    raise SystemExit("linux-package-helper: " + message)


def crash_at(point):
    if os.environ.get("CLAWDLINE_TRANSITION_CRASH_AT") == point:
        os._exit(99)


def fsync_directory_fd(descriptor):
    os.fsync(descriptor)


def fsync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        fsync_directory_fd(descriptor)
    finally:
        os.close(descriptor)


def split_safe(path):
    if not path:
        fail("empty path")
    absolute = os.path.isabs(path)
    parts = path.split("/")
    if absolute:
        parts = parts[1:]
    if not parts or any(part in ("", ".", "..") for part in parts):
        fail("path contains an empty, dot, or traversal component")
    return absolute, parts


def open_pinned(path):
    absolute, parts = split_safe(path)
    descriptor = os.open("/" if absolute else ".",
                         os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        for component in parts[:-1]:
            child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                            dir_fd=descriptor)
            metadata = os.fstat(child)
            if not stat.S_ISDIR(metadata.st_mode):
                os.close(child)
                fail("input ancestor is not a directory")
            os.close(descriptor)
            descriptor = child
        result = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC,
                         dir_fd=descriptor)
    finally:
        os.close(descriptor)
    metadata = os.fstat(result)
    if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1:
        os.close(result)
        fail("input must be one non-hardlinked regular file")
    return result, metadata


def read_fd(descriptor, maximum=1 << 30):
    chunks = []
    total = 0
    while True:
        chunk = os.read(descriptor, 1024 * 1024)
        if not chunk:
            break
        total += len(chunk)
        if total > maximum:
            fail("bounded input is too large")
        chunks.append(chunk)
    return b"".join(chunks)


def atomic_bytes(directory, name, content, mode=0o600):
    directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    temporary = ".%s.%s.tmp" % (name, next(tempfile._get_candidate_names()))
    descriptor = None
    try:
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                             | os.O_NOFOLLOW | os.O_CLOEXEC, mode, dir_fd=directory_fd)
        offset = 0
        while offset < len(content):
            offset += os.write(descriptor, content[offset:])
        os.fchmod(descriptor, mode)
        os.fsync(descriptor)
        os.close(descriptor)
        descriptor = None
        os.rename(temporary, name, src_dir_fd=directory_fd, dst_dir_fd=directory_fd)
        fsync_directory_fd(directory_fd)
    finally:
        if descriptor is not None:
            os.close(descriptor)
        try:
            os.unlink(temporary, dir_fd=directory_fd)
        except FileNotFoundError:
            pass
        os.close(directory_fd)


def command_snapshot(args):
    os.mkdir(args.directory, 0o700)
    directory_fd = os.open(args.directory,
                           os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fchmod(directory_fd, 0o700)
        outputs = {}
        for name, source in (("archive", args.archive), ("provenance", args.provenance),
                             ("signature", args.signature), ("public-key", args.public_key)):
            source_fd, metadata = open_pinned(source)
            try:
                content = read_fd(source_fd)
                restat = os.fstat(source_fd)
                if (metadata.st_dev, metadata.st_ino, metadata.st_size, metadata.st_mtime_ns) != (
                        restat.st_dev, restat.st_ino, restat.st_size, restat.st_mtime_ns):
                    fail("input changed while its pinned descriptor was copied")
            finally:
                os.close(source_fd)
            target_fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                                | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory_fd)
            try:
                offset = 0
                while offset < len(content):
                    offset += os.write(target_fd, content[offset:])
                os.fsync(target_fd)
            finally:
                os.close(target_fd)
            outputs[name.replace("-", "_")] = os.path.join(args.directory, name)
        fsync_directory_fd(directory_fd)
    finally:
        os.close(directory_fd)
    print(json.dumps(outputs, sort_keys=True, separators=(",", ":")))


def load_json(path, maximum=8 * 1024 * 1024):
    descriptor, _ = open_pinned(path)
    try:
        return json.loads(read_fd(descriptor, maximum))
    finally:
        os.close(descriptor)


def validate_manifest(root, provenance, require_owner=True):
    manifest = load_json(os.path.join(root, MANIFEST_PATH))
    keys = {"schemaVersion", "packageVersion", "buildIdentity", "sourceCommit", "architecture",
            "configurationSchemaVersion", "configurationReadableMinimum", "durableSchema",
            "protocolIdentity", "files"}
    if set(manifest) != keys or manifest["schemaVersion"] != 1:
        fail("internal release manifest has unknown or missing fields")
    for name in ("packageVersion", "buildIdentity", "sourceCommit", "architecture",
                 "configurationSchemaVersion", "configurationReadableMinimum", "durableSchema",
                 "protocolIdentity"):
        if manifest[name] != provenance[name]:
            fail("internal and signed release identities differ: " + name)
    if set(manifest["files"]) != set(EXPECTED_MODES):
        fail("internal payload inventory is not exact")
    actual_files = set()
    actual_directories = set()
    for base, directories, files in os.walk(root, followlinks=False):
        for name in directories:
            path = os.path.join(base, name)
            rel = os.path.relpath(path, root)
            metadata = os.lstat(path)
            if not stat.S_ISDIR(metadata.st_mode) or stat.S_IMODE(metadata.st_mode) != 0o755:
                fail("release contains an unsafe directory or mode: " + rel)
            if require_owner and metadata.st_uid != os.geteuid():
                fail("release directory owner is not the installer identity: " + rel)
            actual_directories.add(rel)
        for name in files:
            path = os.path.join(base, name)
            rel = os.path.relpath(path, root)
            metadata = os.lstat(path)
            wanted_mode = (0o644 if rel == MANIFEST_PATH else
                           EXPECTED_MODES.get(rel, DERIVED_MODES.get(rel)))
            if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1 or wanted_mode is None \
                    or stat.S_IMODE(metadata.st_mode) != wanted_mode:
                fail("release contains an unsafe file/type/link/mode: " + rel)
            if require_owner and metadata.st_uid != os.geteuid():
                fail("release file owner is not the installer identity: " + rel)
            actual_files.add(rel)
    valid_file_sets = (EXPECTED_FILES, EXPECTED_FILES | set(DERIVED_MODES))
    if actual_files not in valid_file_sets or actual_directories != EXPECTED_DIRECTORIES:
        fail("release payload topology is not exact")
    for path, wanted in manifest["files"].items():
        descriptor, _ = open_pinned(os.path.join(root, path))
        try:
            got = hashlib.sha256(read_fd(descriptor)).hexdigest()
        finally:
            os.close(descriptor)
        if got != wanted:
            fail("payload digest mismatch: " + path)
    return manifest


def command_extract(args):
    provenance = load_json(args.provenance)
    archive_fd, _ = open_pinned(args.archive)
    archive_file = os.fdopen(archive_fd, "rb", closefd=True)
    try:
        with tarfile.open(fileobj=archive_file, mode="r:gz") as archive:
            members = archive.getmembers()
            names = [member.name.rstrip("/") for member in members]
            if len(names) != len(set(names)):
                fail("archive contains duplicate members")
            if set(names) != EXPECTED_DIRECTORIES | EXPECTED_FILES:
                fail("archive member inventory is not exact")
            os.mkdir(args.destination, 0o755)
            for name in sorted(EXPECTED_DIRECTORIES, key=lambda value: (value.count("/"), value)):
                os.mkdir(os.path.join(args.destination, name), 0o755)
            by_name = {member.name.rstrip("/"): member for member in members}
            for name in EXPECTED_DIRECTORIES:
                member = by_name[name]
                if not member.isdir() or member.uid != 0 or member.gid != 0 or member.mode != 0o755:
                    fail("archive directory metadata is unsafe: " + name)
            for name in sorted(EXPECTED_FILES):
                member = by_name[name]
                wanted_mode = 0o644 if name == MANIFEST_PATH else EXPECTED_MODES[name]
                if not member.isreg() or member.uid != 0 or member.gid != 0 \
                        or member.mode != wanted_mode:
                    fail("archive file metadata is unsafe: " + name)
                extracted = archive.extractfile(member)
                if extracted is None:
                    fail("archive regular file cannot be read: " + name)
                content = extracted.read()
                target = os.path.join(args.destination, name)
                descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                                     | os.O_NOFOLLOW | os.O_CLOEXEC, wanted_mode)
                try:
                    offset = 0
                    while offset < len(content):
                        offset += os.write(descriptor, content[offset:])
                    os.fchmod(descriptor, wanted_mode)
                    os.fsync(descriptor)
                finally:
                    os.close(descriptor)
    finally:
        archive_file.close()
    validate_manifest(args.destination, provenance)
    for directory in sorted(EXPECTED_DIRECTORIES, key=lambda value: value.count("/"), reverse=True):
        fsync_directory(os.path.join(args.destination, directory))
    fsync_directory(args.destination)


def command_verify_release(args):
    validate_manifest(args.release, load_json(args.provenance))


def command_state_compatible(args):
    loaded_from_legacy = False
    try:
        descriptor, metadata = open_pinned(args.state)
    except FileNotFoundError:
        if not args.legacy_state:
            if args.require_authority:
                fail("durable authority disappeared before rollback revalidation")
            return
        try:
            descriptor, metadata = open_pinned(args.legacy_state)
        except FileNotFoundError:
            if args.require_authority:
                fail("durable authority disappeared before rollback revalidation")
            return
        loaded_from_legacy = True
    if metadata.st_uid != args.expected_uid:
        os.close(descriptor)
        fail("durable state owner is not the configured service uid")
    if stat.S_IMODE(metadata.st_mode) & 0o077:
        os.close(descriptor)
        fail("durable state permissions are not private")
    if metadata.st_size <= 0 or metadata.st_size > 8 * 1024 * 1024:
        os.close(descriptor)
        fail("durable state size is outside the daemon bound")
    try:
        state = json.loads(read_fd(descriptor, 8 * 1024 * 1024))
    finally:
        os.close(descriptor)
    if not isinstance(state, dict) or isinstance(state.get("schemaVersion"), bool) \
            or not isinstance(state.get("schemaVersion"), int):
        fail("durable state has no authoritative schema identity")
    if "recordKind" in state:
        if state.get("recordKind") == "clawdline_linux_task_authority_rollback_fence":
            fail("package rollback fence is not task authority")
        fail("durable task authority has an unsupported recordKind")
    if loaded_from_legacy and state["schemaVersion"] >= 3:
        fail("legacy fallback accepts only pre-migration task authority")
    minimum_reader = state.get("minimumReaderVersion", 1)
    if isinstance(minimum_reader, bool) or not isinstance(minimum_reader, int) or minimum_reader < 1:
        fail("durable state has no authoritative minimum-reader identity")
    manifest = load_json(args.manifest)
    schema = manifest["durableSchema"]
    if not schema["readMinimum"] <= state["schemaVersion"] <= schema["readMaximum"]:
        fail("image cannot read the current durable state; image rollback is not database rollback")
    if minimum_reader > schema["readMaximum"]:
        fail("image is older than the durable state's minimum reader")


def ensure_directory(parent_fd, name, mode, uid, gid, manage=True):
    created = False
    try:
        os.mkdir(name, mode, dir_fd=parent_fd)
        created = True
    except FileExistsError:
        pass
    descriptor = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                         dir_fd=parent_fd)
    metadata = os.fstat(descriptor)
    if not stat.S_ISDIR(metadata.st_mode):
        os.close(descriptor)
        fail("protected path component is not a directory")
    if metadata.st_uid not in (0, uid) or metadata.st_mode & 0o022:
        os.close(descriptor)
        fail("protected path component has an unexpected owner")
    if manage or created:
        os.fchmod(descriptor, mode)
        os.fchown(descriptor, uid, gid)
        fsync_directory_fd(descriptor)
        fsync_directory_fd(parent_fd)
    return descriptor


def open_root(path):
    metadata = os.lstat(path)
    if not stat.S_ISDIR(metadata.st_mode):
        fail("install root must be a real directory")
    return os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)


def command_prepare_state(args):
    root_fd = open_root(args.install_root)
    opened = [root_fd]
    try:
        var_fd = ensure_directory(root_fd, "var", 0o755, os.geteuid(), os.getegid(), False); opened.append(var_fd)
        lib_fd = ensure_directory(var_fd, "lib", 0o755, os.geteuid(), os.getegid(), False); opened.append(lib_fd)
        state_fd = ensure_directory(lib_fd, "clawdline", 0o700, args.uid, args.gid); opened.append(state_fd)
        secrets_fd = ensure_directory(state_fd, "secrets", 0o700, args.uid, args.gid); opened.append(secrets_fd)
        before = os.fstat(secrets_fd)
        hook = os.environ.get("CLAWDLINE_STATE_AFTER_OPEN_HOOK")
        if hook:
            subprocess.run([hook, args.install_root], check=True)
        try:
            secret_fd = os.open("daemon.secret", os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                                dir_fd=secrets_fd)
            metadata = os.fstat(secret_fd)
            if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1 \
                    or metadata.st_uid != args.uid or metadata.st_gid != args.gid \
                    or stat.S_IMODE(metadata.st_mode) != 0o600:
                os.close(secret_fd)
                fail("existing daemon secret is not one owned non-hardlinked 0600 file")
            os.close(secret_fd)
        except FileNotFoundError:
            secret_fd = os.open("daemon.secret", os.O_WRONLY | os.O_CREAT | os.O_EXCL
                                | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=secrets_fd)
            try:
                os.write(secret_fd, os.urandom(48))
                os.fchmod(secret_fd, 0o600)
                os.fchown(secret_fd, args.uid, args.gid)
                os.fsync(secret_fd)
            finally:
                os.close(secret_fd)
            fsync_directory_fd(secrets_fd)

        rebound = os.open("secrets", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                          dir_fd=state_fd)
        try:
            original_metadata = os.fstat(secrets_fd)
            rebound_metadata = os.fstat(rebound)
            if (original_metadata.st_dev, original_metadata.st_ino) != (
                    rebound_metadata.st_dev, rebound_metadata.st_ino):
                fail("service-owned secrets directory changed during descriptor-bound update")
        finally:
            os.close(rebound)
        replacement = os.open("secrets", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                              dir_fd=state_fd)
        try:
            after = os.fstat(replacement)
            if (before.st_dev, before.st_ino) != (after.st_dev, after.st_ino):
                fail("service-owned secrets directory changed during descriptor-bound preparation")
        finally:
            os.close(replacement)

        project_fd = ensure_directory(lib_fd, "clawdline-projects", 0o700,
                                      args.uid, args.gid); opened.append(project_fd)
        etc_fd = ensure_directory(root_fd, "etc", 0o755, os.geteuid(), os.getegid(), False); opened.append(etc_fd)
        config_fd = ensure_directory(etc_fd, "clawdline", 0o750,
                                     os.geteuid(), args.gid); opened.append(config_fd)
        try:
            existing = os.open("daemon.json", os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                               dir_fd=config_fd)
            metadata = os.fstat(existing)
            os.close(existing)
            if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1:
                fail("existing daemon configuration is unsafe")
        except FileNotFoundError:
            # Re-encode only after exact substitutions, avoiding sed/pathname writes as root.
            raw_fd, _ = open_pinned(args.template)
            try:
                raw = read_fd(raw_fd, 64 * 1024).decode("utf-8")
            finally:
                os.close(raw_fd)
            content = raw.replace("@SERVICE_UID@", str(args.uid)).replace(
                "@SERVICE_GID@", str(args.gid)).encode("utf-8")
            temporary = ".daemon.%s.tmp" % next(tempfile._get_candidate_names())
            output = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                             | os.O_NOFOLLOW | os.O_CLOEXEC, 0o640, dir_fd=config_fd)
            try:
                os.write(output, content)
                os.fchmod(output, 0o640)
                os.fchown(output, os.geteuid(), args.gid)
                os.fsync(output)
            finally:
                os.close(output)
            os.rename(temporary, "daemon.json", src_dir_fd=config_fd, dst_dir_fd=config_fd)
            fsync_directory_fd(config_fd)
    finally:
        for descriptor in reversed(opened):
            os.close(descriptor)


def target_or_none(value):
    if value == "none":
        return None
    if not TARGET_RE.fullmatch(value):
        fail("release link target is outside releases/<version-digest>")
    return value


def proc_start(pid):
    try:
        text = open("/proc/%d/stat" % pid, "r", encoding="utf-8").read()
    except (FileNotFoundError, PermissionError):
        return None
    close = text.rfind(")")
    fields = text[close + 2:].split()
    return fields[19] if close >= 0 and len(fields) > 19 else None


def atomic_json(path, value, mode=0o600):
    directory, name = os.path.split(path)
    atomic_bytes(directory, name,
                 (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode(), mode)


def atomic_link(prefix, name, target):
    target = target_or_none(target) if target is not None else None
    temporary = ".link.%s.tmp" % next(tempfile._get_candidate_names())
    if target is None:
        try:
            os.unlink(os.path.join(prefix, name))
        except FileNotFoundError:
            pass
    else:
        os.symlink(target, os.path.join(prefix, temporary))
        os.replace(os.path.join(prefix, temporary), os.path.join(prefix, name))
    fsync_directory(prefix)


def restore_journal(prefix, journal):
    atomic_link(prefix, "previous", journal.get("oldPrevious"))
    atomic_link(prefix, "current", journal.get("oldCurrent"))
    try:
        os.unlink(os.path.join(prefix, "transition.json"))
    except FileNotFoundError:
        pass
    fsync_directory(prefix)


def complete_journal(prefix, journal):
    atomic_link(prefix, "previous", journal.get("newPrevious"))
    atomic_link(prefix, "current", journal.get("newCurrent"))
    if "receipt" in journal:
        receipts = os.path.join(prefix, "receipts")
        os.makedirs(receipts, mode=0o755, exist_ok=True)
        atomic_json(os.path.join(receipts, "last-install.json"), journal["receipt"])
    try:
        os.unlink(os.path.join(prefix, "transition.json"))
    except FileNotFoundError:
        pass
    fsync_directory(prefix)


def remove_lock(prefix):
    lock = os.path.join(prefix, ".package-lock")
    try:
        os.unlink(os.path.join(lock, "owner.json"))
        fsync_directory(lock)
    except FileNotFoundError:
        pass
    try:
        os.rmdir(lock)
    except FileNotFoundError:
        pass
    fsync_directory(prefix)


def recover_stale(prefix):
    lock = os.path.join(prefix, ".package-lock")
    owner_path = os.path.join(lock, "owner.json")
    if not os.path.isdir(lock):
        return
    try:
        owner = load_json(owner_path)
    except Exception:
        owner = {}
    pid = owner.get("pid")
    start = owner.get("startTime")
    if isinstance(pid, int) and isinstance(start, str) and proc_start(pid) == start:
        fail("another live package transition owns the install root")
    journal_path = os.path.join(prefix, "transition.json")
    if os.path.exists(journal_path):
        journal = load_json(journal_path)
        if journal.get("phase") == "recovery_required":
            recovery = journal.get("recovery", {})
            fail("transition requires operator recovery: "
                 + str(recovery.get("code", "package_recovery_required")))
        if journal.get("phase") == "committing":
            complete_journal(prefix, journal)
        else:
            restore_journal(prefix, journal)
    remove_lock(prefix)


def require_lock(prefix, owner_pid):
    owner = load_json(os.path.join(prefix, ".package-lock", "owner.json"))
    actual_start = proc_start(owner_pid)
    if owner.get("pid") != owner_pid or owner.get("startTime") != actual_start:
        fail("transition lock owner identity changed "
             "(expected pid=%r start=%r, actual pid=%r start=%r)" % (
                 owner.get("pid"), owner.get("startTime"), owner_pid, actual_start))
    return owner


def command_transition(args):
    prefix = args.prefix
    os.makedirs(prefix, mode=0o755, exist_ok=True)
    metadata = os.lstat(prefix)
    if not stat.S_ISDIR(metadata.st_mode):
        fail("package prefix is not a real directory")
    if args.action == "acquire":
        recover_stale(prefix)
        journal_path = os.path.join(prefix, "transition.json")
        if os.path.exists(journal_path):
            journal = load_json(journal_path)
            if journal.get("phase") == "recovery_required":
                recovery = journal.get("recovery", {})
                fail("transition requires operator recovery: "
                     + str(recovery.get("code", "package_recovery_required")))
            fail("an unowned package transition journal requires recovery")
        lock = os.path.join(prefix, ".package-lock")
        os.mkdir(lock, 0o700)
        start = proc_start(args.owner_pid)
        if start is None:
            fail("package lock owner has no verifiable process start identity")
        atomic_json(os.path.join(lock, "owner.json"), {
            "schemaVersion": 1, "pid": args.owner_pid, "startTime": start,
            "operation": args.operation,
        })
        fsync_directory(prefix)
        crash_at("after_lock")
    elif args.action == "begin":
        require_lock(prefix, args.owner_pid)
        journal = {
            "schemaVersion": 1, "operation": args.operation, "phase": "prepared",
            "oldCurrent": target_or_none(args.old_current),
            "oldPrevious": target_or_none(args.old_previous),
            "newCurrent": target_or_none(args.new_current),
            "newPrevious": target_or_none(args.new_previous),
        }
        atomic_json(os.path.join(prefix, "transition.json"), journal)
        crash_at("after_prepared")
        atomic_link(prefix, "previous", journal["newPrevious"])
        crash_at("after_previous_link")
        journal["phase"] = "previous_written"
        atomic_json(os.path.join(prefix, "transition.json"), journal)
        atomic_link(prefix, "current", journal["newCurrent"])
        crash_at("after_current_link")
        journal["phase"] = "switched"
        atomic_json(os.path.join(prefix, "transition.json"), journal)
        crash_at("after_switched")
    elif args.action == "health-passed":
        require_lock(prefix, args.owner_pid)
        journal = load_json(os.path.join(prefix, "transition.json"))
        if journal.get("phase") != "switched":
            fail("transition is not ready for health acceptance")
        journal["phase"] = "health_passed"
        atomic_json(os.path.join(prefix, "transition.json"), journal)
        crash_at("after_health_passed")
    elif args.action == "commit":
        require_lock(prefix, args.owner_pid)
        journal = load_json(os.path.join(prefix, "transition.json"))
        if journal.get("phase") not in ("switched", "health_passed"):
            fail("transition phase cannot be committed")
        receipt = json.loads(base64.b64decode(args.receipt_base64, validate=True))
        journal["phase"] = "committing"
        journal["receipt"] = receipt
        atomic_json(os.path.join(prefix, "transition.json"), journal)
        receipts = os.path.join(prefix, "receipts")
        os.makedirs(receipts, mode=0o755, exist_ok=True)
        atomic_json(os.path.join(receipts, "last-install.json"), receipt)
        crash_at("after_receipt")
        os.unlink(os.path.join(prefix, "transition.json"))
        fsync_directory(prefix)
        remove_lock(prefix)
    elif args.action == "recovery-required":
        require_lock(prefix, args.owner_pid)
        journal = load_json(os.path.join(prefix, "transition.json"))
        if journal.get("phase") not in ("switched", "health_passed"):
            fail("transition phase cannot enter recovery")
        journal["phase"] = "recovery_required"
        journal["recovery"] = {
            "code": args.recovery_code,
            "message": args.recovery_message,
        }
        atomic_json(os.path.join(prefix, "transition.json"), journal)
        remove_lock(prefix)
    elif args.action == "abort":
        require_lock(prefix, args.owner_pid)
        journal_path = os.path.join(prefix, "transition.json")
        if os.path.exists(journal_path):
            restore_journal(prefix, load_json(journal_path))
        remove_lock(prefix)


def command_fsync_tree(args):
    for base, directories, files in os.walk(args.path, followlinks=False):
        for name in directories:
            metadata = os.lstat(os.path.join(base, name))
            if not stat.S_ISDIR(metadata.st_mode):
                fail("fsync tree contains a linked directory")
        for name in files:
            path = os.path.join(base, name)
            descriptor, _ = open_pinned(path)
            try:
                os.fsync(descriptor)
            finally:
                os.close(descriptor)
    for base, _, _ in os.walk(args.path, topdown=False, followlinks=False):
        fsync_directory(base)
    fsync_directory(os.path.dirname(args.path))


def parser():
    value = argparse.ArgumentParser()
    commands = value.add_subparsers(dest="command", required=True)
    snapshot = commands.add_parser("snapshot")
    snapshot.add_argument("--directory", required=True)
    snapshot.add_argument("--archive", required=True)
    snapshot.add_argument("--provenance", required=True)
    snapshot.add_argument("--signature", required=True)
    snapshot.add_argument("--public-key", required=True)
    snapshot.set_defaults(function=command_snapshot)
    extract = commands.add_parser("extract")
    extract.add_argument("--archive", required=True)
    extract.add_argument("--provenance", required=True)
    extract.add_argument("--destination", required=True)
    extract.set_defaults(function=command_extract)
    verify = commands.add_parser("verify-release")
    verify.add_argument("--release", required=True)
    verify.add_argument("--provenance", required=True)
    verify.set_defaults(function=command_verify_release)
    compatible = commands.add_parser("state-compatible")
    compatible.add_argument("--state", required=True)
    compatible.add_argument("--legacy-state")
    compatible.add_argument("--manifest", required=True)
    compatible.add_argument("--expected-uid", required=True, type=int)
    compatible.add_argument("--require-authority", action="store_true")
    compatible.set_defaults(function=command_state_compatible)
    state = commands.add_parser("prepare-state")
    state.add_argument("--install-root", required=True)
    state.add_argument("--template", required=True)
    state.add_argument("--uid", required=True, type=int)
    state.add_argument("--gid", required=True, type=int)
    state.set_defaults(function=command_prepare_state)
    sync = commands.add_parser("fsync-tree")
    sync.add_argument("--path", required=True)
    sync.set_defaults(function=command_fsync_tree)
    transition = commands.add_parser("transition")
    transition.add_argument("action", choices=(
        "acquire", "begin", "health-passed", "commit", "recovery-required", "abort"))
    transition.add_argument("--prefix", required=True)
    transition.add_argument("--owner-pid", required=True, type=int)
    transition.add_argument("--operation")
    transition.add_argument("--old-current", default="none")
    transition.add_argument("--old-previous", default="none")
    transition.add_argument("--new-current", default="none")
    transition.add_argument("--new-previous", default="none")
    transition.add_argument("--recovery-code", default="package_recovery_required")
    transition.add_argument("--recovery-message", default="Package transition requires operator recovery.")
    transition.add_argument("--receipt-base64")
    transition.set_defaults(function=command_transition)
    return value


def main():
    args = parser().parse_args()
    args.function(args)


if __name__ == "__main__":
    main()
