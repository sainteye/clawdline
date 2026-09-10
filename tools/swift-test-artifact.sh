#!/bin/bash
# Sourced by test.sh. This helper never acquires a compile slot of its own.

clawdline_swift_artifact_python() {
  python3 - "$@" <<'PY'
import hashlib
import ctypes
import errno
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import uuid

SCHEMA = "clawdline-swift-test-artifact-v1"


class Refusal(Exception):
    def __init__(self, code, status=2):
        self.code, self.status = code, status


def refuse(code, status=2):
    raise Refusal(code, status)


def canonical(value):
    return json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(",", ":")).encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def file_digest(path):
    result = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


def inventory(path, ancestors=()):
    """Content, modes, names and link destinations; never trust timestamps as identity."""
    path = Path(path)
    info = path.lstat()
    mode = stat.S_IMODE(info.st_mode)
    if path.is_symlink():
        # SDKs contain intentional dangling links and links back to an ancestor (Ruby headers
        # do this). An ancestor is already being fully enumerated, so seal the graph edge;
        # do not recursively enumerate it forever or omit its other children.
        target = path.resolve(strict=False)
        if str(path.absolute()) in ancestors or str(target) in ancestors:
            return {"link": os.readlink(path), "mode": mode, "reference": str(target)}
        return {"link": os.readlink(path), "mode": mode, "target": str(target),
                "content": inventory(target, ancestors + (str(path.absolute()),))
                if target.exists() else {"absent": True}}
    if stat.S_ISREG(info.st_mode):
        return {"sha256": file_digest(path), "size": info.st_size, "mode": mode}
    if stat.S_ISDIR(info.st_mode):
        if str(path.absolute()) in ancestors:
            refuse("resource_directory_cycle")
        return {"mode": mode, "entries": [
            [entry.name, inventory(entry, ancestors + (str(path.absolute()),))]
            for entry in sorted(path.iterdir(), key=lambda entry: os.fsencode(entry.name))]}
    refuse("unsupported_input_file_type")


def command(argv, cwd=None, env=None):
    result = subprocess.run(argv, cwd=cwd, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, check=False)
    if result.returncode != 0 or not result.stdout.strip():
        refuse("identity_command_failed")
    return result.stdout


def selection(required=False):
    raw = os.environ.get("CLAWDLINE_TEST_GROUPS")
    if raw is None and not required:
        return []
    if raw is None or not raw or not raw.strip():
        refuse("focused_selection_empty")
    # Match Swift's newline split/Set: omit empty rows, preserve whitespace inside titles.
    selected = [title for title in raw.split("\n") if title]
    manifest = Path("Tests/TestGroupManifest.swift").read_text()
    match = re.search(r"let expectedOrderedTestGroupTitles: \[String\] = (\[.*?\n\])", manifest, re.S)
    if not match:
        refuse("focused_manifest_invalid")
    try:
        # The checked-in Swift array uses both literals and literal + literal continuations.
        # Parse that closed grammar, never eval Swift or quietly skip an unfamiliar expression.
        remaining, titles = match[1][1:].lstrip(), []
        decoder = json.JSONDecoder()
        while not remaining.startswith("]"):
            title, end = decoder.raw_decode(remaining)
            if not isinstance(title, str):
                raise ValueError()
            remaining = remaining[end:].lstrip()
            while remaining.startswith("+"):
                part, end = decoder.raw_decode(remaining[1:].lstrip())
                if not isinstance(part, str):
                    raise ValueError()
                title += part
                remaining = remaining[1:].lstrip()[end:].lstrip()
            titles.append(title)
            if remaining.startswith(","):
                remaining = remaining[1:].lstrip()
            elif not remaining.startswith("]"):
                raise ValueError()
        if remaining != "]":
            raise ValueError()
    except ValueError:
        refuse("focused_manifest_invalid")
    if (not titles or any(not isinstance(title, str) or not title for title in titles)
            or len(set(titles)) != len(titles)):
        refuse("focused_manifest_invalid")
    if set(selected) - set(titles):
        refuse("focused_selection_unknown")
    return [title for title in titles if title in set(selected)]


