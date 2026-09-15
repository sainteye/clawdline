import Foundation

/// Provider-neutral presentation facts derived from one complete terminal-grid capture.
///
/// A screen is evidence for a live activity line and for quietness only when the capture itself
/// completed. It is not a semantic transcript and, without a provider menu/hook signal, it is not
/// evidence that a person is being asked a question unless one of the provider-neutral menu
/// shapes is complete. Strong menu evidence is checked before activity because providers may
/// leave a stale spinner above a dialog. Ambiguous menu-shaped captures fail closed as unknown.
public enum TerminalSessionPresentation {
    public enum Observation: Equatable, Sendable {
        case working(String)
        case waiting
        case idle
        case unknown

        public var state: String {
            switch self {
            case .working: return "working"
            case .waiting: return "waiting"
            case .idle: return "idle"
            case .unknown: return "unknown"
            }
        }

        public var workState: String {
            switch self {
            case .working: return "working"
            case .waiting: return "waiting_you"
            case .idle: return "unknown"
            case .unknown: return "unknown"
            }
        }

        public var line: String? {
            guard case .working(let line) = self else { return nil }
            return line
        }
    }

    private static let claudeSpinners: Set<Character> = [
        "✳", "✻", "✽", "✢", "✶", "✱", "✴", "·", "*",
        // Claude Code 2.1.228 introduced the half-circle animation family.
        "◐", "◑", "◒", "◓", "◴", "◵", "◶", "◷",
    ]
    private static let codexBullets: Set<Character> = ["•", "‣", "▪", "·", "*"]

    /// A missing capture is an unknown observation, never an idle answer.
    public static func observe(
        _ screen: String?, assistant: Assistant, tailLines: Int = 25
    ) -> Observation {
        guard let screen, !screen.isEmpty else { return .unknown }
        switch menuEvidence(in: screen, assistant: assistant, tailLines: max(30, tailLines)) {
        case .waiting: return .waiting
        case .ambiguous: return .unknown
        case .none: break
        }
        if let line = workingLine(in: screen, assistant: assistant, tailLines: tailLines) {
            return .working(line)
        }
        return .idle
    }

    private enum MenuEvidence { case waiting, ambiguous, none }
    private static let menuCarets: Set<Character> = ["❯", "›", "▸", "▶"]
    private static let menuBoxes: Set<Character> = ["│", "┃", "|", "▌", "▏", "╎", "┆", "┊"]
    private static let horizontalRules: Set<Character> = ["─", "━", "═", "-", "╌", "╍"]

    /// The strong numbered menu shapes already shared by Claude and Codex terminal readers.
    /// Claude's indented caret is sufficient screen evidence. Its flush-left AskUserQuestion and
    /// unnumbered pickers need an independent hook, which Linux does not yet have, so a plausible
    /// framed shape is unknown rather than idle/working. Codex instead proves a menu when the last
    /// caret on screen heads one of at least two contiguous numbered rows.
    private static func menuEvidence(
        in screen: String, assistant: Assistant, tailLines: Int
    ) -> MenuEvidence {
        let lines = plain(screen).split(separator: "\n", omittingEmptySubsequences: false)
            .map(String.init).filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
            .suffix(max(0, tailLines))
        let tail = Array(lines)
        guard !tail.isEmpty else { return .none }

        if assistant == .codex {
            guard let caret = tail.lastIndex(where: hasMenuCaret),
                  let selected = menuOption(tail[caret]), selected.caret else { return .none }
            var count = 1
            var before = caret - 1
            while before >= 0, menuOption(tail[before]) != nil { count += 1; before -= 1 }
            var after = caret + 1
            while after < tail.count, menuOption(tail[after]) != nil { count += 1; after += 1 }
            return count >= 2 ? .waiting : .none
        }

        guard let caret = tail.lastIndex(where: hasMenuCaret),
              let selected = menuOption(tail[caret]), selected.caret else {
            return hasFramedIndentedCaret(tail) ? .ambiguous : .none
        }
        var count = 1
        var before = caret - 1
        while before >= 0, menuOption(tail[before]) != nil { count += 1; before -= 1 }
        var after = caret + 1
        while after < tail.count, menuOption(tail[after]) != nil { count += 1; after += 1 }
        guard count >= 2 else {
            return hasFramedIndentedCaret(tail) ? .ambiguous : .none
        }
        if selected.indented { return .waiting }
        if selected.caret { return .ambiguous }
        return hasFramedIndentedCaret(tail) ? .ambiguous : .none
    }

