import Foundation

/// Which way out of the machine — and `off` is the one that ships.
enum TunnelMode: String {
    case off
    case quick
    case named

    /// Anything unrecognised is `off`. A typo in a config file must not be a config file that
    /// opens a tunnel; the only way to get one is to have written the word correctly.
    init(configured: String) {
        let word = configured.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        self = TunnelMode(rawValue: word) ?? .off
    }
}

/// The child's pid, outside the class on purpose.
///
/// The `atexit` hook has to be able to reach it, and an `atexit` hook that reaches through
/// `RemoteTunnel.shared` would touch a lazily-initialised static — from a thread that is already
/// inside `exit`, possibly while that same initialiser is still running, which is a deadlock
/// waiting for the one day it happens. A bare `pid_t` needs no initialiser and no lock.
private var liveTunnelPID: pid_t = 0

/// The way in from outside, as a child process.
///
/// ``RemoteServer`` binds loopback and nothing else, which is the right default and also a wall:
/// the phone on the sofa is not on loopback. A tunnel is how that gets crossed without opening a
/// port — `cloudflared` dials **out** to Cloudflare and the traffic comes back down the connection
/// it made, so nothing on this machine is listening for the internet and no router has to be told
/// anything. The alternative was asking people to forward a port, which is a worse thing to ask
/// for and a far worse thing to get wrong.
///
/// **`cloudflared` is never bundled.** It is the user's own install, exactly like `tmux` and
/// `whisper-cli`: found where package managers put it, reported on when it is missing, and never
/// something this app downloads. See ``Whisper`` for the same shape — the reasoning there applies
/// here word for word.
///
/// **It refuses to start without authentication, and that refusal is the point of the file.**
/// Everything the server answers is somebody's working life: repository names, branch names, the
/// titles of the tasks they are running, and — through `/v1/sessions/…/transcript` — the
/// conversation itself. Behind loopback that is fine. Behind a public hostname with no
/// authentication it is a stranger's index of somebody's private work, reachable by anyone who
/// guesses four English words. The check lives here, in the thing that opens the door, rather
/// than in the settings window, because a settings window can be walked around by editing
/// `~/.config/clawdline/config.json` — and *"I set `remote_tunnel` to `quick` and it went live"*
/// should not be a sentence anybody is able to say. One config key must never be the whole
/// distance between private and public.
///
/// Two modes, and they fail in different places. `quick` needs no Cloudflare account and invents
/// a hostname per run, which this end can only learn by reading cloudflared's logging. `named`
/// uses the user's own tunnel out of `~/.cloudflared/`, where the hostname is in *their* config
/// and never printed at all — so it comes from `remote_hostname` and there is no way to check it.
final class RemoteTunnel {

    static let shared = RemoteTunnel()

    private init() {
        // A tunnel that outlives the app is the worst kind of leak: nothing on screen says it is
        // there, and it carries on working. `atexit` covers an ordinary quit and a crash that
        // unwinds. It does not cover being SIGKILLed — Darwin has no equivalent of Linux's
        // PDEATHSIG, so there is no way to make the kernel do it for us, and pretending otherwise
        // in a comment would be worse than saying so.
        atexit {
            // No captures, so this converts to a C function pointer. It runs on whichever thread
            // called `exit`, which is why it does the one thing that needs no state: a signal.
            if liveTunnelPID > 0 { kill(liveTunnelPID, SIGKILL) }
        }
    }

    /// Off, coming up, up, or broken with a reason.
    ///
    /// `failed` carries a sentence rather than an error code because there is nowhere else for it
    /// to go: this is a background process talking to a settings window, and "status 1" is not
    /// something anybody can act on.
    enum State: Equatable {
        case off
        case starting
        case up(url: String)
        case failed(String)
    }

    private(set) var state: State = .off

    /// Called on the main thread, after `state` has already changed — so a handler can read it
    /// rather than being handed it.
    var onChange: (() -> Void)?

    // MARK: - Knobs, with the reasons attached

