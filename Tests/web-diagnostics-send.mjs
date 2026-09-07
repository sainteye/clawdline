#!/usr/bin/env node

// The press, and what the panel says afterwards — driven, not read.
//
// A button that only changes colour is not a result. The person pressing it is holding a phone
// and has no other way to find out what happened. "Sent" is not an answer; the answer is the path, so they can
// read it to whoever asked, and on a refusal it is which refusal. So the assertions below are about
// the *text on the screen* containing the server's own path and the server's own typed code — not
// about a request having been made. A panel that posts perfectly and says nothing is the failure.
//
// It deliberately does not go near HTTP or the disk — `Tests/diagnostic-report-focused.mjs` owns
// what happens on this Mac. The one thing held across the gap is the route string, which is spelled
// once in JavaScript and once in Swift.

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..");
const panelPath = resolve(process.env.CLAWDLINE_DIAGNOSTIC_PANEL_SOURCE
  || join(repoRoot, "Resources", "web", "app", "js", "core", "layout-diagnostics.js"));

let checks = 0;
let failures = 0;
const check = (what, ok) => {
  checks += 1;
  if (!ok) failures += 1;
  console.log(`  ${ok ? "✓" : "✗"} ${what}`);
};
/* ---- a document, small enough to read -------------------------------------------------------- */
function element(tag) {
  return {
    tagName: String(tag).toUpperCase(),
    childNodes: [], style: { cssText: "" }, listeners: {},
    hidden: false, disabled: false, textContent: "", className: "", id: "", type: "",
    appendChild(child) { this.childNodes.push(child); return child; },
    addEventListener(name, fn) { (this.listeners[name] = this.listeners[name] || []).push(fn); },
    press(event) { (this.listeners.click || []).forEach((fn) => fn(event || {})); },
    querySelector(selector) {
      const matches = (node) => (selector.charAt(0) === "."
        ? String(node.className).split(/\s+/).indexOf(selector.slice(1)) >= 0
        : node.tagName === selector.toUpperCase());
      const walk = (node) => {
        for (let i = 0; i < node.childNodes.length; i += 1) {
          if (matches(node.childNodes[i])) return node.childNodes[i];
          const deeper = walk(node.childNodes[i]);
          if (deeper) return deeper;
        }
        return null;
      };
      return walk(this);
    },
  };
}

const root = element("html");
let posted = [];
let answer = null;

// `navigator` and `fetch` are getter-only accessors on `globalThis` in node 24, so every stand-in
// is installed the same way rather than by assignment — one road, no per-name surprises.
const install = (name, value) =>
  Object.defineProperty(globalThis, name, { value, configurable: true, writable: true });

install("document", {
  documentElement: root,
  hidden: false,
  createElement: element,
  querySelector: () => null,
  addEventListener: () => {},
});
install("window", { addEventListener: () => {} });
install("location", { search: "", hash: "" });
install("navigator", { userAgent: "node" });
install("performance", { now: () => Date.now() });
install("requestAnimationFrame", (fn) => setTimeout(fn, 0));
install("localStorage", { getItem: () => null, setItem: () => {}, removeItem: () => {} });
install("fetch", (url, options) => {
  posted.push({ url, options });
  return answer();
});

const { Diagnostics, REPORT_ROUTE } = await import(panelPath);

/* ---- the route, spelled once in each language ------------------------------------------------- */
const serverSource = readFileSync(join(repoRoot, "Sources", "RemoteServer.swift"), "utf8");
check("the panel names the route it posts to", typeof REPORT_ROUTE === "string" && !!REPORT_ROUTE);
check("and the Swift server answers that exact string",
      serverSource.includes(`case ("POST", "${REPORT_ROUTE}")`));

/* ---- what the report knows it is missing ------------------------------------------------------ */
Diagnostics.bind({ state: {}, elements: {} });

const limit = Diagnostics.completeness().trace.limit;
check("the recorder states the size of its own ring buffer", limit === 80);
check("an untouched recorder has dropped nothing",
      Diagnostics.completeness().trace.dropped === 0);
check("and calls itself whole", Diagnostics.completeness().whole === true);

for (let i = 0; i < limit + 20; i += 1) Diagnostics.note(`fixture.${i}`, { i });
const afterOverflow = Diagnostics.completeness();
check("a trace that overflowed says how many entries it threw away",
      afterOverflow.trace.dropped === 20);
check("and keeps exactly its limit", afterOverflow.trace.kept === limit);
check("and says the clock reading it truncated at, so the window is not guessed",
      typeof afterOverflow.trace.droppedThrough === "number"
      && afterOverflow.trace.droppedThrough <= afterOverflow.trace.from);
check("a report that dropped entries does not call itself whole",
      afterOverflow.whole === false);

// Requirement four, driven rather than asserted: this file is about notifications today and must
// not be about notifications tomorrow. An event name nothing has heard of goes through untouched.
Diagnostics.note("gesture.longpress.begin", { fingers: 1 });
const trace = Diagnostics.report().currentTrace;
check("an event name nothing in this module knows about is recorded verbatim",
      trace[trace.length - 1].event === "gesture.longpress.begin");
check("with its data", trace[trace.length - 1].data.fingers === 1);

