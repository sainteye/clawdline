import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
    bindTimelinePage,
    githubCommitURL,
    timelineDateGroup,
    timelineStatus,
} from "../Resources/web/app/js/view/timeline.js";
import { CloudClient } from "../Resources/web/app/js/net/cloud-client.js";

class FakeNode {
    constructor(document) {
        this.ownerDocument = document; this.children = []; this.listeners = {};
        this.className = ""; this.dataset = {}; this.hidden = false; this.disabled = false;
        this.textContent = ""; this.value = ""; this.checked = false;
        this.classList = { toggle() {} };
    }
    appendChild(child) { this.children.push(child); return child; }
    replaceChildren(...children) { this.children = children; }
    addEventListener(type, listener) { this.listeners[type] = listener; }
    setAttribute(name, value) { this[name] = value; }
    fire(type, fields = {}) { return this.listeners[type]?.({ target: this, stopPropagation() {}, ...fields }); }
}

function timelineElements() {
    const document = { documentElement: { lang: "zh-Hant" }, createElement() { return new FakeNode(document); } };
    const ids = ["timeline", "timeline-back", "timeline-refresh", "timeline-board-tab", "timeline-title",
        "timeline-project-mark", "timeline-subtitle", "timeline-status", "timeline-environment",
        "timeline-category", "timeline-upcoming", "timeline-items", "timeline-detail",
        "settings-timeline-title", "settings-timeline-say", "settings-timeline-toggle",
        "settings-timeline-history", "settings-timeline-status"];
    return Object.fromEntries(ids.map(id => [id, new FakeNode(document)]));
}

function descendants(node) {
    return [node, ...node.children.flatMap(descendants)];
}

function snapshot(revision, enabled, entries = [], nextCursor = null) {
    return { timeline: { revision, enabled, viewer: { canManage: true }, status: "ready",
        project: { label: "Timeline Project" }, entries, selected: null, nextCursor } };
}

function timelineRow(id, effectiveAt) {
    return { id, projectId: "project-one", primaryCategory: "feature", originalTitle: id,
        summary: "", boardItemIds: [], sourceRevisions: [],
        projection: { status: "available", effectiveAt, availableTargets: 1, requiredTargets: 1 } };
}

assert.equal(
    githubCommitURL("github:sainteye/clawdline", "6f8aa65ea0e2211d3b359ec583c7cdf2aa24ab48"),
    "https://github.com/sainteye/clawdline/commit/6f8aa65ea0e2211d3b359ec583c7cdf2aa24ab48"
);
assert.equal(githubCommitURL("local:/private/repo", "6f8aa65e"), null);
assert.equal(githubCommitURL("github:sainteye/clawdline", "not-a-sha"), null);

assert.deepEqual(timelineStatus("available", "zh-Hant"), {
    label: "已上線",
    tone: "success",
});
assert.deepEqual(timelineStatus("deploy_pending", "zh-Hant"), {
    label: "部署成功，待可用性確認",
    tone: "pending",
});
assert.deepEqual(timelineStatus("partial", "en"), {
    label: "Partially available",
    tone: "warning",
});

const now = new Date("2026-09-10T12:00:00+08:00");
assert.equal(timelineDateGroup(1788969600, now, "zh-Hant"), "今天");
assert.equal(timelineDateGroup(null, now, "zh-Hant"), "時間未知");

const calls = [];
const cloud = {
    _knownMachines: () => ["mac-a", "mac-b"],
    _onlyMachine: () => { throw new Error("explicit machine must not fall back"); },
    _machineRequest: (...args) => { calls.push(args); return args; },
};
CloudClient.prototype.board.call(cloud, "project-one", "item-one", null, "mac-b");
assert.equal(calls.at(-1)[0], "mac-b");
assert.equal(calls.at(-1)[1], "board");
CloudClient.prototype.timeline.call(cloud, "project-one", "entry-one", "40", "production", "feature", true, "mac-a");
assert.equal(calls.at(-1)[0], "mac-a");
assert.equal(calls.at(-1)[1], "timeline");
assert.deepEqual(calls.at(-1)[2], {
    project: "project-one", entry: "entry-one", cursor: "40",
    environment: "production", category: "feature", upcoming: true,
});
assert.throws(() => CloudClient.prototype.board.call(cloud, "p", "i", null, "gone"),
    error => error.code === "cloud_machine_unavailable");
assert.throws(() => CloudClient.prototype.timeline.call(cloud, "p", "e", "", "production", "", false, "gone"),
    error => error.code === "cloud_machine_unavailable");

const timelineSource = readFileSync(new URL("../Resources/web/app/js/view/timeline.js", import.meta.url), "utf8");
assert.doesNotMatch(timelineSource, /environment\.onMode\(snapshot\)/,
    "Timeline's independent mode must not be applied to BoardControls");

const modeElements = timelineElements();
let serverRevision = 1, serverEnabled = true, readCalls = 0, offlineOnce = false;
const commandCalls = [];
const modePage = bindTimelinePage(modeElements, {
    read: async () => {
        readCalls += 1;
        if (readCalls > 1 && serverRevision === 1) serverRevision = 2;
        return snapshot(serverRevision, serverEnabled);
    },
    command: async body => {
        commandCalls.push({ ...body });
        if (body.expectedRevision === 1) throw Object.assign(new Error("stale"), { code: "revision_conflict" });
        if (offlineOnce) { offlineOnce = false; throw Object.assign(new Error("offline"), { code: "offline" }); }
        serverRevision += 1; serverEnabled = body.enabled;
        return snapshot(serverRevision, serverEnabled);
    },
    openBoard() {}, onMode() { throw new Error("Timeline must not apply Board mode"); },
});
await modePage.enter("project-one", { label: "Timeline Project" });
assert.deepEqual(modeElements["timeline-environment"].children.map(option => option.value),
    ["production", "staging", "preview", "development", "all"], "environment filter is usable");