    /// How long a child has to have been alive before its death counts as a fresh problem rather
    /// than the same one getting worse. Under this, the backoff keeps climbing.
    static let steadyRun: TimeInterval = 60

    /// Tries before giving up and saying so. Six is a little over a minute of backoff, which is
    /// long enough to ride out a laptop waking up and short enough that a wrong tunnel name is
    /// reported while somebody is still looking at the window.
    static let giveUpAfter = 6

    /// Between `terminate()` and `SIGKILL`. cloudflared is launched with a two-second grace
    /// period of its own (see ``arguments(for:)``), so anything still alive after three has
    /// ignored the polite request.
    static let grace: TimeInterval = 3

    // MARK: - What is running

    /// Everything a launch is a function of. Equatable so `apply()` can tell "the config changed"
    /// from "the config was touched", and leave a working tunnel alone in the second case —
    /// restarting a quick tunnel throws the hostname away and hands the user a new one.
    struct Plan: Equatable {
        var mode: TunnelMode
        var name = ""
        var hostname = ""
        var port = 7717
    }

    private var process: Process?
    private var running: Plan?
    private var startedAt: Date?
    private var retry: DispatchWorkItem?
    private var failures = 0
    private var carry = ""
    private var pendingURL: String?
    private var lastComplaint: String?
    private var loggedURLs = Set<String>()

    /// Which launch we are on. Every child is stamped with the number that was current when it
    /// started, and its exit is ignored unless the stamp still matches.
    ///
    /// A boolean "we meant to stop it" flag was the obvious version and it was wrong: a stop
    /// immediately followed by a start clears the flag before the *old* child's termination
    /// handler has run, so the corpse of the tunnel we killed on purpose gets counted as a crash
    /// of the tunnel we just started — one failure charged to something that never failed.
    private var generation = 0

    // MARK: - Finding the binary

    /// `cloudflared`, if this Mac has one.
    ///
    /// Configured path first, then where package managers actually put it, then `PATH` — which is
    /// last because it is almost never the answer. A GUI app is launched by launchd and not by a
    /// login shell, so its `PATH` is the bare system one and Homebrew is not in it; this is the
    /// same problem ``Tmux/binary`` solves, and it is solved the same way on purpose.
    static func binaryPath() -> String? {
        let configured = Config.shared.cloudflaredPath.trimmingCharacters(in: .whitespaces)
        if !configured.isEmpty, FileManager.default.isExecutableFile(atPath: configured) {
            return configured
        }
        for path in ["/opt/homebrew/bin/cloudflared",     // Homebrew, Apple silicon
                     "/usr/local/bin/cloudflared",        // Homebrew on Intel, and the .pkg
                     "/usr/bin/cloudflared",
                     "/opt/local/bin/cloudflared"]        // MacPorts
        where FileManager.default.isExecutableFile(atPath: path) {
            return path
        }
        for dir in (ProcessInfo.processInfo.environment["PATH"] ?? "").split(separator: ":") {
            let path = "\(dir)/cloudflared"
            if FileManager.default.isExecutableFile(atPath: path) { return path }
        }
        return nil
    }

    static var isInstalled: Bool { binaryPath() != nil }

    // MARK: - Lifecycle

    /// Start, stop, or restart to match the config. Safe to call whenever anything changes, and
    /// deliberately cheap when nothing has: a running tunnel whose plan still matches is left
    /// exactly where it is.
    func apply() {
        onMain { self.applyNow() }
    }

