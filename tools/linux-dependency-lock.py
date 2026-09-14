#!/usr/bin/env python3
"""Fail closed unless SwiftPM's consumed resolution equals signed Linux provenance."""

import argparse
import json
import os
import re


def fail(message):
    raise SystemExit("linux-dependency-lock: " + message)


def load(path, label):
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    except OSError:
        fail(label + " is missing or cannot be opened safely")
    try:
        metadata = os.fstat(descriptor)
        if metadata.st_size < 2 or metadata.st_size > 256 * 1024:
            fail(label + " has an invalid size")
        data = b""
        while len(data) <= 256 * 1024:
            chunk = os.read(descriptor, min(65536, 256 * 1024 + 1 - len(data)))
            if not chunk:
                break
            data += chunk
    finally:
        os.close(descriptor)
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


parser = argparse.ArgumentParser()
parser.add_argument("command", choices=["verify"])
parser.add_argument("--resolved", required=True)
parser.add_argument("--lock", required=True)
arguments = parser.parse_args()
verify(arguments.resolved, arguments.lock)
print("linux dependency lock: exact resolution verified")
