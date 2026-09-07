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
const mainPath = resolve(process.env.CLAWDLINE_MAIN_SOURCE
  || join(repoRoot, "Resources", "web", "app", "js", "main.js"));
// The third read point lives here rather than in `main.js`: the session list is where `openWanted`
// has always retried, and the record is the other half of the same request.
const listPath = resolve(process.env.CLAWDLINE_LIST_SOURCE
  || join(repoRoot, "Resources", "web", "app", "js", "view", "list.js"));

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
const mainSource = readFileSync(mainPath, "utf8");
const listSource = readFileSync(listPath, "utf8");

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

/* ---- and the second store, which is the one that is acted on ---------------------------------
   The trace is a numbered history nobody obeys; the record below is an instruction carried out
   once and destroyed. Sharing a store would mean sharing a read-modify-write, a sequence number
   and a pruning rule between the two, and the first bug in either would be a tap that vanished or
   a tap that fired twice. So the names must differ, and the two copies of each name — one in
   Swift, one in `route.js` — must not. */
const wantCache = workerConstant("WANT_CACHE");
const wantURL = workerConstant("WANT_URL");
check("the worker names the store it leaves the tap's request in",
      typeof wantCache === "string" && !!wantCache);
check("and the record inside it", typeof wantURL === "string" && !!wantURL);
equal(routeConstant("WORKER_WANT_CACHE"), wantCache, "the page opens that same store");
equal(routeConstant("WORKER_WANT_URL"), wantURL, "and reads that same record");
check("evidence and instruction are kept apart, which is the whole reason there are two",
      wantCache !== cacheName && wantURL !== traceURL);

/* ---- the URL a notification actually carries --------------------------------------------------
   Written here rather than imported, because the Swift that writes it is what this is holding the
   page against: RFC 3986's unreserved set and nothing else — see `WebPush.sessionURL`. */
const sessionURL = (id) =>
  "/#session=" + id.replace(/[^A-Za-z0-9\-._~]/g, (c) =>
    "%" + c.charCodeAt(0).toString(16).toUpperCase().padStart(2, "0"));

const PANE = "%208";                      // Clawdfather's own pane on this Mac, and a real shape
const OTHER = "%141";                     // a second one, for the taps that arrive in twos
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

async function makeWorld({ deliver = true, listed = [], startHash = "", noCaches = false,
                           focusFails = false, windowsOpen = 1 } = {}) {
  worlds += 1;

  // Set once the promise handed to `waitUntil` has settled: after that a device is free to stop
  // the worker, and a write still in flight never lands.
  const workerStopped = { value: false };

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
      // **A write that takes a turn to land, and a worker that can be shut down under it.**
      // A device stops a service worker as soon as the promise it gave `waitUntil` settles, and
      // anything still in flight is simply gone. Node has no such thing, so a handler that
      // abandons its own writes looks identical to one that awaits them — which is a test with
      // no power to disagree, and this suite has been caught by exactly that shape before.
      put: (url, res) => new Promise((done) => setTimeout(() => {
        if (!workerStopped.value) entries.set(url, res);
        done();
      }, 0)),
    };
  };
  // What the worker left in the second store, read the way a person would read it off a device
  // rather than through the page's own reader: these have to be able to say "the record is still
  // there" and "the record is gone", which the reader's answer alone cannot.
  const wantRecord = () => {
    const entries = stores.get(wantCache);
    const found = entries && entries.get(wantURL);
    return found ? JSON.parse(found.text) : null;
  };
  const putWant = (record) => {
    if (!stores.has(wantCache)) stores.set(wantCache, new Map());
    stores.get(wantCache).set(wantURL, new Res(JSON.stringify(record)));
  };
  // Every `caches.open`, by name. The third read point is called with every session list, and a
  // list arrives on every state change on this Mac — so "does it stop asking" is a fact about the
  // store being opened, and a count is the only way to see it from out here.
  const opens = [];
  // `noCaches` is a browser that has no Cache Storage, or one that refuses it. Both contexts
  // lose the store together, because on a device it is one origin's storage that is missing.
  const caches = noCaches ? undefined : {
    open: (name) => { opens.push(name); return Promise.resolve(cacheFor(name)); },
    keys: () => Promise.resolve([...stores.keys()]),
    delete: (name) => { stores.delete(name); return Promise.resolve(true); },
  };

  // ---- the page ----
  const notes = [];
  const declared = [];
  const reads = [];
  const opened = [];
  const inList = new Set(listed);
  const hashListeners = [];
  const messageListeners = [];
  // The window taking the focus back. On a phone that is what a notification banner tapped
  // while the app is already in front leaves behind: nothing loads, nothing was hidden, so
  // neither `boot` nor `visibilitychange` comes — see the block that drives these.
  const focusListeners = [];
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
    // `source` and `sourceRead` are the completeness half: `route.js` declares the worker's
    // recorder at module load and reports what became of every read, so a report can tell "not
    // folded in yet" from "folded in, and there was nothing". Recorded here rather than ignored,
    // because this suite's whole subject is that those two are different roads.
    Diagnostics: {
      note: (event, data) => { notes.push({ event, data: data || {} }); },
      source: (name) => { declared.push(name); },
      sourceRead: (name, state, entries) => { reads.push({ name, state, entries }); },
    },
    window: { addEventListener: (type, fn) => {
      if (type === "hashchange") hashListeners.push(fn);
      if (type === "focus") focusListeners.push(fn);
    } },
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
    focus() {
      focused.push(true); this.focused = true;
      // **iOS rejects this often enough to matter.** It used to sit in the middle of the chain
      // `waitUntil` was given, so one rejection ended the handler with the record unwritten.
      return focusFails ? Promise.reject(new Error("focus refused")) : Promise.resolve();
    },
    postMessage(message) {
      posted.push(message);
      if (deliver) messageListeners.forEach((fn) => fn({ data: message }));
    },
    navigate(url) { navigated.push(url); return Promise.resolve(); },
  };
  const postedSecond = [];
  // A second window the reader cannot see — a copy the system kept, suspended. The tap used to
  // go to whichever of these `matchAll` returned first and to no other.
  const second = {
    id: "window-2", type: "window", url: "https://clawdline.example/",
    focused: false, visibilityState: "hidden",
    focus() { return Promise.resolve(); },
    postMessage(message) { postedSecond.push(message); },
    navigate() { return Promise.resolve(); },
  };
  const windows = { list: windowsOpen > 1 ? [second, client] : [client] };
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

  // Taking over, which is where the worker says which worker it is. Driven rather than asserted
  // as source: a check that plants the mark itself cannot tell whether the worker writes one.
  const activate = async () => {
    const waited = [];
    handlers.activate[0]({ waitUntil: (p) => { waited.push(p); return p; } });
    await Promise.all(waited);
  };

  const tap = async (url, { stopAfter = false } = {}) => {
    const waited = [];
    handlers.notificationclick[0]({
      notification: { close: () => {}, data: url === undefined ? undefined : { url } },
      waitUntil: (p) => { waited.push(p); return p; },
    });
    await Promise.all(waited.map((p) => Promise.resolve(p).catch(() => {})));
    // The device's half of the bargain: what `waitUntil` was given has settled, so the worker may
    // now be stopped. A handler that let go of its own writes loses them here and only here.
    if (stopAfter) { workerStopped.value = true; await new Promise((d) => setTimeout(d, 5)); }
  };

  return {
    page, notes, opened, posted, navigated, focused, openedWindows, windows, client, inList,
    location, tap, declared, reads,
    events: () => notes.map((n) => n.event),
    saw: (event) => notes.some((n) => n.event === event),
    noteFor: (event) => notes.find((n) => n.event === event) || null,
    notesFor: (event) => notes.filter((n) => n.event === event),
    // **Both halves, because `boot` and `visibilitychange` both call both.** A wake-up that read
    // only the trace would be a page that still cannot act on anything, which is the state this
    // whole file is about leaving. The trace's count is what comes back, so that the readings
    // above about numbering still say what they said.
    wake: async () => { const read = await page.readWorkerTrace(); await page.readWorkerWant(); return read; },
    readWant: () => page.readWorkerWant(),
    // What a session list does: `list.js` calls this beside `openWanted`, which is the retry point
    // the request has always had.
    retry: () => page.retryWorkerWant(),
    wantOpens: () => opens.filter((name) => name === wantCache).length,
    // A message handed to the page by itself, which `deliver: true` cannot express: the worker
    // posts during the tap, and the order this exists for is the one where the page is given that
    // same message *afterwards* — a client message queued while iOS had the page suspended, and
    // dispatched behind the read that woke it.
    deliverMessage: (message) => { messageListeners.forEach((fn) => fn({ data: message })); },
    // The worker's own note about itself, and the ability to plant one — a page whose worker
    // never wrote one is exactly the state this reading exists to name.
    activate,
    postedSecond: () => postedSecond.slice(),
    lookAgain: () => page.lookAgainForWant(),
    putForeign: (name) => stores.set(name, new Map()),
    hasStore: (name) => stores.has(name),
    readMark: () => page.readWorkerMark(),
    workerBuild: () => page.workerBuildSeen(),
    putMark: (value) => stores.set(page.WORKER_MARK_CACHE, new Map(
      [[page.WORKER_MARK_URL, new Res(JSON.stringify(value))]])),
    wantRecord, putWant,
    // A record written that many milliseconds ago. Rewriting the stored `at` rather than moving
    // the clock: the clock is `Date.now()` in two contexts and a fake one would prove something
    // about the fake.
    ageWant: (ms) => {
      const record = wantRecord();
      if (!record) return null;
      record.at = Date.now() - ms;
      putWant(record);
      return record;
    },
    // The window regaining the focus, and how many handlers the page put there — a road nothing
    // calls is a road that does not exist, and here that can be driven rather than read.
    focusWindow: () => { focusListeners.forEach((fn) => fn()); },
    focusHandlers: () => focusListeners.length,
    // One turn of the event loop, for the deletions that are started but never awaited: nothing
    // in the page holds the tap up for a cache write.
    settle: () => new Promise((done) => setTimeout(done, 0)),
  };
}

