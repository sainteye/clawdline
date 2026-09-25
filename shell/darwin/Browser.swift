// The window's second half: a bar across the top, and a clear way out.
//
// The WKWebView is only this machine's console. It carries this machine's local
// token in its cookie store and is allowed to be nowhere else. Cloud belongs in
// the person's browser, where their login and password manager already live;
// the Cloud pill hands only `cloudHome` to that browser.
//
// The address is a reading, not a destination field. It is selectable so the
// value can be copied, but it has no border, focus ring, action or editable
// state that could promise navigation this shell does not offer.
import AppKit
import WebKit

/// The Cloud console. `CLAWDLINE_NEXT_CLOUD` points the external Cloud button
/// at something else, which is how a development build opens a test service.
let cloudHome: URL = {
    let raw = ProcessInfo.processInfo.environment["CLAWDLINE_NEXT_CLOUD"] ?? ""
    if !raw.isEmpty, let url = URL(string: raw), url.scheme == "http" || url.scheme == "https" {
        return url
    }
    return URL(string: "https://app.clawdline.com")!
}()

/// Whether a URL is the console this shell serves — scheme, host and port
/// compared as the URL's own pieces, never as a prefix of a string, which is
/// how `127.0.0.1.example.com` gets to be this machine.
func isConsole(_ url: URL) -> Bool {
    guard url.scheme == "http" else { return false }
    guard url.host == home.host else { return false }
    return (url.port ?? 80) == (home.port ?? 80)
}

// MARK: - The console's colours, natively

/// The values in `web/console/src/legacy/tokens.css`, which is the Swift app's
/// own sheet copied byte for byte. Written here as numbers because a native
/// view cannot read a CSS variable; the name of each one is the variable's, so
/// a change over there is findable here.
enum Ink {
    static func hex(_ value: UInt32, _ alpha: CGFloat = 1) -> NSColor {
        NSColor(srgbRed: CGFloat((value >> 16) & 0xff) / 255,
                green: CGFloat((value >> 8) & 0xff) / 255,
                blue: CGFloat(value & 0xff) / 255,
                alpha: alpha)
    }
    static let bg = hex(0x0e_0e11)          // --bg
    static let card = hex(0x16_161a)        // --card
    static let line = hex(0x24_242b)        // --line
    static let ink = hex(0xe8_e6e3)         // --ink
    static let dim = hex(0x9a_978f)         // --dim
    static let faint = hex(0x6d_6a64)       // --faint
    static let accent = hex(0xd9_7757)      // --accent
    static let accentIn = hex(0xd9_7757, 0.13)   // --accent-in
    static let accentEd = hex(0xd9_7757, 0.34)   // --accent-ed
    static let ok = hex(0x5f_9e73)          // --ok: `.conn[data-state="live"] .dot`
    static let radius: CGFloat = 11         // --radius
}

/// A button in the `.conn` pill's shape: a hairline, the card fill, 11.5px of
/// `--dim`. The one being shown takes `.stale .go`'s accent outline instead,
/// and one that cannot be pressed goes to `--faint` — the console's own word
/// for something present but not to be read first.
///
/// A pill is a word or an icon. An icon still has its word: the same Copy.swift
/// string is its tooltip and what VoiceOver reads, so a button that stopped
/// showing its name did not stop having one.
final class PillButton: NSButton {
    var isSelected = false { didSet { paint() } }
    override var isEnabled: Bool {
        didSet {
            paint()
            // The pointer below is decided when the rects are asked for, so a
            // button that changes its mind has to say so.
            window?.invalidateCursorRects(for: self)
        }
    }
    private let isIcon: Bool
    /// A status pill: `.conn` rather than a tab. Selected means "live", shown
    /// by the dot alone; the word, fill and edge stay the plain pill's.
    private var hasDot = false

    init(_ label: String, target: AnyObject?, action: Selector) {
        isIcon = false
        super.init(frame: .zero)
        self.target = target
        self.action = action
        title = label
        setUp()
    }

