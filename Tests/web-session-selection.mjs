import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import {
    createSessionSelectionLifecycle,
    machinePresentation,
    machinePresentationForFleet,
    SessionSelection,
    sameSessionSelection,
    sessionSelectionIdentity,
    sessionSelectionKey
} from "../Resources/web/app/js/session/selection.js";
import { clampSkillPickerIndex, selectedSkill } from
    "../Resources/web/app/js/input/skill-picker-state.js";

let checks = 0;
function check(condition, message) { checks += 1; assert.ok(condition, message); }
function equal(actual, expected, message) { checks += 1; assert.equal(actual, expected, message); }

function row(id, machine, conversation, extra = {}) {
    return { id, machine, sessionId: conversation,
        identity: { machine, session: id }, title: extra.title || "Display title", ...extra };
}

const EN_MACHINE_COPY = { webMachineThisMac: "This Mac", webMachineMac: "Mac",
    webMachineLinux: "Linux", webMachineLinuxAWS: "Linux / AWS" };
const ZH_HANT_MACHINE_COPY = { webMachineThisMac: "這台 Mac", webMachineMac: "Mac",
    webMachineLinux: "Linux", webMachineLinuxAWS: "Linux / AWS" };

assert.deepEqual(machinePresentation({ machine: "this-mac" }, EN_MACHINE_COPY), {
    id: "this-mac", name: "This Mac", platform: "macos", provider: null,
    kind: "mac", label: "Mac · This Mac"
}); checks += 1;
assert.deepEqual(machinePresentation({ machine: "aws-node-1", machineName: "Builder East",
    machinePlatform: "linux", cloudProvider: "aws" }, EN_MACHINE_COPY), {
    id: "aws-node-1", name: "Builder East", platform: "linux", provider: "aws",
    kind: "linux-aws", label: "Linux / AWS · Builder East"
}); checks += 1;
equal(machinePresentation({ machine: "mac_opaque", label: "Session title" }).label, "Machine · c_opaque",
    "an opaque machine id is shown authoritatively without guessing from its prefix or session title");
equal(machinePresentation({ machine: "machine-1", machineName: "Build\nHost" }).label, "Build Host",
    "display metadata cannot inject a second line into a Session row");
equal(machinePresentation({ machine: "this-mac" }, ZH_HANT_MACHINE_COPY).label, "Mac · 這台 Mac",
    "a zh-Hant local Session row receives its machine words from served Copy");
const spoofedMachineID = "worker\u202e/evil\u2069\u200b-id";
const spoofed = machinePresentation({ machine: spoofedMachineID,
    machineName: "Build\u202e Mac\u2069\u200b" });
equal(spoofed.id, spoofedMachineID,
    "display sanitizing never changes the opaque routing authority");
equal(spoofed.label, "Build Mac",
    "bidi controls, isolates and zero-width spoof characters cannot enter a badge");
const duplicateFleet = [
    { machine: "machine-one-shared88", machineName: "Builder" },
    { machine: "machine-two-shared88", machineName: "Builder" }
];
const firstDuplicate = machinePresentationForFleet(duplicateFleet[0], duplicateFleet, EN_MACHINE_COPY);
const secondDuplicate = machinePresentationForFleet(duplicateFleet[1], duplicateFleet, EN_MACHINE_COPY);
check(firstDuplicate.label !== secondDuplicate.label && firstDuplicate.label.includes("shared88-1") &&
    secondDuplicate.label.includes("shared88-2"),
"duplicate display names carry visible short-id disambiguators without relying on hover");

const a = row("terminal-1", "mac-a", "conversation-a");
const b = row("terminal-1", "mac-b", "conversation-b");
const identity = sessionSelectionIdentity(a);
assert.deepEqual(identity.route, { machine: "mac-a", session: "terminal-1" }); checks += 1;
equal(identity.conversation, "conversation-a", "provider conversation is part of selection identity");
equal(sessionSelectionKey({ ...a, title: "Renamed", label: "New label", tty: "ttys999" }),
    identity.key, "title, label and terminal display metadata never change authority");
check(Object.isFrozen(identity) && Object.isFrozen(identity.route),
    "selection identities and transport routes are immutable snapshots");

const lifecycle = createSessionSelectionLifecycle();
equal(lifecycle.resolve("terminal-1", [a, b]), null,
    "a duplicate bare terminal id across machines is refused instead of guessed");
