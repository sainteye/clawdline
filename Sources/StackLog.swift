import AppKit

/// Turning a dev stack's log output into something worth reading.
///
/// Three shapes arrive here, because a project declares *a command* rather than a format:
///
/// - **Clean lines.** `process-compose process logs api` — the in-memory buffer, already
///   unwrapped. This is what the projects here use, and what a live log should come from.
/// - **Prefixed lines.** The same command over several processes: `[web  ]  ✓ Ready in 244ms`.
/// - **A JSON envelope.** What lands in the *files* a supervisor writes:
///   `{"level":"error","process":"api","message":"INFO: GET / 200"}` — ninety characters of
///   bookkeeping in front of the thirty that mean something.
///
/// **`level` in that envelope is a lie, and colouring by it is the trap.** process-compose marks
/// everything a process writes to stderr as `error`, and uvicorn, mkdocs, next and cloudflared
/// all log ordinary progress there. Trusting the field paints a healthy stack entirely red,
/// after which nobody reads the colour again. Severity has to come from the text.
enum StackLog {

    struct Entry: Equatable {
        var process: String
        var message: String
    }

    /// Every line, with the process it came from, whichever of the three shapes it arrived in.
    static func entries(_ raw: String) -> [Entry] {
        var out: [Entry] = []
        var current = ""
        for line in raw.split(separator: "\n", omittingEmptySubsequences: false) {
            var text = String(line)

            // `tail`'s banner when a project's logs command reads files.
            if text.hasPrefix("==> "), text.hasSuffix(" <==") {
                var path = String(text.dropFirst(4).dropLast(4))
                if let slash = path.lastIndex(of: "/") { path = String(path[path.index(after: slash)...]) }
                if path.hasSuffix(".log") { path = String(path.dropLast(4)) }
                current = path
                continue
            }

            // process-compose's own multi-process prefix: "[web\t]  message".
            var name = current
            if text.hasPrefix("["), let close = text.firstIndex(of: "]") {
                let inside = text[text.index(after: text.startIndex)..<close]
                    .trimmingCharacters(in: .whitespaces)
                if !inside.isEmpty, !inside.contains(" ") {
                    name = inside
                    text = String(text[text.index(after: close)...])
                    if text.hasPrefix("  ") { text = String(text.dropFirst(2)) }
                }
            }

            let message = unwrap(text)
            if message.trimmingCharacters(in: .whitespaces).isEmpty { continue }
            out.append(Entry(process: name, message: message))
        }
        return out
    }

    /// The line a process actually printed, out of whatever wrapped it.
    ///
    /// Returns the line unchanged when it is not an envelope — a project whose `logs` command is
    /// plain `tail` must not come out empty just because it was not JSON.
    static func unwrap(_ line: String) -> String { envelope(line).message }

