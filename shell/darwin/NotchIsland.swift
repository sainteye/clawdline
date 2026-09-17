// The black rectangle at the top of a MacBook's screen, borrowed.
//
// Ported from the Swift app's Sources/NotchIsland.swift (1,148 lines) and the
// part of Sources/Mascot.swift that draws a pack. The reasoning in the original
// is kept where the code is kept, because it is what says why a number is what
// it is; where this app had to differ, the difference is written beside it.
//
// **Why this one is native while everything else moves to the web.** The rule
// for this rewrite is that the pictures live in the React console and the shell
// keeps only what a web page cannot do. A window flush with the top of the
// screen, above the menu bar, on every Space, that the window server is asked
// not to constrain, is exactly that: there is no browser surface for it on any
// platform. So the island is drawn here, and docs/cross-platform.md says what
// Linux and Windows get instead.
//
// What it reads is the daemon's own reading of the machine — the `sessions`
// frames the console is already streaming from /v1/events, which is the same
// inventory /v1/sessions answers. Nothing here looks at a terminal.
import AppKit
import WebKit

enum Notch {

    /// How far the drawn shape overshoots the reported notch on each side.
    ///
    /// The rectangle macOS reports is the area windows may not use, and the
    /// *physical* cutout is a shade wider than that with rounded corners at its
    /// bottom. A shape drawn to the reported width therefore leaves a sliver of
    /// wallpaper showing in each of those corners — a hairline seam across the
    /// join, which is the one thing the whole illusion cannot survive.
    static let overshoot: CGFloat = 4

    /// The concave flare where the shape meets the menu bar, and the convex
    /// corners at the bottom. The notch is a specific shape and these are what
    /// make a rectangle stop looking like a rectangle stuck underneath it.
    static let flare: CGFloat = 6
    static let corner: CGFloat = 14

    /// The notch's own rectangle, in screen coordinates, or nil on a display
    /// without one.
    ///
    /// Read from the two menu-bar areas macOS reports either side of it rather
    /// than from a table of machines: the gap between them *is* the notch, on
    /// any model, including ones that do not exist yet.
    static func rect(of screen: NSScreen) -> NSRect? {
        guard screen.safeAreaInsets.top > 0,
              let left = screen.auxiliaryTopLeftArea,
              let right = screen.auxiliaryTopRightArea,
              right.minX > left.maxX
        else { return nil }
        return NSRect(x: left.maxX,
                      y: screen.frame.maxY - screen.safeAreaInsets.top,
                      width: right.minX - left.maxX,
                      height: screen.safeAreaInsets.top)
    }

    /// The notch outline, in a view's own coordinates: concave where it meets
    /// the menu bar, rounded where it hangs below.
    ///
    /// `NSBezierPath` has no quadratic segment before macOS 14, so each one is
    /// written as the cubic it is equal to — the control points are two thirds
    /// of the way from each end to the quadratic's single control point.
    static func path(in b: NSRect, pill: Bool = false) -> NSBezierPath {
        // A display with no cutout has nothing to merge with, so the shape is
        // just a shape: a rounded pill hanging under the menu bar. Drawing the
        // notch outline there would flare outwards into a top edge that is not
        // the top of anything — the concave corners only read as "this grew out
        // of the hole" when there is a hole above them.
        if pill {
            return NSBezierPath(roundedRect: b, xRadius: b.height / 2, yRadius: b.height / 2)
        }
        let path = NSBezierPath()
        let top = b.maxY, bottom = b.minY
        let f = min(flare, b.width / 2), c = min(corner, b.width / 2 - f, b.height)

        func quad(from p0: NSPoint, to p1: NSPoint, via ctrl: NSPoint) {
            let c1 = NSPoint(x: p0.x + 2.0 / 3 * (ctrl.x - p0.x), y: p0.y + 2.0 / 3 * (ctrl.y - p0.y))
            let c2 = NSPoint(x: p1.x + 2.0 / 3 * (ctrl.x - p1.x), y: p1.y + 2.0 / 3 * (ctrl.y - p1.y))
            path.curve(to: p1, controlPoint1: c1, controlPoint2: c2)
        }

        path.move(to: NSPoint(x: b.minX, y: top))
        quad(from: NSPoint(x: b.minX, y: top),
             to: NSPoint(x: b.minX + f, y: top - f),
             via: NSPoint(x: b.minX + f, y: top))
        path.line(to: NSPoint(x: b.minX + f, y: bottom + c))
        quad(from: NSPoint(x: b.minX + f, y: bottom + c),
             to: NSPoint(x: b.minX + f + c, y: bottom),
             via: NSPoint(x: b.minX + f, y: bottom))
        path.line(to: NSPoint(x: b.maxX - f - c, y: bottom))
        quad(from: NSPoint(x: b.maxX - f - c, y: bottom),
             to: NSPoint(x: b.maxX - f, y: bottom + c),
             via: NSPoint(x: b.maxX - f, y: bottom))
        path.line(to: NSPoint(x: b.maxX - f, y: top - f))
        quad(from: NSPoint(x: b.maxX - f, y: top - f),
             to: NSPoint(x: b.maxX, y: top),
             via: NSPoint(x: b.maxX - f, y: top))
        path.close()
        return path
    }

    /// The screen to hang this off: whichever one the pointer is on.
    ///
    /// Not "the one with the notch". Plugged into a display, that is the laptop
    /// screen — which is behind you, or shut. An ambient signal on a screen you
    /// are not looking at is not an ambient signal.
    ///
    /// The external one has no cutout, so there the shape becomes a pill under
    /// the menu bar instead. It is the same information wearing the only
    /// costume available.
    static func screen() -> NSScreen? {
        let p = NSEvent.mouseLocation
        return NSScreen.screens.first { NSMouseInRect(p, $0.frame, false) }
            ?? NSScreen.screens.first(where: { rect(of: $0) != nil })
            ?? NSScreen.main
    }
}

/// What the island is showing. Ordered by who deserves the screen: a question
/// beats a celebration, and a celebration beats a progress report.
///
/// `resting` is the floor rather than an absence, and it is the one the machine
/// is in for most of the *day*: nothing is running, and the character is still
/// up there, asleep. `hidden` is what the config asks for and what a screen with
/// no cutout gets — see `refresh()`.
///
/// One case is thinner than the Swift app's: `working` there carries the live
/// line of the session in front, and the snapshot this shell hears has no such
/// field — the console derives that line from a session's screen, one request at
/// a time. The tip says the count and the name, and nothing it cannot know.
enum IslandMode: Equatable {
    case hidden
    case resting
    case working(count: Int)
    case waiting(String)
    case finished(String)
}

/// One session, as the console's `sessions` frame describes it.
///
/// The daemon's own reading (`internal/contract.InventorySession`): the id is
/// what every /v1/sessions/… route takes, the label is what the list draws, and
/// the state is the same word the list colours its rows by.
struct IslandSession: Equatable {
    let id: String
    let label: String
    let state: String
}

final class NotchIsland: NSObject, WKScriptMessageHandler {

    static let shared = NotchIsland()
    private override init() { super.init() }

    private var panel: NSPanel?
    private var view: IslandView?
    private var mode: IslandMode = .hidden
    private var celebrationUntil: CFAbsoluteTime = 0
    private var celebrating: IslandSession?
    /// Which session the shape is currently talking about. Decided with the
    /// words rather than looked up again when a click arrives — see `refresh()`.
    private(set) var subject: IslandSession?
    private var retract: DispatchWorkItem?

