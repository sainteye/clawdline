const OPEN = '<clawdline-workflow version="1" authority="metadata-not-user">\n';
const CLOSE = "\n</clawdline-workflow>";
const UUID = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;
const KEYS = [
    "authority", "board_epoch", "content_reference", "conversation_id", "coverage",
    "helper", "input_kind", "mode_gap", "process_generation", "project_id", "provider",
    "required_first_action", "run_id", "terminal_id", "version"
].sort();
const OPTIONAL = ["begin_template", "helper_path", "previous_item"];
const ITEM_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const TEMPLATE_KEY = /^[a-z_]{1,64}$/;

/**
 * The structural v1 contract for `begin_template`, identical in `Transcript.swift`: an object of
 * 2–16 fields named `[a-z_]{1,64}`, each a string of at most 256 UTF-8 bytes, whose `operation`
 * is `begin` and whose `run_id` is the envelope's own. Placeholder text and which optional fields
 * appear are the producer's to change, so a later template edit keeps historical envelopes folding.
 */
function validBeginTemplate(template, runID) {
    if (!template || typeof template !== "object" || Array.isArray(template)) return false;
    const keys = Object.keys(template);
    return keys.length >= 2 && keys.length <= 16
        && keys.every(key => TEMPLATE_KEY.test(key) && typeof template[key] === "string"
            && new TextEncoder().encode(template[key]).length <= 256)
        && template.operation === "begin" && template.run_id === runID;
}

// Optional in v1: an advisory settled item id only beside the template, and the template held to
// its structural v1 contract rather than to the producer's current text.
function validBeginAssistance(value) {
    const hasTemplate = Object.hasOwn(value, "begin_template");
    if (Object.hasOwn(value, "previous_item") && (!hasTemplate
        || typeof value.previous_item !== "string" || !ITEM_ID.test(value.previous_item))) return false;
    return !hasTemplate || validBeginTemplate(value.begin_template, value.run_id);
}

function outsideFence(source, offset) {
    const lines = source.slice(0, offset).split("\n");
    let fence = null;
    for (const line of lines) {
        const match = /^(?: {0,3})(`{3,}|~{3,})(.*)$/.exec(line);
        if (!match) continue;
        if (!fence) {
            fence = { mark: match[1][0], length: match[1].length };
        } else if (match[1][0] === fence.mark && match[1].length >= fence.length
            && /^\s*$/.test(match[2])) {
            fence = null;
        }
    }
    return fence === null;
}

function validMetadata(value) {
    if (!value || typeof value !== "object" || Array.isArray(value)) return false;
    const keys = Object.keys(value).filter(key => !OPTIONAL.includes(key)).sort();
    if (keys.length !== KEYS.length || keys.some((key, index) => key !== KEYS[index])) return false;
    if (!validBeginAssistance(value)) return false;
    if (Object.hasOwn(value, "helper_path") && (typeof value.helper_path !== "string"
        || !value.helper_path.startsWith("/") || !value.helper_path.endsWith("/clawdline-board-workflow")
        || new TextEncoder().encode(value.helper_path).length > 4096
        || /[\x00-\x1f\x7f-\x9f]/.test(value.helper_path)
        || value.helper_path.split("/").some(part => part === "." || part === ".."))) return false;
    return value.version === 1
        && value.authority === "clawdline_metadata_not_user_authorization"
        && Number.isSafeInteger(value.board_epoch) && value.board_epoch >= 1
        && /^terminal-request:[0-9a-f]{24}$/.test(value.content_reference)
        && UUID.test(value.conversation_id)
        && value.coverage === "managed_ingress"
        && value.helper === "clawdline-board-workflow <conversation-id> <stable-idempotency-key>"
        && ["image", "text_and_image", "text_or_transcribed_voice"].includes(value.input_kind)
        && (value.mode_gap === null || typeof value.mode_gap === "string")
        && typeof value.process_generation === "string" && value.process_generation.length > 0
        && /^project-[0-9a-f]{24}$/.test(value.project_id)
        && ["claude", "codex"].includes(value.provider)
        && value.required_first_action === "begin"
        && /^run-[0-9a-f]{32}$/.test(value.run_id)
        && typeof value.terminal_id === "string" && /^%[^\s]{1,126}$/.test(value.terminal_id);
}

/**
 * Split one broker-authored metadata block out of a user turn for presentation only.
 * The transcript entry itself is never changed. Quoted, fenced, malformed, duplicated and
 * non-user lookalikes remain ordinary visible prose.
 */
export function parseBoardWorkflowRecord(text, role) {
    if (role !== "user" || typeof text !== "string") return null;
    const candidates = [];
    let outsideOpenings = 0;
    let cursor = 0;
    while (cursor < text.length) {
        const start = text.indexOf(OPEN, cursor);
        if (start < 0) break;
        if ((start === 0 || text[start - 1] === "\n") && outsideFence(text, start)) {
            outsideOpenings++;
        }
        const jsonStart = start + OPEN.length;
        const close = text.indexOf(CLOSE, jsonStart);
        if (close >= 0 && (start === 0 || text[start - 1] === "\n")
            && (close + CLOSE.length === text.length || text[close + CLOSE.length] === "\n")
            && !text.slice(jsonStart, close).includes("\n") && outsideFence(text, start)) {
            candidates.push({ start, close: close + CLOSE.length,
                raw: text.slice(jsonStart, close) });
        }
        cursor = start + OPEN.length;
    }
    if (outsideOpenings !== 1 || candidates.length !== 1) return null;
    const found = candidates[0];
    let metadata;
    try { metadata = JSON.parse(found.raw); } catch (_) { return null; }
    if (!validMetadata(metadata)) return null;

    let before = text.slice(0, found.start);
    const after = text.slice(found.close);
    // `wireText` inserts exactly one blank separator after a non-empty original request.
    // Remove only that separator; a following attachment/path line remains exactly where it was.
    if (before.endsWith("\n\n")) before = before.slice(0, -2);
    let visible = before + after;
    if (!before && visible.startsWith("\n")) visible = visible.slice(1);
    return { text: visible, metadata, raw: found.raw };
}

/** Static, escaped disclosure markup. Raw agent metadata remains available without filling chat. */
export function boardWorkflowRecordHTML(record, options = {}) {
    if (!record || !record.metadata || typeof options.escape !== "function") return "";
    const esc = options.escape;
    const metadata = record.metadata;
    const label = options.label || "Board record";
    const summary = [metadata.provider, metadata.coverage, metadata.input_kind]
        .filter(Boolean).join(" · ");
    return '<details class="board-workflow-record"><summary><span>' + esc(label)
        + '</span><small>' + esc(summary) + '</small></summary><pre>'
        + esc(record.raw) + "</pre></details>";
}
