import AppKit

final class PromptController: NSObject, NSWindowDelegate, NSTextViewDelegate {
    static let shared = PromptController()

    private var panel: PromptPanel!
    private var container: DropTargetView!         // the animation scales this layer: mascot and card together
    private var cardHost: NSView!          // exists only to cast the shadow (the card clips its corners, and clipping kills a shadow)
    private var card: NSVisualEffectView!
    /// A dark layer between the frosted material and everything drawn on it, so what is behind
    /// the window stops deciding what colour the card is.
    private var scrim: NSView!
    private var chrome: CardChrome!
    private var mascot: MascotView!
    private var glow: GlowView!
    private var chevron: NSTextField!
    private var micButton: NSButton!
    private let voice = Voice()
    /// What was in the box when dictation started. Each partial result replaces the dictated
    /// tail rather than being appended, or "hello" becomes "hellohello world".
    private var voiceBaseText = ""
    private var scroll: NSScrollView!
    private var textView: PromptTextView!
    private var hintLine: NSView!
    private var listTopLine: NSView!
    private var listBox: NSView!
    private var outputHost: NSScrollView!
    private var outputView: NSTextView!
    private var outputLine: NSView!
    /// What the session is doing right now. Hidden when it is not doing anything.
    private var activityLabel: NSTextField!
    /// The heading's own ground, so it reads as the top of the pane rather than as the bottom
    /// of the input row — which is what it looked like sitting on the card's own colour.
    private var paneHeader: NSView!
    private var targetLabel: NSTextView!
    /// The project's pixel mark, drawn to the left of its name.
    private var iconView: ProjectIconView!
    private var hints: KeyHintsView!
    private var hintsAll: [KeyHintsView.Hint] = []

    private var targets: [TargetSession] = []
    private var rows: [TargetRow] = []
    private var targetIndex = 0
    /// Which list the panel is showing, if any. Sessions and mascots share the UI.
    private enum ListMode { case none, sessions, mascots }
    private var listMode: ListMode = .none
    private var mascotNames: [String] = []
    private var mascotIndex = 0
    private var scanning = false

    /// The target the user picked by hand, plus which session iTerm was on at that moment.
    /// Why the second one: an explicit pick should not be overwritten by "where iTerm is now",
    /// but once the user actually moves to a different Claude tab, it should follow them there.
    private var stickyID: String?
    private var stickyBase: String?
    private var lastKnownCurrentID: String?
    /// Which repository each session is in, by session id. Cleared on every summon: the branch
    /// and the count of uncommitted files both move while you work.
    private var projectCache: [String: ProjectInfo] = [:]
    private var iconCache: [String: ProjectIcon.Grid] = [:]
    private var statusCache: [String: ProjectStatus.Snapshot] = [:]
    /// When each session was last looked up, so a summon repaints from what is known and asks
    /// again in the background rather than showing nothing while it waits.
    private var projectSeen: [String: CFAbsoluteTime] = [:]

    /// A full-screen blur behind everything, shown only while the output pane is open.
    /// Reading a transcript is a different mode from firing off one line, and the rest of
    /// the screen should stop competing for attention while you are in it.
    private var backdrop: NSPanel?
    private var previousApp: NSRunningApplication?
    private var shownAt = Date.distantPast
    private var dismissing = false
    private var showToken = 0
    private var historyCursor = -1
    private var hintResetWork: DispatchWorkItem?
    private var idleWork: DispatchWorkItem?
    private var outputOpen = false
    /// The keycap row costs most of the footer's width to say things you learn once. Collapsed
    /// to a single ⌘/ until asked for.
    private var keysShown = false
    /// Set when the panel goes away because the user switched apps rather than dismissed it.
    /// Only what was hidden this way comes back — Esc and sending mean closed, and something
    /// you shut on purpose reappearing on its own is the app arguing with you.
    private var hiddenByAppSwitch = false
    /// Filling the screen is a size, not macOS's fullscreen: the native one moves the window to
    /// its own Space, which for a panel you summon over whatever you were doing is the opposite
    /// of what it is for. This just makes the frame the size of the screen.
    private var fullscreen = false
    /// Owns the window frame while a size change is walking to its target.
    private var resizeTimer: Timer?
    /// The pane height the last layout actually used, which is where an animation starts from.
    private var lastOutputH: CGFloat = 0
    private var outputTimer: Timer?
    private var lastOutput: String?
    /// Set while the pane is showing a transcript from a file for a screenshot. The refresh
    /// loop stands down: it runs a beat later than the fill and would otherwise put the live
    /// session back, which looks like the file never loaded.
    private var cannedTranscript: String?
    /// Set when the pane comes back on screen: wherever the reader had scrolled to belongs to
    /// the last time they looked, and what they want now is what has happened since.
    private var jumpToNewestOnFill = false
    /// While set, the target label keeps its stand-in. The session scan lands a beat after the
    /// fill and would otherwise write a real tab title into a picture bound for the README.
    private var usingStandInLabel = false
    /// Which folded runs of tool calls the reader has opened. Cleared when the pane closes:
    /// it is a reading position, not a setting, and it should not outlive the session on screen.
    private var expandedFolds: Set<String> = []
    private var danceWork: DispatchWorkItem?

    private var currentTarget: TargetSession? {
        guard targets.indices.contains(targetIndex) else { return nil }
        return targets[targetIndex]
    }

    var isVisible: Bool { panel.isVisible && !dismissing }

    // MARK: - Construction

    private override init() {
        super.init()
        buildPanel()
    }

    private func buildPanel() {
        let W = Config.shared.width
        panel = PromptPanel(contentRect: NSRect(x: 0, y: 0, width: W, height: 140),
                            styleMask: [.borderless, .nonactivatingPanel],
                            backing: .buffered, defer: false)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        // The shadow is drawn on cardHost's layer. Left to the window, every frame of the zoom would recompute the outline and stutter.
        panel.hasShadow = false
        panel.level = .floating
        panel.hidesOnDeactivate = false
        panel.isMovableByWindowBackground = false
        panel.animationBehavior = .none
        // Follow the user across Spaces. Their windows are spread over several desktops; a prompt bar pinned to one is half-useless.
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]
        panel.delegate = self

        container = DropTargetView(frame: NSRect(x: 0, y: 0, width: W, height: 140))
        container.acceptDrops()
        container.wantsLayer = true
        container.layer?.masksToBounds = false
        container.autoresizesSubviews = false
        panel.contentView = container

        cardHost = NSView()
        cardHost.wantsLayer = true
        cardHost.autoresizesSubviews = false
        if let l = cardHost.layer {
            l.masksToBounds = false
            l.shadowColor = NSColor.black.cgColor
            l.shadowOpacity = 0.55
            l.shadowRadius = 30
            l.shadowOffset = CGSize(width: 0, height: -12)
        }
        container.addSubview(cardHost)

        card = NSVisualEffectView()
        card.material = .hudWindow
        card.blendingMode = .behindWindow
        card.state = .active
        card.wantsLayer = true
        card.layer?.cornerRadius = Style.corner
        card.layer?.masksToBounds = true
        card.autoresizesSubviews = false
        cardHost.addSubview(card)

        scrim = NSView()
        scrim.wantsLayer = true
        scrim.autoresizingMask = [.width, .height]
        card.addSubview(scrim)
        applyCardOpacity()

        chevron = NSTextField(labelWithString: "❯")
        chevron.font = NSFont.monospacedSystemFont(ofSize: 17, weight: .bold)
        chevron.textColor = Style.accent
        chevron.alignment = .center
        card.addSubview(chevron)

        micButton = NSButton()
        micButton.isBordered = false
        micButton.bezelStyle = .regularSquare
        micButton.imagePosition = .imageOnly
        micButton.image = NSImage(systemSymbolName: "mic", accessibilityDescription: L.t.hintVoice)
        micButton.contentTintColor = .tertiaryLabelColor
        micButton.toolTip = L.t.hintVoice
        micButton.target = self
        micButton.action = #selector(toggleVoice)
        card.addSubview(micButton)

        textView = PromptTextView()
        textView.minSize = NSSize(width: 0, height: 0)
        textView.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = false
        textView.autoresizingMask = [.width]
        textView.textContainer?.widthTracksTextView = true
        textView.textContainer?.containerSize = NSSize(width: 0, height: CGFloat.greatestFiniteMagnitude)
        textView.textContainer?.lineFragmentPadding = 0
        textView.textContainerInset = .zero
        textView.drawsBackground = false
        // Rich text so a dropped file can be a thumbnail rather than forty characters of path.
        // Typing attributes are pinned below, so what you type still looks like what you typed.
        textView.isRichText = true
        textView.isAutomaticLinkDetectionEnabled = false
        textView.importsGraphics = false
        textView.allowsUndo = true
        textView.font = NSFont.systemFont(ofSize: Style.textSize, weight: .regular)
        textView.textColor = .labelColor
        // Pinned, because rich text otherwise lets a paste bring its own font in with it —
        // and because replacing the text at all would otherwise lose the look.
        textView.baseAttributes = [
            .font: NSFont.systemFont(ofSize: Style.textSize, weight: .regular),
            .foregroundColor: NSColor.labelColor,
        ]
        textView.insertionPointColor = Style.accent
        textView.isAutomaticQuoteSubstitutionEnabled = false
        textView.isAutomaticDashSubstitutionEnabled = false
        textView.isAutomaticTextReplacementEnabled = false

        scroll = NSScrollView()
        scroll.drawsBackground = false
        scroll.borderType = .noBorder
        scroll.hasVerticalScroller = false
        scroll.hasHorizontalScroller = false
        scroll.autohidesScrollers = true
        scroll.documentView = textView
        card.addSubview(scroll)

        listTopLine = line()
        listBox = NSView()
        listBox.autoresizesSubviews = false
        card.addSubview(listBox)
        card.addSubview(listTopLine)

        outputView = PromptController.makeOutputView()
        outputView.delegate = self

        // Sits at the top of the pane rather than in the text: it changes every second, and
        // rewriting the transcript that often would throw away the reader's scroll position.
        paneHeader = NSView()
        paneHeader.wantsLayer = true
        paneHeader.layer?.backgroundColor = Style.outputBg.cgColor
        paneHeader.isHidden = true
        card.addSubview(paneHeader)

