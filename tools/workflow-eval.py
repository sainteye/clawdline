#!/usr/bin/env python3
"""Prepare and summarize a manually run, blinded workflow experiment offline.

This tool never invokes an assistant, a shell command, a network service or a
paid API. Human grading is required; quoted evidence is checked for provenance,
not scored for meaning. Private trial output must stay outside this checkout.
"""

import argparse
import hashlib
import json
import random
import re
import sys
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SKILLS = ("evidence-debugging", "evidence-review")
SUITE = ROOT / "tools/workflow-eval/cases.json"


class Invalid(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise Invalid(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def load_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def load_suite():
    suite = load_json(SUITE)
    require(suite.get("schema_version") == 1, "unsupported case schema")
    require(isinstance(suite.get("context"), str) and suite["context"].strip(), "missing case context")
    cases = suite.get("cases")
    require(isinstance(cases, list) and cases, "empty case suite")
    seen = set()
    for case in cases:
        require(isinstance(case, dict), "invalid case")
        name = case.get("id")
        require(isinstance(name, str) and re.fullmatch(r"[a-z][a-z0-9-]*", name), "invalid case ID")
        require(name not in seen, "duplicate case ID")
        seen.add(name)
        for field in ("request", "artifacts"):
            require(isinstance(case.get(field), str) and case[field].strip(), "missing " + field)
        criteria = case.get("criteria")
        require(isinstance(criteria, dict) and criteria, "missing criteria")
        require(all(isinstance(k, str) and re.fullmatch(r"[a-z][a-z0-9-]*", k)
                    and isinstance(v, str) and v.strip() for k, v in criteria.items()), "invalid criterion")
    return suite


def contained(root, relative):
    require(isinstance(relative, str) and relative and "\\" not in relative, "invalid artifact path")
    path = Path(relative)
    require(not path.is_absolute() and ".." not in path.parts, "artifact path escapes experiment")
    resolved = (root / path).resolve()
    require(resolved.is_relative_to(root.resolve()), "artifact symlink escapes experiment")
    return resolved


def prepare(out, repeats, model, seed=None):
    out = out.resolve()
    require(not out.is_relative_to(ROOT) and not any((parent / ".git").exists() for parent in (out, *out.parents)),
            "private trial data must be outside every repository")
    require(not out.exists(), "output already exists; preserve the previous experiment")
    require(type(repeats) is int and repeats > 0, "repeats must be positive")
    require(isinstance(model, str) and model.strip(), "record the exact model and harness configuration")
    suite = load_suite()
    sources = {"sources/AGENTS.md": (ROOT / "AGENTS.md").read_bytes(),
               "sources/cases.json": SUITE.read_bytes(),
               "sources/workflow-prefix.md": (ROOT / "skills/workflow-prefix.md").read_bytes()}
    for name in SKILLS:
        sources["sources/" + name + "/SKILL.md"] = (ROOT / "skills" / name / "SKILL.md").read_bytes()
    # Read all inputs before creating output. mkdir is exclusive even when two
    # preparations race; an existing experiment is never overwritten.
    out.mkdir(parents=True, exist_ok=False)
    for folder in ("prompts", "responses"):
        (out / folder).mkdir()
    for relative, content in sources.items():
        target = out / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content)
    trials = [{"case": case["id"], "arm": arm, "repeat": repeat}
              for case in suite["cases"] for repeat in range(1, repeats + 1)
              for arm in ("baseline", "candidate")]
    random.Random(seed).shuffle(trials)
    by_id = {case["id"]: case for case in suite["cases"]}
    grading = {"schema_version": 1, "trials": []}
    files = {relative: digest(content) for relative, content in sources.items()}
    rules = sources["sources/AGENTS.md"].decode("utf-8")
    for index, trial in enumerate(trials, 1):
        trial["id"] = f"trial-{index:04d}"
        case = by_id[trial["case"]]
        prompt = "# Existing repository instructions\n\n" + rules + "\n\n# Scenario boundary\n\n" + suite["context"]
        if trial["arm"] == "candidate":
            prompt += "\n\n# Machine integration instructions\n\n"
            prompt += sources["sources/workflow-prefix.md"].decode("utf-8").strip()
            for name in SKILLS:
                content = sources["sources/" + name + "/SKILL.md"].decode("utf-8")
                prompt += "\n\n" + content.strip()
        prompt += "\n\n# Request\n\n" + case["request"] + "\n\n# Supplied artifacts\n\n" + case["artifacts"] + "\n"
        relative = "prompts/" + trial["id"] + ".md"
        (out / relative).write_text(prompt, encoding="utf-8")
        files[relative] = digest(prompt.encode("utf-8"))
        grading["trials"].append({"id": trial["id"], "case": trial["case"],
                                  "reviewer": "", "model": "", "response_sha256": "",
                                  "criteria": {key: {"passed": None, "evidence": "", "reason": ""}
                                               for key in case["criteria"]}})
    manifest = {"schema_version": 1, "created_at": datetime.now(timezone.utc).isoformat(),
                "model": model, "repeats": repeats, "trials": trials, "files": files,
                "executor": "manual; no model calls made by this tool"}
    write_json(out / "grading.json", grading)
    write_json(out / "rubric.json", {case["id"]: case["criteria"] for case in suite["cases"]})
    files["rubric.json"] = digest((out / "rubric.json").read_bytes())
    write_json(out / "manifest.json", manifest)
    (out / "manifest.sha256").write_text(digest((out / "manifest.json").read_bytes()) + "\n", encoding="utf-8")
    return {"status": "prepared", "trials": len(trials), "model_calls": 0,
            "output": str(out), "next": "Run each prompt in a fresh isolated context only within an approved budget."}


def report(out):
    out = out.resolve()
    require(digest((out / "manifest.json").read_bytes()) == (out / "manifest.sha256").read_text().strip(),
            "experiment manifest changed; preserve the original assignments and input inventory")
    manifest = load_json(out / "manifest.json")
    grades = load_json(out / "grading.json")
    require(manifest.get("schema_version") == grades.get("schema_version") == 1, "unsupported experiment schema")
    files = manifest.get("files")
    require(isinstance(files, dict) and files, "missing frozen input inventory")
    require({"sources/AGENTS.md", "sources/cases.json", "sources/workflow-prefix.md", "rubric.json",
             *("sources/" + name + "/SKILL.md" for name in SKILLS)} <= set(files), "missing frozen source")
    for relative, expected in files.items():
        path = contained(out, relative)
        require(path.is_file() and digest(path.read_bytes()) == expected, "frozen input changed: " + relative)
    suite = load_json(out / "sources/cases.json")
    cases = {case["id"]: case for case in suite["cases"]}
    repeats = manifest.get("repeats")
    require(type(repeats) is int and repeats > 0, "invalid repeat count")
    trials = manifest.get("trials")
    require(isinstance(trials, list), "missing trials")
    expected_pairs = {(case, arm, repeat) for case in cases for arm in ("baseline", "candidate")
                      for repeat in range(1, repeats + 1)}
    observed_pairs = []
    trial_ids = []
    for trial in trials:
        require(isinstance(trial, dict), "invalid trial")
        name = trial.get("id")
        require(isinstance(name, str) and re.fullmatch(r"trial-[0-9]+", name), "invalid trial ID")
        trial_ids.append(name)
        observed_pairs.append((trial.get("case"), trial.get("arm"), trial.get("repeat")))
        require("prompts/" + name + ".md" in files, "missing frozen trial prompt")
    require(len(set(trial_ids)) == len(trial_ids), "duplicate trial ID")
    require(len(observed_pairs) == len(expected_pairs) and set(observed_pairs) == expected_pairs,
            "missing, duplicate or unknown trial assignment")
    rows = grades.get("trials")
    require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "missing grading rows")
    row_ids = [row.get("id") for row in rows]
    require(all(isinstance(name, str) for name in row_ids) and len(set(row_ids)) == len(row_ids)
            and set(row_ids) == set(trial_ids), "missing, duplicate or unknown grading row")
    indexed = {row["id"]: row for row in rows}
    result = {"status": "incomplete", "reviewed": 0, "expected": len(trials),
              "missing": [], "adoption": "human_review_required"}
    comparison = {arm: {"passed": 0, "total": 0} for arm in ("baseline", "candidate")}
    per_case = {case: {arm: {"passed": 0, "total": 0} for arm in comparison} for case in cases}
    for trial in trials:
        name = trial["id"]
        row = indexed[name]
        case = trial["case"]
        require(row.get("case") == case, "grade belongs to a different case")
        criteria = row.get("criteria")
        require(isinstance(criteria, dict) and set(criteria) == set(cases[case]["criteria"]), "criterion set changed")
        require(all(isinstance(grade, dict) for grade in criteria.values()), "invalid grade")
        for grade in criteria.values():
            require(grade.get("passed") is None or type(grade.get("passed")) is bool, "score must be boolean or unreviewed")
        response_path = contained(out, "responses/" + name + ".txt")
        if not response_path.is_file() or not response_path.read_text(encoding="utf-8").strip():
            result["missing"].append(name + ": response")
            continue
        response_bytes = response_path.read_bytes()
        response = response_bytes.decode("utf-8")
        complete = all(isinstance(row.get(field), str) and row[field].strip()
                       for field in ("reviewer", "model", "response_sha256"))
        complete = complete and all(type(grade.get("passed")) is bool
                                    and all(isinstance(grade.get(field), str) and grade[field].strip()
                                            for field in ("evidence", "reason")) for grade in criteria.values())
        if not complete:
            result["missing"].append(name + ": grading")
            continue
        require(row["model"] == manifest.get("model"), "model/harness conditions differ")
        require(row["response_sha256"] == digest(response_bytes), "graded response changed")
        require(all(grade["evidence"] in response for grade in criteria.values()), "grade cites absent evidence")
        result["reviewed"] += 1
        passed = all(grade["passed"] for grade in criteria.values())
        for summary in (comparison[trial["arm"]], per_case[case][trial["arm"]]):
            summary["total"] += 1
            summary["passed"] += int(passed)
    if result["reviewed"] == result["expected"]:
        result.update(status="reviewed", comparison=comparison, per_case=per_case,
                      repeated_sample_ready=repeats >= 5,
                      limitation="Human-graded synthetic samples under the recorded configuration; no automatic efficacy or adoption claim.")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    prepare_parser = commands.add_parser("prepare", help="freeze inputs and create prompts; no model calls")
    prepare_parser.add_argument("--out", type=Path, required=True)
    prepare_parser.add_argument("--model", required=True, help="exact model/version, effort and harness configuration label")
    prepare_parser.add_argument("--repeats", type=int, default=5)
    prepare_parser.add_argument("--seed", type=int)
    report_parser = commands.add_parser("report", help="verify frozen evidence and summarize manual grades")
    report_parser.add_argument("directory", type=Path)
    args = parser.parse_args()
    try:
        result = (prepare(args.out, args.repeats, args.model, args.seed)
                  if args.command == "prepare" else report(args.directory))
        print(json.dumps(result, indent=2, ensure_ascii=False))
        return 2 if result["status"] == "incomplete" else 0
    except (Invalid, OSError, ValueError, KeyError, TypeError) as error:
        print("workflow-eval: " + str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
