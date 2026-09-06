/**
 * The Plan page, which is the only screen in this product that can start a payment.
 *
 * Two halves. The first drives the real `view/plan.js` against the real `net/billing.js` with a
 * stand-in document and a stand-in `fetch`, through every answer `api/src/routes/billing.ts` can
 * give. The second holds the document, `main.js` and the drawer against each other, which is
 * where a page nobody can reach or an element nobody defined would otherwise sit unnoticed.
 *
 * **What this suite is actually for.** A billing screen fails in ways that look like success:
 * the dangerous states are the ones where nothing throws. Four of them are asserted here by
 * name, because each has a way of passing a test that only checks "an error was shown":
 *
 *   1. **The checkout could not be created.** A 500 from Lemon Squeezy through
 *      `api/src/server.ts` is an untyped `internal`, and a page that draws it as a spinner has
 *      told somebody their payment is in progress when no checkout exists.
 *   2. **The webhook has not arrived.** The redirect back from Lemon Squeezy proves a form was
 *      submitted; only `handleBillingWebhook` changes the entitlement. A page that congratulates
 *      on the redirect is wrong for as long as the gap lasts, and the gap is real.
 *   3. **Already subscribed, and the upgrade pressed anyway.** This is the one failure the page
 *      can *cause* rather than report: `createCheckout` does not look for an existing
 *      subscription, so a stale screen sells a second one and Lemon Squeezy takes the money.
 *   4. **Not on sale.** `purchasable` is the server's list. Max and Team are off it deliberately
 *      (`docs/RUNBOOK-DEPLOY.md` §1) and the button has to be off the page with them.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const read = (relative) => readFileSync(join(root, relative), "utf8");
const module_ = (relative) => import("file://" + join(root, relative));

let checks = 0;
let failed = false;
function check(condition, message) {
    checks += 1;
    if (condition) return;
    failed = true;
    console.error(`FAIL: ${message}`);
}
function equal(actual, expected, message) {
    check(actual === expected, `${message} — expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}
function contains(haystack, needle, message) {
    check(String(haystack).includes(needle),
          `${message} — ${JSON.stringify(String(haystack))} does not contain ${JSON.stringify(needle)}`);
}

/* ==========================================================================
   A document, in as much as this page reads one
   ========================================================================== */

class FakeNode {
    constructor(doc, tag = "div", id = "") {
        this.ownerDocument = doc;
        this.tagName = String(tag).toUpperCase();
        this.id = id;
        this.children = [];
        this.parentNode = null;
        this.listeners = {};
        this.attributes = {};
        this.hidden = false;
        this.disabled = false;
        this._text = "";
    }
    appendChild(child) { child.parentNode = this; this.children.push(child); return child; }
    removeChild(child) {
        this.children = this.children.filter((item) => item !== child);
        child.parentNode = null;
    }
    get firstChild() { return this.children[0] || null; }
    get textContent() { return this._text + this.children.map((child) => child.textContent).join(""); }
    set textContent(value) { this._text = String(value ?? ""); this.children = []; }
    addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
    click() { for (const handler of this.listeners.click || []) handler({}); }
    setAttribute(name, value) { this.attributes[name] = String(value); }
    getAttribute(name) { return this.attributes[name] ?? null; }
}

class FakeDocument {
    createElement(tag) { return new FakeNode(this, tag); }
}

const IDS = [
    "plan", "plan-title", "plan-lede", "plan-close", "plan-includes", "plan-tier",
    "plan-tier-note", "plan-say", "plan-alert", "plan-limits", "plan-fine", "plan-upgrade",
    "plan-portal", "plan-signin", "plan-retry", "plan-recheck", "plan-elsewhere", "plan-console",
];

function elements() {
    const doc = new FakeDocument();
    const table = {};
    for (const id of IDS) table[id] = new FakeNode(doc, id === "plan-limits" ? "dl" : "div", id);
    return table;
}

