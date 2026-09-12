import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const elements = {
    schedules: { hidden: false },
    "schedules-count": { textContent: "" },
    "schedule-rows": { innerHTML: "" }
};

globalThis.localStorage = {
    getItem: function () { return null; },
    setItem: function () { }
};
globalThis.location = { search: "", protocol: "http:", hostname: "localhost" };
globalThis.window = {
    devicePixelRatio: 1,
    matchMedia: function () { return { matches: false }; }
};
globalThis.document = {
    documentElement: { lang: "en" },
    getElementById: function (id) {
        return elements[id] || { textContent: "", innerHTML: "" };
    }
};

const { loadScheduleProjects } = await import("../Resources/web/app/js/net/schedules.js");
const { renderSchedules, scheduleRunsHTML, scheduleRunPlace } =
    await import("../Resources/web/app/js/view/schedules.js");
const {
    ScheduleWebhookClient,
    scheduleWebhookTimelineHTML,
    scheduleWebhookTimelineEvents,
    scheduleWebhookLatestReceipt,
    scheduleWebhookReceiptHeads,
    scheduleWebhookManagementWarning,
    scheduleWebhookCopy,
    scheduleWebhookCurlExample,
    scheduleWebhookHelpHTML,
    SCHEDULE_WEBHOOK_GUIDE_URL,
    scheduleWebhookSecretIsEphemeral,
    shouldObserveScheduleWebhook,
    scheduleWebhookCanGenerate,
    generateAndBindScheduleWebhook
} = await import("../Resources/web/app/js/net/schedule-webhooks.js");

const scheduleHistorySource = await readFile(
    new URL("../Resources/web/app/js/input/schedule-history.js", import.meta.url), "utf8");
const localScheduleSource = await readFile(
    new URL("../Resources/web/app/js/net/live.js", import.meta.url), "utf8");
const cloudScheduleSource = await readFile(
    new URL("../Resources/web/app/js/net/cloud-client.js", import.meta.url), "utf8");
const cloudBridgeSource = await readFile(
    new URL("../Sources/CloudAppBridge.swift", import.meta.url), "utf8");
const cloudRouteSource = await readFile(
    new URL("../Sources/CloudLocalRoute.swift", import.meta.url), "utf8");
const remoteServerSource = await readFile(
    new URL("../Sources/RemoteServer.swift", import.meta.url), "utf8");
const scheduleWebhookSource = await readFile(
    new URL("../Sources/ScheduleWebhook.swift", import.meta.url), "utf8");
const { scheduleRunConfirmation, scheduleRunCopy, scheduleRunMessage } =
    await import("../Resources/web/app/js/input/schedule-run.js");

const rows = [
    { id: "shown", title: "Morning brief", enabled: true, next_fire: 200 },
    { id: "unavailable", title: "Unavailable", enabled: true, next_fire: 300 },
    { file: "broken.json", state: "invalid", error: "bad input" }
];
const reads = [];
const projectIcon = { accent: "#d97757", cells: [["#d97757"]] };
const hydrated = await loadScheduleProjects(rows, function (id) {
    reads.push(id);
    if (id === "unavailable") return Promise.reject(new Error("gone"));
    return Promise.resolve({
        schedule: { task: { project_dir: "/Users/you/code/<clawdline>" } }
    });
}, function () {
    return Promise.resolve({ places: [{
        path: "/Users/you/code/<clawdline>", label: "clawdline", icon: projectIcon
    }] });
});

assert.deepEqual(reads, ["shown", "unavailable"], "only valid schedule ids need details");
assert.deepEqual(hydrated[0].project, {
    path: "/Users/you/code/<clawdline>", label: "clawdline", icon: projectIcon
});
assert.equal(hydrated[1], rows[1], "a failed detail read preserves the usable summary");
assert.equal(hydrated[2], rows[2], "invalid rows remain local error rows");

