import AppKit

final class PromptController: NSObject, NSWindowDelegate {
    static let shared = PromptController()

    private var panel: PromptPanel!
    private var container: NSView!         // the animation scales this layer: mascot and card together
    private var cardHost: NSView!          // exists only to cast the shadow (the card clips its corners, and clipping kills a shadow)
    private var card: NSVisualEffectView!
    private var chrome: CardChrome!
    private var mascot: MascotView!
    private var glow: GlowView!
    private var chevron: NSTextField!
    private var scroll: NSScrollView!
    private var textView: PromptTextView!
    private var hintLine: NSView!
    private var listTopLine: NSView!
    private var listBox: NSView!
    private var targetLabel: NSTextField!
    private var hints: KeyHintsView!

    private var targets: [TargetSession] = []
    private var rows: [TargetRow] = []
    private var targetIndex = 0
    private var listOpen = false
    private var scanning = false

    /// The target the user picked by hand, plus which session iTerm was on at that moment.
    /// Why the second one: an explicit pick should not be overwritten by "where iTerm is now",
    /// but once the user actually moves to a different Claude tab, it should follow them there.
    private var stickyID: String?
    private var stickyBase: String?
    private var lastKnownCurrentID: String?

    private var previousApp: NSRunningApplication?
    private var shownAt = Date.distantPast
    private var dismissing = false
    private var showToken = 0
    private var historyCursor = -1
    private var hintResetWork: DispatchWorkItem?
    private var idleWork: DispatchWorkItem?
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

        container = NSView(frame: NSRect(x: 0, y: 0, width: W, height: 140))
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

        chevron = NSTextField(labelWithString: "❯")
        chevron.font = NSFont.monospacedSystemFont(ofSize: 17, weight: .bold)
        chevron.textColor = Style.accent
        chevron.alignment = .center
        card.addSubview(chevron)

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
        textView.isRichText = false
        textView.allowsUndo = true
        textView.font = NSFont.systemFont(ofSize: Style.textSize, weight: .regular)
        textView.textColor = .labelColor
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

        hintLine = line()
        card.addSubview(hintLine)

        targetLabel = NSTextField(labelWithString: "")
        targetLabel.font = NSFont.systemFont(ofSize: Style.hintSize)
        targetLabel.textColor = .tertiaryLabelColor
        targetLabel.lineBreakMode = .byTruncatingTail
        targetLabel.maximumNumberOfLines = 1
        card.addSubview(targetLabel)

