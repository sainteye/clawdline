import { esc } from "../core/esc.js";

/* --------------------------------------------------------------------------
   Coordinator controls

   `session.coordinator` is the only boundary into this feature. The sessions API is the
   broker-authenticated source; task ancestry, labels and words such as "father" never infer
   coordinator identity. A missing field therefore remains exactly the old session row.

   Phase A deliberately stops at a preview. The broker command route does not exist yet, so no
   button in this module calls the session transport or claims that a command ran.
   -------------------------------------------------------------------------- */

var SECTIONS = [
    { id: "observe", label: "Observe" },
    { id: "coordinate", label: "Coordinate" },
    { id: "presence", label: "Presence" },
    { id: "administration", label: "Administration" }
];

// Closed on purpose. Labels, placement and safety meaning come from shipped code, never from a
// payload. Adding a server command before adding it here makes it invisible rather than guessed.
var COMMANDS = {
    status_report: {
        label: "Status report", section: "observe", effect: "read_only",
        summary: "Read the coordinator's current status and active obligations."
    },
    since_away: {
        label: "Since you were away", section: "observe", effect: "read_only",
        summary: "Read the changes and decisions recorded since your last return point."
    },
    duplicates_conflicts_ownership: {
        label: "Duplicates, conflicts & ownership", section: "observe", effect: "read_only",
        summary: "Inspect duplicate work, conflicting claims and current ownership."
    },
    landing_closure: {
        label: "Landing closure", section: "observe", effect: "advisory",
        summary: "Draft a judgement about reviewed deliveries that still need root-owned landing."
    },
    coordinate_work: {
        label: "Coordinate work", section: "coordinate", effect: "advisory",
        summary: "Draft coordination steps across the work already in flight."
    },
    dispatch_independent_work: {
        label: "Dispatch independent work", section: "coordinate", effect: "spawns_session",
        summary: "Preview a dispatch that would start an independent child session."
    },
    ask_coordinator: {
        label: "Ask coordinator", section: "coordinate", effect: "advisory",
        summary: "Draft a question for the coordinator's judgement."
    },
    quiet_watch: {
        label: "Quiet watch", section: "presence", effect: "advisory",
        summary: "Draft a quiet monitoring request without raising a human-waiting alert."
    },
    scope_permissions: {
        label: "Scope & permissions", section: "administration", effect: "read_only",
        summary: "Read the coordinator's current scope and granted permissions."
    },
    stop: {
        label: "Stop coordinator", section: "administration", effect: "mutation",
        summary: "Preview stopping this coordinator."
    },
    reconnect: {
        label: "Reconnect coordinator", section: "administration", effect: "mutation",
        summary: "Preview reconnecting this coordinator."
    }
};

var EFFECT_LABELS = {
    read_only: "Read only",
    advisory: "Advisory",
    spawns_session: "Starts session",
    mutation: "Changes state"
};

var STATE_LABELS = {
    available: "Available",
    draft: "Draft",
    unavailable: "Unavailable",
    preview: "Preview",
    disabled: "Disabled"
};

function nonempty(value) {
    return typeof value === "string" && value.trim() ? value.trim() : null;
}

/** Normalize the optional sessions-API boundary, or return null without touching the row. */
export function coordinatorForSession(session) {
    var source = session && session.coordinator;
    if (!source || typeof source !== "object" || Array.isArray(source)) return null;
    return {
        label: nonempty(source.label) || "Clawdfather",
        status: source.status === "online" ? "online" : "offline",
        commands: Array.isArray(source.commands) ? source.commands : []
    };
}

/** The one route selector used by the row. Only the authenticated optional record changes it. */
export function coordinatorRoute(session, target) {
    return target === "mark" && coordinatorForSession(session) ? "controls" : "session";
}

/** Small, testable facts from which the list builds the coordinator-only mark and badge. */
export function coordinatorRowModel(session) {
    var coordinator = coordinatorForSession(session);
    if (!coordinator) return null;
    return {
        badge: "Coordinator",
        label: coordinator.label,
        mark: {
            role: "button",
            ariaHaspopup: "dialog",
            ariaLabel: "Open " + coordinator.label + " Coordinator controls"
        }
    };
}

function stateFor(definition, command, online) {
    if (command.enabled === false) return "disabled";
    if (!online) {
        if (definition.effect === "read_only") return "available";
        if (definition.effect === "advisory") return "draft";
        return "unavailable";
    }
    return definition.effect === "mutation" ? "preview" : "available";
}

