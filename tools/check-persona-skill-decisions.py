#!/usr/bin/env python3
"""Check the public persona/skill decision ledger against the closed Go catalog."""

import argparse
import copy
import json
import posixpath
import re
import sys
from pathlib import Path
from urllib.request import Request, urlopen


ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "docs/persona-skill-decisions.json"
HEX40 = re.compile(r"[0-9a-f]{40}\Z")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
REPO = re.compile(r"https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)\Z")


class Invalid(ValueError):
    pass


def require(ok, message):
    if not ok:
        raise Invalid(message)


def catalog_ids():
    source = (ROOT / "internal/domain/persona/persona.go").read_text()
    match = re.search(r"var Order = \[\]string\{(.*?)\n\}", source, re.S)
    require(match is not None, "persona.Order was not found")
    ids = re.findall(r'"([a-z]+(?:-[a-z]+)*)"', match.group(1))
    require(len(ids) == 42 and len(set(ids)) == 42, "persona.Order must contain 42 unique IDs")
    return ids


def valid_path(path):
    return (isinstance(path, str) and path and not path.startswith("/")
            and posixpath.normpath(path) == path and not path.startswith("../"))


def pinned_url(repo, commit, path):
    return f"{repo}/blob/{commit}/{path}"


def local_references(content, path):
    links = re.findall(r"\[[^\]]+\]\(([^)\s]+)\)", content)
    links += re.findall(r"`((?:references|assets|scripts)/[\w./-]+\.(?:md|py|sh|js|ts|json))`", content)
    found = set()
    for raw in links:
        raw = raw.split("#", 1)[0].strip()
        if not raw or re.match(r"^(?:https?://|mailto:)", raw):
            continue
        if not re.search(r"\.(?:md|py|sh|js|ts|json|csv|ya?ml|txt)$", raw, re.I):
            continue
        name = posixpath.normpath(posixpath.join(posixpath.dirname(path), raw))
        if valid_path(name) and name != path:
            found.add(name)
    return found