/** How old a record may be and still be obeyed, read off the page rather than written down a
 *  second time here — a fixture that carries its own copy of the number under test is a fixture
 *  that keeps passing after somebody changes it. */
const WANT_WINDOW = (await makeWorld({})).page.WORKER_WANT_MAX_AGE_MS;

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
   ran, the window was focused, and the message it sent was never delivered. Until the second road
   existed the tap ended there — this block used to assert that nothing opened, which was true and
   was the bug. What it asserts now is that the tap still arrives, by a road that never depended on
   the message being delivered at all. */
{
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  equal(world.opened.length, 0, "when the handler returns, the dropped message has opened nothing");
  equal(world.focused.length, 1, "though the window is focused, which is what the user sees");
  await world.wake();
  equal(world.opened.join(","), PANE,
        "and the page wakes up, reads what the tap was for, and opens it anyway");
  check("the worker's half is on the device afterwards",
        world.saw("sw.notificationclick") && world.saw("sw.postMessage"));
  check("and the page's half is still missing — which is the reading that names the lost segment",
        !world.saw("page.sw.message"));
  check("so the routing came from the record rather than from a message",
        world.saw("page.want") && world.noteFor("page.want").data.routed === true
        && world.saw("route.to"));
  await world.settle();
  equal(world.wantRecord(), null, "and the record is spent, not left to fire again");
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
  // **Absent is not the same as never looked, and this is where that used to be lost.** With no
  // store, a trace with no `sw.` entry in it looked exactly like a worker that never woke up. The
  // recorder is declared at load and the read reports `unavailable`, so the report can say which
  // of the three it is instead of leaving somebody to guess from an empty list.
  check("the recorder is declared before anything looks at it",
        blind.declared.includes("serviceWorker"));
  // By name and not by position: three more recorders are declared beside it now, and a check
  // that reads `declared[0]` is a check about the order somebody wrote the lines in.
  const blindWorkerReads = blind.reads.filter(function (r) { return r.name === "serviceWorker"; });
  equal(blindWorkerReads.length, 1, "and one read is reported");
  equal(blindWorkerReads[0].state, "unavailable",
        "a browser with no store says so, rather than reporting an empty trace");

  // **The three rows a flood cannot reach.** Declared at load, so a report taken from a page
  // where nothing has happened says `unread` for each rather than leaving the row out.
  check("the message, want and open recorders are declared at load as well",
        blind.declared.includes("notificationMessage")
        && blind.declared.includes("notificationWant")
        && blind.declared.includes("notificationOpen"));
}

