import Foundation

/// Every piece of UI copy, one implementation per language.
///
/// Why a protocol instead of a `.strings` catalog: the compiler refuses to build a language that
/// is missing a string. With `.strings` files a missing key is a blank label at runtime, and
/// nobody notices until somebody who reads that language complains — which is to say, after it
/// has already shipped.
///
/// **Adding a language:** copy `Copy+English.swift` to `Copy+<Language>.swift`, translate the
/// values, and add one line to `L.catalog`. Nothing else, and the build tells you if you missed
/// a string.
///
/// Two rules for the copy itself:
///
/// - **Say what happened, not what went wrong internally.** "Could not send" is for a person;
///   "AppleScript returned -1728" is for a log.
/// - **Keep the hint words short.** They sit in one row along the bottom of the card, and a long
///   word there pushes another one off the end rather than wrapping.
protocol Copy {
    // Input field
    var placeholder: String { get }

    // Hint row
    var hintSend: String { get }
    var hintNewline: String { get }
    var hintSwitch: String { get }
    var hintList: String { get }
    var hintMascot: String { get }
    var hintOutput: String { get }
    var hintFullscreen: String { get }
    var hintKeys: String { get }
    var hintTextSize: String { get }
    var hintOrder: String { get }
    var hintVoice: String { get }
    func voiceListening(onDevice: Bool) -> String
    var voiceNoPermission: String { get }
    var voiceUnavailable: String { get }
    func voiceTranscribing(seconds: Double) -> String
    var whisperMissing: String { get }
    var whisperNothingHeard: String { get }
    func dictationStatus(_ status: Whisper.Status) -> String
    func voiceListeningWhisper() -> String

    // Target state
    var scanning: String { get }
    var noSession: String { get }
    var nothingToSend: String { get }
    var sendFailed: String { get }
    var itermSilent: String { get }
    var scriptMissing: String { get }
    var cannotList: String { get }
    var noOutput: String { get }
    func outputSize(_ pt: Int) -> String
    func foldedTools(_ count: Int) -> String
    func outputOrder(newestFirst: Bool) -> String
    func backlogNow(_ count: Int) -> String
    func dropped(_ count: Int) -> String
    /// Shown once before a project's `.devstack.json` command is run for the first time.
    ///
    /// **The command itself goes in the message.** What that file names is arbitrary code out of
    /// a repository, and "do you trust this workspace" is a question nobody has the information
    /// to answer — the line that is about to run is the only honest way to ask.
    func stackConfirm(_ command: String) -> String
    /// The key-row word for the stack list. One short noun — the row is one line wide.
    var hintStacks: String { get }
    /// Hover text for the servers mark in the footer.
    ///
    /// It sits next to the session counter, and "6/6" beside "3/7" reads as two of the same
    /// thing when they are not related at all — so say which is which in words.
    func stackTip(up: Int, total: Int) -> String
    var stackTipUnknown: String { get }
    /// The row's own words for "we have not been allowed to ask yet".
    ///
    /// **A mark on its own is not enough here.** A grey square next to a green one reads as
    /// "down" — that misreading happened the first day this shipped, and it sent someone
    /// looking for an outage that did not exist. Untrusted is not a health state; it must not
    /// look like one.
    var stackUntrusted: String { get }
    /// The words on a row's action button.
    ///
    /// **A row must not be an invisible button.** The first version made the whole row pressable
    /// and worked out what to do from the state — so one click silently took a public site down
    /// for a minute, with nothing on screen having offered to. What the press will do has to be
    /// written on the thing you press.
    var stackActionStart: String { get }
    var stackActionRestart: String { get }
    /// Stopping was on a modifier key and nothing else for a while — which is to say it was not
    /// really there. A capability only reachable by a keystroke nobody has been told about is
    /// one the person will conclude does not exist, and go looking for a terminal.
    var stackActionStop: String { get }
    /// The one button on the row that changes nothing — so it needs no confirmation, and it is
    /// the answer to "where do I see what these servers printed".
    var stackActionLogs: String { get }
    /// The log pane's tabs: one per process, plus this one for the lot.
    var stackLogAll: String { get }
    /// The way out. A pane you can get into and not out of is a trap, and ⌘J alone did not read
    /// as an exit once the thing on screen was no longer a transcript.
    var stackLogBack: String { get }
    var stackActionAllow: String { get }
    /// What the button says once it is armed and waiting to be confirmed.
    var stackActionAgain: String { get }
    func sessionTip(index: Int, total: Int) -> String
    /// What a session row says when there is a question on its screen and nobody has answered it.
    ///
    /// **Short, and addressed to the reader.** It sits after a tab title on a one-line row, so a
    /// long phrase pushes the title — which is what the row is for — off the end. "Waiting" on
    /// its own is the wrong word: everything in that list is waiting for something. This one is
    /// waiting for *you*, and that is the whole of what the row has to get across.
    var sessionWaiting: String { get }
    /// What the island says when a session that had been running stops.
    ///
    /// **One word.** It is drawn in small capitals above a task name in a strip about as wide as
    /// a notch, and it is on screen for three seconds — a sentence there is a sentence nobody
    /// finishes reading.
    var islandDone: String { get }
    /// The way out of the island's menu and into the whole list.
    ///
    /// That menu offers what is *running*; this is everything, which is a different question and
    /// has to say so — "more…" would read as "the rest of the running ones".
    var islandAllSessions: String { get }
    /// Hover text for the menu bar mark. Names the sessions, because the mark can only say that
    /// *something* wants you and the next question is always which.
    func statusWaiting(_ labels: [String]) -> String
    func statusWorking(_ count: Int) -> String

