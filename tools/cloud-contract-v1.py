#!/usr/bin/env python3
"""Offline Cloud v1 candidate compiler/structural validator. Python standard library only.

The manifest is the authoring input; its separately pinned exact-byte digest is required.
Cryptographic known answers are verified by Tests/cloud-contract-v1.mjs, not by this tool.
"""
from __future__ import annotations

import argparse
import base64
import binascii
import ctypes
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import sys
import tempfile

SAFE_INTEGER = 9_007_199_254_740_991
MAX_INPUT_BYTES = 32 * 1024 * 1024
MAX_MANIFEST_BYTES = 2 * 1024 * 1024
MAX_PACKAGE_BYTES = 16 * 1024 * 1024
MAX_DEPTH = 64
MAX_NODES = 100_000
DEFAULT_PACKAGE = Path(__file__).resolve().parents[1] / "Contracts/Cloud/v1"
SCHEMA_NAMES = {"envelope", "pairing_offer", "pairing_wrapper", "pairing_handover", "pairing_receipt"}
SCHEMA_KEYS = {"$schema", "$id", "title", "description", "type", "properties", "required",
               "additionalProperties", "const", "enum", "minimum", "maximum", "minLength",
               "maxLength", "pattern", "allOf", "oneOf", "if", "then"}


class ContractError(Exception):
    def __init__(self, code: str):
        self.code = code
        super().__init__(code)


def require(condition: bool, code: str) -> None:
    if not condition:
        raise ContractError(code)


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def closed(value, keys, code="manifest_shape") -> None:
    require(type(value) is dict and set(value) == set(keys), code)


def text_value(value, maximum=4096, empty=False) -> None:
    require(type(value) is str and (empty or bool(value)) and len(value) <= maximum, "manifest_shape")


def exact_int(value, minimum=0, maximum=SAFE_INTEGER) -> bool:
    return type(value) is int and minimum <= value <= maximum


def strict_json(data: bytes, limit=MAX_INPUT_BYTES):
    require(bool(data.strip()), "empty_input")
    require(len(data) <= limit, "oversized_input")
    require(not data.startswith(b"\xef\xbb\xbf"), "invalid_json")

    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "duplicate_key")
            result[key] = value
        return result

    def integer(value):
        require(len(value) <= 17, "unsafe_integer")
        number = int(value)
        require(abs(number) <= SAFE_INTEGER, "unsafe_integer")
        return number

    def floating(_):
        raise ContractError("non_integer_number")

    def nonfinite(_):
        raise ContractError("nonfinite_number")

    try:
        value = json.loads(data.decode("utf-8"), object_pairs_hook=pairs, parse_int=integer,
                           parse_float=floating, parse_constant=nonfinite)
        pending = [(value, 0)]
        count = 0
        while pending:
            item, depth = pending.pop()
            count += 1
            require(depth <= MAX_DEPTH and count <= MAX_NODES, "complexity_limit")
            if isinstance(item, str):
                item.encode("utf-8", "strict")
            elif isinstance(item, dict):
                pending.extend((key, depth + 1) for key in item)
                pending.extend((part, depth + 1) for part in item.values())
            elif isinstance(item, list):
                pending.extend((part, depth + 1) for part in item)
        return value
    except (UnicodeError, ValueError, RecursionError) as error:
        raise ContractError("invalid_json") from error


def canonical(value) -> bytes:
    """RFC 8785 string/key rules, restricted to the existing safe-integer domain."""
    def emit(item):
        if item is None:
            return "null"
        if type(item) is bool:
            return "true" if item else "false"
        if type(item) is int:
            require(abs(item) <= SAFE_INTEGER, "unsafe_integer")
            return str(item)
        if type(item) is str:
            item.encode("utf-8", "strict")
            return json.dumps(item, ensure_ascii=False)
        if type(item) is list:
            return "[" + ",".join(emit(part) for part in item) + "]"
        require(type(item) is dict, "invalid_json")
        keys = sorted(item, key=lambda key: key.encode("utf-16-be"))
        return "{" + ",".join(emit(key) + ":" + emit(item[key]) for key in keys) + "}"
    return emit(value).encode("utf-8")


def document(value) -> bytes:
    return canonical(value) + b"\n"


