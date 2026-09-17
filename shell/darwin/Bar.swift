// The input bar's window.
//
// The Swift app draws this whole panel in AppKit: `Sources/Controller.swift`
// (4,134 lines) and `Sources/Panel.swift` (1,292) between them own the card,
// the text view, the rows, the hint row, the mascot and the output pane. Here
// the card is a page — `web/console/src/bar/` — and this file is only what a
// page cannot be:
//
//   * a borderless window above everything, on every Space, that does not
//     activate the application it floats over;
//   * the frosted glass behind it, which is a blur of the desktop and therefore
//     of something the page cannot see;
//   * a key pressed inside another application (`HotKey.swift`);
//   * coming back when the terminal comes back, and going away when the
//     keyboard goes somewhere else;
//   * the editing keys, because a WKWebView only cuts, copies and pastes when
//     something sends it the action and this window has no Edit menu.
//
// Everything else is the page's, including every word on screen, which is why
// nothing here reads `Copy.swift` and nothing here talks to the daemon. The
// contract between the two halves is `docs/shell-bridge.md`; what the other
// platforms cannot do is `docs/cross-platform.md`.
import AppKit
import WebKit

/// Where the card is, how wide, and how it is laid out — the Swift app's
/// `Config` and `Panel.swift` `Style`, for the parts the window owns.
///
/// The page owns the height (it owns the layout, so only it knows when the card
/// grew) and tells this window what it is; see `BarMessage.height`.
enum BarGeometry {
    /// `Config.shared.width`, whose default is 720.
    static let width: CGFloat = 720
    /// `Config.shared.yFraction`: where the card's top sits, as a fraction of
    /// the screen's height, measured from the top.
    static let yFraction: CGFloat = 0.30
    /// `Style.corner`.
    static let corner: CGFloat = 22
    /// What the window is before the page has said anything: one input row and
    /// one hint row (`Style.inputMinHeight + 1 + Style.hintHeight`). A window
    /// that opened at zero would flash.
    static let startingHeight: CGFloat = 62 + 1 + 38
    /// `windowDidResignKey`: "In the instant after it opens, the previous app
    /// may still be grabbing focus back. Without this grace period the panel
    /// closes before you ever see it."
    static let focusGrace: TimeInterval = 0.4
    /// `Controller.returnWindow`: how long a switch away still counts as
    /// "I needed to see something for a moment".
    static let returnWindow: TimeInterval = 90
}

/// `PromptPanel`: "A borderless window cannot become key by default. Without
/// this the caret never blinks and no keys arrive."
///
/// The editing keys are `ConsoleWindow`'s arrangement, for its reason: the page
/// is given the key first, and only what it leaves unhandled comes back here,
/// which is where an Edit menu would have caught it. This window has no menu of
/// its own at all, so without this ⌘V in the bar would do nothing.
final class BarPanel: NSPanel {
    var onReload: (() -> Void)?

    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { true }

    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        if super.performKeyEquivalent(with: event) { return true }
        let flags = event.modifierFlags.intersection([.command, .shift, .option, .control])
        let key = event.charactersIgnoringModifiers?.lowercased() ?? ""
        let shifted: NSEvent.ModifierFlags = [.command, .shift]
        var action: Selector?
        if flags == .command {
            switch key {
            case "x": action = #selector(NSText.cut(_:))
            case "c": action = #selector(NSText.copy(_:))
            case "v": action = #selector(NSText.paste(_:))
            case "a": action = #selector(NSText.selectAll(_:))
            case "z": action = Selector(("undo:"))
            case "r":
                onReload?()
                return true
            default: break
            }
        } else if flags == shifted, key == "z" {
            action = Selector(("redo:"))
        }
        guard let action else { return false }
        return NSApp.sendAction(action, to: nil, from: self)
    }
}

/// What the page asks this window for (`web/console/src/bar/shell.ts`).
private enum BarMessage {
    case height(CGFloat)
    case hide
    case ready

    init?(_ body: Any) {
        guard let object = body as? [String: Any], let kind = object["kind"] as? String else { return nil }
        switch kind {
        case "height":
            guard let value = (object["height"] as? NSNumber)?.doubleValue, value > 0 else { return nil }
            self = .height(CGFloat(value))
        case "hide":
            self = .hide
        case "ready":
            self = .ready
        default:
            return nil
        }
    }
}

