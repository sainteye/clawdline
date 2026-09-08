import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const noop = function () {};
const fixtureElement = new Proxy(function () {}, {
    get: function (_target, key) {
        if (key === Symbol.iterator) return function* () {};
        if (key === "classList") return { add: noop, remove: noop, toggle: noop,
            contains: function () { return false; } };
        if (key === "style" || key === "dataset") return { setProperty: noop, removeProperty: noop };
        if (key === "children" || key === "querySelectorAll") return [];
        if (key === "content") return { cloneNode: function () { return fixtureElement; } };
        return fixtureElement;
    },
    apply: function () { return fixtureElement; }
});
globalThis.document = fixtureElement;
document.documentElement = { lang: "en", style: { setProperty: noop, removeProperty: noop } };
document.getElementById = function () { return fixtureElement; };
document.querySelector = function () { return fixtureElement; };
document.querySelectorAll = function () { return []; };
document.createElement = function () { return fixtureElement; };
document.body = fixtureElement;
globalThis.location = {
    search: "?mock=1", protocol: "https:", hostname: "app.clawdline.com",
    origin: "https://app.clawdline.com"
};
globalThis.window = fixtureElement;
window.devicePixelRatio = 1;
window.matchMedia = function () { return { matches: false, addEventListener: noop }; };
globalThis.requestAnimationFrame = (fn) => fn();
Object.defineProperty(globalThis, "navigator", {
    value: { userAgent: "node", maxTouchPoints: 0 }, configurable: true, writable: true
});
globalThis.localStorage = { getItem: function () { return null; }, setItem: noop };
globalThis.history = { replaceState: noop, pushState: noop };
globalThis.MutationObserver = class { observe() {} disconnect() {} };
globalThis.ResizeObserver = MutationObserver;
globalThis.IntersectionObserver = MutationObserver;

const {
    CANONICAL_DOCUMENT_ORIGIN,
    documentAnswer,
    documentBytesAnswer,
    documentHash,
    documentIdentityForSession,
    documentListing,
    documentLocatorFromHash,
    documentShareURL,
    localDocumentListing,
    normalizeDocumentIdentity,
    normalizeDocumentLocator,
    shareDocument
} = await import("../Resources/web/app/js/net/document-links.js");
const { documentBodyHTML } = await import(
    "../Resources/web/app/js/view/document-render.js"
);
const { bindDocumentsPage } = await import(
    "../Resources/web/app/js/view/documents.js"
);
const { CloudClient } = await import("../Resources/web/app/js/net/cloud-client.js");

const task = {
    machine: "Mac / 台灣",
    session: "session-1",
    scope: "task",
    task: "9e475a3b-a6dc-437a-8630-2fc78375e76d",
    path: "reviews/feature-review.md"
};
const project = {
    machine: "Mac / 台灣",
    session: "session-1",
    scope: "project",
    path: "notes/design.markdown"
};

assert.deepEqual(normalizeDocumentLocator(task), task,
    "a task document retains only its explicit fleet and root identity");
assert.deepEqual(normalizeDocumentLocator(project), project,
    "a project document has no invented task identity");
assert.deepEqual(normalizeDocumentIdentity({ machine: task.machine, session: task.session }),
    { machine: task.machine, session: task.session });
assert.throws(() => normalizeDocumentIdentity({ session: task.session }), /explicit machine/,
    "a document list never resolves an ambiguous bare session");
assert.deepEqual(documentIdentityForSession([
    { id: task.session, identity: { machine: task.machine, session: task.session } }
], task.session, "cloud"), { machine: task.machine, session: task.session },
"the Session action preserves the Cloud row's explicit fleet identity");
assert.throws(() => documentIdentityForSession([
    { id: task.session, identity: { machine: "mac-a", session: task.session } },
    { id: task.session, identity: { machine: "mac-b", session: task.session } }
], task.session, "cloud"), (error) => error && error.code === "cloud_session_ambiguous",
"a duplicate bare session id fails closed instead of choosing the first Mac");
assert.deepEqual(documentIdentityForSession([{ id: "local-session" }], "local-session", "local"),
    { machine: "this-mac", session: "local-session" },
    "the local transport's identity remains explicit without pretending to be Cloud");

