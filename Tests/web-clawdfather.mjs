import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";

import { T, fill } from "../Resources/web/app/js/core/i18n.js";
import {
    attemptClawdfatherAssignment,
    clawdfatherCreationChoice,
    clawdfatherCreationLabel,
    clawdfatherInstruction,
    createClawdfatherAssignmentState,
    createClawdfatherCoordinatorLoader
} from "../Resources/web/app/js/input/clawdfather.js";

/* --------------------------------------------------------------------------
   Choosing Clawdfather while starting a session

   The browser holds a device token and never the orchestrator token, so it cannot register a
   coordinator and must not look as though it could. What it can do is type: the same
   `POST /v1/sessions/:id/send` every other composed message already uses. The session on the
   other end is a local process that can read `~/.config/clawdline/orchestrator-token`, so the
   registration is carried out there, by the one caller the broker already trusts with it.

   These are the facts that decision rests on: the choice is available only for a genuinely new
   session, every configured coordinator (online or offline) closes it, and nothing in the path
   names a privileged orchestrator write route.

   What the switch now reads is one closed word from the Mac. `registration.state` is derived
   from the authoritative durable store status and says `available`, `configured` or `blocked`
   — and nothing else. The old `coordinator` tuple could not answer this: absent, corrupt and
   unsupported all spelled `configured:false, status:"unregistered"`, so a browser looking at a
   coordinator record it must never overwrite saw an invitation to create one, and only found
   out through a `409` after the instruction had already been typed into a session.
   -------------------------------------------------------------------------- */

const ordinary = { id: "35D87610-E7F4-4A9A-95A0-11947CF5115C", assistant: "claude",
                   label: "ordinary session" };

/* The five answers the Mac can actually produce, written the way `deviceBearings` writes them.
   `Tests/main.swift` builds the same five from a real coordinator store and asserts the same
   `registration.state` values, so these are the server's shapes rather than shapes convenient
   for this file. The compatibility `coordinator` tuple is carried along exactly as the server
   still sends it — including the fact that three different stores spell it identically. */
const SERVED = {
    absent: { registration: { state: "available" },
              coordinator: { configured: false, status: "unregistered",
                             lifecycle: "unregistered", scope: "machine",
                             label: "Clawdfather" } },
    online: { registration: { state: "configured" },
              coordinator: { configured: true, status: "online", lifecycle: "standby",
                             scope: "machine", label: "Clawdfather" } },
    offline: { registration: { state: "configured" },
               coordinator: { configured: true, status: "offline", lifecycle: "offline",
                              scope: "machine", label: "Clawdfather" } },
    corrupt: { registration: { state: "blocked" },
               coordinator: { configured: false, status: "unregistered",
                              lifecycle: "unregistered", scope: "machine",
                              label: "Clawdfather" } },
    unsupported: { registration: { state: "blocked" },
                   coordinator: { configured: false, status: "unregistered",
                                  lifecycle: "unregistered", scope: "machine",
                                  label: "Clawdfather" } }
};

const reading = clawdfatherCreationChoice(null, true, true);
assert.equal(reading.shown, true);
assert.equal(reading.enabled, false,
    "the choice fails closed until the machine coordinator record has been read");
assert.equal(reading.checked, false,
    "a stale tick is never shown while coordinator ownership is unknown");

const available = clawdfatherCreationChoice(SERVED.absent, true, true);
assert.equal(available.shown, true);
assert.equal(available.enabled, true);
assert.equal(available.checked, true);
assert.equal(available.state, "selected");
assert.equal(clawdfatherCreationChoice(SERVED.absent, false, true).state, "available");

for (const status of ["online", "offline"]) {
    const assigned = clawdfatherCreationChoice(SERVED[status], true, true);
    assert.equal(assigned.shown, true);
    assert.equal(assigned.enabled, false,
        `an ${status} configured coordinator prevents assigning another one`);
    assert.equal(assigned.checked, false);
    assert.equal(assigned.state, "assigned");
    assert.equal(assigned.coordinator.status, status,
        "the assigned row still carries the words the sheet says out loud");
}

/* The whole point of the new field. Both of these carry the *canonical unregistered tuple* —
   byte for byte what an absent record produces — because that is what the server really sends
   for a store it refuses to overwrite. Reading `coordinator` alone cannot tell them apart; the
   only thing that can is `registration.state`. */