def regular_bytes(path: Path, limit: int) -> bytes:
    # Do not block on a FIFO or follow a symlink supplied as a supposed fixture.
    require(stat.S_ISREG(path.lstat().st_mode), "nonregular_input")
    descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as handle:
        info = os.fstat(handle.fileno())
        require(stat.S_ISREG(info.st_mode), "nonregular_input")
        require(info.st_size <= limit, "oversized_input")
        data = handle.read(limit + 1)
    require(len(data) <= limit, "oversized_input")
    return data


def digest_text(value: str) -> str:
    require(type(value) is str and re.fullmatch(r"[0-9a-f]{64}", value) is not None, "invalid_digest")
    return value


def base64_bytes(value: str) -> bytes:
    require(type(value) is str, "invalid_base64")
    try:
        decoded = base64.b64decode(value, validate=True)
    except (ValueError, binascii.Error) as error:
        raise ContractError("invalid_base64") from error
    require(base64.b64encode(decoded).decode("ascii") == value, "invalid_base64")
    return decoded


def schema_definition(schema, depth=0) -> None:
    """Validate the small, explicit draft-2020-12 subset this compiler implements.

    Unknown keywords are errors, never silently ignored assertions. Every object schema is
    closed; conditional fragments only narrow already-declared properties.
    """
    require(depth < 16 and type(schema) is dict and bool(schema) and
            set(schema) <= SCHEMA_KEYS, "unsupported_schema")
    for key in ("$schema", "$id", "title", "description", "pattern"):
        if key in schema:
            text_value(schema[key])
    if "$schema" in schema:
        require(schema["$schema"] == "https://json-schema.org/draft/2020-12/schema", "unsupported_schema")
    if "type" in schema:
        require(schema["type"] in ("object", "string", "integer", "boolean"), "unsupported_schema")
    if schema.get("type") == "object":
        require(schema.get("additionalProperties") is False and
                type(schema.get("properties")) is dict and bool(schema["properties"]) and
                type(schema.get("required")) is list and
                len(schema["required"]) == len(set(schema["required"])) and
                set(schema["required"]) == set(schema["properties"]), "unclosed_schema")
    if "additionalProperties" in schema:
        require(schema["additionalProperties"] is False, "unclosed_schema")
    if "required" in schema:
        require(type(schema["required"]) is list and all(type(key) is str for key in schema["required"]),
                "unsupported_schema")
    if "properties" in schema:
        require(type(schema["properties"]) is dict and bool(schema["properties"]), "unsupported_schema")
        for name, child in schema["properties"].items():
            text_value(name, 128)
            schema_definition(child, depth + 1)
    for key in ("minimum", "maximum", "minLength", "maxLength"):
        if key in schema:
            require(exact_int(schema[key]), "unsupported_schema")
    if "pattern" in schema:
        # All authored patterns use this ECMAScript/Python-compatible exact end assertion.
        require(schema["pattern"].startswith("^") and schema["pattern"].endswith(r"(?![\s\S])"),
                "unsupported_schema")
        try:
            re.compile(schema["pattern"])
        except re.error as error:
            raise ContractError("unsupported_schema") from error
    if "enum" in schema:
        require(type(schema["enum"]) is list and bool(schema["enum"]) and
                len({canonical(item) for item in schema["enum"]}) == len(schema["enum"]), "unsupported_schema")
    for key in ("allOf", "oneOf"):
        if key in schema:
            require(type(schema[key]) is list and bool(schema[key]), "unsupported_schema")
            for child in schema[key]:
                schema_definition(child, depth + 1)
    require(("if" in schema) == ("then" in schema), "unsupported_schema")
    for key in ("if", "then"):
        if key in schema:
            schema_definition(schema[key], depth + 1)


