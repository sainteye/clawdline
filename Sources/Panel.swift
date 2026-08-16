import AppKit

// MARK: - Visual constants

enum Style {
    static let corner: CGFloat = 22
    static let padH: CGFloat = 24

    static let chevronW: CGFloat = 18
    static let chevronGap: CGFloat = 14

    static let inputMinHeight: CGFloat = 62
    static let inputPadV: CGFloat = 15
    static let textSize: CGFloat = 21
    static let maxTextHeight: CGFloat = 280

    static let hintHeight: CGFloat = 38
    static let hintSize: CGFloat = 11

    /// The output pane. Config can change it; the default is a reading height rather than a
    /// peek, because the pane exists to be read.
    static var outputHeight: CGFloat { Config.shared.outputHeight }
    static let outputSize: CGFloat = 11.5
    /// Falls back rather than failing: a font name that is not installed would otherwise
    /// leave the pane rendering in the system default at a different width.
    static var outputFont: NSFont {
        NSFont(name: Config.shared.outputFont, size: outputSize)
            ?? NSFont.monospacedSystemFont(ofSize: outputSize, weight: .regular)
    }

    static let rowHeight: CGFloat = 34
    static let listPadV: CGFloat = 8
    static let listSize: CGFloat = 13

    /// The mascot stands on the card's top edge with its feet sunk in a little, so it reads
    /// as standing rather than floating. The view is larger than the sprite on purpose: the
    /// jump (26pt), the pop scale (1.14x) and the sway all happen inside it.
    static let mascotW: CGFloat = 136
    static let mascotH: CGFloat = 124
    static let mascotFootInset: CGFloat = 6     // gap between the sprite's feet and the view bottom
    static let mascotOverlap: CGFloat = 3       // how far the feet sink into the card (more buries them)
    static let mascotTopPad: CGFloat = 12
    static let glowInset: CGFloat = 26

    static let accent = NSColor(srgbRed: 0.851, green: 0.467, blue: 0.341, alpha: 1)
    static let ink = NSColor(srgbRed: 0.08, green: 0.08, blue: 0.09, alpha: 1)
    static let hairline = NSColor(white: 1, alpha: 0.08)
    static let border = NSColor(white: 1, alpha: 0.14)
    static let topGloss = NSColor(white: 1, alpha: 0.10)
    static let chipFill = NSColor(white: 1, alpha: 0.07)
    static let chipEdge = NSColor(white: 1, alpha: 0.09)
    /// Darkens whatever is behind it, so it reads as an inset surface in either appearance.
    static let outputBg = NSColor(white: 0, alpha: 0.17)
}

// MARK: - Window

/// A borderless window cannot become key by default. Without this the caret never blinks and no keys arrive.
final class PromptPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { true }
}

/// The card's hairline border plus the highlight along its top edge. Frosted glass has no
/// edge of its own, and without these two strokes it dissolves into a light wallpaper.
final class CardChrome: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func draw(_ dirtyRect: NSRect) {
        let r = bounds.insetBy(dx: 0.5, dy: 0.5)
        let path = NSBezierPath(roundedRect: r, xRadius: Style.corner - 0.5, yRadius: Style.corner - 0.5)

        // Top highlight: light comes from above, and this one stroke gives the card thickness
        NSGraphicsContext.saveGraphicsState()
        path.addClip()
        let gloss = NSGradient(colors: [Style.topGloss, NSColor(white: 1, alpha: 0)])
        gloss?.draw(in: NSRect(x: 0, y: bounds.height - 26, width: bounds.width, height: 26), angle: -90)
        NSGraphicsContext.restoreGraphicsState()

        Style.border.setStroke()
        path.lineWidth = 1
        path.stroke()
    }
}

// MARK: - Mascot

/// The warm glow behind the mascot. A separate, larger view rather than part of the
/// mascot's own draw(): a view clips drawing to its bounds, so it would come out square.
final class GlowView: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    override func draw(_ dirtyRect: NSRect) {
        guard let g = NSGradient(colors: [
            Style.accent.withAlphaComponent(0.40),
            Style.accent.withAlphaComponent(0.13),
            Style.accent.withAlphaComponent(0),
        ]) else { return }
        let c = NSPoint(x: bounds.midX, y: bounds.midY)
        g.draw(fromCenter: c, radius: 0, toCenter: c, radius: bounds.width / 2, options: [])
    }
}