equal(lifecycle.open(a, [a, b]).identity.key, identity.key,
    "an explicit row opens the exact machine/conversation identity");
equal(lifecycle.snapshot().open.machine, "mac-a", "the selected route keeps its machine");

const transcript = lifecycle.beginEffect("transcript");
check(lifecycle.effectIsCurrent(transcript), "an effect is current in the epoch where it began");
const newerTranscript = lifecycle.beginEffect("transcript");
equal(lifecycle.effectIsCurrent(transcript), false,
    "a newer response lane makes the older overlapping response stale");
check(lifecycle.effectIsCurrent(newerTranscript), "the newest overlapping response owns its lane");
const send = lifecycle.beginEffect("send");
check(lifecycle.effectIsCurrent(send), "independent lanes may overlap without cancelling each other");

lifecycle.open(b, [a, b]);
equal(lifecycle.effectIsCurrent(newerTranscript), false,
    "switching machines fences a late transcript response");
equal(lifecycle.effectIsCurrent(send), false,
    "switching machines fences a late send response from clearing the new composer");

const bFirstOpen = lifecycle.beginEffect("info");
lifecycle.close();
lifecycle.open(b, [a, b]);
equal(lifecycle.effectIsCurrent(bFirstOpen), false,
    "close and reopen of the same identity is a new effect epoch");

const beforeReplacement = lifecycle.beginEffect("transcript");
const replaced = row("terminal-1", "mac-b", "conversation-new");
const reconcile = lifecycle.reconcile([a, replaced]);
check(reconcile.openRemoved, "same machine/terminal with a new conversation is replacement, not continuity");
equal(lifecycle.snapshot().open, null, "selection replacement closes the stale open identity");
equal(lifecycle.effectIsCurrent(beforeReplacement), false,
    "replacement invalidates every response issued for the old conversation");

lifecycle.open(replaced, [a, replaced]);
const refresh = lifecycle.beginEffect("transcript");
// A failed refresh does not mutate selection; the caller may keep the last good transcript.
check(lifecycle.effectIsCurrent(refresh), "a failed refresh can settle without unbinding selection");
equal(lifecycle.finishEffect(refresh), true, "the current failed refresh settles only its own lane");
equal(lifecycle.snapshot().open.conversation, "conversation-new",
    "failed refresh leaves the exact selected conversation pinned");

const learning = createSessionSelectionLifecycle();
const unknownConversation = row("terminal-learn", "mac-a", null);
const knownConversation = row("terminal-learn", "mac-a", "learned-uuid");
learning.open(unknownConversation, [unknownConversation]);
const beforeLearning = learning.beginEffect("send");
const learned = learning.reconcile([knownConversation]);
equal(learned.openRemoved, false,
    "learning a previously missing conversation id preserves the open selection");
equal(learning.snapshot().open.conversation, "learned-uuid",
    "the owner upgrades its immutable selection to the newly authoritative id");
check(learning.effectIsCurrent(beforeLearning),
    "missing-to-present enrichment preserves an in-flight effect on the same route");
check(sameSessionSelection(sessionSelectionIdentity(unknownConversation),
    sessionSelectionIdentity(knownConversation)), "the production continuity rule is one-way enrichment");
equal(sameSessionSelection(sessionSelectionIdentity(knownConversation),
    sessionSelectionIdentity({ ...knownConversation, machine: "mac-b",
        identity: { machine: "mac-b", session: "terminal-learn" } })), false,
    "conversation enrichment never infers continuity across machines");

const listActions = createSessionSelectionLifecycle();
listActions.open(a, [a, b]);
const backgroundPrompt = listActions.beginEffect("prompt", {
    identity: sessionSelectionIdentity(b), requiresOpen: false
});
listActions.open(a, [a, b]);
check(listActions.effectIsCurrent(backgroundPrompt),
    "a non-open row action retains typed settlement feedback while its exact target exists");
listActions.reconcile([a, row("terminal-1", "mac-b", "replacement-b")]);
equal(listActions.effectIsCurrent(backgroundPrompt), false,
    "a real conversation replacement still fences the non-open row action");
const disappearingEnd = listActions.beginEffect("end", {
    identity: sessionSelectionIdentity(a), requiresOpen: false, allowMissing: true
});
listActions.reconcile([]);
check(listActions.effectIsCurrent(disappearingEnd),
    "an end action may finish visibly when disappearance is its successful evidence");

