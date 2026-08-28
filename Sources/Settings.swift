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
    private var scroll: NSScrollView?
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
    private var policyCard: NoteCard?
    private var deviceChips: DeviceChips?
    private var tunnelCard: NoteCard?
    private var smartHealthCard: NoteCard?
    private var schedulesControl: ScheduleSettingsControl?
    /// AppKit invokes this window on the main thread, but the older SDK annotations used by the
    /// straight-swiftc build do not carry that fact through NSObject. Keep the actor boundary
    /// explicit where the UI-independent state model is created.
    private lazy var cloudSettings = MainActor.assumeIsolated {
        CloudSettingsModel(
            services: .production(openVerificationURL: { NSWorkspace.shared.open($0) }),
            metadata: .currentMac())
    }
    private var cloudSettingsControl: CloudSettingsControl?
    private var schedulesRefreshAt = Date.distantPast
    private var schedulesRefreshing = false
    private var schedulesRefreshPending = false
    /// The schedule form, while it is up. Non-nil is also what stops a second one being opened
    /// over the first — two sheets on one window is a stack nobody asked for, and the lower one
    /// would still be holding a half-written first message.
    private var scheduleSheet: NSWindow?
    private var scheduleForm: ScheduleFormView?
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
        let wasBuilt = window != nil
        if !wasBuilt { window = build() }
        if wasBuilt { schedulesRefreshAt = .distantPast }
        refreshLive()
        startLiveTimer()
        NSApp.activate(ignoringOtherApps: true)
        window?.center()
        window?.makeKeyAndOrderFront(nil)
    }

    func windowWillClose(_ notification: Notification) {
        stopRecording()
        closeScheduleForm()
        MainActor.assumeIsolated { cloudSettings.close() }
        live?.invalidate()
        live = nil
        // The window itself is kept rather than rebuilt: which tab you were on is a thing you
        // expect to still be true the second time you open it.
    }

    private func tearDown() {
        stopRecording()
        MainActor.assumeIsolated { cloudSettings.close() }
        // Before the window goes: a sheet outlives the window it was attached to, and the only
        // thing that reaches this path is picking a different language — which rebuilds every
        // label in the window, including the ones on that form.
        closeScheduleForm()
        window?.orderOut(nil)
        window = nil
        root = nil
        strip = nil
        panes = []
        schedulesControl = nil
        cloudSettingsControl = nil
        schedulesRefreshAt = .distantPast
        schedulesRefreshing = false
        schedulesRefreshPending = false
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

        // The pane sits inside a scroller rather than straight on the root. Without one the window
        // simply grew to whatever the tab needed, and a tab taller than the screen — the device
        // list, the schedule list — put its last rows below the bottom edge where nothing could
        // reach them. The strip and the footer stay outside it, so the tabs do not scroll away
        // from under the pointer.
        let scroll = NSScrollView(frame: NSRect(x: 0, y: Metric.stripHeight + 22,
                                                width: contentWidth, height: 200))
        scroll.hasVerticalScroller = true
        scroll.hasHorizontalScroller = false
        scroll.autohidesScrollers = true
        // This window is pinned dark; a scroller drawing its own background would put a pale band
        // down the side of it.
        scroll.drawsBackground = false
        scroll.contentView.drawsBackground = false
        root.addSubview(scroll)
        self.scroll = scroll

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

        // The document is the full window width and the pane is inset inside it, so that a legacy
        // scroller — what somebody gets by asking for scroll bars to always be visible — lands in
        // the margin rather than over the text.
        let document = FlippedView(frame: NSRect(x: 0, y: 0, width: contentWidth, height: paneHeight))
        pane.view.frame = NSRect(x: Metric.pad, y: 0, width: inner, height: paneHeight)
        document.addSubview(pane.view)
        scroll?.documentView = document

        let above = Metric.stripHeight + 22
        let below = 24 + Metric.footerHeight
        let natural = (above + paneHeight + below).rounded()
        let fit = Self.contentFit(natural: natural, ceiling: ceilingContentHeight(),
                                  chrome: above + below)
        let height = fit.height
        strip?.frame = NSRect(x: 0, y: 0, width: contentWidth, height: Metric.stripHeight)
        scroll?.frame = NSRect(x: 0, y: above, width: contentWidth, height: fit.viewport)
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

    /// How tall the window ends up, and how much of that the scroller gets to show.
    ///
    /// Split out from the window so the decision can be checked without an `NSWindow` or a screen.
    /// Before it existed the height was simply the natural one, so a pane taller than the display
    /// made a window taller than the display, and its last rows sat below the bottom edge where
    /// nothing could reach them.
    static func contentFit(natural: CGFloat, ceiling: CGFloat,
                           chrome: CGFloat) -> (height: CGFloat, viewport: CGFloat) {
        let height = min(natural, ceiling)
        return (height, max(height - chrome, 0))
    }

    /// The tallest the content is allowed to get: what the screen leaves once the window's own
    /// title bar is taken off it.
    ///
    /// A tab taller than this used to make a window taller than the screen, and the rows past the
    /// bottom edge could not be reached by any means — not by dragging the window, which is pinned
    /// by its title bar, and not by resizing it, which this window does not offer. Capping the
    /// height is what turns the overflow into something the scroller can reach.
    private func ceilingContentHeight() -> CGFloat {
        guard let visible = (window?.screen ?? NSScreen.main)?.visibleFrame else {
            return .greatestFiniteMagnitude
        }
        // A titled window's chrome, asked for rather than assumed — except before the window
        // exists, where the usual title bar is close enough to keep the first open on screen.
        let chrome = window.map {
            $0.frameRect(forContentRect: NSRect(x: 0, y: 0, width: contentWidth, height: 0)).height
        } ?? 28
        return max(240, visible.height - chrome)
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
        // These live here rather than in General because what they control is a phone — there is
        // no notification without a paired device, so each switch belongs beside the devices
        // rather than in a list of things about the bar.
        pane.left.row(L.t.settingsPushFinish,
                      switchFor({ Config.shared.pushOnFinish },
                                { Config.shared.pushOnFinish = $0 }),
                      hint: L.t.settingsPushFinishHint)
        pane.left.row(L.t.settingsSmartNotifications,
                      switchFor({ Config.shared.smartNotifications },
                                { Config.shared.smartNotifications = $0 }),
                      hint: L.t.settingsSmartNotificationsHint)
        // The feature's record, beside its switch. It once failed 784 times in three hours and
        // the only evidence was a log line; whether the model is producing sentences, and why the
        // last attempt fell back, belong where the person who flipped the switch will look.
        let smartHealth = NoteCard()
        smartHealthCard = smartHealth
        pane.left.block(label: nil, view: smartHealth, hint: nil)
        pane.left.row(L.t.settingsPushDeploy,
                      switchFor({ Config.shared.pushOnDeploy },
                                { Config.shared.pushOnDeploy = $0 }),
                      hint: L.t.settingsPushDeployHint)
        pane.left.row(L.t.settingsAgentNotify,
                      switchFor({ Config.shared.orchestratorAgentNotify },
                                { Config.shared.orchestratorAgentNotify = $0 }),
                      hint: L.t.settingsAgentNotifyNote)

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
        pane.right.row(L.t.settingsOrchestratorSubMax, grandchildrenPopUp(),
                       hint: L.t.settingsOrchestratorSubMaxHint)
        pane.right.row(L.t.settingsOrchestratorPermission, permissionPopUp(),
                       hint: L.t.settingsOrchestratorPermissionHint)
        pane.right.row(L.t.settingsOrchestratorNotify,
                       switchFor({ Config.shared.orchestratorNotifyRoot },
                                 { Config.shared.orchestratorNotifyRoot = $0 }),
                       hint: L.t.settingsOrchestratorNotifyHint)
        pane.right.row(L.t.settingsOrchestratorClose, lingerPopUp(),
                       hint: L.t.settingsOrchestratorCloseHint)
        pane.right.block(label: L.t.settingsOrchestratorPolicy, view: policyControl(),
                         hint: L.t.settingsOrchestratorPolicyHint)
        pane.right.mono(Orchestrator.policyURL.path
            .replacingOccurrences(of: NSHomeDirectory(), with: "~"))

        let cloud = CloudSettingsControl(model: cloudSettings)
        cloud.onResize = { [weak self] in self?.relayoutCurrent() }
        cloudSettingsControl = cloud
        pane.wide.block(label: "Clawdline Cloud", view: cloud,
                        hint: "Connect this Mac with GitHub to use Clawdline Cloud. The browser opens only after you confirm the one-time code.")
        pane.wide.block(label: L.t.settingsRemoteDevices, view: devicesControl(),
                        hint: L.t.settingsRemotePhoneHint)
        pane.right.block(label: L.t.settingsSchedules, view: scheduleControl(),
                         hint: Orchestrator.scheduleDirectory.path
                            .replacingOccurrences(of: NSHomeDirectory(), with: "~"))
        return pane
    }

    /// How many child sessions one session may run at once, 1 to 10 — the range `Config` accepts,
    /// so a number picked here is a number the file will still hold after a reload. A popup rather
    /// than a slider because ten stops is a list, and the value is the label.
    private func childrenPopUp() -> NSView {
        popUp((1...10).map { (String($0), String($0)) },
              current: String(Config.shared.orchestratorMaxChildren)) {
            Config.shared.orchestratorMaxChildren = Int($0) ?? Config.shared.orchestratorMaxChildren
        }
    }

    /// The same list one level down, with `0` on the front — and `0` gets a sentence rather than
    /// a digit, because what it turns off is a whole level rather than a count. It is also the
    /// rule this app had before that level existed, which is why it is a stop on the list and not
    /// a separate switch above it.
    private func grandchildrenPopUp() -> NSView {
        let stops: [(label: String, value: String)] =
            [(label: L.t.settingsOrchestratorSubMaxNone, value: "0")]
            + (1...10).map { (label: String($0), value: String($0)) }
        return popUp(stops, current: String(Config.shared.orchestratorMaxGrandchildren)) {
            Config.shared.orchestratorMaxGrandchildren =
                Int($0) ?? Config.shared.orchestratorMaxGrandchildren
        }
    }

    /// The house rules, as a card that says whether there are any and a button that opens them.
    ///
    /// A button rather than a text box, and a file rather than a key in `config.json`, because
    /// what goes in there is paragraphs — it is edited the way prose is edited, in whatever the
    /// person already writes prose in, and read back by this app on every dispatch. The file is
    /// written on the first press and never after: what is in it is theirs, and a default that
    /// grew back after being deleted would be a setting that does not stay set.
    private func policyControl() -> NSView {
        let card = NoteCard()
        let button = ChipButton(title: L.t.settingsOrchestratorPolicyEdit)
        button.action = { NSWorkspace.shared.open(Orchestrator.ensurePolicyFile()) }
        card.trailing = button
        let policy = Orchestrator.policy()
        card.dot = policy == nil ? .idle : .live
        card.text = policy.map { L.t.settingsOrchestratorPolicyOn($0.split(separator: "\n").count) }
            ?? L.t.settingsOrchestratorPolicyOff
        policyCard = card
        return card
    }

    /// How far a dispatched child may go on its own — the most a task may ask for, and the
    /// default for one that asks for nothing.
    ///
    /// Three stops in the order they escalate, and the middle one is the shipped answer. That is the
    /// row worth understanding: on a tab somebody is watching, "ask about everything" is the
    /// careful setting; on a tab nobody is watching it is the setting where the work quietly does
    /// not happen and the session sits at a prompt until it times out. The middle rather than the
    /// first because a dispatched session's whole output is files, and the first stops before
    /// writing one.
    private func permissionPopUp() -> NSView {
        let stops: [(label: String, value: String)] = [
            (label: L.t.settingsOrchestratorPermissionAsk, value: Permission.ask.rawValue),
            (label: L.t.settingsOrchestratorPermissionEdits, value: Permission.edits.rawValue),
            (label: L.t.settingsOrchestratorPermissionFull, value: Permission.full.rawValue),
        ]
        return popUp(stops, current: Config.shared.orchestratorPermission) {
            Config.shared.orchestratorPermission = $0
        }
    }

    /// What becomes of a child's tab after it reports. Three stops rather than a number: the
    /// choice people actually make is now, in a bit, or never — and three minutes is the only
    /// "in a bit" anybody would type. A hand-edited `orchestrator_child_linger` between the stops
    /// shows as the nearest one and is left alone until this control is touched.
    private func lingerPopUp() -> NSView {
        // Label first, value second — the order `popUp` takes and every other caller passes.
        // Reversed, this drew the three seconds counts as its own labels and picked none of them:
        // `current` was matched against the sentences, and a pick handed `Int(_:)` one of them.
        let stops: [(label: String, value: String)] = [
            (label: L.t.settingsOrchestratorCloseNow, value: "0"),
            (label: L.t.settingsOrchestratorCloseLinger, value: "180"),
            (label: L.t.settingsOrchestratorCloseKeep, value: "-1"),
        ]
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
        refreshPolicyCard()
        refreshDevices()
        refreshTunnel()
        refreshSmartHealth()
        if Date() >= schedulesRefreshAt { refreshSchedules() }
    }

    /// Whether smart notifications are actually producing sentences, in the words
    /// ``SmartNotification.healthLine(_:copy:)`` chooses — refreshed with the other live
    /// readings so a failure shows up while the window is open, not on the next launch.
    private func refreshSmartHealth() {
        guard let card = smartHealthCard else { return }
        let health = SmartNotification.healthSnapshot()
        let was = card.text
        if health.attempts == 0 && !Config.shared.smartNotifications {
            card.text = ""
            card.dot = .idle
        } else {
            card.text = SmartNotification.healthLine(health, copy: L.t)
            if let ok = health.lastResolvedWasSuccess {
                card.dot = ok ? .live : .warn
            } else {
                card.dot = .idle
            }
        }
        if card.text != was { relayoutCurrent() }
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

    /// The schedule files are the settings. This view deliberately uses the orchestrator's
    /// inventory rather than decoding a second notion of validity here, then writes the one field
    /// the control owns back to the source file. The next refresh enters through that same
    /// inventory, so an editor save and a switch click converge on one answer.
    private func scheduleControl() -> NSView {
        let control = ScheduleSettingsControl(text: L.t)
        control.onToggle = { [weak self] row, enabled in
            self?.setSchedule(row, enabled: enabled)
        }
        control.onRun = { [weak self] row in self?.runSchedule(row) }
        control.onReveal = { [weak self] row in self?.revealSchedule(row) }
        control.onEdit = { [weak self] row in self?.editSchedule(row) }
        control.onNew = { [weak self] in self?.newSchedule() }
        control.onOpenFolder = { [weak self] in self?.openScheduleFolder() }
        control.onResize = { [weak self] in self?.relayoutCurrent() }
        schedulesControl = control
        refreshSchedules()
        return control
    }

    private func refreshSchedules(force: Bool = false, clearMessage: Bool = true) {
        guard force || Date() >= schedulesRefreshAt else { return }
        if schedulesRefreshing {
            schedulesRefreshPending = schedulesRefreshPending || force
            return
        }
        schedulesRefreshAt = Date().addingTimeInterval(30)
        schedulesRefreshing = true
        if clearMessage { schedulesControl?.message = nil }
        DispatchQueue.global(qos: .utility).async { [weak self] in
            let rows = ScheduleSettingsRow.rows(from: Orchestrator.scheduleRecords())
            DispatchQueue.main.async {
                guard let self else { return }
                self.schedulesRefreshing = false
                if let control = self.schedulesControl, control.rows != rows {
                    control.rows = rows
                    self.relayoutCurrent()
                }
                if self.schedulesRefreshPending {
                    self.schedulesRefreshPending = false
                    self.schedulesRefreshAt = .distantPast
                    self.refreshSchedules(force: true, clearMessage: false)
                }
            }
        }
    }

    private func setSchedule(_ row: ScheduleSettingsRow, enabled: Bool) {
        let url = Orchestrator.scheduleDirectory.appendingPathComponent(row.file)
        do {
            try ScheduleSettingsRow.setEnabled(enabled, at: url)
            schedulesControl?.message = nil
        } catch {
            schedulesControl?.message = error.localizedDescription
        }
        schedulesRefreshAt = .distantPast
        refreshSchedules(force: true, clearMessage: false)
    }

    private func runSchedule(_ row: ScheduleSettingsRow) {
        guard let id = row.id else { return }
        schedulesControl?.message = nil
        RemoteServer.shared.serialized { [weak self] in
            let reply = Orchestrator.runSchedule(id: id)
            DispatchQueue.main.async {
                guard let self else { return }
                switch reply {
                case .ok:
                    self.schedulesControl?.message = L.t.settingsScheduleStarted
                case .refused(_, let code, _, _):
                    self.schedulesControl?.message = code == "schedule_active"
                        ? L.t.settingsScheduleActive : L.t.webRequestFailed
                }
                self.schedulesRefreshAt = .distantPast
                self.refreshSchedules(force: true, clearMessage: false)
            }
        }
    }

    private func revealSchedule(_ row: ScheduleSettingsRow) {
        let url = Orchestrator.scheduleDirectory.appendingPathComponent(row.file)
        NSWorkspace.shared.activateFileViewerSelecting([url])
    }

    // MARK: - Making, changing and removing a schedule

    /// The empty form. `StartPoints.places()` walks two assistants' records of where they have
    /// been run, so it is asked for off the main thread — the same reason the list itself is
    /// refreshed off it.
    private func newSchedule() {
        guard window != nil, scheduleSheet == nil else { return }
        schedulesControl?.message = nil
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let places = StartPoints.places()
            DispatchQueue.main.async {
                self?.presentScheduleForm(editing: nil, places: places)
            }
        }
    }

    /// The same form, filled in from the file. Read through ``Orchestrator/schedules()`` rather
    /// than by decoding the JSON here: the row on screen came through that inventory, and a
    /// second reading of the same file is a second opinion about what it says.
    private func editSchedule(_ row: ScheduleSettingsRow) {
        guard let id = row.id, window != nil, scheduleSheet == nil else { return }
        schedulesControl?.message = nil
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let schedule = Orchestrator.schedules().first { $0.id == id }
            let places = StartPoints.places()
            DispatchQueue.main.async {
                guard let self else { return }
                guard let schedule else {
                    // Gone since the list was drawn — another window, or the Finder. Nothing to
                    // say that the row disappearing does not say better.
                    self.schedulesRefreshAt = .distantPast
                    self.refreshSchedules(force: true)
                    return
                }
                self.presentScheduleForm(editing: schedule, places: places)
            }
        }
    }

    private func presentScheduleForm(editing schedule: Orchestrator.Schedule?,
                                     places known: [StartPoints.Place]) {
        guard let window, scheduleSheet == nil else { return }
        let places = ScheduleFormState.placeChoices(
            known, including: schedule?.taskTemplate["project_dir"] as? String)
        let state = schedule.map { ScheduleFormState(schedule: $0, places: places) }
            ?? ScheduleFormState()
        let form = ScheduleFormView(state: state, places: places, assistants: Assistant.available,
                                    editing: schedule != nil, text: L.t)
        let id = schedule?.id
        form.onCancel = { [weak self] in self?.closeScheduleForm() }
        form.onSave = { [weak self] state in self?.saveSchedule(state, id: id, places: places) }
        form.onDelete = { [weak self] in
            guard let id, let schedule else { return }
            self?.askDeleteSchedule(id: id, title: schedule.title)
        }
        form.onResize = { [weak self] in self?.resizeScheduleSheet() }

        let sheet = NSWindow(contentRect: NSRect(x: 0, y: 0, width: ScheduleFormView.contentWidth,
                                                 height: form.contentHeight),
                             styleMask: [.titled], backing: .buffered, defer: false)
        sheet.backgroundColor = Style.ink
        // Dark whatever the system is, the same as the window it drops out of — every surface
        // colour this project owns is white at a low alpha and disappears on a light ground.
        sheet.appearance = NSAppearance(named: .darkAqua)
        // Held here for as long as it is up, so it must not also be released on close — the same
        // reason the settings window itself says so.
        sheet.isReleasedWhenClosed = false
        sheet.contentView = form
        sheet.initialFirstResponder = form.firstField
        form.frame = NSRect(x: 0, y: 0, width: ScheduleFormView.contentWidth,
                            height: form.contentHeight)
        scheduleSheet = sheet
        scheduleForm = form
        window.beginSheet(sheet)
    }

    /// The fold opened, or a refusal arrived and took a line. The sheet is as tall as what is in
    /// it, the same rule the window itself follows when a tab changes.
    private func resizeScheduleSheet() {
        guard let sheet = scheduleSheet, let form = scheduleForm else { return }
        let height = form.contentHeight
        sheet.setContentSize(NSSize(width: ScheduleFormView.contentWidth, height: height))
        form.frame = NSRect(x: 0, y: 0, width: ScheduleFormView.contentWidth, height: height)
        form.needsLayout = true
    }

    private func closeScheduleForm() {
        guard let sheet = scheduleSheet else { return }
        window?.endSheet(sheet)
        scheduleSheet = nil
        scheduleForm = nil
    }

    /// Make one, or save one that exists — `id` is which of the two this is.
    ///
    /// Both go through ``RemoteServer/serialized(_:)``, the same serial gate a request from a
    /// phone enters, so the Mac's own form cannot be writing a file while a `PATCH` for it is
    /// half-written. Neither is validated here: the body goes to the orchestrator, and whatever
    /// it refuses comes back as the sentence it wrote about the field it did not like.
    private func saveSchedule(_ state: ScheduleFormState, id: String?,
                              places: [StartPoints.Place]) {
        guard let form = scheduleForm, !form.busy else { return }
        form.busy = true
        form.message = nil
        let body = state.body
        RemoteServer.shared.serialized { [weak self] in
            let reply = id.map { Orchestrator.updateSchedule(id: $0, from: body, places: places) }
                ?? Orchestrator.createSchedule(from: body, places: places)
            DispatchQueue.main.async {
                guard let self else { return }
                self.scheduleForm?.busy = false
                switch reply {
                case .ok:
                    self.closeScheduleForm()
                    self.schedulesControl?.message = id == nil
                        ? L.t.webScheduleCreated : L.t.webScheduleSaved
                    self.schedulesRefreshAt = .distantPast
                    self.refreshSchedules(force: true, clearMessage: false)
                case .refused(_, _, let message, _):
                    // Left open, holding what was typed, with the refusal under the fields it is
                    // about. Closing on a refusal would take away the only copy of a first
                    // message somebody just wrote.
                    self.scheduleForm?.message = message.isEmpty ? L.t.webScheduleFailed : message
                }
            }
        }
    }

    /// Asked about first, because there is no undo and no route that could add one.
    ///
    /// `NSAlert` is this window's idiom for a question — see ``toggleHooks()`` — and the sheet it
    /// opens on is the form's own, so the form stays on screen behind it and cancelling puts
    /// everything back exactly as it was.
    private func askDeleteSchedule(id: String, title: String) {
        guard let sheet = scheduleSheet, scheduleForm?.busy == false else { return }
        let alert = NSAlert()
        alert.messageText = L.t.settingsScheduleDelete
        alert.informativeText = L.t.webScheduleDeleteAsk
            .replacingOccurrences(of: "{title}", with: title)
        alert.alertStyle = .warning
        let remove = alert.addButton(withTitle: L.t.settingsScheduleDelete)
        let keep = alert.addButton(withTitle: L.t.webCancel)
        remove.hasDestructiveAction = true
        // Return cancels and does not delete. The default button of a dialog is the one a hand
        // already moving hits, and this one takes a schedule away for good.
        remove.keyEquivalent = ""
        keep.keyEquivalent = "\r"
        alert.beginSheetModal(for: sheet) { [weak self] response in
            guard response == .alertFirstButtonReturn else { return }
            self?.deleteSchedule(id: id)
        }
    }

    private func deleteSchedule(id: String) {
        guard let form = scheduleForm, !form.busy else { return }
        form.busy = true
        form.message = nil
        RemoteServer.shared.serialized { [weak self] in
            let reply = Orchestrator.deleteSchedule(id: id)
            DispatchQueue.main.async {
                guard let self else { return }
                self.scheduleForm?.busy = false
                switch reply {
                case .ok:
                    self.closeScheduleForm()
                    self.schedulesControl?.message = L.t.webScheduleDeleted
                    self.schedulesRefreshAt = .distantPast
                    self.refreshSchedules(force: true, clearMessage: false)
                case .refused(_, _, let message, _):
                    self.scheduleForm?.message = message.isEmpty ? L.t.webScheduleFailed : message
                }
            }
        }
    }

    private func openScheduleFolder() {
        do {
            try FileManager.default.createDirectory(at: Orchestrator.scheduleDirectory,
                withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
            if !NSWorkspace.shared.open(Orchestrator.scheduleDirectory) {
                schedulesControl?.message = Orchestrator.scheduleDirectory.path
            }
        } catch {
            schedulesControl?.message = error.localizedDescription
        }
    }

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
    /// The card is read off the file, and the file is edited somewhere else — so it is re-read on
    /// the same beat everything else on these panes is, rather than only when the pane is built.
    /// Somebody who saves in their editor and looks back at this window should see it.
    private func refreshPolicyCard() {
        guard let card = policyCard else { return }
        let policy = Orchestrator.policy()
        let text = policy.map { L.t.settingsOrchestratorPolicyOn($0.split(separator: "\n").count) }
            ?? L.t.settingsOrchestratorPolicyOff
        let changed = card.text != text
        card.text = text
        card.dot = policy == nil ? .idle : .live
        if changed { relayoutCurrent() }
    }

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

/// The small, typed seam between the orchestrator's JSON-shaped inventory and the hand-built
/// settings control. Kept free of AppKit so malformed-row assembly and the source-file edit can
/// be exercised without constructing a window.
struct ScheduleSettingsRow: Equatable {
    let id: String?
    let file: String
    let title: String
    let enabled: Bool?
    let nextFire: Date?
    let lastState: String?
    let lastAt: Date?
    let lastMissedAt: Date?
    let error: String?

    static func rows(from records: [[String: Any]]) -> [ScheduleSettingsRow] {
        records.compactMap { record in
            if record["state"] as? String == "invalid" {
                let file = record["file"] as? String ?? "schedule.json"
                return ScheduleSettingsRow(id: nil, file: file, title: file, enabled: nil,
                    nextFire: nil, lastState: nil, lastAt: nil, lastMissedAt: nil,
                    error: record["error"] as? String ?? "Invalid schedule")
            }
            // `scheduleRecords()` only emits a valid row after enforcing that its id matches the
            // filename. A defensive row without that id must not grow controls that write a guess.
            guard let id = record["id"] as? String else { return nil }
            let last = record["last_run"] as? [String: Any]
            return ScheduleSettingsRow(id: id, file: "\(id).json",
                title: record["title"] as? String ?? id,
                enabled: record["enabled"] as? Bool,
                nextFire: date(from: record["next_fire"]),
                lastState: last?["state"] as? String,
                lastAt: date(from: last?["at"]),
                lastMissedAt: date(from: record["last_missed_at"]), error: nil)
        }
    }

    enum StatePresentation: Equatable {
        case running, success, failure, timeout, cancelled, spawnFailed
    }

    static func presentation(for state: String) -> StatePresentation? {
        switch state {
        case "queued", "spawning", "briefed": return .running
        case "success": return .success
        case "failure": return .failure
        case "timeout": return .timeout
        case "cancelled": return .cancelled
        case "spawn_failed": return .spawnFailed
        default: return nil
        }
    }

    private static func date(from value: Any?) -> Date? {
        guard let number = value as? NSNumber,
              CFGetTypeID(number) != CFBooleanGetTypeID() else { return nil }
        return Date(timeIntervalSince1970: number.doubleValue)
    }

    enum EditError: LocalizedError {
        case notJSONObject
        case missingEnabled
        case verificationFailed

        var errorDescription: String? {
            switch self {
            case .notJSONObject: return "The schedule file is not a JSON object."
            case .missingEnabled: return "The schedule file has no boolean enabled field."
            case .verificationFailed: return "The schedule file could not be verified after editing."
            }
        }
    }

    /// Replace only the top-level boolean token. These files belong to their authors, so a switch
    /// must not reorder their keys or rewrite their whitespace. The full JSON rewrite is only a
    /// recovery path for a valid object whose source token cannot be located, and is audited.
    static func setEnabled(_ enabled: Bool, at url: URL) throws {
        let data = try Data(contentsOf: url)
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { throw EditError.notJSONObject }
        guard let old = object["enabled"] as? NSNumber,
              CFGetTypeID(old) == CFBooleanGetTypeID() else { throw EditError.missingEnabled }
        let changed: Data
        var rewrote = false
        if let range = topLevelEnabledValue(in: data) {
            var bytes = data
            bytes.replaceSubrange(range, with: Data((enabled ? "true" : "false").utf8))
            changed = bytes
        } else {
            var fallback = object
            fallback["enabled"] = enabled
            changed = try JSONSerialization.data(withJSONObject: fallback,
                options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
            rewrote = true
        }
        try changed.write(to: url, options: .atomic)
        let written = try Data(contentsOf: url)
        guard let verified = try JSONSerialization.jsonObject(with: written) as? [String: Any],
              let value = verified["enabled"] as? NSNumber,
              CFGetTypeID(value) == CFBooleanGetTypeID(), value.boolValue == enabled
        else { throw EditError.verificationFailed }
        if rewrote {
            RemoteAuth.audit("orchestrator.schedule.rewritten", ["file": url.lastPathComponent])
        }
    }

    private static func topLevelEnabledValue(in data: Data) -> Range<Data.Index>? {
        let bytes = [UInt8](data)
        var objectDepth = 0
        var arrayDepth = 0
        var index = 0
        var found: Range<Data.Index>?
        func whitespace(_ byte: UInt8) -> Bool {
            byte == 0x20 || byte == 0x09 || byte == 0x0a || byte == 0x0d
        }
        while index < bytes.count {
            let byte = bytes[index]
            if byte == 0x22 {
                let start = index
                index += 1
                var escaped = false
                while index < bytes.count {
                    let current = bytes[index]
                    index += 1
                    if escaped { escaped = false; continue }
                    if current == 0x5c { escaped = true; continue }
                    if current == 0x22 { break }
                }
                guard index <= bytes.count else { return nil }
                if objectDepth == 1 && arrayDepth == 0 {
                    var colon = index
                    while colon < bytes.count && whitespace(bytes[colon]) { colon += 1 }
                    if colon < bytes.count && bytes[colon] == 0x3a {
                        let wrapped = Data([0x5b] + bytes[start..<index] + [0x5d])
                        let decoded = try? JSONSerialization.jsonObject(with: wrapped)
                        let key = (decoded as? [String])?.first
                        if key == "enabled" {
                            var value = colon + 1
                            while value < bytes.count && whitespace(bytes[value]) { value += 1 }
                            let tail = bytes[value...]
                            if tail.starts(with: Array("true".utf8)) {
                                found = value..<(value + 4)
                            } else if tail.starts(with: Array("false".utf8)) {
                                found = value..<(value + 5)
                            }
                        }
                    }
                }
                continue
            }
            switch byte {
            case 0x7b: objectDepth += 1
            case 0x7d: objectDepth -= 1
            case 0x5b: arrayDepth += 1
            case 0x5d: arrayDepth -= 1
            default: break
            }
            index += 1
        }
        return found
    }
}

/// Everything the Mac's schedule form holds, and the one place it becomes the body
/// ``Orchestrator/createSchedule(from:places:now:isDirectory:)`` and
/// ``Orchestrator/updateSchedule(id:from:places:now:isDirectory:)`` read.
///
/// Kept free of AppKit for the same reason ``ScheduleSettingsRow`` is: this is the part of the
/// form with judgement in it — which weekday codes, which defaults, what an edit carries over —
/// and it can be exercised without a window on screen.
///
/// **It does not validate.** `Orchestrator.schedule(from:)` refuses thirty-one distinct kinds of
/// malformed input and has written a sentence about each; a second opinion here would be a second
/// wording to keep in step, and the one that drifts is always the one nobody is reading. So the
/// form collects fields, hands them over, and shows whatever comes back.
struct ScheduleFormState: Equatable {

    /// Sunday first — the order `Calendar` numbers weekdays in, the order the parser's own table
    /// lists them in, and the order the chips are drawn in.
    static let dayCodes = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"]
    static let closeValues = ["on_success", "always", "never"]
    /// The same two numbers `Orchestrator.schedule(from:)` gives a file that leaves them out, so
    /// somebody who never opens More gets exactly what the Mac would have picked anyway.
    static let catchUpDefault = 6
    static let timeoutDefault = 30
    /// What an empty form opens on. A round hour rather than whatever time it happens to be:
    /// a picker showing 14:37 reads as a value somebody chose, and this one is a placeholder.
    static let timeDefault = "09:00"

    var title = ""
    /// `HH:MM` in local time, produced by the picker rather than typed — see ``time(from:calendar:)``.
    var at = ScheduleFormState.timeDefault
    var daily = true
    /// Only read when ``daily`` is false. Codes from ``dayCodes``.
    var weekdays: Set<String> = []
    /// A `place_id` from ``StartPoints/places()``, never a path. See ``placeChoices(_:including:)``.
    var placeID = ""
    var assistant = ""
    var instructions = ""
    var enabled = true
    var closeTab = "on_success"
    var catchUpHours = ScheduleFormState.catchUpDefault
    var notifyOnFailure = true
    var timeoutMinutes = ScheduleFormState.timeoutDefault
    /// Carried across a save and never shown. There is no control for it — the form offers the
    /// fields the page's form offers — but `scheduleObject(from:)` builds the whole task template
    /// out of the body it is handed, so a field left out of a save is a field taken off the
    /// schedule. Dropping somebody's `"model": "opus"` because this form never offered to change
    /// it would be the form editing something it did not show.
    var model = ""

    init() {}

    /// The form, filled in from a schedule that already exists.
    ///
    /// `places` is passed in rather than read here so that the same list the Where popup is built
    /// from is the list this resolves against — see ``placeChoices(_:including:)`` for why those
    /// two must not be allowed to disagree.
    init(schedule: Orchestrator.Schedule, places: [StartPoints.Place]) {
        let task = schedule.taskTemplate
        title = schedule.title
        at = String(format: "%02d:%02d", schedule.hour, schedule.minute)
        daily = schedule.weekdays == nil
        weekdays = Set((schedule.weekdays ?? []).compactMap(Self.code(forWeekday:)))
        // The file has a path and the form has an id, the same way round as the page's own
        // `placeIdForPath`. Nothing is guessed when it is not on the list: `placeChoices` is what
        // makes sure it is.
        let directory = task["project_dir"] as? String
        placeID = places.first { $0.path == directory }?.id ?? ""
        assistant = task["assistant"] as? String ?? ""
        instructions = task["instructions"] as? String ?? ""
        enabled = schedule.enabled
        closeTab = schedule.closeTab.rawValue
        catchUpHours = schedule.catchUpHours
        notifyOnFailure = schedule.notifyOnFailure
        timeoutMinutes = task["timeout_minutes"] as? Int ?? Self.timeoutDefault
        model = task["model"] as? String ?? ""
    }

    /// The flat body both orchestrator functions read.
    var body: [String: Any] {
        var out: [String: Any] = [
            "title": title,
            "at": at,
            "days": daily ? "daily" : Self.dayCodes.filter { weekdays.contains($0) },
            "place_id": placeID,
            "assistant": assistant,
            "instructions": instructions,
            "enabled": enabled,
            "close_tab": closeTab,
            "catch_up_hours": catchUpHours,
            "notify_on_failure": notifyOnFailure,
            "timeout_minutes": timeoutMinutes,
        ]
        // Empty means "whatever that assistant runs by default", and the allowlist would take
        // `"model": ""` happily — leaving a field in the file whoever opens it later has to read
        // and dismiss. See the same rule in `Orchestrator.scheduleObject(from:)`.
        if !model.isEmpty { out["model"] = model }
        return out
    }

    /// Daily, or some weekdays, never neither — the one shape the parser refuses outright, made
    /// unreachable here rather than sent and refused. Picking a day while Daily is on replaces it
    /// rather than adding to it, because a chip meaning "every day" stops meaning that the moment
    /// one day is chosen instead.
    mutating func toggle(day code: String) {
        guard !daily else {
            daily = false
            weekdays = [code]
            return
        }
        if weekdays.contains(code) {
            weekdays.remove(code)
            if weekdays.isEmpty { daily = true }
        } else {
            weekdays.insert(code)
        }
    }

    /// 1 = Sunday … 7 = Saturday, the numbering `Calendar` uses and the one a parsed schedule
    /// keeps its days in.
    static func code(forWeekday number: Int) -> String? {
        guard (1...7).contains(number) else { return nil }
        return dayCodes[number - 1]
    }

    /// The picker's instant as `when.at`: two digits, a colon, two digits, in local time.
    static func time(from date: Date, calendar: Calendar = .current) -> String {
        let parts = calendar.dateComponents([.hour, .minute], from: date)
        return String(format: "%02d:%02d", parts.hour ?? 0, parts.minute ?? 0)
    }

    /// The reverse, so the picker can open on the time the file already says. Any day will do —
    /// only the hour and the minute are ever read back out of it.
    static func date(forTime text: String, on day: Date = Date(),
                     calendar: Calendar = .current) -> Date? {
        // Two digits each, the same test `Orchestrator.schedule(from:)` makes of `when.at`. A
        // looser reading here would have this form opening happily on a time the parser is about
        // to refuse, and the refusal would arrive on Save naming a field nobody had touched.
        let parts = text.split(separator: ":", omittingEmptySubsequences: false)
        guard parts.count == 2, parts[0].count == 2, parts[1].count == 2,
              let hour = Int(parts[0]), let minute = Int(parts[1]),
              (0...23).contains(hour), (0...59).contains(minute) else { return nil }
        return calendar.date(bySettingHour: hour, minute: minute, second: 0, of: day)
    }

    /// What a box holding a number says, or the default when it says something else.
    ///
    /// The same fallback the page's form makes, and for the same reason: an empty catch-up box is
    /// somebody who did not want to choose, not a request for zero hours. Anything the box can
    /// still hold that this lets through — 4000 hours — is the parser's to refuse, in its words.
    static func number(_ text: String, atLeast floor: Int, or fallback: Int) -> Int {
        guard let value = Int(text.trimmingCharacters(in: .whitespaces)), value >= floor else {
            return fallback
        }
        return value
    }

    /// The list the Where popup offers: the recent places, plus — when it is not already among
    /// them — the directory the schedule being edited already names.
    ///
    /// `StartPoints.places()` is the forty most recently worked-in directories, and a schedule
    /// that has been running quietly since spring is exactly the one whose project has fallen off
    /// that list. Without this, opening such a row to fix a typo would leave Where unanswered and
    /// Save refused, and the only way to keep the schedule would be the text editor this form
    /// exists to replace.
    ///
    /// It is not the loophole it looks like. The argument for `place_id` is that a *device* can
    /// only name a project this Mac has already shown it; this path is the Mac's own window, and
    /// the path it adds came off a file already on disk rather than out of a request. The parser
    /// still refuses it if that directory has since gone.
    static func placeChoices(_ known: [StartPoints.Place],
                             including path: String?) -> [StartPoints.Place] {
        guard let path, !path.isEmpty, !known.contains(where: { $0.path == path }) else {
            return known
        }
        return known + [StartPoints.Place(id: StartPoints.id(for: path), path: path,
                                          label: StartPoints.label(for: path), at: .distantPast)]
    }

    /// What each place is called in the popup: the project's own name, unless two of them share
    /// it — a list with `clawdline` twice in it is a list nobody can pick from. `home` is a
    /// parameter so a test can describe a Mac rather than being run on one.
    static func placeLabels(_ places: [StartPoints.Place],
                            home: String = NSHomeDirectory()) -> [String] {
        var counts: [String: Int] = [:]
        for place in places { counts[place.label, default: 0] += 1 }
        return places.map { place in
            guard (counts[place.label] ?? 0) > 1 else { return place.label }
            return "\(place.label) — \(place.path.replacingOccurrences(of: home, with: "~"))"
        }
    }
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

/// The schedule inventory as a stack of small cards followed by its folder action. The control
/// owns no scheduler state: rebuilding from `scheduleRecords()` is cheap, keeps invalid neighbors
/// visible, and means a file edited outside the app wins on the next thirty-second refresh.
private final class ScheduleSettingsControl: NSView, SelfSizing {
    var rows: [ScheduleSettingsRow] = [] {
        didSet {
            if rows != oldValue { rebuild(); onResize?() }
        }
    }
    var message: String? {
        didSet {
            if message != oldValue { rebuild(); onResize?() }
        }
    }
    var onToggle: ((ScheduleSettingsRow, Bool) -> Void)?
    var onRun: ((ScheduleSettingsRow) -> Void)?
    var onReveal: ((ScheduleSettingsRow) -> Void)?
    var onEdit: ((ScheduleSettingsRow) -> Void)?
    var onNew: (() -> Void)?
    var onOpenFolder: (() -> Void)?
    var onResize: (() -> Void)?

    private let text: Copy
    private let folder: ChipButton
    /// Lit rather than plain: on a card whose other actions are all about a row that already
    /// exists, this is the one that makes one, and it is what somebody arriving at an empty list
    /// is looking for.
    private let new: ChipButton
    private var rowViews: [ScheduleSettingsRowView] = []
    private var emptyLabel: NSTextField?
    private var messageLabel: NSTextField?

    init(text: Copy) {
        self.text = text
        self.folder = ChipButton(title: text.settingsSchedulesFolder)
        self.new = ChipButton(title: text.settingsScheduleNew, prominent: true)
        super.init(frame: NSRect(x: 0, y: 0, width: 700, height: 80))
        folder.action = { [weak self] in self?.onOpenFolder?() }
        new.action = { [weak self] in self?.onNew?() }
        rebuild()
    }
    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    private func rebuild() {
        subviews.forEach { $0.removeFromSuperview() }
        rowViews = rows.map { row in
            let view = ScheduleSettingsRowView(row: row, text: text,
                onToggle: { [weak self] enabled in self?.onToggle?(row, enabled) },
                onRun: { [weak self] in self?.onRun?(row) },
                onReveal: { [weak self] in self?.onReveal?(row) },
                onEdit: { [weak self] in self?.onEdit?(row) })
            addSubview(view)
            return view
        }
        emptyLabel = nil
        if rows.isEmpty {
            let label = makeLabel(text.settingsSchedulesEmpty, Metric.noteFont, Metric.faint)
            addSubview(label)
            emptyLabel = label
        }
        messageLabel = nil
        if let message, !message.isEmpty {
            let label = makeLabel(message, Metric.hintFont, Style.accent)
            addSubview(label)
            messageLabel = label
        }
        addSubview(new)
        addSubview(folder)
        needsLayout = true
        superview?.needsLayout = true
    }

    func height(forWidth width: CGFloat) -> CGFloat {
        var height: CGFloat = 0
        if rows.isEmpty {
            height = max(22, labelHeight(text.settingsSchedulesEmpty, Metric.noteFont, width: width)) + 8
        } else {
            for view in rowViews {
                height += view.height(forWidth: width) + 8
            }
        }
        if let message, !message.isEmpty {
            height += labelHeight(message, Metric.hintFont, width: width) + 8
        }
        return height + (actionsWrap(width: width) ? 68 : 34)
    }

    /// Whether the two actions under the list need a line each. They fit side by side in English
    /// and in most of the fourteen; "Neuen Zeitplan anlegen" beside "Zeitplan-Ordner öffnen" does
    /// not, and a chip drawn past the right edge of a column is a chip nobody can press.
    private func actionsWrap(width: CGFloat) -> Bool {
        new.frame.width + 8 + folder.frame.width > width
    }

    override func layout() {
        super.layout()
        var y: CGFloat = 0
        if let emptyLabel {
            let height = labelHeight(emptyLabel.stringValue, Metric.noteFont, width: bounds.width)
            emptyLabel.preferredMaxLayoutWidth = bounds.width
            emptyLabel.frame = NSRect(x: 0, y: y, width: bounds.width, height: height)
            y += max(22, height) + 8
        } else {
            for view in rowViews {
                let height = view.height(forWidth: bounds.width)
                view.frame = NSRect(x: 0, y: y, width: bounds.width, height: height)
                y += height + 8
            }
        }
        if let messageLabel {
            let height = labelHeight(messageLabel.stringValue, Metric.hintFont, width: bounds.width)
            messageLabel.preferredMaxLayoutWidth = bounds.width
            messageLabel.frame = NSRect(x: 0, y: y, width: bounds.width, height: height)
            y += height + 8
        }
        new.frame.origin = NSPoint(x: 0, y: y + 4)
        if actionsWrap(width: bounds.width) {
            folder.frame.origin = NSPoint(x: 0, y: y + 4 + 34)
        } else {
            folder.frame.origin = NSPoint(x: new.frame.width + 8, y: y + 4)
        }
    }
}

/// One source file. Valid rows have a switch and a manual-run action; invalid rows retain the
/// Finder action but trade controls for the schema error, which keeps a broken file repairable.
private final class ScheduleSettingsRowView: NSView, SelfSizing {
    private let row: ScheduleSettingsRow
    private let title: NSTextField
    private let detail: NSTextField
    private let missed: NSTextField?
    private let toggle: SwitchView?
    private let run: ChipButton?
    private let edit: ChipButton?
    private let reveal: ChipButton

    init(row: ScheduleSettingsRow, text: Copy,
         onToggle: @escaping (Bool) -> Void, onRun: @escaping () -> Void,
         onReveal: @escaping () -> Void, onEdit: @escaping () -> Void) {
        self.row = row
        self.title = makeLabel(row.title, Metric.labelFont, Metric.label)
        self.detail = makeLabel(Self.detail(for: row, text: text), Metric.hintFont,
                                row.error == nil ? Metric.faint : Style.accent)
        self.missed = row.lastMissedAt.map {
            let formatter = RelativeDateTimeFormatter()
            formatter.unitsStyle = .full
            return makeLabel("\(text.webScheduleMissed) \(formatter.localizedString(for: $0, relativeTo: Date()))",
                             Metric.hintFont, Style.accent)
        }
        if let enabled = row.enabled {
            self.toggle = SwitchView(isOn: enabled, onToggle)
            let button = ChipButton(title: text.settingsScheduleRun)
            button.action = onRun
            self.run = button
            // Only a row the orchestrator gave an id to. An invalid file has no id to address, and
            // an edit replaces the whole file — there would be nothing to fill the form in from
            // and nothing to carry `created_at` over off. Reveal in Finder stays, which is the
            // repair a broken file actually wants.
            let editButton = ChipButton(title: text.settingsScheduleEdit)
            editButton.action = onEdit
            self.edit = editButton
        } else {
            self.toggle = nil
            self.run = nil
            self.edit = nil
        }
        self.reveal = ChipButton(title: text.settingsScheduleReveal)
        super.init(frame: NSRect(x: 0, y: 0, width: 700, height: 80))
        reveal.action = onReveal
        addSubview(title)
        addSubview(detail)
        if let missed { addSubview(missed) }
        if let toggle { addSubview(toggle) }
        if let edit { addSubview(edit) }
        if let run { addSubview(run) }
        addSubview(reveal)
    }
    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    private static func detail(for row: ScheduleSettingsRow,
                               text: Copy) -> String {
        if let error = row.error { return error }
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        let next = row.nextFire.map(formatter.string(from:)) ?? text.webScheduleNoNext
        let last: String
        if let state = row.lastState {
            let stateText: String
            switch ScheduleSettingsRow.presentation(for: state) {
            case .running: stateText = text.webTaskRunning
            case .success: stateText = text.webTaskDone
            case .failure, .timeout, .cancelled, .spawnFailed: stateText = text.webTaskFailed
            case nil: stateText = "—"
            }
            last = row.lastAt.map { "\(stateText) · \(formatter.string(from: $0))" } ?? stateText
        } else {
            last = text.settingsScheduleNever
        }
        return "\(text.settingsScheduleNext): \(next)    ·    \(text.settingsScheduleLast): \(last)"
    }

    func height(forWidth width: CGFloat) -> CGFloat {
        let detailHeight = labelHeight(detail.stringValue, Metric.hintFont,
                                       width: max(60, width - 24))
        let missedHeight = missed.map {
            labelHeight($0.stringValue, Metric.hintFont, width: max(60, width - 24)) + 4
        } ?? 0
        return 14 + 16 + 5 + detailHeight + missedHeight + 8 + 26 + 12
    }

    override func layout() {
        super.layout()
        let inset: CGFloat = 12
        let toggleWidth = toggle?.frame.width ?? 0
        title.frame = NSRect(x: inset, y: 11,
            width: max(40, bounds.width - inset * 2 - toggleWidth - (toggle == nil ? 0 : 12)),
            height: 16)
        if let toggle {
            toggle.frame.origin = NSPoint(x: bounds.width - inset - toggle.frame.width, y: 8)
        }
        let detailHeight = labelHeight(detail.stringValue, Metric.hintFont,
                                       width: max(60, bounds.width - inset * 2))
        detail.preferredMaxLayoutWidth = bounds.width - inset * 2
        detail.frame = NSRect(x: inset, y: 32, width: bounds.width - inset * 2,
                              height: detailHeight)
        if let missed {
            let height = labelHeight(missed.stringValue, Metric.hintFont,
                                     width: max(60, bounds.width - inset * 2))
            missed.preferredMaxLayoutWidth = bounds.width - inset * 2
            missed.frame = NSRect(x: inset, y: 32 + detailHeight + 4,
                                  width: bounds.width - inset * 2, height: height)
        }
        let buttonY = bounds.height - 12 - 26
        var x = inset
        // Edit first: it is the one this row is most often pressed for, and the other two are
        // things you do to a schedule you have already decided is right.
        if let edit {
            edit.frame.origin = NSPoint(x: x, y: buttonY)
            x += edit.frame.width + 8
        }
        if let run {
            run.frame.origin = NSPoint(x: x, y: buttonY)
            x += run.frame.width + 8
        }
        reveal.frame.origin = NSPoint(x: x, y: buttonY)
    }

    override func draw(_ dirtyRect: NSRect) {
        let path = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5),
                                xRadius: 8, yRadius: 8)
        Style.chipFill.setFill()
        path.fill()
        (row.error == nil ? Style.chipEdge : Style.accent.withAlphaComponent(0.65)).setStroke()
        path.lineWidth = 1
        path.stroke()
    }
}

/// A row of chips that wraps rather than running off the edge.
///
/// Eight of them — Daily and seven weekdays — do not fit on one line of a sheet this wide in
/// every language, and a chip drawn past the right edge is a chip nobody can press.
private final class ChipWrap: NSView, SelfSizing {

    private let chips: [ChipButton]
    private let gap: CGFloat = 6

    init(chips: [ChipButton]) {
        self.chips = chips
        super.init(frame: NSRect(x: 0, y: 0, width: 400, height: 26))
        for chip in chips { addSubview(chip) }
    }
    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    /// One walk, used both to measure and to place, so the two cannot disagree about where the
    /// line breaks — the mistake that leaves a control drawn outside the frame reserved for it.
    @discardableResult
    private func walk(width: CGFloat, place: Bool) -> CGFloat {
        var x: CGFloat = 0
        var y: CGFloat = 0
        for chip in chips {
            if x > 0, x + chip.frame.width > width {
                x = 0
                y += chip.frame.height + gap
            }
            if place { chip.frame.origin = NSPoint(x: x, y: y) }
            x += chip.frame.width + gap
        }
        return y + (chips.first?.frame.height ?? 26)
    }

    func height(forWidth width: CGFloat) -> CGFloat { walk(width: width, place: false) }

    override func layout() {
        super.layout()
        walk(width: bounds.width, place: true)
    }
}

/// A label with something small on the right of it — what a switch needs and what the stacked
/// label-above-control shape of the rest of this form would read badly for.
private final class FlagRow: NSView {

    private let label: NSTextField
    private let control: NSView

    init(label text: String, control: NSView) {
        self.label = makeLabel(text, Metric.labelFont, Metric.label)
        self.control = control
        super.init(frame: NSRect(x: 0, y: 0, width: 400, height: max(22, control.frame.height)))
        addSubview(label)
        addSubview(control)
    }
    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    override func layout() {
        super.layout()
        let height = labelHeight(label.stringValue, Metric.labelFont,
                                 width: max(60, bounds.width - control.frame.width - 12))
        label.preferredMaxLayoutWidth = bounds.width - control.frame.width - 12
        label.frame = NSRect(x: 0, y: (bounds.height - height) / 2,
                             width: bounds.width - control.frame.width - 12, height: height)
        control.frame.origin = NSPoint(x: bounds.width - control.frame.width,
                                       y: (bounds.height - control.frame.height) / 2)
    }
}

/// The form the Mac never had: one schedule, made or changed.
///
/// **A sheet rather than a block in the Schedules card.** What it holds is a dozen fields, and a
/// tab that grows by four hundred points when somebody presses New is a tab that has moved
/// everything else out from under the pointer — including, on the Remote tab, the switch that
/// turns the server off.
///
/// **Label above control**, not the label column the settings tabs use. These labels are whole
/// sentences in fourteen languages — "If it was missed, catch up within (hours)" — and a column
/// wide enough for the German one is a column with nothing in the right two thirds of it.
///
/// The fields are the page's fields, in the page's order, with the same five behind the same
/// fold: a schedule that takes all the defaults should not make anybody read past them. See
/// `Resources/web/app/js/input/schedule.js`, which this is the Mac's half of.
private final class ScheduleFormView: NSView {

    static let contentWidth: CGFloat = 468
    private static let inset: CGFloat = 22

    /// What the controls have been changed to. Read out with ``gather()`` rather than kept in
    /// step keystroke by keystroke: a text field that reports on every character is a text field
    /// that has to say what it means by a half-typed number.
    private var state: ScheduleFormState
    private let text: Copy
    private let places: [StartPoints.Place]

    var onSave: ((ScheduleFormState) -> Void)?
    var onDelete: (() -> Void)?
    var onCancel: (() -> Void)?
    var onResize: (() -> Void)?

    /// The refusal, in the words whoever refused wrote. See ``SettingsWindow/saveSchedule(_:id:places:)``.
    var message: String? {
        didSet {
            guard message != oldValue else { return }
            rebuildMessage()
            onResize?()
        }
    }

    /// A write is in flight. Everything that could start a second one goes quiet — the file is
    /// written and read back before the answer arrives, and two saves racing on one name is the
    /// one thing this sheet must not be able to ask for.
    var busy = false {
        didSet {
            guard busy != oldValue else { return }
            save.isEnabled = !busy
            cancel.isEnabled = !busy
            delete?.isEnabled = !busy
        }
    }

    private let heading: NSTextField
    private let titleField = NSTextField(string: "")
    private let timePicker = NSDatePicker()
    private var dayChips: [ChipButton] = []
    private let instructions = NSTextView()
    private let more: ChipButton
    private let catchField = NSTextField(string: "")
    private let timeoutField = NSTextField(string: "")
    private var messageLabel: NSTextField?
    private let save: ChipButton
    private let cancel: ChipButton
    private let delete: ChipButton?

    private var expanded = false
    private var fields: [Field] = []

    private struct Field {
        let label: NSTextField?
        let view: NSView
        /// Behind the fold.
        let advanced: Bool
    }

    init(state: ScheduleFormState, places: [StartPoints.Place], assistants: [Assistant],
         editing: Bool, text: Copy) {
        self.state = state
        self.text = text
        self.places = places
        self.heading = makeLabel(editing ? text.webScheduleEdit : text.webScheduleNew,
                                 Metric.tabFont, Metric.label)
        self.more = ChipButton(title: text.webScheduleMore)
        self.save = ChipButton(title: text.settingsScheduleSave, prominent: true)
        self.cancel = ChipButton(title: text.webCancel)
        self.delete = editing ? ChipButton(title: text.settingsScheduleDelete) : nil
        super.init(frame: NSRect(x: 0, y: 0, width: Self.contentWidth, height: 400))
        build(assistants: assistants)
    }
    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    // MARK: - Building it

    private func build(assistants: [Assistant]) {
        addSubview(heading)

        let inner = Self.contentWidth - Self.inset * 2

        titleField.stringValue = state.title
        titleField.font = Metric.labelFont
        titleField.frame = NSRect(x: 0, y: 0, width: inner, height: 24)
        add(text.webScheduleTitle, titleField)

        // A picker rather than a box holding "09:30". The parser has a sentence for a time it
        // cannot read and this form would rather never need it: the control cannot be typed into
        // a shape `when.at` refuses, and it reads back as hour and minute whether the person's
        // locale shows them nine in the morning or 9 AM.
        timePicker.datePickerStyle = .textFieldAndStepper
        timePicker.datePickerElements = [.hourMinute]
        timePicker.dateValue = ScheduleFormState.date(forTime: state.at) ?? Date()
        timePicker.sizeToFit()
        add(text.webScheduleAt, timePicker)

        dayChips = ([text.webScheduleDaily] + Self.dayNames(text)).enumerated().map { index, name in
            let chip = ChipButton(title: name)
            chip.action = { [weak self] in self?.pickDay(index) }
            return chip
        }
        let days = ChipWrap(chips: dayChips)
        days.frame.size = NSSize(width: inner, height: days.height(forWidth: inner))
        add(text.webScheduleOn, days)
        paintDays()

        // The popup picks its first item when it is handed a `current` it cannot find, and says
        // nothing about having done so. Agreeing with it here rather than leaving the selection
        // and the state saying two different things: what is on screen is what gets sent.
        if !places.contains(where: { $0.id == state.placeID }) {
            state.placeID = places.first?.id ?? ""
        }
        let labels = ScheduleFormState.placeLabels(places)
        let placePopUp = ChoicePopUp(options: zip(labels, places.map(\.id))
            .map { (label: $0.0, value: $0.1) }, current: state.placeID) { [weak self] value in
            self?.state.placeID = value
        }
        // The whole path, on the item rather than in the label: two projects called the same
        // thing are told apart by their labels already, and this answers "which one is that" for
        // every other row without spending a line of the sheet on it.
        for (index, place) in places.enumerated() where index < placePopUp.numberOfItems {
            placePopUp.item(at: index)?.toolTip = place.path
        }
        add(text.webScheduleWhere, placePopUp)

        // The same rule the page's own `drawWith` follows: one assistant is not a choice, and a
        // label over a control that is not there is a field somebody will go looking for.
        //
        // The list is what this Mac has installed, plus — when the schedule already names one it
        // has not — the one it names. A row written for Codex on a Mac that has since lost it is
        // still that row's answer, and a popup quietly reading "Claude Code" over a file that
        // says `codex` is a form lying about what it is holding.
        var choices = assistants.map { (label: $0.label, value: $0.rawValue) }
        if !state.assistant.isEmpty, !choices.contains(where: { $0.value == state.assistant }) {
            choices.append((label: Assistant(rawValue: state.assistant)?.label ?? state.assistant,
                            value: state.assistant))
        }
        if state.assistant.isEmpty { state.assistant = choices.first?.value ?? "" }
        if choices.count > 1 {
            add(text.webScheduleWith,
                ChoicePopUp(options: choices, current: state.assistant) { [weak self] value in
                    self?.state.assistant = value
                })
        }

        add(text.webScheduleFirst, instructionsBox(width: inner))

        more.action = { [weak self] in self?.toggleMore() }
        add(nil, more)

        let closeStops: [(label: String, value: String)] = [
            (label: text.webScheduleCloseSuccess, value: "on_success"),
            (label: text.webScheduleCloseAlways, value: "always"),
            (label: text.webScheduleCloseNever, value: "never"),
        ]
        add(text.webScheduleWhenDone,
            ChoicePopUp(options: closeStops, current: state.closeTab) { [weak self] value in
                self?.state.closeTab = value
            }, advanced: true)

        let enabled = SwitchView(isOn: state.enabled) { [weak self] on in self?.state.enabled = on }
        add(nil, FlagRow(label: text.webScheduleEnabled, control: enabled), advanced: true)
        let notify = SwitchView(isOn: state.notifyOnFailure) { [weak self] on in
            self?.state.notifyOnFailure = on
        }
        add(nil, FlagRow(label: text.webScheduleNotify, control: notify), advanced: true)

        catchField.stringValue = String(state.catchUpHours)
        catchField.font = Metric.labelFont
        catchField.frame = NSRect(x: 0, y: 0, width: 92, height: 24)
        add(text.webScheduleCatchUp, catchField, advanced: true)

        timeoutField.stringValue = String(state.timeoutMinutes)
        timeoutField.font = Metric.labelFont
        timeoutField.frame = NSRect(x: 0, y: 0, width: 92, height: 24)
        add(text.webScheduleTimeout, timeoutField, advanced: true)

        save.action = { [weak self] in self?.commit() }
        cancel.action = { [weak self] in self?.onCancel?() }
        delete?.action = { [weak self] in self?.onDelete?() }
        addSubview(save)
        addSubview(cancel)
        if let delete { addSubview(delete) }
    }

    private func add(_ label: String?, _ view: NSView, advanced: Bool = false) {
        let field = Field(label: label.map { makeLabel($0, Metric.hintFont, Metric.soft) },
                          view: view, advanced: advanced)
        if let text = field.label { addSubview(text) }
        addSubview(view)
        fields.append(field)
    }

    private func instructionsBox(width: CGFloat) -> NSView {
        let scroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: width, height: 96))
        scroll.borderType = .bezelBorder
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        let size = scroll.contentSize
        instructions.frame = NSRect(x: 0, y: 0, width: size.width, height: size.height)
        instructions.minSize = NSSize(width: 0, height: size.height)
        instructions.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude,
                                      height: CGFloat.greatestFiniteMagnitude)
        instructions.isVerticallyResizable = true
        instructions.isHorizontallyResizable = false
        instructions.autoresizingMask = [.width]
        instructions.textContainer?.containerSize = NSSize(width: size.width,
                                                           height: CGFloat.greatestFiniteMagnitude)
        instructions.textContainer?.widthTracksTextView = true
        instructions.font = Metric.labelFont
        instructions.isRichText = false
        instructions.allowsUndo = true
        instructions.insertionPointColor = Style.accent
        instructions.string = state.instructions
        scroll.documentView = instructions
        return scroll
    }

    private static func dayNames(_ text: Copy) -> [String] {
        [text.webScheduleSun, text.webScheduleMon, text.webScheduleTue, text.webScheduleWed,
         text.webScheduleThu, text.webScheduleFri, text.webScheduleSat]
    }

    // MARK: - What the controls do

    /// Index 0 is Daily; 1 through 7 are the weekdays in ``ScheduleFormState/dayCodes`` order.
    private func pickDay(_ index: Int) {
        if index == 0 {
            state.daily = true
            state.weekdays = []
        } else {
            state.toggle(day: ScheduleFormState.dayCodes[index - 1])
        }
        paintDays()
    }

    private func paintDays() {
        for (index, chip) in dayChips.enumerated() {
            chip.armed = index == 0
                ? state.daily
                : (!state.daily && state.weekdays.contains(ScheduleFormState.dayCodes[index - 1]))
        }
    }

    private func toggleMore() {
        expanded.toggle()
        more.armed = expanded
        needsLayout = true
        onResize?()
    }

    /// Everything the controls hold that is not read as it is typed. The picker is asked here
    /// rather than on every turn of its stepper, and the two number boxes get their fallback here
    /// rather than leaving a half-typed field to be sent as it stands.
    private func gather() -> ScheduleFormState {
        var out = state
        out.title = titleField.stringValue.trimmingCharacters(in: .whitespaces)
        out.at = ScheduleFormState.time(from: timePicker.dateValue)
        out.instructions = instructions.string
        out.catchUpHours = ScheduleFormState.number(catchField.stringValue, atLeast: 0,
                                                    or: ScheduleFormState.catchUpDefault)
        out.timeoutMinutes = ScheduleFormState.number(timeoutField.stringValue, atLeast: 1,
                                                      or: ScheduleFormState.timeoutDefault)
        return out
    }

    private func commit() {
        guard !busy else { return }
        // The field editor keeps what is being typed to itself until it gives up first responder,
        // so a save pressed straight after a keystroke would send the value from before it.
        window?.makeFirstResponder(nil)
        state = gather()
        // Written back so the boxes show what is about to be sent — a `catch_up_hours` box left
        // holding "seven" must not go on saying that after the six behind it has been used.
        catchField.stringValue = String(state.catchUpHours)
        timeoutField.stringValue = String(state.timeoutMinutes)
        onSave?(state)
    }

    override func cancelOperation(_ sender: Any?) {
        guard !busy else { return }
        onCancel?()
    }

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        guard !busy else { return super.performKeyEquivalent(with: event) }
        // Escape here rather than only in `cancelOperation`: with the caret in the first message,
        // the field editor takes Escape for itself and the sheet would be one that cannot be
        // closed from the keyboard while somebody is typing in it.
        if event.keyCode == 53 {
            onCancel?()
            return true
        }
        // Command-Return saves from anywhere in the sheet, including from inside the first
        // message — where a bare Return is a new line and has to stay one.
        guard event.modifierFlags.contains(.command), event.keyCode == 36 else {
            return super.performKeyEquivalent(with: event)
        }
        commit()
        return true
    }

    // MARK: - Laying it out

    private func rebuildMessage() {
        messageLabel?.removeFromSuperview()
        messageLabel = nil
        guard let message, !message.isEmpty else { return }
        let label = makeLabel(message, Metric.hintFont, Style.accent)
        addSubview(label)
        messageLabel = label
        needsLayout = true
    }

    private func height(of field: Field, width: CGFloat) -> CGFloat {
        var height: CGFloat = 0
        if let label = field.label {
            height += labelHeight(label.stringValue, Metric.hintFont, width: width) + 5
        }
        if let sizing = field.view as? SelfSizing {
            height += sizing.height(forWidth: width)
        } else {
            height += field.view.frame.height
        }
        return height
    }

    /// How tall the sheet has to be for what is currently showing. One walk with the placing
    /// switched off, for the same reason ``ChipWrap`` has one.
    var contentHeight: CGFloat {
        walk(place: false)
    }

    override func layout() {
        super.layout()
        _ = walk(place: true)
    }

    @discardableResult
    private func walk(place: Bool) -> CGFloat {
        let width = Self.contentWidth - Self.inset * 2
        var y: CGFloat = Self.inset

        let headHeight = labelHeight(heading.stringValue, Metric.tabFont, width: width)
        if place {
            heading.preferredMaxLayoutWidth = width
            heading.frame = NSRect(x: Self.inset, y: y, width: width, height: headHeight)
        }
        y += headHeight + 16

        for field in fields {
            let hidden = field.advanced && !expanded
            if place {
                field.label?.isHidden = hidden
                field.view.isHidden = hidden
            }
            guard !hidden else { continue }
            if let label = field.label {
                let height = labelHeight(label.stringValue, Metric.hintFont, width: width)
                if place {
                    label.preferredMaxLayoutWidth = width
                    label.frame = NSRect(x: Self.inset, y: y, width: width, height: height)
                }
                y += height + 5
            }
            let viewHeight = (field.view as? SelfSizing)?.height(forWidth: width)
                ?? field.view.frame.height
            if place {
                // Popups and the two number boxes keep the width they sized themselves to; only
                // the things that fill the sheet are stretched to it.
                let stretch = field.view is ChipWrap || field.view is FlagRow
                    || field.view is NSScrollView || field.view === titleField
                field.view.frame = NSRect(x: Self.inset, y: y,
                                          width: stretch ? width : field.view.frame.width,
                                          height: viewHeight)
            }
            y += viewHeight + 14
        }

        if let label = messageLabel {
            let height = labelHeight(label.stringValue, Metric.hintFont, width: width)
            if place {
                label.preferredMaxLayoutWidth = width
                label.frame = NSRect(x: Self.inset, y: y, width: width, height: height)
            }
            y += height + 12
        }

        if place {
            // Delete on the left, away from the pair that ends the sheet ordinarily. It is the
            // one press here with nothing behind it — see the alert it opens — and putting it
            // next to Save is how somebody means to press Save and does not.
            delete?.frame.origin = NSPoint(x: Self.inset, y: y)
            save.frame.origin = NSPoint(x: Self.contentWidth - Self.inset - save.frame.width, y: y)
            cancel.frame.origin = NSPoint(
                x: Self.contentWidth - Self.inset - save.frame.width - 8 - cancel.frame.width, y: y)
        }
        return (y + 26 + Self.inset).rounded()
    }

    /// Where the keyboard should be when the sheet opens: the first thing somebody has to answer.
    var firstField: NSView { titleField }
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
    /// Greyed and unpressable while something it started is still in flight. Only the schedule
    /// form uses it — everything else in this window applies the moment it is touched, so there
    /// is no interval in which a second press would mean anything.
    var isEnabled = true {
        didSet {
            guard isEnabled != oldValue else { return }
            if !isEnabled { hovering = false; pressed = false }
            needsDisplay = true
        }
    }
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

    override func mouseEntered(with event: NSEvent) {
        guard isEnabled else { return }
        hovering = true
        needsDisplay = true
    }
    override func mouseExited(with event: NSEvent) { hovering = false; needsDisplay = true }

    override func mouseDown(with event: NSEvent) {
        guard isEnabled else { return }
        pressed = true
        needsDisplay = true
    }

    override func mouseUp(with event: NSEvent) {
        pressed = false
        needsDisplay = true
        guard isEnabled, bounds.contains(convert(event.locationInWindow, from: nil)) else { return }
        window?.makeFirstResponder(self)
        action?()
    }

    override func keyDown(with event: NSEvent) {
        if isEnabled, event.keyCode == 49 || event.keyCode == 36 {
            action?()
        } else {
            super.keyDown(with: event)
        }
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
            .font: font,
            .foregroundColor: (lit ? Style.accent : Metric.soft)
                .withAlphaComponent(isEnabled ? 1 : 0.4),
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

/// The Mac's Cloud identity and the explicit GitHub device-code handoff.
///
/// The state machine lives in ``CloudSettingsModel`` so none of the security or cancellation
/// rules depend on a window being present. This view only translates those states into one
/// selectable reading and the actions that are valid at that moment.
private final class CloudSettingsControl: NSView, SelfSizing {

    var onResize: (() -> Void)?

    private let model: CloudSettingsModel
    private let card = NoteCard()
    private var buttons: [ChipButton] = []

    init(model: CloudSettingsModel) {
        self.model = model
        super.init(frame: NSRect(x: 0, y: 0, width: 500, height: 80))
        addSubview(card)
        model.onChange = { [weak self] in self?.refresh() }
        refresh(notifyResize: false)
    }
    required init?(coder: NSCoder) { fatalError() }

    func height(forWidth width: CGFloat) -> CGFloat {
        card.height(forWidth: width) + (buttons.isEmpty ? 0 : 40)
    }

    override func layout() {
        super.layout()
        let cardHeight = card.height(forWidth: bounds.width)
        card.frame = NSRect(x: 0, y: 0, width: bounds.width, height: cardHeight)
        var x: CGFloat = 0
        for button in buttons {
            button.frame.origin = NSPoint(x: x, y: cardHeight + 10)
            x += button.frame.width + 8
        }
    }

    private func refresh(notifyResize: Bool = true) {
        let text: String
        let dot: PixelDot.State
        let actions: [(String, Bool, () -> Void)]

        switch model.phase {
        case .signedOut:
            text = "This Mac is not connected to Clawdline Cloud."
            dot = .idle
            actions = [("Connect with GitHub", true, { [weak model] in model?.connect() })]
        case .starting:
            text = "Starting a secure GitHub connection…"
            dot = .busy
            actions = [("Cancel", false, { [weak model] in model?.cancel() })]
        case .code(let userCode):
            text = "One-time GitHub code: \(userCode)\nConfirm this code before opening GitHub."
            dot = .live
            actions = [
                ("Confirm & Open GitHub", true, { [weak model] in model?.confirmAndOpen() }),
                ("Cancel", false, { [weak model] in model?.cancel() }),
            ]
        case .waiting(let userCode):
            text = "Waiting for GitHub authorization for code \(userCode)…"
            dot = .busy
            actions = [("Cancel", false, { [weak model] in model?.cancel() })]
        case .slowDown(let userCode, let retryAfter):
            text = "GitHub asked Clawdline to slow down. Code \(userCode) will retry in \(retryAfter)s."
            dot = .busy
            actions = [("Cancel", false, { [weak model] in model?.cancel() })]
        case .connected(let identity, let origin):
            let restored = origin == .restored ? " Restored from this Mac's Keychain." : ""
            text = "Connected to Clawdline Cloud. Account \(identity.accountID), Mac \(identity.machineID).\(restored)"
            dot = .live
            actions = [("Sign Out", false, { [weak model] in model?.signOut() })]
        case .denied:
            text = "GitHub connection was denied. No Cloud identity was added."
            dot = .warn
            actions = [("Retry", true, { [weak model] in model?.retry() })]
        case .expired:
            text = "The one-time GitHub code expired."
            dot = .warn
            actions = [("Retry", true, { [weak model] in model?.retry() })]
        case .cancelled:
            text = "GitHub connection was cancelled."
            dot = .idle
            actions = [("Retry", true, { [weak model] in model?.retry() })]
        case .failed(let message):
            text = "GitHub connection failed: \(message)"
            dot = .warn
            actions = [("Retry", true, { [weak model] in model?.retry() })]
        case .signOutFailed(let identity, let message):
            text = "Still connected as account \(identity.accountID), Mac \(identity.machineID). Sign out failed: \(message)"
            dot = .warn
            actions = [("Retry Sign Out", false, { [weak model] in model?.signOut() })]
        }

        card.text = text
        card.dot = dot
        buttons.forEach { $0.removeFromSuperview() }
        buttons = actions.map { title, prominent, action in
            let button = ChipButton(title: title, prominent: prominent)
            button.action = action
            addSubview(button)
            return button
        }
        needsLayout = true
        if notifyResize { onResize?() }
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