// MARK: - Key hints

/// The keycaps on the right of the hint row. Chips read faster than plain text: the eye
/// first counts how many things are pressable, then reads what they are.
final class KeyHintsView: NSView {
    struct Hint {
        let key: String
        let label: String
    }

    var hints: [Hint] = [] { didSet { needsDisplay = true } }

    private let keyFont = NSFont.systemFont(ofSize: Style.hintSize, weight: .medium)
    private let labelFont = NSFont.systemFont(ofSize: Style.hintSize, weight: .regular)
    private let chipPadH: CGFloat = 6
    private let gapKeyLabel: CGFloat = 5
    private let gapPairs: CGFloat = 13

    private func chipWidth(_ key: String) -> CGFloat {
        max(20, key.size(withAttributes: [.font: keyFont]).width + chipPadH * 2)
    }

    var intrinsicWidth: CGFloat {
        var w: CGFloat = 0
        for (i, h) in hints.enumerated() {
            if i > 0 { w += gapPairs }
            w += chipWidth(h.key)
            if !h.label.isEmpty {
                w += gapKeyLabel + h.label.size(withAttributes: [.font: labelFont]).width
            }
        }
        return w
    }

    override func draw(_ dirtyRect: NSRect) {
        var x = bounds.width - intrinsicWidth   // right-aligned
        let chipH: CGFloat = 17
        let cy = bounds.midY

        for (i, h) in hints.enumerated() {
            if i > 0 { x += gapPairs }

            let cw = chipWidth(h.key)
            let chip = NSRect(x: x, y: cy - chipH / 2, width: cw, height: chipH)
            let path = NSBezierPath(roundedRect: chip, xRadius: 5, yRadius: 5)
            Style.chipFill.setFill()
            path.fill()
            Style.chipEdge.setStroke()
            path.lineWidth = 1
            path.stroke()

            let kAttrs: [NSAttributedString.Key: Any] = [
                .font: keyFont, .foregroundColor: NSColor.secondaryLabelColor,
            ]
            let ks = h.key.size(withAttributes: kAttrs)
            h.key.draw(at: NSPoint(x: chip.midX - ks.width / 2, y: chip.midY - ks.height / 2), withAttributes: kAttrs)
            x += cw
            guard !h.label.isEmpty else { continue }
            x += gapKeyLabel

            let lAttrs: [NSAttributedString.Key: Any] = [
                .font: labelFont, .foregroundColor: NSColor.tertiaryLabelColor,
            ]
            let ls = h.label.size(withAttributes: lAttrs)
            h.label.draw(at: NSPoint(x: x, y: cy - ls.height / 2), withAttributes: lAttrs)
            x += ls.width
        }
    }
}

// MARK: - Input field

final class PromptTextView: NSTextView {
    var placeholder = L.t.placeholder

    var onSubmit: (() -> Void)?
    var onCancel: (() -> Void)?
    var onCycleTarget: ((Bool) -> Void)?
    var onToggleList: (() -> Void)?
    var onToggleMascots: (() -> Void)?
    var onToggleOutput: (() -> Void)?
    var onPickIndex: ((Int) -> Void)?
    var onToggleDance: (() -> Void)?
    var onArrow: ((Int) -> Bool)?      // return true if the key was consumed
    var onTextChanged: (() -> Void)?

