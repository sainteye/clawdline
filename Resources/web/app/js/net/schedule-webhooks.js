import { esc } from "../core/esc.js";

/* Browser-session management for Schedule Webhook v1.  The public URL is deliberately never
   retained by this object: create/rotate hand the one response-local value to their caller and
   all later reads contain only the hook id and fingerprint. */

function requestError(status, body) {
    var error = body && body.error || {};
    var value = new Error(error.message || "Schedule webhook request failed");
    value.status = status;
    value.code = error.code || "request_failed";
    return value;
}

function randomIntent() {
    if (globalThis.crypto && typeof globalThis.crypto.randomUUID === "function") {
        return globalThis.crypto.randomUUID().toLowerCase();
    }
    return "intent-" + Date.now() + "-" + Math.random().toString(36).slice(2);
}

export class ScheduleWebhookClient {
    constructor(options) {
        options = options || {};
        this.fetch = options.fetch || globalThis.fetch.bind(globalThis);
        this.origin = String(options.origin || globalThis.location && globalThis.location.origin || "")
            .replace(/\/$/, "");
        this.makeIdempotencyKey = options.idempotencyKey || randomIntent;
    }

    async send(method, path, body, mutate, exactIdempotencyKey) {
        var headers = { "Accept": "application/json" };
        if (body !== undefined) headers["Content-Type"] = "application/json";
        if (exactIdempotencyKey) headers["Idempotency-Key"] = exactIdempotencyKey;
        else if (mutate) headers["Idempotency-Key"] = this.makeIdempotencyKey();
        var response = await this.fetch(this.origin + path, {
            method: method,
            credentials: "include",
            cache: "no-store",
            headers: headers,
            body: body === undefined ? undefined : JSON.stringify(body)
        });
        var data = await response.json().catch(function () { return null; });
        if (!response.ok) throw requestError(response.status, data);
        return data;
    }

    list(machineID) {
        var query = machineID ? "?machine_id=" + encodeURIComponent(machineID) : "";
        return this.send("GET", "/v1/schedule-webhooks" + query);
    }

    read(hookID) {
        return this.send("GET", "/v1/schedule-webhooks/" + encodeURIComponent(hookID));
    }

    deliveries(hookID) {
        return this.send("GET", "/v1/schedule-webhooks/" + encodeURIComponent(hookID) +
            "/deliveries");
    }

    entitlements() {
        return this.send("GET", "/v1/entitlements").then(function (answer) {
            var tier = answer && answer.entitlements && answer.entitlements.tier;
            return typeof tier === "string" ? tier : null;
        });
    }

    async create(machineID) {
        return this.secretResult(await this.send("POST", "/v1/schedule-webhooks",
            { machine_id: machineID }, true));
    }

    async rotate(hookID, revision) {
        return this.secretResult(await this.send("POST", "/v1/schedule-webhooks/" +
            encodeURIComponent(hookID) + "/rotate", { expected_revision: revision }, true));
    }

    disable(hookID, revision) {
        return this.send("POST", "/v1/schedule-webhooks/" + encodeURIComponent(hookID) +
            "/disable", { expected_revision: revision }, true);
    }

    observe(hookID, deliveryID, receiptVersion) {
        return this.send("POST", "/v1/schedule-webhooks/" + encodeURIComponent(hookID) +
            "/deliveries/" + encodeURIComponent(deliveryID) + "/observations",
            { receipt_version: receiptVersion }, false,
            "observed:" + deliveryID + ":" + receiptVersion);
    }

    secretResult(data) {
        return {
            hook: data && data.hook || null,
            publicURL: data && data.secret_available === true && typeof data.public_url === "string"
                ? data.public_url : null,
            secretAvailable: !!(data && data.secret_available),
            duplicate: !!(data && data.duplicate)
        };
    }
}

const stateLabels = {
    ingress_accepted: "Ingress accepted",
    queued: "Queued",
    leased: "Leased to Mac",
    mac_durable_accepted: "Mac durably accepted",
    schedule_dispatch_deferred: "Dispatch deferred",
    schedule_dispatch_accepted: "Schedule dispatch accepted",
    schedule_dispatch_refused: "Schedule dispatch refused",
    task_execution_terminal: "Task execution terminal",
    cloud_receipt_acknowledged: "Cloud receipt acknowledged",
    human_observed: "Observed here",
    expired: "Expired before Mac acceptance",
    canceled: "Canceled",
    outcome_unknown: "Outcome unknown",
    missing: "Receipt unavailable"
};

export function scheduleWebhookTimelineEvents(deliveries, observed) {
    var events = [];
    (deliveries || []).forEach(function (delivery) {
        delivery = delivery || {};
        if (delivery.accepted_at) events.push({
            state: "ingress_accepted", occurred_at: delivery.accepted_at,
            delivery_id: delivery.delivery_id, attempt: delivery.attempt || delivery.attempts
        });
        var receipts = Array.isArray(delivery.receipts) ? delivery.receipts : [];
        if (receipts.length === 0 || ["queued", "leased", "expired", "canceled"].includes(delivery.state)) {
            events.push(Object.assign({}, delivery));
        }
        receipts.forEach(function (receipt) {
            var event = Object.assign({}, receipt, {
                state: receipt.kind === "schedule_dispatch_refused"
                    && receipt.outcome_code === "outcome_unknown"
                    ? "outcome_unknown" : receipt.kind,
                delivery_id: delivery.delivery_id
            });
            events.push(event);
            if (receipt.acknowledged_at) events.push({
                state: "cloud_receipt_acknowledged", occurred_at: receipt.acknowledged_at,
                delivery_id: delivery.delivery_id, receipt_version: receipt.receipt_version
            });
            var key = delivery.delivery_id + ":" + receipt.receipt_version;
            if (observed && observed[key]) events.push({
                state: "human_observed", occurred_at: observed[key],
                delivery_id: delivery.delivery_id, receipt_version: receipt.receipt_version
            });
        });
    });
    return events;
}