    // Settings window
    //
    // Labels for controls, so: short, and a noun rather than a sentence. The row already says
    // what it is by what it does; the label only has to name it.
    var settingsTitle: String { get }
    var settingsGeneral: String { get }
    var settingsBar: String { get }
    var settingsReading: String { get }
    var settingsVoice: String { get }
    var settingsHotkey: String { get }
    /// Shown on the hotkey button while it is listening for one.
    var settingsRecording: String { get }
    var settingsScope: String { get }
    var settingsScopeGlobal: String { get }
    var settingsScopeHint: String { get }
    /// The words on the settings window's app picker.
    ///
    /// It used to be a text field asking for `com.googlecode.iterm2`, which is a string with no
    /// discoverable spelling — so these name applications the way a person does, and "open right
    /// now" heads the short list of what is running, which is where the answer nearly always is.
    var settingsScopeAdd: String { get }
    var settingsScopeChoose: String { get }
    var settingsScopeRunning: String { get }
    var settingsScopeRemove: String { get }
    var settingsLanguage: String { get }
    /// A switch label has to name its subject.
    ///
    /// "Come back with the terminal" read as a fragment with nothing in front of the verb, which
    /// in a language that does not carry an implied English subject is not a sentence at all. Both
    /// of these say what the thing is that does the coming back or the following.
    var settingsReopen: String { get }
    var settingsReopenHint: String { get }
    var settingsFollow: String { get }
    var settingsFollowHint: String { get }
    var settingsNotch: String { get }
    var settingsNotchHint: String { get }
    var settingsPosition: String { get }
    var settingsWidth: String { get }
    var settingsOpacity: String { get }
    var settingsShow: String { get }
    var settingsPaneHeight: String { get }
    var settingsTextSize: String { get }
    var settingsPaneFont: String { get }
    var settingsBlur: String { get }
    var settingsNewestFirst: String { get }
    var settingsEngine: String { get }
    var settingsSettle: String { get }
    var settingsStop: String { get }
    var settingsAuto: String { get }
    var settingsTranscript: String { get }
    var settingsTerminal: String { get }
    var settingsOff: String { get }
    // Claude Code hooks — see Sources/HookBridge.swift
    var settingsHooks: String { get }
    var settingsHooksHint: String { get }
    var settingsHooksInstall: String { get }
    var settingsHooksRemove: String { get }
    /// The three states worth telling apart. "Installed" and "installed and actually running"
    /// look identical from a checkbox, and the gap between them is where a wrong shell, a
    /// settings file somebody else manages, or a plain typo in a path would hide.
    var settingsHooksOff: String { get }
    var settingsHooksOn: String { get }
    var settingsHooksLive: String { get }
    /// The other direction: this app telling somebody else's program that a session moved.
    ///
    /// It has no control, because `on_state_change` is an argument list and a single text box
    /// invites the word-splitting that ruins a path with a space in it. But it is the extension
    /// point everything else hangs off, and a feature nobody can find is a feature nobody has —
    /// so the hooks tab says what it is and where it lives. See Sources/StateHook.swift.
    var settingsStateHook: String { get }
    var settingsStateHookHint: String { get }