for (const kind of ["corrupt", "unsupported"]) {
    const refused = clawdfatherCreationChoice(SERVED[kind], true, true);
    assert.equal(refused.shown, true);
    assert.equal(refused.enabled, false,
        `a ${kind} coordinator store is never an invitation to write over it`);
    assert.equal(refused.checked, false);
    assert.equal(refused.state, "blocked",
        `a ${kind} store is said out loud as blocked, not as a failed read`);
    assert.deepEqual(refused.coordinator, SERVED.absent.coordinator,
        "and the compatibility tuple is identical to absence, which is why it cannot be the gate");
}

// Missing, unknown and malformed all fail closed — including a payload from a Mac that predates
// the field but still spells the canonical unregistered tuple the old gate accepted.
for (const [name, payload] of [
    ["a Mac that predates the field but spells the old accepted tuple",
        { coordinator: SERVED.absent.coordinator }],
    ["an empty answer", {}],
    ["a null projection", { registration: null }],
    ["a projection with no state", { registration: {} }],
    ["a blank state", { registration: { state: "" } }],
    ["a state that is not a string", { registration: { state: true } }],
    ["a projection that is not an object", { registration: "available" }],
    ["the old coordinator word in the new field", { registration: { state: "unregistered" } }],
    ["a state in the wrong case", { registration: { state: "Available" } }],
    ["a state this page has never heard of", { registration: { state: "pending_repair" } }]
]) {
    const refused = clawdfatherCreationChoice(payload, true, true);
    assert.equal(refused.enabled, false, `${name} does not open creation`);
    assert.equal(refused.checked, false, `${name} shows no tick`);
    assert.equal(refused.state, "unavailable", `${name} reads as unavailable`);
}

assert.equal(clawdfatherCreationChoice(SERVED.absent, true, false).shown, false,
"resuming an existing conversation is not creation and never offers the choice");

/* One closed vocabulary, declared once on the Mac. A fifteenth state added to the server has to
   fail here — where somebody must decide what it means — rather than arriving at a browser that
   silently treats it as one of the three it knows. */
const coordinatorSwift = await readFile(
    new URL("../Sources/Coordinator.swift", import.meta.url), "utf8"
);
const declaredStates = coordinatorSwift.match(
    /static let registrationStates:\s*Set<String>\s*=\s*\[([^\]]*)\]/
);
assert.ok(declaredStates,
    "the Mac declares one closed registration vocabulary the page can be checked against");
assert.deepEqual(
    [...declaredStates[1].matchAll(/"([a-z_]+)"/g)].map((m) => m[1]).sort(),
    ["available", "blocked", "configured"],
    "the page knows every registration state the Mac can send");

assert.equal(clawdfatherCreationLabel(), "Name the new session Clawdfather");
assert.doesNotMatch(clawdfatherCreationLabel(), /Make this session/i,
    "creation copy names the new Session instead of sounding like an action on this one");

/* ---- the line that is actually typed -------------------------------------- */

const line = clawdfatherInstruction(ordinary);
assert.ok(line.length > 0);
assert.match(line, /35D87610-E7F4-4A9A-95A0-11947CF5115C/,
    "the browser already knows the terminal-neutral id and hands it over rather than "
    + "leaving the session to work out who it is");
assert.doesNotMatch(line, /\{id\}/, "the hole is filled, not shipped");
assert.match(line, /orchestrator-token/,
    "the session is told which credential the recipe needs, since only it can read one");
assert.doesNotMatch(line, /rebind/i,
    "the creation-only instruction never promises to reconnect an offline coordinator");
assert.equal(clawdfatherInstruction({ assistant: "claude" }), "",
    "no id means no instruction rather than one addressed to nobody");
assert.equal(clawdfatherInstruction(null), "");

/* ---- the last-moment read and strict send gate ---------------------------- */

let blockedReads = 0;
let blockedSends = 0;
const changingOwnerClient = {
    coordinatorBearings: function () {
        blockedReads += 1;
        return Promise.resolve(blockedReads === 1 ? SERVED.absent : SERVED.online);
    },
    send: function () { blockedSends += 1; return Promise.resolve({ ok: true }); }
};
await createClawdfatherCoordinatorLoader(changingOwnerClient).load();
assert.equal(blockedReads, 1, "opening the sheet performs the first Bearings read");
const blockedAssignment = await attemptClawdfatherAssignment(ordinary, changingOwnerClient);
assert.equal(blockedReads, 2,
    "attemptAssignment performs a distinct second Bearings read immediately before sending");
