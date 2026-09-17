// The native "Clawdline 設定" window — a window, and nothing else.
//
// The Swift app draws this window in AppKit, 3,822 lines of it
// (Sources/Settings.swift): a tab strip, two columns of hand-drawn switches,
// sliders and popups, and a footer with the config path. None of that is here.
// The window's inside is a web page served by this machine's own daemon
// (web/console/src/pages/settings/window, at /settings.html), because the rows
// of a settings window are the same rows on Linux and on Windows and the window
// around them is not. What is left on this side is the list a page cannot do:
//
//   - a key combination seen before the page sees it, so one can be recorded;
//   - what an application is called and what its icon looks like, given a
//     bundle identifier, and which ones are open right now;
//   - a file picker that returns an application;
//   - showing a file in the Finder;
//   - the machine's monospaced faces, its mascot packs, whether whisper is
//     installed;
//   - re-applying what the page wrote, because this is what registered it.
//
// The protocol is in docs/shell-bridge.md and its other half is bridge.ts.
// **No sentence crosses it.** A reading goes over as a fact — `{"kind":
// "noModel"}` — and the words for it are in the page's copy.ts, transcribed
// from Copy+Chinese.swift. A shell that sent Chinese would be a shell every
// other platform had to translate again.
import AppKit
import WebKit

/// What the settings window needs from the app around it.
///
/// Two calls, because the hotkey is the app's and not this window's: the
/// combination is let go while a new one is being recorded, and everything that
/// follows from the file is re-applied in the one place that already does it
/// (`Shell.configChanged`, main.swift).
protocol SettingsWindowHost: AnyObject {
    /// Let the configured combination go: a press of it while the recorder is
    /// listening would fire the thing that is waiting for the press.
    func settingsSuspendHotKey()
    /// Read the file again and re-apply the hotkey, the menus and the rest.
    func settingsApplyConfig()
}

/// `Style.ink` (Sources/Panel.swift), so the title bar and the page's own
/// background are one surface rather than two stacked ones.
private let settingsInk = NSColor(srgbRed: 0.08, green: 0.08, blue: 0.09, alpha: 1)