// The read that is skipped. A row whose list entry already says where it runs must not open the
// detail route for it: that read used to happen once per row on a refresh that runs every minute,
// and on the Cloud path each one queued in the same single background lane as a transcript.
const carriedReads = [];
const carried = await loadScheduleProjects([
    { id: "carried", title: "Nightly", enabled: true, next_fire: 400,
      project_dir: "/Users/you/code/<clawdline>" },
    { id: "bare", title: "Older Mac", enabled: true, next_fire: 500 }
], function (id) {
    carriedReads.push(id);
    return Promise.resolve({ schedule: { task: { project_dir: "/Users/you/code/<other>" } } });
}, function () {
    return Promise.resolve({ places: [{
        path: "/Users/you/code/<clawdline>", label: "clawdline", icon: projectIcon
    }] });
});
assert.deepEqual(carriedReads, ["bare"],
    "only a row without project_dir costs a detail read");
assert.deepEqual(carried[0].project, {
    path: "/Users/you/code/<clawdline>", label: "clawdline", icon: projectIcon
}, "the carried path is resolved against places exactly as a read one is");
assert.equal(carried[1].project.path, "/Users/you/code/<other>",
    "a Mac that does not send project_dir yet still resolves through the detail route");

renderSchedules(hydrated, 100);
assert.match(elements["schedule-rows"].innerHTML,
    /<canvas class="schedule-project-mark" aria-hidden="true"><\/canvas><span class="schedule-project-name" title="\/Users\/you\/code\/&lt;clawdline&gt;">clawdline<\/span>/,
    "the schedule row shows the project icon slot and project name, with the path only as its title");
assert.match(elements["schedule-rows"].innerHTML,
    /<span class="schedule-project-name"[^>]*>clawdline<\/span><\/span><span class="schedule-meta-sep" aria-hidden="true"> · <\/span><time class="schedule-next"[^>]*>Next/,
    "the project name shares the metadata line with the next-run time");

const runMarkup = scheduleRunsHTML([
    { task_id: "run-live", state: "briefed", assistant: "codex", created: 300,
      terminal_id: "terminal-live", session_id: "session-live", summary: "still publishing" },
    { task_id: "run-done", state: "success", assistant: "codex", created: 200,
      finished_at: 220, session_id: "session-done", summary: "published <today>",
      project_dir: "/Users/you/code/<blog>" },
    { task_id: "run-lost", state: "failure", assistant: "claude", created: 100 }
], 400, function (terminal) { return terminal === "terminal-live"; });
assert.match(runMarkup, /data-task-id="run-live"[^>]*data-action="open"/,
    "a run whose terminal is still present opens it instead of resuming the transcript twice");
assert.match(runMarkup, /data-task-id="run-done"[^>]*data-action="resume"/,
    "a finished run with a proven conversation id is resumable");
assert.match(runMarkup, /published &lt;today&gt;/,
    "run summaries are escaped before they enter the schedule sheet");
assert.match(runMarkup,
    /<span class="schedule-run-summary" title="published &lt;today&gt;">published &lt;today&gt;<\/span>/,
    "the clamped summary keeps its whole text in the title, escaped the same way");
const scheduleCSS = await readFile(
    new URL("../Resources/web/app/css/schedules.css", import.meta.url), "utf8");
assert.match(scheduleCSS, /\.schedule-run-summary\s*\{[^}]*-webkit-line-clamp:\s*2;/,
    "the picker shows the first lines of a run summary, not the whole report");
assert.match(runMarkup,
    /class="schedule-run-meta" title="\/Users\/you\/code\/&lt;blog&gt;">codex · &lt;blog&gt;<\/span>/,
    "each occurrence names the project it actually used, with the full escaped path available");
assert.match(runMarkup, /data-task-id="run-lost"[^>]*disabled/,
    "a run without a proven conversation stays visible but cannot invent a resume action");
assert.ok(runMarkup.indexOf("run-live") < runMarkup.indexOf("run-done"),
    "the server's newest-first run order is kept");

assert.equal(scheduleRunPlace({ project_dir: "/old/project" }, [
    { id: "current", path: "/new/project" },
    { id: "original", path: "/old/project" }
])?.id, "original", "an old run resumes in the project it actually used, not today's template");

