import Foundation

/// The web interface's own assets, and only those.
///
/// `RemoteServer` owns the socket, the routes and the event stream; this namespace owns the five
/// bodies its asset routes hand back — the document, the stylesheets and modules under `/app/`,
/// the service worker, the manifest, and the strings the page is written in. Nothing here reads a
/// connection, enters a queue or names `Orchestrator`: every answer is a function of the request's
/// `Accept-Language`, the bundle on disk, and this build's stamp.
///
/// It is a separate namespace rather than an `extension RemoteServer` in another file on purpose:
/// an extension moves the text and leaves the dependency where it was. Naming
/// `RemoteServer.Request`, `RemoteServer.Response` and `RemoteServer.buildStamp` in full is the
/// whole of what this file asks of the server, written where a reader can count it.
///
/// **The JavaScript below is a response body.** `serviceWorker()` emits a script, `page(for:)`
/// substitutes into a document, and `stringsScript(for:)` writes JSON into a `<script>` element —
/// so reformatting one of those literals changes bytes a browser has already been handed. `self`
/// inside the service-worker source is that worker's own global scope, not a Swift reference.
enum RemotePage {

    /// The web interface's own words, in whatever language the browser reads.
    ///
    /// **The page translates nothing.** It ships with the English as a fallback so that a fetch
    /// that fails leaves a readable screen rather than a screen of blank labels, and it asks for
    /// this before its first render; everything it says afterwards comes from here. One set of
    /// words in one place is the whole reason ``Copy`` exists, and a second set living in the
    /// HTML would be a second thing to translate and the first thing to forget.
    ///
    /// Which language is ``L/copy(forAcceptLanguage:)``'s answer: the browser's `Accept-Language`
    /// sorted by `q`, unless somebody has named a language in the config, which wins. The person
    /// holding the phone is not necessarily the person the Mac belongs to.
    ///
    /// A flat object of strings, keyed by the name of the ``Copy`` member it came from, so that a
    /// string can be followed from the page to this file to the translations with one search.
    /// Some of them are not new: a session that is waiting for you says the same thing here as it
    /// does in the bar, and the reused ones are named at the bottom.
    static func strings(for request: RemoteServer.Request) -> RemoteServer.Response {
        let t = L.copy(forAcceptLanguage: request.headers["accept-language"])
        let tag = L.tag(of: t)
        var out: [String: Any] = [
            // For `<html lang>`, which is what a screen reader picks a voice from and what a
            // browser offers to translate a page against.
            "lang": tag,
            // And `<html dir>`. Every language this app speaks is written left to right, so this
            // is `ltr` today whatever the header said — it is sent, and derived rather than
            // assumed, so that the day one that is not gets added the page is already asking.
            "dir": L.direction(of: tag),
        ]
        func add(_ pairs: [String: String]) {
            for (key, value) in pairs { out[key] = value }
        }
        // The connection chip.
        add([
            "webConnLive": t.webConnLive,
            "webConnConnecting": t.webConnConnecting,
            "webConnRetrying": t.webConnRetrying,
            "webConnOffline": t.webConnOffline,
            "webConnLocked": t.webConnLocked,
            "webConnTipLive": t.webConnTipLive,
            "webConnTipLocked": t.webConnTipLocked,
            "webConnTipDown": t.webConnTipDown,
        ])
        // The header's counts.
        add([
            "webCountWorking": t.webCountWorking,
            "webCountWaiting": t.webCountWaiting,
            "webCountUnreadable": t.webCountUnreadable,
            "webCountQuietOne": t.webCountQuietOne,
            "webCountQuietMany": t.webCountQuietMany,
            "webCountNone": t.webCountNone,
        ])
        // The list, its filter, and the four ways it can be empty.
        add([
            "webFilterPlaceholder": t.webFilterPlaceholder,
            "webFilterLabel": t.webFilterLabel,
            "webListLabel": t.webListLabel,
            "webPull": t.webPull,
            "webPullRelease": t.webPullRelease,
            "webPullBusy": t.webPullBusy,
            "webEmptyFilterTitle": t.webEmptyFilterTitle,
            "webEmptyFilterHint": t.webEmptyFilterHint,
            "webEmptyLockedTitle": t.webEmptyLockedTitle,
            "webEmptyLockedHint": t.webEmptyLockedHint,
            "webEmptyNoneHint": t.webEmptyNoneHint,
            "webEmptyWaitTitle": t.webEmptyWaitTitle,
            "webEmptyWaitHint": t.webEmptyWaitHint,
            "webStateUnreadable": t.webStateUnreadable,
            "webStateWorking": t.webStateWorking,
            "sessionWorkReady": t.sessionWorkReady,
            "sessionWorkUnknown": t.sessionWorkUnknown,
            "sessionWorkHolding": t.sessionWorkHolding,
            "sessionWorkOwed": t.sessionWorkOwed,
            "sessionWorkSelfStated": t.sessionWorkSelfStated,
            "sessionWorkMilestone": t.sessionWorkMilestone,
            "sessionWorkComplete": t.sessionWorkComplete,
            "closeabilitySafe": t.closeabilitySafe,
            // One established transport key, two renderer forms. The unit separator cannot be
            // spoken copy and keeps older key inventories closed while letting grammar vary.
            "closeabilityBlocked": t.closeabilityBlockedOne + "\u{1f}"
                + t.closeabilityBlockedMany,
            "closeabilityNeedsAttestation": t.closeabilityNeedsAttestation,
            "closeabilityUnknown": t.closeabilityUnknown,
            "closeabilityWhy": t.closeabilityWhy,
            "closeabilityMoverSelf": t.closeabilityMoverSelf,
            "closeabilityMoverPerson": t.closeabilityMoverPerson,
            "closeabilityMoverSession": t.closeabilityMoverSession,
            "closeabilityMoverBroker": t.closeabilityMoverBroker,
            "closeabilityNotProven": t.closeabilityNotProven,
            "closeabilityAttestationExplanation": t.closeabilityAttestationExplanation,
            "closeabilityTechnicalDetails": t.closeabilityTechnicalDetails,
        ])
        // The transcript pane.
        add([
            "webBack": t.webBack,
            "webBackLabel": t.webBackLabel,
            "webMenu": t.webMenu,
            "webPages": t.webPages,
            "webSessions": t.webSessions,
            "webUsage": t.webUsage,
            "webNoSessionOpen": t.webNoSessionOpen,
            "webOrderTip": t.webOrderTip,
            "webShowOnMac": t.webShowOnMac,
            "webShowOnMacTip": t.webShowOnMacTip,
            "webShowOnMacOff": t.webShowOnMacOff,
            "webShowOnMacAsked": t.webShowOnMacAsked,
            "webSessionActions": t.webSessionActions,
            "webSessionGit": t.webSessionGit,
            "webScreenTitle": t.webScreenTitle,
            "webScreenLive": t.webScreenLive,
            "webScreenOnDemand": t.webScreenOnDemand,
            "webScreenGone": t.webScreenGone,
            "webGitTitle": t.webGitTitle,
            "webGitClean": t.webGitClean,
            "webGitNotRepo": t.webGitNotRepo,
            "webGitFailed": t.webGitFailed,
            "webGitRefresh": t.webGitRefresh,
            "webGitClose": t.webGitClose,
            "webGitStaged": t.webGitStaged,
            "webGitUnstaged": t.webGitUnstaged,
            "webGitUntracked": t.webGitUntracked,
            "webGitConflict": t.webGitConflict,
            "webEndSession": t.webEndSession,
            "webConfirmActionTitle": t.webConfirmActionTitle,
            "webConfirmActionSay": t.webConfirmActionSay,
            "webConfirmEndTitle": t.webConfirmEndTitle,
            "webConfirmEndSay": t.webConfirmEndSay,
            "webConfirmEndLoses": t.webConfirmEndLoses,
            "webClosing": t.webClosing,
            "webCancel": t.webCancel,
            "webConfirm": t.webConfirm,
            "webReviewBeforeClosing": t.webReviewBeforeClosing,
            "webConfirmEndAnyway": t.webConfirmEndAnyway,
            "webPickSession": t.webPickSession,
            "webReading": t.webReading,
            "webLoading": t.webLoading,
            "webTranscriptFailed": t.webTranscriptFailed,
            "webImageExpired": t.imageExpired,
            "webImagePreview": t.imagePreview,
            "webImageClose": t.imageClose,
            "webImageTooLarge": t.imageTooLarge,
            "webImageUnavailable": t.imageUnavailable,
            "webWhoYou": t.webWhoYou,
            "webWhoTool": t.webWhoTool,
            "webNoticeTask": t.webNoticeTask,
            "webNoticeCompleted": t.webNoticeCompleted,
            "webNoticeFailed": t.webNoticeFailed,
            "webNoticeTimedOut": t.webNoticeTimedOut,
            "webNoticeCancelled": t.webNoticeCancelled,
            "webNoticeCouldNotStart": t.webNoticeCouldNotStart,
            "webNoticeFinished": t.webNoticeFinished,
            "webNoticeWorkspaceOverlap": t.webNoticeWorkspaceOverlap,
            "webNoticeNoSiblings": t.webNoticeNoSiblings,
            "webNoticeOneSibling": t.webNoticeOneSibling,
            "webNoticeManySiblings": t.webNoticeManySiblings,
            "webNoticeClaimsReleased": t.webNoticeClaimsReleased,
            "webNoticeFileWaitRequested": t.webNoticeFileWaitRequested,
            "webNoticeFileWaitReleased": t.webNoticeFileWaitReleased,
            "webNoticeHandoffPickedUp": t.webNoticeHandoffPickedUp,
            "webNoticeHandoffNeedsDelivery": t.webNoticeHandoffNeedsDelivery,
            "webNoticeRecheckGit": t.webNoticeRecheckGit,
            "webPending": t.webPending,
            "webAttachedImage": t.webAttachedImage,
            "webAttachedImages": t.webAttachedImages,
            "webSteps": t.webSteps,
            "webJustNow": t.webJustNow,
            "webMinutesAgo": t.webMinutesAgo,
            "webCodeCopy": t.webCodeCopy,
            "webCodeCopied": t.webCodeCopied,
            "webCodeCopyFailed": t.webCodeCopyFailed,
        ])

        // The snippets sheet, reached from the `⋯` menu and from the project mark beside it.
        // Everything from `webSnippetNew` down is only ever drawn where the transport can
        // write, and is sent anyway: the payload is what this browser may need, and which
        // controls it draws is decided in the page from `snippetControls`.
        add([
            "webSnippets": t.webSnippets,
            "webSnippetsThisProject": t.webSnippetsThisProject,
            "webSnippetsEveryProject": t.webSnippetsEveryProject,
            "webSnippetsEmpty": t.webSnippetsEmpty,
            "webSnippetsEmptyNew": t.webSnippetsEmptyNew,
            "webSnippetsReadOnly": t.webSnippetsReadOnly,
            "webSnippetNew": t.webSnippetNew,
            "webSnippetEditing": t.webSnippetEditing,
            "webSnippetMore": t.webSnippetMore,
            "webSnippetEdit": t.webSnippetEdit,
            "webSnippetDelete": t.webSnippetDelete,
            "webSnippetDeleteAsk": t.webSnippetDeleteAsk,
            "webSnippetUp": t.webSnippetUp,
            "webSnippetDown": t.webSnippetDown,
            "webSnippetToGlobal": t.webSnippetToGlobal,
            "webSnippetToProject": t.webSnippetToProject,
            "webSnippetTitleLabel": t.webSnippetTitleLabel,
            "webSnippetBodyLabel": t.webSnippetBodyLabel,
            "webSnippetScopeLabel": t.webSnippetScopeLabel,
            "webSnippetSave": t.webSnippetSave,
            "webSnippetFromLast": t.webSnippetFromLast,
            "webSnippetNeedsText": t.webSnippetNeedsText,
            "webSnippetTooLong": t.webSnippetTooLong,
            "webSnippetStarterCommitTitle": t.webSnippetStarterCommitTitle,
            "webSnippetStarterCommitBody": t.webSnippetStarterCommitBody,
            "webSnippetStarterReportTitle": t.webSnippetStarterReportTitle,
            "webSnippetStarterReportBody": t.webSnippetStarterReportBody,
        ])

        // The composer, and what it refuses.
        add([
            "webSend": t.webSend,
            "webAttach": t.webAttach,
            "webRemoveShot": t.webRemoveShot,
            "webWriteOpen": t.webWriteOpen,
            "webWriteOff": t.webWriteOff,
            "webShotsOnlyPictures": t.webShotsOnlyPictures,
            "webShotsTooMany": t.webShotsTooMany,
            "webShotTooBig": t.webShotTooBig,
            "webShotsTooBig": t.webShotsTooBig,
            "webShotUnreadable": t.webShotUnreadable,
            "webShotNeedsSession": t.webShotNeedsSession,
        ])

        // The microphone in the composer, and everything `POST /v1/voice` can come back with.
        // Four of these refusals are the browser's own and three are this Mac's, and the page
        // picks between them by what it was handed — so all seven have to arrive translated or a
        // phone is told in English why a Mac it cannot see has no model on it.
        add([
            "webVoiceStart": t.webVoiceStart,
            "webVoiceStop": t.webVoiceStop,
            "webVoiceDone": t.webVoiceDone,
            "webVoiceListening": t.webVoiceListening,
            "webVoiceReading": t.webVoiceReading,
            "webVoiceSlow": t.webVoiceSlow,
            "webVoiceLimit": t.webVoiceLimit,
            "webVoiceEmpty": t.webVoiceEmpty,
            "webVoiceTooShort": t.webVoiceTooShort,
            "webVoiceDenied": t.webVoiceDenied,
            "webVoiceNoMic": t.webVoiceNoMic,
            "webVoiceInUse": t.webVoiceInUse,
            "webVoiceInsecure": t.webVoiceInsecure,
            "webVoiceUnsupported": t.webVoiceUnsupported,
            "webVoiceBusy": t.webVoiceBusy,
            "webVoiceNoBinary": t.webVoiceNoBinary,
            "webVoiceNoModel": t.webVoiceNoModel,
            "webVoiceFailed": t.webVoiceFailed,
        ])

        // The command sheet: one sentence, the draft it turned into, and the four ways there is
        // no draft to show. Three of those four are this Mac answering `POST /v1/intents` —
        // `no_planner`, `busy` and a turn that came back with nothing — and the fourth is the
        // page noticing it was handed an empty sentence before it spent anything asking.
        add([
            "webCommand": t.webCommand,
            "webCommandLabel": t.webCommandLabel,
            "webCommandSay": t.webCommandSay,
            "webCommandHeard": t.webCommandHeard,
            "webCommandThinking": t.webCommandThinking,
            "webCommandDraft": t.webCommandDraft,
            "webCommandWhere": t.webCommandWhere,
            "webCommandWith": t.webCommandWith,
            "webCommandModel": t.webCommandModel,
            "webCommandFirst": t.webCommandFirst,
            "webCommandGo": t.webCommandGo,
            "webCommandUnsure": t.webCommandUnsure,
            "webCommandFailed": t.webCommandFailed,
            "webCommandNoPlanner": t.webCommandNoPlanner,
            "webCommandBusy": t.webCommandBusy,
            "webCommandEmpty": t.webCommandEmpty,
        ])

        // Starting a session from the page — the sheet, and the wait between the tab opening and
        // the session turning up in the list. The last two carry `{app}`, which the page fills in
        // from the terminal's name in the error object rather than from anything it knows itself.
        add([
            "webStart": t.webStart,
            "webStartLabel": t.webStartLabel,
            "webStartPick": t.webStartPick,
            "webStartWith": t.webStartWith,
            "webStartEmpty": t.webStartEmpty,
            "webStartFilter": t.webStartFilter,
            "webStarting": t.webStarting,
            "webStartWaiting": t.webStartWaiting,
            "webStartSlow": t.webStartSlow,
            "webStartFailed": t.webStartFailed,
            "webStartGone": t.webStartGone,
            "webStartTerminalClosed": t.webStartTerminalClosed,
            "webStartTerminalUnsupported": t.webStartTerminalUnsupported,
            "webStartDetached": t.webStartDetached,
            "webStartOff": t.webStartOff,
            "webResumeWith": t.webResumeWith,
            "webResumePick": t.webResumePick,
            "webResumeFilter": t.webResumeFilter,
            "webResumeEmpty": t.webResumeEmpty,
            "webResumeLive": t.webResumeLive,
            "webResumeBack": t.webResumeBack,
            "webResumeGone": t.webResumeGone,
            "webResumeClaudeOnly": t.webResumeClaudeOnly,
            "webResuming": t.webResuming,
            "webResumeCapped": t.webResumeCapped,
        ])

        // The key row along the bottom, on a desk.
        add([
            "webHintMove": t.webHintMove,
            "webHintOpen": t.webHintOpen,
            "webHintFilter": t.webHintFilter,
            "webHintPane": t.webHintPane,
        ])

        // The shortcuts card.
        add([
            "webKeysLabel": t.webKeysLabel,
            "webKeysTitle": t.webKeysTitle,
            "webKeysMove": t.webKeysMove,
            "webKeysOpen": t.webKeysOpen,
            "webKeysFilter": t.webKeysFilter,
            "webKeysEscape": t.webKeysEscape,
            "webKeysList": t.webKeysList,
            "webKeysPane": t.webKeysPane,
            "webKeysEnds": t.webKeysEnds,
            "webKeysReverse": t.webKeysReverse,
            "webKeysThis": t.webKeysThis,
            "webKeysFoot": t.webKeysFoot,
        ])

        // The door.
        add([
            "webDoorLabel": t.webDoorLabel,
            "webDoorAskLede": t.webDoorAskLede,
            "webDoorAskFine": t.webDoorAskFine,
            "webDoorName": t.webDoorName,
            "webDoorAsk": t.webDoorAsk,
            "webDoorToPassword": t.webDoorToPassword,
            "webDoorCodeLede": t.webDoorCodeLede,
            "webDoorCodeFine": t.webDoorCodeFine,
            "webDoorTwoMinutes": t.webDoorTwoMinutes,
            "webDoorDigit": t.webDoorDigit,
            "webDoorConfirm": t.webDoorConfirm,
            "webDoorRestart": t.webDoorRestart,
            "webDoorPasswordLede": t.webDoorPasswordLede,
            "webDoorPasswordFine": t.webDoorPasswordFine,
            "webDoorPassword": t.webDoorPassword,
            "webDoorPasswordGo": t.webDoorPasswordGo,
            "webDoorToPair": t.webDoorToPair,
            "webDoorAsking": t.webDoorAsking,
            "webDoorAskFailed": t.webDoorAskFailed,
            "webDoorRateLimited": t.webDoorRateLimited,
            "webDoorSixDigits": t.webDoorSixDigits,
            "webDoorChecking": t.webDoorChecking,
            "webDoorFinished": t.webDoorFinished,
            "webDoorWrongCode": t.webDoorWrongCode,
            "webDoorNeedPassword": t.webDoorNeedPassword,
            "webDoorWrongPassword": t.webDoorWrongPassword,
            "webDoorExpired": t.webDoorExpired,
            "webDoorPaired": t.webDoorPaired,
        ])

        // What a request that went wrong says.
        add([
            "webOffline": t.webOffline,
            "webNotJSON": t.webNotJSON,
            "webRequestFailed": t.webRequestFailed,
        ])

        // Notifications.
        add([
            "webNotifyGo": t.webNotifyGo,
            "webNotifyAsking": t.webNotifyAsking,
            "webNotifyStop": t.webNotifyStop,
            "webNotifyStopping": t.webNotifyStopping,
            "webNotifyOff": t.webNotifyOff,
            "webNotifyOn": t.webNotifyOn,
            "webNotifyBlocked": t.webNotifyBlocked,
            "webNotifyUnsupported": t.webNotifyUnsupported,
            "webNotifyHomeScreen": t.webNotifyHomeScreen,
            "webNotifyOnFailed": t.webNotifyOnFailed,
            "webNotifyOffFailed": t.webNotifyOffFailed,
        ])

        // The settings sheet, and the composer's in-flight state.
        add([
            "webSettings": t.webSettings,
            "webSettingsNotify": t.webSettingsNotify,
            "webSettingsAssistantIcons": t.webSettingsAssistantIcons,
            "webSettingsAssistantIconsSay": t.webSettingsAssistantIconsSay,
            "webSettingsAssistantIconsShow": t.webSettingsAssistantIconsShow,
            "webSettingsVersion": t.webSettingsVersion,
            "webClose": t.webClose,
            "webNotifySheetOff": t.webNotifySheetOff,
            "webNotifyTest": t.webNotifyTest,
            "webNotifyTestSent": t.webNotifyTestSent,
            "webNotifyTestNone": t.webNotifyTestNone,
            "webNotifyTestFailed": t.webNotifyTestFailed,
            "webSending": t.webSending,
            "webSendTip": t.webSendTip,
        ])
        // Said in both places, so said once. The bar and the page are two windows onto the same
        // sessions, and a row that reads "waiting for you" on a Mac should not read as something
        // else on the phone next to it.
        add([
            "placeholder": t.placeholder,
            "noSession": t.noSession,
            "noOutput": t.noOutput,
            "sessionWaiting": t.sessionWaiting,
            // A function on the Mac too, for the same reason as the order pair below: English
            // counts, so "1 shell" and "2 shells" are two sentences rather than one with a hole
            // in it. The page picks between them the way the bar does.
            "sessionShellOne": t.sessionShellOne,
            "sessionShellMany": t.sessionShellMany,
            // The owner's half of a file wait. Said here for the same reason as the two above:
            // the page and the bar draw the same row, and an owner who reads the panel on a
            // phone must not be the one person told about it in English.
            "sessionWaitedOnByOne": t.sessionWaitedOnByOne,
            "sessionWaitedOnByMany": t.sessionWaitedOnByMany,
            "sendFailed": t.sendFailed,
            "hintList": t.hintList,
            "hintKeys": t.hintKeys,
            "hintOrder": t.hintOrder,
            // A function on the Mac, where it can be called with the answer; two keys here,
            // because a question with two answers does not cross a JSON boundary as one.
            "webOrderNewest": t.outputOrder(newestFirst: true),
            "webOrderOldest": t.outputOrder(newestFirst: false),
        ])

        // A question with a menu on it, a page that has fallen behind the app.
        //
        // **These were translated into fourteen languages and then not sent for a day.** Nothing
        // broke — the page carries an English copy of everything as a fallback — which is exactly
        // why nobody noticed, and why `webWaitingSend`, a warning that sending from here confirms
        // the wrong option, was in English for everybody who does not read English. The test that
        // now walks every `web*` member on `Copy` and fails on anything missing here is the real
        // fix; this block is the part that was owed.
        add([
            "webAskLabel": t.webAskLabel,
            "webAskAny": t.webAskAny,
            "webWaitingTitle": t.webWaitingTitle,
            "webWaitingSay": t.webWaitingSay,
            "webWaitingSend": t.webWaitingSend,
            "webMenuSay": t.webMenuSay,
            "webMenuHighlighted": t.webMenuHighlighted,
            "webMenuSent": t.webMenuSent,
            "webStale": t.webStale,
            "webStaleGo": t.webStaleGo,
        ])

        // What a session has going in the background.
        add([
            "webAgents": t.webAgents,
            "webAgentsCount": t.webAgentsCount,
            "webAgentDone": t.webAgentDone,
            "webAgentFailed": t.webAgentFailed,
            "agentRunning": t.agentRunning,
            "agentTools": t.agentTools,
            "agentEmpty": t.agentEmpty,
            "agentBack": t.agentBack,
            "webAgentOpen": t.webAgentOpen,
            "webShells": t.webShells,
            "webShellTitle": t.webShellTitle,
            "webShellOpen": t.webShellOpen,
            "webShellRunning": t.webShellRunning,
            "webShellEnded": t.webShellEnded,
            "webShellQuiet": t.webShellQuiet,
            "webShellFailed": t.webShellFailed,
            "webShellClose": t.webShellClose,
            "webShellStop": t.webShellStop,
            "webShellStopTitle": t.webShellStopTitle,
            "webShellStopSay": t.webShellStopSay,
            "webShellStopped": t.webShellStopped,
            "webShellStopFailed": t.webShellStopFailed,
        ])

        // The chips on a root session and on the child it sent off — see `Orchestrator`.
        add([
            "webTaskRoot": t.webTaskRoot,
            "webTaskChild": t.webTaskChild,
            "webTaskTasks": t.webTaskTasks,
            "webTaskDone": t.webTaskDone,
            "webTaskFailed": t.webTaskFailed,
            "webTaskRunning": t.webTaskRunning,
            "webScheduleMissed": t.webScheduleMissed,
            "webScheduleNoNext": t.webScheduleNoNext,
            // The same word the Settings list draws above the same number, sent under a name of
            // its own rather than translated a second time into fourteen languages.
            "webScheduleNext": t.settingsScheduleNext,
        ])

        // The form that makes one — see `POST /v1/orchestrator/schedules`, which is the only
        // route allowed to write a schedule file.
        add([
            "webScheduleNew": t.webScheduleNew,
            "webScheduleNewSay": t.webScheduleNewSay,
            "webScheduleTitle": t.webScheduleTitle,
            "webScheduleAt": t.webScheduleAt,
            "webScheduleOn": t.webScheduleOn,
            "webScheduleWhere": t.webScheduleWhere,
            "webScheduleWith": t.webScheduleWith,
            "webScheduleModel": t.webScheduleModel,
            "webScheduleFirst": t.webScheduleFirst,
            "webScheduleMore": t.webScheduleMore,
            "webScheduleWhenDone": t.webScheduleWhenDone,
            "webScheduleCloseSuccess": t.webScheduleCloseSuccess,
            "webScheduleCloseAlways": t.webScheduleCloseAlways,
            "webScheduleCloseNever": t.webScheduleCloseNever,
            "webScheduleEnabled": t.webScheduleEnabled,
            "webScheduleDisabled": t.webScheduleDisabled,
            "webScheduleNotify": t.webScheduleNotify,
            "webScheduleCatchUp": t.webScheduleCatchUp,
            "webScheduleTimeout": t.webScheduleTimeout,
            "webScheduleCreate": t.webScheduleCreate,
            "webScheduleCreated": t.webScheduleCreated,
            // What `dispatch_enabled: false` on the create means, said where the page can draw
            // it: the file is written and nothing on this Mac will run it until Settings says so.
            "webScheduleDispatchOff": t.webScheduleDispatchOff,
            "webScheduleFailed": t.webScheduleFailed,
            "webScheduleNeedsTime": t.webScheduleNeedsTime,
            "webScheduleNeedsPlace": t.webScheduleNeedsPlace,
            "webScheduleDaily": t.webScheduleDaily,
            "webScheduleSun": t.webScheduleSun,
            "webScheduleMon": t.webScheduleMon,
            "webScheduleTue": t.webScheduleTue,
            "webScheduleWed": t.webScheduleWed,
            "webScheduleThu": t.webScheduleThu,
            "webScheduleFri": t.webScheduleFri,
            "webScheduleSat": t.webScheduleSat,
        ])

        // The same form, opened on a schedule that already exists — see
        // `PATCH`/`DELETE /v1/orchestrator/schedules/:id`. Sent as their own block rather than
        // folded into the one above because they are the words for changing something that is
        // already running unattended, and "Delete" is the only word on this page that asks a
        // question before it does anything.
        add([
            "webScheduleEdit": t.webScheduleEdit,
            "webScheduleSave": t.webScheduleSave,
            "webScheduleSaved": t.webScheduleSaved,
            "webScheduleDelete": t.webScheduleDelete,
            "webScheduleDeleteAsk": t.webScheduleDeleteAsk,
            "webScheduleDeleted": t.webScheduleDeleted,
        ])
        // The Links sheet.
        add([
            "webLinks": t.webLinks,
            "webLinksTip": t.webLinksTip,
            "webLinksPick": t.webLinksPick,
            "webLinksEmpty": t.webLinksEmpty,
            "webLinksFailed": t.webLinksFailed,
            "webLinksLocal": t.webLinksLocal,
            "webLinksFile": t.webLinksFile,
            "webLinksCopy": t.webLinksCopy,
            "webLinksCopied": t.webLinksCopied,
            "webLinksCopyFailed": t.webLinksCopyFailed,
            "webLinkOk": t.webLinkOk,
            "webLinkFail": t.webLinkFail,
            "webLinkDown": t.webLinkDown,
            "webLinkRunning": t.webLinkRunning,
            "webSettingsOrder": t.webSettingsOrder,
            "webSettingsOrderSay": t.webSettingsOrderSay,
        ])
        // The Session info card.
        add([
            "webSessionInfo": t.webSessionInfo,
            "webInfoTitle": t.webInfoTitle,
            "webInfoEditTitle": t.webInfoEditTitle,
            "webInfoCopyTitle": t.webInfoCopyTitle,
            "webInfoTitleSaved": t.webInfoTitleSaved,
            "webInfoTitleLocal": t.webInfoTitleLocal,
            "webInfoTitleQueued": t.webInfoTitleQueued,
            "webInfoTitleNotDurable": t.webInfoTitleNotDurable,
            "webInfoTitleCloud": t.webInfoTitleCloud,
            "webInfoSession": t.webInfoSession,
            "webInfoAssistant": t.webInfoAssistant,
            "webInfoModel": t.webInfoModel,
            "webInfoSessionId": t.webInfoSessionId,
            "webInfoDirectory": t.webInfoDirectory,
            "webInfoRunningFor": t.webInfoRunningFor,
            "webInfoStatus": t.webInfoStatus,
            "webInfoWorkStatusMeaning": t.webInfoWorkStatusMeaning,
            "webInfoCloseabilityMeaning": t.webInfoCloseabilityMeaning,
            "webInfoUsage": t.webInfoUsage,
            "webInfoInput": t.webInfoInput,
            "webInfoOutput": t.webInfoOutput,
            "webInfoCacheRead": t.webInfoCacheRead,
            "webInfoCacheWrite": t.webInfoCacheWrite,
            "webInfoTotal": t.webInfoTotal,
            "webInfoCost": t.webInfoCost,
            "webInfoNoUsage": t.webInfoNoUsage,
            "webInfoLimits": t.webInfoLimits,
            "webInfoLimitHit": t.webInfoLimitHit,
            "webInfoResets": t.webInfoResets,
            "webInfoUnknown": t.webInfoUnknown,
            "webInfoFiles": t.webInfoFiles,
            "webInfoBranch": t.webInfoBranch,
            "webInfoStaged": t.webInfoStaged,
            "webInfoUnstaged": t.webInfoUnstaged,
            "webInfoUntracked": t.webInfoUntracked,
            "webInfoConflict": t.webInfoConflict,
            "webInfoClean": t.webInfoClean,
            "webInfoNotRepo": t.webInfoNotRepo,
            "webInfoDeploy": t.webInfoDeploy,
            "webInfoNoDeploy": t.webInfoNoDeploy,
            "webInfoFailed": t.webInfoFailed,
            "webInfoBusy": t.webInfoBusy,
            "webInfoRefresh": t.webInfoRefresh,
            "webInfoTokens": t.webInfoTokens,
            "webInfoSwitchModel": t.webInfoSwitchModel,
            "webInfoModelOther": t.webInfoModelOther,
            "webInfoModelSent": t.webInfoModelSent,
            "webInfoModelBusy": t.webInfoModelBusy,
            "webInfoSwitchPermission": t.webInfoSwitchPermission,
            "webInfoPermissionAuto": t.webInfoPermissionAuto,
            "webInfoPermissionManual": t.webInfoPermissionManual,
            "webInfoPermissionAcceptEdits": t.webInfoPermissionAcceptEdits,
            "webInfoPermissionPlan": t.webInfoPermissionPlan,
            "webInfoPermissionUnreadable": t.webInfoPermissionUnreadable,
            "webInfoPermissionSent": t.webInfoPermissionSent,
            "webInfoPermissionBusy": t.webInfoPermissionBusy,
            "webInfoLimitsClaude": t.webInfoLimitsClaude,
            "webInfoCopied": t.webInfoCopied,
            "webInfoAsOf": t.webInfoAsOf,
            "webInfoWhyUnknown": t.webInfoWhyUnknown,
        ])
        // Asking a session to make itself Clawdfather. The browser composes the sentence and
        // types it through the ordinary send route; only the session can read the orchestrator
        // token, so only the session performs the registration.
        add([
            "webClawdfatherCreateLabel": t.webClawdfatherCreateLabel,
            "webClawdfatherIs": t.webClawdfatherIs,
            "webClawdfatherRegisterAsk": t.webClawdfatherRegisterAsk,
            "webClawdfatherRegisterSent": t.webClawdfatherRegisterSent,
            "webClawdfatherRegisterLate": t.webClawdfatherRegisterLate,
            "webClawdfatherRegisterBlocked": t.webClawdfatherRegisterBlocked,
        ])

        // The Clawdfather controls panel — sections, the closed commands table, effect and state
        // words, disabled reasons by code, and the rendering of the four connected reads.
        add([
            "webCoordSectionObserve": t.webCoordSectionObserve,
            "webCoordSectionCoordinate": t.webCoordSectionCoordinate,
            "webCoordSectionPresence": t.webCoordSectionPresence,
            "webCoordSectionAdmin": t.webCoordSectionAdmin,
            "webCoordCmdStatusReport": t.webCoordCmdStatusReport,
            "webCoordCmdStatusReportSay": t.webCoordCmdStatusReportSay,
            "webCoordCmdSinceAway": t.webCoordCmdSinceAway,
            "webCoordCmdSinceAwaySay": t.webCoordCmdSinceAwaySay,
            "webCoordCmdDuplicates": t.webCoordCmdDuplicates,
            "webCoordCmdDuplicatesSay": t.webCoordCmdDuplicatesSay,
            "webCoordCmdLandingClosure": t.webCoordCmdLandingClosure,
            "webCoordCmdLandingClosureSay": t.webCoordCmdLandingClosureSay,
            "webCoordCmdCoordinateWork": t.webCoordCmdCoordinateWork,
            "webCoordCmdCoordinateWorkSay": t.webCoordCmdCoordinateWorkSay,
            "webCoordCmdDispatch": t.webCoordCmdDispatch,
            "webCoordCmdDispatchSay": t.webCoordCmdDispatchSay,
            "webCoordCmdAsk": t.webCoordCmdAsk,
            "webCoordCmdAskSay": t.webCoordCmdAskSay,
            "webCoordCmdQuietWatch": t.webCoordCmdQuietWatch,
            "webCoordCmdQuietWatchSay": t.webCoordCmdQuietWatchSay,
            "webCoordCmdScope": t.webCoordCmdScope,
            "webCoordCmdScopeSay": t.webCoordCmdScopeSay,
            "webCoordCmdStop": t.webCoordCmdStop,
            "webCoordCmdStopSay": t.webCoordCmdStopSay,
            "webCoordCmdReconnect": t.webCoordCmdReconnect,
            "webCoordCmdReconnectSay": t.webCoordCmdReconnectSay,
            "webCoordCmdDeepAudit": t.webCoordCmdDeepAudit,
            "webCoordCmdDeepAuditSay": t.webCoordCmdDeepAuditSay,
            "webCoordTokenExpected": t.webCoordTokenExpected,
            "webCoordTokenLow": t.webCoordTokenLow,
            "webCoordTokenMedium": t.webCoordTokenMedium,
            "webCoordTokenHigh": t.webCoordTokenHigh,
            "webCoordTokenUnknown": t.webCoordTokenUnknown,
            "webCoordAuditPreview": t.webCoordAuditPreview,
            "webCoordAuditWhyOffline": t.webCoordAuditWhyOffline,
            "webCoordAuditWhyDisconnected": t.webCoordAuditWhyDisconnected,
            "webCoordAuditWhyNoWrite": t.webCoordAuditWhyNoWrite,
            "webCoordAuditSending": t.webCoordAuditSending,
            "webCoordAuditSent": t.webCoordAuditSent,
            "webCoordAuditFailed": t.webCoordAuditFailed,
            "webCoordEffectRead": t.webCoordEffectRead,
            "webCoordEffectAdvisory": t.webCoordEffectAdvisory,
            "webCoordEffectSpawns": t.webCoordEffectSpawns,
            "webCoordEffectMutation": t.webCoordEffectMutation,
            "webCoordStateAvailable": t.webCoordStateAvailable,
            "webCoordStateDraft": t.webCoordStateDraft,
            "webCoordStateUnavailable": t.webCoordStateUnavailable,
            "webCoordStatePreview": t.webCoordStatePreview,
            "webCoordStateDisabled": t.webCoordStateDisabled,
            "webCoordOnline": t.webCoordOnline,
            "webCoordOffline": t.webCoordOffline,
            "webCoordControlsTitle": t.webCoordControlsTitle,
            "webCoordOpenControls": t.webCoordOpenControls,
            "webCoordEmpty": t.webCoordEmpty,
            "webCoordDisabledFallback": t.webCoordDisabledFallback,
            "webCoordPreviewTitle": t.webCoordPreviewTitle,
            "webCoordPreviewNone": t.webCoordPreviewNone,
            "webCoordPreviewMutation": t.webCoordPreviewMutation,
            "webCoordPreviewDraft": t.webCoordPreviewDraft,
            "webCoordPreviewSpawn": t.webCoordPreviewSpawn,
            "webCoordPreviewContract": t.webCoordPreviewContract,
            "webCoordWhyNoCommandRoute": t.webCoordWhyNoCommandRoute,
            "webCoordWhyNoReturnLedger": t.webCoordWhyNoReturnLedger,
            "webCoordWhyDeviceCannotSpawn": t.webCoordWhyDeviceCannotSpawn,
            "webCoordWhyMachineTokenOnly": t.webCoordWhyMachineTokenOnly,
            "webCoordReadFailed": t.webCoordReadFailed,
            "webCoordActiveTasks": t.webCoordActiveTasks,
            "webCoordPendingLandings": t.webCoordPendingLandings,
            "webCoordOpenWaits": t.webCoordOpenWaits,
            "webCoordCountUnknown": t.webCoordCountUnknown,
            "webCoordStaleSessions": t.webCoordStaleSessions,
            "webCoordUnknown": t.webCoordUnknown,
            "webCoordWaitingList": t.webCoordWaitingList,
            "webCoordBlockingList": t.webCoordBlockingList,
            "webCoordAllQuiet": t.webCoordAllQuiet,
            "webCoordNoLandings": t.webCoordNoLandings,
            "webCoordUnregistered": t.webCoordUnregistered,
            "webCoordScopeLine": t.webCoordScopeLine,
            "webCoordScopeDevice": t.webCoordScopeDevice,
        ])

        // The Projects page, and one Project's finished-Feature worktrees.
        add([
            "webProjects": t.webProjects,
            "webProjectsLede": t.webProjectsLede,
            "webProjectsEmpty": t.webProjectsEmpty,
            "webProjectsUnavailable": t.webProjectsUnavailable,
            "webProjectsLoading": t.webProjectsLoading,
            "webProjectOpenLabel": t.webProjectOpenLabel,
            "webProjectReading": t.webProjectReading,
            "webProjectDelivered": t.webProjectDelivered,
            "webProjectDeliveredSay": t.webProjectDeliveredSay,
            "webProjectDeliveredNone": t.webProjectDeliveredNone,
            "webProjectLanded": t.webProjectLanded,
            "webProjectLandedSay": t.webProjectLandedSay,
            "webProjectActive": t.webProjectActive,
            "webProjectActiveSay": t.webProjectActiveSay,
            "webProjectAbandoned": t.webProjectAbandoned,
            "webProjectAbandonedSay": t.webProjectAbandonedSay,
            "webProjectBranchGone": t.webProjectBranchGone,
            "webProjectBranchGoneSay": t.webProjectBranchGoneSay,
            "webProjectUnknownOutcome": t.webProjectUnknownOutcome,
            "webProjectUnknownSay": t.webProjectUnknownSay,
            "webProjectBranch": t.webProjectBranch,
            "webProjectEvidence": t.webProjectEvidence,
            "webProjectEvidenceRecord": t.webProjectEvidenceRecord,
            "webProjectEvidenceBranchMerged": t.webProjectEvidenceBranchMerged,
            "webProjectEvidenceBranchEmpty": t.webProjectEvidenceBranchEmpty,
            "webProjectEvidenceBranchBaseUnknown": t.webProjectEvidenceBranchBaseUnknown,
            "webProjectEvidenceBranchAbsent": t.webProjectEvidenceBranchAbsent,
            "webProjectEvidenceBranchUnmerged": t.webProjectEvidenceBranchUnmerged,
            "webProjectEvidenceUnknown": t.webProjectEvidenceUnknown,
            "webProjectRuns": t.webProjectRuns,
            "webProjectSeen": t.webProjectSeen,
            "webProjectNoWorktrees": t.webProjectNoWorktrees,
            "webProjectExcluded": t.webProjectExcluded,
            "webProjectUnattributed": t.webProjectUnattributed,
            "webProjectUnattributedSay": t.webProjectUnattributedSay,
            "webProjectRead": t.webProjectRead,
            "webProjectTruncated": t.webProjectTruncated,
            "webProjectNotFound": t.webProjectNotFound,
            "webProjectAmbiguous": t.webProjectAmbiguous,
            "webProjectBusy": t.webProjectBusy,
            "webProjectFailed": t.webProjectFailed,
        ])

        var response = RemoteServer.Response.json(out)
        // The answer depends on a request header, so a cache that keyed on the URL alone would
        // hand the next reader somebody else's language.
        response.headers["Vary"] = "Accept-Language"
        response.headers["Cache-Control"] = "no-store"
        return response
    }

