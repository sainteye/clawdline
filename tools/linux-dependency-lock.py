#!/usr/bin/env python3
"""Fail closed unless SwiftPM's consumed resolution equals signed Linux provenance."""

import argparse
import hashlib
import json
import os
import re
import stat


def fail(message):
    raise SystemExit("linux-dependency-lock: " + message)


MAXIMUM_BYTES = 256 * 1024


def open_pinned(path, label):
    absolute = os.path.isabs(path)
    parts = path.split("/")[1:] if absolute else path.split("/")
    if not parts or any(part in ("", ".", "..") for part in parts):
        fail(label + " path is not canonical: " + path)
    parent = os.open("/" if absolute else ".",
                     os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        for component in parts[:-1]:
            child = os.open(component,
                            os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                            dir_fd=parent)
            os.close(parent)
            parent = child
        descriptor = os.open(parts[-1],
                             os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC,
                             dir_fd=parent)
    except OSError:
        fail(label + " is missing or cannot be opened safely")
    finally:
        os.close(parent)
    metadata = os.fstat(descriptor)
    try:
        if not stat.S_ISREG(metadata.st_mode) or metadata.st_nlink != 1:
            fail(label + " must be one non-hardlinked regular file")
        if metadata.st_uid != os.geteuid():
            fail(label + " is not owned by the build identity")
        if stat.S_IMODE(metadata.st_mode) & 0o022:
            fail(label + " is writable by another identity")
        if metadata.st_size < 2 or metadata.st_size > MAXIMUM_BYTES:
            fail(label + " has an invalid size")
    except BaseException:
        os.close(descriptor)
        raise
    return descriptor, metadata


def read_descriptor(descriptor, metadata, label):
    try:
        data = b""
        while len(data) <= MAXIMUM_BYTES:
            chunk = os.read(descriptor, min(65536, MAXIMUM_BYTES + 1 - len(data)))
            if not chunk:
                break
            data += chunk
        final = os.fstat(descriptor)
        identity = lambda value: (value.st_dev, value.st_ino, value.st_mode, value.st_nlink,
                                  value.st_uid, value.st_size, value.st_mtime_ns,
                                  value.st_ctime_ns)
        if identity(metadata) != identity(final) or len(data) != metadata.st_size:
            fail(label + " changed while its pinned descriptor was read")
    finally:
        os.close(descriptor)
    return data


def read_pinned(path, label):
    descriptor, metadata = open_pinned(path, label)
    return read_descriptor(descriptor, metadata, label)


def load(path, label):
    data = read_pinned(path, label)
    try:
        return json.loads(data)
    except (UnicodeDecodeError, json.JSONDecodeError):
        fail(label + " is not valid UTF-8 JSON")


def closed_string(value, label, pattern=None):
    if not isinstance(value, str) or not value:
        fail(label + " must be a non-empty string")
    if pattern and not re.fullmatch(pattern, value):
        fail(label + " has an invalid value")
    return value


def normalized_lock(path):
    value = load(path, "signed dependency lock")
    if set(value) != {"schemaVersion", "packages"} or value["schemaVersion"] != 1:
        fail("signed dependency lock has an unsupported shape")
    if not isinstance(value["packages"], list) or not value["packages"]:
        fail("signed dependency lock contains no packages")
    rows = {}
    for row in value["packages"]:
        required = {"identity", "kind", "location", "revision", "version"}
        if not isinstance(row, dict) or set(row) != required:
            fail("signed dependency lock pin shape is not exact")
        identity = closed_string(row["identity"], "dependency identity", r"[a-z0-9][a-z0-9._-]{0,127}")
        if identity in rows:
            fail("signed dependency lock contains duplicate identity " + identity)
        rows[identity] = (
            closed_string(row["kind"], "dependency kind"),
            closed_string(row["location"], "dependency location"),
            closed_string(row["version"], "dependency version"),
            closed_string(row["revision"], "dependency revision", r"[0-9a-f]{40}"),
        )
    return rows


def normalized_resolved(path):
    value = load(path, "SwiftPM Package.resolved")
    if not isinstance(value, dict) or value.get("version") not in (2, 3):
        fail("SwiftPM Package.resolved has an unsupported version")
    if set(value) - {"version", "pins", "originHash"} or not isinstance(value.get("pins"), list):
        fail("SwiftPM Package.resolved has an unsupported shape")
    rows = {}
    for pin in value["pins"]:
        if not isinstance(pin, dict) or set(pin) != {"identity", "kind", "location", "state"}:
            fail("SwiftPM resolved pin shape is not exact")
        identity = closed_string(pin["identity"], "resolved identity", r"[a-z0-9][a-z0-9._-]{0,127}")
        state = pin["state"]
        if not isinstance(state, dict) or set(state) != {"revision", "version"}:
            fail("SwiftPM resolved state is not an exact version pin")
        if identity in rows:
            fail("SwiftPM Package.resolved contains duplicate identity " + identity)
        rows[identity] = (
            closed_string(pin["kind"], "resolved kind"),
            closed_string(pin["location"], "resolved location"),
            closed_string(state["version"], "resolved version"),
            closed_string(state["revision"], "resolved revision", r"[0-9a-f]{40}"),
        )
    return rows


def verify(resolved_path, lock_path):
    resolved = normalized_resolved(resolved_path)
    locked = normalized_lock(lock_path)
    missing = sorted(set(locked) - set(resolved))
    extra = sorted(set(resolved) - set(locked))
    drift = sorted(identity for identity in set(locked) & set(resolved)
                   if locked[identity] != resolved[identity])
    if missing:
        fail("SwiftPM resolution is missing locked pin(s): " + ",".join(missing))
    if extra:
        fail("SwiftPM resolution has unsigned extra pin(s): " + ",".join(extra))
    if drift:
        fail("SwiftPM resolution differs in kind/location/version/revision: " + ",".join(drift))


def write_snapshot(directory, name, content):
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        target = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                         0o600, dir_fd=descriptor)
        try:
            offset = 0
            while offset < len(content):
                offset += os.write(target, content[offset:])
            os.fsync(target)
        finally:
            os.close(target)
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def snapshot(resolved_path, lock_path, directory):
    # Open and validate both authorities before publishing either output. The pair in this private
    # directory is the only pair packaging is allowed to stage, hash, or sign.
    resolved_fd, resolved_metadata = open_pinned(resolved_path, "SwiftPM Package.resolved")
    lock_fd, lock_metadata = open_pinned(lock_path, "signed dependency lock")
    try:
        resolved = read_descriptor(resolved_fd, resolved_metadata, "SwiftPM Package.resolved")
        locked = read_descriptor(lock_fd, lock_metadata, "signed dependency lock")
    except BaseException:
        # read_descriptor closes the descriptor it was given. Close whichever sibling is still
        # live without turning a typed validation refusal into a cleanup failure.
        for descriptor in (resolved_fd, lock_fd):
            try:
                os.close(descriptor)
            except OSError:
                pass
        raise
    try:
        os.mkdir(directory, 0o700)
        os.chmod(directory, 0o700)
    except OSError:
        fail("dependency snapshot directory cannot be created safely")
    write_snapshot(directory, "Package.resolved", resolved)
    write_snapshot(directory, "dependencies.lock.json", locked)
    verify(os.path.join(directory, "Package.resolved"),
           os.path.join(directory, "dependencies.lock.json"))
    receipt = {
        "lockSha256": hashlib.sha256(locked).hexdigest(),
        "resolvedSha256": hashlib.sha256(resolved).hexdigest(),
    }
    print(json.dumps(receipt, sort_keys=True, separators=(",", ":")))