const oldTransportEffect = lifecycle.beginEffect("send");
lifecycle.replaceTransport();
equal(lifecycle.snapshot().open, null, "local/Cloud/mock replacement clears the old selection");
equal(lifecycle.effectIsCurrent(oldTransportEffect), false,
    "a response from the replaced transport cannot mutate the next transport's UI");

const selectedAPI = await import("../Resources/web/app/js/net/api.js");
const apiSelection = sessionSelectionIdentity(a);
selectedAPI.useClient({ selectionTransportIdentity: "cloud\u0000relay\u0000acct\u0000device" });
SessionSelection.open(apiSelection, [a]);
const rotatingEffect = SessionSelection.beginEffect("send");
selectedAPI.useClient({ selectionTransportIdentity: "cloud\u0000relay\u0000acct\u0000device" });
check(SessionSelection.snapshot().open && SessionSelection.snapshot().open.key === apiSelection.key,
    "rotating a Cloud credential preserves the open conversation on the same logical route");
check(SessionSelection.effectIsCurrent(rotatingEffect),
    "credential rotation preserves effects already sent through that logical transport");
selectedAPI.useClient({ selectionTransportIdentity: "cloud\u0000other-relay\u0000acct\u0000device" });
equal(SessionSelection.snapshot().open, null,
    "a real relay/account/device transport replacement invalidates selection");
const { CloudClient } = await import("../Resources/web/app/js/net/cloud-client.js");
const cloudBeforeRotation = new CloudClient({ relayURL: "wss://relay.example",
    deviceToken: "short-lived-one", account: "acct", deviceID: "viewer" });
const cloudAfterRotation = new CloudClient({ relayURL: "wss://relay.example",
    deviceToken: "short-lived-two", account: "acct", deviceID: "viewer" });
equal(cloudBeforeRotation.selectionTransportIdentity,
    cloudAfterRotation.selectionTransportIdentity,
    "the real Cloud client identity excludes its rotating credential");
check(new CloudClient({ relayURL: "wss://relay.example", deviceToken: "short-lived-two",
    account: "acct", deviceID: "other-viewer" }).selectionTransportIdentity !==
    cloudAfterRotation.selectionTransportIdentity,
"the real Cloud client identity still distinguishes a viewer replacement");

const mirror = {};
lifecycle.bindLegacyMirror(mirror);
lifecycle.open(a, [a]);
assert.deepEqual({ openId: mirror.openId, selectedId: mirror.selectedId,
    replyComposerIdentity: mirror.replyComposerIdentity }, {
    openId: "terminal-1", selectedId: "terminal-1", replyComposerIdentity: identity.key
}); checks += 1;
lifecycle.close();
equal(mirror.openId, null, "legacy state is a projection of the owner, not a second selection owner");

// The list reconciler asks `byId(open)` whether the session on screen is still in the inventory,
// and that lookup re-keys whatever it is handed. An identity that does not re-key to itself reads
// as "this session is gone" on every accepted frame, which closes the detail under the reader a
// second or two after they opened it. Exercise the real consumer, not a spelling of it.
const { byId } = await import("../Resources/web/app/js/view/derive.js");
const { S } = await import("../Resources/web/app/js/core/state.js");
const liveRow = row("%654", "this-mac", "0bddd3cd-ee32-4bf7-bde3-fe8e850f1b44");
const liveSessions = S.sessions;
S.sessions = [liveRow];
const liveLifecycle = createSessionSelectionLifecycle();
const liveIdentity = liveLifecycle.open(liveRow, [liveRow]).identity;
equal(sessionSelectionKey(liveIdentity), liveIdentity.key,
    "a minted identity re-keys to itself instead of dropping its conversation");
equal(byId(liveIdentity), liveRow,
    "the open selection still resolves to its row in an unchanged inventory");
equal(byId(liveLifecycle.snapshot().open), liveRow,
    "the snapshot the list reconciler reads resolves to the row it came from");
equal(liveLifecycle.reconcile([liveRow]).openRemoved, false,
    "an unchanged inventory frame keeps the open detail");
S.sessions = liveSessions;

const selectionSource = await readFile(
    new URL("../Resources/web/app/js/session/selection.js", import.meta.url), "utf8");

