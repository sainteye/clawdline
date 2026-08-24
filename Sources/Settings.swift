import AppKit
import UniformTypeIdentifiers

/// The settings, as controls rather than as a file.
///
/// The config file is still the truth and still hand-editable — `Config.save()` merges around a
/// hand edit on purpose — but knowing a key exists, knowing what it accepts and not putting a
/// comma in the wrong place is a lot to ask of somebody who only wants a different hotkey. So the
/// settings people actually change have a control each.
///
/// Built by hand in AppKit for the same reason the rest of this is: `swiftc` and nothing else.
///
/// **Every change applies immediately.** There is no OK button, because there is nothing to
/// cancel: this writes what the config file would have said, and the app picks it up the same way
/// it would have picked up an edit — which is also why the long tail of settings that live only
/// in the file keep working exactly as they did.
///
/// ## Why tabs, and why hand-drawn ones
///
/// This was one scrolling column of thirty-odd rows. Everything in it worked and nothing in it
/// could be found: the group a setting belongs to was a grey word that had already scrolled off
/// the top by the time you reached the setting itself. Tabs put the group back on screen next to
/// the thing it names, and taking the height back means the window can be wide instead — two
/// columns, nothing to scroll, the whole of a subject visible at once.
///
/// `NSToolbar` in its preferences style would have been fewer lines. It was not taken because of
/// what it brings with it: an icon-over-label chrome in the system's own idiom, drawn from SF
/// Symbols, in a window that is otherwise near-black surfaces, hairlines and pixels. This app
/// already draws a tab strip — ``StackLogHeader``, over the log pane — as words with an accent
/// rule under the current one, and a second strip in a different accent, at different metrics, in
/// different type would be the only control in the app that came from somewhere else.
///
/// The window is dark whatever the system is, for the same reason ``NotchIsland`` is: the card the
/// app actually lives in is `.hudWindow`, which is dark in either appearance, and every surface
/// colour this project owns — `Style.hairline`, `Style.chipFill`, `Style.chipEdge` — is white at a
/// low alpha and simply disappears on a light ground.
///
/// ## What this window does not have
///
/// There is no "open the config file" button. A button is an invitation, and what that one invited
/// you into was hand-editing JSON in whatever the system thinks edits JSON — which is a way to
/// hand somebody a broken config. The path is in the footer as plain selectable text instead:
/// findable by anybody who goes looking, suggested to nobody who is not. "Reload config" in the
/// menu bar is still there for the person who chose to go and edit it.
///
/// That leaves a handful of keys with no control and no signpost — `send_images_as_paste`,
/// `voice_vocabulary`, `status_dir`, `icons_file`, `tmux_path`. They stay that way deliberately:
/// each is documented in `Config.swift` and in `docs/`, each is for somebody who has already read
/// one of those, and a settings window is not obliged to surface every key. `on_state_change` is
/// the exception and gets a block of its own on the hooks tab — see ``hooksPane()``.
final class SettingsWindow: NSObject, NSWindowDelegate {

    static let shared = SettingsWindow()
    private override init() { super.init() }

    private var window: NSWindow?
    private var root: FlippedView?
    private var strip: TabStrip?
    private var panes: [SettingsPane] = []
    private var current = 0
    private var contentWidth: CGFloat = 760

    /// Which language the window was built in. The labels are baked into views at build time, so
    /// picking a different language has to rebuild rather than repaint — otherwise the one control
    /// whose whole job is to change every word in the window is the one control whose effect you
    /// cannot see without quitting.
    private var builtFor = ""

    private var recorder: ChipButton?
    private var monitor: Any?

    private var hooksButton: ChipButton?
    private var hooksCard: NoteCard?
    private var deviceChips: DeviceChips?
    private var tunnelCard: NoteCard?
    private var mascotMark: MascotView?
    private var scopeView: AppScopeView?
    private var scopeSwitch: SwitchView?

    /// Readings that go stale while the window is open — whether a tunnel came up, whether a
    /// session has reported through the hooks, what is paired. Cheap enough to just ask once a
    /// second; the alternative is a window that says the tunnel is starting for as long as you
    /// leave it open.
    private var live: Timer?

    func show() {
        if window != nil, builtFor != String(describing: type(of: L.t)) { tearDown() }
        if window == nil { window = build() }
        refreshLive()
        startLiveTimer()
        NSApp.activate(ignoringOtherApps: true)
        window?.center()
        window?.makeKeyAndOrderFront(nil)
    }

    func windowWillClose(_ notification: Notification) {
        stopRecording()
        live?.invalidate()
        live = nil
        // The window itself is kept rather than rebuilt: which tab you were on is a thing you
        // expect to still be true the second time you open it.
    }

    private func tearDown() {
        stopRecording()
        window?.orderOut(nil)
        window = nil
        root = nil
        strip = nil
        panes = []
    }

    private func startLiveTimer() {
        guard live == nil else { return }
        let t = Timer(timeInterval: 1, repeats: true) { [weak self] _ in
            guard let self, self.window?.isVisible == true else { return }
            self.refreshLive()
        }
        RunLoop.main.add(t, forMode: .common)
        live = t
    }

    // MARK: - Building it

    private func build() -> NSWindow {
        builtFor = String(describing: type(of: L.t))
        panes = makePanes()

        let strip = TabStrip(titles: panes.map(\.title))
        strip.onPick = { [weak self] i in self?.showPane(i) }
        let mark = mascotHeaderMark()
        strip.mark = mark
        mascotMark = mark
        self.strip = strip

        // One width for every tab, and it is the width the widest tab needs. Measured rather than
        // chosen, because the labels are translated: "Eine Pause beendet den Satz" is half again
        // the length of "A pause ends a sentence", and a number picked while reading the English
        // is a layout that clips for a third of the people using it.
        var width = strip.naturalWidth + Metric.pad * 2
        for pane in panes { width = max(width, pane.naturalWidth + Metric.pad * 2) }
        contentWidth = width.rounded()

        let root = FlippedView(frame: NSRect(x: 0, y: 0, width: contentWidth, height: 400))
        root.addSubview(strip)
        root.addSubview(footer())
        self.root = root

        let w = NSWindow(contentRect: root.frame,
                         styleMask: [.titled, .closable, .miniaturizable],
                         backing: .buffered, defer: false)
        w.title = L.t.settingsTitle
        // The title bar takes the window's own colour rather than the system's, so it and the tab
        // strip under it read as one surface instead of two stacked ones.
        w.titlebarAppearsTransparent = true
        w.backgroundColor = Style.ink
        w.appearance = NSAppearance(named: .darkAqua)
        w.contentView = root
        w.isReleasedWhenClosed = false
        w.delegate = self
        // The root takes no keys, so nothing starts focused. Otherwise the first control in the
        // window opens wearing a ring, which on a chip button reads as "already pressed".
        w.initialFirstResponder = root

        showPane(min(current, panes.count - 1))
        return w
    }

    /// Show one tab, and make the window exactly as tall as that tab.
    ///
    /// A fixed height would have to be the tallest tab's, which leaves the hooks tab — a button and
    /// two paragraphs — sitting in a field of nothing. The width stays put so the window does not
    /// appear to breathe sideways as you move along the strip; only the bottom edge moves, and the
    /// title bar stays where the hand that is about to reach for it last saw it.
    private func showPane(_ index: Int) {
        guard let root, panes.indices.contains(index) else { return }
        current = index
        strip?.current = index

        for pane in panes where pane.view.superview != nil { pane.view.removeFromSuperview() }
        let pane = panes[index]
        let inner = contentWidth - Metric.pad * 2
        let paneHeight = max(pane.layout(width: inner), 140)
        pane.view.frame = NSRect(x: Metric.pad, y: Metric.stripHeight + 22,
                                 width: inner, height: paneHeight)
        root.addSubview(pane.view)

        let height = (Metric.stripHeight + 22 + paneHeight + 24 + Metric.footerHeight).rounded()
        strip?.frame = NSRect(x: 0, y: 0, width: contentWidth, height: Metric.stripHeight)
        for view in root.subviews where view.identifier == Self.footerID {
            view.frame = NSRect(x: 0, y: height - Metric.footerHeight,
                                width: contentWidth, height: Metric.footerHeight)
        }
        root.frame.size = NSSize(width: contentWidth, height: height)

        guard let w = window else { return }
        let sized = w.frameRect(forContentRect: NSRect(x: 0, y: 0, width: contentWidth, height: height))
        var next = w.frame
        // Grow downwards. Anchoring the bottom instead would walk the title bar up and down the
        // screen every time you changed tab, and the title bar is the part of the window the
        // pointer is most often aimed at.
        next.origin.y += next.height - sized.height
        next.size = sized.size
        w.setFrame(next, display: true)
    }

    private static let footerID = NSUserInterfaceItemIdentifier("clawdline.settings.footer")

    /// Where the file is. Text, not a button.
    ///
    /// This window is not the whole of the settings and should not pretend to be: a few keys have
    /// no control here on purpose, and the one thing somebody needs in order to reach them is to
    /// know where to look. Saying so costs a line of monospace. Offering to open it for them is a
    /// different thing entirely — see the note at the top of this file.
    private func footer() -> NSView {
        let box = NSView(frame: NSRect(x: 0, y: 0, width: contentWidth, height: Metric.footerHeight))
        box.identifier = Self.footerID

        let rule = Hairline(frame: NSRect(x: 0, y: Metric.footerHeight - 1,
                                          width: contentWidth, height: 1))
        box.addSubview(rule)

        let path = NSTextField(labelWithString: Config.shared.fileURL.path
            .replacingOccurrences(of: NSHomeDirectory(), with: "~"))
        path.font = Metric.monoFont
        path.textColor = Metric.faint
        path.lineBreakMode = .byTruncatingMiddle
        path.isSelectable = true
        path.frame = NSRect(x: Metric.pad, y: 12, width: contentWidth - Metric.pad * 2, height: 16)
        box.addSubview(path)
        return box
    }

    /// The character, small, at the head of the window — and it is the one *you* chose.
    ///
    /// The only decoration in here, and it earns its place by answering a question the window asks
    /// two rows further down: the mascot popup lists packs by name and nothing anywhere showed you
    /// what a name looks like. Change the pack and this changes with it.
    ///
    /// Still, not animated. A sixty-frame timer running behind a window somebody left open while
    /// they think about a hotkey is a cost with nothing on the other side of it — the character is
    /// here to be recognised, not watched.
    private func mascotHeaderMark() -> MascotView {
        let view = MascotView(frame: .zero)
        view.frozenTime = 0.3
        fitMark(view)
        return view
    }