const hash = documentHash(task);
const shared = new URL(documentShareURL("https://app.clawdline.com/old?token=no", task));
assert.equal(shared.origin, "https://app.clawdline.com");
assert.equal(shared.pathname, "/", "the origin receives only the PWA shell path");
assert.equal(shared.search, "", "no query, token or credential is shared");
assert.equal(shared.href.split("#")[0], "https://app.clawdline.com/",
    "the origin request is only the PWA shell; the locator begins after the fragment boundary");
assert.equal(shared.hash, hash, "all locator fields stay after the fragment boundary");
assert.equal(CANONICAL_DOCUMENT_ORIGIN, "https://app.clawdline.com",
    "there is one canonical paired-phone document origin");
assert.deepEqual(documentLocatorFromHash(hash), task,
    "the strict fragment round-trips the named task document");
assert.equal(/token|secret|authorization|bearer|\/Users\//i.test(hash), false,
    "the shared locator carries no credential or filesystem path");

for (const [bad, why] of [
    ["#document=1&session=s&scope=project&path=a.md", "a bare session cannot choose a Mac"],
    [hash + "&token=secret", "an extra credential-like field is refused"],
    ["#document=1&machine=m&session=s&scope=project&task=t&path=a.md", "project scope has no task"],
    ["#document=1&machine=m&session=s&scope=task&task=not-a-task&path=a.md", "task scope needs a task id"],
    ["#document=1&machine=m&session=s&scope=project&path=../a.md", "dot paths are refused"],
    ["#document=1&machine=m&session=s&scope=project&path=a.html", "only inert text extensions are accepted"],
    ["#document=1&machine=m&session=s&scope=project&path=a/b/c/d/e/f/g.md", "path depth stays bounded"],
]) {
    assert.equal(documentLocatorFromHash(bad), null, why);
}
for (const [origin, locator, why] of [
    ["http://localhost:7717", Object.assign({}, task, { machine: "mac-cloud" }), "localhost is not a paired-phone link"],
    ["http://192.168.1.8:7717", Object.assign({}, task, { machine: "mac-cloud" }), "LAN HTTP is not a paired-phone link"],
    ["https://my-mac.example.net", Object.assign({}, task, { machine: "mac-cloud" }), "a named tunnel is not the canonical Cloud app"],
    [CANONICAL_DOCUMENT_ORIGIN, Object.assign({}, task, { machine: "this-mac" }), "a local machine alias is not a Cloud identity"],
]) {
    assert.throws(() => documentShareURL(origin, locator),
        (error) => error && error.code === "document_share_unavailable", why);
}

const listing = documentListing({ documents: [{
    scope: "task", path: "feature-review.md", bytes: 3258, modified: 1788800000,
    task: task.task, title: "Cloud Markdown"
}] }, { machine: task.machine, session: task.session });
assert.deepEqual(listing.documents[0], {
    machine: task.machine, session: task.session, scope: "task", task: task.task,
    path: "feature-review.md", bytes: 3258, modified: 1788800000,
    title: "Cloud Markdown"
}, "a listing becomes a shareable locator without carrying its local route");
assert.throws(() => documentListing({ documents: [{
    scope: "project", path: "a.md", bytes: 1, modified: 0,
    url: "/v1/sessions/private/documents/project/a.md"
}] }, { machine: "m", session: "s" }), /metadata/,
"a local route cannot cross as Cloud listing metadata");
const localListing = localDocumentListing({ documents: [{
    source: "task", path: "feature-review.md", label: "feature-review.md",
    bytes: 3258, modified: 1788800000,
    url: "/v1/sessions/private/documents/task/" + task.task + "/feature-review.md",
    task: { id: task.task, title: "Cloud Markdown" }
}] }, { machine: "this-mac", session: task.session });
assert.equal(localListing.documents[0].machine, "this-mac");
assert.equal(Object.hasOwn(localListing.documents[0], "url"), false,
    "the legacy local response is normalized before the shared view sees it");

const utf8 = new TextEncoder().encode("# 安全\n<script>alert(1)</script>");
const encoded = Buffer.from(utf8).toString("base64");
const answer = documentAnswer(task, {
    scope: "task", task: task.task, path: task.path,
    media_type: "text/markdown; charset=utf-8", byte_count: utf8.length, data: encoded
});
assert.equal(answer.text, "# 安全\n<script>alert(1)</script>");
assert.match(documentBodyHTML(answer), /&lt;script&gt;alert\(1\)&lt;\/script&gt;/,
    "document bytes use the existing escaped Markdown renderer");
