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
import { clockOf, tint } from "./core/util.js";
import { drawIcon } from "./core/pixels.js";
import { api, useApi } from "./net/api.js";
import "./net/build.js";
import "./net/fetch.js";
import { Schedules } from "./net/schedules.js";
import { Live } from "./net/live.js";
import { Mock } from "./net/mock.js";
import {
    CloudViewerSession, chooseTransport, idleClient, keepConnected, readCloudConfig
} from "./net/cloud-boot.js";
import { handlers } from "./net/handlers.js";
import {
    captureCloudPairingInvitation, clearCloudPairingInvitation, showCloudInstallGate,
    hideCloudGate, showCloudBootError, showCloudDeviceRecovery, showCloudPairing,
    showCloudSignIn
} from "./input/cloud-pairing.js";
import { cloudOnboardingMode, cloudViewerDeviceMetadata } from "./net/cloud-onboarding.js";
import "./door/door.js";
import "./view/derive.js";
import { render, renderConn } from "./view/list.js";
import { renderTranscript } from "./view/transcript.js";
import "./view/terminal.js";
import { bindProjectsPage } from "./view/projects.js";
import { bindUsagePortfolio } from "./view/usage.js";
import "./view/markdown.js";
import "./view/composer.js";
import { paintStatic } from "./view/static.js";
import { Waits } from "./view/waits.js";
import { loadTranscript, observeTranscriptFileRevision, openSession } from "./session/open.js";
import { createTranscriptEventRouter } from "./session/transcript-requests.js";
import "./session/agent.js";
import "./input/keys.js";
import "./input/swipe.js";
import { SessionActions } from "./input/detail-actions.js";
import { CoordinatorControls } from "./input/coordinator-actions.js";
import "./input/user-messages.js";
import "./input/git-panel.js";
import "./input/shell-panel.js";
import "./input/action-confirm.js";
import { routeTo } from "./input/route.js";
import { markSidebarPage } from "./input/sidebar.js";
import { Settings } from "./input/settings.js";
import "./input/start.js";
import "./input/command.js";
import "./input/schedule.js";
import "./input/schedule-history.js";
import "./input/status-line.js";
import "./input/info.js";
import { Push } from "./input/push.js";
import "./input/shots.js";
import "./input/voice.js";
import "./input/composer.js";
import "./input/edges.js";

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
            function () { return S.openId; },
            observeTranscriptFileRevision,
            function (id) { loadTranscript(id, true); }
        ));
    }
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
        var cloudInvitation = null;
        try {
            cloudInvitation = captureCloudPairingInvitation(window, Date.now());
        } catch (invitationError) {
            console.error("clawdline: " + invitationError.message);
        }
        var startCloudViewer = function () {
            keepConnected(cloudSession, {
                onState: function (update) {
                    if (update.state === "connected") {
                        hideCloudGate();
                        useApi(update.client);
                        bindTranscriptEvents(update.client);
                    } else if (update.state === "sign_in") {
                        handlers.conn("locked");
                        showCloudSignIn(update.url);
                    } else if (update.state === "device_limit_reached") {
                        handlers.conn("locked");
                        showCloudDeviceRecovery(cloudSession, update, {
                            onRecovered: startCloudViewer
                        }).catch(function () { /* the recovery screen owns its visible error */ });
                    } else if (update.state === "pairing_required") {
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

// A deterministic visual fixture for the same bundled page. It never runs outside mock mode,
// never opens a Cloud session, and lets mobile layout checks hold the install/scan screen still.
if (MOCK && params.get("cloud-onboarding") === "install") showCloudInstallGate();
if (MOCK && params.get("cloud-onboarding") === "scan") {
    showCloudPairing({}, { scan: true });
}
Diagnostics.bind({ state: S, elements: els });

// Usage lives in the same stamped module graph as the rest of the page. Keeping its import here
// makes the preload URL and the runtime request one identity, while these literal lookups keep the
// DOM contract visible to the permanent repository guard.
var byId = function (id) { return document.getElementById(id); };
// `usage-open` is not in this table any more. It is the drawer's Usage row now, and reaching the
// page is the drawer's business; the portfolio module stopped having an opinion about how somebody
// got to it. The id stays on that row — `usage.css` styles it and the Usage guard looks for it.
/* The Projects page. Two reads, and both of them are absent on the Cloud path — `/v1/places`
   already was, and the worktree join is deliberately not in the paired viewer's read vocabulary
   because its subject is a Project and every read there carries a session. So the transport is
   handed over as thunks and a `carries` question, all three asked when the page is used: `api` is
   a live binding the entry point fills in, and on the Cloud path it is filled in twice. */
var projects = bindProjectsPage({
    "projects": byId("projects"), "sidebar": byId("sidebar"),
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
    "project-read": byId("project-read")
}, {
    carries: function () {
        return typeof api.places === "function" && typeof api.projectWorktrees === "function";
    },
    places: function () { return api.places(); },
    projectWorktrees: function (path) { return api.projectWorktrees(path); },
    // The same seam the Feature table uses, and for the same reason: `view/projects.js` imports
    // nothing but the words, because `core/pixels.js` reaches `window` while it is being
    // evaluated and this module is exercised whole in Node by Tests/web-projects.mjs.
    drawIcon: drawIcon, tint: tint,
    navigate: function (name) { Pages.go(name); }
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
        { name: "projects", element: byId("projects"), focus: "projects-title",
          enter: function () { projects.enter(); }, leave: function () { projects.leave(); } },
        { name: "usage", element: byId("usage-analytics"), focus: "usage-close",
          enter: function () { usage.enter(); }, leave: function () { usage.leave(); } },
        { name: "settings", element: byId("settings"), focus: "settings-close",
          enter: function () { Settings.enter(); } }
    ],
    onChange: markSidebarPage,
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
    paintStatic();

    renderConn();
    // **Before the first paint, not after it.** The two screens that say "there is nothing here"
    // ask whether this wait is still running — see `listUnknown` — and a wait that has not been
    // started yet answers no. Started here, the first render draws neither of them.
    Waits.list.start();
    renderTranscript();
    render();
    // Read before the transport starts: `adoptToken` wipes the fragment when there is a token in
    // it, and a URL can carry both.
    routeTo(location.hash);
    api.start();
    Schedules.start();
    Push.start();
    Diagnostics.ready();
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
    fetch("/v1/strings", { cache: "no-store" })
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
