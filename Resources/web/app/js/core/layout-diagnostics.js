/* A black screen that disappears on reload leaves no DOM to inspect afterwards. This recorder
   keeps only layout and state-machine facts — never transcript text, titles, paths or ids — and
   persists recent stable anomalies so the next load can explain the one that just vanished. */

var TRACE_LIMIT = 80;
var INCIDENT_LIMIT = 5;
var STORAGE_KEY = "clawdline.layout-debug.last";
var DETAIL_KEY = "clawdline.layout-debug.detail";
var trace = [];
var context = null;
var ready = false;
var installed = false;
var scheduled = false;
var settleTimer = null;
var pending = null;
var activeAnomaly = null;
var last = null;
var incidents = [];
var lastDetail = null;
var lastDetailSignature = "";
var debugPanel = null;
// What the ring buffer has thrown away, and where it stopped. A trace that drops silently
// makes "the story starts here" and "the first half of the story is gone" render identically,
// and those are opposite readings of the same file.
var dropped = 0;
var droppedThrough = null;
// Recorders that live somewhere this page cannot see and are folded in when it wakes.
var sources = {};
var sourceOrder = [];

function clock() {
    return typeof performance !== "undefined" && performance.now
        ? Math.round(performance.now()) : Date.now();
}

function plain(value) {
    if (value == null || typeof value === "string" || typeof value === "number" ||
        typeof value === "boolean") return value;
    if (Array.isArray(value)) return value.slice(0, 12).map(plain);
    var out = {};
    Object.keys(value).slice(0, 20).forEach(function (key) {
        try { out[key] = plain(value[key]); } catch (e) { out[key] = "<unreadable>"; }
    });
    return out;
}

function append(event, data) {
    trace.push({ t: clock(), event: event, data: plain(data || {}) });
    if (trace.length > TRACE_LIMIT) {
        var over = trace.length - TRACE_LIMIT;
        droppedThrough = trace[over - 1].t;
        dropped += over;
        trace.splice(0, over);
    }
}

/**
 * One external recorder, by name. Declaring it is what makes its silence readable.
 *
 * The worker's half of a notification is written into Cache Storage while this page is not
 * running, and read back when it wakes. **Never read** and **read, and there was nothing** are
 * different faults with different fixes, and before this they produced the same empty trace —
 * which is the shape this machine has now paid for more than once. A source that has been
 * declared and never read says `unread`; one whose store is not there at all says `unavailable`,
 * which is a third thing again.
 *
 * Names are the caller's and this file knows none of them. A new observation point next month
 * declares its own and appears in the report without anything here or on the Mac changing.
 */
function sourceRow(name) {
    if (!sources[name]) {
        sources[name] = { name: name, state: "unread", entries: 0, reads: 0, at: null };
        sourceOrder.push(name);
    }
    return sources[name];
}

/** What this report knows it is missing, stated rather than left to be noticed. */
function completeness() {
    var rows = sourceOrder.map(function (name) { return sources[name]; });
    var waiting = rows.filter(function (row) { return row.state !== "merged"; });
    return {
        trace: {
            kept: trace.length, limit: TRACE_LIMIT, dropped: dropped,
            droppedThrough: droppedThrough,
            from: trace.length ? trace[0].t : null,
            to: trace.length ? trace[trace.length - 1].t : null
        },
        incidents: { kept: incidents.length, limit: INCIDENT_LIMIT },
        sources: rows,
        // True only when nothing was dropped and every declared source has actually been folded
        // in. It is not a promise that the fault is in here — no counter can say that — it is the
        // narrower claim that nothing this recorder knows about is missing from the file.
        whole: dropped === 0 && waiting.length === 0
    };
}

