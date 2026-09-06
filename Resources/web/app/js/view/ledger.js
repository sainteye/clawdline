import { T, fill } from "../core/i18n.js";

/* ==========================================================================
   The verification ledger

   One page, one question, asked per Feature: **what did reviewing this find,
   what did proving it cost, and how did the tokens divide between building it
   and reading it.** `GET /v1/orchestrator/usage/verification-ledger` answers
   it out of the four receipt tables store version 6 added, joined to the
   interval rows beside them.

   **The screen's subject is not a number, it is which of three answers a
   number is.** Every figure here arrives in one of three states and leaves as
   one of three different things:

   - `present` — the figure, drawn as a figure;
   - `absent`  — *no record*, in its own words and its own colour;
   - `unknown` — *not measurable*, in a third set of words again.

   None of the branches below can put a `0` on screen for the last two, and
   that is the whole of why this page exists. The finding that started this
   line measured 20.4% of this Mac's tokens carrying a Feature key, with 48
   graphs that would have reported `0` — a page drawing "nobody recorded it"
   as "it cost nothing" *is* the defect, printed larger.

   **And a fourth word, for a fourth gap.** `undeclared` is a row whose task
   never said which side of the work it was on. It is a token bucket of its
   own, never added to the implementation figure beside it, because
   `LEFT JOIN … WHEN NULL THEN 'implementation'` is a claim about a side that
   nothing on the row supports — and it lands hardest exactly where a review's
   receipt failed to write.

   **`absent` is the weakest sentence on this page and is worded like it.**
   A receipt whose write failed leaves a log line and no column, so "this Mac
   holds no review receipt for this Feature" is all it may be read as. Nothing
   here says a Feature was never reviewed.

   **The block that names no Feature is drawn first**, above every Feature and
   not inside any of them. The backfill that fills `graph_id` runs when the app
   starts, so on a Mac that has not relaunched this is most of the store — and
   every figure below it is smaller than the truth by exactly that much. A page
   that hid it would be quietly answering a different question.

   This module imports nothing but the words, so `Tests/web-ledger.mjs` can
   drive the whole of it against a stand-in document. The transport arrives
   through `bindLedgerPage`'s second argument: the read does not exist on the
   Cloud path, so over that transport the page says so rather than drawing a
   list that cannot be filled.
   ========================================================================== */

/**
 * What Escape means here, reached from the one module that owns the Escape chain.
 *
 * Not a listener of this module's own — see the note on `Projects.escape`: a second listener on
 * the same document fires after `input/keys.js` has already closed the drawer and returned, and
 * takes the page with it. No stand-in document can catch that.
 */
export var Ledger = { escape: function () { } };

/** The token buckets, in the order they are drawn. `undeclared` is last and is never merged. */
var ROLES = ["implementation", "review", "undeclared"];

function number(value) {
    return new Intl.NumberFormat().format(value);
}

function clear(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
}

function appendText(doc, parent, tag, value, className) {
    var node = doc.createElement(tag);
    if (className) node.className = className;
    node.textContent = value;
    parent.appendChild(node);
    return node;
}

function day(value) {
    if (!value) return "";
    var at = new Date(value);
    if (isNaN(at.getTime())) return String(value);
    return at.toLocaleDateString();
}

/**
 * **The one function this page exists for.**
 *
 * A reading arrives as `{state, …}` and leaves as one of three different things on screen, in
 * three different words and under three different class names. The number is read out of the
 * payload field only on the `present` branch, and that field is `null` on the other two — so
 * there is no path through here that can render an unknown as `0`, and no branch that draws two
 * of the three the same way.
 *
 * `title` carries the sentence saying what the state means, so the difference survives a reader
 * who does not already know the vocabulary.
 */
function drawState(doc, parent, state, value) {
    if (state === "present" && value !== null && value !== undefined) {
        return appendText(doc, parent, "span", number(value), "ledger-figure");
    }
    if (state === "unknown") {
        var unknown = appendText(doc, parent, "span", T.webLedgerNotMeasurable, "ledger-unknown");
        unknown.title = T.webLedgerNotMeasurableSay;
        return unknown;
    }
    var absent = appendText(doc, parent, "span", T.webLedgerNoRecord, "ledger-absent");
    absent.title = T.webLedgerNoRecordSay;
    return absent;
}

/**
 * One token bucket.
 *
 * **A floor and a total are two quantities and both reach the screen.** A bucket holding one row
 * that measured three of its four parts has no total at all, and the payload says so by sending
 * `total: null` beside a `measured` that is still a real number. Drawn as *at least n*, never as
 * `n` — a figure a reader could add up with the one beside it is a figure that has to be the
 * same kind of thing.
 */
