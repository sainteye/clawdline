/* --------------------------------------------------------------------------
   The four billing reads, and one typed answer for each

   This module is the only place in the page that knows the billing routes exist.
   It holds no DOM and reaches no global at module scope, so `view/plan.js` can be
   driven in Node with a fake `fetch` and the same object the browser gets.

   **Every refusal the control plane can give has a name here.** That is the whole
   point of the file: `api/src/routes/billing.ts` answers a failed upgrade with
   `409 not_purchasable`, `409 free_tier`, `409 missing_variant`, `404 unknown_user`
   and `401 no_session`, and it answers a Lemon Squeezy outage with an untyped
   `500 internal` — six different facts that a page which only asks `response.ok`
   would draw as one grey "something went wrong". A person who is trying to give us
   ten dollars a month deserves to be told which of those six it was.

   The origin is injected rather than read from `location`. On the hosted console it
   is `api.clawdline.com` and this page is `app.clawdline.com` — a cross-origin
   request with the session cookie on it, which is why every call carries
   `credentials: "include"` and why `api/src/server.ts` names both origins to CORS.
   -------------------------------------------------------------------------- */

/** The tiers this page will show a price for. The server still decides what is on sale. */
export var PRICES = { pro: "$10 / month", max: "$30 / month", team: "$60 / seat, month" };

export function billingError(code, message, options) {
    options = options || {};
    var error = new Error(message || code);
    error.code = code;
    if (Number.isSafeInteger(options.status)) error.status = options.status;
    if (options.details && typeof options.details === "object") error.details = options.details;
    return error;
}

function refused(status) { return status === 401 || status === 403; }

/**
 * The body, or null. A 502 from something in front of the API is HTML, and a page that
 * lets `response.json()` reject here turns a readable outage into an unhandled rejection.
 */
async function read(fetchImpl, url, init) {
    var response = await fetchImpl(url, Object.assign({ credentials: "include" }, init || {}));
    var body = null;
    try { body = await response.json(); } catch (e) { body = null; }
    return { status: response.status, body: body };
}

function errorFrom(result, fallbackCode, fallbackMessage) {
    var api = result && result.body && result.body.error;
    return billingError(
        api && typeof api.code === "string" ? api.code : fallbackCode,
        api && typeof api.message === "string" ? api.message : fallbackMessage,
        { status: result && result.status, details: api && api.details }
    );
}

/** The tiers that are not free. `effectiveTier` on the server is the authority; this is the
 *  question the page asks of its answer — "is this account paying for something". */
export function isPaidTier(tier) {
    return typeof tier === "string" && tier !== "" && tier !== "free";
}

export function createBillingClient(options) {
    options = options || {};
    var origin = String(options.apiOrigin || "").replace(/\/$/, "");
    var fetchImpl = options.fetch || function (url, init) { return globalThis.fetch(url, init); };
    if (!origin) throw new TypeError("createBillingClient needs an apiOrigin");

    function post(path, payload) {
        return read(fetchImpl, origin + path, {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify(payload || {})
        });
    }

    return {
        origin: origin,

        /**
         * What this account is entitled to, right now.
         *
         * A signed-out browser is not an error and is not drawn as one: `{signedIn: false}` is
         * the answer, and the screen above turns it into a sign-in button rather than a red line.
         */
        async plan() {
            var result = await read(fetchImpl, origin + "/v1/entitlements");
            if (refused(result.status)) return { signedIn: false };
            if (result.status !== 200 || !result.body || !result.body.entitlements) {
                throw errorFrom(result, "plan_unavailable",
                    "Clawdline could not read this account's plan");
            }
            var entitlements = result.body.entitlements;
            return {
                signedIn: true,
                accountId: typeof result.body.account_id === "string" ? result.body.account_id : "",
                tier: typeof entitlements.tier === "string" ? entitlements.tier : "free",
                entitlements: entitlements,
                // The server's own list, never a constant here. Launch sells Pro and nothing
                // else, and the day that changes is a deploy of the API, not of this page.
                purchasable: Array.isArray(result.body.purchasable_tiers)
                    ? result.body.purchasable_tiers.slice() : []
            };
        },

        /**
         * Ask for a checkout. Resolves with the URL Lemon Squeezy will take the person to.
         *
         * Nothing here navigates: the caller does that, after it has told the screen what is
         * about to happen. A module that both asks and navigates cannot be tested without a
         * browser and cannot be stopped once it has started.
         */
        async checkout(tier) {
            var result = await post("/v1/billing/checkout", { tier: tier });
            if (refused(result.status)) {
                throw billingError("no_session", "Sign in before upgrading", { status: result.status });
            }
            if (result.status === 200 && result.body && typeof result.body.url === "string") {
                return { url: result.body.url, sessionId: result.body.session_id || "" };
            }
            if (result.status >= 500) {
                // The gateway throws a plain `Error` when Lemon Squeezy refuses or is down, and
                // `api/src/server.ts` turns that into an untyped `internal`. Name it here so the
                // screen can say "the checkout could not be opened" rather than "internal".
                throw billingError("checkout_unavailable",
                    "Clawdline could not open the checkout", { status: result.status });
            }
            throw errorFrom(result, "checkout_failed", "That upgrade could not be started");
        },

        /** The customer portal, for an account that already has a subscription. */
        async portal() {
            var result = await post("/v1/billing/portal", {});
            if (refused(result.status)) {
                throw billingError("no_session", "Sign in to manage this subscription",
                    { status: result.status });
            }
            if (result.status === 200 && result.body && typeof result.body.url === "string") {
                return { url: result.body.url };
            }
            if (result.status >= 500) {
                throw billingError("portal_unavailable",
                    "Clawdline could not open the customer portal", { status: result.status });
            }
            throw errorFrom(result, "portal_failed", "That subscription could not be opened");
        }
    };
}
