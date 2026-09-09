/**
 * Surface a successful terminal send whose secondary Board journal write failed.
 * The caller has already accepted the terminal effect: this hook never throws and its warning is
 * deliberately non-error, so it cannot retain the composer or invite a second terminal send.
 */
export function observeBoardWorkflowSend(answer, effects) {
    var workflow = answer && answer.workflow;
    if (!workflow || !(workflow.status === "unrecorded"
        || (Number.isInteger(workflow.status) && workflow.status >= 400))) return false;
    var code = String(workflow.code || "workflow_unrecorded");
    if (effects && typeof effects.note === "function") {
        try { effects.note("workflow.send.unrecorded", { code: code }); } catch (_) { /* secondary */ }
    }
    if (effects && typeof effects.toast === "function") {
        try { effects.toast("Message sent; Board workflow was not recorded (" + code + ").", false); }
        catch (_) { /* A feedback failure cannot invite a duplicate send. */ }
    }
    return true;
}
