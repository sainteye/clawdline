import { phone, releaseKeyboardFocus } from "../core/env.js";
import { Diagnostics } from "../core/layout-diagnostics.js";
import { T } from "../core/i18n.js";
import { failureSentence } from "../core/failure-text.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { Pages } from "../core/pages.js";
import { api } from "../net/api.js";
import { byId } from "../view/derive.js";
import { whenVisible } from "../core/visibility.js";
import { Optimistic, Waits } from "../view/waits.js";
import {
    beginTranscriptLoad,
    createPendingTranscriptFollower,
    createTranscriptRefetchPolicy,
    createTranscriptRequests,
    createTranscriptRevisionObserver,
    transcriptSignatureOf
} from "./transcript-requests.js";
import { reconcileOptimisticBeforeSignature } from "../view/optimistic-data.js";
import { SessionSelection } from "./selection.js";
import { callSessionUI } from "./ui.js";

SessionSelection.bindLegacyMirror(S);
// Compatibility note for the legacy source-contract guard: the old owner performed
// `S.replyComposerIdentity = replySessionIdentity(s)` on open and
// `S.replyComposerIdentity = null` on close. The lifecycle mirror now projects both writes.

/* ==========================================================================
   8. Opening a session
   ========================================================================== */

var transcriptRequests = createTranscriptRequests(function (key, demand) {
    var entry = SessionSelection.resolve(key, S.sessions);
    if (!entry) return Promise.reject(Object.assign(new Error("session selection changed"), {
        code: "stale_selection"
    }));
    return api.transcript(entry.identity.route, {
        response: function (data) { Diagnostics.note("transcript.response", data); },
        parse: function (data) { Diagnostics.note("transcript.parse", data); }
    }, demand);
}, settleTranscript, {
    phase: function (name, data) {
        Diagnostics.note("transcript." + name, data);
    }
});

var pendingTranscriptFollower = createPendingTranscriptFollower(function (key) {
    return loadTranscript(key, true);
}, function (key) {
    var entry = SessionSelection.resolve(key, S.sessions);
    return !!entry && Optimistic.entries(entry.identity.key).length > 0;
});

export function followPendingTranscript(id) {
    return pendingTranscriptFollower.start(id);
}

// The renderer emits this only after content has reached a paint opportunity. It contains no
// session id or prose; the currently open session is the only one whose optional hydration can
// be released, and switching has already moved StatusLine's gate to the new id.
document.addEventListener("clawdline:meaningful-transcript-paint", function () {
    var current = SessionSelection.snapshot().open;
    if (current) {
        Diagnostics.note("session.extras.begin", {});
        callSessionUI("resumeStatusLine", current.key);
        callSessionUI("resumeSessionBoard");
    }
});

var transcriptRevisions = createTranscriptRevisionObserver(function (id, revision, quiet, demand) {
    // Demand is not yet a read. A row whose signature already names the transcript on screen —
    // because the pending follower or a file event read it first — has nothing to fetch, and the
    // revision is settled as observed without asking the Mac for the same bytes again.
    if (quiet && transcriptOnScreenIsCurrent(id)) {
        Diagnostics.note("transcript.demand.satisfied", {});
        transcriptRevisions.settle(id, revision, true);
        return;
    }
    loadTranscript(id, quiet, revision, demand);
});

/**
 * What a session row is allowed to cost in transcript reads — see `createTranscriptRefetchPolicy`.
 * Only the open session is ever fed in, so its one held-line timer belongs to that session.
 */
var transcriptRefetch = createTranscriptRefetchPolicy({
    due: function (key) {
        // A held line change comes due. A hidden page reads nothing: the change is looked at
        // again when it is visible, against whatever row is current by then.
        whenVisible(function () {
            var current = SessionSelection.snapshot().open;
            var entry = current && current.key === key ? SessionSelection.resolve(key, S.sessions) : null;
            if (entry && entry.row) observeTranscriptRow(key, entry.row, true);
        });
    }
});

/**
 * Whether the transcript on screen is already the one this row names: same session, a readable
 * answer, the row's own signature, and read while the row was in the state it is in now. Any
 * doubt answers no, and no means an ordinary read.
 */
