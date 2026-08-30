import assert from "node:assert/strict";
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";

let assertionCount = 0;
const verify = new Proxy(assert, {
  get(target, property) {
    const value = target[property];
    if (typeof value !== "function") return value;
    return (...args) => {
      assertionCount += 1;
      return Reflect.apply(value, target, args);
    };
  },
});

const root = process.cwd();
const scriptPath = "Resources/web/app/js/view/usage.js";
const thisFile = fileURLToPath(import.meta.url);
const work = mkdtempSync(join(tmpdir(), "clawdline-web-usage-"));

class FakeNode {
  constructor(document, tag = "div", id = "") {
    this.ownerDocument = document;
    this.tagName = tag.toUpperCase();
    this.id = id;
    this.children = [];
    this.parentNode = null;
    this.listeners = {};
    this.dataset = {};
    this.style = {};
    this.attributes = {};
    this.className = "";
    this.hidden = false;
    this.value = "";
    this.open = false;
    this._text = "";
  }
  appendChild(child) {
    child.parentNode = this;
    this.children.push(child);
    return child;
  }
  removeChild(child) {
    this.children = this.children.filter((item) => item !== child);
    child.parentNode = null;
  }
  remove() { if (this.parentNode) this.parentNode.removeChild(this); }
  get firstChild() { return this.children[0] || null; }
  get textContent() {
    return this._text + this.children.map((child) => child.textContent).join("");
  }
  set textContent(value) {
    this._text = String(value ?? "");
    this.children = [];
  }
  addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
  dispatch(name, event = {}) {
    event.preventDefault ||= () => { event.defaultPrevented = true; };
    for (const handler of this.listeners[name] || []) handler(event);
  }
  click() { this.dispatch("click", {}); }
  focus() { this.ownerDocument.activeElement = this; }
  setAttribute(name, value) {
    this.attributes[name] = String(value);
    if (name === "open") this.open = true;
  }
  getAttribute(name) { return this.attributes[name] ?? null; }
  removeAttribute(name) {
    delete this.attributes[name];
    if (name === "open") this.open = false;
  }
  querySelectorAll(selector) {
    const matches = [];
    const className = selector.startsWith(".") ? selector.slice(1) : null;
    const visit = (node) => {
      if (className && String(node.className).split(/\s+/).includes(className)) matches.push(node);
      for (const child of node.children) visit(child);
    };
    visit(this);
    return matches;
  }
  contains(candidate) {
    return candidate === this || this.children.some((child) => child.contains(candidate));
  }
  close() { this.open = false; }
}

class FakeDocument {
  constructor() {
    this.body = new FakeNode(this, "body", "body");
    this.activeElement = this.body;
    this.listeners = {};
  }
  createElement(tag) { return new FakeNode(this, tag); }
  addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
}

function portfolioPayload() {
  const project = (id, label, output, rank) => ({
    id, label, output, rank, runs: 1, scheduledRuns: 0, unknownOutputRuns: 0,
    cost: { status: "unavailable", reason: "no_cost_series" },
    coverage: { status: "complete", unknownOutputRuns: 0 },
    lineage: { status: "available", rootRuns: 0, childRuns: 1,
               scheduledRuns: 0, unknownRuns: 0 },
    comparison: { status: "comparable", absolute: output, percent: 100 },
    trend: [{ bucket: "2026-08-30", tokens: { output } }],
    assistantMix: [{ label: "codex", output, runs: 1 }],
    workMix: [{ label: "Interactive", output, runs: 1 }],
    recentWork: [{ assistant: "codex", model: "gpt-5.6-sol", tokens: { output } }],
  });
  return {
    range: { from: "2026-08-01", to: "2026-08-30", timezone: "UTC" },
    schemaVersion: 1,
    freshness: { status: "current" },
    rangeFreshness: { dataThrough: "2026-08-30T00:00:00Z" },
    priceSnapshot: { observedIds: [] },
    availability: { status: "complete" },
    totals: {
      rows: 2, tokens: { output: 1200 }, tokenPartsUnknown: { output: 0 },
      coverage: { states: { complete: 2 }, reasons: {}, tokenRowsUnknown: 0 },
    },
    corrections: 0,
    portfolio: {
      runs: 2,
      comparison: { status: "comparable", absolute: 70, percent: 100 },
      projects: [project("alpha", "Alpha", 700, 1), project("beta", "Beta", 500, 2)],
      scheduledWork: {
        status: "available", runs: 2, output: 200, unknownOutputRuns: 1,
        schedules: [{ id: "nightly", runs: 2, activeDays: 1, output: 200,
                      coverage: { status: "partial", unknownOutputRuns: 1 } }],
        unknownSchedule: { runs: 0 },
      },
      features: { groups: [], unknown: { runs: 2 } },
      insights: [],
    },
    rows: [], pagination: { hasMore: false, nextCursor: null },
  };
}