    /// Every reason not to open a tunnel, in one place, as a function of its inputs.
    ///
    /// Pure and separate from ``applyNow`` because this is the part that must be right, and a
    /// test that has to mutate the running app's configuration to reach it is a test nobody
    /// trusts. nil means there is no reason to refuse.
    ///
    /// **Read the interlock before touching it.** What is behind the tunnel is a list of every
    /// repository, branch and task title on this machine. Refusing here is what makes "reachable
    /// from the internet" something a person decided rather than something a config key did.
    static func refusal(mode: TunnelMode, authConfigured: Bool, serverOn: Bool,
                        name: String, hostname: String) -> String? {
        guard mode != .off else { return nil }
        guard authConfigured else {
            return "Not opening a tunnel: this machine has no paired device yet, so anything "
                 + "that found the address could read your sessions. Pair a device first."
        }
        // A tunnel to a port with nothing behind it is not harmless, it is confusing: the address
        // works, the page does not, and the reason is two settings apart.
        guard serverOn else {
            return "The local server is off, so there is nothing to tunnel to. "
                 + "Turn remote access on first."
        }
        guard mode != .named || !name.trimmingCharacters(in: .whitespaces).isEmpty else {
            return "A named tunnel needs remote_tunnel_name."
        }
        guard mode != .named || !hostname.trimmingCharacters(in: .whitespaces).isEmpty else {
            return "A named tunnel needs remote_hostname — the address you point at it."
        }
        return nil
    }

    private func applyNow() {
        let mode = TunnelMode(configured: Config.shared.remoteTunnel)
        guard mode != .off else {
            halt()
            set(.off)
            return
        }
        if let why = Self.refusal(mode: mode,
                                  authConfigured: Config.shared.remoteAuthConfigured,
                                  serverOn: Config.shared.remote,
                                  name: Config.shared.remoteTunnelName,
                                  hostname: Config.shared.remoteHostname) {
            halt()
            set(.failed(why))
            Log.write("tunnel: refused — \(why)")
            return
        }

        let plan = Plan(mode: mode,
                        name: Config.shared.remoteTunnelName.trimmingCharacters(in: .whitespaces),
                        hostname: Config.shared.remoteHostname
                            .trimmingCharacters(in: .whitespaces)
                            .replacingOccurrences(of: "https://", with: "")
                            .replacingOccurrences(of: "http://", with: ""),
                        port: Config.shared.remotePort)

        if let running, running == plan, process?.isRunning == true { return }

        halt()
        failures = 0
        start(plan)
    }

    /// Take it down and mean it.
    func stop() {
        onMain {
            self.halt()
            self.set(.off)
        }
    }

