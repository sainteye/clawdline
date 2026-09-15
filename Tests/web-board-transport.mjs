import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const noop = () => {};
const element = new Proxy(function () {}, {
    get(_target, key) {
        if (key === Symbol.iterator) return function* () {};
        if (key === "classList") return { add: noop, remove: noop, toggle: noop, contains: () => false };
        if (key === "style" || key === "dataset") return { setProperty: noop, removeProperty: noop };
        if (key === "children" || key === "querySelectorAll") return [];
        if (key === "content") return { cloneNode: () => element };
        return element;
    }, apply: () => element
});
globalThis.localStorage = { getItem: () => null, setItem: noop };
globalThis.location = { search: "?mock=1", protocol: "http:", hostname: "localhost", pathname: "/", href: "http://localhost/" };
globalThis.history = { replaceState: noop, pushState: noop };
Object.defineProperty(globalThis, "navigator", { value: { userAgent: "node", maxTouchPoints: 0 }, configurable: true });
globalThis.window = element;
window.devicePixelRatio = 1; window.matchMedia = () => ({ matches: false, addEventListener: noop });
globalThis.document = element;
document.documentElement = { lang: "en", style: { setProperty: noop, removeProperty: noop } };
document.getElementById = () => element; document.querySelector = () => element;
document.querySelectorAll = () => []; document.createElement = () => element; document.body = element;
globalThis.MutationObserver = class { observe() {} disconnect() {} };
globalThis.ResizeObserver = MutationObserver; globalThis.IntersectionObserver = MutationObserver;
const calls = [];
globalThis.fetch = (path, options) => {
    calls.push({ path, options });
    return Promise.resolve({ ok: true, text: () => Promise.resolve('{"board":{"revision":3,"enabled":true}}') });
};
const { Live } = await import("../Resources/web/app/js/net/live.js");
const body = { operation: "set_enabled", enabled: false, expectedRevision: 2, requestId: "same-retry" };
let checks = 0;
function equal(got, expected, why) { assert.deepEqual(got, expected, why); checks++; }
equal(/id="settings-board-history"[^>]*data-page-to="projects"/.test(readFileSync(new URL("../Resources/web/index.html", import.meta.url), "utf8")),
    true, "Settings uses the same Project catalog rather than another global Board menu");
const answer = await Live.boardCommand(body);
equal(calls.length, 1, "a command really sends a request rather than returning fetch options");
equal(calls[0].path, "/v1/board", "local command route");
equal(calls[0].options.method, "POST", "command is POST");
equal(JSON.parse(calls[0].options.body), body, "CAS and idempotency body survives");
equal(answer.board.revision, 3, "server response is delivered");
await Live.board("p&one", "item/two");
equal(calls[1].path, "/v1/board?project=p%26one&item=item%2Ftwo", "opaque identities stay query values");
await Live.board();
equal(calls[2].path, "/v1/board", "optional selectors omitted");

const { CloudClient } = await import("../Resources/web/app/js/net/cloud-client.js");
const cloudCalls = [];
const context = { _accountMachine: () => "owning-machine",
    _machineRequest: (...args) => { cloudCalls.push(args); return Promise.resolve({ board: { revision: 3 } }); } };
const owned = await CloudClient.prototype.board.call(context, "p", "i");
equal(owned.machine, "owning-machine", "a Board answer names the machine it came from, for what the page opens next");
equal(cloudCalls[0], ["owning-machine", "board", { project: "p", item: "i" }, "read"], "Cloud closed read names its owner");
await CloudClient.prototype.board.call(context, "p", "i", "r");
equal(cloudCalls[1], ["owning-machine", "board", { project: "p", item: "report:i:r" }, "read"],
    "Cloud report retrieval preserves item and opaque report selectors in its bounded read");
await CloudClient.prototype.boardCommand.call(context, body);
equal(cloudCalls[2], ["owning-machine", "board-command", { command: body }, "action"], "Cloud command preserves same command identity");
context._accountMachine = () => { throw Object.assign(new Error("ambiguous"), { code: "ambiguous_machine" }); };
await assert.rejects(CloudClient.prototype.board.call(context), { code: "ambiguous_machine" }); checks++;
equal(cloudCalls.length, 3, "ambiguous machine never dispatches a request");