/* ---- the three ways a tap was lost after the worker had already woken ---------------------------
 *
 * Read from a phone on 2026-09-07: a worker that had plainly run and left nothing at all — no
 * trace, no message, no record. These are the three mechanisms that produce it.
 */
{
  // **`focus()` rejecting used to end the handler.** It sat in the middle of the chain given to
  // `waitUntil`, so one rejection skipped the tail, the wait settled, and the worker was shut
  // down with the tap's own record still unwritten.
  const refused = await makeWorld({ deliver: false, listed: [PANE], focusFails: true });
  await refused.tap(sessionURL(PANE), { stopAfter: true });
  check("a focus this device refuses does not take the record with it",
        !!(await refused.wantRecord()));
  await refused.wake();
  check("nor the worker's own account of the tap",
        refused.notes.some((e) => e.event === "sw.notificationclick"));
  check("and the message was still handed over before any of that",
        refused.posted.length === 1);
}

{
  // **Every window, not the first one that will take a message.**
  const twoWindows = await makeWorld({ deliver: false, listed: [PANE], windowsOpen: 2 });
  await twoWindows.tap(sessionURL(PANE));
  equal(twoWindows.postedSecond().length, 1, "the window the loop reaches first is told");
  equal(twoWindows.posted.length, 1,
        "and so is the one behind it, which used to be skipped");
}

{
  // **A record that lands after the read that went looking for it.** No list, no focus, no
  // visibility change — the shape a banner tapped over an app already in front of you produces.
  const late = await makeWorld({ deliver: false, listed: [PANE] });
  equal(await late.readWant(), "none", "the first look is in front of the write");
  await late.tap(sessionURL(PANE));
  late.lookAgain();
  await new Promise((done) => setTimeout(done, 700));
  equal(late.opened.join(","), PANE,
        "and a bounded look-again finds it, which is the tap that used to go nowhere");
}

/* ---- what taking over is allowed to destroy ---------------------------------------------------
 *
 * `activate` used to delete every cache, which was right while nothing wrote to Cache Storage and
 * wrong from the moment a tap started leaving a record there. On 2026-09-07 a phone posted a
 * message, wrote a record, and the page then read that store 151 times and found nothing: the
 * purge had run under it. The cost had been written down as "an update loses one tap" and was in
 * fact every tap the worker was restarted under.
 */
{
  const kept = await makeWorld({ deliver: false, listed: [PANE] });
  await kept.tap(sessionURL(PANE));
  check("a tap leaves a record", !!(await kept.wantRecord()));
  kept.putForeign("some-older-cache-nobody-owns");
  await kept.activate();
  check("and taking over does not take it away — the record outlives the worker that wrote it",
        !!(await kept.wantRecord()));
  check("while a cache this worker does not own is still swept",
        !kept.hasStore("some-older-cache-nobody-owns"));
  equal(await kept.readMark(), "wants",
        "and the mark it writes on the way past is there to be read");
}

/* ---- which worker wrote any of this -----------------------------------------------------------
 *
 * On 2026-09-07 a report from a real phone said the record road had looked 116 times and found
 * nothing, while the worker's own trace was arriving normally. Two faults produce that picture and
 * they have opposite fixes: a road that does not work, and a worker that has not updated and does
 * not know how to write a record at all. Nothing on either side could tell them apart.
 */
{
  const fresh = await makeWorld({ deliver: false, listed: [PANE] });
  // The worker itself, taking over — not a mark this test wrote. A check that plants what it
  // then reads back is a check about the test.
  await fresh.activate();
  equal(await fresh.readMark(), "wants",
        "a worker that left its mark is reported as one that knows how to leave a record");
  const row = fresh.reads.filter((r) => r.name === "serviceWorkerMark").slice(-1)[0] || {};
  equal(row.state, "wants", "and the row says so where a flood cannot reach it");
}

{
  // **The build, not just the capability.** Two consecutive builds both answered `wants` — one
  // keeping the record it wrote and one sweeping it away on the next activation — so a reading
  // taken under either could not say which. The stamp was added to the worker and this reader was
  // left reporting the capability, which is a signal shipped with nobody listening to it.
  const stamped = await makeWorld({ deliver: false, listed: [PANE] });
  stamped.putMark({ wants: true, keeps: true, build: "1788744677", at: Date.now() });
  equal(await stamped.readMark(), "b1788744677",
        "a worker that says which build it is, is reported as that build");
  const row = stamped.reads.filter((r) => r.name === "serviceWorkerMark").slice(-1)[0] || {};
  equal(row.state, "b1788744677", "and the row carries it where a flood cannot reach");
  equal(stamped.workerBuild(), "1788744677",
        "and the page can hold it against the build it is itself talking to");
}

{
  // A worker from before the stamp existed still answers, and says only what it can.
  const older = await makeWorld({ deliver: false, listed: [PANE] });
  older.putMark({ wants: true, at: Date.now() });
  equal(await older.readMark(), "wants",
        "a worker from before the stamp says the capability and no more");
  equal(older.workerBuild(), null, "and offers no build to compare");
}

{
  // The reading this exists for: the worker under this page is older than the page.
  const old = await makeWorld({ deliver: false, listed: [PANE] });
  equal(await old.readMark(), "absent",
        "a worker that never wrote one is named as absent, not left to look like a broken road");
  const row = old.reads.filter((r) => r.name === "serviceWorkerMark").slice(-1)[0] || {};
  equal(row.state, "absent",
        "and that is the difference between a stale worker and a road that does not work");
  equal(row.entries, 0, "and nothing is counted, because nothing was current");
}

{
  // What the worker says it posted, against what arrived. Neither number is the drop; the pair is.
  const counted = await makeWorld({ deliver: false, listed: [PANE] });
  await counted.tap(sessionURL(PANE));
  await counted.tap(sessionURL(OTHER));
  await counted.wake();
  const posted = counted.reads.filter((r) => r.name === "workerPosted")
                              .reduce((n, r) => n + (r.entries || 0), 0);
  const arrived = counted.reads.filter((r) => r.name === "notificationMessage").length;
  equal(posted, 2, "the worker's own count of the taps it handed to a window");
  equal(arrived, 0, "and none of them arrived, which is the drop stated as two numbers");
}