    /// An SF Symbol in place of the word, which becomes the tooltip and the
    /// accessibility label instead.
    init(symbol: String, label: String, target: AnyObject?, action: Selector) {
        isIcon = true
        super.init(frame: .zero)
        self.target = target
        self.action = action
        title = ""
        image = NSImage(systemSymbolName: symbol, accessibilityDescription: label)?
            .withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize: 11.5, weight: .medium))
        // `.momentaryChange` below draws the alternate while held; the same
        // image, so a press does not blank the button.
        alternateImage = image
        imagePosition = .imageOnly
        toolTip = label
        setAccessibilityLabel(label)
        setUp()
    }

    /// An SF Symbol and a short visible word. The longer label remains the
    /// tooltip and VoiceOver name, so compact chrome does not cost meaning.
    init(symbol: String, title: String, label: String, target: AnyObject?, action: Selector) {
        isIcon = false
        super.init(frame: .zero)
        self.target = target
        self.action = action
        self.title = title
        image = NSImage(systemSymbolName: symbol, accessibilityDescription: label)?
            .withSymbolConfiguration(NSImage.SymbolConfiguration(pointSize: 11.5, weight: .medium))
        alternateImage = image
        imagePosition = .imageLeading
        imageHugsTitle = true
        toolTip = label
        setAccessibilityLabel(label)
        setUp()
    }

    /// A word led by `.conn`'s 7px dot, green while `isSelected`.
    ///
    /// The dot is drawn, not `circle.fill`: a symbol sits on the text's
    /// baseline, which put its middle 2.25pt below the word's, measured. A
    /// plain image is centred in the pill, as the word is.
    init(status label: String, target: AnyObject?, action: Selector) {
        isIcon = false
        super.init(frame: .zero)
        self.target = target
        self.action = action
        title = label
        hasDot = true
        imagePosition = .imageLeading
        imageHugsTitle = true
        setAccessibilityLabel(label)
        setUp()
    }

    private func setUp() {
        isBordered = false
        // A borderless push button dims its own title while it is held, which
        // on a coloured pill reads as a fault rather than a press.
        setButtonType(.momentaryChange)
        wantsLayer = true
        layer?.borderWidth = 1
        font = NSFont.systemFont(ofSize: 11.5)
        setContentCompressionResistancePriority(.required, for: .horizontal)
        setContentHuggingPriority(.required, for: .horizontal)
        paint()
    }

    required init?(coder: NSCoder) { fatalError("this shell builds its views in code") }

    override func layout() {
        super.layout()
        layer?.cornerRadius = bounds.height / 2
    }

    /// `.conn` is `padding: 5px 10px 5px 9px`; a borderless button already
    /// carries a little of that, and the rest is here. An icon is as wide as it
    /// is tall, give or take, so a row of them reads as a row of buttons and
    /// not as a row of stretched words.
    override var intrinsicContentSize: NSSize {
        var size = super.intrinsicContentSize
        size.width = isIcon ? max(28, size.width + 12) : size.width + 16
        size.height = 24
        return size
    }

    /// None. A symbol brings alignment insets of its own, different for each
    /// symbol, and the button's frame — which is what the pill is drawn on —
    /// grew past the 24 points above by that much: 28, 29 and 31, measured.
    override var alignmentRectInsets: NSEdgeInsets { NSEdgeInsetsZero }

    /// The pointing hand, as a link in the page beside it has — over something
    /// that can be pressed, and over nothing that cannot.
    override func resetCursorRects() {
        super.resetCursorRects()
        guard isEnabled, !isHidden else { return }
        addCursorRect(bounds, cursor: .pointingHand)
    }

    /// Change the word. `title` is not observed because `paint` itself sets
    /// the title it draws, and a setter that repaints would call itself.
    func show(_ label: String) {
        guard label != title else { return }
        title = label
        paint()
    }

    private func paint() {
        let selected = isSelected && !hasDot
        let colour = !isEnabled ? Ink.faint : ((hasDot && isSelected) ? Ink.ink : (selected ? Ink.accent : Ink.dim))
        if hasDot {
            image = PillButton.dot(isEnabled && isSelected ? Ink.ok : Ink.faint)
            alternateImage = image
        } else if image != nil {
            contentTintColor = colour
        }
        if !isIcon {
            attributedTitle = NSAttributedString(string: title, attributes: [
                .font: font ?? NSFont.systemFont(ofSize: 11.5),
                .foregroundColor: colour,
            ])
        }
        layer?.backgroundColor = (selected ? Ink.accentIn : Ink.card).cgColor
        layer?.borderColor = (selected ? Ink.accentEd : Ink.line).cgColor
    }

    /// Seven points of dot and, after it, `.conn`'s 6px gap to the word:
    /// `imageHugsTitle` leaves only a sliver of its own.
    private static func dot(_ colour: NSColor) -> NSImage {
        let image = NSImage(size: NSSize(width: 11, height: 7), flipped: false) { _ in
            colour.setFill()
            NSBezierPath(ovalIn: NSRect(x: 0, y: 0, width: 7, height: 7)).fill()
            return true
        }
        image.isTemplate = false
        return image
    }
}