def verify_focused(log):
    selected = selection(required=True)
    lines = Path(log).read_text().splitlines()
    # A focused log may contain no full result, Cloud completion or combined seal, even if
    # it also contains a plausible focused line. Never turn those bytes into full evidence.
    if any(re.match(r"[0-9]+ checks passed$|[0-9]+ of [0-9]+.*failed", line)
           or line.startswith(("CLAWDLINE_CLOUD_TESTS_COMPLETE", "CLAWDLINE_TEST_SEAL"))
           for line in lines):
        refuse("focused_receipt_full_or_failed", 125)
    receipts = [line for line in lines if re.fullmatch(r"[0-9]+ focused checks passed", line)]
    executed = [line[len("  ✓ "):] for line in lines if line.startswith("  ✓ ")]
    if (len(receipts) != 1 or not re.fullmatch(r"[1-9][0-9]* focused checks passed", receipts[0])
            or executed != selected or any(line.startswith("  ✗ ") for line in lines)):
        refuse("focused_receipt_missing_or_mismatched", 125)


def repository_subject(root):
    def git(*args):
        return command(["git", *args], cwd=root)
    top = Path(os.fsdecode(git("rev-parse", "--show-toplevel")).strip()).resolve()
    if top != root:
        refuse("repository_root_required")
    common = Path(os.fsdecode(git("rev-parse", "--git-common-dir")).strip())
    common = (root / common).resolve()
    head = subprocess.run(["git", "rev-parse", "--verify", "--quiet", "HEAD^{tree}"],
                          cwd=root, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if head.returncode == 0:
        tree = head.stdout.decode().strip()
        if not re.fullmatch(r"[a-f0-9]{40}|[a-f0-9]{64}", tree):
            refuse("repository_tree_invalid")
        head_state = "present"
    elif head.returncode == 1:
        # The documented archive + git init + git add snapshot has an index but no commit.
        # Prove it is an unborn symbolic ref, then bind the actual overlay without inventing
        # a commit/tree receipt. Other Git failures are not treated as an empty repository.
        ref = git("symbolic-ref", "--quiet", "HEAD").decode().strip()
        missing = subprocess.run(["git", "show-ref", "--verify", "--quiet", ref], cwd=root,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if missing.returncode != 1:
            refuse("repository_head_unreadable")
        tree, head_state = None, "unborn"
    else:
        refuse("repository_head_unreadable")
    tracked = git("ls-files", "--stage", "-z").split(b"\0")
    files = {}
    for row in tracked:
        if not row:
            continue
        header, name = row.split(b"\t", 1)
        mode, _, stage = header.split()
        if stage != b"0" or mode == b"160000":
            refuse("repository_unmerged_or_submodule")
        files[os.fsdecode(name)] = None
    # Empty output is legitimate for this one git query.
    untracked = subprocess.run(["git", "ls-files", "--others", "--exclude-standard", "-z"],
                               cwd=root, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if untracked.returncode:
        refuse("repository_inventory_unreadable")
    for name in untracked.stdout.split(b"\0"):
        if name:
            files[os.fsdecode(name)] = None
    for name in files:
        path = root / name
        # Git overlay identity binds the link itself; compile resources are additionally
        # traversed below, including ignored children and targets outside the repository.
        if path.is_symlink():
            files[name] = {"link": os.readlink(path)}
        elif path.exists():
            files[name] = inventory(path)
        else:
            files[name] = {"absent": True}
    if not files:
        refuse("repository_inventory_empty")
    return {"repository_sha256": digest(os.fsencode(common)), "cwd": str(root),
            "head_tree": tree, "head_state": head_state,
            "working_overlay_sha256": digest(canonical(files))}


def check_lock(lock, token):
    if not token:
        refuse("artifact_lock_required", 73)
    fields = {}
    for line in (Path(lock) / "holder.txt").read_text().splitlines():
        key, sep, value = line.partition("=")
        if sep:
            if key in fields:
                refuse("artifact_lock_ambiguous", 73)
            fields[key] = value
    if fields.get("token") != token:
        refuse("artifact_lock_lost", 73)


def parse_compile(argv):
    # Closed recipe: arbitrary response files, plugin loaders, search paths and linker
    # inputs need a manifest extension before they can become cacheable compiler flags.
    flags, sources = [], []
    i = 0
    while i < len(argv) and argv[i] != "--":
        flag = argv[i]
        if flag in ("-swift-version", "-target", "-j", "-framework", "-D"):
            if i + 1 == len(argv) or not argv[i + 1] or argv[i + 1].startswith("-"):
                refuse("artifact_flags_invalid")
            patterns = {"-swift-version": r"[0-9]+(?:\.[0-9]+)?", "-target": r"[A-Za-z0-9_.-]+",
                        "-j": r"[1-9][0-9]*", "-framework": r"[A-Za-z_][A-Za-z0-9_]*",
                        "-D": r"[A-Za-z_][A-Za-z0-9_]*"}
            if not re.fullmatch(patterns[flag], argv[i + 1]):
                refuse("artifact_flag_value_not_manifested")
            flags.extend(argv[i:i + 2])
            i += 2
        elif flag in ("-O", "-Onone", "-Osize", "-g", "-enable-testing"):
            flags.append(flag)
            i += 1
        else:
            refuse("artifact_flag_not_manifested")
    if i == len(argv):
        refuse("artifact_sources_missing")
    sources = argv[i + 1:]
    if flags.count("-target") != 1 or flags.count("-swift-version") != 1:
        refuse("artifact_target_or_language_ambiguous")
    if not sources or len(set(sources)) != len(sources):
        refuse("artifact_sources_empty_or_duplicate")
    for source in sources:
        path = Path(source)
        if (path.is_absolute() or ".." in path.parts or not source.endswith(".swift")
                or path.parts[0] not in ("Sources", "Tests") or not path.is_file()):
            refuse("artifact_source_not_manifested")
    return flags, sources


def inode(info):
    return info.st_dev, info.st_ino


def within(path, ancestor):
    """Compare existing ancestors by filesystem identity, including case-folded aliases."""
    # Resolve links before walking parents: an alias may point at a *descendant* of ancestor,
    # whose lexical parents live elsewhere. Stat still handles unresolved case spelling.
    path, ancestor = Path(path).resolve(), Path(ancestor).resolve()
    if path == ancestor or ancestor in path.parents:
        return True
    if not ancestor.exists():
        return False
    expected = inode(ancestor.stat())
    for parent in (path, *path.parents):
        if parent.exists() and inode(parent.stat()) == expected:
            return True
    return False


def compiler_environment(compiler_arg, sdk_arg, target):
    if sys.platform != "darwin":
        refuse("artifact_platform_unsupported")
    compiler, sdk = Path(compiler_arg).absolute(), Path(sdk_arg).resolve()
    resolved = compiler.resolve()
    if (not compiler.is_file() or not os.access(compiler, os.X_OK)
            or compiler.parent.name != "bin" or resolved.parent.name != "bin"):
        refuse("artifact_compiler_unavailable")
    toolchain, invoked_toolchain = resolved.parent.parent, compiler.parent.parent.resolve()
    if (not sdk.is_dir() or sdk == Path("/") or toolchain == Path("/")
            or invoked_toolchain == Path("/")):
        refuse("artifact_sdk_or_toolchain_unavailable")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", target):
        refuse("artifact_target_or_language_ambiguous")
    probe_env = {"PATH": str(compiler.parent) + ":/usr/bin:/bin", "LC_ALL": "C", "LANG": "C"}
    version = command([str(compiler), "--version"], env=probe_env).decode()
    target_info = command([str(compiler), "-print-target-info", "-target", target,
                           "-sdk", str(sdk)], env=probe_env).decode()
    try:
        target_facts = json.loads(target_info)
        # Apple Swift 6.2.4 spells the compiler-reported runtime closure `paths` at the
        # document root. Accept that one production shape only: the former `resourcePaths`
        # spelling existed solely in this helper's fake compiler, and accepting both would make
        # a mixed document ambiguous about which closure was sealed.
        required_target_fields = {"compilerVersion", "target", "paths"}
        allowed_target_fields = required_target_fields | {"swiftCompilerTag"}
        if (not isinstance(target_facts, dict)
                or not required_target_fields.issubset(target_facts)
                or set(target_facts) - allowed_target_fields
                or not isinstance(target_facts["compilerVersion"], str)
                or not target_facts["compilerVersion"]
                or ("swiftCompilerTag" in target_facts
                    and not isinstance(target_facts["swiftCompilerTag"], str))
                or not isinstance(target_facts["target"], dict)):
            raise ValueError()
        reported = target_facts["paths"]
        if (not isinstance(reported, dict) or not isinstance(reported.get("runtimeResourcePath"), str)
                or set(reported) - {"sdkPath", "runtimeResourcePath", "runtimeLibraryPaths",
                                    "runtimeLibraryImportPaths"}):
            raise ValueError()
        reported_sdk = reported.get("sdkPath")
        if (reported_sdk is not None
                and (not isinstance(reported_sdk, str) or not reported_sdk
                     or not Path(reported_sdk).is_absolute()
                     or Path(reported_sdk).resolve() != sdk)):
            raise ValueError()
        paths = [reported["runtimeResourcePath"]]
        for key in ("runtimeLibraryPaths", "runtimeLibraryImportPaths"):
            values = reported.get(key, [])
            if not isinstance(values, list):
                raise ValueError()
            paths.extend(values)
        if any(not isinstance(p, str) or not p or not Path(p).is_absolute()
               or Path(p).resolve() == Path("/") for p in paths):
            raise ValueError()
        if not Path(paths[0]).is_dir():
            raise ValueError()
    except (ValueError, TypeError, KeyError):
        refuse("artifact_compiler_resources_unprovable")
    closure = []
    for name in dict.fromkeys(paths):
        path = Path(name).resolve()
        # These roots are fully inventoried below; record the edge without hashing them twice.
        covered = next((parent for parent in (toolchain, invoked_toolchain, sdk)
                        if within(path, parent)), None)
        closure.append({"path": name, "resolved_path": str(path),
                        "content": {"covered_by": str(covered)} if covered else
                        inventory(Path(name)) if Path(name).exists() else {"absent": True}})
    return {"compiler": {"path": str(compiler), "resolved_path": str(resolved),
                         "bytes": inventory(compiler), "resolved_bytes": inventory(resolved),
                         "version": version, "target_info": target_info, "resource_closure": closure},
            "toolchain": {"path": str(toolchain), "content": inventory(toolchain)},
            "invoked_toolchain": {"path": str(invoked_toolchain),
                                  "content": {"covered_by": str(toolchain)} if invoked_toolchain == toolchain
                                  else inventory(invoked_toolchain)},
            "sdk": {"path": str(sdk), "content": inventory(sdk)},
            "host": {"uname": list(os.uname()), "macos_build":
                     command(["/usr/bin/sw_vers", "-buildVersion"]).decode()}}


def environment_identity(compiler, sdk, target):
    # Used before a durable ledger reservation can answer reusable. No compile, cache write,
    # machine lock or caller-supplied digest is involved; use the inner helper's exact closure.
    facts = {"schema": SCHEMA, "environment": compiler_environment(compiler, sdk, target),
             "target": target, "recipe": [[path, inventory(path)] for path in
                ("test.sh", "tools/swift-source-manifest.sh", "tools/swift-test-artifact.sh",
                 "tools/verified-test-run.mjs")], "resources": inventory("Resources")}
    print(digest(canonical(facts)))


def publish_exclusive(artifact, destination):
    # macOS sys/stdio.h: RENAME_EXCL=0x4, renameatx_np available since 10.12.
    # No exists()+rename fallback: even an empty directory appearing now must survive.
    libc = ctypes.CDLL(None, use_errno=True)
    try:
        rename_exclusive = libc.renameatx_np
    except AttributeError:
        refuse("artifact_publication_exclusive_unsupported", 74)
    rename_exclusive.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    rename_exclusive.restype = ctypes.c_int
    source_fd = os.open(artifact.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        target_fd = os.open(destination.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            result = rename_exclusive(source_fd, os.fsencode(artifact.name),
                                      target_fd, os.fsencode(destination.name), 0x4)
            if result:
                code = ctypes.get_errno()
                if code in (errno.EEXIST, errno.ENOTEMPTY):
                    refuse("artifact_publication_conflict", 74)
                if code in (errno.ENOTSUP, errno.ENOSYS, errno.EINVAL):
                    refuse("artifact_publication_exclusive_unsupported", 74)
                refuse("artifact_publication_failed", 74)
        finally:
            os.close(target_fd)
    finally:
        os.close(source_fd)


def compile_artifact(argv):
    output, cache_arg, compiler_arg, sdk_arg, lock, token, resource_json, *compile_args = argv
    root = Path.cwd().resolve()
    # Retain the requested parent spelling so a later symlink change cannot escape detection.
    output = Path(output).absolute()
    cache = Path(cache_arg).resolve()
    if within(cache, root):
        refuse("artifact_cache_must_be_outside_repository")
    if within(output.parent, cache) or within(output, cache):
        refuse("artifact_output_must_be_outside_cache")
    compiler = Path(compiler_arg).absolute()
    sdk = Path(sdk_arg).resolve()
    flags, sources = parse_compile(compile_args)
    resources = json.loads(resource_json)
    if not resources or any(Path(p).is_absolute() or ".." in Path(p).parts for p in resources):
        refuse("artifact_resources_invalid")
    # The invocation contains every ordered argument. The only relocations are private output,
    # home, module-cache and temp locations; those directories start empty on every compile.
    normalized_argv = [*flags, "-sdk", str(sdk), "-tools-directory", str(compiler.parent),
                       "-module-cache-path", "@scratch@/modules",
                       "-o", "@output@/clawdline-tests", *sources]
    normalized_env = {"PATH": str(compiler.parent) + ":/usr/bin:/bin", "LC_ALL": "C",
                      "LANG": "C", "HOME": "@scratch@/home", "TMPDIR": "@scratch@/tmp"}
    target = flags[flags.index("-target") + 1]

    def identity():
        return {"schema": SCHEMA, "subject": repository_subject(root),
                **compiler_environment(compiler, sdk, target),
                "argv": normalized_argv, "environment": normalized_env,
                "sources": [[source, inventory(root / source)] for source in sources],
                "resources": [[path, inventory(root / path)] for path in resources]}

    # The existing test target imports Apple frameworks. A Linux compiler needs a separately
    # manifested target/runtime closure; accepting the host's implicit /usr would not be exact.
    if sys.platform != "darwin":
        refuse("artifact_platform_unsupported")
    check_lock(lock, token)
    facts = identity()
    key = digest(canonical(facts))
    cache.mkdir(parents=True, exist_ok=True, mode=0o700)
    if cache.is_symlink() or cache.stat().st_uid != os.getuid() or cache.stat().st_mode & 0o022:
        refuse("artifact_cache_not_private")
    # Recheck after creation: an initially absent case-folded cache path now has an inode.
    if within(output.parent, cache) or within(output, cache):
        refuse("artifact_output_must_be_outside_cache")
    output.parent.mkdir(parents=True, exist_ok=True)
    output_parent = inode(output.parent.stat())
    destination = cache / key

    def verified_binary():
        if not destination.exists() and not destination.is_symlink():
            return None
        if destination.is_symlink() or not destination.is_dir():
            refuse("artifact_corrupt", 74)
        if set(p.name for p in destination.iterdir()) != {"metadata.json", "clawdline-tests"}:
            refuse("artifact_incomplete", 74)
        metadata, binary = destination / "metadata.json", destination / "clawdline-tests"
        if metadata.is_symlink() or binary.is_symlink() or not metadata.is_file() or not binary.is_file():
            refuse("artifact_corrupt", 74)
        if binary.stat().st_nlink != 1 or binary.stat().st_uid != os.getuid():
            refuse("artifact_binary_not_private", 74)
        try:
            metadata_bytes = metadata.read_bytes()
            saved = json.loads(metadata_bytes)
        except (ValueError, UnicodeError):
            refuse("artifact_metadata_invalid", 74)
        expected = {"schema": SCHEMA, "identity_sha256": key, "identity": facts,
                    "binary_sha256": file_digest(binary), "binary_size": binary.stat().st_size,
                    "binary_mode": stat.S_IMODE(binary.stat().st_mode)}
        if (saved != expected or metadata_bytes != canonical(expected) + b"\n"
                or expected["binary_size"] <= 0 or expected["binary_mode"] != 0o500):
            refuse("artifact_digest_mismatch", 74)
        return binary, saved

    def deliver(binary, metadata, reused):
        check_lock(lock, token)
        def validate_parent(parent_fd=None):
            if (inode(output.parent.stat()) != output_parent
                    or parent_fd is not None and inode(os.fstat(parent_fd)) != output_parent):
                refuse("artifact_output_parent_changed", 74)
            if within(output.parent, cache):
                refuse("artifact_output_must_be_outside_cache")
        validate_parent()
        # Open the resolved directory, then check against the original filesystem identity.
        # All writes/cleanup use this descriptor, never a parent path that can be redirected.
        parent_fd = os.open(output.parent.resolve(), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        staged = ".clawdline-artifact-" + uuid.uuid4().hex
        created = False
        try:
            validate_parent(parent_fd)
            fd = os.open(staged, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=parent_fd)
            created = True
            with os.fdopen(fd, "w+b") as stream, binary.open("rb") as source:
                shutil.copyfileobj(source, stream)
                stream.flush()
                os.fsync(stream.fileno())
                stream.seek(0)
                copied = hashlib.sha256()
                for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                    copied.update(chunk)
                if copied.hexdigest() != metadata["binary_sha256"]:
                    refuse("artifact_copy_changed", 74)
                os.fchmod(stream.fileno(), 0o700)
                # Re-measure before a hit becomes usable, not just on a cache miss.
                if identity() != facts:
                    refuse("artifact_inputs_changed", 74)
                check_lock(lock, token)
                validate_parent(parent_fd)
                private = os.fstat(stream.fileno())
                if (not stat.S_ISREG(private.st_mode) or private.st_nlink != 1
                        or private.st_uid != os.getuid() or inode(private) == inode(os.fstat(source.fileno()))
                        or inode(private) != inode(os.stat(staged, dir_fd=parent_fd, follow_symlinks=False))):
                    refuse("artifact_copy_not_private", 74)
                os.replace(staged, output.name, src_dir_fd=parent_fd, dst_dir_fd=parent_fd)
                created = False
                validate_parent(parent_fd)
                delivered = os.stat(output.name, dir_fd=parent_fd, follow_symlinks=False)
                if inode(delivered) != inode(private) or delivered.st_nlink != 1:
                    refuse("artifact_copy_not_private", 74)
        finally:
            if created:
                os.unlink(staged, dir_fd=parent_fd)
            os.close(parent_fd)
        print("CLAWDLINE_SWIFT_TEST_ARTIFACT " + canonical({
            "version": 1, "kind": "compile_only", "reused": reused,
            "identity_sha256": key, "binary_sha256": metadata["binary_sha256"]}).decode())

    hit = verified_binary()
    if hit:
        deliver(*hit, reused=True)
        return
    publishing = cache / (key + ".publishing")
    try:
        publishing.mkdir(mode=0o700)
    except FileExistsError:
        refuse("artifact_publication_busy", 73)
    try:
        # Another publisher may have finished between the first lookup and our reservation.
        hit = verified_binary()
        if hit:
            deliver(*hit, reused=True)
            return
        artifact = publishing / "artifact"
        artifact.mkdir(mode=0o700)
        scratch = publishing / "scratch"
        for part in ("home", "tmp", "modules"):
            (scratch / part).mkdir(parents=True, mode=0o700)
        binary = artifact / "clawdline-tests"
        real_env = {key: value.replace("@scratch@", str(scratch)) for key, value in normalized_env.items()}
        real_argv = [*flags, "-sdk", str(sdk), "-tools-directory", str(compiler.parent),
                     "-module-cache-path", str(scratch / "modules"), "-o", str(binary), *sources]
        check_lock(lock, token)
        status = subprocess.run([str(compiler), *real_argv], cwd=root, env=real_env).returncode
        if status:
            shell_status = status if status > 0 else 128 - status
            failure = {"version": 1, "kind": "exit" if status > 0 else "signal",
                       "returncode": status, "exit_status": shell_status}
            if status < 0:
                failure["signal"] = -status
            print("CLAWDLINE_SWIFT_TEST_COMPILE_FAILURE " + canonical(failure).decode(), file=sys.stderr)
            refuse("artifact_compile_failed", shell_status)
        if not binary.is_file() or binary.is_symlink() or binary.stat().st_size <= 0 or not os.access(binary, os.X_OK):
            refuse("artifact_binary_missing", 74)
        if identity() != facts:
            refuse("artifact_inputs_changed", 74)
        os.chmod(binary, 0o500)
        metadata = {"schema": SCHEMA, "identity_sha256": key, "identity": facts,
                    "binary_sha256": file_digest(binary), "binary_size": binary.stat().st_size,
                    "binary_mode": 0o500}
        with (artifact / "metadata.json").open("wb") as stream:
            stream.write(canonical(metadata) + b"\n")
            stream.flush()
            os.fsync(stream.fileno())
        with binary.open("rb") as stream:
            os.fsync(stream.fileno())
        check_lock(lock, token)
        publish_exclusive(artifact, destination)
        # The directory rename publishes the pair atomically; validate the published pair too.
        deliver(*verified_binary(), reused=False)
    finally:
        shutil.rmtree(publishing)


try:
    mode, *args = sys.argv[1:]
    if mode == "selection":
        selection(required=args == ["required"])
    elif mode == "focused-receipt" and len(args) == 1:
        verify_focused(args[0])
    elif mode == "compile" and len(args) >= 9:
        compile_artifact(args)
    elif mode == "environment" and len(args) == 3:
        environment_identity(*args)
    else:
        refuse("artifact_usage")
except Refusal as error:
    print("swift-test-artifact: " + error.code, file=sys.stderr)
    sys.exit(error.status)
except (OSError, ValueError, RuntimeError, UnicodeError) as error:
    # Paths/environment may contain private data; keep refusals typed and omit raw inputs.
    print("swift-test-artifact: artifact_input_or_io_unreadable (" + type(error).__name__ + ")", file=sys.stderr)
    sys.exit(74)
PY
}

clawdline_validate_swift_test_selection() {
  clawdline_swift_artifact_python selection "$@"
}

clawdline_verify_focused_test_receipt() {
  clawdline_swift_artifact_python focused-receipt "$1"
}

clawdline_swift_test_artifact_environment() {
  local artifact_compiler artifact_sdk
  . tools/swift-source-manifest.sh || return $?
  artifact_compiler=${CLAWDLINE_SWIFT_TEST_SWIFTC:-$(xcrun --find swiftc)} || return $?
  artifact_sdk=${SDKROOT:-$(xcrun --sdk macosx --show-sdk-path)} || return $?
  clawdline_swift_artifact_python environment "$artifact_compiler" "$artifact_sdk" \
    "${clawdline_swift_test_target:-}"
}

clawdline_swift_test_artifact() {
  # Confirm in the owning shell: a subshell cannot see its parent's renewer in `jobs -p`
  # and would make the existing confirmation start a second renewal loop.
  if ! declare -F clawdline_confirm_suite_lock >/dev/null 2>&1; then
    echo 'swift-test-artifact: artifact_lock_required' >&2
    return 73
  fi
  clawdline_confirm_suite_lock || return $?
  if [ "$#" -lt 2 ]; then
    echo 'swift-test-artifact: artifact_usage' >&2
    return 2
  fi
  local artifact_output=$1 artifact_compiler artifact_sdk artifact_resources
  shift
  if [ -z "${CLAWDLINE_SWIFT_TEST_CACHE_DIR:-}" ]; then
    echo 'swift-test-artifact: artifact_cache_directory_required' >&2
    return 2
  fi
  artifact_compiler=${CLAWDLINE_SWIFT_TEST_SWIFTC:-$(xcrun --find swiftc)} || return $?
  artifact_sdk=${SDKROOT:-$(xcrun --sdk macosx --show-sdk-path)} || return $?
  artifact_resources=$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1:]))' \
    "${clawdline_swift_test_compile_resources[@]}") || return $?
  clawdline_swift_artifact_python compile "$artifact_output" "$CLAWDLINE_SWIFT_TEST_CACHE_DIR" \
    "$artifact_compiler" "$artifact_sdk" "$CLAWDLINE_SUITE_LOCK_DIR" "$clawdline_suite_lock_token" \
    "$artifact_resources" "$@"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  echo 'swift-test-artifact: source_from_test_sh_required' >&2
  exit 2
fi
