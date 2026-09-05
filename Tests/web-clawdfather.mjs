import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readdir, readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

import { T, fill } from "../Resources/web/app/js/core/i18n.js";
import {
    attemptClawdfatherAssignment,
    clawdfatherChoiceSupported,
    clawdfatherCreationChoice,
    clawdfatherCreationLabel,
    clawdfatherInstruction,
    createClawdfatherAssignmentState,
    createClawdfatherCoordinatorLoader
} from "../Resources/web/app/js/input/clawdfather.js";
import { coordinatorOfflineAdvice } from "../Resources/web/app/js/input/coordinator-actions.js";

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
    unknown: { registration: { state: "configured" },
               coordinator: { configured: true, status: "unknown", lifecycle: "unknown",
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

for (const status of ["online", "offline", "unknown"]) {
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
assert.match(start, /coordinatorPresenceText\(coordinator\)/,
    "the creation sheet uses the same three-state coordinator words as the controls");

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

// The procedure moved into the shipped guide when SKILL.md became a discovery stub: an env var
// name and a compare-and-swap field are exactly the detail a copied skill file goes stale on, so
// they are asserted where they now live, beside the build that serves them.
for (const guide of ["../Resources/skill-guides/clawdline.md",
                     "../Resources/skill-guides/clawdline.zh-TW.md"]) {
    const text = await readFile(new URL(guide, import.meta.url), "utf8");
    assert.match(text, /Clawdfather/, `${guide} carries the procedure`);
    assert.match(text, /CODEX_THREAD_ID/, `${guide} names the wrong id`);
    assert.match(text, /expected_generation/, `${guide} names the compare-and-swap value`);
    assert.match(text, /docs\/orchestrator\.md/, `${guide} points at the long form`);
}

// The two lines a handoff sender writes into the package, held here because the package is the only
// carrier that reaches a receiver in a project where nothing of this project is installed. On
// 2026-09-05 only the naming line existed: a receiver made that call on arrival and finished
// reporting nothing, and said afterwards it had searched for a publishing route and concluded a
// root had none. A shipped guide that stops carrying the second line puts that back, and nothing
// else would notice — the receipt is not a route any test here calls.
for (const guide of ["../Resources/skill-guides/clawdline.md",
                     "../Resources/skill-guides/clawdline.zh-TW.md"]) {
    const text = await readFile(new URL(guide, import.meta.url), "utf8");
    assert.match(text, /sessions\/<[^>]*>\/title/,
        `${guide} no longer gives the sender the naming line to write into the package`);
    assert.match(text, /sessions\/<[^>]*>\/complete/,
        `${guide} no longer gives the sender the delivery-receipt line to write into the package`);
    assert.match(text, /orchestrator\/whoami/,
        `${guide}: the receipt line does not say how a session resolves its own terminal id, which `
        + `is the half a receiver cannot look up`);
}

// A session named to a person by its id alone is unreadable — the person cannot tell which of
// their own tabs it is — and the id does not even survive the next app restart, while the label
// does. The wire format already pairs them (`source.label` beside `source.id`); prose was the only
// place the name was dropped, and it was dropped through a whole conversation before anybody said
// so. Held on every surface that tells an assistant how to talk about another session.
// Each surface is asked in its own words. Written once as a shared `/restart/i` this block was
// green on the Chinese guide with that guide's rule deleted, because the English word appears
// elsewhere in a long file — a check passing for a reason unrelated to its subject, which is the
// defect this whole delivery was about.
for (const [surface, expiry] of [
    ["../Resources/skill-guides/clawdline.md", /reissued whenever the app restarts/],
    ["../Resources/skill-guides/clawdline.zh-TW.md", /每次 app 重啟就重發/],
    ["../AGENTS.md", /reissued whenever the app restarts/],
]) {
    const text = await readFile(new URL(surface, import.meta.url), "utf8");
    assert.match(text, /source\.label/,
        `${surface} no longer says the wire already pairs the label with the id`);
    assert.match(text, expiry,
        `${surface} no longer says why the id is the half that expires`);
}

// And the install step for the one carrier this project does not ship: a global CLAUDE.md is the
// user's file, so if the documentation stops telling them it exists, the only people who get it
// are the ones who already knew.
{
    const orchestrator = await readFile(new URL("../docs/orchestrator.md", import.meta.url), "utf8");
    assert.match(orchestrator, /optional line in your global `CLAUDE\.md`/i,
        "docs/orchestrator.md no longer documents the optional global rule");
    for (const readme of ["../README.md", "../README.zh-TW.md"]) {
        const text = await readFile(new URL(readme, import.meta.url), "utf8");
        assert.ok(text.includes("and-one-optional-line-in-your-global-claudemd"),
            `${readme} does not point an installer at the optional global rule`);
    }
}

// What the stub itself still owes: it must route a reader to that guide rather than teach the
// procedure, and it must not have quietly grown the detail back.
for (const stub of ["../skills/clawdline/SKILL.md", "../skills/clawdline/SKILL.zh-TW.md"]) {
    const text = await readFile(new URL(stub, import.meta.url), "utf8");
    assert.match(text, /clawdline-skill\.sh/, `${stub} names the reader`);
    assert.match(text, /CLAWDLINE_SKILL_READER/, `${stub} honours the reader override`);
    assert.match(text, /get clawdline/, `${stub} shows how to load the guide`);
    assert.doesNotMatch(text, /CODEX_THREAD_ID/,
        `${stub} must not re-absorb guide detail that goes stale in a copied file`);
    // Why: the three banned strings above only catch what we thought to ban, and the dangerous
    // direction is the other one — somebody lands skill prose on the stub because that is where
    // it used to live, and every token check stays green. A size ceiling has no such blind spot:
    // the body it replaced was 1,439 lines, so anything approaching that is the old file coming
    // back whatever words it uses. Raise this deliberately if a stub ever needs to say more.
    const lines = text.split("\n").length;
    assert.ok(lines <= 140,
        `${stub}: ${lines} lines. A discovery stub carries routing and the role contract, not the
         guide. Content this size belongs in Resources/skill-guides/, which ships with the build.`);
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

/* ---- a transport with no Bearings read draws no row ----------------------- */

/*
   `docs/api.md` writes the rule down once for all five reads the Cloud path does not carry:
   each is guarded at its call site by `typeof api.X === "function"`, "so the control it belongs
   to is not drawn at all. That is a quieter answer than a button that fails when pressed, and
   it is the deliberate one for a feature that is missing rather than refused."

   This control was the exception, and it was visible on a phone: the row was drawn, greyed, with
   *Could not read Clawdfather's bearings* under it. The sentence is true of the hop and false of
   the machine — the Mac's record was `configured` the whole time, and this page has a correct
   word for that. What it did not have was a way to tell *this transport has no such read* from
   *the read failed*, so it said the second about the first.

   The question is asked of the transport, never of an answer, which is what makes it different
   from every other state on this sheet.
*/

assert.equal(clawdfatherChoiceSupported({ coordinatorBearings: function () { } }), true,
    "a transport that carries the Bearings read may draw the creation choice");
assert.equal(clawdfatherChoiceSupported({}), false,
    "a transport with no Bearings read carries no creation choice to draw");
assert.equal(clawdfatherChoiceSupported({ coordinatorBearings: true }), false,
    "a truthy non-function is not a read; missing is not permission here either");
for (const absent of [null, undefined, 0, "", "coordinatorBearings"]) {
    assert.equal(clawdfatherChoiceSupported(absent), false,
        "no client at all is no read either");
}

/* And the shapes are the real transports', not this file's idea of them. A fixture that has
   drifted from the client it stands for proves the guard against nothing. */
const cloudClient = await readFile(
    new URL("../Resources/web/app/js/net/cloud-client.js", import.meta.url), "utf8");
const liveClient = await readFile(
    new URL("../Resources/web/app/js/net/live.js", import.meta.url), "utf8");
const mockClient = await readFile(
    new URL("../Resources/web/app/js/net/mock.js", import.meta.url), "utf8");
assert.ok(!cloudClient.includes("coordinatorBearings"),
    "the cloud client has no Bearings read — that absence is deliberate and is what this guards");
assert.match(liveClient, /coordinatorBearings:\s*function/,
    "the direct client does carry it, so the guard is not simply always false");
assert.match(mockClient, /coordinatorBearings:\s*function/,
    "and so do the fixtures, or the offline flow would lose the row it is supposed to keep");

/* ---- an offline crown says what puts it back ------------------------------ */

/*
   `Sources/Coordinator.swift` computes this correctly and has done all along: a binding whose
   process is gone reads `status`/`lifecycle` `offline`, on the existing criterion
   (`sessionsObservedAt >= bindingChangedAt`) and no invented time threshold. What was missing is
   downstream of that word. It reached exactly one screen — this sheet — and arrived as a status
   and nothing else, so the one place that could have said the crown had fallen said it in two
   words and named no repair. On 2026-09-04 that cost hours: the coordinator was offline, the
   binding needed a rebind, and nothing anywhere said so.

   The advice is a fact about the Mac's own word and never about this browser's connection. A
   disconnected phone downgrades presence to offline for the reader's sake — see
   `coordinatorPresenceState` — and telling somebody to reconnect Clawdfather because their own
   socket dropped would be advice about the wrong end of the wire.
*/

const offlineAdvice = coordinatorOfflineAdvice({ status: "offline", label: "Clawdfather" });
assert.ok(offlineAdvice, "an offline coordinator is told how it is reconnected");
assert.ok(offlineAdvice.includes(T.webCoordCmdReconnect),
    "the advice names the action — reconnecting the binding — rather than restating the status");
assert.ok(offlineAdvice.includes(T.webCoordWhyMachineTokenOnly),
    "and what that action needs, which is the Mac's own orchestrator token");
assert.ok(!/rebind/i.test(offlineAdvice),
    "said in the page's own translated words, never in a route name only this repository knows");

for (const quiet of [{ status: "online" }, { status: "unknown" }, { status: "unregistered" },
                     {}, null, undefined]) {
    assert.equal(coordinatorOfflineAdvice(quiet), "",
        "nothing but the Mac's own offline word puts repair advice on screen");
}
assert.equal(coordinatorOfflineAdvice({ status: "online" }, { connected: false }), "",
    "a browser that lost its own connection is not a coordinator that lost its binding");

/* ---- the two rows as the phone actually got them -------------------------- */

/*
   Both facts above are decisions about a row on a screen, and a scan of the source cannot see a
   row. What went wrong was that a control *appeared*, greyed, with the wrong reason under it —
   a shape every unit assertion in this file was happy with.

   So `start.js` is loaded against the smallest DOM that will hold it, the technique
   `Tests/web-session-resilience.mjs` uses for the Codex resume path, and the sheet is opened once
   per transport. What is asserted is what the phone showed: whether the row is there at all, and
   what the line under it says.
*/

if (process.env.CLAWDLINE_CLAWDFATHER_SHEET_BEHAVIOR === "1") {
    const noop = function () { };
    const canvas = {
        clearRect: noop, fillRect: noop, beginPath: noop, moveTo: noop, lineTo: noop,
        stroke: noop, save: noop, restore: noop, imageSmoothingEnabled: false,
        fillStyle: "", strokeStyle: ""
    };
    function testElement(tag) {
        const children = [];
        const attributes = new Map();
        const classes = new Set();
        const descendants = new Map();
        const target = {
            tagName: String(tag || "div").toUpperCase(), children, childNodes: children,
            style: {}, dataset: {}, hidden: false, disabled: false, value: "", title: "",
            textContent: "", className: "", placeholder: "",
            scrollHeight: 0, scrollTop: 0, clientHeight: 0, parentNode: null,
            appendChild: function (child) {
                child.parentNode = proxy; children.push(child); return child;
            },
            // The same module puts its button in a named place rather than at the end.
            insertBefore: function (child, before) {
                const at = before ? children.indexOf(before) : -1;
                child.parentNode = proxy;
                if (at >= 0) children.splice(at, 0, child); else children.push(child);
                return child;
            },
            removeChild: function (child) {
                const at = children.indexOf(child);
                if (at >= 0) children.splice(at, 1);
                child.parentNode = null; return child;
            },
            setAttribute: function (name, value) { attributes.set(name, String(value)); },
            getAttribute: function (name) {
                return attributes.has(name) ? attributes.get(name) : null;
            },
            removeAttribute: function (name) { attributes.delete(name); },
            toggleAttribute: function (name, force) {
                const on = force === undefined ? !attributes.has(name) : !!force;
                if (on) attributes.set(name, ""); else attributes.delete(name);
                return on;
            },
            addEventListener: function (name, fn) { target["on" + name] = fn; },
            querySelector: function (selector) {
                if (!descendants.has(selector)) descendants.set(selector, testElement("span"));
                return descendants.get(selector);
            },
            querySelectorAll: function () { return []; },
            closest: function () { return proxy; },
            focus: noop,
            animate: function () { return { cancel: noop, onfinish: null }; },
            getBoundingClientRect: function () {
                return { top: 0, left: 0, width: 0, height: 0, bottom: 0, right: 0 };
            },
            getContext: function () { return canvas; }
        };
        Object.defineProperty(target, "innerHTML", {
            get: function () { return target._innerHTML || ""; },
            set: function (value) {
                target._innerHTML = value; children.splice(0); descendants.clear();
            }
        });
        target.classList = {
            add: function (...names) { names.forEach(function (n) { classes.add(n); }); },
            remove: function (...names) { names.forEach(function (n) { classes.delete(n); }); },
            toggle: function (name, force) {
                const on = force === undefined ? !classes.has(name) : !!force;
                if (on) classes.add(name); else classes.delete(name);
                return on;
            },
            contains: function (name) { return classes.has(name); }
        };
        const proxy = new Proxy(target, {
            get: function (object, key) {
                if (key === Symbol.iterator) return function* () { yield* children; };
                if (key === "content") return { cloneNode: function () { return testElement(); } };
                if (key in object) return object[key];
                return undefined;
            }
        });
        return proxy;
    }

    const root = testElement();
    const elements = new Map();
    function elementWithID(id) {
        if (!elements.has(id)) {
            elements.set(id, testElement(id.includes("filter") ? "input" : "div"));
        }
        return elements.get(id);
    }
    globalThis.localStorage = { getItem: function () { return null; }, setItem: noop };
    globalThis.location = { search: "", protocol: "http:", hostname: "localhost", pathname: "/" };
    globalThis.history = { replaceState: noop };
    globalThis.getComputedStyle = function () { return { opacity: "1", marginLeft: "0" }; };
    Object.defineProperty(globalThis, "navigator", {
        value: { userAgent: "node", maxTouchPoints: 0 }, configurable: true
    });
    globalThis.window = root;
    window.devicePixelRatio = 1;
    window.innerHeight = 800;
    window.visualViewport = { height: 800, offsetTop: 0, addEventListener: noop };
    window.matchMedia = function () { return { matches: false, addEventListener: noop }; };
    globalThis.document = root;
    document.documentElement = { lang: "en", style: { setProperty: noop, removeProperty: noop } };
    document.getElementById = elementWithID;
    document.querySelector = function () { return root; };
    document.querySelectorAll = function () { return []; };
    document.createElement = function (tag) { return testElement(tag); };
    document.body = root;
    // A module that ships its own stylesheet appends the `<link>` at import time, so a
    // fixture with a `body` and no `head` throws before any assertion in this file runs.
    // `Resources/web/app/js/input/snippets.js` is the one that does; on `f95b5cdb` it made
    // every group here unreachable rather than red, and `./test.sh` stops on this suite
    // before the Swift half is ever compiled.
    document.head = testElement("head");
    globalThis.MutationObserver = class { observe() { } disconnect() { } };
    globalThis.ResizeObserver = MutationObserver;
    globalThis.IntersectionObserver = MutationObserver;

    const { useApi } = await import("../Resources/web/app/js/net/api.js");
    const { S } = await import("../Resources/web/app/js/core/state.js");
    const { Start } = await import("../Resources/web/app/js/input/start.js");

    const places = function () {
        return Promise.resolve({
            places: [{ id: "place-one", path: "/repo/one", label: "one" }],
            assistants: [{ id: "claude", label: "Claude" }]
        });
    };
    const row = elementWithID("start-clawdfather-row");
    const line = elementWithID("start-clawdfather-state");
    const settle = function () {
        return new Promise(function (resolve) { setTimeout(resolve, 0); });
    };
    S.write = true;

    // The hosted console, exactly: everything the sheet needs except this one read.
    useApi({ places: places, startPlace: noop });
    Start.open();
    await settle();
    assert.equal(row.hidden, true,
        "a transport with no Bearings read draws no Clawdfather row, per docs/api.md");
    assert.equal(line.textContent, "",
        "and says nothing about a read it never attempted — least of all that one failed");
    Start.close();

    // The same sheet on the direct path, over a Mac whose crown has fallen off.
    useApi({
        places: places, startPlace: noop,
        coordinatorBearings: function () {
            return Promise.resolve({
                registration: { state: "configured" },
                coordinator: { configured: true, status: "offline", lifecycle: "offline",
                               scope: "machine", label: "Clawdfather" }
            });
        }
    });
    Start.open();
    await settle();
    assert.equal(row.hidden, false, "the direct path still offers the row");
    assert.equal(elementWithID("start-clawdfather").disabled, true,
        "a configured coordinator, offline included, still closes the switch");
    assert.ok(line.textContent.includes(T.webCoordCmdReconnect),
        "an offline crown is not left as a status word: the line says what puts it back. Got: "
        + line.textContent);
    assert.ok(line.textContent.includes(T.webCoordWhyMachineTokenOnly),
        "and what reconnecting needs, which is the whole of the next step from a phone");
    Start.close();

    // Online is the case that must not grow the sentence: advice nobody needs is noise, and
    // noise on a status line is how the one that mattered stopped being read.
    useApi({
        places: places, startPlace: noop,
        coordinatorBearings: function () {
            return Promise.resolve({
                registration: { state: "configured" },
                coordinator: { configured: true, status: "online", lifecycle: "standby",
                               scope: "machine", label: "Clawdfather" }
            });
        }
    });
    Start.open();
    await settle();
    assert.equal(row.hidden, false);
    assert.ok(!line.textContent.includes(T.webCoordCmdReconnect),
        "an online Clawdfather is told nothing about reconnecting");
    Start.close();

    // The boundary the first arm draws, from its other side. A transport that *has* this read and
    // cannot answer it is not a transport that lacks it: the first is a failure and is said out
    // loud, the second is an absence and is silent. Widening the hidden case to swallow a failed
    // read is the one regression that turns this delivery back into the bug it fixes, and it left
    // the whole file green — `webCoordReadFailed` appeared in no test in this repository until
    // this arm, so the sentence a reader would actually see was pinned by nothing.
    useApi({
        places: places, startPlace: noop,
        coordinatorBearings: function () { return Promise.reject(new Error("relay is down")); }
    });
    Start.open();
    await settle();
    assert.equal(row.hidden, false,
        "a read this transport carries and could not complete still draws the row");
    assert.ok(line.textContent.includes(T.webCoordReadFailed),
        "and says the read failed, which is true here and was the false sentence before. Got: "
        + line.textContent);
    assert.ok(!line.textContent.includes(T.webCoordCmdReconnect),
        "without advising a reconnect it has no evidence is needed");

    console.log("web clawdfather creation sheet behavior passed");
    process.exit(0);
}

const sheetBehavior = spawnSync(process.execPath, [fileURLToPath(import.meta.url)], {
    cwd: process.cwd(), encoding: "utf8",
    env: { ...process.env, CLAWDLINE_CLAWDFATHER_SHEET_BEHAVIOR: "1" }
});
assert.equal(sheetBehavior.status, 0,
    "the isolated creation-sheet fixture passes: "
    + (sheetBehavior.stderr || sheetBehavior.stdout));
assert.match(sheetBehavior.stdout, /creation sheet behavior passed/,
    "the behavior fixture reached the end of all three transports");

console.log("web clawdfather tests passed");
process.exit(0);