    /// What the page last said was on this machine, and the one thing two
    /// consecutive frames say that neither says alone: something finished.
    private var rows: [IslandSession] = []
    private var heardOnce = false
    /// A session that stopped between the last frame and this one, waiting to be
    /// celebrated. **Consumed once.** It is derived from a change rather than
    /// read from a field, so a `refresh()` that came from anywhere else — the
    /// screen clock, the end of the last celebration — would otherwise find the
    /// same change still true and start the dance again, forever.
    private var pendingFinished: IslandSession?

    /// Opening the console: what the character is pressed for. Handed in by the
    /// shell when the island is attached, so nothing here reaches for a global.
    private var openConsole: (() -> Void)?

    /// The pointer can walk to another display without a single session
    /// changing, and the island would stay on the screen you walked away from.
    /// In the Swift app this came free — `SessionWatch` took a reading on its
    /// own clock and every observer ran — and here the page only speaks when
    /// something changes, so the screen is looked at on a slow clock of its own.
    private var watch: Timer?

    // MARK: - Mounting

    /// The island's half of the page bridge, and where a press on the character
    /// goes. Called once, from the shell's `buildWindow`.
    ///
    /// A second listener on the console's own stream rather than a second
    /// stream: every connection to /v1/events makes the daemon take its own
    /// reading of the machine, so a stream opened from here to learn what the
    /// page already knows would double that work. The script wraps whatever
    /// `window.EventSource` is by then — the shell's fleet bridge wraps it
    /// first — and both wrappers watch the one object the page asked for.
    func attach(to content: WKUserContentController, open: @escaping () -> Void) {
        openConsole = open
        content.addUserScript(WKUserScript(source: Self.bridgeScript,
                                           injectionTime: .atDocumentStart,
                                           forMainFrameOnly: true))
        content.add(WeakMessageHandler(self), name: "shellIsland")
    }