/** A clock the suite turns by hand, so the wait for a webhook is deterministic. */
function clock() {
    let queue = [];
    return {
        setTimeout(fn) { queue.push(fn); return queue.length; },
        clearTimeout() { queue = []; },
        pending() { return queue.length; },
        async tick() {
            const due = queue;
            queue = [];
            for (const fn of due) fn();
            await flush();
        },
    };
}

const flush = async () => {
    for (let i = 0; i < 6; i += 1) await new Promise((done) => setImmediate(done));
};

/* ==========================================================================
   The answers the control plane can give
   ========================================================================== */

const FREE = {
    tier: "free", max_machines: 1, max_concurrent_sessions: 3, max_viewer_devices: 2,
    dispatches_per_month: 50, history_days: 0, fleet: false, team: false, quotas_provisional: true,
};
const PRO = {
    tier: "pro", max_machines: 5, max_concurrent_sessions: null, max_viewer_devices: 5,
    dispatches_per_month: 1000, history_days: 30, fleet: false, team: false, quotas_provisional: true,
};

function entitlements(tier, purchasable = ["pro"]) {
    return {
        status: 200,
        body: {
            account_id: "acct_1", entitlements: tier === "pro" ? PRO : FREE,
            purchasable_tiers: purchasable,
        },
    };
}
const SIGNED_OUT = { status: 401, body: { error: { code: "no_session", message: "Sign in first" } } };

/** A `fetch` that answers from a script, and records what it was asked. */
function transport(script) {
    const calls = [];
    return {
        calls,
        fetch(url, init) {
            const path = String(url).replace("https://api.example", "");
            calls.push({ path, method: (init && init.method) || "GET" });
            const answer = typeof script === "function" ? script(path, calls.length) : script[path];
            if (!answer) throw new Error(`no scripted answer for ${path}`);
            return Promise.resolve({
                status: answer.status,
                json: () => (answer.body === undefined
                    ? Promise.reject(new Error("not json")) : Promise.resolve(answer.body)),
            });
        },
    };
}

const { createBillingClient } = await module_("Resources/web/app/js/net/billing.js");
const { bindPlanPage, planRows, checkoutSentence, portalSentence } = await module_("Resources/web/app/js/view/plan.js");
const { T } = await module_("Resources/web/app/js/core/i18n.js");

function page(script, extra = {}) {
    const table = elements();
    const wire = transport(script);
    const time = clock();
    const navigations = [];
    const view = bindPlanPage(table, Object.assign({
        billing: createBillingClient({ apiOrigin: "https://api.example", fetch: wire.fetch }),
        signInURL: () => "https://api.example/v1/auth/oauth/start",
        navigate: (url) => navigations.push(url),
        setTimeout: time.setTimeout, clearTimeout: time.clearTimeout,
        pollIntervalMs: 1, pollAttempts: 3,
    }, extra));
    return { table, wire, time, navigations, view };
}

/* ==========================================================================
   1. The ordinary readings
   ========================================================================== */

{
    const it = page({ "/v1/entitlements": entitlements("free") });
    await it.view.enter();
    equal(it.view.state(), "free", "an account on Free lands on the state that can be upgraded");
    equal(it.table["plan-tier"].textContent, "Free", "and the plan it is on is the loud thing on the page");
    equal(it.table["plan-upgrade"].hidden, false, "the upgrade button is on screen");
    equal(it.table["plan-portal"].hidden, true, "and the portal, which needs a subscription, is not");
    check(it.table["plan-limits"].children.length === 14,
          "seven rows of entitlement are drawn, each a term and a value");
    contains(it.table["plan-limits"].textContent, "50", "the dispatch ceiling the server sent is on the page");
    equal(it.table["plan-fine"].hidden, false,
          "and `quotas_provisional` is carried out to the reader rather than dropped");
}

{
    const it = page({ "/v1/entitlements": entitlements("pro") });
    await it.view.enter();
    equal(it.view.state(), "paid", "a paying account lands on the state that manages a subscription");
    equal(it.table["plan-upgrade"].hidden, true,
          "**and is never offered the upgrade it already has** — this is the press that sells a second one");
    equal(it.table["plan-portal"].hidden, false, "the customer portal is the control it gets instead");
    contains(it.table["plan-say"].textContent, "Pro", "and the sentence names the plan");
}