/* ---- the three rows a flood cannot reach -----------------------------------------------------
 *
 * The trace holds eighteen seconds and the gesture that sends a report takes longer, so every
 * reading below has been lost twice on a real phone. `completeness.sources` is a row per recorder
 * rather than a ring, and these are the three numbers the road is read from now.
 */
{
  // The tap this whole file exists for: the message is dropped, the record does the work.
  const dropped = await makeWorld({ deliver: false, listed: [PANE] });
  await dropped.tap(sessionURL(PANE));
  await dropped.wake();
  const row = (name) => dropped.reads.filter((r) => r.name === name);
  const total = (name) => row(name).reduce((n, r) => n + (r.entries || 0), 0);
  // A word rather than a subscript into a list that may be empty: a recorder that stopped
  // reporting should fail this block by name, not take the runner down with a TypeError three
  // checks before the one that would have said what happened.
  const last = (name) => (row(name).slice(-1)[0] || {}).state || "(never reported)";

  // **The number none of this could produce before.** No message arrived at all, which is a
  // different fault from one that arrived and was declined, and the two want opposite fixes.
  // **Zero here only means something if this row can be non-zero at all**, and with the reporting
  // removed it would read zero for the same reason a delivered message would. So the pair below
  // is the check, and the delivered case a few lines down is its other half: a recorder that
  // stopped reporting fails there, and this line is then read as the fault it names.
  equal(row("notificationMessage").length, 0,
        "a dropped message leaves the message row at zero reads, which is what says it was dropped");
  check("and the other two rows did report, so zero above is a silent road and not a silent recorder",
        row("notificationWant").length > 0 && row("notificationOpen").length > 0);
  equal(last("notificationWant"), "routed",
        "while the record row says the second road read it and acted");
  equal(total("notificationWant"), 1, "and counts exactly one tap carried out by that road");
  equal(last("notificationOpen"), "found",
        "and the open row says the session the tap named was in the list");
  equal(total("notificationOpen"), 1, "and that one session was opened");
}

{
  // The same tap with the message delivered: the message road wins and the count says so.
  const heard = await makeWorld({ deliver: true, listed: [PANE] });
  await heard.tap(sessionURL(PANE));
  const row = (name) => heard.reads.filter((r) => r.name === name);
  equal(row("notificationMessage").length, 1,
        "a delivered message is one read on the message row, whatever the road does next");
  equal(row("notificationMessage")[0].state, "navigate",
        "and the row carries the type it arrived with");
}

{
  // A cold start whose list has not arrived: the request is held, and the row says `missing`
  // rather than looking like a road that was never walked.
  const early = await makeWorld({ deliver: false, listed: [] });
  await early.tap(sessionURL(PANE));
  await early.wake();
  const opens = early.reads.filter((r) => r.name === "notificationOpen");
  check("a request held against a list that has not arrived is recorded as a miss",
        opens.length >= 1 && opens[opens.length - 1].state === "missing");
  equal(opens.reduce((n, r) => n + (r.entries || 0), 0), 0,
        "and nothing is counted as opened, because nothing was");
}

/* ---- and the same page where the store is there and empty ----------------------------------- */
{
  const quiet = await makeWorld({ deliver: true, listed: [PANE] });
  equal(await quiet.wake(), 0, "a store nobody has written to yields nothing");
  equal(quiet.reads[0].state, "merged",
        "which is `merged` with no entries — the opposite reading from `unavailable`");
  equal(quiet.reads[0].entries, 0, "and the count says how much nothing there was");
}

/* ---- the record and the list, in the order a cold start puts them in --------------------------
   The second road can arrive before the app knows what sessions exist — on iOS that is the usual
   order, because the system launches the web app at `start_url` and the list is a round trip
   through a tunnel behind it. So a record that has been read and could not be honoured must not be
   thrown away: `openWanted` is called again with every list, and the record is what a *reload*
   before that list arrives would otherwise lose. */
{
  const world = await makeWorld({ deliver: false, listed: [] });
  await world.tap(sessionURL(PANE));
  equal(await world.readWant(), "routed", "the record is read and acted on");
  equal(world.opened.length, 0, "but a session the list has not brought yet is not opened");
  check("the request is held instead", !!world.page.wantedSession);
  const miss = world.noteFor("route.openWanted");
  check("and the miss is recorded", !!miss && miss.data.found === false);
  await world.settle();
  check("the record is still on the device, because nothing has been reached yet",
        !!world.wantRecord());
  world.inList.add(PANE);
  equal(world.page.openWanted(), true, "the list arrives and the request is answered");
  equal(world.opened.join(","), PANE, "with the pane the notification named");
  await world.settle();
  equal(world.wantRecord(), null, "and only now is the record spent");
}

/* ---- the record that lands after the read that would have seen it ----------------------------
   The worker starts its cache write and does not hold the tap up for it, and until now the page
   read at `boot` and at `visibilitychange` and nowhere in between. A cold start falls exactly
   between them: `boot` reads before the record has landed, the page is then in the foreground, so
   no `visibilitychange` comes, and by the time one does the record is older than the window and is
   thrown away unread. The second road was silent on precisely the tap it exists for. The session
   list is the retry point `openWanted` has always had, and this is the same request's other half.
*/
{
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  equal(await world.readWant(), "none", "boot reads before the worker has written anything");
  await world.tap(sessionURL(PANE));
  check("the record lands a moment later", !!world.wantRecord());
  equal(world.opened.length, 0,
        "and nothing has read it — a page in the foreground is woken by nothing");
  equal(await world.retry(), "routed", "the next session list reads it and acts on it");
  equal(world.opened.join(","), PANE, "so the tap is not lost between the two old read points");
  // **And it does not stop.** This block used to end here asserting that every list after the
  // first answer was `skipped`, which is the defect the user walked into: the page cannot know
  // that a *new* record has landed without opening the store, so a page that stops looking has
  // closed the road for the rest of its life. What keeps one tap from being carried out twice is
  // the record's own id, not a fact about this page — so the cost is one store read per list, and
  // the thing that is refused is a second routing rather than a second look.
  await world.settle();
  const opened = world.wantOpens();
  equal(await world.retry(), "none", "the list after that looks again and finds the record spent");
  check("which means the store was opened again", world.wantOpens() > opened);
  equal(world.opened.join(","), PANE, "and nothing was opened a second time");
  equal(world.notesFor("route.to").length, 1, "by one routing, not two");
}
{
  // The control group for the guard: until an answer comes back it must keep asking, or the read
  // point that was added is one that never runs on a page where nothing has been tapped yet.
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  equal(await world.retry(), "none", "the first list finds nothing written");
  const opened = world.wantOpens();
  equal(await world.retry(), "none", "and the next one looks again");
  check("which means the store was opened again", world.wantOpens() > opened);
  await world.tap(sessionURL(PANE));
  equal(await world.retry(), "routed", "so the record is found whenever it lands");
  equal(world.opened.join(","), PANE, "and the tap arrives");
}

