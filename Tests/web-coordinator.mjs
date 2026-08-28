import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

import {
    coordinatorAnswerHTML,
    coordinatorExecutes,
    coordinatorForSession,
    coordinatorGroups,
    coordinatorPanelHTML,
    coordinatorPreview,
    coordinatorReason,
    coordinatorRoute,
    coordinatorRowModel
} from "../Resources/web/app/js/input/coordinator-actions.js";
import { T, applyStrings } from "../Resources/web/app/js/core/i18n.js";
import { esc } from "../Resources/web/app/js/core/esc.js";

const ordinary = {
    id: "ordinary",
    label: "ordinary session",
    icon: { accent: "#d97757", cells: [["#d97757"]] }
};

assert.equal(coordinatorForSession(ordinary), null,
    "a session without the optional broker record stays ordinary");
assert.equal(coordinatorRowModel(ordinary), null,
    "ordinary sessions gain no marker DOM model");
assert.equal(coordinatorRoute(ordinary, "mark"), "session",
    "the ordinary project mark keeps opening the session");

const coordinatorSession = {
    id: "clawdfather",
    label: "Clawdfather",
    coordinator: {
        label: "Clawdfather",
        status: "online",
        commands: [
            { type: "status_report", enabled: true },
            { type: "since_away", enabled: false, why: "No return point has been recorded." },
            { type: "duplicates_conflicts_ownership", enabled: true },
            { type: "landing_closure", enabled: true },
            { type: "coordinate_work", enabled: true },
            { type: "dispatch_independent_work", enabled: true },
            { type: "ask_coordinator", enabled: true },
            { type: "quiet_watch", enabled: true },
            { type: "scope_permissions", enabled: true },
            { type: "stop", enabled: true },
            { type: "reconnect", enabled: true },
            { type: "invented_future_command", enabled: true }
        ]
    }
};

const record = coordinatorForSession(coordinatorSession);
assert.equal(record.label, "Clawdfather");
assert.equal(coordinatorRoute(coordinatorSession, "mark"), "controls",
    "only a coordinator mark routes to Coordinator controls");
assert.equal(coordinatorRoute(coordinatorSession, "row"), "session",
    "the rest of a coordinator row still routes to its session");

const row = coordinatorRowModel(coordinatorSession);
assert.equal(row.badge, "Clawdfather");
assert.equal(row.mark.role, "button");
assert.equal(row.mark.ariaHaspopup, "dialog");
assert.equal(row.mark.ariaLabel, "Open Clawdfather controls");

const online = coordinatorGroups(coordinatorSession, { connected: true });
assert.deepEqual(online.map((group) => group.id), [
    "observe", "coordinate", "presence", "administration"
]);
const actions = online.flatMap((group) => group.actions);
assert.equal(actions.some((action) => action.type === "invented_future_command"), false,
    "unknown commands are ignored rather than guessed");
assert.equal(actions.length, 11);

const disabled = actions.find((action) => action.type === "since_away");
assert.equal(disabled.state, "disabled");
assert.equal(disabled.why, "No return point has been recorded.");

assert.equal(actions.find((action) => action.type === "status_report").effect, "read_only");
assert.equal(actions.find((action) => action.type === "landing_closure").effect, "read_only",
    "landing closure is a connected read now, not a drafted judgement");
assert.equal(actions.find((action) => action.type === "dispatch_independent_work").effect,
    "spawns_session");

const mutation = actions.find((action) => action.type === "stop");
assert.equal(mutation.state, "preview");
const preview = coordinatorPreview(mutation);
assert.equal(preview.requiresConfirmation, true);
assert.equal(preview.confirmDisabled, true,
    "Phase A never lets a mutation cross the preview gate");
assert.match(preview.note, /nothing has been sent/i);