// Verification invokes this mode once and expects it to fail: deleting the newest-in-lane guard
// must make the overlapping-response assertion below red.
if (process.argv.includes("--mutate-lane-guard")) {
    const mutatedSource = selectionSource.replace(
        "return options.latest === false || latestByLane.get(token.laneKey) === token.serial;",
        "return true;");
    assert.notEqual(mutatedSource, selectionSource, "mutation target remains present");
    const mutated = await import("data:text/javascript;base64," +
        Buffer.from(mutatedSource).toString("base64"));
    const subject = mutated.createSessionSelectionLifecycle();
    subject.open(a, [a]);
    const old = subject.beginEffect("transcript");
    subject.beginEffect("transcript");
    assert.equal(subject.effectIsCurrent(old), false,
        "an older overlapping response must remain stale");
}
if (process.argv.includes("--mutate-conversation-continuity")) {
    const mutatedSource = selectionSource.replace(
        "(!earlier.conversation && !!later.conversation);", "false;");
    assert.notEqual(mutatedSource, selectionSource, "continuity mutation target remains present");
    const mutated = await import("data:text/javascript;base64," +
        Buffer.from(mutatedSource).toString("base64"));
    const subject = mutated.createSessionSelectionLifecycle();
    subject.open(unknownConversation, [unknownConversation]);
    const result = subject.reconcile([knownConversation]);
    assert.equal(result.openRemoved, false,
        "missing-to-present conversation identity must remain continuity");
}
if (process.argv.includes("--mutate-nonopen-effect")) {
    const mutatedSource = selectionSource.replace(
        "if (token.requiresOpen) {", "if (true) {");
    assert.notEqual(mutatedSource, selectionSource, "non-open mutation target remains present");
    const mutated = await import("data:text/javascript;base64," +
        Buffer.from(mutatedSource).toString("base64"));
    const subject = mutated.createSessionSelectionLifecycle();
    subject.open(a, [a, b]);
    const token = subject.beginEffect("prompt", {
        identity: mutated.sessionSelectionIdentity(b), requiresOpen: false
    });
    assert.equal(subject.effectIsCurrent(token), true,
        "a row action does not need to become the open detail to report its settlement");
}
if (process.argv.includes("--mutate-minted-identity")) {
    const mutatedSource = selectionSource.replace(
        "    var minted = mintedSelectionIdentity(row);\n    if (minted) return minted;",
        "    var minted = null;\n    if (minted) return minted;");
    assert.notEqual(mutatedSource, selectionSource, "minted identity mutation target remains present");
    const mutated = await import("data:text/javascript;base64," +
        Buffer.from(mutatedSource).toString("base64"));
    const subject = mutated.createSessionSelectionLifecycle();
    const open = subject.open(liveRow, [liveRow]).identity;
    assert.equal(mutated.sessionSelectionKey(open), open.key,
        "an identity handed back to the keying function must still name its own row");
}
if (process.argv.includes("--mutate-machine-evidence")) {
    const mutatedSource = selectionSource.replace(
        'platform === "linux" && provider === "aws" ? "linux-aws"',
        'platform === "linux" && provider === "aws" ? "linux"');
    assert.notEqual(mutatedSource, selectionSource, "machine evidence mutation target remains present");
    const mutated = await import("data:text/javascript;base64," +
        Buffer.from(mutatedSource).toString("base64"));
    assert.equal(mutated.machinePresentation({ machine: "worker", machineName: "Builder",
        machinePlatform: "linux", cloudProvider: "aws" }, EN_MACHINE_COPY).label,
        "Linux / AWS · Builder",
        "explicit AWS evidence must remain visible");
}
for (const forbidden of ["../view/", "../input/", "../net/api", "../core/state"]) {
    check(!selectionSource.includes(forbidden), "selection owner stays independent from " + forbidden);
}

const openSource = await readFile(
    new URL("../Resources/web/app/js/session/open.js", import.meta.url), "utf8");
check(openSource.includes("api.transcript(entry.identity.route"),
    "transcript requests carry the frozen machine/session route");
check(openSource.includes('SessionSelection.beginEffect("transcript"'),
    "transcript reads use the shared effect lifetime");

const listSource = await readFile(
    new URL("../Resources/web/app/js/view/list.js", import.meta.url), "utf8");
check(listSource.includes('<span class="machine"></span>'),
    "every Session row reserves a visible machine identity slot");
check(listSource.includes("setText(machineNode, machine.label)"),
    "the row fills that slot from the evidence-aware machine presentation");

const composerSource = await readFile(
    new URL("../Resources/web/app/js/input/composer.js", import.meta.url), "utf8");
check(composerSource.includes("api.send(selected.route, text, pictures)"),
    "composer sends target the captured route rather than display id");