def match(actual_path, snapshot_path, label):
    actual = read_pinned(actual_path, label)
    expected = read_pinned(snapshot_path, "pinned " + label)
    if actual != expected:
        fail(label + " changed after its accepted snapshot")


parser = argparse.ArgumentParser()
parser.add_argument("command", choices=["verify", "snapshot", "match"])
parser.add_argument("--resolved")
parser.add_argument("--lock")
parser.add_argument("--directory")
parser.add_argument("--actual")
parser.add_argument("--expected")
parser.add_argument("--label", default="dependency authority")
arguments = parser.parse_args()
if arguments.command == "verify":
    if not arguments.resolved or not arguments.lock:
        fail("verify requires --resolved and --lock")
    verify(arguments.resolved, arguments.lock)
    print("linux dependency lock: exact resolution verified")
elif arguments.command == "snapshot":
    if not arguments.directory:
        fail("snapshot requires --directory")
    if not arguments.resolved or not arguments.lock:
        fail("snapshot requires --resolved and --lock")
    snapshot(arguments.resolved, arguments.lock, arguments.directory)
else:
    if not arguments.actual or not arguments.expected:
        fail("match requires --actual and --expected")
    match(arguments.actual, arguments.expected, arguments.label)
    print("linux dependency lock: accepted snapshot remains exact")