assert.equal(blockedSends, 0,
    "a coordinator registered while the tab starts wins the race and receives no send");
assert.equal(blockedAssignment.state, "blocked");

// A store that turned unreadable between the sheet opening and the tab becoming addressable is
// the same race with a worse ending: the old gate would have typed the instruction and learned
// about the store from a 409 the browser never sees.
let storeBrokeReads = 0;
let storeBrokeSends = 0;
const storeBrokeAssignment = await attemptClawdfatherAssignment(ordinary, {
    coordinatorBearings: function () {
        storeBrokeReads += 1;
        return Promise.resolve(SERVED.corrupt);
    },
    send: function () { storeBrokeSends += 1; return Promise.resolve({ ok: true }); }
});
assert.equal(storeBrokeReads, 1);
assert.equal(storeBrokeSends, 0,
    "a coordinator record that must never be overwritten receives no registration instruction");
assert.equal(storeBrokeAssignment.state, "blocked");
assert.equal(storeBrokeAssignment.choice.state, "blocked",
    "and the reason the send did not happen survives to whoever reports it");

let allowedReads = 0;
let allowedSends = 0;
const sentAssignment = await attemptClawdfatherAssignment(ordinary, {
    coordinatorBearings: function () {
        allowedReads += 1;
        return Promise.resolve(SERVED.absent);
    },
    send: function (id, text, images) {
        allowedSends += 1;
        assert.equal(id, ordinary.id);
        assert.match(text, /Clawdfather/);
        assert.deepEqual(images, []);
        return Promise.resolve({ ok: true });
    }
});
assert.equal(allowedReads, 1);
assert.equal(allowedSends, 1);
assert.equal(sentAssignment.state, "sent");

let malformedGateSends = 0;
const malformedGate = await attemptClawdfatherAssignment(ordinary, {
    coordinatorBearings: function () { return Promise.resolve({}); },
    send: function () { malformedGateSends += 1; return Promise.resolve({ ok: true }); }
}, {
    choice: function () { return {}; }
});
assert.equal(malformedGateSends, 0,
    "only choice.enabled === true may send; missing is not permission");
assert.equal(malformedGate.state, "blocked");

function outcomeWithin(work, timeoutMs) {
    return Promise.race([
        Promise.resolve(work).then(
            function (value) { return { kind: "resolved", value }; },
            function (error) { return { kind: "rejected", error }; }
        ),
        new Promise(function (resolve) {
            setTimeout(function () { resolve({ kind: "fallback" }); }, timeoutMs);
        })
    ]);
}

const coordinatorTimeout = await outcomeWithin(
    attemptClawdfatherAssignment(ordinary, {
        coordinatorBearings: function () { return new Promise(function () { }); },
        send: function () { throw new Error("send must not run"); }
    }, { timeoutMs: 5 }), 50);
assert.equal(coordinatorTimeout.kind, "rejected",
    "the coordinator timeout settles before the test fallback");
assert.equal(coordinatorTimeout.error && coordinatorTimeout.error.code, "coordinator_timeout");

const assignmentTimeout = await outcomeWithin(
    attemptClawdfatherAssignment(ordinary, {
        coordinatorBearings: function () { return Promise.resolve(SERVED.absent); },
        send: function () { return new Promise(function () { }); }
    }, { timeoutMs: 5 }), 50);
assert.equal(assignmentTimeout.kind, "rejected",
    "the assignment timeout settles before the test fallback");
assert.equal(assignmentTimeout.error && assignmentTimeout.error.code, "assignment_timeout");

const defaultTimeoutTimers = [];
const defaultTimeoutWork = attemptClawdfatherAssignment(ordinary, {
    coordinatorBearings: function () { return new Promise(function () { }); },
    send: function () { throw new Error("send must not run"); }
}, {
    schedule: function (fn, delay) {
        const timer = { fn, delay };
        defaultTimeoutTimers.push(timer);
        return timer;
    },
    cancel: function () {}
});
assert.equal(defaultTimeoutTimers.length, 1,
    "the default coordinator wait installs one bounded timer");
assert.equal(defaultTimeoutTimers[0].delay, 8000,
    "the assignment network boundary stays bounded at eight seconds");
defaultTimeoutTimers[0].fn();
const defaultTimeout = await outcomeWithin(defaultTimeoutWork, 50);
assert.equal(defaultTimeout.kind, "rejected",
    "the captured default timeout rejects before the test fallback");
