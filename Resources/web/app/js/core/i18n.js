import { esc } from "./esc.js";

/* ==========================================================================
   Words
   Every sentence this interface says, and none of them written here twice.
   The values below are the English, baked in so that a page whose request for
   the real ones failed is still a page somebody can read rather than a page of
   blank labels. Everything else — thirteen other languages — arrives from
   `GET /v1/strings` before the first render, chosen from this browser's own
   `Accept-Language`, and is written straight over the top of them.

   The page translates nothing and decides nothing about language. The Mac is
   where the words live, and there is exactly one copy of them: see the web
   section of `Sources/Strings.swift`.
   ========================================================================== */

export var T = {
    webConnLive: "live",
    webConnConnecting: "connecting",
    webConnRetrying: "retrying in {n}s",
    webConnOffline: "offline",
    webConnLocked: "not paired",
    webConnTipLive: "Streaming from the app",
    webConnTipLocked: "This browser has not been paired with the Mac",
    webConnTipDown: "Not connected — click to try now",
    webCountWorking: "{n} working",
    webCountWaiting: "{n} waiting",
    webCountUnreadable: "{n} unreadable",
    webCountQuietOne: "{n} session, all quiet",
    webCountQuietMany: "{n} sessions, all quiet",
    webCountNone: "no sessions",
    webFilterPlaceholder: "Filter by task, project or tty",
    webFilterLabel: "Filter sessions",
    webListLabel: "Claude Code sessions",
    webPull: "Pull to refresh",
    webPullRelease: "Release to refresh",
    webPullBusy: "Refreshing…",
    webEmptyFilterTitle: "Nothing matches “{q}”",
    webEmptyFilterHint: "Esc clears the filter",
    webEmptyLockedTitle: "This device is not paired",
    webEmptyLockedHint: "The app answered 401 — pair it from Clawdline on the Mac",
    webEmptyNoneHint: "Start one in a terminal and it appears here",
    webEmptyWaitTitle: "Waiting for the app",
    webEmptyWaitHint: "Nothing has arrived on the stream yet",
    webStateUnreadable: "screen could not be read",
    webStateWorking: "working",
    webBack: "Sessions",
    webBackLabel: "Back to the session list",
    webNoSessionOpen: "No session open",
    webOrderTip: "Reverse the transcript (r)",
    webShowOnMac: "Show on Mac",
    webShowOnMacTip: "Bring this session's terminal to the front on the Mac",
    webShowOnMacOff: "The server reports write: false — nothing can be sent yet",
    webShowOnMacAsked: "Asked the Mac to bring it forward",
    webSessionActions: "Session actions",
    webSessionGit: "Git changes",
    webGitTitle: "Git changes",
    webGitClean: "Working tree clean",
    webGitNotRepo: "Not a Git repository",
    webGitFailed: "Could not read Git changes",
    webGitRefresh: "Refresh",
    webGitClose: "Close",
    webGitStaged: "Staged",
    webGitUnstaged: "Unstaged",
    webGitUntracked: "Untracked",
    webGitConflict: "Conflict",
    webEndSession: "Close session",
    webConfirmActionTitle: "Run {action}?",
    webConfirmActionSay: "This sends {action} to the current session.",
    webConfirmEndTitle: "Close this session?",
    webConfirmEndSay: "The assistant will quit, then its terminal tab will close.",
    webClosing: "Closing…",
    webCancel: "Cancel",
    webConfirm: "Confirm",
    webPickSession: "Pick a session on the left.",
    webReading: "Reading…",
    webLoading: "Loading…",
    webTranscriptFailed: "Could not read the transcript",
    webWhoYou: "you",
    webWhoTool: "tool",
    webPending: "Waiting for the Mac…",
    webAttachedImage: "{n} image attached",
    webAttachedImages: "{n} images attached",
    webSteps: "{n} steps",
    webJustNow: "just now",
    webMinutesAgo: "{n}m ago",

    // A question Claude stopped to ask, and the state of a session that is still on one.
    webAskLabel: "asked",
    webAskAny: "Any number of these",
    webWaitingTitle: "This session is waiting for an answer",
    webWaitingSay: "Claude Code is showing a menu on the Mac. Clawdline cannot read a menu from here yet, so the choices are only on that screen.",
    webWaitingSend: "*Sending from here will not type your answer into the menu.* It confirms whichever option the menu has highlighted — which is rarely the one you meant. Answer this one on the Mac.",
    // The same menu, once it could be read. The two above are what is said when it could not.
    webMenuSay: "Tap an answer and it goes straight to the session.",
    webMenuHighlighted: "highlighted",
    webMenuSent: "Answer sent.",
    // What a session has going in the background — see `Subagents` on the Mac.
    webAgents: "Background agents",
    webAgentsCount: "{n} running",
    webAgentDone: "done",
    webAgentFailed: "failed",
    // Reading one of them. Said in the same words on the Mac, so these four have no `web` prefix.
    agentRunning: "running",
    agentTools: "{n} tools",
    agentEmpty: "This agent has not written anything down yet.",
    agentBack: "Session",
    webAgentOpen: "See what this agent did",
    // And what it left running — see `Shells` on the Mac. A command started with
    // `run_in_background` outlives the turn that started it, and the terminal says so once,
    // where that turn ended, and then never again.
    webShells: "Background shells",
    // Opening one of them. A command has no conversation, only the file it is printing into, so
    // it lands in a panel of its own — see `input/shell-panel.js`.
    webShellTitle: "Background shell",
    webShellOpen: "See what this command is printing",
    webShellRunning: "still running",
    webShellEnded: "finished",
    webShellQuiet: "Nothing printed yet.",
    webShellFailed: "Could not read this command's output.",
    webShellClose: "Close",
    // Stopping one — the second thing on this page that destroys something, after ending a
    // session, and behind the same two gates: a device allowed to write, and a second press.
    webShellStop: "Stop",
    webShellStopTitle: "Stop this command?",
    webShellStopSay: "{command} is still running. It is asked to stop, and killed five seconds later if it does not. Nothing it has already done is undone.",
    webShellStopped: "Asked it to stop.",
    webShellStopFailed: "Could not stop it.",
    // Work one session handed to another. A background agent lives inside its session; a task
    // has a whole session of its own, which is why it gets a row rather than a chip in a strip.
    webTaskRoot: "Root",
    webTaskChild: "Child",
    webTaskTasks: "Tasks",
    webTaskDone: "done",
    webTaskFailed: "failed",
    webTaskRunning: "running",
    webScheduleNext: "Next",
    webScheduleMissed: "Last missed",
    webScheduleNoNext: "No next run is scheduled.",
    // The page has been open longer than the app it came from.
    webStale: "Clawdline has been rebuilt on the Mac. This page is the older one.",
    webStaleGo: "Reload",
    webSend: "Send",
    webAttach: "Attach a picture",
    webRemoveShot: "Remove {name}",
    webWriteOpen: "Open a session to write to it.",
    webWriteOff: "*Sending is off* — the server reports `write: false`. The box is wired; it switches itself on when that changes.",
    webShotsOnlyPictures: "Only pictures can go with a message",
    webShotsTooMany: "{n} pictures is the most one message can carry",
    webShotTooBig: "That picture is too big to send, even shrunk",
    webShotsTooBig: "Those pictures are too big to send together",
    webShotUnreadable: "That picture could not be read",
    webShotNeedsSession: "Open a session to send a picture to it",
    // Dictation. The phone records, the Mac transcribes, and the words land in the box — they
    // are never sent from here, which is why nothing below says "sent". Most of these are the
    // reasons it could not happen, because a microphone that does nothing and says nothing is
    // the one failure this feature cannot afford.
    webVoiceStart: "Dictate a message",
    webVoiceStop: "Stop and transcribe",
    webVoiceDone: "Done",
    webVoiceListening: "Listening… {t}",
    webVoiceReading: "Transcribing on the Mac… {n}s",
    webVoiceSlow: "The first one after this Mac reboots loads the model first — about twelve seconds.",
    webVoiceLimit: "Recording stopped at {n} minutes, which is as long as one goes. Transcribing what there is.",
    webVoiceEmpty: "Nothing was heard in that.",
    webVoiceTooShort: "That was too short to hear.",
    webVoiceDenied: "The microphone was refused. Turn it back on in this browser's settings for this site.",
    webVoiceNoMic: "No microphone was found on this device.",
    webVoiceInUse: "The microphone is busy — something else on this device has it.",
    webVoiceInsecure: "A microphone needs https, and this page is not on it. The tunnel provides one; a plain address on the local network does not.",
    webVoiceUnsupported: "This browser cannot record audio.",
    webVoiceBusy: "One of these is being transcribed and another is waiting. Try again in a moment.",
    webVoiceNoBinary: "Whisper is not installed on the Mac, so there is nothing there to transcribe with.",
    webVoiceNoModel: "Whisper is on the Mac but has no model to read with. One ggml file in `~/.cache/whisper` is all it wants.",
    webVoiceFailed: "That could not be transcribed.",
    webHintMove: "move",
    webHintOpen: "open",
    webHintFilter: "filter",
    webHintPane: "pane",
    webKeysLabel: "Keyboard shortcuts",
    webKeysTitle: "Keyboard",
    webKeysMove: "Move through the list — `j` and `k` do the same",
    webKeysOpen: "Open the selected session",
    webKeysFilter: "Filter the list",
    webKeysEscape: "Clear the filter, then step back",
    webKeysList: "Focus the session list",
    webKeysPane: "Show or hide the transcript",
    webKeysEnds: "Top or bottom of the transcript",
    webKeysReverse: "Reverse the transcript order",
    webKeysThis: "This card",
    webKeysFoot: "Anything typed into a text box stays in the text box — the shortcuts stand down while you write.",
    webDoorLabel: "Pair this browser",
    webDoorAskLede: "This browser has not been let in yet.",
    webDoorAskFine: "Ask to pair and a six-digit code appears *on the Mac* — it is deliberately not in the reply to this page. Only somebody at that machine can finish this, and that is the point rather than the hurdle.",
    webDoorName: "Call this device",
    webDoorAsk: "Ask to pair",
    webDoorToPassword: "Sign in with a password instead",
    webDoorCodeLede: "Look at the Mac.",
    webDoorCodeFine: "Clawdline is showing six digits for {left}. Type them here.",
    webDoorTwoMinutes: "two minutes",
    webDoorDigit: "Digit {n}",
    webDoorConfirm: "Pair this device",
    webDoorRestart: "Start again",
    webDoorPasswordLede: "Sign in with the password.",
    webDoorPasswordFine: "The one set in Clawdline on the Mac, under Settings → Remote. If none has been set, pairing is the way in.",
    webDoorPassword: "Password",
    webDoorPasswordGo: "Sign in",
    webDoorToPair: "Use a pairing code instead",
    webDoorAsking: "Asking the Mac…",
    webDoorAskFailed: "Could not ask the Mac.",
    webDoorRateLimited: "Each request puts an alert on the Mac, so there are only three every ten minutes.",
    webDoorSixDigits: "Six digits.",
    webDoorChecking: "Checking…",
    webDoorFinished: "That pairing is finished — five wrong codes. Ask again when you are at the Mac.",
    webDoorWrongCode: "That code is not right.",
    webDoorNeedPassword: "The password, first.",
    webDoorWrongPassword: "That is not the password.",
    webDoorExpired: "That code has expired. Ask again and a fresh one appears on the Mac.",
    webDoorPaired: "Paired — this browser is in",
    webOffline: "Could not reach Clawdline. Is it still running on the Mac?",
    webNotJSON: "The server sent something that is not JSON",
    webRequestFailed: "Request failed",
    webNotifyGo: "Notify me",
    webNotifyAsking: "Asking…",
    webNotifyStop: "Stop",
    webNotifyStopping: "Stopping…",
    webNotifyOff: "when a session is waiting for an answer",
    webNotifyOn: "This device is subscribed. What it gets told about is set on the Mac.",
    webNotifyBlocked: "Notifications are blocked for this site. That switch is in the browser's own settings, not here.",
    webNotifyUnsupported: "This browser cannot show notifications.",
    webNotifyHomeScreen: "Add clawdline to your home screen and open it from there — iOS only sends notifications to an app that lives there.",
    webNotifyOnFailed: "Could not turn notifications on",
    webNotifyOffFailed: "Could not turn notifications off",
    webSettings: "Settings",
    webSettingsNotify: "Notifications",
    // The sort order, which is a preference of this browser and now sits where the rest of them
    // do. `webOrderTip` is still the button's hover text — same words, same job, one row down.
    webSettingsOrder: "Transcript",
    webSettingsOrderSay: "Which end of a transcript to start reading from. It applies to every session, and `r` does the same from a keyboard.",
    webSettingsVersion: "Version {v}",
    webClose: "Close",
    webNotifySheetOff: "This device is not told when a session is waiting.",
    webNotifyTest: "Send a test",
    webNotifyTestSent: "Sent — it should arrive on this device in a moment.",
    webNotifyTestNone: "The Mac has no subscription for this device. Turn notifications off and on again, and this browser will register a fresh one.",
    webNotifyTestFailed: "Could not send a test notification",
    webSending: "Sending…",
    webSendTip: "Return sends · Shift-Return starts a line",

    // Starting one. Every one of these is about choosing among what the Mac offered — there is
    // no path in any of them, because there is no field on that route a path could go in.
    webStart: "Start a session",
    webStartLabel: "Start a session in a project",
    webStartPick: "Where should it start?",
    webStartWith: "Start with",
    webStartEmpty: "Nowhere to start yet. Run Claude Code on the Mac once and it will be here.",
    webStartFilter: "Filter projects",
    webStarting: "Opening a tab…",
    // The tab is open by the time this shows, so it says what happened rather than what might.
    webStartWaiting: "Started. Waiting for it to appear…",
    webStartSlow: "The tab is open, but the session has not reported in. Have a look at the Mac.",
    webStartFailed: "That could not be started.",
    webStartGone: "That project is not on the Mac any more.",
    webStartTerminalClosed: "{app} is not running on the Mac. Open it there and try again.",
    webStartTerminalUnsupported: "A session cannot be started in {app} from here. Run tmux in it and this works.",
    webStartOff: "Starting a session is switched off. Settings → Remote on the Mac turns it on.",
    webResumeWith: "Pick up an earlier one",
    webResumePick: "Which conversation?",
    webResumeFilter: "Filter conversations",
    webResumeEmpty: "Nothing has been recorded in that project yet.",
    webResumeLive: "open now",
    webResumeBack: "Another project",
    webResumeGone: "That conversation is not on the Mac any more.",
    webResumeClaudeOnly: "Only Claude Code keeps conversations that can be picked back up.",
    webResuming: "Picking it back up\u2026",
    webResumeCapped: "Older ones than these are not on this list.",

    // Starting a session by saying it, rather than picking it — see the web section of
    // `Sources/Strings.swift` for the fifteen keys and what each is for. Nothing here may say or
    // imply that a session has started, or that anything has been sent, before the last button —
    // the same rule as the composer's own dictation strings, and for the same reason.
    webCommand: "Say what to start",
    webCommandLabel: "Start a session by saying what to do",
    webCommandSay: "Nothing is sent until you press Start below.",
    webCommandHeard: "What should it do?",
    webCommandThinking: "Thinking…",
    webCommandDraft: "Draft",
    webCommandWhere: "Where",
    webCommandWith: "With",
    webCommandFirst: "First message",
    webCommandGo: "Start",
    webCommandUnsure: "Not sure which project — pick one below.",
    webCommandFailed: "Could not turn that into a draft.",
    webCommandNoPlanner: "This Mac has no Claude Code to turn that into a draft.",
    webCommandBusy: "Already turning one sentence into a draft — try again in a moment.",
    webCommandEmpty: "Heard nothing",

    // Making a schedule — the "+" beside the Schedules list, and the sheet a spoken schedule
    // lands in when the planner could not fill in every field. See the same section of
    // `Sources/Strings.swift` for what each key is for. Same rule as `webCommand*` above: nothing
    // here may say or imply that a schedule exists before the Create button is pressed.
    webScheduleNew: "New schedule",
    webScheduleNewSay: "Nothing is scheduled until you press Create below.",
    webScheduleTitle: "Title",
    webScheduleAt: "At",
    webScheduleOn: "On",
    webScheduleWhere: "Where",
    webScheduleWith: "With",
    webScheduleFirst: "First message",
    webScheduleMore: "More",
    webScheduleWhenDone: "When it finishes",
    webScheduleCloseSuccess: "On success",
    webScheduleCloseAlways: "Always",
    webScheduleCloseNever: "Never",
    webScheduleEnabled: "Enabled",
    webScheduleDisabled: "Disabled",
    webScheduleNotify: "Notify if it fails",
    webScheduleCatchUp: "Catch up within (hours)",
    webScheduleTimeout: "Give up after (minutes)",
    webScheduleCreate: "Create",
    webScheduleCreated: "Schedule created.",
    webScheduleFailed: "Could not create the schedule.",
    webScheduleNeedsTime: "Didn't catch a time — set one.",
    webScheduleNeedsPlace: "Didn't catch which project — pick one.",
    webScheduleDaily: "Daily",
    webScheduleSun: "Sun",
    webScheduleMon: "Mon",
    webScheduleTue: "Tue",
    webScheduleWed: "Wed",
    webScheduleThu: "Thu",
    webScheduleFri: "Fri",
    webScheduleSat: "Sat",

    // Where this project can be opened. Three of these exist to be honest about a row that
    // cannot be opened from wherever this page is being read — which is the only reason the
    // list is worth having rather than a page of links that time out.
    webLinks: "Links",
    webLinksTip: "Where this project can be opened",
    webLinksPick: "Everything this project has an address for.",
    webLinksEmpty: "This project has nothing with an address — no site, no deploy, and no dev stack running.",
    webLinksFailed: "Could not read this project's links.",
    webLinksLocal: "On the Mac's own network only. It will not open from here.",
    webLinksFile: "A file on the Mac. It opens there, not in this browser.",
    webLinksCopy: "Copy path",
    webLinksCopied: "Copied.",
    webLinksCopyFailed: "This browser would not let the page copy that.",
    // That thing's own health, in its own words, because "ok" means a passing run and a serving
    // port and there is one row type for both.
    webLinkOk: "ok",
    webLinkFail: "failing",
    webLinkDown: "down",
    webLinkRunning: "running",
    webSessionInfo: "Session info",
    webInfoTitle: "About this session",
    webInfoSession: "This session",
    webInfoAssistant: "Assistant",
    webInfoModel: "Model",
    webInfoSessionId: "Session id",
    webInfoDirectory: "Directory",
    webInfoRunningFor: "Running for",
    webInfoUsage: "Token use",
    webInfoInput: "Input",
    webInfoOutput: "Output",
    webInfoCacheRead: "Cache read",
    webInfoCacheWrite: "Cache write",
    webInfoTotal: "Total",
    webInfoCost: "Estimated cost",
    webInfoNoUsage: "No transcript found for this session yet.",
    webInfoLimits: "Plan limits",
    webInfoLimitHit: "limit reached",
    webInfoResets: "resets {when}",
    webInfoUnknown: "unknown",
    webInfoFiles: "Files",
    webInfoBranch: "Branch",
    webInfoStaged: "Staged",
    webInfoUnstaged: "Changed",
    webInfoUntracked: "Untracked",
    webInfoConflict: "Conflicts",
    webInfoClean: "Nothing uncommitted.",
    webInfoNotRepo: "Not a git repository.",
    webInfoDeploy: "Last deploy",
    webInfoNoDeploy: "No deploy recorded for this project.",
    webInfoFailed: "Could not read this session's info.",
    webInfoBusy: "The Mac is busy reading other sessions — try again in a moment.",
    webInfoRefresh: "Refresh",
    webInfoTokens: "tokens",
    webInfoSwitchModel: "Switch model",
    webInfoModelOther: "another model\u2026",
    webInfoModelSent: "Sent /model {model}. Confirming\u2026",
    webInfoModelBusy: "Switching waits until the session is idle.",
    webInfoSwitchPermission: "Switch permission mode",
    webInfoPermissionAuto: "Auto",
    webInfoPermissionManual: "Manual",
    webInfoPermissionAcceptEdits: "Accept edits",
    webInfoPermissionPlan: "Plan",
    webInfoPermissionUnreadable: "Could not read the current mode.",
    webInfoPermissionSent: "Switched to {mode}. Confirming\u2026",
    webInfoPermissionBusy: "A permission mode change is being sent.",
    webInfoLimitsClaude: "Claude Code only writes a window down once it is spent, so nothing is known until then.",
    webInfoCopied: "Session id copied.",
    webInfoAsOf: "as of {when}",
    webInfoWhyUnknown: "Why this is unknown, and how to get the numbers",

    // Said in both places, so said once — the bar's own words, arriving here too.
    placeholder: "Message Claude Code…",
    noSession: "No Claude Code session found",
    noOutput: "Nothing to read from this session yet.",
    sessionWaiting: "waiting for you",
    // Two of them, because English counts. See `sessionShellOne` in `Sources/Strings.swift`.
    sessionShellOne: "1 shell running",
    sessionShellMany: "{n} shells running",
    sendFailed: "Could not send",
    hintList: "list",
    hintKeys: "keys",
    hintOrder: "reverse",
    webOrderNewest: "Newest first",
    webOrderOldest: "Oldest first"
};

