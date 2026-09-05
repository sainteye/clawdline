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
    var itermBusy: String { get }
    var scriptMissing: String { get }
    var cannotList: String { get }
    var noOutput: String { get }
    var imageExpired: String { get }
    var imagePreview: String { get }
    var imageClose: String { get }
    /// Shown on a tile whose picture is over the size one cloud envelope can carry. `{mb}` is
    /// the image's own size in mebibytes, to one decimal place, so the reader can see that the
    /// picture is what is too big rather than their connection being at fault.
    var imageTooLarge: String { get }
    /// Shown on a tile whose picture did not cross for any other reason — the connection dropped,
    /// the Mac did not answer, or this device may read but not ask.
    var imageUnavailable: String { get }
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
    /// The word before an exit code — "exit 127".
    ///
    /// Only ever shown for a process that died with **nothing else to say**. When it left a
    /// message, the message leads and the number goes in the hover: "bash: npm: command not
    /// found" is the sentence a person can act on, and "exit 127" is the same fact written for a
    /// log. But a process that exits silently — the ones waiting on a build that never finished
    /// do exactly that — has only the number, and a bare ✗ beside a name explains nothing at all.
    var stackExit: String { get }
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

    /// How many agents a session has working in the background, `{n}` for the number. The bar's
    /// version of ``webAgentsCount``, said in the one place the bar has room for it.
    var sessionAgents: String { get }

    /// What a row says when the session is between turns and a command it started is still
    /// running — see ``Shells``. Said in the same words by the bar and by the page, because it
    /// is the same sentence about the same fact.
    ///
    /// **Two of them, because English counts.** Everywhere else in this file a number goes into
    /// `{n}` and the sentence around it holds still; "1 shell" and "2 shells" do not, and a row
    /// that reads "1 shells running" is a row somebody stops trusting. Languages that do not
    /// inflect here can write the same sentence twice.
    ///
    /// Quiet words, like the agent ones above: a command running is the answer to "is this
    /// finished", never a thing that wants somebody.
    var sessionShellOne: String { get }
    var sessionShellMany: String { get }

    /// What a row says when other sessions are parked on *this* one, `{n}` for how many.
    ///
    /// The other half of a file wait, and the only half the app has to write itself. The waiting
    /// side's row is built out of the words the two agents already agreed on — a session label
    /// and the release condition somebody typed — so there is nothing there to translate. This
    /// side has no such sentence to borrow: nobody wrote "and by the way, three of them are
    /// stuck on you", so the app says it, and therefore has to say it in fourteen languages.
    ///
    /// **Second person, and a count rather than a name.** The waiting row names one peer because
    /// that is who to go and ask; this row is read by the person who is *being* asked, and what
    /// they need first is how many are stuck and what would free them — which is why the number
    /// leads and the release condition follows it. It also keeps the two rows apart when the list
    /// clips them: cut to its first few words, this one still reads as a count in the app's own
    /// voice while the other still reads as somebody's tab title.
    ///
    /// Quiet, like the agent and shell lines. A peer wait is never the loud waiting state — that
    /// one means a person must answer — and this row must not start behaving like one.
    ///
    /// **Two of them, for the reason `sessionShellOne` is two.** This sentence has a verb in it,
    /// and in half these languages that verb agrees with the count: one plural form reads "1
    /// warten auf dich", "1 te esperan", "1 t'attendent". One is not the edge case here but the
    /// ordinary one — most of the time a single session is parked on you. Languages that do not
    /// inflect, and the ones that take the singular after a numeral anyway, write the same
    /// sentence twice; the singular hard-codes its 1 exactly as the shell line does.
    var sessionWaitedOnByOne: String { get }
    var sessionWaitedOnByMany: String { get }

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
    /// Which terminal a new session opens in — the second job ``Config/scopeApp`` used to do,
    /// and the reason this row sits directly above the hotkey's scope rather than anywhere else:
    /// the two look alike and are not, so they are next to each other and named apart.
    var settingsSessionTerminal: String { get }
    var settingsSessionTerminalHint: String { get }
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
    var settingsCodexAutoName: String { get }
    var settingsCodexAutoNameHint: String { get }
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
    /// The event half of the one notification this app sends. The body prefixes it with the
    /// project, while the title names the session — short, because a lock screen truncates and the
    /// important word should not be the one that gets cut.
    var pushWaiting: String { get }
    /// The body of `clawdline://push?test=1`. It has to be unmistakably a test — one that
    /// reads like a real notification teaches somebody to distrust the real ones.
    var pushTest: String { get }
    /// The body of the "this session says it delivered" notification. Past tense for the delivery
    /// and present for what is left: the work is done and the next move is the reader's, which is
    /// the whole reason this one is worth a lock screen. It must not say a *turn* ended — that was
    /// the sentence this replaced, and it was true dozens of times a day.
    var pushDelivered: String { get }
    /// The two ends of a deploy. Kept apart rather than one string with a word swapped in: the
    /// languages this speaks do not all agree on where that word goes, and the failure is the one
    /// that has to be unmistakable at a glance.
    var pushDeployOk: String { get }
    var pushDeployFail: String { get }
    /// A child session has stopped to ask something, and nobody is on that tab.
    ///
    /// Separate from ``pushWaiting`` because it is a different fact, not a politer version of the
    /// same one: a root that is waiting has a person a few feet away who will see it, and a child
    /// that is waiting has a timeout counting down and nobody looking. `minutes` is how long is
    /// left, and absent when there is no clock yet or it has already run out.
    func pushChildWaiting(minutes: Int?) -> String
    /// A whole fan-out has come back — every task under one root, counted once.
    func pushBatchDone(done: Int, failed: Int) -> String
    /// Several finishes that arrived together, delivered as one push instead of one each.
    func pushCoalesced(count: Int) -> String
    /// The delivery switch and the fan-out switch, which were one dishonest switch before. Each
    /// label names the event itself, because a label that names a consequence — "tell me when it
    /// is done" — is exactly how the old one came to promise something it could not see.
    var settingsPushDelivery: String { get }
    var settingsPushDeliveryHint: String { get }
    var settingsPushFanout: String { get }
    var settingsPushFanoutHint: String { get }
    var settingsSmartNotifications: String { get }
    var settingsSmartNotificationsHint: String { get }
    // The health card under the smart-notifications switch. It exists because the feature once
    // failed 784 times in three hours with the only evidence in a log nobody opens: the card says
    // what was attempted, what produced a model sentence, and — in words — why the last attempt
    // fell back. The reason strings are sentences a person can act on, and the timeout one names
    // the deadline, because "the model is timing out" is what tells somebody to raise it.
    var settingsSmartHealthIdle: String { get }
    func settingsSmartHealth(attempts: Int, successes: Int) -> String
    func settingsSmartHealthFailure(reason: String, time: String) -> String
    func settingsSmartTimeout(seconds: Int) -> String
    var settingsSmartQueueFull: String { get }
    var settingsSmartModelFailed: String { get }
    var settingsSmartNoSource: String { get }
    var settingsSmartMissing: String { get }
    var settingsPushDeploy: String { get }
    var settingsPushDeployHint: String { get }
    var settingsAgentNotify: String { get }
    var settingsAgentNotifyNote: String { get }

    // Handing work to another session — see Sources/Orchestrator.swift and docs/orchestrator.md.
    //
    // These sit on the Remote tab rather than in General because what they gate is the same thing
    // the rest of that tab gates: something outside this conversation getting to start a session
    // on this Mac. The switch is worth saying plainly — turning it on means a session can open a
    // terminal tab and run an assistant in it, which is code execution asked for by a machine.
    var settingsOrchestrator: String { get }
    var settingsOrchestratorEnabled: String { get }
    var settingsOrchestratorEnabledHint: String { get }
    /// The cap, 1 to 10. The hint names the default, because a number in a popup says nothing
    /// about whether it is the one that was chosen for you.
    var settingsOrchestratorMax: String { get }
    var settingsOrchestratorMaxHint: String { get }
    /// How far a dispatched child may go before stopping to ask. Three stops, and the hint has
    /// to carry the reason the middle one is the default — that a tab nobody is watching does
    /// not stop for approval, it stops for good.
    var settingsOrchestratorPermission: String { get }
    var settingsOrchestratorPermissionHint: String { get }
    var settingsOrchestratorPermissionAsk: String { get }
    /// The stop that is the default: files written without asking, everything else still judged.
    var settingsOrchestratorPermissionEdits: String { get }
    var settingsOrchestratorPermissionFull: String { get }
    var settingsOrchestratorNotify: String { get }
    var settingsOrchestratorNotifyHint: String { get }
    /// What becomes of a child's tab once it has reported. Three stops — now, in a bit, never —
    /// because that is the whole of the choice anybody makes about a tab they are done with. The
    /// hint carries the exception: a child that timed out keeps its tab, so the screen that would
    /// tell you why is still there.
    var settingsOrchestratorClose: String { get }
    var settingsOrchestratorCloseHint: String { get }
    var settingsOrchestratorCloseNow: String { get }
    var settingsOrchestratorCloseLinger: String { get }
    var settingsOrchestratorCloseKeep: String { get }
    /// The house rules for handing work out: a card saying whether there are any, and a button
    /// that opens the file. Named rather than described, because the thing the button opens is a
    /// file on disk and the row above it prints the path.
    var settingsOrchestratorPolicy: String { get }
    var settingsOrchestratorPolicyHint: String { get }
    var settingsOrchestratorPolicyEdit: String { get }
    /// What the card says when there are rules. `lines` is how many lines the file holds — a
    /// count rather than a preview, because the file is prose and three words of it out of
    /// context says less than the fact that it is there.
    func settingsOrchestratorPolicyOn(_ lines: Int) -> String
    var settingsOrchestratorPolicyOff: String { get }
    var settingsSchedules: String { get }
    var settingsSchedulesEmpty: String { get }
    /// The button that opens an empty form — the Mac's own version of ``webScheduleNew``.
    var settingsScheduleNew: String { get }
    /// The one on each row that opens a filled-in form.
    var settingsScheduleEdit: String { get }
    /// The confirming button in the Mac's own form.
    var settingsScheduleSave: String { get }
    /// The one that removes it there.
    var settingsScheduleDelete: String { get }
    var settingsScheduleRun: String { get }
    var settingsScheduleReveal: String { get }
    var settingsSchedulesFolder: String { get }
    var settingsScheduleNext: String { get }
    var settingsScheduleLast: String { get }
    var settingsScheduleNever: String { get }
    var settingsScheduleStarted: String { get }
    var settingsScheduleActive: String { get }
    var webScheduleMissed: String { get }
    var webScheduleNoNext: String { get }
    /// The very first line a child session says, naming the task it has just taken on.
    ///
    /// A child works in a terminal tab somebody may be watching, and without this the tab opens
    /// and goes quiet: an assistant typing away at an errand nobody in the room asked for. The
    /// child is the one speaking, so it speaks to whoever is at this Mac — the interface language,
    /// not the language of the session that dispatched it. `title` is the task's short human title.
    func childAnnounce(_ title: String) -> String
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
    /// Quiet Session work-state labels and the accessible descriptions behind one/two checks.
    /// They are server copy, even though the fallback bundle repeats English for old/offline pages.
    /// The vocabulary contract — what a person should do on seeing each state — is
    /// docs/session-states.md; `unknown` in particular is an absence, and its copy must never
    /// read as an instruction.
    var sessionWorkReady: String { get }
    var sessionWorkUnknown: String { get }
    var sessionWorkHolding: String { get }
    var sessionWorkOwed: String { get }
    /// The small marker after a self-declared state, so a stated state never dresses as a
    /// proven one.
    var sessionWorkSelfStated: String { get }
    var sessionWorkMilestone: String { get }
    var sessionWorkComplete: String { get }
    var closeabilitySafe: String { get }
    var closeabilityBlocked: String { get }
    var closeabilityBlockedOne: String { get }
    var closeabilityBlockedMany: String { get }
    var closeabilityNeedsAttestation: String { get }
    var closeabilityUnknown: String { get }
    var closeabilityWhy: String { get }
    var closeabilityMoverSelf: String { get }
    var closeabilityMoverPerson: String { get }
    var closeabilityMoverSession: String { get }
    var closeabilityMoverBroker: String { get }
    var closeabilityNotProven: String { get }
    var closeabilityAttestationExplanation: String { get }
    var closeabilityTechnicalDetails: String { get }

    /// The way back to the list on a phone. A chevron is drawn in front of it, so this is the
    /// word alone.
    var webBack: String { get }
    var webBackLabel: String { get }

    /// The drawer behind the wordmark, and the wordmark that opens it.
    ///
    /// These four were written into `Resources/web/index.html` in English for one commit, because
    /// a visible word costs a member here and fourteen conformances. What that bought was a
    /// Chinese reader whose only navigation read 清單 / Usage / 設定, and a wordmark whose
    /// accessible name went from 設定 to an untranslated `Menu` — a regression, not a gap left
    /// open. Nothing could have gone red for it either: `tools/check-web-strings.py` crosses
    /// `T.<name>`, the fallback in `core/i18n.js` and the `/v1/strings` payload, and a literal in
    /// the markup is in none of the three.
    ///
    /// ``webSessions`` is not ``webBack``. They are one word in English and two everywhere the
    /// question is asked differently: ``webBack`` is the chevron above a transcript, with
    /// ``webBackLabel`` finishing the sentence beside it, and this is a destination in a menu.
    var webMenu: String { get }
    var webPages: String { get }
    var webSessions: String { get }
    /// The row that leads to the Project Portfolio page.
    var webUsage: String { get }
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
    var webSessionActions: String { get }
    var webSessionGit: String { get }
    /// The live screen panel, and the two words that say what kind of live it is.
    ///
    /// **`webScreenLive` and `webScreenOnDemand` are the interface's whole answer to "which
    /// backend am I looking at".** tmux can say a pane moved; iTerm2 has no such signal and can
    /// only be asked. A translation that makes those two read the same removes the one thing that
    /// stops the panel implying a liveness it does not have on one of the two backends.
    var webScreenTitle: String { get }
    var webScreenLive: String { get }
    var webScreenOnDemand: String { get }
    var webScreenGone: String { get }
    var webGitTitle: String { get }
    var webGitClean: String { get }
    var webGitNotRepo: String { get }
    var webGitFailed: String { get }
    var webGitRefresh: String { get }
    var webGitClose: String { get }
    var webGitStaged: String { get }
    var webGitUnstaged: String { get }
    var webGitUntracked: String { get }
    var webGitConflict: String { get }
    var webEndSession: String { get }
    var webConfirmActionTitle: String { get }
    var webConfirmActionSay: String { get }
    var webConfirmEndTitle: String { get }
    var webConfirmEndSay: String { get }
    /// The line above the list of what a close would take with it — live children, stranded
    /// waiters — shown at the moment of the confirming press, never as a list column.
    var webConfirmEndLoses: String { get }
    /// Naming a newly created Session this Mac's Clawdfather.
    ///
    /// **Registration-only, in every language.** The choice lives on the creation sheet, it is
    /// offered only when this Mac has no coordinator record at all, and the sentence it types
    /// says so: a configured coordinator is left where it is, *including one that is offline*.
    /// Reconnecting an offline owner is the manual repair below the recipe in
    /// `docs/orchestrator.md` and belongs to a person at the machine, not to a web sheet, so no
    /// translation here may name `rebind` or offer to take an existing owner's place.
    ///
    /// The browser cannot register a coordinator — that needs the orchestrator token, and a
    /// paired device does not have one. So the sheet does not do it: it types
    /// `webClawdfatherRegisterAsk` into the new session through the ordinary send route, and the
    /// session, which is a local process that *can* read the token, carries out the
    /// registration itself.
    ///
    /// `{name}` in `webClawdfatherIs` is the label from the authenticated `session.coordinator`
    /// projection; `{id}` in `webClawdfatherRegisterAsk` is the new session's terminal-neutral
    /// id, which the page already holds and hands over rather than leaving it to be worked out
    /// at the far end. A translation that drops that hole would be addressed to nobody, so
    /// `input/clawdfather.js` refuses it and types the English — see the fallbacks there, which
    /// are the layer below the baked-in English in `core/i18n.js`.
    ///
    /// The three outcomes are all said in the reader's language, because the sheet's own words
    /// were localized while the two sentences beside them stayed English literals in
    /// `input/start.js` — a difference `tools/check-web-strings.py` cannot see, since a
    /// hardcoded sentence has no name to compare. `webClawdfatherRegisterBlocked` is the newest
    /// of them and belongs to the newest fact: the Mac now says whether registering would write
    /// over a coordinator record it cannot read, and a person who is refused deserves that
    /// reason rather than a failed-read sentence that is not what happened.
    ///
    /// The names were `webMake…`/`web…Ask`/`web…Asked`, from the retired existing-Session menu
    /// item, and the two `webConfirmClawdfather…` keys were its confirmation sheet. Those two
    /// are deleted rather than renamed; the sheet they belonged to is gone.
    var webClawdfatherCreateLabel: String { get }
    var webClawdfatherIs: String { get }
    var webClawdfatherRegisterAsk: String { get }
    var webClawdfatherRegisterSent: String { get }
    var webClawdfatherRegisterLate: String { get }
    var webClawdfatherRegisterBlocked: String { get }
    /// The Clawdfather controls panel — `input/coordinator-actions.js`.
    ///
    /// The commands table is closed on the client on purpose: labels, placement and safety
    /// meaning come from shipped code, never from a payload — so every label and summary ships
    /// here rather than riding the `commands` record. `{name}` is the coordinator's advertised
    /// label ("Clawdfather" today), handed through `fill` like every other hole.
    var webCoordSectionObserve: String { get }
    var webCoordSectionCoordinate: String { get }
    var webCoordSectionPresence: String { get }
    var webCoordSectionAdmin: String { get }
    var webCoordCmdStatusReport: String { get }
    var webCoordCmdStatusReportSay: String { get }
    var webCoordCmdSinceAway: String { get }
    var webCoordCmdSinceAwaySay: String { get }
    var webCoordCmdDuplicates: String { get }
    var webCoordCmdDuplicatesSay: String { get }
    var webCoordCmdLandingClosure: String { get }
    var webCoordCmdLandingClosureSay: String { get }
    var webCoordCmdCoordinateWork: String { get }
    var webCoordCmdCoordinateWorkSay: String { get }
    var webCoordCmdDispatch: String { get }
    var webCoordCmdDispatchSay: String { get }
    var webCoordCmdAsk: String { get }
    var webCoordCmdAskSay: String { get }
    var webCoordCmdQuietWatch: String { get }
    var webCoordCmdQuietWatchSay: String { get }
    var webCoordCmdScope: String { get }
    var webCoordCmdScopeSay: String { get }
    var webCoordCmdStop: String { get }
    var webCoordCmdStopSay: String { get }
    var webCoordCmdReconnect: String { get }
    var webCoordCmdReconnectSay: String { get }
    var webCoordCmdDeepAudit: String { get }
    var webCoordCmdDeepAuditSay: String { get }
    var webCoordTokenExpected: String { get }
    var webCoordTokenLow: String { get }
    var webCoordTokenMedium: String { get }
    var webCoordTokenHigh: String { get }
    var webCoordTokenUnknown: String { get }
    var webCoordAuditPreview: String { get }
    var webCoordAuditWhyOffline: String { get }
    var webCoordAuditWhyDisconnected: String { get }
    var webCoordAuditWhyNoWrite: String { get }
    var webCoordAuditSending: String { get }
    var webCoordAuditSent: String { get }
    var webCoordAuditFailed: String { get }
    /// The safety meaning under each command, and the state word beside its name.
    var webCoordEffectRead: String { get }
    var webCoordEffectAdvisory: String { get }
    var webCoordEffectSpawns: String { get }
    var webCoordEffectMutation: String { get }
    var webCoordStateAvailable: String { get }
    var webCoordStateDraft: String { get }
    var webCoordStateUnavailable: String { get }
    var webCoordStatePreview: String { get }
    var webCoordStateDisabled: String { get }
    var webCoordOnline: String { get }
    var webCoordOffline: String { get }
    var webCoordControlsTitle: String { get }
    var webCoordOpenControls: String { get }
    var webCoordEmpty: String { get }
    var webCoordDisabledFallback: String { get }
    /// The preview receipt for anything that is not a connected read. Every one of these has to
    /// keep saying that nothing was sent — the same rule as dictation, for the same reason.
    var webCoordPreviewTitle: String { get }
    var webCoordPreviewNone: String { get }
    var webCoordPreviewMutation: String { get }
    var webCoordPreviewDraft: String { get }
    var webCoordPreviewSpawn: String { get }
    var webCoordPreviewContract: String { get }
    /// Why a disabled command is disabled, keyed by the server's closed `reason` code — a code
    /// cannot drift out of the client's vocabulary the way a sentence can. The prose `why` field
    /// stays on the wire for older pages; a page that knows the code says it in its own language.
    var webCoordWhyNoCommandRoute: String { get }
    var webCoordWhyNoReturnLedger: String { get }
    var webCoordWhyDeviceCannotSpawn: String { get }
    var webCoordWhyMachineTokenOnly: String { get }
    /// Rendering the answer of the four connected read-only commands, fed by the
    /// device-readable Bearings projection at `GET /v1/orchestrator/coordinator/bearings`.
    var webCoordReadFailed: String { get }
    var webCoordActiveTasks: String { get }
    var webCoordPendingLandings: String { get }
    var webCoordOpenWaits: String { get }
    var webCoordCountUnknown: String { get }
    var webCoordStaleSessions: String { get }
    var webCoordUnknown: String { get }
    var webCoordWaitingList: String { get }
    var webCoordBlockingList: String { get }
    var webCoordAllQuiet: String { get }
    var webCoordNoLandings: String { get }
    var webCoordUnregistered: String { get }
    var webCoordScopeLine: String { get }
    var webCoordScopeDevice: String { get }
    /// The session after its close was confirmed, while the Mac is still carrying it out.
    var webClosing: String { get }
    var webCancel: String { get }
    var webConfirm: String { get }
    var webReviewBeforeClosing: String { get }
    var webConfirmEndAnyway: String { get }
    var webPickSession: String { get }
    /// Read out where a skeleton is drawn, and never seen — the skeleton is the visible half.
    var webReading: String { get }
    var webLoading: String { get }
    var webTranscriptFailed: String { get }
    /// Who said a line, in the transcript's left margin. Drawn in small capitals, so: one short
    /// word each. Claude's own name is not here — it is a name, and it is not translated.
    var webWhoYou: String { get }
    var webWhoTool: String { get }
    /// Semantic Clawdline notice cards. Clawdline itself is a product name and remains unchanged;
    /// every descriptive word comes from the browser's negotiated language. Counted sibling
    /// sentences use `{n}`, filled and escaped by the web client.
    var webNoticeTask: String { get }
    var webNoticeCompleted: String { get }
    var webNoticeFailed: String { get }
    var webNoticeTimedOut: String { get }
    var webNoticeCancelled: String { get }
    var webNoticeCouldNotStart: String { get }
    var webNoticeFinished: String { get }
    var webNoticeWorkspaceOverlap: String { get }
    var webNoticeNoSiblings: String { get }
    var webNoticeOneSibling: String { get }
    var webNoticeManySiblings: String { get }
    var webNoticeClaimsReleased: String { get }
    var webNoticeFileWaitRequested: String { get }
    var webNoticeFileWaitReleased: String { get }
    var webNoticeHandoffPickedUp: String { get }
    var webNoticeHandoffNeedsDelivery: String { get }
    var webNoticeRecheckGit: String { get }
    /// A sent message that the Mac has not picked up yet.
    var webPending: String { get }
    /// Attached-image counts for pending messages. `{n}` is replaced by the web client.
    var webAttachedImage: String { get }
    var webAttachedImages: String { get }
    /// How many tool calls a folded run stands for. The web's spelling of ``foldedTools``, which
    /// cannot cross a JSON boundary as a function.
    var webSteps: String { get }
    var webJustNow: String { get }
    var webMinutesAgo: String { get }
    /// The copy button on a fenced code block, and the two answers a press can have.
    ///
    /// The failure is a sentence rather than silence on purpose. `navigator.clipboard` is absent
    /// on any page served over plain http — which is how this one is read on a home network —
    /// and a button that does nothing at all reads as a page that has stopped working.
    var webCodeCopy: String { get }
    var webCodeCopied: String { get }
    var webCodeCopyFailed: String { get }

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

    // Dictating into the composer from a phone — the microphone beside the send button, and
    // `POST /v1/voice` behind it.
    //
    // **This is not the bar's dictation said again.** On the Mac, Apple's recogniser writes while
    // you speak and Whisper reads the recording back afterwards; a browser has neither of those to
    // offer, so the phone records, sends the samples, and waits for the Mac to answer. What these
    // words are mostly about is therefore the waiting — and the two quite different places it can
    // go wrong, because a microphone the browser will not open and a Mac with no model on it are
    // fixed in different buildings.

    /// On the microphone itself: what it will do, and then what it will do next. Never seen —
    /// it is an icon — so these are read aloud, and each has to say which of the two states the
    /// button is in without the drawing to help.
    var webVoiceStart: String { get }
    var webVoiceStop: String { get }
    /// And on the button in the row that counts, which is the one place ending a recording is
    /// *read* rather than heard. One word, because by then the reader has already stopped
    /// talking and the only question left is which of the two buttons keeps what they said —
    /// ``webVoiceStop`` describes what happens next, which is what somebody who cannot see the
    /// row needs and is more than a button sitting beside Cancel should be spending on itself.
    var webVoiceDone: String { get }
    /// While it records. `{t}` is how long it has been going, as `m:ss`, replaced every second —
    /// so the words on either side of it have to read with `0:07` and with `2:41` in the middle.
    var webVoiceListening: String { get }
    /// And while the Mac reads it back, where `{n}` is whole seconds counted from the upload. A
    /// number rather than a spinner for the reason the bar counts too: twelve seconds of turning
    /// arc cannot be told apart from a hang.
    var webVoiceReading: String { get }
    /// Added under that count once the wait has run past a few seconds. The twelve is the model
    /// being read off disk, and it happens once per boot of the Mac rather than once per launch of
    /// this app — a difference the sentence has to make, or somebody who quit Clawdline and opened
    /// it again waits twelve seconds expecting one and a half. Worth saying at all before somebody
    /// concludes the feature is broken and stops using it.
    var webVoiceSlow: String { get }
    /// The recording being cut off at the client's own ceiling, where `{n}` is that in minutes.
    /// It opens with what just happened rather than with the rule, because whoever reads it was
    /// talking a second ago and the recording stopped without being asked to — a sentence that
    /// only states the limit leaves them working out which of the two they are looking at. Then
    /// it ends by saying what happens next rather than by apologising: nothing was lost, and the
    /// audio up to the cut is on its way.
    var webVoiceLimit: String { get }
    /// An empty transcript, which the Mac answers with `200` and an empty string. **Not an
    /// error**, and it must not read as one — a microphone that worked perfectly in a quiet room
    /// has nothing to be sorry about.
    var webVoiceEmpty: String { get }
    var webVoiceTooShort: String { get }
    /// The four the browser decides, and they are four rather than one because only the first has
    /// somewhere to go. A permission that was refused can be given back, and the place that is
    /// done is inside the browser rather than anywhere in this app — so that sentence says so,
    /// as an instruction and not as a fact about where a setting lives. The difference is not a
    /// matter of taste: a sentence whose subject is *the settings* and whose verb is *are where
    /// that happens* does not come apart into anything a translator can put back together in
    /// Chinese, Japanese, Korean or Hindi. The other three describe a device instead of asking
    /// for a press.
    var webVoiceDenied: String { get }
    var webVoiceNoMic: String { get }
    var webVoiceInUse: String { get }
    /// `getUserMedia` is absent outside a secure context, so over plain `http` there is no
    /// permission prompt to refuse — nothing happens at all. The two ways to reach this page do
    /// not have the same fix, which is why the sentence keeps them apart: a tunnel arrives with
    /// https and needs nothing said about it, and `http://192.168…` on the local network has no
    /// setting anywhere that would give it one. Telling that second reader to open it over https
    /// reads as one press away and is not.
    var webVoiceInsecure: String { get }
    var webVoiceUnsupported: String { get }
    /// And the three the Mac decides. `busy` is a queue two deep — one recording being read and
    /// one waiting behind it, never two being read at once — and it drains in seconds, so it asks
    /// for a retry; the other two are a machine nobody has set up, where trying again is the one
    /// thing that cannot help — so those name what is missing instead.
    var webVoiceBusy: String { get }
    var webVoiceNoBinary: String { get }
    var webVoiceNoModel: String { get }
    var webVoiceFailed: String { get }

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
    var webSettingsAssistantIcons: String { get }
    var webSettingsAssistantIconsSay: String { get }
    var webSettingsAssistantIconsShow: String { get }
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
    /// Which assistant a press should open. Only ever on screen when the Mac has more than
    /// one of them installed, which is what makes it a question worth asking.
    var webStartWith: String { get }
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
    /// **No `{app}`, and that is the whole of it.** `terminal_unsupported` has one producer —
    /// ``StartPoints/Plan/noTmux`` — and it carries no application name, because the refusal is
    /// *tmux is what Settings asks for and there is no tmux on this Mac*, and tmux is not an
    /// application. So this sentence is written whole rather than around a hole, and it says the
    /// two things somebody can do about it: install tmux, or choose another terminal in Settings.
    var webStartTerminalUnsupported: String { get }
    /// The one success that is not one. `{command}` is the line somebody types on the Mac to
    /// reach the session, and it arrives in the reply's `attach` field rather than in this
    /// sentence — the same shape as ``webStartTerminalClosed``, and for the same reason: the
    /// session name belongs to ``Tmux/attachCommand`` and must not be spelled out fourteen more
    /// times where it can drift.
    ///
    /// **It is the answer to the only start that leaves nothing on screen.** With tmux chosen in
    /// Settings and no server running, the Mac starts one with nothing attached to it: the
    /// session is real, Clawdline lists it and can type into it, and at the Mac it is drawn
    /// nowhere at all. Every other start puts a tab in front of somebody. So this is not a
    /// refusal and must not read as one — what happened is what was asked for, and the sentence
    /// exists to say where it went.
    var webStartDetached: String { get }
    var webStartOff: String { get }

    // Picking a conversation back up instead of starting a new one — see
    // `GET /v1/places/:id/sessions/:assistant` and
    // `POST /v1/places/:id/resume/:assistant/:session`.
    //
    // Same rule as above, one step further in: the page never names a conversation either. It
    // shows the titles the Mac read off its own transcripts and sends back an id out of that
    // list. So these words are about *choosing among what is offered*, and a translation that
    // invites somebody to type an id is describing a thing that does not exist.
    /// The switch on the sheet. It is a question about the **next** press — pick up an earlier
    /// conversation rather than begin a new one — and not a mode the sheet is already in.
    var webResumeWith: String { get }
    var webResumePick: String { get }
    var webResumeFilter: String { get }
    /// A project the selected assistant has been run in but has written nothing down about.
    /// Ordinary, and not a failure.
    var webResumeEmpty: String { get }
    /// On the one row that is a conversation something is writing to **right now**. Resuming it
    /// would put a second process on the same transcript, so this word is the whole warning and
    /// it has to read as a state rather than as a label.
    var webResumeLive: String { get }
    var webResumeBack: String { get }
    var webResumeGone: String { get }
    /// Retained for wire compatibility with older pages; current pages can resume either
    /// assistant and do not draw it.
    var webResumeClaudeOnly: String { get }
    var webResuming: String { get }
    /// Said when the **Mac** stopped listing rather than the page — a project with more
    /// conversations in it than one reply carries. Nothing below it can be reached by scrolling,
    /// which is exactly why it has to be said out loud.
    var webResumeCapped: String { get }

    // Starting a session by saying it, rather than picking it — the microphone beside the plus,
    // the sheet it opens, and `POST /v1/intents` behind it. Whisper hears the sentence; a planner
    // on the Mac turns it into a draft; a person reads the draft and edits it; only the button at
    // the bottom sends anything anywhere.
    //
    // **Nothing here may say or imply that a session has started, or that anything has been sent,
    // before the last button is pressed.** The same rule as the composer's own dictation strings,
    // and for the same reason: the words are read before the action they describe is true, not
    // after, right up until the person decides otherwise.
    var webCommand: String { get }
    /// The header button's aria-label — a whole sentence, since the button carries no text of
    /// its own for a screen reader to fall back on.
    var webCommandLabel: String { get }
    /// One line under the title. Says nothing has happened yet, not what the feature is.
    var webCommandSay: String { get }
    /// Placeholder for the box holding what Whisper heard, before anyone has edited it.
    var webCommandHeard: String { get }
    /// Shown for the few seconds the planner takes to turn a sentence into a draft.
    var webCommandThinking: String { get }
    /// Heading over the draft half of the sheet — the project, the assistant, and the first
    /// message, once the planner has answered.
    var webCommandDraft: String { get }
    /// Label for the project list within the draft.
    var webCommandWhere: String { get }
    /// Label for the choice of assistant. Only worth a row when there is a choice to make.
    var webCommandWith: String { get }
    /// Label over the model chips the planner picked among — haiku, sonnet, opus — for the
    /// assistant chosen in `webCommandWith`. The chips carry their own names, unlocalized;
    /// only this label is a translated word.
    var webCommandModel: String { get }
    /// Label for the editable first message — editable because the planner's writing, in
    /// `instructions`, is read by a person before anything reaches a terminal.
    var webCommandFirst: String { get }
    /// The button that actually starts it. The one place in this block where something happens.
    var webCommandGo: String { get }
    /// Shown when the planner could not tell which project was meant — a person has to pick.
    var webCommandUnsure: String { get }
    /// `POST /v1/intents` failed for a reason other than the two below.
    var webCommandFailed: String { get }
    /// 503 `no_planner` — this Mac has no Claude Code installed to plan with.
    var webCommandNoPlanner: String { get }
    /// 429 `busy` — one sentence is already on its way to becoming a draft.
    var webCommandBusy: String { get }
    /// Whisper returned nothing to plan from.
    var webCommandEmpty: String { get }

    // Making a schedule — the "+" beside the Schedules list, and the sheet a spoken schedule
    // lands in when the planner could not fill in every field. See `Orchestrator.schedule(from:)`
    // and `POST /v1/orchestrator/schedules`, the only route allowed to write one.
    //
    // **The same rule as `webCommand*` above: nothing here may say or imply that a schedule
    // exists before the Create button at the bottom is pressed.**
    var webScheduleNew: String { get }
    /// The same sheet's heading when it is changing a schedule rather than making one.
    var webScheduleEdit: String { get }
    /// The line under the title, said before anyone has touched a field.
    var webScheduleNewSay: String { get }
    var webScheduleTitle: String { get }
    var webScheduleAt: String { get }
    var webScheduleOn: String { get }
    var webScheduleWhere: String { get }
    var webScheduleWith: String { get }
    var webScheduleFirst: String { get }
    /// The disclosure that folds away everything below — a schedule with all the defaults
    /// should not make somebody read six more fields to create it.
    var webScheduleMore: String { get }
    /// Same label as `webCommandModel`, folded away here because a schedule already has a
    /// model — the one the planner picked, or the assistant's own default — before anyone
    /// opens "More".
    var webScheduleModel: String { get }
    var webScheduleWhenDone: String { get }
    var webScheduleCloseSuccess: String { get }
    var webScheduleCloseAlways: String { get }
    var webScheduleCloseNever: String { get }
    var webScheduleEnabled: String { get }
    /// Its opposite, on the same row. The list drew this one in English until the form
    /// arrived and made the row a thing somebody could act on.
    var webScheduleDisabled: String { get }
    var webScheduleNotify: String { get }
    /// Labels a number of hours — the box itself holds a bare number, so the unit lives here.
    var webScheduleCatchUp: String { get }
    /// Labels a number of minutes, the same way.
    var webScheduleTimeout: String { get }
    var webScheduleCreate: String { get }
    /// Shown once `POST /v1/orchestrator/schedules` answers 200 — the only place this feature is
    /// allowed to say a schedule now exists.
    var webScheduleCreated: String { get }
    /// Shown beside `webScheduleCreated` when that same reply's `dispatch_enabled` is false.
    /// Not an error: the schedule was written exactly as asked, and is valid — this Mac's own
    /// dispatch is simply off, so nothing runs it until Settings turns that on. `webStartOff`
    /// is the nearest sentence in tone.
    var webScheduleDispatchOff: String { get }
    var webScheduleFailed: String { get }
    /// The confirming button when the sheet is editing a schedule rather than making one — the
    /// create state's own button is ``webScheduleCreate``.
    var webScheduleSave: String { get }
    /// Shown once `PATCH /v1/orchestrator/schedules/:id` answers 200 — the edit sheet's version
    /// of ``webScheduleCreated``.
    var webScheduleSaved: String { get }
    /// The button that removes the schedule, sitting beside Cancel and ``webScheduleSave``.
    var webScheduleDelete: String { get }
    /// The question asked before that happens. There is no undo and no route that could add
    /// one, so this has to be honest rather than ceremonial — and it names the schedule's own
    /// title where the layout allows. `{title}` is a hole the page fills in.
    var webScheduleDeleteAsk: String { get }
    /// Shown once `DELETE /v1/orchestrator/schedules/:id` answers 200.
    var webScheduleDeleted: String { get }
    /// What the sentence that opened this form did not say. A prompt to fill something in, not
    /// an error — the planner left it blank on purpose rather than guess.
    var webScheduleNeedsTime: String { get }
    var webScheduleNeedsPlace: String { get }
    /// The chip that means every day, alongside the seven below rather than instead of them.
    var webScheduleDaily: String { get }
    /// The seven weekday chips, side by side on a phone. The shortest form this language really
    /// writes a weekday in a picker — not a whole word if nobody writes it that way there.
    var webScheduleSun: String { get }
    var webScheduleMon: String { get }
    var webScheduleTue: String { get }
    var webScheduleWed: String { get }
    var webScheduleThu: String { get }
    var webScheduleFri: String { get }
    var webScheduleSat: String { get }

    // Snippets — the pieces of text somebody wrote once and presses instead of typing again. The
    // sheet is reached two ways, from the `⋯` menu and from the project mark in the session
    // header, and the mark is why these live here rather than in a copy table of the view's own:
    // it is the first thing a reader who does not read English will press.
    //
    // **Nothing here may say or imply that pressing one sends it.** A press puts the words in the
    // composer and closes the sheet; the send button stays the only thing that types into a
    // session. A translation that reads as "send this" describes a feature that was declined.
    var webSnippets: String { get }
    /// The two headings the sheet groups under, and the two halves of the editor's scope control.
    /// `webSnippetsThisProject` is the project the *Mac* resolved — the one the header's mark
    /// stands for, with an isolated worktree folded back into the checkout it was cut from — so
    /// it means *this project* and never *the folder this session happens to sit in*.
    var webSnippetsThisProject: String { get }
    var webSnippetsEveryProject: String { get }
    /// Two empty states, because there are two different facts to say. A device that cannot write
    /// is told where snippets come from; a device that can is shown the door and two starters.
    var webSnippetsEmpty: String { get }
    var webSnippetsEmptyNew: String { get }
    /// Why the rows do nothing on a device the Mac granted `read` and not `send`. It names the
    /// box rather than the snippet, because the composer this would insert into is disabled too —
    /// the honest sentence is about what this device may do, not about the list being broken.
    var webSnippetsReadOnly: String { get }
    /// The editor. Everything from here to ``webSnippetNeedsText`` is drawn only where the
    /// transport can write, so a reader on the relay sees none of it at all.
    var webSnippetNew: String { get }
    /// The same sheet's heading when it is changing a snippet rather than making one.
    var webSnippetEditing: String { get }
    /// The `⋯` on a row, and the five things behind it.
    var webSnippetMore: String { get }
    var webSnippetEdit: String { get }
    var webSnippetDelete: String { get }
    /// What the Delete row says once it has been pressed once. There is no undo and no route
    /// that could add one, so the button becomes the question rather than a dialog appearing in
    /// front of it — which means this has to read as a warning on a button, not as a label.
    var webSnippetDeleteAsk: String { get }
    /// Buttons rather than drag: dragging inside a scrolling sheet on a phone is a fight nobody
    /// wins. Each one moves the row a single place, inside its own scope.
    var webSnippetUp: String { get }
    var webSnippetDown: String { get }
    /// A row's scope, offered as the move it would make. Only ever one of the two on screen: a
    /// project snippet is offered ``webSnippetToGlobal``, a global one ``webSnippetToProject``.
    var webSnippetToGlobal: String { get }
    var webSnippetToProject: String { get }
    /// The editor's three fields. ``webSnippetBodyLabel`` is the text that will land in the
    /// composer, so it is named after what somebody typed — calling it content, payload or body
    /// would describe the stored record instead.
    var webSnippetTitleLabel: String { get }
    var webSnippetBodyLabel: String { get }
    var webSnippetScopeLabel: String { get }
    var webSnippetSave: String { get }
    /// Fills the editor from the newest thing this person sent in this session — the sentence
    /// somebody is about to save is usually one they have just finished typing once.
    var webSnippetFromLast: String { get }
    /// Both fields are required by the store, so the sheet says it here rather than waiting for
    /// `400 malformed_snippet` to come back from the Mac.
    var webSnippetNeedsText: String { get }
    /// The other refusal the editor can reach, and for a while the two shared one sentence — a
    /// snippet that was too long said the fields were empty while both were visibly full.
    /// ``webSnippetFromLast`` is how it is reached: it assigns a whole message to the body, and
    /// a value set in code ignores the textarea's `maxlength`.
    ///
    /// **Both numbers belong in the sentence, and both count UTF-8 bytes**, which is what
    /// `Sources/Snippets.swift` counts and what the file, the audit line and every snapshot
    /// broadcast pay for. A limit somebody cannot see is not help; a limit in units that are not
    /// the ones being enforced is worse.
    var webSnippetTooLong: String { get }
    /// The two starters offered on an empty list, one press each.
    ///
    /// **These four are the one place in this file where a literal translation is the wrong
    /// answer.** Everything above is the interface talking; these are text a person sends to an
    /// assistant, so each language writes what somebody there would actually type — the same two
    /// instructions, in that language's own register, rather than the English rendered word by
    /// word. `git add -A` is a command and stays spelled that way in all of them.
    var webSnippetStarterCommitTitle: String { get }
    var webSnippetStarterCommitBody: String { get }
    var webSnippetStarterReportTitle: String { get }
    var webSnippetStarterReportBody: String { get }

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

    // The Session info card — see `GET /v1/sessions/:id/info` and the sheet the menu's
    // **Session info** row opens. `webInfoUnknown` is the one to keep honest: a plan window
    // nobody reported is *unknown*, never 0%, because a full window drawn as an empty one is
    // the wrong answer that changes what somebody does next.
    var webSessionInfo: String { get }
    var webInfoTitle: String { get }
    var webInfoEditTitle: String { get }
    var webInfoCopyTitle: String { get }
    var webInfoTitleSaved: String { get }
    var webInfoTitleLocal: String { get }
    var webInfoTitleQueued: String { get }
    var webInfoTitleNotDurable: String { get }
    var webInfoTitleCloud: String { get }
    var webInfoSession: String { get }
    var webInfoAssistant: String { get }
    var webInfoModel: String { get }
    var webInfoSessionId: String { get }
    var webInfoDirectory: String { get }
    var webInfoRunningFor: String { get }
    var webInfoStatus: String { get }
    var webInfoWorkStatusMeaning: String { get }
    var webInfoCloseabilityMeaning: String { get }
    var webInfoUsage: String { get }
    var webInfoInput: String { get }
    var webInfoOutput: String { get }
    var webInfoCacheRead: String { get }
    var webInfoCacheWrite: String { get }
    var webInfoTotal: String { get }
    var webInfoCost: String { get }
    var webInfoNoUsage: String { get }
    var webInfoLimits: String { get }
    var webInfoLimitHit: String { get }
    var webInfoResets: String { get }
    var webInfoUnknown: String { get }
    var webInfoFiles: String { get }
    var webInfoBranch: String { get }
    var webInfoStaged: String { get }
    var webInfoUnstaged: String { get }
    var webInfoUntracked: String { get }
    var webInfoConflict: String { get }
    var webInfoClean: String { get }
    var webInfoNotRepo: String { get }
    var webInfoDeploy: String { get }
    var webInfoNoDeploy: String { get }
    var webInfoFailed: String { get }
    /// 429 `busy` — the card is one of three routes sharing a limit, and this one drains in
    /// well under a second. Same rule as ``webVoiceBusy`` and ``webCommandBusy``: a refusal that
    /// fixes itself asks for a retry, while ``webInfoFailed`` beside it reads as a session that
    /// cannot be read at all, which is a different afternoon.
    var webInfoBusy: String { get }
    var webInfoRefresh: String { get }
    var webInfoTokens: String { get }
    var webInfoSwitchModel: String { get }
    var webInfoModelOther: String { get }
    var webInfoModelSent: String { get }
    var webInfoModelBusy: String { get }
    var webInfoSwitchPermission: String { get }
    var webInfoPermissionAuto: String { get }
    var webInfoPermissionManual: String { get }
    var webInfoPermissionAcceptEdits: String { get }
    var webInfoPermissionPlan: String { get }
    var webInfoPermissionUnreadable: String { get }
    var webInfoPermissionSent: String { get }
    var webInfoPermissionBusy: String { get }
    var webInfoLimitsClaude: String { get }
    var webInfoCopied: String { get }
    var webInfoAsOf: String { get }
    var webInfoWhyUnknown: String { get }
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

    // The same menu, once it can be read — which is now the ordinary case rather than the
    // hoped-for one. `webWaitingSay` and `webWaitingSend` above are what is said when it cannot
    // be, and they are still reachable: a dialog drawn in a shape the parser does not recognise
    // is a screen this end has to be honest about rather than guess at.
    //
    // `webMenuHighlighted` marks the row the caret is parked on. It is the one thing a person
    // cannot see from a phone and needs to know before they tap: it is what a bare Return over
    // on the Mac would confirm, so it is also what somebody else in front of that screen is
    // about to answer.
    var webMenuSay: String { get }
    var webMenuHighlighted: String { get }
    var webMenuSent: String { get }

    // The agents a session sent off to work — see ``Subagents``. Deliberately quiet words:
    // background work is context for why a session is busy, never a thing that wants somebody,
    // and it must not read as loudly as a session that is actually waiting.
    var webAgents: String { get }
    var webAgentsCount: String { get }
    var webAgentDone: String { get }
    var webAgentFailed: String { get }

    /// Heads the same strip's second half — the commands the session left running. It counts with
    /// ``webAgentsCount``, which says "{n} running" and is as true of a shell as of an agent.
    var webShells: String { get }

    // Opening one of them, which lands in a panel rather than in the transcript pane: a command
    // has no conversation, only the file it is printing into. See `ShellPanel` on the page.
    //
    /// Heads that panel. Singular — it is about one command, where ``webShells`` heads a list.
    var webShellTitle: String { get }
    /// What the row in the strip does when it is pressed.
    var webShellOpen: String { get }
    /// Whether anything more is coming, said above the output. **The pair carries the whole
    /// point of the panel**: somebody opened it to watch a build land, and the moment it does
    /// this is the only thing on screen that changes.
    var webShellRunning: String { get }
    var webShellEnded: String { get }
    /// A command that has started and printed nothing — `sleep`, or the first second of anything
    /// else. Not an error: there is nothing wrong here except that there is nothing to read.
    var webShellQuiet: String { get }
    var webShellFailed: String { get }
    var webShellClose: String { get }

    // Stopping one. **The second thing on the page that destroys something**, after ending a
    // session, and it destroys the more ordinary thing — so the words carry the two facts
    // somebody needs before they press: what is about to be stopped, and that it does not undo
    // anything the command already did.
    //
    /// The button, in the panel's own header. One word, next to Close.
    var webShellStop: String { get }
    var webShellStopTitle: String { get }
    /// `{command}` is the command line itself, because that is what somebody is agreeing to stop
    /// — the id is nine random characters and is not a description of anything.
    var webShellStopSay: String { get }
    /// **"Asked it to stop", not "stopped it".** The signal is sent from here; what happens next
    /// is between the command and the kernel, and the panel says which it was a moment later.
    var webShellStopped: String { get }
    var webShellStopFailed: String { get }

    // Reading one agent's own conversation, which both the pane and the page can now do. No
    // `web` prefix on the first four: they are said in the same words on the Mac and on a phone,
    // and one string said twice is one thing to translate rather than two to keep in step.
    //
    /// Beside ``webAgentDone`` and ``webAgentFailed``, for the state those two do not cover.
    var agentRunning: String { get }
    /// How many tools an agent reached for before it finished, `{n}` for the number. Sits with
    /// how long it took and what it cost, in the header above its transcript.
    var agentTools: String { get }
    /// An agent whose file exists and has nothing readable in it yet — the first second of its
    /// life, and the whole of it for one that died before writing. Not an error: there is
    /// nothing wrong here except that there is nothing to read.
    var agentEmpty: String { get }
    /// The way back out of an agent's transcript, after `‹` on the page and `←` in the pane.
    /// **The word is the destination, not the act**: what is on the other side of it is the
    /// session's own conversation, and "Back" alone would not say which of the two you land in.
    var agentBack: String { get }
    /// What the row in the composer does when it is clicked. Only the page needs this — the
    /// pane's version is a link, and a link is self-evidently a thing you can follow.
    var webAgentOpen: String { get }

    // The chips on a session that dispatched work, and on the one doing it — see
    // ``Orchestrator`` and the `orchestrator` event on the stream. Furniture, like the agent
    // words above: a child session is somebody else's errand, and it must not shout louder than
    // a session that is actually waiting for a person.
    //
    /// The session that asked. Followed by how many tasks it has out: `Root · 2`.
    var webTaskRoot: String { get }
    /// The session doing one, followed by ``webTaskRunning``, ``webTaskDone`` or
    /// ``webTaskFailed``. Indented under its root in the list.
    var webTaskChild: String { get }
    /// Labels the tooltip that lists a root's task titles.
    var webTaskTasks: String { get }
    var webTaskDone: String { get }
    var webTaskFailed: String { get }
    var webTaskRunning: String { get }

    /// A page that has been open since before the Mac was rebuilt. It does not reload itself —
    /// somebody may be mid-sentence in the composer.
    var webStale: String { get }
    var webStaleGo: String { get }

    // The Projects page — see `Resources/web/app/js/view/projects.js`.
    //
    // Two questions on one screen. The first is where a session could be started, which is
    // `/v1/places`; the second is asked in front of one of those directories and is the point of
    // the page: **which of this Project's worktrees finished a Feature, and did that delivery
    // reach the branch.** The four outcome names below are the rungs of that ladder, and each
    // sentence beside one says what the rung rests on rather than describing it — every one of
    // them is a stored fact, and a reader deciding whether to go and merge something is entitled
    // to know which fact.
    var webProjects: String { get }
    var webProjectsLede: String { get }
    var webProjectsEmpty: String { get }
    /// Neither read exists on the Cloud path: every read a paired viewer may name carries a
    /// session, and this one's subject is a Project. Said once, rather than drawn as a
    /// control that fails when pressed.
    var webProjectsUnavailable: String { get }
    var webProjectsLoading: String { get }
    var webProjectOpenLabel: String { get }
    var webProjectReading: String { get }
    /// **The heading this page was built around.** Thirty-eight of one repository's
    /// seventy-nine Feature-carrying worktrees are in this state — finished, and never
    /// merged — and it is the first number anybody has had for it.
    var webProjectDelivered: String { get }
    var webProjectDeliveredSay: String { get }
    var webProjectDeliveredNone: String { get }
    var webProjectLanded: String { get }
    var webProjectLandedSay: String { get }
    var webProjectActive: String { get }
    var webProjectActiveSay: String { get }
    var webProjectAbandoned: String { get }
    var webProjectAbandonedSay: String { get }
    var webProjectUnknownOutcome: String { get }
    var webProjectUnknownSay: String { get }
    /// The payload carries no branch: the ledger stores none and the registry that does is
    /// swept, so a field present only for recent tasks would read as an old one's absence.
    /// `clawdline/task/<worktree id>` is the convention, and this label says so.
    var webProjectBranch: String { get }
    var webProjectRuns: String { get }
    var webProjectSeen: String { get }
    /// The query ran and matched nothing, which is not the same as a query that never
    /// answered — the receipt beside this sentence is what tells them apart.
    var webProjectNoWorktrees: String { get }
    var webProjectExcluded: String { get }
    var webProjectUnattributed: String { get }
    var webProjectUnattributedSay: String { get }
    /// The receipt. On screen whenever the route answered and absent whenever it did not,
    /// so that an empty answer cannot be mistaken for a failed one.
    var webProjectRead: String { get }
    var webProjectTruncated: String { get }
    var webProjectNotFound: String { get }
    var webProjectAmbiguous: String { get }
    var webProjectBusy: String { get }
    var webProjectFailed: String { get }

    // Menu bar
    var menuOpen: String { get }
    var menuReveal: String { get }
    var menuMascot: String { get }
    var menuLogin: String { get }
    var menuEditConfig: String { get }
    var menuReload: String { get }
    var menuQuit: String { get }
    var menuNoTarget: String { get }

    /// The one row that says a newer Clawdline exists.
    ///
    /// **It has to name both numbers.** "An update is available" is a sentence somebody has to
    /// take on trust; one that names the release and the build in front of them is a sentence
    /// they can check against the page they are about to open, and that is the difference
    /// between a notice and a nag. The row
    /// is only ever there when there is something to move to, so it is not a status line: it is a
    /// thing to click.
    func updateAvailable(latest: String, installed: String) -> String
    /// One assistant in front of us, worded from ``Compat/Standing``.
    ///
    /// Two arms, and they ask for two different things. `behind` explains why a feature is
    /// missing — nothing to do about it but know. `ahead` is the rare one, and it is the only
    /// line in this app that names three versions at once: it is only ever shown when all three
    /// are true and there is a release to move to, so every word of it has to earn its place.
    func compatNote(_ standing: Compat.Standing) -> String

    // Regular application shell and purpose-driven Setup Center.
    var menuApplication: String { get }
    var menuWindow: String { get }
    var menuHelp: String { get }
    var menuClose: String { get }
    var menuHome: String { get }
    var menuHelpDocumentation: String { get }
    func menuAbout(_ app: String) -> String
    var menuServices: String { get }
    func menuHide(_ app: String) -> String
    var menuHideOthers: String { get }
    var menuShowAll: String { get }
    var menuMinimize: String { get }
    var homeTitle: String { get }
    var homeWelcome: String { get }
    var homePurpose: String { get }
    var homeLocalTitle: String { get }
    var homeLocalSummary: String { get }
    var homeCloudPreviewTitle: String { get }
    var homeCloudPreviewSummary: String { get }
    var homeCloudflareTitle: String { get }
    var homeCloudflareSummary: String { get }
    var homeUnavailable: String { get }
    var setupDetected: String { get }
    var setupNextAction: String { get }
    var setupExpected: String { get }
    var setupRecovery: String { get }
    var setupLocalServerOff: String { get }
    var setupLocalConfigurationFailed: String { get }
    var setupLocalChecking: String { get }
    var setupLocalHealthTransport: String { get }
    var setupLocalHealthTimedOut: String { get }
    func setupLocalHealthHTTP(_ status: Int) -> String
    var setupLocalHealthUnhealthy: String { get }
    var setupLocalHealthInvalid: String { get }
    func setupLocalHealthFailure(_ failure: LocalHealthFailure) -> String
    var setupLocalReady: String { get }
    var setupLocalWaiting: String { get }
    var setupLocalConnected: String { get }
    var setupLocalEnable: String { get }
    var setupLocalRetry: String { get }
    var setupLocalOpen: String { get }
    var setupLocalOpenAgain: String { get }
    var setupFinish: String { get }
    func setupLocalExpected(_ phase: LocalBrowserPhase) -> String
    func setupLocalRecovery(_ phase: LocalBrowserPhase) -> String?
    var setupLocalExpected: String { get }
    var setupLocalRecovery: String { get }
    var setupNoRecovery: String { get }
    var setupLocalReadOnlyAction: String { get }
    var setupReadOnly: String { get }
    var setupLocalDeviceName: String { get }
    func setupTunnelFacts(_ mode: String, _ tunnel: String, _ hostname: String,
                          _ status: String) -> String
    var setupTunnelModeOff: String { get }
    var setupTunnelModeQuick: String { get }
    var setupTunnelModeNamed: String { get }
    var setupTunnelQuickName: String { get }
    var setupNone: String { get }
    var setupControlChosen: String { get }
    var setupTunnelMissing: String { get }
    var setupTunnelOff: String { get }
    var setupTunnelStarting: String { get }
    func setupTunnelReady(_ url: String) -> String
    func setupTunnelWaiting(_ url: String) -> String
    func setupTunnelConnected(_ url: String) -> String
    func setupTunnelExpected(_ phase: CloudflareOnboardingPhase) -> String
    func setupTunnelRecovery(_ phase: CloudflareOnboardingPhase) -> String?
    var setupTunnelExpected: String { get }
    var setupTunnelRecovery: String { get }
    var setupOpenRemoteSettings: String { get }
    var setupCheckTunnel: String { get }
    var setupShowPhoneQR: String { get }
    var setupShowPhoneQRAgain: String { get }
    var setupCloudRelayUnauthorized: String { get }
    func setupCloudFacts(_ account: String, _ credential: String, _ relay: String,
                         _ pairing: String, _ receipt: String) -> String
    func setupCloudExpected(_ decision: CloudPreviewDecision) -> String
    func setupCloudRecovery(_ decision: CloudPreviewDecision) -> String?
    var setupCloudExpected: String { get }
    var setupCloudRecovery: String { get }
    var setupOpenCloudSettings: String { get }
    var setupPairCloudPhone: String { get }
    var setupReviewCloudPreview: String { get }
    var setupProofAbsent: String { get }
    var setupProofReading: String { get }
    var setupProofUnavailable: String { get }
    func setupProofFailed(_ reason: String) -> String
    func setupProofFailed(_ failure: CloudPreviewFailure) -> String
    var setupCloudIdentityReadFailed: String { get }
    var setupCloudRelayFailed: String { get }
    var setupCloudPairingReadFailed: String { get }
    var setupCloudPairingAmbiguous: String { get }
    func setupCloudAccountProved(_ account: String) -> String
    func setupCloudCredentialProved(_ machine: String) -> String
    func setupCloudPairingProved(_ device: String) -> String
    var setupCloudflareDeviceName: String { get }
    var setupScanLiveTunnel: String { get }
    var setupDismissalHint: String { get }
    var setupCompletionFailed: String { get }

    // Alerts
    func hotkeyFailedTitle(_ combo: String) -> String
    func hotkeyFailedBody(_ configPath: String) -> String
    var loginFailed: String { get }
}