assert.deepEqual(modeElements["timeline-category"].children.map(option => option.value),
    ["", "deploy", "server", "architecture", "feature", "operation"], "category filter is usable");
assert.equal(modeElements["timeline-refresh"].textContent, "重新整理", "Timeline controls use the UI language");
await modeElements["settings-timeline-toggle"].fire("click");
assert.equal(modePage.state.pendingMode, null, "deterministic CAS refusal clears pending command");
assert.equal(readCalls, 2, "deterministic CAS refusal refreshes current revision");
const rejectedRequest = commandCalls[0].requestId;
await modeElements["settings-timeline-toggle"].fire("click");
assert.equal(commandCalls[1].expectedRevision, 2, "next mode command uses refreshed CAS revision");
assert.notEqual(commandCalls[1].requestId, rejectedRequest, "next mode command uses a new request identity");
offlineOnce = true;
await modeElements["settings-timeline-toggle"].fire("click");
const uncertainRequest = commandCalls.at(-1).requestId;
assert.equal(modePage.state.pendingMode.requestId, uncertainRequest,
    "uncertain transport failure retains the idempotent command");
await modeElements["settings-timeline-toggle"].fire("click");
assert.equal(commandCalls.at(-1).requestId, uncertainRequest,
    "retry after response loss reuses the same request identity");

const pageElements = timelineElements();
const pages = bindTimelinePage(pageElements, {
    read: async (_project, _entry, cursor) => cursor
        ? snapshot(1, true, [timelineRow("second", 100)], null)
        : snapshot(1, true, [timelineRow("first", 200)], "1"),
    command: async () => snapshot(1, true), openBoard() {}, onMode() {},
});
await pages.enter("project-one", { label: "Timeline Project" });
const more = descendants(pageElements["timeline-items"]).find(node => node.className === "timeline-more");
assert.ok(more, "first page exposes Load more");
await more.fire("click");
const visibleEntries = descendants(pageElements["timeline-items"])
    .map(node => node.dataset.timelineEntry).filter(Boolean);
assert.deepEqual(visibleEntries, ["first", "second"], "Load more retains both ordered pages");

// Exercise the real shared navigation adapter, not just CloudClient's optional argument.
const mainSource = readFileSync(new URL("../Resources/web/app/js/main.js", import.meta.url), "utf8");
const adapterSource = mainSource.split("module.bindTimelinePage(timelineElements, ")[1]?.split("\n    });")[0];
assert.ok(adapterSource, "shared Timeline adapter has a bounded extraction seam");
const adapterCalls = [];
const adapter = new Function("api", "BoardControls", "timelineRequested", "return (" + adapterSource + "});")(
    { timeline: (...args) => adapterCalls.push(["read", ...args]),
      timelineCommand: (...args) => adapterCalls.push(["command", ...args]) },
    { open: (...args) => adapterCalls.push(["board", ...args]), apply() {} },
    { machine: "mac-b" });
adapter.read("project-one", null, null, "production", null, false);
adapter.command({ operation: "set_enabled" });
adapter.openBoard("project-one", "item-one");
assert.equal(adapterCalls[0].at(-1), "mac-b", "Board to Timeline preserves the chosen Mac for reads");
assert.equal(adapterCalls[1].at(-1), "mac-b", "Timeline mode commands target the chosen Mac");
assert.equal(adapterCalls[2].at(-1), "mac-b", "Timeline to Board preserves the chosen Mac");
{
    const nodes = timelineElements(), requests = [];
    let writes = 0;
    const history = timelineRow("Git only", 200); history.projection.status = "landed_to_git";
    const page = bindTimelinePage(nodes, {
        read: async (...args) => { requests.push(args); return snapshot(1, true, args[5] ? [history] : []); },
        command: async () => { writes++; }, openBoard() {}
    });
    page.state.environment = "staging"; page.state.category = "feature";
    await page.enter("project-one");
    const action = descendants(nodes["timeline-items"]).find(node => node.dataset.timelineAction === "show-git-history");
    assert.ok(action, "empty availability view offers opt-in Git history instead of a dead end");
    assert.equal(requests.length, 1, "empty view does not load or rebuild raw history automatically");
    await action.fire("click");
    assert.deepEqual(requests.at(-1), ["project-one", null, null, "staging", "feature", true], "history action preserves Project/environment/category");
    assert.equal(nodes["timeline-upcoming"].checked, true, "checkbox reflects the chosen history filter");
    assert.ok(descendants(nodes["timeline-items"]).some(node => node.textContent === "已進 Git"), "Git history never becomes available evidence");
    assert.equal(writes, 0, "showing history performs no ingestion or lifecycle write");
    const off = timelineElements();
    const disabled = bindTimelinePage(off, { read: async () => snapshot(1, false), command() {}, openBoard() {} });
    await disabled.enter("project-one");
    assert.ok(!descendants(off["timeline-items"]).some(node => node.dataset.timelineAction === "show-git-history"), "disabled Timeline does not offer an active history action");
}
console.log("web timeline correction checks passed");
