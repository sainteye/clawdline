/* --------------------------------------------------------------------------
   The Plan page: the one door in this app that leads to a payment

   Everything else on this screen is a reading. This is the only place a person
   can start something that ends with money leaving their bank, so the whole
   module is written around one rule: **the screen never says something the
   control plane has not said.**

   That rule is what most of the states below are. "Pro is active" is only ever
   drawn after `/v1/entitlements` has answered `pro` — never because Lemon
   Squeezy redirected back, because the redirect and the webhook are two
   different events arriving over two different paths and the redirect is the
   faster one. The gap between them is a real state with a real sentence
   (`not_confirmed`), and it is why this page polls instead of congratulating.

   Every word is in the string table. That is not politeness: this is the screen
   that charges a card, and an interface which speaks fourteen languages
   everywhere except the place it asks for money is one that stops speaking your
   language exactly when the stakes rise. `input/cloud-pairing.js` writes its
   English inline and says why; that argument covers a pairing screen and does
   not reach this one.

   No DOM and no global is touched while this module is evaluated, so the suite
   drives `bindPlanPage` against a stand-in document — the only way the failure
   states below can be held still and read.
   -------------------------------------------------------------------------- */

import { T, fill } from "../core/i18n.js";

/** How a limit reads when the plan sets none. Not "unlimited": the relay still has ceilings,
 *  and a page that promises none is the page that has to be argued with later. */
function limitText(value) {
    return value === null || value === undefined ? T.webPlanNoLimit : String(value);
}

function yesNo(value) { return value === true ? T.webPlanIncluded : T.webPlanNotIncluded; }

/** The rows under the plan name, in the pricing page's order so the two agree on sight. */
export function planRows(entitlements) {
    var e = entitlements || {};
    var days = e.history_days;
    return [
        [T.webPlanRowMacs, limitText(e.max_machines)],
        [T.webPlanRowSessions, limitText(e.max_concurrent_sessions)],
        [T.webPlanRowViewers, limitText(e.max_viewer_devices)],
        [T.webPlanRowDispatches, fill(T.webPlanMeter, { n: limitText(e.dispatches_per_month) })],
        [T.webPlanRowHistory, days === 0 || days === null || days === undefined
            ? (days === 0 ? T.webPlanHistoryNone : T.webPlanNoLimit)
            : fill(T.webPlanHistoryDays, { n: days })],
        [T.webPlanRowFleet, yesNo(e.fleet)],
        [T.webPlanRowTeam, yesNo(e.team)]
    ];
}

/** Title case for a tier id, without a table nobody would keep in step with the server. */
export function tierName(tier) {
    var raw = String(tier || "free");
    return raw.charAt(0).toUpperCase() + raw.slice(1);
}

/**
 * What a failed upgrade means, in words, by the code the control plane sent.
 *
 * Exported because the suite asserts on these directly: a screen that draws seven refusals as
 * one sentence passes every test which only checks that "an error was shown". Each of these is
 * a different thing to do next, which is the whole reason `api/src/routes/billing.ts` went to
 * the trouble of typing them.
 */
export function checkoutSentence(code, tier) {
    var name = tierName(tier);
    if (code === "no_session") return T.webPlanFailSignedOut;
    if (code === "not_purchasable") return fill(T.webPlanFailNotForSale, { tier: name });
    if (code === "missing_variant") return fill(T.webPlanFailNoPrice, { tier: name });
    if (code === "free_tier") return T.webPlanFailFree;
    if (code === "unknown_user") return T.webPlanFailNoAccount;
    return T.webPlanFailCheckout;
}

export function portalSentence(code) {
    if (code === "no_subscription") return T.webPlanPortalNone;
    if (code === "provider_unavailable") return T.webPlanPortalProvider;
    if (code === "no_session") return T.webPlanFailSignedOut;
    return T.webPlanPortalFailed;
}

/** Paying for something, as against being on Free. `effectiveTier` on the server is the
 *  authority; this is only the question this page asks of its answer. */
export function isPaidTier(tier) {
    return typeof tier === "string" && tier !== "" && tier !== "free";
}