    // Remote access — see Sources/RemoteServer.swift and Sources/RemoteAuth.swift
    var settingsRemote: String { get }
    var settingsRemoteServe: String { get }
    var settingsRemoteHint: String { get }
    var settingsRemoteDevices: String { get }
    var settingsRemoteNoDevices: String { get }
    var settingsRemoteRevokeAll: String { get }
    var settingsRemoteOpen: String { get }
    /// Shown on the Mac when something asks to pair. The code is deliberately only ever here —
    /// whoever asked cannot finish without walking to this screen, and that is the whole of the
    /// security property.
    func pairingAsks(_ device: String) -> String
    func pairingCode(_ code: String) -> String
    var pairingIgnore: String { get }

    var settingsTunnel: String { get }
    var settingsTunnelQuick: String { get }
    var settingsTunnelNamed: String { get }
    var settingsTunnelHostname: String { get }
    var settingsTunnelHint: String { get }
    var settingsRemoteWrite: String { get }
    var settingsRemoteWriteHint: String { get }
    var settingsRemotePhone: String { get }
    var settingsRemotePhoneHint: String { get }
    var pairingScanTitle: String { get }
    /// The body of the one notification this app sends. The title is the project, so this is the
    /// half that says what happened — short, because a lock screen truncates and the important
    /// word should not be the one that gets cut.
    var pushWaiting: String { get }
    /// The body of `clawdline://push?test=1`. It has to be unmistakably a test — one that
    /// reads like a real notification teaches somebody to distrust the real ones.
    var pushTest: String { get }
    /// The body of the "a long turn finished" notification, off by default. Past tense, because
    /// by the time this arrives the thing is done — "finished" and "is finishing" are a lock
    /// screen apart and only one of them means you can stop waiting.
    var pushFinished: String { get }
    /// The two ends of a deploy. Kept apart rather than one string with a word swapped in: the
    /// languages this speaks do not all agree on where that word goes, and the failure is the one
    /// that has to be unmistakable at a glance.
    var pushDeployOk: String { get }
    var pushDeployFail: String { get }
    var settingsPushFinish: String { get }
    var settingsPushFinishHint: String { get }
    var settingsPushDeploy: String { get }
    var settingsPushDeployHint: String { get }
    /// A number of seconds, as a settings row shows it.
    ///
    /// Here rather than as a bare "s" because it is the one unit in that window that reads as a
    /// foreign word: "1.8 s" is not how a duration is written in every language that this speaks.
    func settingsSeconds(_ value: Double) -> String

    // The web interface — see Resources/web/index.html and Sources/RemoteServer.swift
    //
    // Every word the page says comes from here, fetched once from `GET /v1/strings` before the
    // first render. The page holds an English copy of all of it as a fallback and translates
    // nothing itself: the person holding the phone is not necessarily the person the Mac belongs
    // to, and a second set of these sentences living in the HTML would be a second thing to
    // translate and the first thing to forget.
    //
    // Three conventions, and between them they are all the markup these strings may carry:
    //
    // - **`{name}` is a hole the page fills in** — a count, the words somebody typed into the
    //   filter, how long a pairing code has left. Keep every one that is in the English, and put
    //   it where the sentence wants it; one that goes missing is a number that never arrives.
    // - **`*asterisks*` mark the emphasised words**, drawn bold. Move the pair to whichever
    //   words carry the weight in your language rather than to the same position.
    // - **`` `backticks` `` mark something typed at a machine** — a flag, a key on a keyboard —
    //   drawn in the page's code face. What is between them is not translated.
    //
    // Nothing else is markup. Every one of these is escaped on the way to the screen, so a tag
    // written into a translation arrives as the tag somebody typed rather than as a tag.