    private static let bridgeScript = """
    (function () {
      var Native = window.EventSource;
      var handlers = window.webkit && window.webkit.messageHandlers;
      if (!Native || !handlers || !handlers.shellIsland) return;
      var post = function (m) { try { handlers.shellIsland.postMessage(m); } catch (e) {} };
      function Islanded(url, init) {
        var es = init === undefined ? new Native(url) : new Native(url, init);
        try {
          if (String(url).indexOf('/v1/events') !== -1) {
            es.addEventListener('sessions', function (ev) {
              try {
                var snap = JSON.parse(ev.data);
                var rows = Array.isArray(snap) ? snap : ((snap && snap.sessions) || []);
                var out = [];
                for (var i = 0; i < rows.length; i++) {
                  var s = rows[i] || {};
                  var id = String(s.id || '');
                  if (!id) continue;
                  out.push({ id: id, label: String(s.label || id), state: String(s.state || '') });
                }
                post({ kind: 'sessions', rows: out });
              } catch (e) { post({ kind: 'unreadable' }); }
            });
            es.addEventListener('error', function () { post({ kind: 'error' }); });
          }
        } catch (e) {}
        return es;
      }
      Islanded.prototype = Native.prototype;
      Islanded.CONNECTING = Native.CONNECTING;
      Islanded.OPEN = Native.OPEN;
      Islanded.CLOSED = Native.CLOSED;
      window.EventSource = Islanded;
    })();
    """

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        // Only the console this shell loaded may speak, as with the settings
        // bridge: a page it navigated to by a link is somebody else's.
        guard message.name == "shellIsland",
              message.frameInfo.isMainFrame,
              message.frameInfo.request.url?.host == home.host,
              message.frameInfo.request.url?.port == home.port,
              let body = message.body as? [String: Any],
              let kind = body["kind"] as? String else { return }
        switch kind {
        case "sessions":
            let rows = (body["rows"] as? [[String: Any]] ?? []).compactMap { row -> IslandSession? in
                guard let id = row["id"] as? String, !id.isEmpty else { return nil }
                return IslandSession(id: id,
                                     label: (row["label"] as? String) ?? id,
                                     state: (row["state"] as? String) ?? "")
            }
            update(rows)
        case "error", "unreadable":
            // Not known is drawn as quiet, never as the last thing that was
            // true — the same rule the menu bar mark follows. Nothing has
            // finished, either: a stream that dropped is not a celebration.
            if kind == "unreadable" { shellLog("island: a sessions frame could not be read") }
            update([], finished: false)
        default:
            return
        }
    }

    /// Take one frame.
    ///
    /// `finished` is false for a frame that says nothing rather than says
    /// nothing is running: a stream that dropped is not a celebration.
    private func update(_ next: [IslandSession], finished: Bool = true) {
        // Sessions that were working a moment ago and are not now — the Swift
        // app's `SessionWatch.justFinished`, derived here from two consecutive
        // frames because this shell holds no reading of its own. It is the one
        // thing a reading cannot say alone: "idle" is the same word for a
        // session that has been quiet all afternoon and one that stopped a
        // second ago, and only the second is worth a celebration.
        if finished, heardOnce {
            let busyNow = Set(next.filter { $0.state == "working" }.map(\.id))
            if let done = rows.first(where: { $0.state == "working" && !busyNow.contains($0.id) }) {
                pendingFinished = done
            }
        }
        rows = next
        heardOnce = true
        refresh()
    }

    // MARK: - On and off

    /// Start listening. Safe to call again — this is also the "something in the
    /// config changed" path, so the character is re-read here.
    func install() {
        guard NextConfig.shared.notch else {
            shellLog("island: notch is off in \(NextConfig.shared.fileURL.path); nothing is drawn")
            teardown()
            return
        }
        if watch == nil {
            let t = Timer(timeInterval: 1.5, repeats: true) { [weak self] _ in self?.refresh() }
            RunLoop.main.add(t, forMode: .common)
            watch = t
        }
        view?.reloadMascot()
        refresh()
    }

    func teardown() {
        watch?.invalidate()
        watch = nil
        retract?.cancel()
        tipWork?.cancel()
        view?.stopMascot()
        panel?.orderOut(nil)
        tipPanel?.orderOut(nil)
        panel = nil
        tipPanel = nil
        view = nil
        mode = .hidden
    }

    // MARK: - What to show

    private var waiting: [IslandSession] { rows.filter { $0.state == "waiting" } }
    private var working: [IslandSession] { rows.filter { $0.state == "working" } }

    private func refresh() {
        guard NextConfig.shared.notch else { return }

        // A finished session is a moment rather than a state, so it is held for
        // a few seconds and then falls back to whatever is true underneath it.
        if let done = pendingFinished {
            pendingFinished = nil
            celebrating = done
            celebrationUntil = CFAbsoluteTimeGetCurrent() + 3.4
            let work = DispatchWorkItem { [weak self] in self?.refresh() }
            retract?.cancel()
            retract = work
            DispatchQueue.main.asyncAfter(deadline: .now() + 3.5, execute: work)
        }

        let next: IslandMode
        // **Worked out here, with the words, and remembered.** The two used to
        // be decided separately — the label when the shape was drawn, the
        // session when it was clicked — so pressing an ear went to whatever was
        // true a moment later rather than to the thing it was pointing at.
        if let asking = waiting.first {
            next = .waiting(asking.label)
            subject = asking
        } else if let done = celebrating, CFAbsoluteTimeGetCurrent() < celebrationUntil {
            next = .finished(done.label)
            subject = done
        } else {
            celebrating = nil
            let busy = working
            // Nothing running is not nothing to show. The character stays and
            // sleeps, because a mascot that only exists while a job does is a
            // progress indicator wearing a costume, and the costume was the
            // point.
            //
            // **Except on a screen with no cutout.** There the shape is a pill
            // hanging under the menu bar rather than the notch having grown —
            // fine for the half minute a job takes, and quite another thing
            // parked over somebody's menu bar all day.
            next = busy.isEmpty ? (onNotch ? .resting : .hidden)
                                : .working(count: busy.count)
            subject = busy.first
        }
        show(next)
    }

    /// Whether the screen this would land on has a camera housing to grow out of.
    private var onNotch: Bool { Notch.screen().flatMap { Notch.rect(of: $0) } != nil }

    // MARK: - Drawing it

    private var lastScreen: NSRect = .zero

    private func show(_ next: IslandMode) {
        // The screen counts as part of "has anything changed": the mode can sit
        // still for an hour while you move to another display, and an island
        // that only redraws when a session does would stay on the screen you
        // walked away from.
        let onScreen = Notch.screen()?.frame ?? .zero
        guard next != mode || onScreen != lastScreen else { return }
        let wasHidden = mode == .hidden || onScreen != lastScreen
        lastScreen = onScreen
        mode = next

        guard next != .hidden else {
            // Out of the way entirely rather than shrunk to nothing: an
            // invisible window at the top of the screen still swallows the
            // clicks that land on it.
            tipWork?.cancel()
            hideTip()
            close()
            return
        }
        guard let screen = Notch.screen() else { return }
        let notch = Notch.rect(of: screen)
        let panel = ensurePanel()
        let view = self.view

        // The band it lives in is the menu bar's, not the cutout's. They differ
        // by a few points — 32 against 38 on this machine — and the taller one
        // is the right one: it puts the ears level with the menu bar they sit
        // among, and leaves the shape standing a little proud of the cutout,
        // which is what makes it read as the notch having grown rather than as
        // a sticker stuck over it.
        let bar = notch.map { max($0.height, screen.frame.maxY - screen.visibleFrame.maxY) }
            ?? NSStatusBar.system.thickness
        // Zero where there is no cutout: on a pill there is no hole to leave
        // alone, so the two ears simply sit next to each other.
        let core = notch.map { $0.width + Notch.overshoot } ?? 0
        let (left, right) = ears(for: next, bar: bar)

        view?.mode = next
        view?.notchWidth = core
        view?.leftEar = left
        view?.playRoutine(for: next)
        view?.toolTip = tip(for: next)

        let width = Notch.flare * 2 + left + core + right
        let midX = notch?.midX ?? screen.frame.midX
        // Positioned so the *gap* lands on the cutout, not so the window is
        // centred: the ears are different widths, and it is the hole in the
        // middle that has to line up.
        let x = midX - (Notch.flare + left + core / 2)
        // Flush with the top of the screen when there is a notch to merge with;
        // under the menu bar when there is not, because a black rectangle over
        // the clock is not charming.
        let top = notch != nil ? screen.frame.maxY
                               : screen.frame.maxY - NSStatusBar.system.thickness - 2
        let frame = NSRect(x: x.rounded(), y: (top - bar).rounded(),
                           width: width.rounded(), height: bar.rounded())

        // The interesting number is the one that comes back: a window put
        // somewhere it is not allowed to be does not fail, it lands somewhere
        // else.
        shellLog("island: \(next) asked \(frame)")
        if wasHidden {
            // Arriving with no ears at all, so the first thing it does is grow
            // sideways out of the cutout rather than appear at full width out
            // of nowhere.
            let seed = NSRect(x: (midX - (Notch.flare + core / 2)).rounded(), y: frame.minY,
                              width: (core + Notch.flare * 2).rounded(), height: frame.height)
            panel.setFrame(seed, display: false)
            panel.alphaValue = 1
            panel.orderFrontRegardless()
        }
        NSAnimationContext.runAnimationGroup { ctx in
            ctx.duration = wasHidden ? 0.42 : 0.34
            // Overshoots and settles. The whole appeal of the thing this is
            // copying is that the shape has some weight to it; easing politely
            // to a stop looks like a menu opening.
            ctx.timingFunction = CAMediaTimingFunction(controlPoints: 0.2, 1.35, 0.3, 1)
            panel.animator().setFrame(frame, display: true)
            panel.animator().alphaValue = 1
        } completionHandler: {
            if panel.frame != frame { shellLog("island: landed at \(panel.frame) instead") }
            // Whether it is on screen is not the same question as where it was
            // put, and neither is answered by the frame: a window can be exactly
            // where it was asked for, at the right level, and still be behind
            // something or off every display.
            shellLog("island: visible=\(panel.isVisible)"
                + " occluded=\(!panel.occlusionState.contains(.visible))"
                + " level=\(panel.level.rawValue) screen=\(panel.screen?.frame.size.width ?? -1)"
                + " ears=\(left)/\(right) mascot=\(self.view?.mascotFrame ?? .zero)")
        }
        view?.needsDisplay = true
    }

    /// What hovering it says. The same sentences the menu bar mark uses, so the
    /// two cannot disagree about what is happening.
    private func tip(for mode: IslandMode) -> String? {
        switch mode {
        case .hidden, .resting:
            // Asleep says nothing, because there is nothing to say and a tip
            // that fires every time the pointer crosses the menu bar is a
            // flinch. See `hover(_:)`.
            return nil
        case .working(let count):
            return [L.t.statusWorking(count), subject?.label]
                .compactMap { $0 }.joined(separator: "\n")
        case .waiting:
            return L.t.statusWaiting(waiting.map(\.label))
        case .finished(let who):
            return L.t.islandDone + " — " + who
        }
    }

    // MARK: - The tip, drawn by hand

    private var tipPanel: NSPanel?
    private var tipWork: DispatchWorkItem?

    private func hover(_ inside: Bool) {
        tipWork?.cancel()
        guard inside, let text = tip(for: mode), !text.isEmpty else { hideTip(); return }
        // A beat before it appears, the way a real tip behaves: the pointer
        // crosses the menu bar on its way to somewhere else all day, and a panel
        // that pops up every time it passes is not information, it is a flinch.
        let work = DispatchWorkItem { [weak self] in self?.showTip(text) }
        tipWork = work
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.45, execute: work)
    }

    private func showTip(_ text: String) {
        guard let anchor = panel else { return }
        let font = NSFont.systemFont(ofSize: 11.5)
        let label = NSTextField(wrappingLabelWithString: text)
        label.font = font
        label.textColor = NSColor.white.withAlphaComponent(0.92)
        label.preferredMaxLayoutWidth = 320
        label.sizeToFit()
        let size = NSSize(width: min(320, label.frame.width) + 20, height: label.frame.height + 12)

        let p = tipPanel ?? {
            let n = IslandPanel(contentRect: .zero, styleMask: [.borderless, .nonactivatingPanel],
                                backing: .buffered, defer: false)
            n.isOpaque = false
            n.backgroundColor = .clear
            n.hasShadow = true
            n.appearance = NSAppearance(named: .darkAqua)
            n.isFloatingPanel = true
            // One above the island, so it is never behind the thing it belongs to.
            n.level = .mainMenu + 4
            n.collectionBehavior = [.canJoinAllSpaces, .stationary, .ignoresCycle,
                                    .fullScreenAuxiliary]
            n.ignoresMouseEvents = true      // it must not eat the click it is explaining
            tipPanel = n
            return n
        }()

        let body = IslandTipView(frame: NSRect(origin: .zero, size: size))
        label.frame = NSRect(x: 10, y: 6, width: size.width - 20, height: label.frame.height)
        body.addSubview(label)
        p.contentView = body
        // Under the shape and centred on it, unless that would run off the side
        // of the screen.
        var x = anchor.frame.midX - size.width / 2
        if let visible = anchor.screen?.visibleFrame {
            x = min(max(visible.minX + 8, x), visible.maxX - size.width - 8)
        }
        p.setFrame(NSRect(x: x.rounded(), y: (anchor.frame.minY - size.height - 6).rounded(),
                          width: size.width, height: size.height), display: true)
        p.orderFrontRegardless()
    }

    private func hideTip() {
        tipPanel?.orderOut(nil)
    }

    /// How far the shape reaches out on each side of the cutout.
    ///
    /// Nothing can be drawn *in* the notch — there is a camera behind it and
    /// those pixels do not exist. Everything happens in the two strips beside
    /// it: the left ear is the character, the right ear is the words, and the
    /// hole in the middle is left alone.
    private func ears(for mode: IslandMode, bar: CGFloat) -> (left: CGFloat, right: CGFloat) {
        switch mode {
        case .hidden:
            return (0, 0)
        case .resting:
            // The character and nothing else: no count, no name, no second ear.
            // Deliberately the same measurements as one session working, so
            // waking up moves the routine and not the shape.
            return (bar + IslandView.inset * 2, 0)
        case .working(let count):
            // The character, and a number only when there is more than one
            // thing to count. This is the state the machine is in most of the
            // day, so it has to be able to sit in the menu bar without becoming
            // one of the things you read there.
            return (bar + IslandView.inset * 2, count > 1 ? 30 : 0)
        case .waiting(let who), .finished(let who):
            // Measured, so a short task name does not get a long ear of empty
            // black after it.
            let text = (who as NSString).size(withAttributes: [
                .font: NSFont.systemFont(ofSize: 12, weight: .medium),
            ]).width
            return (bar + IslandView.inset * 2, min(250, max(100, text.rounded() + 44)))
        }
    }

    private func close() {
        guard let panel else { return }
        view?.stopMascot()
        NSAnimationContext.runAnimationGroup({ ctx in
            ctx.duration = 0.2
            panel.animator().alphaValue = 0
        }, completionHandler: { panel.orderOut(nil) })
    }

    private func ensurePanel() -> NSPanel {
        if let panel { return panel }
        let p = IslandPanel(contentRect: NSRect(x: 0, y: 0, width: 10, height: 10),
                            styleMask: [.borderless, .nonactivatingPanel],
                            backing: .buffered, defer: false)
        p.isOpaque = false
        p.backgroundColor = .clear
        p.hasShadow = false
        p.isMovable = false
        // The character is drawn in its own colours on black; a light system
        // appearance was tinting the text drawn beside it.
        p.appearance = NSAppearance(named: .darkAqua)
        p.isFloatingPanel = true
        // Tool tips need the window to be told about the pointer moving over it;
        // without this the tip never appears, and nothing says why.
        p.acceptsMouseMovedEvents = true
        // **After `isFloatingPanel`, and that is not a style preference.**
        // Setting that property assigns `.floating` — level 3 — to the window,
        // so a level set before it is thrown away without a word. The first
        // version of this spent its life at 3: fine below the menu bar, and
        // completely invisible the moment it moved up into it, which is drawn
        // at 24. The log said `visible=true occluded=false` the whole time,
        // because it was: behind the menu bar is still on screen.
        p.level = .mainMenu + 3
        p.hidesOnDeactivate = false
        p.becomesKeyOnlyIfNeeded = true
        // Follows you between desktops and does not join the window cycle: this
        // is furniture, not a window somebody is meant to manage. It stays over
        // a full-screen app for the same reason the menu bar does.
        p.collectionBehavior = [.canJoinAllSpaces, .stationary, .ignoresCycle, .fullScreenAuxiliary]

        let v = IslandView(frame: .zero)
        v.onHover = { [weak self] inside in self?.hover(inside) }
        v.onClickMascot = { [weak self] in self?.openConsole?() }
        v.onClickEar = { [weak self] in self?.reveal() }
        p.contentView = v
        view = v
        panel = p
        return p
    }

    // MARK: - Pressing it

    /// The right ear goes to the session it names.
    ///
    /// Straight to the terminal tab rather than to the window: that ear says
    /// "*that* one", and what you were about to do is look at it. In the Swift
    /// app that was `Targets.reveal`, which drove iTerm2 and tmux directly; here
    /// the daemon owns the machine, so it is `POST /v1/sessions/{id}/focus` —
    /// the same route the console's own "在 Mac 上顯示" uses, so the two cannot
    /// select different things.
    private func reveal() {
        // More than one thing behind that number, so the number cannot answer
        // "which one" and must not pick for you. The count is the only part of
        // this that is ever plural, and a silent guess there is the same bug as
        // the one that used to send clicks to whichever session happened to be
        // running.
        let choices = candidates()
        if choices.count > 1 {
            offer(choices)
            return
        }
        guard let wanted = choices.first ?? subject else {
            openConsole?()
            return
        }
        focus(wanted)
    }

    /// Every session the current shape is speaking for, not just the one it had
    /// room to name.
    private func candidates() -> [IslandSession] {
        switch mode {
        case .waiting:  return waiting
        case .working:  return working
        case .finished: return subject.map { [$0] } ?? []
        case .hidden, .resting: return []
        }
    }

    /// Select that session's terminal tab, through the daemon.
    ///
    /// Selecting, not activating: the route says so, and what comes forward
    /// afterwards is up to whichever emulator is drawing that tmux. The local
    /// token is what this shell already puts in the window's cookie jar; the
    /// route is behind the send gate, because moving somebody's keyboard is not
    /// a read.
    private func focus(_ session: IslandSession) {
        let allowed = CharacterSet(charactersIn: "-_.!~*'()").union(.alphanumerics)
        guard let id = session.id.addingPercentEncoding(withAllowedCharacters: allowed),
              let url = URL(string: "v1/sessions/\(id)/focus", relativeTo: home) else {
            shellLog("island: \(session.label) has an id this shell cannot put in a URL")
            openConsole?()
            return
        }
        guard let token = LocalToken.read() else {
            // No token is not a failure to report to the person; it is the
            // ordinary state before the daemon has written one. Give them the
            // window instead of nothing.
            shellLog("island: no local token yet; opening the console instead of selecting a tab")
            openConsole?()
            return
        }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.timeoutInterval = 15
        shellLog("island: revealing \(session.label)")
        URLSession.shared.dataTask(with: request) { _, response, error in
            // The label and the status, never the token.
            let code = (response as? HTTPURLResponse)?.statusCode ?? -1
            if let error {
                shellLog("island: could not select \(session.label): \(error.localizedDescription)")
            } else if code != 200 {
                shellLog("island: the daemon refused to select \(session.label) (\(code))")
            }
        }.resume()
    }

    /// A menu under the ear, one row per session.
    ///
    /// Built fresh every time rather than kept: it is a list of what is running
    /// *now*, and a menu held over from the last press would offer sessions that
    /// have since finished.
    private func offer(_ sessions: [IslandSession]) {
        guard let view else { return }
        let menu = NSMenu()
        menu.font = NSFont.systemFont(ofSize: 13)
        for session in sessions {
            let item = NSMenuItem(title: session.label, action: #selector(jump(_:)), keyEquivalent: "")
            item.target = self
            item.representedObject = session.id
            menu.addItem(item)
        }
        // The way out of "what is running" and into "everything".
        //
        // A row here rather than a second click on the ear: the first click puts
        // a menu up, and a menu runs its own event loop — the click that would
        // have been the second one is spent dismissing it, and never reaches the
        // view at all.
        menu.addItem(.separator())
        let all = NSMenuItem(title: L.t.islandAllSessions, action: #selector(showAll),
                             keyEquivalent: "k")
        all.keyEquivalentModifierMask = .command
        all.target = self
        menu.addItem(all)

        // Under the ear that was pressed, in the view's own coordinates.
        let ear = view.rightEarRect
        menu.popUp(positioning: nil, at: NSPoint(x: ear.minX, y: ear.minY - 4), in: view)
    }

    @objc private func showAll() {
        openConsole?()
    }

    @objc private func jump(_ sender: NSMenuItem) {
        guard let id = sender.representedObject as? String,
              let session = rows.first(where: { $0.id == id }) else { return }
        focus(session)
    }
}

