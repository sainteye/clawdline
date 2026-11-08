#!/usr/bin/env python3
"""Check the public persona/skill decision ledger against the closed Go catalog."""

import argparse
import copy
import json
import posixpath
import re
import sys
from pathlib import Path
from urllib.error import HTTPError
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
    require(ids and len(set(ids)) == len(ids), "persona.Order must contain unique IDs")
    return ids


MAX_ROLE_SKILLS = 4  # internal/domain/squad.MaxBuiltinRoleSkills


def catalog_skills():
    """Read the built-in catalog: each Skill's source by name, and each role's Skill names in order."""
    source = (ROOT / "internal/domain/squad/builtin_skills.go").read_text()
    specs = re.search(r"var builtinSkillSpecs = \[\]builtinSkillSpec\{(.*?)\n\}", source, re.S)
    require(specs is not None, "builtinSkillSpecs was not found")
    sources = {}
    for row in re.findall(r"^\t\{(.*)\},$", specs.group(1), re.M):
        match = re.fullmatch(r'"[a-z-]+", "([a-z-]+)", .*, (?:"(https://github\.com/[^"]+)"|(persona\.Original)), "[^"]+"', row)
        require(match is not None, f"unreadable builtinSkillSpecs row: {row[:60]}")
        name, url, original = match.groups()
        require(name not in sources, f"{name}: listed twice in builtinSkillSpecs")
        sources[name] = url or original
    require(len(sources) >= 12, "builtinSkillSpecs was not found")
    table = re.search(r"var builtinRoleSkills = map\[string\]\[\]string\{(.*?)\n\}", source, re.S)
    require(table is not None, "builtinRoleSkills was not found")
    roles = {}
    for rid, names in re.findall(r'^\t"([a-z-]+)":\s*\{([^}]*)\},$', table.group(1), re.M):
        require(rid not in roles, f"{rid}: listed twice in builtinRoleSkills")
        roles[rid] = re.findall(r'"([a-z-]+)"', names)
    return sources, roles


def valid_path(path):
    return (isinstance(path, str) and path and not path.startswith("/")
            and posixpath.normpath(path) == path and not path.startswith("../"))


def pinned_url(repo, commit, path):
    return f"{repo}/blob/{commit}/{path}"


def local_references(content, path):
    # A link inside a fenced example (a sample index, a template) names no file.
    content = re.sub(r"^```.*?^```", "", content, flags=re.S | re.M)
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


def sibling_names(content, path):
    """Bare file names in code spans, such as `root-cause-tracing.md`.

    Such a name is a reference only when that file exists beside the entry at
    the pinned commit; otherwise it names a file the skill tells a reader to
    create in their own repository, such as an ADR `template.md`.
    """
    content = re.sub(r"^```.*?^```", "", content, flags=re.S | re.M)
    names = re.findall(r"`([\w-]+(?:\.[\w-]+)*\.(?:md|py|sh|js|ts|json))`", content)
    found = {posixpath.join(posixpath.dirname(path), name) for name in names}
    return {name for name in found if valid_path(name) and name != path}