    /// The connection chip, top right. Four words for four states, and they are lower case
    /// because the chip is furniture: it is read when something is wrong and ignored otherwise.
    var webConnLive: String { get }
    var webConnConnecting: String { get }
    var webConnRetrying: String { get }
    var webConnOffline: String { get }
    var webConnLocked: String { get }
    var webConnTipLive: String { get }
    var webConnTipLocked: String { get }
    var webConnTipDown: String { get }

    /// The header's counts. Sentence fragments on purpose — they are joined with a separator
    /// into one line, so none of them may end in a full stop.
    var webCountWorking: String { get }
    var webCountWaiting: String { get }
    var webCountUnreadable: String { get }
    /// Said when nothing is working, waiting or unreadable, which is the ordinary state of a
    /// machine. Two of them because English counts one session differently from five; a language
    /// that does not can write the same sentence twice.
    var webCountQuietOne: String { get }
    var webCountQuietMany: String { get }
    var webCountNone: String { get }

    var webFilterPlaceholder: String { get }
    /// Read aloud rather than seen: the box beside it says what it is by being a search box.
    var webFilterLabel: String { get }
    var webListLabel: String { get }
    var webPull: String { get }
    var webPullRelease: String { get }
    var webPullBusy: String { get }
    /// The empty states, each a heading and a line under it. There are four, and telling them
    /// apart is the whole point: no sessions, none that match what was typed, not paired yet,
    /// and nothing has arrived yet — one of those is somebody's fault and three are not.
    var webEmptyFilterTitle: String { get }
    var webEmptyFilterHint: String { get }
    var webEmptyLockedTitle: String { get }
    var webEmptyLockedHint: String { get }
    var webEmptyNoneHint: String { get }
    var webEmptyWaitTitle: String { get }
    var webEmptyWaitHint: String { get }
    /// A row whose screen could not be read. Not "idle": a session that could not be looked at
    /// is not a session doing nothing, and drawing it as one is a confident wrong answer.
    var webStateUnreadable: String { get }
    var webStateWorking: String { get }

    /// The way back to the list on a phone. A chevron is drawn in front of it, so this is the
    /// word alone.
    var webBack: String { get }
    var webBackLabel: String { get }
    var webNoSessionOpen: String { get }
    var webOrderTip: String { get }
    /// The button that brings a session's terminal to the front **on the Mac**.
    ///
    /// It was called "Reveal", which is a Finder verb that does not survive being taken out of
    /// Finder — and it sat next to a sort control, so the two read as the same kind of thing.
    /// Name it after what it does, and say where it happens: the press has its effect on another
    /// machine, which is the one fact somebody holding a phone needs before they press it.
    var webShowOnMac: String { get }
    var webShowOnMacTip: String { get }
    var webShowOnMacOff: String { get }
    var webShowOnMacAsked: String { get }
    var webPickSession: String { get }
    /// Read out where a skeleton is drawn, and never seen — the skeleton is the visible half.
    var webReading: String { get }
    var webLoading: String { get }
    var webTranscriptFailed: String { get }
    /// Who said a line, in the transcript's left margin. Drawn in small capitals, so: one short
    /// word each. Claude's own name is not here — it is a name, and it is not translated.
    var webWhoYou: String { get }
    var webWhoTool: String { get }
    /// How many tool calls a folded run stands for. The web's spelling of ``foldedTools``, which
    /// cannot cross a JSON boundary as a function.
    var webSteps: String { get }
    var webJustNow: String { get }
    var webMinutesAgo: String { get }

