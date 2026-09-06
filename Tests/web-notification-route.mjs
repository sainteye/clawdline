#!/usr/bin/env node

// A tapped notification, from the worker that wakes up to the transcript that opens — **joined**.
//
// **Why this file exists.** The road has six segments and five of them had been checked: the URL
// the Mac writes (`HookTests.swift`), the fragment the page reads and the list it looks the id up
// in (`Tests/web-pages.mjs`), the payload the worker draws a notification from and the branch it
// takes when one is tapped (`Tests/web-service-worker.mjs`). The sixth — the worker's message
// actually arriving at the page and turning into a route — had no test at any effort level,
// because the two halves live in two files with a process boundary between them: the worker is a
// response body inside a Swift raw string, and the page is an ES module whose harness in
// `web-pages.mjs` sets `navigator: {}` and therefore never installs the listener that receives it.
//
// **And the fixtures hid it.** `web-service-worker.mjs` taps notifications carrying `/#session-9`,
// `/#cold` and `/#fresh` — not one of them is the `#session=<id>` shape a notification is actually
// written with, so nothing in that file has ever asked whether the URL it forwards can route. Every
// id here is a tmux pane instead (`%208`, `%141`), which is what this Mac's sessions are called and
// the only id shape this family of faults has ever appeared on; the iTerm id beside them is the
// control group, and it has to pass for the same reason a control group has to.
//
// **What it can and cannot say.** It drives both contexts in this process against stand-in
// browser globals, so it proves the two halves agree and that the trace can tell the roads apart.
// It cannot say what iOS does with a `postMessage` to a suspended home-screen web app: that is the
// measurement this diagnostic exists to let somebody make on the phone.

import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createContext, runInContext } from "node:vm";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");
const pagePath = resolve(process.env.CLAWDLINE_REMOTE_PAGE_SOURCE
  || join(repoRoot, "Sources", "RemotePage.swift"));
const routePath = resolve(process.env.CLAWDLINE_ROUTE_SOURCE
  || join(repoRoot, "Resources", "web", "app", "js", "input", "route.js"));

let checks = 0;
let failures = 0;
const check = (what, ok) => {
  checks += 1;
  if (!ok) failures += 1;
  console.log(`  ${ok ? "✓" : "✗"} ${what}`);
};
const equal = (got, want, what) =>
  check(`${what} (${JSON.stringify(got)})`, Object.is(got, want) || got === want);
const stop = (why) => {
  console.log(`  ✗ ${why}`);
  console.log(`notification route: stopped after ${checks} checks — ${why}`);
  process.exit(1);
};
const occurrences = (haystack, needle) => haystack.split(needle).length - 1;

const pageSource = readFileSync(pagePath, "utf8");
const routeSource = readFileSync(routePath, "utf8");

/* ---- lift the worker out of the Swift raw string ----------------------------------------------
   The same extraction `web-service-worker.mjs` does, and checked before it is trusted for the same
   reason: a slice that silently becomes empty is how a source-reading guard turns into a check
   that cannot fail. */
const SIGNATURE = "static func serviceWorker() -> RemoteServer.Response {";
check("RemotePage declares the worker exactly once", occurrences(pageSource, SIGNATURE) === 1);
if (occurrences(pageSource, SIGNATURE) !== 1) stop("cannot find the handler this suite is about");
const from = pageSource.indexOf(SIGNATURE);
const after = pageSource.indexOf("\n    static func", from + SIGNATURE.length);
const body = after === -1 ? pageSource.slice(from) : pageSource.slice(from, after);
if (occurrences(body, '#"""') !== 1 || occurrences(body, '"""#') !== 1) {
  stop("the response body is not one raw string any more, so this suite cannot lift it");
}
const workerScript = body.slice(body.indexOf('#"""') + 4, body.indexOf('"""#'));
check("the lifted worker is the whole script, not a fragment", workerScript.length > 2000);
check("and it is the one that records where a tap went",
      workerScript.includes("sw.notificationclick") && workerScript.includes("sw.postMessage"));