function rectOf(el, viewport) {
    if (!el || typeof el.getBoundingClientRect !== "function") {
        return { width: 0, height: 0, visibleArea: 0 };
    }
    var r = el.getBoundingClientRect();
    var left = Math.max(r.left, viewport.left);
    var top = Math.max(r.top, viewport.top);
    var right = Math.min(r.right, viewport.left + viewport.width);
    var bottom = Math.min(r.bottom, viewport.top + viewport.height);
    var style = typeof getComputedStyle === "function" ? getComputedStyle(el) : {};
    return {
        left: Math.round(r.left), top: Math.round(r.top), right: Math.round(r.right),
        bottom: Math.round(r.bottom), width: Math.round(r.width), height: Math.round(r.height),
        visibleArea: Math.max(0, Math.round(right - left)) * Math.max(0, Math.round(bottom - top)),
        display: style.display || "", visibility: style.visibility || "",
        opacity: style.opacity || "", transform: style.transform || "",
        position: style.position || "", zIndex: style.zIndex || "",
        pointerEvents: style.pointerEvents || "", overflow: style.overflow || "",
        background: style.backgroundColor || ""
    };
}

function hitStack(x, y) {
    var nodes = document.elementsFromPoint ? document.elementsFromPoint(x, y) :
        (document.elementFromPoint ? [document.elementFromPoint(x, y)] : []);
    return nodes.filter(Boolean).slice(0, 6).map(function (el) {
        var classes = typeof el.className === "string" ? el.className : "";
        return { tag: el.tagName || "", id: el.id || "", className: classes.slice(0, 160) };
    });
}

function visibleCount(nodes, viewport) {
    var count = 0;
    Array.prototype.forEach.call(nodes, function (el) {
        var r = el.getBoundingClientRect();
        var style = typeof getComputedStyle === "function" ? getComputedStyle(el) : {};
        if (style.display !== "none" && style.visibility !== "hidden" &&
            r.right > viewport.left && r.left < viewport.left + viewport.width &&
            r.bottom > viewport.top && r.top < viewport.top + viewport.height) count++;
    });
    return count;
}

function activeElement() {
    var el = document.activeElement;
    if (!el) return null;
    return {
        tag: el.tagName || "", id: el.id || "", type: el.type || "",
        editable: !!el.isContentEditable,
        inList: !!(el.closest && el.closest(".pane-list")),
        inDetail: !!(el.closest && el.closest(".pane-detail"))
    };
}

function stateSnapshot(reason) {
    if (!context) return null;
    var vv = window.visualViewport;
    var viewport = {
        left: Math.round(vv ? (vv.offsetLeft || 0) : 0),
        top: Math.round(vv ? (vv.offsetTop || 0) : 0),
        width: Math.round(vv ? vv.width : window.innerWidth),
        height: Math.round(vv ? vv.height : window.innerHeight),
        scale: vv ? vv.scale : 1,
        innerWidth: window.innerWidth, innerHeight: window.innerHeight
    };
    var els = context.elements;
    var state = context.state;
    var root = document.documentElement;
    var header = document.querySelector(".top");
    var list = document.querySelector(".pane-list");
    var detail = document.querySelector(".pane-detail");
    var tx = els.tx;
    var txScroll = els["tx-scroll"];
    var detailHead = els["detail-head"];
    var composer = els.composer;
    var rows = document.querySelectorAll(".pane-list .row");
    var centerX = Math.max(0, Math.round(viewport.width / 2));
    var centerY = Math.max(0, Math.round(viewport.height / 2));
    var centerNode = document.elementFromPoint ? document.elementFromPoint(centerX, centerY) : null;
    var connRect = els.conn && els.conn.getBoundingClientRect();
    var connX = connRect ? Math.round(connRect.left + connRect.width / 2) : viewport.width - 30;
    var connY = connRect ? Math.round(connRect.top + connRect.height / 2) : 26;
    var rootStyle = typeof getComputedStyle === "function" ? getComputedStyle(root) : null;
    return {
        format: 1,
        at: new Date().toISOString(), reason: reason,
        ready: ready, phone: window.matchMedia("(max-width: 899px)").matches,
        hidden: document.hidden,
        standalone: window.matchMedia("(display-mode: standalone)").matches,
        viewport: viewport,
        vvh: {
            inlineHeight: root.style.getPropertyValue("--vvh"),
            inlineTop: root.style.getPropertyValue("--vvt"),
            computedHeight: rootStyle ? rootStyle.getPropertyValue("--vvh").trim() : "",
            computedTop: rootStyle ? rootStyle.getPropertyValue("--vvt").trim() : ""
        },
        header: rectOf(header, viewport), body: rectOf(document.body, viewport),
        app: rectOf(els.app, viewport), list: rectOf(list, viewport),
        detail: rectOf(detail, viewport), detailHead: rectOf(detailHead, viewport),
        transcriptScroller: rectOf(txScroll, viewport), transcript: rectOf(tx, viewport),
        composer: rectOf(composer, viewport),
        view: els.app.dataset.view || "", pane: els.app.dataset.pane || "",
        panel: detail ? (detail.dataset.panel || "") : "", agent: !!state.agent,
        active: activeElement(),
        hasOpen: !!state.openId, selected: !!state.selectedId,
        sessions: state.sessions.length, arrived: !!state.arrived, conn: state.conn,
        rows: { dom: rows.length, visible: visibleCount(rows, viewport) },
        scroll: {
            pageX: Math.round(window.scrollX || 0), pageY: Math.round(window.scrollY || 0),
            transcriptTop: txScroll ? Math.round(txScroll.scrollTop) : 0,
            transcriptHeight: txScroll ? Math.round(txScroll.scrollHeight) : 0,
            transcriptClient: txScroll ? Math.round(txScroll.clientHeight) : 0
        },
        hit: {
            centerInDetail: !!(centerNode && centerNode.closest && centerNode.closest(".pane-detail")),
            center: hitStack(centerX, centerY), conn: hitStack(connX, connY)
        },
        tx: {
            belongsToOpen: !!state.openId && state.tx.id === state.openId,
            entries: (state.tx.entries || []).length,
            loading: !!state.tx.loading, error: !!state.tx.error
        },
        version: state.version || "",
        userAgent: navigator.userAgent
    };
}

