/* --------------------------------------------------------------------------
   The entry point, and the list of what this page is made of.
   Every module is named below, in the order it was written in when all of this
   was one file, whether or not this file needs a name out of it. Most of them
   are reached anyway through somebody else's import; they are all listed because
   a module nobody imports is a module that never runs, and half of these exist
   entirely for what they do on the way in — a listener bound, a clock started.
   The list is the manifest, and it is the one place the whole page is written down.
   -------------------------------------------------------------------------- */
import { MOCK, params } from "./core/env.js";
import "./core/esc.js";
import { applyStrings } from "./core/i18n.js";
import { S } from "./core/state.js";
import { els } from "./core/dom.js";
import { Pages } from "./core/pages.js";
import { Diagnostics } from "./core/layout-diagnostics.js";
import { clockOf, tint, toastFailure } from "./core/util.js";
import { drawIcon } from "./core/pixels.js";
import { api, useApi } from "./net/api.js";
import { Build } from "./net/build.js";
import "./net/fetch.js";
import { Schedules } from "./net/schedules.js";
import { Live } from "./net/live.js";
import { Mock, cloudStatusFixture } from "./net/mock.js";
import {
    CloudViewerSession, chooseTransport, cloudStringsURL, idleClient, keepConnected,
    readCloudConfig
} from "./net/cloud-boot.js";
import { handlers } from "./net/handlers.js";
import { envelopeMetadata, errorFields } from "./net/cloud-viewer-events.js";
import { createBillingClient } from "./net/billing.js";
import { ScheduleWebhookClient } from "./net/schedule-webhooks.js";
import {
    captureCloudPairingInvitation, clearCloudPairingInvitation, cloudSessionAccessProblem,
    showCloudInstallGate,
    hideCloudGate, deferCloudGate, showCloudGate, showCloudBootError, showCloudDeviceRecovery,
    showCloudPairing, showCloudSignIn, showCloudAlreadyPaired, showCloudSessionAccessProblem
} from "./input/cloud-pairing.js";
import { CloudStatus } from "./input/cloud-status.js";
import { cloudOnboardingMode, cloudViewerDeviceMetadata } from "./net/cloud-onboarding.js";
import "./door/door.js";
import "./view/derive.js";
import { closingKey, render, renderConn, renderList, rowNodes } from "./view/list.js";
import { renderDetailHead, renderTranscript } from "./view/transcript.js";
import { renderAgents, renderComposer, renderWaiting } from "./view/composer.js";
import { Terminal } from "./view/terminal.js";
import { bindProjectsPage, localProjectPlaces, readProjectPlaces } from "./view/projects.js";
import { LOCAL_MACHINE } from "./net/client.js";
import { bindLedgerPage } from "./view/ledger.js";
import { bindBoardPage, enterProjectBoard } from "./view/board.js";
import { bindSessionBoard, SessionBoard } from "./input/session-board.js";
import { bindBoardSession, BoardSession } from "./input/board-session.js";
import { bindBoardAssignment } from "./input/board-assignment.js";
import { BoardControls } from "./input/board-settings.js";
import { bindUsagePortfolio } from "./view/usage.js";
import { bindPlanPage } from "./view/plan.js";
import { bindDocumentsPage } from "./view/documents.js";
import { bindDevicesPage } from "./view/devices.js";
import {
    CANONICAL_DOCUMENT_ORIGIN, documentIdentityForSession, documentLocatorFromHash
} from "./net/document-links.js";
import "./view/markdown.js";
import { paintStatic } from "./view/static.js";
import { Waits } from "./view/waits.js";
import {
    closeDetail, loadTranscript, observeTranscriptFileRevision, observeTranscriptRevision,
    openSession, rearmTranscriptRevision
} from "./session/open.js";
import { agentRow, agentsRev, closeAgent, loadAgent, renderAgentHead, agentTokens } from "./session/agent.js";
import { SessionSelection } from "./session/selection.js";
import { bindSessionUI } from "./session/ui.js";
import { createTranscriptEventRouter } from "./session/transcript-requests.js";
import { toggleOrder } from "./input/keys.js";
import { SwipeRows } from "./input/swipe.js";
import { SessionActions } from "./input/detail-actions.js";
import { CoordinatorControls } from "./input/coordinator-actions.js";
import "./input/user-messages.js";
import { Snippets } from "./input/snippets.js";
import { GitPanel } from "./input/git-panel.js";
import { ShellPanel } from "./input/shell-panel.js";
import { ActionConfirm } from "./input/action-confirm.js";
import {
    bindBoardRoute, bindDocumentRoute, bindSessionLocatorRoute, openWanted, routeTo,
    setWantedSession, wantedSession
} from "./input/route.js";
import { markSidebarPage } from "./input/sidebar.js";
import { Settings } from "./input/settings.js";
import { Start } from "./input/start.js";
import "./input/command.js";
import "./input/schedule.js";
import { ScheduleHistory } from "./input/schedule-history.js";
import { StatusLine } from "./input/status-line.js";
import { Info } from "./input/info.js";
import { Push } from "./input/push.js";
import { Shots } from "./input/shots.js";
import { Voice } from "./input/voice.js";
import { fillSuggestedReply, msgText, sending, SkillPicker } from "./input/composer.js";
import "./input/edges.js";

// Compatibility fields in `S` are projections for older controllers. The exact identity and
// every asynchronous effect epoch live in SessionSelection.
SessionSelection.bindLegacyMirror(S);

