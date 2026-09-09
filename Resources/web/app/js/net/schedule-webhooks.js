import { esc } from "../core/esc.js";

export const SCHEDULE_WEBHOOK_GUIDE_URL =
    "https://clawdline.com/docs/schedule-webhooks";

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

const webhookCopy = {
    en: {
        title: "Cloud webhook", generate: "Generate webhook", copy: "Copy URL",
        rotate: "Rotate URL", disable: "Disable webhook",
        once: "Copy this URL now. Clawdline will not show it again.",
        idempotency: "A trigger without Idempotency-Key is a new run each time.",
        unavailable: "Webhook history is unavailable.",
        howToUse: "How to use", hideHelp: "Hide instructions",
        helpTitle: "Start this schedule with a webhook",
        helpAvailability: "Schedule Webhook is available on Pro only.",
        helpPurpose: "Save the copied URL as a secret in the service that should start this schedule.",
        helpSecret: "Treat the URL like a password. Anyone who has it can start this schedule.",
        helpRequest: "Send an HTTP POST with an empty body or the JSON body {}. Opening the URL in a browser sends GET and will not start the schedule. A webhook cannot replace this schedule's saved instructions.",
        helpIdempotency: "If the caller may retry one request, send the same Idempotency-Key each time. A new key, or no key, creates a new run.",
        helpAccepted: "HTTP 202 means Cloud durably accepted the request. It does not mean the Mac has executed it; confirm the later stages in the receipt timeline below.",
        helpRotation: "If the URL is exposed or lost, Rotate URL. Rotation immediately invalidates the old URL.",
        helpExample: "Terminal or automation example",
        helpFullGuide: "Open the complete Schedule Webhook guide",
        copyExample: "Copy example", exampleCopied: "Example copied. Add the URL as a secret named CLAWDLINE_SCHEDULE_WEBHOOK_URL.",
        attempt: "Attempt {n}", retry: "Retry {at}", timestampUnavailable: "Timestamp unavailable",
        warningBindingStore: "The local binding store is unavailable. Repair it before generating another webhook.",
        warningBindingUnknown: "The local binding status is unavailable. Confirm it before generating another webhook.",
        warningPlanUnknown: "Plan status is unavailable. Generate and Rotate stay disabled; Disable and history remain available.",
        warningPro: "Schedule webhooks require Pro. Disable and receipt history remain available.",
        warningActive: "If the one-time URL was lost, Rotate creates a recoverable URL. Rotating immediately invalidates the old URL without affecting accepted runs. Disabling cancels only work the Mac has not durably accepted; accepted work continues with receipts.",
        states: {
            ingress_accepted: "Ingress accepted", queued: "Queued", leased: "Leased to Mac",
            mac_durable_accepted: "Mac durably accepted",
            schedule_dispatch_deferred: "Dispatch deferred",
            schedule_dispatch_accepted: "Schedule dispatch accepted",
            schedule_dispatch_refused: "Schedule dispatch refused",
            task_execution_terminal: "Task execution terminal",
            cloud_receipt_acknowledged: "Cloud receipt acknowledged",
            human_observed: "Observed here", expired: "Expired before Mac acceptance",
            canceled: "Canceled", outcome_unknown: "Outcome unknown",
            missing: "Receipt unavailable"
        }
    },
    traditional: {
        title: "雲端 Webhook", generate: "產生 Webhook", copy: "複製網址",
        rotate: "輪替網址", disable: "停用 Webhook",
        once: "請立即複製此網址；Clawdline 不會再次顯示。",
        idempotency: "未帶 Idempotency-Key 的每次觸發都會建立一次新的執行。",
        unavailable: "目前無法讀取 Webhook 紀錄。",
        howToUse: "如何使用？", hideHelp: "收起說明",
        helpTitle: "使用 Webhook 啟動這個排程",
        helpAvailability: "排程 Webhook 僅供專業版（Pro）使用。",
        helpPurpose: "請將複製的網址以機密資料保存在需要啟動這個排程的服務中。",
        helpSecret: "請把這個網址視同密碼；任何取得網址的人都能啟動這個排程。",
        helpRequest: "請用 HTTP POST 呼叫，要求本文可以留空或使用 JSON {}。直接用瀏覽器開啟會送出 GET，不會啟動排程。Webhook 不能取代這個排程已儲存的指令。",
        helpIdempotency: "若呼叫端可能重試同一個要求，每次請使用相同的 Idempotency-Key。新的 Idempotency-Key 或未帶這個標頭，都會建立新的執行。",
        helpAccepted: "HTTP 202 只代表 Cloud 已持久接受要求，不代表 Mac 已執行；請從下方收據時間軸確認後續階段。",
        helpRotation: "若網址外洩或遺失，請輪替網址；輪替會立即讓舊網址失效。",
        helpExample: "終端機或自動化範例",
        helpFullGuide: "開啟完整的排程 Webhook 指南",
        copyExample: "複製範例", exampleCopied: "已複製範例；請將網址儲存為名為 CLAWDLINE_SCHEDULE_WEBHOOK_URL 的機密資料。",
        attempt: "第 {n} 次嘗試", retry: "重試時間 {at}", timestampUnavailable: "時間戳記不可用",
        warningBindingStore: "本機 Webhook 綁定資料無法讀取；修復前不能產生另一個 Webhook。",
        warningBindingUnknown: "本機 Webhook 綁定狀態不明；確認狀態前不能產生另一個 Webhook。",
        warningPlanUnknown: "目前無法確認方案；產生與輪替保持停用，但停用 Webhook 與收據紀錄仍可使用。",
        warningPro: "排程 Webhook 需要專業版；停用 Webhook 與收據紀錄仍可使用。",
        warningActive: "若一次性網址遺失，可用輪替取得新網址。輪替會立即讓舊網址失效，但不影響已接受的執行；停用只取消尚未被 Mac 持久接受的排隊項目，已接受項目會繼續並保留收據。",
        states: {
            ingress_accepted: "Cloud 已接受", queued: "等待 Mac", leased: "已租借給 Mac",
            mac_durable_accepted: "已由 Mac 持久接受",
            schedule_dispatch_deferred: "排程派送已延後",
            schedule_dispatch_accepted: "排程派送已接受",
            schedule_dispatch_refused: "排程派送遭拒",
            task_execution_terminal: "任務執行已結束",
            cloud_receipt_acknowledged: "Cloud 已確認收據",
            human_observed: "已在此查看", expired: "在 Mac 接受前已過期",
            canceled: "已取消", outcome_unknown: "結果未知",
            missing: "收據不可用"
        }
    }
};