assert.equal(defaultTimeout.error && defaultTimeout.error.code, "coordinator_timeout");

/* ---- latest read wins, and the assignment lifecycle is finite ------------ */

const coordinatorResolvers = [];
const coordinatorStates = [];
const coordinatorLoader = createClawdfatherCoordinatorLoader({
    coordinatorBearings: function () {
        return new Promise(function (resolve, reject) {
            coordinatorResolvers.push({ resolve, reject });
        });
    }
}, function (state) { coordinatorStates.push(state); });
const firstCoordinatorRead = coordinatorLoader.load();
const secondCoordinatorRead = coordinatorLoader.load();
await Promise.resolve();
coordinatorResolvers[1].resolve(SERVED.absent);
await secondCoordinatorRead;
coordinatorResolvers[0].resolve(SERVED.online);
const staleCoordinatorRead = await firstCoordinatorRead;
assert.equal(staleCoordinatorRead.stale, true);
assert.equal(coordinatorStates.at(-1).payload.registration.state, "available",
    "an older response cannot overwrite the newest sheet opening");
assert.equal(coordinatorStates.filter(function (state) { return state.ready; }).length, 1,
    "only the current generation publishes a completed read");

const assignmentTimers = [];
const assignmentEvents = [];
const assignmentState = createClawdfatherAssignmentState({
    timeoutMs: 25,
    schedule: function (fn, delay) {
        const timer = { fn, delay, cancelled: false };
        assignmentTimers.push(timer);
        return timer;
    },
    cancel: function (timer) { timer.cancelled = true; },
    onTimeout: function (id) { assignmentEvents.push(["timeout", id]); }
});
const freshAssignmentTimers = [];
const freshAssignmentState = createClawdfatherAssignmentState({
    schedule: function (fn, delay) {
        const timer = { fn, delay };
        freshAssignmentTimers.push(timer);
        return timer;
    },
    cancel: function () {}
});
assert.equal(freshAssignmentState.pendingID(), null,
    "a fresh assignment state has no request left over from an earlier module lifetime");
assert.equal(freshAssignmentState.selected(), false,
    "a fresh assignment state starts fail-closed and unselected");
freshAssignmentState.begin("default-wait", true);
assert.equal(freshAssignmentTimers.length, 1,
    "the default assistant-ready wait installs one bounded timer");
assert.equal(freshAssignmentTimers[0].delay, 15000,
    "the assistant-ready wait stays bounded at fifteen seconds");
freshAssignmentState.clear();
assignmentState.choose(true);
assignmentState.open();
assert.equal(assignmentState.selected(), false,
    "every sheet opening clears the old creation choice");
assignmentState.choose(true);
assignmentState.begin("new-one", true);
assert.equal(assignmentState.selected(), false,
    "capturing the request clears the switch before a sheet can be reopened");
assert.equal(assignmentState.pendingID(), "new-one");
assert.equal(assignmentTimers.length, 1,
    "begin installs exactly one bounded assistant-ready timer");
assert.equal(assignmentTimers[0].delay, 25);
assignmentTimers[0].fn();
assert.equal(assignmentState.pendingID(), null,
    "waiting for the assistant has a bounded end");
assert.deepEqual(assignmentEvents, [["timeout", "new-one"]],
    "the bounded end is visible to its owner rather than silently disappearing");

assignmentState.choose(true);
assignmentState.begin("new-two", true);
const completed = await assignmentState.attempt(
    { id: "new-two", assistant: "codex" }, {
        coordinatorBearings: function () { return Promise.resolve(SERVED.absent); },
        send: function () { return Promise.resolve({ ok: true }); }
    });
assert.equal(completed.state, "sent");
assert.equal(assignmentState.pendingID(), null,
    "successful assignment clears the pending request");

assignmentState.choose(true);
assignmentState.begin("new-three", true);
const failedAssignment = await assignmentState.attempt(
    { id: "new-three", assistant: "claude" }, {
        coordinatorBearings: function () { return Promise.reject(new Error("offline")); },
        send: function () { throw new Error("send must not run"); }
    });
assert.equal(failedAssignment.state, "failed");
assert.equal(assignmentState.pendingID(), null,
    "failed assignment clears the pending request too");

/* ---- the browser never reaches for a privileged route --------------------- */

