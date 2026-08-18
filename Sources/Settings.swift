import AppKit

/// The settings, as controls rather than as a file.
///
/// "Edit config…" opened `config.json` in whatever the system thinks edits JSON, which is a fine
/// answer for the person who wrote the file format and a poor one for everybody else: you have to
/// know a key exists to change it, know what it accepts, and not put a comma in the wrong place.
/// The file is still the truth and still hand-editable — there is a button here that opens it —
/// but the settings somebody actually changes now have a control each.
///
/// Built by hand in AppKit for the same reason the rest of this is: `swiftc` and nothing else. A
/// form is a column of rows, and a column of rows does not need a storyboard.
///
/// **Every change applies immediately.** There is no OK button, because there is nothing to
/// cancel: this writes what the config file would have said, and the app picks it up the same way
/// it would have picked up an edit — which is also why the long tail of settings that live only
/// in the file keep working exactly as they did.
final class SettingsWindow: NSObject, NSWindowDelegate {

    static let shared = SettingsWindow()
    private override init() { super.init() }

    private var window: NSWindow?
    private var recording: NSButton?
    private var monitor: Any?
    private var hooksButton: NSButton?
    private var hooksStatus: NSTextField?

    func show() {
        if window == nil { window = build() }
        NSApp.activate(ignoringOtherApps: true)
        window?.center()
        window?.makeKeyAndOrderFront(nil)
    }

    func windowWillClose(_ notification: Notification) {
        stopRecording()
        // Kept rather than rebuilt: a window that forgets its scroll position every time it is
        // opened is a window that is annoying to open twice.
    }

    // MARK: - Building it