def validate(data):
    ids = catalog_ids()
    doc_ids = re.findall(r"^\| `([^`]+)` \|", (ROOT / "docs/personas.md").read_text(), re.M)
    require(doc_ids == ids, "docs/personas.md does not follow persona.Order")
    require(data.get("schema_version") == 1, "unsupported schema_version")
    roles = data.get("roles")
    require(isinstance(roles, list), "roles must be a list")
    got = [role.get("id") for role in roles if isinstance(role, dict)]
    require(len(got) == 42 and got == ids, "roles must match all 42 persona.Order IDs in order, once each")
    count = 0
    preview_count = 0
    for role in roles:
        rid = role["id"]
        candidates = role.get("candidates")
        require(isinstance(candidates, list), f"{rid}: candidates must be a list")
        if not candidates:
            require(role.get("conclusion") == "no_suitable_candidate", f"{rid}: empty candidates need a no_suitable_candidate conclusion")
            require(bool(role.get("zero_preview_reason")), f"{rid}: zero candidates need a reason")
        else:
            require(role.get("conclusion") == "bounded_task_candidate", f"{rid}: candidate conclusion is missing")
        eligible = 0
        for candidate in candidates:
            count += 1
            prefix = f"{rid}/{candidate.get('name', '?')}"
            require(candidate.get("fit_status") == "fits_bounded_task", f"{prefix}: bounded fit must be explicit")
            require(bool(candidate.get("fits")) and bool(candidate.get("limits")), f"{prefix}: fit and limits are required")
            require(candidate.get("research_round") in (1, 2), f"{prefix}: research round is required")
            repo = candidate.get("source_repository")
            require(isinstance(repo, str) and REPO.fullmatch(repo), f"{prefix}: invalid source repository")
            commit = candidate.get("source_commit")
            require(isinstance(commit, str) and HEX40.fullmatch(commit), f"{prefix}: source commit must be a full SHA")
            skill_path = candidate.get("skill_path")
            license_path = candidate.get("license_path")
            require(valid_path(skill_path) and skill_path.endswith("/SKILL.md") or skill_path == "SKILL.md", f"{prefix}: invalid SKILL.md path")
            require(valid_path(license_path), f"{prefix}: invalid license path")
            require(candidate.get("skill_url") == pinned_url(repo, commit, skill_path), f"{prefix}: SKILL.md URL is not pinned to its source")
            require(candidate.get("license_url") == pinned_url(repo, commit, license_path), f"{prefix}: license URL is not pinned to its source")
            require(candidate.get("license") in ("MIT", "Apache-2.0"), f"{prefix}: license was not identified")
            require(isinstance(candidate.get("skill_sha256"), str) and HEX64.fullmatch(candidate["skill_sha256"]), f"{prefix}: missing SKILL.md digest")
            require(isinstance(candidate.get("license_sha256"), str) and HEX64.fullmatch(candidate["license_sha256"]), f"{prefix}: missing license digest")
            audit = candidate.get("reference_audit")
            require(isinstance(audit, dict) and audit.get("status") in ("open", "closed_for_manual_preview"), f"{prefix}: reference audit outcome is missing")
            require(bool(audit.get("outcome")), f"{prefix}: reference audit needs a finding")
            if audit["status"] == "closed_for_manual_preview":
                files = audit.get("files")
                require(isinstance(files, list) and files, f"{prefix}: closed audit has no file manifest")
                paths = [item.get("path") for item in files if isinstance(item, dict)]
                require(len(paths) == len(files) and len(set(paths)) == len(paths) and skill_path in paths,
                        f"{prefix}: bundle paths must be unique and include SKILL.md")
                for item in files:
                    path = item["path"]
                    require(valid_path(path), f"{prefix}: invalid bundled path")
                    require(item.get("url") == pinned_url(repo, commit, path), f"{prefix}: bundled file is not pinned")
                    require(isinstance(item.get("sha256"), str) and HEX64.fullmatch(item["sha256"]), f"{prefix}: bundled file has no digest")
                    require(item.get("review") == "read_only_content_reviewed", f"{prefix}: bundled file review missing")
                entry = next(item for item in files if item["path"] == skill_path)
                require(entry["sha256"] == candidate["skill_sha256"], f"{prefix}: entry digest mismatch")
            else:
                require(isinstance(audit.get("observed_entry_references"), list), f"{prefix}: open audit needs an observed reference list")
            preview = candidate.get("preview")
            require(isinstance(preview, dict) and preview.get("status") in ("blocked", "eligible_for_manual_preview"), f"{prefix}: preview decision missing")
            require(bool(preview.get("reason")), f"{prefix}: preview decision needs a reason")
            if preview["status"] == "eligible_for_manual_preview":
                require(audit["status"] == "closed_for_manual_preview", f"{prefix}: preview depends on a closed reference audit")
                eligible += 1
                preview_count += 1
            adoption = candidate.get("adoption")
            require(isinstance(adoption, dict) and adoption.get("status") == "unverified" and adoption.get("evidence") is None,
                    f"{prefix}: no user adoption evidence exists for this research cycle")
        if eligible == 0:
            require(bool(role.get("zero_preview_reason")), f"{rid}: zero preview needs an explicit reason")
        else:
            require(role.get("zero_preview_reason") is None, f"{rid}: zero preview reason contradicts an eligible candidate")
    return count, preview_count