def validate(data, catalog=None):
    ids = catalog_ids()
    # Only a catalog row starts with an id; other tables there name files such as `CHILD.md`.
    doc_ids = re.findall(r"^\| `([a-z]+(?:-[a-z]+)*)` \|", (ROOT / "docs/personas.md").read_text(), re.M)
    # Rows are grouped by team there, so a role moved to the Custom team is listed out of persona.Order.
    require(sorted(doc_ids) == sorted(ids), "docs/personas.md must list every persona.Order ID exactly once")
    require(data.get("schema_version") == 1, "unsupported schema_version")
    roles = data.get("roles")
    require(isinstance(roles, list), "roles must be a list")
    got = [role.get("id") for role in roles if isinstance(role, dict)]
    require(got == ids, f"roles must match all {len(ids)} persona.Order IDs in order, once each")
    count = 0
    preview_count = 0
    for role in roles:
        rid = role["id"]
        candidates = role.get("candidates")
        require(isinstance(candidates, list), f"{rid}: candidates must be a list")
        if role.get("conclusion") == "original_skill":
            original = role.get("original_skill")
            require(not candidates, f"{rid}: an original skill has no upstream candidates")
            require(isinstance(original, dict) and bool(original.get("name")) and bool(original.get("why")),
                    f"{rid}: an original skill needs its name and why no upstream text was used")
            path = original.get("path")
            require(path == f"internal/domain/squad/builtin_skills/{original['name']}.md" and (ROOT / path).is_file(),
                    f"{rid}: the original skill's text is not at its bundled path")
            require(role.get("zero_preview_reason") is None, f"{rid}: an original skill is not an upstream preview")
            continue
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
            require(candidate.get("research_round") in (1, 2, 3, 4, 5), f"{prefix}: research round is required")
            repo = candidate.get("source_repository")
            require(isinstance(repo, str) and REPO.fullmatch(repo), f"{prefix}: invalid source repository")
            commit = candidate.get("source_commit")
            require(isinstance(commit, str) and HEX40.fullmatch(commit), f"{prefix}: source commit must be a full SHA")
            skill_path = candidate.get("skill_path")
            license_path = candidate.get("license_path")
            # A skill's entry is its SKILL.md; a reviewed command file is named
            # as such, so a stray Markdown file is not mistaken for an entry.
            if candidate.get("entry_kind") == "command":
                require(valid_path(skill_path) and skill_path.endswith(".md") and not skill_path.endswith("SKILL.md"),
                        f"{prefix}: invalid command entry path")
            else:
                require("entry_kind" not in candidate, f"{prefix}: unknown entry kind")
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
    by_role = {role["id"]: role for role in roles}
    sources, role_skills = catalog if catalog is not None else catalog_skills()
    carried = set()
    originals = set()
    for rid, names in role_skills.items():
        require(rid in by_role, f"{rid}: a role in builtinRoleSkills is not in persona.Order")
        require(0 < len(names) <= MAX_ROLE_SKILLS, f"{rid}: a built-in role carries 1 to {MAX_ROLE_SKILLS} default skills, not {len(names)}")
        require(len(set(names)) == len(names), f"{rid}: a default skill is listed twice")
        for name in names:
            require(name in sources, f"{rid}: {name} is not a built-in skill")
            carried.add(name)
            if sources[name] == "persona.Original":
                originals.add((rid, name))
                continue
            # One Skill may serve several roles; each pairing is reviewed for its own role.
            matches = [c for c in by_role[rid].get("candidates", []) if c.get("skill_url") == sources[name]]
            require(matches and matches[0]["preview"]["status"] == "eligible_for_manual_preview",
                    f"{rid}: the bundled skill {name}'s source {sources[name]} is not a reviewed candidate of this role")
    require(carried == set(sources), f"built-in skills carried by no role: {sorted(set(sources) - carried)}")
    recorded = {(role["id"], role["original_skill"]["name"]) for role in roles if role.get("conclusion") == "original_skill"}
    require(originals == recorded,
            f"original skills in the catalog {sorted(originals)} do not match the ledger {sorted(recorded)}")
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

    def exists(repo, commit, path):
        try:
            fetch(repo, commit, path)
        except HTTPError as exc:
            if exc.code == 404:
                return False
            raise
        return True

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
                text = body.decode("utf-8")
                todo.extend(local_references(text, path) - found)
                todo.extend(name for name in sibling_names(text, path) - found if exists(repo, commit, name))
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
    rejected(lambda d: next(r for r in d["roles"] if r["id"] == "code-reviewer")["candidates"].pop(), "bundled skill missing from the ledger")
    rejected(lambda d: next(r for r in d["roles"] if r["id"] == "zero-review-lead").update(
        conclusion="no_suitable_candidate", zero_preview_reason="dropped", original_skill=None), "original skill missing from the ledger")
    rejected(lambda d: next(r for r in d["roles"] if r["id"] == "zero-review-lead")["original_skill"].__setitem__("name", "elsewhere"), "original skill at the wrong path")
    # One role, several reviewed Skills; one Skill, several roles.
    rejected(lambda d: next(r for r in d["roles"] if r["id"] == "technical-writer")["candidates"].pop(), "shared skill not reviewed for one of its roles")
    rejected(lambda d: next(r for r in d["roles"] if r["id"] == "minimal-change")["candidates"][0]["preview"].__setitem__("status", "blocked"), "second default skill of a role not reviewed")
    # A command file is an entry only when it says so.
    def command(d):
        return next(c for r in d["roles"] if r["id"] == "ui-finish-gate" for c in r["candidates"] if c.get("entry_kind") == "command")
    rejected(lambda d: command(d).pop("entry_kind"), "a command entry without its kind")
    rejected(lambda d: next(r for r in d["roles"] if r["id"] == "ui-designer")["candidates"][0].__setitem__("entry_kind", "script"), "an unknown entry kind")
    rejected(lambda d: next(r for r in d["roles"] if r["id"] == "ui-finish-gate")["candidates"][-1].__setitem__("research_round", 6), "an unknown research round")
    # One Skill shared by two roles is reviewed for each of them.
    rejected(lambda d: next(r for r in d["roles"] if r["id"] == "ux-architect")["candidates"].pop(), "a shared design skill not reviewed for one of its roles")
    sources, role_skills = catalog_skills()
    def with_roles(change, label):
        changed = copy.deepcopy(role_skills)
        change(changed)
        try:
            validate(copy.deepcopy(data), (sources, changed))
        except Invalid:
            return
        raise Invalid(f"self-test failed to reject {label}")
    with_roles(lambda r: r["code-reviewer"].extend(["humanize-prose", "decision-record", "write-spec"]), "five default skills on one role")
    with_roles(lambda r: r["ui-designer"].append("better-layout"), "a fifth default skill on a role already at the bound")
    with_roles(lambda r: r["code-reviewer"].append("two-axis-code-review"), "a default skill listed twice")
    with_roles(lambda r: r["technical-writer"].remove("humanize-prose") or r["pr"].clear(), "an empty role list")
    with_roles(lambda r: r["email"].append("golang-concurrency"), "a skill on a role whose ledger never reviewed it")
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
    ids = len(data["roles"])
    originals = sum(role.get("conclusion") == "original_skill" for role in data["roles"])
    print(f"{ids}/{ids} personas; {count} pinned candidates; {previews} manual previews; {originals} original skills; {requests} upstream file reads; valid")


if __name__ == "__main__":
    try:
        main()
    except (Invalid, OSError, ValueError) as exc:
        print(f"persona skill decisions: {exc}", file=sys.stderr)
        sys.exit(1)