def matches(schema, value) -> bool:
    kind = schema.get("type")
    if kind == "object" and type(value) is not dict:
        return False
    if kind == "integer" and type(value) is not int:
        return False
    if kind == "string" and type(value) is not str:
        return False
    if kind == "boolean" and type(value) is not bool:
        return False
    if "const" in schema and canonical(value) != canonical(schema["const"]):
        return False
    if "enum" in schema and canonical(value) not in [canonical(item) for item in schema["enum"]]:
        return False
    if type(value) is dict:
        if not set(schema.get("required", [])) <= set(value):
            return False
        properties = schema.get("properties", {})
        if schema.get("additionalProperties") is False and not set(value) <= set(properties):
            return False
        if any(not matches(child, value[key]) for key, child in properties.items() if key in value):
            return False
    if type(value) is str:
        if not schema.get("minLength", 0) <= len(value) <= schema.get("maxLength", MAX_INPUT_BYTES):
            return False
        if "pattern" in schema and re.search(schema["pattern"], value) is None:
            return False
    if type(value) is int:
        if not schema.get("minimum", -SAFE_INTEGER) <= value <= schema.get("maximum", SAFE_INTEGER):
            return False
    if "allOf" in schema and not all(matches(child, value) for child in schema["allOf"]):
        return False
    if "oneOf" in schema and sum(matches(child, value) for child in schema["oneOf"]) != 1:
        return False
    if "if" in schema and matches(schema["if"], value) and not matches(schema["then"], value):
        return False
    return True


def signing_bytes(envelope, manifest) -> bytes:
    return "|".join(str(envelope[key]) for key in manifest["wire"]["signing_fields"]).encode("utf-8")


def fingerprint(key: str) -> str:
    digest = hashlib.sha256(base64_bytes(key)).digest()[:10]
    letters = base64.b32encode(digest).decode("ascii")
    return "-".join(letters[index:index + 4] for index in range(0, len(letters), 4))


def validate_bytes(data: bytes, kind: str, manifest, canonical_required=False):
    require(kind in SCHEMA_NAMES, "unknown_kind")
    value = strict_json(data)
    require(matches(manifest["schemas"][kind], value), "schema_mismatch")
    # These relational/decoded-byte rules cannot be expressed by ordinary JSON Schema.
    if kind in ("envelope", "pairing_wrapper"):
        # The schema's linear alphabet/padding filter deliberately does not use repeated
        # four-character groups (large valid strings overflow ECMAScript regex stacks).
        # Round-trip enforces full quanta, exact padding and unused bits in both kinds.
        ciphertext = base64_bytes(value["ct"])
        require(len(ciphertext) >= (1 if kind == "envelope" else 16), "schema_mismatch")
        require(len(ciphertext) <= manifest["limits"]["ciphertext_bytes"], "ciphertext_too_large")
    if kind == "pairing_handover":
        require(fingerprint(value["machine_signing_key"]) == value["machine_fingerprint"], "fingerprint_mismatch")
    if kind == "pairing_offer":
        require(fingerprint(value["viewer_signing_key"]) == value["viewer_fingerprint"], "fingerprint_mismatch")
        require(value["claim_nonce"] != value["pairing_nonce"], "reused_nonce")
    if canonical_required or kind != "envelope":
        require(canonical(value) == data, "noncanonical_input")
    return value