def verify_upstream(data):
    cache = {}
    def fetch(repo, commit, path):
        key = (repo, commit, path)
        if key not in cache:
            suffix = repo.removeprefix("https://github.com/")
            url = f"https://raw.githubusercontent.com/{suffix}/{commit}/{path}"
            with urlopen(Request(url, headers={"User-Agent": "Clawdline-skill-audit"}), timeout=20) as response:
                cache[key] = response.read()
        return cache[key]

    import hashlib
    for role in data["roles"]:
        for candidate in role["candidates"]:
            rid = role["id"]
            repo, commit = candidate["source_repository"], candidate["source_commit"]
            entry = fetch(repo, commit, candidate["skill_path"])
            require(hashlib.sha256(entry).hexdigest() == candidate["skill_sha256"], f"{rid}: upstream SKILL.md digest changed")
            license_bytes = fetch(repo, commit, candidate["license_path"])
            require(hashlib.sha256(license_bytes).hexdigest() == candidate["license_sha256"], f"{rid}: upstream license digest changed")
            audit = candidate["reference_audit"]
            if audit["status"] != "closed_for_manual_preview":
                continue
            files = {item["path"]: item for item in audit["files"]}
            found = set()
            todo = [candidate["skill_path"]]
            while todo:
                path = todo.pop()
                if path in found:
                    continue
                require(path in files, f"{rid}: referenced file omitted from preview bundle: {path}")
                body = fetch(repo, commit, path)
                require(hashlib.sha256(body).hexdigest() == files[path]["sha256"], f"{rid}: bundled digest mismatch: {path}")
                found.add(path)
                todo.extend(local_references(body.decode("utf-8"), path) - found)
            require(found == set(files), f"{rid}: preview bundle has unreferenced files")
    return len(cache), fetch


def self_test(data, fetch=None):
    def rejected(mutator, label, online=False):
        changed = copy.deepcopy(data)
        mutator(changed)
        try:
            validate(changed)
            if online:
                require(fetch is not None, "online self-test needs upstream cache")
                role = next(r for r in changed["roles"] if r["id"] == "accessibility")
                candidate = role["candidates"][0]
                files = {item["path"]: item for item in candidate["reference_audit"]["files"]}
                entry = candidate["skill_path"]
                refs = local_references(fetch(candidate["source_repository"], candidate["source_commit"], entry).decode(), entry)
                require(refs <= set(files), "referenced file omitted from preview bundle")
        except Invalid:
            return
        raise Invalid(f"self-test failed to reject {label}")

    rejected(lambda d: d["roles"].__setitem__(slice(0, 2), [d["roles"][1], d["roles"][0]]), "swapped IDs")
    rejected(lambda d: d["roles"].pop(0), "missing ID")
    rejected(lambda d: d["roles"].__setitem__(1, copy.deepcopy(d["roles"][0])), "duplicate ID")
    rejected(lambda d: d["roles"][0]["candidates"][0].__setitem__("skill_url", d["roles"][0]["candidates"][0]["skill_url"].replace(d["roles"][0]["candidates"][0]["source_commit"], "main")), "floating URL")
    rejected(lambda d: d["roles"][0]["candidates"][0].__setitem__("license_url", "https://github.com/other/repo/blob/main/LICENSE"), "wrong license evidence")
    rejected(lambda d: d["roles"][0]["candidates"][0]["preview"].__setitem__("status", "eligible_for_manual_preview"), "open audit preview")
    if fetch is not None:
        rejected(lambda d: next(r for r in d["roles"] if r["id"] == "accessibility")["candidates"][0]["reference_audit"]["files"].pop(), "omitted real reference", online=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verify-upstream", action="store_true", help="read pinned public GitHub files and check digests/reference closure")
    parser.add_argument("--self-test", action="store_true", help="prove malformed ledger examples are rejected")
    args = parser.parse_args()
    data = json.loads(DATA.read_text())
    count, previews = validate(data)
    fetch = None
    requests = 0
    if args.verify_upstream:
        requests, fetch = verify_upstream(data)
    if args.self_test:
        self_test(data, fetch)
    print(f"42/42 personas; {count} pinned candidates; {previews} manual previews; {requests} upstream file reads; valid")


if __name__ == "__main__":
    try:
        main()
    except (Invalid, OSError, ValueError) as exc:
        print(f"persona skill decisions: {exc}", file=sys.stderr)
        sys.exit(1)