function transcriptOnScreenIsCurrent(key) {
    var current = SessionSelection.snapshot().open;
    if (!current || current.key !== key) return false;
    var entry = SessionSelection.resolve(key, S.sessions);
    var signature = entry && transcriptSignatureOf(entry.row);
    return !!signature && S.tx.id === entry.identity.rowId && !S.tx.loading && !S.tx.error &&
        S.tx.signature === signature && S.tx.rowState === (entry.row.state || "");
}

/** A session row for the open session arrived. It becomes transcript demand only by policy. */
export function observeTranscriptRow(id, row, quiet) {
    transcriptRevisions.observe(id, transcriptRefetch.revision(id, row), quiet);
}

var transcriptFileSignatures = {};

export function observeTranscriptFileRevision(id, signature) {
    var current = SessionSelection.snapshot().open;
    if (!id || !signature || !current || current.key !== id ||
        transcriptFileSignatures[id] === signature) {
        return;
    }
    transcriptFileSignatures[id] = signature;
    loadTranscript(id, true);
}

export function rearmTranscriptRow(id, row, quiet) {
    transcriptRevisions.rearm(id, transcriptRefetch.revision(id, row), quiet);
}

export function loadTranscript(id, quiet, revision, demand) {
    // Composer/refresh callers predate revision tracking. Fold them into the same observed
    // contract so a direct refresh cannot overwrite the coalesced cycle's revision context.
    var entry = SessionSelection.resolve(id, S.sessions);
    if (!entry) return Promise.resolve({ accepted: false, stale: true });
    var session = entry.row;
    var key = entry.identity.key;
    var rowID = entry.identity.rowId;
    if (revision == null && session) revision = transcriptRefetch.peek(key, session);
    var effect = SessionSelection.beginEffect("transcript", { identity: entry.identity });
    if (!effect) return Promise.resolve({ accepted: false, stale: true });
    var ticket = effect.serial;
    // The row's state when this read was asked for. An answer is "the transcript for this row"
    // only in that state — see `transcriptOnScreenIsCurrent`.
    var context = { revision: revision, effect: effect, rowState: session ? (session.state || "") : null };
    if (!quiet) {
        S.tx = {
            id: rowID, entries: [], signature: null, revision: null, rowState: null,
            loading: true, error: null
        };
        // Only the loud kind waits visibly. A refetch behind a transcript that is already on
        // screen has nothing to stand in for — the reader is reading the last version of it.
        Waits.tx.start();
        return beginTranscriptLoad(function () {
            return transcriptRequests(key, ticket, context, { foreground: true });
        }, function () { callSessionUI("renderTranscript"); });
    }
    // Returned, so a control that started this can wait for the whole coalesced cycle. A revision
    // storm gets one active read and one trailing read, whose answer owns the newest ticket.
    return transcriptRequests(key, ticket, context, {
        foreground: !!(demand && demand.foreground)
    });
}