def validate_vectors(vectors, manifest) -> None:
    closed(vectors, ["format", "cipher", "nonce_bytes", "ed25519_seed", "ed25519_public_key",
                     "master_secret", "envelopes", "receipts", "control_response", "pairing_handover"])
    require(type(vectors["format"]) is int and vectors["format"] == 1 and
            vectors["cipher"] == "AES-256-GCM" and vectors["nonce_bytes"] == 12,
            "vector_shape")
    for key in ("ed25519_seed", "ed25519_public_key", "master_secret"):
        require(len(base64_bytes(vectors[key])) == 32, "vector_shape")
    require(type(vectors["envelopes"]) is list and len(vectors["envelopes"]) == 6, "vector_shape")
    names = set()
    for entry in vectors["envelopes"]:
        closed(entry, ["name", "plaintext", "envelope"])
        text_value(entry["name"], 64)
        require(entry["name"] not in names, "duplicate_fixture")
        names.add(entry["name"])
        clear = base64_bytes(entry["plaintext"])
        envelope = validate_bytes(canonical(entry["envelope"]), "envelope", manifest)
        require(len(base64_bytes(envelope["ct"])) == len(clear) + 16, "vector_shape")
    require(type(vectors["receipts"]) is list and len(vectors["receipts"]) == 5, "vector_shape")
    for entry in vectors["receipts"]:
        closed(entry, ["name", "body", "byte_length", "sha256", "headers"])
        text_value(entry["name"], 64)
        require(entry["name"] not in names, "duplicate_fixture")
        names.add(entry["name"])
        verify_body(entry)
        validate_bytes(entry["body"].encode("utf-8"), "pairing_receipt", manifest)
        closed(entry["headers"], ["X-Clawdline-Receipt-SHA256"])
        require(entry["headers"]["X-Clawdline-Receipt-SHA256"] == entry["sha256"], "wrong_digest")
    cr = vectors["control_response"]
    closed(cr, ["channel", "allowed_classes", "reply_key", "request_envelope", "response_envelope",
                "response_payload", "response_open_results"])
    closed(cr["reply_key"], ["key_id", "key", "decoded_byte_length"])
    require(len(base64_bytes(cr["reply_key"]["key"])) == cr["reply_key"]["decoded_byte_length"] == 32,
            "vector_shape")
    for name in ("request_envelope", "response_envelope", "response_payload"):
        entry = cr[name]
        closed(entry, ["name", "body", "byte_length", "sha256", "field_count"])
        verify_body(entry)
        body = strict_json(entry["body"].encode("utf-8"))
        require(type(body) is dict and len(body) == entry["field_count"] == (9 if name == "response_payload" else 10),
                "vector_shape")
        require(canonical(body) == entry["body"].encode("utf-8"), "noncanonical_input")
        if name != "response_payload":
            validate_bytes(entry["body"].encode("utf-8"), "envelope", manifest)
    response = strict_json(cr["response_envelope"]["body"].encode("utf-8"))
    require(response["ch"] == cr["channel"] and cr["channel"].startswith("ctlr/") and
            response["key_id"] == cr["reply_key"]["key_id"] and cr["allowed_classes"] == ["ctl"], "vector_shape")
    # Canonical comparison preserves closed rows AND Boolean types: Python equality
    # alone treats {"succeeds": 1} as {"succeeds": True}.
    require(canonical(cr["response_open_results"]) == canonical([
        {"name": "response-with-master-secret", "key_id": "ms-1", "succeeds": False},
        {"name": "response-with-reply-key", "key_id": cr["reply_key"]["key_id"], "succeeds": True},
    ]), "vector_shape")
    pairing = vectors["pairing_handover"]
    closed(pairing, ["name", "now_milliseconds", "viewer_ephemeral_private_key", "machine_ephemeral_private_key",
                     "offer", "offer_fragment", "phase_key", "aad", "wrapper", "handover", "sender_device_id"])
    for key in ("viewer_ephemeral_private_key", "machine_ephemeral_private_key", "phase_key"):
        require(len(base64_bytes(pairing[key])) == 32, "vector_shape")
    offer = validate_bytes(pairing["offer"].encode("utf-8"), "pairing_offer", manifest)
    handover = validate_bytes(pairing["handover"].encode("utf-8"), "pairing_handover", manifest)
    wrapper = validate_bytes(canonical(pairing["wrapper"]), "pairing_wrapper", manifest)
    aad = {key: wrapper[key] for key in manifest["wire"]["pairing_aad_fields"]}
    require(canonical(aad) == pairing["aad"].encode("utf-8") and
            base64.urlsafe_b64encode(pairing["offer"].encode("utf-8")).decode("ascii").rstrip("=") == pairing["offer_fragment"] and
            wrapper["pairing_id"] == offer["pairing_id"] and
            wrapper["sender_device_id"] == pairing["sender_device_id"] == handover["machine_id"] and
            offer["account_id"] == handover["account_id"] and
            exact_int(pairing["now_milliseconds"]) and
            0 <= offer["expires_at"] - pairing["now_milliseconds"] <= 600_000, "vector_shape")


def verify_body(entry) -> None:
    text_value(entry["body"], MAX_MANIFEST_BYTES)
    data = entry["body"].encode("utf-8")
    require(exact_int(entry["byte_length"]) and len(data) == entry["byte_length"], "wrong_length")
    require(sha256(data) == digest_text(entry["sha256"]), "wrong_digest")