/// A window that is allowed to be where windows are not allowed to be.
///
/// AppKit runs every frame you set through `constrainFrameRect(_:to:)`, which
/// keeps windows from covering the menu bar — sensible for every window ever
/// written except this one. Asking for a frame flush with the top of the screen
/// quietly got one pushed down to just under the menu bar instead, so the whole
/// shape hung *below* the notch rather than growing out of it: a black tab
/// floating under a black notch, with a seam across the middle, which is
/// precisely the illusion this depends on not having. The proposal is returned
/// untouched.
final class IslandPanel: NSPanel {
    override func constrainFrameRect(_ frameRect: NSRect, to screen: NSScreen?) -> NSRect {
        frameRect
    }
    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
}

/// The little dark slab the tip is written on. Rounded, and the same near-black
/// as the shape it hangs off, so the two read as one object rather than as a
/// window that has appeared.
final class IslandTipView: NSView {
    override func draw(_ dirtyRect: NSRect) {
        NSColor(white: 0.10, alpha: 0.96).setFill()
        NSBezierPath(roundedRect: bounds, xRadius: 8, yRadius: 8).fill()
        NSColor(white: 1, alpha: 0.12).setStroke()
        let edge = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5),
                                xRadius: 8, yRadius: 8)
        edge.lineWidth = 1
        edge.stroke()
    }
}

