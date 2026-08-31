import AppKit
import ServiceManagement

final class AppDelegate: NSObject, NSApplicationDelegate {
    private let hotKey = HotKey()
    private var statusItem: NSStatusItem!

    func applicationDidFinishLaunching(_ notification: Notification) {
        // This remains useful from the status item and global hotkey, but it is also an ordinary
        // application somebody can find in the Dock and reopen from Finder.
        NSApp.setActivationPolicy(.regular)

        Log.write("launch: hotkey=\(Config.shared.hotKey) width=\(Config.shared.width) y=\(Config.shared.yFraction)")
        MascotPack.installBundledPacks()
        installMainMenu()
        buildStatusItem()
        // The reading everything else here is a consumer of. Started before the panel exists,
        // because the whole point of it is the stretches when the panel does not.
        NotchIsland.shared.install()
        // Before the watch, so the first reading already has whatever the notes on disk say.
        // A note that arrives later asks for a reading rather than making one of its own — the
        // screen is still what every state comes from, and this only changes when it is read.
        HookBridge.onNote = { SessionWatch.shared.nudge() }
        HookBridge.start()
        SessionWatch.shared.start()
        // After the watch, because its beat rides the watch's observers; the timer inside is
        // what notices a child finishing while nothing on screen moves.
        Orchestrator.start()
        CodexNaming.shared.apply()
        // Reads the config and does nothing at all when it says off, which is what it says
        // until somebody changes it.
        RemoteServer.shared.apply()
        // Cloud keeps the same rhythm as the local server: applied at launch, re-applied on
        // every config change, and told directly by Settings when this Mac is signed in or out
        // so that connecting does not need a relaunch to take effect. It does nothing at all
        // until there is a machine credential in the Keychain, which is what signing in makes.
        // AppKit runs every one of these on the main thread, but the older SDK annotations the
        // plain-swiftc build compiles against do not carry that fact through NSObject — the
        // same gap `Settings.swift` names where it builds `CloudSettingsModel`. Keep the actor
        // boundary explicit rather than making the delegate's AppKit callbacks `@MainActor`.
        MainActor.assumeIsolated {
            CloudSettingsModel.onConnectionChange = { connected in
                if connected {
                    CloudBridgeLifecycle.shared.apply()
                } else {
                    CloudBridgeLifecycle.shared.signedOut()
                }
            }
            CloudBridgeLifecycle.shared.apply()
        }
        // Somebody, somewhere, is asking to pair. The code is shown **here and nowhere else** —
        // it is never in the reply the asker got — so finishing requires being able to see this
        // screen. That is the whole of the security property, and it is why this interrupts
        // rather than sitting in a badge nobody looks at.
        RemoteAuth.onPairingRequest = { [weak self] pending in self?.showPairing(pending) }
        RemoteAuth.onDevicesChanged = { [weak self] in
            // The tunnel is waiting on exactly this, and the menu bar shows whether anything is
            // paired — so both are asked again the moment the answer can have changed.
            RemoteTunnel.shared.apply()
            self?.refreshStatusItem()
        }
        RemoteTunnel.shared.onChange = { [weak self] in self?.refreshStatusItem() }
        RemoteTunnel.shared.apply()
        // Runs whatever somebody put in `on_state_change`, and nothing at all when that is empty
        // — which it is until they do. See Sources/StateHook.swift.
        StateHook.observe()
        DeployWatch.observe()
        NotificationCenter.default.addObserver(self, selector: #selector(configChanged),
                                               name: .clawdlineConfigChanged, object: nil)

        hotKey.onFire = {
            Log.write("hotkey fired")
            PromptController.shared.toggle()
        }
        // Recompute whether the hotkey should be attached whenever the frontmost app changes
        NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didActivateApplicationNotification, object: nil, queue: .main
        ) { [weak self] note in
            self?.updateHotKeyScope()
            let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
            PromptController.shared.appBecameFrontmost(app?.bundleIdentifier)
        }

        if applyHotKey() {
            Log.write("hotkey registered: \(HotKey.display(Config.shared.hotKey))"
                + (Config.shared.scopeApp.isEmpty ? " (global)" : " (only in \(Config.shared.scopeApp))"))
            updateHotKeyScope()
        } else {
            Log.write("hotkey registration failed: \(Config.shared.hotKey)")
            // A failed registration almost always means something else owns the combination. Say so —
            // otherwise the user presses it for a while and concludes the app never started.
            let a = NSAlert()
            a.messageText = L.t.hotkeyFailedTitle(HotKey.display(Config.shared.hotKey))
            a.informativeText = L.t.hotkeyFailedBody(Config.shared.fileURL.path)
            a.alertStyle = .warning
            a.runModal()
        }

