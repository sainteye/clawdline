// The window's second half: a bar across the top, and somewhere else to go.
//
// The console window was one WKWebView pointed at this machine's daemon. It is
// now two, side by side and never mixed:
//
//   the console   http://127.0.0.1:<port>  — `Shell.web`, the view that has
//                 always been here, carrying this machine's local token in its
//                 cookie store and allowed to be nowhere else.
//   the web       anywhere http or https   — `ExternalWeb`, with a data store
//                 of its own that the token has never been written to.
//
// **Two views rather than one that navigates.** A single view would have to put
// the token cookie in, take it out again, and be right about it every time —
// and the thing it would have to be right about is a credential. Two stores is
// the same rule expressed once, in a place WebKit enforces rather than this
// code: a cookie jar the token was never written to cannot hand it to anybody,
// whatever the page does, wherever it navigates, however long it stays open.
// The cost is that the two do not share a login, which for a local daemon and a
// hosted console is the correct answer anyway.
//
// The bar is native rather than a page of its own, because a page would need a
// third web view to draw it and would have to be told what the other two are
// doing. Its colours and spacing are the console's own — the tokens copied byte
// for byte into web/console/src/legacy/tokens.css — and its words are the Swift
// app's, under the Swift app's own names, except the few Copy.swift marks as
// macOS's own. See `Ink` below and Copy.swift.
import AppKit
import WebKit

/// The Cloud console. `CLAWDLINE_NEXT_CLOUD` points the tab at something else,
/// which is how it gets opened against anything but the real one.
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

/// What a typed address means. Something with a scheme is taken as it is;
/// anything else is tried as `https://`, which is what an address bar is for.
/// Nothing here searches: this shell has no search engine and inventing one
/// would send what somebody typed to a company they did not name.
func address(_ typed: String) -> URL? {
    let text = typed.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !text.isEmpty else { return nil }
    if let url = URL(string: text), let scheme = url.scheme?.lowercased() {
        return (scheme == "http" || scheme == "https") ? url : nil
    }
    guard !text.contains(" ") else { return nil }
    return URL(string: "https://" + text)
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
        let colour = !isEnabled ? Ink.faint : (isSelected ? Ink.accent : Ink.dim)
        if isIcon {
            contentTintColor = colour
        } else {
            attributedTitle = NSAttributedString(string: title, attributes: [
                .font: font ?? NSFont.systemFont(ofSize: 11.5),
                .foregroundColor: colour,
            ])
        }
        layer?.backgroundColor = (isSelected ? Ink.accentIn : Ink.card).cgColor
        layer?.borderColor = (isSelected ? Ink.accentEd : Ink.line).cgColor
    }
}

