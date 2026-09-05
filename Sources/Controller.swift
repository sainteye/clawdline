import AppKit

/// One frame of a clip, written to disk in a format that never varies.
///
/// **This exists because of a bug that made a picture lie, and the lie was invisible.** Frames
/// used to be drawn into an `NSImage` and written out through `tiffRepresentation`, and an
/// `NSImage` picks its depth and its colour space from what is drawn into it and from the screen
/// it is drawn on — per frame. In `picker-live.gif` that changed halfway through: the frames
/// before the mascot pack changed came out eight bits a sample tagged with this monitor's own
/// profile, the ones after it came out sixteen bits tagged Display P3. ffmpeg's image-sequence
/// reader cannot change format mid-stream, so it stopped at the last frame of the first format
/// and wrote a GIF of precisely the half of the clip in which nothing happens — a clip captioned
/// "the character on the bar changes" that had been cut off immediately before it changed. The
/// file existed, its first frame was right, and nothing anywhere said it was half a clip.
///
/// So the buffer is described here instead: sRGB, eight bits a sample, two pixels to the point,
/// the same for every frame of every clip. That also makes good on what `filmstrip` claims about
/// itself — that it reproduces on every rerun — which it could not do while the colour space of
/// the output came from whichever display the machine happened to have.
enum FilmFrame {

    /// Two, rather than the screen's own factor, for the same reason as everything else here: a
    /// clip shot on a machine without a Retina display has to come out the same size as one shot
    /// on a machine with one, or the README's images change size with the contributor.
    static let scale: CGFloat = 2