assert.doesNotMatch(documentBodyHTML(answer), /<script>/,
    "document text cannot become executable markup");
assert.equal(documentBytesAnswer(project, "text/plain; charset=utf-8",
    new TextEncoder().encode("plain")).text, "plain");
for (const [body, why] of [
    [{ scope: "task", task: task.task, path: task.path, media_type: "text/html", byte_count: utf8.length, data: encoded }, "HTML is refused"],
    [{ scope: "task", task: task.task, path: task.path, media_type: "text/markdown; charset=utf-8", byte_count: utf8.length + 1, data: encoded }, "a false byte count is refused"],
    [{ scope: "task", task: task.task, path: task.path, media_type: "text/markdown; charset=utf-8", byte_count: 1, data: "%%%=" }, "non-base64 is refused"],
    [{ scope: "project", path: task.path, media_type: "text/markdown; charset=utf-8", byte_count: utf8.length, data: encoded }, "another scope cannot answer"],
]) assert.throws(() => documentAnswer(task, body), /document answer/, why);

const client = new CloudClient({
    relayURL: "wss://relay.example/v1/connect", deviceToken: "viewer",
    allowWrites: true, nextSequence: () => 1
});
const calls = [];
client._read = function (identity, type, extra, name) {
    calls.push({ identity, type, extra, name });
    if (type === "documents") return Promise.resolve({ documents: [] });
    return Promise.resolve({ scope: task.scope, task: task.task, path: task.path,
        media_type: "text/markdown; charset=utf-8", byte_count: utf8.length, data: encoded });
};
await client.documents({ machine: task.machine, session: task.session });
const readDocument = await client.document(task);
assert.equal(readDocument.text.startsWith("# 安全"), true);
assert.deepEqual(calls[0], {
    identity: { machine: task.machine, session: task.session }, type: "documents", extra: {},
    name: "documents"
}, "the listing is an encrypted session read with explicit fleet identity");
assert.equal(calls[1].identity.machine, task.machine);
assert.equal(calls[1].identity.session, task.session);
assert.equal(calls[1].type, "document");
assert.equal(calls[1].extra.scope, "task");
assert.equal(calls[1].extra.task, task.task);
assert.equal(calls[1].extra.path, task.path);
assert.match(calls[1].name, /^read:/, "concurrent document answers use a request id, not a path");

function control() {
    const node = {
        innerHTML: "", hidden: false, disabled: false, dataset: {}, children: [], title: "",
        addEventListener(type, listener) { this[type] = listener; },
        replaceChildren() { this.children = []; },
        appendChild(child) { this.children.push(child); },
        append(...children) { this.children.push(...children); }
    };
    let text = "";
    Object.defineProperty(node, "textContent", {
        get() { return text; },
        set(value) { text = String(value); node.innerHTML = ""; }
    });
    return node;
}
function documentControls() {
    return {
        title: control(), back: control(), listBack: control(), status: control(),
        listView: control(), rows: control(), viewer: control(), documentTitle: control(),
        meta: control(), body: control(), share: control(), copy: control(), menu: control()
    };
}
const documentElements = documentControls();
const scheduled = [];
let coldReader = () => {
    const error = new Error("not connected"); error.code = "cloud_starting";
    return Promise.reject(error);
};
let documentsPage;
documentsPage = bindDocumentsPage(documentElements, {
    document: { createElement: control }, language: () => "en",
    list: () => Promise.resolve({ documents: [] }), read: (locator) => coldReader(locator),
    shareOrigin: () => CANONICAL_DOCUMENT_ORIGIN,
    navigator: () => ({ clipboard: { writeText() { return Promise.resolve(); } } }),
    schedule: (fn) => { scheduled.push(fn); return scheduled.length; }, cancel: () => {},
    navigate: (name) => { if (name === "documents") documentsPage.enter(); }
});
documentsPage.openDirect(task);
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(documentsPage.state().held, true,
    "the real document page holds a cold direct fragment while the relay client boots");
assert.match(documentElements.status.textContent, /encrypted connection/);
coldReader = () => Promise.resolve(answer);
scheduled.shift()();
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(documentsPage.state().held, false,
    "the page retries its held locator after a connected transport becomes available");