const offline = coordinatorGroups({
    id: "offline",
    coordinator: {
        label: "Clawdfather",
        status: "offline",
        commands: [
            { type: "status_report", enabled: true },
            { type: "coordinate_work", enabled: true },
            { type: "dispatch_independent_work", enabled: true },
            { type: "stop", enabled: true }
        ]
    }
}, { connected: true }).flatMap((group) => group.actions);
assert.equal(offline.find((action) => action.type === "status_report").state, "available");
assert.equal(offline.find((action) => action.type === "coordinate_work").state, "draft");
assert.equal(offline.find((action) => action.type === "dispatch_independent_work").state,
    "unavailable");
assert.equal(offline.find((action) => action.type === "stop").state, "unavailable");

const disconnected = coordinatorGroups(coordinatorSession, { connected: false })
    .flatMap((group) => group.actions);
assert.equal(disconnected.find((action) => action.type === "status_report").state, "available");
assert.equal(disconnected.find((action) => action.type === "coordinate_work").state, "draft");
assert.match(coordinatorPanelHTML(coordinatorSession, { connected: false }),
    /data-status="offline"[^>]*>.*Clawdfather offline/s,
    "a dropped browser connection cannot leave the controls claiming the coordinator is online");

const html = coordinatorPanelHTML(coordinatorSession, { connected: true });
assert.match(html, /data-command="status_report"/);
assert.doesNotMatch(html, /invented_future_command/);
assert.match(html, /No return point has been recorded\./);
assert.match(html, /data-effect="read_only"/);
assert.match(html, /data-effect="advisory"/);
assert.match(html, /data-effect="spawns_session"/);
assert.match(html, /data-command="stop"[^>]*data-state="preview"/);
assert.match(html, /Session actions/,
    "Coordinator controls keep an independent exit to ordinary Session actions");
assert.doesNotMatch(html, />[^<]*Coordinator[^<]*</,
    "user-facing controls consistently say Clawdfather");

const index = await readFile(new URL("../Resources/web/index.html", import.meta.url), "utf8");
assert.match(index,
    /id="coordinator-controls-sheet"[^>]*role="dialog"[^>]*aria-modal="true"[^>]*aria-labelledby="coordinator-controls-title"/,
    "the controls surface has an accessible dialog name and modality");
assert.match(index, /id="coordinator-controls-close"[^>]*type="button"/);
assert.match(index, /id="coordinator-controls-title">Clawdfather controls</);

const listSource = await readFile(
    new URL("../Resources/web/app/js/view/list.js", import.meta.url), "utf8"
);
assert.match(listSource, /coordinatorRoute\([^,]+, "mark"\)/,
    "the logo click uses the tested route selector");
assert.match(listSource, /stopPropagation\(\)/,
    "the logo click cannot bubble into the ordinary row route");
assert.match(listSource, /className = "clawdfather-crown"/,
    "the authenticated Clawdfather mark receives its crown");
assert.match(listSource, /crown\.setAttribute\("aria-hidden", "true"\)/,
    "the decorative crown does not duplicate the accessible Clawdfather label");

const coordinatorCSS = await readFile(
    new URL("../Resources/web/app/css/coordinator.css", import.meta.url), "utf8"
);
assert.match(coordinatorCSS,
    /\.row \.clawdfather-crown\s*\{[^}]*position:\s*absolute[^}]*clip-path:/s,
    "the crown is a deterministic overlay rather than part of the project icon bitmap");
assert.match(coordinatorCSS,
    /\.row \.clawdfather-crown\s*\{[^}]*transform:\s*rotate\(-30deg\)/s,
    "the crown leans 30 degrees into the Clawdfather icon without looking loose");

const deriveSource = await readFile(
    new URL("../Resources/web/app/js/view/derive.js", import.meta.url), "utf8"
);
assert.match(deriveSource, /coordinatorFirst\(a, b\)/,
    "the session ordering applies the authenticated Clawdfather pin");