function settleTranscript(key, ticket, outcome, context) {
    var effect = context && context.effect;
    var revision = context && context.revision;
    var entry = SessionSelection.resolve(key, S.sessions);
    var rowID = entry && entry.identity.rowId;
    var current = !!effect && SessionSelection.effectIsCurrent(effect);
    Diagnostics.note("transcript.settle", {
        openMatches: current, ticketMatches: current,
        failed: !!outcome.error, revisionKnown: revision != null
    });
    if (revision != null) {
        transcriptRevisions.settle(key, revision, !outcome.error, outcome.error);
    }
    // A later request owns both the result and the visible wait. Settling an older request here
    // would take down the skeleton while the request that superseded it is still out.
    if (!current || !entry) return;
    if (!outcome.error) {
        var d = outcome.value || {};
        var received = d.entries || [];
        // Reconcile before trusting the signature. The common first fetch after a send quite
        // correctly says the file is unchanged; that must preserve the echo, while an eventual
        // matching entry must retire it even if an older server reports a stale signature.
        var reconciled = reconcileOptimisticBeforeSignature(function (sessionID, entries) {
            return Optimistic.reconcile(sessionID, entries, d.optimisticIdentity);
        }, entry.identity.key, received);
        // The signature is the server's own answer to "is this the same transcript". Trusting it
        // is what keeps a refetch from throwing the reader's scroll position away every few seconds.
        //
        // **And an unchanged answer is not redrawn.** `renderTranscript` rebuilds every entry's
        // markdown and replaces every node, which restarts the live sweep and was the second half
        // of the cost the phone paid for each byte-identical read. Redraw only when something
        // the pane draws did change: an echo retired, an error line to take down, a skeleton or
        // wait to replace, or the session's working state that decides the live sweep.
        if (d.signature && d.signature === S.tx.signature) {
            if (revision != null) S.tx.revision = revision;
            var redraw = reconciled || !!S.tx.error || !!S.tx.loading ||
                Waits.tx.visible || !!Waits.tx.timer ||
                callSessionUI("transcriptWorkingChanged") !== false;
            S.tx.loading = false;
            S.tx.error = null;
            if (context && context.rowState != null) S.tx.rowState = context.rowState;
            if (reconciled) S.tx.entries = received;
            Diagnostics.note("transcript.unchanged", { redraw: redraw });
            Waits.tx.settle(redraw ? function () { callSessionUI("renderTranscript"); } : null);
            return;
        }
        var stick = atBottom();
        S.tx = {
            id: rowID, entries: received, signature: d.signature || null,
            revision: revision != null ? revision : S.tx.revision,
            rowState: context && context.rowState != null ? context.rowState : null,
            loading: false, error: null
        };
        Waits.tx.settle(function () {
            callSessionUI("renderTranscript");
            if (stick) toBottom();
        });
        return;
    }
    var e = outcome.error;
    // A read that failed keeps whatever is already on screen. It is the same rule as the
    // skeleton above and it is here for the same reason — the reader is reading the last
    // version of it — except that this is the branch where it matters: the list refetches
    // roughly once a second while a session works, so one refused read used to empty the pane
    // somebody was mid-sentence in, with no gesture of theirs behind it.
    //
    // **Not only `busy`.** A dropped connection, a 500 and a refusal all leave the same thing
    // true: the last transcript that arrived is still the best answer there is, and throwing
    // it away buys nothing. Only a first load has nothing to keep, and that one still says so
    // with the whole pane.
    var held = S.tx.id === rowID ? S.tx.entries : [];
    S.tx = {
        id: rowID,
        entries: held,
        // Kept with them. The signature is the server's name for *these* entries, so holding
        // it is what lets the next read that comes back unchanged be believed; nulling it
        // would turn the recovery into a full replace and take the reader's scroll with it.
        signature: held.length ? S.tx.signature : null,
        revision: S.tx.id === rowID ? S.tx.revision : null,
        rowState: S.tx.id === rowID ? S.tx.rowState : null,
        loading: false,
        error: whyTranscript(e)
    };
    Waits.tx.settle(function () { callSessionUI("renderTranscript"); });
}

/**
 * What went wrong, in this page's own words — the same shape as `whyIntents` beside the composer
 * and `why` on the info card.
 *
 * Only `offline` carries its own message through, because `jsonFetch` wrote that one here and it
 * is already translated. Everything else arrives from the Mac in English, and a server sentence
 * put in front of somebody reading Chinese is a fault report in a language they did not pick.
 *
 * With entries held it is drawn as a line above the transcript rather than instead of it — see
 * `renderTranscript`, which has had both branches all along and only ever reached the empty one.
 */
function whyTranscript(e) {
    return failureSentence(e, T.webTranscriptFailed);
}

export function atBottom() {
    var el = els["tx-scroll"];
    return el.scrollTop + el.clientHeight >= el.scrollHeight - 40;
}
export function toBottom() {
    var el = els["tx-scroll"];
    el.scrollTop = el.scrollHeight;
}