extension Copy {
    /// Languages that do not inflect this sentence may keep their established form twice.
    var closeabilityBlockedOne: String { closeabilityBlocked }
    var closeabilityBlockedMany: String { closeabilityBlocked }

    func setupLocalHealthFailure(_ failure: LocalHealthFailure) -> String {
        switch failure {
        case .transport: return setupLocalHealthTransport
        case .timedOut: return setupLocalHealthTimedOut
        case .httpStatus(let status): return setupLocalHealthHTTP(status)
        case .unhealthy: return setupLocalHealthUnhealthy
        case .invalidResponse: return setupLocalHealthInvalid
        }
    }

    func setupLocalExpected(_ phase: LocalBrowserPhase) -> String {
        switch phase {
        case .serverOff, .configurationFailed: return setupLocalChecking
        case .checkingHealth, .healthFailed: return setupLocalReady
        case .readyToOpen: return setupLocalWaiting
        case .awaitingDevice, .connected: return setupLocalConnected
        }
    }

    func setupLocalRecovery(_ phase: LocalBrowserPhase) -> String? {
        HomeRecoveryPolicy.text(
            HomeRecoveryPolicy.local(phase),
            guidance: setupLocalRecovery,
            proved: setupNoRecovery)
    }

    func setupTunnelExpected(_ phase: CloudflareOnboardingPhase) -> String {
        switch phase {
        case .cloudflaredMissing, .tunnelOff: return setupTunnelStarting
        case .starting: return setupTunnelExpected
        case .ready(let url): return setupTunnelWaiting(url)
        case .awaitingDevice(let url), .connected(let url): return setupTunnelConnected(url)
        case .failed: return setupTunnelExpected
        }
    }

    func setupTunnelRecovery(_ phase: CloudflareOnboardingPhase) -> String? {
        HomeRecoveryPolicy.text(
            HomeRecoveryPolicy.tunnel(phase),
            guidance: setupTunnelRecovery,
            proved: setupNoRecovery)
    }

    func setupCloudExpected(_ decision: CloudPreviewDecision) -> String {
        setupCloudExpected
    }

    func setupCloudRecovery(_ decision: CloudPreviewDecision) -> String? {
        HomeRecoveryPolicy.text(
            HomeRecoveryPolicy.cloud(decision),
            guidance: setupCloudRecovery,
            proved: setupNoRecovery)
    }

    func setupProofFailed(_ failure: CloudPreviewFailure) -> String {
        let reason: String
        switch failure {
        case .identityRead: reason = setupCloudIdentityReadFailed
        case .relayUnauthorized: reason = setupCloudRelayUnauthorized
        case .relayFailed: reason = setupCloudRelayFailed
        case .pairingRead: reason = setupCloudPairingReadFailed
        case .pairingAmbiguous: reason = setupCloudPairingAmbiguous
        }
        return setupProofFailed(reason)
    }
}

/// Which language the interface speaks, and the words themselves.
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