    /// Snap the sprite so one drawn pixel is a whole number of points.
    ///
    /// `fit(height:)` on its own gives a cell 2.18pt wide, and every boundary in the art then lands
    /// off-pixel and antialiases — a creature made of squares comes out slightly furry. Asking for
    /// the nearest whole cell gives up a point of height and keeps the edges.
    private func fitMark(_ view: MascotView) {
        view.fit(height: 22)
        if let pack = view.pack, pack.cellSize > 0 {
            view.scale = max(1, (pack.cellSize * view.scale).rounded()) / pack.cellSize
        }
        view.frame.size = view.boxSize
    }

    // MARK: - The tabs
    //
    // Five, cut from the six sections the scrolling form had. Three changes, all of them from
    // reading what is actually in those sections:
    //
    // - **"The bar" and "Reading" share a tab.** They were never two subjects. The reading pane is
    //   something the bar opens, inside the bar, and the two lists side by side are exactly the two
    //   columns this window wanted: the card down the left, what it shows down the right.
    // - **"Send images as images" is gone from the window.** `Targets.send(_:to:)` already falls
    //   back to sending a path on its own whenever the paste route cannot work — not a Claude Code
    //   session, an image whose bytes will not load — so the switch offered a choice between "the
    //   good behaviour, with a fallback" and "the fallback, always". Nobody has the information to
    //   make that choice. `send_images_as_paste` still works in the file.
    // - **The hooks tab gained `on_state_change`**, as a statement rather than a control. See
    //   ``hooksPane()``.

    private func makePanes() -> [SettingsPane] {
        [generalPane(), barPane(), dictationPane(), remotePane(), hooksPane()]
    }

    private func generalPane() -> SettingsPane {
        let pane = SettingsPane(title: L.t.settingsGeneral)

        pane.left.row(L.t.settingsHotkey, hotkeyButton())

        let scope = AppScopeView(ids: Config.shared.scopeApp
            .split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty })
        scope.onChange = { [weak self] ids in self?.scopeChanged(ids) }
        scopeView = scope

        let global = SwitchView(isOn: Config.shared.scopeApp.isEmpty) { [weak self] on in
            self?.globalScopeChanged(on)
        }
        scopeSwitch = global
        scope.isActive = !global.isOn

        pane.left.row(L.t.settingsLanguage, languagePopUp())
        pane.left.row(L.t.menuMascot, mascotPopUp())
        // The tallest thing on the tab, so it goes at the foot of the column it is in rather than
        // in the middle of it — a list that grows as you add to it should push nothing around.
        pane.left.row(L.t.settingsScopeGlobal, global)
        pane.left.block(label: L.t.settingsScope, view: scope, hint: L.t.settingsScopeHint)