/**
 * Where a report goes when somebody presses "Send to Mac".
 *
 * Written twice — here and as a `case` in `Sources/RemoteServer.swift` — because a route is a
 * string on both sides of a wire. `Tests/web-diagnostics-send.mjs` holds the two copies against
 * each other, for the same reason `Tests/web-notification-route.mjs` holds the worker's cache
 * name against the page's: two spellings of one name in two languages is the shape that drifts,
 * and the drift is silent.
 */
export var REPORT_ROUTE = "/v1/diagnostics/report";

export function layoutAnomaly(sample) {
    if (!sample || !sample.ready || !sample.phone || sample.hidden) return null;
    if (sample.header && sample.header.height >= 30 && sample.header.visibleArea > 0 &&
        sample.app && sample.app.height < 80) return "app_collapsed_below_header";
    if (sample.hasOpen && sample.view === "detail") {
        var detail = sample.detail || {};
        if (detail.display === "none" || detail.visibility === "hidden" || detail.opacity === "0") {
            return "detail_hidden";
        }
        if (detail.height < 100) return "detail_collapsed";
        if (!detail.visibleArea) return "detail_offscreen";
        if (sample.hit && sample.hit.centerInDetail === false) return "detail_not_hit_testable";
        if (!sample.panel) {
            var scroller = sample.transcriptScroller || {};
            if (scroller.display === "none" || scroller.visibility === "hidden" ||
                scroller.opacity === "0") return "transcript_scroller_hidden";
            if (scroller.height < 80) return "transcript_scroller_collapsed";
            if (!scroller.visibleArea) return "transcript_scroller_offscreen";
        }
    }
    if (!sample.hasOpen && sample.view === "list" && sample.sessions > 0 &&
        sample.rows && sample.rows.dom === 0) return "list_rows_missing";
    var list = sample.list || {}, pane = sample.detail || {};
    if (sample.app && sample.app.height >= 80 && !list.visibleArea && !pane.visibleArea) {
        return "both_panes_offscreen";
    }
    return null;
}

function readSaved() {
    try {
        var value = localStorage.getItem(STORAGE_KEY);
        if (!value) return [];
        var parsed = JSON.parse(value);
        return Array.isArray(parsed) ? parsed.slice(0, INCIDENT_LIMIT) : [parsed];
    } catch (e) { return []; }
}

function readLastDetail() {
    try {
        var value = localStorage.getItem(DETAIL_KEY);
        return value ? JSON.parse(value) : null;
    } catch (e) { return null; }
}

