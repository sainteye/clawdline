/** Human labels are presentation only. A share address names a machine and conversation. */
const UUID = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i;
const PROJECT = /^project-[0-9a-f]{24}$/;
export const SESSION_ORIGIN = "https://app.clawdline.com";

export function sessionLocatorFromHash(hash) {
    const source = String(hash || "").replace(/^#/, "");
    if (new TextEncoder().encode(source).length > 2048) return null;
    try {
        source.split("&").forEach(part => part.split("=").forEach(value =>
            decodeURIComponent(value.replace(/\+/g, " "))));
    } catch (_) { return null; }
    const params = new URLSearchParams(source);
    const keys = new Set(["session_ref", "machine", "conversation", "project"]);
    for (const key of params.keys())
        if (!keys.has(key) || params.getAll(key).length !== 1) return null;
    const machine = params.get("machine"), conversation = params.get("conversation"),
        project = params.get("project");
    if (params.get("session_ref") !== "1" || !machine || machine === "this-mac"
        || new TextEncoder().encode(machine).length > 200
        || /[\u0000-\u0020\u007f-\u009f]/.test(machine)
        || !UUID.test(conversation || "") || (project !== null && !PROJECT.test(project))) return null;
    return { machine, conversation: conversation.toLowerCase(), project };
}

export function sessionShareURL(value) {
    if (!value || typeof value !== "object") return null;
    const params = new URLSearchParams({ session_ref: "1", machine: value.machine || "",
        conversation: value.conversation || "" });
    if (value.project != null) params.set("project", value.project);
    const locator = sessionLocatorFromHash(params.toString());
    if (!locator) return null;
    params.set("conversation", locator.conversation);
    return SESSION_ORIGIN + "/#" + params.toString();
}
