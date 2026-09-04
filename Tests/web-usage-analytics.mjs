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

// The Feature block the payload carries when nothing produces attribution: §4.5 of the design
// emits `classifier: {configured: false}`, and rows written before the classifier existed carry
// no `classifier` key at all. Both must read as "not configured".
function unconfiguredFeatures() {
  return {
    status: "no_accepted_attribution", automaticAttribution: false,
    groups: [], unknown: { runs: 2 },
  };
}

// Every field here differs from every other field in this payload, so no assertion can pass by
// reading the wrong one: a green "every field survives" over coincident values proves nothing.
function classifiedFeatures() {
  return {
    status: "available",
    automaticAttribution: true,
    policy: "one_unambiguous_accepted_head",
    classifier: {
      configured: true, id: "clawdline-local-feature-merger", version: "7", threshold: 0.75,
    },
    groups: [
      { id: "feature-4a2b", label: "Ledger repair", runs: 9, output: 41000,
        project: { id: "project-1f3a", label: "clawdline",
                   icon: { accent: "#2F6B5E", cells: [["#EEF6F4", null]] } },
        coverage: { status: "complete" } },
      { id: "feature-7c9d", label: "Schedule identity", runs: 4, output: 17500,
        project: { id: null, label: "Unknown Project", reason: "mixed_project_scope" },
        coverage: { status: "partial", unknownOutputRuns: 3 } },
    ],
    unknown: { label: "Unknown Feature", runs: 13, output: 6200, unknownOutputRuns: 6,
               reason: "no_unambiguous_accepted_head" },
  };
}

// `count` Features, every field of every row different from every other: distinct labels, runs,
// outputs, Project ids, Project names and accents. Two coincident values cannot tell two fields
// apart, and this repository has shipped a green "every field survives" assertion that had
// silently dropped three fields because two of them were equal.
function foldableFeatures(count) {
  const groups = [];
  for (let index = 0; index < count; index += 1) {
    const shade = (0x21 + index * 7).toString(16).padStart(2, "0");
    groups.push({
      id: `feature-${(index + 11).toString(16)}${index}`,
      label: `Feature ${index + 1} of ${count}`,
      runs: index + 3,
      output: 90000 - index * 1234,
      // The last row is the one with no mark: `drawIcon` refuses it and the cell must still
      // carry that Project's name rather than an empty cell or a substitute mark.
      project: index === count - 1
        ? { id: null, label: "Unknown Project", reason: "mixed_project_scope" }
        : { id: `project-${(index + 1) * 3}b`, label: `Project ${String.fromCharCode(65 + index)}`,
            icon: { accent: `#${shade}5${index}9${index}c`.slice(0, 7),
                    cells: [[`#a${shade}3f${index}1`.slice(0, 7), null]] } },
      coverage: index % 2 === 0
        ? { status: "complete" }
        : { status: "partial", unknownOutputRuns: index + 1 },
    });
  }
  return {
    status: "available", automaticAttribution: true,
    policy: "one_unambiguous_accepted_head",
    classifier: { configured: true, id: "clawdline-local-feature-merger", version: "7",
                  threshold: 0.75 },
    groups,
    unknown: { label: "Unknown Feature", runs: 37, output: 4100, unknownOutputRuns: 2,
               reason: "no_unambiguous_accepted_head" },
  };
}

function portfolioPayload(features) {
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
        schedules: [{ id: "nightly", label: "Nightly health", runs: 2, activeDays: 1, output: 200,
                      coverage: { status: "partial", unknownOutputRuns: 1 } }],
        unknownSchedule: { runs: 0 },
      },
      features: features || unconfiguredFeatures(),
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
  verify.equal(elements["usage-schedule-body"].firstChild.firstChild.textContent,
               "Nightly health");
  verify.match(elements["usage-feature-summary"].textContent,
               /Automatic Feature attribution is not configured/);
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

function renderPortfolio(usage, mainSource, features, environment = {}) {
  const doc = new FakeDocument();
  const elements = fakeElements(doc, mainSource);
  const controller = usage.bindUsagePortfolio(elements, {
    document: doc,
    fetch: () => Promise.reject(new Error("unexpected request")),
    ...environment,
  });
  controller.render(portfolioPayload(features), false);
  return { elements, controller };
}

function featureRows(elements) {
  return elements["usage-feature-body"].children.map((row) => row.children.map((cell) => ({
    label: cell.dataset.label, text: cell.textContent,
  })));
}