        pane.right.row(L.t.settingsReopen,
                       switchFor({ Config.shared.reopenOnReturn }, { Config.shared.reopenOnReturn = $0 }),
                       hint: L.t.settingsReopenHint)
        pane.right.row(L.t.settingsFollow,
                       switchFor({ Config.shared.followTarget }, { Config.shared.followTarget = $0 }),
                       hint: L.t.settingsFollowHint)
        pane.right.row(L.t.settingsCodexAutoName,
                       switchFor({ Config.shared.codexAutoName },
                                 { Config.shared.codexAutoName = $0 }),
                       hint: L.t.settingsCodexAutoNameHint)
        pane.right.row(L.t.settingsNotch,
                       switchFor({ Config.shared.notch }, { Config.shared.notch = $0 }),
                       hint: L.t.settingsNotchHint)
        return pane
    }

    private func barPane() -> SettingsPane {
        let pane = SettingsPane(title: L.t.settingsBar)

        pane.left.head(L.t.settingsBar)
        pane.left.row(L.t.settingsPosition, slider(0.05...0.80,
            get: { Double(Config.shared.yFraction) },
            set: { Config.shared.yFraction = CGFloat($0) },
            format: { "\(Int($0 * 100))%" }))
        pane.left.row(L.t.settingsWidth, slider(360...1400,
            get: { Double(Config.shared.width) },
            set: { Config.shared.width = CGFloat($0) },
            format: { "\(Int($0)) pt" }))
        pane.left.row(L.t.settingsOpacity, slider(0...1,
            get: { Config.shared.cardOpacity },
            set: { Config.shared.cardOpacity = $0 },
            format: { "\(Int($0 * 100))%" }))

        pane.right.head(L.t.settingsReading)
        pane.right.row(L.t.settingsShow, outputModePopUp())
        pane.right.row(L.t.settingsPaneHeight, slider(80...900,
            get: { Double(Config.shared.outputHeight) },
            set: { Config.shared.outputHeight = CGFloat($0) },
            format: { "\(Int($0)) pt" }))
        pane.right.row(L.t.settingsTextSize, slider(8...28,
            get: { Double(Config.shared.outputSize) },
            set: { Config.shared.outputSize = CGFloat($0) },
            format: { String(format: "%.1f pt", $0) }))
        pane.right.row(L.t.settingsPaneFont, fontPopUp())
        pane.right.row(L.t.settingsBlur, slider(0...1,
            get: { Config.shared.backdropStrength },
            set: { Config.shared.backdropStrength = $0 },
            format: { "\(Int($0 * 100))%" }))
        pane.right.row(L.t.settingsNewestFirst,
                       switchFor({ Config.shared.outputNewestFirst },
                                 { Config.shared.outputNewestFirst = $0 }))
        return pane
    }

    private func dictationPane() -> SettingsPane {
        let pane = SettingsPane(title: L.t.settingsVoice)

        pane.left.row(L.t.settingsEngine, enginePopUp())
        pane.left.row(L.t.settingsSettle, slider(0...8,
            get: { Config.shared.voiceSettleSeconds },
            set: { Config.shared.voiceSettleSeconds = $0 },
            format: { $0 == 0 ? L.t.settingsOff : L.t.settingsSeconds($0) }))
        pane.left.row(L.t.settingsStop, slider(0...30,
            get: { Config.shared.voiceStopSeconds },
            set: { Config.shared.voiceStopSeconds = $0 },
            format: { $0 == 0 ? L.t.settingsOff : L.t.settingsSeconds($0) }))

        // What the recogniser popup will actually get you, on this machine, right now. "Whisper" is
        // a thing you can choose with nothing installed, and the difference between choosing it and
        // having it is a binary and a model file — so the window says which of the two this is.
        let status = Whisper.status(binary: Config.shared.whisperBinary,
                                    model: Config.shared.whisperModel)
        var ready = false
        if case .ready = status { ready = true }
        let card = NoteCard()
        card.dot = ready ? .live : .warn
        card.text = L.t.dictationStatus(status)
        pane.right.block(label: nil, view: card, hint: nil)
        return pane
    }

    private func remotePane() -> SettingsPane {
        let pane = SettingsPane(title: L.t.settingsRemote)

        pane.left.row(L.t.settingsRemoteServe,
                      switchFor({ Config.shared.remote }, { Config.shared.remote = $0 }),
                      hint: L.t.settingsRemoteHint)
        pane.left.row(L.t.settingsRemoteWrite,
                      switchFor({ Config.shared.remoteWrite }, { Config.shared.remoteWrite = $0 }),
                      hint: L.t.settingsRemoteWriteHint)
        // Both off by default, and both here rather than in General because what they control is
        // a phone — there is no notification without a paired device, so the switch belongs
        // beside the devices rather than in a list of things about the bar.
        pane.left.row(L.t.settingsPushFinish,
                      switchFor({ Config.shared.pushOnFinish },
                                { Config.shared.pushOnFinish = $0 }),
                      hint: L.t.settingsPushFinishHint)
        pane.left.row(L.t.settingsPushDeploy,
                      switchFor({ Config.shared.pushOnDeploy },
                                { Config.shared.pushOnDeploy = $0 }),
                      hint: L.t.settingsPushDeployHint)

        pane.right.row(L.t.settingsTunnel, tunnelPopUp(), hint: L.t.settingsTunnelHint)
        pane.right.row(L.t.settingsTunnelHostname, hostnameField())

        let card = NoteCard()
        card.mono = true
        tunnelCard = card
        pane.right.block(label: nil, view: card, hint: nil)

        // On this tab rather than in General because it gates the same thing the rest of the tab
        // gates: something that is not the person at the keyboard getting a session started on
        // this Mac. See Sources/Orchestrator.swift.
        pane.right.head(L.t.settingsOrchestrator)
        pane.right.row(L.t.settingsOrchestratorEnabled,
                       switchFor({ Config.shared.orchestratorEnabled },
                                 { Config.shared.orchestratorEnabled = $0 }),
                       hint: L.t.settingsOrchestratorEnabledHint)
        pane.right.row(L.t.settingsOrchestratorMax, childrenPopUp(),
                       hint: L.t.settingsOrchestratorMaxHint)
        pane.right.row(L.t.settingsOrchestratorNotify,
                       switchFor({ Config.shared.orchestratorNotifyRoot },
                                 { Config.shared.orchestratorNotifyRoot = $0 }),
                       hint: L.t.settingsOrchestratorNotifyHint)
        pane.right.row(L.t.settingsOrchestratorClose, lingerPopUp(),
                       hint: L.t.settingsOrchestratorCloseHint)

        pane.wide.block(label: L.t.settingsRemoteDevices, view: devicesControl(),
                        hint: L.t.settingsRemotePhoneHint)
        return pane
    }

    /// How many child sessions may run at once, 1 to 10 — the range `Config` accepts, so a number
    /// picked here is a number the file will still hold after a reload. A popup rather than a
    /// slider because ten stops is a list, and the value is the label.
    private func childrenPopUp() -> NSView {
        popUp((1...10).map { (String($0), String($0)) },
              current: String(Config.shared.orchestratorMaxChildren)) {
            Config.shared.orchestratorMaxChildren = Int($0) ?? Config.shared.orchestratorMaxChildren
        }
    }

    /// What becomes of a child's tab after it reports. Three stops rather than a number: the
    /// choice people actually make is now, in a bit, or never — and three minutes is the only
    /// "in a bit" anybody would type. A hand-edited `orchestrator_child_linger` between the stops
    /// shows as the nearest one and is left alone until this control is touched.
    private func lingerPopUp() -> NSView {
        let stops: [(String, String)] = [("0", L.t.settingsOrchestratorCloseNow),
                                         ("180", L.t.settingsOrchestratorCloseLinger),
                                         ("-1", L.t.settingsOrchestratorCloseKeep)]
        let current = Config.shared.orchestratorChildLinger
        let shown = current < 0 ? "-1" : (current == 0 ? "0" : "180")
        return popUp(stops, current: shown) {
            Config.shared.orchestratorChildLinger = Int($0) ?? Config.shared.orchestratorChildLinger
        }
    }

    /// The hooks, in and out.
    ///
    /// In: Claude Code telling this app that a turn started. Out: this app telling your program
    /// that a session changed state. They are the same subject from two sides and they belong on
    /// the same tab.
    ///
    /// `on_state_change` gets a statement of what is currently set rather than a control, and the
    /// reason is in `Config.onStateChange`'s own note: it is argv, not a command line. A single
    /// text box invites exactly the word-splitting that note forbids — a path with a space in it is
    /// a path, not two arguments — and the honest control is an argument-list editor, which is a
    /// bigger thing than this window should grow for a key most people never set. But it is also
    /// the extension point everything else hangs off, and **a feature nobody can find is a feature
    /// nobody has**, so the key, its exact JSON and what it does are on screen where somebody
    /// looking at hooks will meet them.
    private func hooksPane() -> SettingsPane {
        let pane = SettingsPane(title: L.t.settingsHooks)

        let card = NoteCard()
        let button = ChipButton(title: L.t.settingsHooksInstall)
        button.action = { [weak self] in self?.toggleHooks() }
        card.trailing = button
        hooksButton = button
        hooksCard = card
        pane.left.block(label: nil, view: card, hint: L.t.settingsHooksHint)
        // Whose file the button writes into. Naming the path is the difference between "a switch in
        // this app" and "an edit to somebody else's settings", which is what it actually is.
        pane.left.mono(HookBridge.settingsURL.path
            .replacingOccurrences(of: NSHomeDirectory(), with: "~"))

        let outgoing = NoteCard()
        outgoing.mono = true
        outgoing.dot = Config.shared.onStateChange.isEmpty ? .idle : .live
        // The key exactly as it is written in the file, so what is on screen is something you can
        // copy rather than something you have to translate back into JSON.
        outgoing.text = "\"on_state_change\": ["
            + Config.shared.onStateChange.map { "\"\($0)\"" }.joined(separator: ", ") + "]"
        pane.right.block(label: L.t.settingsStateHook, view: outgoing,
                         hint: L.t.settingsStateHookHint)
        return pane
    }

    // MARK: - Keeping the live readings honest

    private func refreshLive() {
        refreshHooksControl()
        refreshDevices()
        refreshTunnel()
    }

    /// Something that changes size changed, so the tab is measured again. Only called when a
    /// reading actually moved: re-laying out on every tick would resize the window under the
    /// pointer once a second for no reason.
    private func relayoutCurrent() {
        showPane(current)
    }

    // MARK: - Where the hotkey fires

    /// The apps the hotkey fires in, chosen by picking them.
    ///
    /// This was a text field holding `com.googlecode.iterm2`. Two things were wrong with it and
    /// only one was a bug. The bug: emptying the box means "everywhere", so clearing it by accident
    /// silently changed what the hotkey did, with nothing on screen saying what had been there or
    /// offering to put it back. The design mistake underneath: **nobody knows what a bundle id
    /// is.** Asking for one is asking for a string with no discoverable spelling, and a placeholder
    /// would only have made that a well-labelled dead end.
    ///
    /// So the control is a list of applications with their own icons and their own names, and the
    /// way to add one is to pick it — nearly always out of the apps already running, because the
    /// terminal somebody wants is the one open in front of them.
    ///
    /// **The stored format does not change.** `scope_app` is still a comma-separated list of bundle
    /// ids and a hand-edited config still works, including ids whose app is not installed: those
    /// show as their bare id and are written back untouched, because quietly dropping a line
    /// somebody wrote is worse than showing something plain.
    private func scopeChanged(_ ids: [String]) {
        Config.shared.scopeApp = ids.joined(separator: ",")
        // Empty and "in every app" are the same state, and their looking like two different states
        // is the whole of the original bug. Taking the last app out now says so out loud, and the
        // list stays on screen greyed — which is what makes it recoverable.
        if ids.isEmpty { scopeSwitch?.isOn = true }
        scopeView?.isActive = !ids.isEmpty
        apply()
        relayoutCurrent()
    }

    private func globalScopeChanged(_ on: Bool) {
        if on {
            Config.shared.scopeApp = ""
            scopeView?.isActive = false
        } else {
            // Back to what is still on screen, or — if the last row was taken out rather than the
            // switch being thrown — to what was there before that. Only a config that has always
            // been global falls through to the one terminal this is known to work with, because an
            // empty list here would silently mean the opposite of what was just clicked.
            let remembered = scopeView?.remembered ?? []
            let restored = scopeView?.ids.isEmpty == false ? scopeView!.ids
                : (remembered.isEmpty ? ["com.googlecode.iterm2"] : remembered)
            scopeView?.ids = restored
            scopeView?.isActive = true
            Config.shared.scopeApp = restored.joined(separator: ",")
        }
        apply()
        relayoutCurrent()
    }

    // MARK: - Remote

    /// What can currently reach this Mac, and one button that stops all of it.
    ///
    /// The list is here rather than tucked away because it answers a question somebody only ever
    /// asks in a hurry — *what is connected to my machine right now* — and the button next to it
    /// exists for the same moment. Nothing about that moment is improved by a confirmation sheet,
    /// so there is not one: revoking is instant and re-pairing is cheap.
    ///
    /// Names as chips rather than one run of text separated by dots. The question is *how many, and
    /// which*, and the eye counts shapes faster than it parses a sentence — which is the same
    /// reason the key hints along the bottom of the card are chips.
    private func devicesControl() -> NSView {
        let box = StackedRow()

        let chips = DeviceChips()
        chips.empty = L.t.settingsRemoteNoDevices
        chips.onRemove = { [weak self] id in
            RemoteAuth.revoke(id: id)
            self?.refreshDevices()
        }
        deviceChips = chips
        box.top = chips

        let open = ChipButton(title: L.t.settingsRemoteOpen)
        open.action = { [weak self] in self?.openRemote() }
        let phone = ChipButton(title: L.t.settingsRemotePhone, prominent: true)
        phone.action = { [weak self] in self?.pairPhone() }
        let revoke = ChipButton(title: L.t.settingsRemoteRevokeAll)
        revoke.action = { [weak self] in self?.revokeAllDevices() }
        box.buttons = [open, phone, revoke]

        refreshDevices()
        return box
    }

    private func refreshDevices() {
        let rows = RemoteAuth.approvedDevices.map {
            DeviceChips.Row(id: $0.id, name: $0.name, lastSeen: $0.lastSeen)
        }
        guard let chips = deviceChips, chips.devices != rows else { return }
        chips.devices = rows
        relayoutCurrent()
    }

    private func revokeAllDevices() {
        RemoteAuth.revokeAll()
        refreshDevices()
    }

    private func tunnelPopUp() -> NSView {
        popUp([(L.t.settingsOff, "off"),
               (L.t.settingsTunnelQuick, "quick"),
               (L.t.settingsTunnelNamed, "named")],
              current: Config.shared.remoteTunnel) { Config.shared.remoteTunnel = $0 }
    }

    private func hostnameField() -> NSView {
        let field = MemoField(value: Config.shared.remoteHostname, example: "clawd.example.com")
        field.onCommit = { [weak self] text in
            Config.shared.remoteHostname = text
            self?.apply()
        }
        return field
    }

    /// What the tunnel is doing, in the words the tunnel itself used.
    ///
    /// Its failures are sentences meant for a person — "pair a device first", "the local server is
    /// off" — and passing them through unchanged is better than translating them into a generic
    /// "could not start": the useful part of each one is the specific thing to go and do.
    private func refreshTunnel() {
        guard let card = tunnelCard else { return }
        let was = card.text
        switch RemoteTunnel.shared.state {
        case .off:
            card.text = RemoteTunnel.isInstalled ? "" : "cloudflared is not installed."
            card.dot = .idle
        case .starting:
            card.text = "…"
            // The one place in this window where something is genuinely in progress, so it gets the
            // mark the rest of the app uses for that: eight pixels going round, dim, the same one a
            // working session has beside it in the list.
            card.dot = .busy
        case .up(let url):
            card.text = url
            card.dot = .live
        case .failed(let why):
            card.text = why
            card.dot = .warn
        }
        if card.text != was { relayoutCurrent() }
    }

    /// Open the interface with a key already in it.
    ///
    /// The page needs a token and there is nowhere sensible for somebody to type one, so pressing
    /// this mints a device of its own and hands it over in the URL. A device of its own rather than
    /// the machine's, because they are not the same thing — the local token can send and
    /// administer, and a browser tab should start with neither until somebody says so.
    private func openRemote() {
        guard Config.shared.remote else { return }
        let caps: Set<RemoteAuth.Capability> = Config.shared.remoteWrite ? [.read, .send] : [.read]
        let made = RemoteAuth.addDevice(name: "Browser on this Mac", caps: caps)
        guard let url = URL(string:
            "http://127.0.0.1:\(Config.shared.remotePort)/?t=\(made.token)") else { return }
        NSWorkspace.shared.open(url)
        refreshDevices()
    }

    /// Mint a key for a phone and put it on screen as something a camera can read.
    ///
    /// Its own device, not this Mac's token: a photograph of a screen is a thing that happens, and
    /// what should survive that is a row in the list above with a name on it that can be taken
    /// away — not the key the machine uses for itself.
    private func pairPhone() {
        guard Config.shared.remote else { return }
        let caps: Set<RemoteAuth.Capability> = Config.shared.remoteWrite ? [.read, .send] : [.read]
        let made = RemoteAuth.addDevice(name: "Phone", caps: caps)
        let url = RemoteQR.signInURL(token: made.token,
                                     hostname: Config.shared.remoteHostname,
                                     tunnel: RemoteTunnel.shared.state,
                                     port: Config.shared.remotePort)
        refreshDevices()
        showQR(url)
    }

    private var qrWindow: NSWindow?

    private func showQR(_ url: String) {
        let side: CGFloat = 300
        let content = NSView(frame: NSRect(x: 0, y: 0, width: side + 40, height: side + 108))

        let title = NSTextField(labelWithString: L.t.pairingScanTitle)
        title.font = NSFont.systemFont(ofSize: 13, weight: .semibold)
        title.alignment = .center
        title.frame = NSRect(x: 20, y: side + 74, width: side, height: 20)
        content.addSubview(title)

        let image = NSImageView(frame: NSRect(x: 20, y: 60, width: side, height: side))
        image.image = RemoteQR.image(for: url, side: side)
        // A QR code is squares; letting AppKit smooth it is how a code that should read at arm's
        // length ends up needing a second try.
        image.imageScaling = .scaleNone
        image.wantsLayer = true
        image.layer?.backgroundColor = NSColor.white.cgColor
        content.addSubview(image)

        // The address in text as well, because a phone with no camera permission still has a
        // keyboard, and because somebody will want to see what they are about to scan.
        let text = NSTextField(wrappingLabelWithString: url)
        text.font = NSFont.monospacedSystemFont(ofSize: 9, weight: .regular)
        text.textColor = Metric.faint
        text.alignment = .center
        text.isSelectable = true
        text.frame = NSRect(x: 20, y: 14, width: side, height: 38)
        content.addSubview(text)

        let w = NSWindow(contentRect: content.frame,
                         styleMask: [.titled, .closable], backing: .buffered, defer: false)
        w.title = L.t.settingsRemotePhone
        w.appearance = NSAppearance(named: .darkAqua)
        w.backgroundColor = Style.ink
        w.titlebarAppearsTransparent = true
        w.contentView = content
        w.isReleasedWhenClosed = false
        w.center()
        w.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        qrWindow = w
    }

    // MARK: - Claude Code hooks

    /// A button and a sentence saying what it did.
    ///
    /// The sentence is the point. This writes into `~/.claude/settings.json`, which is somebody
    /// else's file and often somebody else's business — a team may manage it, a plugin may rewrite
    /// it — so "the button says installed" is not a claim worth making on its own. Whether a
    /// session has ever actually reported is the only evidence that the wiring works, and it costs
    /// nothing to look: it is the newest note on disk.
    private func refreshHooksControl() {
        let installed = HookBridge.isInstalled
        hooksButton?.title = installed ? L.t.settingsHooksRemove : L.t.settingsHooksInstall
        // Heard from within the day. Longer and this would go on saying "reporting" about a machine
        // that has not run Claude Code since last week, which is the reassurance the line exists to
        // withhold.
        let heard = HookBridge.lastHeard.map { Date().timeIntervalSince($0) < 86_400 } ?? false
        let text = !installed ? L.t.settingsHooksOff : (heard ? L.t.settingsHooksLive : L.t.settingsHooksOn)
        guard let card = hooksCard else { return }
        let changed = card.text != text
        card.text = text
        card.dot = !installed ? .idle : (heard ? .live : .warn)
        if changed { relayoutCurrent() }
    }

    private func toggleHooks() {
        let problem = HookBridge.isInstalled ? HookBridge.uninstall() : HookBridge.install()
        refreshHooksControl()
        guard let problem else { return }
        let a = NSAlert()
        a.messageText = L.t.settingsHooks
        a.informativeText = problem
        a.alertStyle = .warning
        a.runModal()
    }

    // MARK: - The controls that are not a switch or a slider

    /// A button that listens for one key combination and writes it down.
    ///
    /// Not a text field. "option+space" is a thing you know how to spell only if you have read the
    /// parser, and the failure mode of typing it wrong is a hotkey that silently does not register.
    /// Pressing the keys you mean cannot be spelled wrong.
    private func hotkeyButton() -> NSView {
        let b = ChipButton(title: HotKey.display(Config.shared.hotKey))
        b.minimumWidth = 128
        b.action = { [weak self, weak b] in
            guard let b else { return }
            self?.startRecording(b)
        }
        return b
    }

    private func startRecording(_ button: ChipButton) {
        stopRecording()
        recorder = button
        button.title = L.t.settingsRecording
        // The lit-and-waiting look a stack row's button has once it is armed: something has been
        // pressed and the next thing you do is the answer.
        button.armed = true
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.keyDown, .flagsChanged]) {
            [weak self] event in
            guard let self, event.type == .keyDown else { return event }
            // Escape means "leave it alone", which a recorder without a way out does not offer.
            if event.keyCode == 53 {
                self.stopRecording()
                return nil
            }
            guard let spec = HotKey.spec(forKeyCode: event.keyCode, flags: event.modifierFlags)
            else { return nil }   // swallowed: a bare letter is not an answer, and it is not text either
            Config.shared.hotKey = spec
            self.apply()
            self.stopRecording()
            return nil
        }
    }

    private func stopRecording() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
        recorder?.armed = false
        recorder?.title = HotKey.display(Config.shared.hotKey)
        recorder = nil
    }

    private func languagePopUp() -> NSView {
        var options: [(String, String)] = [(L.t.settingsAuto, "auto")]
        // The tags the catalog actually resolves, each shown in its own language, because a list of
        // languages written in a language you do not read is not a list you can pick from.
        for tag in ["en", "zh-Hant", "zh-Hans", "ja", "ko", "es", "pt", "fr", "de", "ru", "it",
                    "hi", "id", "tr"] {
            let name = Locale(identifier: tag).localizedString(forIdentifier: tag) ?? tag
            options.append((name.prefix(1).uppercased() + name.dropFirst(), tag))
        }
        return popUp(options, current: Config.shared.language) { [weak self] value in
            Config.shared.language = value
            // Every label in this window is now in the wrong language, so it is rebuilt rather than
            // repainted. Deferred by one turn of the run loop, because the popup that asked for
            // this is inside the view tree that is about to be thrown away.
            DispatchQueue.main.async {
                guard let self else { return }
                self.tearDown()
                self.show()
            }
        }
    }

    private func mascotPopUp() -> NSView {
        let names = MascotPack.available()
        return popUp(names.map { ($0, $0) }, current: Config.shared.mascot) { [weak self] value in
            Config.shared.mascot = value
            guard let mark = self?.mascotMark else { return }
            mark.reload()
            self?.fitMark(mark)
            self?.strip?.needsLayout = true
            mark.needsDisplay = true
        }
    }

    private func outputModePopUp() -> NSView {
        popUp([(L.t.settingsAuto, "auto"),
               (L.t.settingsTranscript, "transcript"),
               (L.t.settingsTerminal, "terminal")],
              current: Config.shared.outputMode) { Config.shared.outputMode = $0 }
    }

    private func enginePopUp() -> NSView {
        popUp([(L.t.settingsAuto, "auto"), ("Apple", "apple"), ("Whisper", "whisper")],
              current: Config.shared.voiceEngine) { Config.shared.voiceEngine = $0 }
    }

    /// Monospaced faces only. The pane draws a terminal's box-drawing characters, and in a
    /// proportional face they do not line up — which is a setting you can only get wrong.
    private func fontPopUp() -> NSView {
        var names = NSFontManager.shared.availableFontFamilies.filter { family in
            guard let font = NSFont(name: family, size: 12) else { return false }
            return font.isFixedPitch
        }
        if !names.contains(Config.shared.outputFont) { names.insert(Config.shared.outputFont, at: 0) }
        return popUp(names.map { ($0, $0) }, current: Config.shared.outputFont) {
            Config.shared.outputFont = $0
        }
    }

    private func popUp(_ options: [(label: String, value: String)], current: String,
                       _ set: @escaping (String) -> Void) -> ChoicePopUp {
        ChoicePopUp(options: options, current: current) { [weak self] value in
            set(value)
            self?.apply()
        }
    }

    private func slider(_ range: ClosedRange<Double>, get: () -> Double,
                        set: @escaping (Double) -> Void,
                        format: @escaping (Double) -> String) -> NSView {
        ValueSlider(range: range, value: get(), format: format) { [weak self] v in
            set(v)
            self?.apply()
        }
    }

    private func switchFor(_ get: () -> Bool, _ set: @escaping (Bool) -> Void) -> SwitchView {
        SwitchView(isOn: get()) { [weak self] on in
            set(on)
            self?.apply()
        }
    }

    /// Save, and tell the app. Same path an edit to the file takes, so nothing here can drift away
    /// from what hand-editing does.
    fileprivate func apply() {
        Config.shared.save()
        NotificationCenter.default.post(name: .clawdlineConfigChanged, object: nil)
    }
}

