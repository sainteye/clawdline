#!/usr/bin/env python3
"""Build the deduplicated high-risk daemon/CLI translation review set.

Run in a worktree containing the catalog branches. All output is local and
uses the Python standard library. Review status is retained only while the
exact nine-language text digest stays unchanged.
"""

import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[2]
HERE = ROOT / "cmd/clawdline"
LANGUAGES = ("en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de")
MATRIX = HERE / "localization_risk_matrix.jsonl"
REVIEW = HERE / "localization_risk_review.jsonl"
QUOTED = re.compile(r'"(?:\\.|[^"\\])*"')
CATEGORIES = {
    "uncertainty": re.compile(r"\b(?:unconfirm\w*|unknown|unavailab\w*|unreadab\w*|unreachab\w*|incomplet\w*|uncertain|ambiguous|missing|not\s+(?:confirm\w*|acknowledg\w*|receiv\w*|deliver\w*|verif\w*|read)|(?:could|did)\s+not\s+(?:confirm|acknowledge|receive|deliver|read)|no\s+(?:receipt|result|response|answer))\b", re.I),
    "refusal_negation": re.compile(r"\b(?:no|not|never|nothing|none|without|only|unreadable|invalid|refus\w*|reject\w*|fail\w*|missing|unknown|cannot|could not|unable|exhausted)\b", re.I),
    "destructive": re.compile(r"\b(?:delet\w*|remov\w*|clear\w*|stop\w*|cancel\w*|disabl\w*|discard\w*|revok\w*|releas\w*|evict\w*|expir\w*|force)\b", re.I),
    "authority": re.compile(r"\b(?:permission|authoriz\w*|allow\w*|must|may|require\w*|assign\w*|claim\w*|approv\w*|consent|the person|on its own initiative)\b", re.I),
    "delivery_ack": re.compile(r"\b(?:accept\w*|send|sent|deliver\w*|acknowledg\w*|receiv\w*|read|typed|push|notice|notif\w*|retry|reconcil\w*)\b", re.I),
    "landing_state": re.compile(r"\b(?:deploy\w*|land\w*|merg\w*|commit\w*|build|releas\w*|phase|state|status|pending|succeed\w*|success\w*|complete\w*|finish\w*|verif\w*|implement\w*)\b", re.I),
}


def cli_rows():
    for group in sorted((HERE / "cli_catalogs").iterdir()):
        if not group.is_dir() or not (group / "en.json").exists():
            continue
        catalogs = {}
        for language in LANGUAGES:
            file = group / (language + ".json")
            catalogs[language] = json.loads(file.read_text()) if file.exists() else {}
        for key in sorted(catalogs["en"]):
            yield "cli." + group.name + "." + key, {language: catalogs[language].get(key, "") for language in LANGUAGES}


def productcopy_rows():
    file = ROOT / "internal/productcopy/notices.go"
    if not file.exists():
        return
    for line in file.read_text().splitlines():
        match = re.match(r'^\s*"([a-z][a-z0-9.]+)":\s*\{(.+)\},?$', line)
        if not match:
            continue
        values = [json.loads(raw) for raw in QUOTED.findall(match.group(2))]
        if len(values) != len(LANGUAGES):
            raise ValueError("notification key " + match.group(1) + " does not carry nine strings")
        yield "notification." + match.group(1), dict(zip(LANGUAGES, values))


def schedule_rows():
    file = ROOT / "internal/app/schedule_notifications.go"
    language = ""
    records = {}
    for line in file.read_text().splitlines():
        match = re.match(r'^\s*"(en|zh-Hant|ja|zh-Hans|ko|es|pt-BR|fr|de)":\s*\{$', line)
        if match:
            language = match.group(1)
            continue
        match = re.match(r'^\s*scheduleNotice([A-Za-z]+):\s*("(?:\\.|[^"\\])*")\s*,?$', line)
        if match and language:
            records.setdefault(match.group(1), {})[language] = json.loads(match.group(2))
        if language and line.strip() == "},":
            language = ""
    for key, translations in sorted(records.items()):
        if "en" not in translations:
            raise ValueError("schedule key " + key + " has no English")
        yield "schedule." + key, {lang: translations.get(lang, "") for lang in LANGUAGES}


def entries():
    grouped = {}
    for source, texts in (*cli_rows(), *productcopy_rows(), *schedule_rows()):
        phrase = source.rsplit(".", 1)[-1].replace("_", " ") + " " + texts["en"]
        risks = sorted(category for category, pattern in CATEGORIES.items() if pattern.search(phrase))
        if source.startswith(("cli.item.error.", "cli.verify.misuse.")) and "refusal_negation" not in risks:
            risks.append("refusal_negation")
        if not risks:
            continue
        content = "\0".join(texts[lang] for lang in LANGUAGES)
        digest = hashlib.sha256(content.encode()).hexdigest()
        row = grouped.setdefault(digest, {"digest": digest, "sources": [], "risks": set(), "texts": texts})
        if row["texts"] != texts:
            raise ValueError("digest collision")
        row["sources"].append(source)
        row["risks"].update(risks)
    result = []
    for row in grouped.values():
        row["sources"].sort()
        row["risks"] = sorted(row["risks"])
        result.append(row)
    return sorted(result, key=lambda row: row["sources"][0])


def encoded(rows):
    return "".join(json.dumps(row, ensure_ascii=False, sort_keys=True) + "\n" for row in rows)


def main():
    rows = entries()
    data = encoded(rows)
    if "--check" in sys.argv:
        if not MATRIX.exists() or MATRIX.read_text() != data:
            raise SystemExit("risk matrix is out of date")
    else:
        MATRIX.write_text(data)
    if "--sync-review" in sys.argv:
        previous = {}
        if REVIEW.exists():
            for line in REVIEW.read_text().splitlines():
                row = json.loads(line)
                previous[row["digest"]] = row
        review = []
        for row in rows:
            old = previous.get(row["digest"], {})
            review.append({"digest": row["digest"], "sources": row["sources"], "reviewed": old.get("reviewed", False), "note": old.get("note", "")})
        REVIEW.write_text(encoded(review))
    if "--check-review" in sys.argv:
        review = {row["digest"]: row for row in map(json.loads, REVIEW.read_text().splitlines())}
        pending = [row["sources"] for row in rows if not review.get(row["digest"], {}).get("reviewed")]
        if pending:
            raise SystemExit(f"{len(pending)} high-risk text groups still need semantic QA")
    print(f"high-risk groups: {len(rows)}, source keys: {sum(len(row['sources']) for row in rows)}")


if __name__ == "__main__":
    main()