function traditionalChinese(language) {
    var tag = String(language || "").toLowerCase().replace(/_/g, "-");
    return tag === "zh-tw" || tag === "zh-hk" || tag === "zh-mo"
        || tag === "zh-hant" || tag.indexOf("zh-hant-") === 0;
}

export function scheduleWebhookCopy(language) {
    return traditionalChinese(language) ? webhookCopy.traditional : webhookCopy.en;
}

export function scheduleWebhookCurlExample() {
    return "curl --request POST \"$CLAWDLINE_SCHEDULE_WEBHOOK_URL\" \\\n"
        + "  --header 'Content-Type: application/json' \\\n"
        + "  --header 'Idempotency-Key: request-001' \\\n"
        + "  --data '{}'";
}

export function scheduleWebhookHelpHTML(language) {
    var words = scheduleWebhookCopy(language);
    return '<h4>' + esc(words.helpTitle) + '</h4>'
        + '<p class="schedule-webhook-help-availability">' +
        esc(words.helpAvailability) + '</p>'
        + '<p>' + esc(words.helpPurpose) + '</p>'
        + '<ul><li>' + esc(words.helpSecret) + '</li>'
        + '<li>' + esc(words.helpRequest) + '</li>'
        + '<li>' + esc(words.helpIdempotency) + '</li>'
        + '<li>' + esc(words.helpAccepted) + '</li>'
        + '<li>' + esc(words.helpRotation) + '</li></ul>'
        + '<p><a href="' + SCHEDULE_WEBHOOK_GUIDE_URL +
        '" target="_blank" rel="noreferrer">' + esc(words.helpFullGuide) + '</a></p>'
        + '<p class="schedule-webhook-help-example-label">' + esc(words.helpExample) + '</p>'
        + '<pre><code>' + esc(scheduleWebhookCurlExample()) + '</code></pre>';
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

export function scheduleWebhookTimelineHTML(rows, observed, language) {
    var words = scheduleWebhookCopy(language);
    var stateLabels = words.states;
    return scheduleWebhookTimelineEvents(rows, observed).map(function (row) {
        row = row || {};
        var state = typeof row.state === "string" && stateLabels[row.state]
            ? row.state : "missing";
        var at = row.acknowledged_at || row.occurred_at || row.accepted_at || "";
        var detail = [];
        if (Number.isInteger(row.attempt)) {
            detail.push(words.attempt.replace("{n}", String(row.attempt)));
        }
        if (row.outcome_code) detail.push(String(row.outcome_code));
        if (row.task_terminal_state) detail.push(String(row.task_terminal_state));
        if (row.retry_at) detail.push(words.retry.replace("{at}", String(row.retry_at)));
        return '<li class="schedule-webhook-event" data-webhook-state="' + esc(state) + '">' +
            '<span class="schedule-webhook-event-name">' + esc(stateLabels[state]) + '</span>' +
            (at ? '<time datetime="' + esc(at) + '">' + esc(new Date(at).toLocaleString()) +
                '</time>' : '<span class="schedule-webhook-event-time">' +
                esc(words.timestampUnavailable) + '</span>') +
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
    var words = scheduleWebhookCopy(context.language);
    if (context.bindingAvailability === "binding_store_unavailable") {
        return words.warningBindingStore;
    }
    if (context.bindingAvailability !== "unbound"
        && context.bindingAvailability !== "active") {
        return words.warningBindingUnknown;
    }
    if (context.effectiveTier === null) {
        return words.warningPlanUnknown;
    }
    if (context.effectiveTier === "free" || hook && hook.availability === "plan_paused") {
        return words.warningPro;
    }
    if (hook && hook.state === "active") {
        return words.warningActive;
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