const { createBoardMock } = await import("../Resources/web/app/js/net/board-mock.js");
const mock = createBoardMock();
const initial = await mock.board();
equal(initial.board.enabled, true, "preview follows default ON");
equal(initial.board.items.length, 0, "catalog read never carries nested work items");
const initialWork = (await mock.board("preview-project")).board.items;
equal(initialWork.length, 5, "a selected Project independently loads its five records");
const html = readFileSync(new URL("../Resources/web/index.html", import.meta.url), "utf8");
for (const id of ["nav-board", "usage-open", "nav-ledger"]) {
    equal(new RegExp('<button[^>]*id="' + id + '"[^>]*\\bhidden\\b').test(html), true,
        id + " stays hidden before a mode response, including disconnected cold startup");
}
equal(html.includes('id="sidebar-board-projects"'), false, "sidebar has one Projects entry, not a raw project directory");
const command = { operation: "set_enabled", enabled: false, expectedRevision: initial.board.revision, requestId: "off" };
const off = await mock.boardCommand(command);
equal(off.board.enabled, false, "preview mode can be disabled");
equal((await mock.boardCommand(command)).board.revision, off.board.revision, "retry does not mutate again");
equal((await mock.board("preview-project")).board.items.length, initialWork.length, "off preserves history");
const css = readFileSync(new URL("../Resources/web/app/css/pages.css", import.meta.url), "utf8");
assert.match(css, /\.app\[hidden\]\s*\{\s*display:\s*none/); checks++;
assert.match(css, /\.sidebar \[hidden\]\s*\{\s*display:\s*none/); checks++;

// Use the real settings controller and transport live binding, not copied source.
const { useClient } = await import("../Resources/web/app/js/net/api.js");
const { BoardControls } = await import("../Resources/web/app/js/input/board-settings.js");
const nodes = new Map();
function control(id) {
    if (!nodes.has(id)) nodes.set(id, { hidden: false, disabled: false, textContent: "", listeners: {}, children: [],
        classList: { toggle() {} }, setAttribute() {}, replaceChildren() { this.children = []; },
        appendChild(child) { this.children.push(child); }, addEventListener(event, action) { this.listeners[event] = action; } });
    return nodes.get(id);
}
globalThis.document = { documentElement: { lang: "en", dataset: {} }, getElementById: control, createElement: () => control(Symbol()) };
const manager = { id: "admin", canWrite: true, canManage: true };
let settingsBoard = { enabled: true, revision: 1, projects: [], viewer: manager };
const settingCalls = [];
let outcome = "offline";
useClient({ board: async () => ({ board: settingsBoard }), boardCommand: body => {
    settingCalls.push(structuredClone(body));
    if (outcome === "sync") throw Object.assign(new Error("ambiguous machine"), { code: "cloud_machine_ambiguous" });
    if (outcome === "offline") return Promise.reject(Object.assign(new Error("offline"), { code: "offline" }));
    settingsBoard = { ...settingsBoard, revision: body.expectedRevision + 1,
        ...(body.operation === "set_ai_consent" ? { narrativeConsent: body.enabled ? body.provider : null } : { enabled: body.enabled }) };
    return Promise.resolve({ board: settingsBoard });
} });
const settled = async () => { for (let i = 0; i < 4; i++) await new Promise(resolve => setImmediate(resolve)); };
await BoardControls.refresh();
equal(control("nav-projects").hidden, false, "Projects remains the primary entry in board mode");
equal(control("nav-board").hidden, true, "no competing global Board navigation");
equal(control("usage-open").hidden, true, "standalone usage is folded into Project work");
control("settings-board-toggle").listeners.click();
await settled();
equal(control("settings-board-toggle").disabled, false, "ambiguous failure exposes retry");
await BoardControls.refresh();
equal(control("settings-board-toggle").disabled, false, "refresh cannot lock out retryable pending request");
outcome = "success";
control("settings-board-toggle").listeners.click();
await settled();
equal(settingCalls[1], settingCalls[0], "ambiguous retry preserves exact CAS body and request identity");
equal(control("nav-projects").hidden, false, "successful off restores old navigation");
equal(control("usage-open").hidden, false, "off restores Usage");
equal(control("nav-ledger").hidden, false, "off restores Ledger");
equal(control("sidebar-board-projects").children.length, 0, "off never creates sidebar project shortcuts");
BoardControls.apply({ ...settingsBoard, enabled: true, revision: 1 });
equal(control("nav-projects").hidden, false, "older read cannot roll mode back");
BoardControls.apply({ ...settingsBoard, revision: 3, viewer: { id: "writer", canWrite: true, canManage: false } });
equal(control("settings-board-toggle").disabled, true, "writer cannot change administrative mode");
control("settings-board-toggle").listeners.click();
await settled();
equal(settingCalls.length, 2, "controller also guards unauthorized programmatic event");
BoardControls.apply({ ...settingsBoard, revision: 4 });
outcome = "sync";
assert.doesNotThrow(() => control("settings-board-toggle").listeners.click(), "synchronous transport refusal is handled"); checks++;
await settled();
equal(control("settings-board-toggle").disabled, false, "synchronous refusal never leaves saving latched");
assert.match(control("settings-board-status").textContent, /ambiguous/); checks++;
settingsBoard = { ...settingsBoard, enabled: false, revision: 5 };
BoardControls.apply(settingsBoard);
outcome = "success";
control("settings-board-toggle").listeners.click();
await settled();
equal(settingCalls.at(-1).enabled, true, "the same setting can turn Board back on");
equal(document.documentElement.dataset.boardMode, "board", "successful re-enable restores Board mode");
equal(control("usage-open").hidden, true, "re-enable folds Usage back into work items");
equal(control("nav-ledger").hidden, true, "re-enable folds Ledger back into work items");
equal(control("sidebar-board-projects").children.length, 0, "re-enable never recreates a sidebar project directory");
settingsBoard = { ...settingsBoard, revision: 20, enabled: false, narrativeConsent: null,
    viewer: { ...manager, narrativeProvider: "codex" } };
BoardControls.apply(settingsBoard);
equal(control("settings-board-ai-toggle").textContent.includes("OpenAI"), true, "consent names the external destination before opt-in");
control("settings-board-ai-toggle").listeners.click();
await settled();
equal(settingCalls.at(-1).operation, "set_ai_consent", "AI sharing is not a Board-mode command");
equal(settingCalls.at(-1).policy, "board-reading-v1", "consent names its data-purpose policy");
equal(settingsBoard.enabled, false, "AI consent cannot enable Board workflow");
equal(settingsBoard.narrativeConsent, "codex", "successful consent is provider scoped");
control("settings-board-ai-toggle").listeners.click();
await settled();
equal(settingsBoard.narrativeConsent, null, "the same visible control revokes AI sharing");
const backOn = await mock.boardCommand({ operation: "set_enabled", enabled: true,
    expectedRevision: off.board.revision, requestId: "on-again" });
equal(backOn.board.enabled, true, "mock lifecycle also supports off then on");
equal((await mock.board("preview-project")).board.items.length, initialWork.length, "off then on retains the same history");
let createFailure = null;
try {
    await mock.boardCommand({ operation: "create", projectId: "preview-project", title: "Conversation-created task",
        type: "task", expectedRevision: backOn.board.revision, requestId: "create-from-conversation" });
} catch (error) { createFailure = error; }
equal(createFailure, null, "conversation creation updates materialized mock counts without requiring a progress projection");
const consentMock = createBoardMock();
const previewConsent = await consentMock.board();
equal(previewConsent.board.narrativeConsent, null, "preview also starts without AI permission");
const previewAllowed = await consentMock.boardCommand({ operation: "set_ai_consent", enabled: true,
    provider: "codex", policy: "board-reading-v1", expectedRevision: previewConsent.board.revision, requestId: "preview-consent" });
equal(previewAllowed.board.narrativeConsent, "codex", "preview can demonstrate provider-specific consent without external calls");
console.log(`${checks} web board transport checks passed`);
process.exit(0); // imported preview transport owns timers unrelated to this bounded question