// The entry point is the sole composition root. Controllers and renderers depend only on the
// callback surface below, so their imports stay one-way while every existing synchronous UI
// behavior and public facade remains intact.
bindSessionUI({
    render: render,
    renderList: renderList,
    renderTranscript: renderTranscript,
    renderDetailHead: renderDetailHead,
    renderAgentHead: renderAgentHead,
    renderComposer: renderComposer,
    renderWaiting: renderWaiting,
    renderAgents: renderAgents,
    closeDetail: closeDetail,
    closeAgent: closeAgent,
    openSession: openSession,
    observeTranscriptRevision: observeTranscriptRevision,
    rearmTranscriptRevision: rearmTranscriptRevision,
    agentRow: agentRow,
    agentsRev: agentsRev,
    loadAgent: loadAgent,
    agentTokens: agentTokens,
    rowNode: function (key) { return rowNodes[key] || null; },
    closingSelectionKey: function () { return closingKey; },
    closeSessionActions: function () { return SessionActions.close.apply(SessionActions, arguments); },
    sessionActionsOpener: function () { return SessionActions.opener; },
    sessionActionsLevel: function () { return SessionActions.level.apply(SessionActions, arguments); },
    sessionActionsOnGit: function () { return SessionActions.onGit(); },
    sessionActionItems: function () { return SessionActions.items(); },
    endSession: function () { return SessionActions.end.apply(SessionActions, arguments); },
    promptSession: function () { return SessionActions.prompt.apply(SessionActions, arguments); },
    focusMac: function () { return SessionActions.focusMac(); },
    closeActionConfirm: function () { return ActionConfirm.close.apply(ActionConfirm, arguments); },
    syncActionConfirm: function () { return ActionConfirm.sync(); },
    openGitPanel: function () { return GitPanel.open.apply(GitPanel, arguments); },
    refreshGitPanel: function () { return GitPanel.refresh.apply(GitPanel, arguments); },
    closeGitPanel: function () { return GitPanel.close.apply(GitPanel, arguments); },
    followGitPanel: function () { return GitPanel.follow.apply(GitPanel, arguments); },
    closeShellPanel: function () { return ShellPanel.close.apply(ShellPanel, arguments); },
    followShellPanel: function () { return ShellPanel.follow.apply(ShellPanel, arguments); },
    openTerminal: function () { return Terminal.open.apply(Terminal, arguments); },
    closeTerminal: function () { return Terminal.close.apply(Terminal, arguments); },
    followTerminal: function () { return Terminal.follow.apply(Terminal, arguments); },
    openInfo: function () { return Info.open.apply(Info, arguments); },
    followInfo: function () { return Info.follow.apply(Info, arguments); },
    followSnippets: function () { return Snippets.follow.apply(Snippets, arguments); },
    followStatusLine: function () { return StatusLine.follow.apply(StatusLine, arguments); },
    deferStatusLine: function () { return StatusLine.defer.apply(StatusLine, arguments); },
    resumeStatusLine: function () { return StatusLine.resume.apply(StatusLine, arguments); },
    followSessionBoard: function () { return SessionBoard.follow.apply(SessionBoard, arguments); },
    syncSessionBoard: function () { return SessionBoard.sync.apply(SessionBoard, arguments); },
    resumeSessionBoard: function () { return SessionBoard.resume.apply(SessionBoard, arguments); },
    observeBoardSession: function () { return BoardSession.observe.apply(BoardSession, arguments); },
    clearShots: function () { return Shots.clear(); },
    shotsBusy: function () { return Shots.busy(); },
    shotsCount: function () { return Shots.count(); },
    voiceBusy: function () { return Voice.busy(); },
    voiceLive: function () { return Voice.live(); },
    composerSending: function () { return sending; },
    messageText: msgText,
    fillSuggestedReply: fillSuggestedReply,
    skillPickerChanged: function () { return SkillPicker.changed.apply(SkillPicker, arguments); },
    skillPickerClose: function () { return SkillPicker.close.apply(SkillPicker, arguments); },
    resetSwipeRows: function () { return SwipeRows.reset.apply(SwipeRows, arguments); },
    wantedSession: function () { return wantedSession; },
    setWantedSession: setWantedSession,
    openWanted: openWanted,
    startPlaceholder: function () { return Start.placeholder.apply(Start, arguments); },
    arrangeStartRows: function () { return Start.arrange.apply(Start, arguments); },
    startArriving: function () { return Start.arriving.apply(Start, arguments); },
    checkStart: function () { return Start.check.apply(Start, arguments); },
    syncStart: function () { return Start.sync.apply(Start, arguments); },
    redrawPush: function () { return Push.redraw.apply(Push, arguments); },
    togglePush: function () { return Push.toggle.apply(Push, arguments); },
    toggleOrder: toggleOrder,
    sessionGone: function () { return SessionActions.gone.apply(SessionActions, arguments); }
});

/* ==========================================================================
   10. Go
   ========================================================================== */

// The one thing that has to happen before anything on this page can call the API: which of the
// three it is. `net/api.js` holds the name and knows about none of them — see the note there,
// and `net/cloud-boot.js` for why the third is decided by a build declaration rather than by
// looking at the hostname.
function bindTranscriptEvents(transport) {
    if (transport && typeof transport.events === "function") {
        transport.events(createTranscriptEventRouter(
            function () { return SessionSelection.snapshot().open; },
            observeTranscriptFileRevision,
            function (id) { loadTranscript(id, true); }
        ));
    }
}

/* The Plan page's two seams, decided by the transport and nowhere else.
   `null` is the local and mock answer and it is not a failure: this page served by the Mac has
   no Cloud account to charge, and `view/plan.js` says exactly that rather than drawing a broken
   upgrade button. See the `not_here` state there. */
var planBilling = null;
var planSignIn = function () { return ""; };
var scheduleWebhookManagement = null;

/* Lemon Squeezy hands a finished checkout back to `${WEB_APP_URL}/billing/done` — the success
   URL `api/src/services/billing.ts` sends with every session. Cloudflare Pages serves this same
   document for that path, so by the time this line runs the console is already booting.

   The path is read once and then written out of the address. A reload of `/billing/done` would
   otherwise start a second wait for a webhook that arrived twenty minutes ago. */
var returningFromCheckout = location.pathname === "/billing/done";

/* Whether the cloud door is still the answer, even while the Plan page sits in front of it.
   Two of the door's screens block a browser that is already signed in — no account key yet, and
   the viewer-device limit — and neither has anything to do with being able to pay. So the Plan
   page may be opened over the top of them, and this is what says the door has to come back when
   it is closed again. */
var cloudGateUp = false;

/* The encryption/permission door as rows the paired Mac receives (`net/cloud-viewer-events.js`):
   which receive failure raised it, what lowered it, how long it stood and how many more failures
   arrived meanwhile. Observation only — nothing below decides whether the door shows. A raise
   while it is already up is counted rather than recorded, so one realign cannot fill the log. */