/* ---- the two copies of the store's name -------------------------------------------------------
   The worker cannot import a module — it is a response body — so the cache it writes to is named
   twice, once in Swift and once in `route.js`. Two copies of a string in two languages is exactly
   the shape that drifts, and drift here is silent: the page opens an empty cache, reads nothing,
   and reports a road that never happened as a road that stopped at the first step. */
const workerConstant = (name) => {
  const found = new RegExp(`var ${name} = "([^"]+)"`).exec(workerScript);
  return found ? found[1] : null;
};
const routeConstant = (name) => {
  const found = new RegExp(`export var ${name} = "([^"]+)"`).exec(routeSource);
  return found ? found[1] : null;
};
const cacheName = workerConstant("TRACE_CACHE");
const traceURL = workerConstant("TRACE_URL");
check("the worker names the cache it writes to", typeof cacheName === "string" && !!cacheName);
check("and the entry inside it", typeof traceURL === "string" && !!traceURL);
equal(routeConstant("WORKER_TRACE_CACHE"), cacheName, "the page opens the cache the worker writes");
equal(routeConstant("WORKER_TRACE_URL"), traceURL, "and reads the entry the worker wrote");

/* ---- the URL a notification actually carries --------------------------------------------------
   Written here rather than imported, because the Swift that writes it is what this is holding the
   page against: RFC 3986's unreserved set and nothing else — see `WebPush.sessionURL`. */
const sessionURL = (id) =>
  "/#session=" + id.replace(/[^A-Za-z0-9\-._~]/g, (c) =>
    "%" + c.charCodeAt(0).toString(16).toUpperCase().padStart(2, "0"));

const PANE = "%208";                      // Clawdfather's own pane on this Mac, and a real shape
const ITERM = "w0t0p0:1234-ABCD";         // the control group: no per-cent, nothing to encode
equal(sessionURL(PANE), "/#session=%25208", "a tmux pane's notification URL");
equal(sessionURL(ITERM), "/#session=w0t0p0%3A1234-ABCD", "and an iTerm session's");
check("the fixture this suite rests on is a tmux id — the only shape this fault appears on",
      PANE.indexOf("%") === 0 && ITERM.indexOf("%") === -1);

/* ---- one world: a worker, a page, and a wire between them -------------------------------------
   `deliver` is the whole experiment. `true` is a browser that hands a client message to a page;
   `false` is the shape the fault would have if iOS drops one aimed at a page it has suspended.
   Both are real behaviours of real browsers, and this suite's job is that they look different. */
let worlds = 0;