/// The bar: where the window can go, and where it is.
///
/// `.top`'s measurements — 14 points in from each edge, 12 between groups — and
/// `.composer .box`'s shape for the address field, which is the console's only
/// other place somebody types.
final class BrowserBar: NSView {
    let back: PillButton
    let forward: PillButton
    let refresh: PillButton
    let consoleTab: PillButton
    let cloudTab: PillButton
    let field = NSTextField()
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
        consoleTab = PillButton(L.t.homeLocalTitle, target: target,
                                action: #selector(Shell.browserShowConsole))
        cloudTab = PillButton(L.t.homeCloudPreviewTitle, target: target,
                              action: #selector(Shell.browserShowWeb))
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

        field.isBordered = false
        field.drawsBackground = false
        field.focusRingType = .none
        field.font = NSFont.systemFont(ofSize: 12)
        field.textColor = Ink.ink
        field.usesSingleLineMode = true
        field.lineBreakMode = .byTruncatingTail
        field.cell?.isScrollable = true
        field.target = target
        field.action = #selector(Shell.browserGo)
        field.translatesAutoresizingMaskIntoConstraints = false

        box.wantsLayer = true
        box.layer?.backgroundColor = Ink.card.cgColor
        box.layer?.borderColor = Ink.line.cgColor
        box.layer?.borderWidth = 1
        box.layer?.cornerRadius = Ink.radius
        box.translatesAutoresizingMaskIntoConstraints = false
        box.addSubview(field)
        box.setContentHuggingPriority(NSLayoutConstraint.Priority(rawValue: 1), for: .horizontal)

        let row = NSStackView(views: [back, forward, refresh, consoleTab, cloudTab, box, zoom, outside])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.distribution = .fill
        row.spacing = 6
        row.setCustomSpacing(12, after: refresh)
        row.setCustomSpacing(12, after: cloudTab)
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

    /// Follow both views, so the arrows and the address are what is true rather
    /// than what was true when something was last pressed.
    func follow(_ views: [WKWebView], onChange: @escaping () -> Void) {
        watch = views.flatMap { view in
            [view.observe(\.url) { _, _ in onChange() },
             view.observe(\.canGoBack) { _, _ in onChange() },
             view.observe(\.canGoForward) { _, _ in onChange() }]
        }
    }

    /// Whether somebody's own text is in it, in which case nothing overwrites
    /// it.
    ///
    /// **Holding the keyboard is not the same as having typed.** An empty box
    /// holds no unsent text, so filling it takes nothing away from anybody —
    /// and reading focus alone as typing left the address blank for a whole
    /// run of the app: a window hands the keyboard to the first key view it
    /// finds, this field is it, and so the guard below was true before the
    /// first page had even loaded and true for every refresh after. What is
    /// protected here is a draft, not a caret.
    var isBeingTyped: Bool { field.currentEditor() != nil && !field.stringValue.isEmpty }
}

// MARK: - The web side

/// Everywhere that is not this machine's daemon, in a store of its own.
///
/// It has its own delegates rather than the shell's: the shell's grant the
/// microphone to the console's origin and hand the console's outgoing links to
/// the person's browser, and neither sentence is true out here. What is true
/// out here is that it is an ordinary browser tab with no standing at all.
final class ExternalWeb: NSObject, WKNavigationDelegate, WKUIDelegate {
    let view: WKWebView

    /// The bar's cue: something about where this view is has changed.
    var onNavigation: (() -> Void)?
    /// A page finished loading, which is when the cookie wall is worth reading.
    var onLoaded: ((WKWebView) -> Void)?

    /// A fixed identifier, so signing in to the Cloud console survives a
    /// relaunch, and an identifier *of its own*, so this is a different jar
    /// from the one the local token is written into. Generated once, here;
    /// changing it is throwing away everybody's logins.
    private static let storeID = UUID(uuidString: "2faf7ef2-488c-47db-98d9-050ec1781455")!

    override init() {
        let config = WKWebViewConfiguration()
        // Deliberately not the shell's content controller: the fleet bridge and
        // the settings words are the console's half of a conversation with this
        // shell, and a page out there must not be handed either of them.
        if #available(macOS 14.0, *) {
            config.websiteDataStore = WKWebsiteDataStore(forIdentifier: ExternalWeb.storeID)
        } else {
            // Before macOS 14 there is one persistent store and it is the
            // console's. In memory then: a login that does not last is a
            // nuisance, a shared jar is a leak.
            config.websiteDataStore = .nonPersistent()
        }
        view = WKWebView(frame: .zero, configuration: config)
        super.init()
        view.navigationDelegate = self
        view.uiDelegate = self
        view.allowsBackForwardNavigationGestures = true
    }

    func load(_ url: URL) {
        shellLog("browser: going to \(url.absoluteString)")
        view.load(URLRequest(url: url))
    }

    // MARK: Where it may go

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = action.request.url else {
            decisionHandler(.allow)
            return
        }
        // Only where this view itself is going. A subframe is the page's own
        // business, and a page whose iframes could be cancelled one at a time
        // is a page that does not work.
        if let frame = action.targetFrame, !frame.isMainFrame {
            decisionHandler(.allow)
            return
        }
        let scheme = url.scheme?.lowercased() ?? ""
        if scheme == "about" {
            decisionHandler(.allow)
            return
        }
        guard scheme == "http" || scheme == "https" else {
            // mailto:, an app's own scheme, anything the system knows. A web
            // view cannot show them and cancelling silently looks broken —
            // except `file:`, which is this Mac's disk and is not a page out
            // there's to open.
            decisionHandler(.cancel)
            if scheme == "file" {
                shellLog("browser: refused \(url.absoluteString) — a page may not open this Mac's files")
            } else {
                NSWorkspace.shared.open(url)
            }
            return
        }
        // **The console's own address is refused here.** Not because it would
        // leak anything — this store has no token in it, so the daemon would
        // answer 401 — but because a page out there must not be able to aim
        // this window at the console at all. The tab beside it is how somebody
        // gets there, and the person is the one who presses it.
        if isConsole(url) {
            decisionHandler(.cancel)
            shellLog("browser: refused \(url.absoluteString) — the console is reached by its own tab")
            return
        }
        decisionHandler(.allow)
    }