/* ---- the tap that is made while the app is already in front ----------------------------------
   **The shape the user actually walked into, and the one every block above misses.** All of them
   wake the page: `boot` reads, or `visibilitychange` does, or a list arrives at a page that had
   read nothing yet. He was holding the phone with the app open on the session list, and tapped the
   banner. Nothing loaded, so there is no `boot`; nothing was ever hidden, so no `visibilitychange`
   comes; and the message road is the one already known to get a single attempt. What is left is
   what this section is: a page that is running, a record that has just landed, and the read points
   that can still notice it.

   The first two blocks are the fault as he met it — a page whose *first* read answered anything at
   all had closed the road for the rest of its life, and after a night of testing there is always
   something in that store to answer it. */
{
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  // Last night's tap, left in the store: older than the window, so it is refused — correctly.
  world.putWant({ at: Date.now() - (WANT_WINDOW + 60000), url: sessionURL(OTHER), id: "last-night" });
  equal(await world.readWant(), "stale", "boot finds a record from last night and refuses it");
  await world.settle();
  equal(world.opened.length, 0, "so nothing is opened, which is the whole point of the window");
  // And now a real tap, on a page that has been open all along.
  await world.tap(sessionURL(PANE));
  check("the record the tap left is a different one", (world.wantRecord() || {}).id !== "last-night");
  equal(await world.retry(), "routed",
        "the next session list reads it — a refusal is about that record, not about this page");
  equal(world.opened.join(","), PANE, "and the tap arrives");
}
{
  // The same thing with the other non-`none` answer a first read can come back with. A record
  // naming no session is declined, and declining one says nothing at all about the next one.
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  world.putWant({ at: Date.now(), url: "/", id: "test-push" });
  equal(await world.readWant(), "declined", "boot finds a record that names no session");
  await world.settle();
  equal(world.opened.length, 0, "and opens nothing for it");
  await world.tap(sessionURL(PANE));
  equal(await world.retry(), "routed", "the tap after it is still read");
  equal(world.opened.join(","), PANE, "and still arrives");
}
{
  // **Two taps, in the foreground, one after the other.** The second is the one that used to do
  // nothing: the first read had answered `routed`, and every list after that was refused by a flag
  // about this page rather than by anything about the record in front of it. Somebody testing this
  // by tapping two notifications is exactly this order, and so is an ordinary morning.
  const world = await makeWorld({ deliver: false, listed: [PANE, OTHER] });
  await world.tap(sessionURL(PANE));
  equal(world.opened.length, 0, "the dropped message opens nothing, as it never did");
  equal(await world.retry(), "routed", "the session list behind it reads what the tap was for");
  equal(world.opened.join(","), PANE, "and opens that session");
  await world.settle();
  await world.tap(sessionURL(OTHER));
  equal(await world.retry(), "routed", "a second tap on the same page is read the same way");
  equal(world.opened.join(","), `${PANE},${OTHER}`, "and opens the session *it* named");
  equal(world.notesFor("route.to").length, 2, "two taps, two routings");
}
{
  // The other half of that, and the reason the guard has to be the record's id: three lists in a
  // row must not carry one record out three times. The copy put back by hand is the store refusing
  // a deletion — the case where the page has nothing but its own memory to go on.
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  const record = { ...world.wantRecord() };
  equal(await world.retry(), "routed", "the first list carries the record out");
  await world.settle();
  equal(await world.retry(), "none", "the second finds it spent");
  equal(await world.retry(), "none", "and so does the third");
  world.putWant(record);
  equal(await world.retry(), "settled", "a copy that outlived its deletion is recognised");
  equal(world.opened.join(","), PANE, "one record, one opening, however many lists arrive");
  equal(world.notesFor("route.to").length, 1, "and one routing");
}

/* ---- the read point a foreground tap can actually reach ---------------------------------------
   The list is the minimum and it is not enough on its own. A session list arrives when something
   on the Mac changes — and the change that *sent* the notification is the one that pushed the list
   before the banner was ever tapped. After that the stream can be quiet for as long as the Mac is,
   so "the next list" is not a moment anybody can point at.

   So the window taking the focus back is read as well. On a phone a banner is system UI over the
   app: it takes the key window while it is up and hands it back when it goes, which is a signal
   that costs one store read and arrives on the tap rather than on the Mac's next state change.
   It is not a substitute for the list — a page that is already focused when the record lands gets
   nothing from it — which is why both are here. */
{
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  equal(world.focusHandlers(), 1, "the page listens for its window getting the focus back");
  await world.tap(sessionURL(PANE));
  equal(world.opened.length, 0, "the dropped message has opened nothing, and no list has arrived");
  world.focusWindow();
  await world.settle();
  equal(world.opened.join(","), PANE,
        "the focus coming back is a read point of its own — no boot, no visibilitychange, no list");
  check("and it routed from the record rather than from a message",
        (world.noteFor("page.want") || { data: {} }).data.routed === true
        && !world.saw("page.sw.message"));
}
{
  // And it is the same guard, not a second road: focus and a list in either order carry one record
  // out once. A phone hands the focus back and pushes a list within the same second all the time.
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  world.focusWindow();
  await world.retry();
  await world.settle();
  world.focusWindow();
  await world.settle();
  equal(world.opened.join(","), PANE, "one tap, one opening, whichever read point gets there first");
  equal(world.notesFor("route.to").length, 1, "and one routing behind it");
}

