import { esc } from "../core/esc.js";
import { T, fill } from "../core/i18n.js";
import { api } from "../net/api.js";

/* --------------------------------------------------------------------------
   Coordinator controls

   `session.coordinator` is the only boundary into this feature. The sessions API is the
   broker-authenticated source; task ancestry, labels and words such as "father" never infer
   coordinator identity. A missing field therefore remains exactly the old session row.

   The four read-only commands are connected: pressing one reads the device-readable Bearings
   projection at `GET /v1/orchestrator/coordinator/bearings` and renders the answer here.
   Nothing in this module sends a command into a session, starts one, or mutates coordinator
   state — those buttons arrive disabled from the server with a closed `reason` code, said in
   this page's own language below.

   Every visible word is a `T` name resolved at render time, never at module load: the
   translated strings arrive from `/v1/strings` after this module is evaluated, and a label
   frozen at import time would stay English forever.
   -------------------------------------------------------------------------- */

var SECTIONS = [
    { id: "observe", label: "webCoordSectionObserve" },
    { id: "coordinate", label: "webCoordSectionCoordinate" },
    { id: "presence", label: "webCoordSectionPresence" },
    { id: "administration", label: "webCoordSectionAdmin" }
];

// Closed on purpose. Labels, placement and safety meaning come from shipped code, never from a
// payload. Adding a server command before adding it here makes it invisible rather than guessed.
var COMMANDS = {
    status_report: {
        label: "webCoordCmdStatusReport", section: "observe", effect: "read_only",
        summary: "webCoordCmdStatusReportSay"
    },
    since_away: {
        label: "webCoordCmdSinceAway", section: "observe", effect: "read_only",
        summary: "webCoordCmdSinceAwaySay"
    },
    duplicates_conflicts_ownership: {
        label: "webCoordCmdDuplicates", section: "observe", effect: "read_only",
        summary: "webCoordCmdDuplicatesSay"
    },
    landing_closure: {
        label: "webCoordCmdLandingClosure", section: "observe", effect: "read_only",
        summary: "webCoordCmdLandingClosureSay"
    },
    coordinate_work: {
        label: "webCoordCmdCoordinateWork", section: "coordinate", effect: "advisory",
        summary: "webCoordCmdCoordinateWorkSay"
    },
    dispatch_independent_work: {
        label: "webCoordCmdDispatch", section: "coordinate", effect: "spawns_session",
        summary: "webCoordCmdDispatchSay"
    },
    ask_coordinator: {
        label: "webCoordCmdAsk", section: "coordinate", effect: "advisory",
        summary: "webCoordCmdAskSay"
    },
    quiet_watch: {
        label: "webCoordCmdQuietWatch", section: "presence", effect: "advisory",
        summary: "webCoordCmdQuietWatchSay"
    },
    scope_permissions: {
        label: "webCoordCmdScope", section: "administration", effect: "read_only",
        summary: "webCoordCmdScopeSay"
    },
    stop: {
        label: "webCoordCmdStop", section: "administration", effect: "mutation",
        summary: "webCoordCmdStopSay"
    },
    reconnect: {
        label: "webCoordCmdReconnect", section: "administration", effect: "mutation",
        summary: "webCoordCmdReconnectSay"
    }
};

var EFFECT_LABELS = {
    read_only: "webCoordEffectRead",
    advisory: "webCoordEffectAdvisory",
    spawns_session: "webCoordEffectSpawns",
    mutation: "webCoordEffectMutation"
};

var STATE_LABELS = {
    available: "webCoordStateAvailable",
    draft: "webCoordStateDraft",
    unavailable: "webCoordStateUnavailable",
    preview: "webCoordStatePreview",
    disabled: "webCoordStateDisabled"
};

// The server's closed disabled-reason codes, said in this page's language. A code this page
// does not know falls back to the server's prose `why`, and a row with neither gets the
// generic sentence — never an empty explanation on a disabled button.
var REASON_KEYS = {
    no_command_route: "webCoordWhyNoCommandRoute",
    no_return_ledger: "webCoordWhyNoReturnLedger",
    device_cannot_spawn: "webCoordWhyDeviceCannotSpawn",
    machine_token_only: "webCoordWhyMachineTokenOnly"
};

function nonempty(value) {
    return typeof value === "string" && value.trim() ? value.trim() : null;
}