        activityLabel = NSTextField(labelWithString: "")
        activityLabel.font = NSFont.monospacedSystemFont(ofSize: Style.hintSize, weight: .regular)
        activityLabel.textColor = Style.accent
        activityLabel.lineBreakMode = .byTruncatingTail
        activityLabel.isHidden = true
        card.addSubview(activityLabel)

        outputHost = NSScrollView()
        outputHost.drawsBackground = false
        outputHost.borderType = .noBorder
        outputHost.hasVerticalScroller = true
        outputHost.autohidesScrollers = true
        outputHost.documentView = outputView
        card.addSubview(outputHost)
        outputLine = line()
        card.addSubview(outputLine)

        hintLine = line()
        card.addSubview(hintLine)

        // A text view rather than a label: the deploy and backlog chips are links, and a label
        // cannot make part of itself clickable. Selectable is what makes a link take a click.
        targetLabel = NSTextView()
        targetLabel.isEditable = false
        targetLabel.isSelectable = true
        targetLabel.drawsBackground = false
        targetLabel.textContainerInset = .zero
        targetLabel.textContainer?.lineFragmentPadding = 0
        targetLabel.textContainer?.maximumNumberOfLines = 1
        targetLabel.textContainer?.lineBreakMode = .byTruncatingTail
        targetLabel.font = NSFont.systemFont(ofSize: Style.hintSize)
        targetLabel.linkTextAttributes = [.cursor: NSCursor.pointingHand]
        targetLabel.delegate = self
        card.addSubview(targetLabel)

        hintsAll = [
            .init(key: "⇥", label: L.t.hintSwitch),
            .init(key: "⌘K", label: L.t.hintList),
            .init(key: "⌘M", label: L.t.hintMascot),
            .init(key: "⌘J", label: L.t.hintOutput),
            .init(key: "⌘F", label: L.t.hintFullscreen),
            .init(key: "⌘R", label: L.t.hintOrder),
            .init(key: "⌘+", label: L.t.hintTextSize),
        ]

        iconView = ProjectIconView()
        card.addSubview(iconView)

        hints = KeyHintsView()
        hints.onClick = { [weak self] in self?.toggleKeys() }
        applyHints()
        card.addSubview(hints)

        chrome = CardChrome()
        card.addSubview(chrome)

        // The glow sits under the card, so the half that spills onto it is hidden — the light reads as coming from behind the mascot
        glow = GlowView()
        container.addSubview(glow, positioned: .below, relativeTo: cardHost)

        mascot = MascotView()
        container.addSubview(mascot)