    /// ⌘A / ⌘C / ⌘V / ⌘X / ⌘Z have to be wired by hand. This app is an accessory (no Dock icon,
    /// no menu bar), and macOS routes the standard edit commands through the main menu's key
    /// equivalents — with no menu, not one of them ever arrives.
    override func performKeyEquivalent(with event: NSEvent) -> Bool {
        let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        guard flags.contains(.command) else { return super.performKeyEquivalent(with: event) }

        let onlyCmd = flags == [.command]
        let cmdShift = flags == [.command, .shift]
        let key = event.charactersIgnoringModifiers?.lowercased() ?? ""

        if onlyCmd {
            switch key {
            case "a": selectAll(nil); return true
            case "c": copy(nil); return true
            case "v": paste(nil); return true
            case "x": cut(nil); return true
            case "z": undoManager?.undo(); return true
            case "k": onToggleList?(); return true
            case "m": onToggleMascots?(); return true
            case "j": onToggleOutput?(); return true
            case "d": onToggleDance?(); return true
            case "w": onCancel?(); return true
            case "\r": onSubmit?(); return true
            default:
                if let n = Int(key), n >= 1, n <= 9 { onPickIndex?(n - 1); return true }
            }
        }
        if cmdShift, key == "z" { undoManager?.redo(); return true }

        return super.performKeyEquivalent(with: event)
    }

    override func keyDown(with event: NSEvent) {
        // Hand every key back untouched while the IME is composing, or Enter mid-composition submits.
        if hasMarkedText() {
            super.keyDown(with: event)
            return
        }

        let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask)
        let shift = flags.contains(.shift)

        switch event.keyCode {
        case 36, 76:                                  // Return / Enter
            if shift { super.keyDown(with: event) } else { onSubmit?() }
            return
        case 53:                                      // Esc
            onCancel?()
            return
        case 48:                                      // Tab
            onCycleTarget?(!shift)
            return
        case 126:                                     // ↑
            if onArrow?(-1) == true { return }
        case 125:                                     // ↓
            if onArrow?(1) == true { return }
        default:
            break
        }

        super.keyDown(with: event)
        onTextChanged?()
    }

    override func didChangeText() {
        super.didChangeText()
        onTextChanged?()
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        guard string.isEmpty, let font = self.font else { return }
        let attrs: [NSAttributedString.Key: Any] = [
            .font: font,
            .foregroundColor: NSColor.tertiaryLabelColor,
        ]
        let x = textContainerInset.width + (textContainer?.lineFragmentPadding ?? 0)
        placeholder.draw(at: NSPoint(x: x, y: textContainerInset.height), withAttributes: attrs)
    }
}

// MARK: - One row of the session list

/// One row of whichever list is open — a session, or a mascot pack.
/// Both lists look and behave the same, so they share the row rather than the row
/// knowing what a session is.
final class TargetRow: NSView {
    let title: String
    let index: Int
    var isSelected = false { didSet { needsDisplay = true } }
    var onClick: (() -> Void)?

    init(title: String, index: Int) {
        self.title = title
        self.index = index
        super.init(frame: .zero)
    }
    required init?(coder: NSCoder) { fatalError() }

    override func mouseDown(with event: NSEvent) { onClick?() }

    override func draw(_ dirtyRect: NSRect) {
        if isSelected {
            let r = bounds.insetBy(dx: Style.padH - 10, dy: 2)
            Style.accent.withAlphaComponent(0.16).setFill()
            NSBezierPath(roundedRect: r, xRadius: 9, yRadius: 9).fill()

            // The short marker on the left reads as "this one" more clearly than a tinted background
            Style.accent.setFill()
            NSBezierPath(roundedRect: NSRect(x: r.minX + 6, y: bounds.midY - 7, width: 2.5, height: 14),
                         xRadius: 1.25, yRadius: 1.25).fill()
        }

        let badge = index < 9 ? "⌘\(index + 1)" : ""
        badge.draw(at: NSPoint(x: Style.padH + 4, y: bounds.midY - 7), withAttributes: [
            .font: NSFont.monospacedDigitSystemFont(ofSize: 10.5, weight: .medium),
            .foregroundColor: isSelected ? Style.accent : NSColor.tertiaryLabelColor,
        ])

        let text = NSMutableAttributedString(string: title, attributes: [
            .font: NSFont.systemFont(ofSize: Style.listSize, weight: isSelected ? .medium : .regular),
            .foregroundColor: isSelected ? NSColor.labelColor : NSColor.secondaryLabelColor,
        ])
        let x = Style.padH + 40
        text.draw(in: NSRect(x: x, y: bounds.midY - 9, width: bounds.width - x - Style.padH, height: 18))
    }
}