/* ---- two notifications tapped before the list arrives ----------------------------------------
   The store holds one record and the newest tap replaces it whole — that is deliberate, and it is
   why the page cannot treat "there is a record" as "there is *this* record". The page is still
   holding the first tap's id, so the opening that follows is the first tap's; a deletion that
   simply emptied the store took the second tap's record with it, unread, and nothing on the screen
   said a notification had been dropped. Somebody testing this by pressing a test push twice is
   exactly this order. */
{
  const world = await makeWorld({ deliver: false, listed: [] });
  await world.tap(sessionURL(PANE));
  equal(await world.readWant(), "routed", "the first tap is read and held against an empty list");
  const first = world.wantRecord();
  await world.tap(sessionURL(OTHER));
  const second = world.wantRecord();
  check("the second tap replaced the record, which is the store's one rule",
        !!second && second.id !== first.id);
  world.inList.add(PANE);
  world.inList.add(OTHER);
  equal(world.page.openWanted(), true, "the list arrives and the first tap's request is answered");
  equal(world.opened.join(","), PANE, "with the pane the first notification named");
  await world.settle();
  const left = world.wantRecord();
  check("and the second tap's record is still there, because nothing has carried it out",
        !!left && left.id === second.id);
  equal(await world.readWant(), "routed", "so the notification just tapped is acted on");
  equal(world.opened.join(","), `${PANE},${OTHER}`, "and opens the session it named");
  await world.settle();
  equal(world.wantRecord(), null, "and only then is it spent");
}

/* ---- an opening that was not this record's ---------------------------------------------------
   The same rule from the other side, and the cheaper way to state it: the record is spent by the
   opening of the session *it* asked for. A page whose list has not brought that session yet can
   still open another one — a hashchange, somebody pressing a row — and an opening is not evidence
   that the tap has been answered. */
{
  const world = await makeWorld({ deliver: false, listed: [OTHER] });
  await world.tap(sessionURL(PANE));
  equal(await world.readWant(), "routed", "the record asks for a session the list has not brought");
  equal(world.opened.length, 0, "so nothing has opened yet");
  world.location.hash = `#session=${encodeURIComponent(OTHER)}`;
  equal(world.opened.join(","), OTHER, "the page goes somewhere else, and that session opens");
  await world.settle();
  check("the record is untouched, because that was not the session it asked for",
        !!world.wantRecord());
  world.inList.add(PANE);
  world.page.routeTo(`#session=${encodeURIComponent(PANE)}`);
  equal(world.opened.join(","), `${OTHER},${PANE}`, "and it is still there to be carried out");
  await world.settle();
  equal(world.wantRecord(), null, "spent by the opening of its own session, and only by that one");
}

/* ---- and the other end of the same rope: a request let go of ---------------------------------
   `list.js` calls `setWantedSession(null)` on the first whole list, because a session that is not
   in it is gone — a notification about a tab somebody has since closed. The record made that
   request, so it has to go with it: while it was still `pendingWant` every later wake-up called it
   settled *and* refused to collect it, and a reload inside the two-minute window carried out a
   request this page had already established was for a session that is not coming. */
{
  const world = await makeWorld({ deliver: false, listed: [] });
  await world.tap(sessionURL(PANE));
  equal(await world.readWant(), "routed", "the record is read and the request is held");
  world.page.setWantedSession(null);
  equal(world.page.openWanted(), false, "the first whole list lets go of the request");
  equal(await world.readWant(), "settled", "a later wake-up still recognises the tap it answered");
  await world.settle();
  equal(world.wantRecord(), null,
        "and collects the record, because nothing is waiting to carry it out any more");
}

/* ---- a notification tapped long enough ago that acting on it would be wrong -------------------
   The cost of a durable request is that it outlives the moment somebody made it. Two minutes is
   wide enough for an iOS cold start and narrow enough that a notification tapped in a lift does
   not move somebody who has since opened the app to read something else — and the failure it
   prevents is the worse of the two, because nothing on the screen would say why the app jumped.
   The control group is the same record one second inside the window: if that did not route, this
   test would be measuring the reader rather than the age. */
{
  const limit = (await makeWorld({})).page.WORKER_WANT_MAX_AGE_MS;
  check("the page states the window it obeys, rather than hiding it in a comparison",
        typeof limit === "number" && limit > 0);

  const fresh = await makeWorld({ deliver: false, listed: [PANE] });
  await fresh.tap(sessionURL(PANE));
  fresh.ageWant(limit - 1000);
  equal(await fresh.readWant(), "routed", "a second inside the window, the record is obeyed");
  equal(fresh.opened.join(","), PANE, "and the session opens");

  const old = await makeWorld({ deliver: false, listed: [PANE] });
  await old.tap(sessionURL(PANE));
  old.ageWant(limit + 1000);
  equal(await old.readWant(), "stale", "a second outside it, the record is refused");
  equal(old.opened.length, 0, "so a notification tapped days ago moves nobody");
  const refusal = old.noteFor("page.want");
  check("and the refusal is recorded as age rather than as absence",
        !!refusal && refusal.data.stale === true);
  await old.settle();
  equal(old.wantRecord(), null, "a record too old to obey is thrown away, not read again");
}

/* ---- waking up twice --------------------------------------------------------------------------
   `boot` reads, and so does every `visibilitychange`. A phone does the second of those constantly.
   Two things stop that becoming two jumps, and they are tested separately because they fail
   separately: the record is deleted once the session it names is open, and the id it carries is
   remembered by this page whether or not the deletion ever landed. The second is the one that
   holds when a store refuses a write, which is exactly when a page is least able to tell. */
{
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  const record = { ...world.wantRecord() };
  equal(await world.readWant(), "routed", "the first wake acts on it");
  equal(world.opened.length, 1, "one tap, one opening");
  await world.settle();
  equal(await world.readWant(), "none", "and the second finds nothing left to act on");
  // The deletion did not land — a store that refused it, or a wake-up that read the record before
  // it did. The id is the second lock, and it is the one that has to hold here.
  world.putWant(record);
  equal(await world.readWant(), "settled",
        "a record that outlived its deletion is recognised, not obeyed again");
  equal(world.opened.length, 1, "so waking twice still opens one session, once");
  equal(world.notesFor("route.to").length, 1, "and routes once");
  await world.settle();
  equal(world.wantRecord(), null,
        "and the deletion is tried again, because the session it names is already open");
}