    private func start(_ plan: Plan) {
        guard let bin = Self.binaryPath() else {
            set(.failed("cloudflared is not installed — `brew install cloudflared`, or put its "
                        + "path in \"cloudflared_path\"."))
            return
        }
        if plan.mode == .named {
            guard !plan.name.isEmpty else {
                set(.failed("Set \"remote_tunnel_name\" to the name of the tunnel you created "
                            + "with `cloudflared tunnel create`."))
                return
            }
            // No hostname, no address to publish. cloudflared never prints one for a named
            // tunnel — the routing lives in the user's own `~/.cloudflared/config.yml` and this
            // end has no way to read it back — so an empty field here is not a cosmetic gap, it
            // is a tunnel that comes up and can never be told to anybody.
            guard !plan.hostname.isEmpty else {
                set(.failed("Set \"remote_hostname\" to the hostname you routed to that tunnel. "
                            + "cloudflared does not print it, so this end cannot work it out."))
                return
            }
        }

        // Written before the launch, every time: the port or the hostname may have moved since
        // the last one, and a stale file is a tunnel pointing somewhere that is no longer there.
        let credentials = plan.mode == .named
            ? Self.credentialsFile(forTunnelNamed: plan.name, binary: bin) : nil
        let config = Self.configFile(for: plan, credentials: credentials)
        try? FileManager.default.createDirectory(at: Self.configURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? config.write(to: Self.configURL, atomically: true, encoding: .utf8)

        let task = Process()
        task.executableURL = URL(fileURLWithPath: bin)
        task.arguments = Self.arguments(for: plan)
        // cloudflared never reads stdin, and a child holding an inherited terminal is a child
        // that can stop for a reason nothing here would ever see.
        task.standardInput = FileHandle.nullDevice
        let out = Pipe()
        let err = Pipe()
        task.standardOutput = out
        task.standardError = err
        drain(out.fileHandleForReading)
        drain(err.fileHandleForReading)

        generation += 1
        let mine = generation
        task.terminationHandler = { [weak self] finished in
            let status = finished.terminationStatus
            DispatchQueue.main.async { self?.childExited(status: status, from: mine) }
        }

        do {
            try task.run()
        } catch {
            set(.failed("cloudflared would not start — \(error.localizedDescription)"))
            Log.write("tunnel: launch failed — \(error.localizedDescription)")
            return
        }

        process = task
        liveTunnelPID = task.processIdentifier
        running = plan
        startedAt = Date()
        carry = ""
        pendingURL = nil
        lastComplaint = nil
        set(.starting)
        Log.write("tunnel: \(plan.mode.rawValue) starting, pid \(task.processIdentifier)")
    }

    /// The command line, as a pure function of the plan — so the flags, and the reasons for them,
    /// can be read and tested without launching anything.
    ///
    /// The order matters and is not cosmetic: cloudflared's usage is
    /// `cloudflared tunnel [tunnel command options] run [subcommand options] [TUNNEL]`, so these
    /// three flags have to sit in front of `run`.
    ///
    /// - `--no-autoupdate`: a cloudflared installed from Cloudflare's `.pkg` replaces its own
    ///   binary and restarts, which from this end is indistinguishable from the tunnel dying —
    ///   we would back off and retry against something that was already coming back.
    /// - `--metrics 127.0.0.1:0`: cloudflared binds a metrics server, and left alone it takes the
    ///   first free port from `20241…20245` **in order**. The Mac most likely to have cloudflared
    ///   installed is the Mac already running it for something else, and taking 20241 out from
    ///   under somebody's production tunnel to serve a page nothing here reads is not a trade
    ///   worth making. `:0` asks for any free port instead.
    /// - `--grace-period 2s`: the default is **thirty seconds**, and on SIGTERM cloudflared spends
    ///   it waiting for in-flight requests to finish. `/v1/events` is a server-sent event stream
    ///   that never finishes on purpose, so the default would turn every quit into a half-minute
    ///   of a process refusing to die.
    /// Where the configuration we write for cloudflared lives. Ours, not theirs.
    static var configURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline/cloudflared.yml")
    }

    /// **`--config` is not optional here, and the reason took a live test to find.**
    ///
    /// cloudflared reads `~/.cloudflared/config.yml` unless told otherwise, and that file belongs
    /// to whatever else the person runs tunnels for. Two ways it goes wrong, and both were
    /// observed on a real machine:
    ///
    /// - Its `tunnel:` key **overrides the name on the command line**, so `tunnel run clawdline`
    ///   quietly started somebody's production tunnel instead.
    /// - Its `ingress:` list applies to a quick tunnel too, and a well-written one ends with
    ///   `- service: http_status:404`. So the tunnel connected, the address resolved, and every
    ///   request answered 404 — a failure that looks like Cloudflare being slow and is actually
    ///   somebody else's config file politely refusing a hostname it has never heard of.
    ///
    /// Pointing at our own file makes both impossible, and costs one write.
    static func arguments(for plan: Plan) -> [String] {
        var args = ["tunnel",
                    "--config", configURL.path,
                    "--no-autoupdate",
                    "--metrics", "127.0.0.1:0",
                    "--grace-period", "2s"]
        switch plan.mode {
        case .quick:
            args += ["--url", "http://127.0.0.1:\(plan.port)"]
        case .named:
            args += ["run", plan.name]
        case .off:
            break
        }
        return args
    }