check(composerSource.includes("SessionSelection.effectIsCurrent(effect)"),
    "composer settlement is fenced before painting");
equal(clampSkillPickerIndex(8, 2), 1,
    "a narrowed skill result list clamps the prior keyboard index to its last row");
equal(selectedSkill([{ name: "one" }, { name: "two" }], 8).name, "two",
    "accepting after a narrowed query chooses a complete command instead of submitting the partial query");
check(/selected = clampSkillPickerIndex\(selected, matches\.length\)/.test(composerSource),
    "SkillPicker clamps its numeric index after every filtered result change");
check(/aria-selected", i === selected \? "true" : "false"/.test(composerSource),
    "SkillPicker exposes the same clamped index through aria-selected");
/* ---- the box empties at Send and nothing ever fills it again ------------- */

// The rule, decided 2026-09-16: a send that fails in the browser may already have been
// delivered — a Cloud round trip times out at this end after the Mac answered `200` — so the
// composer is emptied before the request exists and no settlement path writes into it. What
// this replaced held the text until a byte-identical payload came back, and left a delivered
// message in the box whenever anything had touched it in flight.
//
// `submit` is run here rather than read, because "it is not restored" is a statement about what
// happens after a promise settles and no arrangement of the source text says it. The two whole
// functions are lifted out and given fakes for everything they reach for.
const clearSource = composerSource.match(/\nfunction clearComposer\(\) \{[\s\S]*?\n\}/)?.[0] || "";
const submitSource = composerSource.match(/\nfunction submit\(\) \{[\s\S]*?\n\}/)?.[0];
check(!!submitSource, "the composer's submit is readable as one whole function");
const composerDependencies = ["sending", "els", "document", "blankness", "caretToEnd", "Shots",
    "msgText", "callSessionUI", "SkillPicker", "SessionSelection", "S", "closingKey", "byId",
    "atBottom", "optimisticSendSnapshot", "renderComposer", "renderDetailHead", "renderTranscript",
    "api", "Diagnostics", "observeBoardWorkflowSend", "toast", "toastFailure", "T", "closeDetail",
    "Optimistic", "authoritativeSendTime", "followPendingTranscript", "toBottom", "loadTranscript"];
const composerHarness = new Function("deps",
    "var {" + composerDependencies.join(", ") + "} = deps;\n" +
    clearSource + "\n" + submitSource + "\nreturn submit;");

async function pressSend(draft, attachments, outcome) {
    const box = { textContent: draft, classList: { toggle() {} } };
    const seen = { requests: 0, boxAtRequest: null, attachmentsAtRequest: null,
        sentText: null, sentPictures: null, failures: 0 };
    let held = attachments.slice();
    const open = { key: "mac-a|terminal-1", rowId: "terminal-1",
        route: { machine: "mac-a", session: "terminal-1" } };
    composerHarness({
        sending: false,
        els: { msg: box },
        document: { activeElement: box },
        blankness() {},
        caretToEnd() {},
        Shots: { busy: () => false, urls: () => held, clear() { held = []; } },
        msgText: () => String(box.textContent).trim(),
        callSessionUI: () => false,
        SkillPicker: { accept: () => false },
        SessionSelection: { snapshot: () => ({ open }), beginEffect: () => ({}),
            effectIsCurrent: () => true, matches: () => true, finishEffect() {} },
        S: { agent: null, write: true, tx: { id: "terminal-1", entries: [] } },
        closingKey: null,
        byId: () => ({ assistant: "claude" }),
        atBottom: () => false,
        optimisticSendSnapshot: () => ({ known: [], startedAt: 0 }),
        renderComposer() {}, renderDetailHead() {}, renderTranscript() {},
        api: {
            send(route, text, pictures) {
                seen.requests += 1;
                seen.boxAtRequest = box.textContent;
                seen.attachmentsAtRequest = held.slice();
                seen.sentText = text;
                seen.sentPictures = pictures;
                return outcome === "delivered" ? Promise.resolve({})
                    : Promise.reject({ code: "cloud_read_timeout" });
            },
            end: () => Promise.resolve({})
        },
        Diagnostics: { note() {} },
        observeBoardWorkflowSend() {},
        toast() {},
        toastFailure() { seen.failures += 1; },
        T: { sendFailed: "never rendered here" },
        closeDetail() {},
        Optimistic: { add() {} },
        authoritativeSendTime: () => 0,
        followPendingTranscript() {},
        toBottom() {},
        loadTranscript() {}
    })();
    // Three `.then` stages settle inside one macrotask, so this is the whole send.
    await new Promise((done) => setTimeout(done, 0));
    return { box, attachments: () => held, seen };
}