const controlsSource = await readFile(
    new URL("../Resources/web/app/js/input/coordinator-actions.js", import.meta.url), "utf8"
);
const showControlsAt = controlsSource.indexOf("this.dom.overlay.hidden = false;");
const resetControlsAt = controlsSource.indexOf("this.dom.body.scrollTop = 0;");
const focusControlsAt = controlsSource.indexOf("first.focus({ preventScroll: true });");
assert.ok(showControlsAt >= 0 && resetControlsAt > showControlsAt &&
    focusControlsAt > resetControlsAt,
    "the visible controls reset stale scroll state before focusing the first command");

// --- The server's closed disabled-reason codes, said in the page's own words -----------------

assert.equal(coordinatorReason({ enabled: false, reason: "device_cannot_spawn",
    why: "English prose from the wire." }), T.webCoordWhyDeviceCannotSpawn,
    "a known reason code wins over the compatibility prose");
assert.equal(coordinatorReason({ enabled: false, reason: "machine_token_only" }),
    T.webCoordWhyMachineTokenOnly);
assert.equal(coordinatorReason({ enabled: false, reason: "no_command_route" }),
    T.webCoordWhyNoCommandRoute);
assert.equal(coordinatorReason({ enabled: false, reason: "no_return_ledger" }),
    T.webCoordWhyNoReturnLedger);
assert.equal(coordinatorReason({ enabled: false, reason: "invented_future_reason",
    why: "The prose fallback." }), "The prose fallback.",
    "an unknown code falls back to the server's prose rather than guessing");
assert.equal(coordinatorReason({ enabled: false }), T.webCoordDisabledFallback,
    "a row with neither code nor prose still explains itself");

// --- Only the connected reads execute; everything else keeps its honest receipt ---------------

assert.equal(coordinatorExecutes({ effect: "read_only", state: "available" }), true);
assert.equal(coordinatorExecutes({ effect: "read_only", state: "disabled" }), false,
    "a server-disabled read never fetches");
assert.equal(coordinatorExecutes({ effect: "mutation", state: "preview" }), false,
    "a mutation never executes from this panel");
assert.equal(coordinatorExecutes({ effect: "advisory", state: "draft" }), false);
assert.equal(coordinatorExecutes(null), false);

// --- Rendering the four connected reads from the device-readable Bearings projection ----------

const bearingsPayload = {
    version: 1, observed_at: 1787832060,
    coordinator: { configured: true, label: "Clawdfather", scope: "machine",
                   status: "online", lifecycle: "standby" },
    bearings: {
        observed_at: 1787832060, coordinator_lifecycle: "standby",
        work_state_counts: { ready: 0, working: 3, waiting_human: 1, waiting_session: 1,
                             needs_triage: 2, milestone_complete: 1, work_complete: 0 },
        active_task_count: 2, pending_landing_count: 1, open_wait_count: 4,
        needs_triage: [{ id: "T-1", label: "<script>alert(1)</script>",
                         work_state: "needs_triage" }],
        waiting: [{ id: "W-1", label: "the signup flow", work_state: "waiting_human" }],
        blocking: [],
        sources: { sessions: { observed_at: 1787832060, freshness: "stale" } }
    }
};

const statusHTML = coordinatorAnswerHTML({ type: "status_report" }, bearingsPayload);
assert.ok(statusHTML.includes("Clawdfather online"),
    "the status report says whether Clawdfather is online");
assert.ok(statusHTML.includes("3 working") && statusHTML.includes("2 waiting"),
    "waiting_human and waiting_session are counted together");
assert.ok(statusHTML.includes("2 need triage"));
assert.ok(statusHTML.includes("2 tasks in flight")
    && statusHTML.includes("1 deliveries awaiting landing")
    && statusHTML.includes("4 open file waits"));
assert.ok(statusHTML.includes(esc(T.webCoordStaleSessions)),
    "a stale session watch is said, never silently drawn as current");

const duplicatesHTML = coordinatorAnswerHTML(
    { type: "duplicates_conflicts_ownership" }, bearingsPayload);