/// The shape and its contents. One view, because there are only ever a few
/// things on it and a hierarchy of subviews for three labels is a worse thing to
/// read than the drawing code.
final class IslandView: NSView {

    var mode: IslandMode = .hidden { didSet { needsDisplay = true } }
    /// The width of the hole in the middle, and how much of this view sits to
    /// the left of it. Everything is laid out around these: the camera is behind
    /// that gap and anything drawn there is drawn on the back of it.
    var notchWidth: CGFloat = 0
    var leftEar: CGFloat = 0
    var onClickMascot: (() -> Void)?
    var onClickEar: (() -> Void)?

    private let mascot = IslandMascotView(frame: .zero)
    /// What was asked for last time a routine was chosen. Only `resting` is ever
    /// read out of it: leaving that state is a beat, and every other change is a
    /// cut.
    private var lastPlayed: IslandMode = .hidden

    /// Read the pack again — the menu is not the only thing that names one.
    func reloadMascot() {
        mascot.reload()
        needsLayout = true
    }

    /// Let go of the timer while nothing is on screen. A sixty-a-second redraw
    /// of a window that has been ordered out is work nobody can see.
    func stopMascot() { mascot.stop() }

    /// For the log: where the character actually ended up, which is not always
    /// where the layout says it should be.
    var mascotFrame: NSRect { mascot.frame }
    /// Where the words are, for hanging a menu off.
    var rightEarRect: NSRect { right }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        addSubview(mascot)
    }
    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { false }

    func playRoutine(for mode: IslandMode) {
        // Getting up is not the same as arriving, and this is the only place
        // that can tell the two apart: the shape does not move between resting
        // and one session working, so without this the character would go from
        // asleep to at-work between two frames.
        let waking = lastPlayed == .resting && mode != .resting
        lastPlayed = mode
        // Eyes shut for the whole of resting, and only for the fallback. A pack
        // with its own `sleep` routine said so in its keys and is left alone —
        // an author who drew a squint for sleeping should get their squint.
        mascot.eyesOverride = (mode == .resting && !mascot.has("sleep")) ? "blink" : nil
        switch mode {
        case .resting:
            // Nothing is running, and the character is asleep rather than gone.
            //
            // This is on screen all day, so the budget for how much it may do is
            // not the budget the other states get. A pack that has `sleep`
            // states its own pace and is obeyed; a pack that does not gets its
            // own `idle` at a little under half speed, which lands on roughly
            // the same five-second breath, with the eyes held shut above.
            let resting = mascot.has("sleep") ? "sleep" : "idle"
            if mascot.routine != resting { mascot.play(resting, then: resting) }
            mascot.rate = resting == "sleep" ? 1 : 0.45
        case .finished:
            mascot.rate = 1
            mascot.play(mascot.has("dance") ? "dance" : "cheer", then: "idle")
        case .waiting:
            // Waiting is not busy. Whatever it was doing, it has stopped and is
            // looking at you.
            mascot.rate = 1
            mascot.play(mascot.has("pop") ? "pop" : "idle", then: "idle")
        case .working(let count):
            // How hard it is working is how much *you* have running.
            //
            // Two ways at once, because either alone is too subtle: past a
            // couple of sessions it switches to the busiest looping routine the
            // pack has, and underneath that the clock itself speeds up. One
            // session is the character's own pace; six is visibly frantic.
            // Capped, because past a point faster stops reading as busy and
            // starts reading as broken.
            let busy = count >= 3 && mascot.has("typing") ? "typing" : "idle"
            // Woken up, so it gets up: one pass of `stretch` and then straight
            // into the work. Reusing the routine every pack already has rather
            // than writing a transition — it is a yawn with its eyes opening on
            // the first key, which is exactly the beat that was missing.
            if waking, mascot.has("stretch") {
                mascot.play("stretch", then: busy)
            } else if mascot.routine != busy, mascot.routine != "stretch" {
                mascot.play(busy, then: busy)
            }
            mascot.rate = min(2.6, 1 + Double(count - 1) * 0.35)
        case .hidden:
            break
        }
        mascot.start()
    }

    /// The strip to the left of the hole, and the strip to the right of it.
    private var left: NSRect {
        NSRect(x: Notch.flare, y: 0, width: leftEar, height: bounds.height)
    }
    private var right: NSRect {
        let x = Notch.flare + leftEar + notchWidth
        return NSRect(x: x, y: 0, width: max(0, bounds.width - Notch.flare - x), height: bounds.height)
    }

    /// How far the contents keep away from the outside edges.
    ///
    /// Without it an ear is exactly as wide as what is in it, so the character
    /// sits *on* the rounded corner and the count sits on the other one —
    /// everything shoved against the two outermost points of the shape, which is
    /// where nothing should be. The shape needs to look like it has room in it.
    static let inset: CGFloat = 10

    override func layout() {
        super.layout()
        guard bounds.height > 6 else { return }
        // Sized to the band rather than to a number picked by hand, so a pack
        // drawn twice as tall as the shipped one still stands in the menu bar
        // rather than through it.
        mascot.fit(height: bounds.height - 12)
        // Nowhere, rather than wherever it was last. With no ear there is no
        // strip to stand in, and a frame left over from the last time there was
        // one puts the character on the camera housing.
        guard leftEar > 0 else { mascot.frame = .zero; return }
        // Against the hole rather than against the outside edge: the character
        // should look like it is standing next to the notch, which is the thing
        // it is leaning on.
        let side = bounds.height
        mascot.frame = NSRect(x: left.maxX - side - Self.inset, y: 1,
                              width: side, height: bounds.height - 2)
    }

    /// The two ears are two different buttons, because they are two different
    /// sentences.
    ///
    /// The character is the app: pressing it should give you the app. The words
    /// are a particular session: pressing them should give you that session. One
    /// handler for both had to guess, and guessed "the session" — so clicking
    /// the little animal took you to a terminal tab, which is the one place the
    /// animal exists to keep you out of.
    ///
    /// The middle does nothing on purpose. There is a camera behind it and
    /// nothing drawn on it, and a click target you cannot see is a click target
    /// that will be pressed by accident.
    var onHover: ((Bool) -> Void)?

    /// **`.activeAlways`, and that is the whole reason this exists.**
    ///
    /// `NSView.toolTip` was set here first and never once appeared: macOS shows
    /// tool tips for the *active* application, and this app is by construction
    /// usually not the active one — you are in a terminal, which is the point. A
    /// tracking area asked for `.activeAlways` gets entered and exited events
    /// regardless of who is in front, so the tip can be drawn by hand.
    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(
            rect: .zero,
            options: [.mouseEnteredAndExited, .mouseMoved, .activeAlways, .inVisibleRect],
            owner: self))
    }

    override func mouseEntered(with event: NSEvent) { pointer(at: event) }
    override func mouseMoved(with event: NSEvent) { pointer(at: event) }

    override func mouseExited(with event: NSEvent) {
        onHover?(false)
        NSCursor.arrow.set()
        wasOverEar = false
    }

    private var wasOverEar = false

    /// Set the cursor, and say whether there is anything here worth a tip.
    ///
    /// **`resetCursorRects` is not usable here for the same reason `toolTip` was
    /// not**: cursor rectangles belong to the active application, and this one
    /// usually is not. So the cursor is set by hand from the tracking area,
    /// which was asked for `.activeAlways`.
    ///
    /// Set on every move rather than once on entering: the window server
    /// re-asserts its own cursor as the pointer crosses windows, and a single
    /// `set()` on the way in is undone by the next thing that happens.
    private func pointer(at event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        let overEar = left.contains(p) || right.contains(p)
        // The middle is a camera. Nothing is drawn there and nothing happens
        // when it is clicked, so it must not offer a hand — a pointer over a
        // hole is a promise of something that cannot be there.
        if overEar { NSCursor.pointingHand.set() } else { NSCursor.arrow.set() }
        guard overEar != wasOverEar else { return }
        wasOverEar = overEar
        onHover?(overEar)
    }

    override func mouseDown(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        if left.contains(p) { onClickMascot?() }
        else if right.contains(p) { onClickEar?() }
    }

    override func draw(_ dirtyRect: NSRect) {
        // The body. Black, because the cutout it grows out of is black and the
        // join between the two has to be invisible.
        NSColor.black.setFill()
        Notch.path(in: bounds, pill: notchWidth <= 0).fill()

        let r = right
        switch mode {
        case .hidden, .resting:
            // Asleep is the character and the black shape and nothing else. No
            // dot, no name, no count — there is nothing to count, and a mark on
            // an empty ear is a thing to read.
            return
        case .working(let count):
            guard count > 1 else { return }
            draw("\(count)", in: NSRect(x: r.minX + (r.width - 20) / 2, y: r.midY - 7,
                                        width: 20, height: 14),
                 colour: accent, size: 11, weight: .semibold, align: .center)
        case .waiting(let who):
            ear(who, dot: accent, in: r)
        case .finished(let who):
            ear(who, dot: NSColor.systemGreen, in: r)
        }
    }

    /// The right ear: a dot in the colour that says what happened, then which
    /// session it happened to. One line, because the ear is as tall as the menu
    /// bar and a second line there is a second line nobody can read.
    ///
    /// The Swift app draws the project's own mark between the two. That registry
    /// is Go-side here (`internal/domain/icon`, served with /v1/projects) and the
    /// snapshot this shell hears carries no project, so the dot and the name are
    /// what there is; see docs/cross-platform.md for what it would take.
    private func ear(_ name: String, dot: NSColor, in r: NSRect) {
        let d: CGFloat = 5
        var x = r.minX + Self.inset
        // The dot is the *state* — accent for a question, green for something
        // that has finished.
        dot.setFill()
        NSBezierPath(ovalIn: NSRect(x: x, y: r.midY - d / 2, width: d, height: d)).fill()
        x += d + 7

        draw(name, in: NSRect(x: x, y: r.midY - 8, width: max(0, r.maxX - Self.inset - x),
                              height: 16),
             colour: NSColor.white.withAlphaComponent(0.92), size: 12, weight: .medium)
    }

    private func draw(_ text: String, in rect: NSRect, colour: NSColor, size: CGFloat,
                      weight: NSFont.Weight, align: NSTextAlignment = .left) {
        let style = NSMutableParagraphStyle()
        style.alignment = align
        style.lineBreakMode = .byTruncatingTail
        let attrs: [NSAttributedString.Key: Any] = [
            .font: NSFont.systemFont(ofSize: size, weight: weight),
            .foregroundColor: colour,
            .paragraphStyle: style,
        ]
        NSAttributedString(string: text, attributes: attrs).draw(in: rect)
    }
}