/**
 * Select known commands and downgrade them for the current connection.
 *
 * Read-only controls remain visible offline. Judgement becomes a draft; anything that would
 * start a session or mutate coordinator state becomes unavailable. A server-disabled command
 * remains in place with its explanation in every connection state.
 */
export function coordinatorGroups(session, context) {
    var coordinator = coordinatorForSession(session);
    if (!coordinator) return [];
    var online = coordinator.status === "online" && (!context || context.connected !== false);
    var grouped = {};
    var seen = {};
    SECTIONS.forEach(function (section) { grouped[section.id] = []; });

    coordinator.commands.forEach(function (command) {
        if (!command || typeof command !== "object") return;
        var type = nonempty(command.type);
        var definition = type && COMMANDS[type];
        if (!definition || seen[type]) return;
        seen[type] = true;
        var state = stateFor(definition, command, online);
        grouped[definition.section].push({
            type: type,
            label: definition.label,
            summary: definition.summary,
            section: definition.section,
            effect: definition.effect,
            effectLabel: EFFECT_LABELS[definition.effect],
            state: state,
            stateLabel: STATE_LABELS[state],
            why: state === "disabled"
                ? (nonempty(command.why) || "Disabled by the coordinator.")
                : ""
        });
    });

    return SECTIONS.map(function (section) {
        return { id: section.id, label: section.label, actions: grouped[section.id] };
    }).filter(function (section) { return section.actions.length > 0; });
}

/** A Phase-A preview receipt. There is intentionally no execute callback behind it. */
export function coordinatorPreview(action) {
    var mutation = action && action.effect === "mutation";
    var note;
    if (!action) {
        note = "No command selected; nothing has been sent.";
    } else if (mutation) {
        note = "Preview only — nothing has been sent. Confirmation stays unavailable until the Coordinator command API is connected.";
    } else if (action.state === "draft") {
        note = "Offline draft only — nothing has been sent or presented as coordinator judgement.";
    } else if (action.effect === "spawns_session") {
        note = "Preview only — no session has been started.";
    } else {
        note = "Phase A shows this command contract only; nothing has been sent.";
    }
    return {
        title: action ? action.label : "Coordinator command",
        summary: action ? action.summary : "",
        note: note,
        requiresConfirmation: mutation,
        confirmDisabled: true
    };
}

function actionHTML(action) {
    var disabled = action.state === "disabled" || action.state === "unavailable";
    return '<button class="coordinator-command" type="button" data-command="' +
        esc(action.type) + '" data-state="' + esc(action.state) + '" data-effect="' +
        esc(action.effect) + '"' + (disabled ? " disabled" : "") + '>' +
        '<span class="coordinator-command-head"><b>' + esc(action.label) + "</b>" +
        '<span class="coordinator-command-state">' + esc(action.stateLabel) + "</span></span>" +
        '<span class="coordinator-command-summary">' + esc(action.summary) + "</span>" +
        '<span class="coordinator-command-effect">' + esc(action.effectLabel) + "</span>" +
        (action.why ? '<span class="coordinator-command-why">' + esc(action.why) + "</span>" : "") +
        "</button>";
}

/** The controls body is a pure rendering of the broker record plus current connection state. */
export function coordinatorPanelHTML(session, context) {
    var coordinator = coordinatorForSession(session);
    if (!coordinator) return "";
    var online = coordinator.status === "online" && (!context || context.connected !== false);
    var groups = coordinatorGroups(session, context);
    var sections = groups.map(function (group) {
        return '<section class="coordinator-group" aria-labelledby="coordinator-group-' +
            esc(group.id) + '"><h3 id="coordinator-group-' + esc(group.id) + '">' +
            esc(group.label) + "</h3>" + group.actions.map(actionHTML).join("") + "</section>";
    }).join("");
    if (!sections) {
        sections = '<p class="coordinator-empty">No supported Coordinator commands were advertised.</p>';
    }
    return '<div class="coordinator-presence" data-status="' + (online ? "online" : "offline") + '">' +
        '<span class="coordinator-presence-dot" aria-hidden="true"></span><span>' +
        esc(online ? "Coordinator online" : "Coordinator offline") +
        "</span></div>" +
        '<div class="coordinator-groups">' + sections + "</div>" +
        '<section class="coordinator-preview" aria-live="polite" hidden></section>' +
        '<button class="chip wide coordinator-session-actions" type="button" ' +
        'data-coordinator-session-actions>Session actions</button>';
}