extension Notification.Name {
    /// Posted when the settings window changes something. The delegate re-applies everything that
    /// has to be re-applied — the hotkey, the language, the mascot, the island — in one place,
    /// which is the same place "Reload config" has always used.
    static let clawdlineConfigChanged = Notification.Name("dev.sainteye.clawdline.configChanged")
}

// MARK: - Measurements and ink

private enum Metric {
    static let pad: CGFloat = 26            // window edge to content
    static let gutter: CGFloat = 36         // between the two columns
    static let rowGap: CGFloat = 18         // between a label and the thing it names
    static let rowPad: CGFloat = 8          // above and below a row's contents
    static let stripHeight: CGFloat = 46
    static let footerHeight: CGFloat = 40
    static let minColumn: CGFloat = 316
    static let maxColumn: CGFloat = 452
    /// The widest a paragraph is allowed to get, whatever the band it sits in. Past about this
    /// the eye starts losing its place on the way back to the left margin.
    static let maxMeasure: CGFloat = 600

    static let labelFont = NSFont.systemFont(ofSize: 12)
    static let hintFont = NSFont.systemFont(ofSize: 10.5)
    static let headFont = NSFont.systemFont(ofSize: 10, weight: .semibold)
    static let tabFont = NSFont.systemFont(ofSize: 12.5, weight: .medium)
    static let noteFont = NSFont.systemFont(ofSize: 11.5)
    static let monoFont = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)

    /// Stated rather than taken from the system's semantic colours. The window is pinned to dark,
    /// so these are known quantities — and `tertiaryLabelColor` on this ground is dim enough that a
    /// hint written in it is a hint nobody reads.
    static let label = NSColor(white: 1, alpha: 0.92)
    static let soft = NSColor(white: 1, alpha: 0.62)
    static let faint = NSColor(white: 1, alpha: 0.44)
}

/// For text this file draws itself — a tab, the word on a chip.
private func textWidth(_ text: String, _ font: NSFont) -> CGFloat {
    ceil(text.size(withAttributes: [.font: font]).width)
}

private func makeLabel(_ text: String, _ font: NSFont, _ colour: NSColor,
                       width: CGFloat? = nil) -> NSTextField {
    let field = NSTextField(wrappingLabelWithString: text)
    field.font = font
    field.textColor = colour
    field.isSelectable = false
    field.drawsBackground = false
    field.lineBreakMode = .byWordWrapping
    if let width { field.preferredMaxLayoutWidth = width }
    return field
}

/// How tall a wrapping label actually comes out at a given width.
///
/// Measured with the control that is going to draw it, not with
/// `NSAttributedString.boundingRect`. The two disagree by the text cell's own inset — a couple of
/// points — and the disagreement only shows on the labels that *nearly* fit: they wrap to a second
/// line, and the frame computed from the string has room for one. Every clipped hint in the first
/// draft of this window was that, and it was invisible in English on the row it was measured
/// against and glaring one language over.
private func labelHeight(_ text: String, _ font: NSFont, width: CGFloat) -> CGFloat {
    guard !text.isEmpty, width > 6 else { return 0 }
    // Measured four points short of the room it is going to get. `preferredMaxLayoutWidth` and a
    // frame of the same number are not the same width — the cell keeps a line fragment's padding
    // for itself — so a string that clears the measurement by a point wraps in the frame that was
    // sized for one line, and the second line is drawn outside it. Erring the other way costs a
    // few points of slack and nothing else.
    return ceil(makeLabel(text, font, .white, width: width - 4).fittingSize.height)
}