    var webSend: String { get }
    var webAttach: String { get }
    var webRemoveShot: String { get }
    var webWriteOpen: String { get }
    /// Why the composer will not take what you type. `write: false` is the server's own answer,
    /// quoted rather than described, so that somebody searching for it finds this sentence.
    var webWriteOff: String { get }
    var webShotsOnlyPictures: String { get }
    var webShotsTooMany: String { get }
    var webShotTooBig: String { get }
    var webShotsTooBig: String { get }
    var webShotUnreadable: String { get }
    var webShotNeedsSession: String { get }

    /// The key row along the bottom of a desktop window. Same rule as ``hintSend`` and the rest:
    /// one row, no wrapping, so a long word here pushes another one off the end.
    var webHintMove: String { get }
    var webHintOpen: String { get }
    var webHintFilter: String { get }
    var webHintPane: String { get }

    var webKeysLabel: String { get }
    var webKeysTitle: String { get }
    var webKeysMove: String { get }
    var webKeysOpen: String { get }
    var webKeysFilter: String { get }
    var webKeysEscape: String { get }
    var webKeysList: String { get }
    var webKeysPane: String { get }
    var webKeysEnds: String { get }
    var webKeysReverse: String { get }
    var webKeysThis: String { get }
    var webKeysFoot: String { get }

    // The door — the whole page until the server knows who is asking.
    var webDoorLabel: String { get }
    var webDoorAskLede: String { get }
    /// The one paragraph that has to land. **The code is shown on the Mac and nowhere else**, and
    /// that is not a hurdle to apologise for — it is the entire security property, so say it as
    /// the reason it is.
    var webDoorAskFine: String { get }
    var webDoorName: String { get }
    var webDoorAsk: String { get }
    var webDoorToPassword: String { get }
    var webDoorCodeLede: String { get }
    /// `{left}` is a clock counting down, and it is replaced every second — so the words on
    /// either side of it have to read with `1:58` in the middle as well as with the first
    /// version of it, which is ``webDoorTwoMinutes``.
    var webDoorCodeFine: String { get }
    var webDoorTwoMinutes: String { get }
    var webDoorDigit: String { get }
    var webDoorConfirm: String { get }
    var webDoorRestart: String { get }
    var webDoorPasswordLede: String { get }
    var webDoorPasswordFine: String { get }
    var webDoorPassword: String { get }
    var webDoorPasswordGo: String { get }
    var webDoorToPair: String { get }
    var webDoorAsking: String { get }
    var webDoorAskFailed: String { get }
    /// Added to the server's own refusal when somebody has asked three times. Without it,
    /// "rate limited" reads as a fault rather than as a door working correctly.
    var webDoorRateLimited: String { get }
    var webDoorSixDigits: String { get }
    var webDoorChecking: String { get }
    var webDoorFinished: String { get }
    var webDoorWrongCode: String { get }
    var webDoorNeedPassword: String { get }
    var webDoorWrongPassword: String { get }
    var webDoorExpired: String { get }
    var webDoorPaired: String { get }

    /// What a request that never arrived says. The browser's own words for this are "Failed to
    /// fetch", which is not an explanation anybody can act on.
    var webOffline: String { get }
    var webNotJSON: String { get }
    var webRequestFailed: String { get }

    // Notifications. Four states and only one of them is "on" — a button that has been pressed
    // and did nothing is the worst of them, so each state says which one it is in words.
    var webNotifyGo: String { get }
    var webNotifyAsking: String { get }
    var webNotifyStop: String { get }
    var webNotifyStopping: String { get }
    /// The line under the button while notifications are off. It finishes the sentence the
    /// button starts — "Notify me" *when a session is waiting for an answer* — so it begins in
    /// lower case and carries no full stop.
    var webNotifyOff: String { get }
    /// Shown in the settings sheet once this device is subscribed.
    ///
    /// **It deliberately does not say what a notification is for.** What sets one off is decided
    /// on the Mac — a session waiting, a long turn finishing, a deploy ending, and more later —
    /// and a page that lists them from memory is a page that will be wrong about it. Where the
    /// switches are is the useful half, and it is the half this can be sure of.
    var webNotifyOn: String { get }
    var webNotifyBlocked: String { get }
    var webNotifyUnsupported: String { get }
    /// **On iOS this is the entire feature until it has been read.** The push API is absent from
    /// a Safari tab — not broken, absent — so there is no button to press and nothing to explain
    /// afterwards; this sentence is shown instead of one.
    var webNotifyHomeScreen: String { get }
    var webNotifyOnFailed: String { get }
    var webNotifyOffFailed: String { get }