// Which Feature rows a reader can actually see. Folding is not filtering: every row stays in the
// DOM and every total still counts it, so what the fold changes is `hidden` and nothing else.
function visibleFeatureLabels(elements) {
  return elements["usage-feature-body"].children
    .filter((row) => !row.hidden)
    .map((row) => row.children[1] && row.children[1].textContent);
}

// Scenario: not configured keeps the honest sentence and never a table pretending
function exerciseUnconfiguredFeatures(usage, mainSource) {
  const honest = "Automatic Feature attribution is not configured."
    + " Accepted manual or external assignments appear here.";
  for (const [name, features] of [["absent classifier object", undefined],
                                  ["classifier.configured false", unconfiguredFeatures()]]) {
    const { elements } = renderPortfolio(usage, mainSource, features);
    verify.equal(elements["usage-feature-summary"].textContent, honest,
                 `${name} must keep the unconfigured Feature sentence byte for byte`);
    const rows = featureRows(elements);
    verify.equal(rows.length, 1, `${name} must render exactly the empty-table row`);
    verify.deepEqual(rows[0], [{ label: "Features",
                                 text: "No accepted Feature attribution in this range" }],
                     `${name} must keep the unconfigured empty-table cell byte for byte`);
    verify.equal(elements["usage-unknown-feature"].textContent,
                 "2 runs remain Unknown Feature. Proposals, rejections,"
                 + " and conflicting accepted heads never enter a named total.",
                 `${name} must still report its Unknown Feature runs`);
  }
}

// Scenario: configured renders the real Feature table and keeps Unknown
function exerciseConfiguredFeatures(usage, mainSource) {
  const { elements } = renderPortfolio(usage, mainSource, classifiedFeatures());
  const summary = elements["usage-feature-summary"].textContent;
  verify.match(summary, /clawdline-local-feature-merger/,
               "a configured summary must name the classifier id");
  verify.match(summary, /\bv7\b/, "a configured summary must name the classifier version");
  verify.match(summary, /≥ 0\.75\b/, "a configured summary must name the acceptance threshold");
  verify.equal(summary,
               "Local classifier clawdline-local-feature-merger v7 proposes;"
               + " the policy accepts at confidence ≥ 0.75."
               + " Only one unambiguous accepted head enters a named total.",
               "a configured summary must not fall back to the unconfigured sentence");
  verify.deepEqual(featureRows(elements), [
    [{ label: "Project", text: "clawdline" },
     { label: "Feature", text: "Ledger repair" },
     { label: "Agent work", text: "9" },
     { label: "Generated output", text: "41,000" },
     { label: "Coverage", text: "Complete" }],
    [{ label: "Project", text: "Unknown Project" },
     { label: "Feature", text: "Schedule identity" },
     { label: "Agent work", text: "4" },
     { label: "Generated output", text: "17,500" },
     { label: "Coverage", text: "Partial · 3 unknown output" }],
  ], "two accepted Features with different labels, runs and outputs must both render, "
     + "each naming the Project it belongs to");
  verify.equal(elements["usage-unknown-feature"].textContent,
               "13 runs remain Unknown Feature. Proposals, rejections,"
               + " and conflicting accepted heads never enter a named total.",
               "Unknown Feature must report its own runs, never zero and never suppressed");

  // An empty table under a configured classifier is a different statement from an empty table
  // under no classifier, so it must not borrow the unconfigured sentence.
  const barren = classifiedFeatures();
  barren.status = "no_accepted_attribution";
  barren.groups = [];
  barren.unknown = { label: "Unknown Feature", runs: 21, output: 8300, unknownOutputRuns: 5,
                     reason: "no_unambiguous_accepted_head" };
  const { elements: empty } = renderPortfolio(usage, mainSource, barren);
  verify.deepEqual(featureRows(empty),
                   [[{ label: "Features",
                       text: "No Feature reached the acceptance threshold in this range" }]],
                   "a configured classifier with no accepted head must say so in its own words");
  verify.equal(empty["usage-unknown-feature"].textContent,
               "21 runs remain Unknown Feature. Proposals, rejections,"
               + " and conflicting accepted heads never enter a named total.",
               "an empty configured table must still report its Unknown Feature runs");
  verify.equal(empty["usage-feature-count"].textContent, "0 Features",
               "an empty table still says how many Features there are, which is none");
  verify.equal(empty["usage-feature-fold"].hidden, true,
               "there is nothing to fold when there are no Feature rows");
}

