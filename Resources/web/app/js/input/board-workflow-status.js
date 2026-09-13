var machineCapacityNoticeShown = false;

function capacityMessage() {
    var language = typeof document !== "undefined" && document.documentElement
        ? String(document.documentElement.lang || "").toLowerCase() : "";
    return language.startsWith("zh")
        ? "訊息已送出，但這台 Mac 的看板紀錄已滿；新的活動暫時不會出現在看板。"
        : "Message sent, but this Mac's Project Board history is full; new activity will not appear there yet.";
}

/**
 * Surface a successful terminal send whose secondary Board journal write failed.
 * The caller has already accepted the terminal effect: this hook never throws and its warning is
 * deliberately non-error, so it cannot retain the composer or invite a second terminal send.
 */
export function observeBoardWorkflowSend(answer, effects) {
    var workflow = answer && answer.workflow;
    if (workflow && workflow.status === "ingress_recorded") {
        machineCapacityNoticeShown = false;
        return false;
    }
    if (!workflow || !(workflow.status === "unrecorded"
        || (Number.isInteger(workflow.status) && workflow.status >= 400))) return false;
    var code = String(workflow.code || "workflow_unrecorded");
    if (effects && typeof effects.note === "function") {
        try { effects.note("workflow.send.unrecorded", { code: code }); } catch (_) { /* secondary */ }
    }
    if (code === "workflow_capacity_reached") {
        if (machineCapacityNoticeShown) return true;
        machineCapacityNoticeShown = true;
        if (effects && typeof effects.toast === "function") {
            try { effects.toast(capacityMessage(), false); }
            catch (_) { /* A feedback failure cannot invite a duplicate send. */ }
        }
        return true;
    }
    if (effects && typeof effects.toast === "function") {
        try { effects.toast("Message sent; Board workflow was not recorded (" + code + ").", false); }
        catch (_) { /* A feedback failure cannot invite a duplicate send. */ }
    }
    return true;
}