{
    const it = page({ "/v1/entitlements": SIGNED_OUT });
    await it.view.enter();
    equal(it.view.state(), "signed_out", "a signed-out browser is a state, not an error");
    equal(it.table["plan-signin"].hidden, false, "and it is offered the way in");
    equal(it.table["plan-alert"].hidden, true, "with nothing in the alert line: being signed out is not a fault");
    it.table["plan-signin"].click();
    equal(it.navigations[0], "https://api.example/v1/auth/oauth/start",
          "pressing it is a top-level navigation to OAuth, which is the only way the cookie comes back");
}

/* ==========================================================================
   2. Not on sale — `purchasable` is the server's list, not this page's constant
   ========================================================================== */

{
    const it = page({ "/v1/entitlements": entitlements("free", []) });
    await it.view.enter();
    equal(it.view.state(), "free_not_for_sale",
          "with nothing purchasable the page is still a page, and says so");
    equal(it.table["plan-upgrade"].hidden, true,
          "**the button is absent rather than failing on the press** — Max and Team stay unpurchasable by runbook §1");
    equal(it.wire.calls.filter((call) => call.path === "/v1/billing/checkout").length, 0,
          "and no checkout is ever asked for");
}

/* ==========================================================================
   3. The upgrade, and the four ways it does not work
   ========================================================================== */

{
    const it = page({
        "/v1/entitlements": entitlements("free"),
        "/v1/billing/checkout": { status: 200, body: { url: "https://store.lemonsqueezy.com/checkout/x", session_id: "co_1" } },
    });
    await it.view.enter();
    it.table["plan-upgrade"].click();
    await flush();
    equal(it.navigations[0], "https://store.lemonsqueezy.com/checkout/x",
          "a working upgrade leaves for the URL the control plane returned, and for no other");
    const checkout = it.wire.calls.find((call) => call.path === "/v1/billing/checkout");
    equal(checkout.method, "POST", "the checkout is created with a POST, under the session cookie");
}

{
    // (1) Lemon Squeezy is down, or refused. `api/src/server.ts` turns the gateway's plain Error
    // into an untyped 500, which is precisely the answer a page is most likely to draw as nothing.
    const it = page({
        "/v1/entitlements": entitlements("free"),
        "/v1/billing/checkout": { status: 500, body: { error: { code: "internal", message: "Something went wrong" } } },
    });
    await it.view.enter();
    it.table["plan-upgrade"].click();
    await flush();
    equal(it.view.state(), "checkout_failed", "a 500 from the checkout is a state the page has a name for");
    equal(it.navigations.length, 0, "**nothing is navigated to** — there is no checkout to navigate to");
    equal(it.table["plan-alert"].hidden, false, "the failure is in the alert line, not the calm one");
    contains(it.table["plan-alert"].textContent, "Nothing was charged",
             "and it says the thing somebody in front of a failed payment needs first");
    equal(it.table["plan-retry"].hidden, false, "with a way to try again");
    equal(it.table["plan-say"].hidden, true, "and the reassuring line is gone, not left underneath");
}

{
    // (3) The press that sells a second subscription. The page has been open long enough for its
    // reading to go stale — a phone left on this screen — and the account became Pro meanwhile.
    let plan = entitlements("free");
    const it = page((path) => (path === "/v1/entitlements" ? plan : { status: 200, body: { url: "https://store.lemonsqueezy.com/checkout/second" } }));
    await it.view.enter();
    equal(it.view.state(), "free", "the page is showing Free, because that is what it was told");
    plan = entitlements("pro");                       // the webhook landed while nobody looked
    it.table["plan-upgrade"].click();
    await flush();
    equal(it.view.state(), "already_paid",
          "**pressing upgrade re-reads the plan first, and stops** when the account already pays");
    equal(it.wire.calls.filter((call) => call.path === "/v1/billing/checkout").length, 0,
          "**no second checkout is created** — this is the one failure this page could cause rather than report");
    equal(it.navigations.length, 0, "and nothing leaves for Lemon Squeezy");
    contains(it.table["plan-alert"].textContent, "Pro", "the reader is told which plan they already have");
    equal(it.table["plan-portal"].hidden, false, "and is given the control that manages it");
}