    /// The page, with the three things done to it that only this end can do.
    ///
    /// The document on disk is the one the dev server and the `file://` mock read, so none of this
    /// may be baked into it — every step below is a substitution made on the way out, and the file
    /// is still a working page without any of them.
    ///
    /// 1. **Every `/app/` URL gets this build's stamp in its path**, so the stylesheets and modules
    ///    can be cached forever (see `asset`). The stamp goes in the *path* rather than a query
    ///    string because a module's own `import "./core/env.js"` resolves against the directory it
    ///    was served from and drops any query — `/app/v123/js/main.js` pulls its imports out of
    ///    `/app/v123/js/`, and a bare `?v=` would have versioned exactly one file out of forty.
    /// 2. **The interface's words are written into the document** instead of fetched. They were a
    ///    round trip that could not even *start* until all forty modules had arrived and run, and
    ///    the page is held blank until they land — so on a phone through the tunnel it was the last
    ///    half-second of the dark rectangle, and often past the two-second fallback that gives up
    ///    and shows English.
    /// 3. **Every module is named in the head**, so the browser asks for all forty at once rather
    ///    than learning about thirty-nine of them after `main.js` has been fetched and parsed.
    static func page(for request: RemoteServer.Request) -> RemoteServer.Response {
        guard let url = Bundle.main.url(forResource: "index", withExtension: "html",
                                        subdirectory: "web"),
              var html = try? String(contentsOf: url, encoding: .utf8) else {
            return .error(404, "not_found", "The web interface is not in this build")
        }
        // Only ever inside `href="` / `src="` — the sole other mention of `/app/` in that document
        // is prose inside a comment, and it is not quoted.
        html = html.replacingOccurrences(of: "\"/app/", with: "\"/app/\(Self.assetVersion)/")
        html = html.replacingOccurrences(of: Self.stringsSlot, with: stringsScript(for: request))
        html = html.replacingOccurrences(of: Self.modulesSlot, with: Self.modulePreloads)
        return RemoteServer.Response(status: 200,
                                     headers: ["Content-Type": "text/html; charset=utf-8"],
                                     body: Data(html.utf8))
    }