async function makeWorld({ deliver = true, listed = [], startHash = "", noCaches = false } = {}) {
  worlds += 1;

  // Cache Storage, shared by both contexts, because on a device it is one origin's storage.
  const stores = new Map();
  class Res {
    constructor(text) { this.text = text; }
    json() { return Promise.resolve(JSON.parse(this.text)); }
  }
  const cacheFor = (name) => {
    if (!stores.has(name)) stores.set(name, new Map());
    const entries = stores.get(name);
    return {
      match: (url) => Promise.resolve(entries.get(url) || undefined),
      put: (url, res) => { entries.set(url, res); return Promise.resolve(); },
    };
  };
  // `noCaches` is a browser that has no Cache Storage, or one that refuses it. Both contexts
  // lose the store together, because on a device it is one origin's storage that is missing.
  const caches = noCaches ? undefined : {
    open: (name) => Promise.resolve(cacheFor(name)),
    keys: () => Promise.resolve([...stores.keys()]),
    delete: (name) => { stores.delete(name); return Promise.resolve(true); },
  };

  // ---- the page ----
  const notes = [];
  const opened = [];
  const inList = new Set(listed);
  const hashListeners = [];
  const messageListeners = [];
  const location = {
    value: startHash,
    get hash() { return this.value; },
    set hash(next) {
      if (next === this.value) return;
      this.value = next;
      hashListeners.forEach((fn) => fn());
    },
  };
  const env = {
    Pages: { knows: () => true, go: () => {}, goHome: () => {}, current: () => "sessions",
             home: () => "sessions" },
    pageInHash: () => null,
    byId: (id) => (inList.has(id) ? { id } : null),
    openSession: (id) => { opened.push(id); },
    Diagnostics: { note: (event, data) => { notes.push({ event, data: data || {} }); } },
    window: { addEventListener: (type, fn) => { if (type === "hashchange") hashListeners.push(fn); } },
    location,
    // `"serviceWorker" in navigator` is the gate on the listener this whole file is about, and
    // `web-pages.mjs` passes `{}` — which is why that suite has never installed it.
    navigator: { serviceWorker: {
      addEventListener: (type, fn) => { if (type === "message") messageListeners.push(fn); },
    } },
    document: { hidden: true },
    caches,
  };
  globalThis[`__routeEnv${worlds}`] = env;

  const standalone =
    `const window = globalThis.__routeEnv${worlds}.window;\n` +
    `const location = globalThis.__routeEnv${worlds}.location;\n` +
    `const navigator = globalThis.__routeEnv${worlds}.navigator;\n` +
    `const document = globalThis.__routeEnv${worlds}.document;\n` +
    `const caches = globalThis.__routeEnv${worlds}.caches;\n` +
    routeSource
      .replace('import { Pages, pageInHash } from "../core/pages.js";',
        `const Pages = globalThis.__routeEnv${worlds}.Pages;\n` +
        `const pageInHash = globalThis.__routeEnv${worlds}.pageInHash;`)
      .replace('import { byId } from "../view/derive.js";',
        `const byId = globalThis.__routeEnv${worlds}.byId;`)
      .replace('import { openSession } from "../session/open.js";',
        `const openSession = globalThis.__routeEnv${worlds}.openSession;`)
      .replace('import { Diagnostics } from "../core/layout-diagnostics.js";',
        `const Diagnostics = globalThis.__routeEnv${worlds}.Diagnostics;`);
  if (/^import /m.test(standalone)) {
    stop("an import in route.js was left behind — it would pull the whole app in and hang");
  }
  // A distinct URL per world: identical bytes are one module instance in node, and one instance
  // would carry `wantedSession` and `readThrough` from one experiment into the next.
  const page = await import("data:text/javascript;base64," +
    Buffer.from(`${standalone}\n// world ${worlds}\n`).toString("base64"));

  // ---- the worker ----
  const posted = [];
  const navigated = [];
  const focused = [];
  const openedWindows = [];
  const handlers = {};
  // A real `WindowClient`. **`postMessage` is on `Client` itself**, so every window a worker can
  // reach has one — which is the fact the fallback below is written as though were not true.
  const client = {
    id: "window-1", type: "window", url: "https://clawdline.example/",
    focused: false, visibilityState: "hidden",
    focus() { focused.push(true); this.focused = true; return Promise.resolve(); },
    postMessage(message) {
      posted.push(message);
      if (deliver) messageListeners.forEach((fn) => fn({ data: message }));
    },
    navigate(url) { navigated.push(url); return Promise.resolve(); },
  };
  const windows = { list: [client] };
  const self = {
    addEventListener: (type, fn) => { (handlers[type] = handlers[type] || []).push(fn); },
    skipWaiting: () => {},
    registration: { showNotification: () => Promise.resolve() },
  };
  const clients = {
    claim: () => Promise.resolve(),
    matchAll: () => Promise.resolve(windows.list),
    openWindow: (url) => { openedWindows.push(url); return Promise.resolve(); },
  };
  self.clients = clients;
  runInContext(workerScript, createContext({
    self, clients, caches, Response: Res, fetch: () => Promise.resolve({}),
    Promise, console, setTimeout, Date, JSON, Array,
  }));

  const tap = async (url) => {
    const waited = [];
    handlers.notificationclick[0]({
      notification: { close: () => {}, data: url === undefined ? undefined : { url } },
      waitUntil: (p) => { waited.push(p); return p; },
    });
    await Promise.all(waited);
  };

  return {
    page, notes, opened, posted, navigated, focused, openedWindows, windows, client, inList,
    location, tap,
    events: () => notes.map((n) => n.event),
    saw: (event) => notes.some((n) => n.event === event),
    noteFor: (event) => notes.find((n) => n.event === event) || null,
    wake: () => page.readWorkerTrace(),
  };
}