var cloudDoorRaised = null;
function noteCloudDoor(client, change, detail) {
    try {
        var log = client && client.viewerEvents;
        if (!log || typeof log.record !== "function") return;
        var now = log.now();
        if (change === "raised") {
            if (cloudDoorRaised) { cloudDoorRaised.failures += 1; return; }
            var thrown = errorFields(detail.error);
            var cause = detail.error && detail.error.viewerEvent;
            var row = log.record("cloud.door.raised", {
                kind: detail.kind, code: thrown.code, error_name: thrown.name,
                error_class: thrown.class, error_message: thrown.message,
                cause_n: cause ? cause.n : null,
                cause_rate_limited: cause ? cause.rateLimited === true : null,
                cause_key: cause ? cause.key : null,
                invitation: detail.invitation === true
            }, "cloud.door.raised|" + detail.kind + "|" + thrown.code);
            cloudDoorRaised = { n: row.n, at_ms: now, kind: detail.kind, failures: 0 };
            return;
        }
        if (!cloudDoorRaised) return;
        var raised = cloudDoorRaised;
        cloudDoorRaised = null;
        var event = detail.event || {};
        var meta = event.envelope ? envelopeMetadata(event.envelope, event.realign) : {};
        log.record("cloud.door.hidden", {
            by: detail.by, kind: raised.kind, raised_n: raised.n,
            ms_raised: Math.max(0, now - raised.at_ms), failures_while_raised: raised.failures,
            channel_kind: meta.channel_kind || null, machine: meta.machine || null,
            sender: meta.sender || null, seq: meta.seq === undefined ? null : meta.seq,
            realign: event.realign === true, authoritative: event.authoritative === true,
            self_healed: event.selfHealed === true
        }, "cloud.door.hidden|" + detail.by);
    } catch (e) { /* observing the door must never move it */ }
}

var cloudConfig = null;
try {
    cloudConfig = readCloudConfig(window);
} catch (cloudConfigError) {
    // A build that declares a cloud console badly must not quietly become a local one.
    console.error("clawdline: " + cloudConfigError.message);
}
var transportKind = chooseTransport({
    mock: MOCK, origin: location.origin, config: cloudConfig
});

if (transportKind === "cloud") {
    // The seam is filled before anything can call it, and filled again — with the same live
    // binding — once the relay handshake has actually completed.
    useApi(idleClient());
    handlers.conn("connecting");
    // Every failure line on this page opens the Cloud status sheet at its `ref` from here on, and
    // the sheet's key-drift row leads to the same encryption repair the Cloud door already offers.
    CloudStatus.bindFailureLines();
    CloudStatus.onRepair(function () {
        // The door a typed key error raises, reached through that same event rather than a copy
        // of its handler below: the connected client's listener decides it is `encryption`.
        if (api && typeof api._emit === "function") {
            api._emit({ type: "error", error: Object.assign(new Error("key id drift"),
                { code: "unreadable_envelope" }) });
        }
    });
    var cloudOnboarding = cloudOnboardingMode(window);
    if (cloudOnboarding === "install") {
        // Do not call ensureSession here. A Safari viewer would consume a device slot and its
        // non-extractable key cannot cross into the Home Screen app's isolated IndexedDB.
        // A QR opened in Safari is intentionally discarded too: keeping its secret in the
        // address bar would invite a flow this storage container is not allowed to finish.
        try { captureCloudPairingInvitation(window, Date.now()); } catch (discardedInvitation) {
            console.error("clawdline: " + discardedInvitation.message);
        }
        clearCloudPairingInvitation(window.sessionStorage);
        handlers.conn("locked");
        showCloudInstallGate();
    } else {
        var cloudDevice = cloudViewerDeviceMetadata(window);
        var cloudSession = new CloudViewerSession({
            config: cloudConfig,
            handlers: handlers,
            deviceKind: cloudDevice.kind,
            deviceName: cloudDevice.name
        });
        planBilling = createBillingClient({ apiOrigin: cloudConfig.apiOrigin });
        scheduleWebhookManagement = new ScheduleWebhookClient({ origin: cloudConfig.apiOrigin });
        planSignIn = function () { return cloudSession.signInURL(); };
        var cloudInvitation = null;
        try {
            cloudInvitation = captureCloudPairingInvitation(window, Date.now());
        } catch (invitationError) {
            console.error("clawdline: " + invitationError.message);
        }
        var cloudConnection = null;
        var startCloudViewer = function () {
            cloudConnection = keepConnected(cloudSession, {
                onState: function (update) {
                    if (update.state === "connected") {
                        // Relay readiness proves the socket and viewer identity only. Do not call
                        // it live until a decryptable Session inventory — even an authoritative
                        // empty one — arrives. A typed access/key error raises the pairing door
                        // instead of rendering an ordinary "no sessions" answer.
                        if (update.client && typeof update.client.events === "function") {
                            update.client.events(function (event) {
                                if (event && event.type === "sessions") {
                                    handlers.conn("live");
                                    if (!cloudInvitation) {
                                        noteCloudDoor(update.client, "hidden",
                                            { by: "sessions", event: event });
                                        cloudGateUp = false;
                                        hideCloudGate();
                                    }
                                    return;
                                }
                                var accessProblem = event && event.type === "error"
                                    ? cloudSessionAccessProblem(event.error) : null;
                                if (!accessProblem) return;
                                noteCloudDoor(update.client, "raised", { kind: accessProblem,
                                    error: event.error, invitation: !!cloudInvitation });
                                S.locked = true;
                                handlers.conn("locked");
                                cloudGateUp = true;
                                if (!cloudInvitation) showCloudSessionAccessProblem(accessProblem);
                            });
                        }
                        handlers.conn(S.arrived ? "live" : "connecting");
                        cloudGateUp = !!cloudInvitation;
                        if (cloudInvitation) {
                            // A viewer signing identity may remain authorized after its local
                            // account key drifts. Keep the invitation until an explicit repair
                            // replaces those E2E keys; the Mac accepts only this same signed
                            // device, so this neither creates a device nor expands authority.
                            showCloudAlreadyPaired({ onRepair: function () {
                                var repairInvitation = cloudInvitation;
                                if (!repairInvitation) return;
                                if (cloudConnection) cloudConnection.stop();
                                handlers.conn("locked");
                                showCloudPairing(cloudSession, {
                                    invitation: repairInvitation,
                                    onPaired: function () {
                                        clearCloudPairingInvitation(window.sessionStorage);
                                        cloudInvitation = null;
                                    }
                                }).then(startCloudViewer);
                            } });
                        } else {
                            noteCloudDoor(update.client, "hidden", { by: "connected" });
                            cloudGateUp = false;
                            hideCloudGate();
                        }
                        useApi(update.client);
                        bindTranscriptEvents(update.client);
                    } else if (update.state === "sign_in") {
                        handlers.conn("locked");
                        showCloudSignIn(update.url);
                    } else if (update.state === "device_limit_reached") {
                        handlers.conn("locked");
                        cloudGateUp = true;
                        showCloudDeviceRecovery(cloudSession, update, {
                            onRecovered: startCloudViewer
                        }).catch(function () { /* the recovery screen owns its visible error */ });
                    } else if (update.state === "pairing_required") {
                        cloudGateUp = true;
                        // Signed in, but this app holds no account key yet. The installed PWA
                        // scans the Mac's QR itself so the key is born in the storage that keeps it.
                        handlers.conn("locked");
                        showCloudPairing(cloudSession, {
                            invitation: cloudInvitation,
                            scan: cloudOnboarding === "pwa" && !cloudInvitation,
                            onPaired: function () {
                                clearCloudPairingInvitation(window.sessionStorage);
                                cloudInvitation = null;
                            }
                        }).then(startCloudViewer);
                    } else if (update.state === "retrying") {
                        handlers.conn("retrying", Math.max(1, Math.ceil(update.afterMs / 1000)));
                        showCloudBootError(update);
                    } else if (update.state === "terminal_error") {
                        handlers.conn("locked");
                        showCloudBootError(update, { onRetry: startCloudViewer });
                    } else if (update.state === "revoked") {
                        handlers.conn("locked");
                        showCloudBootError({
                            state: "terminal_error",
                            error: update.error || new Error("This viewer device has been revoked. Sign in again to continue.")
                        }, {
                            label: "Sign in again",
                            onRetry: function () { location.assign(cloudSession.signInURL()); }
                        });
                    } else if (update.state === "reconnecting") {
                        handlers.conn("connecting");
                    }
                }
            });
        };
        startCloudViewer();
    }
} else if (transportKind === "blocked") {
    useApi(idleClient());
    handlers.conn("offline");
    console.error("clawdline: this console build is for " + cloudConfig.appOrigin
        + " and is being served from " + location.origin);
} else {
    useApi(transportKind === "mock" ? Mock : Live);
    bindTranscriptEvents(api);
}