/// The bar: one window, one web view, and the rules about when it is up.
final class BarController: NSObject, NSWindowDelegate, WKNavigationDelegate, WKScriptMessageHandler {
    private let home: URL
    private let page: URL
    private var panel: BarPanel!
    private var web: WKWebView!
    private var glass: NSVisualEffectView!

    /// The app that was in front when the bar was summoned, so a dismissal can
    /// hand the front back — `Controller.previousApp`.
    private var previousApp: NSRunningApplication?
    /// When the bar was last put up, for the grace period below.
    private var shownAt = Date.distantPast
    /// When a switch to another application put the bar away, so that coming
    /// back to the terminal can take it out again — `Controller.hiddenByAppSwitch`.
    private var hiddenByAppSwitch: Date?
    /// Whether the page has drawn once. A summon before that shows the window
    /// anyway: an empty card for a moment beats a key that does nothing.
    private var pageReady = false
    /// Which load a retry belongs to, as the console window counts them.
    private var loadGeneration = 0
    private var failedLoads = 0

    /// Whether the window is on screen, for the hotkey's scope.
    var isVisible: Bool { panel?.isVisible == true }

    /// Which applications count as "the terminal" for coming back, and whether
    /// coming back is wanted at all — the Swift app's `scope_app` and
    /// `reopen_on_return`. Read from the shell's config through `NextConfig`,
    /// which is the only file that reads it.
    private var terminalScope: [String] {
        NextConfig.shared.scopeApp
            .split(separator: ",")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    init(home: URL) {
        self.home = home
        self.page = home.appendingPathComponent("bar.html")
        super.init()
        build()
    }

    // MARK: - Building it

    private func build() {
        let config = WKWebViewConfiguration()
        let content = WKUserContentController()
        // One handler and nothing else. The page is told nothing at document
        // start — it has no words to be handed and no data to be given — so
        // whether it is inside a shell is exactly whether this handler is
        // there (`web/console/src/bar/shell.ts`).
        content.add(WeakMessageHandler(self), name: "shellBar")
        config.userContentController = content

        web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = self
        // The page is drawn over the frosted glass below, so the web view must
        // not paint its own white page behind it. `drawsBackground` is not in
        // the public interface; there is no supported alternative, and this is
        // the same key every transparent WKWebView on this platform uses.
        web.setValue(false, forKey: "drawsBackground")

        panel = BarPanel(contentRect: NSRect(x: 0, y: 0, width: BarGeometry.width,
                                             height: BarGeometry.startingHeight),
                         styleMask: [.borderless, .nonactivatingPanel],
                         backing: .buffered, defer: false)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.level = .floating
        panel.hidesOnDeactivate = false
        panel.isMovableByWindowBackground = false
        panel.animationBehavior = .none
        // "Follow the user across Spaces. Their windows are spread over several
        // desktops; a prompt bar pinned to one is half-useless." Not
        // `.stationary`: with it set, Show Desktop slides everything away and
        // leaves the panel sitting on the wallpaper.
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        panel.delegate = self
        panel.onReload = { [weak self] in self?.reload() }
        // It is a panel over somebody else's application; it must never appear
        // in the Window menu or be cycled to with ⌘`.
        panel.isExcludedFromWindowsMenu = true

        // `card`: an `NSVisualEffectView` with `Style.ink` at `cardOpacity` over
        // it. The blur is of the desktop behind the window, which is the one
        // part of this card a page cannot draw — so the view is here and the
        // scrim, the gloss and the border are in `bar.css`, over it.
        glass = NSVisualEffectView(frame: NSRect(x: 0, y: 0, width: BarGeometry.width,
                                                 height: BarGeometry.startingHeight))
        glass.material = .hudWindow
        glass.blendingMode = .behindWindow
        glass.state = .active
        glass.wantsLayer = true
        glass.layer?.cornerRadius = BarGeometry.corner
        glass.layer?.masksToBounds = true
        glass.autoresizingMask = [.width, .height]

        web.frame = glass.bounds
        web.autoresizingMask = [.width, .height]
        glass.addSubview(web)
        panel.contentView = glass
    }

    // MARK: - Loading

    func reload() {
        loadGeneration += 1
        pageReady = false
        let generation = loadGeneration
        // Every route but the console's own document needs a token, and
        // `bar.html` is not on the gate's open list — deliberately, since only
        // a shell that already holds this machine's token ever loads it.
        LocalToken.install(in: web.configuration.websiteDataStore.httpCookieStore, for: home) { [weak self] _ in
            guard let self, generation == self.loadGeneration else { return }
            self.web.load(URLRequest(url: self.page))
        }
    }

    func webView(_ webView: WKWebView, didFinish nav: WKNavigation!) {
        failedLoads = 0
        shellLog("bar: loaded \(webView.url?.absoluteString ?? "?")")
        sendState()
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation nav: WKNavigation!,
                 withError error: Error) {
        failedLoads += 1
        pageReady = false
        let generation = loadGeneration
        shellLog("bar: load failed (\(failedLoads)): \(error.localizedDescription)")
        guard failedLoads <= 30 else { return }
        let delay = min(Double(failedLoads) * 2, 10)
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            guard let self, generation == self.loadGeneration else { return }
            self.reload()
        }
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        shellLog("bar: web content process ended; reloading")
        reload()
    }

