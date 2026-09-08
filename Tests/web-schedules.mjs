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
    scheduleWebhookSecretIsEphemeral,
    shouldObserveScheduleWebhook,
    scheduleWebhookCanGenerate,
    generateAndBindScheduleWebhook
} = await import("../Resources/web/app/js/net/schedule-webhooks.js");

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