/** The honest words under a disabled command: known code first, prose second, never silence. */
export function coordinatorReason(command) {
    var key = REASON_KEYS[nonempty(command && command.reason)];
    if (key) return T[key];
    return nonempty(command && command.why) || T.webCoordDisabledFallback;
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
        badge: "Clawdfather",
        label: coordinator.label,
        mark: {
            role: "button",
            ariaHaspopup: "dialog",
            ariaLabel: fill(T.webCoordOpenControls, { name: coordinator.label })
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
            label: T[definition.label],
            summary: T[definition.summary],
            section: definition.section,
            effect: definition.effect,
            effectLabel: T[EFFECT_LABELS[definition.effect]],
            state: state,
            stateLabel: T[STATE_LABELS[state]],
            why: state === "disabled" ? coordinatorReason(command) : ""
        });
    });

    return SECTIONS.map(function (section) {
        return { id: section.id, label: T[section.label], actions: grouped[section.id] };
    }).filter(function (section) { return section.actions.length > 0; });
}

/** Whether pressing this command performs the connected read rather than showing a receipt. */
export function coordinatorExecutes(action) {
    return !!action && action.effect === "read_only" && action.state === "available";
}

/**
 * The body of one read-only command's answer, rendered from the device-readable Bearings
 * projection. Pure: the payload and today's `T` words in, escaped HTML out. The heading is the
 * command's own label and is added by the caller, which also owns loading and failure states.
 */
export function coordinatorAnswerHTML(action, data) {
    var record = data && typeof data === "object" ? data : {};
    var coordinator = record.coordinator && typeof record.coordinator === "object"
        ? record.coordinator : {};
    var bearings = record.bearings && typeof record.bearings === "object"
        ? record.bearings : {};
    var counts = bearings.work_state_counts || {};
    var name = nonempty(coordinator.label) || "Clawdfather";
    var type = action && action.type;
    var out = [];

    function line(text, kind) {
        return '<p class="coordinator-answer' + (kind ? "-" + kind : "-line") + '">' +
            esc(text) + "</p>";
    }
    function list(title, rows) {
        if (!Array.isArray(rows) || !rows.length) return "";
        return '<div class="coordinator-answer-list"><h4>' + esc(title) + "</h4><ul>" +
            rows.map(function (row) {
                return "<li>" + esc(nonempty(row && row.label) || nonempty(row && row.id) || "?") +
                    "</li>";
            }).join("") + "</ul></div>";
    }
    function presence() {
        if (coordinator.configured === false) return T.webCoordUnregistered;
        return fill(coordinator.status === "online" ? T.webCoordOnline : T.webCoordOffline,
                    { name: name });
    }

    if (type === "status_report") {
        out.push(line(presence()));
        out.push(line([
            fill(T.webCountWorking, { n: counts.working || 0 }),
            fill(T.webCountWaiting, {
                n: (counts.waiting_you || 0) + (counts.waiting_session || 0)
            }),
            fill(T.webCoordCountUnknown, { n: counts.unknown || 0 })
        ].join(" · ")));
        out.push(line([
            fill(T.webCoordActiveTasks, { n: bearings.active_task_count || 0 }),
            fill(T.webCoordPendingLandings, { n: bearings.pending_landing_count || 0 }),
            fill(T.webCoordOpenWaits, { n: bearings.open_wait_count || 0 })
        ].join(" · ")));
    } else if (type === "duplicates_conflicts_ownership") {
        var lists = list(T.webCoordBlockingList, bearings.blocking) +
            list(T.webCoordWaitingList, bearings.waiting) +
            list(T.webCoordUnknown, bearings.unknown);
        out.push(lists || line(T.webCoordAllQuiet));
        out.push(line(fill(T.webCoordOpenWaits, { n: bearings.open_wait_count || 0 })));
    } else if (type === "landing_closure") {
        var landings = bearings.pending_landing_count || 0;
        out.push(line(landings
            ? fill(T.webCoordPendingLandings, { n: landings })
            : T.webCoordNoLandings));
    } else if (type === "scope_permissions") {
        out.push(line(coordinator.configured === false
            ? T.webCoordUnregistered
            : fill(T.webCoordScopeLine, { name: name })));
        out.push(line(presence()));
        out.push(line(T.webCoordScopeDevice));
    }

    var sessions = bearings.sources && bearings.sources.sessions;
    if (sessions && sessions.freshness === "stale") {
        out.push(line(T.webCoordStaleSessions, "stale"));
    }
    return out.join("");
}