    /// The comments in `index.html` that this end writes over. They are comments so that the copy
    /// on disk stays a page: served by `tools/web-serve.py`, or opened off a disk in mock mode,
    /// each one stays exactly what it looks like and the page falls back to fetching its strings.
    private static let stringsSlot = "<!-- clawdline:strings -->"
    private static let modulesSlot = "<!-- clawdline:modules -->"

    /// The path segment that makes this build's copy of a file a different file.
    ///
    /// The executable's modification time, for the reason `/v1/health` gives: it is the one thing
    /// about a build that cannot be forgotten to be bumped. Because the URL changes whenever the
    /// binary does, the files underneath it can be `immutable` — a rebuilt Mac serves a page that
    /// names *different* URLs, so there is no version of this that can hand somebody a stale
    /// stylesheet. `index.html` itself stays `no-store`, which is what makes that true.
    static let assetVersion = "v\(RemoteServer.buildStamp)"

    /// A `modulepreload` for every module except the entry point.
    ///
    /// Read from the bundle rather than from `main.js`'s import list, because that list is the
    /// page's own manifest and copying it here would be a second one to keep in step. The
    /// directory *is* the list: a module nobody imports never runs, so anything in there that is
    /// not reached is already a bug, and preloading it costs a request that was going to be made.
    ///
    /// Order does not matter — this is a fetch hint, and what executes in what order is still
    /// decided by the import graph.
    private static let modulePreloads: String = {
        guard let root = Bundle.main.resourceURL?
                .appendingPathComponent("web", isDirectory: true)
                .appendingPathComponent("app", isDirectory: true)
                .appendingPathComponent("js", isDirectory: true),
              let walk = FileManager.default.enumerator(at: root, includingPropertiesForKeys: nil)
        else { return "" }
        let base = root.standardizedFileURL.path + "/"
        var names: [String] = []
        for case let file as URL in walk where file.pathExtension == "js" {
            let path = file.standardizedFileURL.path
            guard path.hasPrefix(base) else { continue }
            let name = String(path.dropFirst(base.count))
            if name == "main.js" { continue }       // already a `<script type="module">` below
            names.append(name)
        }
        return names.sorted()
            .map { "<link rel=\"modulepreload\" href=\"/app/\(assetVersion)/js/\($0)\">" }
            .joined(separator: "\n")
    }()

