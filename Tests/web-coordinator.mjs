import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

import {
    coordinatorForSession,
    coordinatorGroups,
    coordinatorPanelHTML,
    coordinatorPreview,
    coordinatorRoute,
    coordinatorRowModel
} from "../Resources/web/app/js/input/coordinator-actions.js";

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
assert.equal(row.badge, "Coordinator");
assert.equal(row.mark.role, "button");
assert.equal(row.mark.ariaHaspopup, "dialog");
assert.match(row.mark.ariaLabel, /Clawdfather.*Coordinator controls/i);

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
assert.equal(actions.find((action) => action.type === "landing_closure").effect, "advisory");
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
    /data-status="offline"[^>]*>.*Coordinator offline/s,
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

const index = await readFile(new URL("../Resources/web/index.html", import.meta.url), "utf8");
assert.match(index,
    /id="coordinator-controls-sheet"[^>]*role="dialog"[^>]*aria-modal="true"[^>]*aria-labelledby="coordinator-controls-title"/,
    "the controls surface has an accessible dialog name and modality");
assert.match(index, /id="coordinator-controls-close"[^>]*type="button"/);

const listSource = await readFile(
    new URL("../Resources/web/app/js/view/list.js", import.meta.url), "utf8"
);
assert.match(listSource, /coordinatorRoute\([^,]+, "mark"\)/,
    "the logo click uses the tested route selector");
assert.match(listSource, /stopPropagation\(\)/,
    "the logo click cannot bubble into the ordinary row route");

const controlsSource = await readFile(
    new URL("../Resources/web/app/js/input/coordinator-actions.js", import.meta.url), "utf8"
);
const showControlsAt = controlsSource.indexOf("this.dom.overlay.hidden = false;");
const resetControlsAt = controlsSource.indexOf("this.dom.body.scrollTop = 0;");
const focusControlsAt = controlsSource.indexOf("first.focus({ preventScroll: true });");
assert.ok(showControlsAt >= 0 && resetControlsAt > showControlsAt &&
    focusControlsAt > resetControlsAt,
    "the visible controls reset stale scroll state before focusing the first command");

console.log("web coordinator tests passed");
process.exit(0);
