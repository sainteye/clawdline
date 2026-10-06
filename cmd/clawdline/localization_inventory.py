#!/usr/bin/env python3
"""Rebuild the daemon/CLI output-site inventory from source, without network access.

Run at the repository root: python3 cmd/clawdline/localization_inventory.py
The TSV is a search index, not an assertion that a candidate is translated.
"""

from pathlib import Path
import hashlib
import json
import re
import sys


ROOT = Path(__file__).resolve().parents[2]
SOURCES = (ROOT / "cmd/clawdline", ROOT / "internal/app", ROOT / "internal/transport/http", ROOT / "internal/domain/capacity", ROOT / "internal/productcopy")
OUTPUT = ROOT / "cmd/clawdline/localization_matrix.tsv"
HTTP_ENGLISH = json.loads((ROOT / "internal/productcopy/http_refusals/en.json").read_text())
CLOUD_MESSAGE = re.compile(r'Message:\s*("(?:\\.|[^"\\])*")')
CLI = re.compile(r"\b(?:fmt\.(?:Print|Fprint)(?:f|ln)?|log\.(?:Print|Fatal)(?:f|ln)?)\s*\(")
DIRECT = re.compile(r"\b(?:json\.NewEncoder\((?:os\.Stdout|stdout|out)\)|(?:os\.Stdout|stdout|out)\.Write\s*\()")
BUILDER = re.compile(r"\.WriteString\s*\(")
ERROR_CONSTRUCTOR = re.compile(r"\b(?:fmt\.Errorf|errors\.New)\s*\(")
REFUSAL = re.compile(r"\bwriteRefusal(?:About)?\s*\(")
NOTICE = re.compile(r"\b(?:scheduleNotice|\.Push|\.Notify)\s*\(")
NOTICE_COPY = re.compile(r"\b(?:waitingText|NoticeText|pushTestBody|deadLetterTitle|deadLetterBody|deadLetterHeldBody|workV2CompletionEffect|FinishedLine|callbackFinishedLine|ShowCommand|landingLine|ReminderWire|completionWire)\b")
NOTICE_FUNCTIONS = {"waitingText", "NoticeText", "workV2CompletionEffect", "FinishedLine", "callbackFinishedLine", "ShowCommand", "landingLine", "ReminderWire", "completionWire"}
FUNC = re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z_0-9]*)\s*\(")
CODE = re.compile(r'"([a-z][a-z0-9_]+)"')
LITERAL = re.compile(r'"(?:\\.|[^"\\])*"|`[^`]*`')
FORMAT = re.compile(r'%(?:\[[0-9]+\])?[+#0 .-]*[0-9]*(?:\.[0-9]+)?[a-zA-Z%]')


def cells(*values):
    return "\t".join(str(value).replace("\t", " ").replace("\n", " ") for value in values)


def rows():
    for folder in SOURCES:
        for path in sorted(folder.rglob("*.go")):
            if "testdata" in path.parts or path.name.endswith("_test.go") or path.name.startswith("zz_generated"):
                continue
            lines = path.read_text().splitlines()
            function = "package"
            for number, line in enumerate(lines, 1):
                found = FUNC.match(line)
                if found:
                    function = found.group(1)
                category = ""
                channel = ""
                contract = ""
                if folder.name == "clawdline" and CLI.search(line):
                    category = "diagnostic" if "log." in line else "human_composed"
                    channel = "stderr" if "os.Stderr" in line or "stderr" in line else "stdout_or_writer"
                    contract = "stable_log" if category == "diagnostic" else "inspect_source"
                    if "guide-version:" in line or "Idempotency-Key:" in line or "fmt.Println(version)" in line:
                        category, contract = "machine_output", "stable_prefix_or_value"
                    elif re.search(r'fmt\.Fprintf\([^,]+, "clawdline (?:item )?%s: %v\\n"', line):
                        category, contract = "machine_or_passthrough", "preserve_error_and_command_name"
                    elif path.name == "item.go" and "fmt.Fprintln(stderr, nothing)" in line:
                        category, contract = "localized_human", "localized_argument_from_callers"
                    elif category != "diagnostic":
                        literals = LITERAL.findall(line)
                        if "cliCopy(" in line:
                            category, contract = "localized_human", "catalog_key_with_english_fallback"
                        elif literals:
                            copy = " ".join(literals)
                            words = FORMAT.sub("", copy)
                            words = re.sub(r"\\(?:x[0-9a-fA-F]{2}|u[0-9a-fA-F]{4}|U[0-9a-fA-F]{8}|[a-zA-Z])", "", words)
                            if re.search(r"[A-Za-zÀ-ÿ\u3400-\u9fff\u3040-\u30ff\uac00-\ud7af]", words):
                                category, contract = "fixed_human", "translate_literal_preserve_arguments"
                            else:
                                category, contract = "layout_or_data", "preserve_values_and_layout"
                        elif re.search(r"fmt\.(?:F?Print(?:f|ln)?)\s*\([^)]*\b(?:string\(|\.URL\b|\.Body\b|\.ID\b)", line):
                            category, contract = "machine_or_passthrough", "preserve_bytes"
                        elif re.search(r"fmt\.(?:Fprintln|Println)\s*\(\s*(?:w|stdout)?\s*\)", line):
                            category, contract = "layout_or_data", "blank_line_only"
                        elif any(token in line for token in (
                            "orchestrator.ChildGuide()", "d.Body)", "fmt.Fprintln(stdout, link)",
                            "fmt.Fprintln(stdout, t)", "fmt.Fprintln(stdout, name)",
                            "fmt.Fprintln(stdout, string(body))", "fmt.Fprintln(f.stdout, f.redact(line))",
                            "fmt.Fprintln(f.stdout, id)", "Log:          func(line string)",
                        )):
                            category, contract = "machine_or_passthrough", "preserve_bytes"
                elif folder.name == "clawdline" and DIRECT.search(line):
                    category, channel, contract = "machine_or_passthrough", "stdout", "preserve_bytes"
                elif folder.name == "clawdline" and BUILDER.search(line):
                    category, channel, contract = "human_composition_source", "builder", "inspect_literal_and_data"
                elif folder.name == "clawdline" and "cliCopy(" in line and not line.lstrip().startswith("func "):
                    category, channel, contract = "localized_human", "cli_catalog", "catalog_key_with_english_fallback"
                elif folder.name == "clawdline" and ERROR_CONSTRUCTOR.search(line) and LITERAL.search(line):
                    category, channel, contract = "fixed_error_source", "stderr_via_caller", "translate_fixed_copy_preserve_error_arguments"
                elif folder.name == "productcopy" and path.name == "notices.go" and re.match(r'\s*"[a-z][a-z0-9.]+":\s*\{', line):
                    category, channel, contract = "notification_copy_source", "push", "nine_language_catalog_key"
                elif path.parent.name == "cloudops" and "fixedCopy: true" in line and "Message:" in line:
                    match = CLOUD_MESSAGE.search(line)
                    if match:
                        english = json.loads(match.group(1))
                        key = "http." + hashlib.sha256(english.encode()).hexdigest()[:16]
                        category, channel = "cloud_bridge_fixed_copy", "cloud_error"
                        contract = "verified_cloud_bridge_catalog_key" if HTTP_ENGLISH.get(key) == english else "pending_cloud_bridge_catalog_key"
                elif path.name == "remedy.go" and '"succession_required": {Because:' in line:
                    category, channel = "fixed_remediation_copy", "nested_remediation"
                    contract = ("verified_remediation_catalog_key" if "http.351b72abb57acd1e" in HTTP_ENGLISH
                                else "pending_remediation_catalog_key")
                elif folder.name == "http" and REFUSAL.search(line):
                    category, channel, contract = "actionable_refusal_candidate", "http_json", "error_and_detail_stable"
                elif folder.name == "app" and NOTICE.search(line):
                    category, channel, contract = "notification_candidate", "push_or_schedule", "code_and_user_data_stable"
                elif NOTICE_COPY.search(line) and not line.lstrip().startswith("//"):
                    category, channel, contract = "notification_copy_source", "push", "translate_fixed_copy_only"
                elif folder.name != "clawdline" and function in NOTICE_FUNCTIONS and LITERAL.search(line) and not line.lstrip().startswith("//"):
                    category, channel, contract = "notification_copy_source", "push", "translate_fixed_copy_only"
                elif path.name == "schedule_notifications.go" and re.search(r"scheduleNotice(?:Invalid|Project|Missed|Refused)", line) and LITERAL.search(line):
                    category, channel, contract = "notification_copy_source", "push", "translate_fixed_copy_only"
                if not category:
                    continue
                # Completion text is carried inside the clawdline.notice / reminder
                # protocol sent to an agent Session. Its command and state words
                # are part of that agent-facing envelope, not a product-language
                # notification to a person. Preserve it with the wire fields.
                if category == "notification_copy_source" and (
                    path.relative_to(ROOT).as_posix() == "internal/app/orchestrator/notice.go"
                    or path.name == "callback.go" and function == "callbackFinishedLine"
                ):
                    category, channel, contract = "agent_protocol_copy", "clawdline.notice", "preserve_agent_envelope_text"
                elif category == "notification_copy_source" and (
                    function == "workV2CompletionEffect" or "workV2CompletionEffect(" in line
                ):
                    category, contract = "localized_human", "productcopy_catalog_with_english_fallback"
                elif category == "notification_copy_source" and (
                    path.name in {"waiting.go", "notice.go"} and function in {"waitingText", "NoticeText"}
                ):
                    category, contract = "localized_human", "legacy_zh_hant_wrapper_around_productcopy"
                elif category == "fixed_human" and folder.name == "clawdline":
                    if path.name == "project_unify.go" and any(token in line for token in (
                        '"clawdline:"', '"status: ', '"drift: ', '"conflict: ',
                    )):
                        category, contract = "machine_output", "preserve_command_prefix_or_check_column"
                    elif path.name == "doctor.go" and "capacity drill:" in line:
                        category, channel, contract = "diagnostic", "capacity_drill_log", "preserve_log_stimulus"
                    elif path.name == "report.go" and '"http://127.0.0.1:' in line:
                        category, contract = "machine_output", "preserve_local_report_url"
                    elif path.name == "update.go" and any(f'"{prefix}' in line for prefix in ("git ", "cd ")):
                        category, contract = "machine_output", "preserve_executable_command"
                    elif path.name == "webhook_fire.go" and "stdout, one line:" in line:
                        category, contract = "machine_output", "preserve_documented_stdout_grammar"
                    elif '"clawdline' in line and any(token in line for token in (
                        "err)", "failure)", "PinnedError)", "humanHTTPAnswer(", "+msg)",
                    )):
                        category, contract = "machine_or_passthrough", "stable_command_prefix_with_separately_owned_detail"
                elif category == "notification_candidate":
                    if path.name == "lifecycle.go" and function == "sendNotify":
                        category, contract = "user_agent_or_external_original", "preserve_authored_notification"
                    elif path.name in {"capacity_push.go", "effects.go", "waiting_push.go", "work_item_push.go"}:
                        category, contract = "localized_human", "producer_rendered_and_persisted_notification"
                    elif path.name in {"proposals.go", "scheduler.go", "schedules.go", "schedule_notifications.go"}:
                        category, contract = "localized_human", "productcopy_body_preserve_authored_title_and_options"
                elif path.name == "project_unify.go" and category == "fixed_human" and (
                    '"clawdline:"' in line
                    or any(f'"{prefix}: ' in line for prefix in ("status", "drift", "conflict"))
                ):
                    category, contract = "machine_output", "preserve_command_prefix_or_check_column"
                if category == "machine_or_passthrough":
                    if any(token in line for token in ("d.Body)", "f.redact(line)", "Log:          func(line string)")):
                        category, contract = "user_agent_or_external_original", "preserve_source_bytes"
                    elif "orchestrator.ChildGuide()" in line or "stdout.Write(text)" in line:
                        category, contract = "guide_copy", "owned_by_guide_catalog"
                context = " ".join(lines[number - 1 : min(len(lines), number + 3)])
                code = ""
                if category == "actionable_refusal_candidate":
                    hits = CODE.findall(context)
                    code = next((hit for hit in hits if "_" in hit), "dynamic")
                elif category == "notification_candidate":
                    code = next((hit for hit in CODE.findall(context) if "_" in hit), "dynamic")
                site = f"{path.relative_to(ROOT)}:{number}"
                excerpt = line.strip()[:220].rstrip()
                if category == "localized_human" or category == "notification_copy_source" and path.name in {"schedule_notifications.go", "notices.go"}:
                    coverage = "covered"
                elif category in {"cloud_bridge_fixed_copy", "fixed_remediation_copy"}:
                    coverage = "covered" if contract.startswith("verified_") else "pending"
                elif category in {"fixed_human", "fixed_error_source", "human_composed", "human_composition_source", "actionable_refusal_candidate", "notification_copy_source", "notification_candidate"}:
                    coverage = "pending"
                else:
                    coverage = "preserve"
                yield cells(site, function, code, category, channel, contract, coverage, "daemon_cli", excerpt)


def main():
    header = cells("site", "trigger_function", "message_code", "classification", "channel", "contract", "coverage", "owner", "source_excerpt")
    data = "\n".join((header, *rows())) + "\n"
    if "--check" in sys.argv:
        if not OUTPUT.exists() or OUTPUT.read_text() != data:
            print("localization matrix is out of date", file=sys.stderr)
            raise SystemExit(1)
        print(f"localization matrix current: {data.count(chr(10)) - 1} sites")
        return
    OUTPUT.write_text(data)
    print(f"wrote {OUTPUT.relative_to(ROOT)}: {data.count(chr(10)) - 1} sites")


if __name__ == "__main__":
    main()