/**
 * @param {object} elements — the `plan-*` table, supplied by `main.js`.
 * @param {object} seams
 *   `billing`       — a `net/billing.js` client, or **null** on the local and mock transports.
 *                     Null is not a failure: it is the honest answer that this copy of the page
 *                     is not the hosted console and has no Cloud account to charge.
 *   `signInURL`     — where a signed-out browser goes. A thunk: the cloud session owns it.
 *   `navigate`      — a top-level navigation. Injected so the suite can watch without following.
 *   `consoleOrigin` — the hosted console, named for the copy of this page that is not it.
 *   `setTimeout` / `clearTimeout`, `pollIntervalMs`, `pollAttempts` — the wait for the webhook.
 */
export function bindPlanPage(elements, seams) {
    seams = seams || {};
    var billing = seams.billing || null;
    var signInURL = typeof seams.signInURL === "function" ? seams.signInURL : function () { return ""; };
    var navigate = seams.navigate || function (url) { globalThis.location.assign(url); };
    var later = seams.setTimeout || function (fn, ms) { return setTimeout(fn, ms); };
    var stopLater = seams.clearTimeout || function (timer) { clearTimeout(timer); };
    var consoleOrigin = seams.consoleOrigin || "https://app.clawdline.com";
    var pollIntervalMs = Number.isSafeInteger(seams.pollIntervalMs) ? seams.pollIntervalMs : 2000;
    var pollAttempts = Number.isSafeInteger(seams.pollAttempts) ? seams.pollAttempts : 10;

    var state = "idle";
    var plan = null;          // the last answer from `/v1/entitlements`
    var timer = null;
    var polls = 0;
    var generation = 0;       // leaving invalidates whatever is still in flight

    function node(id) { return elements[id] || null; }

    function text(id, words) {
        var element = node(id);
        if (element) element.textContent = words || "";
    }

    function show(id, on) {
        var element = node(id);
        if (element) element.hidden = !on;
    }

    /** The calm line. Everything going to plan is said here and nowhere else. */
    function say(words) {
        text("plan-say", words);
        show("plan-say", !!words);
    }

    /** The line that is a problem. A separate element with `role="alert"`, so a screen reader is
     *  interrupted by a refused payment and is not interrupted by "reading your plan…". */
    function trouble(words) {
        text("plan-alert", words);
        show("plan-alert", !!words);
    }

    var CONTROLS = ["plan-upgrade", "plan-portal", "plan-signin", "plan-retry", "plan-recheck"];

    function hideControls() {
        CONTROLS.concat(["plan-elsewhere", "plan-limits", "plan-fine"])
            .forEach(function (id) { show(id, false); });
        CONTROLS.forEach(function (id) { var b = node(id); if (b) b.disabled = false; });
    }

    function drawLimits(entitlements) {
        var list = node("plan-limits");
        if (!list || !list.ownerDocument) return;
        var doc = list.ownerDocument;
        while (list.firstChild) list.removeChild(list.firstChild);
        planRows(entitlements).forEach(function (row) {
            var term = doc.createElement("dt");
            var value = doc.createElement("dd");
            term.textContent = row[0];
            value.textContent = row[1];
            list.appendChild(term);
            list.appendChild(value);
        });
        show("plan-limits", true);
        // The numbers are provisional and the server says so on every answer. A page that
        // quietly drops that flag is promising a quota nobody has committed to.
        var provisional = entitlements && entitlements.quotas_provisional === true;
        text("plan-fine", provisional ? T.webPlanProvisional : "");
        show("plan-fine", provisional);
    }

    function drawTier(tier, note) {
        text("plan-tier", tierName(tier));
        text("plan-tier-note", note || (tier === "free" ? T.webPlanFreeNote : T.webPlanPaidNote));
    }

    /**
     * The page's own furniture, in the language that came back.
     *
     * Painted on arrival rather than at bind time, and that is not a detail: `main.js` binds
     * this page while the document is still holding its English, and `applyStrings` runs later
     * in `boot`. A label written at bind time is a label written before the words exist.
     * `view/static.js` paints the drawer row instead, because that one is on screen from the
     * first frame whether this page has ever been opened or not.
     */
    function paintLabels() {
        text("plan-title", T.webPlan);
        text("plan-lede", T.webPlanLede);
        text("plan-includes", T.webPlanIncludes);
        text("plan-close", "\u2039 " + T.webSessions);
        text("plan-upgrade", T.webPlanUpgrade);
        text("plan-portal", T.webPlanManage);
        text("plan-signin", T.webPlanSignIn);
        text("plan-retry", T.webPlanRetry);
        text("plan-recheck", T.webPlanRecheck);
    }

    /* ==========================================================================
       The states. One function each, because each is a sentence somebody has to
       be able to read out loud and check against what actually happened.
       ========================================================================== */

    function toLoading() {
        state = "loading";
        hideControls();
        trouble("");
        say(T.webPlanLoading);
    }

    /** This copy of the page is the Mac's own, or the fixture. There is no Cloud account here. */
    function toElsewhere() {
        state = "not_here";
        hideControls();
        trouble("");
        drawTier("free", T.webPlanElsewhereNote);
        say("");
        text("plan-elsewhere", T.webPlanElsewhere);
        var link = node("plan-console");
        if (link) {
            link.textContent = consoleOrigin.replace(/^https:\/\//, "");
            link.setAttribute("href", consoleOrigin + "/#page=plan");
        }
        show("plan-elsewhere", true);
    }

    function toSignedOut() {
        state = "signed_out";
        hideControls();
        trouble("");
        drawTier("free", T.webPlanSignedOutNote);
        say(T.webPlanSignedOut);
        show("plan-signin", true);
    }

    function toSettled(answer) {
        plan = answer;
        hideControls();
        trouble("");
        drawTier(answer.tier);
        drawLimits(answer.entitlements);
        if (isPaidTier(answer.tier)) {
            state = "paid";
            say(fill(T.webPlanActive, { tier: tierName(answer.tier) }));
            show("plan-portal", true);
            return;
        }
        // Only what the server calls purchasable. Max and Team are deliberately absent from that
        // list at launch, and the button is absent with them rather than failing on the press.
        if (answer.purchasable.indexOf("pro") >= 0) {
            state = "free";
            text("plan-upgrade", T.webPlanUpgrade);
            say(T.webPlanOnFree);
            show("plan-upgrade", true);
        } else {
            state = "free_not_for_sale";
            say(T.webPlanNotForSale);
        }
    }

    function toUnreadable() {
        state = "unavailable";
        hideControls();
        say("");
        trouble(T.webPlanUnreadable);
        show("plan-retry", true);
    }

    /* ==========================================================================
       Reading the plan
       ========================================================================== */

    function load() {
        if (!billing) { toElsewhere(); return Promise.resolve(); }
        var mine = generation;
        toLoading();
        return billing.plan().then(function (answer) {
            if (mine !== generation) return;
            if (!answer.signedIn) { toSignedOut(); return; }
            toSettled(answer);
        }, function () {
            if (mine !== generation) return;
            toUnreadable();
        });
    }

    /* ==========================================================================
       Coming back from Lemon Squeezy

       The redirect proves the person finished the checkout form. It proves nothing
       about this account's entitlement, which changes only when the webhook lands
       and `handleBillingWebhook` commits. So this waits, says it is waiting, and —
       when the wait runs out — says *that*, rather than drawing a plan the account
       does not have.
       ========================================================================== */

    function toWaiting() {
        state = "waiting";
        hideControls();
        trouble("");
        say(T.webPlanWaiting);
    }

    function toNotConfirmed() {
        state = "not_confirmed";
        hideControls();
        say("");
        trouble(fill(T.webPlanNotConfirmed, { tier: tierName(plan && plan.tier) }));
        show("plan-recheck", true);
        // Whatever the entitlement actually is, drawn under the alert. This page is not allowed
        // to leave the reader guessing which plan they are on while it waits.
        if (plan) drawLimits(plan.entitlements);
    }

    function poll() {
        var mine = generation;
        billing.plan().then(function (answer) {
            if (mine !== generation) return;
            if (!answer.signedIn) { toSignedOut(); return; }
            plan = answer;
            if (isPaidTier(answer.tier)) {
                toSettled(answer);
                say(fill(T.webPlanConfirmed, { tier: tierName(answer.tier) }));
                return;
            }
            polls += 1;
            if (polls >= pollAttempts) { toNotConfirmed(); return; }
            timer = later(poll, pollIntervalMs);
        }, function () {
            if (mine !== generation) return;
            polls += 1;
            if (polls >= pollAttempts) { toUnreadable(); return; }
            timer = later(poll, pollIntervalMs);
        });
    }

    function waitForWebhook() {
        polls = 0;
        toWaiting();
        timer = later(poll, pollIntervalMs);
    }

    /* ==========================================================================
       The two presses that cost money
       ========================================================================== */

    /**
     * Upgrade.
     *
     * **The plan is read again before the checkout is asked for**, and that re-read is the whole
     * of the fix for the one failure this page can cause rather than merely report: a screen
     * showing Free for an account that has since become Pro sells a second subscription to
     * somebody who already has one, and Lemon Squeezy will take the money for it. Hiding the
     * button in the `paid` state is not enough — this page can sit open on a phone for a day.
     */
    function upgrade() {
        if (!billing || state === "opening") return Promise.resolve();
        var mine = generation;
        var button = node("plan-upgrade");
        if (button) button.disabled = true;
        state = "opening";
        trouble("");
        say(T.webPlanOpening);
        return billing.plan().then(function (answer) {
            if (mine !== generation) return;
            if (!answer.signedIn) { toSignedOut(); return; }
            plan = answer;
            if (isPaidTier(answer.tier)) {
                state = "already_paid";
                hideControls();
                say("");
                trouble(fill(T.webPlanAlready, { tier: tierName(answer.tier) }));
                drawTier(answer.tier);
                drawLimits(answer.entitlements);
                show("plan-portal", true);
                return;
            }
            return billing.checkout("pro").then(function (session) {
                if (mine !== generation) return;
                state = "leaving";
                navigate(session.url);
            });
        }).catch(function (error) {
            if (mine !== generation) return;
            state = "checkout_failed";
            hideControls();
            say("");
            trouble(checkoutSentence(error && error.code, "pro"));
            if (error && error.code === "no_session") show("plan-signin", true);
            else show("plan-retry", true);
        });
    }

    function portal() {
        if (!billing || state === "opening_portal") return Promise.resolve();
        var mine = generation;
        var button = node("plan-portal");
        var wasAlreadyPaid = state === "already_paid";
        if (button) button.disabled = true;
        state = "opening_portal";
        trouble("");
        say(T.webPlanPortalOpening);
        return billing.portal().then(function (session) {
            if (mine !== generation) return;
            state = "leaving";
            navigate(session.url);
        }, function (error) {
            if (mine !== generation) return;
            state = wasAlreadyPaid ? "already_paid" : "portal_failed";
            say("");
            trouble(portalSentence(error && error.code));
            if (button) button.disabled = false;
            show("plan-portal", true);
            show("plan-retry", true);
        });
    }

    function signIn() {
        var url = signInURL();
        if (url) navigate(url);
    }

    CONTROLS.forEach(function (id) {
        var button = node(id);
        if (!button || typeof button.addEventListener !== "function") return;
        button.addEventListener("click", function () {
            if (id === "plan-upgrade") upgrade();
            else if (id === "plan-portal") portal();
            else if (id === "plan-signin") signIn();
            else load();
        });
    });

    return {
        /**
         * Arrival, however it happened. `options.returning` is the one thing this page cannot
         * work out for itself: that this navigation is Lemon Squeezy handing the person back.
         */
        enter: function (options) {
            generation += 1;
            if (timer) { stopLater(timer); timer = null; }
            paintLabels();
            if (options && options.returning) {
                if (!billing) { toElsewhere(); return Promise.resolve(); }
                waitForWebhook();
                return Promise.resolve();
            }
            return load();
        },

        /** Leaving stops the wait. A poll that outlives the page paints a screen nobody is on. */
        leave: function () {
            generation += 1;
            if (timer) { stopLater(timer); timer = null; }
        },

        /** For the suite, and for anything that needs to know what is on screen. */
        state: function () { return state; },
        plan: function () { return plan; }
    };
}