        hints = KeyHintsView()
        hints.hints = [
            .init(key: "↵", label: L.t.hintSend),
            .init(key: "⇧↵", label: L.t.hintNewline),
            .init(key: "⇥", label: L.t.hintSwitch),
            .init(key: "⌘K", label: L.t.hintList),
            .init(key: "esc", label: ""),
        ]
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
            if self.listOpen { self.listOpen = false; self.relayout() } else { self.hide() }
        }
        textView.onCycleTarget = { [weak self] forward in self?.cycle(forward: forward) }
        textView.onToggleList = { [weak self] in
            guard let self else { return }
            self.listOpen.toggle()
            self.rebuildRows()
            self.relayout()
        }
        textView.onPickIndex = { [weak self] i in self?.pick(i) }
        textView.onToggleDance = { [weak self] in self?.toggleDance() }
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
        listOpen = false
        resetHint()
        reloadMascot()
        refreshTargets()
        relayout()
        position()
        shownAt = Date()

        panel.alphaValue = 0
        NSApp.activate(ignoringOtherApps: true)
        panel.makeKeyAndOrderFront(nil)
        panel.makeFirstResponder(textView)
        animateIn()
        Log.write("show: frame=\(panel.frame) prev=\(previousApp?.localizedName ?? "-")")
    }

    func hide() {
        guard panel.isVisible, !dismissing else { return }
        dismissing = true
        idleWork?.cancel()
        danceWork?.cancel()
        let token = showToken
        animateOut { [weak self] in
            guard let self, token == self.showToken else { return }
            self.panel.orderOut(nil)
            self.mascot.stop()          // once hidden, stop burning a 60fps timer
            self.listOpen = false
            self.dismissing = false
            if let prev = self.previousApp,
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
        hide()
    }

    private func position() {
        let screen = screenUnderMouse()
        let f = screen.frame
        let h = panel.frame.height
        let x = f.midX - Config.shared.width / 2
        // y_fraction refers to the top of the *card*, not the top of the window, so changing the
        // mascot's height never pushes the input line somewhere else.
        let cardTop = f.maxY - f.height * Config.shared.yFraction
        let windowTop = cardTop + (Style.mascotH - Style.mascotFootInset - Style.mascotOverlap) + Style.mascotTopPad
        panel.setFrameOrigin(NSPoint(x: round(x), y: round(windowTop - h)))
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

    private func relayout() {
        let W = Config.shared.width
        let inputH = max(Style.inputMinHeight, textHeight() + Style.inputPadV * 2)
        let visibleRows = listOpen ? min(rows.count, 9) : 0
        let listH = visibleRows > 0 ? CGFloat(visibleRows) * Style.rowHeight + Style.listPadV * 2 : 0
        let cardH = inputH + (listH > 0 ? listH + 1 : 0) + 1 + Style.hintHeight
        let total = cardH + (Style.mascotH - Style.mascotFootInset - Style.mascotOverlap) + Style.mascotTopPad

        var f = panel.frame
        let top = f.maxY
        f.size = NSSize(width: W, height: total)
        f.origin.y = top - total
        panel.setFrame(f, display: true)

        container.frame = NSRect(origin: .zero, size: f.size)
        cardHost.frame = NSRect(x: 0, y: 0, width: W, height: cardH)
        cardHost.layer?.shadowPath = CGPath(roundedRect: cardHost.bounds,
                                            cornerWidth: Style.corner, cornerHeight: Style.corner,
                                            transform: nil)
        card.frame = cardHost.bounds
        mascot.frame = NSRect(x: (W - Style.mascotW) / 2,
                              y: cardH - Style.mascotOverlap - Style.mascotFootInset,
                              width: Style.mascotW, height: Style.mascotH)
        // The glow lines up with the sprite, not the view — the view is larger (that is the jump room)
        let sr = mascot.spriteRect
        let side = sr.width + Style.glowInset * 2
        glow.frame = NSRect(x: mascot.frame.minX + sr.midX - side / 2,
                            y: mascot.frame.minY + sr.midY - side / 2,
                            width: side, height: side)

        layoutCard(size: card.bounds.size, inputH: inputH, listH: listH)
    }

    private func layoutCard(size: NSSize, inputH: CGFloat, listH: CGFloat) {
        let W = size.width, H = size.height
        chrome.frame = NSRect(origin: .zero, size: size)

        chevron.frame = NSRect(x: Style.padH, y: H - Style.inputPadV - 25, width: Style.chevronW, height: 24)

        let tx = Style.padH + Style.chevronW + Style.chevronGap
        scroll.frame = NSRect(x: tx, y: H - inputH + Style.inputPadV,
                              width: W - tx - Style.padH, height: inputH - Style.inputPadV * 2)

        hintLine.frame = NSRect(x: 0, y: Style.hintHeight, width: W, height: 1)

        let hintsW = min(hints.intrinsicWidth, W - Style.padH * 2 - 120)
        let hintsX = W - Style.padH - hintsW
        hints.frame = NSRect(x: hintsX, y: 0, width: hintsW, height: Style.hintHeight)
        targetLabel.frame = NSRect(x: Style.padH, y: (Style.hintHeight - 15) / 2,
                                   width: max(60, hintsX - Style.padH - 16), height: 15)

        if listH > 0 {
            listBox.isHidden = false
            listTopLine.isHidden = false
            listBox.frame = NSRect(x: 0, y: Style.hintHeight + 1, width: W, height: listH)
            listTopLine.frame = NSRect(x: 0, y: listBox.frame.maxY, width: W, height: 1)
            for (i, row) in rows.prefix(9).enumerated() {
                row.frame = NSRect(x: 0,
                                   y: listH - Style.listPadV - CGFloat(i + 1) * Style.rowHeight,
                                   width: W, height: Style.rowHeight)
            }
        } else {
            listBox.isHidden = true
            listTopLine.isHidden = true
        }
    }

    // MARK: - Targets

    private func refreshTargets() {
        scanning = targets.isEmpty
        updateTargetLabel()
        DispatchQueue.global(qos: .userInitiated).async {
            let snap = ITerm.snapshot()
            DispatchQueue.main.async { self.apply(snap) }
        }
    }

    private func apply(_ snap: ITerm.Snapshot) {
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
        if let e = snap.error { setHint(e, warn: true) }
        relayout()
    }

    private func rebuildRows() {
        rows.forEach { $0.removeFromSuperview() }
        rows = targets.prefix(9).enumerated().map { i, s in
            let row = TargetRow(session: s, index: i)
            row.isSelected = (i == targetIndex)
            row.onClick = { [weak self] in self?.pick(i) }
            listBox.addSubview(row)
            return row
        }
    }

    private func cycle(forward: Bool) {
        guard targets.count > 1 else { return }
        let next = (targetIndex + (forward ? 1 : targets.count - 1)) % targets.count
        pick(next, closeList: false)
    }

    private func pick(_ i: Int, closeList: Bool = true) {
        guard targets.indices.contains(i) else { return }
        targetIndex = i
        stickyID = targets[i].id
        stickyBase = lastKnownCurrentID
        Config.shared.lastTargetID = targets[i].id
        Config.shared.save()
        for (n, row) in rows.enumerated() { row.isSelected = (n == i) }
        updateTargetLabel()
        if closeList && listOpen { listOpen = false; relayout() }
    }

    private func updateTargetLabel() {
        let s = NSMutableAttributedString()
        if let t = currentTarget {
            s.append(NSAttributedString(string: "● ", attributes: [
                .foregroundColor: t.isClaude ? Style.accent : NSColor.tertiaryLabelColor,
                .font: NSFont.systemFont(ofSize: 10),
            ]))
            var name = t.label
            if name.count > 40 { name = String(name.prefix(40)) + "…" }
            s.append(NSAttributedString(string: name, attributes: [
                .foregroundColor: NSColor.secondaryLabelColor,
                .font: NSFont.systemFont(ofSize: Style.hintSize),
            ]))
            if targets.count > 1 {
                s.append(NSAttributedString(string: "  \(targetIndex + 1)/\(targets.count)", attributes: [
                    .foregroundColor: NSColor.tertiaryLabelColor,
                    .font: NSFont.monospacedDigitSystemFont(ofSize: Style.hintSize - 0.5, weight: .regular),
                ]))
            }
        } else {
            s.append(NSAttributedString(string: scanning ? L.t.scanning : L.t.noSession, attributes: [
                .foregroundColor: NSColor.tertiaryLabelColor,
                .font: NSFont.systemFont(ofSize: Style.hintSize),
            ]))
        }
        targetLabel.attributedStringValue = s
    }

    // MARK: - Hint row

    private func resetHint() {
        hintResetWork?.cancel()
        hints.isHidden = false
        hints.needsDisplay = true
    }

    private func setHint(_ text: String, warn: Bool) {
        hintResetWork?.cancel()
        guard warn else { resetHint(); return }
        // When something breaks, the whole keycap row gives way to the reason. That is what the user needs then, not shortcuts.
        hints.isHidden = true
        targetLabel.attributedStringValue = NSAttributedString(string: "⚠ " + text, attributes: [
            .foregroundColor: Style.accent,
            .font: NSFont.systemFont(ofSize: Style.hintSize, weight: .medium),
        ])
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
        if listOpen {
            let next = max(0, min(rows.count - 1, targetIndex + delta))
            pick(next, closeList: false)
            return true
        }
        let hist = Config.shared.history
        guard !hist.isEmpty else { return false }
        if delta < 0 {
            guard textView.string.isEmpty || historyCursor >= 0 else { return false }
            historyCursor = min(hist.count - 1, historyCursor + 1)
            textView.string = hist[hist.count - 1 - historyCursor]
        } else {
            guard historyCursor >= 0 else { return false }
            historyCursor -= 1
            textView.string = historyCursor < 0 ? "" : hist[hist.count - 1 - historyCursor]
        }
        textView.setSelectedRange(NSRange(location: textView.string.count, length: 0))
        relayout()
        return true
    }

    // MARK: - Sending

    private func submit() {
        let text = textView.string.trimmingCharacters(in: .whitespacesAndNewlines)
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

        textView.string = ""
        historyCursor = -1
        idleWork?.cancel()
        danceWork?.cancel()
        mascot.play("cheer")

        DispatchQueue.global(qos: .userInitiated).async {
            let err = ITerm.send(text, to: target.id)
            guard let err else { return }
            DispatchQueue.main.async { self.restoreAfterFailure(text: text, error: err) }
        }
        // Let the jump finish before closing: an action needs a result, or pressing Enter feels like nothing happened.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.18) { [weak self] in self?.hide() }
    }

    /// Give the text back when the send fails. Someone typed two hundred characters; an iTerm hiccup must not swallow them.
    private func restoreAfterFailure(text: String, error: String) {
        show()
        textView.string = text
        textView.setSelectedRange(NSRange(location: text.count, length: 0))
        relayout()
        setHint(error, warn: true)
    }

    /// Send a string to the current target without opening the panel.
    /// Used by clawdline://send?text=… so external tools (Shortcuts, Stream Deck, scripts) can push
    /// text in — and so "does the whole path work" can be verified with nobody at the keyboard.
    func sendDirect(_ text: String) {
        let body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty else { return }
        DispatchQueue.global(qos: .userInitiated).async {
            let snap = ITerm.snapshot()
            var list = snap.claudeSessions
            if list.isEmpty { list = snap.sessions }
            let target = list.first(where: { $0.id == snap.currentID })
                ?? list.first(where: { $0.id == Config.shared.lastTargetID })
                ?? list.first
            guard let target else {
                Log.write("sendDirect: no target (\(snap.error ?? "the list was empty"))")
                return
            }
            let err = ITerm.send(body, to: target.id)
            Log.write("sendDirect → \(target.label): \(err ?? "ok")")
        }
    }

    /// Render what the panel draws into a PNG.
    /// The reason is practical: `screencapture` needs Screen Recording permission, and without it
    /// the result is the wallpaper with every window stripped out — which leaves you blind while
    /// tuning layout. This path only paints our own layers, so it needs no permission at all.
    func snapshot(to path: String, routine: String? = nil, at time: Double? = nil) {
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

        if panel.isVisible {
            render()
        } else {
            show()
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.55) {
                render()
                self.hide()
            }
        }
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

        // A stand-in target. Real tab titles would publish what the user happens to be working on.
        let fake = NSMutableAttributedString()
        fake.append(NSAttributedString(string: "● ", attributes: [
            .foregroundColor: Style.accent, .font: NSFont.systemFont(ofSize: 10)]))
        fake.append(NSAttributedString(string: "✳ my-project", attributes: [
            .foregroundColor: NSColor.secondaryLabelColor,
            .font: NSFont.systemFont(ofSize: Style.hintSize)]))
        fake.append(NSAttributedString(string: "  2/3", attributes: [
            .foregroundColor: NSColor.tertiaryLabelColor,
            .font: NSFont.monospacedDigitSystemFont(ofSize: Style.hintSize - 0.5, weight: .regular)]))

        let margin: CGFloat = 64
        textView.string = ""
        relayout()
        let panelSize = container.bounds.size
        let canvas = NSSize(width: panelSize.width + margin * 2, height: panelSize.height + margin * 2)

        let total = Int(seconds * fps)
        for i in 0..<total {
            let t = Double(i) / fps
            let step = Self.timeline(script: script, t: t, seconds: seconds, text: text)

            textView.string = step.text
            mascot.play(step.routine, then: step.routine)
            mascot.frozenTime = step.mascotTime
            relayout()
            targetLabel.attributedStringValue = fake

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
        textView.string = ""
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
    private static func timeline(script: String, t: Double, seconds: Double, text: String) -> Step {
        var s = Step()

        if script == "dance" {
            s.routine = "dance"
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

    func revealCurrentTarget() {
        guard let t = currentTarget else { return }
        ITerm.reveal(t.id)
    }
}