        // A first launch has a visible destination. Once the versioned introduction is complete,
        // background launch-at-login stays quiet until Home, Dock/Finder reopen, or the status
        // item asks for it.
        if AppLaunchPolicy.shouldShowHome(
            onboardingComplete: OnboardingCompletionStore.production.isCurrent
        ) {
            DispatchQueue.main.async { HomeWindow.shared.show() }
        }
    }

    func applicationShouldHandleReopen(_ sender: NSApplication,
                                       hasVisibleWindows flag: Bool) -> Bool {
        if !HomeReopenPolicy.shouldShowHome(hasVisibleWindows: flag) {
            NSApp.activate(ignoringOtherApps: true)
            return false
        }
        HomeWindow.shared.show()
        return true
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        false
    }

    /// Bring the machine forward and say the code out loud.
    ///
    /// Deliberately blunt. A request to pair is a request for somebody to be able to read every
    /// repository name and task title on this Mac, and the only person who should be able to
    /// grant it is the one who can read this window.
    private var pairingShowing = false

    private func showPairing(_ pending: RemoteAuth.Pending) {
        // One at a time. `runModal` blocks the main thread, so a second request arriving while the
        // first is on screen would queue another window behind it — and something hammering the
        // pairing route would stack a wall of them the user has to dismiss one by one. That is a
        // denial of service against the machine, delivered through the one route that has to stay
        // open. The rate limit in RemoteServer is the other half of this.
        guard !pairingShowing else {
            Log.write("pairing: another request while one was on screen — ignored")
            return
        }
        pairingShowing = true
        defer { pairingShowing = false }
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert()
        alert.messageText = L.t.pairingAsks(pending.name)
        alert.informativeText = L.t.pairingCode(pending.code)
        alert.alertStyle = .informational
        alert.addButton(withTitle: L.t.pairingIgnore)
        alert.runModal()
    }

    // MARK: - Where the hotkey applies

    private var hotKeyActive = false

    @discardableResult
    private func applyHotKey() -> Bool {
        hotKeyActive = hotKey.register(Config.shared.hotKey)
        return hotKeyActive
    }

    /// Carbon hotkeys are global; there is no "only in this app" option.
    /// So attach it when iTerm2 is frontmost (or the panel is open) and detach it when it is not.
    /// Same result, and ⌥Space stays itself in every other app instead of being swallowed here.
    private func updateHotKeyScope() {
        let scope = Config.shared.scopeApp
        guard !scope.isEmpty else {
            if !hotKeyActive { applyHotKey() }
            return
        }
        let front = NSWorkspace.shared.frontmostApplication?.bundleIdentifier ?? ""
        let mine = Bundle.main.bundleIdentifier ?? ""
        let allowed = scope.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
        let want = allowed.contains(front) || front == mine || PromptController.shared.isVisible

        if want, !hotKeyActive {
            applyHotKey()
            Log.write("hotkey attached (frontmost: \(front))")
        } else if !want, hotKeyActive {
            hotKey.unregister()
            hotKeyActive = false
            Log.write("hotkey detached (frontmost: \(front))")
        }
    }

    /// The normal App, Window, and Help menus are part of being a visible Mac application. Keep
    /// their conventional key equivalents even though the status item remains available.
    private func installMainMenu() {
        let main = NSMenu()
        let appItem = NSMenuItem(title: L.t.menuApplication, action: nil, keyEquivalent: "")
        let appMenu = NSMenu()

        appMenu.addItem(NSMenuItem(
            title: L.t.menuAbout(L.t.menuApplication),
            action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)),
            keyEquivalent: ""))
        appMenu.addItem(.separator())

        let home = NSMenuItem(title: L.t.menuHome, action: #selector(showHome), keyEquivalent: "h")
        home.keyEquivalentModifierMask = [.command, .shift]
        home.target = self
        appMenu.addItem(home)
        appMenu.addItem(.separator())

        let settings = NSMenuItem(title: L.t.menuEditConfig, action: #selector(editConfig),
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

        let windowItem = NSMenuItem(title: L.t.menuWindow, action: nil, keyEquivalent: "")
        let windowMenu = NSMenu()
        windowMenu.addItem(NSMenuItem(title: L.t.menuClose, action: #selector(NSWindow.performClose(_:)),
                                      keyEquivalent: "w"))
        windowMenu.addItem(NSMenuItem(title: L.t.menuMinimize,
                                      action: #selector(NSWindow.performMiniaturize(_:)),
                                      keyEquivalent: "m"))
        windowItem.submenu = windowMenu
        main.addItem(windowItem)
        NSApp.windowsMenu = windowMenu

        let helpItem = NSMenuItem(title: L.t.menuHelp, action: nil, keyEquivalent: "")
        let helpMenu = NSMenu()
        let documentation = NSMenuItem(title: L.t.menuHelpDocumentation,
                                       action: #selector(openDocumentation), keyEquivalent: "?")
        documentation.target = self
        helpMenu.addItem(documentation)
        helpItem.submenu = helpMenu
        main.addItem(helpItem)
        NSApp.helpMenu = helpMenu

        NSApp.mainMenu = main
    }

    private func buildStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem.button?.title = "✳"
        statusItem.button?.font = NSFont.systemFont(ofSize: 13, weight: .medium)
        statusItem.menu = buildMenu()
        SessionWatch.shared.observers["menubar"] = { [weak self] in self?.refreshStatusItem() }
    }

    /// The one piece of screen this app owns all day, finally carrying something.
    ///
    /// It was a fixed character that opened a menu — permanently visible and permanently saying
    /// nothing. Everything the bar knows it knew only while it was on screen, which is exactly
    /// the wrong way round for the one question worth being told without asking: *is something
    /// waiting for me right now.*
    ///
    /// Quiet by construction. Nothing running and it is the character it always was; things
    /// running and it carries a count; something waiting for an answer and it says so, in the
    /// accent, because that is the only state that costs you anything for going unnoticed.
    private func refreshStatusItem() {
        guard let button = statusItem?.button else { return }
        let watch = SessionWatch.shared
        let waiting = watch.waiting
        let working = watch.working

        let title = NSMutableAttributedString(string: "✳", attributes: [
            .font: NSFont.systemFont(ofSize: 13, weight: .medium),
            .foregroundColor: waiting.isEmpty ? NSColor.labelColor : Style.accent,
        ])
        if !waiting.isEmpty {
            title.append(NSAttributedString(string: " ●", attributes: [
                .font: NSFont.systemFont(ofSize: 9, weight: .bold),
                .foregroundColor: Style.accent,
            ]))
        } else if working.count > 1 {
            title.append(NSAttributedString(string: " \(working.count)", attributes: [
                .font: NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .medium),
                .foregroundColor: NSColor.secondaryLabelColor,
            ]))
        }
        button.attributedTitle = title
        button.toolTip = waiting.isEmpty
            ? (working.isEmpty ? nil : L.t.statusWorking(working.count))
            : L.t.statusWaiting(waiting.map(\.displayLabel))
    }

    private func buildMenu() -> NSMenu {
        let menu = NSMenu()
        menu.delegate = self

        let open = NSMenuItem(title: L.t.menuOpen, action: #selector(openPanel), keyEquivalent: "")
        open.target = self
        menu.addItem(open)

        let reveal = NSMenuItem(title: L.t.menuReveal, action: #selector(revealTarget), keyEquivalent: "")
        reveal.target = self
        menu.addItem(reveal)

        let home = NSMenuItem(title: L.t.menuHome, action: #selector(showHome), keyEquivalent: "")
        home.target = self
        menu.addItem(home)

        // Half-installed is the state worth naming, and it is invisible everywhere else: the
        // microphone works either way, so "it seems not to be on" is all you can tell from
        // using it. Clicking opens the page that says what to do about it.
        let status = Whisper.status(binary: Config.shared.whisperBinary,
                                    model: Config.shared.whisperModel)
        let dictation = NSMenuItem(title: L.t.dictationStatus(status),
                                   action: status == .ready(model: "") ? nil : #selector(openWhisperDocs),
                                   keyEquivalent: "")
        if case .ready = status {} else { dictation.target = self }
        menu.addItem(dictation)

        // Only when an assistant in front of us is older than the one this was built against.
        // Newer is the normal state — they update themselves and this does not — and a line that
        // is there every week is one nobody reads on the week it matters. Usually none of them,
        // occasionally one, and both only on a machine that is behind twice. See Compat.swift.
        for note in Compat.notes() {
            let item = NSMenuItem(title: note, action: #selector(openCompatibility),
                                  keyEquivalent: "")
            item.target = self
            menu.addItem(item)
        }

        // Browsing beats editing a config file: the submenu is how you find out what you have.
        let mascot = NSMenuItem(title: L.t.menuMascot, action: nil, keyEquivalent: "")
        mascot.tag = 200
        mascot.submenu = buildMascotMenu()
        menu.addItem(mascot)

        menu.addItem(.separator())

        let login = NSMenuItem(title: L.t.menuLogin, action: #selector(toggleLogin), keyEquivalent: "")
        login.target = self
        login.tag = 100
        menu.addItem(login)

        let edit = NSMenuItem(title: L.t.menuEditConfig, action: #selector(editConfig), keyEquivalent: "")
        edit.target = self
        menu.addItem(edit)

        let reload = NSMenuItem(title: L.t.menuReload, action: #selector(reloadConfig), keyEquivalent: "")
        reload.target = self
        menu.addItem(reload)

        menu.addItem(.separator())

        let quit = NSMenuItem(title: L.t.menuQuit, action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        menu.addItem(quit)
        return menu
    }

    /// clawdline://open — so any tool (a hotkey utility, Stream Deck, Shortcuts, a shell script)
    /// can summon it, instead of being locked to the one built-in combination.
    func application(_ application: NSApplication, open urls: [URL]) {
        Log.write("url: \(urls.map { $0.absoluteString }.joined(separator: " "))")
        guard let url = urls.first else { return }
        switch url.host ?? "" {
        case "toggle":
            PromptController.shared.toggle()
        case "push":
            // `clawdline://push?test=1` — send one, to everything subscribed.
            //
            // A notification feature you cannot fire on purpose is one nobody trusts: the only
            // other way to see it is to make a session ask you a question and wait, which is a
            // long way to go to find out whether a key was minted correctly. It says what it is
            // in the notification itself, so a test that arrives is never mistaken for a session
            // that needs you.
            WebPush.send(title: "Clawdline", body: L.t.pushTest, url: "/", tag: "test")
            Log.write("push: test sent to \(WebPush.subscriptions.count) subscription(s)")
        case "settings":
            SettingsWindow.shared.show()
        case "home", "setup":
            HomeWindow.shared.show()
        case "hooks":
            // `?install=1` and `?install=0`, so the wiring can be put in and taken out by a
            // script — a setup step somebody runs once on a new machine, or a line in an
            // uninstaller. The window has the same two buttons; neither is the real interface.
            let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            let want = items.first(where: { $0.name == "install" })?.value ?? "1"
            let problem = want == "0" ? HookBridge.uninstall() : HookBridge.install()
            Log.write("hooks: \(want == "0" ? "uninstall" : "install") — \(problem ?? "ok")")
        case "send":
            let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            let text = items.first(where: { $0.name == "text" })?.value ?? ""
            PromptController.shared.sendDirect(text, target: items.first(where: { $0.name == "target" })?.value)
        case "snapshot":
            let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            let path = items.first(where: { $0.name == "path" })?.value ?? ""
            // The island paints itself rather than the card, so it forks here rather than
            // growing another argument onto a call that has plenty.
            // Parsed before the fork, because the island wants it too: resting is a five-second
            // breath and one frame from the middle of it says nothing about the swing.
            let t = items.first(where: { $0.name == "t" })?.value.flatMap(Double.init)
            if let island = items.first(where: { $0.name == "island" })?.value, !path.isEmpty {
                NotchIsland.shared.snapshot(to: path, mode: island, at: t)
                return
            }
            let routine = items.first(where: { $0.name == "routine" })?.value
            let list = items.first(where: { $0.name == "list" })?.value
            let out = items.first(where: { $0.name == "output" })?.value == "1"
            let session = items.first(where: { $0.name == "session" })?.value
            let full = items.first(where: { $0.name == "full" })?.value.map { $0 == "1" }
            if !path.isEmpty {
                PromptController.shared.snapshot(
                    to: path, routine: routine, at: t, list: list, output: out,
                    session: session, full: full,
                    transcript: items.first(where: { $0.name == "transcript" })?.value,
                    // `agent=<part of what it was asked to do>` opens one of the session's
                    // background agents in the pane before the shot. Same reason `session=`
                    // exists: the second room in that pane is otherwise reachable only by
                    // clicking, which means it can never appear in a picture.
                    agent: items.first(where: { $0.name == "agent" })?.value)
            }
        case "filmstrip":
            let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            func q(_ n: String) -> String? { items.first(where: { $0.name == n })?.value }
            guard let dir = q("dir"), !dir.isEmpty else { return }
            // The island paints itself rather than the card, so it forks here for the same
            // reason `snapshot` does one case up: its storyboard is four states of a shape in
            // the menu bar and has nothing in common with the card's beyond writing frames.
            if q("island") != nil {
                NotchIsland.shared.filmstrip(
                    dir: dir,
                    fps: q("fps").flatMap(Double.init) ?? 12,
                    seconds: q("seconds").flatMap(Double.init) ?? 8.6)
                return
            }
            PromptController.shared.filmstrip(
                dir: dir,
                fps: q("fps").flatMap(Double.init) ?? 24,
                seconds: q("seconds").flatMap(Double.init) ?? 4.4,
                script: q("script") ?? "demo",
                text: q("text") ?? "add retry with backoff to the upload handler")
        default:
            PromptController.shared.show()
        }
    }

    private func buildMascotMenu() -> NSMenu {
        let sub = NSMenu()
        for name in PromptController.shared.mascotNamesForMenu {
            let item = NSMenuItem(title: name, action: #selector(chooseMascot(_:)), keyEquivalent: "")
            item.target = self
            item.representedObject = name
            item.state = (name == Config.shared.mascot) ? .on : .off
            sub.addItem(item)
        }
        if sub.items.isEmpty {
            sub.addItem(NSMenuItem(title: MascotPack.userDirectory.path, action: nil, keyEquivalent: ""))
        }
        return sub
    }

    @objc private func chooseMascot(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String else { return }
        PromptController.shared.selectMascot(named: name)
        statusItem.menu = buildMenu()
    }

    @objc private func openPanel() { PromptController.shared.show() }
    @objc private func revealTarget() { PromptController.shared.revealCurrentTarget() }
    @objc private func showHome() { HomeWindow.shared.show() }

    @objc private func toggleLogin() {
        let svc = SMAppService.mainApp
        do {
            if svc.status == .enabled { try svc.unregister() } else { try svc.register() }
        } catch {
            let a = NSAlert()
            a.messageText = L.t.loginFailed
            a.informativeText = error.localizedDescription
            a.runModal()
        }
    }

    @objc private func editConfig() {
        SettingsWindow.shared.show()
    }

    /// Re-apply everything a changed setting can change.
    ///
    /// The same work "Reload config" does, minus the reading from disk — the settings window has
    /// already written what it changed, and re-reading its own file back would only add a way for
    /// the two to disagree. Both routes end here, so a control and a hand edit cannot drift apart.
    @objc private func configChanged() {
        L.reload()
        installMainMenu()
        PromptController.shared.reloadMascot()
        PromptController.shared.applyCardOpacity()
        applyHotKey()
        updateHotKeyScope()
        statusItem.menu = buildMenu()
        NotchIsland.shared.install()
        RemoteServer.shared.apply()
        RemoteTunnel.shared.apply()
        MainActor.assumeIsolated { CloudBridgeLifecycle.shared.apply() }
        HomeWindow.shared.rebuildIfVisible()
        CodexNaming.shared.apply()
        refreshStatusItem()
    }

    @objc private func openWhisperDocs() {
        NSWorkspace.shared.open(URL(string: "https://github.com/sainteye/clawdline/blob/main/docs/whisper.md")!)
    }

    @objc private func openCompatibility() {
        NSWorkspace.shared.open(URL(string: "https://github.com/sainteye/clawdline/blob/main/docs/compatibility.md")!)
    }

    @objc private func openDocumentation() {
        NSWorkspace.shared.open(URL(string: "https://github.com/sainteye/clawdline#readme")!)
    }

    @objc private func reloadConfig() {
        Config.shared.load()
        L.reload()
        installMainMenu()
        PromptController.shared.reloadMascot()
        PromptController.shared.applyCardOpacity()
        if !applyHotKey() {
            let a = NSAlert()
            a.messageText = L.t.hotkeyFailedTitle(HotKey.display(Config.shared.hotKey))
            a.informativeText = L.t.hotkeyFailedBody(Config.shared.fileURL.path)
            a.runModal()
        }
        updateHotKeyScope()
        statusItem.menu = buildMenu()
        // `"notch": false` has to take effect on a reload rather than on a relaunch: the whole
        // reason it exists is that somebody may want it gone the moment it annoys them.
        NotchIsland.shared.install()
        RemoteServer.shared.apply()
        RemoteTunnel.shared.apply()
        MainActor.assumeIsolated { CloudBridgeLifecycle.shared.apply() }
        HomeWindow.shared.rebuildIfVisible()
        CodexNaming.shared.apply()
        refreshStatusItem()
    }
}

extension AppDelegate: NSMenuDelegate {
    func menuWillOpen(_ menu: NSMenu) {
        menu.item(at: 0)?.title = "\(L.t.menuOpen)   \(HotKey.display(Config.shared.hotKey))"
        menu.item(at: 1)?.title = "\(L.t.menuReveal)   \(PromptController.shared.targetSummary)"
        menu.item(withTag: 200)?.submenu = buildMascotMenu()
        menu.item(withTag: 100)?.state = (SMAppService.mainApp.status == .enabled) ? .on : .off
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