assert.equal(documentElements.viewer.hidden, false);
assert.doesNotMatch(documentElements.body.innerHTML, /<script>/,
    "the cold-start path ends in the same safe renderer as an ordinary open");
assert.equal(documentsPage.state().hasAnswer, true,
    "the lifecycle witness sees decrypted plaintext while the document is open");
documentsPage.leave();
assert.deepEqual(documentsPage.state(), {
    active: false, held: false, identity: null, locator: null,
    hasAnswer: false, displayTitle: ""
}, "leaving clears the decrypted answer, locator, identity, title and retry state");
assert.equal(documentElements.body.innerHTML, "", "leaving removes plaintext from the document DOM");
assert.equal(documentElements.meta.textContent, "", "leaving clears document metadata");
assert.equal(documentElements.documentTitle.textContent, "", "leaving clears the document title");
assert.equal(documentElements.share.disabled && documentElements.copy.disabled, true,
    "leaving disables both link controls");
documentsPage.openDirect(null, "Invalid document link.");
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(documentsPage.state().identity, null,
    "a malformed direct link cannot inherit the previously opened Session identity");
assert.match(documentElements.status.textContent, /^malformed_document_locator:/,
    "an old Session list cannot overwrite the direct-link refusal after navigation");

let resolvePending;
const pendingElements = documentControls();
let pendingPage;
pendingPage = bindDocumentsPage(pendingElements, {
    document: { createElement: control }, language: () => "en",
    list: () => Promise.resolve({ documents: [] }),
    read: () => new Promise((resolve) => { resolvePending = resolve; }),
    shareOrigin: () => CANONICAL_DOCUMENT_ORIGIN, navigator: () => ({}),
    navigate: (name) => { if (name === "documents") pendingPage.enter(); }
});
pendingPage.openDirect(task);
await Promise.resolve();
pendingPage.hide();
resolvePending(answer);
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(pendingPage.state().hasAnswer, false,
    "hiding cancels a pending read so its later plaintext answer cannot be retained");
assert.equal(pendingElements.body.innerHTML, "",
    "a cancelled pending read cannot repopulate the hidden DOM");

const syncElements = documentControls();
let syncPage;
syncPage = bindDocumentsPage(syncElements, {
    document: { createElement: control }, language: () => "en",
    list: () => { throw Object.assign(new Error("fixture exploded"), { code: "mock_document_failure" }); },
    read: () => { throw Object.assign(new Error("transport exploded"), { code: "document_transport_failure" }); },
    shareOrigin: () => CANONICAL_DOCUMENT_ORIGIN, navigator: () => ({}),
    navigate: (name) => { if (name === "documents") syncPage.enter(); }
});
assert.equal(syncPage.openSession({ machine: "this-mac", session: "sync" }), true,
    "a synchronously throwing listing transport is admitted to the page's Promise boundary");
await new Promise((resolve) => setTimeout(resolve, 0));
assert.match(syncElements.status.textContent, /^mock_document_failure:/,
    "a synchronous listing failure becomes a visible typed error");
syncPage.openDirect(Object.assign({}, project, { session: "sync" }));
await new Promise((resolve) => setTimeout(resolve, 0));
assert.match(syncElements.status.textContent, /^document_transport_failure:/,
    "a synchronous document failure becomes a visible typed error");

const { Mock } = await import("../Resources/web/app/js/net/mock.js");
const { params } = await import("../Resources/web/app/js/core/env.js");
params.delete("documents");
const mockIdentity = { machine: "this-mac", session: "8F3A-1C" };
const mockListing = await Mock.documents(mockIdentity);
assert.equal(mockListing.documents.length > 0 && mockListing.documents.length <= 3, true,
    "the production Mock transport exposes a bounded document listing");
assert.equal(mockListing.documents.some((row) => Object.hasOwn(row, "url")), false,
    "Mock listing metadata carries no local route");
const mockRow = mockListing.documents[0];
const mockLocator = { machine: mockRow.machine, session: mockRow.session,
    scope: mockRow.scope, path: mockRow.path };
if (mockRow.scope === "task") mockLocator.task = mockRow.task;
const mockAnswer = await Mock.document(mockLocator);
assert.equal(mockAnswer.machine, "this-mac");
assert.match(mockAnswer.text, /encrypted Cloud document fixture/i,
    "the production Mock transport exposes bounded document bytes");