// Scenario: the table folds to ten rows, says how many it is hiding, and expands back
function exerciseFeatureFold(usage, mainSource) {
  const total = 12;
  const { elements } = renderPortfolio(usage, mainSource, foldableFeatures(total));
  const everyLabel = foldableFeatures(total).groups.map((group) => group.label);

  verify.equal(featureRows(elements).length, total,
               "folding is not filtering: every Feature row stays in the table");
  verify.deepEqual(visibleFeatureLabels(elements), everyLabel.slice(0, 10),
                   "a folded Feature table shows the first ten rows and no more");
  verify.equal(elements["usage-feature-count"].textContent, "12 Features · 2 hidden",
               "a reader must never have to expand the table to learn how many rows it has");
  verify.equal(elements["usage-feature-fold"].hidden, false,
               "twelve Features must offer a control that reaches the other two");
  verify.equal(elements["usage-feature-fold"].textContent, "Show 2 more",
               "the collapsed control names the number of rows it is hiding");
  verify.equal(elements["usage-feature-fold"].getAttribute("aria-expanded"), "false");
  verify.equal(elements["usage-feature-fold"].getAttribute("aria-controls"), null,
               "aria-controls belongs to the markup, so nothing writes it at render time");
  const unknownLine = "37 runs remain Unknown Feature. Proposals, rejections,"
    + " and conflicting accepted heads never enter a named total.";
  verify.equal(elements["usage-unknown-feature"].textContent, unknownLine,
               "the Unknown Feature line is outside the fold and visible while folded");
  verify.equal(elements["usage-unknown-feature"].hidden, false,
               "the honest remainder is never one of the rows the fold puts away");

  elements["usage-feature-fold"].click();
  verify.deepEqual(visibleFeatureLabels(elements), everyLabel,
                   "expanding the fold reveals every remaining Feature row");
  verify.equal(elements["usage-feature-count"].textContent, "12 Features",
               "an expanded table hides nothing, and says so by naming no hidden rows");
  verify.equal(elements["usage-feature-fold"].textContent, "Show the first 10",
               "the expanded control offers the way back");
  verify.equal(elements["usage-feature-fold"].getAttribute("aria-expanded"), "true");
  verify.equal(elements["usage-unknown-feature"].textContent, unknownLine,
               "the Unknown Feature line is unchanged by expanding the fold");

  elements["usage-feature-fold"].click();
  verify.deepEqual(visibleFeatureLabels(elements), everyLabel.slice(0, 10),
                   "the control collapses as well as expands");

  const short = renderPortfolio(usage, mainSource, foldableFeatures(10)).elements;
  verify.equal(short["usage-feature-count"].textContent, "10 Features",
               "exactly ten Features hides nothing, so the count names no hidden rows");
  verify.equal(short["usage-feature-fold"].hidden, true,
               "a table that fits offers no fold control");
  verify.equal(visibleFeatureLabels(short).length, 10);
}