/// How wide it would be on one line — what a column has to offer before anything wraps. Plus the
/// same padding, for the same reason.
private func labelWidth(_ text: String, _ font: NSFont) -> CGFloat {
    guard !text.isEmpty else { return 0 }
    return ceil(makeLabel(text, font, .white).fittingSize.width) + 4
}

/// A view that knows how tall it wants to be once it is told how wide it is — a wrapping row of
/// chips, a list of applications. Everything else in a column states a fixed size.
private protocol SelfSizing {
    func height(forWidth width: CGFloat) -> CGFloat
}

// MARK: - A column of settings

/// A list of rows: what it is on the left, the thing that changes it on the right.
///
/// Not a label column and a control column. The old form right-aligned every label at a fixed 168
/// points, which is the arrangement that made the window read as generated — one hard vertical seam
/// down the middle of a page with nothing else in it. Here the label starts at the left edge, the
/// control finishes at the right edge, and a hairline runs between rows: a list, which is what a
/// page of settings is.
///
/// It also survives translation, which the fixed column did not. A German label half again as long
/// simply takes more of the row, and only wraps once it has genuinely run out of room.
private final class SettingsColumn {

    private enum Item {
        case head(String)
        case row(String, NSView, String?)
        case block(String?, NSView, String?)
        case mono(String)
    }

    private var items: [Item] = []

    func head(_ title: String) { items.append(.head(title)) }
    func row(_ label: String, _ control: NSView, hint: String? = nil) {
        items.append(.row(label, control, hint))
    }
    func block(label: String?, view: NSView, hint: String?) {
        items.append(.block(label, view, hint))
    }
    func mono(_ text: String) { items.append(.mono(text)) }

    var isEmpty: Bool { items.isEmpty }

    /// The width at which nothing wraps that should not have to.
    var naturalWidth: CGFloat {
        var widest: CGFloat = 0
        for item in items {
            switch item {
            case .row(let label, let control, _):
                widest = max(widest, labelWidth(label, Metric.labelFont)
                    + Metric.rowGap + control.frame.width)
            case .block(_, let view, _):
                widest = max(widest, view.frame.width)
            case .head, .mono:
                break   // these wrap or truncate, so they never decide how wide anything is
            }
        }
        return min(max(widest, Metric.minColumn), Metric.maxColumn)
    }

    func height(forWidth width: CGFloat) -> CGFloat { walk(width: width, into: nil) }

    @discardableResult
    func place(in parent: NSView, x: CGFloat, top: CGFloat, width: CGFloat) -> CGFloat {
        walk(width: width, into: parent, x: x, top: top)
    }

    /// Measuring and placing are the same walk, so the two cannot disagree about how tall a row is
    /// — which is the bug that leaves the last control of a tab hanging past the bottom edge.
    private func walk(width: CGFloat, into parent: NSView?,
                      x: CGFloat = 0, top: CGFloat = 0) -> CGFloat {
        var y = top
        var previousWasRow = false

        func add(_ view: NSView, _ frame: NSRect) {
            guard let parent else { return }
            view.frame = frame
            view.needsLayout = true
            parent.addSubview(view)
        }

        for item in items {
            switch item {
            case .head(let title):
                if y > top { y += 22 }
                add(makeLabel(title.uppercased(), Metric.headFont, Metric.faint),
                    NSRect(x: x, y: y, width: width, height: 13))
                y += 21
                previousWasRow = false

            case .row(let label, let control, let hint):
                if previousWasRow {
                    add(Hairline(), NSRect(x: x, y: y, width: width, height: 1))
                    y += 1
                }
                let controlWidth = control.frame.width
                let controlHeight = control.frame.height
                let textWidth = max(60, width - controlWidth - Metric.rowGap)
                let textHeight = max(15, labelHeight(label, Metric.labelFont, width: textWidth))
                let inner = max(textHeight, controlHeight)
                // A hand-drawn control has no label of its own, so the row lends it one. Without
                // this, VoiceOver reads a settings window as a column of anonymous checkboxes.
                if control.accessibilityLabel() == nil { control.setAccessibilityLabel(label) }
                add(makeLabel(label, Metric.labelFont, Metric.label, width: textWidth),
                    NSRect(x: x, y: y + Metric.rowPad + (inner - textHeight) / 2,
                           width: textWidth, height: textHeight))
                add(control, NSRect(x: x + width - controlWidth,
                                    y: y + Metric.rowPad + (inner - controlHeight) / 2,
                                    width: controlWidth, height: controlHeight))
                y += inner + Metric.rowPad * 2
                y += place(hint: hint, x: x, y: y, width: width, into: parent, lead: -3, trail: 8)
                previousWasRow = true

            case .block(let label, let view, let hint):
                let viewHeight = (view as? SelfSizing)?.height(forWidth: width) ?? view.frame.height
                // A reading with nothing to report — the tunnel status before a tunnel exists —
                // says so by taking up no room at all. An empty card is a control that looks
                // broken.
                guard viewHeight > 0 else { break }
                if y > top { y += 16 }
                if let label {
                    add(makeLabel(label, Metric.labelFont, Metric.label, width: width),
                        NSRect(x: x, y: y, width: width, height: 16))
                    y += 22
                }
                add(view, NSRect(x: x, y: y, width: width, height: viewHeight))
                y += viewHeight
                y += place(hint: hint, x: x, y: y, width: width, into: parent, lead: 7, trail: 4)
                previousWasRow = false

            case .mono(let text):
                if y > top { y += 10 }
                let field = NSTextField(labelWithString: text)
                field.font = Metric.monoFont
                field.textColor = Metric.faint
                field.isSelectable = true
                field.lineBreakMode = .byTruncatingMiddle
                add(field, NSRect(x: x, y: y, width: width, height: 16))
                y += 16
                previousWasRow = false
            }
        }
        return y - top
    }

    /// A hint under the thing it explains.
    ///
    /// Capped in width even where the column is not. A wide band — the paired-device list runs the
    /// whole window — makes a paragraph eight hundred points across, and a line that long is one
    /// the eye loses its place in on the way back to the left margin.
    private func place(hint: String?, x: CGFloat, y: CGFloat, width: CGFloat,
                       into parent: NSView?, lead: CGFloat, trail: CGFloat) -> CGFloat {
        guard let hint, !hint.isEmpty else { return 0 }
        let measure = min(width, Metric.maxMeasure)
        let height = labelHeight(hint, Metric.hintFont, width: measure)
        if let parent {
            let field = makeLabel(hint, Metric.hintFont, Metric.faint, width: measure)
            field.frame = NSRect(x: x, y: y + lead, width: measure, height: height)
            parent.addSubview(field)
        }
        return lead + height + trail
    }
}

/// One tab: two columns, and an optional band underneath that runs the whole width.
///
/// The band exists for things that would be squeezed into nonsense in half a window — the paired
/// device list is a row of names and three buttons. Nothing is forced into a column that does not
/// want to be in one.
private final class SettingsPane {
    let title: String
    let left = SettingsColumn()
    let right = SettingsColumn()
    let wide = SettingsColumn()
    let view = FlippedView()

    init(title: String) { self.title = title }

    var naturalWidth: CGFloat {
        if left.isEmpty || right.isEmpty {
            return max(left.naturalWidth, right.naturalWidth, wide.naturalWidth)
        }
        return max(max(left.naturalWidth, right.naturalWidth) * 2 + Metric.gutter, wide.naturalWidth)
    }

    /// Lay the whole tab out at a width and say how tall it came to.
    ///
    /// Done again on every switch rather than remembered, which is what lets a tab whose contents
    /// changed under it — a device paired, a hook installed, an app added to the scope — come back
    /// the right height instead of the height it was the first time it was looked at.
    func layout(width: CGFloat) -> CGFloat {
        view.subviews.forEach { $0.removeFromSuperview() }
        let twoUp = !left.isEmpty && !right.isEmpty
        let column = twoUp ? (width - Metric.gutter) / 2 : width

        var height: CGFloat = 0
        if !left.isEmpty {
            height = max(height, left.place(in: view, x: 0, top: 0, width: column))
        }
        if !right.isEmpty {
            height = max(height, right.place(in: view, x: twoUp ? column + Metric.gutter : 0,
                                             top: 0, width: column))
        }
        if !wide.isEmpty {
            if height > 0 { height += 24 }
            height += wide.place(in: view, x: 0, top: height, width: width)
        }
        view.frame.size = NSSize(width: width, height: height)
        return height
    }
}

/// Rows are added downwards, so the code that adds them counts downwards too. Anything else is
/// arithmetic waiting to be got wrong.
private final class FlippedView: NSView {
    override var isFlipped: Bool { true }
}

// MARK: - The strip of tabs

/// Words, with an accent rule under the current one and the character at the left.
///
/// Deliberately the same idiom as ``StackLogHeader``, which does this over the log pane: same
/// weight of type, same rule, same colour. Two tab strips in one app that do not look alike are two
/// tab strips somebody has to learn separately.
private final class TabStrip: NSView {

    var current = 0 {
        didSet {
            needsDisplay = true
            window?.invalidateCursorRects(for: self)
        }
    }
    var onPick: ((Int) -> Void)?
    var mark: NSView? {
        didSet {
            oldValue?.removeFromSuperview()
            if let mark { addSubview(mark) }
            needsLayout = true
            needsDisplay = true
        }
    }

    private let titles: [String]
    private var hovering: Int?
    private var tracking: NSTrackingArea?

    init(titles: [String]) {
        self.titles = titles
        super.init(frame: NSRect(x: 0, y: 0, width: 400, height: Metric.stripHeight))
    }
    required init?(coder: NSCoder) { fatalError() }

    private let gap: CGFloat = 22
    private var markWidth: CGFloat { mark.map { $0.frame.width + 14 } ?? 0 }

    var naturalWidth: CGFloat {
        markWidth + titles.reduce(0) { $0 + textWidth($1, Metric.tabFont) }
            + gap * CGFloat(max(0, titles.count - 1))
    }

    private var tabRects: [NSRect] {
        var out: [NSRect] = []
        var x = Metric.pad + markWidth
        for title in titles {
            let w = textWidth(title, Metric.tabFont)
            out.append(NSRect(x: x, y: 15, width: w, height: 16))
            x += w + gap
        }
        return out
    }

    override func layout() {
        super.layout()
        // Standing on the same line the words sit on, rather than centred in the band: a creature
        // floating half a line above the type reads as a sticker somebody put there.
        guard let mark = mark as? MascotView else { return }
        mark.frame.origin = NSPoint(x: Metric.pad, y: 13 - mark.footInset)
    }