{
    const it = page({
        "/v1/entitlements": entitlements("free"),
        "/v1/billing/checkout": { status: 409, body: { error: { code: "not_purchasable", message: "max is not on sale yet" } } },
    });
    await it.view.enter();
    it.table["plan-upgrade"].click();
    await flush();
    contains(it.table["plan-alert"].textContent, "not on sale",
             "a typed 409 keeps its own sentence rather than becoming the generic one");
    check(checkoutSentence("missing_variant", "pro") !== checkoutSentence("not_purchasable", "pro"),
          "and the six refusals are six sentences: a page that merges them has thrown the typing away");
    check(checkoutSentence("free_tier") !== checkoutSentence("unknown_user"),
          "including the two nobody expects to see");
}

{
    const it = page({
        "/v1/entitlements": entitlements("free"),
        "/v1/billing/checkout": SIGNED_OUT,
    });
    await it.view.enter();
    it.table["plan-upgrade"].click();
    await flush();
    equal(it.table["plan-signin"].hidden, false,
          "a session that expired between the read and the press offers the way back in, not a retry that would fail again");
}

/* ==========================================================================
   4. Coming back from Lemon Squeezy — the gap between the redirect and the webhook
   ========================================================================== */

{
    // The webhook lands on the second poll. This is the ordinary happy return.
    let answers = 0;
    const it = page(() => (++answers >= 2 ? entitlements("pro") : entitlements("free")));
    await it.view.enter({ returning: true });
    equal(it.view.state(), "waiting", "the redirect puts the page in a wait, not in a celebration");
    contains(it.table["plan-say"].textContent, "Waiting",
             "**and says it is waiting** — the redirect proves a form was submitted, nothing more");
    equal(it.table["plan-tier"].textContent !== "Pro", true,
          "the plan is not drawn as Pro before the control plane has said so");
    await it.time.tick();
    equal(it.view.state(), "waiting",
          "a poll that still answers Free leaves the page waiting rather than guessing");
    await it.time.tick();
    equal(it.view.state(), "paid", "when the entitlement changes, the page changes with it");
    contains(it.table["plan-say"].textContent, "confirmed", "and only then is the payment called confirmed");
}

{
    // (2) The webhook never arrives. The wait runs out, and what is on screen has to be true.
    const it = page({ "/v1/entitlements": entitlements("free") });
    await it.view.enter({ returning: true });
    await it.time.tick();
    await it.time.tick();
    await it.time.tick();
    equal(it.view.state(), "not_confirmed", "a wait that runs out is its own state");
    equal(it.table["plan-alert"].hidden, false, "and it is said in the alert line");
    contains(it.table["plan-alert"].textContent, "Free",
             "**naming the plan the account is actually still on** rather than the one that was paid for");
    contains(it.table["plan-alert"].textContent, "not lost",
             "while saying the payment is not lost, because it is not: the confirmation is retried");
    equal(it.table["plan-recheck"].hidden, false, "with a way to ask again");
    equal(it.table["plan-limits"].children.length, 14,
          "and the entitlement is still drawn: waiting is not a reason to stop saying what somebody has");
}

{
    const it = page({ "/v1/entitlements": entitlements("free") });
    await it.view.enter({ returning: true });
    it.view.leave();
    equal(it.time.pending(), 0, "leaving the page stops the wait; a poll that outlives it paints a screen nobody is on");
}

/* ==========================================================================
   5. The portal, and the copy of this page the Mac itself serves
   ========================================================================== */