// MARK: - The character
//
// A mascot is data, not code: everything about how it looks and moves lives in
// one JSON file under Resources/mascots, so swapping in a different character
// never means editing this file. The format is the Swift app's, byte for byte —
// the packs shipped here are its packs — and this is the part of its
// Sources/Mascot.swift that reads and draws one. What is not carried is the
// card: the panel that used to hold this character is a web page now, and a
// mascot on it will be drawn by the page, not by this.

/// One frame of the animation: which pose to draw and how to transform it.
struct IslandMascotFrame {
    var pose: String = ""
    var dx: CGFloat = 0
    var dy: CGFloat = 0
    var rot: CGFloat = 0        // radians
    var sx: CGFloat = 1
    var sy: CGFloat = 1
    var eyes: String = "open"   // open | blink | happy
}

struct IslandMascotPack: Decodable {
    struct Grid: Decodable {
        let cols: Int
        let rows: Int
    }

    struct Key: Decodable {
        let t: Double           // 0…1, a fraction of the routine's duration
        var pose: String?
        var dx: Double?
        var dy: Double?
        var rot: Double?
        var sx: Double?
        var sy: Double?
        var eyes: String?
        var ease: String?       // linear (default) | inout | out
    }

    /// How big to draw it, independent of how fine the grid is. Without this, a
    /// 32x24 pack would render half the size of a 16x11 one for no reason other
    /// than having more cells — resolution would decide scale.
    struct Display: Decodable {
        var height: Double?
        var overlap: Double?
        var jumpRoom: Double?
        var sideRoom: Double?
        var footInset: Double?
    }

    struct Blink: Decodable {
        var everyMin: Double
        var everyMax: Double
        var duration: Double
    }

    struct Routine: Decodable {
        let duration: Double
        var loop: Bool?
        var keys: [Key]
        var blink: Blink?
    }

    let name: String
    var author: String?
    let grid: Grid
    /// Character in a pose row → colour. `accent` follows the app tint;
    /// `transparent` paints nothing.
    let palette: [String: String]
    /// Which characters are eyes. Blink hides the top row of them, happy hides
    /// the bottom row.
    var eyeChars: [String]?
    /// The character a closed eye turns into — whatever the face is made of.
    var skin: String?
    var display: Display?
    let poses: [String: [String]]
    let routines: [String: Routine]

    // Defaults chosen to reproduce the original hand-tuned Clawd geometry exactly.
    var spriteHeight: CGFloat { CGFloat(display?.height ?? 77) }
    var jumpRoom: CGFloat { CGFloat(display?.jumpRoom ?? 41) }
    var sideRoom: CGFloat { CGFloat(display?.sideRoom ?? 12) }
    var footInset: CGFloat { CGFloat(display?.footInset ?? 6) }

    /// One cell, in points. Rounded so pixel edges land on whole points and stay
    /// crisp.
    var cellSize: CGFloat { max(1, (spriteHeight / CGFloat(grid.rows)).rounded()) }
    var spriteSize: NSSize {
        NSSize(width: cellSize * CGFloat(grid.cols), height: cellSize * CGFloat(grid.rows))
    }