if (scheduleWebhookManagement) {
    ScheduleHistory.bindWebhook({
        client: scheduleWebhookManagement,
        machine: function (scheduleID) { return api._scheduleMachine(scheduleID); },
        bind: async function (scheduleID, hookID, replaceHookID) {
            var requestID = crypto.randomUUID().toLowerCase();
            var machine = api._scheduleMachine(scheduleID);
            await api._publishCommand(machine, "schedule-webhook-bind-v1", {
                request_id: requestID, hook_id: hookID, schedule_id: scheduleID,
                replace_hook_id: replaceHookID
            }, "ctl");
            // The encrypted publish is transport acceptance, not activation. The management
            // read is the Cloud authority proving the Mac's machine-credential activate landed.
            for (var attempt = 0; attempt < 20; attempt += 1) {
                var answer = await scheduleWebhookManagement.read(hookID);
                var current = answer && (answer.hook || answer);
                if (current && current.state === "active") return current;
                await new Promise(function (resolve) { setTimeout(resolve, 250); });
            }
            throw new Error("webhook activation not observed");
        },
        copy: function (value) { return navigator.clipboard.writeText(value); }
    });
}

// A deterministic visual fixture for the same bundled page. It never runs outside mock mode,
// never opens a Cloud session, and lets mobile layout checks hold the install/scan screen still.
if (MOCK && params.get("cloud-onboarding") === "install") showCloudInstallGate();
// The Cloud failure line and status sheet, held still for a layout check: a toast that says a
// fixed refusal with its `code · ref`, and with `open` the sheet it opens. Mock mode only.
if (MOCK && /^(line|open)$/.test(params.get("cloud-status") || "")) {
    var cloudFixture = cloudStatusFixture();
    useApi(Object.assign(Object.create(Mock), cloudFixture.transport));
    CloudStatus.bindFailureLines();
    setTimeout(function () {
        toastFailure(cloudFixture.failure);
        if (params.get("cloud-status") === "open") CloudStatus.open({ ref: cloudFixture.failure.ref });
    }, 800);
}
if (MOCK && params.get("cloud-onboarding") === "scan") {
    showCloudPairing({}, { scan: true });
}
Diagnostics.bind({ state: S, elements: els, transport: function () { return api; } });

// Usage lives in the same stamped module graph as the rest of the page. Keeping its import here
// makes the preload URL and the runtime request one identity, while these literal lookups keep the
// DOM contract visible to the permanent repository guard.
var byId = function (id) { return document.getElementById(id); };
var devices = bindDevicesPage({
    "devices": byId("devices"), "devices-title": byId("devices-title"),
    "devices-lede": byId("devices-lede"), "devices-close": byId("devices-close"),
    "devices-status": byId("devices-status"), "devices-empty": byId("devices-empty"),
    "devices-rows": byId("devices-rows")
}, {
    machines: function () { return api.machines(); },
    start: function (machine) { Pages.go("sessions"); Start.open(machine); }
});
var documents = bindDocumentsPage({
    page: byId("documents-page"), title: byId("documents-title"),
    back: byId("documents-back"), listBack: byId("document-list-back"),
    status: byId("documents-status"), listView: byId("documents-list-view"),
    rows: byId("documents-rows"), viewer: byId("document-viewer"),
    documentTitle: byId("document-title"), meta: byId("document-meta"),
    body: byId("document-body"), share: byId("document-share"),
    copy: byId("document-copy"), menu: byId("session-documents")
}, {
    document: document,
    language: function () { return document.documentElement.lang || navigator.language || "en"; },
    list: function (identity) { return api.documents(identity); },
    read: function (locator) { return api.document(locator); },
    shareOrigin: function () {
        return transportKind === "cloud" ? CANONICAL_DOCUMENT_ORIGIN : null;
    },
    navigator: function () { return navigator; },
    // A list has no stable address of its own because it needs a Session identity. A selected
    // document writes its complete fragment through the share controls instead; entering either
    // form must not replace that locator with the lossy `#page=documents` spelling.
    navigate: function (name, options) {
        return Pages.go(name, name === "documents" ? { hash: false } : options);
    }
});
bindDocumentRoute(
    function (locator, error) { documents.openDirect(locator, error); },
    documentLocatorFromHash,
    function () { documents.hide(); }
);
byId("session-documents").addEventListener("click", function () {
    if (!S.openId) return;
    var identity;
    try { identity = documentIdentityForSession(S.sessions, S.openId, transportKind); }
    catch (error) {
        SessionActions.close();
        documents.openSessionError(error);
        return;
    }
    SessionActions.close();
    documents.openSession(identity);
});
// `usage-open` is not in this table any more. It is the drawer's Usage row now, and reaching the
// page is the drawer's business; the portfolio module stopped having an opinion about how somebody
// got to it. The id stays on that row — `usage.css` styles it and the Usage guard looks for it.
/* The Projects page. Its legacy directory/delivery join remains separate from the new bounded
   lifecycle snapshot. Cloud carries lifecycle read+refresh on its authenticated machine channel;
   no browser transport carries cleanup. Every capability is asked when the page is used because
   `api` is a live binding filled twice while Cloud starts. */
