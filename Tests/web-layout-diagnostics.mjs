import assert from "node:assert/strict";
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
        }
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
assert.equal(layoutAnomaly(sample({ ready: false })), null,
    "the intentional blank before boot cannot become an incident");
assert.equal(layoutAnomaly(sample({ hidden: true })), null,
    "a background page is not diagnosed from geometry WebKit is not laying out");

console.log("web layout diagnostics tests passed");