export function openSession(id, keepFocus, forceRefresh) {
    var resolved = SessionSelection.resolve(id, S.sessions);
    if (!resolved || callSessionUI("closingSelectionKey") === resolved.identity.key) return;
    var before = SessionSelection.snapshot().open;
    var switching = !before || before.key !== resolved.identity.key;
    var s = resolved.row;
    var identity = resolved.identity;
    // A session lives on the sessions page, so opening one means being there. It matters for the
    // push that arrives while somebody is reading Usage: the fragment routes, the transcript
    // loads, and without this line all of it happens underneath a page that is still on screen.
    Pages.goHome();
    Diagnostics.note("session.open.begin", {
        switching: switching, phone: phone(), view: els.app.dataset.view,
        forceRefresh: !!forceRefresh
    });
    SessionSelection.select(identity, S.sessions);
    if (switching) {
        if (before) {
            pendingTranscriptFollower.stop(before.key);
            transcriptRevisions.stop(before.key);
            transcriptRefetch.forget(before.key);
            delete transcriptFileSignatures[before.key];
        }
        callSessionUI("closeSessionActions");
        callSessionUI("closeActionConfirm");
        // An agent belongs to the session that sent it away. Carrying one over into the next
        // session would leave somebody reading one session's background work under another
        // session's name, which is the one thing this pane must never do.
        callSessionUI("closeAgent", true);
        // Inventory/quiet refresh cannot rebind this pin; an explicit open establishes it.
        SessionSelection.open(identity, S.sessions);
        // Which runs were open is where a reader had got to in that transcript, not a setting.
        // Fold keys come from content and so would not collide across sessions, but carrying
        // them over means arriving in a new transcript with something already open.
        S.expanded = {};
        // And a picture picked for one session is not a picture for the next one.
        callSessionUI("clearShots");
        callSessionUI("deferStatusLine", identity.key);
        callSessionUI("followSessionBoard", s);
        transcriptRequests.activate(identity.key);
        observeTranscriptRow(identity.key, s, false);
        // These surfaces may draw immediately, so they follow the synchronous transcript issue.
        callSessionUI("followInfo");
        callSessionUI("followSnippets");
        callSessionUI("followGitPanel");
        callSessionUI("followShellPanel");
        callSessionUI("followTerminal");
    } else if (forceRefresh) loadTranscript(identity.key, true);
    if (phone()) {
        // A touch on a row does not reliably take focus from the filter on iOS. Release it before
        // the list becomes invisible so the keyboard's outgoing viewport cannot become the
        // detail pane's permanent height. Do this only for the screen transition: routing a push
        // back to the session already being composed in must not dismiss that composer.
        if (els.app.dataset.view !== "detail") releaseKeyboardFocus();
        els.app.dataset.view = "detail";
        // The phone's own back gesture should mean what it looks like it means.
        try { history.pushState({ view: "detail", id: identity.rowId }, ""); } catch (e) { }
    } else if (!S.paneOpen) {
        S.paneOpen = true;
        els.app.dataset.pane = "on";
    }
    callSessionUI("render");
    Diagnostics.note("session.open.rendered", {
        view: els.app.dataset.view, loading: !!S.tx.loading,
        entries: (S.tx.entries || []).length
    });
    callSessionUI("skillPickerChanged");
    if (!keepFocus && !phone()) {
        var node = callSessionUI("rowNode", identity.key, identity.rowId);
        if (node) node.focus({ preventScroll: true });
    }
}

export function closeDetail(silent) {
    var current = SessionSelection.snapshot().open;
    // The confirmation owns this session until its one-way request settles. In particular, a
    // phone back gesture must not uncover a writable-looking list while the same session is
    // still closing underneath it; the successful end clears `closingID` before coming here.
    if (current && callSessionUI("closingSelectionKey") === current.key) return;
    callSessionUI("closeActionConfirm");
    if (current) {
        pendingTranscriptFollower.stop(current.key);
        transcriptRevisions.stop(current.key);
        transcriptRefetch.forget(current.key);
        delete transcriptFileSignatures[current.key];
    }
    SessionSelection.close();
    callSessionUI("followSessionBoard", null);
    transcriptRequests.activate(null);
    S.agent = null;
    S.tx = {
        id: null, entries: [], signature: null, revision: null, rowState: null,
        loading: false, error: null
    };
    S.expanded = {};
    callSessionUI("clearShots");
    callSessionUI("followInfo");
    callSessionUI("followSnippets");
    callSessionUI("followGitPanel");
    callSessionUI("followShellPanel");
    callSessionUI("followTerminal");
    callSessionUI("skillPickerClose");
    if (phone()) {
        els.app.dataset.view = "list";
        // A notification arrives at `#session=…`. Leaving its detail must also leave that route:
        // otherwise the list is drawn under an address that still asks for a Session, and a later
        // hash read or reload opens it again. Replace rather than assign so clearing the route does
        // not create one more Back step.
        try {
            history.replaceState({ view: "list" }, "", location.pathname + location.search);
        } catch (e) {
            try { location.hash = ""; } catch (ignored) { }
        }
    }
    callSessionUI("renderTranscript");
    if (!silent) callSessionUI("render");
}