    /// `target="_blank"` out here is an ordinary link: this is the browser, so
    /// it opens in it. Returning nil is what tells WebKit it was handled.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if let url = action.request.url, !isConsole(url),
           url.scheme == "http" || url.scheme == "https" {
            view.load(URLRequest(url: url))
        }
        return nil
    }

    /// Nothing out here gets a camera or a microphone. The console's grant is
    /// in Microphone.swift and is about the console; this is a different page
    /// with a different answer, written down rather than left to a default.
    @available(macOS 12.0, *)
    func webView(_ webView: WKWebView,
                 requestMediaCapturePermissionFor origin: WKSecurityOrigin,
                 initiatedByFrame frame: WKFrameInfo,
                 type: WKMediaCaptureType,
                 decisionHandler: @escaping (WKPermissionDecision) -> Void) {
        shellLog("browser: \(origin.host) asked for a capture device; denied")
        decisionHandler(.deny)
    }

    // MARK: What happened

    func webView(_ webView: WKWebView, didFinish nav: WKNavigation!) {
        shellLog("browser: loaded \(webView.url?.absoluteString ?? "?") title=\(webView.title ?? "?")")
        onNavigation?()
        onLoaded?(webView)
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation nav: WKNavigation!,
                 withError error: Error) {
        let failed = (error as NSError).userInfo[NSURLErrorFailingURLStringErrorKey] as? String
        shellLog("browser: could not load \(failed ?? "?") — \(error.localizedDescription)")
        onNavigation?()
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        shellLog("browser: the page's process ended")
        if webView.url != nil { webView.reload() }
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

/// Where the two zooms are remembered: `<NextConfig.directory>/shell-zoom.json`,
/// beside `shell-introduced.json`, as `{"console": 1.25, "web": 1}`.
///
/// One per view rather than one for the app: the console is one page somebody
/// reads all day, the web side is whatever site is in it, and making one bigger
/// is not a reason to make the other bigger too. Not per site: that is a list
/// that grows, and it would need a limit and an eviction of its own.
///
/// Its own file rather than a key in config.json, which the settings page
/// writes through the daemon: one file, one writer.
struct ZoomStore {
    var fileURL: URL { NextConfig.directory.appendingPathComponent("shell-zoom.json") }

    /// Both, at 100% where the file, or the key, is missing or unreadable, and
    /// pulled inside the limits where a hand edit put them outside — said in
    /// the log, since a value quietly changed looks like a value obeyed.
    func read() -> (console: CGFloat, web: CGFloat) {
        guard let data = try? Data(contentsOf: fileURL) else { return (1, 1) }
        guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            shellLog("zoom: \(fileURL.path) is not a JSON object; both at 100%")
            return (1, 1)
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
        return (value("console"), value("web"))
    }

    func write(console: CGFloat, web: CGFloat) {
        let obj: [String: Any] = ["console": Double(console), "web": Double(web)]
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
    /// Which of the two the window is showing.
    var active: WKWebView { showingWeb ? webTab.view : web }

    /// The window's contents: the bar, and whichever view is being shown.
    func buildBrowser() -> NSView {
        let root = NSView()
        root.wantsLayer = true
        root.layer?.backgroundColor = Ink.bg.cgColor

        webTab = ExternalWeb()
        bar = BrowserBar(target: self)
        content = NSView()
        bar.translatesAutoresizingMaskIntoConstraints = false
        content.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(bar)
        root.addSubview(content)
        NSLayoutConstraint.activate([
            bar.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            bar.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            bar.topAnchor.constraint(equalTo: root.topAnchor),
            bar.heightAnchor.constraint(equalToConstant: 40),
            content.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            content.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            content.topAnchor.constraint(equalTo: bar.bottomAnchor),
            content.bottomAnchor.constraint(equalTo: root.bottomAnchor),
        ])

        // Before either loads anything: a zoom set on a view is kept across
        // everything it navigates to, so this is the only time it is applied.
        let saved = ZoomStore().read()
        web.pageZoom = saved.console
        webTab.view.pageZoom = saved.web
        shellLog("zoom: console \(PageZoom.percent(saved.console))% web \(PageZoom.percent(saved.web))%")

        bar.follow([web, webTab.view]) { [weak self] in self?.refreshBrowserBar() }
        webTab.onNavigation = { [weak self] in self?.refreshBrowserBar() }
        webTab.onLoaded = { [weak self] page in self?.auditCookieWall(after: page) }
        showTab(web: false)
        return root
    }

    /// Put one of the two in the window. The web side is not loaded until the
    /// first time somebody asks for it: an app that opens a connection to a
    /// hosted service at launch is one that has decided for the person.
    func showTab(web wantWeb: Bool) {
        showingWeb = wantWeb
        let view: NSView = wantWeb ? webTab.view : self.web
        if view.superview !== content {
            content.subviews.forEach { $0.removeFromSuperview() }
            view.translatesAutoresizingMaskIntoConstraints = false
            content.addSubview(view)
            NSLayoutConstraint.activate([
                view.leadingAnchor.constraint(equalTo: content.leadingAnchor),
                view.trailingAnchor.constraint(equalTo: content.trailingAnchor),
                view.topAnchor.constraint(equalTo: content.topAnchor),
                view.bottomAnchor.constraint(equalTo: content.bottomAnchor),
            ])
            shellLog("browser: showing \(wantWeb ? "the web side" : "the console")")
        }
        if wantWeb, webTab.view.url == nil { webTab.load(cloudHome) }
        refreshBrowserBar()
        // Nil while the window is still being built; the first responder is
        // whatever the window gives it then.
        window?.makeFirstResponder(view)
    }

    /// The arrows, the tabs and the address, from what the views actually say.
    func refreshBrowserBar() {
        guard bar != nil else { return }
        let view = active
        bar.back.isEnabled = view.canGoBack
        bar.forward.isEnabled = view.canGoForward
        bar.consoleTab.isSelected = !showingWeb
        bar.cloudTab.isSelected = showingWeb
        let zoom = view.pageZoom
        bar.zoom.show(PageZoom.label(zoom))
        bar.zoom.isSelected = PageZoom.larger(than: zoom) == nil || PageZoom.smaller(than: zoom) == nil
        bar.zoom.isHidden = PageZoom.percent(zoom) == 100
        bar.outside.isEnabled = outsideURL != nil
        guard !bar.isBeingTyped else { return }
        bar.field.stringValue = view.url?.absoluteString ?? ""
    }

    // MARK: The bar's buttons

    @objc func browserBack() { active.goBack() }
    @objc func browserForward() { active.goForward() }
    @objc func browserShowConsole() { showTab(web: false) }
    @objc func browserShowWeb() { showTab(web: true) }

    @objc func browserRefresh() { reloadActive() }

    /// ⌘R, the button, and the menu bar's "Open" when the daemon was not up
    /// yet. The console's reload goes through the token again: the daemon
    /// replaces the file when its own store no longer matches it.
    func reloadActive() {
        guard showingWeb else {
            load(web.url.flatMap { isConsole($0) ? $0 : home } ?? home)
            return
        }
        if webTab.view.url == nil { webTab.load(cloudHome) } else { webTab.view.reload() }
    }

    /// Return in the address field.
    @objc func browserGo() {
        let typed = bar.field.stringValue
        guard let url = address(typed) else {
            shellLog("browser: \(typed) is not an address this window can open")
            refreshBrowserBar()
            return
        }
        go(to: url)
        window.makeFirstResponder(active)
    }

    /// Where an address goes: the console's own address is the console's view,
    /// with the token; everything else is the web side, without it. This is the
    /// only place the two are chosen between, and it is the person choosing.
    func go(to url: URL) {
        if isConsole(url) {
            showTab(web: false)
            load(url)
        } else {
            showTab(web: true)
            webTab.load(url)
        }
    }

    // MARK: Zoom

    @objc func browserZoomIn() { zoomActive(by: 1) }
    @objc func browserZoomOut() { zoomActive(by: -1) }
    @objc func browserZoomReset() { zoomActive(by: 0) }

    /// One step up, one step down, or back to 100%, on the view in front, and
    /// remembered for next time. See `PageZoom` for the limits and what a press
    /// past one does.
    func zoomActive(by step: Int) {
        let view = active
        let name = showingWeb ? "web" : "console"
        let before = view.pageZoom
        let after = step == 0 ? 1 : (step > 0 ? PageZoom.larger(than: before) : PageZoom.smaller(than: before))
        guard let after else {
            NSSound.beep()
            shellLog("zoom: \(name) is at its \(step > 0 ? "ceiling" : "floor"), \(PageZoom.percent(before))%;"
                     + " the press changed nothing")
            refreshBrowserBar()
            return
        }
        guard after != before else { return }
        view.pageZoom = after
        ZoomStore().write(console: web.pageZoom, web: webTab.view.pageZoom)
        shellLog("zoom: \(name) \(PageZoom.percent(before))% → \(PageZoom.percent(after))%")
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
    /// `ConsoleWindow`, so they act whichever view — or the address field —
    /// has the keyboard; the items here are how they are found, and what the
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

    // MARK: The wall, read out loud

    /// What the two cookie stores actually hold, once a page out there has
    /// loaded.
    ///
    /// Names and domains, never a value. The claim being settled is that this
    /// machine's token is in one store and not in the other, and that is a
    /// claim about names — so this can be read off a log on a machine with no
    /// screen recording permission, which is the only evidence available here.
    func auditCookieWall(after page: WKWebView) {
        let mine = web.configuration.websiteDataStore
        let theirs = page.configuration.websiteDataStore
        shellLog("cookie-wall: separate-stores=\(mine !== theirs) web-store-persistent=\(theirs.isPersistent)")
        // What the page itself can see. The token cookie is HttpOnly, so this
        // is not the proof — the store listing below is — but it is what the
        // page would use if it were there.
        let probe = "[location.origin, document.cookie.length, document.cookie.indexOf('clawdline-next') >= 0]"
        page.evaluateJavaScript(probe) { value, _ in
            let parts = value as? [Any] ?? []
            let origin = parts.first as? String ?? "?"
            let length = parts.count > 1 ? "\(parts[1])" : "?"
            let visible = parts.count > 2 ? "\(parts[2])" : "?"
            shellLog("cookie-wall: \(origin) sees \(length) characters of cookie, token-visible=\(visible)")
        }
        mine.httpCookieStore.getAllCookies { ours in
            shellLog("cookie-wall: console store — \(cookieNames(ours))")
            theirs.httpCookieStore.getAllCookies { outside in
                let leaked = outside.filter { $0.name == "clawdline-next" || $0.domain == home.host }
                shellLog("cookie-wall: web store — \(cookieNames(outside));"
                         + " token-cookies-here=\(leaked.count)")
            }
        }
    }
}

/// Cookies as names and domains. A value is never written to this log.
func cookieNames(_ cookies: [HTTPCookie]) -> String {
    cookies.isEmpty ? "(none)" : cookies.map { "\($0.name)@\($0.domain)" }.sorted().joined(separator: " ")
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
        guard webTab != nil, window?.isVisible == true,
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