    // MARK: - Show and hide

    /// `Controller.toggle()`.
    func toggle() {
        if panel.isVisible && panel.isKeyWindow && NSApp.isActive { hide(returnFocus: true) } else { show() }
    }

    /// `Controller.show()`, minus everything that belongs to the card.
    ///
    /// The page is told it was summoned and does its own half — the list and
    /// the keys go back to what a fresh summon has, and the caret lands in the
    /// box. Nothing here touches what is in that box.
    func show() {
        let front = NSWorkspace.shared.frontmostApplication
        if front?.processIdentifier != NSRunningApplication.current.processIdentifier {
            previousApp = front
        }
        position()
        shownAt = Date()
        NSApp.activate(ignoringOtherApps: true)
        panel.makeKeyAndOrderFront(nil)
        panel.makeFirstResponder(web)
        dispatch("clawdline-bar-shown", [:])
        sendState()
        shellLog("bar: shown frame=\(panel.frame) drawn=\(pageReady)"
                 + " prev=\(previousApp?.localizedName ?? "-")")
    }

    /// `Controller.hide(returnFocus:)`.
    ///
    /// "`returnFocus` puts the app you came from back in front. That is right
    /// when you dismissed the panel… It is wrong when the panel is closing
    /// *because* you went somewhere else."
    func hide(returnFocus: Bool) {
        guard panel.isVisible else { return }
        if returnFocus { hiddenByAppSwitch = nil }
        panel.orderOut(nil)
        dispatch("clawdline-bar-hidden", [:])
        if returnFocus, let previous = previousApp,
           previous.processIdentifier != NSRunningApplication.current.processIdentifier,
           !previous.isTerminated {
            previous.activate()
        }
    }

    func windowDidResignKey(_ notification: Notification) {
        guard notification.object as? NSWindow === panel, panel.isVisible else { return }
        // The grace period, for the instant after it opens where the previous
        // application is still grabbing focus back.
        if Date().timeIntervalSince(shownAt) < BarGeometry.focusGrace {
            NSApp.activate(ignoringOtherApps: true)
            panel.makeKeyAndOrderFront(nil)
            panel.makeFirstResponder(web)
            return
        }
        // "Losing focus is the app-switch path; Esc and sending come through
        // hide() directly and must not arm the return, or dismissing it would
        // only postpone it."
        hiddenByAppSwitch = Date()
        hide(returnFocus: false)
    }