    /// The interface's words, as a line of script rather than a request.
    ///
    /// `<` is escaped throughout — a `</script>` anywhere inside a sentence would otherwise end
    /// this element early, and the escape is legal JSON and legal JavaScript wherever it can
    /// appear. The two line separators go with it: JSON allows them raw inside a string and older
    /// JavaScript did not, and the cost of being sure is one more pass.
    static func stringsScript(for request: RemoteServer.Request) -> String {
        guard var json = String(data: strings(for: request).body, encoding: .utf8) else { return "" }
        json = json.replacingOccurrences(of: "<", with: "\\u003c")
        json = json.replacingOccurrences(of: "\u{2028}", with: "\\u2028")
        json = json.replacingOccurrences(of: "\u{2029}", with: "\\u2029")
        return "<script>window.__strings = \(json);</script>"
    }

    /// One file from under `Resources/web/app`, and only from under there.
    ///
    /// **`request.path` is never percent-decoded** — see `Request.init` — so what arrives here is
    /// the literal string the client put on the wire. That makes the safe rule a whitelist rather
    /// than a blacklist: there is no `%2e%2e%2f` to recognise, because `%` is not in the alphabet
    /// below. A segment may only be letters, digits, `.`, `-`, `_`; segments are separated by `/`;
    /// no empty segment, no `.` and no `..`; and the extension has to be one this app knows how to
    /// label. Everything else is a 404 before a path is ever built.
    ///
    /// **A leading `v<digits>` is a version, not a directory.** `page` writes this build's stamp
    /// into every URL it hands out, so what arrives is `/app/v1756100000/js/core/env.js`; the
    /// segment is taken off here and the file is read from where it has always been. It earns the
    /// request a year of `immutable` cache, which is the whole point: a reload then asks for the
    /// document and nothing else, because every stylesheet and module it names is a URL the
    /// browser already has and has been told will never change. A stamp that is *not* this
    /// build's gets the same bytes without the promise — that only happens to a tab left open
    /// across a rebuild, and caching today's file under yesterday's name is how a page ends up
    /// holding a mixture of the two.
    static func asset(_ name: String) -> RemoteServer.Response {
        let types = ["css": "text/css; charset=utf-8",
                     "js": "text/javascript; charset=utf-8"]
        // An explicit set of characters rather than `isLetter` / `isNumber`: those ask a question
        // about Unicode's categories, and the question here is "is this the name of a file we put
        // in the bundle ourselves". It also keeps this compiling on the older Swift in CI.
        let allowed = Set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-_")

        var parts = name.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        var stamp: String?
        if let first = parts.first, first.count > 1, first.hasPrefix("v"),
           first.dropFirst().allSatisfy({ $0.isNumber }) {
            stamp = parts.removeFirst()
        }
        let plain = { (part: String) -> Bool in
            if part.isEmpty || part == "." || part == ".." { return false }
            return part.allSatisfy { allowed.contains($0) }
        }
        guard parts.count >= 1, parts.count <= 4, parts.allSatisfy(plain),
              let file = parts.last,
              let dot = file.lastIndex(of: "."),
              let type = types[String(file[file.index(after: dot)...])] else {
            return .error(404, "not_found", "No such file")
        }

        guard let root = Bundle.main.resourceURL?
                .appendingPathComponent("web", isDirectory: true)
                .appendingPathComponent("app", isDirectory: true) else {
            return .error(404, "not_found", "The web interface is not in this build")
        }
        var url = root
        for part in parts { url.appendPathComponent(part) }
        // Belt, and now braces. The whitelist above already makes this impossible; it is here so
        // that the day somebody widens the alphabet, the file that actually gets read is still
        // under the directory it was resolved from.
        guard url.standardizedFileURL.path.hasPrefix(root.standardizedFileURL.path + "/"),
              let data = try? Data(contentsOf: url) else {
            return .error(404, "not_found", "No such file")
        }
        var headers = ["Content-Type": type]
        if stamp == Self.assetVersion {
            // A year, and `immutable` on top of it: without that word a plain reload revalidates
            // every subresource, which through a tunnel is the entire saving given back. `public`
            // is the truth about these files — they are served to anyone who asks, before any
            // token, and they name no session, repository or path.
            headers["Cache-Control"] = "public, max-age=31536000, immutable"
        }
        return RemoteServer.Response(status: 200, headers: headers, body: data)
    }