    /// The same unwrapping, keeping the pipe the line came down.
    ///
    /// **`level` is worthless for colouring and valuable for post-mortems**, which is why it is
    /// carried here rather than thrown away. Live, it says only "this went to stderr", and
    /// uvicorn, mkdocs, next and cloudflared all report ordinary progress there — colouring by
    /// it paints a healthy stack red (see the note at the top of this file). But in the tail of
    /// a process that has *already exited*, "the last thing it wrote to stderr" is a far better
    /// guess at what killed it than "the last thing it wrote at all". `DevStack.Process.reason`
    /// is the one caller, and that is the question it is asking.
    ///
    /// - Returns: the message, and the envelope's `level` when there was an envelope. A line
    ///   that was never wrapped comes back with a `nil` level — unwrapped is not the same as
    ///   "logged at no particular level", and a caller ranking lines must be able to tell.
    static func envelope(_ line: String) -> (message: String, level: String?) {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard trimmed.hasPrefix("{"), trimmed.hasSuffix("}"),
              let data = trimmed.data(using: .utf8),
              let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return (line, nil) }
        let level = (obj["level"] as? String)?.lowercased()
        guard let message = obj["message"] as? String else { return ("", level) }
        return (message, level)
    }

    /// How loud a line is, judged by what it says rather than by which pipe it came down.
    enum Level { case error, warning, normal }

    private static let errorWords: Set<String> =
        ["ERROR", "FATAL", "CRITICAL", "EXCEPTION", "TRACEBACK", "ERR", "PANIC"]
    private static let warnWords: Set<String> = ["WARNING", "WARN", "WRN"]
    private static let calmWords: Set<String> = ["INFO", "INF", "DEBUG", "DBG", "TRACE", "NOTICE"]

    private static func token(_ s: Substring) -> String {
        s.trimmingCharacters(in: CharacterSet(charactersIn: "[](){}<>:;,.-|")).uppercased()
    }

    static func level(of message: String) -> Level {
        // **Whole words, near the front — not a substring anywhere.**
        //
        // `GET /api/v1/error-report 200` contains "error" and is a perfectly ordinary request;
        // colouring it red teaches people that red means nothing. Real severities are a token of
        // their own within the first few, whether the line leads with them (`INFO:  …`) or puts
        // a timestamp first (`2026-08-18 10:39:28 ERROR …`).
        for t in message.split(separator: " ", omittingEmptySubsequences: true).prefix(5).map(token) {
            if errorWords.contains(t) { return .error }
            if warnWords.contains(t) { return .warning }
        }
        return .normal
    }

    /// Whether the line names its own severity, and names an ordinary one.
    ///
    /// **The counterweight to the envelope's `level`.** process-compose stamps everything a
    /// process writes to stderr as `error`, and cloudflared writes its startup chatter there:
    /// `2026-08-22T13:03:55Z INF Tunnel connection curve preferences…`. Believing the envelope
    /// about a line like that offers a connection notice as the reason a tunnel died — which is
    /// the same wrong answer, arrived at from the other direction, as trusting the last line.
    ///
    /// So where the text says what it is, the text wins, and the envelope only gets to speak for
    /// lines that never said. Nothing here decides a line *is* an error; it only takes the
    /// envelope's word away. See `DevStack.Process.reason`, the one caller.
    static func declaresCalm(_ message: String) -> Bool {
        for t in message.split(separator: " ", omittingEmptySubsequences: true).prefix(5).map(token) {
            if calmWords.contains(t) { return true }
            // A line that leads with a real severity has already answered a different question,
            // and must not be read as calm because "info" turns up later in the sentence.
            if errorWords.contains(t) || warnWords.contains(t) { return false }
        }
        return false
    }

    /// How many characters of leading timestamp a line starts with, if any.
    ///
    /// Dimming it is most of what makes a wall of log readable: every line begins with the same
    /// eighteen characters, they are the least informative part, and they are the first thing the
    /// eye has to skip past on every single row.
    static func timestampLength(_ message: String) -> Int {
        var count = 0
        var sawColon = false
        var sawDigit = false
        for ch in message {
            if ch.isNumber { sawDigit = true }
            else if ch == ":" { sawColon = true }
            else if !"-.TZ+/ ".contains(ch) { break }
            count += 1
            if count > 30 { break }
        }
        guard sawDigit, sawColon else { return 0 }
        // Stop at the last space inside the run, so the following word is not swallowed.
        let head = String(message.prefix(count))
        guard let lastSpace = head.lastIndex(of: " ") else { return 0 }
        return head.distance(from: head.startIndex, to: lastSpace) + 1
    }

    /// The whole pane: one line per entry, the process it came from when several are mixed, and
    /// whatever colours the process itself emitted.
    /// - Parameter only: show just this process, out of a fetch that covered all of them.
    ///   **Filtering here rather than fetching again** is the difference between a tab switch
    ///   costing nothing and costing a second: `process-compose process logs` has a fixed ~1s
    ///   cost per call whatever you ask it for, so the whole stack is fetched once and sliced.
    static func render(_ raw: String, mono: NSFont, showNames: Bool,
                       only: String? = nil) -> NSAttributedString {
        let out = NSMutableAttributedString()
        let dim = NSColor.tertiaryLabelColor
        var list = entries(raw)
        if let only { list = list.filter { $0.process == only } }
        let width = showNames ? (list.map(\.process.count).max() ?? 0) : 0

        for entry in list {
            if showNames, !entry.process.isEmpty {
                let pad = String(repeating: " ", count: max(0, width - entry.process.count))
                out.append(NSAttributedString(string: entry.process + pad + "  ",
                                              attributes: [.font: mono, .foregroundColor: dim]))
            }

            // The server's own colours win wherever it bothered to emit them — that is the
            // formatting its author chose, and it beats anything guessed here. (Which is why the
            // stacks set FORCE_COLOR: these tools all format beautifully and then switch it off
            // the moment they notice they are writing to a pipe.)
            if Ansi.hasEscapes(entry.message) {
                out.append(Ansi.attributed(entry.message, font: mono, defaultColor: .labelColor))
                out.append(NSAttributedString(string: "\n", attributes: [.font: mono]))
                continue
            }

            let colour: NSColor
            switch level(of: entry.message) {
            case .error: colour = .systemRed
            case .warning: colour = .systemOrange
            case .normal: colour = .labelColor
            }

            let stamp = timestampLength(entry.message)
            if stamp > 0 {
                out.append(NSAttributedString(string: String(entry.message.prefix(stamp)),
                                              attributes: [.font: mono, .foregroundColor: dim]))
            }
            out.append(NSAttributedString(string: String(entry.message.dropFirst(stamp)),
                                          attributes: [.font: mono, .foregroundColor: colour]))
            out.append(NSAttributedString(string: "\n", attributes: [.font: mono]))
        }
        if out.length == 0 {
            out.append(NSAttributedString(string: "—", attributes: [.font: mono, .foregroundColor: dim]))
        }
        return out
    }
}