/* ---- the two roads meeting, which is the ordinary case ----------------------------------------
   The message is not going away: when it arrives it does the work, exactly as it did before, and
   the record must then keep out of the way. It cannot do that by comparing addresses — somebody
   who taps a notification, reads the session and moves to another one is at a different address a
   moment later, and a record that fired then would take them back. So the tap's own id travels on
   the message, and the page marks the record answered on the way past. */
{
  const world = await makeWorld({ deliver: true, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  equal(world.opened.join(","), PANE, "the message road opens the session, as it always did");
  check("and the message carried the id of the record that tap left behind",
        world.posted.length === 1 && typeof world.posted[0].want === "string"
        && world.posted[0].want.length > 0);
  // **The worker's write can land behind the page that has already acted**, because the handler
  // does not hold the tap up for it — so what is asserted here is not that the record is gone by
  // now but that it can never be obeyed twice. The read below recognises it by the id the message
  // carried, and collects it on the way past.
  equal(await world.readWant(), "settled",
        "a record that outlived the deletion started for it is recognised, not obeyed");
  equal(world.opened.length, 1, "and nobody is moved twice by it");
  // The collection is started, not awaited, by the read that recognises it — one turn later it
  // has landed.
  await new Promise((done) => setTimeout(done, 0));
  equal(await world.readWant(), "none", "and the read that recognised it also collected it");
  // The deletion is a write to a store that can refuse one, and the id is what holds when it
  // does: a copy of that record put back by hand is recognised as the tap that has been answered.
  world.putWant({ at: Date.now(), url: sessionURL(PANE), id: world.posted[0].want });
  equal(await world.readWant(), "settled", "a copy that outlived it is recognised, not obeyed");
  equal(world.opened.length, 1, "so nobody is moved twice");
  equal(world.notesFor("route.to").length, 1, "by one routing, not two");
}
{
  // The same meeting on a cold start: the message arrived, the list had not. The record is settled
  // — this page will not act on it again — but it is kept, because the session it names is still
  // not open and a reload before the list arrives would be left with nothing.
  const world = await makeWorld({ deliver: true, listed: [] });
  await world.tap(sessionURL(PANE));
  equal(await world.readWant(), "settled", "the message settled it before the wake-up read it");
  await world.settle();
  check("but it is kept while the session it names is still unreached", !!world.wantRecord());
  world.inList.add(PANE);
  equal(world.page.openWanted(), true, "the list arrives and the message's request is answered");
  await world.settle();
  equal(world.wantRecord(), null, "and the record goes with it, whichever road did the opening");
}
{
  // **And the same meeting in the other order, which is the one nothing refused.** A page resumed
  // from the background starts its read at `visibilitychange`, and the client message queued while
  // iOS had the page suspended is dispatched during the three asynchronous hops that read takes. So
  // the record acts first and the message lands behind it, announcing a tap that has already been
  // carried out. Both roads acting is what the "answered once" rule says cannot happen, and the
  // cost is not abstract: `openSession` fetches the transcript again, and on a phone it pushes a
  // second history entry — one back gesture that does nothing, in this exact flow.
  const world = await makeWorld({ deliver: false, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  const message = world.posted[0];
  equal(await world.readWant(), "routed", "the record road acts first, the message not yet given");
  equal(world.opened.join(","), PANE, "and opens the session the tap named");
  world.deliverMessage(message);
  equal(world.opened.length, 1, "the message behind it must not open the same session again");
  equal(world.notesFor("route.to").length, 1, "nor route a second time");
  // Declined and not ignored: the note is written before the judgement, and it has to be able to
  // say *which* refusal this was — a tap already carried out, or a message naming no session.
  const seen = world.notesFor("page.sw.message");
  equal(seen.length, 1, "the message is still recorded arriving");
  check("and the trace says why it was declined — the tap had already been answered",
        seen[0].data.answered === true);
}
{
  // The control group for it, because a guard that refuses everything would pass the block above:
  // the ordinary order, where the message is the road that works and must not be turned away.
  const world = await makeWorld({ deliver: true, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  equal(world.opened.join(","), PANE, "a message arriving first still opens the session");
  check("and was not declined as one already answered",
        world.noteFor("page.sw.message").data.answered === false);
}

/* ---- what routing to the same address twice actually does -------------------------------------
   The second road has to keep out of the way of the message road, and *how* depends on a fact
   about `routeTo` that had been assumed rather than measured: it does not refuse a repeat. Sent to
   an address it is already at, it re-reads the fragment, finds the id and opens the session again.
   So an address that already matches is not evidence that the page has already been moved there —
   which is exactly why the two roads are told apart by the tap's id and not by comparing
   `location.hash`. Measured here so that the reasoning above rests on a check rather than on a
   reading of the code. */
{
  const world = await makeWorld({ deliver: true, listed: [PANE] });
  await world.tap(sessionURL(PANE));
  equal(world.opened.length, 1, "the tap opens the session once");
  world.page.routeTo(world.location.hash);
  equal(world.opened.length, 2,
        "and routing to the address it is already at opens it again — a repeat is not refused");
}

/* ---- a notification with nowhere to go, on the second road ------------------------------------
   `/v1/push/test` and `/v1/orchestrator/notify` both send `url: "/"`. A record saying "the person
   wanted the session list" would send whoever tapped a test push back to the list they were
   already on, every time they opened the app for the next two minutes. The worker writes none, and
   the page declines one anyway: the worker's gate is a second copy of a judgement `route.js`
   already owns, and second copies drift. */
{
  const world = await makeWorld({ deliver: true, listed: [PANE] });
  await world.tap("/");
  equal(world.wantRecord(), null, "a test push leaves nothing behind to act on later");
  equal(await world.readWant(), "none", "so a wake-up after one finds nothing");
  equal(world.opened.length, 0, "and opens nothing");
  equal(world.posted[0].want, "", "the message says so too, rather than carrying an id");
}
{
  // The two gates, driven against each other rather than compared as text. The page's is
  // `sessionCandidates`, reached here through the message road; the worker's is `wantedFragment`,
  // reached by whether a record appears. A URL either interests both of them or neither.
  //
  // **The seven URLs this started with all agreed, and the two gates did not.** Every one of them
  // had at most one `session=` in it, which is the one shape a `[^&]+` test and a `[^&]*` test
  // followed by an emptiness check cannot come apart on — so the row that would have shown the
  // divergence was the row nobody had written. A green here says the table asked; it says nothing
  // about the pairs it did not carry, and these are the ones that separate the two spellings:
  // a second `session=` behind an empty first one, the pre-encoding spellings still sitting on
  // somebody's phone (`%208`, `%141` — a fragment the page reads and cannot resolve is still a
  // fragment both gates must agree is one), a fragment inside a fragment, and a query string,
  // which is not a fragment at all however much it looks like this one.
  const gate = ["/", "/#", "/#page=usage", "/#session=", "/#session=%25208",
                "/#session=w0t0p0%3A1234-ABCD", "/#page=usage&session=%25141",
                "/#session=&session=%25141", "/#session=%208", "/#session=%141",
                "/#x#session=%25208", "/?session=%25208#page=usage",
                "/#session=&", "/#a=1&session=", "/#session", "/#Session=%25208",
                "/#session==%25208", "/#session=%25208&session=%25141", "/?session=%25208"];
  for (const url of gate) {
    const world = await makeWorld({ deliver: true, listed: [] });
    await world.tap(url);
    const workerWants = !!world.wantRecord();
    const pageRoutes = world.saw("route.to");
    check(`the two gates agree about ${url} (${workerWants ? "want" : "no want"})`,
          workerWants === pageRoutes);
  }
}

/* ---- a message the page routes nothing for has answered nothing ------------------------------
   The gates agree today, and the table above is what says so. This is what the second road costs
   if they ever stop agreeing: a message carrying an id for a URL this page declines used to mark
   that tap answered anyway, because the assignment sat above the gate. The record was then
   `settled` on a page that had done nothing about it and `pendingWant` kept it from being
   collected, so the tap was lost on both roads until a reload — a drifted gate costing two roads
   instead of one. Driven with a message rather than a tap, because the worker no longer sends one:
   what is being held is the page's own behaviour when it is handed something it declines. */
{
  const world = await makeWorld({ deliver: true, listed: [PANE] });
  world.deliverMessage({ type: "navigate", url: "/#session=", want: "9.1" });
  check("the message is recorded arriving", world.saw("page.sw.message"));
  check("and nothing was routed, because it names no session", !world.saw("route.to"));
  world.putWant({ at: Date.now(), url: sessionURL(PANE), id: "9.1" });
  equal(await world.readWant(), "routed",
        "so the record left by that tap is still acted on rather than called settled");
  equal(world.opened.join(","), PANE, "and the session it names opens");
}

/* ---- and the page that has to call it ---------------------------------------------------------
   Everything above drives `readWorkerWant` directly, which says nothing at all about whether the
   app ever calls it. The two wake-ups are `boot` and `visibilitychange` in `main.js`, they are the
   same two the trace is read at, and a road nothing calls is a road that does not exist. Read as
   source rather than driven, because driving `main.js` means booting the whole app. */
{
  check("main.js reads which worker it is running over, at both wake-ups",
        occurrences(mainSource, "readWorkerMark()") === 2);
  check("and asks the browser for a newer worker at both of them",
        occurrences(mainSource, "Push.recheck()") === 2);
  // **And stops asking when the two builds are known and different.** Registering on every load
  // and calling `update()` at both wake-ups are the polite forms, and a phone ran a page over an
  // older worker through several rounds of both.
  // **The cable is an instrument too.** Safari attaches to a device over USB and gives the page
  // and the worker separate consoles, which is the only reading of this road that arrives while it
  // is happening. Both halves are prefixed so they can be filtered to; a prefix somebody quietly
  // removes takes the instrument with it.
  check("both halves of the road say what they are doing, under one prefix",
        pageSource.includes("[clawdline/sw] ") && routeSource.includes("[clawdline/page] "));
  // And quiet where it would otherwise print once per session list.
  // **And the worker's half reaches the page's console.** Safari only lists a running worker as an
  // inspectable target and stops one that is idle, so the console that can always be attached to
  // is the page's. The carrier is Cache Storage, not a message: a diagnostic that travels the road
  // being diagnosed is silent exactly when it is wanted.
  check("what the worker wrote is repeated into the page's console as it is read back",
        /say\("worker said"/.test(routeSource));
  check("the look that found nothing does not say so",
        routeSource.includes('if (answer !== "none") say('));
  check("a worker that is not this build is thrown away and installed again",
        mainSource.includes("Push.reinstall()")
        && occurrences(mainSource, "readWorkerMark().then(matchWorkerToBuild)") === 2);
  check("main.js imports the reader from route.js",
        /import \{[^}]*\breadWorkerWant\b[^}]*\} from "\.\/input\/route\.js";/.test(mainSource));
  equal(occurrences(mainSource, "readWorkerWant()"), 2,
        "and calls it at both of the moments a page wakes up");
  equal(occurrences(mainSource, "readWorkerTrace()"), 2,
        "which are the two the trace is already read at");
}
{
  // And the third, which is the one this road did not have. It is in `list.js` rather than
  // `main.js` because the moment it needs is a session list arriving, which is where the same
  // request's other half — `openWanted` — has always been retried. Read as source for the same
  // reason: driving `list.js` means rendering the whole app.
  check("list.js imports the retry from route.js",
        /import \{[^}]*\bretryWorkerWant\b[^}]*\} from "\.\.\/input\/route\.js";/.test(listSource));
  equal(occurrences(listSource, "retryWorkerWant()"), 1, "and calls it, once, with every list");
  check("beside the openWanted call it is the other half of",
        /openWanted\(\);[^]{0,700}retryWorkerWant\(\);/.test(listSource));
}

console.log(failures === 0
  ? `notification route: all ${checks} checks passed`
  : `notification route: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
