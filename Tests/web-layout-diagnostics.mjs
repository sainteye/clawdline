import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

let checks = 0;
function equal(actual, expected, message) { assert.deepEqual(actual, expected, message); checks += 1; }
function ok(value, message) { assert.ok(value, message); checks += 1; }
import { Diagnostics, LAYOUT_PROBE_INTERVAL_MS, layoutAnomaly } from "../Resources/web/app/js/core/layout-diagnostics.js";

function sample(changes = {}) {
    return Object.assign({
        ready: true, phone: true, hidden: false, hasOpen: true, view: "detail",
        header: { width: 390, height: 52, visibleArea: 20280 },
        app: { width: 390, height: 792, visibleArea: 308880 },
        list: { width: 390, height: 792, visibleArea: 0, visibility: "hidden", display: "flex" },
        detail: {
            width: 390, height: 844, visibleArea: 329160,
            visibility: "visible", display: "flex", opacity: "1", transform: "none"
        },
        transcriptScroller: {
            width: 390, height: 680, visibleArea: 265200,
            visibility: "visible", display: "block", opacity: "1", transform: "none"
        },
        panel: "", agent: false, sessions: 3,
        rows: { dom: 3, visible: 3 }
    }, changes);
}

assert.equal(layoutAnomaly(sample()), null,
    "a visible phone transcript is not an incident");
assert.equal(layoutAnomaly(sample({
    app: { width: 390, height: 0, visibleArea: 0 }
})), "app_collapsed_below_header",
"a surviving header over a collapsed app is the reported black-screen shape");
assert.equal(layoutAnomaly(sample({
    detail: {
        width: 390, height: 844, visibleArea: 0,
        visibility: "visible", display: "flex", opacity: "1",
        transform: "matrix(1, 0, 0, 1, 390, 0)"
    }
})), "detail_offscreen",
"an open chat translated wholly offscreen is distinguished from a height collapse");
assert.equal(layoutAnomaly(sample({
    detail: {
        width: 390, height: 844, visibleArea: 0,
        visibility: "hidden", display: "flex", opacity: "1", transform: "none"
    }
})), "detail_hidden",
"an open chat hidden by CSS gets its own diagnosis");
assert.equal(layoutAnomaly(sample({
    transcriptScroller: {
        width: 390, height: 680, visibleArea: 0,
        visibility: "visible", display: "block", opacity: "1", transform: "none"
    }
})), "transcript_scroller_offscreen",
"a painted detail shell with its transcript scroller missing is still an incident");
assert.equal(layoutAnomaly(sample({
    hit: { centerInDetail: false }
})), "detail_not_hit_testable",
"a detail pane that owns geometry but not the hit-test surface records a compositor failure");
assert.equal(layoutAnomaly(sample({
    panel: "git",
    transcriptScroller: {
        width: 390, height: 680, visibleArea: 0,
        visibility: "hidden", display: "none", opacity: "1", transform: "none"
    }
})), null,
"an intentional full-pane panel may hide the transcript scroller");
assert.equal(layoutAnomaly(sample({
    hasOpen: false, view: "list", rows: { dom: 0, visible: 0 }
})), "list_rows_missing",
"a populated state whose list has no rows preserves the blank-screen incident");
assert.equal(layoutAnomaly(sample({ ready: false })), null,
    "the intentional blank before boot cannot become an incident");
assert.equal(layoutAnomaly(sample({ hidden: true })), null,
    "a background page is not diagnosed from geometry WebKit is not laying out");

const responsive = readFileSync(new URL("../Resources/web/app/css/responsive.css", import.meta.url), "utf8");
assert.match(responsive, /\.pane-detail\s*\{[^}]*visibility:\s*hidden[^}]*transition:\s*none/s,
    "the phone detail pane does not depend on a transformed compositor layer while offstage");
assert.match(responsive, /\.app\[data-view="detail"\] \.pane-detail\s*\{[^}]*visibility:\s*visible/s,
    "entering a phone chat makes its fixed layer visible atomically");