    /// `Controller.appBecameFrontmost`: "Leaving a panel you had open is 'I
    /// need to see something for a moment', not 'I am done' — Esc is how you
    /// say the second one."
    ///
    /// The Swift app's `reopen_on_return` is on by default and this shell's
    /// config does not carry the key yet, so it follows the one fact the config
    /// does carry: a scope means "these are my terminals", and an empty scope —
    /// the hotkey is live everywhere — names none, so nothing reopens.
    func appBecameFrontmost(_ bundleID: String?) {
        let scope = terminalScope
        guard !scope.isEmpty, let bundleID, scope.contains(bundleID) else { return }
        guard let since = hiddenByAppSwitch, !panel.isVisible else { return }
        hiddenByAppSwitch = nil
        let away = Date().timeIntervalSince(since)
        guard away < BarGeometry.returnWindow else {
            shellLog("bar: reopen declined, away \(Int(away))s")
            return
        }
        // "Not on this turn of the loop. The notification arrives while macOS
        // is still raising the terminal's windows."
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) { [weak self] in
            guard let self, !self.panel.isVisible else { return }
            let front = NSWorkspace.shared.frontmostApplication?.bundleIdentifier
            guard let front, self.terminalScope.contains(front) else { return }
            self.show()
        }
    }

    // MARK: - Where it sits, and how big

    /// `originFor(width:total:)`: centred on the screen under the pointer, with
    /// the card's top at `yFraction` of that screen's height.
    private func position() {
        let screen = screenUnderMouse()
        let f = screen.frame
        var frame = panel.frame
        frame.size.width = BarGeometry.width
        let top = f.maxY - f.height * BarGeometry.yFraction
        frame.origin = NSPoint(x: (f.midX - frame.width / 2).rounded(),
                               y: (top - frame.height).rounded())
        panel.setFrame(frame, display: false)
    }

    private func screenUnderMouse() -> NSScreen {
        let p = NSEvent.mouseLocation
        return NSScreen.screens.first { NSMouseInRect(p, $0.frame, false) }
            ?? NSScreen.main ?? NSScreen.screens[0]
    }

    /// The page measured its card; make the window that tall.
    ///
    /// The top edge stays where it is and the card grows downward, which is
    /// what `originFor` does: `yFraction` names the top of the card, so that
    /// opening the list never moves the line somebody is typing in.
    private func setHeight(_ height: CGFloat) {
        guard height > 0 else { return }
        let screen = panel.screen ?? screenUnderMouse()
        let capped = min(height, screen.visibleFrame.height)
        var frame = panel.frame
        guard abs(frame.height - capped) >= 0.5 else { return }
        let top = frame.maxY
        frame.size.height = capped
        frame.origin.y = (top - capped).rounded()
        panel.setFrame(frame, display: true)
    }

    // MARK: - The page's half

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        // Only the page this window loaded may ask; one it navigated to by a
        // link is somebody else's. Host and port as the URL's own pieces, never
        // as a prefix of a string.
        guard message.name == "shellBar",
              message.frameInfo.isMainFrame,
              message.frameInfo.request.url?.host == home.host,
              message.frameInfo.request.url?.port == home.port else { return }
        guard let request = BarMessage(message.body) else { return }
        switch request {
        case .height(let height):
            setHeight(height)
        case .hide:
            hide(returnFocus: true)
        case .ready:
            pageReady = true
            sendState()
        }
    }

    /// What this platform can tell the page about itself.
    ///
    /// `follow` is off, and it is off for a reason rather than for now: this
    /// daemon's `POST /v1/sessions/<id>/focus` selects the pane *and* raises
    /// the terminal application, and the Swift app's `follow(_:)` is explicitly
    /// the other thing — "**Without activating.** Bringing iTerm2 forward on
    /// every Tab press would hand it the keyboard, which is the one thing this
    /// whole application exists to avoid doing." Raising it here would take the
    /// keyboard out of the bar that asked for it, so the page's wire is in
    /// place and switched off until that route can be asked not to activate.
    /// See `docs/cross-platform.md`.
    private func sendState() {
        dispatch("clawdline-bar-state", [
            "follow": false,
            "hotkey": NextConfig.shared.hotKey.isEmpty ? "" : HotKey.display(NextConfig.shared.hotKey),
        ])
    }

    private func dispatch(_ event: String, _ detail: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: detail, options: [.sortedKeys]),
              let json = String(data: data, encoding: .utf8) else { return }
        let script = "window.dispatchEvent(new CustomEvent(\(Self.jsString(event)), { detail: \(json) }));"
        web.evaluateJavaScript(script) { _, error in
            if let error { shellLog("bar: could not reach the page: \(error.localizedDescription)") }
        }
    }

    /// A string as a JavaScript literal: JSON's quoting is JavaScript's.
    private static func jsString(_ s: String) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: [s]),
              let array = String(data: data, encoding: .utf8) else { return "\"\"" }
        return String(array.dropFirst().dropLast())
    }
}
