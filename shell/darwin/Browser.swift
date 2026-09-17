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
// app's, under the Swift app's own names. See `Ink` below and Copy.swift.
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
final class PillButton: NSButton {
    var isSelected = false { didSet { paint() } }
    override var isEnabled: Bool { didSet { paint() } }

    init(_ label: String, target: AnyObject?, action: Selector) {
        super.init(frame: .zero)
        self.target = target
        self.action = action
        title = label
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
    /// carries a little of that, and the rest is here.
    override var intrinsicContentSize: NSSize {
        var size = super.intrinsicContentSize
        size.width += 16
        size.height = 24
        return size
    }

    private func paint() {
        let colour = !isEnabled ? Ink.faint : (isSelected ? Ink.accent : Ink.dim)
        attributedTitle = NSAttributedString(string: title, attributes: [
            .font: font ?? NSFont.systemFont(ofSize: 11.5),
            .foregroundColor: colour,
        ])
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

    private let box = NSView()
    private var watch: [NSKeyValueObservation] = []

    init(target: AnyObject) {
        // The two arrows have no word in the Swift app — it has no browser —
        // and inventing one is what Copy.swift exists to prevent, so they are
        // the glyphs and nothing else.
        back = PillButton("‹", target: target, action: #selector(Shell.browserBack))
        forward = PillButton("›", target: target, action: #selector(Shell.browserForward))
        refresh = PillButton(L.t.webInfoRefresh, target: target, action: #selector(Shell.browserRefresh))
        consoleTab = PillButton(L.t.homeLocalTitle, target: target,
                                action: #selector(Shell.browserShowConsole))
        cloudTab = PillButton(L.t.homeCloudPreviewTitle, target: target,
                              action: #selector(Shell.browserShowWeb))
        super.init(frame: .zero)

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

        let row = NSStackView(views: [back, forward, refresh, consoleTab, cloudTab, box])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.distribution = .fill
        row.spacing = 6
        row.setCustomSpacing(12, after: refresh)
        row.setCustomSpacing(12, after: cloudTab)
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

    /// Whether somebody is typing in it, in which case nothing overwrites it.
    var isBeingTyped: Bool { field.currentEditor() != nil }
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
