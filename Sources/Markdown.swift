import AppKit

/// Just enough Markdown to read a Claude Code answer.
///
/// Not a general renderer — the input is one known kind of document, so the goal is that
/// nothing in a normal answer shows up as punctuation. Anything unrecognised falls through as
/// plain text rather than being swallowed, which is the failure mode that matters: a stray
/// asterisk is a blemish, a disappeared sentence is a bug.
///
/// Tables keep their pipes and are set in monospace. Laying them out properly would mean
/// NSTextTable and measuring every cell, and the pipes already align once the face is fixed —
/// which is the whole job.
enum Markdown {

    struct Theme {
        var body: NSFont
        var mono: NSFont
        var text: NSColor
        var dim: NSColor
        var accent: NSColor
        /// Inline code is coloured rather than given a ground, which is what the terminal does
        /// and what a chip cannot do safely: a background attribute is painted across a line's
        /// trailing whitespace to the margin, so a chip that breaks at a space leaves a bar.
        var code: NSColor
        var codeBackground: NSColor
        var ruleColor: NSColor
    }

    // MARK: - Blocks

    static func render(_ source: String, theme: Theme) -> NSAttributedString {
        let out = NSMutableAttributedString()
        let lines = source.components(separatedBy: "\n")
        var i = 0

        while i < lines.count {
            let line = lines[i]
            let trimmed = line.trimmingCharacters(in: .whitespaces)

            // Fenced code. The opening line may carry a language tag, which is not content.
            if trimmed.hasPrefix("```") {
                var body: [String] = []
                i += 1
                while i < lines.count, !lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                    body.append(lines[i]); i += 1
                }
                i += 1
                appendCode(body.joined(separator: "\n"), to: out, theme: theme)
                continue
            }

            if trimmed.isEmpty { i += 1; continue }

            if isRule(trimmed) {
                appendRule(to: out, theme: theme)
                i += 1
                continue
            }

            if let (level, text) = heading(trimmed) {
                appendHeading(text, level: level, to: out, theme: theme)
                i += 1
                continue
            }

            // A table is a run of lines containing pipes. Detected as a block rather than a
            // line so the separator row does not end up as a paragraph of dashes.
            if looksLikeTableRow(trimmed), i + 1 < lines.count,
               isTableSeparator(lines[i + 1].trimmingCharacters(in: .whitespaces)) {
                var rows: [String] = []
                while i < lines.count, looksLikeTableRow(lines[i].trimmingCharacters(in: .whitespaces)) {
                    rows.append(lines[i].trimmingCharacters(in: .whitespaces)); i += 1
                }
                appendTable(rows, to: out, theme: theme)
                continue
            }

            if trimmed.hasPrefix(">") {
                var quoted: [String] = []
                while i < lines.count, lines[i].trimmingCharacters(in: .whitespaces).hasPrefix(">") {
                    var q = lines[i].trimmingCharacters(in: .whitespaces)
                    q.removeFirst()
                    quoted.append(q.trimmingCharacters(in: .whitespaces)); i += 1
                }
                appendQuote(quoted.joined(separator: " "), to: out, theme: theme)
                continue
            }

            if let (marker, text) = listItem(line) {
                appendListItem(marker, text, to: out, theme: theme)
                i += 1
                continue
            }

            // Paragraph: gather until something else starts.
            var paragraph = [trimmed]
            i += 1
            while i < lines.count {
                let next = lines[i].trimmingCharacters(in: .whitespaces)
                if next.isEmpty || next.hasPrefix("```") || next.hasPrefix(">")
                    || heading(next) != nil || listItem(lines[i]) != nil
                    || isRule(next) || looksLikeTableRow(next) { break }
                paragraph.append(next); i += 1
            }
            appendParagraph(paragraph.joined(separator: " "), to: out, theme: theme)
        }
        return out
    }

    // MARK: - Block recognisers

    private static func heading(_ line: String) -> (Int, String)? {
        var level = 0
        var rest = Substring(line)
        while rest.first == "#", level < 6 { level += 1; rest = rest.dropFirst() }
        guard level > 0, rest.first == " " else { return nil }
        return (level, rest.trimmingCharacters(in: .whitespaces))
    }

    private static func listItem(_ line: String) -> (String, String)? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        for bullet in ["- ", "* ", "+ "] where trimmed.hasPrefix(bullet) {
            return ("•", String(trimmed.dropFirst(2)))
        }
        // "1. " through "999. "
        let digits = trimmed.prefix(while: \.isNumber)
        if !digits.isEmpty, digits.count <= 3 {
            let after = trimmed.dropFirst(digits.count)
            if after.hasPrefix(". ") { return (digits + ".", String(after.dropFirst(2))) }
        }
        return nil
    }

    private static func isRule(_ line: String) -> Bool {
        guard line.count >= 3 else { return false }
        return Set(line) == ["-"] || Set(line) == ["_"] || Set(line) == ["*"]
    }

    private static func looksLikeTableRow(_ line: String) -> Bool {
        line.contains("|") && line.filter { $0 == "|" }.count >= 2
    }

    private static func isTableSeparator(_ line: String) -> Bool {
        guard looksLikeTableRow(line) else { return false }
        return line.allSatisfy { "|-: ".contains($0) }
    }

    /// The cells of one table row. Leading and trailing pipes are borders, not empty cells.
    static func cells(_ row: String) -> [String] {
        var parts = row.split(separator: "|", omittingEmptySubsequences: false).map(String.init)
        if parts.first?.trimmingCharacters(in: .whitespaces).isEmpty == true { parts.removeFirst() }
        if parts.last?.trimmingCharacters(in: .whitespaces).isEmpty == true { parts.removeLast() }
        return parts.map { $0.trimmingCharacters(in: .whitespaces) }
    }

    // MARK: - Block rendering

    private static func paragraphStyle(spacingBefore: CGFloat = 0, spacing: CGFloat = 9,
                                       indent: CGFloat = 0, firstIndent: CGFloat = 0,
                                       lineSpacing: CGFloat = 3.5) -> NSMutableParagraphStyle {
        let p = NSMutableParagraphStyle()
        p.paragraphSpacingBefore = spacingBefore
        p.paragraphSpacing = spacing
        p.headIndent = indent
        p.firstLineHeadIndent = firstIndent
        p.lineSpacing = lineSpacing
        return p
    }

    private static func appendParagraph(_ text: String, to out: NSMutableAttributedString, theme: Theme) {
        out.append(inline(text, theme: theme, style: paragraphStyle()))
        out.append(NSAttributedString(string: "\n"))
    }

    private static func appendHeading(_ text: String, level: Int,
                                      to out: NSMutableAttributedString, theme: Theme) {
        let bump: CGFloat = [0, 5.5, 3, 1.5, 0.5, 0, 0][min(level, 6)]
        let weight: NSFont.Weight = level <= 2 ? .bold : .semibold
        let font = NSFont.systemFont(ofSize: theme.body.pointSize + bump, weight: weight)
        let style = paragraphStyle(spacingBefore: level <= 2 ? 18 : 13, spacing: 5, lineSpacing: 1)
        var theme2 = theme
        theme2.body = font
        let run = NSMutableAttributedString(attributedString: inline(text, theme: theme2, style: style))
        // A heading is a heading whatever the inline markup said.
        run.addAttributes([.font: font, .foregroundColor: theme.text],
                          range: NSRange(location: 0, length: run.length))
        run.addAttribute(.paragraphStyle, value: style, range: NSRange(location: 0, length: run.length))
        out.append(run)
        out.append(NSAttributedString(string: "\n"))
    }

    private static func appendListItem(_ marker: String, _ text: String,
                                       to out: NSMutableAttributedString, theme: Theme) {
        let indent: CGFloat = marker == "•" ? 15 : 22
        let style = paragraphStyle(spacing: 4, indent: indent, firstIndent: 2, lineSpacing: 3)
        out.append(NSAttributedString(string: marker + "\t", attributes: [
            .font: theme.body,
            .foregroundColor: marker == "•" ? theme.accent : theme.dim,
            .paragraphStyle: style,
        ]))
        out.append(inline(text, theme: theme, style: style))
        out.append(NSAttributedString(string: "\n"))
    }

    private static func appendQuote(_ text: String, to out: NSMutableAttributedString, theme: Theme) {
        // A bar, not just dimmer text: an indent alone reads as a stray paragraph, and the one
        // thing a quote has to say is that someone else said it.
        let style = paragraphStyle(spacingBefore: 6, spacing: 9, indent: 22, firstIndent: 8)
        out.append(NSAttributedString(string: "▍ ", attributes: [
            .font: theme.body,
            .foregroundColor: theme.accent.withAlphaComponent(0.65),
            .paragraphStyle: style,
        ]))
        var quoted = theme
        quoted.text = theme.dim
        let run = NSMutableAttributedString(attributedString: inline(text, theme: quoted, style: style))
        run.addAttribute(.foregroundColor, value: theme.dim,
                         range: NSRange(location: 0, length: run.length))
        out.append(run)
        out.append(NSAttributedString(string: "\n"))
    }

    private static func appendCode(_ code: String, to out: NSMutableAttributedString, theme: Theme) {
        let trimmed = code.trimmingCharacters(in: .newlines)
        guard !trimmed.isEmpty else { return }
        // Every newline starts a paragraph, so block spacing has to sit on the outside two lines
        // only. Put it on all of them and a four-line shell snippet reads as four snippets.
        let lines = trimmed.components(separatedBy: "\n")
        for (i, line) in lines.enumerated() {
            let style = paragraphStyle(spacingBefore: i == 0 ? 8 : 0,
                                       spacing: i == lines.count - 1 ? 10 : 0,
                                       indent: 12, firstIndent: 12, lineSpacing: 2)
            out.append(NSAttributedString(string: line + "\n", attributes: [
                .font: theme.mono,
                .foregroundColor: theme.code,
                .backgroundColor: theme.codeBackground,
                .paragraphStyle: style,
            ]))
        }
    }

    /// Tables are drawn as real tables, with borders.
    ///
    /// Two simpler things were tried first and both fail on this content. Leaving the pipes in
    /// and setting the whole table in monospace does not align: the source is not padded, and a
    /// CJK glyph comes from a fallback face whose advance is not reliably twice the monospace
    /// one, so the pipes land somewhere different on every row. Measured tab stops do align,
    /// but a table with no rules reads as three loose columns of text.
    ///
    /// `NSTextTable` needs TextKit 1 — see the note where the pane's text view is built.
    private static func appendTable(_ rows: [String], to out: NSMutableAttributedString, theme: Theme) {
        let grid = rows.filter { !isTableSeparator($0) }.map { cells($0) }
        guard let columns = grid.map(\.count).max(), columns > 0 else { return }

        let table = NSTextTable()
        table.numberOfColumns = columns
        table.layoutAlgorithm = .automaticLayoutAlgorithm
        table.collapsesBorders = true
        table.hidesEmptyCells = false

        for (r, row) in grid.enumerated() {
            for c in 0..<columns {
                let block = NSTextTableBlock(table: table, startingRow: r, rowSpan: 1,
                                             startingColumn: c, columnSpan: 1)
                block.setBorderColor(theme.ruleColor.withAlphaComponent(0.45))
                block.setWidth(1, type: .absoluteValueType, for: .border)
                block.setWidth(7, type: .absoluteValueType, for: .padding)
                if r == 0 { block.backgroundColor = theme.codeBackground.withAlphaComponent(0.12) }

                let style = NSMutableParagraphStyle()
                style.textBlocks = [block]
                style.lineSpacing = 2
                // Spacing here would push the rows apart inside their own borders.
                style.paragraphSpacing = 0
                style.paragraphSpacingBefore = 0

                let cell = c < row.count ? row[c] : ""
                let run = NSMutableAttributedString(
                    attributedString: inline(cell, theme: theme, style: style, bold: r == 0))
                run.addAttribute(.paragraphStyle, value: style,
                                 range: NSRange(location: 0, length: run.length))
                if r == 0 {
                    run.addAttribute(.foregroundColor, value: theme.dim,
                                     range: NSRange(location: 0, length: run.length))
                }
                out.append(run)
                out.append(NSAttributedString(string: "\n", attributes: [.paragraphStyle: style]))
            }
        }

        // A table block swallows the paragraph spacing of its own rows, so the air after the
        // table has to come from a paragraph that is outside it.
        out.append(NSAttributedString(string: "\n", attributes: [
            .font: NSFont.systemFont(ofSize: 4),
            .paragraphStyle: paragraphStyle(spacing: 6),
        ]))
    }

    private static func appendRule(to out: NSMutableAttributedString, theme: Theme) {
        let style = paragraphStyle(spacingBefore: 10, spacing: 12, lineSpacing: 0)
        out.append(NSAttributedString(string: String(repeating: "─", count: 40) + "\n", attributes: [
            .font: NSFont.monospacedSystemFont(ofSize: theme.body.pointSize * 0.7, weight: .regular),
            .foregroundColor: theme.ruleColor,
            .paragraphStyle: style,
        ]))
    }

    // MARK: - Inline

    /// `**bold**`, `*italic*`, `` `code` ``, `~~struck~~`, `[text](url)`.
    /// Unmatched markers stay as text: losing a sentence to a stray asterisk is far worse
    /// than showing the asterisk.
    static func inline(_ text: String, theme: Theme, style: NSParagraphStyle,
                       bold: Bool = false, italic: Bool = false) -> NSAttributedString {
        let out = NSMutableAttributedString()
        let chars = Array(text)
        var i = 0
        var plain = ""

        func face() -> NSFont {
            var f = theme.body
            let manager = NSFontManager.shared
            if bold { f = manager.convert(f, toHaveTrait: .boldFontMask) }
            if italic { f = manager.convert(f, toHaveTrait: .italicFontMask) }
            return f
        }

        func flush() {
            guard !plain.isEmpty else { return }
            out.append(NSAttributedString(string: plain, attributes: [
                .font: face(), .foregroundColor: theme.text, .paragraphStyle: style,
            ]))
            plain = ""
        }

        /// Index just past the closing marker, or nil if it never closes.
        func find(_ marker: [Character], from start: Int) -> Int? {
            var j = start
            while j + marker.count <= chars.count {
                if Array(chars[j..<(j + marker.count)]) == marker { return j }
                j += 1
            }
            return nil
        }

        while i < chars.count {
            let c = chars[i]

            if c == "`" , let close = find(["`"], from: i + 1) {
                flush()
                out.append(NSAttributedString(string: String(chars[(i + 1)..<close]), attributes: [
                    .font: theme.mono,
                    .foregroundColor: theme.code,
                    .paragraphStyle: style,
                ]))
                i = close + 1
                continue
            }

            if c == "*", i + 1 < chars.count, chars[i + 1] == "*",
               let close = find(["*", "*"], from: i + 2) {
                flush()
                out.append(inline(String(chars[(i + 2)..<close]), theme: theme, style: style,
                                  bold: true, italic: italic))
                i = close + 2
                continue
            }

            if c == "~", i + 1 < chars.count, chars[i + 1] == "~",
               let close = find(["~", "~"], from: i + 2) {
                flush()
                let inner = NSMutableAttributedString(
                    attributedString: inline(String(chars[(i + 2)..<close]), theme: theme,
                                             style: style, bold: bold, italic: italic))
                inner.addAttributes([.strikethroughStyle: NSUnderlineStyle.single.rawValue,
                                     .foregroundColor: theme.dim],
                                    range: NSRange(location: 0, length: inner.length))
                out.append(inner)
                i = close + 2
                continue
            }

            // Single * or _ for italic, but only when it is not sitting inside a word:
            // snake_case names are far more common here than emphasis.
            if c == "*" || c == "_" {
                let before = i > 0 ? chars[i - 1] : " "
                let opensWord = before == " " || before == "(" || i == 0
                if opensWord, let close = find([c], from: i + 1),
                   close > i + 1, chars[close - 1] != " " {
                    flush()
                    out.append(inline(String(chars[(i + 1)..<close]), theme: theme, style: style,
                                      bold: bold, italic: true))
                    i = close + 1
                    continue
                }
            }

            if c == "[", let closeBracket = find(["]"], from: i + 1),
               closeBracket + 1 < chars.count, chars[closeBracket + 1] == "(",
               let closeParen = find([")"], from: closeBracket + 2) {
                flush()
                let label = String(chars[(i + 1)..<closeBracket])
                let url = String(chars[(closeBracket + 2)..<closeParen])
                // Underlined here rather than by the text view: it styles every link the same
                // way, and the pane has other things that are links without being hyperlinks.
                out.append(NSAttributedString(string: label, attributes: [
                    .font: face(),
                    .foregroundColor: theme.accent,
                    .underlineStyle: NSUnderlineStyle.single.rawValue,
                    .link: url,
                    .paragraphStyle: style,
                ]))
                i = closeParen + 1
                continue
            }

            plain.append(c)
            i += 1
        }
        flush()
        return out
    }
}
