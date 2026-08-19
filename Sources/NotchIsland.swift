import AppKit
import Foundation

/// The black rectangle at the top of a MacBook's screen, borrowed.
///
/// This one is play, and it is meant to read that way. Nothing here tells you anything the menu
/// bar mark does not already tell you — it is the same reading, wearing a costume. The notch is a
/// hole in the display that Apple put there for a camera, it is already pure black, and a black
/// window drawn flush against it is indistinguishable from the notch having grown. That is the
/// whole trick, and it is too good a trick to leave alone.
///
/// So: your character lives up there. It leans out while something is running, says which session
/// wants you when one does, and dances when a long job finishes. `"notch": false` in the config
/// turns the entire thing off and nothing else changes.
///
/// **Nothing can be drawn where the notch is, so the design is entirely about its two sides.**
/// The camera housing occludes that rectangle — pixels painted behind it are painted on the back
/// of a camera — which the first version of this ignored, hanging a tab *below* the hole instead.
/// That works, in the sense that you can see it, and it is wrong: it covers whatever window is
/// underneath, and it reads as a black box stuck to the notch rather than as the notch itself.
///
/// What every implementation that looks right does instead: sit in the menu bar's own band, the
/// same height as it, and grow **sideways** — the left ear and the right ear, with the hole
/// between them left strictly alone. Nothing is ever covered except menu bar space, and the shape
/// stretching out of the cutout is the animation, rather than something that needs one.
enum Notch {

    /// How far the drawn shape overshoots the reported notch on each side.
    ///
    /// The rectangle macOS reports is the area windows may not use, and the *physical* cutout is
    /// a shade wider than that with rounded corners at its bottom. A shape drawn to the reported
    /// width therefore leaves a sliver of wallpaper showing in each of those corners — a hairline
    /// seam across the join, which is the one thing the whole illusion cannot survive. Every
    /// implementation of this overshoots; four points is what the ones that look right use.
    static let overshoot: CGFloat = 4

    /// The concave flare where the shape meets the menu bar, and the convex corners at the
    /// bottom. Numbers from DynamicNotchKit by way of boring.notch — the notch is a specific
    /// shape and these are what make a rectangle stop looking like a rectangle stuck underneath
    /// it. The straight sides sit `flare` in from the window's edges; the top edge spans the lot.
    static let flare: CGFloat = 6
    static let corner: CGFloat = 14

    /// The notch's own rectangle, in screen coordinates, or nil on a display without one.
    ///
    /// Read from the two menu-bar areas macOS reports either side of it rather than from a table
    /// of machines: the gap between them *is* the notch, on any model, including ones that do not
    /// exist yet.
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

    /// The notch outline, in a view's own coordinates: concave where it meets the menu bar,
    /// rounded where it hangs below.
    ///
    /// `NSBezierPath` has no quadratic segment before macOS 14, so each one is written as the
    /// cubic it is equal to — the control points are two thirds of the way from each end to the
    /// quadratic's single control point.
    static func path(in b: NSRect, pill: Bool = false) -> NSBezierPath {
        // A display with no cutout has nothing to merge with, so the shape is just a shape: a
        // rounded pill hanging under the menu bar. Drawing the notch outline there would flare
        // outwards into a top edge that is not the top of anything — the concave corners only
        // read as "this grew out of the hole" when there is a hole above them.
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
    /// Not "the one with the notch". Plugged into a display, that is the laptop screen — which
    /// is behind you, or shut. An ambient signal on a screen you are not looking at is not an
    /// ambient signal.
    ///
    /// The external one has no cutout, so there the shape becomes a pill under the menu bar
    /// instead. It is the same information wearing the only costume available.
    static func screen() -> NSScreen? {
        let p = NSEvent.mouseLocation
        return NSScreen.screens.first { NSMouseInRect(p, $0.frame, false) }
            ?? NSScreen.screens.first(where: { rect(of: $0) != nil })
            ?? NSScreen.main
    }
}

/// What the island is showing. Ordered by who deserves the screen: a question beats a
/// celebration, and a celebration beats a progress report.
///
/// `resting` is the floor rather than an absence, and it is the one the machine is in for most of
/// the *day*: nothing is running, and the character is still up there, asleep. `hidden` is now
/// only what the config asks for and what a screen with no cutout gets — see `refresh()`.
enum IslandMode: Equatable {
    case hidden
    case resting
    case working(count: Int, line: String?)
    case waiting(String)
    case finished(String)
}

final class NotchIsland: NSObject {

    static let shared = NotchIsland()
    private override init() { super.init() }

    private var panel: NSPanel?
    private var view: IslandView?
    private var mode: IslandMode = .hidden
    private var celebrationUntil: CFAbsoluteTime = 0
    private var celebrating: TargetSession?
    /// Which session the shape is currently talking about. Decided with the words rather than
    /// looked up again when a click arrives — see `refresh()`.
    private(set) var subject: TargetSession?
    private var retract: DispatchWorkItem?

    /// Start listening. Safe to call again — re-registering replaces the observer, and this is
    /// also the "something in the config changed" path, so the character is re-read here.
    func install() {
        guard Config.shared.notch else { teardown(); return }
        SessionWatch.shared.observers["island"] = { [weak self] in self?.refresh() }
        view?.reloadMascot()
        refresh()
    }

    func teardown() {
        SessionWatch.shared.observers.removeValue(forKey: "island")
        retract?.cancel()
        tipWork?.cancel()
        panel?.orderOut(nil)
        tipPanel?.orderOut(nil)
        panel = nil
        tipPanel = nil
        view = nil
        mode = .hidden
    }

    // MARK: - What to show