assert.ok(duplicatesHTML.includes("&lt;script&gt;"),
    "session labels from the wire are escaped, never markup");
assert.ok(!duplicatesHTML.includes("<script>"));
assert.ok(duplicatesHTML.includes(T.webCoordWaitingList)
    && duplicatesHTML.includes(T.webCoordNeedsTriage));
assert.ok(!duplicatesHTML.includes(T.webCoordBlockingList),
    "an empty list is omitted rather than drawn as an empty heading");

const quietHTML = coordinatorAnswerHTML({ type: "duplicates_conflicts_ownership" }, {
    coordinator: { configured: true, label: "Clawdfather", status: "online" },
    bearings: { needs_triage: [], waiting: [], blocking: [], open_wait_count: 0 }
});
assert.ok(quietHTML.includes(T.webCoordAllQuiet));

const landingsHTML = coordinatorAnswerHTML({ type: "landing_closure" }, bearingsPayload);
assert.ok(landingsHTML.includes("1 deliveries awaiting landing"));
assert.ok(coordinatorAnswerHTML({ type: "landing_closure" }, {
    coordinator: {}, bearings: { pending_landing_count: 0 }
}).includes(T.webCoordNoLandings));

const scopeHTML = coordinatorAnswerHTML({ type: "scope_permissions" }, bearingsPayload);
assert.ok(scopeHTML.includes("Clawdfather coordinates this Mac"));
assert.ok(scopeHTML.includes(esc(T.webCoordScopeDevice)),
    "scope & permissions says plainly what a device may never do");
assert.ok(coordinatorAnswerHTML({ type: "scope_permissions" }, {
    coordinator: { configured: false }, bearings: {}
}).includes(T.webCoordUnregistered));

// --- The wiring behind the connected reads, checked as source like the focus rules above ------

const bearingsCallAt = controlsSource.indexOf("api.coordinatorBearings()");
const executesGateAt = controlsSource.indexOf("if (coordinatorExecutes(action))");
assert.ok(executesGateAt >= 0 && bearingsCallAt > executesGateAt,
    "only a command that executes reaches the Bearings fetch");
assert.match(controlsSource, /typeof api\.coordinatorBearings !== "function"/,
    "a transport without this read fails honestly instead of throwing");

const liveSource = await readFile(
    new URL("../Resources/web/app/js/net/live.js", import.meta.url), "utf8");
assert.match(liveSource, /"\/v1\/orchestrator\/coordinator\/bearings"/,
    "the live transport reads the device-readable projection route");

// --- The panel speaks whatever language /v1/strings answered in ------------------------------
// `applyStrings` mutates the module-global T, so this block stays last.

applyStrings({
    webCoordSectionObserve: "觀察",
    webCoordCmdStatusReport: "狀態報告",
    webCoordCmdStatusReportSay: "讀取 Clawdfather 目前的狀態與進行中的職責。",
    webCoordOnline: "{name} 在線",
    webCoordStateAvailable: "可用",
    webCoordEffectRead: "唯讀",
    webCoordWhyDeviceCannotSpawn: "配對裝置永遠不能開 session。",
    webCoordControlsTitle: "{name} 控制面板"
});
const localised = coordinatorPanelHTML(coordinatorSession, { connected: true });
assert.ok(localised.includes("觀察") && localised.includes("狀態報告")
    && localised.includes("讀取 Clawdfather 目前的狀態與進行中的職責。"),
    "sections, labels and summaries all come from T at render time");
assert.ok(localised.includes("Clawdfather 在線"),
    "presence fills the coordinator's name into the translated pattern");
assert.ok(localised.includes("可用") && localised.includes("唯讀"),
    "state and effect words are translated too");
assert.equal(coordinatorReason({ enabled: false, reason: "device_cannot_spawn",
    why: "English prose from the wire." }), "配對裝置永遠不能開 session。",
    "a reason code is said in the page's language even when the wire prose is English");

console.log("web coordinator tests passed");
process.exit(0);
