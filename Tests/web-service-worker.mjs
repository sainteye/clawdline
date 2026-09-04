#!/usr/bin/env node

// `GET /sw.js` — the route, and the script it serves, actually run.
//
// **Why this file exists.** `RemotePage` was extracted from `RemoteServer` with five entry points.
// Four had route tests; `serviceWorker` had none, and the commit that found that
// (`B-SERVICE-WORKER-HAS-NO-ROUTE-TEST`) was careful to say the extraction neither caused it nor
// fixed it — the gap was invisible inside a 6,449-line file and nameable once it was not.
//
// **What a service worker fault looks like from outside is a stale page**, which is the one thing
// this script exists to prevent: `sw.js` is served `no-cache` so a browser revalidates it, and when
// it changes it installs, skips waiting, claims the open tabs and from then on fetches the document
// itself instead of the browser's cache. A device stuck on a pre-`no-store` copy has no other way
// out. Every step of that is one line, and one line is exactly what nobody notices deleting.
//
// **So the script is executed, not read.** The JavaScript is a response body inside a Swift raw
// string, and it is lifted out of `Sources/RemotePage.swift` and run in a `node:vm` context with
// stand-in worker globals: `install`, `activate`, `fetch`, `push` and `notificationclick` are each
// driven and what they did is asserted. A grep for `skipWaiting` would go on passing after the
// handler was replaced with an empty function.
//
// **The extraction is checked before it is trusted.** The function must occur once, the raw-string
// delimiters must occur once each inside it, and the lifted body must contain the five handlers. A
// slice that silently becomes empty is how a source-reading guard turns into a check that cannot
// fail.

import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createContext, runInContext } from "node:vm";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");
const pagePath = resolve(process.env.CLAWDLINE_REMOTE_PAGE_SOURCE
  || join(repoRoot, "Sources", "RemotePage.swift"));
const serverPath = resolve(process.env.CLAWDLINE_REMOTE_SERVER_SOURCE
  || join(repoRoot, "Sources", "RemoteServer.swift"));
const registrationPath = resolve(process.env.CLAWDLINE_PUSH_SOURCE
  || join(repoRoot, "Resources", "web", "app", "js", "input", "push.js"));

let checks = 0;
let failures = 0;
const check = (what, ok) => {
  checks += 1;
  if (!ok) failures += 1;
  console.log(`  ${ok ? "✓" : "✗"} ${what}`);
};
const stop = (why) => {
  console.log(`  ✗ ${why}`);
  console.log(`service worker: stopped after ${checks} checks — ${why}`);
  process.exit(1);
};
const occurrences = (haystack, needle) => haystack.split(needle).length - 1;

const page = readFileSync(pagePath, "utf8");
const server = readFileSync(serverPath, "utf8");
const registration = readFileSync(registrationPath, "utf8");

// ---- the route ---------------------------------------------------------------------------------
// Three files have to agree on one string for a service worker to exist at all: the page registers
// a path, the router answers that path, and the handler behind it is this one. Two of the three
// could be right and the worker would never install.
const SIGNATURE = "static func serviceWorker() -> RemoteServer.Response {";
check("the router answers GET /sw.js exactly once",
      occurrences(server, 'case ("GET", "/sw.js"):') === 1);
check("and it answers it with RemotePage.serviceWorker(), which is called nowhere else",
      occurrences(server, "RemotePage.serviceWorker()") === 1
      && /case \("GET", "\/sw\.js"\):\s*\n\s*return RemotePage\.serviceWorker\(\)/.test(server));
check("and the page registers that same path with the browser",
      occurrences(registration, 'navigator.serviceWorker.register("/sw.js")') === 1);
check("RemotePage declares that handler exactly once", occurrences(page, SIGNATURE) === 1);
if (occurrences(page, SIGNATURE) !== 1) stop("cannot find the handler this suite is about");

