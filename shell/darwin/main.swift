// The macOS shell: a window, a menu bar item, and the console inside a WKWebView.
//
// The shell owns no domain state and answers no route. Everything it shows
// comes from the daemon over http://127.0.0.1:7727, which is the same surface
// the browser and the phone use — so there is one client, not three, and a
// thing proved in one place is proved for all of them.
//
// What it is for is the part a web page cannot do: living in the menu bar,
// answering a global hotkey, starting at login, and being a window that comes
// back when the Dock asks. Each of those follows the Swift app's main.swift;
// where this one differs, the difference is written beside it.
import AppKit
import ServiceManagement
import WebKit

let port = ProcessInfo.processInfo.environment["CLAWDLINE_NEXT_PORT"] ?? "7727"
let home = URL(string: "http://127.0.0.1:\(port)/")!

/// The Swift app's accent (Panel.swift `Style.accent`), which the menu bar mark
/// takes while something is waiting for an answer.
let accent = NSColor(srgbRed: 0.851, green: 0.467, blue: 0.341, alpha: 1)

/// The console's window.
///
/// It answers the editing keys itself. The Swift app has no Edit menu — its
/// panel is a native text view that handles them — and this shell keeps the
/// Swift app's menus as they are, but a WKWebView only cuts, copies and pastes
/// when something sends it the action. The page gets the key first; only what
/// it leaves unhandled comes back here, which is where an Edit menu would have
/// caught it.
final class ConsoleWindow: NSWindow {
    var onReload: (() -> Void)?
    /// ⌘[ and ⌘], which are a browser's keys and not a text view's, so nothing
    /// under here would ever claim them. See Browser.swift.
    var onBack: (() -> Void)?
    var onForward: (() -> Void)?
    /// ⌘+ ⌘- ⌘0 (1, -1, 0) and ⇧⌘O — the 顯示方式 menu's keys. Answered here
    /// rather than left to the menu so that "+" pressed without shift, which
    /// arrives as "=", zooms as well, and so that a key pressed at a limit
    /// always beeps rather than depending on what a greyed item does with it.
    var onZoom: ((Int) -> Void)?
    var onOpenOutside: (() -> Void)?

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
            case "[":
                onBack?()
                return true
            case "]":
                onForward?()
                return true
            case "=", "+":
                onZoom?(1)
                return true
            case "-":
                onZoom?(-1)
                return true
            case "0":
                onZoom?(0)
                return true
            default: break
            }
        } else if flags == shifted {
            switch key {
            case "z": action = Selector(("redo:"))
            // Where "+" is the shifted "=", as on a US keyboard.
            case "+":
                onZoom?(1)
                return true
            case "o":
                onOpenOutside?()
                return true
            default: break
            }
        }
        guard let action else { return false }
        return NSApp.sendAction(action, to: nil, from: self)
    }
}

/// Holds the shell weakly for the content controller, which retains its
/// message handlers for as long as the web view lives.
final class WeakMessageHandler: NSObject, WKScriptMessageHandler {
    weak var target: WKScriptMessageHandler?
    init(_ target: WKScriptMessageHandler) { self.target = target }
    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        target?.userContentController(controller, didReceive: message)
    }
}

/// What the menu bar mark is drawn from: the same `sessions` frames the console
/// is already reading.
///
/// Listened to inside the page rather than asked for again. Every stream a
/// client opens makes the daemon take its own reading of the machine — process
/// table, tmux, screens — so a second connection from the shell would double
/// that work to learn what the page already knows. The script wraps the page's
/// EventSource so each stream to /v1/events also reports to the shell; the page
/// sees the same object it asked for.
let fleetBridgeScript = """
(function () {
  var Native = window.EventSource;
  var handlers = window.webkit && window.webkit.messageHandlers;
  if (!Native || !handlers || !handlers.shellFleet) return;
  var post = function (m) { try { handlers.shellFleet.postMessage(m); } catch (e) {} };
  function Watched(url, init) {
    var es = init === undefined ? new Native(url) : new Native(url, init);
    try {
      if (String(url).indexOf('/v1/events') !== -1) {
        es.addEventListener('sessions', function (ev) {
          try {
            var snap = JSON.parse(ev.data);
            var rows = Array.isArray(snap) ? snap : ((snap && snap.sessions) || []);
            var waiting = [], working = 0;
            for (var i = 0; i < rows.length; i++) {
              var s = rows[i] || {};
              if (s.state === 'waiting') waiting.push(String(s.label || s.id || ''));
              else if (s.state === 'working') working++;
            }
            post({ kind: 'sessions', waiting: waiting, working: working });
          } catch (e) { post({ kind: 'unreadable' }); }
        });
        es.addEventListener('error', function () { post({ kind: 'error', readyState: es.readyState }); });
      }
    } catch (e) {}
    return es;
  }
  Watched.prototype = Native.prototype;
  Watched.CONNECTING = Native.CONNECTING;
  Watched.OPEN = Native.OPEN;
  Watched.CLOSED = Native.CLOSED;
  window.EventSource = Watched;
})();
"""

/// The settings window's words, handed to the page before its first script
/// runs. The console's catalog has no keys for them — in the Swift app they
/// were never on a web page — and a page that made its own would be inventing
/// them. See web/console/src/pages/settings/shell.ts for the other half.
func settingsWordsScript() -> String {
    let words: [String: String] = [
        "hotkey": L.t.settingsHotkey,
        "recording": L.t.settingsRecording,
        "scope": L.t.settingsScope,
        "scopeGlobal": L.t.settingsScopeGlobal,
        "off": L.t.settingsOff,
    ]
    let json = (try? JSONSerialization.data(withJSONObject: words, options: [.sortedKeys]))
        .flatMap { String(data: $0, encoding: .utf8) } ?? "{}"
    return """
    (function () {
      var shell = window.__clawdlineShell || (window.__clawdlineShell = {});
      shell.settings = { words: \(json) };
    })();
    """
}