    // MARK: Loading

    /// Search order: this app's own pack directory first, then the ones bundled
    /// with it. A user copy always wins, so editing a bundled pack never gets
    /// undone by an update.
    static func load(named name: String) -> (pack: IslandMascotPack?, error: String?) {
        var candidates: [URL] = [MascotPacks.userDirectory.appendingPathComponent("\(name).json")]
        if let bundled = Bundle.main.url(forResource: name, withExtension: "json", subdirectory: "mascots") {
            candidates.append(bundled)
        }
        guard let url = candidates.first(where: { FileManager.default.fileExists(atPath: $0.path) }) else {
            return (nil, "No mascot named \"\(name)\" in \(MascotPacks.userDirectory.path)")
        }
        do {
            let pack = try JSONDecoder().decode(IslandMascotPack.self, from: Data(contentsOf: url))
            if let problem = pack.validate() { return (nil, "\(url.lastPathComponent): \(problem)") }
            return (pack, nil)
        } catch {
            return (nil, "\(url.lastPathComponent): \(error.localizedDescription)")
        }
    }

    // MARK: Validation
    //
    // Every message here names the file, the key and what was expected. A mascot
    // pack is usually written by an agent that cannot see the screen, so the
    // error text is the only feedback it gets — "invalid pack" would leave it
    // guessing.

    func validate() -> String? {
        guard grid.cols > 0, grid.rows > 0 else { return "grid.cols and grid.rows must be positive" }
        for (poseName, rows) in poses {
            guard rows.count == grid.rows else {
                return "pose \"\(poseName)\" has \(rows.count) rows, grid.rows says \(grid.rows)"
            }
            for (i, row) in rows.enumerated() where row.count != grid.cols {
                return "pose \"\(poseName)\" row \(i) is \(row.count) characters, grid.cols says \(grid.cols)"
            }
            for row in rows {
                for ch in row where palette[String(ch)] == nil {
                    return "pose \"\(poseName)\" uses \"\(ch)\", which is not in palette"
                }
            }
        }
        if poses.isEmpty { return "no poses defined" }
        for (routineName, routine) in routines {
            guard routine.duration > 0 else { return "routine \"\(routineName)\" needs a positive duration" }
            guard !routine.keys.isEmpty else { return "routine \"\(routineName)\" has no keys" }
            for key in routine.keys {
                if let p = key.pose, poses[p] == nil {
                    return "routine \"\(routineName)\" refers to pose \"\(p)\", which does not exist"
                }
            }
        }
        for required in ["idle"] where routines[required] == nil {
            return "routine \"\(required)\" is required"
        }
        return nil
    }

    // MARK: Sampling

    /// `#RGB` / `#RRGGBB` / `#RRGGBBAA`, written here rather than as an
    /// extension on `NSColor`: this file adds no members to a framework type
    /// that another part of the shell might add differently.
    static func colour(hex: String) -> NSColor? {
        var s = hex.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("#") { s.removeFirst() }
        if s.count == 3 { s = s.map { "\($0)\($0)" }.joined() }
        guard s.count == 6 || s.count == 8, let v = UInt64(s, radix: 16) else { return nil }
        let shift = s.count == 8 ? 8 : 0
        let a = s.count == 8 ? CGFloat(v & 0xFF) / 255 : 1
        return NSColor(srgbRed: CGFloat((v >> (16 + shift)) & 0xFF) / 255,
                       green: CGFloat((v >> (8 + shift)) & 0xFF) / 255,
                       blue: CGFloat((v >> shift) & 0xFF) / 255,
                       alpha: a)
    }

    func color(for ch: Character, accent: NSColor) -> NSColor? {
        guard let spec = palette[String(ch)] else { return nil }
        switch spec.lowercased() {
        case "transparent", "none", "": return nil
        case "accent": return accent
        default: return Self.colour(hex: spec) ?? accent
        }
    }

    var eyeCharacterSet: Set<Character> {
        Set((eyeChars ?? ["o"]).flatMap { $0 })
    }

    /// The character an eye turns into when it closes.
    var skinCharacter: Character {
        if let s = skin, let ch = s.first { return ch }
        for (ch, spec) in palette where spec.lowercased() == "accent" { return Character(ch) }
        return palette.keys.sorted().first.map { Character($0) } ?? "#"
    }

    /// Interpolate the routine at `time` seconds. Transforms blend between keys;
    /// pose and eyes step, because a half-drawn pose is not a thing.
    func frame(routine routineName: String, at time: Double) -> IslandMascotFrame {
        guard let routine = routines[routineName] ?? routines["idle"] else { return IslandMascotFrame() }
        let looping = routine.loop ?? false
        let raw = looping ? time.truncatingRemainder(dividingBy: routine.duration) : min(time, routine.duration)
        let p = routine.duration > 0 ? raw / routine.duration : 0

        let keys = routine.keys.sorted { $0.t < $1.t }
        guard !keys.isEmpty else { return IslandMascotFrame() }
        var before = keys[0]
        var after = keys[keys.count - 1]
        for (i, k) in keys.enumerated() where k.t <= p {
            before = k
            after = i + 1 < keys.count ? keys[i + 1] : k
        }

        let span = after.t - before.t
        var u = span > 0 ? (p - before.t) / span : 0
        u = max(0, min(1, u))
        switch after.ease ?? "linear" {
        case "inout": u = u < 0.5 ? 4 * u * u * u : 1 - pow(-2 * u + 2, 3) / 2
        case "out":   u = 1 - pow(1 - u, 3)
        default:      break
        }

        func mix(_ a: Double?, _ b: Double?, _ dflt: Double) -> CGFloat {
            let from = a ?? dflt, to = b ?? a ?? dflt
            return CGFloat(from + (to - from) * u)
        }

        var f = IslandMascotFrame()
        f.dx = mix(before.dx, after.dx, 0)
        f.dy = mix(before.dy, after.dy, 0)
        f.rot = mix(before.rot, after.rot, 0)
        f.sx = mix(before.sx, after.sx, 1)
        f.sy = mix(before.sy, after.sy, 1)
        // Stepped: whatever the last key that mentioned one said.
        f.pose = lastValue(keys, upTo: p, \.pose) ?? poses.keys.sorted().first ?? ""
        f.eyes = lastValue(keys, upTo: p, \.eyes) ?? "open"
        return f
    }

    private func lastValue(_ keys: [Key], upTo p: Double, _ path: KeyPath<Key, String?>) -> String? {
        var found: String?
        for k in keys where k.t <= p {
            if let v = k[keyPath: path] { found = v }
        }
        return found ?? keys.first?[keyPath: path]
    }
}

/// Draws the current mascot pack.
///
/// The animation deliberately avoids Core Animation: a 60fps timer drives it and
/// `draw()` computes each offset and scale. A CALayer transform pivots on its
/// anchorPoint, and for AppKit layer-backed views that point depends on context —
/// guess wrong and the sprite grows out of a corner.
final class IslandMascotView: NSView {
    private(set) var routine = "idle"
    private(set) var pack: IslandMascotPack?
    private(set) var loadError: String?

    private var started = CACurrentMediaTime()
    private var fallback = "idle"
    private var nextBlink = Double.random(in: 1.8...4.0)
    private var timer: Timer?

    /// Hold one expression whatever the routine says, and stop the random blink
    /// while it is held.
    ///
    /// There is exactly one caller and it is the island's resting state. A pack
    /// that has its own `sleep` routine has already shut the eyes in its keys
    /// and this stays nil; a pack written before resting existed falls back to
    /// its `idle`, and `idle` is a character that is awake and — worse —
    /// blinking, which is the one thing a sleeping character must not do.
    var eyesOverride: String?