/* ---- the panel, and what it says --------------------------------------------------------------- */
Diagnostics.reveal();
const panel = root.childNodes[root.childNodes.length - 1];
check("the panel is on screen", !!panel && !!panel.querySelector(".layout-debug-toggle"));

const labels = [];
const collect = (node) => {
  node.childNodes.forEach((child) => {
    if (child.tagName === "BUTTON") labels.push(child.textContent);
    collect(child);
  });
};
collect(panel);
check("the Copy report button it grew up beside is still there",
      labels.indexOf("Copy report") >= 0);
check("and there is a button that sends it to this Mac", labels.indexOf("Send to Mac") >= 0);

const sendButton = (function find(node) {
  for (let i = 0; i < node.childNodes.length; i += 1) {
    const child = node.childNodes[i];
    if (child.tagName === "BUTTON" && child.textContent === "Send to Mac") return child;
    const deeper = find(child);
    if (deeper) return deeper;
  }
  return null;
}(panel));
const said = () => panel.querySelector(".layout-debug-said");
check("the panel has somewhere to say what happened", !!said());
check("and says nothing before it has been pressed", said().hidden === true);

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

// ---- it worked -------------------------------------------------------------------------------
const writtenTo = "/Users/somebody/Library/Logs/Clawdline/diagnostics/report.json";
const before = "/Users/somebody/Library/Logs/Clawdline/diagnostics/previous.json";
posted = [];
answer = () => Promise.resolve({
  ok: true, status: 200,
  text: () => Promise.resolve(JSON.stringify({
    ok: true, path: writtenTo, previous: before, bytes: 18204, completeness_stated: true,
  })),
});
sendButton.press();
await settle(); await settle(); await settle();

check("one request went out", posted.length === 1);
check("to the route both languages agree on", posted[0].url === REPORT_ROUTE);
check("as a POST", posted[0].options.method === "POST");
check("carrying this browser's credential rather than a token typed into the page",
      posted[0].options.credentials === "same-origin");
const sent = JSON.parse(posted[0].options.body);
check("and the report itself", Array.isArray(sent.currentTrace) && sent.currentTrace.length > 0);
check("with its completeness block, so the file can say what is missing from it",
      !!sent.completeness && sent.completeness.trace.dropped
        === Diagnostics.completeness().trace.dropped
      && sent.completeness.trace.dropped >= 20);
check("the panel is speaking", said().hidden === false);
check("and it says the path the server wrote, so it can be read out to somebody",
      said().textContent.indexOf(writtenTo) >= 0);
check("and how big it was", said().textContent.indexOf("18204") >= 0);
check("and where the press before it went", said().textContent.indexOf(before) >= 0);
check("the button can be pressed again", sendButton.disabled === false);

// The path must be the server's, not one the page composed. A panel that prints a constant it
// holds itself will name a file nobody wrote the day either end moves.
posted = [];
const elsewhere = "/Users/somebody/Library/Logs/Clawdline/diagnostics/report.json.tmp-4823";
answer = () => Promise.resolve({
  ok: true, status: 200,
  text: () => Promise.resolve(JSON.stringify({ ok: true, path: elsewhere, previous: "", bytes: 12 })),
});
sendButton.press();
await settle(); await settle(); await settle();
check("the path on screen is whatever the server answered with, never a copy kept here",
      said().textContent.indexOf(elsewhere) >= 0);
check("a first press with nothing behind it says nothing about a previous file",
      said().textContent.indexOf("the press before it") < 0);

// ---- it was refused --------------------------------------------------------------------------
answer = () => Promise.resolve({
  ok: false, status: 413,
  text: () => Promise.resolve(JSON.stringify({
    error: { code: "report_too_large", message: "That report was 3145728 bytes and the limit is 2097152." },
  })),
});
sendButton.press();
await settle(); await settle(); await settle();
check("a refusal is named on screen by its code",
      said().textContent.indexOf("report_too_large") >= 0);
check("with the sentence the server sent, numbers and all",
      said().textContent.indexOf("3145728") >= 0 && said().textContent.indexOf("2097152") >= 0);
check("and the file that was not written is not named as though it had been",
      said().textContent.indexOf(writtenTo) < 0);
check("the button comes back after a refusal too", sendButton.disabled === false);

// ---- a refusal with no JSON in it ------------------------------------------------------------
answer = () => Promise.resolve({
  ok: false, status: 502, text: () => Promise.resolve("<html>bad gateway</html>"),
});
sendButton.press();
await settle(); await settle(); await settle();
check("a refusal that is not JSON still reaches the screen with its status",
      said().textContent.indexOf("http_502") >= 0);

// ---- there was no Mac to reach ---------------------------------------------------------------
answer = () => Promise.reject(new Error("Failed to fetch"));
sendButton.press();
await settle(); await settle(); await settle();
check("a request that never arrived says so rather than looking like a refusal",
      said().textContent.indexOf("Could not reach this Mac") >= 0);
check("and repeats what the browser said", said().textContent.indexOf("Failed to fetch") >= 0);
check("the button survives that as well", sendButton.disabled === false);

if (failures) {
  console.log(`web diagnostics send: ${failures} of ${checks} checks failed`);
  process.exit(1);
}
console.log(`web diagnostics send: ${checks} checks passed`);