function drawTokens(doc, parent, role, reading) {
    var row = doc.createElement("div");
    row.className = "ledger-token ledger-token-" + role;
    var name = appendText(doc, row, "span", roleName(role), "ledger-token-name");
    if (role === "undeclared") name.title = T.webLedgerUndeclaredSay;
    if (!reading) {
        drawState(doc, row, "absent", null);
        parent.appendChild(row);
        return row;
    }
    if (reading.state === "present" && (reading.total === null || reading.total === undefined)) {
        var floor = appendText(doc, row, "span",
                               fill(T.webLedgerAtLeast, { n: number(reading.measured || 0) }),
                               "ledger-floor");
        floor.title = fill(T.webLedgerFloorSay, { n: number(reading.incompleteRows || 0) });
    } else {
        drawState(doc, row, reading.state, reading.total);
    }
    appendText(doc, row, "span", fill(T.webLedgerRows, { n: number(reading.rows || 0) }),
               "ledger-token-rows");
    parent.appendChild(row);
    return row;
}

function roleName(role) {
    if (role === "implementation") return T.webLedgerImplementation;
    if (role === "review") return T.webLedgerReview;
    return T.webLedgerUndeclared;
}

/** The severity distribution, worst first — the order the payload already put them in. */
function drawSeverities(doc, parent, severities) {
    for (var i = 0; i < (severities || []).length; i++) {
        var one = severities[i];
        appendText(doc, parent, "span", one.severity + " " + number(one.count),
                   "ledger-severity ledger-severity-" + String(one.severity).replace(/\W+/g, "-"));
    }
}

/**
 * One Feature's card.
 *
 * The heading is the graph id, because that is the only name this side holds: the destination a
 * graph was dispatched with lives in the task registry, which is swept. An id under a word saying
 * it is one beats a label that is right for a fortnight and blank afterwards.
 */
function drawFeature(context, parent, feature, options) {
    var doc = context.document;
    var card = doc.createElement("section");
    card.className = "ledger-card";

    var head = doc.createElement("div");
    head.className = "ledger-card-head";
    appendText(doc, head, "span", T.webLedgerFeature, "ledger-card-kind");
    var identity = feature.graphId
        ? appendText(doc, head, "code", feature.graphId, "ledger-card-id")
        : appendText(doc, head, "span", T.webLedgerUnattributed, "ledger-card-id ledger-absent");
    // **Through `fill`, like the eight other holed strings on this page.** This one was the ninth
    // and the only one used raw, so the first word of the tooltip was the characters `{rows}` —
    // in all fourteen languages, because every translation of it carries the same hole.
    if (!feature.graphId) {
        identity.title = fill(T.webLedgerUnattributedSay,
                              { rows: number(feature.rows || 0) });
    }
    card.appendChild(head);

    var facts = doc.createElement("div");
    facts.className = "ledger-facts";
    appendText(doc, facts, "span", fill(T.webLedgerTasks, { n: number(feature.tasks || 0) }),
               "ledger-fact");
    var seen = day(feature.lastSeenAt);
    if (seen) {
        appendText(doc, facts, "span", T.webLedgerSeen + " " + seen, "ledger-fact");
    }
    card.appendChild(facts);

    // Findings. `present` with a total of zero is a Feature somebody reviewed and found nothing
    // in, which is the answer worth reaching and is said in those words rather than as "0".
    var findings = doc.createElement("div");
    findings.className = "ledger-row ledger-row-findings";
    appendText(doc, findings, "span", T.webLedgerFindings, "ledger-row-name");
    var found = feature.findings || {};
    if (found.state === "present" && found.total === 0) {
        appendText(doc, findings, "span", T.webLedgerReviewedClean, "ledger-clean");
    } else {
        drawState(doc, findings, found.state, found.total);
        drawSeverities(doc, findings, found.severities);
    }
    card.appendChild(findings);

    // Verification. Runs and seconds are one sentence, because a run count with no seconds beside
    // it reads as an achievement rather than as a cost.
    var proving = doc.createElement("div");
    proving.className = "ledger-row ledger-row-verification";
    appendText(doc, proving, "span", T.webLedgerVerification, "ledger-row-name");
    var verification = feature.verification || {};
    if (verification.state === "present") {
        appendText(doc, proving, "span", fill(T.webLedgerVerificationSay, {
            runs: number(verification.runs || 0),
            seconds: number(verification.seconds || 0),
            red: number(verification.endedRed || 0),
        }), "ledger-figure");
    } else {
        drawState(doc, proving, verification.state, null);
    }
    card.appendChild(proving);

    var tokens = doc.createElement("div");
    tokens.className = "ledger-tokens";
    appendText(doc, tokens, "span", T.webLedgerTokens, "ledger-row-name");
    for (var i = 0; i < ROLES.length; i++) {
        drawTokens(doc, tokens, ROLES[i], (feature.tokens || {})[ROLES[i]]);
    }
    card.appendChild(tokens);

    if (options && options.open && feature.graphId) {
        var open = doc.createElement("button");
        open.className = "ledger-open";
        open.type = "button";
        open.textContent = fill(T.webLedgerOpenLabel, { name: feature.graphId });
        open.addEventListener("click", function () { options.open(feature.graphId); });
        card.appendChild(open);
    }
    parent.appendChild(card);
    return card;
}