final class Shell: NSObject, NSApplicationDelegate, NSWindowDelegate, WKNavigationDelegate,
                   WKScriptMessageHandler {
    var window: ConsoleWindow!
    var web: WKWebView!
    var statusItem: NSStatusItem!
    var daemon: Process?

    /// The other place this window can be: anywhere on the web, in a cookie
    /// store this machine's token has never been written to. Browser.swift.
    var webTab: ExternalWeb!
    /// The bar across the top, and the part of the window under it.
    var bar: BrowserBar!
    var content: NSView!
    /// Which of the two views `content` is holding.
    var showingWeb = false

    private let hotKey = HotKey()
    private var hotKeyActive = false
    /// The input bar: its own borderless window, over its own page. See Bar.swift.
    lazy var inputBar = BarController(home: home)
    /// Pairing codes, heard on the daemon's local-only stream.
    private let pairing = PairingWatcher(base: home)
    private var readings = MenuReadings()
    /// The app that was in front when the window was summoned, so a hotkey
    /// dismissal can hand the front back — the Swift panel's `previousApp`.
    private var previousApp: NSRunningApplication?

    /// Unknown until the page's first frame, and unknown again when its stream
    /// fails. Unknown draws as the plain mark, which is also what "nothing
    /// running" draws as; the log says which.
    private var waiting: [String] = []
    private var working = 0

    /// Which load a probe or a retry belongs to. A probe that outlives its
    /// page reports on whatever replaced it.
    private var loadGeneration = 0
    private var failedLoads = 0
    /// Whether the console has finished loading in the current generation, so
    /// a page can be asked for now or must be asked for once it has.
    private var pageLoaded = false
    /// A page to go to once the console has loaded — "Settings…" pressed while
    /// the daemon was still coming up.
    private var pendingPage: String?

    /// The settings page's key recorder, while it is listening.
    private var recorder: Any?
    /// The combination is let go while a new one is being recorded, and until
    /// the page has written it or given up.
    private var hotKeySuspended = false

    // MARK: - Launch

    func applicationDidFinishLaunching(_ note: Notification) {
        // A menu bar item that is also an ordinary application somebody can find
        // in the Dock and reopen from Finder — the Swift app's policy.
        NSApp.setActivationPolicy(.regular)

        let config = NextConfig.shared
        shellLog("launch: config=\(config.fileURL.path) hotkey=\(hotKeyLogName(config))"
                 + " scope=\(config.scopeApp.isEmpty ? "(global)" : config.scopeApp)")
        if let problem = config.problem { shellLog("config: \(problem)") }

        MascotPacks.installBundled()
        installMainMenu()
        buildWindow()
        readings.dictation = DictationEngine.status()
        readings.mascots = MascotPacks.available()
        buildStatusItem()
        NotchIsland.shared.install()
        takeSlowReadings()
        if UpdateOffer.feed == nil {
            shellLog("update-check: no release feed for this app; the update row stays absent")
        }

        // The hotkey opens the input bar, which is what it opens in the Swift
        // app and what the menu item above it is called ("打開輸入框"). The
        // console window keeps the Dock, ⇧⌘H and clawdline-next://open. See
        // Bar.swift.
        hotKey.onFire = { [weak self] in
            shellLog("hotkey fired")
            self?.inputBar.toggle()
        }
        // Whether the hotkey should be attached is recomputed whenever the
        // frontmost app changes — and the bar comes back when the terminal it
        // was summoned from does (Bar.appBecameFrontmost).
        NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didActivateApplicationNotification, object: nil, queue: .main
        ) { [weak self] note in
            self?.updateHotKeyScope()
            let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
            self?.inputBar.appBecameFrontmost(app?.bundleIdentifier)
        }
        // The Swift app opened again, or put away. Carbon refuses neither app
        // the shared combination, so nothing here fails; the log and the
        // settings window say that one press now opens two bars (HotKey.swift).
        for name in [NSWorkspace.didLaunchApplicationNotification, NSWorkspace.didTerminateApplicationNotification] {
            NSWorkspace.shared.notificationCenter.addObserver(forName: name, object: nil, queue: .main) { note in
                let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
                guard app?.bundleIdentifier == HotKey.legacyBundleID else { return }
                let launched = name == NSWorkspace.didLaunchApplicationNotification
                shellLog("legacy app \(launched ? "launched" : "quit");"
                         + " same hotkey: \(launched && HotKey.legacyHolds(NextConfig.shared.hotKey))")
                SettingsWindowController.shared.refresh()
            }
        }
        applyConfiguredHotKey(alertOnFailure: true)

        startBundledDaemon()
        // Somebody, somewhere, is asking to pair. The code is shown here and
        // nowhere else — it is never in the reply the asker got — so finishing
        // requires being able to see this screen.
        pairing.onPairing = { [weak self] notice in self?.showPairing(notice) }
        pairing.start()
        // The daemon needs a moment to bind. A failed first load is retried by
        // the navigation delegate below.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.8) { [weak self] in
            self?.reload()
            // The bar's page is loaded now and kept loaded behind a window that
            // is not on screen, so that a hotkey press is a window appearing
            // rather than a page starting (Bar.swift).
            self?.inputBar.reload()
        }

        // A first launch has a visible destination. Once the window has been
        // put away, a launch — at login above all — stays quiet until the Dock,
        // the menu or the hotkey asks. The page loads either way, so the menu
        // bar mark is live and the window is ready when it is asked for.
        if !IntroductionStore().isCurrent {
            DispatchQueue.main.async { [weak self] in self?.showConsole() }
        } else {
            shellLog("window: introduced before; launching quietly")
        }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if flag {
            NSApp.activate(ignoringOtherApps: true)
            return false
        }
        showConsole()
        return true
    }

    // Closing the window leaves the app, the menu bar item and the hotkey.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }

    // The daemon this shell started is this shell's to stop. Leaving it behind
    // would put a second writer over the same state the next time somebody
    // opens the app.
    func applicationWillTerminate(_ note: Notification) {
        pairing.stop()
        daemon?.terminate()
    }

    /// Start the daemon that ships inside this bundle.
    ///
    /// The one in the bundle, never whichever is on PATH: a shell that talks to
    /// a different build than it was made with is a bug nobody can reproduce.
    /// If something already holds the port, the bundled one fails to bind and
    /// exits, and the one already there is what the window shows — a second
    /// daemon over the same state directory would be two writers.
    func startBundledDaemon() {
        guard let dir = Bundle.main.executableURL?.deletingLastPathComponent() else { return }
        let binary = dir.appendingPathComponent("clawdline")
        guard FileManager.default.isExecutableFile(atPath: binary.path) else { return }

        let task = Process()
        task.executableURL = binary
        task.arguments = ["serve"]
        var env = ProcessInfo.processInfo.environment
        if let web = Bundle.main.resourceURL?.appendingPathComponent("web"),
           FileManager.default.fileExists(atPath: web.path) {
            env["CLAWDLINE_NEXT_WEB"] = web.path
            env["CLAWDLINE_NEXT_STANDALONE"] = "1"
            env["CLAWDLINE_NEXT_OWN_SESSIONS"] = "1"
        }
        task.environment = env
        do {
            try task.run()
            daemon = task
            shellLog("daemon: started from the bundle")
        } catch {
            shellLog("daemon: could not start: \(error.localizedDescription)")
        }
    }

    // MARK: - The window

    private func buildWindow() {
        let config = WKWebViewConfiguration()
        let content = WKUserContentController()
        content.addUserScript(WKUserScript(source: fleetBridgeScript,
                                           injectionTime: .atDocumentStart,
                                           forMainFrameOnly: true))
        content.add(WeakMessageHandler(self), name: "shellFleet")
        content.addUserScript(WKUserScript(source: settingsWordsScript(),
                                           injectionTime: .atDocumentStart,
                                           forMainFrameOnly: true))
        content.add(WeakMessageHandler(self), name: "shellSettings")
        // The notch island listens to the same stream and opens this window
        // when its character is pressed (NotchIsland.swift).
        NotchIsland.shared.attach(to: content, open: { [weak self] in self?.showConsole() })
        config.userContentController = content
        web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = self
        // The microphone, and only for the console's own origin — see
        // Microphone.swift. Without a UI delegate a WKWebView refuses
        // `getUserMedia` silently, so dictation would look broken in the app
        // and work in a browser.
        web.uiDelegate = self

        window = ConsoleWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1200, height: 800),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false)
        window.title = "Clawdline"
        // Kept, not rebuilt: the page stays loaded behind a closed window, and
        // the size somebody gave it is still the size when it comes back.
        window.isReleasedWhenClosed = false
        // The Swift app's Home window floor.
        window.minSize = NSSize(width: 680, height: 620)
        // The console is one of the two views this window can show; the bar and
        // the switch between them are in Browser.swift.
        window.contentView = buildBrowser()
        window.delegate = self
        window.onReload = { [weak self] in self?.reloadActive() }
        window.onBack = { [weak self] in self?.browserBack() }
        window.onForward = { [weak self] in self?.browserForward() }
        window.onZoom = { [weak self] step in self?.zoomActive(by: step) }
        window.onOpenOutside = { [weak self] in self?.browserOpenOutside() }
        window.center()
    }

    /// Bring the window forward, where the person is.
    ///
    /// Nothing about its place is remembered between launches, as nothing is in
    /// the Swift app: a window that was not on screen comes back centred on the
    /// display under the pointer — the display the Swift panel opens on — at the
    /// size it last had in this run.
    func showConsole() {
        let front = NSWorkspace.shared.frontmostApplication
        if front?.processIdentifier != NSRunningApplication.current.processIdentifier {
            previousApp = front
        }
        if window.isMiniaturized {
            window.deminiaturize(nil)
        } else if !window.isVisible {
            placeOnPointerScreen()
        }
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
        updateHotKeyScope()
    }

    /// The hotkey's toggle. A window in front goes away and hands the front
    /// back to whoever had it; a window anywhere else — behind, minimised,
    /// closed — comes forward.
    func toggleConsole() {
        if window.isVisible && window.isKeyWindow && NSApp.isActive {
            window.orderOut(nil)
            if let previousApp, !previousApp.isTerminated {
                previousApp.activate()
            }
            updateHotKeyScope()
        } else {
            showConsole()
        }
    }

    private func placeOnPointerScreen() {
        let p = NSEvent.mouseLocation
        guard let screen = NSScreen.screens.first(where: { NSMouseInRect(p, $0.frame, false) })
                ?? NSScreen.main ?? NSScreen.screens.first else { return }
        let v = screen.visibleFrame
        var f = window.frame
        f.size.width = min(f.width, v.width)
        f.size.height = min(f.height, v.height)
        f.origin = NSPoint(x: v.midX - f.width / 2, y: v.midY - f.height / 2)
        window.setFrame(f, display: false)
        window.center()
    }

    func windowWillClose(_ notification: Notification) {
        guard notification.object as? NSWindow === window else { return }
        stopRecording(restore: true)
        // Putting the window away is what the Swift app records when Home is
        // dismissed: from now on a launch does not open it by itself.
        let store = IntroductionStore()
        if !store.isCurrent, store.record() {
            shellLog("window: closed for the first time; later launches stay quiet")
        }
        // Still visible while it closes; ask once it has gone.
        DispatchQueue.main.async { [weak self] in self?.updateHotKeyScope() }
    }

    func windowDidMiniaturize(_ notification: Notification) { updateHotKeyScope() }
    func windowDidDeminiaturize(_ notification: Notification) { updateHotKeyScope() }

    @objc func reload() { load(home) }

    /// Load a page of the console's, with this machine's token in place first.
    ///
    /// `url` is always the console's own origin — `go(to:)` in Browser.swift is
    /// the only caller that passes anything but `home`, and it checks. Every
    /// route but the page itself needs a token, this window included. The
    /// cookie goes in first; see LocalToken.install for why it is not adopted
    /// through the page.
    func load(_ url: URL) {
        loadGeneration += 1
        pageLoaded = false
        let generation = loadGeneration
        LocalToken.install(in: web.configuration.websiteDataStore.httpCookieStore, for: home) { [weak self] _ in
            guard let self, generation == self.loadGeneration else { return }
            self.web.load(URLRequest(url: url))
        }
    }

    // MARK: - Pairing

    /// The Swift app's alert, word for word, with its one button.
    ///
    /// One at a time: something hammering the pairing route must not stack a
    /// wall of alerts to dismiss one by one. The watcher holds that gate (see
    /// PairingWatcher for why it cannot be held here); the daemon's rate limit
    /// is the other half.
    private func showPairing(_ pending: PairingNotice) {
        defer { pairing.alertClosed() }
        // The name, never the code.
        shellLog("pairing: \(pending.name) is asking; alert shown")
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert()
        alert.messageText = L.t.pairingAsks(pending.name)
        alert.informativeText = L.t.pairingCode(pending.code)
        alert.alertStyle = .informational
        alert.addButton(withTitle: L.t.pairingIgnore)
        alert.runModal()
    }

    // MARK: - Where the hotkey applies

    private enum HotKeyOutcome { case none, registered, failed }

    /// The combination as the log names it, saying whether the file chose it.
    private func hotKeyLogName(_ config: NextConfig) -> String {
        if config.hotKey.isEmpty { return "(none)" }
        return config.hotKeyIsDefault ? "\(config.hotKey) (default)" : config.hotKey
    }

    /// Take up what the config names, or nothing when it names nothing.
    ///
    /// The combination is asked about first and registered only where the
    /// scope wants it. The Swift app registered it everywhere here and let the
    /// scope take it back a moment later; with a default somebody never chose,
    /// even that moment is ⌥Space taken from whatever app was in front.
    @discardableResult
    private func applyConfiguredHotKey(alertOnFailure: Bool) -> HotKeyOutcome {
        let config = NextConfig.shared
        // A fresh reading of the file: what went wrong with the last one is
        // not this one's trouble.
        hotKey.turnOff()
        hotKeyActive = false
        guard !config.hotKey.isEmpty else {
            shellLog("hotkey: set to none in \(config.fileURL.path); nothing registered")
            return .none
        }
        if hotKey.check(config.hotKey) == nil {
            updateHotKeyScope()
        }
        guard let trouble = hotKey.trouble else {
            shellLog("hotkey: \(HotKey.display(config.hotKey))"
                     + (config.hotKeyIsDefault ? " (default)" : "")
                     + (config.scopeApp.isEmpty ? " (global)" : " (only while \(config.scopeApp) or this app is in front)")
                     + (hotKeyActive ? ", registered now" : ", not in front yet"))
            if HotKey.legacyHolds(config.hotKey) {
                shellLog("hotkey: the Swift app (\(HotKey.legacyBundleID)) is running with the same"
                         + " combination; one press opens both input bars")
            }
            return .registered
        }
        shellLog("hotkey registration failed: \(config.hotKey) — \(trouble)")
        if alertOnFailure {
            // Said, or somebody presses it for a while and concludes the app
            // never started. Short, and without the file's path: the way round
            // it is the menu bar, and the way to change it is the settings
            // window. After the launch has finished rather than inside it, so
            // an unanswered alert does not keep the daemon from starting.
            let title = L.t.hotkeyFailedTitle(HotKey.display(config.hotKey))
            let body = L.t.hotkeyFailedBody(system: trouble == .system)
            DispatchQueue.main.async {
                let a = NSAlert()
                a.messageText = title
                a.informativeText = body
                a.alertStyle = .warning
                a.runModal()
            }
        }
        return .failed
    }

    /// Carbon hotkeys are global; there is no "only in this app" option. So the
    /// combination is attached while one of the scoped apps is frontmost (or
    /// this one, or the window is up) and detached otherwise — and stays itself
    /// in every other app instead of being swallowed here.
    private func updateHotKeyScope() {
        let config = NextConfig.shared
        guard !config.hotKey.isEmpty, !hotKeySuspended else { return }
        let scope = config.scopeApp
        guard !scope.isEmpty else {
            if !hotKeyActive {
                hotKeyActive = hotKey.register(config.hotKey)
            }
            return
        }
        let front = NSWorkspace.shared.frontmostApplication?.bundleIdentifier ?? ""
        let mine = Bundle.main.bundleIdentifier ?? ""
        let allowed = scope.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
        // The bar counts as well as the console window: it is what the hotkey
        // now opens, and a combination detached while it is up would be a
        // combination that cannot close it (Bar.swift).
        let visible = (window?.isVisible == true && window?.isMiniaturized == false) || inputBar.isVisible
        let want = allowed.contains(front) || front == mine || visible

        if want, !hotKeyActive {
            hotKeyActive = hotKey.register(config.hotKey)
            shellLog("hotkey \(hotKeyActive ? "attached" : "could not attach") (frontmost: \(front))")
        } else if !want, hotKeyActive {
            hotKey.unregister()
            hotKeyActive = false
            shellLog("hotkey detached (frontmost: \(front))")
        }
    }

    // MARK: - Main menu

    /// The App, Window and Help menus, as the Swift app has them, and 顯示方式
    /// between the first two, which the Swift app has no need of: it is the
    /// browser's menu (Browser.swift).
    private func installMainMenu() {
        let main = NSMenu()
        let appItem = NSMenuItem(title: L.t.menuApplication, action: nil, keyEquivalent: "")
        let appMenu = NSMenu()

        appMenu.addItem(NSMenuItem(
            title: L.t.menuAbout(L.t.menuApplication),
            action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)),
            keyEquivalent: ""))
        appMenu.addItem(.separator())

        let homeItem = NSMenuItem(title: L.t.menuHome, action: #selector(showHome), keyEquivalent: "h")
        homeItem.keyEquivalentModifierMask = [.command, .shift]
        homeItem.target = self
        appMenu.addItem(homeItem)
        appMenu.addItem(.separator())

        // The native settings window, in its own window — SettingsWindow.swift.
        let settings = NSMenuItem(title: L.t.menuEditConfig, action: #selector(openSettings),
                                  keyEquivalent: ",")
        settings.target = self
        appMenu.addItem(settings)
        appMenu.addItem(.separator())

        let services = NSMenuItem(title: L.t.menuServices, action: nil, keyEquivalent: "")
        let servicesMenu = NSMenu()
        services.submenu = servicesMenu
        appMenu.addItem(services)
        NSApp.servicesMenu = servicesMenu
        appMenu.addItem(.separator())

        appMenu.addItem(NSMenuItem(title: L.t.menuHide(L.t.menuApplication),
                                   action: #selector(NSApplication.hide(_:)), keyEquivalent: "h"))
        let hideOthers = NSMenuItem(title: L.t.menuHideOthers,
                                    action: #selector(NSApplication.hideOtherApplications(_:)),
                                    keyEquivalent: "h")
        hideOthers.keyEquivalentModifierMask = [.command, .option]
        appMenu.addItem(hideOthers)
        appMenu.addItem(NSMenuItem(title: L.t.menuShowAll,
                                   action: #selector(NSApplication.unhideAllApplications(_:)),
                                   keyEquivalent: ""))
        appMenu.addItem(.separator())
        appMenu.addItem(NSMenuItem(title: L.t.menuQuit,
                                   action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"))
        appItem.submenu = appMenu
        main.addItem(appItem)
        main.addItem(browserMenuItem())

        let windowItem = NSMenuItem(title: L.t.menuWindow, action: nil, keyEquivalent: "")
        let windowMenu = NSMenu(title: L.t.menuWindow)
        windowMenu.addItem(NSMenuItem(title: L.t.menuClose, action: #selector(NSWindow.performClose(_:)),
                                      keyEquivalent: "w"))
        windowMenu.addItem(NSMenuItem(title: L.t.menuMinimize,
                                      action: #selector(NSWindow.performMiniaturize(_:)),
                                      keyEquivalent: "m"))
        windowItem.submenu = windowMenu
        main.addItem(windowItem)
        NSApp.windowsMenu = windowMenu

        let helpItem = NSMenuItem(title: L.t.menuHelp, action: nil, keyEquivalent: "")
        let helpMenu = NSMenu(title: L.t.menuHelp)
        let documentation = NSMenuItem(title: L.t.menuHelpDocumentation,
                                       action: #selector(openDocumentation), keyEquivalent: "?")
        documentation.target = self
        helpMenu.addItem(documentation)
        helpItem.submenu = helpMenu
        main.addItem(helpItem)
        NSApp.helpMenu = helpMenu

        NSApp.mainMenu = main
    }

    // MARK: - Status item

    private static let loginTag = 100
    private static let mascotTag = 200

    private func buildStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem.button?.title = "✳"
        statusItem.button?.font = NSFont.systemFont(ofSize: 13, weight: .medium)
        statusItem.menu = buildMenu()
    }

    /// The mark carries whether something is waiting and how much is running —
    /// the Swift app's `refreshStatusItem`, fed from the page's frames.
    private func refreshStatusItem() {
        guard let button = statusItem?.button else { return }
        let title = NSMutableAttributedString(string: "✳", attributes: [
            .font: NSFont.systemFont(ofSize: 13, weight: .medium),
            .foregroundColor: waiting.isEmpty ? NSColor.labelColor : accent,
        ])
        if !waiting.isEmpty {
            title.append(NSAttributedString(string: " ●", attributes: [
                .font: NSFont.systemFont(ofSize: 9, weight: .bold),
                .foregroundColor: accent,
            ]))
        } else if working > 1 {
            title.append(NSAttributedString(string: " \(working)", attributes: [
                .font: NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .medium),
                .foregroundColor: NSColor.secondaryLabelColor,
            ]))
        }
        button.attributedTitle = title
        button.toolTip = waiting.isEmpty
            ? (working == 0 ? nil : L.t.statusWorking(working))
            : L.t.statusWaiting(waiting)
    }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        if message.name == "shellSettings" {
            settingsMessage(message)
            return
        }
        guard message.name == "shellFleet", let body = message.body as? [String: Any] else { return }
        let kind = body["kind"] as? String ?? ""
        let before = (waiting, working)
        switch kind {
        case "sessions":
            waiting = body["waiting"] as? [String] ?? []
            working = (body["working"] as? NSNumber)?.intValue ?? 0
        case "error", "unreadable":
            // Not known is drawn as quiet, never as the last thing that was true.
            waiting = []
            working = 0
            if kind == "unreadable" { shellLog("fleet: a sessions frame could not be read") }
        default:
            return
        }
        if before.0 != waiting || before.1 != working {
            shellLog("fleet: \(kind) working=\(working) waiting=\(waiting.count)")
        }
        refreshStatusItem()
    }

    private func buildMenu() -> NSMenu {
        let menu = NSMenu()
        menu.delegate = self

        let open = NSMenuItem(title: L.t.menuOpen, action: #selector(openPanel), keyEquivalent: "")
        open.target = self
        menu.addItem(open)

        // The Swift app focuses the terminal tab its panel is aimed at. Nothing
        // here aims at a tab, and the daemon has no route that raises one.
        menu.addItem(NSMenuItem(title: L.t.menuReveal, action: nil, keyEquivalent: ""))

        let homeItem = NSMenuItem(title: L.t.menuHome, action: #selector(showHome), keyEquivalent: "")
        homeItem.target = self
        menu.addItem(homeItem)

        // What dictation would use, read the way the Swift app reads it. This
        // app does not dictate, so the row says the same thing and does nothing.
        menu.addItem(NSMenuItem(title: L.t.dictationStatus(readings.dictation),
                                action: nil, keyEquivalent: ""))

        if let latest = readings.newerRelease {
            let update = NSMenuItem(
                title: L.t.updateAvailable(latest: latest, installed: UpdateOffer.installedVersion),
                action: #selector(openReleases), keyEquivalent: "")
            update.target = self
            menu.addItem(update)
        }

        for standing in readings.standings {
            let item = NSMenuItem(title: L.t.compatNote(standing),
                                  action: #selector(openCompatibility), keyEquivalent: "")
            item.target = self
            menu.addItem(item)
        }

        let mascot = NSMenuItem(title: L.t.menuMascot, action: nil, keyEquivalent: "")
        mascot.tag = Self.mascotTag
        mascot.submenu = buildMascotMenu()
        menu.addItem(mascot)

        menu.addItem(.separator())

        let login = NSMenuItem(title: L.t.menuLogin, action: #selector(toggleLogin), keyEquivalent: "")
        login.target = self
        login.tag = Self.loginTag
        menu.addItem(login)

        let edit = NSMenuItem(title: L.t.menuEditConfig, action: #selector(openSettings), keyEquivalent: "")
        edit.target = self
        menu.addItem(edit)

        let reloadItem = NSMenuItem(title: L.t.menuReload, action: #selector(reloadConfig), keyEquivalent: "")
        reloadItem.target = self
        menu.addItem(reloadItem)

        menu.addItem(.separator())

        menu.addItem(NSMenuItem(title: L.t.menuQuit, action: #selector(NSApplication.terminate(_:)),
                                keyEquivalent: "q"))
        return menu
    }

    /// The packs there are, with the configured one ticked. The island draws
    /// the ticked one; picking a different one from here is not wired, so the
    /// items are listed and off, and the config file is what chooses.
    private func buildMascotMenu() -> NSMenu {
        let sub = NSMenu()
        for name in readings.mascots {
            let item = NSMenuItem(title: name, action: nil, keyEquivalent: "")
            item.state = (name == NextConfig.shared.mascot) ? .on : .off
            sub.addItem(item)
        }
        if sub.items.isEmpty {
            sub.addItem(NSMenuItem(title: MascotPacks.userDirectory.path, action: nil, keyEquivalent: ""))
        }
        return sub
    }

    /// The readings that start programs — `claude --version`, `codex
    /// --version` — are taken off the main thread and the menu rebuilt when
    /// they land. Usually they add nothing, which is the point of them.
    private func takeSlowReadings() {
        DispatchQueue.global(qos: .utility).async { [weak self] in
            let taken = MenuReadings.take()
            DispatchQueue.main.async {
                guard let self else { return }
                self.readings = taken
                self.statusItem.menu = self.buildMenu()
                shellLog("menu: dictation=\(taken.dictation) compat-rows=\(taken.standings.count)"
                         + " mascots=\(taken.mascots.joined(separator: ","))")
            }
        }
    }

    // MARK: - Actions

    // "打開輸入框" opens the input bar, which is what it says and what it opens
    // in the Swift app. The rows under it — "主頁／設定中心", "設定⋯" — are still
    // the console window's (Bar.swift).
    @objc private func openPanel() { inputBar.toggle() }

    /// "Home" is the console, so it is also the tab the window is on.
    @objc private func showHome() {
        showConsole()
        showTab(web: false)
    }

    /// Launch at login, through SMAppService, off until somebody turns it on.
    /// Registering changes the person's login items; nothing here does it on
    /// its own.
    @objc private func toggleLogin() {
        let svc = SMAppService.mainApp
        do {
            if svc.status == .enabled { try svc.unregister() } else { try svc.register() }
            shellLog("login item: now \(svc.status == .enabled ? "on" : "off")")
        } catch {
            let a = NSAlert()
            a.messageText = L.t.loginFailed
            a.informativeText = error.localizedDescription
            a.runModal()
        }
    }

    /// Read the config again and re-apply what it can change.
    @objc private func reloadConfig() {
        let config = NextConfig.shared
        config.load()
        if let problem = config.problem { shellLog("config: \(problem)") }
        installMainMenu()
        applyConfiguredHotKey(alertOnFailure: true)
        updateHotKeyScope()
        readings.dictation = DictationEngine.status()
        readings.mascots = MascotPacks.available()
        statusItem.menu = buildMenu()
        refreshStatusItem()
        NotchIsland.shared.install()
        sendSettingsState()
    }

    @objc private func openReleases() {
        if let feed = UpdateOffer.feed { NSWorkspace.shared.open(feed) }
    }

    @objc private func openCompatibility() {
        NSWorkspace.shared.open(URL(string: "https://github.com/sainteye/clawdline/blob/main/docs/compatibility.md")!)
    }

    @objc private func openDocumentation() {
        NSWorkspace.shared.open(URL(string: "https://github.com/sainteye/clawdline#readme")!)
    }

    /// clawdline-next://open, so a hotkey utility, Shortcuts or a script can
    /// summon the window without the built-in combination. Its own scheme: the
    /// Swift app owns clawdline://. Only what this app can do is answered.
    func application(_ application: NSApplication, open urls: [URL]) {
        shellLog("url: \(urls.map { $0.absoluteString }.joined(separator: " "))")
        guard let url = urls.first else { return }
        switch url.host ?? "" {
        case "toggle":
            toggleConsole()
        case "", "open", "home", "setup":
            showConsole()
        default:
            shellLog("url: \(url.host ?? "") is a Swift app route this app does not have; showing the window")
            showConsole()
        }
    }

    // MARK: - Navigation

    /// The console's view stays on the console.
    ///
    /// This is the view that carries this machine's token in its cookie store,
    /// so where it is allowed to go is not a matter of taste. A link out of the
    /// console opens in the person's own browser — what the Swift app does with
    /// every link it shows, `NSWorkspace.shared.open` — and the browser beside
    /// it is reached by its own tab, never by a page steering this one.
    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url else {
            decisionHandler(.allow)
            return
        }
        // No target frame means a new window, which `createWebViewWith` in
        // Microphone.swift answers; a subframe is the page's own business, and
        // an iframe that could open the person's browser would be a page
        // reaching out through this window.
        guard let frame = action.targetFrame, frame.isMainFrame else {
            decisionHandler(.allow)
            return
        }
        // `about:blank` is the page this shell writes itself when the daemon is
        // not answering.
        if isConsole(url) || url.scheme == "about" {
            decisionHandler(.allow)
            return
        }
        decisionHandler(.cancel)
        // `file:` is this Mac's disk; the console has no reason to ask for one
        // and a page is not the thing that decides to open it.
        guard url.scheme != "file" else {
            shellLog("browser: refused \(url.absoluteString) — a page may not open this Mac's files")
            return
        }
        shellLog("browser: \(url.absoluteString) is not the console; handing it to the browser")
        NSWorkspace.shared.open(url)
    }

    // The shell says what it actually loaded. A window that came up is not
    // evidence that the console is in it, and on a machine without screen
    // recording permission this is the only honest way to tell.
    //
    // It asks after the page has drawn, not when the document arrived. The
    // console is rendered by script and stays hidden under `booting` until its
    // words land, so counting at didFinish reported one element for a page that
    // was about to have hundreds — a true number about the wrong moment.
    func webView(_ webView: WKWebView, didFinish nav: WKNavigation!) {
        guard webView.url?.host == home.host else { return }
        failedLoads = 0
        pageLoaded = true
        if let page = pendingPage {
            pendingPage = nil
            goToPage(page)
        }
        report(webView, attempt: 0, generation: loadGeneration)
    }

    private func report(_ webView: WKWebView, attempt: Int, generation: Int) {
        let probe = "[document.documentElement.classList.contains('booting'), document.querySelectorAll('[id]').length, document.querySelectorAll('li.row').length]"
        webView.evaluateJavaScript(probe) { [weak self] value, _ in
            guard let self, generation == self.loadGeneration else { return }
            let parts = value as? [Any] ?? []
            let booting = parts.first as? Bool ?? true
            let rowsNow = parts.count > 2 ? (parts[2] as? Int ?? 0) : 0
            // The list arrives after the words do, so a page that is showing
            // but has no rows yet is asked again rather than reported empty.
            if (booting || rowsNow == 0) && attempt < 40 {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) {
                    self.report(webView, attempt: attempt + 1, generation: generation)
                }
                return
            }
            let nodes = parts.count > 1 ? (parts[1] as? Int ?? -1) : -1
            let rows = parts.count > 2 ? (parts[2] as? Int ?? -1) : -1
            shellLog("loaded: \(webView.url?.absoluteString ?? "?") title=\(webView.title ?? "?") booting=\(booting) elements-with-id=\(nodes) rows=\(rows) visible=\(self.window.isVisible)")
        }
    }

    // A daemon that is not running is the ordinary case on a fresh machine,
    // and the shell says which door it knocked on rather than showing a blank
    // page — then knocks again, less often each time, because the daemon it
    // started is usually a second away from answering.
    func webView(_ webView: WKWebView, didFailProvisionalNavigation nav: WKNavigation!,
                 withError error: Error) {
        failedLoads += 1
        pageLoaded = false
        let generation = loadGeneration
        let retrying = failedLoads <= 30
        let html = """
        <body style="font:14px -apple-system;padding:40px;color:#ddd;background:#1c1c1e">
        <h2>The daemon is not answering</h2>
        <p>This shell shows what <code>\(home.absoluteString)</code> serves, and nothing is
        listening there.</p>
        <p style="color:#888">Start it with <code>clawdline serve</code>. \(retrying
            ? "This window tries again by itself; ⌘R tries now."
            : "This window has stopped trying; ⌘R tries again.")</p>
        <p style="color:#666">\(error.localizedDescription)</p></body>
        """
        webView.loadHTMLString(html, baseURL: nil)
        shellLog("load failed (\(failedLoads)): \(error.localizedDescription)")
        guard retrying else { return }
        let delay = min(Double(failedLoads) * 2, 10)
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            guard let self, generation == self.loadGeneration else { return }
            self.reload()
        }
    }

    // A page whose process died leaves a blank window; load it again.
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        shellLog("web content process ended; reloading")
        reload()
    }
}

// MARK: - Settings

/// "Settings…", and the settings page's half of the conversation.
///
/// The Swift app's settings window writes the config file, then posts
/// `clawdlineConfigChanged`, and the app re-applies the hotkey and the rest in
/// one place. Here the page writes the file through the daemon and then says
/// `changed`; the shell reads the file again and re-applies it the same way,
/// then tells the page what it actually registered. A hand edit still takes
/// "Reload config", as there — nothing watches the file.
extension Shell {
    @objc func openSettings() {
        // The settings window is its own window now (SettingsWindow.swift),
        // the way the Swift app's is, rather than a page in the console.
        SettingsWindowController.shared.show(host: self)
    }

    /// Ask the console for a page the way its own address does. Before the
    /// console has loaded, the request waits for it.
    func goToPage(_ name: String) {
        guard pageLoaded else {
            pendingPage = name
            return
        }
        let fragment = "#page=" + (name.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? name)
        let script = """
        (function () {
          try { history.replaceState(history.state, '', \(jsString(fragment))); }
          catch (e) { location.hash = \(jsString(fragment)); return; }
          window.dispatchEvent(new HashChangeEvent('hashchange'));
        })();
        """
        web.evaluateJavaScript(script) { _, error in
            if let error { shellLog("page: could not go to \(name): \(error.localizedDescription)") }
        }
    }

    func settingsMessage(_ message: WKScriptMessage) {
        // Only the console this shell loaded may ask; a page it navigated to
        // by a link is somebody else's.
        guard message.frameInfo.isMainFrame,
              message.frameInfo.request.url?.host == home.host,
              message.frameInfo.request.url?.port == home.port,
              let body = message.body as? [String: Any],
              let kind = body["kind"] as? String else { return }
        switch kind {
        case "state":
            sendSettingsState()
        case "record":
            startRecording()
        case "stopRecording":
            stopRecording(restore: true)
        case "changed":
            configChanged()
        default:
            shellLog("settings: unknown request \(kind)")
        }
    }

    /// `configChanged`: what "Reload config" does, without the alert — the
    /// page says a failed registration under the chip instead.
    func configChanged() {
        stopRecording(restore: false)
        hotKeySuspended = false
        let config = NextConfig.shared
        config.load()
        if let problem = config.problem { shellLog("config: \(problem)") }
        installMainMenu()
        applyConfiguredHotKey(alertOnFailure: false)
        updateHotKeyScope()
        statusItem.menu = buildMenu()
        refreshStatusItem()
        NotchIsland.shared.install()
        shellLog("settings: applied hotkey=\(hotKeyLogName(config))"
                 + " registered=\(hotKey.isRegistered) scope=\(config.scopeApp.isEmpty ? "(global)" : config.scopeApp)")
        sendSettingsState()
    }

    func sendSettingsState() {
        guard pageLoaded else { return }
        let config = NextConfig.shared
        let display = config.hotKey.isEmpty ? "" : HotKey.display(config.hotKey)
        let state: [String: Any] = [
            "hotkey": config.hotKey,
            "display": display,
            "registered": hotKey.isRegistered,
            "failure": (!config.hotKey.isEmpty && hotKey.trouble != nil) ? L.t.hotkeyFailedTitle(display) : "",
            "scopeApp": config.scopeApp,
        ]
        dispatchToPage("clawdline-shell-settings", state)
    }

    /// `startRecording`: the next key pressed with a modifier is the answer,
    /// Escape leaves the setting alone, and a bare letter is swallowed — it is
    /// not an answer, and it is not text either.
    ///
    /// One difference: the configured combination is let go while listening.
    /// In the Swift app, pressing it there toggled the panel behind the
    /// settings window; here the settings page *is* the window, and the press
    /// would hide the thing that is waiting for it.
    func startRecording() {
        stopRecording(restore: false)
        hotKey.unregister()
        hotKeyActive = false
        hotKeySuspended = true
        recorder = NSEvent.addLocalMonitorForEvents(matching: [.keyDown]) { [weak self] event in
            guard let self, event.window === self.window else { return event }
            if event.keyCode == 53 {
                self.stopRecording(restore: true)
                return nil
            }
            guard let spec = HotKey.spec(forKeyCode: event.keyCode, flags: event.modifierFlags) else {
                return nil
            }
            // The combination comes back once the page has written the new one
            // and said so; if the write fails the page says stopRecording, and
            // closing the page or the window does the same.
            self.stopRecording(restore: false)
            self.dispatchToPage("clawdline-shell-hotkey", ["spec": spec, "display": HotKey.display(spec)])
            return nil
        }
        shellLog("settings: recording a hotkey")
    }

    /// Stop listening. `restore` puts back the combination the file names;
    /// a page still waiting for an answer is told there will not be one.
    func stopRecording(restore: Bool) {
        let listening = recorder != nil
        if let monitor = recorder {
            NSEvent.removeMonitor(monitor)
            recorder = nil
        }
        guard restore else { return }
        if hotKeySuspended {
            hotKeySuspended = false
            applyConfiguredHotKey(alertOnFailure: false)
            updateHotKeyScope()
        }
        if listening { dispatchToPage("clawdline-shell-hotkey", ["cancelled": true]) }
    }

    private func dispatchToPage(_ event: String, _ detail: [String: Any]) {
        guard pageLoaded,
              let data = try? JSONSerialization.data(withJSONObject: detail, options: [.sortedKeys]),
              let json = String(data: data, encoding: .utf8) else { return }
        let script = "window.dispatchEvent(new CustomEvent(\(jsString(event)), { detail: \(json) }));"
        web.evaluateJavaScript(script) { _, error in
            if let error { shellLog("settings: could not reach the page: \(error.localizedDescription)") }
        }
    }

    /// A string as a JavaScript literal: JSON's quoting is JavaScript's.
    private func jsString(_ s: String) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: [s]),
              let array = String(data: data, encoding: .utf8) else { return "\"\"" }
        return String(array.dropFirst().dropLast())
    }
}

/// The settings window's two hooks into the app (SettingsWindow.swift).
///
/// Here rather than there because both touch members this file keeps private,
/// and because they are the whole of what that window may reach: it draws
/// nothing of this app's state and changes nothing but the file.
extension Shell: SettingsWindowHost {
    func settingsSuspendHotKey() {
        stopRecording(restore: false)
        hotKey.unregister()
        hotKeyActive = false
        hotKeySuspended = true
    }

    func settingsApplyConfig() { configChanged() }
}

extension Shell: NSMenuDelegate {
    func menuWillOpen(_ menu: NSMenu) {
        guard menu === statusItem.menu else { return }
        // No combination beside the item when there is none that works: a
        // menu showing ⌥Space is a menu saying ⌥Space opens this.
        let spec = NextConfig.shared.hotKey
        menu.item(at: 0)?.title = spec.isEmpty || hotKey.trouble != nil
            ? L.t.menuOpen : "\(L.t.menuOpen)   \(HotKey.display(spec))"
        menu.item(at: 1)?.title = "\(L.t.menuReveal)   \(L.t.menuNoTarget)"
        menu.item(withTag: Self.mascotTag)?.submenu = buildMascotMenu()
        menu.item(withTag: Self.loginTag)?.state = (SMAppService.mainApp.status == .enabled) ? .on : .off
    }
}

let app = NSApplication.shared
let shell = Shell()
app.delegate = shell
app.run()