def validate_crypto_negatives(cases) -> None:
    closed(cases, ["format", "pairing_agreement", "pairing_shared_secret"])
    require(type(cases["format"]) is int and cases["format"] == 1, "vector_shape")
    specifications = {
        "pairing_agreement": ("peer_public_key", [
            ("x25519-low-order-zero", bytes(32)),
            ("x25519-low-order-one", b"\x01" + bytes(31)),
        ]),
        "pairing_shared_secret": ("shared_secret", [
            ("x25519-all-zero-result", bytes(32)),
            ("x25519-short-result", bytes(31)),
        ]),
    }
    for name, (field, expected) in specifications.items():
        rows = cases[name]
        require(type(rows) is list and len(rows) == len(expected), "vector_shape")
        for row, (label, raw) in zip(rows, expected):
            closed(row, ["name", field, "expected"], "vector_shape")
            require(row["name"] == label and base64_bytes(row[field]) == raw and
                    row["expected"] == "reject_before_hkdf", "vector_shape")


def read_manifest(path: Path, expected: str | None):
    raw = regular_bytes(path, MAX_MANIFEST_BYTES)
    if expected is None:
        pin = regular_bytes(path.with_name("source.sha256"), 65)
        require(re.fullmatch(rb"[0-9a-f]{64}\n", pin) is not None, "invalid_digest")
        expected = pin[:-1].decode("ascii")
    require(sha256(raw) == digest_text(expected), "wrong_digest")
    manifest = strict_json(raw, MAX_MANIFEST_BYTES)
    closed(manifest, ["format", "id", "version", "authority", "compatibility", "provenance", "limits",
                      "wire", "schemas", "vocabularies", "vectors", "crypto_negatives", "negative_cases", "readme", "changelog"])
    require(document(manifest) == raw, "noncanonical_manifest")
    require(type(manifest["format"]) is int and manifest["format"] == 1 and manifest["id"] == "clawdline.cloud.contract" and
            type(manifest["version"]) is str and re.fullmatch(r"1\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", manifest["version"]),
            "unsupported_version")
    closed(manifest["authority"], ["status", "normative_prose", "cutover_required"])
    require(canonical(manifest["authority"]) == canonical({"status": "candidate", "normative_prose": "clawdline-cloud/docs/PROTOCOL.md",
                                                          "cutover_required": True}), "authority_changed")
    compatibility = manifest["compatibility"]
    closed(compatibility, ["wire_min", "wire_max", "package_floor", "package_range", "client_floor", "rollout"])
    require(type(compatibility["wire_min"]) is int and type(compatibility["wire_max"]) is int and
            compatibility["wire_min"] == compatibility["wire_max"] == 1 and
            compatibility["package_floor"] == "1.0.0" and compatibility["package_range"] == ">=1.0.0 <2.0.0" and
            compatibility["client_floor"] == "unchanged_pending_observed_window" and compatibility["rollout"] == [
                "additive_readers", "pinned_mirrors", "relay_api_consumers", "mac_producer", "ubuntu_producer",
                "observed_window", "raise_floor"], "compatibility_changed")
    require(manifest["limits"] == {"raw_input_bytes": MAX_INPUT_BYTES, "ciphertext_bytes": 25_162_752,
                                    "manifest_bytes": MAX_MANIFEST_BYTES, "nesting_depth": MAX_DEPTH}, "unsupported_limits")
    wire = manifest["wire"]
    require(wire == {"envelope_fields": ["v", "ch", "seq", "ts", "class", "key_id", "nonce", "ct", "sender", "sig"],
                     "signing_fields": ["v", "ch", "seq", "ts", "class", "key_id", "nonce", "ct"],
                     "pairing_aad_fields": ["v", "phase", "pairing_id", "sender_device_id", "ephemeral_key"],
                     "nonce_bytes": 12, "signature_bytes": 64, "aead_tag_bytes": 16,
                     "canonicalization": "rfc8785-safe-integers-utf16", "signing": "utf8-pipe-no-trailing-separator"},
            "wire_changed")
    require(type(manifest["provenance"]) is list and bool(manifest["provenance"]), "manifest_shape")
    seen = set()
    for source in manifest["provenance"]:
        closed(source, ["repository", "commit", "path", "sha256", "role"])
        for key in source:
            text_value(source[key])
        require(re.fullmatch(r"[0-9a-f]{40}", source["commit"]) is not None, "manifest_shape")
        digest_text(source["sha256"])
        identity = (source["repository"], source["path"])
        require(identity not in seen, "manifest_shape")
        seen.add(identity)
    closed(manifest["schemas"], SCHEMA_NAMES)
    for schema in manifest["schemas"].values():
        schema_definition(schema)
    closed(manifest["vocabularies"], ["classes", "channels", "evidence", "reserved_prefixes"])
    vocabulary = manifest["vocabularies"]
    require(vocabulary["classes"] == ["stream", "ctl", "dispatch", "ho"] and vocabulary["reserved_prefixes"] == ["wh"],
            "manifest_shape")
    closed(vocabulary["channels"], ["s", "t", "orch", "ctl", "ctlr", "ho"])
    segments = {"s": ["machine", "session"], "t": ["machine", "session"], "orch": ["machine"],
                "ctl": ["machine"], "ctlr": ["machine", "viewer_device"], "ho": ["account", "handoff"]}
    for prefix, channel in vocabulary["channels"].items():
        closed(channel, ["segments", "classes", "carries"])
        require(channel["segments"] == segments[prefix] and
                type(channel["classes"]) is list and bool(channel["classes"]) and
                set(channel["classes"]) <= set(vocabulary["classes"]) and
                len(set(channel["classes"])) == len(channel["classes"]), "manifest_shape")
        text_value(channel["carries"])
    closed(vocabulary["evidence"], ["relay_fanout", "command_effect", "pairing_finalization",
                                    "webhook_ingress", "machine_durable_acceptance", "human_observation"])
    for evidence in vocabulary["evidence"].values():
        closed(evidence, ["signal", "proves", "does_not_prove"])
        for value in evidence.values():
            text_value(value)
    for key in ("readme", "changelog"):
        text_value(manifest[key], 32_768)
    validate_vectors(manifest["vectors"], manifest)
    validate_crypto_negatives(manifest["crypto_negatives"])
    # Both generated surfaces must describe the same language, not merely be valid JSON.
    for prefix, channel in vocabulary["channels"].items():
        for envelope_class in vocabulary["classes"]:
            probe = dict(manifest["vectors"]["envelopes"][0]["envelope"])
            probe.update(ch=prefix + "/" + "/".join("id" for _ in channel["segments"]),
                         key_id="rk-zF3jN8rQ4Wm2pV6sT0uYxA" if prefix == "ctlr" else "ms-1")
            probe["class"] = envelope_class
            require(matches(manifest["schemas"]["envelope"], probe) == (envelope_class in channel["classes"]),
                    "vocabulary_schema_mismatch")
    require(type(manifest["negative_cases"]) is list and bool(manifest["negative_cases"]), "empty_fixtures")
    return raw, manifest