const delivered = await pressSend("a message", ["picture-one"], "delivered");
equal(delivered.seen.requests, 1, "one press makes exactly one request");
equal(delivered.seen.boxAtRequest, "", "the box is already empty when the request is made");
equal(delivered.seen.attachmentsAtRequest.length, 0, "and so are the attachments");
equal(delivered.seen.sentText, "a message", "the request still carries the words that were in it");
equal(delivered.seen.sentPictures.length, 1, "and the picture that was attached to them");
equal(delivered.box.textContent, "", "a delivered send writes nothing back into the box");
equal(delivered.attachments().length, 0, "and puts no picture back either");

const failed = await pressSend("a message", ["picture-one"], "failed");
equal(failed.seen.boxAtRequest, "", "a send that will fail empties the box before it is made");
equal(failed.box.textContent, "",
    "a browser-side failure may still have been delivered, so the draft is never restored");
equal(failed.attachments().length, 0, "and neither are its attachments");
equal(failed.seen.failures, 1, "the failure is still reported to the person");

const composerSettlement = composerSource.slice(composerSource.indexOf("var request = quit"));
check(!/els\.msg|textContent|Shots\.clear\(|clearComposer\(/.test(composerSettlement),
    "no delivery or failure path touches the composer once the request exists");
check(!composerSource.includes('addEventListener("beforeinput"'),
    "an empty box is not frozen in flight — what is typed then is the next message");

if (process.argv.includes("--mutate-skill-clamp")) {
    const skillStateSource = await readFile(
        new URL("../Resources/web/app/js/input/skill-picker-state.js", import.meta.url), "utf8");
    const mutatedSource = skillStateSource.replace(
        "return Math.max(0, Math.min(value, last));", "return Math.max(0, value);");
    assert.notEqual(mutatedSource, skillStateSource, "skill clamp mutation target remains present");
    const mutated = await import("data:text/javascript;base64," +
        Buffer.from(mutatedSource).toString("base64"));
    assert.equal(mutated.selectedSkill([{ name: "one" }, { name: "two" }], 8).name, "two",
        "a narrowed query still selects a complete visible command");
}

const mockSource = await readFile(
    new URL("../Resources/web/app/js/net/mock.js", import.meta.url), "utf8");
check(mockSource.includes("function mockSessionID(value)"),
    "the mock facade accepts the same route object as local and Cloud transports");

const exactRouteSources = [
    ["../Resources/web/app/js/view/terminal.js", /api\.screen\(selected\.route\)/],
    ["../Resources/web/app/js/input/git-panel.js", /api\.git\(selected\.route\)/],
    ["../Resources/web/app/js/input/shell-panel.js", /api\.killShell\(sid\.route, id\)/],
    ["../Resources/web/app/js/input/snippets.js", /api\.snippets\(selected\.route/],
    ["../Resources/web/app/js/input/user-messages.js", /Optimistic\.entries\(sessionIdentity\.key\)/],
    ["../Resources/web/app/js/input/keys.js", /openSession\(selected\)/]
];
for (const [relative, pattern] of exactRouteSources) {
    const source = await readFile(new URL(relative, import.meta.url), "utf8");
    check(pattern.test(source), relative + " consumes the exact selection owner");
}

const detailActionsSource = await readFile(
    new URL("../Resources/web/app/js/input/detail-actions.js", import.meta.url), "utf8");
check(/beginEffect\("prompt", \{\s*identity: selected\.identity, requiresOpen: false/.test(
    detailActionsSource), "a non-open prompt has an inventory-scoped settlement lifetime");
check(/beginEffect\("end", \{\s*identity: selected\.identity, requiresOpen: false, allowMissing: true/.test(
    detailActionsSource), "End may settle when disappearance is the action's success evidence");
const handlersSource = await readFile(
    new URL("../Resources/web/app/js/net/handlers.js", import.meta.url), "utf8");
check(/!selectionChange\.openReplacement[\s\S]*SessionActions\.gone\(previousOpen\.rowId\);\s*\n\s*\} else closeDetail\(true\);/.test(
    handlersSource),
"inventory disappearance settles the active End wait instead of closing its detail underneath it");

console.log(`web session selection: ${checks} checks passed`);