    /// The file that goes with those arguments. Pure, so what gets written can be tested.
    ///
    /// A quick tunnel gets an almost empty one: its address is generated per run and cannot be in
    /// any ingress list, so `--url` on the command line is the whole routing story and anything
    /// else here would only be another chance to be wrong. A named tunnel gets the mapping it
    /// needs and a 404 for everything else, because a tunnel that answers for hostnames it was
    /// never asked about is an open proxy.
    static func configFile(for plan: Plan, credentials: String? = nil) -> String {
        var out = "# Written by Clawdline. Edits here are overwritten every time it starts.\n"
        out += "#\n"
        out += "# It exists so that ~/.cloudflared/config.yml is never read for this: that file's\n"
        out += "# `tunnel:` key overrides the name on the command line, and its ingress list would\n"
        out += "# answer 404 for an address it has never heard of.\n"
        // A file of nothing but comments parses as null, and cloudflared logs an error about an
        // empty configuration before carrying on anyway. It is only noise, but it is noise in the
        // one place somebody will look when a tunnel does not come up — so give it something
        // true to read that changes nothing.
        out += "no-autoupdate: true\n"
        guard plan.mode == .named else { return out }
        out += "\ntunnel: \(plan.name)\n"
        if let credentials, !credentials.isEmpty { out += "credentials-file: \(credentials)\n" }
        out += "\ningress:\n"
        out += "  - hostname: \(plan.hostname)\n"
        out += "    service: http://127.0.0.1:\(plan.port)\n"
        out += "    originRequest:\n"
        // /v1/events is a stream that is meant never to end. Without this a reader on a phone is
        // disconnected on a timer, for no reason it could act on.
        out += "      connectTimeout: 30s\n"
        // Anything else is not ours to answer, and a tunnel that answers for hostnames it was
        // never asked about is an open proxy.
        out += "  - service: http_status:404\n"
        return out
    }