export function scheduleWebhookReceiptHeads(deliveries) {
    var heads = [];
    (deliveries || []).forEach(function (delivery) {
        var latest = null;
        (Array.isArray(delivery && delivery.receipts) ? delivery.receipts : []).forEach(function (receipt) {
            var version = Number(receipt && receipt.receipt_version || 0);
            if (Number.isInteger(version) && version > 0 &&
                (!latest || version > latest.receiptVersion)) {
                latest = { deliveryID: delivery.delivery_id, receiptVersion: version };
            }
        });
        if (latest) heads.push(latest);
    });
    return heads;
}

export function scheduleWebhookLatestReceipt(deliveries) {
    return scheduleWebhookReceiptHeads(deliveries).reduce(function (latest, value) {
        return !latest || value.receiptVersion > latest.receiptVersion ? value : latest;
    }, null);
}

export function scheduleWebhookTimelineHTML(rows, observed) {
    return scheduleWebhookTimelineEvents(rows, observed).map(function (row) {
        row = row || {};
        var state = typeof row.state === "string" && stateLabels[row.state]
            ? row.state : "missing";
        var at = row.acknowledged_at || row.occurred_at || row.accepted_at || "";
        var detail = [];
        if (Number.isInteger(row.attempt)) detail.push("Attempt " + row.attempt);
        if (row.outcome_code) detail.push(String(row.outcome_code));
        if (row.task_terminal_state) detail.push(String(row.task_terminal_state));
        if (row.retry_at) detail.push("Retry " + String(row.retry_at));
        return '<li class="schedule-webhook-event" data-webhook-state="' + esc(state) + '">' +
            '<span class="schedule-webhook-event-name">' + esc(stateLabels[state]) + '</span>' +
            (at ? '<time datetime="' + esc(at) + '">' + esc(new Date(at).toLocaleString()) +
                '</time>' : '<span class="schedule-webhook-event-time">Timestamp unavailable</span>') +
            (detail.length ? '<span class="schedule-webhook-event-detail">' +
                esc(detail.join(" · ")) + '</span>' : '') + '</li>';
    }).join("");
}

export function shouldObserveScheduleWebhook(context) {
    return !!(context && context.open && context.visibilityState === "visible"
        && Number.isInteger(context.renderedVersion) && context.renderedVersion > 0);
}

export function scheduleWebhookCanGenerate(hook) {
    var context = arguments.length > 1 && arguments[1] || {};
    return (context.bindingAvailability === "unbound"
            || context.bindingAvailability === "active")
        && context.effectiveTier !== null && context.effectiveTier !== "free"
        && (!hook || hook.state === "disabled");
}

export function scheduleWebhookManagementWarning(hook, context) {
    context = context || {};
    var traditional = String(context.language || "").toLowerCase() === "zh-tw";
    if (context.bindingAvailability === "binding_store_unavailable") {
        return traditional ? "本機 Webhook 綁定資料無法讀取；修復前不能產生另一個 Webhook。"
            : "The local binding store is unavailable. Repair it before generating another webhook.";
    }
    if (context.bindingAvailability !== "unbound"
        && context.bindingAvailability !== "active") {
        return traditional ? "本機 Webhook 綁定狀態不明；確認狀態前不能產生另一個 Webhook。"
            : "The local binding status is unavailable. Confirm it before generating another webhook.";
    }
    if (context.effectiveTier === null) {
        return traditional ? "目前無法確認方案；產生與輪替保持停用，但停用 Webhook 與收據紀錄仍可使用。"
            : "Plan status is unavailable. Generate and Rotate stay disabled; Disable and history remain available.";
    }
    if (context.effectiveTier === "free" || hook && hook.availability === "plan_paused") {
        return traditional ? "排程 Webhook 需要專業版；停用 Webhook 與收據紀錄仍可使用。"
            : "Schedule webhooks require Pro. Disable and receipt history remain available.";
    }
    if (hook && hook.state === "active") {
        return traditional
            ? "若一次性網址遺失，可用輪替取得新網址。輪替會立即讓舊網址失效，但不影響已接受的執行；停用只取消尚未被 Mac 持久接受的排隊項目，已接受項目會繼續並保留收據。"
            : "If the one-time URL was lost, Rotate creates a recoverable URL. Rotating immediately invalidates the old URL without affecting accepted runs. Disabling cancels only work the Mac has not durably accepted; accepted work continues with receipts.";
    }
    return "";
}

export async function generateAndBindScheduleWebhook(client, bind, machineID, scheduleID,
                                                       currentHook) {
    var replacement = currentHook && currentHook.state === "disabled"
        ? currentHook.hook_id : null;
    var made = await client.create(machineID);
    if (!made.hook) throw new Error("hook unavailable");
    var activeHook = await bind(scheduleID, made.hook.hook_id, replacement);
    if (!activeHook || activeHook.state !== "active") throw new Error("hook activation unavailable");
    made.hook = activeHook;
    return made;
}

/* A small invariant checker used by the focused suite and diagnostics: callers may keep the
   capability in a lexical variable for Copy, but it cannot appear in serialized surfaces. */
export function scheduleWebhookSecretIsEphemeral(secret, surfaces) {
    if (!secret || !surfaces) return false;
    return ![surfaces.html, JSON.stringify(surfaces.localStorage || {}),
        JSON.stringify(surfaces.sessionStorage || {})].some(function (value) {
        return String(value || "").includes(secret);
    });
}
