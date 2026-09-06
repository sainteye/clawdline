/**
 * The verification ledger page.
 *
 * The same two halves `Tests/web-projects.mjs` has. The first drives the real `view/ledger.js`
 * against a stand-in document; the second holds the document, `main.js` and the registry against
 * each other, which is where a page nobody can reach or an element nobody defined would sit
 * unnoticed.
 *
 * **What the first half is for is one sentence: the three states have to be three different
 * things on the screen.** `present` is a figure, `absent` is the words *no record*, `unknown` is
 * the words *not measurable* — and none of the three may render as `0`. That is not a style
 * preference. The route goes to real trouble to keep them apart, the finding this whole line
 * came from is a page that would have drawn 48 Features as `0`, and a front end is exactly where
 * that work is thrown away: one `|| 0` and all three become the same grey rectangle.
 *
 * So every assertion below either compares two of the three states with each other, or asserts
 * that a digit is absent from what was drawn. A check that only asked "is the text right" would
 * stay green through the defect this page exists to prevent.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const read = (relative) => readFileSync(join(root, relative), "utf8");

let checks = 0;
let failed = false;
function check(condition, message) {
    checks += 1;
    if (condition) return;
    failed = true;
    console.error(`FAIL: ${message}`);
}
function equal(actual, expected, message) {
    check(actual === expected,
          `${message} — expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

/* ==========================================================================
   A document, in as much as this module reads one
   ========================================================================== */