    /// The credentials file for a named tunnel, if it can be found.
    ///
    /// Asked of cloudflared rather than guessed at: the file is named after the tunnel's id, and
    /// the person configuring this knows it by its name. Left out when it cannot be resolved —
    /// cloudflared can often find it from `cert.pem` on its own, and a wrong path is worse than
    /// a missing one.
    static func credentialsFile(forTunnelNamed name: String, binary: String) -> String? {
        let listing = capture(binary, ["tunnel", "list", "--output", "json"])
        guard let data = listing.data(using: .utf8),
              let rows = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]],
              let id = rows.first(where: { ($0["name"] as? String) == name })?["id"] as? String
        else { return nil }
        let path = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".cloudflared/\(id).json").path
        return FileManager.default.fileExists(atPath: path) ? path : nil
    }

    private static func capture(_ launch: String, _ args: [String]) -> String {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: launch)
        task.arguments = args
        let out = Pipe()
        task.standardOutput = out
        task.standardError = Pipe()
        guard (try? task.run()) != nil else { return "" }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        task.waitQuietly()
        return String(data: data, encoding: .utf8) ?? ""
    }

    // MARK: - Reading what it says

    /// Read as it arrives, on both pipes.
    ///
    /// `readDataToEndOfFile` would be shorter and would hang: cloudflared writes several hundred
    /// bytes in its first second and keeps going, and a `Process` whose pipe buffer fills up
    /// blocks in the kernel with nothing anywhere to say so. The URL we are waiting for is in
    /// that stream, so waiting for the end of it is waiting for something that only happens after
    /// the tunnel we wanted has closed.
    private func drain(_ handle: FileHandle) {
        handle.readabilityHandler = { [weak self] h in
            let data = h.availableData
            guard !data.isEmpty else {
                // EOF. Letting the handler stand keeps the descriptor and the queue alive for as
                // long as the app does, once per tunnel restart.
                h.readabilityHandler = nil
                return
            }
            guard let text = String(data: data, encoding: .utf8) else { return }
            DispatchQueue.main.async { self?.ingest(text) }
        }
    }

    /// Whole lines only. A read lands wherever the kernel says it lands, and the half of the
    /// buffer holding the hostname is as likely to be the second half as the first.
    private func ingest(_ chunk: String) {
        carry += chunk
        while let newline = carry.firstIndex(of: "\n") {
            let line = String(carry[carry.startIndex..<newline])
            carry = String(carry[carry.index(after: newline)...])
            consider(line)
        }
        // Something writing a megabyte without a newline is not a cloudflared this understands,
        // and an ever-growing string is not where that should be discovered.
        if carry.utf8.count > 64 * 1024 { carry = "" }
    }

    private func consider(_ line: String) {
        if let complaint = Self.complaint(in: line) {
            lastComplaint = complaint
            Log.write("tunnel: \(complaint)")
        }
        guard let plan = running else { return }

        // A quick tunnel prints its address about a second before it has a connection to carry
        // anything, and says so itself — *"it may take some time to be reachable"*. Publishing on
        // the banner alone means handing somebody a URL that 404s, and they will conclude the
        // feature is broken rather than that it is early. So the banner is remembered and the
        // registration is what promotes it.
        if plan.mode == .quick, let url = Self.quickURL(in: line) {
            pendingURL = url
        }
        guard let location = Self.registeredConnection(in: line) else { return }
        switch plan.mode {
        case .quick:
            guard let url = pendingURL else { return }
            announce(url, via: location)
        case .named:
            announce("https://\(plan.hostname)", via: location)
        case .off:
            break
        }
    }

    private func announce(_ url: String, via location: String) {
        set(.up(url: url))
        logOnce(url, via: location)
    }

    /// The address, in the log, **once**.
    ///
    /// It has to be there at all. A quick tunnel's hostname is generated fresh on every start and
    /// exists nowhere else — not in the config, not on any dashboard, not on Cloudflare's side in
    /// any form the user can look up — so if the app quits before anybody read it off the panel,
    /// the log is the only copy there has ever been.
    ///
    /// And once, because it is a password wearing a hostname's clothes: while the tunnel is up,
    /// holding those four words *is* the access. This file is also the file people attach to bug
    /// reports. One line is something a person can notice and think about before pasting; a line
    /// per reconnect is something that survives being skim-read, and an overnight tunnel that
    /// flaps would write dozens. Reconnections still get a line — they are worth knowing about —
    /// with the random part taken out, which says everything the reconnection needs to say.
    private func logOnce(_ url: String, via location: String) {
        if loggedURLs.insert(url).inserted {
            Log.write("tunnel: up at \(url) — via \(location)")
        } else {
            Log.write("tunnel: back up at \(Self.redacted(url)) — via \(location)")
        }
    }

    // MARK: - The parsers
    //
    // Pure, static, `String` in and an Optional out, for the same reason `Tmux.parsePanes` and
    // `ITerm.parseClaudeTTYs` are: this is where the bugs live, and a test that has to open a
    // tunnel to Cloudflare to find one is a test nobody runs. The fixtures in these comments are
    // real lines, captured from cloudflared 2026.6.1 against 127.0.0.1:7717.

    /// The address a quick tunnel was given, out of one line of logging.
    ///
    /// It arrives inside an ASCII banner on **stderr** — which is where cloudflared puts all of
    /// its output; stdout stays empty for the whole run:
    ///
    /// ```text
    /// 2026-08-18T09:31:17Z INF +-------------------------------------------------------------+
    /// 2026-08-18T09:31:17Z INF |  Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):  |
    /// 2026-08-18T09:31:17Z INF |  https://denied-franchise-william-jade.trycloudflare.com                                   |
    /// 2026-08-18T09:31:17Z INF +-------------------------------------------------------------+
    /// ```
    ///
    /// Matched by its suffix rather than by where it sits, and every `https://` on the line is
    /// tried rather than the first, because the run prints two other URLs before this one:
    ///
    /// ```text
    /// 2026-08-18T09:31:14Z INF Thank you for trying Cloudflare Tunnel. … (https://www.cloudflare.com/website-terms/) … following: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps
    /// ```
    ///
    /// A rule as reasonable-sounding as "the first URL in the output" would have published
    /// Cloudflare's terms of use as the address of this machine.
    static func quickURL(in line: String) -> String? {
        var search = line.startIndex..<line.endIndex
        while let mark = line.range(of: "https://", range: search) {
            // Stop at the first character no host name can contain. The banner pads the line out
            // with spaces and closes it with a `|`, so that is what ends it in practice.
            let run = line[mark.lowerBound...].prefix { ch in
                ch.isASCII && (ch.isLetter || ch.isNumber
                               || ch == "." || ch == "-" || ch == ":" || ch == "/")
            }
            var text = String(run)
            while text.hasSuffix("/") { text.removeLast() }
            if text.hasSuffix(".trycloudflare.com"),
               text.count > "https://.trycloudflare.com".count {
                return text
            }
            search = mark.upperBound..<line.endIndex
        }
        return nil
    }

    /// Whether this line says a connection to Cloudflare's edge is live — and where it landed.
    ///
    /// Returns the edge location rather than a `Bool` so the log line can say something useful,
    /// and so this reads like every other parser here. A line that announces a connection without
    /// naming a location answers `"?"` and not nil: whether the tunnel is up must not depend on a
    /// field that exists for logging.
    ///
    /// Observed on both a quick tunnel and a named one — same line, same code path:
    ///
    /// ```text
    /// 2026-08-18T09:31:18Z INF Registered tunnel connection connIndex=0 connection=da4d66ef-9c21-4b23-82c8-bc14bb60cc8e event=0 ip=2606:4700:a8::5 location=tpe01 protocol=quic
    /// ```
    ///
    /// **`unregistered` contains `registered`.** cloudflared prints `Unregistered tunnel
    /// connection` on the way down, and a plain `contains` reads a tunnel closing as a tunnel
    /// opening — which, at the exact moment the app is quitting or the network has gone, is the
    /// last thing that should be published.
    static func registeredConnection(in line: String) -> String? {
        let lower = line.lowercased()
        guard !lower.contains("unregistered") else { return nil }
        let announced = lower.contains("registered tunnel connection")
            // Older builds put the words the other way round. Kept because the binary belongs to
            // the user, not to us, and Homebrew is not the only way it arrives on a Mac.
            || (lower.contains(" registered ") && lower.contains("connindex="))
        guard announced else { return nil }
        return field("location=", in: line) ?? "?"
    }

    /// The human half of an error, if this line is one.
    ///
    /// Two shapes, and the second is the one that mattered. cloudflared's log lines are
    /// `<timestamp> <LEVEL> <message>` with three-letter levels, so an `ERR` or `FTL` is easy to
    /// pick out. But the failure a person is actually going to hit — a typo in the tunnel name —
    /// never reaches the logger at all. It is a bare line on stderr, no timestamp, no level,
    /// exit status 1:
    ///
    /// ```text
    /// error parsing tunnel ID: clawdline-no-such-tunnel is neither the ID nor the name of any of your tunnels
    /// ```
    ///
    /// Which is why anything that is not a log line counts as a complaint. The cost of being
    /// wrong about that is one extra line in the log; the cost of the tidier rule was reporting
    /// the commonest configuration mistake as "cloudflared kept exiting (status 1)".
    static func complaint(in line: String) -> String? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return nil }
        let parts = trimmed.split(separator: " ", maxSplits: 2, omittingEmptySubsequences: false)
        if parts.count >= 2, ["DBG", "INF", "WRN", "ERR", "FTL"].contains(String(parts[1])) {
            guard parts[1] == "ERR" || parts[1] == "FTL", parts.count > 2 else { return nil }
            return String(parts[2].prefix(200))
        }
        // Long enough to name a cause, short enough to sit in a settings window.
        return String(trimmed.prefix(200))
    }

    /// A URL with the part that is secret taken out: `https://….trycloudflare.com`.
    static func redacted(_ url: String) -> String {
        guard let scheme = url.range(of: "://") else { return url }
        let host = url[scheme.upperBound...].prefix { $0 != "/" }
        guard let dot = host.firstIndex(of: ".") else { return url }
        return String(url[url.startIndex..<scheme.upperBound]) + "…" + String(host[dot...])
    }

    /// `key=value`, up to the next space. cloudflared's structured fields are all this shape.
    private static func field(_ key: String, in line: String) -> String? {
        guard let mark = line.range(of: key) else { return nil }
        let value = line[mark.upperBound...].prefix { !$0.isWhitespace }
        return value.isEmpty ? nil : String(value)
    }

    /// 1, 2, 4 … capped at 60 seconds.
    ///
    /// Doubling because the two things that actually take a tunnel down — a laptop that has just
    /// woken up, and an edge refusing connections — both come back on their own or not at all,
    /// and retrying harder helps with neither.
    static func backoff(_ attempt: Int) -> TimeInterval {
        min(60, pow(2, Double(max(0, attempt - 1))))
    }

    // MARK: - When it dies

    private func childExited(status: Int32, from stamp: Int) {
        // Somebody else's corpse: either we stopped this one deliberately, or a newer child has
        // already replaced it. Either way it is not a failure of what is running now.
        guard stamp == generation else { return }

        let lived = startedAt.map { Date().timeIntervalSince($0) } ?? 0
        process = nil
        liveTunnelPID = 0

        // A child that ran for a while and then died is a fresh problem rather than the same one
        // getting worse, so the backoff starts over instead of resuming at an hour's worth of
        // waiting. Deliberately measured from launch and not from the moment it came up: a tunnel
        // that registers and immediately drops, over and over, is exactly what the counter is for.
        if lived >= Self.steadyRun { failures = 0 }
        failures += 1

        guard failures <= Self.giveUpAfter else {
            let why = lastComplaint ?? "cloudflared exited with status \(status)."
            set(.failed("The tunnel would not stay up. \(why)"))
            Log.write("tunnel: gave up after \(failures - 1) tries — \(why)")
            running = nil
            return
        }

        let wait = Self.backoff(failures)
        Log.write("tunnel: cloudflared exited (status \(status)) after \(Int(lived))s — "
                  + "try \(failures) of \(Self.giveUpAfter) in \(Int(wait))s")
        set(.starting)

        let work = DispatchWorkItem { [weak self] in
            guard let self, let plan = self.running else { return }
            self.retry = nil
            self.start(plan)
        }
        retry = work
        DispatchQueue.main.asyncAfter(deadline: .now() + wait, execute: work)
    }

    // MARK: - Taking it down

    /// Everything except deciding what to say afterwards, which is the caller's business — a stop
    /// is `.off`, a refusal is `.failed`, and a restart is neither.
    private func halt() {
        retry?.cancel()
        retry = nil
        // Bumping the generation is what tells the termination handler this death was wanted.
        generation += 1
        running = nil
        pendingURL = nil
        startedAt = nil
        carry = ""

        guard let task = process else {
            liveTunnelPID = 0
            return
        }
        process = nil
        (task.standardOutput as? Pipe)?.fileHandleForReading.readabilityHandler = nil
        (task.standardError as? Pipe)?.fileHandleForReading.readabilityHandler = nil
        liveTunnelPID = 0
        guard task.isRunning else { return }

        let pid = task.processIdentifier
        task.terminate()                                    // SIGTERM, and it usually is enough
        // And a hard stop if it was not. `--grace-period 2s` means cloudflared has agreed to be
        // gone well inside this; anything still here has stopped listening, and a tunnel is not a
        // thing to leave running because a process was rude about closing.
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + Self.grace) {
            if task.isRunning { kill(pid, SIGKILL) }
        }
        Log.write("tunnel: stopped (pid \(pid))")
    }

    // MARK: - Threading

    /// Everything mutable here lives on the main thread — the readability handlers and the
    /// termination handler both hop onto it — so that `state` can be read straight from a menu or
    /// a settings window without a lock between them and it.
    private func onMain(_ body: @escaping () -> Void) {
        if Thread.isMainThread { body() } else { DispatchQueue.main.async(execute: body) }
    }

    private func set(_ next: State) {
        guard state != next else { return }
        state = next
        onChange?()
    }
}