/* ---- how often the page measures itself ----------------------------------------------------
 *
 * `note` is called for every stream frame, transcript phase and attribute mutation. Each probe
 * reads about nine rects and every row and hit-tests the screen three times; on 2026-09-15 every
 * note scheduled one in the next frame and re-armed a 380 ms settle probe as well. A burst is one
 * probe now, the state after its last note is still measured, and nothing is measured on a desk
 * browser with the panel shut, where no incident can be recorded.
 * -------------------------------------------------------------------------- */

const noop = function () {};
let now = 10_000;
let phone = true;
let probes = 0;
const frames = [];
const timers = [];
function stubElement() {
    return {
        dataset: {}, style: { getPropertyValue: function () { return ""; } },
        getBoundingClientRect: function () {
            return { left: 0, top: 0, right: 390, bottom: 800, width: 390, height: 800 };
        },
        closest: function () { return null; }
    };
}
const install = function (name, value) {
    Object.defineProperty(globalThis, name, { value: value, configurable: true, writable: true });
};
install("document", {
    hidden: false, documentElement: Object.assign(stubElement(), { appendChild: noop }),
    body: stubElement(), activeElement: null,
    querySelector: function () { return stubElement(); },
    querySelectorAll: function () { return []; },
    addEventListener: noop,
    elementFromPoint: function () { probes += 1; return null; },
    elementsFromPoint: function () { return []; }
});
install("window", {
    innerWidth: 390, innerHeight: 800, visualViewport: null, addEventListener: noop,
    matchMedia: function (query) { return { matches: query.indexOf("max-width") >= 0 ? phone : false }; }
});
install("location", { search: "" });
install("navigator", { userAgent: "node" });
install("localStorage", { getItem: function () { return null; }, setItem: noop, removeItem: noop });
install("getComputedStyle", function () { return { getPropertyValue: function () { return ""; } }; });
install("performance", { now: function () { return now; } });
install("requestAnimationFrame", function (fn) { frames.push(fn); });
const realSetTimeout = globalThis.setTimeout;
const realClearTimeout = globalThis.clearTimeout;
install("setTimeout", function (fn, ms) { const t = { fn: fn, at: now + ms, live: true }; timers.push(t); return t; });
install("clearTimeout", function (t) { if (t) t.live = false; });
function advance(ms) {
    now += ms;
    frames.splice(0).forEach(function (fn) { fn(); });
    timers.filter(function (t) { return t.live && t.at <= now; }).forEach(function (t) { t.live = false; t.fn(); });
}
const state = { sessions: [], tx: { entries: [] }, openId: null };
const elements = { app: stubElement(), tx: stubElement(), "tx-scroll": stubElement(),
    "detail-head": stubElement(), composer: stubElement(), conn: stubElement() };
Diagnostics.bind({ state: state, elements: elements });
Diagnostics.ready();
advance(0);
const afterReady = probes;
equal(afterReady, 1, "ready measures once");
for (let i = 0; i < 40; i += 1) Diagnostics.note("sessions.accepted", { rows: i });
advance(16);
equal(probes, afterReady, "forty notes inside the interval measure nothing yet");
advance(LAYOUT_PROBE_INTERVAL_MS);
equal(probes, afterReady + 1, "and are covered by one trailing probe, after the last of them");
advance(5 * LAYOUT_PROBE_INTERVAL_MS);
equal(probes, afterReady + 1, "a quiet page is not measured again");
Diagnostics.note("session.open.begin", {});
advance(16);
equal(probes, afterReady + 2, "the first note after a quiet interval is measured in the next frame");
for (let second = 0; second < 10; second += 1) {
    for (let i = 0; i < 20; i += 1) { Diagnostics.note("sessions.accepted", {}); advance(50); }
}
ok(probes - (afterReady + 2) <= 10 && probes - (afterReady + 2) >= 9,
    "ten seconds of a note every 50 ms are about one probe a second, not two hundred (got " +
    (probes - (afterReady + 2)) + ")");
phone = false;
advance(5 * LAYOUT_PROBE_INTERVAL_MS);
const desk = probes;
for (let i = 0; i < 20; i += 1) { Diagnostics.note("sessions.accepted", {}); advance(LAYOUT_PROBE_INTERVAL_MS); }
equal(probes, desk, "a desk browser with the panel shut is never measured, because nothing can be recorded there");
install("setTimeout", realSetTimeout);
install("clearTimeout", realClearTimeout);

console.log("web layout diagnostics tests passed");
