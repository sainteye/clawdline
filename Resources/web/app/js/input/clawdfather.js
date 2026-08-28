import { T, fill } from "../core/i18n.js";
import { coordinatorForSession } from "./coordinator-actions.js";

/* --------------------------------------------------------------------------
   Asking a session to make itself Clawdfather

   Registering the machine coordinator needs `X-Clawdline-Orchestrator`, and a browser holds a
   device token. That boundary is deliberate and is not widened here: this module adds no route
   and reads no coordinator state of its own.

   What it does instead is compose one sentence. The session on the other end is a local process
   running as the person who owns this Mac, so it can read
   `~/.config/clawdline/orchestrator-token` and carry out the documented register/rebind recipe
   itself — the same trust boundary every other Clawdline dispatch already stands on, and the
   same one a person crosses by typing the curl by hand. The sentence travels over
   `POST /v1/sessions/:id/send`, which the page can already reach and which every other composed
   message goes through.

   The browser also knows something the session would otherwise have to work out: the
   terminal-neutral id. That is exactly the `id` on a Session row, so it is handed over in the
   instruction rather than left to `$ITERM_SESSION_ID` archaeology at the other end.
   -------------------------------------------------------------------------- */

/**
 * Whether this session can be asked, and what the menu item says.
 *
 * Three answers, and only one of them is a button worth pressing:
 *
 * - **bound** — the authenticated `session.coordinator` projection is on this row, so the
 *   answer has already arrived. Shown and disabled: it is the status, and taking it off screen
 *   the moment it becomes true would hide the thing somebody pressed the item to find out.
 * - **unaddressable** — no assistant. A bare shell prompt is an address that cannot read an
 *   instruction, which is the same reason `GET /v1/orchestrator/sessions` does not list one.
 * - **available** — everything else. Every live addressable session, not only the row that
 *   already wears the crown.
 */
export function clawdfatherRequest(session) {
    var coordinator = coordinatorForSession(session);
    if (coordinator) {
        return {
            state: "bound", shown: true, enabled: false, status: coordinator.status,
            label: fill(T.webClawdfatherIs, { name: coordinator.label })
        };
    }
    var assistant = session && typeof session.assistant === "string"
        ? session.assistant.trim() : "";
    return {
        state: assistant ? "available" : "unaddressable",
        shown: !!assistant, enabled: !!assistant, status: null,
        label: T.webMakeClawdfather
    };
}

/** The one line typed into the session, in the language this page was served in. */
export function clawdfatherInstruction(session) {
    var id = session && typeof session.id === "string" ? session.id.trim() : "";
    if (!id) return "";
    return fill(T.webClawdfatherAsk, { id: id });
}