    /// How fast the clock runs, as a multiple.
    ///
    /// A pack states its own timings and everything else obeys them — so "look
    /// busier" cannot be a new routine without asking every author to draw one.
    /// Running the same routine faster is something every pack already supports
    /// without knowing it.
    var rate: Double = 1 {
        didSet {
            guard rate != oldValue, rate > 0 else { return }
            // Rebased rather than just scaled from zero, or a change of pace
            // would jump the character to a different point in its routine
            // instead of carrying on from here.
            let now = CACurrentMediaTime()
            started = now - (now - started) * (oldValue / rate)
            needsDisplay = true
        }
    }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.masksToBounds = false
        reload()
    }
    required init?(coder: NSCoder) { fatalError() }

    // MARK: Pack

    /// One entry per character in the palette: the colour it paints, and the key
    /// the cell paths are grouped under. Nil for the ones that paint nothing.
    ///
    /// **Resolved per character rather than per cell.** Parsing a hex string
    /// into a fresh `NSColor` and asking that colour to describe itself is two
    /// allocations for every one of a couple of hundred cells, sixty times a
    /// second. The island sleeps up there all day, so that loop runs whether or
    /// not anybody is looking, and it is where the drawing time goes.
    private var swatches: [Character: (color: NSColor, key: String)?] = [:]
    private var swatchAccent: NSColor?
    private var colors: [String: NSColor] = [:]

    @discardableResult
    func reload() -> String? {
        swatches.removeAll()
        let result = IslandMascotPack.load(named: NextConfig.shared.mascot)
        if let p = result.pack {
            pack = p
            loadError = nil
        } else {
            loadError = result.error
            shellLog("mascot: \(result.error ?? "unknown error")")
        }
        needsDisplay = true
        return loadError
    }

    // MARK: Playback

    /// Whether this pack defines a routine. A pack that does not is not broken —
    /// playing a missing routine falls back to `idle`, which reads as nothing
    /// happening — so callers ask first and pick something the pack does have.
    func has(_ routine: String) -> Bool { pack?.routines[routine] != nil }

    func play(_ r: String, then back: String = "idle") {
        routine = r
        fallback = back
        started = CACurrentMediaTime()
        nextBlink = Double.random(in: 1.8...4.0)
        startTimer()
        needsDisplay = true
    }

    func stop() {
        timer?.invalidate()
        timer = nil
    }

    /// Keep drawing without changing what is being drawn: the island wants the
    /// character breathing the moment it appears, whatever it is already doing.
    func start() { startTimer() }

    private func startTimer() {
        guard timer == nil else { return }
        let t = Timer(timeInterval: 1.0 / 60.0, repeats: true) { [weak self] _ in
            self?.needsDisplay = true
        }
        // .common: keep animating while a menu is open.
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    private func finishOneShot(_ routine: IslandMascotPack.Routine, elapsed: Double) {
        guard !(routine.loop ?? false), elapsed >= routine.duration, self.routine != fallback else { return }
        // Changing state inside draw() only takes effect next tick, so bounce it
        // to the main queue.
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.play(self.fallback, then: self.fallback)
        }
    }

    // MARK: Geometry

    /// Drawn smaller than the pack asks for, without the pack having to know.
    ///
    /// A pack states the size it was drawn for and the card obeys it — that is
    /// the contract, and it is why a mascot somebody made looks right without
    /// being told about the layout. The island is a thirty-point band under a
    /// camera housing, and no pack is going to be drawn for that, so it asks for
    /// the same character at a third of the size instead of asking authors to
    /// supply a second one.
    var scale: CGFloat = 1 { didSet { needsDisplay = true } }

    /// Scale the character so it stands roughly this tall, whatever size its
    /// pack was drawn for.
    func fit(height: CGFloat) {
        guard let pack, pack.spriteSize.height > 0, height > 0 else { return }
        scale = height / pack.spriteSize.height
    }

    /// Where the sprite sits in view coordinates. Feet near the bottom edge, the
    /// rest is jump room.
    var spriteRect: NSRect {
        guard let pack else { return .zero }
        let s = pack.spriteSize
        return NSRect(x: (bounds.width - s.width * scale) / 2, y: pack.footInset * scale,
                      width: s.width * scale, height: s.height * scale)
    }

    // MARK: Painting

    override func draw(_ dirtyRect: NSRect) {
        guard let pack else { return }
        let t = (CACurrentMediaTime() - started) * rate
        var f = pack.frame(routine: routine, at: t)

        if let r = pack.routines[routine] { finishOneShot(r, elapsed: t) }

        // Blinking is random rather than keyframed: a blink on a fixed beat
        // looks mechanical.
        if let blink = pack.routines[routine]?.blink {
            if t > nextBlink {
                if t < nextBlink + blink.duration { f.eyes = "blink" }
                else { nextBlink = t + Double.random(in: blink.everyMin...blink.everyMax) }
            }
        }
        // After the blink, not before it: the point of holding an expression is
        // to override the routine *and* the random one on top of it.
        if let held = eyesOverride { f.eyes = held }

        guard let grid = pack.poses[f.pose] ?? pack.poses.values.first else { return }
        let rect = spriteRect
        guard rect.width > 0, rect.height > 0 else { return }
        let cell = rect.width / CGFloat(pack.grid.cols)
        let eyeChars = pack.eyeCharacterSet
        // Thrown away if the tint ever differs. It is a constant today, and a
        // cache that would silently outlive it becoming a setting is not worth
        // the two lines it saves.
        let tint = accent
        if swatchAccent != tint {
            swatches.removeAll()
            swatchAccent = tint
        }
        let eyeTop = grid.firstIndex { $0.contains(where: { eyeChars.contains($0) }) } ?? 0

        NSGraphicsContext.saveGraphicsState()
        let tf = NSAffineTransform()
        let cx = rect.midX, cy = rect.minY + rect.height * 0.42   // pivot low, so it turns on its feet
        // The offsets are points, stated for the size the pack was drawn at, so
        // they scale with it: a jump that clears the character's own head on the
        // card must not clear the screen in the island.
        tf.translateX(by: cx + f.dx * scale, yBy: cy + f.dy * scale)
        tf.rotate(byRadians: f.rot)
        tf.scaleX(by: f.sx, yBy: f.sy)
        tf.translateX(by: -cx, yBy: -cy)
        tf.concat()

        // Collect each colour into one path and fill once. Filling cell by cell
        // antialiases every boundary separately once scaling lands them
        // off-pixel, and a faint grid appears on the body.
        var paths: [String: NSBezierPath] = [:]
        for (r, row) in grid.enumerated() {
            for (c, character) in row.enumerated() {
                var ch = character
                // Blink hides the top row of the eyes, happy hides the bottom row.
                if eyeChars.contains(ch) {
                    let hide = (f.eyes == "blink" && r == eyeTop) || (f.eyes == "happy" && r != eyeTop)
                    if hide { ch = pack.skinCharacter }
                }
                let swatch: (color: NSColor, key: String)?
                if let hit = swatches[ch] {
                    swatch = hit
                } else {
                    swatch = pack.color(for: ch, accent: tint).map { ($0, $0.description) }
                    swatches[ch] = swatch
                }
                guard let (color, key) = swatch else { continue }
                let path = paths[key] ?? { let p = NSBezierPath(); paths[key] = p; return p }()
                path.appendRect(NSRect(x: rect.minX + CGFloat(c) * cell,
                                       y: rect.minY + rect.height - CGFloat(r + 1) * cell,
                                       width: cell, height: cell))
                colors[key] = color
            }
        }
        for (key, path) in paths {
            colors[key]?.setFill()
            path.fill()
        }
        NSGraphicsContext.restoreGraphicsState()
    }
}