    private func refresh() {
        guard Config.shared.notch else { return }
        let watch = SessionWatch.shared

        // A finished session is a moment rather than a state, so it is held for a few seconds
        // and then falls back to whatever is true underneath it.
        if let done = watch.justFinished.first {
            celebrating = done
            celebrationUntil = CFAbsoluteTimeGetCurrent() + 3.4
            let work = DispatchWorkItem { [weak self] in self?.refresh() }
            retract?.cancel()
            retract = work
            DispatchQueue.main.asyncAfter(deadline: .now() + 3.5, execute: work)
        }

        let next: IslandMode
        // **Worked out here, with the words, and remembered.** The two used to be decided
        // separately — the label when the shape was drawn, the session when it was clicked — so
        // pressing an ear went to whatever was true a moment later rather than to the thing it
        // was pointing at. The state that made that unmistakable is this one: a session that has
        // just *finished* is neither waiting nor working, so a click on its name went to some
        // third session that happened to be running.
        if let asking = watch.waiting.first {
            next = .waiting(asking.label)
            subject = asking
        } else if let done = celebrating, CFAbsoluteTimeGetCurrent() < celebrationUntil {
            next = .finished(done.label)
            subject = done
        } else {
            celebrating = nil
            let busy = watch.working
            // Nothing running is not nothing to show. The character stays and sleeps, because a
            // mascot that only exists while a job does is a progress indicator wearing a costume,
            // and the costume was the point.
            //
            // **Except on a screen with no cutout.** There the shape is a pill hanging under the
            // menu bar rather than the notch having grown — fine for the half minute a job takes,
            // and quite another thing parked over somebody's menu bar all day. A display without
            // a camera housing behaves exactly as it did before this existed.
            next = busy.isEmpty ? (onNotch ? .resting : .hidden)
                                : .working(count: busy.count, line: watch.liveLine(of: busy[0].id))
            subject = busy.first
        }
        show(next)
    }

    /// Whether the screen this would land on has a camera housing to grow out of.
    private var onNotch: Bool { Notch.screen().flatMap { Notch.rect(of: $0) } != nil }

    // MARK: - Drawing it

    private var lastScreen: NSRect = .zero

    private func show(_ next: IslandMode) {
        // The screen counts as part of "has anything changed": the mode can sit still for an
        // hour while you move to another display, and an island that only redraws when a session
        // does would stay on the screen you walked away from.
        let onScreen = Notch.screen()?.frame ?? .zero
        guard next != mode || onScreen != lastScreen else { return }
        let wasHidden = mode == .hidden || onScreen != lastScreen
        lastScreen = onScreen
        mode = next

        guard next != .hidden else {
            // Out of the way entirely rather than shrunk to nothing: an invisible window at the
            // top of the screen still swallows the clicks that land on it.
            tipWork?.cancel()
            hideTip()
            close()
            return
        }
        guard let screen = Notch.screen() else { return }
        let notch = Notch.rect(of: screen)
        let panel = ensurePanel()
        let view = self.view

        // The band it lives in is the menu bar's, not the cutout's. They differ by a few points —
        // 32 against 38 on this machine — and the taller one is the right one: it puts the ears
        // level with the menu bar they sit among, and leaves the shape standing a little proud of
        // the cutout, which is what makes it read as the notch having grown rather than as a
        // sticker stuck over it.
        let bar = notch.map { max($0.height, screen.frame.maxY - screen.visibleFrame.maxY) }
            ?? NSStatusBar.system.thickness
        // Zero where there is no cutout: on a pill there is no hole to leave alone, so the two
        // ears simply sit next to each other and the shape is centred on the screen.
        let core = notch.map { $0.width + Notch.overshoot } ?? 0
        let (left, right) = ears(for: next, bar: bar)

        view?.mode = next
        view?.notchWidth = core
        view?.leftEar = left
        view?.playRoutine(for: next)
        // A mark and a number can only say *that* something is going on. The rest — which
        // sessions, and what the one in front is actually doing — goes here rather than into a
        // wider shape, because the shape is sitting in the menu bar and the menu bar is not ours.
        view?.projectGrid = subject.flatMap { SessionWatch.shared.grid(of: $0.id) }
        view?.toolTip = tip(for: next)

        let width = Notch.flare * 2 + left + core + right
        let midX = notch?.midX ?? screen.frame.midX
        // Positioned so the *gap* lands on the cutout, not so the window is centred: the ears are
        // different widths, and it is the hole in the middle that has to line up.
        let x = midX - (Notch.flare + left + core / 2)
        // Flush with the top of the screen when there is a notch to merge with; under the menu
        // bar when there is not, because a black rectangle over the clock is not charming.
        let top = notch != nil ? screen.frame.maxY
                               : screen.frame.maxY - NSStatusBar.system.thickness - 2
        let frame = NSRect(x: x.rounded(), y: (top - bar).rounded(),
                           width: width.rounded(), height: bar.rounded())

        // The interesting number is the one that comes back: a window put somewhere it is not
        // allowed to be does not fail, it lands somewhere else.
        Log.write("island: \(next) asked \(frame)")
        if wasHidden {
            // Arriving with no ears at all, so the first thing it does is grow sideways out of
            // the cutout rather than appear at full width out of nowhere.
            let seed = NSRect(x: (midX - (Notch.flare + core / 2)).rounded(), y: frame.minY,
                              width: (core + Notch.flare * 2).rounded(), height: frame.height)
            panel.setFrame(seed, display: false)
            panel.alphaValue = 1
            panel.orderFrontRegardless()
        }
        NSAnimationContext.runAnimationGroup { ctx in
            ctx.duration = wasHidden ? 0.42 : 0.34
            // Overshoots and settles. The whole appeal of the thing this is copying is that the
            // shape has some weight to it; easing politely to a stop looks like a menu opening.
            ctx.timingFunction = CAMediaTimingFunction(controlPoints: 0.2, 1.35, 0.3, 1)
            panel.animator().setFrame(frame, display: true)
            panel.animator().alphaValue = 1
        } completionHandler: {
            if panel.frame != frame { Log.write("island: landed at \(panel.frame) instead") }
            // Whether it is on screen is not the same question as where it was put, and neither
            // is answered by the frame: a window can be exactly where it was asked for, at the
            // right level, and still be behind something or off every display.
            Log.write("island: visible=\(panel.isVisible)"
                + " occluded=\(!panel.occlusionState.contains(.visible))"
                + " level=\(panel.level.rawValue) screen=\(panel.screen?.frame.size.width ?? -1)"
                + " ears=\(left)/\(right) mascot=\(self.view?.mascotFrame ?? .zero)")
        }
        view?.needsDisplay = true
    }