final class SettingsWindowController: NSObject, NSWindowDelegate, WKNavigationDelegate,
                                      WKScriptMessageHandler, WKUIDelegate {

    static let shared = SettingsWindowController()
    private override init() { super.init() }

    private weak var host: SettingsWindowHost?
    private var window: NSWindow?
    private var web: WKWebView?
    private var loaded = false
    /// The key recorder, while it is listening.
    private var recorder: Any?
    /// Set when the app let its combination go for this window's recorder.
    private var suspended = false

    /// The window's own address. A second document rather than the console's
    /// `#page=settings`: this is a settings window, and the session list, the
    /// drawer and the event stream have no business being loaded inside one.
    private var address: URL { URL(string: "settings.html", relativeTo: home) ?? home }

    // MARK: - Showing it

    /// `SettingsWindow.show()`: forward, centred, and on the tab you were last
    /// on — the window is kept rather than rebuilt, so the page keeps its state.
    func show(host: SettingsWindowHost) {
        self.host = host
        if window == nil { build() }
        guard let window else { return }
        if !window.isVisible { window.center() }
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
        if loaded { sendState() } else { load() }
    }

    private func build() {
        let config = WKWebViewConfiguration()
        let content = WKUserContentController()
        content.addUserScript(WKUserScript(source: Self.announceScript,
                                           injectionTime: .atDocumentStart,
                                           forMainFrameOnly: true))
        content.add(WeakMessageHandler(self), name: "shellSettingsWindow")
        config.userContentController = content

        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = self
        web.uiDelegate = self
        self.web = web

        // Not the console's window class: ⌘R reloading a settings window is a
        // way to lose a recording in progress, and this one has no session to
        // go back to. The editing keys still come from the main menu.
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 940, height: 660),
                              styleMask: [.titled, .closable, .miniaturizable, .resizable],
                              backing: .buffered, defer: false)
        window.title = L.t.settingsTitle
        window.titlebarAppearsTransparent = true
        window.backgroundColor = settingsInk
        window.appearance = NSAppearance(named: .darkAqua)
        window.isReleasedWhenClosed = false
        window.minSize = NSSize(width: 520, height: 400)
        window.contentView = web
        window.delegate = self
        window.center()
        self.window = window
    }

    private func load() {
        guard let web else { return }
        // The cookie goes in first, for the reason LocalToken.install gives:
        // this window's page is behind the same gate every other client is.
        LocalToken.install(in: web.configuration.websiteDataStore.httpCookieStore, for: home) { [weak self] _ in
            guard let self, let web = self.web else { return }
            web.load(URLRequest(url: self.address))
        }
    }

    func windowWillClose(_ notification: Notification) {
        stopRecording(restore: true)
    }

    // MARK: - The page

    func webView(_ webView: WKWebView, didFinish nav: WKNavigation!) {
        loaded = true
        shellLog("settings-window: loaded \(address.absoluteString)")
        sendState()
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation nav: WKNavigation!,
                 withError error: Error) {
        loaded = false
        shellLog("settings-window: could not load \(address.absoluteString) — \(error.localizedDescription)")
    }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        // Only the page this window loaded may ask; one it navigated to by a
        // link is somebody else's.
        guard message.frameInfo.isMainFrame,
              message.frameInfo.request.url?.host == home.host,
              message.frameInfo.request.url?.port == home.port,
              let body = message.body as? [String: Any],
              let kind = body["kind"] as? String else { return }
        switch kind {
        case "state":
            sendState()
        case "record":
            startRecording()
        case "stopRecording":
            stopRecording(restore: true)
        case "changed":
            stopRecording(restore: false)
            suspended = false
            host?.settingsApplyConfig()
            sendState()
        case "chooseApp":
            chooseApp()
        case "reveal":
            reveal(what: body["what"] as? String ?? "config")
        case "hooks":
            // Not wired in this build, and refused rather than half-done: see
            // `hooksReading` for why, and docs/shell-bridge.md for what turning
            // it on would take.
            shellLog("settings-window: the Claude Code hook is not installable in this build")
            sendState()
        case "close":
            window?.performClose(nil)
        default:
            shellLog("settings-window: unknown request \(kind)")
        }
    }

    // MARK: - What only this side knows

    private func sendState() {
        let config = NextConfig.shared
        let display = config.hotKey.isEmpty ? "" : HotKey.display(config.hotKey)
        let ids = config.scopeApp.split(separator: ",")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
        let state: [String: Any] = [
            "hotkey": config.hotKey,
            "display": display,
            "registered": HotKey.shared?.isRegistered ?? false,
            "failed": !config.hotKey.isEmpty && HotKey.shared?.isRegistered != true,
            "scopeApp": config.scopeApp,
            "apps": ids.map(Self.describe),
            "runningApps": Self.runningApps(excluding: Set(ids)),
            "mascots": MascotPacks.available(),
            "fonts": Self.monospacedFamilies(),
            "dictation": Self.dictation(),
            "configPath": Self.tilde(config.fileURL.path),
            "hooks": Self.hooksReading(),
            "platform": "darwin",
        ]
        dispatch("clawdline-settings-state", state)
    }

    /// One application, named and drawn.
    ///
    /// A bundle id whose app is not installed is still described, as
    /// `unresolved`: dropping a line somebody wrote into their own config file
    /// because this machine happened not to resolve it is worse than showing
    /// something plain.
    private static func describe(_ id: String) -> [String: Any] {
        guard let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: id) else {
            return ["id": id, "name": id, "unresolved": true]
        }
        var row: [String: Any] = ["id": id, "name": FileManager.default.displayName(atPath: url.path)]
        if let icon = pngDataURL(NSWorkspace.shared.icon(forFile: url.path)) { row["icon"] = icon }
        return row
    }

    /// What is open right now, which is where the terminal somebody wants
    /// nearly always is.
    private static func runningApps(excluding taken: Set<String>) -> [[String: Any]] {
        var seen = taken
        var rows: [(name: String, row: [String: Any])] = []
        for app in NSWorkspace.shared.runningApplications where app.activationPolicy == .regular {
            guard let id = app.bundleIdentifier, !seen.contains(id) else { continue }
            seen.insert(id)
            let name = app.localizedName ?? id
            var row: [String: Any] = ["id": id, "name": name]
            if let icon = app.icon, let data = pngDataURL(icon) { row["icon"] = data }
            rows.append((name, row))
        }
        rows.sort { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        return rows.map(\.row)
    }

    /// A 16pt icon as a data URL. Small enough that a list of them is a few
    /// kilobytes of JSON, which is cheaper than a route per icon.
    private static func pngDataURL(_ image: NSImage) -> String? {
        let side = 32 // two device pixels per point, for a Retina screen
        let target = NSImage(size: NSSize(width: side, height: side))
        target.lockFocus()
        image.draw(in: NSRect(x: 0, y: 0, width: side, height: side),
                   from: .zero, operation: .sourceOver, fraction: 1)
        target.unlockFocus()
        guard let tiff = target.tiffRepresentation,
              let rep = NSBitmapImageRep(data: tiff),
              let png = rep.representation(using: .png, properties: [:]) else { return nil }
        return "data:image/png;base64," + png.base64EncodedString()
    }

    /// Monospaced faces only. The reading pane draws a terminal's box-drawing
    /// characters, and in a proportional face they do not line up — a setting
    /// you can only get wrong.
    private static func monospacedFamilies() -> [String] {
        NSFontManager.shared.availableFontFamilies.filter { family in
            guard let font = NSFont(name: family, size: 12) else { return false }
            return font.isFixedPitch
        }
    }

    private static func dictation() -> [String: Any] {
        switch DictationEngine.status() {
        case .ready(let model): return ["kind": "ready", "model": model]
        case .noBinary: return ["kind": "noBinary"]
        case .noModel: return ["kind": "noModel"]
        }
    }

    /// Whether this app's own Claude Code hook is wired into `~/.claude/settings.json`.
    ///
    /// **`supported` is false, and that is the decision rather than an
    /// omission.** Installing means writing into another program's settings
    /// file so that Claude Code runs a command at eight moments in every turn;
    /// in the Swift app those commands leave notes that the app reads
    /// milliseconds later, and this build has no note reader, no hook script
    /// and no notes directory. A button that bought nothing and cost somebody
    /// else's config file is not a safe thing to leave switched on.
    ///
    /// The reading half is real, so the row tells the truth rather than
    /// guessing: it looks for commands naming `clawdline-next/hook.sh`, this
    /// app's own path. The Swift app's entries name `clawdline/hook.sh` and are
    /// a different string, so neither app can see or remove the other's — which
    /// is the property that has to hold before either may write that file.
    private static func hooksReading() -> [String: Any] {
        let url = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".claude/settings.json")
        var installed = false
        if let data = try? Data(contentsOf: url),
           let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let hooks = obj["hooks"] as? [String: Any] {
            outer: for (_, value) in hooks {
                for group in (value as? [[String: Any]]) ?? [] {
                    for handler in (group["hooks"] as? [[String: Any]]) ?? [] {
                        if let command = handler["command"] as? String,
                           command.contains("clawdline-next/hook.sh") {
                            installed = true
                            break outer
                        }
                    }
                }
            }
        }
        return [
            "supported": false,
            "installed": installed,
            "heard": false,
            "path": tilde(url.path),
        ]
    }

    private static func tilde(_ path: String) -> String {
        let home = NSHomeDirectory()
        return path.hasPrefix(home) ? "~" + path.dropFirst(home.count) : path
    }

    // MARK: - Recording a combination

    /// The next key pressed with a modifier is the answer, Escape leaves the
    /// setting alone, and a bare letter is swallowed — it is not an answer, and
    /// it is not text either.
    private func startRecording() {
        stopRecording(restore: false)
        host?.settingsSuspendHotKey()
        suspended = true
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
            // and said `changed`; a failed write says `stopRecording`, and so
            // does closing the window.
            self.stopRecording(restore: false)
            self.dispatch("clawdline-settings-hotkey",
                          ["spec": spec, "display": HotKey.display(spec)])
            return nil
        }
        shellLog("settings-window: recording a hotkey")
    }

    /// Stop listening. `restore` puts back the combination the file names; a
    /// page still waiting for an answer is told there will not be one.
    private func stopRecording(restore: Bool) {
        let listening = recorder != nil
        if let monitor = recorder {
            NSEvent.removeMonitor(monitor)
            recorder = nil
        }
        guard restore else { return }
        if suspended {
            suspended = false
            host?.settingsApplyConfig()
        }
        if listening { dispatch("clawdline-settings-hotkey", ["cancelled": true]) }
    }

    // MARK: - Picking an application, and showing a file

    private func chooseApp() {
        guard let window else { return }
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = false
        panel.allowedContentTypes = [.application]
        panel.directoryURL = URL(fileURLWithPath: "/Applications")
        // A sheet rather than a modal run: `runModal` inside a message handler
        // stops the web view's own run loop work, and the window behind it is
        // the page that is waiting for the answer.
        panel.beginSheetModal(for: window) { [weak self] response in
            guard let self else { return }
            guard response == .OK, let url = panel.url,
                  let id = Bundle(url: url)?.bundleIdentifier else {
                self.dispatch("clawdline-settings-app", ["cancelled": true])
                return
            }
            self.dispatch("clawdline-settings-app", ["id": id])
        }
    }

    /// Show a file in the Finder. Selecting it rather than opening it: the
    /// settings file is JSON, and handing somebody a JSON file in whatever the
    /// system thinks edits JSON is a way to hand them a broken config.
    private func reveal(what: String) {
        let url: URL
        switch what {
        case "hooks":
            url = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".claude/settings.json")
        default:
            url = NextConfig.shared.fileURL
        }
        guard FileManager.default.fileExists(atPath: url.path) else {
            NSWorkspace.shared.activateFileViewerSelecting([url.deletingLastPathComponent()])
            return
        }
        NSWorkspace.shared.activateFileViewerSelecting([url])
    }

    // MARK: - Talking to the page

    private func dispatch(_ event: String, _ detail: [String: Any]) {
        guard loaded, let web,
              let data = try? JSONSerialization.data(withJSONObject: detail, options: [.sortedKeys]),
              let json = String(data: data, encoding: .utf8) else { return }
        web.evaluateJavaScript("window.dispatchEvent(new CustomEvent(\(Self.jsString(event)), { detail: \(json) }));") { _, error in
            if let error {
                shellLog("settings-window: could not reach the page: \(error.localizedDescription)")
            }
        }
    }

    private static func jsString(_ s: String) -> String {
        guard let data = try? JSONSerialization.data(withJSONObject: [s]),
              let array = String(data: data, encoding: .utf8) else { return "\"\"" }
        return String(array.dropFirst().dropLast())
    }

    /// Said before the page's first script runs, so the window knows it is in a
    /// shell before it draws anything. The page finds its transport by itself
    /// (`webkit.messageHandlers.shellSettingsWindow`); this only names the
    /// platform, for the one line of the report that says which shell this is.
    private static let announceScript = """
    (function () {
      var shell = window.__clawdlineShell || (window.__clawdlineShell = {});
      shell.settingsWindow = { platform: 'darwin' };
    })();
    """
}