// ---- lift the response out of the handler --------------------------------------------------------
const from = page.indexOf(SIGNATURE);
const after = page.indexOf("\n    static func", from + SIGNATURE.length);
const body = after === -1 ? page.slice(from) : page.slice(from, after);
check("the handler's body is a slice of the file, not the whole of it",
      body.length > 500 && body.length < page.length);
check("it holds exactly one raw string, which is the script it serves",
      occurrences(body, '#"""') === 1 && occurrences(body, '"""#') === 1);
if (occurrences(body, '#"""') !== 1 || occurrences(body, '"""#') !== 1) {
  stop("the response body is not one raw string any more, so this suite cannot lift it");
}

const open = body.indexOf('#"""');
const close = body.indexOf('"""#');
check("the raw string opens before it closes", close > open);
if (close <= open) stop("the raw string's delimiters are inverted");
const script = body.slice(open + 4, close);
check("the lifted script is the whole worker, not a fragment", script.length > 2000);

// ---- the two headers that are the mechanism ------------------------------------------------------
// `no-cache` is not a detail. The worker is the only lever that reaches a browser already holding a
// stale copy of the page, and it can only be that lever while the browser revalidates the worker
// itself. Serving it cacheable is the same bug one level up, and it would look like nothing.
check("the response is 200", /RemoteServer\.Response\(status: 200,/.test(body));
check("served as JavaScript",
      body.includes('"Content-Type": "text/javascript; charset=utf-8"'));
check("and with Cache-Control: no-cache, which is what lets a changed worker ever install",
      body.includes('"Cache-Control": "no-cache"'));

// ---- run it --------------------------------------------------------------------------------------
// A worker's globals, and nothing more of them than the script touches. Every stand-in records what
// it was asked, so an assertion can be about what happened rather than about what is written.
const runWorker = (knobs = {}) => {
  const calls = {
    skipWaiting: 0, claim: 0, deleted: [], fetched: [], shown: [], opened: [], waited: [],
    responded: [],
  };
  const handlers = {};
  const cacheKeys = knobs.cacheKeys || (() => Promise.resolve([]));
  const fetchResult = knobs.fetch || (() => Promise.resolve({ ok: true, from: "network" }));
  const self = {
    addEventListener: (type, fn) => { (handlers[type] = handlers[type] || []).push(fn); },
    skipWaiting: () => { calls.skipWaiting += 1; },
    registration: {
      showNotification: (title, options) => {
        calls.shown.push({ title, options });
        return Promise.resolve();
      },
    },
  };
  const clients = {
    claim: () => { calls.claim += 1; return Promise.resolve(); },
    matchAll: (options) => Promise.resolve(knobs.clients ? knobs.clients(options) : []),
    openWindow: (url) => { calls.opened.push(url); return Promise.resolve(); },
  };
  self.clients = clients;
  const caches = {
    keys: () => cacheKeys(),
    delete: (name) => { calls.deleted.push(name); return Promise.resolve(true); },
  };
  const fetch = (input, init) => {
    calls.fetched.push({ input, init });
    return fetchResult(input, init);
  };
  const context = createContext({ self, clients, caches, fetch, Promise, console, setTimeout });
  runInContext(script, context);
  const fire = (type, event) => {
    const list = handlers[type] || [];
    const results = list.map((fn) => fn(event));
    return results.length === 1 ? results[0] : results;
  };
  return { calls, handlers, fire };
};

// A `waitUntil`/`respondWith` that hands the promise back so the test can await what the handler
// actually started, rather than sleeping and hoping.
const eventFor = (calls, extra = {}) => {
  const event = {
    waitUntil: (p) => { calls.waited.push(p); return p; },
    respondWith: (p) => { calls.responded.push(p); return p; },
    ...extra,
  };
  return event;
};

const worker = runWorker();
check("the script installs the five handlers the app depends on",
      ["install", "activate", "fetch", "push", "notificationclick"]
        .every((type) => (worker.handlers[type] || []).length === 1));
check("and adds no sixth listener nobody asked for",
      Object.keys(worker.handlers).length === 5);

// install
{
  const w = runWorker();
  w.fire("install", eventFor(w.calls));
  check("install skips waiting, so a new worker does not queue behind an open tab",
        w.calls.skipWaiting === 1);
}

// activate
{
  const w = runWorker({ cacheKeys: () => Promise.resolve(["old-a", "old-b"]) });
  w.fire("activate", eventFor(w.calls));
  await Promise.all(w.calls.waited);
  check("activate empties Cache Storage — every key, not the first",
        w.calls.deleted.length === 2 && w.calls.deleted.join(",") === "old-a,old-b");
  check("and then claims the open tabs, which is what takes over a page already loaded",
        w.calls.claim === 1);
}
{
  // The `catch` in that chain is load-bearing: a browser that refuses Cache Storage must not cost
  // the claim, because the claim is the half that unsticks a stale page.
  const w = runWorker({ cacheKeys: () => Promise.reject(new Error("no cache storage")) });
  w.fire("activate", eventFor(w.calls));
  await Promise.all(w.calls.waited);
  check("and it still claims them when Cache Storage refuses to answer", w.calls.claim === 1);
}

// fetch
{
  const w = runWorker();
  w.fire("fetch", eventFor(w.calls, { request: { mode: "cors", url: "https://host/app/js/app.js" } }));
  check("a subresource is left to the browser — the worker answers navigations only",
        w.calls.responded.length === 0 && w.calls.fetched.length === 0);
}
{
  const w = runWorker({ fetch: () => Promise.resolve({ ok: true, from: "network" }) });
  w.fire("fetch", eventFor(w.calls, { request: { mode: "navigate", url: "https://host/?s=1" } }));
  check("a navigation is answered by the worker", w.calls.responded.length === 1);
  const served = await w.calls.responded[0];
  check("with the document fetched fresh from the network", served.from === "network");
  check("asked for by URL, bypassing the HTTP cache and carrying the session cookie",
        w.calls.fetched.length === 1
        && w.calls.fetched[0].input === "https://host/?s=1"
        && w.calls.fetched[0].init.cache === "reload"
        && w.calls.fetched[0].init.credentials === "include");
}
{
  // Offline. The second attempt passes the Request itself, which is what lets the browser answer
  // from its own cache: stale and readable beats an error page.
  const request = { mode: "navigate", url: "https://host/" };
  let attempt = 0;
  const w = runWorker({
    fetch: (input) => {
      attempt += 1;
      if (attempt === 1) return Promise.reject(new Error("offline"));
      return Promise.resolve({ ok: true, from: "browser cache", input });
    },
  });
  w.fire("fetch", eventFor(w.calls, { request }));
  const served = await w.calls.responded[0];
  check("offline, the navigation falls back to whatever the browser would have done",
        served.from === "browser cache");
  check("and that second attempt hands over the Request, not the URL string",
        w.calls.fetched.length === 2 && w.calls.fetched[1].input === request);
}

// push
{
  const payload = {
    title: "clawdline — main", body: "a task finished", tag: "session-7",
    icon: "/icon/project.png", url: "/#session-7",
  };
  const w = runWorker();
  w.fire("push", eventFor(w.calls, { data: { json: () => payload } }));
  await Promise.all(w.calls.waited);
  check("a push shows one notification", w.calls.shown.length === 1);
  const shown = w.calls.shown[0];
  check("with the sender's title and body", shown.title === payload.title
        && shown.options.body === payload.body);
  check("its tag, so ten minutes in a pocket is one line and not six",
        shown.options.tag === "session-7" && shown.options.renotify === true);
  check("the project's own mark, and the URL to route to on the notification's data",
        shown.options.icon === "/icon/project.png" && shown.options.data.url === "/#session-7");
}
{
  // A push with nothing readable in it still has to draw something. `event.data.json()` throwing is
  // not hypothetical — it is what a malformed or empty payload does.
  const w = runWorker();
  w.fire("push", eventFor(w.calls, { data: { json: () => { throw new Error("not json"); } } }));
  await Promise.all(w.calls.waited);
  const shown = w.calls.shown[0];
  check("a payload that will not parse still shows the app's own notification",
        w.calls.shown.length === 1 && shown.title === "Clawdline" && shown.options.body === "");
  check("with the default tag, the app's creature, and the root URL",
        shown.options.tag === "clawdline" && shown.options.icon === "/icon-192.png"
        && shown.options.data.url === "/");
}
{
  const w = runWorker();
  w.fire("push", eventFor(w.calls, { data: null }));
  await Promise.all(w.calls.waited);
  check("and so does a push carrying no data at all", w.calls.shown.length === 1
        && w.calls.shown[0].title === "Clawdline");
}

// notificationclick
{
  const messages = [];
  const focused = [];
  const client = {
    focus: () => { focused.push(true); return Promise.resolve(); },
    postMessage: (message) => { messages.push(message); },
  };
  const w = runWorker({ clients: () => [client] });
  let closed = 0;
  w.fire("notificationclick", eventFor(w.calls, {
    notification: { close: () => { closed += 1; }, data: { url: "/#session-9" } },
  }));
  await Promise.all(w.calls.waited);
  check("a tapped notification is dismissed", closed === 1);
  check("an open page is told where to go rather than being navigated",
        messages.length === 1 && messages[0].type === "navigate"
        && messages[0].url === "/#session-9");
  check("and it is focused, because the point of the tap is to reach the session",
        focused.length === 1 && w.calls.opened.length === 0);
}
{
  // A client this worker does not control has no `postMessage` here; `navigate` is the fallback,
  // and it is only reached because the message could not have done it.
  const navigated = [];
  const client = {
    focus: () => Promise.resolve(),
    navigate: (url) => { navigated.push(url); return Promise.resolve(); },
  };
  const w = runWorker({ clients: () => [client] });
  w.fire("notificationclick", eventFor(w.calls, {
    notification: { close: () => {}, data: { url: "/#cold" } },
  }));
  await Promise.all(w.calls.waited);
  check("a client that cannot be messaged is navigated instead",
        navigated.length === 1 && navigated[0] === "/#cold");
}
{
  const w = runWorker({ clients: () => [] });
  w.fire("notificationclick", eventFor(w.calls, {
    notification: { close: () => {}, data: { url: "/#fresh" } },
  }));
  await Promise.all(w.calls.waited);
  check("with no window open at all, one is opened on that URL",
        w.calls.opened.length === 1 && w.calls.opened[0] === "/#fresh");
}
{
  const w = runWorker({ clients: () => [] });
  w.fire("notificationclick", eventFor(w.calls, { notification: { close: () => {} } }));
  await Promise.all(w.calls.waited);
  check("and a notification carrying no URL opens the root rather than nothing",
        w.calls.opened.length === 1 && w.calls.opened[0] === "/");
}
{
  // `includeUncontrolled` is what makes the search find the tab that has not been claimed yet,
  // which is every tab until the page has been reloaded once after this worker installed.
  let asked = null;
  const w = runWorker({ clients: (options) => { asked = options; return []; } });
  w.fire("notificationclick", eventFor(w.calls, {
    notification: { close: () => {}, data: { url: "/" } },
  }));
  await Promise.all(w.calls.waited);
  check("the search for a window includes the ones this worker does not control yet",
        asked !== null && asked.type === "window" && asked.includeUncontrolled === true);
}

console.log(failures === 0
  ? `service worker: all ${checks} checks passed`
  : `service worker: ${failures} of ${checks} checks failed`);
process.exit(failures === 0 ? 0 : 1);