    private static func menuOption(_ raw: String) -> (caret: Bool, indented: Bool)? {
        let chars = Array(raw)
        var i = 0
        while i < chars.count,
              chars[i] == " " || chars[i] == "\t" || menuBoxes.contains(chars[i]) { i += 1 }
        let indented = i > 0
        var caret = false
        if i < chars.count, menuCarets.contains(chars[i]) {
            caret = true; i += 1
            while i < chars.count, chars[i] == " " { i += 1 }
        }
        let digits = i
        while i < chars.count, ("0"..."9").contains(chars[i]) { i += 1 }
        guard i > digits, i < chars.count, chars[i] == "." else { return nil }
        i += 1
        guard i < chars.count, chars[i] == " " else { return nil }
        while i < chars.count, chars[i] == " " { i += 1 }
        return i < chars.count ? (caret, indented) : nil
    }

    private static func hasMenuCaret(_ raw: String) -> Bool {
        for character in raw {
            if character == " " || character == "\t" || menuBoxes.contains(character) { continue }
            return menuCarets.contains(character)
        }
        return false
    }

    private static func hasFramedIndentedCaret(_ lines: [String]) -> Bool {
        let framed = lines.contains { line in
            var horizontal = false
            for character in line {
                if character == " " || character == "\t" || menuBoxes.contains(character) { continue }
                if horizontalRules.contains(character) { horizontal = true; continue }
                if "┌┐└┘├┤┬┴┼╭╮╰╯".contains(character) { continue }
                return false
            }
            return horizontal
        }
        guard framed else { return false }
        return lines.contains { line in
            let chars = Array(line)
            var i = 0
            while i < chars.count,
                  chars[i] == " " || chars[i] == "\t" || menuBoxes.contains(chars[i]) { i += 1 }
            return i > 0 && i < chars.count && menuCarets.contains(chars[i])
        }
    }

    /// The transient line either provider draws while a turn is running.
    public static func workingLine(
        in screen: String, assistant: Assistant, tailLines: Int = 25
    ) -> String? {
        // tmux `capture-pane -e` puts SGR before the visible glyph. Strip controls here so every
        // caller, including the Linux batch reader, applies the same shape boundary.
        let lines = plain(screen)
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }

        for line in lines.suffix(max(0, tailLines)).reversed() {
            guard let glyph = line.first else { continue }
            let rest = String(line.dropFirst()).trimmingCharacters(in: .whitespaces)
            switch assistant {
            case .claude:
                guard claudeSpinners.contains(glyph), rest.contains("…"),
                      elapsed(in: rest) != nil else { continue }
                return rest
            case .codex:
                guard codexBullets.contains(glyph), elapsed(in: rest) != nil else { continue }
                guard let close = rest.firstIndex(of: ")") else { return rest }
                return String(rest[rest.startIndex...close])
            }
        }
        return nil
    }

    /// The provider's own elapsed clock, in seconds.
    public static func elapsed(in text: String) -> TimeInterval? {
        let chars = Array(text)
        var i = 0
        while i < chars.count {
            guard chars[i] == "(" else { i += 1; continue }
            var j = i + 1
            var total = 0
            while j < chars.count, chars[j] != ")" {
                guard chars[j].isNumber else { j += 1; continue }
                let start = j
                while j < chars.count, chars[j].isNumber { j += 1 }
                guard let value = Int(String(chars[start..<j])), j < chars.count else {
                    j = max(j, start + 1)
                    continue
                }
                // The unit must immediately follow its number. Without that boundary `(3 stages)`
                // would look like three seconds. Unsupported units are skipped with explicit
                // cursor progress so malformed provider text can never hold this parser.
                switch chars[j] {
                case "h": total += value * 3_600
                case "m": total += value * 60
                case "s": return TimeInterval(total + value)
                default: j += 1; continue
                }
                j += 1
            }
            i += 1
        }
        return nil
    }

    /// Remove terminal controls before a capture is used as shape or plain transcript text.
    /// CSI and OSC are consumed as closed sequences; unterminated controls consume the remainder
    /// rather than letting an attacker-controlled title or style payload become visible prose.
    public static func plain(_ text: String) -> String {
        // Swift treats CRLF as one extended grapheme cluster. Normalise line endings before the
        // Character scanner so filtering C0 controls cannot collapse `a\r\nb` into `ab`.
        let normalized = text.replacingOccurrences(of: "\r\n", with: "\n")
            .replacingOccurrences(of: "\r", with: "\n")
        guard normalized.contains("\u{1b}") || normalized.unicodeScalars.contains(where: {
            $0.value < 0x20 && $0 != "\n" && $0 != "\t"
        }) else { return normalized }
        let chars = Array(normalized)
        var output = ""
        output.reserveCapacity(text.count)
        var i = 0
        while i < chars.count {
            let character = chars[i]
            if character == "\u{1b}" {
                guard i + 1 < chars.count else { break }
                if chars[i + 1] == "[" {
                    var j = i + 2
                    while j < chars.count, !("@"..."~").contains(chars[j]) { j += 1 }
                    i = min(j + 1, chars.count)
                } else if chars[i + 1] == "]" {
                    var j = i + 2
                    while j < chars.count {
                        if chars[j] == "\u{07}" { j += 1; break }
                        if chars[j] == "\u{1b}", j + 1 < chars.count, chars[j + 1] == "\\" {
                            j += 2
                            break
                        }
                        j += 1
                    }
                    i = j
                } else {
                    i += 2
                }
                continue
            }
            if character == "\n" || character == "\t" || character.unicodeScalars.allSatisfy({
                $0.value >= 0x20 && $0.value != 0x7f
            }) {
                output.append(character)
            }
            i += 1
        }
        return output
    }

    /// Terminal row count: a final line terminator does not invent another display row.
    public static func lineCount(_ text: String) -> Int {
        guard !text.isEmpty else { return 0 }
        let count = text.split(separator: "\n", omittingEmptySubsequences: false).count
        return text.hasSuffix("\n") ? max(0, count - 1) : count
    }
}

/// Shared tmux batch framing. The marker is a C0 byte sequence that terminal-grid content cannot
/// manufacture; pane ids are closed ASCII words because they are interpolated into a tmux script.
public enum TmuxBatchedCapture {
    public static let marker = "\u{1}clawdline-pane\u{1}"

    public static func paneID(_ raw: String) -> String? {
        guard raw.first == "%", raw.count <= 16 else { return nil }
        let digits = raw.dropFirst()
        guard !digits.isEmpty, digits.allSatisfy({ ("0"..."9").contains($0) }) else { return nil }
        return raw
    }

    public static func script(_ paneIDs: [String], scrollback: Int = 0) -> String {
        var seen: Set<String> = []
        let lines = paneIDs.compactMap(paneID).filter { seen.insert($0).inserted }.flatMap { id in
            ["display-message -p -t \(id) \"\(marker)#{pane_id}\(marker)\"",
             "capture-pane -p -e -J -S -\(scrollback) -t \(id)"]
        }
        return lines.isEmpty ? "" : lines.joined(separator: "\n") + "\n"
    }

    public static func parse(_ output: String) -> [String: String] {
        var screens: [String: String] = [:]
        var current: String?
        var lines: [String] = []
        func close() {
            defer { current = nil; lines = [] }
            guard let id = current, paneID(id) != nil, !lines.isEmpty,
                  screens[id] == nil else { return }
            screens[id] = lines.joined(separator: "\n") + "\n"
        }
        var text = output
        if text.hasSuffix("\n") { text.removeLast() }
        let width = marker.count
        for line in text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            if line.count >= 2 * width, line.hasPrefix(marker), line.hasSuffix(marker) {
                close()
                current = String(line.dropFirst(width).dropLast(width))
            } else if current != nil {
                lines.append(line)
            }
        }
        close()
        return screens
    }
}