    override func draw(_ dirtyRect: NSRect) {
        for (i, rect) in tabRects.enumerated() {
            let isCurrent = i == current
            let colour: NSColor = isCurrent ? Metric.label
                : (hovering == i ? Metric.soft : Metric.faint)
            titles[i].draw(at: rect.origin, withAttributes: [
                .font: Metric.tabFont, .foregroundColor: colour,
            ])
            guard isCurrent else { continue }
            Style.accent.setFill()
            NSBezierPath(roundedRect: NSRect(x: rect.minX, y: rect.minY - 7,
                                             width: rect.width, height: 2),
                         xRadius: 1, yRadius: 1).fill()
        }

        Style.hairline.setFill()
        NSRect(x: 0, y: 0, width: bounds.width, height: 1).fill()
    }

    /// Generous around the words. A tab is a target, and the word "Lesen" is thirty points wide.
    private func target(_ rect: NSRect) -> NSRect { rect.insetBy(dx: -9, dy: -11) }

    override func mouseDown(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        for (i, rect) in tabRects.enumerated() where target(rect).contains(p) {
            if i != current { onPick?(i) }
            return
        }
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let tracking { removeTrackingArea(tracking) }
        let area = NSTrackingArea(rect: bounds,
                                  options: [.mouseMoved, .mouseEnteredAndExited, .activeInKeyWindow],
                                  owner: self, userInfo: nil)
        addTrackingArea(area)
        tracking = area
    }

    override func mouseMoved(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        let hit = tabRects.firstIndex { target($0).contains(p) }
        if hit != hovering {
            hovering = hit
            needsDisplay = true
        }
    }

    override func mouseExited(with event: NSEvent) {
        hovering = nil
        needsDisplay = true
    }

    override func resetCursorRects() {
        super.resetCursorRects()
        for (i, rect) in tabRects.enumerated() where i != current {
            addCursorRect(target(rect), cursor: .pointingHand)
        }
    }
}

// MARK: - The pieces drawn by hand

/// One point of `Style.hairline`. The separator this project already uses everywhere else, rather
/// than `NSBox`, whose separator is the system's colour and not this one.
private final class Hairline: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    override func draw(_ dirtyRect: NSRect) {
        Style.hairline.setFill()
        bounds.fill()
    }
}

/// A square of colour saying what state something is in, and the app's own spinner when the honest
/// answer is "ask again in a moment".
///
/// A square rather than a dot, because everything else this app draws is made of squares — and
/// drawn rather than taken from SF Symbols for the same reason the mascot is.
private final class PixelDot: NSView {
    enum State { case idle, warn, live, busy }

    var state: State = .idle {
        didSet {
            guard oldValue != state else { return }
            state == .busy ? start() : stop()
            needsDisplay = true
        }
    }

    private var timer: Timer?
    private var phase: Double = 0

    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    private func start() {
        guard timer == nil else { return }
        let t = Timer(timeInterval: PixelSpinner.step, repeats: true) { [weak self] _ in
            self?.phase += PixelSpinner.step
            self?.needsDisplay = true
        }
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    private func stop() {
        timer?.invalidate()
        timer = nil
        phase = 0
    }

    deinit { timer?.invalidate() }

    override func draw(_ dirtyRect: NSRect) {
        if state == .busy {
            PixelSpinner.draw(at: NSPoint(x: (bounds.midX - PixelSpinner.size / 2).rounded(),
                                          y: (bounds.midY - PixelSpinner.size / 2).rounded()),
                              time: phase, colour: Style.accent)
            return
        }
        let colour: NSColor
        switch state {
        case .live: colour = Style.accent
        case .warn: colour = Style.accent.withAlphaComponent(0.42)
        default:    colour = NSColor(white: 1, alpha: 0.20)
        }
        colour.setFill()
        NSRect(x: (bounds.midX - 2.5).rounded(), y: (bounds.midY - 2.5).rounded(),
               width: 5, height: 5).fill()
    }
}

/// A sentence in a chip, with a mark in front of it and sometimes a button after it.
///
/// The same fill and edge the key hints along the bottom of the card use. It is for the places in
/// this window that *report* rather than ask — what the hooks are doing, what the tunnel is doing,
/// what will run when a session changes state — and setting those apart from the rows is what keeps
/// a reading from looking like a setting you failed to change.
private final class NoteCard: NSView, SelfSizing {

    private let label = NSTextField(wrappingLabelWithString: "")
    private let dotView = PixelDot()

    var trailing: ChipButton? {
        didSet {
            oldValue?.removeFromSuperview()
            if let trailing { addSubview(trailing) }
            needsLayout = true
        }
    }

    var text: String {
        get { label.stringValue }
        set {
            guard label.stringValue != newValue else { return }
            label.stringValue = newValue
            needsLayout = true
        }
    }

    var dot: PixelDot.State {
        get { dotView.state }
        set { dotView.state = newValue }
    }

    var mono = false {
        didSet { label.font = mono ? Metric.monoFont : Metric.noteFont }
    }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        label.font = Metric.noteFont
        label.textColor = Metric.soft
        label.isSelectable = true
        label.lineBreakMode = .byWordWrapping
        addSubview(dotView)
        addSubview(label)
    }
    required init?(coder: NSCoder) { fatalError() }

    private var textWidthAvailable: (CGFloat) -> CGFloat {
        { [weak self] width in
            let button = self?.trailing.map { $0.frame.width + 12 } ?? 0
            return max(40, width - 30 - 12 - button)
        }
    }

    func height(forWidth width: CGFloat) -> CGFloat {
        guard !label.stringValue.isEmpty || trailing != nil else { return 0 }
        return max(40, labelHeight(label.stringValue, label.font ?? Metric.noteFont,
                                   width: textWidthAvailable(width)) + 22)
    }

    override func layout() {
        super.layout()
        dotView.frame = NSRect(x: 12, y: bounds.midY - 6, width: 12, height: 12)
        if let trailing {
            trailing.frame.origin = NSPoint(x: bounds.width - 12 - trailing.frame.width,
                                            y: (bounds.midY - trailing.frame.height / 2).rounded())
        }
        let width = textWidthAvailable(bounds.width)
        label.preferredMaxLayoutWidth = width
        let height = labelHeight(label.stringValue, label.font ?? Metric.noteFont, width: width)
        label.frame = NSRect(x: 30, y: (bounds.midY - height / 2).rounded(),
                             width: width, height: height)
    }

    override func draw(_ dirtyRect: NSRect) {
        let path = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5), xRadius: 8, yRadius: 8)
        Style.chipFill.setFill()
        path.fill()
        Style.chipEdge.setStroke()
        path.lineWidth = 1
        path.stroke()
    }
}

/// A switch, drawn.
///
/// `NSSwitch` would be one line, and it is tinted with the system's accent colour, which on most
/// machines is blue. Every other lit thing in this app is `#D97757`, and a window where the one
/// colour that means "on" is somebody's System Settings preference is a window that belongs to
/// macOS rather than to this. So it is the app's own chip — same fill, same edge — with a knob that
/// moves across it.
private final class SwitchView: NSView {

    var isOn: Bool {
        didSet {
            guard isOn != oldValue else { return }
            animate()
            setAccessibilityValue(NSNumber(value: isOn))
        }
    }

    private let onChange: (Bool) -> Void
    private var shown: CGFloat
    private var timer: Timer?

    init(isOn: Bool, _ onChange: @escaping (Bool) -> Void) {
        self.isOn = isOn
        self.shown = isOn ? 1 : 0
        self.onChange = onChange
        super.init(frame: NSRect(x: 0, y: 0, width: 38, height: 22))
        setAccessibilityRole(.checkBox)
        setAccessibilityValue(NSNumber(value: isOn))
    }
    required init?(coder: NSCoder) { fatalError() }

    override var acceptsFirstResponder: Bool { true }
    override func becomeFirstResponder() -> Bool { needsDisplay = true; return true }
    override func resignFirstResponder() -> Bool { needsDisplay = true; return true }
    override func resetCursorRects() { addCursorRect(bounds, cursor: .pointingHand) }

    override func mouseDown(with event: NSEvent) { flip() }

    override func keyDown(with event: NSEvent) {
        // Space, the key a checkbox has answered to on this platform for thirty years.
        if event.keyCode == 49 { flip() } else { super.keyDown(with: event) }
    }

    private func flip() {
        window?.makeFirstResponder(self)
        isOn.toggle()
        onChange(isOn)
    }

    /// A sixth of a second of travel, so the eye can see which way it went. Longer and it is an
    /// animation you wait for; instant and a mis-click is indistinguishable from nothing having
    /// happened at all.
    private func animate() {
        timer?.invalidate()
        let target: CGFloat = isOn ? 1 : 0
        let t = Timer(timeInterval: 1.0 / 60, repeats: true) { [weak self] timer in
            guard let self else { timer.invalidate(); return }
            self.shown += (target - self.shown) * 0.34
            if abs(target - self.shown) < 0.01 {
                self.shown = target
                timer.invalidate()
                self.timer = nil
            }
            self.needsDisplay = true
        }
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    deinit { timer?.invalidate() }

    override func draw(_ dirtyRect: NSRect) {
        let track = NSRect(x: 0, y: (bounds.height - 20) / 2, width: 38, height: 20)
            .insetBy(dx: 0.5, dy: 0.5)
        let path = NSBezierPath(roundedRect: track, xRadius: track.height / 2,
                                yRadius: track.height / 2)
        (shown > 0 ? Style.accent.withAlphaComponent(0.18 + shown * 0.72) : Style.chipFill).setFill()
        path.fill()
        (shown > 0.5 ? Style.accent : Style.chipEdge).setStroke()
        path.lineWidth = 1
        path.stroke()

        let d: CGFloat = 14
        NSColor(white: 1, alpha: 0.5 + shown * 0.45).setFill()
        NSBezierPath(ovalIn: NSRect(x: track.minX + 3 + shown * (track.width - d - 6),
                                    y: track.midY - d / 2, width: d, height: d)).fill()

        guard window?.firstResponder === self else { return }
        Style.accent.withAlphaComponent(0.55).setStroke()
        let ring = NSBezierPath(roundedRect: track.insetBy(dx: -2.5, dy: -2.5),
                                xRadius: 13, yRadius: 13)
        ring.lineWidth = 1.5
        ring.stroke()
    }
}

/// A button, drawn as the app's chip.
///
/// The same shape as the buttons on a session row, and for the same reason as the switch above: an
/// `NSButton` in `.rounded` is the system's grey pill, and beside a hand-drawn strip and a
/// hand-drawn switch it is the one thing that arrived from somewhere else.
///
/// `prominent` is for the one action on a tab that is the reason somebody came to the tab. `armed`
/// is the state a stack row's button uses — pressed, and waiting for the next thing you do — which
/// is exactly what the hotkey recorder is doing while it listens.
private final class ChipButton: NSView {