/** The list, with the block that names no Feature standing above it. */
function renderList(context, ledger) {
    var doc = context.document;
    var elements = context.elements;
    clear(elements["ledger-rows"]);
    clear(elements["ledger-unattributed"]);
    elements["ledger-unattributed"].hidden = true;
    if (!ledger) {
        elements["ledger-count"].textContent = "";
        return;
    }
    var read = ledger.read || {};
    elements["ledger-count"].textContent = fill(T.webLedgerRead, {
        rows: number(read.rowsScanned || 0), features: number(read.featuresFound || 0),
    }) + (read.truncated ? " " + T.webLedgerTruncated : "");

    // Above the list, always, whenever it holds anything: every figure below it is short by
    // exactly this much, and the backfill that would move these rows runs at app launch.
    // **Three sources on the route's side, so three questions here.** The block is grown from
    // interval rows, from review receipts and from verification receipts; asking about two of
    // them threw the third away whole, and a Feature-less verification receipt — its runs, its
    // seconds, the times it ended red — vanished behind a row count of nought. A screen must not
    // re-derive what the payload is made of; when this list grows again, it grows here too.
    var unattributed = ledger.unattributed;
    if (unattributed && (unattributed.rows > 0
                         || (unattributed.findings || {}).reviewReceipts > 0
                         || (unattributed.verification || {}).receipts > 0)) {
        elements["ledger-unattributed"].hidden = false;
        appendText(doc, elements["ledger-unattributed"], "h3", T.webLedgerUnattributed,
                   "ledger-block-name");
        appendText(doc, elements["ledger-unattributed"], "p",
                   fill(T.webLedgerUnattributedSay, { rows: number(unattributed.rows || 0) }),
                   "ledger-block-say");
        drawFeature(context, elements["ledger-unattributed"], unattributed, null);
    }

    var features = ledger.features || [];
    if (!features.length) {
        elements["ledger-status"].textContent = T.webLedgerEmpty;
        return;
    }
    elements["ledger-status"].textContent = "";
    for (var i = 0; i < features.length; i++) {
        drawFeature(context, elements["ledger-rows"], features[i], { open: context.open });
    }
}

/** One Feature, with the findings themselves. */
function renderDetail(context, feature) {
    var doc = context.document;
    var elements = context.elements;
    clear(elements["ledger-detail-rows"]);
    if (!feature) return;
    elements["ledger-detail-title"].textContent = feature.graphId || T.webLedgerUnattributed;
    drawFeature(context, elements["ledger-detail-rows"], feature, null);

    var verdicts = feature.verdicts || [];
    if (verdicts.length) {
        var block = doc.createElement("div");
        block.className = "ledger-row ledger-row-verdicts";
        appendText(doc, block, "span", T.webLedgerVerdicts, "ledger-row-name");
        for (var v = 0; v < verdicts.length; v++) {
            appendText(doc, block, "span", verdicts[v].verdict + " " + number(verdicts[v].count),
                       "ledger-verdict");
        }
        elements["ledger-detail-rows"].appendChild(block);
    }

    var axes = feature.axes || [];
    if (axes.length) {
        var axisBlock = doc.createElement("div");
        axisBlock.className = "ledger-row ledger-row-axes";
        appendText(doc, axisBlock, "span", T.webLedgerAxes, "ledger-row-name");
        for (var a = 0; a < axes.length; a++) {
            appendText(doc, axisBlock, "span",
                       axes[a].axis + " · " + axes[a].status + " · " + number(axes[a].findingCount),
                       "ledger-axis ledger-axis-" + String(axes[a].status).replace(/\W+/g, "-"));
        }
        elements["ledger-detail-rows"].appendChild(axisBlock);
    }

    var items = feature.items || [];
    for (var i = 0; i < items.length; i++) {
        var item = items[i];
        var finding = doc.createElement("article");
        finding.className = "ledger-finding ledger-finding-"
            + String(item.severity).replace(/\W+/g, "-");
        appendText(doc, finding, "span", item.severity, "ledger-severity");
        appendText(doc, finding, "code", item.findingId, "ledger-finding-id");
        appendText(doc, finding, "p", item.summary, "ledger-finding-summary");
        var evidence = item.evidence || [];
        if (evidence.length) {
            var list = doc.createElement("ul");
            list.className = "ledger-evidence";
            appendText(doc, list, "li", T.webLedgerEvidence, "ledger-evidence-name");
            for (var e = 0; e < evidence.length; e++) {
                appendText(doc, list, "li", evidence[e], "ledger-evidence-item");
            }
            finding.appendChild(list);
        }
        elements["ledger-detail-rows"].appendChild(finding);
    }
}

