import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

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
   -------------------------------------------------------------------------- */

const ordinary = { id: "35D87610-E7F4-4A9A-95A0-11947CF5115C", assistant: "claude",
                   label: "ordinary session" };
const reading = clawdfatherCreationChoice(null, true, true);
assert.equal(reading.shown, true);
assert.equal(reading.enabled, false,
    "the choice fails closed until the machine coordinator record has been read");
assert.equal(reading.checked, false,
    "a stale tick is never shown while coordinator ownership is unknown");

const available = clawdfatherCreationChoice({
    coordinator: { configured: false, status: "unregistered", lifecycle: "unregistered" }
}, true, true);
assert.equal(available.shown, true);
assert.equal(available.enabled, true);
assert.equal(available.checked, true);
assert.equal(available.state, "selected");

for (const status of ["online", "offline"]) {
    const assigned = clawdfatherCreationChoice({
        coordinator: { configured: true, label: "Clawdfather", status }
    }, true, true);
    assert.equal(assigned.shown, true);
    assert.equal(assigned.enabled, false,
        `an ${status} configured coordinator prevents assigning another one`);
    assert.equal(assigned.checked, false);
    assert.equal(assigned.state, "assigned");
}

// These are future projection shapes, not proof that today's device response distinguishes a
// corrupt/unsupported record from absence. They keep a later backend F2 from opening the switch
// merely because it starts exposing a more precise state.
for (const malformed of [
    { configured: false, status: "corrupt", lifecycle: "unregistered" },
    { configured: false, status: "unsupported", lifecycle: "unregistered" },
    { configured: false, status: "unregistered" },
    { configured: false, lifecycle: "unregistered" }
]) {
    const refused = clawdfatherCreationChoice({ coordinator: malformed }, true, true);
    assert.equal(refused.enabled, false,
        "only the canonical tuple opens; synthetic future states remain fail-closed");
    assert.equal(refused.checked, false);
    assert.equal(refused.state, "unavailable");
}

assert.equal(clawdfatherCreationChoice({
    coordinator: { configured: false }
}, true, false).shown, false,
"resuming an existing conversation is not creation and never offers the choice");

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
        return Promise.resolve({
            coordinator: blockedReads === 1
                ? { configured: false, status: "unregistered", lifecycle: "unregistered" }
                : { configured: true, label: "Clawdfather", status: "online",
                    lifecycle: "standby" }
        });
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

let allowedReads = 0;
let allowedSends = 0;
const sentAssignment = await attemptClawdfatherAssignment(ordinary, {
    coordinatorBearings: function () {
        allowedReads += 1;
        return Promise.resolve({
            coordinator: { configured: false, status: "unregistered",
                           lifecycle: "unregistered" }
        });
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
        coordinatorBearings: function () { return Promise.resolve({
            coordinator: { configured: false, status: "unregistered",
                           lifecycle: "unregistered" }
        }); },
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
coordinatorResolvers[1].resolve({
    coordinator: { configured: false, status: "unregistered", lifecycle: "unregistered" }
});
await secondCoordinatorRead;
coordinatorResolvers[0].resolve({
    coordinator: { configured: true, status: "online", lifecycle: "standby" }
});
const staleCoordinatorRead = await firstCoordinatorRead;
assert.equal(staleCoordinatorRead.stale, true);
assert.equal(coordinatorStates.at(-1).payload.coordinator.configured, false,
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
        coordinatorBearings: function () { return Promise.resolve({
            coordinator: { configured: false, status: "unregistered",
                           lifecycle: "unregistered" }
        }); },
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

console.log("web clawdfather tests passed");
process.exit(0);