/* ---- the road, whole ------------------------------------------------------------------------ */
{
  const world = await makeWorld({ deliver: true, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  await world.wake();
  equal(world.opened.join(","), PANE,
        "a tapped notification opens the pane it names — the whole road, for the first time");
  equal(world.posted.length, 1, "by one message to the open window");
  const points = ["sw.notificationclick", "sw.postMessage", "page.sw.message",
                  "route.to", "route.openWanted"];
  points.forEach((point) => check(`the trace holds ${point}`, world.saw(point)));
  check("and the routing note says the request was answered",
        world.noteFor("route.openWanted").data.found === true);
  check("the worker's note says which window it chose out of how many",
        world.noteFor("sw.postMessage").data.windows === 1
        && world.noteFor("sw.postMessage").data.index === 0);
  check("and no id is written into the trace — it keeps shapes, not names",
        JSON.stringify(world.notes).indexOf(PANE) === -1);
}

/* ---- the same road with the message dropped --------------------------------------------------
   The fault the user reports, in the only shape that fits every fact known about it: the worker
   ran, the window was focused, and nothing else happened. What this asserts is not that the app
   recovers — it does not, and choosing how it should is a design question — but that after it
   fails there is now something on the device that says where it stopped. */
{
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  await world.wake();
  equal(world.opened.length, 0, "a message that is never delivered opens nothing");
  equal(world.focused.length, 1, "though the window is focused, which is what the user sees");
  check("the worker's half is on the device afterwards",
        world.saw("sw.notificationclick") && world.saw("sw.postMessage"));
  check("and the page's half is not — which is the reading that names the broken segment",
        !world.saw("page.sw.message") && !world.saw("route.to"));
  // Both entries, in order, and **both of them present** — the two `indexOf` calls are compared
  // only after each has been found, because `-1 < 0` is true and an absent entry would otherwise
  // read as an early one. That is not hypothetical: the first draft of the worker wrote these two
  // concurrently, they took the same sequence number, the second `put` overwrote the first, and
  // this check passed while `sw.notificationclick` was missing entirely.
  const order = world.events();
  check("the message was numbered after the click, so neither hides the other",
        order.indexOf("sw.notificationclick") >= 0 && order.indexOf("sw.postMessage") >= 0
        && order.indexOf("sw.notificationclick") < order.indexOf("sw.postMessage"));
}

/* ---- a window this worker cannot message ------------------------------------------------------
   `web-service-worker.mjs` covers this branch with a client that has `focus` and `navigate` and no
   `postMessage`. **No such client exists.** `postMessage` is defined on `Client`, which every
   `WindowClient` is one of, so the navigate fallback beside it has never run in a browser and
   cannot. This is not a bug being fixed here — it is the reason the handler has exactly one road
   for an open window, and therefore the reason a dropped message ends the tap. */
{
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  equal(world.navigated.length, 0,
        "a real window client is never navigated, because it always has postMessage");
  check("so an open window has one road and no second attempt behind it",
        world.posted.length === 1 && world.focused.length === 1);
}

/* ---- the cold start: the message arrives before the session list does -------------------------
   The other real way for this to end on the list, and the one `byId` owns. Nothing is opened, the
   request is held, and the note says the answer was no — which is the state that used to leave no
   trace at all, because `session.open.begin` is only written when the answer is yes. */
{
  const world = await makeWorld({ deliver: true, listed: [] });
  await world.tap(sessionURL(PANE));
  await world.wake();
  equal(world.opened.length, 0, "a session the list has not brought yet is not opened");
  check("but the request is held", !!world.page.wantedSession);
  check("and the miss is recorded, which no note used to be",
        world.noteFor("route.openWanted").data.found === false);
  world.inList.add(PANE);
  equal(world.page.openWanted(), true, "the list arrives and the request is answered");
  equal(world.opened.join(","), PANE, "with the pane the notification named");
  equal(world.notes.filter((n) => n.event === "route.openWanted").length, 2,
        "two attempts, two notes — a miss then a hit");
}

/* ---- the control group ------------------------------------------------------------------------
   An id with no per-cent in it. It has to pass, and it passing is what says the tmux arm above is
   measuring the encoding rather than the wiring. */
{
  const world = await makeWorld({ deliver: true, listed: [ITERM] });
  await world.tap(sessionURL(ITERM));
  equal(world.opened.join(","), ITERM, "an iTerm session opens by the same road");
}

/* ---- a notification with nowhere to go --------------------------------------------------------
   `/v1/push/test` and `/v1/orchestrator/notify` both send `url: "/"`. The page is right to route
   nothing, and the trace has to show that as a message that arrived and was declined rather than
   as a message that never came — those two have different fixes and used to look the same. */
{
  const world = await makeWorld({ deliver: true, listed: [PANE] });
  await world.tap("/");
  await world.wake();
  equal(world.opened.length, 0, "the test push opens no session");
  check("the message arrived and is recorded arriving", world.saw("page.sw.message"));
  check("and was declined before routing", !world.saw("route.to"));
  check("the worker recorded the road it took", world.saw("sw.postMessage"));
}

/* ---- no window at all -------------------------------------------------------------------------
   The other road, which `manifest.start_url` is `/` for — so it is the one that can lose the
   fragment on iOS, and the trace has to say when it was taken. */
{
  const world = await makeWorld({ deliver: true, listed: [PANE] });
  world.windows.list = [];
  await world.tap(sessionURL(PANE));
  await world.wake();
  equal(world.openedWindows.join(","), "/#session=%25208", "a window is opened on that URL");
  check("and the trace says this tap took the other road",
        world.saw("sw.openWindow") && !world.saw("sw.postMessage"));
  check("with no window found", world.noteFor("sw.notificationclick").data.windows === 0);
}

/* ---- reading twice ----------------------------------------------------------------------------
   A phone wakes this page more than once. The reader must not report the same tap again on the
   second wake, and must not skip a real entry because two of them share a millisecond — which is
   what the click and the message it sends always do. */
{
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  equal(await world.wake(), 2, "the first wake reads both of the worker's entries");
  equal(await world.wake(), 0, "and the second reads none of them again");
  await world.tap(sessionURL(PANE));
  equal(await world.wake(), 2, "a second tap is two more");
  equal(world.notes.filter((n) => n.event === "sw.notificationclick").length, 2,
        "and neither tap was swallowed by the other");
}

/* ---- a browser with no Cache Storage ----------------------------------------------------------
   The worker must take the same road it always did, and the page must answer "nothing to read"
   rather than throwing into a boot sequence. A diagnostic that can break the app it watches is
   worse than no diagnostic. */
{
  const blind = await makeWorld({ deliver: true, listed: [PANE], noCaches: true });
  await blind.tap(sessionURL(PANE));
  equal(blind.opened.join(","), PANE, "the tap still opens the session with no store to write to");
  equal(await blind.wake(), 0, "and reading answers nothing rather than throwing");
  check("the page's own three notes are unaffected — they never needed the store",
        blind.saw("page.sw.message") && blind.saw("route.to") && blind.saw("route.openWanted"));
  check("and the worker's two are simply absent",
        !blind.saw("sw.notificationclick") && !blind.saw("sw.postMessage"));
}

console.log(failures === 0
  ? `notification route: all ${checks} checks passed`
  : `notification route: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