var projects = bindProjectsPage({
    "projects": byId("projects"),
    "projects-list-view": byId("projects-list-view"),
    "projects-detail-view": byId("projects-detail-view"),
    "projects-title": byId("projects-title"), "projects-count": byId("projects-count"),
    "projects-status": byId("projects-status"), "projects-rows": byId("projects-rows"),
    "projects-back": byId("projects-back"),
    "project-mark": byId("project-mark"), "project-name": byId("project-name"),
    "project-path": byId("project-path"), "project-status": byId("project-status"),
    "project-truncated": byId("project-truncated"),
    "project-delivered": byId("project-delivered"),
    "project-delivered-count": byId("project-delivered-count"),
    "project-delivered-title": byId("project-delivered-title"),
    "project-delivered-say": byId("project-delivered-say"),
    "project-delivered-list": byId("project-delivered-list"),
    "project-delivered-none": byId("project-delivered-none"),
    "project-none": byId("project-none"), "project-groups": byId("project-groups"),
    "project-excluded": byId("project-excluded"),
    "project-unattributed": byId("project-unattributed"),
    "project-unattributed-title": byId("project-unattributed-title"),
    "project-unattributed-say": byId("project-unattributed-say"),
    "project-read": byId("project-read"),
    "project-worktree-lifecycle": byId("project-worktree-lifecycle"),
    "project-worktree-status": byId("project-worktree-status"),
    "project-worktree-summary": byId("project-worktree-summary"),
    "project-worktree-rows": byId("project-worktree-rows"),
    "project-worktree-refresh": byId("project-worktree-refresh")
}, {
    carries: function () {
        return typeof api.board === "function" || typeof api.places === "function" && typeof api.projectWorktrees === "function";
    },
    places: async function () {
        return readProjectPlaces(api, function (board) { BoardControls.apply(board); });
    },
    openBoard: function (place) { BoardControls.open(place.boardProjectId, null, place); },
    projectWorktrees: function (place) { return api.projectWorktrees(place); },
    lifecycleAvailable: function () {
        return typeof api.projectWorktreeLifecycle === "function"
            && typeof api.projectWorktreeLifecycleRefresh === "function";
    },
    projectWorktreeLifecycle: function (place) {
        return api.projectWorktreeLifecycle(place.boardProjectId || place.id);
    },
    projectWorktreeLifecycleRefresh: function (place) {
        return api.projectWorktreeLifecycleRefresh(place.boardProjectId || place.id);
    },
    openWorktreeOwner: function (locator, place) {
        return boardSession.open(locator.session, {
            id: place.boardProjectId || place.id, displayPath: place.path
        }, locator.machine);
    },
    // The same seam the Feature table uses, and for the same reason: `view/projects.js` imports
    // nothing but the words, because `core/pixels.js` reaches `window` while it is being
    // evaluated and this module is exercised whole in Node by Tests/web-projects.mjs.
    drawIcon: drawIcon, tint: tint,
    navigate: function (name) { Pages.go(name); }
});

/* The verification ledger. One read, absent on the Cloud path for the same reason the worktree
   join is — its subject is a Feature, and every read a paired viewer may name carries a session —
   so it arrives as a thunk and a `carries` question asked when the page is used. */
var ledger = bindLedgerPage({
    "ledger": byId("ledger"),
    "ledger-list-view": byId("ledger-list-view"),
    "ledger-detail-view": byId("ledger-detail-view"),
    "ledger-title": byId("ledger-title"), "ledger-count": byId("ledger-count"),
    "ledger-status": byId("ledger-status"), "ledger-rows": byId("ledger-rows"),
    "ledger-unattributed": byId("ledger-unattributed"),
    "ledger-back": byId("ledger-back"),
    "ledger-detail-title": byId("ledger-detail-title"),
    "ledger-detail-status": byId("ledger-detail-status"),
    "ledger-detail-rows": byId("ledger-detail-rows")
}, {
    carries: function () { return typeof api.verificationLedger === "function"; },
    verificationLedger: function (graphID) { return api.verificationLedger(graphID); },
    navigate: function (name) { Pages.go(name); }
});

