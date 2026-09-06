import { Pages, pageInHash } from "../core/pages.js";
import { byId } from "../view/derive.js";
import { openSession } from "../session/open.js";
import { Diagnostics } from "../core/layout-diagnostics.js";

/* ---- arriving at a session from somewhere else ---------------------------
 *
 * A notification about a session carries `/#session=<id>`, and tapping it landed on the session
 * list. Three things had to be true for it to land on the session; this file was the third.
 *
 * **A fragment is only read when a document loads.** Sending an already-open page to
 * `/#session=abc` changes the address and navigates nothing — same document, no reload, and this
 * page had no other opinion about the fragment, so the tap put the app in front of somebody at
 * the screen they were already on. The service worker now sends a message as well, for the case
 * where it cannot navigate the client at all; both roads end here.
 *
 * The id is held rather than acted on when the list has not arrived yet: a cold start routes
 * before it knows what sessions exist, and `onSessions` tries again with every list.
 */
export var wantedSession = null;

/**
 * The same request, spelled the other way, and it is not a nicety.
 *
 * A session id on this Mac is usually a tmux pane: `%141`. Written into the fragment raw — which
 * is what every notification did until `WebPush.sessionURL(forSessionID:)` — it arrives here as
 * `session=%141`, and `decodeURIComponent` does not refuse that. `%14` is a complete escape, so
 * the id became U+0014 followed by `1`, `byId` found no such session, the first whole list let go
 * of the request, and the tap stopped on the session list with nothing to say why. Ids without a
 * per-cent in them — iTerm's `w0t0p0:…` — went through both roads unchanged, which is why this
 * survived every fixture in `Tests/` and only ever happened on a phone.
 *
 * The address is written encoded now. This is the other half: the notifications already sitting
 * on somebody's phone carry the old spelling and are tapped days later, so the text exactly as it
 * was written stays a candidate beside the decoding of it. Held rather than resolved, because a
 * cold start routes before the list has arrived and both candidates have to survive that wait.
 */
var wantedSessionAsWritten = null;

/// Cleared from the list as well, once the first list has settled whether the session it names is
/// there at all, and a name arriving there by import is read-only. Same variable, one more hop —
/// and the second candidate goes with it, or letting go would not be letting go.
///
/// **And the worker's record goes with it too, when the request being let go of is the one that
/// record made.** The first whole list is the answer to "is this session here at all", and
/// `list.js` calls this when the answer is no. Until this line the record outlived that answer: it
/// was still `pendingWant`, so every later wake-up called it settled *and* refused to collect it,
/// and a reload inside the two-minute window carried out a request this page had already
/// established was for a session that is gone.
export function setWantedSession(id) {
    if (!id && pendingWant &&
        (pendingWantNames(wantedSession) || pendingWantNames(wantedSessionAsWritten))) {
        pendingWant = "";
        pendingWantFor = null;
    }
    wantedSession = id;
    wantedSessionAsWritten = null;
}

/**
 * What the fragment could be naming: the decoding first, and the raw text after it when the two
 * differ. Nothing when the fragment names no session at all.
 */
