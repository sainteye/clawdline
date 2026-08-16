import AppKit

/// Turning terminal escape sequences into attributed text.
///
/// Only tmux can supply these: `capture-pane -e` keeps them. iTerm2's scripting hands over a
/// plain string — it will tell you which red it uses for ANSI red, but not which characters
/// are red — so that path arrives here with nothing to parse and passes straight through.
///
/// Everything that is not SGR (colour and weight) is dropped rather than rendered. Cursor
/// moves and clears describe changes to a grid, and this is a text view, not a grid.
enum Ansi {

    /// The 16 base colours. Values follow the usual xterm palette, lifted a little so the
    /// darker ones stay legible on a translucent card.
    private static let palette: [NSColor] = [
        NSColor(srgbRed: 0.30, green: 0.30, blue: 0.32, alpha: 1),   // 0 black
        NSColor(srgbRed: 0.84, green: 0.35, blue: 0.32, alpha: 1),   // 1 red
        NSColor(srgbRed: 0.45, green: 0.72, blue: 0.44, alpha: 1),   // 2 green
        NSColor(srgbRed: 0.85, green: 0.68, blue: 0.34, alpha: 1),   // 3 yellow
        NSColor(srgbRed: 0.40, green: 0.60, blue: 0.86, alpha: 1),   // 4 blue
        NSColor(srgbRed: 0.72, green: 0.50, blue: 0.82, alpha: 1),   // 5 magenta
        NSColor(srgbRed: 0.36, green: 0.72, blue: 0.72, alpha: 1),   // 6 cyan
        NSColor(srgbRed: 0.80, green: 0.80, blue: 0.79, alpha: 1),   // 7 white
        NSColor(srgbRed: 0.48, green: 0.48, blue: 0.50, alpha: 1),   // 8 bright black
        NSColor(srgbRed: 0.93, green: 0.47, blue: 0.44, alpha: 1),   // 9
        NSColor(srgbRed: 0.56, green: 0.84, blue: 0.55, alpha: 1),   // 10
        NSColor(srgbRed: 0.95, green: 0.80, blue: 0.45, alpha: 1),   // 11
        NSColor(srgbRed: 0.52, green: 0.72, blue: 0.95, alpha: 1),   // 12
        NSColor(srgbRed: 0.84, green: 0.62, blue: 0.92, alpha: 1),   // 13
        NSColor(srgbRed: 0.48, green: 0.85, blue: 0.85, alpha: 1),   // 14
        NSColor(srgbRed: 0.95, green: 0.95, blue: 0.94, alpha: 1),   // 15
    ]

    /// xterm's 256-colour cube and greyscale ramp.
    private static func indexed(_ n: Int) -> NSColor {
        if n < 16 { return palette[n] }
        if n < 232 {
            let i = n - 16
            let steps: [CGFloat] = [0, 0.373, 0.525, 0.671, 0.816, 1]
            return NSColor(srgbRed: steps[(i / 36) % 6],
                           green: steps[(i / 6) % 6],
                           blue: steps[i % 6], alpha: 1)
        }
        let g = CGFloat(n - 232) / 23
        return NSColor(srgbRed: g, green: g, blue: g, alpha: 1)
    }

    private struct Style {
        var color: NSColor?
        var bold = false
        var faint = false
    }

    /// True when there is anything here worth parsing.
    static func hasEscapes(_ text: String) -> Bool { text.contains("\u{1b}") }

    static func attributed(_ text: String, font: NSFont, defaultColor: NSColor) -> NSAttributedString {
        let bold = NSFontManager.shared.convert(font, toHaveTrait: .boldFontMask)
        let out = NSMutableAttributedString()
        var style = Style()

        func flush(_ run: String) {
            guard !run.isEmpty else { return }
            var attrs: [NSAttributedString.Key: Any] = [
                .font: style.bold ? bold : font,
                .foregroundColor: style.color ?? defaultColor,
            ]
            if style.faint {
                attrs[.foregroundColor] = (style.color ?? defaultColor).withAlphaComponent(0.55)
            }
            out.append(NSAttributedString(string: run, attributes: attrs))
        }

        var run = ""
        let chars = Array(text)
        var i = 0
        while i < chars.count {
            guard chars[i] == "\u{1b}", i + 1 < chars.count else {
                run.append(chars[i]); i += 1; continue
            }
            // CSI … final-byte. Anything else (OSC titles, single-character escapes) is
            // skipped to its terminator rather than printed as gibberish.
            if chars[i + 1] == "[" {
                var j = i + 2
                var params = ""
                while j < chars.count, !("@"..."~").contains(chars[j]) {
                    params.append(chars[j]); j += 1
                }
                let final = j < chars.count ? chars[j] : "m"
                if final == "m" {
                    flush(run); run = ""
                    apply(params, to: &style)
                }
                i = j + 1
            } else if chars[i + 1] == "]" {
                // OSC: runs until BEL or ST.
                var j = i + 2
                while j < chars.count, chars[j] != "\u{07}" {
                    if chars[j] == "\u{1b}", j + 1 < chars.count, chars[j + 1] == "\\" { j += 1; break }
                    j += 1
                }
                i = j + 1
            } else {
                i += 2
            }
        }
        flush(run)
        return out
    }

    private static func apply(_ params: String, to style: inout Style) {
        let codes = params.split(separator: ";", omittingEmptySubsequences: false)
            .map { Int($0) ?? 0 }
        var k = 0
        // An empty parameter list means reset, which is why "\u{1b}[m" has to behave like 0.
        if codes.isEmpty { style = Style(); return }
        while k < codes.count {
            switch codes[k] {
            case 0: style = Style()
            case 1: style.bold = true
            case 2: style.faint = true
            case 22: style.bold = false; style.faint = false
            case 30...37: style.color = palette[codes[k] - 30]
            case 90...97: style.color = palette[codes[k] - 90 + 8]
            case 39: style.color = nil
            case 38, 48:
                let isForeground = codes[k] == 38
                if k + 1 < codes.count, codes[k + 1] == 5, k + 2 < codes.count {
                    if isForeground { style.color = indexed(codes[k + 2]) }
                    k += 2
                } else if k + 1 < codes.count, codes[k + 1] == 2, k + 4 < codes.count {
                    if isForeground {
                        style.color = NSColor(srgbRed: CGFloat(codes[k + 2]) / 255,
                                              green: CGFloat(codes[k + 3]) / 255,
                                              blue: CGFloat(codes[k + 4]) / 255, alpha: 1)
                    }
                    k += 4
                }
            default: break   // backgrounds and everything else are left to the card
            }
            k += 1
        }
    }
}