/// The bar: where the window can go, and where it is.
///
/// `.top`'s measurements — 14 points in from each edge, 12 between groups —
/// with the current address presented as a quiet, read-only value.
final class BrowserBar: NSView {
    let back: PillButton
    let forward: PillButton
    let refresh: PillButton
    let consoleTab: PillButton
    let cloudButton: PillButton
    let field = NSTextField(labelWithString: "")
    /// How big the page in front is drawn, shown only when it is not 100%;
    /// pressing it is ⌘0.
    let zoom: PillButton
    /// The page in front, in the person's own browser.
    let outside: PillButton

    private let box = NSView()
    private var watch: [NSKeyValueObservation] = []

    init(target: AnyObject) {
        // The three that move within a page's history are icons, all three:
        // a word beside two glyphs drew one button in a font and two in
        // another, at different weights and on different baselines. Symbols
        // are one family, and each keeps its word as its tooltip and label.
        back = PillButton(symbol: "chevron.left", label: L.t.browserBack,
                          target: target, action: #selector(Shell.browserBack))
        forward = PillButton(symbol: "chevron.right", label: L.t.browserForward,
                             target: target, action: #selector(Shell.browserForward))
        refresh = PillButton(symbol: "arrow.clockwise", label: L.t.webInfoRefresh,
                             target: target, action: #selector(Shell.browserRefresh))
        // The `.conn` pill's green dot: this machine's console is the one
        // place that is always here, and it says so while it is answering.
        consoleTab = PillButton(status: L.t.homeLocalTitle, target: target,
                                action: #selector(Shell.browserShowConsole))
        cloudButton = PillButton(symbol: "cloud", title: L.t.browserCloud,
                                 label: L.t.browserOpenCloud, target: target,
                                 action: #selector(Shell.browserOpenCloud))
        zoom = PillButton(L.t.zoomLevel(100), target: target, action: #selector(Shell.browserZoomReset))
        // Not a compass: the default browser is whichever one the person chose,
        // and Safari's mark would be a claim about which.
        outside = PillButton(symbol: "arrow.up.forward.app", label: L.t.settingsRemoteOpen,
                             target: target, action: #selector(Shell.browserOpenOutside))
        super.init(frame: .zero)

        zoom.toolTip = L.t.menuActualSize
        zoom.isHidden = true

        wantsLayer = true
        layer?.backgroundColor = Ink.card.cgColor

        field.isEditable = false
        field.isSelectable = true
        field.font = NSFont.systemFont(ofSize: 12)
        field.textColor = Ink.dim
        field.usesSingleLineMode = true
        field.lineBreakMode = .byTruncatingTail
        field.cell?.isScrollable = true
        field.translatesAutoresizingMaskIntoConstraints = false

        box.translatesAutoresizingMaskIntoConstraints = false
        box.addSubview(field)
        box.setContentHuggingPriority(NSLayoutConstraint.Priority(rawValue: 1), for: .horizontal)

        let row = NSStackView(views: [back, forward, refresh, consoleTab, cloudButton, box, zoom, outside])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.distribution = .fill
        row.spacing = 6
        row.setCustomSpacing(12, after: refresh)
        row.setCustomSpacing(12, after: cloudButton)
        row.setCustomSpacing(8, after: box)
        row.edgeInsets = NSEdgeInsets(top: 0, left: 14, bottom: 0, right: 14)
        row.translatesAutoresizingMaskIntoConstraints = false
        addSubview(row)

        // The hairline the console draws under `.top`.
        let hair = NSView()
        hair.wantsLayer = true
        hair.layer?.backgroundColor = Ink.line.cgColor
        hair.translatesAutoresizingMaskIntoConstraints = false
        addSubview(hair)

        NSLayoutConstraint.activate([
            row.leadingAnchor.constraint(equalTo: leadingAnchor),
            row.trailingAnchor.constraint(equalTo: trailingAnchor),
            row.topAnchor.constraint(equalTo: topAnchor),
            row.bottomAnchor.constraint(equalTo: bottomAnchor),
            box.heightAnchor.constraint(equalToConstant: 26),
            field.leadingAnchor.constraint(equalTo: box.leadingAnchor, constant: 10),
            field.trailingAnchor.constraint(equalTo: box.trailingAnchor, constant: -10),
            field.centerYAnchor.constraint(equalTo: box.centerYAnchor),
            hair.leadingAnchor.constraint(equalTo: leadingAnchor),
            hair.trailingAnchor.constraint(equalTo: trailingAnchor),
            hair.bottomAnchor.constraint(equalTo: bottomAnchor),
            hair.heightAnchor.constraint(equalToConstant: 1),
        ])
    }

    required init?(coder: NSCoder) { fatalError("this shell builds its views in code") }

    /// Follow the console so the arrows and address are what is true rather
    /// than what was true when something was last pressed.
    func follow(_ view: WKWebView, onChange: @escaping () -> Void) {
        watch = [view.observe(\.url) { _, _ in onChange() },
                 view.observe(\.canGoBack) { _, _ in onChange() },
                 view.observe(\.canGoForward) { _, _ in onChange() }]
    }

}

// MARK: - How big a page is drawn

/// ⌘+ ⌘- ⌘0, on `WKWebView.pageZoom` — the browser's own zoom, which lays the
/// page out again at the new size rather than magnifying a picture of it.
///
/// The steps are Chrome's, cut to the range below, so a press here does what
/// the same press does in the browser the person already uses.
///
/// **Bounded** (docs/design-guidelines.md DG-2). The limit is 50% to 300%.
/// When full, the press changes nothing and is not silent: the bar's zoom pill
/// reads 上限／下限 with the number, in the accent; the menu item is greyed; the
/// press beeps; and the log says which limit. The person pressing is who finds
/// out. Nothing is evicted — it is one number per view, and ⌘0 puts it back.
enum PageZoom {
    static let steps: [CGFloat] = [0.5, 0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3]
    static var lowest: CGFloat { steps.first! }
    static var highest: CGFloat { steps.last! }

    /// The next step up, or nil at the ceiling. A value between steps — one
    /// written into the file by hand — goes to the nearest step past it.
    static func larger(than zoom: CGFloat) -> CGFloat? { steps.first { $0 > zoom + 0.001 } }
    static func smaller(than zoom: CGFloat) -> CGFloat? { steps.last { $0 < zoom - 0.001 } }

    static func percent(_ zoom: CGFloat) -> Int { Int((zoom * 100).rounded()) }

    /// What the bar's pill says: the number, and at a limit which limit.
    static func label(_ zoom: CGFloat) -> String {
        let p = percent(zoom)
        if larger(than: zoom) == nil { return L.t.zoomCeiling(p) }
        if smaller(than: zoom) == nil { return L.t.zoomFloor(p) }
        return L.t.zoomLevel(p)
    }
}

/// Where the console zoom is remembered: `<NextConfig.directory>/shell-zoom.json`,
/// beside `shell-introduced.json`, as `{"console": 1.25}`.
///
/// Its own file rather than a key in config.json, which the settings page
/// writes through the daemon: one file, one writer.
struct ZoomStore {
    var fileURL: URL { NextConfig.directory.appendingPathComponent("shell-zoom.json") }

    /// At 100% where the file, or the key, is missing or unreadable, and pulled
    /// inside the limits where a hand edit put it outside — said in
    /// the log, since a value quietly changed looks like a value obeyed.
    func read() -> CGFloat {
        guard let data = try? Data(contentsOf: fileURL) else { return 1 }
        guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            shellLog("zoom: \(fileURL.path) is not a JSON object; using 100%")
            return 1
        }
        func value(_ key: String) -> CGFloat {
            guard let number = obj[key] as? NSNumber else { return 1 }
            let raw = CGFloat(number.doubleValue)
            let kept = min(PageZoom.highest, max(PageZoom.lowest, raw))
            if kept != raw {
                shellLog("zoom: \(key)=\(raw) in \(fileURL.path) is outside"
                         + " \(PageZoom.percent(PageZoom.lowest))–\(PageZoom.percent(PageZoom.highest))%;"
                         + " using \(PageZoom.percent(kept))%")
            }
            return kept
        }
        return value("console")
    }

    func write(console: CGFloat) {
        let obj: [String: Any] = ["console": Double(console)]
        do {
            try FileManager.default.createDirectory(at: fileURL.deletingLastPathComponent(),
                                                    withIntermediateDirectories: true)
            let data = try JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys])
            try data.write(to: fileURL, options: .atomic)
        } catch {
            // The zoom still changed; only remembering it failed.
            shellLog("zoom: could not remember it — \(error.localizedDescription)")
        }
    }
}

// MARK: - The shell's half

extension Shell {
    var active: WKWebView { web }

    /// The window's contents: the bar and the local console.
    func buildBrowser() -> NSView {
        let root = NSView()
        root.wantsLayer = true
        root.layer?.backgroundColor = Ink.bg.cgColor

        bar = BrowserBar(target: self)
        bar.translatesAutoresizingMaskIntoConstraints = false
        web.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(bar)
        root.addSubview(web)
        NSLayoutConstraint.activate([
            bar.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            bar.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            bar.topAnchor.constraint(equalTo: root.topAnchor),
            bar.heightAnchor.constraint(equalToConstant: 40),
            web.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            web.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            web.topAnchor.constraint(equalTo: bar.bottomAnchor),
            web.bottomAnchor.constraint(equalTo: root.bottomAnchor),
        ])

        // Before it loads anything: a zoom set on a view is kept across every
        // local route it navigates to, so this is the only time it is applied.
        let saved = ZoomStore().read()
        web.pageZoom = saved
        shellLog("zoom: console \(PageZoom.percent(saved))%")

        bar.follow(web) { [weak self] in self?.refreshBrowserBar() }
        refreshBrowserBar()
        return root
    }

    /// The arrows and address, from what the console actually says.
    func refreshBrowserBar() {
        guard bar != nil else { return }
        bar.back.isEnabled = web.canGoBack
        bar.forward.isEnabled = web.canGoForward
        // Green only while the console is actually loaded: a daemon that
        // stopped answering takes the pill back to plain, as `.conn` does.
        bar.consoleTab.isSelected = pageLoaded
        let zoom = web.pageZoom
        bar.zoom.show(PageZoom.label(zoom))
        bar.zoom.isSelected = PageZoom.larger(than: zoom) == nil || PageZoom.smaller(than: zoom) == nil
        bar.zoom.isHidden = PageZoom.percent(zoom) == 100
        bar.outside.isEnabled = outsideURL != nil
        bar.field.stringValue = web.url?.absoluteString ?? ""
        bar.field.toolTip = web.url?.absoluteString
    }

    // MARK: The bar's buttons

    @objc func browserBack() { active.goBack() }
    @objc func browserForward() { active.goForward() }
    @objc func browserShowConsole() { window?.makeFirstResponder(web) }

    @objc func browserOpenCloud() {
        shellLog("browser: handing \(cloudHome.absoluteString) to the default browser")
        NSWorkspace.shared.open(cloudHome)
    }

    @objc func browserRefresh() { reloadActive() }

    /// ⌘R, the button, and the menu bar's "Open" when the daemon was not up
    /// yet. The console's reload goes through the token again: the daemon
    /// replaces the file when its own store no longer matches it.
    func reloadActive() {
        load(web.url.flatMap { isConsole($0) ? $0 : home } ?? home)
    }

    // MARK: Zoom

    @objc func browserZoomIn() { zoomActive(by: 1) }
    @objc func browserZoomOut() { zoomActive(by: -1) }
    @objc func browserZoomReset() { zoomActive(by: 0) }

    /// One step up, one step down, or back to 100%, on the console, and
    /// remembered for next time. See `PageZoom` for the limits and what a press
    /// past one does.
    func zoomActive(by step: Int) {
        let view = active
        let before = view.pageZoom
        let after = step == 0 ? 1 : (step > 0 ? PageZoom.larger(than: before) : PageZoom.smaller(than: before))
        guard let after else {
            NSSound.beep()
            shellLog("zoom: console is at its \(step > 0 ? "ceiling" : "floor"), \(PageZoom.percent(before))%;"
                     + " the press changed nothing")
            refreshBrowserBar()
            return
        }
        guard after != before else { return }
        view.pageZoom = after
        ZoomStore().write(console: web.pageZoom)
        shellLog("zoom: console \(PageZoom.percent(before))% → \(PageZoom.percent(after))%")
        refreshBrowserBar()
    }

    // MARK: The way out

    /// The page in front, if it is one another browser can open: http or https,
    /// never the `about:blank` this shell writes when the daemon is not up.
    var outsideURL: URL? {
        guard let url = active.url, let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https" else { return nil }
        return url
    }

    /// 用瀏覽器打開: the page in front, in the person's default browser.
    ///
    /// This window cannot fill in a password. The ones somebody has saved live
    /// in their browser, encrypted with that browser's own key, and this app
    /// neither can nor should read them — so the answer to "I do not remember
    /// it" is to take the page to where the password is. Only the address goes:
    /// no cookie, no form, nothing typed into the page here.
    ///
    /// The console's own address carries no token either (the token is a
    /// cookie in this window's store), so the browser opening it is asked to
    /// pair as any other browser is — the Swift app's own button does the same.
    @objc func browserOpenOutside() {
        guard let url = outsideURL else {
            NSSound.beep()
            shellLog("browser: nothing in front another browser could open")
            return
        }
        shellLog("browser: handing \(url.absoluteString) to the default browser")
        NSWorkspace.shared.open(url)
    }

    // MARK: The menu

    /// 顯示方式: the zoom keys and the way out, where somebody looks for keys
    /// they do not know yet. The keys themselves are answered by
    /// `ConsoleWindow`, so they act while the console has the keyboard; the
    /// items here are how they are found, and what the
    /// greyed ones say at a limit.
    func browserMenuItem() -> NSMenuItem {
        let menu = NSMenu(title: L.t.menuView)
        func add(_ title: String, _ action: Selector, _ key: String,
                 _ mask: NSEvent.ModifierFlags = .command) {
            let item = NSMenuItem(title: title, action: action, keyEquivalent: key)
            item.keyEquivalentModifierMask = mask
            item.target = self
            menu.addItem(item)
        }
        add(L.t.menuActualSize, #selector(browserZoomReset), "0")
        add(L.t.menuZoomIn, #selector(browserZoomIn), "+")
        add(L.t.menuZoomOut, #selector(browserZoomOut), "-")
        menu.addItem(.separator())
        add(L.t.settingsRemoteOpen, #selector(browserOpenOutside), "o", [.command, .shift])
        let item = NSMenuItem(title: L.t.menuView, action: nil, keyEquivalent: "")
        item.submenu = menu
        return item
    }

}

/// Greyed where pressing would do nothing: a zoom already at its limit, or
/// already at 100%; no page another browser could open; or another of this
/// app's windows — settings, the input bar — in front, which is not the page
/// these act on. Every other item this shell puts in a menu is left as it was.
extension Shell: NSMenuItemValidation {
    func validateMenuItem(_ item: NSMenuItem) -> Bool {
        let mine: [Selector?] = [#selector(browserZoomIn), #selector(browserZoomOut),
                                 #selector(browserZoomReset), #selector(browserOpenOutside)]
        guard mine.contains(item.action) else { return true }
        guard web != nil, window?.isVisible == true,
              NSApp.keyWindow == nil || NSApp.keyWindow === window else { return false }
        let zoom = active.pageZoom
        switch item.action {
        case #selector(browserZoomIn): return PageZoom.larger(than: zoom) != nil
        case #selector(browserZoomOut): return PageZoom.smaller(than: zoom) != nil
        case #selector(browserZoomReset): return PageZoom.percent(zoom) != 100
        default: return outsideURL != nil
        }
    }
}