class FakeNode {
    constructor(doc, tag = "div", id = "") {
        this.ownerDocument = doc;
        this.tagName = String(tag).toUpperCase();
        this.id = id;
        this.children = [];
        this.parentNode = null;
        this.listeners = {};
        this.style = {};
        this.attributes = {};
        this.className = "";
        this.hidden = false;
        this.title = "";
        this._text = "";
    }
    appendChild(child) { child.parentNode = this; this.children.push(child); return child; }
    removeChild(child) {
        this.children = this.children.filter((item) => item !== child);
        child.parentNode = null;
    }
    get firstChild() { return this.children[0] || null; }
    get textContent() {
        return this._text + this.children.map((child) => child.textContent).join("");
    }
    set textContent(value) { this._text = String(value ?? ""); this.children = []; }
    addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
    dispatch(name, event = {}) {
        event.preventDefault ||= () => { event.defaultPrevented = true; };
        for (const handler of this.listeners[name] || []) handler(event);
    }
    click() { this.dispatch("click", {}); }
    focus(options) {
        this.focusCalls = (this.focusCalls || 0) + 1;
        this.focusOptions = options || null;
        this.ownerDocument.activeElement = this;
    }
    setAttribute(name, value) { this.attributes[name] = String(value); }
    getAttribute(name) { return this.attributes[name] ?? null; }
    all(className) {
        const found = [];
        const visit = (node) => {
            if (String(node.className).split(/\s+/).includes(className)) found.push(node);
            for (const child of node.children) visit(child);
        };
        visit(this);
        return found;
    }
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

const flush = async () => {
    for (let i = 0; i < 4; i += 1) await new Promise((done) => setImmediate(done));
};

/* ==========================================================================
   The fixtures — one Feature per state, and never two that differ only in size
   ========================================================================== */

function tokens(state, rows, measured, total, extra) {
    return Object.assign({ state, rows, unknownRows: state === "unknown" ? rows : 0,
                           incompleteRows: 0, reasons: [], measured, total }, extra || {});
}

function feature(graphId, extra) {
    return Object.assign({
        graphId, rows: 6, tasks: 4,
        findings: { state: "absent", reviewReceipts: 0, total: null, severities: [],
                    truncated: false },
        verification: { state: "absent", receipts: 0, runs: null, seconds: null, endedRed: null,
                        scopes: [] },
        verdicts: [],
        tokens: { implementation: tokens("absent", 0, null, null),
                  review: tokens("absent", 0, null, null),
                  undeclared: tokens("absent", 0, null, null) },
        firstSeenAt: "2026-09-05T09:08:09Z", lastSeenAt: "2026-09-06T14:13:12Z",
    }, extra || {});
}

/** Everything present and measured: the Feature a reader has no trouble with. */
const MEASURED = feature("graph-measured", {
    findings: { state: "present", reviewReceipts: 2, total: 3,
                severities: [{ severity: "blocking", count: 1 }, { severity: "minor", count: 2 }],
                truncated: false },
    verification: { state: "present", receipts: 3, runs: 6, seconds: 1840, endedRed: 1,
                    scopes: ["swift suite"] },
    tokens: { implementation: tokens("present", 4, 8412300, 8412300),
              review: tokens("present", 2, 3155900, 3155900),
              undeclared: tokens("absent", 0, null, null) },
});

/** Nobody has reviewed it, and its rows measured nothing: the two states most easily lost. */
const BLANK = feature("graph-blank", {
    tokens: { implementation: tokens("unknown", 2, null, null),
              review: tokens("absent", 0, null, null),
              undeclared: tokens("absent", 0, null, null) },
});

/** Reviewed and clean, with one bucket that has a floor and no total. */
const CLEAN = feature("graph-clean", {
    findings: { state: "present", reviewReceipts: 1, total: 0, severities: [], truncated: false },
    verification: { state: "present", receipts: 1, runs: 1, seconds: 288, endedRed: 0,
                    scopes: ["web"] },
    tokens: { implementation: tokens("present", 3, 1204880, null, { incompleteRows: 1 }),
              review: tokens("present", 1, 402100, 402100),
              undeclared: tokens("present", 1, 90200, 90200) },
});

/**
 * **A payload that contradicts itself**, which is the only fixture that can tell whether the
 * screen trusts the state word or the number beside it.
 *
 * Production never sends this: `payload(of reading:)` writes `null` into `measured` and `total`
 * for every state but `present`, in one place, and that is exactly why the screen's own guard was
 * untestable — with the route holding the line, `state === "present"` could be deleted from
 * `drawState` and all 81 checks stayed green. What was keeping three answers apart was the route
 * being the only producer, not anything on this side.
 *
 * So this Feature says `unknown` and sends a number anyway. Nothing here may draw it.
 */
const LYING = feature("graph-lying", {
    findings: { state: "unknown", reviewReceipts: 4, total: 7, severities: [], truncated: false },
    verification: { state: "unknown", receipts: 2, runs: 9, seconds: 613, endedRed: 3,
                    scopes: [] },
    tokens: { implementation: tokens("unknown", 5, 5432100, 5432100),
              review: tokens("absent", 0, 998877, 998877),
              undeclared: tokens("unknown", 1, null, 424242) },
});

const UNATTRIBUTED = feature(null, {
    rows: 812,
    tokens: { implementation: tokens("present", 640, 51204880, 51204880),
              review: tokens("present", 150, 20118400, 20118400),
              undeclared: tokens("unknown", 22, null, null) },
});

function payload(overrides) {
    return {
        verificationLedger: Object.assign({
            schemaVersion: 1,
            features: [MEASURED, CLEAN, BLANK],
            unattributed: UNATTRIBUTED,
            read: { rowsScanned: 1240, featuresFound: 3, featuresListed: 3, truncated: false,
                    at: "2026-09-06T15:00:00Z" },
        }, overrides || {}),
    };
}

const IDS = ["ledger", "ledger-list-view", "ledger-detail-view", "ledger-title", "ledger-count",
             "ledger-status", "ledger-rows", "ledger-unattributed", "ledger-back",
             "ledger-detail-title", "ledger-detail-status", "ledger-detail-rows"];

function page(environment) {
    const doc = new FakeDocument();
    const elements = {};
    for (const id of IDS) elements[id] = doc.body.appendChild(new FakeNode(doc, "div", id));
    return { doc, elements, environment: Object.assign({ document: doc }, environment || {}) };
}

/** The one thing no state but `present` may contain. */
function hasDigit(text) { return /\d/.test(String(text)); }

/**
 * **Every string that reached the screen**, tooltips included, so the check below can be asked of
 * all of them at once rather than of the one that was noticed.
 */
function drawnStrings(node, out = []) {
    if (node.title) out.push(node.title);
    if (!node.children.length && node.textContent) out.push(node.textContent);
    for (const child of node.children) drawnStrings(child, out);
    return out;
}

/**
 * A hole that never got filled.
 *
 * `core/i18n.js`'s `fill` leaves a placeholder it has no value for exactly as it found it, which
 * is the right thing for a missing value and the wrong thing to look at: the reader sees `{rows}`
 * where a number belongs. A string used without `fill` at all does the same, and that is what
 * happened here — one of the nine `webLedger*` uses on this page was the sentence a tooltip
 * carried, in all fourteen languages, with the brace still in it.
 *
 * So this is asked of the whole screen and not of that one tooltip. The next string with a hole
 * in it will be drawn by a branch nobody is looking at either.
 */
const PLACEHOLDER = /\{[A-Za-z][A-Za-z0-9_]*\}/;
function noPlaceholders(where, node) {
    const drawn = drawnStrings(node);
    const unfilled = drawn.filter((text) => PLACEHOLDER.test(text));
    check(drawn.length > 0, `${where}: something was drawn for this scan to read`);
    check(unfilled.length === 0,
          `${where}: ${unfilled.length} of ${drawn.length} strings reached the screen with a hole `
          + `still in them — ${JSON.stringify(unfilled)}`);
}

async function main() {
    const { bindLedgerPage, Ledger } = await import(
        pathToFileURL(join(root, "Resources/web/app/js/view/ledger.js")).href);

    /* ---- the three states, drawn as three different things ---------------- */
    {
        const { doc, elements, environment } = page({
            verificationLedger: () => Promise.resolve(payload()),
        });
        const view = bindLedgerPage(elements, environment);
        await view.enter();
        await flush();

        const cards = elements["ledger-rows"].all("ledger-card");
        equal(cards.length, 3, "one card per Feature");

        // A measured Feature's implementation tokens are a figure and nothing else.
        const measuredTokens = cards[0].all("ledger-token");
        equal(measuredTokens.length, 3,
              "three buckets, and the undeclared one is never merged into the two beside it");
        const implementation = measuredTokens[0].all("ledger-figure");
        equal(implementation.length, 1, "a measured bucket draws a figure");
        check(hasDigit(implementation[0].textContent), "and the figure carries the number");

        // The Feature nobody reviewed. **Both of its empty states are on this card and they are
        // not the same words**, which is the assertion the whole page hangs on.
        const blank = cards[2];
        const absent = blank.all("ledger-absent");
        const unknown = blank.all("ledger-unknown");
        check(absent.length > 0, "a Feature with no receipts draws the absent state");
        check(unknown.length > 0, "and rows that measured nothing draw the unknown state");
        check(absent[0].textContent !== unknown[0].textContent,
              "the two are different words, not one grey rectangle for both");
        check(absent[0].className !== unknown[0].className,
              "and they are told apart by class as well as by wording");
        for (const node of absent.concat(unknown)) {
            check(!hasDigit(node.textContent),
                  `neither empty state contains a digit — got ${JSON.stringify(node.textContent)}`);
        }
        equal(blank.all("ledger-figure").length, 0,
              "and nothing on that card is drawn as a figure at all");
        // The sentence that keeps `absent` from being read as a claim.
        check(/log/i.test(absent[0].title),
              "the absent state says a failed receipt write leaves only a log line");

        // Reviewed and found nothing is its own answer, in words, and never the digit 0.
        const clean = cards[1];
        equal(clean.all("ledger-clean").length, 1,
              "a Feature reviewed with no findings says so rather than showing a count");
        check(!hasDigit(clean.all("ledger-clean")[0].textContent),
              "and that sentence carries no digit either");

        // A bucket that measured part of what it spent is a floor, said as one.
        const floor = clean.all("ledger-floor");
        equal(floor.length, 1, "a bucket with no total draws a floor");
        check(floor[0].textContent !== "" && floor[0].className !== "ledger-figure",
              "and a floor is not drawn as a total");
        check(hasDigit(floor[0].textContent), "the floor still carries the number it is a floor of");

        // The block that names no Feature stands above the list, outside every card.
        check(!elements["ledger-unattributed"].hidden,
              "records naming no Feature are shown, not dropped");
        check(elements["ledger-unattributed"].all("ledger-block-say").length === 1,
              "with the sentence saying every figure below is short by that much");
        equal(elements["ledger-rows"].all("ledger-card")
                  .filter((card) => card.all("ledger-card-id")
                      .some((id) => id.className.includes("ledger-absent"))).length, 0,
              "and it is not one of the Features in the list");
        check(hasDigit(elements["ledger-count"].textContent),
              "the read receipt says how much was scanned");

        // Every string on the list, tooltips included. Nine `webLedger*` strings carry holes and
        // eight of them were filled; the ninth was a tooltip, and it printed `{rows}`.
        noPlaceholders("the list", doc.body);
        const tooltips = [];
        (function walk(node) {
            if (node.title) tooltips.push(node.title);
            for (const child of node.children) walk(child);
        })(elements["ledger-unattributed"]);
        check(tooltips.some(hasDigit),
              "the block's own tooltip says how many rows it stands for, as a number");
    }

    /* ---- the state word decides, not the number beside it ----------------- */
    {
        /* **This is the assertion the page's own sentence claims and nothing was making true.**
           "The number is read out of the payload field only on the `present` branch, and that
           field is `null` on the other two" — the second half of that was doing all the work.
           Delete `state === "present"` from `drawState` and every check above stays green,
           because no fixture ever handed it a number the state said not to draw. One does now. */
        const { elements, environment } = page({
            verificationLedger: () => Promise.resolve(payload({ features: [LYING] })),
        });
        const view = bindLedgerPage(elements, environment);
        await view.enter();
        await flush();
        const card = elements["ledger-rows"].all("ledger-card")[0];
        equal(card.all("ledger-figure").length, 0,
              "a state that is not `present` draws no figure, whatever number came with it");
        equal(card.all("ledger-floor").length, 0, "and no floor either");
        const empties = card.all("ledger-unknown").concat(card.all("ledger-absent"));
        check(empties.length >= 4,
              "every quantity on that card is drawn as one of the two empty states");
        for (const node of empties) {
            check(!hasDigit(node.textContent),
                  `and none of them carries a digit — got ${JSON.stringify(node.textContent)}`);
        }
        check(!/5,?432,?100|424,?242|998,?877/.test(card.textContent),
              "none of the numbers the payload contradicted itself with reached the screen");
    }

    /* ---- an empty answer, a refusal, and the moment before either --------- */
    {
        const { elements, environment } = page({
            verificationLedger: () => Promise.resolve(payload({ features: [], unattributed:
                feature(null, { rows: 0 }) })),
        });
        const view = bindLedgerPage(elements, environment);
        await view.enter();
        await flush();
        equal(elements["ledger-rows"].children.length, 0, "an empty answer lists nothing");
        check(elements["ledger-status"].textContent !== "",
              "and says so in words rather than leaving the page blank");
        check(hasDigit(elements["ledger-count"].textContent),
              "with the receipt still on screen, which is what makes it an answer");
        check(elements["ledger-unattributed"].hidden,
              "and an unattributed block with nothing in it is not drawn");
    }
    {
        /* **The block is grown from three sources on the route's side, so it is shown from three
           here.** The route adds to it from interval rows, from review receipts and from
           verification receipts; a gate that asks about two of the three throws the third away
           whole — its runs, its seconds and the times it ended red, hidden behind `rows > 0`
           being false. Each of the three is asked on its own below, because a gate that answers
           correctly for the wrong reason is what a fixture carrying all three would leave. */
        const only = (extra) => payload({
            features: [], unattributed: feature(null, Object.assign({ rows: 0 }, extra)),
        });
        const shows = async (extra) => {
            const { elements, environment } = page({
                verificationLedger: () => Promise.resolve(only(extra)),
            });
            const view = bindLedgerPage(elements, environment);
            await view.enter();
            await flush();
            return !elements["ledger-unattributed"].hidden;
        };
        check(await shows({ verification: { state: "present", receipts: 3, runs: 6, seconds: 900,
                                            endedRed: 1, scopes: ["swift suite"] } }),
              "records naming no Feature are shown when all this Mac holds for them is "
              + "verification receipts");
        check(await shows({ findings: { state: "present", reviewReceipts: 2, total: 1,
                                        severities: [{ severity: "minor", count: 1 }],
                                        truncated: false } }),
              "and when all it holds for them is review receipts");
        check(await shows({ rows: 4 }), "and when all it holds for them is interval rows");
        check(!(await shows({})), "and not when it holds none of the three");
    }
    {
        /* **The refusal has to arrive after an answer, or the assertion below proves nothing.**
           A page that never clears the last read's receipt looks identical to one that does,
           until a second read fails — and then the numbers from the answer that worked are still
           on screen above the sentence saying this one did not. That is the shape this page must
           not produce, and it is only reachable from a read that succeeded first. */
        let refuse = false;
        const { elements, environment } = page({
            verificationLedger: () => (refuse
                ? Promise.reject(Object.assign(new Error("nope"), { code: "graph_not_found" }))
                : Promise.resolve(payload())),
        });
        const view = bindLedgerPage(elements, environment);
        await view.enter();
        await flush();
        check(hasDigit(elements["ledger-count"].textContent),
              "the first read leaves a receipt on screen");
        equal(elements["ledger-rows"].all("ledger-card").length, 3, "and its Features");

        refuse = true;
        await view.load();
        await flush();
        equal(elements["ledger-count"].textContent, "",
              "a refusal clears the receipt rather than leaving the last answer's numbers up");
        equal(elements["ledger-rows"].all("ledger-card").length, 0,
              "and takes the last answer's Features off the screen with it");
        check(elements["ledger-unattributed"].hidden,
              "including the block above them");
        check(elements["ledger-status"].textContent !== "", "and says what was refused");
    }
    {
        // A transport that does not carry this read says so rather than drawing an empty list.
        const { elements, environment } = page({});
        const view = bindLedgerPage(elements, environment);
        await view.enter();
        await flush();
        equal(elements["ledger-rows"].children.length, 0, "no transport draws no rows");
        equal(elements["ledger-count"].textContent, "", "and no receipt");
        check(elements["ledger-status"].textContent !== "",
              "with a sentence saying this connection cannot read it");
    }

    /* ---- one Feature, and the step Escape has to take inside the page ----- */
    {
        let asked = null;
        const detail = feature("graph-measured", {
            findings: MEASURED.findings, verification: MEASURED.verification,
            tokens: MEASURED.tokens,
            verdicts: [{ verdict: "changes_required", count: 1 }],
            axes: [{ taskId: "t", axis: "specification", status: "findings", findingCount: 1 }],
            items: [{ findingId: "F1", severity: "blocking", summary: "an unknown became a zero",
                      axis: "specification", taskId: "t", evidence: ["Sources/X.swift:1"] }],
        });
        const { doc, elements, environment } = page({
            verificationLedger: (graphID) => {
                asked = graphID ?? null;
                return Promise.resolve(graphID
                    ? { verificationLedger: { schemaVersion: 1, feature: detail } }
                    : payload());
            },
            navigate: (name) => { doc.wentTo = name; },
        });
        const view = bindLedgerPage(elements, environment);
        await view.enter();
        await flush();
        equal(asked, null, "the list is asked for without a graph");

        elements["ledger-rows"].all("ledger-open")[0].click();
        await flush();
        equal(asked, "graph-measured", "opening one Feature asks the route for that Feature");
        check(!elements["ledger-detail-view"].hidden, "and shows the detail view");
        check(elements["ledger-list-view"].hidden, "with the list put away");
        equal(elements["ledger-detail-rows"].all("ledger-finding").length, 1,
              "the findings themselves are drawn");
        equal(elements["ledger-detail-rows"].all("ledger-evidence-item").length, 1,
              "with the evidence the reviewer named");
        equal(elements["ledger-detail-rows"].all("ledger-axis").length, 1,
              "and which axes were answered");

        // Escape is a step inside the page before it is a way out of it.
        Ledger.escape();
        check(!elements["ledger-list-view"].hidden, "the first Escape gives the list back");
        equal(doc.wentTo, undefined, "and does not leave the page as well");
        Ledger.escape();
        equal(doc.wentTo, "sessions", "the second Escape leaves");
        check(!/addEventListener\s*\(\s*["']keydown/
            .test(read("Resources/web/app/js/view/ledger.js")),
              "and this module owns no key listener of its own: the Escape chain is one place");
    }
    {
        // The detail view draws strings the list never reaches, so it is scanned separately.
        const detail = feature("graph-measured", {
            findings: MEASURED.findings, verification: MEASURED.verification,
            tokens: CLEAN.tokens,
            verdicts: [{ verdict: "changes_required", count: 1 }],
            axes: [{ taskId: "t", axis: "specification", status: "findings", findingCount: 1 }],
            items: [{ findingId: "F1", severity: "minor", summary: "a hole nobody filled",
                      axis: "specification", taskId: "t", evidence: ["Sources/X.swift:1"] }],
        });
        const { doc, elements, environment } = page({
            verificationLedger: (graphID) => Promise.resolve(graphID
                ? { verificationLedger: { schemaVersion: 1, feature: detail } }
                : payload()),
        });
        const view = bindLedgerPage(elements, environment);
        await view.enter();
        await flush();
        await view.openFeature("graph-measured");
        await flush();
        noPlaceholders("one Feature's detail", doc.body);
    }

    /* ---- the page, the drawer and the registry, held against each other --- */
    {
        const index = read("Resources/web/index.html");
        const main = read("Resources/web/app/js/main.js");
        const staticView = read("Resources/web/app/js/view/static.js");
        const keys = read("Resources/web/app/js/input/keys.js");
        check(/data-page-to="ledger"/.test(index), "the drawer has a row that names this page");
        check(/id="nav-ledger"/.test(index), "with an id its paint can find");
        check(/text\(els\["nav-ledger"\], T\.webLedger\)/.test(staticView),
              "and that row is painted from the words rather than left as the English in markup");
        check(/data-page-view="ledger"/.test(index), "the document has the section");
        check(/name:\s*"ledger"/.test(main), "and `main.js` registers it, or nothing can reach it");
        check(/Pages\.current\(\)\s*===\s*"ledger"/.test(keys),
              "the Escape chain gives this page its turn, in the one place Escape is ordered");
        for (const id of IDS) {
            check(new RegExp(`id="${id}"`).test(index), `index.html defines #${id}`);
            check(main.includes(`byId("${id}")`), `main.js hands #${id} to the page`);
        }
        check(read("Resources/web/app/css/ledger.css").includes(".ledger-unknown"),
              "the unknown state has a style of its own, not the absent one's");
        check(/verificationLedger/.test(read("Resources/web/app/js/net/live.js")),
              "the local transport carries the read");
        check(/verificationLedger/.test(read("Resources/web/app/js/net/mock.js")),
              "and the fixtures do, so the page can be looked at without a Mac");
    }

    if (failed) {
        console.error(`web ledger: ${checks} checks, with failures`);
        process.exit(1);
    }
    console.log(`ok: web verification ledger, ${checks} checks`);
}

main();