function rememberDetail(sample) {
    if (!sample || !sample.hasOpen || sample.view !== "detail") return;
    var signature = JSON.stringify({
        loading: sample.tx.loading, entries: sample.tx.entries,
        detail: sample.detail.visibleArea,
        scroller: sample.transcriptScroller.visibleArea,
        transcript: sample.transcript.visibleArea,
        head: sample.detailHead.visibleArea, composer: sample.composer.visibleArea,
        hit: sample.hit.centerInDetail,
        transform: sample.detail.transform, visibility: sample.detail.visibility
    });
    if (signature === lastDetailSignature) return;
    lastDetailSignature = signature;
    lastDetail = { sample: sample, trace: trace.slice() };
    try { localStorage.setItem(DETAIL_KEY, JSON.stringify(lastDetail)); } catch (e) { }
}

function save(kind, sample, event) {
    last = { kind: kind, event: event || null, sample: sample, trace: trace.slice() };
    incidents.unshift(last);
    incidents = incidents.slice(0, INCIDENT_LIMIT);
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(incidents)); } catch (e) { }
    drawDebug();
}

function shortLayout(sample) {
    if (!sample) return {};
    return {
        reason: sample.reason, view: sample.view, open: sample.hasOpen,
        bodyH: sample.body.height, appH: sample.app.height,
        listArea: sample.list.visibleArea, detailArea: sample.detail.visibleArea,
        detailHeadArea: sample.detailHead.visibleArea,
        scrollerArea: sample.transcriptScroller.visibleArea,
        transcriptArea: sample.transcript.visibleArea, composerArea: sample.composer.visibleArea,
        detailVisibility: sample.detail.visibility, detailTransform: sample.detail.transform,
        detailPointerEvents: sample.detail.pointerEvents,
        vvh: sample.vvh.inlineHeight, vvt: sample.vvh.inlineTop,
        innerH: sample.viewport.innerHeight, visualH: sample.viewport.height,
        active: sample.active && (sample.active.id || sample.active.tag),
        rows: sample.rows, centerInDetail: sample.hit.centerInDetail,
        centerHit: sample.hit.center[0] || null, connHit: sample.hit.conn[0] || null
    };
}

function probe(reason) {
    if (!ready || document.hidden) return;
    var sample = stateSnapshot(reason);
    if (!sample) return;
    append("layout", shortLayout(sample));
    rememberDetail(sample);
    var kind = layoutAnomaly(sample);
    var now = clock();
    if (!kind) { pending = null; activeAnomaly = null; drawDebug(); return; }
    if (kind === activeAnomaly) return;
    if (!pending || pending.kind !== kind) {
        pending = { kind: kind, at: now };
        return;
    }
    if (now - pending.at >= 300) {
        save(kind, sample);
        activeAnomaly = kind;
        pending = null;
    }
}

function schedule(reason) {
    if (!ready) return;
    if (!scheduled) {
        scheduled = true;
        requestAnimationFrame(function () {
            scheduled = false;
            probe(reason);
        });
    }
    clearTimeout(settleTimer);
    settleTimer = setTimeout(function () { probe(reason + ":settled"); }, 380);
}

function errorText(value) {
    if (!value) return "unknown";
    if (typeof value === "string") return value.slice(0, 1000);
    return String(value.stack || value.message || value).slice(0, 4000);
}

function captureError(kind, value) {
    var event = { message: errorText(value) };
    append(kind, event);
    var sample = ready ? stateSnapshot(kind) : null;
    save(kind, sample, event);
}

function report() {
    return {
        completeness: completeness(),
        current: ready ? stateSnapshot("debug_report") : null,
        savedIncidents: incidents.slice(),
        lastDetail: lastDetail,
        currentTrace: trace.slice()
    };
}

function drawDebug() {
    if (!debugPanel) return;
    var pre = debugPanel.querySelector("pre");
    var saved = incidents[0] || null;
    debugPanel.querySelector(".layout-debug-toggle").textContent =
        "LAYOUT DEBUG" + (saved ? " · " + saved.kind : "");
    if (!pre.hidden) pre.textContent = JSON.stringify(report(), null, 2);
}