    // The settings sheet, behind the wordmark in the top left.
    //
    // **Nothing app-level is allowed to charge rent on the list.** Turning notifications off is a
    // once-a-year press, and as a permanent footer it was taking a row of every phone screen from
    // the only thing the page is for. What is still in the flow is the one state that is asking
    // to be pressed; everything else lives in here.
    var webSettings: String { get }
    var webSettingsNotify: String { get }
    var webSettingsVersion: String { get }
    var webClose: String { get }
    /// The sheet's version of ``webNotifyOff``. That one finishes the button's sentence and is a
    /// fragment; in here there is no button above it to finish, so it is a sentence of its own.
    var webNotifySheetOff: String { get }
    /// **The answer to "did that actually work".** The alternative was waiting for a real session
    /// to need you, which is a long way to go to find out whether a key was minted correctly —
    /// and the moment permission has just been granted is exactly when somebody wants proof.
    var webNotifyTest: String { get }
    var webNotifyTestSent: String { get }
    /// What a 409 from `/v1/push/test` means, said plainly. It is not a failure to explain away:
    /// this browser thinks notifications are on and the Mac has nothing to send to, and the way
    /// out of that is one sentence long.
    var webNotifyTestNone: String { get }
    var webNotifyTestFailed: String { get }
    /// On the send button while a message is in flight, and on the test button too. It used to
    /// say "Send" throughout, which on a phone over a tunnel is a second of a page that looks
    /// like it did nothing — and a second is long enough to press it again.
    var webSending: String { get }
    /// Hover text on the send button, and **only where there is a keyboard to do it with**. On a
    /// touch screen Return is a new line and the button is how you send, the way it is in every
    /// messaging app — so there is nothing to explain there and this is not shown.
    var webSendTip: String { get }

    // Starting a session from the page — see `GET /v1/places` and `POST /v1/places/:id/start`.
    //
    // The page never names a directory: it shows a list the Mac built and sends back an id. So
    // these words are about *choosing among what is offered*, never about typing a path, and a
    // translation that invites somebody to enter one is describing a thing that does not exist.
    var webStart: String { get }
    var webStartLabel: String { get }
    var webStartPick: String { get }
    /// Shown when the Mac has no projects to offer. It has to say what would put one there —
    /// an empty list with no explanation reads as a broken feature rather than a new machine.
    var webStartEmpty: String { get }
    var webStartFilter: String { get }
    var webStarting: String { get }
    /// The gap between the tab existing and the session reporting in. **The tab is already open
    /// by the time this shows**, so the words must not suggest the request is still in doubt —
    /// somebody who reads this as "it might not have worked" presses the button again and gets a
    /// second tab.
    var webStartWaiting: String { get }
    var webStartSlow: String { get }
    var webStartFailed: String { get }
    var webStartGone: String { get }
    /// `{app}` is the terminal's macOS display name, substituted by the page.
    var webStartTerminalClosed: String { get }
    var webStartTerminalUnsupported: String { get }
    var webStartOff: String { get }

    // The addresses a project has — see `GET /v1/sessions/:id/links` and the sheet the Links
    // chip opens. `webLinksLocal` and `webLinksFile` are the two that matter: one address only
    // resolves on the Mac's network and the other is not a web address at all, and a link that
    // silently does nothing when tapped is worse than a line of text that explains itself.
    var webLinks: String { get }
    var webLinksTip: String { get }
    var webLinksPick: String { get }
    var webLinksEmpty: String { get }
    var webLinksFailed: String { get }
    var webLinksLocal: String { get }
    var webLinksFile: String { get }
    var webLinksCopy: String { get }
    var webLinksCopied: String { get }
    var webLinksCopyFailed: String { get }
    var webLinkOk: String { get }
    var webLinkFail: String { get }
    var webLinkDown: String { get }
    var webLinkRunning: String { get }
    var webSettingsOrder: String { get }
    var webSettingsOrderSay: String { get }