    /// The service worker, which exists for one reason: **a page cannot receive a push while it
    /// is closed, and a service worker can.**
    ///
    /// Deliberately tiny, and served rather than shipped as a file, because a service worker is
    /// the one script a browser keeps and re-runs on its own — the smaller its surface, the less
    /// there is to be wrong in a copy somebody installed last month.
    static func serviceWorker() -> RemoteServer.Response {
        let js = #"""
        // Clawdline's service worker. Its whole job is to be awake when the page is not.
        // **The one lever that can reach a page already stuck on an old copy.**
        //
        // For a long time no route set `Cache-Control`, and a browser with no header applies
        // heuristic freshness — Safari on a home-screen web app especially. Serving `no-store`
        // fixes it for every load after the fix, and does nothing for a device that already
        // holds the old copy: it never asks again, so it never learns. Reloading does not help,
        // because the reload is served from the same cache.
        //
        // A worker can break that, and it is the only thing that can. `sw.js` itself is sent
        // `no-cache`, so a browser revalidates it on its own schedule; when this file changes it
        // installs, `skipWaiting` stops it queuing behind open tabs, `clients.claim` takes those
        // tabs over, and from that moment the handler below fetches the page itself instead of
        // the cache. **One more reload after that and the device is out.**
        self.addEventListener("install", function () { self.skipWaiting(); });

        self.addEventListener("activate", function (event) {
            event.waitUntil(
                // Nothing here writes to Cache Storage, so normally there is nothing to delete.
                // It is done anyway, because "nothing wrote to it" is a claim about every version
                // of this file that has ever run on somebody's phone, and this is two lines.
                caches.keys()
                    .then(function (names) { return Promise.all(names.map(function (n) { return caches.delete(n); })); })
                    .catch(function () {})
                    .then(function () { return self.clients.claim(); })
            );
        });

        self.addEventListener("fetch", function (event) {
            // Only the page. Everything else on this origin is either an API answer, which is
            // `no-store` already, a drawn icon, which is worth its day of cache, or a stylesheet
            // or module under a URL naming the build it came from, which is worth a year.
            //
            // **`reload` stays, and this handler stays, and both were measured before that was
            // decided.** Answering a navigation from here means the worker has to be running
            // before the request can go out: cold, that is 35ms in front of a 630ms trip through
            // the tunnel, and warm it is nothing. Navigation preload would win those 35ms back
            // and cost the only thing this is for — a preloaded request goes through the HTTP
            // cache, which is exactly what a device stuck on a pre-`no-store` copy needs bypassed.
            //
            // The half-second is the document's own round trip, and no arrangement of this file
            // removes a trip that has to be made. Serving the document from Cache Storage first
            // would, and that is the one thing this must never do: it now carries the versioned
            // URLs of every stylesheet and module *and* the interface's words, so a stale document
            // is a stale build rather than stale markup, and the only way out of one is the reload
            // the notice in `build.js` exists to avoid taking without asking.
            if (event.request.mode !== "navigate") { return; }
            event.respondWith(
                // The URL rather than the request, and it costs nothing: the header the page now
                // picks its language from is added by the browser to a worker's `fetch` too —
                // checked against a local origin, all four ways of building this request arrive
                // carrying the same `Accept-Language`.
                fetch(event.request.url, { cache: "reload", credentials: "include" })
                    // Offline: hand back whatever the browser would have done on its own, which
                    // is the stale copy. Stale and readable beats an error page.
                    .catch(function () { return fetch(event.request); })
            );
        });

        self.addEventListener("push", function (event) {
            var payload = {};
            try { payload = event.data ? event.data.json() : {}; } catch (e) {}
            event.waitUntil(self.registration.showNotification(payload.title || "Clawdline", {
                body: payload.body || "",
                // The tag collapses repeats about one session into a single line rather than a
                // stack: a phone that was in a pocket for ten minutes should find one notification
                // about a session, not six.
                tag: payload.tag || "clawdline",
                renotify: true,
                // The project's own mark, so two notifications from two projects are told apart
                // before either sentence is read. Falls back to the app's creature, which is what
                // every notification looked like before this existed.
                //
                // **Honoured by Chrome and by Firefox, and ignored by iOS.** Measured on a real
                // iPhone on 2026-08-25: a home-screen web app draws the icon from the manifest
                // whatever this says, whether the mark is fetched from a URL or carried whole
                // inside the sealed message, and `image` is ignored the same way. That matches
                // what everybody else reports — the Apple forum thread about it has no reply and
                // no workaround. So on an iPhone a notification is told apart by its words alone,
                // and there is nothing this end can do about that. This line stays for the
                // platforms that do honour it, and costs one field.
                icon: payload.icon || "/icon-192.png",
                data: { url: payload.url || "/" }
            }));
        });

        self.addEventListener("notificationclick", function (event) {
            event.notification.close();
            var url = (event.notification.data && event.notification.data.url) || "/";
            // Focus a window that is already open before making another one — the point of
            // tapping this is to reach the session, not to collect tabs.
            //
            // **Three things had to be true for this to land on the session and only one of them
            // was.** `navigate()` throws on a client this worker does not control, which is every
            // client until the page has been reloaded once after the worker installed — and a
            // rejected promise here is silent, so it read as "focus worked, routing did not".
            // Second, a URL differing only in its fragment is a same-document navigation: even
            // when `navigate()` succeeds the page is not reloaded, so nothing re-reads it. Third,
            // the page only ever looked at the fragment on first load.
            //
            // So the message is the mechanism and the navigation is the fallback: an open page
            // routes itself, and a cold start gets the fragment the ordinary way.
            event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true })
                .then(function (list) {
                    for (var i = 0; i < list.length; i++) {
                        var client = list[i];
                        if (!("focus" in client)) { continue; }
                        if (client.postMessage) {
                            client.postMessage({ type: "navigate", url: url });
                        }
                        return client.focus().then(function () {
                            // Only for a client we control, and only when the message could not
                            // have done it. `catch` because navigating an uncontrolled client
                            // rejects, and an unhandled rejection here would take the whole
                            // handler down with it.
                            if (!client.postMessage && client.navigate) {
                                return client.navigate(url).catch(function () {});
                            }
                        });
                    }
                    return clients.openWindow(url);
                }));
        });
        """#
        return RemoteServer.Response(status: 200,
                                     headers: ["Content-Type": "text/javascript; charset=utf-8",
                                               "Cache-Control": "no-cache"],
                                     body: Data(js.utf8))
    }

    static func manifest() -> RemoteServer.Response {
        let obj: [String: Any] = [
            "id": "/",
            "name": "Clawdline",
            "short_name": "Clawdline",
            "display": "standalone",
            "background_color": "#0e0e11",
            "theme_color": "#0e0e11",
            "start_url": "/",
            "scope": "/",
            // `maskable` as well as `any`, so a launcher that wants to crop this into its own
            // shape crops the dark tile rather than clipping the creature's ears off.
            "icons": [
                ["src": "/icon-192.png", "sizes": "192x192", "type": "image/png",
                 "purpose": "any maskable"],
                ["src": "/icon-512.png", "sizes": "512x512", "type": "image/png",
                 "purpose": "any maskable"],
            ],
        ]
        let data = (try? JSONSerialization.data(withJSONObject: obj, options: [.prettyPrinted])) ?? Data()
        return RemoteServer.Response(
            status: 200,
            headers: ["Content-Type": "application/manifest+json; charset=utf-8"],
            body: data)
    }
}