function debugUI(force, reveal) {
    var params = new URLSearchParams(location.search);
    if (!force && params.get("debug") !== "layout") return;
    if (debugPanel) {
        if (reveal) {
            var existing = debugPanel.querySelector("section");
            var existingPre = debugPanel.querySelector("pre");
            existing.hidden = false; existingPre.hidden = false; drawDebug();
        }
        return;
    }
    var box = document.createElement("aside");
    box.id = "layout-debug";
    box.style.cssText = "position:fixed;left:8px;bottom:8px;z-index:2147483647;" +
        "max-width:calc(100vw - 16px);font:11px/1.35 ui-monospace,monospace;color:#f3eee9;";
    var toggle = document.createElement("button");
    toggle.type = "button"; toggle.className = "layout-debug-toggle";
    toggle.style.cssText = "padding:7px 9px;border:1px solid #8d695b;border-radius:7px;" +
        "background:#211b1a;color:#f2b49e;font:inherit";
    var panel = document.createElement("section");
    panel.hidden = true;
    panel.style.cssText = "margin-top:6px;width:min(720px,calc(100vw - 16px));padding:8px;" +
        "border:1px solid #5a4b46;border-radius:8px;background:rgba(14,14,17,.97)";
    var actions = document.createElement("div");
    actions.style.cssText = "display:flex;gap:6px;margin-bottom:6px";
    function button(label, fn) {
        var b = document.createElement("button");
        b.type = "button"; b.textContent = label;
        b.style.cssText = "padding:6px;border:1px solid #4b4b54;border-radius:6px;" +
            "background:#202027;color:#ddd;font:inherit";
        b.addEventListener("click", fn); actions.appendChild(b); return b;
    }
    // **The answer, in words, on the screen.** A button that only changes colour tells somebody
    // holding a phone that *something* happened; what they need is the path, so they can read it
    // to whoever asked for it — and, when it failed, which refusal it was. Selectable, wrapping,
    // and never cleared by the next render.
    var said = document.createElement("div");
    said.className = "layout-debug-said";
    said.hidden = true;
    said.style.cssText = "margin-bottom:6px;white-space:pre-wrap;word-break:break-all;" +
        "-webkit-user-select:text;user-select:text;color:#e8ded6";
    function say(text) { said.hidden = false; said.textContent = text; }
    var pre = document.createElement("pre");
    pre.hidden = false;
    pre.style.cssText = "max-height:55vh;margin:0;overflow:auto;white-space:pre-wrap;word-break:break-word";
    toggle.addEventListener("click", function () {
        panel.hidden = !panel.hidden; pre.hidden = panel.hidden; drawDebug();
    });
    button("Capture now", function () {
        var sample = stateSnapshot("manual"); save("manual_capture", sample);
    });
    var copy = button("Copy report", function () {
        var text = JSON.stringify(report(), null, 2);
        if (!navigator.clipboard || !navigator.clipboard.writeText) {
            copy.textContent = "Clipboard unavailable"; return;
        }
        navigator.clipboard.writeText(text).then(function () {
            copy.textContent = "Copied";
        }, function () { copy.textContent = "Copy failed"; });
    });
    var send = button("Send to Mac", function () {
        send.disabled = true;
        say("Sending to this Mac\u2026");
        var fetcher = typeof fetch === "function" ? fetch : null;
        if (!fetcher) { say("This browser has no fetch, so the report cannot be sent."); send.disabled = false; return; }
        fetcher(REPORT_ROUTE, {
            method: "POST", credentials: "same-origin",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(report())
        }).then(function (res) {
            return res.text().then(function (text) {
                var data = null;
                try { data = text ? JSON.parse(text) : null; } catch (e) { data = null; }
                if (res.ok && data && data.path) {
                    // The path is the server's, never this file's guess at it: a page that prints
                    // a path it composed itself is a page that can name a file nobody wrote.
                    say("Written to " + data.path + "\n" + data.bytes + " bytes" +
                        (data.previous ? "\nthe press before it: " + data.previous : "") +
                        (data.completeness_stated ? "" : "\nthis build sent no completeness block"));
                    return;
                }
                var err = data && data.error;
                say(err ? "Refused: " + err.code + " \u2014 " + err.message
                        : "Refused: http_" + res.status + " \u2014 " + (text || "no answer"));
            });
        }).catch(function (e) {
            say("Could not reach this Mac: " + ((e && e.message) || "no answer"));
        }).then(function () { send.disabled = false; });
    });
    button("Clear saved", function () {
        last = null; incidents = []; lastDetail = null; lastDetailSignature = "";
        trace = []; dropped = 0; droppedThrough = null;
        try {
            localStorage.removeItem(STORAGE_KEY); localStorage.removeItem(DETAIL_KEY);
        } catch (e) { }
        drawDebug();
    });
    panel.appendChild(actions); panel.appendChild(said); panel.appendChild(pre);
    box.appendChild(toggle); box.appendChild(panel);
    document.documentElement.appendChild(box);
    debugPanel = box;
    if (reveal) { panel.hidden = false; pre.hidden = false; }
    drawDebug();
}