    /// What hovering it says. The same sentences the menu bar mark uses, so the two cannot
    /// disagree about what is happening, plus the live line — which is the one thing here that
    /// nothing else on screen is showing while the panel is down.
    private func tip(for mode: IslandMode) -> String? {
        switch mode {
        case .hidden, .resting:
            // Asleep says nothing, because there is nothing to say and a tip that fires every
            // time the pointer crosses the menu bar is a flinch. See `hover(_:)`.
            return nil
        case .working(let count, let line):
            return [L.t.statusWorking(count), subject?.label, line]
                .compactMap { $0 }.joined(separator: "\n")
        case .waiting:
            return L.t.statusWaiting(SessionWatch.shared.waiting.map(\.label))
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
        // A beat before it appears, the way a real tip behaves: the pointer crosses the menu bar
        // on its way to somewhere else all day, and a panel that pops up every time it passes is
        // not information, it is a flinch.
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

        let body = TipView(frame: NSRect(origin: .zero, size: size))
        label.frame = NSRect(x: 10, y: 6, width: size.width - 20, height: label.frame.height)
        body.addSubview(label)
        p.contentView = body
        // Under the shape and centred on it, unless that would run off the side of the screen.
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
    /// Nothing can be drawn *in* the notch — there is a camera behind it and those pixels do not
    /// exist — which is the fact the first version of this ignored, hanging everything below the
    /// hole instead. Everything happens in the two strips beside it: the left ear is the
    /// character, the right ear is the words, and the hole in the middle is left alone.
    private func ears(for mode: IslandMode, bar: CGFloat) -> (left: CGFloat, right: CGFloat) {
        switch mode {
        case .hidden:
            return (0, 0)
        case .resting:
            // The character and nothing else: no count, no name, no second ear. Deliberately the
            // same measurements as one session working, so waking up moves the routine and not
            // the shape — the thing you notice is the animal getting up, not the menu bar
            // rearranging itself around it.
            return (bar + IslandView.inset * 2, 0)
        case .working(let count, _):
            // The character, and a number only when there is more than one thing to count. This
            // is the state the machine is in most of the day, so it has to be able to sit in the
            // menu bar without becoming one of the things you read there.
            return (bar + IslandView.inset * 2, count > 1 ? 30 : 0)
        case .waiting(let who), .finished(let who):
            // Measured, so a short task name does not get a long ear of empty black after it.
            let text = (who as NSString).size(withAttributes: [
                .font: NSFont.systemFont(ofSize: 12, weight: .medium),
            ]).width
            return (bar + IslandView.inset * 2, min(250, max(100, text.rounded() + 44)))
        }
    }

    private func close() {
        guard let panel else { return }
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
        // The character is drawn in its own colours on black; a light system appearance was
        // tinting the text drawn beside it.
        p.appearance = NSAppearance(named: .darkAqua)
        p.isFloatingPanel = true
        // Tool tips need the window to be told about the pointer moving over it; without this
        // the tip never appears, and nothing says why.
        p.acceptsMouseMovedEvents = true
        // **After `isFloatingPanel`, and that is not a style preference.** Setting that property
        // assigns `.floating` — level 3 — to the window, so a level set before it is thrown away
        // without a word. The first version of this set them the other way round and spent its
        // life at 3: fine while the shape hung below the notch over an ordinary window at level
        // 0, and completely invisible the moment it moved up into the menu bar, which is drawn at
        // 24. The log said `visible=true occluded=false` the whole time, because it was: behind
        // the menu bar is still on screen.
        p.level = .mainMenu + 3
        p.hidesOnDeactivate = false
        p.becomesKeyOnlyIfNeeded = true
        // Follows you between desktops and does not join the window cycle: this is furniture, not
        // a window somebody is meant to manage.
        p.collectionBehavior = [.canJoinAllSpaces, .stationary, .ignoresCycle, .fullScreenAuxiliary]

        let v = IslandView(frame: .zero)
        v.onHover = { [weak self] inside in self?.hover(inside) }
        v.onClickMascot = { [weak self] in PromptController.shared.show(focusing: self?.subject?.id) }
        v.onClickEar = { [weak self] in self?.reveal() }
        p.contentView = v
        view = v
        panel = p
        return p
    }

    // MARK: - Pictures
    //
    // Every picture of the island on the README pages comes out of here: the app drawing itself
    // offscreen, so no Screen Recording permission is involved and nothing that is actually on
    // the screen can find its way into a file that is about to be pushed. The same reason the
    // card has this — without it, a shot of the top of the screen comes back as the wallpaper
    // with every window stripped out, which is no way to tune something fourteen points tall.

    /// How much menu bar is kept either side of the hole.
    ///
    /// **The frame is a menu bar, and that is the whole point of it.** These pictures used to be
    /// the shape on a flat grey field with a black rectangle over the middle for the cutout, and
    /// grey reads as *nothing* — not a bar, not a desktop, not a wall — so the island appeared to
    /// float in the middle of an explanation of itself. What the design says is "menu bar space,
    /// and the hole in the middle left strictly alone", which is not a claim a picture containing
    /// neither of those things can make.
    ///
    /// Not symmetric, because the two sides are not. The left ear is one character wide and never
    /// grows, so the room over there only has to hold the first menus of whatever is in front.
    /// The right ear is a task name and reaches 250 points, and past *that* there still has to be
    /// space for the things that live at that end of a bar — otherwise this is a strip with a
    /// hole in it and nothing anywhere saying it is a *menu* bar.
    private static let barLeft: CGFloat = 220
    private static let barRight: CGFloat = 370

    /// The bar a picture is drawn into: how tall it is, how wide the hole is, and whether this
    /// machine has a hole at all. Measured from the screen rather than from a table of models,
    /// the same way the live island measures it.
    private struct Stage {
        let bar: CGFloat
        let core: CGFloat
        let hasNotch: Bool
        var size: NSSize {
            NSSize(width: NotchIsland.barLeft + core + NotchIsland.barRight, height: bar)
        }
    }

    private func stage() -> Stage {
        let screen = Notch.screen()
        let notch = screen.flatMap { Notch.rect(of: $0) }
        let bar = (screen != nil && notch != nil)
            ? max(notch!.height, screen!.frame.maxY - screen!.visibleFrame.maxY)
            : NSStatusBar.system.thickness
        return Stage(bar: bar, core: (notch?.width ?? 150) + Notch.overshoot, hasNotch: notch != nil)
    }

    /// The states the pictures are made of, and the invented sessions they name.
    ///
    /// Read out of the card's own stand-in list rather than written down a second time here, so
    /// two pictures on one page cannot end up describing two different pieces of work.
    private static func demo(_ named: String) -> (mode: IslandMode, hue: Int) {
        let rows = PromptController.standInSessions()
        switch named {
        case "waiting":  return (.waiting(rows[0].label), rows[0].hue)
        case "finished": return (.finished(rows[1].label), rows[1].hue)
        case "working2": return (.working(count: 3, line: nil), rows[0].hue)
        case "resting":  return (.resting, rows[0].hue)
        default:         return (.working(count: 1, line: nil), rows[0].hue)
        }
    }

    /// The bar itself, and just enough furniture on it to say what it is.
    ///
    /// Invented, like every other word in these scenes: an application's first menus at one end,
    /// and at the other the things that live there — a couple of status items and a clock. None
    /// of it is read from this machine, and the time is the one Apple has been putting on its own
    /// pictures for forty years.
    private func drawMenuBar(_ s: Stage) {
        NSGradient(colors: [NSColor(white: 0.175, alpha: 1), NSColor(white: 0.125, alpha: 1)])?
            .draw(in: NSRect(origin: .zero, size: s.size), angle: -90)

        func run(_ str: String, size: CGFloat, weight: NSFont.Weight, alpha: CGFloat) -> NSAttributedString {
            NSAttributedString(string: str, attributes: [
                .font: NSFont.systemFont(ofSize: size, weight: weight),
                .foregroundColor: NSColor.white.withAlphaComponent(alpha),
            ])
        }
        func place(_ a: NSAttributedString, at x: CGFloat) {
            a.draw(at: NSPoint(x: x, y: ((s.bar - a.size().height) / 2).rounded()))
        }
        func symbol(_ name: String, _ points: CGFloat) -> NSImage? {
            let cfg = NSImage.SymbolConfiguration(pointSize: points, weight: .regular)
                .applying(NSImage.SymbolConfiguration(
                    paletteColors: [NSColor.white.withAlphaComponent(0.9)]))
            return NSImage(systemSymbolName: name, accessibilityDescription: nil)?
                .withSymbolConfiguration(cfg)
        }
        func place(_ image: NSImage, at x: CGFloat) {
            image.draw(in: NSRect(x: x, y: ((s.bar - image.size.height) / 2).rounded(),
                                  width: image.size.width, height: image.size.height))
        }

        // Left: the menus, stopping before the shape rather than running under it. On a real
        // screen the island does cover menu-bar space and what is beneath it is simply not
        // visible — but a menu title sliced down the middle in a *picture* reads as a drawing
        // error rather than as that, so the last one that would not fit is left out instead.
        let ceiling = Self.barLeft - Notch.flare - (s.bar + IslandView.inset * 2)
        var x: CGFloat = 16
        if let apple = symbol("apple.logo", 14) {
            place(apple, at: x)
            x += apple.size.width + 15
        }
        for (i, title) in ["Terminal", "Shell", "Edit", "View"].enumerated() {
            let a = run(title, size: 13, weight: i == 0 ? .semibold : .regular,
                        alpha: i == 0 ? 0.95 : 0.8)
            guard x + a.size().width < ceiling else { break }
            place(a, at: x)
            x += a.size().width + 17
        }

        // Right: filled from the edge inwards, the way that end of a menu bar fills up.
        let clock = run("Wed 9:41", size: 13, weight: .regular, alpha: 0.9)
        var r = s.size.width - 16 - clock.size().width
        place(clock, at: r)
        for name in ["switch.2", "wifi", "battery.75"] {
            guard let image = symbol(name, 14) else { continue }
            r -= image.size.width + 14
            place(image, at: r)
        }
    }

    /// Paint one frame: the bar, the hole, and the shape over both.
    private func paint(_ view: IslandView, on s: Stage) {
        drawMenuBar(s)
        // The hole, punched over the bar rather than left as a gap in it. There is a camera
        // behind those pixels and they are the one part of this design that is never drawn on,
        // so the picture has to have them too — a shot without it is a black bar with a gap of
        // black in the middle, which is not what anybody sees.
        if s.hasNotch {
            NSColor.black.setFill()
            NSRect(x: Self.barLeft, y: 0, width: s.core, height: s.bar).fill()
        }
        guard view.bounds.width > 1,
              let rep = view.bitmapImageRepForCachingDisplay(in: view.bounds) else { return }
        view.cacheDisplay(in: view.bounds, to: rep)
        // Pinned to the hole, not to the frame: the shape grows around a cutout that does not
        // move, which is what makes the growing read as growing rather than as sliding.
        let box = NSRect(x: Self.barLeft - Notch.flare - view.leftEar, y: 0,
                         width: view.bounds.width, height: view.bounds.height)
        // **Clipped to the shape, because the shape does not fill its own bounds.** It has a
        // concave flare at each shoulder, and the buffer `cacheDisplay` hands back is opaque
        // white wherever the view drew nothing — so every picture ever taken of the island had a
        // white hairline curling down both of its sides. It never showed up on screen, where the
        // panel is transparent and only what is drawn exists; it only ever existed in the
        // pictures. Same path the view fills itself with, so the two cannot disagree.
        NSGraphicsContext.current?.saveGraphicsState()
        Notch.path(in: box, pill: !s.hasNotch).addClip()
        rep.draw(in: box)
        NSGraphicsContext.current?.restoreGraphicsState()
    }

    /// Put the view into one state, at one instant, at one width.
    private func pose(_ v: IslandView, mode: IslandMode, hue: Int,
                      left: CGFloat, right: CGFloat, on s: Stage, at t: Double) {
        v.setFrameSize(NSSize(width: Notch.flare * 2 + left + s.core + right, height: s.bar))
        v.leftEar = left
        // A made-up project's mark, so the picture carries the same thing the real ear does.
        v.projectGrid = ProjectIcon.demoGrid(hue: hue)
        v.mode = mode
        v.poseMascot(for: mode, at: t)
        // **Asked for by hand, because `leftEar` is a plain stored property.** Changing it does
        // not mark the view as needing layout, and `layout()` is where the character is scaled to
        // the band — so the frame in which the ear first opens was drawn with the character still
        // at the size its pack was drawn for, twice the height of the menu bar and cut off by the
        // top of the picture. It corrected itself one frame later, which is exactly the kind of
        // thing that survives being looked at once.
        v.needsLayout = true
        v.layoutSubtreeIfNeeded()
    }

    /// Draw one state of the island into a PNG, without it having to be on screen. For tuning a
    /// single state; the picture that ships is the clip below.
    /// `at` is seconds into the routine, for the states where one instant is not enough: resting
    /// breathes over five seconds and the interesting question about it is whether the whole
    /// swing is small, which is two or three frames of the same routine rather than one.
    func snapshot(to path: String, mode named: String, at t: Double? = nil) {
        let s = stage()
        let (mode, hue) = Self.demo(named)
        let want = ears(for: mode, bar: s.bar)
        let view = IslandView(frame: NSRect(origin: .zero, size: NSSize(width: s.core, height: s.bar)))
        view.notchWidth = s.core
        // A frame from the middle of the routine rather than its first instant, which for a jump
        // is the character standing perfectly still on the ground.
        pose(view, mode: mode, hue: hue, left: want.left, right: want.right, on: s, at: t ?? 0.45)
        FilmFrame.write(size: s.size, to: path) { self.paint(view, on: s) }
        view.releaseMascot()
        Log.write("island snapshot → \(path) (\(named) at \(t ?? 0.45)s)")
    }

    /// The island demonstrating itself, one frame at a time.
    ///
    /// **Three stills concatenated could not make this section's argument.** What the page claims
    /// is that the mascot leans out while something runs, names the session that wants you, and
    /// *dances* when a long job finishes — three verbs, none of which a still can do. The old
    /// picture held each state for two seconds and cut to the next, so the one thing it could not
    /// show was the only thing it was captioned with.
    ///
    /// The order is the sentence: something is running, then more than one thing is and the
    /// character is visibly working harder, then one of them wants you and is named in the
    /// accent colour, then one finishes and it dances. The dance is last because it is the
    /// ending. Beats are long enough to read rather than long enough to fit.
    ///
    /// The shape grows sideways into each beat instead of cutting, because growing sideways out
    /// of the hole *is* the animation — the thing the section is named after — and a cut is the
    /// one edit that hides it.
    func filmstrip(dir: String, fps: Double, seconds: Double) {
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        let s = stage()
        let rows = PromptController.standInSessions()
        let beats: [(mode: IslandMode, hue: Int, length: Double)] = [
            (.working(count: 1, line: nil), rows[0].hue, 1.7),
            (.working(count: 3, line: nil), rows[0].hue, 1.9),
            (.waiting(rows[0].label), rows[0].hue, 2.4),
            (.finished(rows[1].label), rows[1].hue, 2.6),
        ]
        // Stated as durations that read well and then stretched to whatever the caller asked
        // for, so the shape of the storyboard survives somebody changing its length.
        let span = beats.reduce(0) { $0 + $1.length }
        let view = IslandView(frame: NSRect(origin: .zero, size: NSSize(width: s.core, height: s.bar)))
        view.notchWidth = s.core

        let total = Int(seconds * fps)
        Log.write("island filmstrip: frames=\(total) stage=\(s.size) dir=\(dir)")
        for i in 0..<total {
            let t = Double(i) / fps * (span / max(0.1, seconds))
            var start = 0.0, index = 0
            for (k, beat) in beats.enumerated() {
                index = k
                if t < start + beat.length { break }
                if k < beats.count - 1 { start += beat.length }
            }
            let beat = beats[index]
            let local = t - start

            // Opens from nothing, so the first thing the clip does is the thing it is about: the
            // character leaning out of a menu bar that a moment ago was only a menu bar.
            let want = ears(for: beat.mode, bar: s.bar)
            let from = index == 0 ? (left: CGFloat(0), right: CGFloat(0))
                                  : ears(for: beats[index - 1].mode, bar: s.bar)
            let p = CGFloat(min(1, local / 0.28))
            let e = 1 - pow(1 - p, 3)
            pose(view, mode: beat.mode, hue: beat.hue,
                 left: from.left + (want.left - from.left) * e,
                 right: from.right + (want.right - from.right) * e,
                 on: s, at: local)

            FilmFrame.write(size: s.size,
                            to: URL(fileURLWithPath: dir)
                                .appendingPathComponent(String(format: "f%04d.png", i)).path) {
                self.paint(view, on: s)
            }
        }
        view.releaseMascot()
        Log.write("island filmstrip → \(dir) (\(total) frames @ \(Int(fps))fps)")
    }

    /// The right ear goes to the session it names.
    ///
    /// Straight to the terminal tab rather than to the bar: that ear says "*that* one", and what
    /// you were about to do is look at it. Revealing also makes it iTerm2's current session, so
    /// ⌥Space immediately afterwards opens the bar already aimed there — the two paths agree
    /// without either knowing about the other.
    private func reveal() {
        // More than one thing behind that number, so the number cannot answer "which one" and
        // must not pick for you. The count is the only part of this that is ever plural, and a
        // silent guess there is the same bug as the one that used to send clicks to whichever
        // session happened to be running.
        let choices = candidates()
        if choices.count > 1 {
            offer(choices)
            return
        }
        guard let wanted = choices.first ?? subject else {
            PromptController.shared.show()
            return
        }
        Log.write("island: revealing \(wanted.label)")
        Targets.reveal(wanted)
    }

    /// Every session the current shape is speaking for, not just the one it had room to name.
    private func candidates() -> [TargetSession] {
        let watch = SessionWatch.shared
        switch mode {
        case .waiting:  return watch.waiting
        case .working:  return watch.working
        case .finished: return subject.map { [$0] } ?? []
        case .hidden, .resting: return []
        }
    }

    /// A menu under the ear, one row per session.
    ///
    /// Built fresh every time rather than kept: it is a list of what is running *now*, and a menu
    /// held over from the last press would offer sessions that have since finished.
    private func offer(_ sessions: [TargetSession]) {
        guard let view, let panel else { return }
        let menu = NSMenu()
        menu.font = NSFont.systemFont(ofSize: 13)
        for session in sessions {
            let item = NSMenuItem(title: session.label, action: #selector(jump(_:)),
                                  keyEquivalent: "")
            item.target = self
            item.representedObject = session.id
            // The project's own mark, the same one the bar's footer and the terminal's status
            // line draw. A tab title is the *task*, and two projects can be working on tasks that
            // read alike — the icon is the part that tells them apart without being read.
            item.image = SessionWatch.shared.grid(of: session.id)?.image(height: 12)
            // What it is doing, after the name, dimmed — the same line the bar's own list shows,
            // so picking from here and picking from there are the same decision.
            if let line = SessionWatch.shared.liveLine(of: session.id) {
                let title = NSMutableAttributedString(string: session.label + "  ", attributes: [
                    .font: NSFont.systemFont(ofSize: 13),
                ])
                title.append(NSAttributedString(string: String(line.prefix(38)), attributes: [
                    .font: NSFont.systemFont(ofSize: 11),
                    .foregroundColor: NSColor.secondaryLabelColor,
                ]))
                item.attributedTitle = title
            }
            menu.addItem(item)
        }
        // The way out of "what is running" and into "everything".
        //
        // A row here rather than a second click on the ear: the first click puts a menu up, and a
        // menu runs its own event loop — the click that would have been the second one is spent
        // dismissing it, and never reaches the view at all. Disambiguating by waiting out the
        // double-click interval before opening the menu would work and would put a quarter of a
        // second of nothing in front of the common action, which is the wrong trade.
        menu.addItem(.separator())
        let all = NSMenuItem(title: L.t.islandAllSessions, action: #selector(showAll),
                             keyEquivalent: "k")
        all.keyEquivalentModifierMask = .command
        all.target = self
        menu.addItem(all)

        // Under the ear that was pressed, in the view's own coordinates.
        let ear = view.rightEarRect
        menu.popUp(positioning: nil, at: NSPoint(x: ear.minX, y: ear.minY - 4), in: view)
        _ = panel
    }

    @objc private func showAll() {
        PromptController.shared.showSessionList()
    }

    @objc private func jump(_ sender: NSMenuItem) {
        guard let id = sender.representedObject as? String,
              let session = SessionWatch.shared.targets.first(where: { $0.id == id }) else { return }
        Log.write("island: revealing \(session.label) (picked from \(sender.menu?.numberOfItems ?? 0))")
        Targets.reveal(session)
    }
}

/// A window that is allowed to be where windows are not allowed to be.
///
/// AppKit runs every frame you set through `constrainFrameRect(_:to:)`, which keeps windows from
/// covering the menu bar — sensible for every window ever written except this one. Asking for a
/// frame flush with the top of the screen quietly got one pushed down to just under the menu bar
/// instead, so the whole shape hung *below* the notch rather than growing out of it: a black tab
/// floating under a black notch, with a seam across the middle, which is precisely the illusion
/// this depends on not having. The proposal is returned untouched.
final class IslandPanel: NSPanel {
    override func constrainFrameRect(_ frameRect: NSRect, to screen: NSScreen?) -> NSRect {
        frameRect
    }
    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
}

/// The little dark slab the tip is written on. Rounded, and the same near-black as the shape it
/// hangs off, so the two read as one object rather than as a window that has appeared.
final class TipView: NSView {
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

/// The shape and its contents. One view, because there are only ever a few things on it and a
/// hierarchy of subviews for three labels is a worse thing to read than the drawing code.
final class IslandView: NSView {

    var mode: IslandMode = .hidden { didSet { needsDisplay = true } }
    /// The width of the hole in the middle, and how much of this view sits to the left of it.
    /// Everything is laid out around these: the camera is behind that gap and anything drawn
    /// there is drawn on the back of it.
    var notchWidth: CGFloat = 0
    var leftEar: CGFloat = 0
    var onClickMascot: (() -> Void)?
    var onClickEar: (() -> Void)?
    /// The mark of the project the named session belongs to.
    var projectGrid: ProjectIcon.Grid? { didSet { needsDisplay = true } }

    private let mascot = MascotView(frame: .zero)
    /// What was asked for last time a routine was chosen. Only `resting` is ever read out of it:
    /// leaving that state is a beat, and every other change is a cut.
    private var lastPlayed: IslandMode = .hidden

    /// Put the character where a live one would be `t` seconds into this mode.
    ///
    /// **The handover is the part that has to be done by hand.** A live view has a timer, and the
    /// tick that notices a one-shot routine has run out is what hands it over to `idle`. A frozen
    /// clock never reaches that tick, so a beat longer than the routine it opens with ends as a
    /// character standing perfectly still — in a clip whose entire subject is that it moves. The
    /// pace is folded in for the same reason: `working` says how busy it is by running the same
    /// routine faster, and a frozen frame that ignored the rate would be the wrong frame.
    func poseMascot(for mode: IslandMode, at t: Double) {
        playRoutine(for: mode)
        let local = t * mascot.rate
        if let r = mascot.pack?.routines[mascot.routine], !(r.loop ?? false), local >= r.duration {
            mascot.play("idle", then: "idle")
            mascot.frozenTime = local - r.duration
        } else {
            mascot.frozenTime = local
        }
    }

    /// Let the clock go again, and let go of the timer a posed routine started. A view built for
    /// one picture is thrown away straight afterwards, and its timer is on the main run loop.
    func releaseMascot() {
        mascot.frozenTime = nil
        mascot.stop()
    }

    /// Read the pack again — the one on the card is not the only copy of it.
    func reloadMascot() {
        mascot.reload()
        needsLayout = true
    }
    /// For the log: where the character actually ended up, which is not always where the layout
    /// says it should be.
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
        // Getting up is not the same as arriving, and this is the only place that can tell the
        // two apart: the shape does not move between resting and one session working, so without
        // this the character would go from asleep to at-work between two frames.
        let waking = lastPlayed == .resting && mode != .resting
        lastPlayed = mode
        // Eyes shut for the whole of resting, and only for the fallback. A pack with its own
        // `sleep` routine said so in its keys and is left alone — an author who drew a squint for
        // sleeping should get their squint.
        mascot.eyesOverride = (mode == .resting && !mascot.has("sleep")) ? "blink" : nil
        switch mode {
        case .resting:
            // Nothing is running, and the character is asleep rather than gone.
            //
            // This is on screen all day, so the budget for how much it may do is not the budget
            // the other states get: they last seconds and are allowed to be seen. A pack that
            // has `sleep` states its own pace and is obeyed. A pack that does not — every pack
            // written before this existed — gets its own `idle` at a little under half speed,
            // which lands on roughly the same five-second breath, with the eyes held shut above.
            let resting = mascot.has("sleep") ? "sleep" : "idle"
            if mascot.routine != resting { mascot.play(resting, then: resting) }
            mascot.rate = resting == "sleep" ? 1 : 0.45
        case .finished:
            mascot.rate = 1
            mascot.play(mascot.has("dance") ? "dance" : "cheer", then: "idle")
        case .waiting:
            // Waiting is not busy. Whatever it was doing, it has stopped and is looking at you.
            mascot.rate = 1
            mascot.play(mascot.has("pop") ? "pop" : "idle", then: "idle")
        case .working(let count, _):
            // How hard it is working is how much *you* have running.
            //
            // Two ways at once, because either alone is too subtle: past a couple of sessions it
            // switches to the busiest looping routine the pack has, and underneath that the clock
            // itself speeds up. One session is the character's own pace; six is visibly frantic.
            // Capped, because past a point faster stops reading as busy and starts reading as
            // broken.
            let busy = count >= 3 && mascot.has("typing") ? "typing" : "idle"
            // Woken up, so it gets up: one pass of `stretch` and then straight into the work.
            // Reusing the routine every pack already has rather than writing a transition —
            // it is a yawn with its eyes opening on the first key, which is exactly the beat
            // that was missing, and nothing has to be maintained for it.
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
    /// Without it an ear is exactly as wide as what is in it, so the character sits *on* the
    /// rounded corner and the count sits on the other one — everything shoved against the two
    /// outermost points of the shape, which is where nothing should be. The shape needs to look
    /// like it has room in it.
    static let inset: CGFloat = 10

    override func layout() {
        super.layout()
        guard bounds.height > 6 else { return }
        // Sized to the band rather than to a number picked by hand, so a pack drawn twice as tall
        // as the shipped one still stands in the menu bar rather than through it.
        //
        // Done before the ear is checked, not after. A clip of the shape opening draws this view
        // while the ear is still shut, and the scale that had never been set is the one the pack
        // was drawn for — so the first frame carried a character twice the height of the menu bar
        // with the top of it cut off by the top of the picture.
        mascot.fit(height: bounds.height - 12)
        // Nowhere, rather than wherever it was last. With no ear there is no strip to stand in,
        // and a frame left over from the last time there was one puts the character on the
        // camera housing.
        guard leftEar > 0 else { mascot.frame = .zero; return }
        // Against the hole rather than against the outside edge: the character should look like
        // it is standing next to the notch, which is the thing it is leaning on.
        let side = bounds.height
        mascot.frame = NSRect(x: left.maxX - side - Self.inset, y: 1,
                              width: side, height: bounds.height - 2)
    }

    /// The two ears are two different buttons, because they are two different sentences.
    ///
    /// The character is the app: pressing it should give you the app. The words are a particular
    /// session: pressing them should give you that session. One handler for both had to guess,
    /// and guessed "the session" — so clicking the little animal took you to a terminal tab,
    /// which is the one place the animal exists to keep you out of.
    ///
    /// The middle does nothing on purpose. There is a camera behind it and nothing drawn on it,
    /// and a click target you cannot see is a click target that will be pressed by accident.
    /// Told when the pointer is over an ear, so somebody else can put a tip up.
    var onHover: ((Bool) -> Void)?

    /// **`.activeAlways`, and that is the whole reason this exists.**
    ///
    /// `NSView.toolTip` was set here first and never once appeared: macOS shows tool tips for the
    /// *active* application, and this app is by construction never the active one — you are in a
    /// terminal, which is the point. A tracking area asked for `.activeAlways` gets entered and
    /// exited events regardless of who is in front, so the tip can be drawn by hand.
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
    /// **`resetCursorRects` is not usable here for the same reason `toolTip` was not**: cursor
    /// rectangles belong to the active application, and this one is never active — you are in a
    /// terminal. So the cursor is set by hand from the tracking area, which was asked for
    /// `.activeAlways` and therefore keeps reporting whoever is in front.
    ///
    /// Set on every move rather than once on entering: the window server re-asserts its own
    /// cursor as the pointer crosses windows, and a single `set()` on the way in is undone by the
    /// next thing that happens.
    private func pointer(at event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        let overEar = left.contains(p) || right.contains(p)
        // The middle is a camera. Nothing is drawn there and nothing happens when it is clicked,
        // so it must not offer a hand — a pointer over a hole is a promise of something that
        // cannot be there.
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
        // The body. Black, because the cutout it grows out of is black and the join between the
        // two has to be invisible.
        NSColor.black.setFill()
        Notch.path(in: bounds, pill: notchWidth <= 0).fill()

        let r = right
        switch mode {
        case .hidden, .resting:
            // Asleep is the character and the black shape and nothing else. No dot, no name, no
            // count — there is nothing to count, and a mark on an empty ear is a thing to read.
            return
        case .working(let count, _):
            guard count > 1 else { return }
            draw("\(count)", in: NSRect(x: r.minX + (r.width - 20) / 2, y: r.midY - 7,
                                        width: 20, height: 14),
                 colour: Style.accent, size: 11, weight: .semibold, align: .center)
        case .waiting(let who):
            ear(who, dot: Style.accent, in: r)
        case .finished(let who):
            ear(who, dot: NSColor.systemGreen, in: r)
        }
    }

    /// The right ear: a dot in the colour that says what happened, then which session it happened
    /// to. One line, because the ear is as tall as the menu bar and a second line there is a
    /// second line nobody can read.
    private func ear(_ name: String, dot: NSColor, in r: NSRect) {
        let d: CGFloat = 5
        var x = r.minX + Self.inset
        // The dot is the *state* — accent for a question, green for something that has finished.
        dot.setFill()
        NSBezierPath(ovalIn: NSRect(x: x, y: r.midY - d / 2, width: d, height: d)).fill()
        x += d + 7

        // The mark is *which project*, and it answers a different question: a task name is a
        // task name, and two of them can read alike. Same registry as the ⌘K rows and the
        // footer, so the three cannot be looking at different pictures of the same project.
        if let grid = projectGrid, let icon = grid.image(height: 11) {
            icon.draw(in: NSRect(x: x, y: (r.midY - icon.size.height / 2).rounded(),
                                 width: icon.size.width, height: icon.size.height))
            x += icon.size.width + 7
        }

        draw(name, in: NSRect(x: x, y: r.midY - 8, width: max(0, r.maxX - Self.inset - x),
                              height: 16),
             colour: NSColor.white.withAlphaComponent(0.92), size: 12, weight: .medium)
    }

    private func draw(_ text: String, in rect: NSRect, colour: NSColor, size: CGFloat,
                      weight: NSFont.Weight, tracking: CGFloat = 0,
                      align: NSTextAlignment = .left) {
        let style = NSMutableParagraphStyle()
        style.alignment = align
        style.lineBreakMode = .byTruncatingTail
        var attrs: [NSAttributedString.Key: Any] = [
            .font: NSFont.systemFont(ofSize: size, weight: weight),
            .foregroundColor: colour,
            .paragraphStyle: style,
        ]
        if tracking > 0 { attrs[.kern] = tracking }
        NSAttributedString(string: text, attributes: attrs).draw(in: rect)
    }
}