    // A question with a menu on it — see `Transcript.askPayload` and the ask block in the page.
    //
    // The important one is `webWaitingSend`. A picker throws away a bracketed paste and then acts
    // on the Return after it, so typing from a phone **confirms whatever row is highlighted**.
    // The server now refuses that outright; this is the sentence that says why, and its emphasis
    // is load-bearing rather than decorative.
    var webAskLabel: String { get }
    var webAskAny: String { get }
    var webWaitingTitle: String { get }
    var webWaitingSay: String { get }
    var webWaitingSend: String { get }
    /// A page that has been open since before the Mac was rebuilt. It does not reload itself —
    /// somebody may be mid-sentence in the composer.
    var webStale: String { get }
    var webStaleGo: String { get }

    // Menu bar
    var menuOpen: String { get }
    var menuReveal: String { get }
    var menuMascot: String { get }
    var menuLogin: String { get }
    var menuEditConfig: String { get }
    var menuReload: String { get }
    var menuQuit: String { get }
    var menuNoTarget: String { get }

    // Alerts
    func hotkeyFailedTitle(_ combo: String) -> String
    func hotkeyFailedBody(_ configPath: String) -> String
    var loginFailed: String { get }
}

/// The reference values, for the extension below and for nothing else.
enum L {
    /// The active language. Cached because it is read on every redraw.
    private(set) static var t: Copy = pick()

    /// Call this after the config changes.
    static func reload() { t = pick() }

    /// Matched by prefix, so **more specific tags come first** — `zh-Hans` must not fall into the
    /// Traditional bucket, and it would, because `"zh-Hans-CN".hasPrefix("zh-Hant")` is false but
    /// a bare `"zh"` entry ahead of it would swallow both.
    ///
    /// One entry per script or region only where the words actually differ. Portuguese is one
    /// entry because the interface words are the same on both sides of the Atlantic; Chinese is
    /// two because they are not written in the same characters.
    static let catalog: [(tag: String, copy: Copy)] = [
        ("en", English()),
        ("zh-Hant", TraditionalChinese()),
        ("zh-TW", TraditionalChinese()),
        ("zh-HK", TraditionalChinese()),
        ("zh-MO", TraditionalChinese()),
        ("zh-Hans", SimplifiedChinese()),
        ("zh-CN", SimplifiedChinese()),
        ("zh-SG", SimplifiedChinese()),
        ("zh", SimplifiedChinese()),
        ("ja", Japanese()),
        ("ko", Korean()),
        ("es", Spanish()),
        ("pt", Portuguese()),
        ("fr", French()),
        ("de", German()),
        ("ru", Russian()),
        ("it", Italian()),
        ("hi", Hindi()),
        ("id", Indonesian()),
        ("tr", Turkish()),
    ]

    /// The BCP-47 tag for a `Copy`, for the page's `<html lang>`.
    ///
    /// Read back out of ``catalog`` rather than carried on `Copy` itself: the catalog is already
    /// the one list of what this app speaks, and a tag stored a second time on each translation
    /// is a second thing that can disagree with it. The first entry for a script wins, which is
    /// why `zh-Hant` comes before `zh-TW` up there — a browser can do more with the script than
    /// with one region of it.
    static func tag(of copy: Copy) -> String {
        catalog.first(where: { type(of: $0.copy) == type(of: copy) })?.tag ?? "en"
    }

    /// `ltr` or `rtl`, for the page's `<html dir>`.
    ///
    /// Every language in the catalog today is written left to right, so this answers `ltr` every
    /// time — but it answers it by asking rather than by assuming, because the day Arabic or
    /// Hebrew is added the page should already be laying itself out the right way round rather
    /// than waiting for somebody to notice.
    static func direction(of tag: String) -> String {
        let rightToLeft = ["ar", "he", "fa", "ur", "yi", "ps", "sd", "ckb"]
        return rightToLeft.contains(where: { tag.hasPrefix($0) }) ? "rtl" : "ltr"
    }