        wireKeys()
    }

    private func line() -> NSView {
        let v = NSView()
        v.wantsLayer = true
        v.layer?.backgroundColor = Style.hairline.cgColor
        return v
    }

    private func wireKeys() {
        textView.onSubmit = { [weak self] in self?.submit() }
        textView.onCancel = { [weak self] in
            guard let self else { return }
            if self.listMode != .none { self.listMode = .none; self.relayout() } else { self.hide() }
        }
        textView.onCycleTarget = { [weak self] forward in self?.cycle(forward: forward) }
        textView.onToggleList = { [weak self] in self?.showList(.sessions) }
        textView.onToggleMascots = { [weak self] in self?.showList(.mascots) }
        textView.onPickIndex = { [weak self] i in self?.pick(i) }
        textView.onToggleDance = { [weak self] in self?.toggleDance() }
        textView.onToggleOutput = { [weak self] in self?.toggleOutput() }
        textView.onToggleFullscreen = { [weak self] in self?.toggleFullscreen() }
        textView.onToggleOrder = { [weak self] in self?.toggleOutputOrder() }
        textView.onToggleKeys = { [weak self] in self?.toggleKeys() }
        textView.acceptDrops()
        container.onDrop = { [weak self] paths in self?.textView.insertPaths(paths) }
        container.onDragActive = { [weak self] on in self?.chrome?.highlighted = on }
        // The transcript takes drags by default and would swallow one over half the window,
        // trying to insert text into a view that is not editable.
        outputView.unregisterDraggedTypes()
        textView.onDropped = { [weak self] n in
            self?.setHint(L.t.dropped(n), warn: false)
            self?.relayout()
        }
        textView.onZoomOutput = { [weak self] step in self?.zoomOutput(step) }
        textView.onTextChanged = { [weak self] in
            self?.relayout()
            self?.noteTyping()
        }
        textView.onArrow = { [weak self] delta in self?.handleArrow(delta) ?? false }
    }

    // MARK: - Show and hide

    func toggle() {
        if isVisible { hide() } else { show() }
    }

    func show() {
        // Every show gets a new number. When a failed send brings the panel back, the previous
        // dismissal animation may only just be finishing — without this number it would close the
        // panel that was just reopened, and the user sees "my text vanished and the panel blinked".
        showToken &+= 1
        dismissing = false
        previousApp = NSWorkspace.shared.frontmostApplication
        historyCursor = -1
        listMode = .none
        resetHint()
        reloadMascot()
        // Summoning aims at the tab you are looking at. A pick made with Tab is an override for
        // as long as the panel is up, not a new home: coming back to a different tab and finding
        // the text still pointed at the old one is how a message lands in the wrong session.
        stickyID = nil
        stickyBase = nil
        refreshTargets()
        relayout()
        position()
        shownAt = Date()

        // Both of these only ever live for the length of one debug snapshot. Left set, they
        // switch the refresh loop off and pin a stand-in label — for the rest of the process.
        cannedTranscript = nil
        usingStandInLabel = false
        // The pane keeps its state across a hide, so its refresh loop has to be picked back up
        // here. Without this the first Esc freezes it until you press ⌘J twice.
        if outputOpen {
            jumpToNewestOnFill = true
            startOutput()
            // Torn down by hide() along with everything else. Same shape of bug as the refresh
            // loop: the pane came back and the blur behind it did not, so the screen behind
            // stayed sharp and the pane looked like it was floating on nothing.
            showBackdrop()
        }

        panel.alphaValue = 0
        NSApp.activate(ignoringOtherApps: true)
        panel.makeKeyAndOrderFront(nil)
        panel.makeFirstResponder(textView)
        animateIn()
        Log.write("show: frame=\(panel.frame) prev=\(previousApp?.localizedName ?? "-")")
    }

    /// The terminal came forward again.
    ///
    /// Leaving a panel you had open is "I need to see something for a moment", not "I am done" —
    /// Esc is how you say the second one. So what a switch put away, coming back takes out again,
    /// at whatever size it was.
    func appBecameFrontmost(_ bundleID: String?) {
        guard Config.shared.reopenOnReturn else { return }
        let scope = Config.shared.scopeApp
        let isTerminal = !scope.isEmpty && bundleID.map { scope.contains($0) } == true
        guard isTerminal, hiddenByAppSwitch, !panel.isVisible else { return }
        hiddenByAppSwitch = false
        // Not on this turn of the loop. The notification arrives while macOS is still raising
        // the terminal's windows, and showing here calls NSApp.activate in the middle of that:
        // the menu bar says iTerm2 and the screen still shows whatever you were just in. Let the
        // switch finish, then check the terminal is still where you are before taking the front.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { [weak self] in
            guard let self, !self.panel.isVisible else { return }
            let front = NSWorkspace.shared.frontmostApplication?.bundleIdentifier
            guard front.map({ Config.shared.scopeApp.contains($0) }) == true else { return }
            self.show()
        }
    }

    /// `returnFocus` puts the app you came from back in front. That is right when you dismissed
    /// the panel — you were in the terminal, you are done here, go back. It is wrong when the
    /// panel is closing *because* you went somewhere else: doing it then drags you back out of
    /// the app you just switched to, and if the switch also armed the full-screen return, that
    /// yank lands on the terminal and opens the panel again. You could not leave.
    func hide(returnFocus: Bool = true) {
        guard panel.isVisible, !dismissing else { return }
        // Returning focus is what every deliberate dismissal does — Esc, sending, the hotkey —
        // and a deliberate dismissal means closed. Only the app-switch path (returnFocus: false)
        // leaves the return armed.
        //
        // Clearing it here rather than when the panel comes back: a return that gets skipped —
        // the commonest being coming back while the last dismissal is still animating out —
        // used to leave the flag set for the rest of the session, and the next time you closed
        // the panel by hand it would let itself back in.
        if returnFocus { hiddenByAppSwitch = false }
        dismissing = true
        voice.stop()        // a microphone left open behind a hidden window is not acceptable
        resizeTimer?.invalidate()
        resizeTimer = nil
        hideBackdrop()
        idleWork?.cancel()
        danceWork?.cancel()
        let token = showToken
        animateOut { [weak self] in
            guard let self, token == self.showToken else { return }
            self.panel.orderOut(nil)
            self.mascot.stop()          // once hidden, stop burning a 60fps timer
            self.stopOutput()
            self.hideBackdrop(animated: false)
            self.listMode = .none
            self.dismissing = false
            if returnFocus, let prev = self.previousApp,
               prev.processIdentifier != NSRunningApplication.current.processIdentifier {
                prev.activate()
            }
        }
    }

    func windowDidResignKey(_ notification: Notification) {
        guard panel.isVisible, !dismissing else { return }
        // In the instant after it opens, the previous app may still be grabbing focus back.
        // Without this grace period the panel closes before you ever see it.
        if Date().timeIntervalSince(shownAt) < 0.4 {
            NSApp.activate(ignoringOtherApps: true)
            panel.makeKeyAndOrderFront(nil)
            panel.makeFirstResponder(textView)
            return
        }
        // Losing focus is the app-switch path; Esc and sending come through hide() directly and
        // must not arm the return, or dismissing it would only postpone it.
        hiddenByAppSwitch = true
        // Whoever took focus keeps it. This is the path where the user chose to be elsewhere.
        hide(returnFocus: false)
    }

    private func position() {
        guard resizeTimer == nil else { return }   // the animation owns the frame while it runs
        panel.setFrameOrigin(originFor(width: panelWidth, total: panel.frame.height))
    }

    /// Where a window of this size belongs. Split out from `position()` so a size change can
    /// walk the origin at the same rate as the size — moving one without the other is what
    /// makes a resize look like two separate things happening.
    private func originFor(width W: CGFloat, total: CGFloat) -> NSPoint {
        let screen = screenUnderMouse()
        if fullscreen {
            let v = screen.visibleFrame
            return NSPoint(x: round(v.minX), y: round(v.minY))
        }
        let f = screen.frame
        let x = f.midX - W / 2
        // y_fraction refers to the top of the *card*, not the top of the window, so changing the
        // mascot's height never pushes the input line somewhere else.
        // With the pane open the card is nearly twice as tall; leaving the top pinned would
        // hang all of that below the eye line. Lift it by part of what it grew.
        let lift = outputOpen ? Style.outputHeight * 0.34 : 0
        let cardTop = f.maxY - f.height * Config.shared.yFraction + lift
        return NSPoint(x: round(x), y: round(cardTop + mascotHeadroom - total))
    }

    private func screenUnderMouse() -> NSScreen {
        let p = NSEvent.mouseLocation
        return NSScreen.screens.first { NSMouseInRect(p, $0.frame, false) } ?? NSScreen.main ?? NSScreen.screens[0]
    }

    // MARK: - Animation

    private static func transform(scale s: CGFloat, dy: CGFloat, size: CGSize) -> CATransform3D {
        var t = CATransform3DIdentity
        t = CATransform3DTranslate(t, size.width / 2, size.height / 2 + dy, 0)
        t = CATransform3DScale(t, s, s, 1)
        t = CATransform3DTranslate(t, -size.width / 2, -size.height / 2, 0)
        return t
    }

    private func animateIn() {
        guard let layer = container.layer else { return }
        layer.removeAllAnimations()
        let size = container.bounds.size

        let zoom = CABasicAnimation(keyPath: "transform")
        zoom.fromValue = NSValue(caTransform3D: Self.transform(scale: 0.90, dy: -12, size: size))
        zoom.toValue = NSValue(caTransform3D: CATransform3DIdentity)
        zoom.duration = 0.30
        // Fast out, slow in: it bursts out at the start and almost stops at the end. That is the
        // feel these panels have now — neither linear nor a bouncing spring.
        zoom.timingFunction = CAMediaTimingFunction(controlPoints: 0.16, 1.0, 0.28, 1.0)
        layer.add(zoom, forKey: "zoomIn")
        layer.transform = CATransform3DIdentity

        NSAnimationContext.runAnimationGroup { ctx in
            ctx.duration = 0.18
            ctx.timingFunction = CAMediaTimingFunction(name: .easeOut)
            panel.animator().alphaValue = 1
        }

        mascot.play("pop")
        armDance(after: 6)
    }

    // MARK: - The mascot's state machine
    //
    // Each of the five routines has a real reason to fire, rather than motion for its own sake:
    // pop on entry, typing while you type, idle when you stop, dance when idle too long (or ⌘D), cheer on send.

    private func noteTyping() {
        if mascot.routine != "typing" { mascot.play("typing") }
        idleWork?.cancel()
        let w = DispatchWorkItem { [weak self] in
            guard let self, self.mascot.routine == "typing" else { return }
            self.mascot.play("idle")
        }
        idleWork = w
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2, execute: w)
        armDance(after: 7)
    }

    private func armDance(after seconds: Double) {
        danceWork?.cancel()
        let w = DispatchWorkItem { [weak self] in
            guard let self, self.panel.isVisible, self.mascot.routine == "idle" else { return }
            self.mascot.play("dance")
        }
        danceWork = w
        DispatchQueue.main.asyncAfter(deadline: .now() + seconds, execute: w)
    }

    private func toggleDance() {
        danceWork?.cancel()
        mascot.play(mascot.routine == "dance" ? "idle" : "dance")
        if mascot.routine == "idle" { armDance(after: 7) }
    }

    private func animateOut(completion: @escaping () -> Void) {
        guard let layer = container.layer else { completion(); return }
        let size = container.bounds.size

        let zoom = CABasicAnimation(keyPath: "transform")
        zoom.fromValue = NSValue(caTransform3D: CATransform3DIdentity)
        zoom.toValue = NSValue(caTransform3D: Self.transform(scale: 0.955, dy: -6, size: size))
        // Closing runs at half the speed of opening — something being dismissed does not need admiring.
        zoom.duration = 0.15
        zoom.timingFunction = CAMediaTimingFunction(controlPoints: 0.4, 0, 1, 1)
        zoom.fillMode = .forwards
        zoom.isRemovedOnCompletion = false
        layer.add(zoom, forKey: "zoomOut")

        NSAnimationContext.runAnimationGroup({ ctx in
            ctx.duration = 0.15
            ctx.timingFunction = CAMediaTimingFunction(name: .easeIn)
            panel.animator().alphaValue = 0
        }, completionHandler: {
            layer.removeAnimation(forKey: "zoomOut")
            layer.transform = CATransform3DIdentity
            completion()
        })
    }

    // MARK: - Layout

    private func textHeight() -> CGFloat {
        guard let lm = textView.layoutManager, let tc = textView.textContainer else { return 26 }
        lm.ensureLayout(for: tc)
        let used = ceil(lm.usedRect(for: tc).height)
        let oneLine = ceil((textView.font?.ascender ?? 16) - (textView.font?.descender ?? -5) + 2)
        return min(Style.maxTextHeight, max(oneLine, used))
    }

    /// Terminal lines are long, and 720pt wraps most of them. Widen while the pane is open,
    /// then hand the width back so the bar is a bar again when it closes.
    private var panelWidth: CGFloat {
        if fullscreen { return screenUnderMouse().visibleFrame.width }
        guard outputOpen else { return Config.shared.width }
        return min(Config.shared.width * 1.45, screenUnderMouse().frame.width * 0.88).rounded()
    }

    /// What a layout comes out to, for a given outer width and pane height.
    ///
    /// Separate from applying it so the resize animation can walk the two values it changes —
    /// the width and the pane — and get every frame in between for free.
    private func geometry(width W: CGFloat, outputH: CGFloat)
        -> (inputH: CGFloat, listH: CGFloat, cardH: CGFloat, total: CGFloat) {
        let inputH = max(Style.inputMinHeight, textHeight() + Style.inputPadV * 2)
        let visibleRows = listMode != .none ? min(rows.count, 9) : 0
        let listH = visibleRows > 0 ? CGFloat(visibleRows) * Style.rowHeight + Style.listPadV * 2 : 0
        let fixed = inputH + (listH > 0 ? listH + 1 : 0) + 1 + Style.hintHeight
        let cardH = fixed + (outputH > 0 ? outputH + 1 : 0)
        // The mascot stands on the top edge of the card, so its room comes off the top at every
        // size — full screen included. The card takes what is left rather than the whole screen.
        return (inputH, listH, cardH, cardH + mascotHeadroom)
    }

    private var mascotHeadroom: CGFloat {
        (mascot.boxSize.height - mascot.footInset - mascot.overlap) + Style.mascotTopPad
    }

    /// The pane's height for the current state. Full screen gives it whatever is left rather
    /// than growing everything: the input line and the footer are the same size at any size.
    private var targetOutputHeight: CGFloat {
        guard outputOpen else { return 0 }
        guard fullscreen else { return Style.outputHeight }
        let inputH = max(Style.inputMinHeight, textHeight() + Style.inputPadV * 2)
        let visibleRows = listMode != .none ? min(rows.count, 9) : 0
        let listH = visibleRows > 0 ? CGFloat(visibleRows) * Style.rowHeight + Style.listPadV * 2 : 0
        let fixed = inputH + (listH > 0 ? listH + 1 : 0) + 1 + Style.hintHeight
        return max(80, screenUnderMouse().visibleFrame.height - mascotHeadroom - fixed - 1)
    }

    private func relayout() {
        guard resizeTimer == nil else { return }   // the animation owns the frame while it runs
        let W = panelWidth
        let outputH = targetOutputHeight
        let g = geometry(width: W, outputH: outputH)

        var f = panel.frame
        let top = f.maxY
        f.size = NSSize(width: W, height: g.total)
        f.origin.y = top - g.total
        panel.setFrame(f, display: true)
        applyLayout(width: W, outputH: outputH, geometry: g)
    }

    /// Everything inside the window, for an already-decided outer size.
    private func applyLayout(width W: CGFloat, outputH: CGFloat,
                             geometry g: (inputH: CGFloat, listH: CGFloat,
                                          cardH: CGFloat, total: CGFloat)) {
        container.frame = NSRect(origin: .zero, size: NSSize(width: W, height: g.total))
        cardHost.frame = NSRect(x: 0, y: 0, width: W, height: g.cardH)
        cardHost.layer?.shadowPath = CGPath(roundedRect: cardHost.bounds,
                                            cornerWidth: Style.corner, cornerHeight: Style.corner,
                                            transform: nil)
        card.frame = cardHost.bounds
        let box = mascot.boxSize
        mascot.frame = NSRect(x: (W - box.width) / 2,
                              y: g.cardH - mascot.overlap - mascot.footInset,
                              width: box.width, height: box.height)
        // The glow lines up with the sprite, not the view — the view is larger (that is the jump room)
        let sr = mascot.spriteRect
        let side = sr.width + Style.glowInset * 2
        glow.frame = NSRect(x: mascot.frame.minX + sr.midX - side / 2,
                            y: mascot.frame.minY + sr.midY - side / 2,
                            width: side, height: side)

        layoutCard(size: card.bounds.size, inputH: g.inputH, listH: g.listH, outputH: outputH)
        lastOutputH = outputH
    }

    func applyCardOpacity() {
        scrim?.layer?.backgroundColor = Style.ink
            .withAlphaComponent(CGFloat(Config.shared.cardOpacity)).cgColor
    }

    private func layoutCard(size: NSSize, inputH: CGFloat, listH: CGFloat, outputH: CGFloat) {
        let W = size.width, H = size.height
        scrim.frame = NSRect(origin: .zero, size: size)
        chrome.frame = NSRect(origin: .zero, size: size)

        chevron.frame = NSRect(x: Style.padH, y: H - Style.inputPadV - 25, width: Style.chevronW, height: 24)

        let tx = Style.padH + Style.chevronW + Style.chevronGap
        let micW: CGFloat = 26
        micButton.frame = NSRect(x: W - Style.padH - micW, y: H - Style.inputPadV - 26,
                                 width: micW, height: 26)
        scroll.frame = NSRect(x: tx, y: H - inputH + Style.inputPadV,
                              width: W - tx - Style.padH - micW - 6,
                              height: inputH - Style.inputPadV * 2)

        hintLine.frame = NSRect(x: 0, y: Style.hintHeight, width: W, height: 1)

        let hintsW = min(hints.intrinsicWidth, W - Style.padH * 2 - 120)
        let hintsX = W - Style.padH - hintsW
        hints.frame = NSRect(x: hintsX, y: 0, width: hintsW, height: Style.hintHeight)

        let iconH: CGFloat = 14
        let iconW = ProjectIconView.width(for: iconView.grid, height: iconH)
        iconView.isHidden = iconW == 0
        iconView.frame = NSRect(x: Style.padH, y: (Style.hintHeight - iconH) / 2,
                                width: iconW, height: iconH)
        let labelX = Style.padH + (iconW > 0 ? iconW + 7 : 0)
        targetLabel.frame = NSRect(x: labelX, y: (Style.hintHeight - 15) / 2,
                                   width: max(60, hintsX - labelX - 16), height: 15)

        // Stacked upward from the hint row, so the footer stays the footer.
        var y = Style.hintHeight + 1

        outputHost.isHidden = outputH == 0
        outputLine.isHidden = outputH == 0
        activityLabel.isHidden = outputH == 0 || activityLabel.stringValue.isEmpty
        paneHeader.isHidden = activityLabel.isHidden
        if outputH > 0 {
            // Sized to the text, not to the footer's row height. Borrowing that 38pt put the
            // label in the middle of a box twice its height, and the leftover showed up as a
            // hole between this line and the first line of the transcript.
            let labelH: CGFloat = 18
            let strip: CGFloat = activityLabel.isHidden ? 0 : labelH + 18
            outputHost.frame = NSRect(x: 0, y: y, width: W, height: outputH - strip)
            if strip > 0 {
                let headerY = y + outputH - strip
                paneHeader.frame = NSRect(x: 0, y: headerY, width: W, height: strip)
                // Air on both sides, and a little more below: this line is a header, and a
                // header that touches the first line of what it heads reads as part of it.
                activityLabel.frame = NSRect(x: Style.padH, y: headerY + 7,
                                             width: W - Style.padH * 2, height: labelH)
            }
            // The document view starts at zero width, and with widthTracksTextView that makes
            // the text container zero wide too — the text is there and simply has nowhere to
            // go. Give it the clip view's width by hand.
            let docWidth = outputHost.contentSize.width
            if abs(outputView.frame.width - docWidth) > 0.5 {
                outputView.setFrameSize(NSSize(width: docWidth,
                                               height: max(outputH, outputView.frame.height)))
            }
            y += outputH
            outputLine.frame = NSRect(x: 0, y: y, width: W, height: 1)
            y += 1
        }

        listBox.isHidden = listH == 0
        listTopLine.isHidden = listH == 0
        if listH > 0 {
            listBox.frame = NSRect(x: 0, y: y, width: W, height: listH)
            y += listH
            listTopLine.frame = NSRect(x: 0, y: y, width: W, height: 1)
            for (i, row) in rows.prefix(9).enumerated() {
                row.frame = NSRect(x: 0,
                                   y: listH - Style.listPadV - CGFloat(i + 1) * Style.rowHeight,
                                   width: W, height: Style.rowHeight)
            }
        }
    }

    // MARK: - Backdrop

    private func makeBackdrop() -> NSPanel {
        let p = NSPanel(contentRect: .zero,
                        styleMask: [.borderless, .nonactivatingPanel],
                        backing: .buffered, defer: false)
        p.isOpaque = false
        p.backgroundColor = .clear
        p.hasShadow = false
        // One step below the bar, so it covers the screen without covering the bar.
        p.level = NSWindow.Level(rawValue: panel.level.rawValue - 1)
        p.ignoresMouseEvents = true
        p.hidesOnDeactivate = false
        p.animationBehavior = .none
        p.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]

        let blur = NSVisualEffectView()
        // hudWindow rather than fullScreenUI: the heavier material erased everything behind
        // it, and the point is to push the background back, not to delete it.
        blur.material = .hudWindow
        blur.blendingMode = .behindWindow
        blur.state = .active
        blur.autoresizingMask = [.width, .height]

        let tint = NSView()
        tint.wantsLayer = true
        tint.layer?.backgroundColor = NSColor(white: 0, alpha: 0.14).cgColor
        tint.autoresizingMask = [.width, .height]
        blur.addSubview(tint)

        p.contentView = blur
        return p
    }

    private func showBackdrop() {
        // A blur at less than full opacity composites over the sharp original, which reads as
        // softened rather than blanked out. That is the knob, not the material.
        let strength = Config.shared.backdropStrength
        guard strength > 0.01 else { return }
        let p = backdrop ?? makeBackdrop()
        backdrop = p
        p.setFrame(screenUnderMouse().frame, display: false)
        p.contentView?.frame = NSRect(origin: .zero, size: p.frame.size)
        p.contentView?.subviews.forEach { $0.frame = p.contentView!.bounds }
        p.alphaValue = 0
        p.order(.below, relativeTo: panel.windowNumber)
        NSAnimationContext.runAnimationGroup { ctx in
            ctx.duration = 0.22
            ctx.timingFunction = CAMediaTimingFunction(name: .easeOut)
            p.animator().alphaValue = CGFloat(strength)
        }
    }

    private func hideBackdrop(animated: Bool = true) {
        guard let p = backdrop, p.isVisible else { return }
        guard animated else { p.orderOut(nil); return }
        NSAnimationContext.runAnimationGroup({ ctx in
            ctx.duration = 0.14
            ctx.timingFunction = CAMediaTimingFunction(name: .easeIn)
            p.animator().alphaValue = 0
        }, completionHandler: { p.orderOut(nil) })
    }

    // MARK: - Reading the session back
    //
    // The bar can already switch between sessions; being able to see what one of them says
    // is what makes that switch a decision rather than a guess.

    private func toggleOutput(layout: Bool = true) {
        let from = currentFrameState()
        // Full screen exists to read in. Closing the pane and leaving a screen-sized card with
        // one input line in it would be a state nobody asked for — so ⌘J on a full-screen pane
        // leaves both, and the two changes travel together rather than one snapping first.
        let leavingFullscreen = outputOpen && fullscreen
        outputOpen.toggle()
        lastOutput = nil
        if leavingFullscreen {
            fullscreen = false
            mascot.play(mascot.has("stretch") ? "stretch" : "cheer", then: "idle")
            animateLayout(from: from)
        } else if layout {
            relayout()
            position()      // width changed, so re-centre
        }
        if outputOpen { showBackdrop() } else { hideBackdrop() }
        if outputOpen {
            startOutput()
        } else {
            stopOutput()
            lastOutput = nil
            // Which runs were open is where the reader had got to, not a preference.
            expandedFolds.removeAll()
        }
    }

    /// ⌘F. Reading is the only reason to want the whole screen, so it brings the pane with it —
    /// a full-screen window with one input line in it would be a worse version of the bar.
    private func toggleFullscreen() {
        let from = currentFrameState()
        // Opened without its own layout pass: the resize below covers the same distance, and
        // doing both makes the pane pop to one size and then travel to another.
        if !fullscreen && !outputOpen { toggleOutput(layout: false) }
        fullscreen.toggle()
        mascot.play(mascot.has("stretch") ? "stretch" : "cheer", then: "idle")
        animateLayout(from: from)
    }

    private func currentFrameState() -> (w: CGFloat, output: CGFloat, origin: NSPoint) {
        (panel.frame.width, lastOutputH, panel.frame.origin)
    }

    /// Walk the window to whatever the state now says it should be.
    ///
    /// Every frame is laid out for real rather than the window being scaled: the input line and
    /// the footer are fixed-height at any size, so a scaled window would show them stretching
    /// and settling back, which is the thing that reads as cheap.
    private func animateLayout(from: (w: CGFloat, output: CGFloat, origin: NSPoint),
                               duration: Double = 0.30) {
        resizeTimer?.invalidate()
        let toW = panelWidth
        let toOutput = targetOutputHeight
        let toOrigin = originFor(width: toW, total: geometry(width: toW, outputH: toOutput).total)
        let start = CACurrentMediaTime()

        let tick: (Timer) -> Void = { [weak self] timer in
            // Clearing the handle matters as much as stopping the timer: relayout and position
            // stand down while it is set, so a timer that dies without clearing it freezes the
            // window's geometry for the rest of the session.
            guard let self else { timer.invalidate(); return }
            guard self.panel.isVisible else {
                timer.invalidate()
                self.resizeTimer = nil
                return
            }
            let raw = min(1, (CACurrentMediaTime() - start) / duration)
            // Ease out: a window that decelerates into its size feels like it arrived, and one
            // that stops dead feels like it was cut off.
            let e = CGFloat(1 - pow(1 - raw, 3))
            let w = from.w + (toW - from.w) * e
            let output = from.output + (toOutput - from.output) * e
            let g = self.geometry(width: w, outputH: output)
            let origin = NSPoint(x: from.origin.x + (toOrigin.x - from.origin.x) * e,
                                 y: from.origin.y + (toOrigin.y - from.origin.y) * e)
            if raw >= 1 {
                timer.invalidate()
                self.resizeTimer = nil
                self.relayout()      // land on the exact numbers, not on an interpolation
                self.position()
                return
            }
            self.panel.setFrame(NSRect(origin: origin,
                                       size: NSSize(width: w, height: g.total)), display: true)
            self.applyLayout(width: w, outputH: output, geometry: g)
        }

        let timer = Timer(timeInterval: 1.0 / 60.0, repeats: true, block: tick)
        RunLoop.main.add(timer, forMode: .common)
        resizeTimer = timer
    }

    /// ⌘+ / ⌘- / ⌘0. The size is persisted, because a size you have to set again every
    /// launch is not really a setting.
    private func zoomOutput(_ step: Int) {
        let before = Config.shared.outputSize
        let after: CGFloat = step == 0 ? 11.5 : min(28, max(8, before + CGFloat(step)))
        guard after != before else { return }
        Config.shared.outputSize = after
        Config.shared.save()
        outputView.font = Style.outputFont
        // ANSI runs carry their own font attribute, so the new size only reaches them by
        // rebuilding the text — which means asking for the capture again.
        lastOutput = nil
        if outputOpen { refreshOutput() } else { setHint(L.t.outputSize(Int(after)), warn: false) }
    }

    /// ⌘R. Persisted, because which way round a transcript reads is a preference and not a
    /// per-session mood. Only the transcript flips: a terminal capture is a picture of a grid,
    /// and reversing its lines would have a wrapped sentence reading upwards.
    private func toggleOutputOrder() {
        Config.shared.outputNewestFirst.toggle()
        Config.shared.save()
        lastOutput = nil
        setHint(L.t.outputOrder(newestFirst: Config.shared.outputNewestFirst), warn: false)
        if outputOpen {
            refreshOutput()
            // The reader asked for the newest end; put them at it rather than leaving them
            // wherever the old order had them scrolled to.
            DispatchQueue.main.async { self.scrollOutputToNewest() }
        }
    }

    /// The pane's refresh loop, with one owner.
    ///
    /// It is stopped every time the panel goes away, and for a while nothing put it back: the
    /// pane stayed on screen showing whatever it last said, for as long as you kept summoning
    /// the panel. Frozen text looks exactly like a session that has gone quiet.
    private func startOutput() {
        stopOutput()
        guard outputOpen else { return }
        Log.write("output: following \(currentTarget?.name ?? "-")")
        refreshOutput()
        let t = Timer(timeInterval: 1.2, repeats: true) { [weak self] _ in self?.refreshOutput() }
        RunLoop.main.add(t, forMode: .common)
        outputTimer = t
    }

    /// Show or clear the "what is it doing right now" strip.
    private func setActivity(_ text: String?) {
        let next = text ?? ""
        guard next != activityLabel.stringValue else { return }
        activityLabel.stringValue = next
        // Appearing and disappearing changes the pane's height, so the card has to be told.
        relayout()
    }

    private func stopOutput() {
        setActivity(nil)
        if outputTimer != nil { Log.write("output: stopped following") }
        outputTimer?.invalidate()
        outputTimer = nil
    }

    private func refreshOutput() {
        guard cannedTranscript == nil else { return }
        guard outputOpen, panel.isVisible, let target = currentTarget else { return }
        DispatchQueue.global(qos: .utility).async {
            // The transcript is the better source when there is one: it has the message
            // boundaries the screen only implies, and it is not truncated to a viewport.
            if Config.shared.outputMode != "terminal",
               let rendered = self.renderTranscript(for: target) {
                DispatchQueue.main.async {
                    guard self.outputOpen, rendered.signature != self.lastOutput else { return }
                    self.lastOutput = rendered.signature
                    // Which edge is "keeping up" depends on the order: newest-first puts the
                    // arriving message at the top, so following it means staying at the top.
                    let following = self.jumpToNewestOnFill
                        || self.outputView.string.isEmpty
                        || self.outputIsAtNewestEdge
                    self.jumpToNewestOnFill = false
                    let clip = self.outputHost.contentView
                    let saved = clip.bounds.origin
                    self.outputView.textStorage?.setAttributedString(rendered.text)
                    if let tc = self.outputView.textContainer {
                        self.outputView.layoutManager?.ensureLayout(for: tc)
                    }
                    if following {
                        self.scrollOutputToNewest()
                    } else {
                        clip.setBoundsOrigin(saved)
                        self.outputHost.reflectScrolledClipView(clip)
                    }
                }
                // The status line is painted on the terminal and never written to the file, so
                // reading the conversation from disk still leaves this one thing to scrape.
                let running = Activity.parse(Targets.capture(target) ?? "")
                DispatchQueue.main.async { self.setActivity(running) }
                return
            }
            guard Config.shared.outputMode != "transcript" else { return }
            let raw = Targets.capture(target)
            // Trailing blank lines are most of what a terminal screen is; dropping them puts
            // the last real line at the bottom, which is where the eye goes.
            var lines = (raw ?? "").split(separator: "\n", omittingEmptySubsequences: false)
            let blank: (Substring) -> Bool = { $0.trimmingCharacters(in: .whitespaces).isEmpty }
            while lines.first.map(blank) == true { lines.removeFirst() }
            while lines.last.map(blank) == true { lines.removeLast() }
            let text = lines.joined(separator: "\n")
            DispatchQueue.main.async {
                guard self.outputOpen else { return }
                // A terminal that is not changing produces an identical capture. Rewriting
                // the text storage anyway relaid out 3000pt of glyphs and threw the scroll
                // position around while somebody was reading it.
                guard text != self.lastOutput else { return }
                self.lastOutput = text
                // On the first fill the document has just grown from nothing, so the
                // "already at the bottom" test is false and it would sit at the top of a
                // terminal screen — which is the oldest and usually emptiest part of it.
                let clip = self.outputHost.contentView
                let atBottom = self.jumpToNewestOnFill
                    || self.outputView.string.isEmpty
                    || self.outputIsScrolledToBottom
                self.jumpToNewestOnFill = false
                let saved = clip.bounds.origin
                let body = text.isEmpty ? L.t.noOutput : text
                if Ansi.hasEscapes(body) {
                    self.outputView.textStorage?.setAttributedString(
                        Ansi.attributed(body, font: Style.outputFont, defaultColor: .labelColor))
                } else {
                    self.outputView.string = body
                }
                if let tc = self.outputView.textContainer {
                    self.outputView.layoutManager?.ensureLayout(for: tc)
                }
                if atBottom {
                    self.outputView.scrollToEndOfDocument(nil)
                } else {
                    // Put the reader back exactly where they were, rather than wherever
                    // relayout happened to leave them.
                    clip.setBoundsOrigin(saved)
                    self.outputHost.reflectScrolledClipView(clip)
                }

            }
        }
    }

    /// The transcript for a session, already laid out. Nil when there is none to be found,
    /// which is the signal to fall back to scraping the terminal.
    private func renderTranscript(for target: TargetSession)
        -> (text: NSAttributedString, signature: String)? {
        guard target.isClaude,
              let cwd = Targets.workingDirectory(of: target),
              let file = Transcript.locate(cwd: cwd, tabTitle: target.name),
              // Eight megabytes, because the limit that bites is bytes and not entries: at the
              // 400KB this used to read, a busy session yielded sixteen records and the reader
              // hit the top of the pane almost immediately.
              let text = Transcript.tail(of: file, bytes: 8_000_000)
        else { return nil }
        let entries = Transcript.parse(text)
        guard !entries.isEmpty else { return nil }
        let folds = expandedFolds
        let newestFirst = Config.shared.outputNewestFirst
        return (Transcript.render(entries, size: Config.shared.outputSize,
                                  mono: Style.outputFont, expanded: folds, newestFirst: newestFirst),
                Transcript.signature(of: file)
                    + "-\(Config.shared.outputSize)-\(newestFirst)"
                    + "-\(folds.sorted().joined(separator: ","))")
    }

    /// A folded run of tool calls was clicked. The pane is read-only, so a link is the only
    /// thing in it that can be clicked at all — which is why folds are links rather than, say,
    /// a disclosure triangle drawn into the text.
    func textView(_ view: NSTextView, clickedOnLink link: Any, at index: Int) -> Bool {
        guard let url = (link as? URL)?.absoluteString ?? link as? String else { return false }
        guard url.hasPrefix("clawdline://fold/") else {
            // A deploy run, or the backlog page. Opening it is the whole point of showing it.
            if let real = URL(string: url) { NSWorkspace.shared.open(real) }
            hide()
            return true
        }
        let key = String(url.dropFirst("clawdline://fold/".count))
        if expandedFolds.contains(key) { expandedFolds.remove(key) } else { expandedFolds.insert(key) }
        // The signature carries the fold set, so this re-renders rather than being skipped.
        refreshOutput()
        return true
    }

    /// Whether the reader is parked where new messages land. Only auto-scroll from there —
    /// yanking the view while somebody is reading elsewhere is worse than not following at all.
    private var outputIsAtNewestEdge: Bool {
        Config.shared.outputNewestFirst
            ? outputHost.contentView.bounds.minY <= 24
            : outputIsScrolledToBottom
    }

    private func scrollOutputToNewest() {
        if Config.shared.outputNewestFirst {
            outputView.scrollToBeginningOfDocument(nil)
        } else {
            outputView.scrollToEndOfDocument(nil)
        }
    }

    private var outputIsScrolledToBottom: Bool {
        guard let doc = outputHost.documentView else { return true }
        let visible = outputHost.contentView.bounds
        return visible.maxY >= doc.frame.height - 24
    }

    // MARK: - Targets

    private func refreshTargets() {
        scanning = targets.isEmpty
        updateTargetLabel()
        DispatchQueue.global(qos: .userInitiated).async {
            let snap = Targets.snapshot()
            DispatchQueue.main.async { self.apply(snap) }
        }
    }

    private func apply(_ snap: Targets.Snapshot) {
        scanning = false
        lastKnownCurrentID = snap.currentID
        var list = snap.claudeSessions
        // With no Claude Code running, fall back to every session so text can still reach some shell.
        if list.isEmpty { list = snap.sessions }
        targets = list

        var chosen = 0
        if let sticky = stickyID, snap.currentID == stickyBase,
           let i = targets.firstIndex(where: { $0.id == sticky }) {
            chosen = i
        } else if let cur = snap.currentID, let i = targets.firstIndex(where: { $0.id == cur }) {
            chosen = i
            stickyID = nil
        } else if let last = Config.shared.lastTargetID, let i = targets.firstIndex(where: { $0.id == last }) {
            chosen = i
        }
        targetIndex = targets.isEmpty ? 0 : chosen

        rebuildRows()
        updateTargetLabel()
        refreshProjectInfo()
        // The session scan is async, so an output pane opened before it landed had no target
        // to read and gave up. Nothing retried it, and it stayed blank for good.
        if outputOpen { refreshOutput() }
        if let e = snap.error { setHint(e, warn: true) }
        relayout()
    }

    private func rebuildRows() {
        rows.forEach { $0.removeFromSuperview() }
        let titles: [String]
        let selected: Int
        switch listMode {
        case .mascots:
            titles = mascotNames
            selected = mascotIndex
        default:
            titles = targets.map { $0.label }
            selected = targetIndex
        }
        rows = titles.prefix(9).enumerated().map { i, t in
            let row = TargetRow(title: t, index: i)
            row.isSelected = (i == selected)
            row.onClick = { [weak self] in self?.choose(i) }
            listBox.addSubview(row)
            return row
        }
    }

    /// Open (or close) one of the lists.
    private func showList(_ mode: ListMode) {
        if listMode == mode { listMode = .none; rebuildRows(); relayout(); return }
        listMode = mode
        if mode == .mascots {
            mascotNames = MascotPack.available()
            mascotIndex = max(0, mascotNames.firstIndex(of: Config.shared.mascot) ?? 0)
        }
        rebuildRows()
        relayout()
    }

    /// Route a selection to whichever list is open.
    private func choose(_ i: Int) {
        if listMode == .mascots { pickMascot(i) } else { pick(i) }
    }

    /// Switching previews immediately — you pick a character by looking at it, not by
    /// reading its name, so the change has to happen while the list is still open.
    private func pickMascot(_ i: Int, closeList: Bool = true) {
        guard mascotNames.indices.contains(i) else { return }
        mascotIndex = i
        Config.shared.mascot = mascotNames[i]
        Config.shared.save()
        if let why = mascot.reload() { setHint(why, warn: true) }
        mascot.play("pop")
        for (n, row) in rows.enumerated() { row.isSelected = (n == i) }
        if closeList { listMode = .none }
        relayout()
    }

    private func cycle(forward: Bool) {
        guard targets.count > 1 else { return }
        let next = (targetIndex + (forward ? 1 : targets.count - 1)) % targets.count
        pick(next, closeList: false)
    }

    private func pick(_ i: Int, closeList: Bool = true) {
        guard targets.indices.contains(i) else { return }
        // Fold keys are derived from content, so they would not collide across sessions — but
        // carrying them over means arriving in a new transcript with something already open.
        if targetIndex != i { expandedFolds.removeAll() }
        targetIndex = i
        stickyID = targets[i].id
        stickyBase = lastKnownCurrentID
        Config.shared.lastTargetID = targets[i].id
        Config.shared.save()
        for (n, row) in rows.enumerated() { row.isSelected = (n == i) }
        updateTargetLabel()
        refreshProjectInfo()
        refreshOutput()
        if closeList && listMode != .none { listMode = .none; relayout() }
    }

    /// Ask git which project the selected session is in, once per session per summon.
    ///
    /// The tab title is the task, and two projects can be working on tasks that read the same
    /// at a glance. The repository name is what tells them apart — and a message sent to the
    /// wrong one cannot be taken back by reading it.
    private func refreshProjectInfo() {
        guard let target = currentTarget else { return }
        // Kept between summons and refreshed behind the last answer. Clearing it first meant the
        // footer opened blank and filled in a beat later, every time — and the branch and the
        // count are worth being a second stale to have the name there the moment you look.
        if let seen = projectSeen[target.id], CFAbsoluteTimeGetCurrent() - seen < 5 { return }
        projectSeen[target.id] = CFAbsoluteTimeGetCurrent()
        DispatchQueue.global(qos: .utility).async {
            guard let cwd = Targets.workingDirectory(of: target),
                  let info = Project.info(cwd: cwd) else { return }
            let icon = ProjectIcon.grid(forCwd: cwd)
            let status = ProjectStatus.read(cwd: cwd, remote: info.remote,
                                            registry: ProjectIcon.row(forCwd: cwd))
            DispatchQueue.main.async {
                self.projectCache[target.id] = info
                self.iconCache[target.id] = icon
                self.statusCache[target.id] = status
                if self.currentTarget?.id == target.id { self.updateTargetLabel() }
            }
        }
    }

    /// The deploy, the backlog and the health check, as things you can click.
    ///
    /// The terminal status line makes these hyperlinks with OSC 8; a window has real links, so
    /// the same rows become the same destinations. A run you cannot open is a number you have to
    /// go and look up somewhere else, which is most of the reason nobody looks.
    private func appendStatusChips(_ status: ProjectStatus.Snapshot?,
                                   to s: NSMutableAttributedString) {
        guard let status, !status.isEmpty else { return }
        let font = NSFont.systemFont(ofSize: Style.hintSize - 0.5)
        let mono = NSFont.monospacedSystemFont(ofSize: Style.hintSize - 1.5, weight: .regular)

        func chip(_ text: String, _ colour: NSColor, link: String?, font: NSFont = font) {
            var attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: colour]
            if let link, !link.isEmpty { attrs[.link] = link }
            s.append(NSAttributedString(string: text, attributes: attrs))
        }

        if let d = status.deploy {
            let now = Date().timeIntervalSince1970
            chip("   " + d.label + " ", d.state == "fail" ? .systemRed : NSColor.secondaryLabelColor,
                 link: d.url)
            if d.state == "running" {
                chip(ProjectStatus.bar(d.progress(now: now)), Style.accent, link: d.url, font: mono)
                chip(" " + ProjectStatus.duration(d.elapsed(now: now))
                     + "/" + ProjectStatus.duration(Int(d.typicalSeconds)),
                     .tertiaryLabelColor, link: d.url)
            } else {
                chip(d.state == "ok" ? "✓" : "✗",
                     d.state == "ok" ? .systemGreen : .systemRed, link: d.url)
            }
        }
        if let b = status.backlog {
            // The lane asking for attention leads; the total is context for it.
            chip("   ≡\(b.total)", .tertiaryLabelColor,
                 link: b.artifact.map { "file://" + $0 })
            if b.now > 0 {
                chip(" " + L.t.backlogNow(b.now), Style.accent,
                     link: b.artifact.map { "file://" + $0 })
            }
        }
        if let h = status.health {
            let live = h.state == "ok"
            chip("   ● ", live ? .systemGreen : .systemRed, link: h.url)
            // Coloured and underlined when there is somewhere to go, the way the terminal's own
            // status line marks it — a link that does not look like one is a link nobody presses.
            var attrs: [NSAttributedString.Key: Any] = [
                .font: font,
                .foregroundColor: h.url == nil ? NSColor.tertiaryLabelColor
                                               : (live ? NSColor.systemGreen : NSColor.systemRed),
            ]
            if let url = h.url {
                attrs[.link] = url
                attrs[.underlineStyle] = NSUnderlineStyle.single.rawValue
            }
            s.append(NSAttributedString(string: h.label, attributes: attrs))
        }
    }

    /// Talk instead of type. Wired here rather than in the text view because the state it puts
    /// the bar into — listening, and on which machine — belongs to the whole card.
    @objc private func toggleVoice() {
        voiceBaseText = textView.resolvedText()
        voice.onText = { [weak self] text in
            guard let self else { return }
            let joiner = self.voiceBaseText.isEmpty
                || self.voiceBaseText.hasSuffix(" ") ? "" : " "
            self.textView.setPlainText(self.voiceBaseText + joiner + text)
            self.textView.setSelectedRange(NSRange(location: self.textView.string.count, length: 0))
            self.relayout()
        }
        voice.onState = { [weak self] state in
            guard let self else { return }
            switch state {
            case .idle:
                self.micButton.image = NSImage(systemSymbolName: "mic",
                                               accessibilityDescription: L.t.hintVoice)
                self.micButton.contentTintColor = .tertiaryLabelColor
            case .listening(let onDevice):
                self.micButton.image = NSImage(systemSymbolName: "mic.fill",
                                               accessibilityDescription: L.t.hintVoice)
                self.micButton.contentTintColor = Style.accent
                self.setHint(L.t.voiceListening(onDevice: onDevice), warn: false)
            case .failed(let why):
                self.micButton.image = NSImage(systemSymbolName: "mic.slash",
                                               accessibilityDescription: L.t.hintVoice)
                self.micButton.contentTintColor = .systemRed
                self.setHint(why, warn: true)
            }
            self.relayout()
        }
        voice.toggle(locale: Self.voiceLocales())
    }

    /// What to listen in: what the bar is set to, then whatever the Mac is set to.
    static func voiceLocales() -> [String] {
        switch Config.shared.language {
        case "zh-Hant": return ["zh-TW"]
        case "en": return ["en-US"]
        default: return []
        }
    }

    /// ⌘/ — the key this is behind almost everywhere else.
    private func toggleKeys() {
        keysShown.toggle()
        applyHints()
        relayout()
    }

    private func applyHints() {
        // Enter sends and Esc closes in every box like this one; the rest are worth showing,
        // but not worth the width all the time.
        hints.hints = keysShown ? hintsAll : [.init(key: "⌘/", label: L.t.hintKeys)]
    }

    private func setFooter(_ text: NSAttributedString) {
        targetLabel.textStorage?.setAttributedString(text)
    }

    private func updateTargetLabel() {
        iconView?.grid = currentTarget.flatMap { iconCache[$0.id] }
        guard !usingStandInLabel else {
            setFooter(Self.standInTarget())
            return
        }
        let s = NSMutableAttributedString()
        if let t = currentTarget {
            s.append(NSAttributedString(string: "● ", attributes: [
                .foregroundColor: t.isClaude ? Style.accent : NSColor.tertiaryLabelColor,
                .font: NSFont.systemFont(ofSize: 10),
            ]))
            let project = projectCache[t.id]
            if let p = project, !p.name.isEmpty {
                // The project's own colour, so the name and the mark beside it agree — and so
                // two tabs that read alike differ before you have finished reading either.
                s.append(NSAttributedString(string: p.name + "  ", attributes: [
                    .foregroundColor: iconCache[t.id]?.accent ?? NSColor.labelColor,
                    .font: NSFont.systemFont(ofSize: Style.hintSize, weight: .semibold),
                ]))
            }
            var name = t.label
            let room = project == nil ? 40 : 28
            if name.count > room { name = String(name.prefix(room)) + "…" }
            // Full strength: this is the one thing on the card that says which conversation
            // everything else belongs to, and at secondary it read as a caption.
            s.append(NSAttributedString(string: name, attributes: [
                .foregroundColor: NSColor.labelColor,
                .font: NSFont.systemFont(ofSize: Style.hintSize),
            ]))
            if let p = project, !p.branch.isEmpty {
                var git = "  ⎇ " + p.branch
                if p.dirty > 0 { git += " *\(p.dirty)" }
                s.append(NSAttributedString(string: git, attributes: [
                    .foregroundColor: NSColor.tertiaryLabelColor,
                    .font: NSFont.systemFont(ofSize: Style.hintSize - 0.5),
                ]))
            }
            appendStatusChips(statusCache[t.id], to: s)
            if targets.count > 1 {
                s.append(NSAttributedString(string: "  \(targetIndex + 1)/\(targets.count)", attributes: [
                    .foregroundColor: NSColor.secondaryLabelColor,
                    .font: NSFont.monospacedDigitSystemFont(ofSize: Style.hintSize - 0.5, weight: .regular),
                ]))
            }
        } else {
            s.append(NSAttributedString(string: scanning ? L.t.scanning : L.t.noSession, attributes: [
                .foregroundColor: NSColor.tertiaryLabelColor,
                .font: NSFont.systemFont(ofSize: Style.hintSize),
            ]))
        }
        setFooter(s)
    }

    // MARK: - Hint row

    private func resetHint() {
        hintResetWork?.cancel()
        hints.isHidden = false
        hints.needsDisplay = true
    }

    private func setHint(_ text: String, warn: Bool) {
        hintResetWork?.cancel()
        guard warn else {
            hints.isHidden = true
            setFooter(NSAttributedString(string: text, attributes: [
                .foregroundColor: NSColor.secondaryLabelColor,
                .font: NSFont.systemFont(ofSize: Style.hintSize, weight: .medium),
            ]))
            let back = DispatchWorkItem { [weak self] in
                self?.resetHint(); self?.updateTargetLabel(); self?.relayout()
            }
            hintResetWork = back
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.6, execute: back)
            return
        }
        // When something breaks, the whole keycap row gives way to the reason. That is what the user needs then, not shortcuts.
        hints.isHidden = true
        setFooter(NSAttributedString(string: "⚠ " + text, attributes: [
            .foregroundColor: Style.accent,
            .font: NSFont.systemFont(ofSize: Style.hintSize, weight: .medium),
        ]))
        targetLabel.frame = NSRect(x: Style.padH, y: (Style.hintHeight - 15) / 2,
                                   width: card.bounds.width - Style.padH * 2, height: 15)
        let work = DispatchWorkItem { [weak self] in
            self?.resetHint()
            self?.updateTargetLabel()
            self?.relayout()
        }
        hintResetWork = work
        DispatchQueue.main.asyncAfter(deadline: .now() + 5, execute: work)
    }

    // MARK: - Keyboard

    private func handleArrow(_ delta: Int) -> Bool {
        if listMode == .mascots {
            pickMascot(max(0, min(rows.count - 1, mascotIndex + delta)), closeList: false)
            return true
        }
        if listMode == .sessions {
            pick(max(0, min(rows.count - 1, targetIndex + delta)), closeList: false)
            return true
        }
        let hist = Config.shared.history
        guard !hist.isEmpty else { return false }
        if delta < 0 {
            guard textView.string.isEmpty || historyCursor >= 0 else { return false }
            historyCursor = min(hist.count - 1, historyCursor + 1)
            textView.setPlainText(hist[hist.count - 1 - historyCursor])
        } else {
            guard historyCursor >= 0 else { return false }
            historyCursor -= 1
            textView.setPlainText(historyCursor < 0 ? "" : hist[hist.count - 1 - historyCursor])
        }
        textView.setSelectedRange(NSRange(location: textView.string.count, length: 0))
        relayout()
        return true
    }

    // MARK: - Sending

    private func submit() {
        let text = textView.resolvedText().trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { hide(); return }
        guard let target = currentTarget else {
            setHint(L.t.nothingToSend, warn: true)
            return
        }

        var hist = Config.shared.history
        hist.removeAll { $0 == text }
        hist.append(text)
        Config.shared.history = Array(hist.suffix(60))
        Config.shared.save()

        textView.clearText()
        historyCursor = -1
        idleWork?.cancel()
        danceWork?.cancel()
        mascot.play("cheer")

        DispatchQueue.global(qos: .userInitiated).async {
            let err = Targets.send(text, to: target)
            guard let err else { return }
            DispatchQueue.main.async { self.restoreAfterFailure(text: text, error: err) }
        }
        // Let the jump finish before closing: an action needs a result, or pressing Enter feels like nothing happened.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.18) { [weak self] in self?.hide() }
    }

    /// Give the text back when the send fails. Someone typed two hundred characters; an iTerm hiccup must not swallow them.
    private func restoreAfterFailure(text: String, error: String) {
        show()
        textView.setPlainText(text)
        textView.setSelectedRange(NSRange(location: text.count, length: 0))
        relayout()
        setHint(error, warn: true)
    }

    /// Send a string to the current target without opening the panel.
    /// Used by clawdline://send?text=… so external tools (Shortcuts, Stream Deck, scripts) can push
    /// text in — and so "does the whole path work" can be verified with nobody at the keyboard.
    func sendDirect(_ text: String, target wanted: String? = nil) {
        let body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty else { return }
        DispatchQueue.global(qos: .userInitiated).async {
            let snap = Targets.snapshot()
            var list = snap.claudeSessions
            if list.isEmpty { list = snap.sessions }
            // An explicit target is searched across every session, not just the Claude ones:
            // naming one means you know what you are doing.
            let target = snap.sessions.first(where: { $0.id == wanted })
                ?? list.first(where: { $0.id == snap.currentID })
                ?? list.first(where: { $0.id == Config.shared.lastTargetID })
                ?? list.first
            guard let target else {
                Log.write("sendDirect: no target (\(snap.error ?? "the list was empty"))")
                return
            }
            let err = Targets.send(body, to: target)
            Log.write("sendDirect → \(target.label): \(err ?? "ok")")
        }
    }

    /// Render what the panel draws into a PNG.
    /// The reason is practical: `screencapture` needs Screen Recording permission, and without it
    /// the result is the wallpaper with every window stripped out — which leaves you blind while
    /// tuning layout. This path only paints our own layers, so it needs no permission at all.
    func snapshot(to path: String, routine: String? = nil, at time: Double? = nil, list: String? = nil,
                  output: Bool = false, session: String? = nil, full: Bool? = nil,
                  transcript: String? = nil) {
        // Opening the pane has to happen before the wait, not inside the render: the transcript
        // arrives asynchronously, so a pane opened at draw time is always drawn empty.
        let arrange = { [weak self] in
            guard let self else { return }
            // Naming a session makes a particular transcript reproducible to look at, which is
            // the only way to check how something rare — a table, a long code block — comes out.
            if let want = session, !want.isEmpty,
               let i = self.targets.firstIndex(where: { $0.label.localizedCaseInsensitiveContains(want) }) {
                self.pick(i, closeList: false)
            }
            if output, !self.outputOpen { self.toggleOutput() }
            // A canned transcript, for the pictures on the README. Shooting a real session
            // would publish whatever the machine happened to be working on that afternoon.
            if let file = transcript, !file.isEmpty { self.showCannedTranscript(at: file) }
            if let want = full, want != self.fullscreen { self.toggleFullscreen() }
            if list == "mascots" { self.showList(.mascots) }
            else if list == "sessions" { self.showList(.sessions) }
        }

        let render = { [weak self] in
            guard let self else { return }
            // Naming a routine and a time draws one specific frame of the animation — otherwise animation can only be eyeballed, never tuned
            if let r = routine, !r.isEmpty {
                self.mascot.play(r, then: r)
                self.mascot.frozenTime = time ?? 0.25
            }
            defer { self.mascot.frozenTime = nil }
            let size = self.container.bounds.size
            guard size.width > 10,
                  let rep = self.container.bitmapImageRepForCachingDisplay(in: self.container.bounds) else { return }
            self.container.cacheDisplay(in: self.container.bounds, to: rep)

            let img = NSImage(size: size)
            img.lockFocus()
            NSColor(white: 0.30, alpha: 1).setFill()                     // stand-in for the wallpaper
            NSRect(origin: .zero, size: size).fill()
            NSColor(white: 0.11, alpha: 0.94).setFill()                  // stand-in for the frosted glass
            NSBezierPath(roundedRect: self.cardHost.frame,
                         xRadius: Style.corner, yRadius: Style.corner).fill()
            rep.draw(in: NSRect(origin: .zero, size: size))
            img.unlockFocus()

            guard let tiff = img.tiffRepresentation,
                  let bmp = NSBitmapImageRep(data: tiff),
                  let png = bmp.representation(using: .png, properties: [:]) else { return }
            try? png.write(to: URL(fileURLWithPath: path))
            Log.write("snapshot → \(path) (\(Int(size.width))×\(Int(size.height)))")
        }

        let wasVisible = panel.isVisible
        if !wasVisible { show() }
        // The session list arrives from an async scan, so picking one has to wait for it —
        // arranging immediately picks from an empty list and silently keeps the current target.
        DispatchQueue.main.asyncAfter(deadline: .now() + (session == nil ? 0 : 1.4)) {
            arrange()
            // Reading a session back costs an osascript round trip, so give it room.
            DispatchQueue.main.asyncAfter(deadline: .now() + (output ? 2.2 : 0.55)) {
                render()
                if !wasVisible { self.hide() }
            }
        }
    }

    /// Fill the pane from a transcript file on disk and stop it being refreshed away.
    ///
    /// The file goes through the same parse and render as a live session — a picture made any
    /// other way would be a picture of a mock-up, and would stop matching the day it drifted.
    private func showCannedTranscript(at path: String) {
        stopOutput()
        cannedTranscript = path
        usingStandInLabel = true
        guard let text = Transcript.tail(of: URL(fileURLWithPath: path), bytes: 8_000_000)
        else { return }
        let entries = Transcript.parse(text)
        guard !entries.isEmpty else { return }
        outputView.textStorage?.setAttributedString(
            Transcript.render(entries, size: Config.shared.outputSize, mono: Style.outputFont,
                              expanded: expandedFolds,
                              newestFirst: Config.shared.outputNewestFirst))
        if let tc = outputView.textContainer { outputView.layoutManager?.ensureLayout(for: tc) }
        scrollOutputToNewest()
        setFooter(Self.standInTarget())
    }

    /// Render a whole demo, frame by frame, into PNGs for ffmpeg to turn into a GIF.
    ///
    /// Why not a screen recording: it needs Screen Recording permission, and it would capture real
    /// tab titles and project names — publishing that to GitHub publishes your work along with it.
    /// Drawing every frame gives full control and reproduces byte for byte on every rerun.
    func filmstrip(dir: String, fps: Double, seconds: Double, script: String, text: String) {
        show()
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
            self.renderFilmstrip(dir: dir, fps: fps, seconds: seconds, script: script, text: text)
            self.hide()
        }
    }

    private func renderFilmstrip(dir: String, fps: Double, seconds: Double, script: String, text: String) {
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)

        let fake = Self.standInTarget()

        let margin: CGFloat = 64
        textView.clearText()
        relayout()
        let panelSize = container.bounds.size
        let canvas = NSSize(width: panelSize.width + margin * 2, height: panelSize.height + margin * 2)

        let total = Int(seconds * fps)
        for i in 0..<total {
            let t = Double(i) / fps
            let step = Self.timeline(script: script, t: t, seconds: seconds, text: text)

            textView.setPlainText(step.text)
            mascot.play(step.routine, then: step.routine)
            mascot.frozenTime = step.mascotTime
            relayout()
            setFooter(fake)

            guard let rep = container.bitmapImageRepForCachingDisplay(in: container.bounds) else { continue }
            container.cacheDisplay(in: container.bounds, to: rep)
            let panel = NSImage(size: panelSize)
            panel.addRepresentation(rep)

            let out = NSImage(size: canvas)
            out.lockFocus()
            Self.drawBackdrop(NSRect(origin: .zero, size: canvas))

            // The card itself (frosted glass cannot be captured, so approximate it)
            let box = NSRect(x: margin, y: margin, width: panelSize.width, height: panelSize.height)
            NSGraphicsContext.current?.saveGraphicsState()
            let tf = NSAffineTransform()
            tf.translateX(by: canvas.width / 2, yBy: canvas.height / 2)
            tf.scaleX(by: step.scale, yBy: step.scale)
            tf.translateX(by: -canvas.width / 2, yBy: -canvas.height / 2)
            tf.concat()
            NSColor(white: 0.10, alpha: 0.90 * step.alpha).setFill()
            NSBezierPath(roundedRect: NSRect(x: box.minX, y: box.minY,
                                             width: panelSize.width, height: self.cardHost.frame.height),
                         xRadius: Style.corner, yRadius: Style.corner).fill()
            panel.draw(in: box, from: .zero, operation: .sourceOver, fraction: step.alpha)
            NSGraphicsContext.current?.restoreGraphicsState()
            out.unlockFocus()

            if let tiff = out.tiffRepresentation, let bmp = NSBitmapImageRep(data: tiff),
               let png = bmp.representation(using: .png, properties: [:]) {
                let name = String(format: "f%04d.png", i)
                try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
            }
        }
        mascot.frozenTime = nil
        textView.clearText()
        Log.write("filmstrip → \(dir) (\(total) frames @ \(Int(fps))fps)")
    }

    private struct Step {
        var text = ""
        var routine = "idle"
        var mascotTime: Double = 0
        var alpha: CGFloat = 1
        var scale: CGFloat = 1
    }

    /// The demo storyboard. The timings are fixed on purpose: the README image has to be reproducible.
    /// A stand-in for the target label. Real tab titles would publish what the machine happens
    /// to be working on, and every picture in this repo is meant to be safe to publish.
    static func standInTarget() -> NSAttributedString {
        let out = NSMutableAttributedString()
        out.append(NSAttributedString(string: "● ", attributes: [
            .foregroundColor: Style.accent, .font: NSFont.systemFont(ofSize: 10)]))
        out.append(NSAttributedString(string: "✳ my-project", attributes: [
            .foregroundColor: NSColor.secondaryLabelColor,
            .font: NSFont.systemFont(ofSize: Style.hintSize)]))
        out.append(NSAttributedString(string: "  2/3", attributes: [
            .foregroundColor: NSColor.tertiaryLabelColor,
            .font: NSFont.monospacedDigitSystemFont(ofSize: Style.hintSize - 0.5, weight: .regular)]))
        return out
    }

    private static func timeline(script: String, t: Double, seconds: Double, text: String) -> Step {
        var s = Step()

        // Any routine name plays that routine straight through, which is how the pack
        // gallery and the per-routine clips are shot.
        if !script.isEmpty, script != "demo" {
            s.routine = script
            s.mascotTime = t
            s.text = text
            return s
        }

        // Full demo: entrance → idle → typing → cheer on send → dismiss
        let typeStart = 0.95, typeEnd = 2.70, cheerAt = 3.30, outAt = seconds - 0.35
        if t < 0.35 {
            s.routine = "pop"; s.mascotTime = t
            let p = min(1, t / 0.30)
            s.alpha = CGFloat(p)
            s.scale = CGFloat(0.90 + 0.10 * (1 - pow(1 - p, 3)))
        } else if t < typeStart {
            s.routine = "idle"; s.mascotTime = t - 0.35
        } else if t < typeEnd {
            let p = (t - typeStart) / (typeEnd - typeStart)
            let n = Int(Double(text.count) * min(1, p * 1.08))
            s.text = String(text.prefix(n))
            s.routine = "typing"; s.mascotTime = t - typeStart
        } else if t < cheerAt {
            s.text = text
            s.routine = "idle"; s.mascotTime = t - typeEnd
        } else if t < outAt {
            s.routine = "cheer"; s.mascotTime = t - cheerAt
        } else {
            s.routine = "cheer"; s.mascotTime = t - cheerAt
            let p = min(1, (t - outAt) / 0.30)
            s.alpha = CGFloat(1 - p)
            s.scale = CGFloat(1 - 0.05 * p)
        }
        return s
    }

    private static func drawBackdrop(_ rect: NSRect) {
        let g = NSGradient(colors: [
            NSColor(srgbRed: 0.09, green: 0.09, blue: 0.11, alpha: 1),
            NSColor(srgbRed: 0.16, green: 0.12, blue: 0.10, alpha: 1),
            NSColor(srgbRed: 0.07, green: 0.07, blue: 0.09, alpha: 1),
        ])
        g?.draw(in: rect, angle: -60)
        if let warm = NSGradient(colors: [Style.accent.withAlphaComponent(0.16),
                                          Style.accent.withAlphaComponent(0)]) {
            let c = NSPoint(x: rect.midX, y: rect.maxY - rect.height * 0.22)
            warm.draw(fromCenter: c, radius: 0, toCenter: c, radius: rect.width * 0.45, options: [])
        }
    }

    /// Re-read the mascot pack from disk. Called on every show, so an agent editing the
    /// JSON sees the result the next time the panel opens — no relaunch, no rebuild.
    func reloadMascot() {
        if let why = mascot.reload() { setHint(why, warn: true) }
        relayout()
    }

    // MARK: - Used by the menu bar

    var targetSummary: String { currentTarget?.label ?? L.t.menuNoTarget }

    /// Used by the menu bar's mascot submenu.
    var mascotNamesForMenu: [String] { MascotPack.available() }
    func selectMascot(named name: String) {
        Config.shared.mascot = name
        Config.shared.save()
        if let why = mascot.reload() { Log.write("mascot: \(why)") }
        if panel.isVisible { mascot.play("pop"); relayout() }
    }

    func revealCurrentTarget() {
        guard let t = currentTarget else { return }
        Targets.reveal(t)
    }
}