const webhookCalls = [];
const webhookClient = new ScheduleWebhookClient({
    fetch: async function (url, options) {
        webhookCalls.push({ url, options });
        return {
            ok: true,
            status: 200,
            json: async function () {
                return { hook: { hook_id: "swh_0123456789abcdefghjkmnpqrs", revision: 2 },
                    public_url: "https://api.clawdline.com/v1/schedule-webhook-trigger/swhk_secret",
                    secret_available: true };
            }
        };
    },
    origin: "https://app.clawdline.com",
    idempotencyKey: function () { return "stable-intent"; }
});
const rotated = await webhookClient.rotate("swh_0123456789abcdefghjkmnpqrs", 1);
assert.equal(webhookCalls[0].url,
    "https://app.clawdline.com/v1/schedule-webhooks/swh_0123456789abcdefghjkmnpqrs/rotate",
    "rotate stays on the authenticated management origin");
assert.equal(webhookCalls[0].options.headers["Idempotency-Key"], "stable-intent",
    "every management mutation carries a stable idempotency key");
assert.equal(webhookCalls[0].options.credentials, "include",
    "management requests use the browser session rather than a local orchestrator credential");
assert.equal(rotated.publicURL.includes("swhk_secret"), true,
    "the one-shot response keeps the bearer URL in JS memory");
assert.equal(scheduleWebhookSecretIsEphemeral(rotated.publicURL, {
    html: "<button data-hook-id='swh_0123456789abcdefghjkmnpqrs'>Copy</button>",
    localStorage: {}, sessionStorage: {}
}), true, "the bearer URL is absent from DOM attributes and browser storage");

const traditional = scheduleWebhookCopy("zh-Hant");
assert.equal(traditional.title, "雲端 Webhook",
    "the app's canonical zh-Hant language renders the Webhook card in Traditional Chinese");
assert.equal(scheduleWebhookCopy("zh-TW").copy, "複製網址",
    "the regional zh-TW spelling reaches the same Traditional Chinese copy");
assert.equal(scheduleWebhookCopy("zh-HK").howToUse, "如何使用？",
    "Traditional Chinese regional variants share the discoverable help action");
assert.equal(scheduleWebhookCopy("zh-Hans").title, "Cloud webhook",
    "unsupported Simplified Chinese falls back honestly instead of claiming a translation");

const curlExample = scheduleWebhookCurlExample();
assert.match(curlExample, /curl[\s\S]*--request POST/,
    "the help gives a runnable POST example");
assert.match(curlExample, /CLAWDLINE_SCHEDULE_WEBHOOK_URL/,
    "the example uses a secret variable rather than inviting a capability into source code");
assert.match(curlExample, /Idempotency-Key/,
    "the example shows the retry identity header");
assert.match(curlExample, /--data '\{\}'/,
    "the example sends the protocol's only JSON body");
assert.doesNotMatch(curlExample, /swhk_secret/,
    "the generic example never contains the one-shot capability URL");

const traditionalHelp = scheduleWebhookHelpHTML("zh-Hant");
for (const fact of ["Pro", "POST", "{}", "GET", "不會啟動排程", "Idempotency-Key", "202", "不代表 Mac 已執行", "密碼"]) {
    assert.match(traditionalHelp, new RegExp(fact.replace(/[{}]/g, "\\$&")),
        `Traditional Chinese help explains ${fact}`);
}
assert.doesNotMatch(traditionalHelp, /swhk_secret/,
    "help markup never repeats the bearer URL");
assert.equal(SCHEDULE_WEBHOOK_GUIDE_URL,
    "https://clawdline.com/docs/schedule-webhooks",
    "the in-app help points to the public canonical guide");
assert.match(traditionalHelp,
    /href="https:\/\/clawdline\.com\/docs\/schedule-webhooks"[^>]*target="_blank"[^>]*rel="noreferrer"/,
    "the discoverable Traditional Chinese help opens the public guide safely");
assert.match(scheduleHistorySource, /aria-expanded/,
    "the inline help toggle exposes its state to assistive technology");
assert.match(scheduleHistorySource, /role["']?,?\s*["']region|setAttribute\(["']role["'],\s*["']region["']\)/,
    "the expanded instructions are a named accessible region");