var boardElements = {};
["board", "board-title", "board-project-mark", "board-subtitle", "board-items", "board-detail", "board-status",
 "board-search", "board-back", "board-timeline-tab", "board-refresh"].forEach(function (id) {
    boardElements[id] = byId(id);
});
var boardSession = bindBoardSession(document, {
    sessions: function () { return S.sessions; }, canWrite: function () { return S.write; },
    inventoryReady: function () { return S.arrived; },
    project: async function (id, machine) {
        const answer = await api.board(id, null, null, machine);
        return answer?.board?.projects?.find(project => project.id === id) || null;
    },
    places: async function () {
        const answer = await api.places();
        return api === Live ? localProjectPlaces(answer, LOCAL_MACHINE) : answer;
    },
    history: function (place, assistant) { return api.pastSessions(place, assistant); },
    resume: function (place, conversation, assistant, requestId) { return api.resumePlace(place, conversation, assistant, requestId); },
    openLive: openSession,
    began: function (answer, place) {
        Pages.go(Pages.home()); Start.began(answer.id, place, false, answer.attach);
    }
});
bindSessionLocatorRoute(function (locator, error) {
    boardSession.openLocator(locator, error);
}, function () { boardSession.close(); });
var boardAssignment = bindBoardAssignment(document, {
    requireMachine: transportKind === "cloud",
    sessions: function () { return S.sessions; }, canWrite: function () { return S.write; },
    inventoryReady: function () { return S.arrived; },
    read: function (project, item, machine) { return api.board(project, item, null, machine); },
    command: function (body, machine) { return api.boardCommand(body, machine); },
    copy: function (text) { return navigator.clipboard.writeText(text); },
    openLive: openSession,
    changed: function () { board.refresh(); }
});
var board = bindBoardPage(boardElements, {
    read: function (project, item, machine) { return api.board(project, item, null, machine); },
    sessions: function () { return S.sessions; },
    copy: function (value) { return navigator.clipboard.writeText(value); },
    replaceURL: function (value) { history.replaceState(null, "", new URL(value).hash); },
    drawIcon: drawIcon, tint: tint,
    navigate: function (name) { Pages.go(name); },
    openSession: function (id, project, machine) { return boardSession.open(id, project, machine); },
    assignSession: function (target) { return boardAssignment.open(target); },
    onMode: function (snapshot) { BoardControls.apply(snapshot); }
});
BoardControls.escape = function () { return board.escape(); };
bindSessionBoard(byId("session-board"), {
    discoverMode: transportKind === "cloud",
    requireMachine: transportKind === "cloud",
    read: function (project, item, machine) { return api.board(project, item, null, machine); },
    visible: function () { return Pages.current() === Pages.home() && !!S.openId && !S.agent; },
    ready: function () { return !!S.openId && !S.tx.loading; },
    open: function (project, item, presentation, machine) { Info.close(); BoardControls.open(project, item, presentation, machine); }
});
BoardControls.onChange = function (enabled) {
    // Cloud mode is learned from the selected Session's machine-scoped response.
    // A global settings read (or another Mac's revision) is not its authority.
    if (transportKind !== "cloud") SessionBoard.setEnabled(enabled);
    else SessionBoard.revalidateMode();
};
BoardControls.open = function (project, item, presentation, machine) {
    if (!project) { Pages.go("projects"); return; }
    board.open(project, item, presentation, machine);
    Pages.go("board");
};
bindBoardRoute(function (locator, error) {
    board.openLocator(locator, error);
    Pages.go("board", { hash: false });
}, function () {});

var timelineController = null;
var timelineRequested = { project: null, presentation: null, machine: null };
var timelineElements = {};
["timeline", "timeline-back", "timeline-refresh", "timeline-board-tab", "timeline-title",
 "timeline-project-mark", "timeline-subtitle", "timeline-status", "timeline-environment",
 "timeline-category", "timeline-upcoming", "timeline-items", "timeline-detail",
 "timeline-environment-label", "timeline-category-label", "timeline-upcoming-label",
 "settings-timeline-title", "settings-timeline-say", "settings-timeline-toggle",
 "settings-timeline-history", "settings-timeline-status"].forEach(function (id) {
    timelineElements[id] = byId(id);
});
var timelineReady = import("./view/timeline.js").then(function (module) {
    timelineController = module.bindTimelinePage(timelineElements, {
        read: function (project, entry, cursor, environment, category, includeUpcoming) {
            return api.timeline(project, entry, cursor, environment, category, includeUpcoming,
                timelineRequested.machine || undefined);
        },
        command: function (body) { return api.timelineCommand(body, timelineRequested.machine || undefined); },
        openBoard: function (project, item) {
            BoardControls.open(project, item, null, timelineRequested.machine);
        },
        onMode: function (snapshot) { BoardControls.apply(snapshot); }
    });
    if (!timelineRequested.project) return timelineController;
    return Promise.resolve(timelineController.enter(
        timelineRequested.project, timelineRequested.presentation
    )).then(function () { return timelineController; });
}).catch(function (error) {
    var chinese = /^zh/i.test(document.documentElement.lang || "");
    timelineElements["timeline-status"].textContent =
        (chinese ? "此版本無法使用時間軸。" : "Timeline is unavailable in this build. ")
        + (error.message || String(error));
    return null;
});
var timeline = {
    enter: function (project, presentation, machine) {
        if (project) timelineRequested = { project: project, presentation: presentation || null,
            machine: machine || (presentation && presentation.machine) || null };
        return timelineReady.then(function (controller) {
            if (!controller) return;
            return project ? controller.enter(project, presentation) : controller.enter();
        });
    },
    leave: function () { if (timelineController) timelineController.leave(); },
    escape: function () { return timelineController ? timelineController.escape() : Pages.go("projects"); }
};
byId("board-timeline-tab").addEventListener("click", function () {
    if (!board.state.projectId) { Pages.go("projects"); return; }
    timeline.enter(board.state.projectId, board.state.projectPresentation, board.state.machine);
    Pages.go("timeline");
});
byId("settings-timeline-history").addEventListener("click", function () {
    if (!timelineController || !timelineController.state.projectId) Pages.go("projects");
});