/**
 * What a refusal says, in the reader's own words.
 *
 * A code this build has no sentence for keeps the server's own message rather than being
 * flattened into "something went wrong": the server is the only thing that knows what happened.
 */
function refusalText(error) {
    var code = error && (error.code || (error.body && error.body.code));
    if (code === "graph_not_found") return T.webLedgerNotFound;
    var message = error && (error.message || (error.body && error.body.message));
    return message ? String(message) : T.webLedgerFailed;
}

export function bindLedgerPage(elements, environment) {
    environment = environment || {};
    var doc = environment.document || document;
    var state = { view: "list", ledger: null, graphID: null, loading: 0 };
    var context = {
        document: doc, elements: elements, state: state,
        open: function (graphID) { openFeature(graphID); },
    };
    /* Absent on the Cloud path, like the two Projects reads and for the same reason: what a
       paired viewer may name carries a session, and this read's subject is a Feature. Asked when
       the page is used rather than when it is bound — `net/api.js` holds a live binding the entry
       point fills in, twice on the Cloud path. */
    var readLedger = typeof environment.verificationLedger === "function"
        ? environment.verificationLedger : null;
    var carries = typeof environment.carries === "function"
        ? environment.carries
        : function () { return !!readLedger; };

    function showView(view) {
        state.view = view;
        elements["ledger-list-view"].hidden = view !== "list";
        elements["ledger-detail-view"].hidden = view !== "detail";
    }

    /**
     * The list.
     *
     * **Everything on screen comes off before the request goes out.** A receipt left over from
     * the last read sitting above a refusal is the one shape this page must not produce: it is
     * the difference between "scanned 726 rows and found none" and "this was never answered",
     * and it is the difference the route went to trouble to keep.
     */
    function load() {
        state.ledger = null;
        renderList(context, null);
        if (!readLedger || !carries()) {
            elements["ledger-status"].textContent = T.webLedgerUnavailable;
            return Promise.resolve();
        }
        var ticket = ++state.loading;
        elements["ledger-status"].textContent = T.webLedgerLoading;
        return readLedger().then(function (data) {
            if (ticket !== state.loading) return;
            state.ledger = (data && data.verificationLedger) || null;
            elements["ledger-status"].textContent = "";
            renderList(context, state.ledger);
        }).catch(function (error) {
            if (ticket !== state.loading) return;
            elements["ledger-status"].textContent = refusalText(error);
        });
    }

    function openFeature(graphID) {
        state.graphID = graphID;
        showView("detail");
        elements["ledger-detail-title"].textContent = graphID;
        clear(elements["ledger-detail-rows"]);
        elements["ledger-detail-status"].textContent = "";
        if (!readLedger || !carries()) {
            elements["ledger-detail-status"].textContent = T.webLedgerUnavailable;
            return Promise.resolve();
        }
        var ticket = ++state.loading;
        elements["ledger-detail-status"].textContent = T.webLedgerLoading;
        return readLedger(graphID).then(function (data) {
            if (ticket !== state.loading) return;
            elements["ledger-detail-status"].textContent = "";
            renderDetail(context, (data && data.verificationLedger || {}).feature);
        }).catch(function (error) {
            if (ticket !== state.loading) return;
            elements["ledger-detail-status"].textContent = refusalText(error);
        });
    }

    function backToList() {
        showView("list");
        state.graphID = null;
        var title = elements["ledger-title"];
        if (title && title.focus) title.focus({ preventScroll: true });
    }

    function enter() {
        showView("list");
        state.graphID = null;
        return load();
    }

    function leave() { }

    elements["ledger-back"].addEventListener("click", backToList);

    var navigate = environment.navigate || function () { };
    Ledger.escape = function () {
        if (state.view === "detail") { backToList(); return; }
        navigate("sessions");
    };

    showView("list");
    return { enter: enter, leave: leave, load: load, openFeature: openFeature,
             escape: Ledger.escape, state: state };
}