params.set("documents", "empty");
assert.deepEqual(await Mock.documents(mockIdentity), { documents: [] },
    "the empty visual fixture is deliberate and deterministic");
params.set("documents", "error");
await assert.rejects(Mock.documents(mockIdentity),
    (error) => error && error.code === "mock_document_failure",
    "the error visual fixture is typed");
params.delete("documents");

const mockElements = documentControls();
let mockPage;
mockPage = bindDocumentsPage(mockElements, {
    document: { createElement: control }, language: () => "en",
    list: (identity) => Mock.documents(identity), read: (locator) => Mock.document(locator),
    shareOrigin: () => null, navigator: () => ({}),
    navigate: (name) => { if (name === "documents") mockPage.enter(); }
});
mockPage.openSession(mockIdentity);
await new Promise((resolve) => setTimeout(resolve, 30));
mockElements.rows.children[0].children[0].click();
await new Promise((resolve) => setTimeout(resolve, 30));
assert.match(mockElements.body.innerHTML, /encrypted Cloud document fixture/i,
    "the production controller and Mock transport compose into a readable document page");
assert.equal(mockElements.share.disabled && mockElements.copy.disabled, true,
    "the local Mock composition does not promise a cross-device Cloud link");

let changingOrigin = CANONICAL_DOCUMENT_ORIGIN;
const shareFailureElements = documentControls();
let shareFailurePage;
shareFailurePage = bindDocumentsPage(shareFailureElements, {
    document: { createElement: control }, language: () => "en",
    list: () => Promise.resolve({ documents: [] }), read: () => Promise.resolve(answer),
    shareOrigin: () => changingOrigin,
    navigator: () => ({ clipboard: { writeText: () => Promise.resolve() } }),
    navigate: (name) => { if (name === "documents") shareFailurePage.enter(); }
});
shareFailurePage.openDirect(task);
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(shareFailureElements.copy.disabled, false,
    "a Cloud document with a real fleet identity enables canonical sharing");
changingOrigin = "http://192.168.1.8:7717";
shareFailureElements.copy.click();
await new Promise((resolve) => setTimeout(resolve, 0));
assert.match(shareFailureElements.status.textContent, /^document_share_unavailable:/,
    "a synchronous Copy URL-construction refusal is caught and painted");
shareFailureElements.status.textContent = "";
shareFailureElements.share.click();
await new Promise((resolve) => setTimeout(resolve, 0));
assert.match(shareFailureElements.status.textContent, /^document_share_unavailable:/,
    "a Share URL-construction refusal is caught and painted by the same boundary");

const mainSource = readFileSync(new URL("../Resources/web/app/js/main.js", import.meta.url), "utf8");
const sessionDocumentAction = /byId\("session-documents"\)\.addEventListener[\s\S]*?\n\}\);/.exec(mainSource)?.[0] || "";
assert.match(sessionDocumentAction, /documentIdentityForSession\(S\.sessions, S\.openId, transportKind\)/,
    "production Session wiring resolves the complete identity through the fail-closed helper");
assert.doesNotMatch(sessionDocumentAction, /S\.sessions\.find/,
    "production Session wiring never picks the first duplicate bare id");
assert.match(mainSource, /shareOrigin:[\s\S]*?CANONICAL_DOCUMENT_ORIGIN/,
    "production sharing is wired to the canonical Cloud origin rather than location.origin");
assert.match(mainSource, /function \(\) \{ documents\.hide\(\); \}/,
    "the non-document fragment route explicitly clears the document controller");

let sharedURL = "";
assert.equal(await shareDocument(task, "Cloud review", {
    origin: "https://app.clawdline.com", navigator: {
        share: ({ url }) => { sharedURL = url; return Promise.resolve(); }
    }
}), "shared", "a user gesture uses the mobile share sheet");
assert.equal(sharedURL, documentShareURL("https://app.clawdline.com", task));
let copied = "";
assert.equal(await shareDocument(task, "Cloud review", {
    origin: "https://app.clawdline.com", navigator: {
        share: () => Promise.reject(new Error("cancelled")),
        clipboard: { writeText: (value) => { copied = value; return Promise.resolve(); } }
    }
}), "copied", "a refused share sheet falls back to copying the direct URL");
assert.equal(copied, sharedURL);

console.log("web Cloud documents: 85 assertions passed");
process.exit(0);