function sessionCandidates(hash) {
    var found = /(?:^|[#&])session=([^&]*)/.exec(String(hash || ""));
    if (!found || !found[1]) return null;
    var raw = found[1];
    var decoded = raw;
    try { decoded = decodeURIComponent(raw); } catch (e) { decoded = raw; }
    if (decoded === raw) return [raw];
    // **The second candidate is kept only when the decoding is itself the evidence.** A link
    // written before the encoding decodes to something no session could be called: `%141` becomes
    // U+0014 followed by `1`, because a pane id's per-cent was read as the start of an escape.
    // A link written after it decodes to a real id — `%25141` to `%141` — and there the raw text
    // is a *different real id* rather than a spelling of this one: `%252` is how `%2` is written
    // now, and a machine that has reached pane `%252` would open that stranger's session the day
    // `%2` closes. Nothing on screen would say so. So the raw text stays a candidate only where
    // the decoded one is impossible, and a control character is what impossible looks like.
    //
    // The cost is one narrow range: an old link naming `%20`–`%39` decodes to a printable
    // character — `%25` to `%`, `%20` to a space — so it is not rescued. That is where this was
    // before today, which is "does not route", and never "routes somewhere else".
    return /[\u0000-\u001f\u007f-\u009f]/.test(decoded) ? [decoded, raw] : [decoded];
}

/**
 * A fragment can name a page as well as a session, and both are read here.
 *
 * `#page=usage` is what the menu writes when you press a row, so it is also what a bookmark, a
 * reload and the browser's own Back are answering — one address for one screen. A name this build
 * has no page for is ignored rather than obeyed: an old link, or a hand-typed one, leaves you
 * where you were instead of in front of a blank rectangle.
 *
 * The page is applied before the session, because `#session=…` means the session list with that
 * session open on it, and `openSession` says so itself.
 *
 * **And a fragment that names no page means the home page**, which is the half this had no `else`
 * for. Without it the address could stop saying where you are while the screen stayed put:
 * measured in a browser as `page=usage`, `hash=""`, Usage still drawn and `#app` still hidden —
 * one screen with no address, and a reload from there landing on the session list instead. On a
 * phone that is the back gesture: `session/open.js` pushes one entry, the pop closes the session
 * underneath, and the hashchange that followed used to change nothing at all.
 */
export function routeTo(hash) {
    var candidates = sessionCandidates(hash);
    // The fourth of the five places a tapped notification can stop. What is recorded is the
    // *shape* of the request and never the id: `layout-diagnostics.js` keeps layout and
    // state-machine facts and no names, and a session id is a name.
    Diagnostics.note("route.to", {
        fragment: !!hash, page: pageInHash(hash) || "",
        names: candidates ? candidates.length : 0,
        rescued: !!(candidates && candidates.length > 1)
    });
    var page = pageInHash(hash);
    // A name this build has no page for is still ignored rather than obeyed — an old link leaves
    // you where you were — so the two cases are "named one" and "named none", not "knows it".
    if (page) { if (Pages.knows(page)) Pages.go(page, { hash: false }); }
    else Pages.goHome({ hash: false });
    var ids = candidates;
    if (!ids) return;
    wantedSession = ids[0];
    wantedSessionAsWritten = ids.length > 1 ? ids[1] : null;
    openWanted();
}

/** Open the session the URL asked for, if it is in the list yet. */
export function openWanted() {
    if (!wantedSession) return false;
    var id = byId(wantedSession) ? wantedSession
        : (wantedSessionAsWritten && byId(wantedSessionAsWritten) ? wantedSessionAsWritten : null);
    // The fifth place, and the only one that was ever recorded — as `session.open.begin`, which
    // is written after the decision and therefore says nothing at all about the times the answer
    // was no. A miss is the interesting reading: it is what a cold start looks like before the
    // list arrives, and what a session that has closed looks like for ever.
    Diagnostics.note("route.openWanted", {
        found: !!id, rescued: !!(id && id === wantedSessionAsWritten),
        held: !!wantedSessionAsWritten
    });
    if (!id) return false;
    wantedSession = null;
    wantedSessionAsWritten = null;
    // The tap has landed, so the worker's record of what it was for has been carried out and can
    // go. Written here rather than where the record is read, because the road that reads it can
    // end in a miss — a cold start whose list has not arrived — and `list.js` calls this again
    // with every list until it does not.
    //
    // **The one that is spent is the one that asked for this session**, which is not the same as
    // "the next opening that happens to succeed". Two notifications tapped before the list arrives
    // are one record — the newest replaces the older, deliberately — while the page is still
    // holding the *first* tap's id, so the opening that follows is the first tap's and the record
    // it would have thrown away is the second tap's, unread. `forgetWant` is given the id it means
    // to collect for the same reason: the record in the store need not still be the one this call
    // is spending.
    if (pendingWant && pendingWantNames(id)) {
        var spent = pendingWant;
        pendingWant = "";
        pendingWantFor = null;
        forgetWant(spent);
    }
    // A notification means there is something new to read. Force a transcript read even when
    // this session was already open and its fragment therefore routes to the same screen.
    openSession(id, false, true);
    return true;
}

window.addEventListener("hashchange", function () { routeTo(location.hash); });

// The service worker's `{type: "navigate", url}`. It is sent to a focused client instead of
// navigating it, because a client the worker does not control cannot be navigated at all and a
// URL that differs only in its fragment would not reload one that it could.
if ("serviceWorker" in navigator) {
    navigator.serviceWorker.addEventListener("message", function (ev) {
        var data = ev && ev.data;
        // The third place, and the first one on this side of the gap. Noted before the message is
        // judged, because "a message arrived that this handler declined" and "no message arrived"
        // are the two readings the trace has to be able to tell apart.
        Diagnostics.note("page.sw.message", {
            type: (data && data.type) || "", url: typeof (data && data.url) === "string",
            want: !!(data && data.want), hidden: document.hidden,
            // Which of the two ways this handler can decline a message it did receive: the tap it
            // announces has already been carried out by the road below. Without this the trace
            // shows a message arriving and no routing behind it, which is the same picture a
            // message naming no session leaves.
            answered: !!(data && data.want && data.want === settledWant)
        });
        if (!data || data.type !== "navigate" || typeof data.url !== "string") return;
        // **The same guard, in the other direction, and it was missing.** `readWorkerWant` refuses
        // a record this page has already answered; nothing refused a *message* announcing a tap the
        // record road had already acted on. That order is not hypothetical — a page resumed from
        // the background starts the read at `visibilitychange`, and the client message queued while
        // it was suspended is dispatched during the three asynchronous hops that read takes. Both
        // roads then acted on one tap: the transcript was fetched twice, and on a phone
        // `session/open.js` pushed a second history entry, which is one back gesture that does
        // nothing — in the very flow this exists to repair.
        //
        // Compared before it is assigned, because `settledWant` is set by whichever road acts
        // first: comparing after the assignment below would refuse the message road every time,
        // including the ordinary order where the message is what does the work.
        if (data.want && data.want === settledWant) return;
        var cut = data.url.indexOf("#");
        var hash = cut < 0 ? "" : data.url.slice(cut);
        var wanted = sessionCandidates(hash);
        if (!wanted) return;  // `/` — the test push, and nothing to route to
        // This message and the worker's record are two announcements of one tap, and the id is
        // what joins them. A message that arrives is the road working, so the record must not fire
        // again behind it — that is what would send somebody who has since moved on back to where
        // the notification pointed. `pendingWant` as well, so the record is spent by whichever
        // call opens the session, which on a cold start is not this one.
        //
        // **Below the gate, not above it.** A message this page routes nothing for has answered
        // nothing, and marking the tap answered there left the record `settled` on a page that had
        // done nothing about it — and pending, so it could not be collected either. The tap was
        // then lost on both roads until a reload. The worker is not supposed to send one of those
        // at all; that it could is exactly the drift the two gates are driven against each other
        // for, and this is what keeps a drifted gate from costing the second road as well.
        if (typeof data.want === "string" && data.want) {
            settledWant = data.want;
            pendingWant = data.want;
            pendingWantFor = wanted;
        }
        // Written into the address as well as acted on, so that a reload from here lands in the
        // same place. Setting it fires `hashchange`, which routes; when it is already what we
        // were sent, nothing fires and this does the routing itself.
        if (hash === location.hash) routeTo(hash);
        else location.hash = hash;
    });
}

/* ---- reading the worker's half of the road -------------------------------
 *
 * Five things have to happen between a thumb on a lock screen and a transcript on the screen, and
 * until now the page could only ever report the last two of them. The worker is shut down between
 * events; a message it sent to a page that was not listening leaves nothing behind at either end,
 * and that silence is indistinguishable from a worker that never woke up.
 *
 * So the worker writes `sw.notificationclick` and then `sw.postMessage` or `sw.openWindow` into
 * Cache Storage — the one store both a worker and a page can open — and this reads them back into
 * the same trace the page's own notes go into. **The reading is the whole point**: a trace that
 * ends at `sw.postMessage` says the message was sent and never arrived, and one that has no
 * `sw.` entry at all says the worker never ran. Those are different faults with different fixes,
 * and before this there was no way to be looking at either of them.
 *
 * Nothing here acts on what it reads. Deciding what to *do* about a dropped message is a design
 * question with more than one answer, and this exists so that the question can be asked of
 * evidence rather than of a guess.
 *
 * The names are the worker's, and the worker's copy of them lives in `RemotePage.serviceWorker()`
 * because that script is a response body rather than a module. `Tests/web-notification-route.mjs`
 * holds the two copies against each other.
 */
export var WORKER_TRACE_CACHE = "clawdline-notification-trace";
export var WORKER_TRACE_URL = "/__clawdline/notification-trace";

/** The name this recorder answers to in a report's completeness block. */
export var WORKER_TRACE_SOURCE = "serviceWorker";

// Declared at load, before anything has been read. That is the whole of what makes a silent
// worker legible: a report taken before `readWorkerTrace` has run says `unread`, one taken after
// a run that found nothing says `merged` with no entries, and a browser with no Cache Storage
// says `unavailable`. Those were one empty trace until this line existed.
Diagnostics.source(WORKER_TRACE_SOURCE);

/** The newest entry already read into the trace, so that waking up twice does not report twice.
 *
 *  The worker's own counter rather than its clock: a click and the message it sends are written
 *  in the same millisecond, and a timestamp would have read one of the two and dropped the other
 *  without saying so. */
var readThrough = 0;

/**
 * Fold whatever the worker has written since the last call into the page's own diagnostics.
 * Answers the number of entries that were new, and never rejects: a browser with no Cache
 * Storage, or one that refuses it, is a page with no worker half to read and not a page with a
 * problem.
 */
export function readWorkerTrace() {
    try {
        if (typeof caches === "undefined" || !caches || !caches.open) {
            Diagnostics.sourceRead(WORKER_TRACE_SOURCE, "unavailable", 0);
            return Promise.resolve(0);
        }
    } catch (e) {
        Diagnostics.sourceRead(WORKER_TRACE_SOURCE, "unavailable", 0);
        return Promise.resolve(0);
    }
    return caches.open(WORKER_TRACE_CACHE)
        .then(function (cache) { return cache.match(WORKER_TRACE_URL); })
        .then(function (found) { return found ? found.json() : []; })
        .then(function (list) {
            if (!Array.isArray(list)) {
                Diagnostics.sourceRead(WORKER_TRACE_SOURCE, "failed", 0);
                return 0;
            }
            // `activate` empties Cache Storage, so a worker update starts the numbering again. A
            // reader holding a higher number would then skip every entry for ever and report a
            // silent road as an empty one — the exact failure this file exists to make visible.
            var newest = list.length ? (list[list.length - 1].seq || 0) : 0;
            if (newest < readThrough) readThrough = 0;
            var fresh = 0;
            for (var i = 0; i < list.length; i++) {
                var entry = list[i];
                if (!entry || typeof entry.seq !== "number" || entry.seq <= readThrough) continue;
                readThrough = entry.seq;
                fresh += 1;
                Diagnostics.note(entry.event, entry.data);
            }
            Diagnostics.sourceRead(WORKER_TRACE_SOURCE, "merged", fresh);
            return fresh;
        })
        .catch(function () {
            Diagnostics.sourceRead(WORKER_TRACE_SOURCE, "failed", 0);
            return 0;
        });
}

/* ---- the second road: doing something about a message that never arrived ---
 *
 * Everything above this line reads the worker's half and acts on none of it. That was the right
 * shape while the question was still *where does a tap stop*; it is the wrong one once the answer
 * is known, and the answer is that an already-open window has exactly one road and gets one
 * attempt at it. `postMessage` is defined on `Client`, so every window `clients.matchAll` returns
 * has one and the `client.navigate` fallback beside it cannot run; `clients.openWindow` is reached
 * only when there is no window at all, and on iOS the system opens the web app itself — at the
 * manifest's `start_url`, which is `/` — before `notificationclick` runs. So by the time the
 * handler looks, there is a window, it is showing the session list, and the single message aimed
 * at it either lands or the tap is over.
 *
 * So the worker writes down *what the tap was for* as well as sending the message, and this reads
 * it back at the two moments the page wakes up. **It is not a retry of the message**: it is the
 * same request arriving by a road that does not depend on delivery, and it works precisely when
 * the interesting failure has already happened.
 *
 * Four things keep a record from being worse than the fault it fixes:
 *
 * - **It goes stale.** A notification tapped on Tuesday must not move anybody on Thursday, and the
 *   window has to be wide enough for an iOS cold start — see `WORKER_WANT_MAX_AGE_MS`.
 * - **It is answered once.** `settledWant` holds the id this page instance has already responded
 *   to, by either road, so waking twice does not route twice.
 * - **Whichever road acts first marks the tap answered**, and the other declines it: a message that
 *   arrives marks the record settled on the way past, and a message that arrives behind a record
 *   already carried out is refused by the same comparison. So the two roads cannot both act on one
 *   tap, in either order.
 * - **`/` is not a request.** The test push and a fan-out notification both carry it, and the
 *   worker writes no record for a URL naming no session. This declines it again anyway: the
 *   worker's copy of that judgement is a second copy, and second copies drift.
 *
 * **And a worker update loses the record**, because `activate` empties every cache — the same
 * cost the trace pays, for the same reason and with a different consequence: an update lands on
 * one tap, and that tap keeps only the message road it had before this existed.
 */
export var WORKER_WANT_CACHE = "clawdline-notification-wanted";
export var WORKER_WANT_URL = "/__clawdline/notification-wanted";

/**
 * How old a record may be and still be obeyed.
 *
 * Two minutes, which is not a guess about attention but about the slowest thing between the tap
 * and this line: iOS launches the web app cold, the document comes down the tunnel, the modules
 * load, and only then does `boot` reach the read. Seconds would drop exactly the taps this exists
 * for. Days would let a notification from last week move somebody who has just opened the app to
 * read something else, which is a worse failure than the one being fixed, because nothing on the
 * screen would say why it happened.
 */
export var WORKER_WANT_MAX_AGE_MS = 120000;

/** The record this page has already answered — by this road or by the message. */
var settledWant = "";

/** The record whose session has not been opened yet, spent by `openWanted` when that session is. */
var pendingWant = "";

/** The sessions that record asked for — `sessionCandidates` of its own URL, both spellings when it
 *  has two. This is what makes the line above say *that* session rather than "the next one". */
var pendingWantFor = null;

/** Is the record still waiting the one that asked for this session? */
function pendingWantNames(id) {
    if (!id || !pendingWantFor) return false;
    for (var i = 0; i < pendingWantFor.length; i++) {
        if (pendingWantFor[i] === id) return true;
    }
    return false;
}

/**
 * Throw the record away — the one named, and not simply whatever is in the store.
 *
 * The store holds one record and the newest tap replaces it whole, so by the time a deletion runs
 * the record there may belong to a *later* tap that nothing has read yet. Deleting that is the
 * second road losing exactly the notification it was built for: tap A before the list arrives, tap
 * B behind it, the list arrives and opens A, and B is thrown away unread with nothing on screen to
 * say a tap was dropped.
 *
 * Never rejects: a store that will not answer is a page with no second road. It also does not
 * delete what it could not read, which is the safer half of that — an un-deleted record is bounded
 * by staleness, by `settledWant`, and by the collection this same branch does on the next wake-up,
 * while a wrongly deleted one is a tap nobody can get back. The read and the delete are two steps
 * and a tap can land between them; the window is one turn of the microtask queue, and losing that
 * race costs the same record this used to lose every time.
 */
function forgetWant(id) {
    try {
        if (typeof caches === "undefined" || !caches || !caches.open || !caches.delete) {
            return Promise.resolve(false);
        }
    } catch (e) { return Promise.resolve(false); }
    return caches.open(WORKER_WANT_CACHE)
        .then(function (cache) { return cache.match(WORKER_WANT_URL); })
        .then(function (found) { return found ? found.json() : null; })
        .then(function (record) {
            if (!record || record.id !== id) return false;
            return caches.delete(WORKER_WANT_CACHE).then(function () { return true; });
        })
        .catch(function () { return false; });
}

/** A read is in flight. Lists arrive faster than Cache Storage answers, and two reads of one
 *  record are two roads again — the second would find it `settled` at best and act on it twice at
 *  worst. */
var wantReading = false;

/**
 * Read what the last tap asked for, and route there if it is still worth doing.
 *
 * Answers what it decided, as a word rather than a count, because every outcome here is a
 * different reading and a number would flatten them: `unavailable` (no Cache Storage at all),
 * `none` (nothing written, or nothing readable), `settled` (this page has answered that tap
 * already), `stale` (older than the window), `declined` (names no session — the test push) or
 * `routed`. Never rejects, for the same reason `readWorkerTrace` does not: this runs inside
 * `boot`.
 */
export function readWorkerWant() {
    wantReading = true;
    return readWantRecord().then(function (answer) {
        wantReading = false;
        return answer;
    });
}

/**
 * The third read point, and the reason it exists: **the write is not awaited and the reads were
 * both at the edges.**
 *
 * The worker starts its cache write and does not hold the tap up for it; the page read at `boot`
 * and at `visibilitychange` and nowhere in between. A cold start is exactly the order that falls
 * between them — `boot` reads before the record has landed, and the page is then in the foreground,
 * so no `visibilitychange` comes. By the time one does, the record is usually older than
 * `WORKER_WANT_MAX_AGE_MS` and is thrown away unread. The second road was silent on precisely the
 * tap it exists for.
 *
 * So `list.js` calls this with every session list, which is where `openWanted` has always retried
 * for the same reason: a cold start does its routing before it knows what sessions exist. Not a
 * retry of the worker's message — a re-read of what it wrote down.
 *
 * **And it keeps looking.** This used to stop as soon as a read came back with anything other than
 * "nothing there yet", on the reasoning that a record arriving later would arrive with a wake-up
 * of its own. That sentence is false, and the way it is false cost the whole road: a page that had
 * been left open for a night of testing reads a record from last night at `boot`, answers `stale`
 * — correctly — and never looks again, so every tap after it lands in a store nobody is reading.
 * A `/` record from a test push closes it the same way, and so does a tap that worked. The flag
 * was about this page's life; what it was trying to say is about one record, and `settledWant`
 * already says that, by id, inside the read.
 *
 * One guard is left, and it is the one that was always right: a read already in flight. Lists
 * arrive faster than Cache Storage answers.
 *
 * What that costs is one store read per list on a page where a notification is never tapped, and
 * there is no cheaper honest version of it — the id that decides whether there is anything to do
 * is *inside* the record, so it cannot be consulted without opening the store. Measured in Chrome
 * on this Mac, 500 reads an arm: a median of 0.2ms against an empty store and 0.5ms against one
 * holding a record, beside a median of 0.2ms for one three-row `innerHTML` and a forced layout on
 * the same page. Nothing waits on it either — it is three promise hops off the render's path. What
 * that measurement cannot say is what iOS Safari charges. See `docs/notifications.md`.
 *
 * Answers `skipped` when it did not look.
 */
export function retryWorkerWant() {
    if (wantReading) return Promise.resolve("skipped");
    return readWorkerWant();
}

/**
 * The fourth read point, and the one a tap made *in front of the app* can actually reach.
 *
 * The three above are all wake-ups, and a foreground tap is not one: nothing loads, so there is no
 * `boot`; nothing was hidden, so no `visibilitychange` comes; and the session list only arrives
 * when something on the Mac changes — the change that *sent* the notification, which pushed a list
 * before the banner was ever tapped. So on the road's second-most likely shape the page had one
 * `postMessage` and nothing behind it, which is what this whole file exists to stop being true.
 *
 * A banner is system UI drawn over the app. It takes the key window while it is up and hands it
 * back when it goes, so the focus returning is a signal that arrives on the tap rather than on the
 * Mac's next state change. It does not replace the list — a page that already has the focus when
 * the record lands gets nothing from this — which is why both are here, and why they share one
 * guard rather than being two roads.
 */
window.addEventListener("focus", function () { retryWorkerWant(); });

function readWantRecord() {
    try {
        if (typeof caches === "undefined" || !caches || !caches.open) {
            return Promise.resolve("unavailable");
        }
    } catch (e) { return Promise.resolve("unavailable"); }
    return caches.open(WORKER_WANT_CACHE)
        .then(function (cache) { return cache.match(WORKER_WANT_URL); })
        .then(function (found) { return found ? found.json() : null; })
        .then(function (record) {
            if (!record || typeof record.id !== "string" || !record.id ||
                typeof record.url !== "string" || typeof record.at !== "number") {
                Diagnostics.note("page.want", { found: false });
                return "none";
            }
            var age = Date.now() - record.at;
            // Answered already — and whether it may go turns on whether the session it names is
            // open yet. Still pending means the message road is holding the request against a
            // list that has not arrived, and a reload before it does would have nothing left to
            // work from, so the record stays. Not pending means the tap has landed and this copy
            // outlived the deletion that was started for it: a write to a store that can refuse
            // one, or one the worker's own write overtook, because `notificationclick` posts the
            // message before its cache write has finished. Left there, a reload inside the window
            // would take somebody back to a session they have already read and moved on from.
            if (record.id === settledWant) {
                Diagnostics.note("page.want", { found: true, settled: true,
                                                pending: pendingWant === record.id,
                                                age: Math.round(age / 1000) });
                if (pendingWant !== record.id) forgetWant(record.id);
                return "settled";
            }
            if (!(age <= WORKER_WANT_MAX_AGE_MS)) {
                Diagnostics.note("page.want", { found: true, stale: true,
                                                age: Math.round(age / 1000) });
                forgetWant(record.id);
                return "stale";
            }
            var cut = record.url.indexOf("#");
            var hash = cut < 0 ? "" : record.url.slice(cut);
            var wanted = sessionCandidates(hash);
            if (!wanted) {
                Diagnostics.note("page.want", { found: true, declined: true });
                forgetWant(record.id);
                return "declined";
            }
            // Marked before anything asynchronous can happen, so that two wake-ups a moment apart
            // cannot both read this record and both act on it. What it asked for is remembered
            // beside it: this record is spent by the opening of *that* session and by no other.
            settledWant = record.id;
            pendingWant = record.id;
            pendingWantFor = wanted;
            Diagnostics.note("page.want", { found: true, routed: true,
                                            age: Math.round(age / 1000) });
            // The same two lines the message road ends in, and for the same reasons: the address
            // has to say where the page is so that a reload lands in the same place, and setting
            // it is what routes — except when it is already what we were sent, which is when
            // nothing fires and this has to do the routing itself.
            if (hash === location.hash) routeTo(hash);
            else location.hash = hash;
            return "routed";
        })
        .catch(function () {
            Diagnostics.note("page.want", { found: false, failed: true });
            return "none";
        });
}