    var title: String {
        didSet {
            guard title != oldValue else { return }
            resize()
            setAccessibilityLabel(title)
            needsDisplay = true
        }
    }
    var armed = false { didSet { if armed != oldValue { needsDisplay = true } } }
    var minimumWidth: CGFloat = 0 { didSet { resize() } }
    var action: (() -> Void)?

    private let prominent: Bool
    private var hovering = false
    private var pressed = false
    private var tracking: NSTrackingArea?

    init(title: String, prominent: Bool = false) {
        self.title = title
        self.prominent = prominent
        super.init(frame: NSRect(x: 0, y: 0, width: 100, height: 26))
        setAccessibilityRole(.button)
        setAccessibilityLabel(title)
        resize()
    }
    required init?(coder: NSCoder) { fatalError() }

    private var font: NSFont { NSFont.systemFont(ofSize: 11.5, weight: .medium) }

    private func resize() {
        frame.size = NSSize(width: max(minimumWidth, textWidth(title, font) + 26), height: 26)
        superview?.needsLayout = true
    }

    override var acceptsFirstResponder: Bool { true }
    override func becomeFirstResponder() -> Bool { needsDisplay = true; return true }
    override func resignFirstResponder() -> Bool { needsDisplay = true; return true }
    override func resetCursorRects() { addCursorRect(bounds, cursor: .pointingHand) }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let tracking { removeTrackingArea(tracking) }
        let area = NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeInKeyWindow],
                                  owner: self, userInfo: nil)
        addTrackingArea(area)
        tracking = area
    }

    override func mouseEntered(with event: NSEvent) { hovering = true; needsDisplay = true }
    override func mouseExited(with event: NSEvent) { hovering = false; needsDisplay = true }

    override func mouseDown(with event: NSEvent) {
        pressed = true
        needsDisplay = true
    }

    override func mouseUp(with event: NSEvent) {
        pressed = false
        needsDisplay = true
        guard bounds.contains(convert(event.locationInWindow, from: nil)) else { return }
        window?.makeFirstResponder(self)
        action?()
    }

    override func keyDown(with event: NSEvent) {
        if event.keyCode == 49 || event.keyCode == 36 { action?() } else { super.keyDown(with: event) }
    }

    override func draw(_ dirtyRect: NSRect) {
        let path = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5), xRadius: 7, yRadius: 7)
        let lit = armed || prominent
        var fill = lit ? Style.accent.withAlphaComponent(armed ? 0.28 : 0.16) : Style.chipFill
        if pressed { fill = fill.blended(withFraction: 0.25, of: .black) ?? fill }
        else if hovering { fill = fill.blended(withFraction: 0.10, of: .white) ?? fill }
        fill.setFill()
        path.fill()
        (lit ? Style.accent : Style.chipEdge).setStroke()
        path.lineWidth = 1
        path.stroke()

        if window?.firstResponder === self {
            Style.accent.withAlphaComponent(0.55).setStroke()
            let ring = NSBezierPath(roundedRect: bounds.insetBy(dx: -2, dy: -2),
                                    xRadius: 9, yRadius: 9)
            ring.lineWidth = 1.5
            ring.stroke()
        }

        let attrs: [NSAttributedString.Key: Any] = [
            .font: font, .foregroundColor: lit ? Style.accent : Metric.soft,
        ]
        let size = title.size(withAttributes: attrs)
        title.draw(at: NSPoint(x: (bounds.midX - size.width / 2).rounded(),
                               y: (bounds.midY - size.height / 2).rounded()),
                   withAttributes: attrs)
    }
}

/// What can reach this Mac, as one chip each, with the last time it did.
///
/// **The chips carry a time because the names collide.** Pressing *Open in a browser* twice mints
/// two devices called the same thing, and two identical rows is a list nobody can act on: you
/// cannot tell which is the tab you have open, so you remove neither and it grows forever. What
/// distinguishes them is not a better name — two browsers deserve the same name — it is a fact.
/// A device that has never been seen is one that was minted and never used, which is exactly the
/// one that is safe to take away.
///
/// And each one can be removed on its own. "Disconnect everything" is the right control for the
/// moment you need it and the wrong one for tidying up, and having only that means the list is
/// either intact or empty.
/// Internal rather than private so that ago(_:now:) can be tested. It is the one piece of
/// judgement in this file — everything else here is layout.
final class DeviceChips: NSView, SelfSizing {

    struct Row: Equatable {
        var id: String
        var name: String
        var lastSeen: Date?
    }

    var devices: [Row] = [] { didSet { needsDisplay = true; rebuildTracking() } }
    var empty = ""
    /// Called with the device to forget.
    var onRemove: ((String) -> Void)?

    private let font = NSFont.systemFont(ofSize: 11)
    private let small = NSFont.monospacedDigitSystemFont(ofSize: 10, weight: .regular)
    private let chipHeight: CGFloat = 22
    private let gap: CGFloat = 6
    private let cross: CGFloat = 16

    /// `2m`, `3h`, `4d` — and nothing at all for a device that has never connected.
    ///
    /// Short and numeric on purpose. This is the same shape the live line and the status line
    /// already use, and it needs no translating: a digit and a unit letter read the same in every
    /// language this app speaks, which is why it can be added without a round of copy.
    static func ago(_ date: Date?, now: Date = Date()) -> String? {
        guard let date else { return nil }
        let seconds = max(0, now.timeIntervalSince(date))
        if seconds < 60 { return "now" }
        if seconds < 3_600 { return "\(Int(seconds / 60))m" }
        if seconds < 86_400 { return "\(Int(seconds / 3_600))h" }
        return "\(Int(seconds / 86_400))d"
    }

    private func label(_ row: Row) -> (name: String, when: String?) {
        (row.name, Self.ago(row.lastSeen))
    }

    private func width(of row: Row) -> CGFloat {
        let parts = label(row)
        var w = textWidth(parts.name, font) + 20 + cross
        if let when = parts.when { w += textWidth(when, small) + 8 }
        return w
    }

    private func rows(forWidth width: CGFloat) -> [[(row: Row, width: CGFloat)]] {
        var out: [[(row: Row, width: CGFloat)]] = [[]]
        var x: CGFloat = 0
        for device in devices {
            let w = self.width(of: device)
            if x > 0, x + w > width { out.append([]); x = 0 }
            out[out.count - 1].append((device, w))
            x += w + gap
        }
        return out
    }

    func height(forWidth width: CGFloat) -> CGFloat {
        guard !devices.isEmpty else { return 18 }
        let lines = rows(forWidth: width).count
        return CGFloat(lines) * chipHeight + CGFloat(lines - 1) * gap
    }

    /// Where each chip's ✕ landed, so a click can be matched to a device without laying out twice.
    private var crosses: [(rect: NSRect, id: String)] = []

    override func draw(_ dirtyRect: NSRect) {
        crosses = []
        guard !devices.isEmpty else {
            empty.draw(at: NSPoint(x: 0, y: 1),
                       withAttributes: [.font: font, .foregroundColor: Metric.faint])
            return
        }
        var y = bounds.height - chipHeight
        for line in rows(forWidth: bounds.width) {
            var x: CGFloat = 0
            for chip in line {
                let rect = NSRect(x: x, y: y, width: chip.width, height: chipHeight)
                let path = NSBezierPath(roundedRect: rect, xRadius: 5, yRadius: 5)
                Style.chipFill.setFill()
                path.fill()
                Style.chipEdge.setStroke()
                path.lineWidth = 1
                path.stroke()

                let parts = label(chip.row)
                var textX = rect.minX + 10
                let attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: Metric.soft]
                let size = parts.name.size(withAttributes: attrs)
                parts.name.draw(at: NSPoint(x: textX.rounded(),
                                            y: (rect.midY - size.height / 2).rounded()),
                                withAttributes: attrs)
                textX += size.width + 8

                if let when = parts.when {
                    let dim: [NSAttributedString.Key: Any] = [.font: small, .foregroundColor: Metric.faint]
                    let s = when.size(withAttributes: dim)
                    when.draw(at: NSPoint(x: textX.rounded(), y: (rect.midY - s.height / 2).rounded()),
                              withAttributes: dim)
                    textX += s.width + 8
                }

                let box = NSRect(x: rect.maxX - cross - 4, y: rect.midY - cross / 2,
                                 width: cross, height: cross)
                drawCross(in: box)
                crosses.append((box, chip.row.id))
                x += chip.width + gap
            }
            y -= chipHeight + gap
        }
    }

    private func drawCross(in box: NSRect) {
        let inset = box.insetBy(dx: 4.5, dy: 4.5)
        let path = NSBezierPath()
        path.move(to: NSPoint(x: inset.minX, y: inset.minY))
        path.line(to: NSPoint(x: inset.maxX, y: inset.maxY))
        path.move(to: NSPoint(x: inset.minX, y: inset.maxY))
        path.line(to: NSPoint(x: inset.maxX, y: inset.minY))
        path.lineWidth = 1.2
        path.lineCapStyle = .round
        Metric.faint.setStroke()
        path.stroke()
    }

    override func mouseDown(with event: NSEvent) {
        let point = convert(event.locationInWindow, from: nil)
        // A generous target: the drawn ✕ is sixteen points and a pointer is not that accurate.
        guard let hit = crosses.first(where: { $0.rect.insetBy(dx: -4, dy: -4).contains(point) })
        else { return }
        onRemove?(hit.id)
    }

    /// The cursor says which part of a chip is a button, because nothing else here does.
    private func rebuildTracking() {
        for area in trackingAreas { removeTrackingArea(area) }
        addTrackingArea(NSTrackingArea(rect: bounds,
                                       options: [.activeInKeyWindow, .cursorUpdate, .inVisibleRect],
                                       owner: self, userInfo: nil))
    }

    override func cursorUpdate(with event: NSEvent) {
        let point = convert(event.locationInWindow, from: nil)
        if crosses.contains(where: { $0.rect.insetBy(dx: -4, dy: -4).contains(point) }) {
            NSCursor.pointingHand.set()
        } else {
            NSCursor.arrow.set()
        }
    }
}

/// Something to read, with the buttons that act on it underneath.
private final class StackedRow: NSView, SelfSizing {

    var top: NSView? {
        didSet {
            oldValue?.removeFromSuperview()
            if let top { addSubview(top) }
            needsLayout = true
        }
    }

    var buttons: [ChipButton] = [] {
        didSet {
            oldValue.forEach { $0.removeFromSuperview() }
            buttons.forEach { addSubview($0) }
            needsLayout = true
        }
    }

    private func topHeight(_ width: CGFloat) -> CGFloat {
        (top as? SelfSizing)?.height(forWidth: width) ?? top?.frame.height ?? 0
    }

    func height(forWidth width: CGFloat) -> CGFloat {
        topHeight(width) + (buttons.isEmpty ? 0 : 40)
    }

