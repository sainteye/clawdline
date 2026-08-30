import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const help = await import(new URL(
    "../Resources/web/app/js/view/closeability-help.js", import.meta.url));

const strings = {
    closeabilityAttestationExplanation: "Plain explanation",
    closeabilityTechnicalDetails: "Technical details",
    webReviewBeforeClosing: "Review session",
    webConfirmEndAnyway: "Close anyway"
};
const attestation = {
    state: "needs_attestation",
    reasons: [{ code: "attestation_missing", kind: "attestation" }]
};
assert.deepEqual(help.closeabilityHelpModel(attestation, strings), {
    explanation: "Plain explanation",
    detailsLabel: "Technical details",
    cancelLabel: "Review session",
    confirmLabel: "Close anyway"
}, "the common attestation state gets plain-language copy and explicit choices");
assert.equal(help.closeabilityHelpModel({ state: "blocked", reasons: [] }, strings), null,
    "a positive obligation keeps the existing blocker presentation");
assert.equal(help.closeabilityHelpModel({
    state: "needs_attestation", reasons: [{ code: "terminal_working", kind: "obligation" }]
}, strings), null, "malformed attestation data does not receive reassuring copy");

const confirmSource = await readFile(new URL(
    "../Resources/web/app/js/input/action-confirm.js", import.meta.url), "utf8");
assert.match(confirmSource, /closeabilityHelpModel\(/,
    "the close sheet selects the tested explanation model");
assert.match(confirmSource, /createElement\("details"\)/,
    "technical closeability data is disclosed on demand");
assert.match(confirmSource, /technical[\s\S]*why[\s\S]*createElement\("li"\)/,
    "the disclosure retains the raw reason, subject id, and mover line");
assert.match(confirmSource, /help\.cancelLabel[\s\S]*help\.confirmLabel/,
    "the two choices say what each press does");

const chinese = await readFile(new URL(
    "../Sources/Copy+Chinese.swift", import.meta.url), "utf8");
assert.match(chinese, /這不是系統發現工作尚未完成/,
    "Traditional Chinese leads by removing the false-error reading");
assert.match(chinese, /回到 session 檢查/);
assert.match(chinese, /仍要關閉/);

const css = await readFile(new URL(
    "../Resources/web/app/css/sheets.css", import.meta.url), "utf8");
assert.match(css, /\.closeability-technical\s*>\s*summary[^{]*\{[^}]*min-height:\s*44px/s,
    "the small help affordance still has a phone-sized touch target");

console.log("✓ close confirmation explains attestation before exposing technical details");