extension PromptController {

    /// The transcript pane's text view.
    ///
    /// Its own function so a test can hold one: two of the settings below fail silently rather
    /// than loudly, and a silent failure with no carrier comes back.
    static func makeOutputView() -> NSTextView {
        // Read-only but selectable — being able to copy an error out of it is most of the point
        // of being able to see it at all.
        let view = NSTextView()
        view.isEditable = false
        view.isSelectable = true
        view.font = Style.outputFont
        view.defaultParagraphStyle = {
            let p = NSMutableParagraphStyle()
            p.lineSpacing = 1.5
            return p
        }()
        // secondaryLabelColor at 11pt over a blurred card was not readable. This pane is
        // something you read, not a caption, so it gets full-strength text and a ground of
        // its own — the contrast comes from the surface as much as the ink.
        view.textColor = .labelColor
        // A fresh text view is TextKit 2, and NSTextTable — which draws the borders on a
        // Markdown table — does not exist there. Touching layoutManager pins it to TextKit 1.
        // Skip this and the cells lay out as ordinary paragraphs: no warning, no error, the
        // table just quietly loses its rules.
        _ = view.layoutManager
        // Only the cursor. Whatever else goes in here wins over the renderer's own attributes
        // for every link equally — which would paint the fold controls to look like hyperlinks,
        // when the whole point is that one of them opens a browser and the other does not.
        view.linkTextAttributes = [.cursor: NSCursor.pointingHand]
        view.drawsBackground = true
        view.backgroundColor = Style.outputBg
        view.textContainerInset = NSSize(width: Style.padH, height: 10)
        // A text view inside a scroll view needs all of this or its frame stays at zero and it
        // draws nothing — no warning, no error, just an empty pane.
        view.minSize = NSSize(width: 0, height: 0)
        view.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude,
                              height: CGFloat.greatestFiniteMagnitude)
        view.isVerticallyResizable = true
        view.isHorizontallyResizable = false
        view.autoresizingMask = [.width]
        view.textContainer?.containerSize = NSSize(width: 0, height: CGFloat.greatestFiniteMagnitude)
        view.textContainer?.widthTracksTextView = true
        view.textContainer?.lineFragmentPadding = 0
        return view
    }
}