    private static func pick() -> Copy {
        let want = Config.shared.language
        let tags = want == "auto" ? Locale.preferredLanguages : [want]
        return copy(preferring: tags)
    }

    /// The first language in a list of preferences that this app actually speaks.
    ///
    /// Split out of ``pick`` because the web interface asks the same question about a *different*
    /// reader: the person holding the phone is not necessarily the person the Mac belongs to, and
    /// their browser is the only thing that knows what they read.
    static func copy(preferring tags: [String]) -> Copy {
        for tag in tags {
            if let hit = catalog.first(where: { tag.hasPrefix($0.tag) }) { return hit.copy }
        }
        return English()
    }

    /// What to speak to a browser, from its `Accept-Language` header.
    ///
    /// **A named language in the config wins**, because that is somebody having said what they
    /// want rather than a default being inferred — and if they picked Japanese for the bar, a
    /// second window of the same app should not answer in English. `auto` is the interesting
    /// case: on the Mac it means "follow this machine", and here it has to mean "follow the
    /// browser", because a phone in a kitchen is a different reader from the desk it is talking
    /// to. Neither answer is more correct in general; each is right about the screen it is on.
    static func copy(forAcceptLanguage header: String?) -> Copy {
        let want = Config.shared.language
        guard want == "auto" else { return copy(preferring: [want]) }
        return copy(preferring: preferences(in: header ?? ""))
    }

    /// `zh-TW,zh;q=0.9,en-US;q=0.8` → the tags, best first.
    ///
    /// Sorted by the quality value rather than by the order they appear, because those are not
    /// the same list — a browser is allowed to write its preferences in any order and let `q` say
    /// what it means, and several do. A tag with no `q` is 1.0, which is the specification's
    /// default and also the common case.
    static func preferences(in header: String) -> [String] {
        header.split(separator: ",").compactMap { piece -> (tag: String, q: Double)? in
            let parts = piece.split(separator: ";")
            guard let tag = parts.first?.trimmingCharacters(in: .whitespaces), !tag.isEmpty,
                  tag != "*" else { return nil }
            let q = parts.dropFirst()
                .first(where: { $0.trimmingCharacters(in: .whitespaces).hasPrefix("q=") })
                .flatMap { Double($0.trimmingCharacters(in: .whitespaces).dropFirst(2)) } ?? 1
            return (tag, q)
        }
        // Stable within a quality: `sorted(by:)` is not, so the index keeps ties in the order the
        // browser wrote them, which is what it meant by writing them that way.
        .enumerated()
        .sorted { $0.element.q == $1.element.q ? $0.offset < $1.offset : $0.element.q > $1.element.q }
        .map(\.element.tag)
    }
}

/// English, for the sixteen the Links sheet added, until the thirteen files have them.
/// Same temporary shape as the ones before it — **delete once translated**.
private let reference = English()

extension Copy {
    var webLinks: String { reference.webLinks }
    var webLinksTip: String { reference.webLinksTip }
    var webLinksPick: String { reference.webLinksPick }
    var webLinksEmpty: String { reference.webLinksEmpty }
    var webLinksFailed: String { reference.webLinksFailed }
    var webLinksLocal: String { reference.webLinksLocal }
    var webLinksFile: String { reference.webLinksFile }
    var webLinksCopy: String { reference.webLinksCopy }
    var webLinksCopied: String { reference.webLinksCopied }
    var webLinksCopyFailed: String { reference.webLinksCopyFailed }
    var webLinkOk: String { reference.webLinkOk }
    var webLinkFail: String { reference.webLinkFail }
    var webLinkDown: String { reference.webLinkDown }
    var webLinkRunning: String { reference.webLinkRunning }
    var webSettingsOrder: String { reference.webSettingsOrder }
    var webSettingsOrderSay: String { reference.webSettingsOrderSay }
}
