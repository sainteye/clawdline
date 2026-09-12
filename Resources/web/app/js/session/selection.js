/* --------------------------------------------------------------------------
   Session selection and effect lifetime

   A terminal id is display metadata inside a fleet.  The selectable thing is the route that
   owns it (machine + terminal/session id), plus the provider conversation id when the inventory
   has one.  This module owns that identity and the epoch in which an asynchronous effect began.
   It deliberately imports no transport, state or renderer: transports accept its frozen route,
   and renderers receive its frozen snapshots or callbacks from the composition root.
   -------------------------------------------------------------------------- */

export const LOCAL_SESSION_MACHINE = "this-mac";

function nonempty(value) {
    return typeof value === "string" && value.length > 0 ? value : null;
}

/**
 * An identity this module has already minted is closed. Re-deriving one from row fields would
 * read a `sessionId` that an identity does not carry — it spells that field `conversation` — and
 * silently return a key with `null` in the conversation position. Every caller that hands a
 * snapshot back (`sessionSelectionKey(open)`, and `byId(open)` through it) would then stop
 * matching the very row the selection came from.
 */
function mintedSelectionIdentity(value) {
    return value && typeof value === "object" && !Array.isArray(value) &&
        typeof value.key === "string" && value.key &&
        typeof value.rowId === "string" && value.rowId &&
        typeof value.machine === "string" && value.machine &&
        typeof value.session === "string" && value.session ? value : null;
}

/** The closed, immutable identity used for selection and stale-response checks. */
export function sessionSelectionIdentity(row, fallbackMachine) {
    var minted = mintedSelectionIdentity(row);
    if (minted) return minted;
    if (!row || typeof row !== "object" || Array.isArray(row)) return null;
    var carried = row.identity && typeof row.identity === "object" ? row.identity : null;
    var machine = nonempty(carried && carried.machine) || nonempty(row.machine) ||
        nonempty(fallbackMachine) || LOCAL_SESSION_MACHINE;
    var routeSession = nonempty(carried && carried.session) || nonempty(row.session) ||
        nonempty(row.id);
    var rowID = nonempty(row.id) || routeSession;
    if (!machine || !routeSession || !rowID) return null;
    var conversation = nonempty(row.sessionId);
    // Keep the established composer-pin spelling while making the owning route explicit.
    var key = JSON.stringify([rowID, machine, conversation || null]);
    return Object.freeze({
        key: key,
        rowId: rowID,
        machine: machine,
        session: routeSession,
        conversation: conversation,
        route: Object.freeze({ machine: machine, session: routeSession })
    });
}

export function sessionSelectionKey(row, fallbackMachine) {
    var identity = sessionSelectionIdentity(row, fallbackMachine);
    return identity ? identity.key : null;
}

function asSelectionIdentity(value) {
    return sessionSelectionIdentity(value);
}

function sameIdentity(left, right) {
    return !!left && !!right && left.key === right.key;
}

function sameRouteIdentity(left, right) {
    return !!left && !!right && left.rowId === right.rowId && left.machine === right.machine &&
        left.session === right.session;
}

/**
 * Whether a later inventory identity is still the one an earlier operation targeted.
 *
 * A provider conversation UUID may be learned after a terminal first appears.  That one-way
 * enrichment is continuity because the already-authenticated machine/session route did not move.
 * Once a UUID is known, however, a different UUID (or its disappearance) is replacement.  In
 * particular this rule never compares titles or guesses across machines.
 */
export function sameSessionSelection(earlier, later) {
    earlier = asSelectionIdentity(earlier);
    later = asSelectionIdentity(later);
    if (!sameRouteIdentity(earlier, later)) return false;
    return earlier.conversation === later.conversation ||
        (!earlier.conversation && !!later.conversation);
}

function frozenSnapshot(selected, open, epoch, transportEpoch) {
    return Object.freeze({ selected: selected, open: open, epoch: epoch,
        transportEpoch: transportEpoch });
}