{
    const it = page({
        "/v1/entitlements": entitlements("pro"),
        "/v1/billing/portal": { status: 409, body: { error: { code: "no_subscription", message: "This account has no billing subscription" } } },
    });
    await it.view.enter();
    it.table["plan-portal"].click();
    await flush();
    contains(it.table["plan-alert"].textContent, "no subscription",
             "a portal refused for want of a subscription says that, and not `409`");
    equal(it.navigations.length, 0, "nothing navigates on a refusal");
    // The reassurance belongs to the codes where there *is* something to reassure about. Saying
    // "your subscription is unaffected" to an account that has none is noise dressed as care.
    contains(portalSentence("portal_unavailable"), "unaffected",
             "a portal that would not open says the subscription behind it is unaffected");
    check(!portalSentence("no_subscription").includes("unaffected"),
          "and the account with no subscription is not told about one");
}

{
    const it = page({ "/v1/entitlements": entitlements("free") }, { billing: null });
    await it.view.enter();
    equal(it.view.state(), "not_here",
          "the copy of this page the Mac serves has no Cloud account, and says so rather than half-working");
    equal(it.table["plan-elsewhere"].hidden, false, "the explanation is on screen");
    equal(it.table["plan-console"].getAttribute("href"), "https://app.clawdline.com/#page=plan",
          "with the one link that leads to a page where the upgrade does exist");
    equal(it.table["plan-upgrade"].hidden, true, "and no button that cannot work");
}

{
    const it = page({ "/v1/entitlements": { status: 503, body: null } });
    await it.view.enter();
    equal(it.view.state(), "unavailable", "an unreadable plan is a state and not a blank page");
    contains(it.table["plan-alert"].textContent, "has not changed",
             "and it says the one thing that is certainly true: nothing about the plan moved");
}

/* ==========================================================================
   6. The rows, and the fair-use meter the pricing page also carries
   ========================================================================== */

{
    const rows = planRows(PRO);
    equal(rows.length, 7, "seven rows, the pricing page's own list");
    const dispatches = rows.find((row) => row[0] === T.webPlanRowDispatches);
    contains(dispatches[1], "1000", "the ceiling the server sent");
    contains(dispatches[1], "fair-use", "**named a meter and not a bill** — D7, and the same words the pricing page uses");
    const sessions = rows.find((row) => row[0] === T.webPlanRowSessions);
    equal(sessions[1], T.webPlanNoLimit,
          "a null limit reads as no fixed limit rather than as `null`, and never as `unlimited`: the relay still has ceilings");
}

/* ==========================================================================
   7. The page, the registry and the drawer, held against each other
   ========================================================================== */

const page_ = read("Resources/web/index.html");
const mainSource = read("Resources/web/app/js/main.js");