var usage = bindUsagePortfolio({
    "usage-analytics": byId("usage-analytics"),
    "usage-close": byId("usage-close"), "usage-overview": byId("usage-overview"),
    "usage-agent-work": byId("usage-agent-work"), "usage-controls": byId("usage-controls"),
    "usage-range": byId("usage-range"), "usage-from": byId("usage-from"),
    "usage-to": byId("usage-to"), "usage-timezone": byId("usage-timezone"),
    "usage-refresh": byId("usage-refresh"), "usage-meta": byId("usage-meta"),
    "usage-availability": byId("usage-availability"), "usage-status": byId("usage-status"),
    "usage-overview-panel": byId("usage-overview-panel"),
    "usage-agent-work-panel": byId("usage-agent-work-panel"),
    "usage-measured": byId("usage-measured"), "usage-output-change": byId("usage-output-change"),
    "usage-run-count": byId("usage-run-count"), "usage-scheduled-output": byId("usage-scheduled-output"),
    "usage-scheduled-runs": byId("usage-scheduled-runs"), "usage-coverage-kpi": byId("usage-coverage-kpi"),
    "usage-unknown-count": byId("usage-unknown-count"), "usage-project-count": byId("usage-project-count"),
    "usage-project-list": byId("usage-project-list"), "usage-project-detail": byId("usage-project-detail"),
    "usage-project-detail-title": byId("usage-project-detail-title"),
    "usage-project-rank": byId("usage-project-rank"), "usage-project-summary": byId("usage-project-summary"),
    "usage-project-trend": byId("usage-project-trend"), "usage-project-mix": byId("usage-project-mix"),
    "usage-project-lineage": byId("usage-project-lineage"), "usage-project-recent": byId("usage-project-recent"),
    "usage-insights": byId("usage-insights"), "usage-schedule-body": byId("usage-schedule-body"),
    "usage-unknown-schedule": byId("usage-unknown-schedule"), "usage-feature-body": byId("usage-feature-body"),
    "usage-feature-summary": byId("usage-feature-summary"),
    "usage-feature-count": byId("usage-feature-count"), "usage-feature-fold": byId("usage-feature-fold"),
    "usage-unknown-feature": byId("usage-unknown-feature"),
    "usage-coverage-panel": byId("usage-coverage-panel"), "usage-coverage-list": byId("usage-coverage-list"),
    "usage-export-csv": byId("usage-export-csv"), "usage-export-json": byId("usage-export-json"),
    "usage-agent-list": byId("usage-agent-list"), "usage-more": byId("usage-more"),
    "usage-detail": byId("usage-detail"), "usage-detail-list": byId("usage-detail-list"),
    "usage-detail-close": byId("usage-detail-close")
}, {
    // The Feature table draws a Project's pixel mark, and it draws it with the page's one
    // `drawIcon` rather than a second copy. It arrives through this seam rather than an import
    // because `view/usage.js` deliberately imports nothing: `core/pixels.js` reaches `window` at
    // module scope, and the Usage module is exercised whole in Node by Tests/web-usage-analytics.mjs.
    drawIcon: drawIcon, tint: tint
});

/* The Plan page. The same shape as the two above — an element table and a set of seams — and
   for the same reason: `view/plan.js` reaches no global at module scope, so the states that only
   happen around a payment can be driven in Node and read.

   `returning` is the one thing the page cannot work out for itself. It is consumed rather than
   read, because arriving at the Plan page a second time in the same visit is an ordinary arrival
   and must not re-enter the wait for a webhook. */
var plan = bindPlanPage({
    "plan": byId("plan"), "plan-title": byId("plan-title"), "plan-lede": byId("plan-lede"),
    "plan-close": byId("plan-close"), "plan-includes": byId("plan-includes"),
    "plan-tier": byId("plan-tier"), "plan-tier-note": byId("plan-tier-note"),
    "plan-say": byId("plan-say"), "plan-alert": byId("plan-alert"),
    "plan-limits": byId("plan-limits"), "plan-fine": byId("plan-fine"),
    "plan-upgrade": byId("plan-upgrade"), "plan-portal": byId("plan-portal"),
    "plan-signin": byId("plan-signin"), "plan-retry": byId("plan-retry"),
    "plan-recheck": byId("plan-recheck"),
    "plan-elsewhere": byId("plan-elsewhere"), "plan-console": byId("plan-console"),
    "plan-simulation": byId("plan-simulation"),
    "plan-simulation-title": byId("plan-simulation-title"),
    "plan-simulation-values": byId("plan-simulation-values"),
    "plan-simulation-actual": byId("plan-simulation-actual"),
    "plan-simulation-free": byId("plan-simulation-free"),
    "plan-simulation-pro": byId("plan-simulation-pro")
}, {
    billing: planBilling,
    signInURL: function () { return planSignIn(); },
    consoleOrigin: cloudConfig ? cloudConfig.appOrigin : "https://app.clawdline.com"
});

function consumeCheckoutReturn() {
    var returning = returningFromCheckout;
    returningFromCheckout = false;
    return returning;
}

/**
 * The pages, in the order the menu names them, home first.
 *
 * This array is the whole registry: `core/pages.js` knows no page names of its own, so a page is
 * added by putting a section in the document, a row in the drawer, and a line here. The Projects
 * page belongs between `sessions` and `usage` — see `docs/web-pages.md`.
 *
 * `enter` is what a page does on arrival however the arrival happened — a row in the menu, a
 * pasted `#page=…`, the browser's Back — and `focus` is where the keyboard lands once it has.
 */
Pages.bind({
    document: document,
    root: document.documentElement,
    // Where the keyboard goes when the page arrived at names no control of its own — the session
    // list does not, and it is the page every Close and Escape leads to. The wordmark is on screen
    // whatever page this is, and it is what opens the way to the others.
    focusFallback: "brand",
    pages: [
        { name: "sessions", element: byId("app") },
        { name: "devices", element: byId("devices"), focus: "devices-title",
          enter: function () { devices.enter(); }, leave: function () { devices.leave(); } },
        { name: "documents", element: byId("documents-page"), focus: "documents-title",
          enter: function () { documents.enter(); }, leave: function () { documents.leave(); } },
        { name: "board", element: byId("board"), focus: "board-title",
          enter: function () { enterProjectBoard(board, function (name) { Pages.go(name); }); }, leave: function () { board.leave(); } },
        { name: "timeline", element: byId("timeline"), focus: "timeline-title",
          enter: function () { timeline.enter(); }, leave: function () { timeline.leave(); } },
        { name: "projects", element: byId("projects"), focus: "projects-title",
          enter: function () { projects.enter(); }, leave: function () { projects.leave(); } },
        { name: "usage", element: byId("usage-analytics"), focus: "usage-close",
          enter: function () { usage.enter(); }, leave: function () { usage.leave(); } },
        { name: "ledger", element: byId("ledger"), focus: "ledger-title",
          enter: function () { ledger.enter(); }, leave: function () { ledger.leave(); } },
        // The one page that may be opened from in front of the cloud door, because paying needs
        // the session cookie and nothing the door is about.
        { name: "plan", element: byId("plan"), focus: "plan-title",
          enter: function () {
              deferCloudGate();
              plan.enter({ returning: consumeCheckoutReturn() });
          },
          leave: function () {
              plan.leave();
              if (cloudGateUp) showCloudGate();
          } },
        { name: "settings", element: byId("settings"), focus: "settings-close",
          enter: function () { Settings.enter(); } }
    ],
    onChange: function (name, previous) {
        markSidebarPage(name, previous);
        if (name === Pages.home() && S.openId && !S.tx.loading) SessionBoard.resume();
    },
    // Written with `replaceState` rather than by assigning to `location.hash`, because a page is
    // where you are and not a step you took: a reload lands back on it, and the Back button still
    // means the screen before this app rather than three menu presses ago. The fragment is
    // deliberately the whole of it — arriving at a page is arriving away from `#session=…`.
    writeHash: function (hash) {
        try { history.replaceState(history.state, "", hash); }
        catch (e) { location.hash = hash; }
    }
});