    override func layout() {
        super.layout()
        let above = topHeight(bounds.width)
        top?.frame = NSRect(x: 0, y: bounds.height - above, width: bounds.width, height: above)
        var x: CGFloat = 0
        for button in buttons {
            button.frame.origin = NSPoint(x: x, y: 4)
            x += button.frame.width + 8
        }
    }
}

// MARK: - The apps the hotkey fires in

/// The chosen applications, by icon and by the name their own developer gave them.
///
/// Rows are dimmed rather than emptied when "in every app" is on. The list is what turning that
/// switch back off will restore, and a control that goes blank the moment it stops applying is a
/// control you cannot find your way back to — which is the failure this whole thing exists to fix.
private final class AppScopeView: NSView, SelfSizing {

    var ids: [String] {
        didSet {
            if !ids.isEmpty { remembered = ids }
            rebuild()
        }
    }
    var isActive = true { didSet { rebuild() } }
    var onChange: (([String]) -> Void)?

    /// The last list that was not empty. What "in every app" gives back when it is switched off
    /// again — the apps that were actually there, rather than a default that happens to be right
    /// for whoever wrote this line.
    private(set) var remembered: [String]

    private let add: ChipButton
    private var rows: [AppRow] = []

    init(ids: [String]) {
        self.ids = ids
        self.remembered = ids
        self.add = ChipButton(title: L.t.settingsScopeAdd)
        super.init(frame: NSRect(x: 0, y: 0, width: 300, height: 60))
        addSubview(add)
        add.action = { [weak self] in
            guard let self else { return }
            self.offer(from: self.add)
        }
        rebuild()
    }
    required init?(coder: NSCoder) { fatalError() }

    private let rowHeight: CGFloat = 27

    func height(forWidth width: CGFloat) -> CGFloat {
        CGFloat(ids.count) * rowHeight + 34
    }

    private func rebuild() {
        rows.forEach { $0.removeFromSuperview() }
        rows = ids.map { id in
            let row = AppRow(id: id)
            row.onRemove = { [weak self] in self?.remove(id) }
            row.alphaValue = isActive ? 1 : 0.36
            addSubview(row)
            return row
        }
        add.alphaValue = isActive ? 1 : 0.36
        needsLayout = true
        superview?.needsLayout = true
    }

    private func remove(_ id: String) {
        guard isActive else { return }
        ids.removeAll { $0 == id }
        onChange?(ids)
    }

    override func layout() {
        super.layout()
        var y = bounds.height - rowHeight
        for row in rows {
            row.frame = NSRect(x: 0, y: y, width: bounds.width, height: rowHeight)
            y -= rowHeight
        }
        add.frame.origin = NSPoint(x: 0, y: 0)
    }

    /// What to offer: the apps that are open right now, which is where the terminal somebody wants
    /// nearly always is, and a way to go and find one that is not.
    private func offer(from sender: NSView) {
        guard isActive else { return }
        let menu = NSMenu()

        var seen = Set(ids)
        var running: [(id: String, name: String, icon: NSImage?)] = []
        for app in NSWorkspace.shared.runningApplications where app.activationPolicy == .regular {
            guard let id = app.bundleIdentifier, !seen.contains(id) else { continue }
            seen.insert(id)
            running.append((id, app.localizedName ?? id, app.icon))
        }
        running.sort { $0.name.localizedStandardCompare($1.name) == .orderedAscending }

        if !running.isEmpty {
            let head = NSMenuItem(title: L.t.settingsScopeRunning, action: nil, keyEquivalent: "")
            head.isEnabled = false
            menu.addItem(head)
            for app in running {
                let item = NSMenuItem(title: app.name, action: #selector(pick(_:)), keyEquivalent: "")
                item.target = self
                item.representedObject = app.id
                app.icon?.size = NSSize(width: 16, height: 16)
                item.image = app.icon
                menu.addItem(item)
            }
            menu.addItem(.separator())
        }

        let browse = NSMenuItem(title: L.t.settingsScopeChoose, action: #selector(browse(_:)),
                                keyEquivalent: "")
        browse.target = self
        menu.addItem(browse)
        menu.popUp(positioning: nil, at: NSPoint(x: 0, y: -3), in: sender)
    }

    @objc private func pick(_ sender: NSMenuItem) {
        guard let id = sender.representedObject as? String, !ids.contains(id) else { return }
        ids.append(id)
        onChange?(ids)
    }

    @objc private func browse(_ sender: NSMenuItem) {
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = false
        panel.allowedContentTypes = [.application]
        panel.directoryURL = URL(fileURLWithPath: "/Applications")
        guard panel.runModal() == .OK, let url = panel.url,
              let id = Bundle(url: url)?.bundleIdentifier, !ids.contains(id) else { return }
        ids.append(id)
        onChange?(ids)
    }
}

/// One application in the list: its icon, its real name, and a way to take it out again.
///
/// A bundle id whose app is not installed still gets a row, showing the id itself in monospace. The
/// alternative is dropping a line somebody wrote into their own config file because this machine
/// happened not to resolve it, which is a worse thing to do than showing something plain.
private final class AppRow: NSView {

    let id: String
    var onRemove: (() -> Void)?

    private let icon = NSImageView()
    private let name = NSTextField(labelWithString: "")
    private let remove = ChipButton(title: "✕")

    init(id: String) {
        self.id = id
        super.init(frame: .zero)

        if let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: id) {
            let image = NSWorkspace.shared.icon(forFile: url.path)
            image.size = NSSize(width: 16, height: 16)
            icon.image = image
            name.stringValue = FileManager.default.displayName(atPath: url.path)
            name.font = NSFont.systemFont(ofSize: 12)
            name.textColor = Metric.label
        } else {
            name.stringValue = id
            name.font = Metric.monoFont
            name.textColor = Metric.faint
        }
        name.lineBreakMode = .byTruncatingTail
        addSubview(icon)
        addSubview(name)

        remove.minimumWidth = 28
        remove.setAccessibilityLabel(L.t.settingsScopeRemove)
        remove.toolTip = L.t.settingsScopeRemove
        remove.action = { [weak self] in self?.onRemove?() }
        addSubview(remove)
    }
    required init?(coder: NSCoder) { fatalError() }

    override func layout() {
        super.layout()
        icon.frame = NSRect(x: 0, y: (bounds.midY - 8).rounded(), width: 16, height: 16)
        remove.frame.origin = NSPoint(x: bounds.width - remove.frame.width,
                                      y: (bounds.midY - 13).rounded())
        name.frame = NSRect(x: 24, y: (bounds.midY - 8).rounded(),
                            width: max(20, bounds.width - 24 - remove.frame.width - 8), height: 16)
    }

    override func draw(_ dirtyRect: NSRect) {
        Style.hairline.setFill()
        NSRect(x: 0, y: 0, width: bounds.width, height: 1).fill()
    }
}

// MARK: - The system controls, each carrying its own closure

/// `target` is held weakly by every AppKit control, which is why the old form kept a bag of little
/// handler objects alive by hand. A control that is its own target has no such problem: the view
/// tree already owns it.
private final class ChoicePopUp: NSPopUpButton {

    private let values: [String]
    private let onPick: (String) -> Void

    init(options: [(label: String, value: String)], current: String,
         _ onPick: @escaping (String) -> Void) {
        self.values = options.map(\.value)
        self.onPick = onPick
        super.init(frame: NSRect(x: 0, y: 0, width: 200, height: 25), pullsDown: false)
        addItems(withTitles: options.map(\.label))
        if let i = values.firstIndex(of: current) { selectItem(at: i) }
        cell?.lineBreakMode = .byTruncatingTail
        sizeToFit()
        // Sized to its own contents, then clamped: the font list holds "Andale Mono" and it holds
        // names three times that long, and one of them must not decide how wide the window is.
        frame.size = NSSize(width: min(max(frame.width, 130), 210), height: 25)
        target = self
        action = #selector(fire)
    }
    required init?(coder: NSCoder) { fatalError() }

    @objc private func fire() {
        guard values.indices.contains(indexOfSelectedItem) else { return }
        onPick(values[indexOfSelectedItem])
    }
}

/// A slider and the number it is currently at, as one control.
///
/// The number is not decoration. "Card opacity" and "width" are both a knob between two ends, and
/// without a readout the only way to find out what you just set is to go and read the config file.
private final class ValueSlider: NSView {

    private let slider: NSSlider
    private let readout = NSTextField(labelWithString: "")
    private let format: (Double) -> String
    private let onChange: (Double) -> Void

    init(range: ClosedRange<Double>, value: Double,
         format: @escaping (Double) -> String, _ onChange: @escaping (Double) -> Void) {
        self.format = format
        self.onChange = onChange
        self.slider = NSSlider(value: value, minValue: range.lowerBound,
                               maxValue: range.upperBound, target: nil, action: nil)
        super.init(frame: NSRect(x: 0, y: 0, width: 206, height: 22))

        slider.frame = NSRect(x: 0, y: 1, width: 142, height: 20)
        // The filled half of the track in the app's colour rather than the system's, for the same
        // reason the switch is drawn by hand.
        slider.trackFillColor = Style.accent
        slider.target = self
        slider.action = #selector(fire)
        addSubview(slider)

        readout.font = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .regular)
        readout.textColor = Metric.soft
        readout.alignment = .right
        readout.stringValue = format(value)
        readout.frame = NSRect(x: 150, y: 3, width: 56, height: 15)
        addSubview(readout)
    }
    required init?(coder: NSCoder) { fatalError() }

    @objc private func fire() {
        readout.stringValue = format(slider.doubleValue)
        onChange(slider.doubleValue)
    }
}

/// A text field that leaves behind what it used to hold.
///
/// The hostname is the last free-text box in this window, and free-text boxes lose things: clear
/// one by accident and nothing on screen says what was there. The last non-empty value becomes the
/// placeholder, so an empty box always answers "what did I just delete?" — and until there has ever
/// been one, the placeholder is an example instead, which is what a first-time reader needs.
private final class MemoField: NSTextField {

    var onCommit: ((String) -> Void)?
    private let example: String
    private var remembered: String

    init(value: String, example: String) {
        self.example = example
        self.remembered = value
        super.init(frame: NSRect(x: 0, y: 0, width: 206, height: 22))
        stringValue = value
        font = Metric.monoFont
        placeholderString = value.isEmpty ? example : value
        target = self
        action = #selector(commit)
    }
    required init?(coder: NSCoder) { fatalError() }

    override func textDidEndEditing(_ notification: Notification) {
        super.textDidEndEditing(notification)
        commit()
    }

    @objc private func commit() {
        let text = stringValue.trimmingCharacters(in: .whitespaces)
        if !text.isEmpty { remembered = text }
        placeholderString = remembered.isEmpty ? example : remembered
        onCommit?(text)
    }
}