// Scenario: the Project column draws the page's own pixel mark, and never a substitute
function exerciseFeatureProjectMarks(usage, mainSource) {
  const total = 12;
  const drawn = [];
  const tinted = [];
  const { elements } = renderPortfolio(usage, mainSource, foldableFeatures(total), {
    // `false` for the last row, which is the Project this payload has no mark for — exactly what
    // the real `drawIcon` answers for an absent `cells`, and the branch that must reach the CSS
    // placeholder rather than draw something else.
    drawIcon: (canvas, icon, cellPx) => {
      drawn.push({ tag: canvas.tagName, cellPx, accent: icon && icon.accent });
      return !!(icon && icon.cells);
    },
    tint: (hex) => { tinted.push(hex); return `tinted(${hex})`; },
  });

  verify.equal(drawn.length, total, "every Feature row asks the page to draw its Project's mark");
  verify.ok(drawn.every((call) => call.tag === "CANVAS"),
            "the mark is drawn on a canvas, the one surface drawIcon knows how to write");
  verify.ok(drawn.every((call) => call.cellPx === 3),
            "every mark is drawn at one cell size, so the column does not stagger");
  verify.equal(new Set(drawn.map((call) => call.accent)).size, total,
               "each row was handed its own Project's icon, not a neighbour's");

  const rows = elements["usage-feature-body"].children;
  const first = rows[0].children[0].children[0];
  verify.equal(first.children[0].className, "usage-feature-project-mark",
               "a Project with a mark carries no placeholder class");
  verify.equal(first.children[1].textContent, "Project A",
               "the Project cell carries that Project's name beside its mark");
  verify.equal(first.children[1].style.color, `tinted(${tinted[0]})`,
               "a Project's name is tinted with that Project's own accent");
  verify.equal(tinted[0], foldableFeatures(total).groups[0].project.icon.accent);

  const last = rows[total - 1].children[0].children[0];
  verify.equal(last.children[0].className, "usage-feature-project-mark none",
               "a Project with no mark falls back to the CSS placeholder");
  verify.equal(last.children[1].textContent, "Unknown Project",
               "a Project with no mark still renders its name");
  verify.equal(last.children[1].style.color, "",
               "there is no accent to tint an unmarked Project's name with");
  verify.equal(tinted.length, total - 1, "tint is asked only about Projects that have a mark");

  // The page has exactly one implementation of both, and this module reaches them through the
  // seam `main.js` fills in. A second drawing path is the thing this assertion exists to refuse.
  verify.match(mainSource, /import \{[^}]*\bdrawIcon\b[^}]*\} from "\.\/core\/pixels\.js"/,
               "main.js must draw Feature marks with the page's own drawIcon");
  verify.match(mainSource, /import \{[^}]*\btint\b[^}]*\} from "\.\/core\/util\.js"/,
               "main.js must tint Feature Projects with the page's own tint");
  verify.match(mainSource, /bindUsagePortfolio\(\{[\s\S]*drawIcon: drawIcon, tint: tint/,
               "main.js must hand both to bindUsagePortfolio");
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
                    "usage-feature-count", "usage-feature-fold",
                    "usage-insights", "usage-agent-list", "usage-coverage-panel"]) {
    verify.match(page, new RegExp(`id="${id}"`), `Portfolio DOM must contain #${id}`);
  }
  verify.match(page, /<th scope="col" role="columnheader">Project<\/th><th scope="col" role="columnheader">Feature<\/th>/,
               "the Feature table names its Project column, first and before the Feature name");
  verify.match(page, /id="usage-feature-fold"[^>]*aria-controls="usage-feature-body"/,
               "the fold control must say in the markup which rows it folds");
  verify.match(css, /\.usage-feature-project-mark\.none \{[^}]*background: #202028/,
               "an unmarked Project falls back to the same placeholder the session list uses");
  // The fold sets `hidden` on rows, and the card breakpoints give `tr` a `display` of their own.
  // A class selector outranks the browser's `[hidden] { display: none }`, so without a rule of at
  // least that specificity the fold hides nothing below 1280px — which no fake DOM can notice.
  verify.match(css, /^\.usage-analytics tbody tr\[hidden\] \{ display: none; \}$/m,
               "a folded row must be hidden at every width, over the card breakpoints' own display");
  const foldRule = css.indexOf(".usage-analytics tbody tr[hidden]");
  const firstCardDisplay = css.search(/^\s+\.usage-project-table tr,$/m);
  verify.ok(foldRule >= 0 && firstCardDisplay >= 0,
            "both halves of that specificity argument must still be in the sheet");
  verify.match(page, /role="table"[\s\S]*role="rowgroup"[\s\S]*role="columnheader"/,
               "card breakpoints must retain explicit table semantics");
  verify.match(css, /@media \(max-width: 1280px\)[\s\S]*\.usage-project-table td::before[\s\S]*color: var\(--dim\); font-size: 12px/,
               "Project selection and accessible labels must survive 391–1280px");
  verify.match(css, /@media \(max-width: 390px\)/);
  verify.match(css, /@media \(max-width: 320px\)/);
  verify.match(css, /@media \(max-width: 680px\)[\s\S]*\.usage-controls fieldset \{ grid-template-columns: 1fr;/,
               "phone date controls must stack before Safari's intrinsic date width can overflow");
  verify.match(css, /@media \(max-width: 680px\)[\s\S]*\.usage-project-table tbody \{[\s\S]*overflow-x: auto;[\s\S]*scroll-snap-type: x mandatory;/,
               "phone Projects must be a horizontal snap carousel");
  verify.match(css, /:focus-visible/);
  verify.match(page, /Estimated spending \(Claude Code\)/,
               "the Claude-only spending scope must be visible in the column name");
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
  exerciseUnconfiguredFeatures(usage, mainSource);
  exerciseConfiguredFeatures(usage, mainSource);
  exerciseFeatureFold(usage, mainSource);
  exerciseFeatureProjectMarks(usage, mainSource);

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