export function createSessionSelectionLifecycle() {
    var selected = null;
    var open = null;
    var epoch = 0;
    var transportEpoch = 0;
    var nextEffect = 0;
    var latestByLane = new Map();
    var listeners = new Set();
    var mirror = null;
    var inventory = [];

    function snapshot() {
        return frozenSnapshot(selected, open, epoch, transportEpoch);
    }

    function project() {
        if (mirror) {
            mirror.selectedId = selected ? selected.rowId : null;
            mirror.openId = open ? open.rowId : null;
            mirror.replyComposerIdentity = open ? open.key : null;
        }
        var value = snapshot();
        listeners.forEach(function (listener) { listener(value); });
        return value;
    }

    function invalidate() {
        epoch += 1;
    }

    function identities(rows) {
        return (Array.isArray(rows) ? rows : []).map(function (row) {
            return { row: row, identity: sessionSelectionIdentity(row) };
        }).filter(function (entry) { return !!entry.identity; });
    }

    function remember(rows) {
        inventory = identities(rows);
        return inventory;
    }

    function matchingEntry(identity, entries) {
        var exact = entries.find(function (entry) {
            return entry.identity.key === identity.key;
        });
        if (exact) return exact;
        // Only missing -> present is continuity. Requiring one candidate keeps duplicate or
        // malformed inventory fail closed even when its route fields happen to agree.
        if (identity.conversation) return null;
        var enriched = entries.filter(function (entry) {
            return sameSessionSelection(identity, entry.identity);
        });
        return enriched.length === 1 ? enriched[0] : null;
    }

    /** Resolve an explicit row/identity. A duplicate bare terminal id is never guessed. */
    function resolve(value, rows) {
        var entries = identities(rows);
        var explicit = asSelectionIdentity(value);
        if (explicit) {
            return matchingEntry(explicit, entries);
        }
        if (typeof value !== "string" || !value) return null;
        var exact = entries.filter(function (entry) { return entry.identity.key === value; });
        if (exact.length === 1) return exact[0];
        var bare = entries.filter(function (entry) {
            return entry.identity.rowId === value || entry.identity.session === value;
        });
        return bare.length === 1 ? bare[0] : null;
    }

    function select(value, rows) {
        remember(rows);
        var entry = resolve(value, rows);
        var next = entry ? entry.identity : null;
        if (sameIdentity(selected, next)) return entry;
        selected = next;
        project();
        return entry;
    }

    /** Explicit open/reopen always starts a new epoch, even when the route is unchanged. */
    function openSession(value, rows) {
        remember(rows);
        var entry = resolve(value, rows);
        if (!entry) return null;
        var changed = !sameIdentity(open, entry.identity);
        selected = entry.identity;
        open = entry.identity;
        invalidate();
        project();
        return Object.freeze({ row: entry.row, identity: entry.identity, changed: changed });
    }

    function closeSession() {
        if (!open && !selected) return snapshot();
        open = null;
        invalidate();
        project();
        return snapshot();
    }

    /**
     * Reconcile an inventory atomically.  A row with the same terminal id but a different machine
     * or provider conversation is a replacement, not continuity, so the old selection is closed.
     */
    function reconcile(rows) {
        var entries = remember(rows);
        function retention(identity) {
            if (!identity) return { identity: null, replacement: false };
            var entry = matchingEntry(identity, entries);
            if (entry) return { identity: entry.identity, replacement: false };
            return { identity: null, replacement: entries.some(function (candidate) {
                return sameRouteIdentity(identity, candidate.identity);
            }) };
        }
        var oldOpen = open, oldSelected = selected;
        var openRetention = retention(open), selectedRetention = retention(selected);
        var nextOpen = openRetention.identity;
        var nextSelected = selectedRetention.identity;
        var openChanged = !!open && !nextOpen;
        var selectedChanged = !!selected && !nextSelected;
        var enriched = (!!oldOpen && !!nextOpen && oldOpen.key !== nextOpen.key) ||
            (!!oldSelected && !!nextSelected && oldSelected.key !== nextSelected.key);
        if (!openChanged && !selectedChanged && !enriched) return Object.freeze({
            openRemoved: false, openReplacement: false, selectedRemoved: false,
            snapshot: snapshot()
        });
        open = nextOpen;
        selected = nextSelected;
        if (openChanged) invalidate();
        project();
        return Object.freeze({ openRemoved: openChanged,
            openReplacement: openChanged && openRetention.replacement,
            selectedRemoved: selectedChanged, snapshot: snapshot() });
    }

    /** Switching local/mock/Cloud clients is a hard effect boundary. */
    function replaceTransport() {
        transportEpoch += 1;
        latestByLane.clear();
        if (open || selected) {
            open = null;
            selected = null;
        }
        invalidate();
        project();
        return snapshot();
    }

    /**
     * Start one named asynchronous effect. By default the newest effect in a lane supersedes the
     * older one; independent operations use distinct lane names and may overlap safely.
     */
    function beginEffect(lane, options) {
        options = options || {};
        var identity = asSelectionIdentity(options.identity) || open;
        if (!identity || typeof lane !== "string" || !lane) return null;
        var serial = ++nextEffect;
        // Conversation-id enrichment must not split one logical route into two newest-effect
        // lanes. A real UUID replacement invalidates the epoch/presence separately.
        var laneKey = [lane, identity.rowId, identity.machine, identity.session].join("\u0000");
        var token = Object.freeze({ lane: lane, serial: serial, identity: identity,
            laneKey: laneKey, epoch: epoch, transportEpoch: transportEpoch,
            requiresOpen: options.requiresOpen !== false,
            allowMissing: options.allowMissing === true });
        if (options.latest !== false) latestByLane.set(laneKey, serial);
        return token;
    }

    function effectIsCurrent(token, options) {
        options = options || {};
        if (!token || token.transportEpoch !== transportEpoch) return false;
        if (token.requiresOpen) {
            if (token.epoch !== epoch || !open || !sameSessionSelection(token.identity, open)) {
                return false;
            }
        } else if (!token.allowMissing && !inventory.some(function (entry) {
            return sameSessionSelection(token.identity, entry.identity);
        })) return false;
        return options.latest === false || latestByLane.get(token.laneKey) === token.serial;
    }

    function finishEffect(token, options) {
        var current = effectIsCurrent(token, options);
        if (token && latestByLane.get(token.laneKey) === token.serial) {
            latestByLane.delete(token.laneKey);
        }
        return current;
    }

    return Object.freeze({
        snapshot: snapshot,
        resolve: resolve,
        select: select,
        open: openSession,
        close: closeSession,
        reconcile: reconcile,
        replaceTransport: replaceTransport,
        matches: sameSessionSelection,
        beginEffect: beginEffect,
        effectIsCurrent: effectIsCurrent,
        finishEffect: finishEffect,
        bindLegacyMirror: function (state) { mirror = state || null; project(); },
        subscribe: function (listener) {
            if (typeof listener !== "function") throw new TypeError("selection listener required");
            listeners.add(listener);
            listener(snapshot());
            return function () { listeners.delete(listener); };
        }
    });
}

export const SessionSelection = createSessionSelectionLifecycle();