const source = await readFile(
    new URL("../Resources/web/app/js/input/clawdfather.js", import.meta.url), "utf8"
);
// Prose may name the routes and credentials it is explaining; code may not reach for one. So the
// comments come off first, and what is left has no transport in it at all — this module composes
// a sentence and returns it.
const code = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
assert.doesNotMatch(code, /\bfetch\s*\(|\bapi\./,
    "the composer never speaks to the network itself");
assert.doesNotMatch(code, /X-Clawdline-Orchestrator|v1\/orchestrator/,
    "the orchestrator credential and its routes are not something a browser can hold");
assert.match(code, /export function clawdfatherCreationChoice/);

const start = await readFile(
    new URL("../Resources/web/app/js/input/start.js", import.meta.url), "utf8"
);
assert.match(source, /typeof[^\n]*assistant[^\n]*===\s*["']string["']/,
    "the instruction waits for the assistant rather than being typed into the newborn shell");
assert.doesNotMatch(start, /v1\/orchestrator/);

const menu = await readFile(
    new URL("../Resources/web/app/js/input/action-confirm.js", import.meta.url), "utf8"
);
assert.doesNotMatch(menu, /#session-clawdfather/,
    "the Session menu no longer offers direct Clawdfather assignment");

const index = await readFile(
    new URL("../Resources/web/index.html", import.meta.url), "utf8"
);
assert.doesNotMatch(index, /id="session-clawdfather"/,
    "there is no stale hidden menu affordance");
assert.match(index, /id="start-clawdfather"[^>]*type="button"/,
    "the creation sheet owns the real, keyboard-addressable choice");
assert.match(index, /id="start-clawdfather-state"[^>]*role="status"[^>]*aria-live="polite"/,
    "coordinator availability changes are announced");
assert.match(index, /id="detail-clawdfather-crown"[^>]*role="img"[^>]*aria-label="Clawdfather"/,
    "the SessionChat crown has an accessible name rather than being hidden from speech");

const dom = await readFile(
    new URL("../Resources/web/app/js/core/dom.js", import.meta.url), "utf8"
);
assert.doesNotMatch(dom, /"session-clawdfather"/);
assert.match(dom, /"start-clawdfather"/);
assert.match(dom, /"detail-clawdfather-crown"/);

const transcript = await readFile(
    new URL("../Resources/web/app/js/view/transcript.js", import.meta.url), "utf8"
);
assert.match(transcript, /detail-clawdfather-crown[^\n]*hidden\s*=\s*!/,
    "the crown follows the authenticated coordinator projection on the open Session row");

/* ---- the recipe the far end is asked to follow ---------------------------- */

// The instruction names a procedure by name. A session that arrives at it and finds nothing there
// is back to reading Swift source, which is the thing this item exists to avoid — so the written
// recipe is part of the feature and is checked with it.
const recipe = await readFile(
    new URL("../docs/orchestrator.md", import.meta.url), "utf8"
);
assert.doesNotMatch(recipe, /web app's \*Make this session Clawdfather\* item/,
    "the protocol no longer documents the removed existing-Session menu item");
assert.match(recipe, /new-Session \*Name the new session Clawdfather\* choice/,
    "the protocol names the creation-time owner of the instruction");
assert.doesNotMatch(recipe,
    /coordinator\.configured\s*:\s*false[^.\n]{0,160}(?:enable|permission|register)/i,
    "the creation recipe never treats configured:false as permission to register");
assert.match(recipe, /registration-only/i,
    "the web creation flow is separated from the manual offline rebind repair");
assert.match(recipe, /creation sheet never asks a new Session to rebind or replace an offline owner/,
    "the creation flow does not inherit the manual repair branch below it");
assert.match(recipe, /### Becoming Clawdfather/,
    "the procedure has a heading somebody can be sent to");
assert.match(recipe, /ITERM_SESSION_ID/);
assert.match(recipe, /CODEX_THREAD_ID/,
    "the id a Codex session would reach for first, and why it is the wrong one");
assert.match(recipe, /POST \/v1\/orchestrator\/coordinator\/register/);
assert.match(recipe, /coordinator\/rebind/);
assert.match(recipe, /expected_generation/);
assert.match(recipe, /coordinator_online/,
    "the refusal that says a live coordinator is not to be taken over");

for (const skill of ["../skills/clawdline/SKILL.md", "../skills/clawdline/SKILL.zh-TW.md"]) {
    const text = await readFile(new URL(skill, import.meta.url), "utf8");
    assert.match(text, /Clawdfather/, `${skill} carries the procedure`);
    assert.match(text, /CODEX_THREAD_ID/, `${skill} names the wrong id`);
    assert.match(text, /expected_generation/, `${skill} names the compare-and-swap value`);
    assert.match(text, /docs\/orchestrator\.md/, `${skill} points at the long form`);
}

// Phase A2's preview-only command list is a separate, still-unresolved feature.
const controls = await readFile(
    new URL("../Resources/web/app/js/input/coordinator-actions.js", import.meta.url), "utf8"
);
assert.match(controls, /confirmDisabled: true/,
    "the Clawdfather command preview gate is untouched by this affordance");

/* ---- the localized creation copy contract --------------------------------- */

/*
   The choice on the creation sheet used to be a menu item on an existing Session, and its words
   still were: fourteen translations of *Make this session Clawdfather*, and fourteen instructions
   telling the far end to `rebind` a coordinator that had gone offline. Both are now wrong in the
   same direction — this flow registers a genuinely unregistered coordinator and nothing else — and
   copy that says otherwise is not cosmetic here, because the sentence is typed into an assistant
   that will act on it.

   So three things are checked, in the three places a word crosses on its way to the page.

   * **Every locale in `L.catalog`**, read out of the one catalog rather than a list written here,
     translates all five keys, and none of them still names `rebind`. That term survived
     untranslated in all fourteen old instructions, which makes it the one language-independent
     fingerprint of the retired branch.
   * **The retired labels are frozen below by value.** A locale quietly put back the way it was
     has no other tell — "make this session" has no shape a regular expression can find in Hindi.
   * **The page consumes the served string.** Not by grepping for `T.`, which a hardcoded literal
     beside it would satisfy, but by serving a different one and reading what comes out.
*/

const CREATION_KEYS = ["webClawdfatherCreateLabel", "webClawdfatherRegisterAsk",
                       "webClawdfatherRegisterSent", "webClawdfatherRegisterLate",
                       "webClawdfatherRegisterBlocked"];

/* The identifiers the retired Session menu item left behind. The words were corrected first and
   the names second, which is the order that leaves a window where a `webMake…` key still ships
   the creation sentence and the next reader cannot tell which contract they are holding. That
   window is closed here rather than described: none of these names may appear anywhere the page,
   the Mac or the wire can still read one.

   The two `webConfirm…` names are deleted outright rather than renamed. They were the removed
   confirmation sheet's words; the scan below is the proof that nothing reads them — including
   the key-name tables in `coordinator-actions.js`, `info.js` and `schedule.js`, which a grep for
   `T.webConfirm…` would miss and a scan of every JS byte does not. */
const RETIRED_KEYS = ["webMakeClawdfather", "webClawdfatherAsk", "webClawdfatherAsked",
                      "webConfirmClawdfatherTitle", "webConfirmClawdfatherSay"];

const swiftSources = (await readdir(new URL("../Sources/", import.meta.url)))
    .filter((name) => name.endsWith(".swift")).sort();
const jsModules = [];
async function collect(directory) {
    for (const entry of await readdir(new URL(directory, import.meta.url),
                                      { withFileTypes: true })) {
        if (entry.isDirectory()) await collect(directory + entry.name + "/");
        else if (entry.name.endsWith(".js")) jsModules.push(directory + entry.name);
    }
}
await collect("../Resources/web/app/js/");
assert.ok(jsModules.length > 20, "every page module is scanned, not a hand-written list");

for (const relative of [...jsModules, "../Resources/web/index.html",
                        ...swiftSources.map((name) => "../Sources/" + name)]) {
    const text = await readFile(new URL(relative, import.meta.url), "utf8");
    for (const retired of RETIRED_KEYS) {
        assert.ok(!text.includes(retired),
            `${relative} still carries the retired name ${retired}`);
    }
}

// The exact sentences withdrawn with the Session menu item. Listed rather than described: the
// point of the list is that a translator or a bad merge cannot put one back unnoticed.
const RETIRED_COPY = [
    "Make this session Clawdfather",
    "讓這個 session 成為 Clawdfather",
    "让这个 session 成为 Clawdfather",
    "このセッションを Clawdfather にする",
    "이 세션을 Clawdfather로 만들기",
    "Hacer de esta sesión el Clawdfather",
    "Tornar esta sessão o Clawdfather",
    "Faire de cette session le Clawdfather",
    "Diese Sitzung zum Clawdfather machen",
    "Сделать эту сессию Clawdfather",
    "Rendi questa sessione il Clawdfather",
    "इस session को Clawdfather बनाएँ",
    "Jadikan sesi ini Clawdfather",
    "Bu oturumu Clawdfather yap",
    "Asked the session to become Clawdfather"
];

const stringsSwift = await readFile(
    new URL("../Sources/Strings.swift", import.meta.url), "utf8"
);

// The typed contract, and the paragraph above it that says what the five members are for.
const docStart = stringsSwift.indexOf("/// Naming a newly created Session");
assert.notEqual(docStart, -1,
    "the Strings protocol documents naming a new Session, not turning this one into Clawdfather");
const lastMember = "var webClawdfatherRegisterBlocked: String { get }";
const docEnd = stringsSwift.indexOf(lastMember, docStart);
assert.notEqual(docEnd, -1, "the declarations follow the paragraph that explains them");
const contract = stringsSwift.slice(docStart, docEnd + lastMember.length);
assert.match(contract, /registration-only/i,
    "the documented contract names the one branch this flow performs");
// Prose may name what it forbids — the same rule the source scan below stands on — so the ban on
// `rebind` is checked where it bites: on the fourteen values, and on the sentence actually typed.
assert.match(contract, /offline/i,
    "the paragraph says out loud that an offline configured coordinator is still not replaceable");
for (const key of CREATION_KEYS) {
    assert.ok(contract.includes(`var ${key}: String { get }`),
        `${key} is declared inside the creation paragraph it belongs to`);
}

// Which languages there are is the catalog's business, not this file's — a fifteenth added
// without these five strings has to fail here rather than ship in English.
const catalogAt = stringsSwift.indexOf("static let catalog:");
assert.notEqual(catalogAt, -1);
const catalogBody = stringsSwift.slice(catalogAt, stringsSwift.indexOf("\n    ]", catalogAt));
const localeTypes = [...new Set(
    [...catalogBody.matchAll(/\("[A-Za-z-]+",\s*([A-Za-z]+)\(\)\)/g)].map((m) => m[1])
)];
assert.ok(localeTypes.length >= 14,
    `the catalog should name every translation, found ${localeTypes.length}`);

const copyBodies = new Map();
for (const name of (await readdir(new URL("../Sources/", import.meta.url))).sort()) {
    if (!name.startsWith("Copy+") || !name.endsWith(".swift")) continue;
    const text = await readFile(new URL("../Sources/" + name, import.meta.url), "utf8");
    const marks = [...text.matchAll(/^struct ([A-Za-z]+): Copy \{$/gm)];
    for (let i = 0; i < marks.length; i += 1) {
        const end = i + 1 < marks.length ? marks[i + 1].index : text.length;
        copyBodies.set(marks[i][1], { file: name, body: text.slice(marks[i].index, end) });
    }
}

function localeString(body, key) {
    const found = body.match(new RegExp('^\\s*let ' + key + ' = "([^"\\n]*)"$', "m"));
    return found ? found[1] : null;
}

for (const type of localeTypes) {
    const entry = copyBodies.get(type);
    assert.ok(entry, `${type} is named by the catalog and has a Copy conformance to read`);
    for (const key of CREATION_KEYS) {
        const value = localeString(entry.body, key);
        assert.ok(value && value.trim(),
            `${type} (${entry && entry.file}) translates ${key}`);
        assert.doesNotMatch(value, /\brebind\b/i,
            `${type}'s ${key} no longer names the retired offline-reconnect branch`);
        assert.ok(!RETIRED_COPY.includes(value),
            `${type}'s ${key} is not one of the withdrawn "make this session" sentences`);
    }
    // The instruction is typed into an assistant, so what it must carry is not a phrase but the
    // four things the far end cannot work out for itself.
    const ask = localeString(entry.body, "webClawdfatherRegisterAsk");
    assert.ok(ask.includes("{id}"),
        `${type} keeps the id hole, or the line is addressed to nobody`);
    assert.ok(ask.includes("~/.config/clawdline/orchestrator-token"),
        `${type} names the one credential only the session can read`);
    assert.ok(ask.includes("docs/orchestrator.md"),
        `${type} sends the session to the written recipe`);
    assert.ok(ask.includes("Clawdfather"),
        `${type} names the role, which is not translated`);
}

// The baked-in English on the page and the English the Mac would send are one sentence, not two.
// `check-web-strings.py` compares the names at this boundary; nothing compared the words.
const englishCopy = copyBodies.get("English");
assert.ok(englishCopy, "there is an English Copy to compare the page's fallback against");
for (const key of CREATION_KEYS) {
    assert.equal(T[key], localeString(englishCopy.body, key),
        `the page's baked-in ${key} is the same sentence /v1/strings would send in English`);
}

/* ---- the page prints what it was sent ------------------------------------- */

const bakedLabel = T.webClawdfatherCreateLabel;
const bakedAsk = T.webClawdfatherRegisterAsk;

T.webClawdfatherCreateLabel = "把新的 session 命名為 Clawdfather";
assert.equal(clawdfatherCreationLabel(), "把新的 session 命名為 Clawdfather",
    "the sheet prints the served translation rather than a hardcoded English literal");

for (const unusable of ["", "   ", null, 42]) {
    T.webClawdfatherCreateLabel = unusable;
    assert.equal(clawdfatherCreationLabel(), bakedLabel,
        "an unusable served value falls back to English on purpose, never to a blank chip");
}
T.webClawdfatherCreateLabel = bakedLabel;

assert.match(start, /start-clawdfather-label"\]\.textContent = clawdfatherCreationLabel\(\)/,
    "the creation sheet overwrites the markup's English with the localized label on every draw");

const englishLine = fill(bakedAsk, { id: ordinary.id });

T.webClawdfatherRegisterAsk = "請把 {id} 註冊成這台 Mac 的 Clawdfather。"
    + "先讀 ~/.config/clawdline/orchestrator-token。";
const localizedLine = clawdfatherInstruction(ordinary);
assert.match(localizedLine, /^請把 35D87610-E7F4-4A9A-95A0-11947CF5115C 註冊成/,
    "the typed sentence is localized too, with the id filled into the served translation");

// A translation is copy, and copy can be wrong. These three shapes are refused rather than typed,
// because the thing on the other end acts on the sentence it is given.
T.webClawdfatherRegisterAsk = "Register this session as Clawdfather, and reconnect an offline one "
    + "with rebind. Your id is {id}.";
const rebindLine = clawdfatherInstruction(ordinary);
assert.doesNotMatch(rebindLine, /rebind/i,
    "a translation that still teaches offline replacement is refused, not typed into a session");
assert.equal(rebindLine, englishLine,
    "and what is typed instead is the registration-only English");

T.webClawdfatherRegisterAsk = "Register this new session as this Mac's Clawdfather.";
assert.equal(clawdfatherInstruction(ordinary), englishLine,
    "a translation with no id hole would address nobody, so the English line is sent instead");

for (const unusable of ["", "   ", null, 42]) {
    T.webClawdfatherRegisterAsk = unusable;
    assert.equal(clawdfatherInstruction(ordinary), englishLine,
        "an unusable served instruction falls back to English rather than sending nothing");
}
T.webClawdfatherRegisterAsk = bakedAsk;
assert.equal(clawdfatherInstruction(ordinary), englishLine,
    "and the default page is back where it started");
assert.equal(clawdfatherInstruction(null), "",
    "no session is still no instruction, whichever language is loaded");

/* ---- every word on this flow comes from the Mac ---------------------------- */

/*
   The label was localized first and the two toasts beside it were not, because they were in a
   file the copy task did not own. `tools/check-web-strings.py` cannot see the difference: it
   compares names, and a hardcoded English literal has no name. So the sentences are checked
   here, where they are — one scan for the literals, and one read of the `T` names that replaced
   them.
*/

const CREATION_SENTENCES = [
    "did not become ready in time",
    "was asked to become Clawdfather",
    "Name the new session Clawdfather"
];
for (const sentence of CREATION_SENTENCES) {
    assert.ok(!start.includes(sentence),
        `start.js composes "${sentence}" from a served string rather than shipping it in English`);
}
for (const key of ["webClawdfatherRegisterSent", "webClawdfatherRegisterLate",
                   "webClawdfatherRegisterBlocked"]) {
    assert.ok(start.includes("T." + key),
        `the creation sheet says ${key} in the reader's language, resolved at render time`);
}

// The markup's own fallback was the third place the English sentence lived. It was never seen —
// the row ships hidden and `drawClawdfather` overwrites the span before unhiding it — but it was
// one merge away from being the only sentence a non-English reader got.
assert.match(index, /<span id="start-clawdfather-label"><\/span>/,
    "the creation chip ships empty and is filled from /v1/strings, never from markup English");

console.log("web clawdfather tests passed");
process.exit(0);