// The controls module keeps its pure command selection importable without a browser. DOM and
// the one route back to ordinary Session actions are supplied here, at the page boundary.
CoordinatorControls.bind({
    overlay: document.getElementById("coordinator-controls"),
    sheet: document.getElementById("coordinator-controls-sheet"),
    title: document.getElementById("coordinator-controls-title"),
    body: document.getElementById("coordinator-controls-body"),
    close: document.getElementById("coordinator-controls-close"),
    context: function () { return { connected: S.conn === "live", write: S.write === true }; },
    onSessionActions: function (id) {
        openSession(id);
        setTimeout(function () { SessionActions.open(els["detail-actions-trigger"]); }, 0);
    }
});

// The wordmark wears the project's own mark, drawn by the same code the rows use — it comes
// from ~/.claude/project-icons.json in the app, and it is the one icon this page knows by heart.
var mark = {
    accent: "#d97757",
    cells: [".######.", ".#o##o#.", "########", ".##..##."].map(function (row) {
        return row.split("").map(function (ch) {
            return ch === "#" ? "#d97757" : (ch === "o" ? "#141416" : "#33201a");
        });
    })
};
drawIcon(els["brand-mark"], mark, 3);
drawIcon(els["door-mark"], mark, 3);

/**
 * Nothing is drawn until the interface has its words.
 *
 * The strings are the one thing the page cannot start without and cannot get for itself: they
 * decide what every label says, and repainting the furniture a moment after it has been read is
 * how a page comes to look like it changed its mind. So the first render waits, and the head of
 * this document holds the paint back while it does.
 *
 * Everything that can go wrong here ends the same way — the English already written into the
 * markup stands, and the page comes up. That is what it is in the document for.
 */
var booted = false;
function boot(data) {
    if (booted) return;
    booted = true;
    // Off first, and before anything that can throw: a page that is visible and half in English
    // is a page, and a page that is permanently invisible is not.
    document.documentElement.classList.remove("booting");
    try { applyStrings(data); } catch (e) { /* English, then */ }
    documents.paint();
    paintStatic();

    renderConn();
    // **Before the first paint, not after it.** The two screens that say "there is nothing here"
    // ask whether this wait is still running — see `listUnknown` — and a wait that has not been
    // started yet answers no. Started here, the first render draws neither of them.
    Waits.list.start();
    renderTranscript();
    render();
    // Lemon Squeezy's return path, turned into an ordinary page address before `routeTo` reads
    // it. `replaceState` rather than an assignment for the reason the page registry gives: this
    // is where you *are*, and the browser's Back should still mean the screen before this app.
    if (returningFromCheckout) {
        try { history.replaceState(history.state, "", "/#page=plan"); }
        catch (e) { location.hash = "#page=plan"; }
    }
    // Read before the transport starts: `adoptToken` wipes the fragment when there is a token in
    // it, and a URL can carry both.
    routeTo(location.hash);
    api.start();
    BoardControls.refresh();
    Schedules.start();
    Push.start();
    watchForStaleness();
    Diagnostics.ready();
}

/**
 * Coming back to a page that was put away, and asking whether it is still telling the truth.
 *
 * **A background tab's connection is suspended, not closed**, so the page returns holding
 * whatever it was holding — and a session whose state finished moving while the page was asleep
 * has no further frame to send. The transport is fine, the stream is open, and the screen is
 * wrong. `visibilitychange` is the one moment the page knows it may have missed something, and
 * `revalidate` is the transport's answer to "is this still true"; a transport without one keeps
 * whatever it has, which is what every transport did before this existed.
 *
 * Bound here rather than inside a transport because it is a fact about the *page* — being put
 * away and brought back — and both transports want the same thing done about it.
 */
function watchForStaleness() {
    document.addEventListener("visibilitychange", function () {
        if (document.hidden) return;
        if (api && typeof api.revalidate === "function") api.revalidate("visible");
    });
}

if (window.__strings) {
    // The ordinary path, and the fast one: the app writes the words into the document it serves,
    // so by the time this line runs they are already here and the page can be drawn in the frame
    // the modules finished in. The fetch below is what this replaced — a round trip that could
    // not be sent until every module had arrived and run, in front of a page held blank.
    boot(window.__strings);
} else if (location.protocol === "file:") {
    // A copy opened off a disk has no server to ask, and asking one that is not there is the
    // failed request in an otherwise clean console that teaches somebody to stop reading it.
    boot(null);
} else {
    // Not through `jsonFetch`: its own "could not reach Clawdline" is one of the strings being
    // fetched here, and a page cannot explain a failure in words it has not been given yet.
    // `no-store` because the answer depends on a header no cache is keyed on.
    var hostedStrings = cloudStringsURL(cloudConfig,
        typeof navigator !== "undefined" ? navigator.languages : []);
    fetch(hostedStrings || "/v1/strings", { cache: hostedStrings ? "force-cache" : "no-store" })
        .then(function (res) { return res.ok ? res.json() : null; })
        .catch(function () { return null; })
        .then(boot);
    // And a page that is never drawn at all is worse than one drawn in English. A local answer
    // takes a millisecond; something in front of a tunnel on a bad connection takes longer, and
    // two seconds is well past the point where a dark rectangle stops reading as "loading".
    setTimeout(function () { boot(null); }, 2000);
}

// A page left open all day: "2m ago" is only true for a minute. The timestamps are rewritten in
// place rather than by rendering the transcript again — a re-render replaces the whole pane, and
// the reader would find themselves back at the top of it every minute for no reason they could see.
setInterval(function () {
    var stamps = els.tx.querySelectorAll("time[data-at]");
    for (var i = 0; i < stamps.length; i++) {
        stamps[i].textContent = clockOf(parseInt(stamps[i].getAttribute("data-at"), 10));
    }
}, 60000);