/**
 * The strings, as the server sent them.
 *
 * Only keys this page already knows are taken. A payload is not a place to learn new names from
 * — an unknown key is a version of the app that knows something this page does not, and the
 * right thing to do with it is nothing.
 */
export function applyStrings(data) {
    if (!data) return;
    for (var key in T) {
        if (typeof data[key] === "string" && data[key]) T[key] = data[key];
    }
    // What a screen reader picks a voice from, and what a browser offers to translate against.
    if (data.lang) document.documentElement.lang = data.lang;
    // None of the fourteen is written right to left, so this is `ltr` every time today. It is
    // set from the answer rather than left alone so that the day one is added, the page is
    // already laying itself out the way that language is read.
    if (data.dir) document.documentElement.dir = data.dir;
}

/**
 * `{n}`, `{q}`, `{left}` — the holes in a string, filled in.
 *
 * **Order matters when the result is going into HTML.** Fill first and escape the whole thing
 * afterwards — `esc(fill(s, …))` — and one pass covers both the copy and whatever went into it.
 * Escaping first and filling second would put an unescaped value into an escaped string, which
 * is the bug this note exists to prevent. A hole with nothing to put in it is left as it was
 * written, so a missing value shows up as `{n}` rather than as a hole in a sentence.
 */
export function fill(s, holes) {
    return String(s == null ? "" : s).replace(/\{(\w+)\}/g, function (all, key) {
        return holes && holes[key] != null ? String(holes[key]) : all;
    });
}

/**
 * Interface copy, ready to be written as HTML.
 *
 * Two markers and no more: `*emphasis*` and `` `something typed at a machine` ``. They are
 * turned into tags **after** the string has been escaped, which is what makes them safe — by
 * then the only `<` and `>` in the string are the ones put there on this line, and a translation
 * that contains a tag has already had it turned into the text somebody typed.
 *
 * Only for strings that carry no filled-in value: a value is escaped, but it is not copy, and it
 * has no business being read for markers. Those go through `esc(fill(…))` instead.
 */
export function words(s) {
    return esc(s)
        .replace(/\*([^*]+)\*/g, "<b>$1</b>")
        .replace(/`([^`]+)`/g, "<code>$1</code>");
}
