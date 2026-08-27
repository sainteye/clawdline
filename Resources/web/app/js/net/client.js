/* --------------------------------------------------------------------------
   The transport seam

   JavaScript has no runtime interface declarations, so ClawdlineClient is a
   deliberately small structural contract.  Both transports may expose more
   compatibility methods, but the view's portable core depends only on these.
   -------------------------------------------------------------------------- */

export const LOCAL_MACHINE = "this-mac";

export const ClawdlineClient = Object.freeze({
    methods: Object.freeze([
        "events", "sessions", "transcript", "send", "answer", "dispatch", "schedules"
    ])
});

export function assertClawdlineClient(candidate) {
    if (!candidate || (typeof candidate !== "object" && typeof candidate !== "function")) {
        throw new TypeError("ClawdlineClient must be an object");
    }
    ClawdlineClient.methods.forEach(function (method) {
        if (typeof candidate[method] !== "function") {
            throw new TypeError("ClawdlineClient is missing " + method + "()");
        }
    });
    return candidate;
}

/** Turn the old local id, or the cloud pair, into the one identity shape. */
export function sessionIdentity(value, fallbackMachine) {
    var machine = fallbackMachine || LOCAL_MACHINE;
    var session = value;
    if (value && typeof value === "object") {
        machine = value.machine || machine;
        session = value.session !== undefined ? value.session : value.id;
    }
    if (typeof machine !== "string" || !machine || typeof session !== "string" || !session) {
        throw new TypeError("a session identity needs non-empty machine and session strings");
    }
    return { machine: machine, session: session };
}

/** Stable only inside the client; unlike a slash join it cannot collide. */
export function sessionIdentityKey(value, fallbackMachine) {
    var identity = sessionIdentity(value, fallbackMachine);
    return identity.machine + "\u0000" + identity.session;
}

export function sessionPath(value) {
    var identity = sessionIdentity(value);
    return "/m/" + encodeURIComponent(identity.machine) + "/s/" +
        encodeURIComponent(identity.session);
}

/** New fleet links, plus the old local-only /s/<session> spelling. */
export function parseSessionPath(pathname) {
    var path = String(pathname || "");
    var fleet = /^\/m\/([^/]+)\/s\/([^/]+)\/?$/.exec(path);
    if (fleet) return decodedIdentity(fleet[1], fleet[2]);
    var local = /^\/s\/([^/]+)\/?$/.exec(path);
    return local ? decodedIdentity(encodeURIComponent(LOCAL_MACHINE), local[1]) : null;
}

function decodedIdentity(machine, session) {
    try {
        return sessionIdentity({
            machine: decodeURIComponent(machine),
            session: decodeURIComponent(session)
        });
    } catch (e) {
        return null;
    }
}

/** Protocol channel segments are printable ASCII; percent encoding preserves opaque ids. */
export function channelSegment(value) {
    if (typeof value !== "string" || !value) throw new TypeError("empty channel segment");
    return encodeURIComponent(value).replace(/\|/g, "%7C");
}

export function decodedChannelSegment(value) {
    try { return decodeURIComponent(value); } catch (e) { return value; }
}
