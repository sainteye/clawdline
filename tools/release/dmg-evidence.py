#!/usr/bin/env python3
"""DMG delivery evidence, separate from the installer's immutable manifest.

All inputs are local. Signature, attestation, notarization and privacy checks
belong to dmg.sh/gate.sh; this file never treats a JSON claim as their verdict.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import tarfile


INSTALL = ("Drag Clawdline Next.app to Applications, then open it from Applications.\n"
           "Requires an Apple Silicon Mac.\n\n"
           "將 Clawdline Next.app 拖移至 Applications，再從 Applications 開啟。\n"
           "需要 Apple Silicon Mac。\n")
WORKFLOWS = (".github/workflows/dmg.yml", ".github/workflows/release.yml")


def digest(path):
    with open(path, "rb") as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def read_json(path):
    return json.loads(Path(path).read_text())


def write_json(path, obj):
    Path(path).write_text(json.dumps(obj, ensure_ascii=False, indent=2) + "\n")


def inventory(root):
    """Include modes and empty directories; never follow a symbolic link."""
    result = []

    def visit(directory):
        for p in sorted(directory.iterdir()):
            s = p.lstat()
            item = {"path": p.relative_to(root).as_posix(), "mode": stat.S_IMODE(s.st_mode)}
            if p.is_symlink():
                item.update(type="symlink", target=os.readlink(p))
            elif p.is_dir():
                item.update(type="directory")
            elif p.is_file():
                item.update(type="file", sha256=digest(p), size=s.st_size)
            else:
                raise ValueError("unsupported filesystem object: " + str(p))
            result.append(item)
            if item["type"] == "directory":
                visit(p)
    visit(Path(root))
    return result


def extract_app(archive, destination):
    """Reject traversal, special files, duplicate names and symlink parents.

    Links inside the bundle must remain inside it. Hardlinks are refused: the
    official app packager does not need them. No archive-supplied code runs.
    """
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=False)
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        names, links = set(), set()
        for member in members:
            raw = member.name
            while raw.startswith("./"):
                raw = raw[2:]
            if raw in ("", ".") and member.isdir():
                continue
            path = PurePosixPath(raw)
            raw = str(path)
            if (path.is_absolute() or ".." in path.parts or not path.parts or
                    path.parts[0] != "Clawdline Next.app" or raw in names):
                raise ValueError("unsafe archive path: " + member.name)
            names.add(raw)
            if not (member.isdir() or member.isfile() or member.issym()):
                raise ValueError("unsupported archive entry: " + raw)
            if member.issym():
                links.add(path)
                target = member.linkname
                resolved = os.path.normpath(str(path.parent / target))
                if (os.path.isabs(target) or not resolved.startswith("Clawdline Next.app/")):
                    raise ValueError("escaping app symlink: " + raw)
        for name in names:
            if any(p in links for p in PurePosixPath(name).parents):
                raise ValueError("archive entry below symlink: " + name)
        # Extraction is manual so Python/macOS version-specific tar defaults
        # cannot weaken the policy above. Apply directory modes last.
        directories = []
        for member in members:
            raw = member.name
            while raw.startswith("./"):
                raw = raw[2:]
            raw = str(PurePosixPath(raw))
            if raw in ("", "."):
                continue
            p = destination / raw
            p.parent.mkdir(parents=True, exist_ok=True)
            if member.isdir():
                p.mkdir(exist_ok=True)
                directories.append((p, member.mode))
            elif member.issym():
                p.symlink_to(member.linkname)
            else:
                with tar.extractfile(member) as src, open(p, "xb") as dst:
                    import shutil
                    shutil.copyfileobj(src, dst)
                p.chmod(member.mode & 0o777)
        for p, mode in reversed(directories):
            p.chmod(mode & 0o777)
    if not (destination / "Clawdline Next.app/Contents/Info.plist").is_file():
        raise ValueError("archive has no Clawdline Next.app")


def source_info(source, version, source_commit):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[0-9]+)?", version):
        raise ValueError("not a publishable version")
    if not re.fullmatch(r"[0-9a-f]{40}", source_commit):
        raise ValueError("source commit must be a full SHA")
    manifest = read_json(source / "manifest.json")
    if manifest["version"] != version or manifest["commit"] != source_commit:
        raise ValueError("source version or commit differs from expected")
    name = f"Clawdline-{version}-macos-arm64.tar.gz"
    matches = [a for a in manifest["artifacts"] if a["name"] == name]
    if len(matches) != 1:
        raise ValueError("manifest must name exactly one arm64 app archive")
    a = matches[0]
    if (a["kind"], a["os"], a["arch"]) != ("app", "darwin", "arm64"):
        raise ValueError("source is not the arm64 app")
    if a["sha256"] != digest(source / name) or a["size"] != (source / name).stat().st_size:
        raise ValueError("source archive differs from manifest")
    sbom = read_json(source / (name + ".spdx.json"))
    return manifest, a, sbom


def documents(source, delivery, source_tree, mounted, version, source_commit,
              packaging_commit, workflow, fixture=False):
    if not re.fullmatch(r"[0-9a-f]{40}", packaging_commit) or workflow not in WORKFLOWS:
        raise ValueError("invalid packaging commit or workflow")
    manifest, app, app_sbom = source_info(source, version, source_commit)
    app_name = "Clawdline Next.app"
    if (mounted / app_name).is_symlink() or not (mounted / app_name).is_dir():
        raise ValueError("DMG App must be a directory, not a symlink")
    if stat.S_IMODE((source_tree / app_name).stat().st_mode) != stat.S_IMODE((mounted / app_name).stat().st_mode):
        raise ValueError("DMG app root mode differs from source archive")
    if inventory(source_tree / app_name) != inventory(mounted / app_name):
        raise ValueError("DMG app payload differs from source archive")
    if sorted(p.name for p in mounted.iterdir()) != ["Applications", app_name, "Install.txt"]:
        raise ValueError("DMG has unexpected or missing top-level entries")
    if not (mounted / "Applications").is_symlink() or os.readlink(mounted / "Applications") != "/Applications":
        raise ValueError("Applications must link to /Applications")
    if (mounted / "Install.txt").is_symlink() or (mounted / "Install.txt").read_text() != INSTALL:
        raise ValueError("incorrect installation instructions")
    name = f"Clawdline-{version}-macos-arm64.dmg"
    checks = {n: digest(source / n) for n in
              ("manifest.json", app["name"], app["name"] + ".spdx.json")}
    # A standard release builds before manifest signing; the evidence binds
    # manifest bytes, while gate.sh separately requires its trusted signature.
    evidence = {
        "schema": "clawdline-dmg-delivery-v1", "version": version, "arch": "arm64",
        "source_commit": source_commit, "packaging_commit": packaging_commit,
        "packaging_workflow": workflow, "fixture": fixture,
        "source_sha256": checks,
        "dmg": {"name": name, "sha256": digest(delivery / name), "size": (delivery / name).stat().st_size},
        "payload": inventory(mounted),
    }
    sbom = {
        "spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
        "name": name,
        "documentNamespace": f"https://github.com/sainteye/clawdline/releases/download/{version}/{name}.spdx.json#{evidence['dmg']['sha256']}",
        "creationInfo": {"created": manifest["committed_at"], "creators": ["Tool: clawdline tools/release/dmg-evidence.py"]},
        "externalDocumentRefs": [{"externalDocumentId": "DocumentRef-App", "spdxDocument": app_sbom["documentNamespace"],
                                  "checksum": {"algorithm": "SHA256", "checksumValue": checks[app["name"] + ".spdx.json"]}}],
        "packages": [{"SPDXID": "SPDXRef-DMG", "name": name, "versionInfo": version,
                      "downloadLocation": f"https://github.com/sainteye/clawdline/releases/download/{version}/{name}",
                      "filesAnalyzed": False, "primaryPackagePurpose": "ARCHIVE",
                      "checksums": [{"algorithm": "SHA256", "checksumValue": evidence["dmg"]["sha256"]}],
                      "sourceInfo": f"App source {source_commit}; packaging {packaging_commit} ({workflow}).",
                      "licenseConcluded": "NOASSERTION", "licenseDeclared": "NOASSERTION", "copyrightText": "NOASSERTION"}],
        "files": [],
        "relationships": [{"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": "SPDXRef-DMG"},
                          {"spdxElementId": "SPDXRef-DMG", "relationshipType": "GENERATED_FROM", "relatedSpdxElement": "DocumentRef-App:SPDXRef-Archive"}],
        "comment": "The external App SBOM inventories the unchanged App and its source dependencies. Files below inventory the mounted image; symlink digests hash symlink: plus target text. Directory metadata is in delivery evidence.",
    }
    for entry in evidence["payload"]:
        if entry["type"] == "directory":
            continue
        fid = "SPDXRef-File-" + hashlib.sha256(entry["path"].encode()).hexdigest()
        checksum = entry.get("sha256") or hashlib.sha256(("symlink:" + entry["target"]).encode()).hexdigest()
        sbom["files"].append({"SPDXID": fid, "fileName": "./" + entry["path"],
                              "checksums": [{"algorithm": "SHA256", "checksumValue": checksum}],
                              "licenseConcluded": "NOASSERTION", "copyrightText": "NOASSERTION"})
        sbom["relationships"].append({"spdxElementId": "SPDXRef-DMG", "relationshipType": "CONTAINS", "relatedSpdxElement": fid})
    return evidence, sbom


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("extract", "source", "generate", "verify", "instructions"))
    parser.add_argument("args", nargs="*")
    parser.add_argument("--fixture", action="store_true")
    opts = parser.parse_args()
    if opts.mode == "instructions":
        Path(opts.args[0]).write_text(INSTALL)
        return
    if opts.mode == "extract":
        extract_app(*opts.args)
        return
    if opts.mode == "source":
        source_info(Path(opts.args[0]), *opts.args[1:])
        return
    source, delivery, tree, mounted, version, sc, pc, workflow = opts.args
    evidence, sbom = documents(Path(source), Path(delivery), Path(tree), Path(mounted), version, sc, pc, workflow, opts.fixture)
    name = evidence["dmg"]["name"]
    for suffix, doc in ((".delivery.json", evidence), (".spdx.json", sbom)):
        path = Path(delivery) / (name + suffix)
        if opts.mode == "generate":
            write_json(path, doc)
        elif read_json(path) != doc:
            raise ValueError("delivery evidence or SBOM does not describe " + name)
    print("dmg evidence: unchanged App, image payload and source SBOM verified")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, tarfile.TarError) as error:
        raise SystemExit("dmg evidence: " + str(error))