function fakeElements(doc, mainSource) {
  const ids = new Set([...mainSource.matchAll(/\bbyId\("([^"]+)"\)/g)].map((match) => match[1]));
  const elements = {};
  for (const id of ids) elements[id] = new FakeNode(doc, "div", id);
  for (const id of ["app", "brand", "settings"]) {
    elements[id] ||= new FakeNode(doc, "div", id);
  }
  elements["usage-detail"].showModal = function () { this.open = true; };
  doc.body.appendChild(elements.app);
  doc.body.appendChild(elements["usage-analytics"]);
  return elements;
}

async function flush() {
  await new Promise((done) => setImmediate(done));
  await new Promise((done) => setImmediate(done));
}

async function exerciseRealModule(usage, mainSource) {
  const doc = new FakeDocument();
  const elements = fakeElements(doc, mainSource);
  const requests = [];
  let blobReads = 0;
  let fetchImpl = () => Promise.reject(new Error("unexpected request"));
  const request = (...args) => {
    requests.push(args[0]);
    return fetchImpl(...args);
  };
  const controller = usage.bindUsagePortfolio(elements, { document: doc, fetch: request });
  verify.ok(controller && typeof controller.render === "function",
            "executing Usage harness requires the real bindUsagePortfolio controller");
  verify.ok((elements["usage-controls"].listeners.submit || []).length === 1,
            "executing Usage harness requires the real submit binding");

  const data = portfolioPayload();
  controller.render(data, false);
  verify.equal(elements["usage-measured"].textContent, "1,200");
  verify.equal(elements["usage-run-count"].textContent, "2");
  verify.match(elements["usage-scheduled-output"].textContent, /200 measured/);
  verify.match(elements["usage-scheduled-runs"].textContent, /1 Unknown output/);
  const buttons = elements["usage-project-list"].querySelectorAll(".usage-open-project");
  verify.equal(buttons.length, 2);
  verify.equal(buttons[0].parentNode.parentNode.getAttribute("role"), "row");
  verify.equal(buttons[0].parentNode.getAttribute("role"), "cell");
  buttons[1].focus();
  buttons[1].click();
  verify.equal(doc.activeElement, buttons[1], "opening a Project must preserve keyboard focus");
  verify.equal(elements["usage-project-detail-title"].textContent, "Beta");

  elements["usage-from"].value = "2020-01-01";
  elements["usage-to"].value = "2020-01-02";
  fetchImpl = () => Promise.resolve({
    ok: false, status: 500,
    json: () => Promise.resolve({ error: { message: "Usage could not be read." } }),
  });
  elements["usage-controls"].dispatch("submit");
  await flush();
  verify.equal(elements["usage-analytics"].getAttribute("data-stale"), "true");
  verify.equal(elements["usage-measured"].textContent, "1,200");
  verify.match(elements["usage-availability"].textContent,
               /Showing stale data for 2026-08-01…2026-08-30.*2020-01-01…2020-01-02/);

  requests.length = 0;
  fetchImpl = () => Promise.resolve({
    ok: false, status: 413,
    json: () => Promise.resolve({ error: { code: "export_too_large" } }),
    blob: () => { blobReads += 1; return Promise.resolve(new Blob()); },
  });
  elements["usage-export-csv"].click();
  await flush();
  verify.match(requests[0], /^\/v1\/orchestrator\/usage\/analytics\.csv\?/);
  verify.equal(blobReads, 0, "a failed export must never be consumed as a Blob");
  verify.match(elements["usage-status"].textContent, /exceeds the matched-row export limit/);

  requests.length = 0;
  const pending = [];
  fetchImpl = () => new Promise((resolveRequest) => pending.push(resolveRequest));
  elements["usage-controls"].dispatch("submit");
  elements["usage-controls"].dispatch("submit");
  verify.equal(requests.length, 1, "an overlapping refresh is queued rather than started");
  verify.equal(elements["usage-status"].textContent, "Refresh queued…");
  pending.shift()({ ok: true, json: () => Promise.resolve({ usage: data }) });
  await flush();
  verify.equal(requests.length, 2, "the queued refresh starts after the active read settles");
  pending.shift()({ ok: true, json: () => Promise.resolve({ usage: data }) });
  await flush();

  verify.equal(usage.formatUsageNumber(null), "Unknown");
  verify.equal(usage.describeUsageComparison({ status: "unavailable", reason: "incomplete_output" }),
               "Change unavailable · output coverage is incomplete");
  verify.deepEqual(usage.rankUsageProjects([
    { id: "late", output: 8 }, { id: "unknown", output: null }, { id: "first", output: 12 },
  ]).map((item) => item.id), ["first", "late", "unknown"]);
}

function guard(tool, options = {}) {
  return spawnSync("python3", [tool], {
    cwd: root, encoding: "utf8",
    env: {
      ...process.env,
      ...(options.webRoot ? { CLAWDLINE_WEB_ROOT: options.webRoot } : {}),
      ...(options.index ? { CLAWDLINE_WEB_INDEX: options.index } : {}),
    },
  });
}

async function main() {
  const mainSource = readFileSync("Resources/web/app/js/main.js", "utf8");
  const selectedModule = process.env.CLAWDLINE_USAGE_MODULE || resolve(scriptPath);
  const usage = await import(pathToFileURL(selectedModule).href + `?test=${Date.now()}`);
  if (process.env.CLAWDLINE_USAGE_MUTATION === "1") {
    await exerciseRealModule(usage, mainSource);
    return;
  }

  const page = readFileSync("Resources/web/index.html", "utf8");
  const css = readFileSync("Resources/web/app/css/usage.css", "utf8");
  const script = readFileSync(scriptPath, "utf8");
  const diagnostics = readFileSync("Resources/web/app/js/core/layout-diagnostics.js", "utf8");
  const detailActions = readFileSync("Resources/web/app/js/input/detail-actions.js", "utf8");
  verify.match(page, /id="usage-open"/, "the Logo settings menu must contain Usage");
  verify.match(page, /href="\/app\/css\/usage\.css"/);
  verify.match(mainSource, /from "\.\/view\/usage\.js"/,
               "Usage must load from the stamped main.js module graph");
  verify.doesNotMatch(page, /from "\.\/app\/js\/view\/usage\.js"/,
                      "index.html must not bypass stamped module imports");
  verify.doesNotMatch(page, /id="usage-breakdown-body"/);
  for (const id of ["usage-project-list", "usage-project-detail", "usage-project-trend",
                    "usage-project-mix", "usage-project-lineage", "usage-project-recent",
                    "usage-schedule-body", "usage-feature-body", "usage-unknown-feature",
                    "usage-insights", "usage-agent-list", "usage-coverage-panel"]) {
    verify.match(page, new RegExp(`id="${id}"`), `Portfolio DOM must contain #${id}`);
  }
  verify.match(page, /role="table"[\s\S]*role="rowgroup"[\s\S]*role="columnheader"/,
               "card breakpoints must retain explicit table semantics");
  verify.match(css, /@media \(max-width: 1280px\)[\s\S]*\.usage-project-table td::before[\s\S]*color: var\(--dim\); font-size: 12px/,
               "Project selection and accessible labels must survive 391–1280px");
  verify.match(css, /@media \(max-width: 390px\)/);
  verify.match(css, /@media \(max-width: 320px\)/);
  verify.match(css, /:focus-visible/);
  verify.match(page, /Generated output is an operational signal, not a productivity score\./);
  verify.doesNotMatch(diagnostics, /installDebugButton/);
  verify.match(detailActions, /els\.conn\.addEventListener\("click"[\s\S]*api\.refresh/);

  for (const tool of ["tools/check-web-ids.py", "tools/check-web-strings.py"]) {
    const result = guard(tool);
    verify.equal(result.status, 0, `${tool} baseline failed: ${result.stdout}${result.stderr}`);
    verify.match(result.stdout, /resolved and non-empty|Portfolio protocol reads guarded/);
  }

  const mutatedIndex = join(work, "index-old-list.html");
  const begin = page.indexOf("<!-- Usage Portfolio: begin -->");
  const endMarker = "<!-- Usage Portfolio: end -->";
  const end = page.indexOf(endMarker, begin);
  verify.ok(begin >= 0 && end > begin, "Portfolio mutation boundaries must exist");
  writeFileSync(mutatedIndex, page.slice(0, begin)
    + `<section id="usage-overview-panel"><div id="usage-breakdown-body"></div></section>`
    + page.slice(end + endMarker.length));
  const oldList = guard("tools/check-web-ids.py", { index: mutatedIndex });
  verify.notEqual(oldList.status, 0, "restoring the old list must make the DOM guard RED");
  verify.match(oldList.stdout + oldList.stderr, /usage-project-list/);

  const missingCSSRoot = join(work, "missing-css");
  cpSync("Resources/web", missingCSSRoot, { recursive: true });
  rmSync(join(missingCSSRoot, "app/css/usage.css"));
  const missingCSS = guard("tools/check-web-ids.py", { webRoot: missingCSSRoot });
  verify.notEqual(missingCSS.status, 0, "deleting usage.css must make the asset guard RED");
  verify.match(missingCSS.stdout + missingCSS.stderr, /missing: .*usage\.css/);

  const missingJSRoot = join(work, "missing-js");
  cpSync("Resources/web", missingJSRoot, { recursive: true });
  rmSync(join(missingJSRoot, "app/js/view/usage.js"));
  const missingJS = guard("tools/check-web-ids.py", { webRoot: missingJSRoot });
  verify.notEqual(missingJS.status, 0, "deleting usage.js must make the import guard RED");
  verify.match(missingJS.stdout + missingJS.stderr, /missing: .*usage\.js/);

  await exerciseRealModule(usage, mainSource);

  const stub = join(work, "usage-stub.mjs");
  writeFileSync(stub, `
    export function formatUsageNumber(v) { return String(v); }
    export function describeUsageComparison() { return "stub"; }
    export function rankUsageProjects(v) { return v; }
    export function bindUsagePortfolio() { return { render() {}, load() {}, selectView() {} }; }
  `);
  const stubbed = spawnSync("node", [thisFile], {
    cwd: root, encoding: "utf8",
    env: { ...process.env, CLAWDLINE_USAGE_MODULE: stub, CLAWDLINE_USAGE_MUTATION: "1" },
  });
  verify.notEqual(stubbed.status, 0, "stubbing the real module must make the executing harness RED");
  verify.match(stubbed.stdout + stubbed.stderr, /real submit binding/);

  const syntax = spawnSync("node", ["--check", scriptPath], { encoding: "utf8" });
  verify.equal(syntax.status, 0, syntax.stdout + syntax.stderr);
  verify.match(script, /state\.pending = \{ cursor: cursor, append: append \}/);
  verify.match(script, /maximumSignificantDigits: 6/);

  console.log(`web usage portfolio guards: ${assertionCount} assertions executed`);
}

try {
  await main();
} finally {
  rmSync(work, { recursive: true, force: true });
}
