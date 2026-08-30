/** A preference that belongs to this browser, kept beside the other browser-local settings. */
function storedBool(key, fallback) {
    try {
        var value = localStorage.getItem(key);
        return value === null ? fallback : value === "1";
    } catch (e) { return fallback; }
}

export function storeBool(key, value) {
    try { localStorage.setItem(key, value ? "1" : "0"); } catch (e) { }
}

/* ==========================================================================
   1. State
   One object, and every render reads from it. The stream sends a whole list
   every time, so there is no merge step and therefore no class of bug where
   half an update landed.
   ========================================================================== */

export var S = {
    sessions: [],           // as the server sent them, unmodified
    // The app's own dispatched work: a root session that asked for something, and the child
    // session it was given. Whole list every time, same as the sessions — see `handlers.tasks`.
    // An app too old to have the route leaves this empty, and everything below reads as before.
    tasks: [],
    at: 0,
    // Whether a session list — any list, an empty one included — has ever landed. Two screens
    // are drawn from this and not from `sessions.length`: the list's "there are none" and the
    // transcript's "none is open". Both are answers, and before the first frame the page has
    // not been given one; `sessions` starting empty is how it is declared, not what the Mac
    // said. See `listUnknown`.
    arrived: false,
    write: false,           // /v1/health and the hello event both carry it
    version: "",
    conn: "connecting",     // connecting | live | retrying | offline
    retryIn: 0,             // seconds, while conn is "retrying"
    locked: false,          // the server answered 401: this browser is not a paired device

    selectedId: null,       // the highlight in the list
    openId: null,           // whose transcript is on screen
    filter: "",
    paneOpen: true,         // ⌘J, desktop only
    newestFirst: false,
    assistantIcons: storedBool("clawdline.assistant-icons", true),

    tx: { id: null, entries: [], signature: null, loading: false, error: null },
    // A deliberately separate, lossy view of the terminal while the durable transcript catches
    // up. It is local-only and never merged into `tx`, so screen chrome cannot become history.
    live: { id: null, text: "", signature: null },
    // Which of the open session's agents is being read, if any. **The session stays open
    // underneath**: `openId` is untouched while this is set, so the stream keeps updating the
    // row, the list keeps its place, and leaving is putting this back to null rather than
    // re-opening anything. `sid` is remembered with it because an agent belongs to a session,
    // and a reader who followed a link and then switched sessions must not be left reading
    // somebody else's background work under the new session's name.
    agent: null,            // { sid, id, what, entries, signature, loading, error, meta }
    // Which folded runs of tool calls the reader has opened, by fold key. Kept beside the
    // transcript rather than in it: this pane redraws whenever the session moves, and a run
    // that closed itself under somebody who was reading it would be worse than never folding.
    expanded: {},
    // What each session looked like last render, so a change can be noticed: the transcript
    // is refetched when its session moves, and a row that has just stopped gets its pulse.
    seen: {}
};

// A successful send belongs to the browser until the Mac writes the same turn into its own
// transcript. It cannot live in `S.tx`: every fetch replaces that object, including the quiet
// fetch made immediately after a send, which is exactly when this entry most needs to survive.
export var optimisticBySession = {};