    private func build() -> NSWindow {
        let form = Form(width: 560)

        form.section(L.t.settingsGeneral)
        form.row(L.t.settingsHotkey, hotkeyButton())
        form.row(L.t.settingsScope, scopeControl(), hint: L.t.settingsScopeHint)
        form.row(L.t.settingsLanguage, languagePopUp())
        form.row(L.t.menuMascot, mascotPopUp())
        form.check(L.t.settingsReopen, get: { Config.shared.reopenOnReturn },
                   set: { Config.shared.reopenOnReturn = $0 })
        form.check(L.t.settingsFollow, get: { Config.shared.followTarget },
                   set: { Config.shared.followTarget = $0 })
        form.check(L.t.settingsNotch, get: { Config.shared.notch },
                   set: { Config.shared.notch = $0 }, hint: L.t.settingsNotchHint)

        form.section(L.t.settingsBar)
        form.slider(L.t.settingsPosition, 0.05...0.80,
                    get: { Double(Config.shared.yFraction) },
                    set: { Config.shared.yFraction = CGFloat($0) },
                    format: { "\(Int($0 * 100))%" })
        form.slider(L.t.settingsWidth, 360...1400,
                    get: { Double(Config.shared.width) },
                    set: { Config.shared.width = CGFloat($0) },
                    format: { "\(Int($0)) pt" })
        form.slider(L.t.settingsOpacity, 0...1,
                    get: { Config.shared.cardOpacity }, set: { Config.shared.cardOpacity = $0 },
                    format: { "\(Int($0 * 100))%" })
        form.check(L.t.settingsImagesPaste, get: { Config.shared.sendImagesAsPaste },
                   set: { Config.shared.sendImagesAsPaste = $0 })

        form.section(L.t.settingsReading)
        form.row(L.t.settingsShow, outputModePopUp())
        form.slider(L.t.settingsPaneHeight, 80...900,
                    get: { Double(Config.shared.outputHeight) },
                    set: { Config.shared.outputHeight = CGFloat($0) },
                    format: { "\(Int($0)) pt" })
        form.slider(L.t.settingsTextSize, 8...28,
                    get: { Double(Config.shared.outputSize) },
                    set: { Config.shared.outputSize = CGFloat($0) },
                    format: { String(format: "%.1f pt", $0) })
        form.row(L.t.settingsPaneFont, fontPopUp())
        form.slider(L.t.settingsBlur, 0...1,
                    get: { Config.shared.backdropStrength },
                    set: { Config.shared.backdropStrength = $0 },
                    format: { "\(Int($0 * 100))%" })
        form.check(L.t.settingsNewestFirst, get: { Config.shared.outputNewestFirst },
                   set: { Config.shared.outputNewestFirst = $0 })

        form.section(L.t.settingsVoice)
        form.row(L.t.settingsEngine, enginePopUp(), hint: L.t.dictationStatus(
            Whisper.status(binary: Config.shared.whisperBinary, model: Config.shared.whisperModel)))
        form.slider(L.t.settingsSettle, 0...8,
                    get: { Config.shared.voiceSettleSeconds },
                    set: { Config.shared.voiceSettleSeconds = $0 },
                    format: { $0 == 0 ? L.t.settingsOff : L.t.settingsSeconds($0) })
        form.slider(L.t.settingsStop, 0...30,
                    get: { Config.shared.voiceStopSeconds },
                    set: { Config.shared.voiceStopSeconds = $0 },
                    format: { $0 == 0 ? L.t.settingsOff : L.t.settingsSeconds($0) })

        form.section(L.t.settingsRemote)
        form.check(L.t.settingsRemoteServe, get: { Config.shared.remote },
                   set: { Config.shared.remote = $0 }, hint: L.t.settingsRemoteHint)
        form.check(L.t.settingsRemoteWrite, get: { Config.shared.remoteWrite },
                   set: { Config.shared.remoteWrite = $0 }, hint: L.t.settingsRemoteWriteHint)
        form.row(L.t.settingsRemoteDevices, devicesControl())
        form.row(L.t.settingsTunnel, tunnelPopUp(), hint: L.t.settingsTunnelHint)
        form.row(L.t.settingsTunnelHostname, hostnameField())
        form.hint(tunnelStatusText())

        form.section(L.t.settingsHooks)
        form.row("", hooksControl(), hint: L.t.settingsHooksHint)

        form.footer(L.t.settingsOpenFile) {
            let url = Config.shared.fileURL
            if !FileManager.default.fileExists(atPath: url.path) { Config.shared.save() }
            NSWorkspace.shared.open(url)
        }
        form.finish()

        let scroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: 560, height: min(660, form.height)))
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.documentView = form
        scroll.autohidesScrollers = true

        let w = NSWindow(contentRect: scroll.frame,
                         styleMask: [.titled, .closable, .miniaturizable],
                         backing: .buffered, defer: false)
        w.title = L.t.settingsTitle
        w.contentView = scroll
        w.isReleasedWhenClosed = false
        w.delegate = self
        return w
    }

    // MARK: - Remote

    private var devicesLabel: NSTextField?

    /// What can currently reach this Mac, and one button that stops all of it.
    ///
    /// The list is here rather than tucked away because it is the answer to a question somebody
    /// only ever asks in a hurry — *what is connected to my machine right now* — and the button
    /// next to it exists for the same moment. Nothing about that moment is improved by a
    /// confirmation sheet, so there is not one: revoking is instant and re-pairing is cheap.
    private func remoteDevicesControl() -> NSView { devicesControl() }

    private func devicesControl() -> NSView {
        let width: CGFloat = 560 - (22 + 168 + 14) - 22
        let box = NSView(frame: NSRect(x: 0, y: 0, width: width, height: 52))

        let label = NSTextField(wrappingLabelWithString: "")
        label.font = NSFont.systemFont(ofSize: 11)
        label.textColor = .secondaryLabelColor
        label.frame = NSRect(x: 0, y: 26, width: width, height: 22)
        box.addSubview(label)
        devicesLabel = label

        let open = NSButton(title: L.t.settingsRemoteOpen, target: self, action: #selector(openRemote))
        open.bezelStyle = .rounded
        open.frame = NSRect(x: 0, y: 0, width: 150, height: 24)
        box.addSubview(open)

        let revoke = NSButton(title: L.t.settingsRemoteRevokeAll, target: self,
                              action: #selector(revokeAllDevices))
        revoke.bezelStyle = .rounded
        revoke.frame = NSRect(x: 158, y: 0, width: 190, height: 24)
        box.addSubview(revoke)

        refreshDevices()
        return box
    }

    private func refreshDevices() {
        let devices = RemoteAuth.approvedDevices
        devicesLabel?.stringValue = devices.isEmpty
            ? L.t.settingsRemoteNoDevices
            : devices.map(\.name).joined(separator: " · ")
    }

    @objc private func revokeAllDevices() {
        RemoteAuth.revokeAll()
        refreshDevices()
    }

    private var tunnelStatus: NSTextField?

    private func tunnelPopUp() -> NSView {
        popUp([(L.t.settingsOff, "off"),
               (L.t.settingsTunnelQuick, "quick"),
               (L.t.settingsTunnelNamed, "named")],
              current: Config.shared.remoteTunnel) { Config.shared.remoteTunnel = $0 }
    }

    private func hostnameField() -> NSView {
        let field = NSTextField(string: Config.shared.remoteHostname)
        field.placeholderString = "clawd.example.com"
        field.frame = NSRect(x: 0, y: 0, width: 300, height: 22)
        field.font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
        field.target = self
        field.action = #selector(hostnameEdited(_:))
        return field
    }

    @objc private func hostnameEdited(_ sender: NSTextField) {
        Config.shared.remoteHostname = sender.stringValue.trimmingCharacters(in: .whitespaces)
        apply()
    }

    /// What the tunnel is doing, in the words the tunnel itself used.
    ///
    /// Its failures are sentences meant for a person — "pair a device first", "the local server
    /// is off" — and passing them through unchanged is better than translating them into a
    /// generic "could not start": the useful part of each one is the specific thing to go and do.
    private func tunnelStatusText() -> String {
        switch RemoteTunnel.shared.state {
        case .off:              return RemoteTunnel.isInstalled ? "" : "cloudflared is not installed."
        case .starting:         return "…"
        case .up(let url):      return url
        case .failed(let why):  return why
        }
    }

    /// Open the interface with a key already in it.
    ///
    /// The page needs a token and there is nowhere sensible for somebody to type one, so pressing
    /// this mints a device of its own and hands it over in the URL's fragment. A fragment is the
    /// right place: browsers do not send it to servers and do not keep it in history the way a
    /// query string ends up in logs, and the page trades it for a cookie and clears it.
    ///
    /// A device of its own rather than the machine's, because they are not the same thing — the
    /// local token can send and administer, and a browser tab should start with neither until
    /// somebody says so.
    @objc private func openRemote() {
        guard Config.shared.remote else { return }
        let caps: Set<RemoteAuth.Capability> = Config.shared.remoteWrite ? [.read, .send] : [.read]
        let made = RemoteAuth.addDevice(name: "Browser on this Mac", caps: caps)
        guard let url = URL(string:
            "http://127.0.0.1:\(Config.shared.remotePort)/#t=\(made.token)") else { return }
        NSWorkspace.shared.open(url)
        refreshDevices()
    }

    // MARK: - Claude Code hooks

    /// A button and a sentence saying what it did.
    ///
    /// The sentence is the point. This writes into `~/.claude/settings.json`, which is somebody
    /// else's file and often somebody else's business — a team may manage it, a plugin may
    /// rewrite it — so "the button says installed" is not a claim worth making on its own.
    /// Whether a session has ever actually reported is the only evidence that the wiring works,
    /// and it costs nothing to look: it is the newest note on disk.
    private func hooksControl() -> NSView {
        let width: CGFloat = 560 - (22 + 168 + 14) - 22
        let box = NSView(frame: NSRect(x: 0, y: 0, width: width, height: 24))

        let b = NSButton(title: "", target: self, action: #selector(toggleHooks))
        b.bezelStyle = .rounded
        b.frame = NSRect(x: 0, y: 0, width: 104, height: 24)
        box.addSubview(b)
        hooksButton = b

        let status = NSTextField(labelWithString: "")
        status.font = NSFont.systemFont(ofSize: 11)
        status.textColor = .secondaryLabelColor
        status.lineBreakMode = .byTruncatingTail
        status.frame = NSRect(x: 114, y: 4, width: width - 114, height: 16)
        box.addSubview(status)
        hooksStatus = status

        refreshHooksControl()
        return box
    }

    private func refreshHooksControl() {
        let installed = HookBridge.isInstalled
        hooksButton?.title = installed ? L.t.settingsHooksRemove : L.t.settingsHooksInstall
        // Heard from within the day. Longer and this would go on saying "reporting" about a
        // machine that has not run Claude Code since last week, which is the reassurance the
        // line exists to withhold.
        let heard = HookBridge.lastHeard.map { Date().timeIntervalSince($0) < 86_400 } ?? false
        hooksStatus?.stringValue = !installed ? L.t.settingsHooksOff
            : (heard ? L.t.settingsHooksLive : L.t.settingsHooksOn)
    }

    @objc private func toggleHooks() {
        let problem = HookBridge.isInstalled ? HookBridge.uninstall() : HookBridge.install()
        refreshHooksControl()
        guard let problem else { return }
        let a = NSAlert()
        a.messageText = L.t.settingsHooks
        a.informativeText = problem
        a.alertStyle = .warning
        a.runModal()
    }

    // MARK: - The controls that are not a checkbox or a slider

    /// A button that listens for one key combination and writes it down.
    ///
    /// Not a text field. "option+space" is a thing you know how to spell only if you have read the
    /// parser, and the failure mode of typing it wrong is a hotkey that silently does not
    /// register. Pressing the keys you mean cannot be spelled wrong.
    private func hotkeyButton() -> NSView {
        let b = NSButton(title: HotKey.display(Config.shared.hotKey), target: self,
                         action: #selector(startRecording(_:)))
        b.bezelStyle = .rounded
        b.setButtonType(.momentaryPushIn)
        b.frame.size = NSSize(width: 150, height: 24)
        return b
    }

    @objc private func startRecording(_ sender: NSButton) {
        stopRecording()
        recording = sender
        sender.title = L.t.settingsRecording
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
        recording?.title = HotKey.display(Config.shared.hotKey)
        recording = nil
    }

    /// Where the hotkey fires. A checkbox for "everywhere", and the bundle ids underneath it for
    /// the ordinary case of one or two terminals.
    private func scopeControl() -> NSView {
        let box = NSView(frame: NSRect(x: 0, y: 0, width: 340, height: 52))
        let global = NSButton(checkboxWithTitle: L.t.settingsScopeGlobal, target: self,
                              action: #selector(toggleGlobalScope(_:)))
        global.state = Config.shared.scopeApp.isEmpty ? .on : .off
        global.frame = NSRect(x: 0, y: 30, width: 340, height: 18)
        box.addSubview(global)

        let field = NSTextField(string: Config.shared.scopeApp)
        field.frame = NSRect(x: 0, y: 0, width: 340, height: 22)
        field.font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
        field.isEnabled = !Config.shared.scopeApp.isEmpty
        field.target = self
        field.action = #selector(scopeEdited(_:))
        field.tag = 77
        box.addSubview(field)
        return box
    }

    @objc private func toggleGlobalScope(_ sender: NSButton) {
        let field = sender.superview?.subviews.first { $0.tag == 77 } as? NSTextField
        if sender.state == .on {
            Config.shared.scopeApp = ""
            field?.isEnabled = false
        } else {
            // Back to what it was, or to the one terminal this is known to work with, rather than
            // to an empty box that silently means "everywhere" — the opposite of what was clicked.
            let restored = field?.stringValue.isEmpty == false
                ? field!.stringValue : "com.googlecode.iterm2"
            Config.shared.scopeApp = restored
            field?.stringValue = restored
            field?.isEnabled = true
        }
        apply()
    }

    @objc private func scopeEdited(_ sender: NSTextField) {
        Config.shared.scopeApp = sender.stringValue.trimmingCharacters(in: .whitespaces)
        apply()
    }

    private func languagePopUp() -> NSView {
        var options: [(String, String)] = [(L.t.settingsAuto, "auto")]
        // The tags the catalog actually resolves, each shown in its own language, because a list
        // of languages written in a language you do not read is not a list you can pick from.
        for tag in ["en", "zh-Hant", "zh-Hans", "ja", "ko", "es", "pt", "fr", "de", "ru", "it",
                    "hi", "id", "tr"] {
            let name = Locale(identifier: tag).localizedString(forIdentifier: tag) ?? tag
            options.append((name.prefix(1).uppercased() + name.dropFirst(), tag))
        }
        return popUp(options, current: Config.shared.language) { Config.shared.language = $0 }
    }

    private func mascotPopUp() -> NSView {
        let names = MascotPack.available()
        return popUp(names.map { ($0, $0) }, current: Config.shared.mascot) {
            Config.shared.mascot = $0
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

    private var popUpActions: [ObjectIdentifier: (String) -> Void] = [:]
    private var popUpValues: [ObjectIdentifier: [String]] = [:]

    private func popUp(_ options: [(label: String, value: String)], current: String,
                       _ set: @escaping (String) -> Void) -> NSPopUpButton {
        let p = NSPopUpButton(frame: NSRect(x: 0, y: 0, width: 220, height: 26))
        p.addItems(withTitles: options.map(\.label))
        if let i = options.firstIndex(where: { $0.value == current }) { p.selectItem(at: i) }
        p.target = self
        p.action = #selector(popUpChanged(_:))
        popUpActions[ObjectIdentifier(p)] = set
        popUpValues[ObjectIdentifier(p)] = options.map(\.value)
        return p
    }

    @objc private func popUpChanged(_ sender: NSPopUpButton) {
        let key = ObjectIdentifier(sender)
        guard let values = popUpValues[key], values.indices.contains(sender.indexOfSelectedItem)
        else { return }
        popUpActions[key]?(values[sender.indexOfSelectedItem])
        apply()
    }

    /// Save, and tell the app. Same path an edit to the file takes, so nothing here can drift
    /// away from what hand-editing does.
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

// MARK: - The form

/// A column of rows, laid out top-down as they are added.
///
/// Flipped, so "add a row" means "move down", which is how the code reads and therefore how it
/// stays correct. No auto layout: every row is one line tall and the width is fixed, so
/// constraints would be a solver asked to confirm arithmetic.
private final class Form: NSView {

    private let labelW: CGFloat = 168
    private let pad: CGFloat = 22
    private var y: CGFloat = 20
    private var controlX: CGFloat { pad + labelW + 14 }

    init(width: CGFloat) {
        super.init(frame: NSRect(x: 0, y: 0, width: width, height: 10))
    }
    required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }
    var height: CGFloat { y }

    func section(_ title: String) {
        if y > 20 { y += 16 }
        let label = NSTextField(labelWithString: title.uppercased())
        label.font = NSFont.systemFont(ofSize: 10, weight: .semibold)
        label.textColor = .tertiaryLabelColor
        label.frame = NSRect(x: pad, y: y, width: bounds.width - pad * 2, height: 14)
        addSubview(label)
        y += 22
    }

    func row(_ title: String, _ control: NSView, hint: String? = nil) {
        let label = NSTextField(labelWithString: title)
        label.alignment = .right
        label.font = NSFont.systemFont(ofSize: 12)
        label.lineBreakMode = .byWordWrapping
        label.frame = NSRect(x: pad, y: y + 4, width: labelW, height: 16)
        addSubview(label)

        control.frame.origin = NSPoint(x: controlX, y: y)
        addSubview(control)
        y += max(control.frame.height, 22) + 6
        if let hint { self.hint(hint) }
        y += 6
    }

    func check(_ title: String, get: () -> Bool, set: @escaping (Bool) -> Void,
               hint: String? = nil) {
        let box = NSButton(checkboxWithTitle: title, target: nil, action: nil)
        box.state = get() ? .on : .off
        box.frame = NSRect(x: controlX, y: y, width: bounds.width - controlX - pad, height: 18)
        let handler = Toggle(set: set)
        box.target = handler
        box.action = #selector(Toggle.fire(_:))
        keepAlive.append(handler)
        addSubview(box)
        y += 24
        if let hint { self.hint(hint) }
    }

    func slider(_ title: String, _ range: ClosedRange<Double>, get: () -> Double,
                set: @escaping (Double) -> Void, format: @escaping (Double) -> String) {
        let value = NSTextField(labelWithString: format(get()))
        value.font = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .regular)
        value.textColor = .secondaryLabelColor
        value.alignment = .right
        value.frame = NSRect(x: bounds.width - pad - 68, y: y + 3, width: 68, height: 14)
        addSubview(value)

        let s = NSSlider(value: get(), minValue: range.lowerBound, maxValue: range.upperBound,
                         target: nil, action: nil)
        s.frame = NSRect(x: controlX, y: y, width: bounds.width - controlX - pad - 78, height: 20)
        let handler = Slide(set: set, format: format, label: value)
        s.target = handler
        s.action = #selector(Slide.fire(_:))
        keepAlive.append(handler)
        addSubview(s)

        let label = NSTextField(labelWithString: title)
        label.alignment = .right
        label.font = NSFont.systemFont(ofSize: 12)
        label.frame = NSRect(x: pad, y: y + 2, width: labelW, height: 16)
        addSubview(label)
        y += 30
    }

    func hint(_ text: String) {
        let label = NSTextField(wrappingLabelWithString: text)
        label.font = NSFont.systemFont(ofSize: 10.5)
        label.textColor = .tertiaryLabelColor
        label.frame = NSRect(x: controlX, y: y, width: bounds.width - controlX - pad, height: 14)
        label.sizeToFit()
        label.frame.size.width = bounds.width - controlX - pad
        addSubview(label)
        y += max(14, label.frame.height) + 4
    }

    func footer(_ title: String, _ action: @escaping () -> Void) {
        y += 18
        let line = NSBox(frame: NSRect(x: pad, y: y, width: bounds.width - pad * 2, height: 1))
        line.boxType = .separator
        addSubview(line)
        y += 14

        let b = NSButton(title: title, target: nil, action: nil)
        b.bezelStyle = .rounded
        b.frame = NSRect(x: pad, y: y, width: 200, height: 24)
        let handler = Press(action: action)
        b.target = handler
        b.action = #selector(Press.fire)
        keepAlive.append(handler)
        addSubview(b)
        y += 34
    }

    /// The document view has to be as tall as what is in it, and it is only known once the last
    /// row has been added.
    func finish() {
        y += 8
        frame.size.height = y
    }

    /// Targets are held weakly by controls, so the little objects that carry each closure have to
    /// be kept somewhere or the first click would go to a deallocated one.
    private var keepAlive: [AnyObject] = []

    private final class Toggle: NSObject {
        let set: (Bool) -> Void
        init(set: @escaping (Bool) -> Void) { self.set = set }
        @objc func fire(_ sender: NSButton) {
            set(sender.state == .on)
            SettingsWindow.shared.apply()
        }
    }

    private final class Slide: NSObject {
        let set: (Double) -> Void
        let format: (Double) -> String
        weak var label: NSTextField?
        init(set: @escaping (Double) -> Void, format: @escaping (Double) -> String,
             label: NSTextField) {
            self.set = set; self.format = format; self.label = label
        }
        @objc func fire(_ sender: NSSlider) {
            set(sender.doubleValue)
            label?.stringValue = format(sender.doubleValue)
            SettingsWindow.shared.apply()
        }
    }

    private final class Press: NSObject {
        let action: () -> Void
        init(action: @escaping () -> Void) { self.action = action }
        @objc func fire() { action() }
    }
}
