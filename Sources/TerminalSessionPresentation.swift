import Foundation

/// Provider-neutral presentation facts derived from one complete terminal-grid capture.
///
/// A screen is evidence for a live activity line and for quietness only when the capture itself
/// completed. It is not a semantic transcript and, without a provider menu/hook signal, it is not
/// evidence that a person is being asked a question. Linux therefore publishes only working,
/// idle, or unknown from this reader; the Mac's richer `SessionState` keeps its independently
/// observed waiting evidence.
public enum TerminalSessionPresentation {
    public enum Observation: Equatable, Sendable {
        case working(String)
        case idle
        case unknown

        public var state: String {
            switch self {
            case .working: return "working"
            case .idle: return "idle"
            case .unknown: return "unknown"
            }
        }

        public var workState: String {
            switch self {
            case .working: return "working"
            case .idle: return "ready"
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
        "◐", "◑", "◒", "◓", "◴", "◵", "◶", "◷",
    ]
    private static let codexBullets: Set<Character> = ["•", "‣", "▪", "·", "*"]

    /// A missing capture is an unknown observation, never an idle answer.
    public static func observe(
        _ screen: String?, assistant: Assistant, tailLines: Int = 25
    ) -> Observation {
        guard let screen else { return .unknown }
        if let line = workingLine(in: screen, assistant: assistant, tailLines: tailLines) {
            return .working(line)
        }
        return .idle
    }

    /// The transient line either provider draws while a turn is running.
    public static func workingLine(
        in screen: String, assistant: Assistant, tailLines: Int = 25
    ) -> String? {
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
                guard let value = Int(String(chars[start..<j])), j < chars.count else { continue }
                switch chars[j] {
                case "h": total += value * 3_600
                case "m": total += value * 60
                case "s": return TimeInterval(total + value)
                default: continue
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
        guard text.contains("\u{1b}") || text.unicodeScalars.contains(where: {
            $0.value < 0x20 && $0 != "\n" && $0 != "\t"
        }) else { return text }
        let chars = Array(text)
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