/** The preview receipt for anything that is not a connected read. Nothing behind it sends. */
export function coordinatorPreview(action) {
    var mutation = action && action.effect === "mutation";
    var note;
    if (!action) {
        note = T.webCoordPreviewNone;
    } else if (mutation) {
        note = T.webCoordPreviewMutation;
    } else if (action.state === "draft") {
        note = T.webCoordPreviewDraft;
    } else if (action.effect === "spawns_session") {
        note = T.webCoordPreviewSpawn;
    } else {
        note = T.webCoordPreviewContract;
    }
    return {
        title: action ? action.label : T.webCoordPreviewTitle,
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
        sections = '<p class="coordinator-empty">' + esc(T.webCoordEmpty) + "</p>";
    }
    return '<div class="coordinator-presence" data-status="' + (online ? "online" : "offline") + '">' +
        '<span class="coordinator-presence-dot" aria-hidden="true"></span><span>' +
        esc(fill(online ? T.webCoordOnline : T.webCoordOffline, { name: coordinator.label })) +
        "</span></div>" +
        '<div class="coordinator-groups">' + sections + "</div>" +
        '<section class="coordinator-preview" aria-live="polite" hidden></section>' +
        '<button class="chip wide coordinator-session-actions" type="button" ' +
        'data-coordinator-session-actions>' + esc(T.webSessionActions) + "</button>";
}

/** DOM wiring is injected by `main.js`; importing this module in Node stays browser-independent. */
export var CoordinatorControls = {
    dom: null,
    session: null,
    opener: null,
    onSessionActions: null,
    // A ticket per connected read, so an answer that arrives after the sheet closed or after
    // another command was pressed is dropped rather than drawn over the wrong heading.
    ticket: 0,

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
        this.dom.title.textContent =
            fill(T.webCoordControlsTitle, { name: coordinatorForSession(session).label });
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
        var panel = this.dom.body.querySelector(".coordinator-preview");
        if (!panel) return;
        panel.hidden = false;
        if (coordinatorExecutes(action)) {
            this.showAnswer(action, panel);
        } else {
            var preview = coordinatorPreview(action);
            panel.innerHTML = '<h3>' + esc(preview.title) + '</h3><p>' +
                esc(preview.summary) + '</p><p class="coordinator-preview-note">' +
                esc(preview.note) + "</p>" + (preview.requiresConfirmation
                    ? '<button class="chip" type="button" disabled>' +
                      esc(T.webConfirm) + "</button>" : "");
        }
        panel.setAttribute("tabindex", "-1");
        panel.focus({ preventScroll: true });
    },

    /** The connected read behind the four read-only commands. */
    showAnswer: function (action, panel) {
        var heading = '<h3>' + esc(action.label) + "</h3>";
        // The fixtures and older transports may not carry this read; a missing method is the
        // same honest sentence as a failed one, not a button that pretends to have worked.
        if (!api || typeof api.coordinatorBearings !== "function") {
            panel.innerHTML = heading +
                '<p class="coordinator-answer-err">' + esc(T.webCoordReadFailed) + "</p>";
            return;
        }
        var self = this;
        var mine = ++this.ticket;
        panel.innerHTML = heading + '<p class="coordinator-answer-line">' +
            esc(T.webLoading) + "</p>";
        api.coordinatorBearings().then(function (data) {
            if (mine !== self.ticket || !self.dom || self.dom.overlay.hidden) return;
            panel.innerHTML = heading + coordinatorAnswerHTML(action, data);
        }).catch(function () {
            if (mine !== self.ticket || !self.dom || self.dom.overlay.hidden) return;
            panel.innerHTML = heading +
                '<p class="coordinator-answer-err">' + esc(T.webCoordReadFailed) + "</p>";
        });
    },

    close: function (restore) {
        if (!this.dom || this.dom.overlay.hidden) return;
        var opener = this.opener;
        this.ticket += 1;
        this.dom.overlay.hidden = true;
        if (opener) opener.setAttribute("aria-expanded", "false");
        this.session = null;
        this.opener = null;
        if (restore && opener && document.contains(opener)) {
            opener.focus({ preventScroll: true });
        }
    }
};