def compile_package(raw: bytes, manifest):
    output = {"manifest.json": raw, "source.sha256": (sha256(raw) + "\n").encode("ascii"),
              "README.md": manifest["readme"].encode("utf-8"), "CHANGELOG.md": manifest["changelog"].encode("utf-8"),
              "vocabularies.json": document(manifest["vocabularies"]), "vectors.json": document(manifest["vectors"]),
              "crypto-negative-vectors.json": document(manifest["crypto_negatives"])}
    for name, schema in manifest["schemas"].items():
        output[f"schemas/{name}.schema.json"] = (json.dumps(schema, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode("utf-8")
    index = []
    bases = {}

    def add_fixture(name, kind, body):
        require(re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,63}", name) is not None and name not in bases, "duplicate_fixture")
        value = validate_bytes(body, kind, manifest)
        path = f"fixtures/valid/{name}.json"
        output[path] = body
        row = {"name": name, "kind": kind, "valid": True, "path": path, "sha256": sha256(body), "bytes": len(body)}
        if kind == "envelope":
            signed = signing_bytes(value, manifest)
            row["signing_path"] = f"fixtures/signing/{name}.bin"
            row["signing_sha256"] = sha256(signed)
            output[row["signing_path"]] = signed
        index.append(row)
        bases[name] = (kind, body)

    vectors = manifest["vectors"]
    for entry in vectors["envelopes"]:
        add_fixture(entry["name"], "envelope", canonical(entry["envelope"]))
    for key in ("request_envelope", "response_envelope"):
        add_fixture("control-" + key.replace("_", "-"), "envelope", vectors["control_response"][key]["body"].encode("utf-8"))
    for entry in vectors["receipts"]:
        add_fixture("receipt-" + entry["name"], "pairing_receipt", entry["body"].encode("utf-8"))
    pairing = vectors["pairing_handover"]
    add_fixture("pairing-offer", "pairing_offer", pairing["offer"].encode("utf-8"))
    add_fixture("pairing-wrapper", "pairing_wrapper", canonical(pairing["wrapper"]))
    add_fixture("pairing-handover", "pairing_handover", pairing["handover"].encode("utf-8"))
    names = set(bases)
    for case in manifest["negative_cases"]:
        closed(case, ["name", "base", "operation", "field", "value", "error"])
        name = case["name"]
        require(type(name) is str and re.fullmatch(r"[a-z0-9][a-z0-9_-]{0,63}", name) is not None and name not in names,
                "duplicate_fixture")
        names.add(name)
        require(type(case["base"]) is str and case["base"] in bases, "unknown_fixture")
        text_value(case["field"], 128, empty=True)
        text_value(case["error"], 64)
        kind, original = bases[case["base"]]
        value = strict_json(original)
        operation = case["operation"]
        if operation == "set":
            value[case["field"]] = case["value"]
            body = canonical(value)
        elif operation == "delete":
            require(case["field"] in value and case["value"] is None, "unknown_mutation")
            del value[case["field"]]
            body = canonical(value)
        elif operation == "duplicate":
            require(case["field"] in value and case["value"] is None, "unknown_mutation")
            body = original[:-1] + b"," + canonical(case["field"]) + b":" + canonical(value[case["field"]]) + b"}"
        elif operation == "raw":
            require(case["field"] == "", "unknown_mutation")
            body = base64_bytes(case["value"])
        else:
            raise ContractError("unknown_mutation")
        try:
            validate_bytes(body, kind, manifest)
        except ContractError as error:
            require(error.code == case["error"], "fixture_error_mismatch")
        else:
            raise ContractError("negative_fixture_accepted")
        path = f"fixtures/invalid/{name}.json"
        output[path] = body
        index.append({"name": name, "kind": kind, "valid": False, "path": path,
                      "sha256": sha256(body), "bytes": len(body), "error": case["error"]})
    output["fixtures/index.json"] = document({"format": 1, "source_sha256": sha256(raw), "fixtures": index})
    inventory = [{"path": path, "bytes": len(body), "sha256": sha256(body)} for path, body in sorted(output.items())]
    output["integrity.json"] = document({"format": 1, "version": manifest["version"], "source_sha256": sha256(raw), "files": inventory})
    output["package.sha256"] = (sha256(output["integrity.json"]) + "\n").encode("ascii")
    require(sum(map(len, output.values())) <= MAX_PACKAGE_BYTES, "oversized_package")
    return output, index


def check_package(package: Path, expected: str | None):
    require(stat.S_ISDIR(package.lstat().st_mode), "nonregular_input")
    raw, manifest = read_manifest(package / "manifest.json", expected)
    output, index = compile_package(raw, manifest)
    actual = set()
    directories = {str(Path(path).parent) for path in output if "/" in path}
    directories |= {str(Path(path).parent) for path in directories if "/" in path}
    for current, subdirs, files in os.walk(package, followlinks=False):
        for name in subdirs:
            path = Path(current) / name
            require(stat.S_ISDIR(path.lstat().st_mode), "nonregular_input")
            require(path.relative_to(package).as_posix() in directories, "unknown_package_path")
        for name in files:
            path = Path(current) / name
            relative = path.relative_to(package).as_posix()
            require(relative in output, "unknown_package_path")
            require(regular_bytes(path, MAX_PACKAGE_BYTES) == output[relative], "package_byte_mismatch")
            actual.add(relative)
    require(actual == set(output), "missing_package_path")
    return manifest, output, index


def rename_noreplace(source: Path, destination: Path) -> None:
    """Publish a directory in one native no-replace operation; never emulate with exists/rename.

    macOS renamex_np(RENAME_EXCL), Linux libc renameat2(RENAME_NOREPLACE).
    An absent syscall, unsupported filesystem or unsupported OS fails closed.
    """
    library = ctypes.CDLL(None, use_errno=True)
    if sys.platform == "darwin":
        operation = getattr(library, "renamex_np", None)
        arguments = [os.fsencode(source), os.fsencode(destination), 0x00000004]
        types = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_uint]
    elif sys.platform.startswith("linux"):
        operation = getattr(library, "renameat2", None)
        arguments = [-100, os.fsencode(source), -100, os.fsencode(destination), 1]
        types = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    else:
        raise ContractError("publication_unsupported")
    require(operation is not None, "publication_unsupported")
    operation.argtypes = types
    operation.restype = ctypes.c_int
    if operation(*arguments) != 0:
        number = ctypes.get_errno()
        if number in (errno.EEXIST, errno.ENOTEMPTY):
            raise ContractError("output_exists")
        if number in (errno.ENOSYS, errno.EINVAL, errno.ENOTSUP, errno.EOPNOTSUPP):
            raise ContractError("publication_unsupported")
        raise OSError(number, os.strerror(number))


def generate(output_path: Path, output) -> None:
    require(not output_path.exists() and not output_path.is_symlink(), "output_exists")
    require(output_path.parent.is_dir(), "missing_output_parent")
    temporary = Path(tempfile.mkdtemp(prefix=".cloud-contract-", dir=output_path.parent))
    try:
        for relative, data in output.items():
            target = temporary / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        rename_noreplace(temporary, output_path)
    finally:
        if temporary.exists():
            shutil.rmtree(temporary)


class ContractArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        raise ContractError("cli_usage")


def main(argv=None) -> int:
    parser = ContractArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    create = commands.add_parser("generate", help="compile into a NEW directory; never overwrite a package")
    create.add_argument("--manifest", type=Path, default=DEFAULT_PACKAGE / "manifest.json")
    create.add_argument("--output", type=Path, required=True)
    check = commands.add_parser("check", help="require every package byte and path to match its manifest")
    check.add_argument("--package", type=Path, default=DEFAULT_PACKAGE)
    validate = commands.add_parser("validate", help="structural validation only; no auth, decryption, freshness or effect proof")
    validate.add_argument("--package", type=Path, default=DEFAULT_PACKAGE)
    validate.add_argument("--kind", required=True, help="one of: " + ", ".join(sorted(SCHEMA_NAMES)))
    validate.add_argument("--canonical", action="store_true", help="also require envelope canonical JSON bytes")
    validate.add_argument("--expected-sha256", help="pin exact input bytes; requires exactly one input")
    validate.add_argument("inputs", type=Path, nargs="+")
    for command in (create, check, validate):
        command.add_argument("--expected-source-digest", help="trusted SHA-256 pin; defaults to sibling source.sha256")
    try:
        args = parser.parse_args(argv)
        if args.command == "validate":
            require(args.kind in SCHEMA_NAMES, "unknown_kind")
        if args.command == "generate":
            raw, manifest = read_manifest(args.manifest, args.expected_source_digest)
            output, index = compile_package(raw, manifest)
            generate(args.output, output)
        else:
            manifest, output, index = check_package(args.package, args.expected_source_digest)
        result = {"ok": True, "command": args.command, "source_sha256": sha256(output["manifest.json"]),
                  "package_sha256": sha256(output["integrity.json"]), "files": len(output),
                  "valid_fixtures": sum(row["valid"] for row in index),
                  "invalid_fixtures": sum(not row["valid"] for row in index)}
        if args.command == "validate":
            require(args.expected_sha256 is None or len(args.inputs) == 1, "ambiguous_input_digest")
            # Buffer the whole result, so a later invalid member cannot leave partial success on stdout.
            for path in args.inputs:
                data = regular_bytes(path, MAX_INPUT_BYTES)
                if args.expected_sha256 is not None:
                    require(sha256(data) == digest_text(args.expected_sha256), "wrong_digest")
                validate_bytes(data, args.kind, manifest, args.canonical)
            result.update({"validated": len(args.inputs), "kind": args.kind, "scope": "structure_only"})
        sys.stdout.buffer.write(document(result))
        return 0
    except ContractError as error:
        code = error.code
    except (OSError, UnicodeError, ValueError, KeyError, TypeError, RecursionError):
        code = "unreadable_or_malformed_input"
    sys.stderr.buffer.write(document({"ok": False, "code": "cloud_contract_" + code}))
    return 2


if __name__ == "__main__":
    sys.exit(main())