assert.match(scheduleHistorySource, /scheduleWebhookCurlExample/,
    "the inline help can copy the safe placeholder command");
assert.match(scheduleCSS, /\.schedule-webhook-help[\s\S]*overflow-x:\s*auto/,
    "the inline instructions keep the command readable on narrow screens");
assert.equal(scheduleWebhookCopy("zh-Hant").showDetails, "展開 Webhook 詳情",
    "the compact Webhook card has a Traditional Chinese disclosure label");
assert.match(scheduleHistorySource,
    /webhookOpen\s*=\s*false[\s\S]*dataset\.collapsed[\s\S]*aria-expanded/,
    "Webhook details start collapsed and expose their disclosure state");
assert.match(scheduleHistorySource,
    /open:\s*!els\["schedule-history"\]\.hidden\s*&&\s*webhookOpen/,
    "a collapsed receipt timeline is not recorded as human-observed");
assert.match(scheduleCSS,
    /\.schedule-history-sheet\s*\{[\s\S]*overflow-y:\s*auto/,
    "the complete Schedule detail sheet scrolls when its content exceeds the viewport");
assert.match(scheduleCSS,
    /\.schedule-webhook\[data-collapsed="true"\][\s\S]*display:\s*none/,
    "the collapsed Webhook card hides its instructions, controls, and receipt timeline");

assert.equal(scheduleRunCopy("zh-Hant").button, "立即執行",
    "the canonical Traditional Chinese schedule sheet translates Run now");
assert.equal(scheduleRunCopy("zh-TW").running, "正在啟動…",
    "the regional Traditional Chinese locale translates the pending state");
assert.match(scheduleRunConfirmation("Atrium", "zh-Hant"),
    /Atrium[\s\S]*Mac[\s\S]*真實工作/,
    "the confirmation names the schedule and warns that real Mac work starts");
assert.match(scheduleRunConfirmation("Atrium", "en"), /counts as that occurrence/,
    "the confirmation explains that a due occurrence can be consumed");
assert.equal(scheduleRunMessage({ code: "schedule_active" }, "zh-Hant"),
    "此排程已有一個執行中的工作。",
    "a duplicate press gets the typed active-run explanation");
assert.equal(scheduleRunMessage({ code: "schedule_spent" }, "zh-Hant"),
    "這個單次排程已經執行過；若要再次執行，請建立新排程。",
    "a spent one-shot is not presented as retryable");
assert.match(scheduleHistorySource,
    /id\s*=\s*["']schedule-history-run-now["'][\s\S]*window\.confirm[\s\S]*api\.runSchedule/,
    "the schedule detail creates a discoverable button, confirms, then uses the typed API");
assert.match(scheduleHistorySource,
    /runningNow\s*=\s*true[\s\S]*Schedules\.refresh\(\)[\s\S]*api\.schedule\(id\)/,
    "a successful press refreshes both the list and the execution history");
assert.match(localScheduleSource,
    /LocalClient\.runSchedule[\s\S]*Idempotency-Key[\s\S]*uuid\(\)/,
    "the direct paired-browser command carries a per-press idempotency key");
assert.doesNotMatch(localScheduleSource.match(
    /LocalClient\.runSchedule[\s\S]*?\n};/)[0], /X-Clawdline-Orchestrator|dispatchToken/i,
    "the browser Run now implementation never receives the machine orchestrator credential");
assert.match(cloudScheduleSource,
    /runSchedule\(id\)[\s\S]*_machineRequest[\s\S]*["']schedule-run["']/,
    "the hosted viewer sends one closed encrypted command rather than naming a local route");
assert.match(cloudBridgeSource,
    /case "schedule-delete", "schedule-run"[\s\S]*type == "schedule-delete"\s*\?\s*\.scheduleDelete\(id: id\)\s*:\s*\.scheduleRun\(id: id\)/,
    "the Mac decoder keeps Run now distinct from deleting the same schedule");
assert.match(cloudRouteSource,
    /case \.scheduleRun\(let id\):[\s\S]*\/v1\/orchestrator\/schedules\/\\\(Self\.segment\(id\)\)\/run/,
    "the closed command maps to the existing named manual-run route");
assert.match(scheduleWebhookSource,
    /func receiptV1[\s\S]*taskID:\s*nil/,
    "the receipt v1 normalizer removes the early private task identity");
assert.match(scheduleWebhookSource,
    /pending\.receiptVersion == 1, pending\.taskID != nil[\s\S]*pending = receiptV1/,
    "startup repairs the released receipt v1 shape before retrying it to Cloud");
assert.match(remoteServerSource,
    /routeVerifiedCloudCommand[\s\S]*isOrchestratorTerminalWorkerRoute\(request\.path\)[\s\S]*terminalMutation\(request\)/,
    "verified Cloud Run now enters the bounded filed terminal worker");
assert.match(remoteServerSource,
    /unfiledTerminalMutation[\s\S]*hasPrefix\("\/v1\/orchestrator\/schedules\/"\)[\s\S]*hasSuffix\("\/run"\)[\s\S]*!Orchestrator\.verifyDispatch[\s\S]*terminalMutation\(request, deliver: deliver\)/,
    "direct paired Run now enters the write/send/idempotency gate before its worker");

assert.match(scheduleWebhookManagementWarning(null, {
    language: "zh-Hant", bindingAvailability: "unbound", effectiveTier: "free"
}), /需要專業版/,
"the canonical Traditional Chinese locale translates the Pro entitlement warning");

const productionDeliveries = [
    { delivery_id: "swd_1", state: "leased", accepted_at: "2026-09-08T00:00:00.000Z",
      attempt: 2, receipts: [
        { receipt_version: 1, kind: "mac_durable_accepted",
          occurred_at: "2026-09-08T00:00:01.000Z",
          acknowledged_at: "2026-09-08T00:00:02.000Z" },
        { receipt_version: 2, kind: "schedule_dispatch_deferred", outcome_code: "terminal_busy",
          retry_at: "2026-09-08T00:01:00.000Z" },
        { receipt_version: 3, kind: "schedule_dispatch_accepted" },
        { receipt_version: 4, kind: "task_execution_terminal",
          task_terminal_state: "spawn_failed", acknowledged_at: "2026-09-08T00:02:00.000Z" }
      ] },
    { delivery_id: "swd_2", state: "queued", accepted_at: "2026-09-08T00:03:00.000Z",
      receipts: [] },
    { delivery_id: "swd_3", state: "expired", receipts: [] },
    { delivery_id: "swd_4", state: "schedule_dispatch_refused", receipts: [
        { receipt_version: 2, kind: "schedule_dispatch_refused", outcome_code: "schedule_disabled" }
      ] },
    { delivery_id: "swd_5", state: "outcome_unknown", receipts: [
        { receipt_version: 2, kind: "schedule_dispatch_refused", outcome_code: "outcome_unknown" }
      ] },
    { delivery_id: "swd_6", state: null, receipts: [] }
];
const timeline = scheduleWebhookTimelineHTML(productionDeliveries,
    { "swd_1:4": "2026-09-08T00:02:01.000Z" });
for (const state of ["queued", "leased", "mac_durable_accepted", "schedule_dispatch_deferred",
    "schedule_dispatch_refused", "task_execution_terminal", "cloud_receipt_acknowledged",
    "human_observed", "expired", "outcome_unknown", "missing"]) {
    assert.match(timeline, new RegExp('data-webhook-state="' + state + '"'),
        state + " has its own manager-visible rendering");
}
assert.doesNotMatch(timeline, /swhk_/, "timeline rendering never carries the bearer URL");
const traditionalTimeline = scheduleWebhookTimelineHTML(productionDeliveries, {}, "zh-Hant");
assert.match(traditionalTimeline, /已由 Mac 持久接受/,
    "the receipt timeline follows the Webhook card into Traditional Chinese");
assert.match(traditionalTimeline, /第 2 次嘗試/,
    "attempt metadata is translated instead of leaving an English fragment");
assert.match(traditionalTimeline, /時間戳記不可用/,
    "missing timestamps are translated explicitly");
assert.deepEqual(scheduleWebhookLatestReceipt(productionDeliveries),
    { deliveryID: "swd_1", receiptVersion: 4 },
    "observation targets the newest nested production receipt");
assert.deepEqual(scheduleWebhookReceiptHeads(productionDeliveries), [
    { deliveryID: "swd_1", receiptVersion: 4 },
    { deliveryID: "swd_4", receiptVersion: 2 },
    { deliveryID: "swd_5", receiptVersion: 2 }
], "each visible delivery gets its own monotonic observation head");
assert.equal(scheduleWebhookTimelineEvents(productionDeliveries, {})
    .some((event) => event.state === "human_observed"), false,
    "a GET or render cannot invent human_observed before the mutation acknowledgement");
assert.equal(shouldObserveScheduleWebhook({ open: true, visibilityState: "visible", renderedVersion: 4 }), true,
    "a visible, open, rendered timeline may be observed");
assert.equal(shouldObserveScheduleWebhook({ open: true, visibilityState: "hidden", renderedVersion: 4 }), false,
    "a hidden refresh is not human observation");
assert.equal(shouldObserveScheduleWebhook({ open: false, visibilityState: "visible", renderedVersion: 4 }), false,
    "prefetch outside the sheet is not human observation");
assert.equal(shouldObserveScheduleWebhook({ open: true, visibilityState: "visible", renderedVersion: 0 }), false,
    "an empty or missing receipt is not observed");

const disabledHook = { hook_id: "swh_0123456789abcdefghjkmnpqrs", state: "disabled" };
assert.equal(scheduleWebhookCanGenerate(disabledHook, {
    bindingAvailability: "active", effectiveTier: "pro"
}), true,
    "a disabled hook exposes Generate rather than leaving the schedule with no action");
assert.equal(scheduleWebhookCanGenerate(null, { effectiveTier: "pro" }), false,
    "a missing binding projection is not silently treated as an empty binding");
let replacementBind = null;
const generated = await generateAndBindScheduleWebhook({
    create: async () => ({ hook: { hook_id: "swh_1123456789abcdefghjkmnpqrs", revision: 0 },
        publicURL: "https://api.clawdline.com/v1/schedule-webhook-trigger/swhk_new" })
}, async function (scheduleID, hookID, replaceHookID) {
    replacementBind = { scheduleID, hookID, replaceHookID };
    return { hook_id: hookID, state: "active", availability: "active", revision: 1 };
}, "mac-1", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", disabledHook);
assert.deepEqual(replacementBind, {
    scheduleID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
    hookID: "swh_1123456789abcdefghjkmnpqrs",
    replaceHookID: "swh_0123456789abcdefghjkmnpqrs"
}, "Generate after disable binds the new hook with the old hook id as the local replace CAS");
assert.equal(generated.hook.revision, 1,
    "Generate returns the activated revision used by the next Rotate or Disable CAS");

await webhookClient.observe("swh_0123456789abcdefghjkmnpqrs", "swd_1", 4);
assert.equal(webhookCalls.at(-1).options.headers["Idempotency-Key"],
    "observed:swd_1:4",
    "observation uses the browser-constructible deterministic key; authenticated identity scopes it server-side");
assert.match(scheduleWebhookManagementWarning(null, {
    bindingAvailability: "binding_store_unavailable", effectiveTier: "pro"
}), /binding store is unavailable/i,
"corrupt local binding projection blocks a second provisioning attempt with an explicit reason");
assert.match(scheduleWebhookManagementWarning(null, {
    bindingAvailability: "unbound", effectiveTier: "free"
}), /require Pro/i, "effective Free explains the Pro gate while history remains readable");
const activeWarning = scheduleWebhookManagementWarning({ state: "active" }, {
    bindingAvailability: "active", effectiveTier: "pro"
});
assert.match(activeWarning, /Rotating immediately invalidates the old URL/i,
    "Rotate states that the old capability is revoked immediately");
assert.match(activeWarning, /Disabling cancels only work the Mac has not durably accepted/i,
    "Disable states the queued-versus-accepted consequence");

console.log("web schedule tests passed");
process.exit(0);