    static func write(size: NSSize, to path: String, _ paint: () -> Void) {
        let w = Int((size.width * scale).rounded()), h = Int((size.height * scale).rounded())
        guard w > 0, h > 0,
              let space = CGColorSpace(name: CGColorSpace.sRGB),
              let cg = CGContext(data: nil, width: w, height: h, bitsPerComponent: 8,
                                 bytesPerRow: 0, space: space,
                                 bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        else { return }
        cg.scaleBy(x: scale, y: scale)
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(cgContext: cg, flipped: false)
        paint()
        NSGraphicsContext.restoreGraphicsState()
        // **Every frame comes out opaque, and this is what makes sure of it.** Each script fills
        // the whole canvas before it draws anything, so these frames are opaque by construction —
        // except along an antialiased edge, where compositing left a hairline of alpha 192 down
        // both sides of the island. That hairline was enough: `paletteuse` reserved a transparent
        // entry for it, which changed the way the GIF encodes its inter-frame differences, and
        // the clip stopped decoding as whole frames. A clip you cannot read frame by frame cannot
        // be checked, and checking is the entire reason these are drawn rather than recorded.
        //
        // Painted *behind* what is already there rather than as a flag on the buffer: a context
        // without an alpha channel takes a slow path through AppKit's image drawing, and the same
        // storyboard went from a fifth of a second a frame to three and a half seconds.
        cg.setBlendMode(.destinationOver)
        cg.setFillColor(gray: 0, alpha: 1)
        cg.fill(CGRect(origin: .zero, size: size))
        guard let image = cg.makeImage(),
              let png = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:])
        else { return }
        try? png.write(to: URL(fileURLWithPath: path))
    }
}

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
    private var micButton: MicButton!
    /// Made when somebody first dictates. See the note on `Voice.engine`: this used to be built
    /// with the panel, and everything it owns was therefore built at launch.
    private lazy var voice = Voice()
    private var scroll: NSScrollView!
    private var textView: PromptTextView!
    private var hintLine: NSView!
    private var listTopLine: NSView!
    private var listBox: NSView!
    private var outputHost: NSScrollView!
    private var outputView: NSTextView!
    private var outputLine: NSView!
    private let imagePreview = SessionImagePreview()
    /// What the session is doing right now. Hidden when it is not doing anything.
    private var activityLabel: ActivityLabel!
    /// The heading's own ground, so it reads as the top of the pane rather than as the bottom
    /// of the input row — which is what it looked like sitting on the card's own colour.
    private var paneHeader: NSView!
    private var targetLabel: NSTextView!
    /// The project's pixel mark, drawn to the left of its name.
    private var iconView: ProjectIconView!
    private var hints: KeyHintsView!
    /// What the microphone is doing, in the footer's right-hand slot. Its own label rather than
    /// the shared hint line: that one replaces the whole footer, so the project name and the
    /// keys vanished and the bar looked emptied out at exactly the moment it was busiest.
    private var voiceLabel: NSTextField!
    private var hintsAll: [KeyHintsView.Hint] = []

    private var targets: [TargetSession] = []
    private var rows: [TargetRow] = []
    private var targetIndex = 0
    /// Which list the panel is showing, if any. They share the same nine-row surface and keys.
    private enum ListMode { case none, sessions, mascots, stacks, skills }
    private var listMode: ListMode = .none {
        didSet {
            guard listMode != oldValue else { return }
            // Set here rather than at each of the six places that close a list, because the one
            // that gets forgotten is the leak.
            syncSessionStatePolling()
        }
    }
    /// The live line each session last showed, by session id.
    ///
    /// It is the same reading the session list makes — `SessionState.working` carries it — kept
    /// so that switching can put the strip up in the *same* turn as the text. Reading it fresh
    /// costs a round trip to the terminal, which used to land a beat after the pane had already
    /// been painted: the conversation appeared, and then a moment later a line arrived above it
    /// and shoved the whole thing down. One late strip is more noticeable than a slow pane.
    private var activityCache: [String: String] = [:]
    /// What each session is doing, by session id.
    ///
    /// Thrown away when the panel goes down, and deliberately: a reading is only true for about
    /// as long as it took to make. Keeping it would mean opening the bar an hour later onto a
    /// row that says a session is waiting for you when it has long since been answered — and a
    /// mark that is confidently wrong is worse than the plain row this replaces.
    private var sessionStates: [String: SessionState] = [:]
    /// What the rows were last drawn against — see the guard in `applyWatchedStates`.
    private var sessionAgents: [String: [Subagents.Agent]] = [:]
    private var sessionShells: [String: [Shells.Shell]] = [:]
    private var sessionLabels: [String: String] = [:]
    private var mascotNames: [String] = []
    private var mascotIndex = 0
    /// Skills for the selected assistant session, and the subset matching what follows `/` or `$`.
    /// The catalog is cached per target: reading frontmatter or a rollout header is cheap when a
    /// session changes and still needless work on every letter somebody types.
    private var skillCatalog: [AssistantSkill] = []
    private var skillMatches: [AssistantSkill] = []
    private var skillIndex = 0
    private var skillTargetID: String?
    private var skillLoadingTargetID: String?
    private var skillLoadToken = 0
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
    /// The project mark for every session in the list, as an image, by session id.
    ///
    /// Separate from `iconCache`, which only ever holds the *selected* session's grid because
    /// that is all the footer needed. A list needs one per row, and working out which project a
    /// session is in costs a process listing and an `lsof` on a cold cache — so it is resolved
    /// once, off the main thread, and the rows are rebuilt when it lands.
    private var rowIcons: [String: NSImage] = [:]
    private var statusCache: [String: ProjectStatus.Snapshot] = [:]
    /// The project's own servers, when it describes them (`.devstack.json`). Keyed by repository
    /// root rather than by session, because two sessions in one repository are looking at one
    /// set of servers — and because the panel has to show projects no session is sitting in.
    private var stackSpecCache: [String: DevStack.Spec] = [:]
    private var stackCache: [String: DevStack.State] = [:]
    /// Which repository root each session's stack was found in — the join between the two.
    private var stackRootOfSession: [String: String] = [:]
    /// The stack list, in the order it is shown. Rebuilt when the list opens, refreshed while
    /// it is open — a panel of servers that stops updating while you look at it is a panel you
    /// have to close and reopen to trust.
    private var stackRows: [DevStack.Spec] = []
    private var stackBusy: Set<String> = []
    /// The project whose button has been pressed once and is waiting to be confirmed. Cleared
    /// whenever the panel closes, so an agreement never survives out of sight of what it was about.
    private var armedStack: (root: String, stop: Bool)?
    /// A stack's log, while it is what the ⌘J pane is showing. Non-nil parks the transcript
    /// refresh — otherwise the next poll would paint the conversation over the log a second
    /// after it arrived, which reads as the button not working.
    private var stackLog: NSAttributedString?
    /// Which stack's log is on screen, and which of its processes — nil meaning all of them.
    private var stackLogSpec: DevStack.Spec?
    private var stackLogProcess: String?
    /// The whole stack's log, fetched once. Tabs slice this rather than fetching again —
    /// `process-compose process logs` costs a flat second per call however little you ask it
    /// for, so paying that per tab made switching feel broken.
    private var stackLogRaw: String?
    /// Pinned to the top of the log pane. See PaneHeader for why it is a view and not the
    /// first two lines of the text.
    private var floatingHeaderView: PaneHeader?
    private var savedOutputInset: NSSize?
    /// Which of the current session's background agents the pane is reading, if any.
    ///
    /// **The session it belongs to is not put down to read it.** The target does not move, the
    /// bar keeps saying what the session is doing, and coming back is this going to nil rather
    /// than anything being opened again. Cleared whenever the target changes: an agent belongs
    /// to the session that sent it away, and carrying one across would put another session's
    /// background work under the wrong name.
    private var agentID: String?
    /// What the strip above the transcript last drew, so a beat that changes nothing costs
    /// nothing. See `updateAgentStrip`.
    private var agentStripState = ""
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
    private var outputOpen = false {
        didSet {
            guard outputOpen != oldValue else { return }
            syncSessionStatePolling()
        }
    }
    /// The keycap row costs most of the footer's width to say things you learn once. Collapsed
    /// to a single ⌘/ until asked for.
    private var keysShown = false
    /// When the panel went away because the user switched apps rather than dismissed it.
    /// Only what was hidden this way comes back — Esc and sending mean closed, and something
    /// you shut on purpose reappearing on its own is the app arguing with you.
    ///
    /// **A moment, not a flag.** This was a `Bool`, and a `Bool` has no way to go stale: leave
    /// the panel open, click into a browser, and spend the afternoon there, and it was still
    /// armed hours later. Nothing cleared it but the terminal coming forward — so the next time
    /// that happened, on a trip that had nothing to do with the panel and long after anyone had
    /// forgotten it was open, the panel let itself in. Which reads, correctly, as "switching to
    /// iTerm2 summons Clawdline", and is the opposite of what this is for.
    ///
    /// Stepping away is a short thing. Past `returnWindow` it is not a return, it is a new
    /// visit, and a window that shows up uninvited on a new visit is the app arguing with you.
    private var hiddenByAppSwitch: Date?

    /// How long "I am looking at something for a moment" lasts.
    ///
    /// Long enough to check a browser tab or read a message and come back with the thing you
    /// left half-typed still there; short enough that it has clearly expired by the time you
    /// have started doing something else.
    private static let returnWindow: TimeInterval = 60
    /// True while a system permission sheet is up, so losing the key window is not read as the
    /// user going elsewhere. Set from `Voice.onPermissionPrompt`.
    private var awaitingPermission = false
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
    /// Queued work that lays out the sessions either side of the selected one. Cancelled and
    /// re-armed on every move, so holding ↓ through eight tabs does not start eight of them.
    private var prefetchWork: DispatchWorkItem?
    /// A session the bar was asked to open on, waiting for the scan that will contain it.
    private var pendingFocusID: String?
    /// Set when the bar was asked to come up on its session list rather than on the box.
    private var pendingShowList = false
    /// While set, the list shows invented sessions and nothing replaces them. Snapshots only.
    private var standInList = false
    /// Where the stand-in list is in its little story. Negative means "the still".
    private var standInTime: Double = -1
    private var standInRow: (label: String, state: SessionState, hue: Int, project: String)?
    private var spinnerTimer: Timer?

    private var currentTarget: TargetSession? {
        guard targets.indices.contains(targetIndex) else { return nil }
        return targets[targetIndex]
    }

    var isVisible: Bool { panel.isVisible && !dismissing }

    // MARK: - Construction

    private override init() {
        super.init()
        buildPanel()
        SessionWatch.shared.observers["panel"] = { [weak self] in self?.applyWatchedStates() }
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
        //
        // Not .stationary, whatever the name suggests it would help with: that flag means "stay
        // put during Exposé", and Show Desktop is Exposé. With it set, everything on screen slid
        // away and the panel stayed behind, sitting on the wallpaper.
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
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

        micButton = MicButton()
        micButton.toolTip = L.t.hintVoice
        // Updated on every show, because installing whisper does not restart this app.
        refreshVoiceTooltip()
        micButton.onClick = { [weak self] in self?.toggleVoice() }
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

        // `ActivityLabel` rather than a plain field: it is the only label in the bar that
        // is sometimes a control, and it carries its own pointer for the times it is.
        activityLabel = ActivityLabel(labelWithString: "")
        activityLabel.font = NSFont.monospacedSystemFont(ofSize: Style.hintSize, weight: .regular)
        activityLabel.textColor = Style.accent
        activityLabel.lineBreakMode = .byTruncatingTail
        activityLabel.isHidden = true
        // **"2 in the background" is the only thing in the bar that answers "on what", and until
        // now it was the end of the road**: the agents themselves live in the ⌘J pane, and a
        // reader who had never opened that pane had no way to know it. Clicking the line opens
        // it. Only when there is something to open — on a session with no agents this is the
        // live line and nothing else, and a line that swallows clicks for no reason is worse
        // than one that ignores them.
        activityLabel.addGestureRecognizer(
            NSClickGestureRecognizer(target: self, action: #selector(activityClicked)))
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
            .init(key: "⌘S", label: L.t.hintStacks),
            .init(key: "⌘F", label: L.t.hintFullscreen),
            .init(key: "⌘L", label: L.t.hintVoice),
            .init(key: "⌘R", label: L.t.hintOrder),
            .init(key: "⌘+", label: L.t.hintTextSize),
        ]

        iconView = ProjectIconView()
        card.addSubview(iconView)

        voiceLabel = NSTextField(labelWithString: "")
        voiceLabel.font = NSFont.systemFont(ofSize: Style.hintSize, weight: .medium)
        voiceLabel.alignment = .right
        voiceLabel.lineBreakMode = .byTruncatingTail
        voiceLabel.isHidden = true
        card.addSubview(voiceLabel)

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
        textView.onToggleStacks = { [weak self] in self?.showList(.stacks) }
        textView.onStopIndex = { [weak self] i in self?.stopStack(i) }
        // Through `choose`, not straight to `pick`: ⌘n means "the nth row of whatever is open",
        // and with no list open that is still a session. Wired to `pick` directly, ⌘n over the
        // stack list quietly switched sessions instead — the list looked inert, and the only
        // way to act on a stack was a key that appeared to do nothing.
        textView.onPickIndex = { [weak self] i in self?.choose(i) }
        textView.onToggleDance = { [weak self] in self?.toggleDance() }
        textView.onToggleOutput = { [weak self] in self?.toggleOutput() }
        textView.onToggleFullscreen = { [weak self] in self?.toggleFullscreen() }
        textView.onToggleOrder = { [weak self] in self?.toggleOutputOrder() }
        textView.onToggleKeys = { [weak self] in self?.toggleKeys() }
        textView.onToggleVoice = { [weak self] in self?.toggleVoice() }
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
            self?.updateSkillSuggestions()
            self?.relayout()
            self?.noteTyping()
        }
        textView.onArrow = { [weak self] delta in self?.handleArrow(delta) ?? false }
        textView.onAcceptSuggestion = { [weak self] in self?.acceptSkill() ?? false }
    }

    // MARK: - Show and hide

    func toggle() {
        if isVisible { hide() } else { show() }
    }

    /// Open the bar already pointed at a particular session.
    ///
    /// For the island and the menu bar, whose whole message is "*this one* wants you" — arriving
    /// at the bar and then having to find which of nine rows it meant would undo the point of
    /// having been told. The id is remembered rather than applied, because the session scan is
    /// asynchronous and there is nothing to select until it lands.
    func show(focusing sessionID: String?) {
        pendingFocusID = sessionID
        if isVisible {
            applyPendingFocus()
            NSApp.activate(ignoringOtherApps: true)
            panel.makeKeyAndOrderFront(nil)
        } else {
            show()
        }
    }

    /// Open the bar with the session list already up, the way ⌘K leaves it.
    ///
    /// The island's menu lists what is *running*; this lists everything, which is the question
    /// the number cannot answer and the reason there is a way out of that menu into here.
    func showSessionList() {
        pendingShowList = true
        if isVisible { applyPendingList() } else { show() }
    }

    private func applyPendingList() {
        guard pendingShowList else { return }
        pendingShowList = false
        if listMode != .sessions { showList(.sessions) }
        NSApp.activate(ignoringOtherApps: true)
        panel.makeKeyAndOrderFront(nil)
        panel.makeFirstResponder(textView)
    }

    /// Select the session the island was pointing at, once the scan has produced it.
    private func applyPendingFocus() {
        guard let wanted = pendingFocusID else { return }
        guard let i = targets.firstIndex(where: { $0.id == wanted }) else { return }
        pendingFocusID = nil
        pick(i, closeList: false)
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
        refreshVoiceTooltip()
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
        guard isTerminal, let since = hiddenByAppSwitch, !panel.isVisible else { return }
        hiddenByAppSwitch = nil
        // Cleared either way: whether or not this trip counts as coming back, the arming has now
        // been answered. Leaving it set is what let one forgotten switch reopen the panel on
        // every visit to the terminal thereafter.
        let away = Date().timeIntervalSince(since)
        guard away < Self.returnWindow else {
            Log.write("reopen declined: away \(Int(away))s, longer than a moment")
            return
        }
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
        imagePreview.close()
        // Returning focus is what every deliberate dismissal does — Esc, sending, the hotkey —
        // and a deliberate dismissal means closed. Only the app-switch path (returnFocus: false)
        // leaves the return armed.
        //
        // Clearing it here rather than when the panel comes back: a return that gets skipped —
        // the commonest being coming back while the last dismissal is still animating out —
        // used to leave the flag set for the rest of the session, and the next time you closed
        // the panel by hand it would let itself back in.
        if returnFocus { hiddenByAppSwitch = nil }
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
            self.syncSpinner()
            self.prefetchWork?.cancel()
            Transcript.forgetRenders()
            // Where each session's transcript is, and what each agent was last doing. Both are
            // only ever asked for on behalf of something on screen, and both are keyed on files
            // that a shut panel has no opinion about.
            Subagents.forget()
            Shells.forget()
            // And which agent was being read. A panel that comes back up should come back to the
            // conversation, not to the middle of somebody else's errand from an hour ago.
            self.agentID = nil
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
        // A permission sheet takes the key window away exactly like an app switch does, and the
        // grace period below is far too short to cover one — nobody answers in 0.4 seconds.
        // Treating it as a switch hid the panel the first time anyone dictated, and took the
        // microphone with it, so granting the permission bought nothing.
        guard !awaitingPermission else { return }
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
        hiddenByAppSwitch = Date()
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
        micButton.frame = NSRect(x: W - Style.padH - micW, y: H - Style.inputPadV - 28,
                                 width: micW, height: 28)
        scroll.frame = NSRect(x: tx, y: H - inputH + Style.inputPadV,
                              width: W - tx - Style.padH - micW - 6,
                              height: inputH - Style.inputPadV * 2)

        hintLine.frame = NSRect(x: 0, y: Style.hintHeight, width: W, height: 1)

        let speaking = !voiceLabel.stringValue.isEmpty
        voiceLabel.isHidden = !speaking
        hints.isHidden = hints.isHidden || speaking
        let hintsW = speaking
            ? min(voiceLabel.intrinsicContentSize.width + 4, W - Style.padH * 2 - 120)
            : min(hints.intrinsicWidth, W - Style.padH * 2 - 120)
        let hintsX = W - Style.padH - hintsW
        hints.frame = NSRect(x: hintsX, y: 0, width: hintsW, height: Style.hintHeight)
        voiceLabel.frame = NSRect(x: hintsX, y: (Style.hintHeight - 16) / 2,
                                  width: hintsW, height: 16)

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
        p.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]

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
        // ⌘J on a stack's log hands the pane back to the transcript rather than closing it.
        //
        // This is the way out. Without it the log was a room with no door: the key that opened
        // the pane shut it, reopening showed the log again — because the log is what the pane
        // now held — and the conversation never came back at all.
        if stackLog != nil, outputOpen {
            clearStackLog()
            return
        }
        // Same door, for the same reason: an agent's transcript is another room inside the pane,
        // and the key that got you in should give the conversation back before it shuts the pane.
        if agentID != nil, outputOpen {
            clearAgent()
            return
        }
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
        // The pane's strip is fed by the same reading the list uses, so opening it turns that
        // reading on. Here as well as in the property, because a summon that comes back with the
        // pane already open changes nothing to observe.
        syncSessionStatePolling()
        refreshOutput()
        let t = Timer(timeInterval: 1.2, repeats: true) { [weak self] _ in self?.refreshOutput() }
        RunLoop.main.add(t, forMode: .common)
        outputTimer = t
    }

    /// How many agents this session has out right now.
    private func runningAgents(of id: String) -> Int {
        SessionWatch.shared.agents(of: id).filter(\.isRunning).count
    }

    /// How many commands it left running right now — see ``Shells``.
    private func runningShells(of id: String) -> Int {
        SessionWatch.shared.shells(of: id).count
    }

    /// What the strip above the transcript says: the live line, and what is happening away from
    /// the screen it was read off.
    ///
    /// **Either half can be the whole of it.** A session between turns with two agents still out
    /// has no live line at all — the terminal is showing a prompt — and that is precisely the
    /// state where the strip saying nothing was a lie by omission.
    private func activityLine(for id: String) -> String? {
        let live = activityCache[id]
        // Both kinds of off-screen work, in the order they cost somebody something: an agent runs
        // while its session is busy anyway, and a shell is the one that outlives the turn.
        let away = [runningAgents(of: id), runningShells(of: id)]
        let said = [away[0] > 0 ? agentsSaid(away[0]) : nil,
                    away[1] > 0 ? shellsSaid(away[1]) : nil].compactMap { $0 }
        guard !said.isEmpty else { return live }
        let tail = said.joined(separator: "  ·  ")
        guard let live, !live.isEmpty else { return tail }
        return live + "  ·  " + tail
    }

    /// Show or clear the "what is it doing right now" strip.
    private func setActivity(_ text: String?) {
        // Whether the line leads anywhere, which is not the same question as what it says: a
        // session working on its own has a live line and nowhere to go from it.
        activityLabel.clickable = currentTarget.map {
            !SessionWatch.shared.agents(of: $0.id).isEmpty
        } ?? false
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
        guard cannedTranscript == nil, stackLog == nil else { return }
        guard outputOpen, panel.isVisible, let target = currentTarget else { return }
        // The agents this session has out, above whatever the pane is showing — and, when one of
        // them is open, the pane is showing that agent instead of the session.
        updateAgentStrip(for: target)
        if let id = agentID {
            refreshAgentTranscript(for: target, agent: id)
            return
        }
        // Not `.utility`. This runs because somebody pressed ↓, and on a machine with nine
        // sessions on it — which is the machine this is for — background priority is the
        // difference between the pane keeping up with the key and lagging a beat behind it.
        DispatchQueue.global(qos: .userInitiated).async {
            // The transcript is the better source when there is one: it has the message
            // boundaries the screen only implies, and it is not truncated to a viewport.
            if Config.shared.outputMode != "terminal",
               let rendered = self.renderTranscript(for: target) {
                DispatchQueue.main.async {
                    // Whoever this was read for may not be who is selected by the time it
                    // lands: holding ↓ starts one of these per session passed through, and
                    // without this the pane could finish by painting a conversation you had
                    // already moved off — confidently, under the next session's name.
                    guard self.currentTarget?.id == target.id else { return }
                    // And the pane may have been handed a stand-in while this was in flight.
                    // The check at the top of this function is made before the read; a read
                    // that started a fraction earlier lands *after* the canned transcript and
                    // overwrites it — which is how a real conversation got into a picture
                    // bound for the README, twice, with the guard already there.
                    guard self.cannedTranscript == nil, self.stackLog == nil else { return }
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
                // The live line used to be scraped here, with a round trip of its own, after the
                // text had already been handed over — which is exactly why it arrived late and
                // pushed the conversation down on its way in. The state poller reads every
                // session's screen on the same 1.2s cadence and keeps the answer, so by the time
                // this runs the strip is already up and this path has nothing to do.
                return
            }
            guard Config.shared.outputMode != "transcript" else { return }
            let raw = Targets.screenWithHistory(of: target)
            // Trailing blank lines are most of what a terminal screen is; dropping them puts
            // the last real line at the bottom, which is where the eye goes.
            var lines = (raw ?? "").split(separator: "\n", omittingEmptySubsequences: false)
            let blank: (Substring) -> Bool = { $0.trimmingCharacters(in: .whitespaces).isEmpty }
            while lines.first.map(blank) == true { lines.removeFirst() }
            while lines.last.map(blank) == true { lines.removeLast() }
            let text = lines.joined(separator: "\n")
            DispatchQueue.main.async {
                guard self.currentTarget?.id == target.id, self.outputOpen else { return }
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
        guard let record = Transcript.record(of: target) else { return nil }
        let file = record.url

        let folds = expandedFolds
        let newestFirst = Config.shared.outputNewestFirst
        let size = Config.shared.outputSize
        let signature = Transcript.signature(of: file)
            + "-\(size)-\(newestFirst)"
            + "-\(folds.sorted().joined(separator: ","))"

        // Worked out before anything is read, because it is also the answer to "has this
        // changed": a session you switched away from and came back to is one stat away from
        // being on screen, rather than eight megabytes and a re-layout away.
        if let kept = Transcript.cachedRender(for: signature),
           SessionImagePresentation.cacheIsCurrent(kept) {
            return (kept, signature + "-" + SessionImagePresentation.cacheSignature(kept))
        }

        // Eight megabytes, because the limit that bites is bytes and not entries: at the
        // 400KB this used to read, a busy session yielded sixteen records and the reader
        // hit the top of the pane almost immediately.
        guard let text = Transcript.tail(of: file, bytes: 8_000_000) else { return nil }
        let entries = Transcript.parse(text, assistant: record.assistant)
        guard !entries.isEmpty else { return nil }
        let rendered = Transcript.render(entries, size: size, mono: Style.outputFont,
                                         expanded: folds, newestFirst: newestFirst)
        Transcript.remember(rendered, for: signature)
        return (rendered, signature + "-" + SessionImagePresentation.cacheSignature(rendered))
    }

    /// A folded run of tool calls was clicked. The pane is read-only, so a link is the only
    /// thing in it that can be clicked at all — which is why folds are links rather than, say,
    /// a disclosure triangle drawn into the text.
    func textView(_ view: NSTextView, clickedOnLink link: Any, at index: Int) -> Bool {
        guard let url = (link as? URL)?.absoluteString ?? link as? String else { return false }
        if let id = SessionImagePresentation.artifactID(url) {
            switch SessionImageArtifactStore().lookup(id: id) {
            case .live(_, let data):
                imagePreview.show(data: data, over: panel, returnFocus: outputView)
            case .expired, .missing:
                Transcript.forgetRenders()
                lastOutput = nil
                refreshOutput()
            }
            return true
        }
        // The way out of the log pane, and the tabs across the top of it.
        if url == "clawdline://stacklog-back" { clearStackLog(); return true }
        if url.hasPrefix("clawdline://stacklog/") {
            let name = String(url.dropFirst("clawdline://stacklog/".count))
            if let spec = stackLogSpec { showStackLog(spec, process: name.isEmpty ? nil : name) }
            return true
        }
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
        var list = snap.assistantSessions
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
        // Asked for by the island or the menu bar before this list existed. It wins over every
        // rule above it, because it is the most explicit statement of intent there is: somebody
        // clicked the thing that named this session.
        if let wanted = pendingFocusID, let i = targets.firstIndex(where: { $0.id == wanted }) {
            targetIndex = i
            stickyID = wanted
            stickyBase = snap.currentID
            pendingFocusID = nil
        }

        // A `/` may have been typed while the target scan was still in flight. The working
        // directory, and therefore the skills, become knowable at exactly this point.
        updateSkillSuggestions()
        rebuildRows()
        applyPendingList()
        updateTargetLabel()
        refreshProjectInfo()
        // The session scan is async, so an output pane opened before it landed had no target
        // to read and gave up. Nothing retried it, and it stayed blank for good.
        if outputOpen { refreshOutput() }
        refreshRowIcons()
        // The watch has almost certainly read these screens already — it never stops — so the
        // rows can be marked from the reading in hand rather than sitting plain until the next
        // one comes round.
        if listMode == .sessions { applyWatchedStates() }
        syncSpinner()
        if let e = snap.error { setHint(e, warn: true) }
        relayout()
    }

    // MARK: - What each session is doing
    //
    // The list is the one place the bar shows every session at once, and every row on it used to
    // say the same kind of thing: a tab title, which is the task. Four tabs on tasks that read
    // alike are four identical rows — and the question you open that list with is not "what are
    // they called", it is "which one stopped, and which one wants something from me".
    //
    // Which is the half of this tool that was still missing. Not looking at the terminal works
    // for one session; with four, you were back to going round the tabs to find out who had
    // finished. The mark on the row is what makes the list answer that instead.

    /// Both readers of these screens: the list, which draws a mark per row, and the strip above
    /// the transcript pane, which needs the *selected* session's live line the instant it is
    /// switched to rather than a round trip later.
    ///
    /// One reading serves both, which is why the pane opting in costs nothing extra: it was
    /// already asking the terminal for the current session once a second on its own.
    /// Whether anything on screen is showing these readings. It does not decide *whether* they
    /// happen — `SessionWatch` reads all day for the menu bar and the island — only whether they
    /// happen at the pace of a keypress or at the pace of a background chore.
    private var shouldPollStates: Bool { listMode == .sessions || outputOpen }

    /// Keep the spinners turning.
    ///
    /// One timer for the whole list rather than one per row, and it only exists while there is
    /// something to animate: the phase is read from the shared clock inside `draw`, so a tick
    /// here is nothing but "everybody who is busy, redraw".
    private func syncSpinner() {
        let wanted = listMode == .sessions && panel.isVisible && rows.contains { $0.isBusy }
        guard wanted != (spinnerTimer != nil) else { return }
        if wanted {
            let t = Timer(timeInterval: PixelSpinner.step, repeats: true) { [weak self] _ in
                self?.rows.forEach { if $0.isBusy { $0.needsDisplay = true } }
            }
            RunLoop.main.add(t, forMode: .common)
            spinnerTimer = t
        } else {
            spinnerTimer?.invalidate()
            spinnerTimer = nil
        }
    }

    private func syncSessionStatePolling() {
        SessionWatch.shared.isForeground = shouldPollStates && panel.isVisible
    }

    /// Take the newest reading. Called by the watch, not by a timer of this class's own: four
    /// pollers asking the same terminal the same question was the thing worth avoiding.
    private func applyWatchedStates() {
        let states = SessionWatch.shared.states
        // The live lines are worth keeping whether or not the rows changed: this is the only
        // place the *other* sessions' screens are read, so it is the only chance to know what
        // their strip should say before one of them is switched to.
        for (id, state) in states {
            if case .working(let line) = state { activityCache[id] = line }
            else { activityCache.removeValue(forKey: id) }
        }
        let agents = SessionWatch.shared.agents
        let shells = SessionWatch.shared.shells
        let labels = Dictionary(uniqueKeysWithValues: targets.map { ($0.id, $0.displayLabel) })
        guard panel.isVisible else { return }
        // The strip follows the selected session even when the list is shut, because the pane is
        // open on its own account and its live line has to keep ticking.
        //
        // Except over a canned transcript: that is a picture of a stand-in, and a live line from
        // whatever this machine is really doing has no business being in it.
        if let id = currentTarget?.id, outputOpen, cannedTranscript == nil {
            setActivity(activityLine(for: id))
        }

        // Rows are rebuilt only when something actually moved. A list that rebuilds once a second
        // discards the view under the pointer every second with it, and an unchanged screen is
        // the common case: most of the time three of the four sessions are doing exactly what
        // they were doing a second ago.
        // **The agents are in this guard too.** Without them a session that was already
        // `working` could start and finish three of them without the list redrawing once: the
        // states map would be identical each time, and this returned before the rows were built.
        guard listMode == .sessions,
              states != sessionStates || agents != sessionAgents || shells != sessionShells
                || labels != sessionLabels else {
            return
        }
        sessionStates = states
        sessionAgents = agents
        sessionShells = shells
        sessionLabels = labels
        rebuildRows()
        syncSpinner()
        relayout()
    }

    /// Work out each session's project mark, off the main thread, and redraw once they are in.
    ///
    /// Only for sessions that do not have one yet: this runs on every scan, and the answer for a
    /// session that has not moved cannot have changed — a Claude Code session is started in a
    /// directory and stays there.
    private func refreshRowIcons() {
        let wanted = targets.prefix(9).filter { rowIcons[$0.id] == nil }
        guard !wanted.isEmpty else { return }
        // **Drawn on the main thread.** `NSImage.lockFocus` is main-thread work; done on a
        // background queue it does not fail loudly, it just hands back an image with nothing in
        // it — which is exactly what the rows showed the first time. The expensive half, working
        // out which project a session is in, already happened in the watch.
        var drew = false
        for target in wanted {
            guard let grid = SessionWatch.shared.grid(of: target.id),
                  let image = grid.image(height: 11) else { continue }
            rowIcons[target.id] = image
            drew = true
        }
        guard drew, listMode == .sessions else { return }
        rebuildRows()
        relayout()
    }

    /// Lay out the sessions either side of the selected one, before anybody asks for them.
    ///
    /// Reading a conversation costs about the same whenever it is done; the only question is
    /// whether it is done while a key is held down. Moving through the list is the one time it is
    /// possible to know what will be wanted next — it is the row above and the row below — so
    /// that work happens in the gap between keystrokes, and ↓ finds it already laid out.
    ///
    /// Nothing is given up for this. It writes only into the cache that the switch was going to
    /// consult anyway, at a priority below the pane the user is actually looking at, and after a
    /// pause long enough that running through eight tabs queues nothing at all.
    private func prefetchNeighbours() {
        prefetchWork?.cancel()
        guard outputOpen, listMode == .sessions, targets.count > 1,
              cannedTranscript == nil, Config.shared.outputMode != "terminal" else { return }
        let wanted = [targetIndex - 1, targetIndex + 1]
            .filter { targets.indices.contains($0) }
            .map { targets[$0] }
        guard !wanted.isEmpty else { return }

        let work = DispatchWorkItem { [weak self] in
            guard let self else { return }
            for target in wanted {
                guard self.outputOpen else { return }
                _ = self.renderTranscript(for: target)   // its own answer goes into the cache
            }
        }
        prefetchWork = work
        // Long enough that a held key never starts one, short enough to be ready before the
        // next deliberate press.
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 0.35, execute: work)
    }

    /// The stand-in rows, drawn by the same rules as the real ones.
    private func standInRowText(_ row: (label: String, state: SessionState, hue: Int, project: String),
                                selected: Bool) -> NSAttributedString {
        let s = NSMutableAttributedString()
        let base = NSFont.systemFont(ofSize: Style.listSize, weight: selected ? .medium : .regular)
        let small = NSFont.systemFont(ofSize: Style.listSize - 1.5)
        func add(_ text: String, _ colour: NSColor, _ font: NSFont) {
            s.append(NSAttributedString(string: text,
                                        attributes: [.font: font, .foregroundColor: colour]))
        }
        add(row.label, selected ? .labelColor : .secondaryLabelColor, base)
        _ = small
        return s
    }

    /// One session row: the persisted task title when there is one, and what its screen is doing.
    ///
    /// Nil when there is nothing to add, so the row falls back to the plain title it has always
    /// drawn — an idle session and one whose screen could not be read both look exactly as they
    /// did before, which is the honest drawing for "nothing to say" and for "no idea".
    private func sessionRowText(_ target: TargetSession, selected: Bool) -> NSAttributedString? {
        guard sessionStates[target.id] != nil else { return nil }
        let s = NSMutableAttributedString()
        let base = NSFont.systemFont(ofSize: Style.listSize, weight: selected ? .medium : .regular)
        let small = NSFont.systemFont(ofSize: Style.listSize - 1.5)
        func add(_ text: String, _ colour: NSColor, _ font: NSFont) {
            s.append(NSAttributedString(string: text,
                                        attributes: [.font: font, .foregroundColor: colour]))
        }

        add(target.displayLabel, selected ? .labelColor : .secondaryLabelColor, base)
        // Always name the assistant. A list containing only one kind is still ambiguous at a
        // glance — especially when several tabs share the same project name and therefore the
        // same project mark on the left. The product mark answers "Claude or Codex?" while that
        // left-hand pixel mark continues to answer "which project?".
        if let assistant = target.assistant {
            add("  ", .tertiaryLabelColor, small)
            if let image = assistant.logoImage(height: 11) {
                let attachment = NSTextAttachment()
                attachment.image = image
                attachment.bounds = NSRect(x: 0, y: -1.5, width: 11, height: 11)
                s.append(NSAttributedString(attachment: attachment))
                add(" ", .tertiaryLabelColor, small)
            }
            add(assistant.short, .tertiaryLabelColor, small)
        }
        return s
    }

    /// What the row says after its label. Split from the label so the row can put the spinner
    /// between the two and lay the three of them out itself.
    private func coordinationWaitSaid(for terminalID: String) -> String? {
        Self.coordinationWaitSaid(Orchestrator.coordination(forTerminal: terminalID)) { id in
            targets.first(where: { $0.id == id })?.displayLabel
        }
    }

    /// The same sentence, out of the registry's reach so the suite can hold it.
    ///
    /// Pure on purpose. What a row says about a file wait is a rule with three cases in it —
    /// blocked, blocking, and both at once — and a rule that can only be read by looking at a
    /// running app with two real terminals parked on each other is a rule nobody checks.
    /// `label` is how a session id becomes a name; it answers nil for a session this Mac cannot
    /// see, which is a relationship that outlived a tab rather than an error.
    /// `copy` is a parameter for the same reason `label` is: the sentence this rule picks
    /// differs by language — one waiter and many waiters are two sentences wherever the verb
    /// agrees with the count — and a rule that can only be read in English is a rule whose
    /// German half nobody checks.
    static func coordinationWaitSaid(_ coordination: Orchestrator.Coordination,
                                     copy: Copy = L.t,
                                     label: (String) -> String?) -> String? {
        let waits = coordination.waitingOn
        let owed = coordination.waitedOnBy
        /// The release condition of a wait, or the word for the thing that has to happen. The
        /// fallback is very nearly unreachable — registering rejects an empty condition — and it
        /// is deliberately the same on both sides, because a row that fell back on one side and
        /// not the other would read as two different features.
        func condition(_ wait: [String: Any]) -> String {
            wait["releaseCondition"] as? String ?? "release"
        }
        // What this session is holding up, in the app's own words. The count is the whole point:
        // a name here would be the wrong question answered, because the owner does not go and
        // ask anybody — the owner releases, and what they need to know is how many that frees.
        let owedSaid: String?
        if owed.isEmpty { owedSaid = nil }
        else if owed.count == 1 { owedSaid = copy.sessionWaitedOnByOne }
        else {
            owedSaid = copy.sessionWaitedOnByMany
                .replacingOccurrences(of: "{n}", with: "\(owed.count)")
        }

        if let first = waits.first {
            let ownerID = first["ownerSessionId"] as? String ?? ""
            let owner = label(ownerID) ?? (ownerID.isEmpty ? "Clawdline" : ownerID)
            let more = waits.count > 1 ? "  +\(waits.count - 1)" : ""
            // Waiting leads when a session is on both sides of the board — which is the rule the
            // API already states, since `coordination.state` is `waiting_on_session` whenever
            // `waitingOn` is not empty. The owed count still goes on the end rather than being
            // dropped: a session that waits on one peer while another waits on it is exactly the
            // row that used to say nothing about the half a person has to act on.
            return "⏳ \(owner) · \(condition(first))\(more)"
                + (owedSaid.map { "  ·  \($0)" } ?? "")
        }
        // The half nothing drew. The broker worked it out and the API sent it; both lists read
        // past it, so an owner's row looked exactly like a row in no relationship at all — and
        // the only other thing telling an owner is one message typed into a terminal that a
        // permission menu can swallow whole.
        //
        // No `+N` to go with the count, unlike the waiting side. There the leading field names
        // *one* peer and the suffix says how many were not named; here the leading field is
        // already all of them, and a `+N` beside it would be the same sessions counted twice.
        // The condition beside it belongs to the group the first row came from.
        guard let owedSaid, let first = owed.first else { return nil }
        return "⏳ \(owedSaid) · \(condition(first))"
    }

    private func sessionRowDetail(_ state: SessionState, selected: Bool,
                                  agents: Int = 0, shells: Int = 0,
                                  coordinationWait: String? = nil) -> NSAttributedString? {
        let s = NSMutableAttributedString()
        let small = NSFont.systemFont(ofSize: Style.listSize - 1.5)
        func add(_ text: String, _ colour: NSColor, _ font: NSFont) {
            s.append(NSAttributedString(string: text,
                                        attributes: [.font: font, .foregroundColor: colour]))
        }
        /// Both kinds of work happening off the screen, in one run of text. `leading` is about
        /// what came before them on the row, so only the first of the two ever asks for it.
        func addAway(_ colour: NSColor, _ font: NSFont, leading: Bool = true) {
            var first = leading
            for said in [agents > 0 ? agentsSaid(agents) : nil,
                         shells > 0 ? shellsSaid(shells) : nil].compactMap({ $0 }) {
                add((first ? "  ·  " : "") + said, colour, font)
                first = true
            }
        }
        func addCoordination(_ colour: NSColor, leading: Bool = true) {
            guard let coordinationWait else { return }
            add((leading ? "  ·  " : "") + coordinationWait, colour, small)
        }
        let away = agents + shells
        switch state {
        case .working(let line):
            // Quiet, deliberately. A session that is working wants nothing from you, and four
            // rows calling for attention at once is the same as none of them calling.
            //
            // A step less quiet on the selected row: it draws on a tinted background, and the
            // grey that reads as "in the background" against the card reads as illegible there.
            let quiet: NSColor = selected ? .secondaryLabelColor : .tertiaryLabelColor
            // Cut with a mark on it. The row clips whatever runs past its edge, and a sentence
            // that stops mid-word with nothing to say so reads as a bug rather than as a limit.
            // Shorter when something has to go after it. The row clips at its own edge either
            // way; what changes is which half survives, and "three agents are out" is worth more
            // than the last eleven characters of a sentence that is already ellipsised.
            let room = away > 0 ? 28 : 44
            add(line.count > room ? line.prefix(room - 1) + "…" : line, quiet, small)
            addCoordination(quiet)
            addAway(quiet, small)
        case .waiting:
            // The one loud thing in the list, because it is the only state that costs you
            // something for every second it goes unnoticed.
            add("● " + L.t.sessionWaiting, Style.accent, small)
            // Underneath it in the reading order and in the colour. A session can be waiting on
            // a permission dialog while three agents keep working, and the question is still the
            // only part of that anybody has to act on.
            addCoordination(.tertiaryLabelColor)
            addAway(.tertiaryLabelColor, small)
        case .idle, .unknown:
            // **Not nil any more, if anything is out.** An idle-looking session with three agents
            // still running is the exact case the screen gets wrong — the main agent is between
            // turns, nothing is on the terminal, and the work is elsewhere. That row said
            // nothing at all before.
            //
            // A command left running is the same mistake made worse. An agent at least belongs to
            // a turn that is still going; a background shell is what a *finished* turn leaves
            // behind, so this row is the one place anything says the build is still on.
            guard away > 0 || coordinationWait != nil else { return nil }
            addCoordination(.tertiaryLabelColor, leading: false)
            addAway(.tertiaryLabelColor, small, leading: coordinationWait != nil)
        }
        return s
    }

    /// "· 3 in the background", or nothing. Quiet in every state it can appear in: background
    /// work explains a session, it never asks anything of you.
    private func agentsSaid(_ count: Int) -> String {
        L.t.sessionAgents.replacingOccurrences(of: "{n}", with: "\(count)")
    }

    /// "· 1 shell running", or nothing. The same register as ``agentsSaid(_:)`` and for the same
    /// reason — this explains a session, it never asks anything of you — but it is the one that
    /// appears on a row with no spinner on it, which is the whole point: an idle row that has
    /// this on it is not a row that has finished.
    private func shellsSaid(_ count: Int) -> String {
        count == 1 ? L.t.sessionShellOne
                   : L.t.sessionShellMany.replacingOccurrences(of: "{n}", with: "\(count)")
    }

    /// Fill the list with invented sessions and pin it there, for the README picture.
    ///
    /// Same path as the real rows — `sessionRowText` draws them and `TargetRow` lays them out —
    /// so the picture cannot drift away from what the app does. It can only be entered from the
    /// snapshot URL, and the pinning is what stops the next reading from the watch replacing
    /// somebody's invented work with their real work half a second later.
    private func showStandInSessions(at t: Double = -1) {
        standInTime = t
        standInList = true
        // The footer is a copy of the terminal's status line — that is the point of it — so in a
        // picture that shows both, the two have to be looking at the same session. It used to
        // keep the *real* current project's mark while the rows and the status line moved.
        let rows = Self.standInSessions(at: t)
        let pick = t < 0 ? 1 : Self.standInSelection(at: t)
        standInRow = rows[min(pick, rows.count - 1)]
        // The footer names the project too, and it is the same problem one line down: a picture
        // of the list must not carry this machine's repository, branch and uncommitted count.
        usingStandInLabel = true
        updateTargetLabel()
        listMode = .sessions
        rebuildRows()
        relayout()
    }

    // MARK: - Assistant skills

    /// The incomplete skill mention currently occupying the whole box. `/` is Clawdline's common
    /// opener for both assistants; Codex's native `$` spelling works too. A space ends completion:
    /// from then on the words are arguments, and the ordinary Return-to-send path owns them.
    private var skillQuery: String? {
        guard let assistant = currentTarget?.assistant else { return nil }
        let text = textView.string
        let opens = text.hasPrefix("/") || (assistant == .codex && text.hasPrefix("$"))
        guard opens, !text.contains("\n") else { return nil }
        let query = String(text.dropFirst())
        guard !query.contains(where: { $0.isWhitespace }) else { return nil }
        return query
    }

    private func updateSkillSuggestions() {
        guard let query = skillQuery,
              let target = currentTarget, target.assistant != nil else {
            if listMode == .skills {
                listMode = .none
                skillMatches = []
                rebuildRows()
            }
            return
        }

        if skillTargetID == target.id {
            showSkillMatches(query)
            return
        }
        // One lookup per target while it is in flight. The callback rechecks both the token and
        // the selection: a slow `lsof` for the previous tab must not paint its skills under the
        // new one's prompt.
        guard skillLoadingTargetID != target.id else {
            if listMode != .skills { listMode = .skills; rebuildRows() }
            return
        }
        skillLoadToken += 1
        let token = skillLoadToken
        skillLoadingTargetID = target.id
        skillMatches = []
        skillIndex = 0
        listMode = .skills
        rebuildRows()
        relayout()

        DispatchQueue.global(qos: .userInitiated).async {
            let found: [AssistantSkill]
            switch target.assistant {
            case .claude:
                let cwd = Targets.workingDirectory(of: target)
                found = cwd.map { ClaudeSkills.available(cwd: $0) } ?? []
            case .codex:
                let record = Transcript.record(of: target)
                found = record.flatMap {
                    $0.assistant == .codex ? CodexSkills.available(in: $0.url) : nil
                } ?? []
            case nil:
                found = []
            }
            DispatchQueue.main.async {
                guard self.skillLoadToken == token else { return }
                self.skillLoadingTargetID = nil
                guard self.currentTarget?.id == target.id else { return }
                self.skillTargetID = target.id
                self.skillCatalog = found
                if let latest = self.skillQuery { self.showSkillMatches(latest) }
            }
        }
    }

    private func showSkillMatches(_ query: String) {
        skillMatches = Array(ClaudeSkills.matching(skillCatalog, query: query).prefix(9))
        skillIndex = min(skillIndex, max(0, skillMatches.count - 1))
        listMode = .skills
        rebuildRows()
        relayout()
    }

    /// Complete, do not execute. Many skills take arguments; a first Return chooses the command,
    /// leaves a space after it, and the next Return sends the finished line through the same path
    /// as every other prompt.
    @discardableResult
    private func acceptSkill() -> Bool {
        guard listMode == .skills, skillMatches.indices.contains(skillIndex) else { return false }
        let prefix = currentTarget?.assistant?.skillInvocationPrefix ?? "/"
        let text = "\(prefix)\(skillMatches[skillIndex].command) "
        textView.setPlainText(text)
        textView.setSelectedRange(NSRange(location: (text as NSString).length, length: 0))
        listMode = .none
        skillMatches = []
        rebuildRows()
        relayout()
        return true
    }

    private func rebuildRows() {
        rows.forEach { $0.removeFromSuperview() }
        if standInList {
            let t = standInTime
            let pick = t < 0 ? 1 : Self.standInSelection(at: t)
            rows = Self.standInSessions(at: t).prefix(9).enumerated().map { i, row in
                let selected = (i == pick)
                let view = TargetRow(title: row.label, index: i,
                                     rich: standInRowText(row, selected: selected))
                view.detail = sessionRowDetail(row.state, selected: selected)
                if case .working = row.state { view.isBusy = true }
                if t >= 0 { view.spinnerTime = t }
                view.icon = ProjectIcon.demoGrid(hue: row.hue).image(height: 11)
                view.isSelected = selected
                listBox.addSubview(view)
                return view
            }
            return
        }
        if listMode == .stacks {
            rows = stackRows.prefix(9).enumerated().map { i, spec in
                let row = TargetRow(title: spec.name, index: i,
                                    rich: stackRowText(spec, selected: false))
                let state = stackCache[spec.root]
                row.toolTip = (state == nil || state!.processes.isEmpty)
                    ? L.t.stackTipUnknown
                    : L.t.stackTip(up: state!.upCount, total: state!.processes.count)
                // The buttons are the only things on the row that act; links open; the rest is
                // just text to read. Two earlier versions got this wrong in opposite directions
                // — one made nothing clickable and looked broken, the next made the whole row
                // clickable and took a public site down without asking.
                //
                // Order matters: the everyday action is rightmost, under the hand. Reading the
                // log is leftmost because it is the only one that changes nothing.
                var buttons: [TargetRow.Button] = []
                var handlers: [() -> Void] = []
                if let log = stackLogButton(spec) {
                    buttons.append(log)
                    handlers.append { [weak self] in self?.showStackLog(spec) }
                }
                if let stop = stackStop(spec) {
                    buttons.append(stop)
                    handlers.append { [weak self] in self?.stopStack(i) }
                }
                if let act = stackAction(spec) {
                    buttons.append(act)
                    handlers.append { [weak self] in self?.actOnStack(i) }
                }
                row.buttons = buttons
                row.onButton = { k in
                    guard handlers.indices.contains(k) else { return }
                    handlers[k]()
                }
                row.onOpen = { url in
                    guard let u = URL(string: url) else { return }
                    NSWorkspace.shared.open(u)
                }
                listBox.addSubview(row)
                return row
            }
            return
        }
        if listMode == .mascots {
            rows = mascotNames.prefix(9).enumerated().map { i, name in
                let row = TargetRow(title: name, index: i)
                row.isSelected = (i == mascotIndex)
                row.onClick = { [weak self] in self?.choose(i) }
                listBox.addSubview(row)
                return row
            }
            return
        }
        if listMode == .skills {
            rows = skillMatches.enumerated().map { i, skill in
                let prefix = currentTarget?.assistant?.skillInvocationPrefix ?? "/"
                let row = TargetRow(title: prefix + skill.command, index: i)
                if !skill.description.isEmpty {
                    row.detail = NSAttributedString(string: skill.description, attributes: [
                        .font: NSFont.systemFont(ofSize: Style.listSize - 1.5),
                        .foregroundColor: NSColor.tertiaryLabelColor,
                    ])
                    row.toolTip = skill.description
                }
                row.isSelected = (i == skillIndex)
                row.onClick = { [weak self] in
                    self?.skillIndex = i
                    _ = self?.acceptSkill()
                }
                listBox.addSubview(row)
                return row
            }
            return
        }
        rows = targets.prefix(9).enumerated().map { i, target in
            let selected = (i == targetIndex)
            let state = sessionStates[target.id] ?? .unknown
            let row = TargetRow(title: target.displayLabel, index: i,
                                rich: sessionRowText(target, selected: selected))
            row.detail = sessionRowDetail(state, selected: selected,
                                          agents: runningAgents(of: target.id),
                                          shells: runningShells(of: target.id),
                                          coordinationWait: coordinationWaitSaid(for: target.id))
            if case .working = state { row.isBusy = true }
            row.icon = rowIcons[target.id]
            row.isSelected = selected
            row.onClick = { [weak self] in self?.choose(i) }
            listBox.addSubview(row)
            return row
        }
    }

    /// Open (or close) one of the lists.
    private func showList(_ mode: ListMode) {
        armedStack = nil
        clearStackLog()
        if listMode == mode { listMode = .none; rebuildRows(); relayout(); return }
        listMode = mode
        if mode == .mascots {
            mascotNames = MascotPack.available()
            mascotIndex = max(0, mascotNames.firstIndex(of: Config.shared.mascot) ?? 0)
        }
        if mode == .stacks { refreshStacks() }
        // Opening the list is the moment the neighbours become worth having: it is the only
        // reason anybody is about to press ↓.
        if mode == .sessions { prefetchNeighbours() }
        rebuildRows()
        syncSpinner()
        relayout()
    }

    /// Route a selection to whichever list is open.
    private func choose(_ i: Int) {
        switch listMode {
        case .mascots: pickMascot(i)
        case .stacks: actOnStack(i)
        case .skills:
            skillIndex = i
            _ = acceptSkill()
        default: pick(i)
        }
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
        // The character is in two places now. Reloading only the one on the card left the island
        // wearing the previous mascot until the app was restarted — two different animals on
        // screen at once, which is a bug you can see from across the room.
        NotificationCenter.default.post(name: .clawdlineConfigChanged, object: nil)
    }

    // MARK: - Dev stacks
    //
    // The list answers the question a background process cannot answer for itself: is it still
    // there. Moving a stack off a terminal tab is a straight loss until something watches it —
    // a foreground process at least dies where you can see it.

    /// Every project that describes a stack, whether or not a session is open in it.
    ///
    /// Sessions alone would not do: the project whose servers have quietly fallen over is
    /// exactly the one you have no session in. The icon registry is the closest thing to a list
    /// of "projects I work on" that already exists, so this reads that rather than asking for a
    /// second list to keep current.
    private func refreshStacks() {
        let sessionDirs = targets.compactMap { $0.cwd }
        DispatchQueue.global(qos: .utility).async {
            var byRoot: [String: DevStack.Spec] = [:]
            for path in ProjectIcon.knownPaths() + sessionDirs {
                if let spec = DevStack.find(fromCwd: path) { byRoot[spec.root] = spec }
            }
            let specs = byRoot.values.sorted { $0.name < $1.name }
            DispatchQueue.main.async {
                self.stackRows = specs
                for s in specs { self.stackSpecCache[s.root] = s }
                if self.listMode == .stacks { self.rebuildRows(); self.relayout() }
            }
            // One at a time, painting as each answers. A trusted `status` is a subprocess, and
            // waiting for the slowest project before showing any of them is how a panel comes
            // up blank for a second every time it opens.
            for spec in specs {
                let state = DevStack.read(spec)
                DispatchQueue.main.async {
                    self.stackCache[spec.root] = state
                    if self.listMode == .stacks { self.rebuildRows(); self.relayout() }
                    self.updateTargetLabel()
                }
            }
        }
    }

    /// One row: the project, what its servers are doing, and — when something is broken — which
    /// one and why. The error is on the row because "4/5" sends you looking and "4/5 build-web"
    /// does not.
    private func stackRowText(_ spec: DevStack.Spec, selected: Bool) -> NSAttributedString {
        let s = NSMutableAttributedString()
        let base = NSFont.systemFont(ofSize: Style.listSize, weight: selected ? .medium : .regular)
        let small = NSFont.systemFont(ofSize: Style.listSize - 1.5)
        func add(_ text: String, _ colour: NSColor, _ font: NSFont = NSFont.systemFont(ofSize: 12),
                 link: String? = nil, tip: String? = nil) {
            var attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: colour]
            // Underlined, the way the footer marks its health link: a link that does not look
            // like one is a link nobody presses.
            if let link, !link.isEmpty {
                attrs[.link] = link
                attrs[.underlineStyle] = NSUnderlineStyle.single.rawValue
                attrs[.underlineColor] = colour.withAlphaComponent(0.4)
            }
            if let tip, !tip.isEmpty { attrs[.clawdlineTip] = tip }
            s.append(NSAttributedString(string: text, attributes: attrs))
        }

        let accent = ProjectIcon.grid(forCwd: spec.root)?.accent ?? NSColor.labelColor
        add(spec.name.padding(toLength: max(spec.name.count, 10), withPad: " ", startingAt: 0),
            selected ? accent : accent.withAlphaComponent(0.75), base)

        guard let state = stackCache[spec.root] else { add("  …", .tertiaryLabelColor, small); return s }
        if stackBusy.contains(spec.root) { add("  ⟳", Style.accent, base) }

        switch state.state {
        case "running":
            add("  ▮ \(state.upCount)/\(state.processes.count)", .systemGreen, small)
        case "partial":
            add("  ▮ \(state.upCount)/\(state.processes.count)", .systemRed, small)
        case "stopped":
            add("  ▯ 0/\(state.processes.count)", .tertiaryLabelColor, small)
        default:
            // In words, not a mark. A grey square beside a green one reads as "down", and that
            // misreading cost someone a hunt for an outage that was not happening — the project
            // was running perfectly and had simply never been agreed to.
            add("  ▨ " + L.t.stackUntrusted, .tertiaryLabelColor, small)
        }

        // How long it has been up. Answers the question a green mark cannot: whether this thing
        // has been quietly restarting all morning, or has genuinely been there since you started.
        if let since = state.since {
            let up = Int(Date().timeIntervalSince1970 - since)
            if up > 0 { add("  " + ProjectStatus.duration(up), .tertiaryLabelColor, small) }
        }

        // Each port is the place it names, so it opens. Confirming a server is really up should
        // not mean reading a number off a row and typing it into a browser.
        for p in state.processes where p.isUp {
            guard let port = p.port else { continue }
            add("  ", .tertiaryLabelColor, small)
            add("\(p.name):\(port)", .tertiaryLabelColor, small,
                link: "http://localhost:\(port)")
        }
        // The address a tunnel is serving, last and marked, because it is the one thing in the
        // row that is not a local implementation detail — it is what a visitor would open.
        for p in state.processes where p.isUp {
            guard let url = p.url, !url.isEmpty else { continue }
            let host = URL(string: url)?.host ?? url
            add("  ↗ ", .tertiaryLabelColor, small)
            add(host, .secondaryLabelColor, small, link: url)
        }
        // **The one that broke, not the first one listed** — see `DevStack.State.rootCause`, and
        // `+n` for how many others went down with it. The old row named whichever failure came
        // first alphabetically and showed seventy characters of the JSON envelope around its
        // last log line, which on all three projects here was either a truncated brace or a
        // `200 OK` from before the crash. The whole hover is the rest of them.
        if let broken = state.rootCause {
            let tip = stackFailureTip(state)
            let others = state.brokenNames.count - 1
            add("  ✗ " + broken.name + (others > 0 ? " +\(others)" : ""), .systemRed, small, tip: tip)
            if let reason = broken.reason {
                // Cut long, not short: the row clips whatever will not fit, and a limit chosen
                // here only decides how much the hover has left to add.
                add("  " + String(reason.prefix(160)), .secondaryLabelColor, small, tip: tip)
            } else if let code = broken.exitCode, code != 0 {
                // Nothing was said, so the number is all there is — and a lone ✗ beside a name
                // is the row saying "something is wrong" and refusing to say what.
                add("  \(L.t.stackExit) \(code)", .secondaryLabelColor, small, tip: tip)
            }
        }
        return s
    }

    /// Every broken process, one per line, for the hover.
    ///
    /// A row has space for one name and one explanation. `haven` had six processes down at once,
    /// with three separate causes among them — a missing `npm`, a Docker daemon that was not
    /// running, and a tunnel that had lost its connection — and a row can only ever have shown
    /// one of the three. This is where the other five go: a hover away rather than a trip to the
    /// log pane, which is the difference between seeing the second cause and fixing the first
    /// one twice.
    private func stackFailureTip(_ state: DevStack.State) -> String {
        state.processes.filter { $0.isDown }.map { p in
            var line = p.name
            if let code = p.exitCode, code != 0 { line += "  \(L.t.stackExit) \(code)" }
            if let reason = p.reason { line += "  " + reason }
            return line
        }.joined(separator: "\n")
    }

    /// The word on a row's button, and the hover that says what pressing it will do.
    ///
    /// Derived in one place so the button and the press can never disagree about which action
    /// this row is offering — which is the failure the button exists to prevent.
    private func stackAction(_ spec: DevStack.Spec) -> TargetRow.Button? {
        // No universal picture for "you may ask this project about itself", so this one keeps
        // its word. Play, stop and restart have pictures everyone already knows.
        if !DevStack.isTrusted(spec) {
            return TargetRow.Button(symbol: nil, label: L.t.stackActionAllow,
                                    tip: L.t.stackTipUnknown,
                                    armed: armedStack?.root == spec.root)
        }
        guard let state = stackCache[spec.root], !state.isUnknown else { return nil }
        let up = state.isStopped
        guard let template = up ? spec.up : spec.restart else { return nil }
        let label = up ? L.t.stackActionStart : L.t.stackActionRestart
        let command = DevStack.expand(template, process: nil)
        let armed = armedStack?.root == spec.root && armedStack?.stop == false
        // The command goes in the hover. It is the most specific true thing available, and for
        // restart it is the difference between a picture and a minute of downtime. Once armed,
        // the hover says so — the icon stays put so the button does not resize under the cursor.
        return TargetRow.Button(
            symbol: up ? "play.fill" : "arrow.clockwise",
            label: label,
            tip: armed ? L.t.stackActionAgain + " — " + command
                        : label + " — " + command,
            armed: armed)
    }

    /// The stop button, on rows that have something to stop.
    private func stackStop(_ spec: DevStack.Spec) -> TargetRow.Button? {
        guard DevStack.isTrusted(spec), let template = spec.down,
              let state = stackCache[spec.root], !state.isUnknown, !state.isStopped
        else { return nil }
        let command = DevStack.expand(template, process: nil)
        let armed = armedStack?.root == spec.root && armedStack?.stop == true
        return TargetRow.Button(
            symbol: "stop.fill",
            label: L.t.stackActionStop,
            tip: armed ? L.t.stackActionAgain + " — " + command
                       : L.t.stackActionStop + " — " + command,
            armed: armed)
    }

    /// Hand the ⌘J pane back to the transcript.
    ///
    /// Called whenever the list opens or closes, so a log never outlives the moment you asked
    /// for it — the pane's usual job is the conversation, and a stale log sitting in it looks
    /// like the session has stopped saying anything.
    private func clearStackLog() {
        guard stackLog != nil else { return }
        stackLog = nil
        stackLogSpec = nil
        stackLogProcess = nil
        stackLogRaw = nil
        dropFloatingHeader()
        lastOutput = ""
        refreshOutput()
    }

    // MARK: - The pane's header, whichever mode is using it

    /// Made once and handed to whoever is filling the pane.
    ///
    /// **One header, because there is one inset.** Both the log and a session's agents want a
    /// strip pinned above the text, and two views installed over one another would each push the
    /// text down by their own height — leaving a gap the size of the one that is not on screen.
    private func floatingHeader() -> PaneHeader {
        if let existing = floatingHeaderView { return existing }
        let header = PaneHeader()
        floatingHeaderView = header
        // AppKit's own answer to "stay put while the content scrolls" — correct through
        // live resize and elastic scrolling in a way that repositioning by hand is not.
        outputHost.addFloatingSubview(header, for: .vertical)
        if savedOutputInset == nil { savedOutputInset = outputView.textContainerInset }
        outputView.textContainerInset = NSSize(
            width: outputView.textContainerInset.width,
            height: PaneHeader.height + 6)
        return header
    }

    private func dropFloatingHeader() {
        guard let header = floatingHeaderView else { return }
        header.removeFromSuperview()
        floatingHeaderView = nil
        agentStripState = ""
        if let saved = savedOutputInset {
            outputView.textContainerInset = saved
            savedOutputInset = nil
        }
    }

    /// Sized to the pane it floats over, and asked to draw itself again.
    private func layoutFloatingHeader(_ header: PaneHeader) {
        header.frame = NSRect(x: 0, y: 0, width: outputHost.contentSize.width,
                              height: PaneHeader.height)
        header.needsDisplay = true
        header.window?.invalidateCursorRects(for: header)
    }

    // MARK: - Reading one of the session's background agents

    /// The strip above the transcript: how many agents are out, a tab per agent, and — once one
    /// is open — its name and the way back to the conversation that sent it.
    ///
    /// Runs on every beat of the pane and does nothing on nearly all of them: the fast path is a
    /// session with no agents, which is one dictionary lookup and a `guard`.
    private func updateAgentStrip(for target: TargetSession) {
        let agents = SessionWatch.shared.agents(of: target.id)
        guard !agents.isEmpty else {
            // The last of them settled while somebody was reading it. Put the pane back rather
            // than leave a header with nothing behind it.
            if agentID != nil { agentID = nil; lastOutput = "" }
            dropFloatingHeader()
            return
        }
        // An agent quiet long enough to leave the strip takes its transcript with it. Landing
        // back in the session is the only honest answer: there is no longer a row to return to.
        if let id = agentID, !agents.contains(where: { $0.id == id }) {
            agentID = nil
            lastOutput = ""
        }

        let header = floatingHeader()
        header.onBack = { [weak self] in self?.clearAgent() }
        header.onTab = { [weak self] value in
            guard let value else { self?.clearAgent(); return }
            self?.showAgent(value)
        }
        let open = agentID.flatMap { id in agents.first(where: { $0.id == id }) }
        // Reading one: its own description, the only string here somebody wrote to explain what
        // they wanted. Reading none: what this strip *is*, and deliberately not how many are out
        // — the activity line directly above it already says that, and a header repeating the
        // line above it spends a row of the pane saying nothing new. `webAgents` carries a `web`
        // prefix because the page asked for it first; the words in it are these words.
        header.title = open.map { Self.agentLabel($0.description, limit: 40) } ?? L.t.webAgents
        header.titleColor = .labelColor
        // Empty until there is somewhere to go back to — the list itself is the top of this
        // pane, and an arrow on it would point at nothing. See `PaneHeader.draw`.
        header.backLabel = open == nil ? "" : L.t.agentBack
        header.current = agentID
        // `main` first, the way the terminal draws the same tree: the conversation all of them
        // hang under, and the row that takes you back out of one. `nil` is already this
        // header's word for "no tab in particular", which is exactly what going back means.
        var tabs: [(label: String, value: String?)] = [("main", nil)]
        tabs += agents.map { (Self.agentLabel($0.description, limit: 22), $0.id) }
        header.tabs = tabs

        // Laid out only when something about it changed. This runs on every beat of the pane,
        // and a header told to redraw once a second for the whole time an agent is out is a
        // second of drawing per second of nothing happening.
        let state = "\(agentID ?? "-")|\(header.title)|\(header.backLabel)|"
            + header.tabs.map { $0.label }.joined(separator: ",")
        if state != agentStripState || header.frame.width != outputHost.contentSize.width {
            agentStripState = state
            layoutFloatingHeader(header)
        }
    }

    /// A tab is a strip of a panel that is 720 points wide with five other tabs on it, so what
    /// somebody wrote to describe a whole piece of work has to fit in a few words. Cut on a
    /// space when there is one near the end, because half a word reads as a rendering fault.
    private static func agentLabel(_ text: String, limit: Int) -> String {
        let clean = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard clean.count > limit else { return clean }
        let cut = String(clean.prefix(limit))
        if let space = cut.lastIndex(of: " "), cut.distance(from: space, to: cut.endIndex) < 8 {
            return String(cut[cut.startIndex..<space]) + "…"
        }
        return cut + "…"
    }

    /// The activity strip was clicked. See where it is built for why this is a gesture on a
    /// label rather than a button.
    @objc private func activityClicked() {
        guard let id = currentTarget?.id,
              !SessionWatch.shared.agents(of: id).isEmpty,
              !outputOpen else { return }
        toggleOutput()
    }

    /// The first agent whose description contains `text`, opened. For `clawdline://snapshot`.
    private func showAgent(matching text: String) {
        guard let target = currentTarget else { return }
        let agents = SessionWatch.shared.agents(of: target.id)
        guard let hit = agents.first(where: {
            $0.description.localizedCaseInsensitiveContains(text)
        }) else { return }
        showAgent(hit.id)
    }

    private func showAgent(_ id: String) {
        guard agentID != id else { return }
        agentID = id
        lastOutput = ""
        if !outputOpen { toggleOutput() }
        refreshOutput()
    }

    private func clearAgent() {
        guard agentID != nil else { return }
        agentID = nil
        lastOutput = ""
        refreshOutput()
    }

    /// One agent's conversation, in the pane the session's was in.
    ///
    /// The same three lines the session's transcript is built from — tail the file, parse it,
    /// render it — because it is the same kind of file. What is not the same is where the file
    /// comes from and that there is no terminal fallback: an agent has no screen to scrape, and
    /// this pane is the only place its work has ever been visible.
    private func refreshAgentTranscript(for target: TargetSession, agent id: String) {
        let size = Config.shared.outputSize
        let newestFirst = Config.shared.outputNewestFirst
        let folds = expandedFolds
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let self else { return }
            guard let file = Subagents.transcript(of: target, agent: id) else {
                DispatchQueue.main.async { self.setAgentNote(L.t.agentEmpty, for: id, of: target) }
                return
            }
            let signature = "agent-\(id)-" + Transcript.signature(of: file)
                + "-\(size)-\(newestFirst)-\(folds.sorted().joined(separator: ","))"
            // `sidechains: true` — an agent's file is nothing but sidechain, which is what the
            // session's own transcript drops. See `Transcript.parse`.
            let entries = Transcript.tail(of: file, bytes: 8_000_000)
                .map { Transcript.parse($0, assistant: .claude, sidechains: true) } ?? []
            guard !entries.isEmpty else {
                DispatchQueue.main.async { self.setAgentNote(L.t.agentEmpty, for: id, of: target) }
                return
            }
            let rendered = Transcript.render(entries, size: size, mono: Style.outputFont,
                                             expanded: folds, newestFirst: newestFirst)
            DispatchQueue.main.async {
                // Whoever this was read for may not be who is on screen by the time it lands —
                // the same race the session's own transcript runs, with one more way to lose it:
                // the reader can have gone back to the session while this was in flight.
                guard self.agentID == id, self.currentTarget?.id == target.id,
                      self.outputOpen, self.stackLog == nil, self.cannedTranscript == nil,
                      signature != self.lastOutput else { return }
                self.lastOutput = signature
                let following = self.outputView.string.isEmpty || self.outputIsAtNewestEdge
                let clip = self.outputHost.contentView
                let saved = clip.bounds.origin
                self.outputView.textStorage?.setAttributedString(rendered)
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
        }
    }

    /// An agent whose file has nothing readable in it. Not an error and not an empty pane: the
    /// first second of every agent's life looks like this, and so does the whole of one that
    /// died before it wrote anything.
    private func setAgentNote(_ text: String, for id: String, of target: TargetSession) {
        guard agentID == id, currentTarget?.id == target.id, outputOpen else { return }
        let signature = "agent-note-\(id)"
        guard signature != lastOutput else { return }
        lastOutput = signature
        outputView.textStorage?.setAttributedString(NSAttributedString(
            string: text,
            attributes: [.font: NSFont.systemFont(ofSize: Config.shared.outputSize),
                         .foregroundColor: NSColor.secondaryLabelColor]))
    }

    /// The log button. No confirmation and no armed state — it is the only one of the three
    /// that changes nothing, which is also why it is the safe thing to reach for first.
    private func stackLogButton(_ spec: DevStack.Spec) -> TargetRow.Button? {
        guard DevStack.isTrusted(spec), let template = spec.logs else { return nil }
        return TargetRow.Button(
            symbol: "text.alignleft",
            label: L.t.stackActionLogs,
            tip: L.t.stackActionLogs + " — " + DevStack.expand(template, process: nil))
    }

    /// Put a stack's logs in the ⌘J pane.
    ///
    /// Reusing that pane rather than growing a window of its own: it is already the place where
    /// long text is read here, it already scrolls and scales, and having two of them would mean
    /// two answers to "where do I look".
    ///
    /// The stack's `logs` command decides what arrives. With no process named, the projects here
    /// tail every process's file at once — `tail` labels each with its name, which is exactly the
    /// overview you want before you know which one you care about.
    private func showStackLog(_ spec: DevStack.Spec, process: String? = nil) {
        guard DevStack.isTrusted(spec), spec.logs != nil else { return }
        if !outputOpen { toggleOutput() }
        let sameStack = stackLogSpec?.root == spec.root
        stackLogSpec = spec
        stackLogProcess = process
        // Already have this stack's log: switching tabs is a re-render, not a fetch.
        if sameStack, let raw = stackLogRaw {
            setStackLog(raw, spec: spec)
            return
        }
        stackLogRaw = nil
        setStackLog("…", spec: spec)
        DispatchQueue.global(qos: .userInitiated).async {
            // Every process at once, deep enough that picking a tab is still worth reading.
            let result = DevStack.run(spec, .logs, process: nil, lines: 300, timeout: 30)
            DispatchQueue.main.async {
                guard self.stackLogSpec?.root == spec.root else { return }
                self.stackLogRaw = result.output
                self.setStackLog(result.output, spec: spec)
            }
        }
    }

    /// The pane's own chrome: which project, a tab per process, and the way back.
    ///
    /// Tabs are links because the pane is a read-only text view — a link is the only thing in it
    /// that can be clicked at all, which is the same reason the transcript's folds are links.
    private func setStackLog(_ text: String, spec: DevStack.Spec) {
        let header = floatingHeader()
        // Set every time rather than once at birth: the same view is handed back and forth
        // between the log and an agent, and a stale closure here would send the back button to
        // whichever of the two was in the pane first.
        header.onBack = { [weak self] in self?.clearStackLog() }
        header.onTab = { [weak self] value in
            guard let self, let spec = self.stackLogSpec else { return }
            self.showStackLog(spec, process: value)
        }
        header.title = spec.name
        header.titleColor = ProjectIcon.grid(forCwd: spec.root)?.accent ?? .labelColor
        header.backLabel = L.t.stackLogBack
        header.current = stackLogProcess
        var tabs: [(label: String, value: String?)] = [(L.t.stackLogAll, nil)]
        for p in (stackCache[spec.root]?.processes ?? []) { tabs.append((p.name, p.name)) }
        header.tabs = tabs
        layoutFloatingHeader(header)

        let body = StackLog.render(text, mono: Style.outputFont,
                                   showNames: stackLogProcess == nil,
                                   only: stackLogProcess)
        stackLog = body
        outputView.textStorage?.setAttributedString(body)
        scrollOutputToNewest()
    }

    /// Show what the next press will run, and wait to be asked again.
    private func armStack(_ spec: DevStack.Spec, command: String, stop: Bool = false) {
        armedStack = (spec.root, stop)
        setHint(L.t.stackConfirm(command), warn: true)
        rebuildRows()
        relayout()
        // Expires with the message that asked. An agreement that outlives the line it was about
        // is one you give without seeing what you are agreeing to — the next press would run
        // the command with nothing on screen to say so.
        DispatchQueue.main.asyncAfter(deadline: .now() + 6) { [weak self] in
            guard let self, self.armedStack?.root == spec.root else { return }
            self.armedStack = nil
            self.rebuildRows()
            self.relayout()
        }
    }

    /// The button, or ⌘n, on a stack.
    ///
    /// **Anything that can take a live site off the air is asked twice**, and the second ask is
    /// on the button itself — it changes to "press again" and the actual command appears below.
    /// Starting something that is already down is the one action that cannot make things worse,
    /// so that one goes on the first press.
    ///
    /// An untrusted project only ever buys the right to *ask*: its state is `unknown` by
    /// construction, and acting on unknown means guessing — the guess would be "it must be down,
    /// start it", which for a project that is in fact serving traffic is the worse wrong answer.
    private func actOnStack(_ i: Int) {
        guard stackRows.indices.contains(i) else { return }
        let spec = stackRows[i]
        guard !stackBusy.contains(spec.root) else { return }

        // What `.devstack.json` names is arbitrary code out of a repository, so what is shown is
        // the `status` command — genuinely the first thing that will run. A dialog asking "do
        // you trust this workspace" is a question nobody has the information to answer.
        if !DevStack.isTrusted(spec) {
            if armedStack?.root == spec.root, armedStack?.stop == false {
                DevStack.trust(spec)
                armedStack = nil
                refreshStacks()
            } else {
                armStack(spec, command: spec.status ?? spec.up ?? "?")
            }
            return
        }

        let state = stackCache[spec.root]
        // Still nothing read back: do not guess.
        guard let state, !state.isUnknown else { refreshStacks(); return }
        let up = state.isStopped
        let action: DevStack.Action = up ? .up : .restart
        guard let template = up ? spec.up : spec.restart else { return }
        let command = DevStack.expand(template, process: nil)

        if !up, !(armedStack?.root == spec.root && armedStack?.stop == false) {
            armStack(spec, command: command)
            return
        }
        armedStack = nil

        // No hint text for progress: a hint clears itself after a second and a half, and a
        // front-end build is a minute. The ⟳ on the row is what stays for as long as it is true.
        stackBusy.insert(spec.root)
        rebuildRows()
        DispatchQueue.global(qos: .userInitiated).async {
            let result = DevStack.run(spec, action)
            let state = DevStack.read(spec)
            DispatchQueue.main.async {
                self.stackBusy.remove(spec.root)
                self.stackCache[spec.root] = state
                self.rebuildRows()
                self.relayout()
                self.updateTargetLabel()
                // A failed restart puts its own output in the input bar. That is the whole loop
                // this exists for: the build error that stopped the deploy is already the
                // message you would have typed, so stop making the person retype it.
                if !result.ok, !result.output.isEmpty {
                    self.textView.setPlainText(
                        "\(spec.name): \(command) failed\n\n" + result.output.suffix(3000))
                    self.relayout()
                } else if state.rootCause != nil,
                          self.textView.string.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    // **The command can succeed while the stack does not**, and this is the case
                    // that looked like nothing happening at all. `make stack-up` returns as soon
                    // as its supervisor is daemonised — long before the processes under it have
                    // finished starting — so a front-end build that fails a minute later exits
                    // into a command that already reported success. The press appeared to work,
                    // the spinner stopped, and the site was down.
                    //
                    // The state read a line above already knows better, so say so. Only into an
                    // empty box: the failure is worth putting where it can be sent, and never at
                    // the cost of a message somebody was part way through writing.
                    self.textView.setPlainText(
                        "\(spec.name): \(command)\n\n" + self.stackFailureTip(state))
                    self.relayout()
                }
            }
        }
    }

    /// ⌘⇧n: stop it. On its own modifier because it is the one action here you cannot undo by
    /// pressing the same key again — a public tunnel taken down stays down until you notice.
    private func stopStack(_ i: Int) {
        guard listMode == .stacks, stackRows.indices.contains(i) else { return }
        let spec = stackRows[i]
        // Never the first thing a project does on this machine: stopping something you have not
        // knowingly started yet is an odd first act, and it is the one press with no undo.
        guard let template = spec.down, DevStack.isTrusted(spec),
              !stackBusy.contains(spec.root) else { return }

        // Asked twice, like restart — more so, since this one does not bring anything back.
        if !(armedStack?.root == spec.root && armedStack?.stop == true) {
            armStack(spec, command: DevStack.expand(template, process: nil), stop: true)
            return
        }
        armedStack = nil

        stackBusy.insert(spec.root)
        rebuildRows()
        DispatchQueue.global(qos: .userInitiated).async {
            _ = DevStack.run(spec, .down)
            let state = DevStack.read(spec)
            DispatchQueue.main.async {
                self.stackBusy.remove(spec.root)
                self.stackCache[spec.root] = state
                self.rebuildRows()
                self.relayout()
                self.updateTargetLabel()
            }
        }
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
        // An agent goes for the stronger reason: it belongs to the session that sent it away,
        // and carrying one across would put its work under another session's name.
        if targetIndex != i { expandedFolds.removeAll(); agentID = nil }
        targetIndex = i
        stickyID = targets[i].id
        stickyBase = lastKnownCurrentID
        Config.shared.lastTargetID = targets[i].id
        Config.shared.save()
        // Rebuilt rather than re-flagged. A row that carries state draws its own title, so the
        // weight and the colour that say "this one" are part of that text and cannot be switched
        // on from the outside afterwards.
        if closeList && listMode != .none { listMode = .none }
        rebuildRows()
        // Before the pane is asked for anything. The strip changes the card's height, so it has
        // to be right *now* rather than whenever the terminal answers: put it up late and the
        // conversation gets painted first and then shoved down by a line arriving above it.
        setActivity(activityLine(for: targets[i].id))
        updateTargetLabel()
        refreshProjectInfo()
        refreshOutput()
        prefetchNeighbours()
        follow(targets[i])
        relayout()
    }

    /// Move the terminal's own tab to the session the bar is now pointing at.
    ///
    /// Off the main thread and unwaited-for: this is an osascript round trip, and it is a
    /// courtesy rather than part of the switch — the pane and the rows must not sit still for a
    /// couple of hundred milliseconds waiting for a terminal to finish selecting a tab.
    ///
    /// **Without activating.** Bringing iTerm2 forward on every Tab press would hand it the
    /// keyboard, which is the one thing this whole application exists to avoid doing.
    private func follow(_ target: TargetSession) {
        guard Config.shared.followTarget else { return }
        DispatchQueue.global(qos: .utility).async {
            Targets.reveal(target, activate: false)
        }
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
            // The servers, if the project says how to ask. This is the slow one — a trusted
            // `status` command is a subprocess, and on a busy machine it is a good fraction of a
            // second — which is why it lands separately below rather than holding up the name.
            let spec = DevStack.find(fromCwd: cwd)
            DispatchQueue.main.async {
                self.projectCache[target.id] = info
                self.iconCache[target.id] = icon
                self.statusCache[target.id] = status
                if let spec {
                    self.stackSpecCache[spec.root] = spec
                    self.stackRootOfSession[target.id] = spec.root
                } else {
                    self.stackRootOfSession.removeValue(forKey: target.id)
                }
                if self.currentTarget?.id == target.id { self.updateTargetLabel() }
            }
            guard let spec else { return }
            let stack = DevStack.read(spec)
            DispatchQueue.main.async {
                self.stackCache[spec.root] = stack
                if self.currentTarget?.id == target.id { self.updateTargetLabel() }
            }
        }
    }

    /// The stack belonging to whichever session is selected, if it has one.
    private func currentStack() -> (spec: DevStack.Spec, state: DevStack.State?)? {
        guard let t = currentTarget,
              let root = stackRootOfSession[t.id],
              let spec = stackSpecCache[root] else { return nil }
        return (spec, stackCache[root])
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
        // The same chip for a test or a build running on this Mac. It sits beside the deploy
        // rather than replacing it: one is happening here and the other in somebody's cloud, and
        // the footer has room to say both. `log` is a path, so it opens the way the backlog
        // artifact does; there is no web page to send anybody to.
        if let r = status.run {
            let now = Date().timeIntervalSince1970
            let link = r.log.map { "file://" + $0 }
            chip("   " + r.label + " ",
                 r.state == "fail" ? .systemRed : NSColor.secondaryLabelColor, link: link)
            if r.state == "running" {
                chip(ProjectStatus.bar(r.progress(now: now)), Style.accent, link: link, font: mono)
                // A phase takes the place of the clock, the way it takes the place of the
                // percentage on the page: the bar is already saying how far along this is, and
                // "compiling" answers a question the elapsed seconds cannot.
                if let phase = r.phase, !phase.isEmpty {
                    chip(" " + phase, .tertiaryLabelColor, link: link)
                } else {
                    chip(" " + ProjectStatus.duration(r.elapsed(now: now))
                         + "/" + ProjectStatus.duration(Int(r.typicalSeconds)),
                         .tertiaryLabelColor, link: link)
                }
            } else {
                chip(r.state == "ok" ? "✓" : "✗",
                     r.state == "ok" ? .systemGreen : .systemRed, link: link)
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

    /// The project's own servers, in the footer's rightmost slot.
    ///
    /// One glyph and a count, because the footer's job here is to answer "do I need to care",
    /// not "what is going on" — that question has a whole panel. The one exception is a broken
    /// process, which is named: "4/5" sends you looking, "4/5 build-web" does not.
    private func appendStackChip(_ state: DevStack.State?, to s: NSMutableAttributedString) {
        guard let state else { return }
        let font = NSFont.systemFont(ofSize: Style.hintSize - 0.5)
        var tip = state.processes.isEmpty
            ? L.t.stackTipUnknown
            : L.t.stackTip(up: state.upCount, total: state.processes.count)
        // With something down, the count is the least of what the hover can say. This is the
        // footer's only route to *why*, and without it the mark says "two of eight" and leaves
        // the person to open the panel to find out which two and what happened to them.
        if state.state == "partial" || state.state == "stopped" {
            let failures = stackFailureTip(state)
            if !failures.isEmpty { tip += "\n\n" + failures }
        }
        func chip(_ text: String, _ colour: NSColor) {
            s.append(NSAttributedString(string: text, attributes: [
                .font: font, .foregroundColor: colour, .clawdlineTip: tip,
            ]))
        }
        switch state.state {
        case "running":
            // **No denominator while everything is up.** "6/6" sat immediately left of the
            // session counter "3/7", and two fractions side by side read as one pair of related
            // numbers when they have nothing to do with each other. The count still says how
            // much is being watched; the fraction only earns its place when part of it is gone.
            chip("   ▮ \(state.upCount)", .systemGreen)
        case "partial":
            chip("   ▮ \(state.upCount)/\(state.processes.count)", .systemRed)
            // The cause, not the alphabetically first casualty — the same choice the row makes,
            // and for the same reason. One word fits here, so it had better be the right word.
            if let broken = state.rootCause { chip(" " + broken.name, .systemRed) }
        case "stopped":
            chip("   ▯", .tertiaryLabelColor)
        default:
            // Unknown: the project has a stack and we have not been allowed to ask about it.
            // Drawn like "stopped" it would be a confident wrong answer; drawn like "running"
            // it would be a dangerous one. The hover says which — see stackUntrusted for why a
            // mark alone was not enough.
            chip("   ▨", .tertiaryLabelColor)
        }
    }

    /// Talk instead of type. Wired here rather than in the text view because the state it puts
    /// the bar into — listening, and on which machine — belongs to the whole card.
    /// Whisper when it is installed and wanted, Apple otherwise.
    ///
    /// "auto" rather than a switch you have to find: installing whisper is the deliberate act,
    /// and having done it, being asked again in a config file is a second hoop for no reason.
    private var useWhisper: Bool {
        switch Config.shared.voiceEngine {
        case "whisper": return true
        case "apple": return false
        default:
            return Whisper.isAvailable(binary: Config.shared.whisperBinary,
                                       model: Config.shared.whisperModel)
        }
    }

    private func toggleVoice() {
        voice.onText = { [weak self] text in
            guard let self else { return }
            self.textView.updateDictation(text)
            self.relayout()
        }
        voice.onLevel = { [weak self] level in self?.micButton.level = level }

        let words = Voice.vocabulary(
            from: Config.shared.history,
            extras: Voice.alwaysExpected
                + [currentTarget.flatMap { projectCache[$0.id]?.name },
                   currentTarget.flatMap { projectCache[$0.id]?.branch }].compactMap { $0 })

        textView.onDictationDisplaced = { [weak self] in self?.voice.forgetAccumulated() }
        voice.onState = { [weak self] state in self?.showVoice(state, whisper: self?.useWhisper == true) }
        voice.vocabulary = words
        voice.refineWithWhisper = useWhisper
        // A settled stretch stops being speech's to rewrite: end the run and open the next one
        // after it, exactly as if the user had paused and started a new thought — which they did.
        voice.onSettled = { [weak self] in
            self?.textView.endDictation()
            self?.textView.beginDictation()
        }
        voice.onPermissionPrompt = { [weak self] asking in
            guard let self else { return }
            self.awaitingPermission = asking
            // Take focus back once the sheet is gone. Answering it leaves the panel visible but
            // no longer key, and a box you cannot type into looks broken in exactly the same way
            // as one that closed itself.
            if !asking, self.panel.isVisible {
                NSApp.activate(ignoringOtherApps: true)
                self.panel.makeKeyAndOrderFront(nil)
                self.panel.makeFirstResponder(self.textView)
            }
        }
        if !voice.isListening { textView.beginDictation() }
        voice.toggle(locale: Self.voiceLocales())
    }

    /// Say which recogniser the microphone will use, before it is pressed rather than after.
    private func refreshVoiceTooltip() {
        let status = Whisper.status(binary: Config.shared.whisperBinary,
                                    model: Config.shared.whisperModel)
        micButton.toolTip = L.t.hintVoice + " · " + L.t.dictationStatus(status)
        if case .ready = status { micButton.hasSecondPass = true }
        else { micButton.hasSecondPass = false }
    }

    /// One place that turns a voice state into what the card shows.
    private func showVoice(_ state: Voice.State, whisper: Bool) {
        switch state {
        case .idle:
            micButton.isListening = false
            micButton.isThinking = false
            stopVoiceClock()
            setVoiceStatus("", colour: .clear)
            textView.endDictation()
            if sendWhenVoiceFinishes {
                sendWhenVoiceFinishes = false
                submit()
            }
        case .listening(let onDevice):
            micButton.isListening = true
            micButton.isThinking = false
            stopVoiceClock()
            setVoiceStatus(whisper ? L.t.voiceListeningWhisper()
                                   : L.t.voiceListening(onDevice: onDevice),
                           colour: Style.accent)
        case .transcribing:
            micButton.isListening = false
            micButton.isThinking = true
            // A clock, because this is the one part with no other sign of life — and the first
            // run after a reboot spends twelve seconds loading the model before it reads a word.
            startVoiceClock()
        case .failed(let why):
            micButton.isListening = false
            micButton.isThinking = false
            stopVoiceClock()
            setVoiceStatus("", colour: .clear)
            textView.endDictation()
            setHint(why, warn: true)
            // The words on screen are still the live ones, and they are what was asked for.
            if sendWhenVoiceFinishes {
                sendWhenVoiceFinishes = false
                submit()
            }
        }
        relayout()
    }

    private func setVoiceStatus(_ text: String, colour: NSColor) {
        voiceLabel.stringValue = text
        voiceLabel.textColor = colour
        voiceLabel.isHidden = text.isEmpty
        if text.isEmpty { hints.isHidden = false }
    }

    private func startVoiceClock() {
        stopVoiceClock()
        let began = Date()
        let tick = { [weak self] in
            guard let self else { return }
            self.setVoiceStatus(L.t.voiceTranscribing(seconds: Date().timeIntervalSince(began)),
                                colour: Style.accent)
            self.relayout()
        }
        tick()
        let t = Timer(timeInterval: 0.1, repeats: true) { _ in tick() }
        RunLoop.main.add(t, forMode: .common)
        voiceClock = t
    }

    private func stopVoiceClock() {
        voiceClock?.invalidate()
        voiceClock = nil
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

    private var footerTipOwners: [NSString] = []

    private func setFooter(_ text: NSAttributedString) {
        targetLabel.textStorage?.setAttributedString(text)
        // What each mark means, for anyone who has not learned them yet — which is everyone at
        // first, and the marks are small enough that "learning them" mostly does not happen.
        // Measured from run widths rather than asked of a layout manager: this view is one
        // truncating line, and the two TextKits answer that question differently.
        targetLabel.removeAllToolTips()
        // Held, because `addToolTip(_:owner:userData:)` does not retain its owner — see the note
        // on `StackRow.tipOwners`. A bridged `NSString` created at the call site is owned by
        // nobody, and the tool tip manager reaches for it half a second later.
        footerTipOwners = []
        for zone in TextZones.of(text, key: .clawdlineTip, x0: 0,
                                 height: targetLabel.bounds.height) {
            let owner = zone.value as NSString
            footerTipOwners.append(owner)
            _ = targetLabel.addToolTip(zone.rect, owner: owner, userData: nil)
        }
    }

    private func updateTargetLabel() {
        let assistant = currentTarget?.assistant ?? .claude
        let placeholder = assistant.promptPlaceholder(from: L.t.placeholder)
        if textView.placeholder != placeholder {
            textView.placeholder = placeholder
            textView.needsDisplay = true
        }
        iconView?.grid = currentTarget.flatMap { iconCache[$0.id] }
        guard !usingStandInLabel else {
            iconView?.grid = standInRow.map { ProjectIcon.demoGrid(hue: $0.hue) }
            setFooter(Self.standInTarget(row: standInRow))
            return
        }
        let s = NSMutableAttributedString()
        if let t = currentTarget {
            s.append(NSAttributedString(string: "● ", attributes: [
                .foregroundColor: t.isAssistant ? Style.accent : NSColor.tertiaryLabelColor,
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
            var name = t.displayLabel
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
            appendStackChip(currentStack()?.state, to: s)
            if targets.count > 1 {
                s.append(NSAttributedString(string: "  \(targetIndex + 1)/\(targets.count)", attributes: [
                    .foregroundColor: NSColor.secondaryLabelColor,
                    .font: NSFont.monospacedDigitSystemFont(ofSize: Style.hintSize - 0.5, weight: .regular),
                    .clawdlineTip: L.t.sessionTip(index: targetIndex + 1, total: targets.count),
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
        if listMode == .skills {
            guard !skillMatches.isEmpty else { return true }
            skillIndex = max(0, min(skillMatches.count - 1, skillIndex + delta))
            for (i, row) in rows.enumerated() { row.isSelected = (i == skillIndex) }
            return true
        }
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

    /// Set when Enter arrived mid-sentence: send as soon as the words are final.
    private var sendWhenVoiceFinishes = false
    private var voiceClock: Timer?

    private func submit() {
        // Pressing Enter while still talking means "that was the end of it", not "send what you
        // have managed to write down so far". Stop, let the second pass finish, then send.
        if voice.isListening {
            sendWhenVoiceFinishes = true
            voice.stop()
            return
        }
        let pieces = textView.resolvedPieces()
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
            // The pieces rather than the flattened string, so an image can go over as an image.
            // With none in it this is one bracketed paste, exactly as it always was.
            let err = Targets.send(pieces, to: target)
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
            var list = snap.assistantSessions
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
            Log.write("sendDirect → \(target.displayLabel): \(err ?? "ok")")
        }
    }

    /// Render what the panel draws into a PNG.
    /// The reason is practical: `screencapture` needs Screen Recording permission, and without it
    /// the result is the wallpaper with every window stripped out — which leaves you blind while
    /// tuning layout. This path only paints our own layers, so it needs no permission at all.
    func snapshot(to path: String, routine: String? = nil, at time: Double? = nil, list: String? = nil,
                  output: Bool = false, session: String? = nil, full: Bool? = nil,
                  transcript: String? = nil, agent: String? = nil) {
        // Opening the pane has to happen before the wait, not inside the render: the transcript
        // arrives asynchronously, so a pane opened at draw time is always drawn empty.
        let arrange = { [weak self] in
            guard let self else { return }
            // Naming a session makes a particular transcript reproducible to look at, which is
            // the only way to check how something rare — a table, a long code block — comes out.
            if let want = session, !want.isEmpty,
               let i = self.targets.firstIndex(where: { $0.displayLabel.localizedCaseInsensitiveContains(want) }) {
                self.pick(i, closeList: false)
            }
            if output, !self.outputOpen { self.toggleOutput() }
            // And one of that session's agents in the pane, named by any part of what it was
            // asked to do. The pane's second room has no URL of its own and no keystroke — it is
            // entered by clicking a tab — so without this it could never be photographed.
            if let want = agent, !want.isEmpty { self.showAgent(matching: want) }
            // A canned transcript, for the pictures on the README. Shooting a real session
            // would publish whatever the machine happened to be working on that afternoon.
            if let file = transcript, !file.isEmpty { self.showCannedTranscript(at: file) }
            if let want = full, want != self.fullscreen { self.toggleFullscreen() }
            if list == "mascots" { self.showList(.mascots) }
            else if list == "sessions" { self.showList(.sessions) }
            else if list == "demo" { self.showStandInSessions() }
            else if list == "stacks" { self.showList(.stacks) }
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
        let wasFullscreen = fullscreen
        if !wasVisible { show() }
        resetForSnapshot(keepingOutput: output)
        // The session list arrives from an async scan, so picking one has to wait for it —
        // arranging immediately picks from an empty list and silently keeps the current target.
        DispatchQueue.main.asyncAfter(deadline: .now() + (session == nil ? 0 : 1.4)) {
            arrange()
            // Reading a session back costs an osascript round trip, so give it room.
            DispatchQueue.main.asyncAfter(deadline: .now() + (output ? 2.2 : 0.55)) {
                render()
                // Put back what was borrowed. A shot that asked for ⌘F used to leave the panel
                // filling the screen afterwards, and the next ⌥Space came up as a window the
                // size of the display with no obvious way out — which reads as the application
                // having hung, and was reported as exactly that.
                if self.fullscreen != wasFullscreen { self.toggleFullscreen() }
                self.standInList = false
                self.cannedTranscript = nil
                self.usingStandInLabel = false
                if !wasVisible { self.hide() }
            }
        }
    }

    /// Put the panel into a known state before a picture is taken of it.
    ///
    /// A shot asks for exactly what it wants and gets whatever was left over as well: a panel
    /// that is already open keeps its list, its size and whatever transcript it was showing,
    /// because `show()` — which resets all of that — is skipped when it is already up.
    ///
    /// **This is a privacy fix, not a tidiness one.** The picture that prompted it had the
    /// session list open from an earlier shot, so a README image came out carrying the real
    /// names of the projects on this machine and the real conversation in one of them. Every
    /// picture in that folder is supposed to be a stand-in.
    private func resetForSnapshot(keepingOutput: Bool) {
        listMode = .none
        standInList = false
        cannedTranscript = nil
        setActivity(nil)
        // **The stand-in footer is the default, not something a shot opts into.**
        //
        // It used to be turned on only by the canned-transcript path, so a picture that did not
        // ask for a transcript — the mascot picker, for one — came out with the real footer on
        // it: this machine's repository, its branch, its uncommitted count, a failing deploy and
        // the domain one of them serves. That shot was one `git push` away from a public page.
        //
        // This URL exists to make documentation. Nothing that comes out of it should be able to
        // name a real project by accident, so naming one has to be the thing you go out of your
        // way to do rather than the thing that happens when you forget.
        usingStandInLabel = true
        pendingShowList = false
        pendingFocusID = nil
        if fullscreen { toggleFullscreen() }
        if outputOpen && !keepingOutput { toggleOutput() }
        rebuildRows()
        relayout()
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
        // The same known state a still starts from, and for the same reason. The strip draws
        // `container`, so whatever the ⌘J pane happens to be showing is drawn with it — and what
        // it is showing is a real conversation. One of these went out with this machine's own
        // session in the background of a clip about dictation.
        resetForSnapshot(keepingOutput: false)
        usingStandInLabel = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
            self.renderFilmstrip(dir: dir, fps: fps, seconds: seconds, script: script, text: text)
            self.hide()
        }
    }

    private func renderFilmstrip(dir: String, fps: Double, seconds: Double, script: String, text: String) {
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)

        let fake = Self.standInTarget()

        // Two clips are of the session list and they are about different things, so they are
        // framed differently. `sessions` is the claim that the *terminal* follows the selection,
        // and needs room above and below for a tab bar and a status line to follow it in.
        // `rows` is the claim the list section itself makes — that the rows change state while
        // you watch — and a terminal drawn behind that is furniture arguing with the subject.
        let withTerminal = script == "sessions"
        // Room for the tab bar above and the status line below. The other strips are the card on
        // a wallpaper and want none of it.
        let margin: CGFloat = withTerminal ? 108 : 64
        textView.clearText()
        // The list has to be up *before* the canvas is measured, or every frame is drawn at the
        // height of a bar with no list under it and the rows fall off the bottom.
        let listStrip = withTerminal || script == "rows"
        // The mascot picker's claim is about motion — "the character on the bar changes while the
        // list is still open, so you pick by looking rather than by reading names" — which a still
        // of a list of names is the one thing that cannot show.
        let packStrip = script == "mascots"
        if listStrip { showStandInSessions(at: 0) }
        if packStrip { showList(.mascots) }
        relayout()
        let panelSize = container.bounds.size
        let canvas = NSSize(width: panelSize.width + margin * 2, height: panelSize.height + margin * 2)

        let total = Int(seconds * fps)
        Log.write("filmstrip: script=\(script) frames=\(total) panel=\(panelSize) dir=\(dir)")
        for i in 0..<total {
            let t = Double(i) / fps
            if listStrip {
                showStandInSessions(at: t)
                relayout()
            }
            if packStrip, mascotNames.count > 1 {
                // Down the list and back to the top, which is what the comment here used to
                // claim and the arithmetic underneath it never did. It matters most with the two
                // packs that ship: walking down them once is a single change in the middle of
                // the clip, which reads as a picture that happens to have two halves rather than
                // as a list being walked. Down and back is two changes, in opposite directions,
                // and the second one is what says the first was not a cut.
                let stops = Array(0..<mascotNames.count) + Array((0..<(mascotNames.count - 1)).reversed())
                let beat = seconds / Double(stops.count)
                let want = stops[min(stops.count - 1, Int(t / beat))]
                if want != mascotIndex { pickMascot(want, closeList: false) }
            }
            let step = Self.timeline(script: script, t: t, seconds: seconds, text: text)

            if step.provisional {
                textView.setPlainText("")
                textView.beginDictation()
                textView.updateDictation(step.text)
            } else {
                textView.setPlainText(step.text)
            }
            micButton.isListening = step.listening
            micButton.isThinking = step.thinking
            micButton.level = step.level
            micButton.hasSecondPass = true
            setVoiceStatus(step.status, colour: Style.accent)
            mascot.play(step.routine, then: step.routine)
            mascot.frozenTime = step.mascotTime
            relayout()
            // The list strip's footer names the session the rows are pointed at, and changes
            // with them; `fake` is computed once and would pin it to one project for the whole
            // clip, next to a status line that was moving.
            setFooter(listStrip ? Self.standInTarget(row: standInRow) : fake)

            guard let rep = container.bitmapImageRepForCachingDisplay(in: container.bounds) else { continue }
            container.cacheDisplay(in: container.bounds, to: rep)
            let panel = NSImage(size: panelSize)
            panel.addRepresentation(rep)

            FilmFrame.write(size: canvas,
                            to: URL(fileURLWithPath: dir)
                                .appendingPathComponent(String(format: "f%04d.png", i)).path) {
                if withTerminal {
                    Self.drawTerminal(NSRect(origin: .zero, size: canvas),
                                      selected: Self.standInSelection(at: t))
                } else {
                    Self.drawBackdrop(NSRect(origin: .zero, size: canvas))
                }

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
        /// The dictation half of the storyboard.
        var listening = false
        var thinking = false
        var level: Float = 0
        var status = ""
        /// Draw the text as speech in progress: underlined, the way it really looks.
        var provisional = false
    }

    /// The demo storyboard. The timings are fixed on purpose: the README image has to be reproducible.
    /// A stand-in for the target label. Real tab titles would publish what the machine happens
    /// to be working on, and every picture in this repo is meant to be safe to publish.
    /// A made-up session list, for the picture of one.
    ///
    /// The list is the feature that most needs a picture and the one that most cannot have a
    /// real one: every row is a project name and a task title off this machine. So the rows are
    /// invented — three projects, three states, and the marks come from the same renderer the
    /// real rows use, so the picture cannot show something the app would not draw.
    static func standInSessions(at t: Double = -1)
        -> [(label: String, state: SessionState, hue: Int, project: String)] {
        // A still shows what the rows *are*; the strip has to show what they *do*, so the states
        // move. One session is answered and goes quiet, one finishes, and one that was quiet
        // starts asking — which is the whole argument for the list in four seconds.
        let asking: SessionState = t < 0 || t < 2.6 ? .waiting : .idle
        let long: SessionState = t < 0 || t < 4.0
            ? .working("Herding… (54s)") : .idle
        let last: SessionState = t < 0 || t < 3.2 ? .idle : .waiting
        // Five made-up projects rather than one: a tab title is the task, and the point of the
        // mark beside it is that two tasks can read alike while belonging to different work.
        return [("investigate the webhook", .working("Crystallizing… (13m 46s · ↓ 48.2k tokens)"), 2, "harbour"),
                ("port the Android feature", asking, 9, "atlas"),
                ("evaluate the coverage", .idle, 5, "pier"),
                ("rename the split components", long, 13, "relay"),
                ("draft the release notes", last, 6, "mono")]
    }

    /// Which row the strip has walked to by then — ⌘K, then ↓ twice.
    static func standInSelection(at t: Double) -> Int {
        switch t {
        case ..<1.4: return 0
        case ..<3.0: return 1
        default:     return 4
        }
    }

    static func standInTarget(row: (label: String, state: SessionState, hue: Int, project: String)? = nil)
        -> NSAttributedString {
        let out = NSMutableAttributedString()
        out.append(NSAttributedString(string: "● ", attributes: [
            .foregroundColor: Style.accent, .font: NSFont.systemFont(ofSize: 10)]))
        out.append(NSAttributedString(string: "✳ " + (row?.project ?? "my-project"), attributes: [
            .foregroundColor: NSColor.secondaryLabelColor,
            .font: NSFont.systemFont(ofSize: Style.hintSize)]))
        out.append(NSAttributedString(string: "  2/3", attributes: [
            .foregroundColor: NSColor.tertiaryLabelColor,
            .font: NSFont.monospacedDigitSystemFont(ofSize: Style.hintSize - 0.5, weight: .regular)]))
        return out
    }

    private static func timeline(script: String, t: Double, seconds: Double, text: String) -> Step {
        var s = Step()

        // Dictation, end to end: listen, hear it come out live, then watch the second pass
        // replace it. Shot rather than described, because "it gets better when you stop" is the
        // one thing about this feature that a screenshot cannot show.
        if script == "voice" {
            // text=<what was heard>|<what the second pass makes of it>, so the same storyboard
            // can be shot in whatever language the page it is going on is written in.
            let halves = text.split(separator: "|", maxSplits: 1).map(String.init)
            let live = halves.first ?? "cambia el retry a exponential backoff"
            let better = halves.count > 1 ? halves[1]
                : "cambia el retry a exponential backoff, y después corre los tests."
            let speakEnd = seconds * 0.52
            let thinkEnd = seconds * 0.74
            s.routine = "typing"
            s.mascotTime = t
            if t < speakEnd {
                // The words arrive as they are heard, underlined, and the halo answers a voice.
                let p = t / speakEnd
                s.text = String(live.prefix(Int(Double(live.count) * min(1, p * 1.15))))
                s.provisional = true
                s.listening = true
                s.level = Float(0.35 + 0.35 * abs(sin(t * 7)))
                s.status = L.t.voiceListeningWhisper()
            } else if t < thinkEnd {
                s.text = live
                s.provisional = true
                s.thinking = true
                s.status = L.t.voiceTranscribing(seconds: t - speakEnd)
            } else {
                // Settled: the better sentence, and the line gone from under it.
                s.text = better
                s.routine = "idle"
                s.mascotTime = t - thinkEnd
            }
            return s
        }

        // Any routine name plays that routine straight through, which is how the pack
        // gallery and the per-routine clips are shot.
        // The list strip is not about the character or the box: you have just pressed ⌘K and are
        // looking at the rows, so the box is empty and the mascot is doing what it does.
        if script == "sessions" || script == "rows" || script == "mascots" {
            s.routine = "idle"
            s.mascotTime = t
            s.text = ""
            return s
        }
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

    /// A terminal to put the bar in front of, for the strip that is about switching sessions.
    ///
    /// **This one is drawn rather than photographed, and that is a deliberate exception.** Every
    /// other picture in docs/assets is the app rendering itself, so that a change to the app
    /// changes the picture. This cannot be: the other half of what it shows is somebody else's
    /// application, and there is no permission this can be granted that would let it record a
    /// screen. Drawing it is honest here for a reason that took an argument to see — **a
    /// terminal has no canonical appearance.** Fonts, colours, tab position, whether the tab bar
    /// is even shown: all of it is configured, so nobody's iTerm2 looks like anybody else's, and
    /// a schematic is not pretending otherwise.
    ///
    /// What has to be true is the *behaviour*: the tab that is selected, and the status line
    /// underneath it, follow the row the bar is pointing at. That is the claim being made.
    /// The hue of the session a tab belongs to, for tinting its screen.
    private static func row0Hue(_ index: Int) -> Int {
        let rows = standInSessions()
        return rows[min(max(0, index), rows.count - 1)].hue
    }

    /// A few lines of invented transcript per tab, newest last.
    ///
    /// Kept short and dull deliberately: this sits behind a translucent card and its job is to be
    /// *different* when the tab changes, not to be read. Anything long enough to read would pull
    /// the eye off the thing the clip is about.
    private static func standInScreen(_ index: Int) -> [(String, Int)] {
        let screens: [[(String, Int)]] = [
            [("> why does the retry fire twice", 1),
             ("The second one is the queue's, not ours — it redelivers", 0),
             ("on any non-2xx, and we answer 500 before logging.", 0),
             ("  Read  handlers/webhook.rb", 2),
             ("  Grep  redeliver", 2)],
            [("> port the picker to the Android build", 1),
             ("The iOS one leans on a modal presentation that has no", 0),
             ("equivalent here. Two options, and they differ in what", 0),
             ("happens when the user rotates mid-choice.", 0),
             ("  Read  app/src/main/java/…/Picker.kt", 2)],
            [("> is the coverage number honest", 1),
             ("No. It counts generated files, which are 40% of the", 0),
             ("lines and are never wrong in an interesting way.", 0),
             ("  Bash  bundle exec rspec --dry-run", 2)],
            [("> rename SplitPane to Divider everywhere", 1),
             ("Forty-one call sites. Three of them are in strings that", 0),
             ("end up in the UI, so those are not a rename.", 0),
             ("  Grep  SplitPane", 2),
             ("  Edit  src/components/Divider.tsx", 2)],
            [("> draft the release notes", 1),
             ("Reading the log since the last tag. Most of it is one", 0),
             ("change described five times, so this is shorter than", 0),
             ("the commit count suggests.", 0),
             ("  Bash  git log --oneline v0.4.0..HEAD", 2)],
        ]
        return screens[min(max(0, index), screens.count - 1)]
    }

    private static func drawTerminal(_ rect: NSRect, selected: Int) {
        let rows = standInSessions()
        NSColor(srgbRed: 0.055, green: 0.055, blue: 0.066, alpha: 1).setFill()
        rect.fill()

        func text(_ str: String, _ x: CGFloat, _ y: CGFloat, _ size: CGFloat,
                  _ colour: NSColor, mono: Bool = true, bold: Bool = false) -> CGFloat {
            let font = mono
                ? NSFont.monospacedSystemFont(ofSize: size, weight: bold ? .bold : .regular)
                : NSFont.systemFont(ofSize: size, weight: bold ? .bold : .regular)
            let a = NSAttributedString(string: str, attributes: [.font: font, .foregroundColor: colour])
            a.draw(at: NSPoint(x: x, y: y))
            return a.size().width
        }

        // The tab bar. Names are shortened the way a tab does it, because a tab is narrow and
        // that is the whole reason the bar's list exists.
        let tabH: CGFloat = 26
        let tabY = rect.maxY - tabH
        NSColor(white: 0.11, alpha: 1).setFill()
        NSRect(x: 0, y: tabY, width: rect.width, height: tabH).fill()
        let tabW = rect.width / CGFloat(rows.count)
        for (i, row) in rows.enumerated() {
            let box = NSRect(x: CGFloat(i) * tabW, y: tabY, width: tabW, height: tabH)
            if i == selected {
                NSColor(white: 0.17, alpha: 1).setFill()
                box.fill()
                Style.accent.setFill()
                NSRect(x: box.minX, y: box.maxY - 2, width: box.width, height: 2).fill()
            }
            let name = row.label.count > 22 ? String(row.label.prefix(21)) + "…" : row.label
            let colour = i == selected ? NSColor.white.withAlphaComponent(0.85)
                                       : NSColor.white.withAlphaComponent(0.35)
            _ = text("✳ " + name, box.minX + 12, box.minY + 7, 9.5, colour, mono: false)
        }

        // What the tab is showing underneath. **Without this the clip cannot make its own point:**
        // the caption says the terminal's tab and status line follow the selection, and against an
        // empty black rectangle the only thing that visibly moves is a highlight sliding along a
        // strip of grey. A reader has to take the claim on trust from a picture that was supposed
        // to be the evidence.
        //
        // Invented, like every other word in this scene, and different per tab on purpose — the
        // switch has to be legible in one frame, so the text changes *and* one line carries the
        // project's own hue, which is the fastest difference an eye picks up.
        //
        // Drawn from under the tab bar downward and simply covered where the card lands. Placing
        // it around the card instead would tie this to the card's size, and the card is laid out
        // from the list it happens to be showing.
        let screen = standInScreen(selected)
        var line = tabY - 22
        for (text_, kind) in screen {
            guard line > 96 else { break }
            let colour: NSColor
            switch kind {
            case 0: colour = NSColor.white.withAlphaComponent(0.30)                 // prose
            case 1: colour = ProjectIcon.demoGrid(hue: row0Hue(selected)).accent.withAlphaComponent(0.55)
            default: colour = NSColor.white.withAlphaComponent(0.16)                // a tool line
            }
            _ = text(text_, 22, line, 9, colour)
            line -= 15
        }

        // The status line the terminal itself draws — the project's mark and name, what it is
        // doing, where it is, and the budget. The bar's footer is a copy of this on purpose, and
        // showing both is the clearest way to say so.
        let row = rows[min(selected, rows.count - 1)]
        let grid = ProjectIcon.demoGrid(hue: row.hue)
        var y: CGFloat = 46
        if let icon = grid.image(height: 22) {
            icon.draw(in: NSRect(x: 16, y: y - 6, width: icon.size.width, height: icon.size.height))
        }
        var x: CGFloat = 52
        x += text(row.project, x, y + 2, 10.5, grid.accent, mono: false, bold: true) + 10
        _ = text(row.label, x, y + 2, 10.5, NSColor.white.withAlphaComponent(0.75), mono: false)

        y -= 15
        x = 52
        x += text("~/code/" + row.project, x, y, 9.5, NSColor.white.withAlphaComponent(0.30)) + 12
        x += text("⎇ main", x, y, 9.5, NSColor.white.withAlphaComponent(0.30)) + 8
        x += text("*1", x, y, 9.5, NSColor.white.withAlphaComponent(0.30)) + 10
        x += text("✓ ci", x, y, 9.5, NSColor(srgbRed: 0.45, green: 0.75, blue: 0.45, alpha: 1)) + 12
        _ = text("Opus 5 (1M context) · xhigh", x, y, 9.5, NSColor.white.withAlphaComponent(0.30))

        y -= 15
        x = 16
        x += text("⏵⏵ ", x, y, 9.5, Style.accent)
        _ = text("auto mode on (shift+tab to cycle)", x, y, 9.5, NSColor.white.withAlphaComponent(0.30))
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

    var targetSummary: String { currentTarget?.displayLabel ?? L.t.menuNoTarget }

    /// Used by the menu bar's mascot submenu.
    var mascotNamesForMenu: [String] { MascotPack.available() }
    func selectMascot(named name: String) {
        Config.shared.mascot = name
        Config.shared.save()
        if let why = mascot.reload() { Log.write("mascot: \(why)") }
        if panel.isVisible { mascot.play("pop"); relayout() }
        // The island holds its own copy of the character and has to be told as well.
        NotificationCenter.default.post(name: .clawdlineConfigChanged, object: nil)
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
