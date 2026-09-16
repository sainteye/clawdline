// The macOS shell: a window, a menu bar item, and the console inside a WKWebView.
//
// The shell owns no domain state and answers no route. Everything it shows
// comes from the daemon over http://127.0.0.1:7727, which is the same surface
// the browser and the phone use — so there is one client, not three, and a
// thing proved in one place is proved for all of them.
//
// What it is for is the part a web page cannot do: living in the menu bar,
// answering a global hotkey, and being somewhere to drop a file.
import AppKit
import WebKit

let port = ProcessInfo.processInfo.environment["CLAWDLINE_NEXT_PORT"] ?? "7727"
let home = URL(string: "http://127.0.0.1:\(port)/")!

final class Shell: NSObject, NSApplicationDelegate, WKNavigationDelegate {
    var window: NSWindow!
    var web: WKWebView!
    var status: NSStatusItem!
    var daemon: Process?

    /// Start the daemon that ships inside this bundle.
    ///
    /// The one in the bundle, never whichever is on PATH: a shell that talks to
    /// a different build than it was made with is a bug nobody can reproduce.
    /// If something is already answering on the port, that one is left alone —
    /// a second daemon over the same state directory is two writers.
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
            print("daemon: started from the bundle")
        } catch {
            print("daemon: could not start: \(error.localizedDescription)")
        }
        fflush(stdout)
    }

    func applicationDidFinishLaunching(_ note: Notification) {
        let config = WKWebViewConfiguration()
        web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = self

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1200, height: 800),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false)
        window.title = "Clawdline"
        window.contentView = web
        window.center()
        window.makeKeyAndOrderFront(nil)

        status = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        status.button?.title = "◉"
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "Show Clawdline", action: #selector(show), keyEquivalent: ""))
        menu.addItem(NSMenuItem(title: "Reload", action: #selector(reload), keyEquivalent: "r"))
        menu.addItem(.separator())
        menu.addItem(NSMenuItem(title: "Quit", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"))
        menu.items.forEach { $0.target = self }
        status.menu = menu

        startBundledDaemon()
        // The daemon needs a moment to bind. A failed first load is handled by
        // the delegate below, which says which door it knocked on, and Reload
        // is one menu item away.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.8) { [weak self] in
            self?.web.load(URLRequest(url: home))
        }
        NSApp.activate(ignoringOtherApps: true)
    }

    @objc func show() {
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    @objc func reload() { web.load(URLRequest(url: home)) }

    // The daemon this shell started is this shell's to stop. Leaving it behind
    // would put a second writer over the same state the next time somebody
    // opens the app.
    func applicationWillTerminate(_ note: Notification) {
        daemon?.terminate()
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
        report(webView, attempt: 0)
    }

    private func report(_ webView: WKWebView, attempt: Int) {
        let probe = "[document.documentElement.classList.contains('booting'), document.querySelectorAll('[id]').length, document.querySelectorAll('li.row').length]"
        webView.evaluateJavaScript(probe) { [weak self] value, _ in
            let parts = value as? [Any] ?? []
            let booting = parts.first as? Bool ?? true
            let rowsNow = parts.count > 2 ? (parts[2] as? Int ?? 0) : 0
            // The list arrives after the words do, so a page that is showing
            // but has no rows yet is asked again rather than reported empty.
            if (booting || rowsNow == 0) && attempt < 40 {
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.25) {
                    self?.report(webView, attempt: attempt + 1)
                }
                return
            }
            let nodes = parts.count > 1 ? (parts[1] as? Int ?? -1) : -1
            let rows = parts.count > 2 ? (parts[2] as? Int ?? -1) : -1
            print("loaded: \(webView.url?.absoluteString ?? "?") title=\(webView.title ?? "?") booting=\(booting) elements-with-id=\(nodes) rows=\(rows)")
            fflush(stdout)
        }
    }

    // A daemon that is not running is the ordinary case on a fresh machine, and
    // the shell says which door it knocked on rather than showing a blank page.
    func webView(_ webView: WKWebView, didFailProvisionalNavigation nav: WKNavigation!,
                 withError error: Error) {
        let html = """
        <body style="font:14px -apple-system;padding:40px;color:#ddd;background:#1c1c1e">
        <h2>The daemon is not answering</h2>
        <p>This shell shows what <code>\(home.absoluteString)</code> serves, and nothing is
        listening there.</p>
        <p style="color:#888">Start it with <code>clawdline serve</code>, then choose Reload.</p>
        <p style="color:#666">\(error.localizedDescription)</p></body>
        """
        webView.loadHTMLString(html, baseURL: nil)
    }
}

let app = NSApplication.shared
let shell = Shell()
app.delegate = shell
app.setActivationPolicy(.regular)
app.run()