for (const id of IDS) {
    check(page_.includes(`id="${id}"`), `the document defines #${id}, which the page looks up`);
}
check(/data-page-view="plan"/.test(page_), "the section declares itself a page");
check(/data-page-to="plan"/.test(page_), "and one control in the drawer names it");
check(/{ name: "plan", element: byId\("plan"\)/.test(mainSource), "the registry in main.js carries it");
check(/leave: function \(\) \{[\s\S]{0,160}?plan\.leave\(\);/.test(mainSource),
      "with a `leave` that stops the page, because it starts a timer and a page that keeps one "
      + "must put it back");
check(mainSource.includes('location.pathname === "/billing/done"'),
      "main.js recognises the success URL `api/src/services/billing.ts` sends to Lemon Squeezy");
check(/consumeCheckoutReturn/.test(mainSource),
      "and consumes it once: arriving at the Plan page again is an ordinary arrival, not a second wait");
check(mainSource.includes('history.replaceState(history.state, "", "/#page=plan")'),
      "the return path is written out of the address, so a reload is not a second wait either");
check(/planBilling = createBillingClient\(\{ apiOrigin: cloudConfig\.apiOrigin \}\)/.test(mainSource),
      "the client is built only on the cloud transport, from the build's declared API origin");
check(/var planBilling = null;/.test(mainSource),
      "and is null everywhere else, which is what the `not_here` state is drawn from");

const linked = [...page_.matchAll(/href="\/app\/css\/([^"]+)"/g)].map((m) => m[1]);
check(linked.includes("plan.css"), "the page's stylesheet is linked");
check(linked.indexOf("pages.css") < linked.indexOf("plan.css"),
      "after pages.css, which owns the frame this page sits in");

const staticSource = read("Resources/web/app/js/view/static.js");
check(staticSource.includes('text(els["nav-plan"], T.webPlan)'),
      "the drawer row is painted from the string table like every other row");

const planSource = read("Resources/web/app/js/view/plan.js");
check(!/["'][A-Z][a-z]+ [a-z]+ [a-z]+/.test(planSource.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*/g, "")),
      "**no sentence is written into the module as a literal**: this is the screen that charges a card, "
      + "and it speaks whatever language the rest of the interface does");
const billingSource = read("Resources/web/app/js/net/billing.js");
check(billingSource.includes('credentials: "include"'),
      "every billing call carries the session cookie: the console and the API are different origins");
for (const code of ["not_purchasable", "missing_variant", "free_tier", "unknown_user", "no_session"]) {
    check(planSource.includes(code) || billingSource.includes(code),
          `the typed refusal ${code} is one the page knows by name`);
}

/* ==========================================================================
   8. The two doors this page had to be moved out from behind

   Both were found by a person trying to pay and getting nowhere, which is the
   only reason they are asserted here: neither was visible from inside the Plan
   page's own tests, because in both cases the page is *correct* and simply
   never reached.
   ========================================================================== */

const doorSource = read("Resources/web/app/js/input/cloud-pairing.js");

check(/data-page-to="plan"/.test(page_.slice(page_.indexOf('id="cloud-door"'))),
      "the cloud door carries a way to the Plan page — paying needs the session cookie and "
      + "nothing the door is about");
check(/id="cloud-door-plan"/.test(page_), "and the document defines that control");
check(doorSource.includes('"cloud-door-plan"'),
      "the door's control table hides it with the rest, so it cannot leak between screens");
check((doorSource.match(/offerPlan\(\);/g) || []).length === 2,
      "it is offered by exactly the two screens that block a browser which is already signed in: "
      + "no account key yet, and the viewer-device limit");
check(/function offerPlan[\s\S]*?T\.webPlanFromGate/.test(doorSource),
      "labelled from the string table like everything else on this path");
check(/door\.hidden = deferred/.test(doorSource),
      "**the door respects the deferral when it redraws** — a pairing poll that keeps redrawing "
      + "would otherwise jump over the page somebody is paying on");
check(/export function deferCloudGate/.test(doorSource) && /export function showCloudGate/.test(doorSource),
      "stepping aside and coming back are both named");
check(/deferCloudGate\(\);\s+plan\.enter/.test(mainSource),
      "arriving at the Plan page steps the door aside");
check(/if \(cloudGateUp\) showCloudGate\(\);/.test(mainSource),
      "and leaving brings it back, because what it was about has not been answered");
check(/cloudGateUp = false;\s+hideCloudGate\(\);/.test(mainSource),
      "a connected viewer clears it, so the door does not come back after pairing succeeds");
check((mainSource.match(/cloudGateUp = true;/g) || []).length === 2,
      "raised by exactly the two states that block a signed-in browser");

// The QR decoder's worker. `default-src 'none'` with no `worker-src` forbids every worker,
// including the blob worker `qr-scanner` falls back to when there is no `BarcodeDetector` — which
// on Safari is always. The camera opened, the preview ran, and nothing ever decoded.
const buildSource = read("tools/build-web-app.py");
check(/"worker-src 'self' blob:"/.test(buildSource),
      "**the console's CSP allows the QR decoder's worker**: qr-scanner falls back to "
      + "`new Worker(URL.createObjectURL(new Blob([…])))`, and Safari has no BarcodeDetector to "
      + "take the other branch, so without this pairing by QR cannot work on an iPhone at all");
check(read("Resources/web/app/js/vendor/qr-scanner-worker.min.js").includes("URL.createObjectURL"),
      "and that is really how the bundled decoder builds it — asserted against the vendored file "
      + "rather than remembered, because the day it stops being a blob worker this line should say so");

console.log(`${failed ? "not ok" : "ok"}: web billing and the Plan page, ${checks} checks`);
if (failed) process.exit(1);