function observe() {
    ["resize", "orientationchange", "pageshow", "popstate", "hashchange"].forEach(function (name) {
        window.addEventListener(name, function () { Diagnostics.note("window." + name); });
    });
    ["visibilitychange", "focusin", "focusout"].forEach(function (name) {
        document.addEventListener(name, function () { Diagnostics.note("document." + name); }, true);
    });
    document.addEventListener("click", function (event) {
        var target = event.target;
        Diagnostics.note("document.click", {
            tag: target && target.tagName, id: target && target.id,
            row: !!(target && target.closest && target.closest(".row"))
        });
    }, true);
    if (typeof ResizeObserver === "function") {
        var ro = new ResizeObserver(function () { Diagnostics.note("resizeObserver"); });
        [document.body, context.elements.app, document.querySelector(".pane-list"),
         document.querySelector(".pane-detail")].forEach(function (el) { if (el) ro.observe(el); });
    }
    if (typeof MutationObserver === "function") {
        var mo = new MutationObserver(function (records) {
            Diagnostics.note("layoutMutation", { count: records.length });
        });
        [document.documentElement, document.body, context.elements.app,
         document.querySelector(".pane-list"), document.querySelector(".pane-detail")]
            .forEach(function (el) {
                if (el) mo.observe(el, { attributes: true,
                    attributeFilter: ["class", "style", "hidden", "data-view", "data-pane"] });
            });
    }
}

export var Diagnostics = {
    bind: function (value) {
        if (installed) return;
        installed = true; context = value; incidents = readSaved(); last = incidents[0] || null;
        lastDetail = readLastDetail();
        window.__clawdlineDiagnostics = Diagnostics;
        window.addEventListener("error", function (event) {
            captureError("javascript_error", event.error || event.message);
        });
        window.addEventListener("unhandledrejection", function (event) {
            captureError("unhandled_rejection", event.reason);
        });
        observe(); debugUI();
    },
    ready: function () { ready = true; append("boot.ready"); schedule("boot.ready"); },
    note: function (event, data) { append(event, data); schedule(event); },
    /**
     * Declare a recorder that lives somewhere this page cannot see.
     *
     * Call it once, at module load, for anything that will later be folded into this trace. That
     * is what buys the report the difference between "not folded in yet" and "there was nothing
     * to fold in" — and the caller names it, so a new observation point next month needs no
     * change here and none on the Mac.
     */
    source: function (name) { sourceRow(name); },
    /**
     * What became of one read of that recorder. `merged` with a count, `unavailable` when the
     * store it lives in is not there, `failed` when reading it threw or gave back nonsense.
     */
    sourceRead: function (name, state, entries) {
        var row = sourceRow(name);
        row.state = state || "merged";
        row.reads += 1;
        row.entries += typeof entries === "number" ? entries : 0;
        row.at = clock();
    },
    completeness: completeness,
    capture: function (reason) {
        var sample = stateSnapshot(reason || "manual"); save("manual_capture", sample); return sample;
    },
    /**
     * Put the panel on screen without `?debug=layout`.
     *
     * **The query string is not reachable from the one device this exists for.** A home-screen
     * web app launches at the manifest's `start_url` and has no address bar, so on the phone
     * where the notification is actually tapped there has never been a way to ask for this
     * panel — the recorder was running and the reading was unreachable. `input/settings.js`
     * calls this after five presses on the version line, which is the oldest idiom there is for
     * a door that must not be found by accident and must not need a word on the screen.
     */
    reveal: function () { debugUI(true, true); },
    report: report
};