/** DOM wiring is injected by `main.js`; importing this module in Node stays browser-independent. */
export var CoordinatorControls = {
    dom: null,
    session: null,
    opener: null,
    onSessionActions: null,

    bind: function (options) {
        if (this.dom || !options) return;
        var overlay = options.overlay, sheet = options.sheet, close = options.close;
        if (!overlay || !sheet || !close || !options.title || !options.body) return;
        this.dom = options;
        this.onSessionActions = options.onSessionActions || null;
        var self = this;
        close.addEventListener("click", function () { self.close(true); });
        overlay.addEventListener("click", function () { self.close(true); });
        sheet.addEventListener("click", function (event) {
            event.stopPropagation();
            var sessionActions = event.target.closest &&
                event.target.closest("[data-coordinator-session-actions]");
            if (sessionActions) {
                var id = self.session && self.session.id;
                self.close(false);
                if (id && self.onSessionActions) self.onSessionActions(id);
                return;
            }
            var button = event.target.closest && event.target.closest("[data-command]");
            if (!button || button.disabled) return;
            self.showPreview(button.getAttribute("data-command"));
        });
        document.addEventListener("keydown", function (event) {
            if (!self.dom || self.dom.overlay.hidden) return;
            if (event.key === "Escape") {
                event.preventDefault(); event.stopPropagation(); self.close(true); return;
            }
            if (event.key !== "Tab") return;
            var items = Array.prototype.slice.call(self.dom.sheet.querySelectorAll(
                "button:not(:disabled), [href], [tabindex]:not([tabindex='-1'])"
            ));
            if (!items.length) { event.preventDefault(); return; }
            var at = items.indexOf(document.activeElement);
            if ((!event.shiftKey && at === items.length - 1) || (event.shiftKey && at <= 0)) {
                event.preventDefault();
                items[event.shiftKey ? items.length - 1 : 0].focus({ preventScroll: true });
            }
        }, true);
    },

    open: function (session, opener, context) {
        if (!this.dom || !coordinatorForSession(session)) return false;
        this.session = session;
        this.opener = opener || null;
        this.dom.title.textContent = coordinatorForSession(session).label + " Coordinator controls";
        this.dom.body.innerHTML = coordinatorPanelHTML(session, context);
        this.dom.overlay.hidden = false;
        // The body is reused across opens. A preview can scroll it to the bottom, so reset the
        // now-visible element before focusing a different command list or a narrower viewport.
        // Resetting while the overlay is hidden lets the browser restore its old scroll anchor.
        this.dom.body.scrollTop = 0;
        if (this.opener) this.opener.setAttribute("aria-expanded", "true");
        var first = this.dom.body.querySelector("button:not(:disabled)") || this.dom.close;
        first.focus({ preventScroll: true });
        return true;
    },

    showPreview: function (type) {
        if (!this.session || !this.dom) return;
        var actions = coordinatorGroups(this.session, this.dom.context ? this.dom.context() : {})
            .reduce(function (all, group) { return all.concat(group.actions); }, []);
        var action = actions.find(function (item) { return item.type === type; });
        if (!action || action.state === "disabled" || action.state === "unavailable") return;
        var preview = coordinatorPreview(action);
        var panel = this.dom.body.querySelector(".coordinator-preview");
        if (!panel) return;
        panel.hidden = false;
        panel.innerHTML = '<h3>' + esc(preview.title) + '</h3><p>' +
            esc(preview.summary) + '</p><p class="coordinator-preview-note">' +
            esc(preview.note) + "</p>" + (preview.requiresConfirmation
                ? '<button class="chip" type="button" disabled>Confirm</button>' : "");
        panel.setAttribute("tabindex", "-1");
        panel.focus({ preventScroll: true });
    },

    close: function (restore) {
        if (!this.dom || this.dom.overlay.hidden) return;
        var opener = this.opener;
        this.dom.overlay.hidden = true;
        if (opener) opener.setAttribute("aria-expanded", "false");
        this.session = null;
        this.opener = null;
        if (restore && opener && document.contains(opener)) {
            opener.focus({ preventScroll: true });
        }
    }
};
