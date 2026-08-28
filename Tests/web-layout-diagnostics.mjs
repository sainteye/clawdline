import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { layoutAnomaly } from "../Resources/web/app/js/core/layout-diagnostics.js";

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

console.log("web layout diagnostics tests passed");
