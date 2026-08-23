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
    static var outputSize: CGFloat { Config.shared.outputSize }
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
    /// Inline code in the transcript. Blue rather than the accent, so a `path` and a link stay
    /// distinguishable — both are set apart from prose, but they do different things.
    static let code = NSColor(srgbRed: 0.55, green: 0.73, blue: 0.98, alpha: 1)
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
    /// Lit while a file is being dragged over the window, because a drop with no answer until
    /// you let go is a guess.
    var highlighted = false { didSet { if oldValue != highlighted { needsDisplay = true } } }

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

        (highlighted ? Style.accent : Style.border).setStroke()
        path.lineWidth = highlighted ? 2 : 1
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
    /// Clicking the row is the other way to open it, for people who do not know the key yet —
    /// which is everyone, the first time.
    var onClick: (() -> Void)?

    override func mouseDown(with event: NSEvent) { onClick?() }
    override func resetCursorRects() {
        addCursorRect(bounds, cursor: onClick == nil ? .arrow : .pointingHand)
    }

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

/// The microphone, and what it does while it is listening.
///
/// A halo that answers your voice rather than a spinner that runs on its own: a spinner says the
/// app is busy, and what you actually want to know is whether it can hear you. The level comes
/// from the same audio buffers being transcribed, so a halo that will not move means the
/// microphone is picking up nothing — which is the failure you would otherwise discover by
/// reading an empty box afterwards.
///
/// Slow on purpose. The first version ran at 60fps with a ring leaving every third of a second,
/// which read as an alarm going off next to the thing you were trying to talk into. This one
/// breathes on a four-second cycle and follows your voice over about half a second: at a glance
/// it is a soft glow, and it is only when you watch it that it is obviously listening.
final class MicButton: NSView {
    var onClick: (() -> Void)?
    var isListening = false {
        didSet {
            guard oldValue != isListening else { return }
            isListening ? start() : stop()
            needsDisplay = true
        }
    }

    /// Newest reading. Smoothed towards on each frame, so the rings breathe instead of flicker.
    var level: Float = 0
    /// A second pass is armed. Drawn as one dot: this is a thing to notice once and then stop
    /// noticing, not a thing to read.
    var hasSecondPass = false { didSet { if oldValue != hasSecondPass { needsDisplay = true } } }
    /// Not listening any more, but not finished either: the recording is being read back.
    /// A different shape from the listening halo, because it is a different thing happening —
    /// an arc that goes round says "working", a halo that breathes says "I can hear you".
    var isThinking = false {
        didSet {
            guard oldValue != isThinking else { return }
            isThinking ? start() : stop()
            needsDisplay = true
        }
    }
    private var shown: CGFloat = 0
    private var phase: CGFloat = 0
    private var spin: CGFloat = 0
    private var timer: Timer?
    private var hovering = false
    private var tracking: NSTrackingArea?

    override func mouseDown(with event: NSEvent) { onClick?() }
    override func resetCursorRects() { addCursorRect(bounds, cursor: .pointingHand) }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let t = tracking { removeTrackingArea(t) }
        let t = NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeAlways],
                               owner: self, userInfo: nil)
        addTrackingArea(t)
        tracking = t
    }
    override func mouseEntered(with event: NSEvent) { hovering = true; needsDisplay = true }
    override func mouseExited(with event: NSEvent) { hovering = false; needsDisplay = true }

    private func start() {
        guard timer == nil else { return }
        spin = 0
        // 30fps, because nothing here moves fast enough to need more — and this sits in the
        // corner of a window that is open while you think.
        let t = Timer(timeInterval: 1.0 / 30.0, repeats: true) { [weak self] _ in
            guard let self else { return }
            // A slow follow: loud syllables should swell it, not make it flicker.
            self.shown += (CGFloat(self.level) - self.shown) * 0.06
            self.phase += 1.0 / (30.0 * 4.0)      // one breath every four seconds
            self.spin += 1.0 / (30.0 * 1.4)       // one turn every 1.4 seconds
            if self.spin > 1 { self.spin -= 1 }
            if self.phase > 1 { self.phase -= 1 }
            self.needsDisplay = true
        }
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    private func stop() {
        timer?.invalidate()
        timer = nil
        shown = 0
        phase = 0
    }

    override func draw(_ dirtyRect: NSRect) {
        let centre = NSPoint(x: bounds.midX, y: bounds.midY)

        if isThinking {
            // Three quarters of a circle, going round. Deliberately not a full ring: an arc
            // with a gap in it is the difference between "turning" and "sitting there".
            let r: CGFloat = 9
            let path = NSBezierPath()
            path.appendArc(withCenter: centre, radius: r,
                           startAngle: spin * 360, endAngle: spin * 360 + 260)
            path.lineWidth = 1.6
            path.lineCapStyle = .round
            Style.accent.withAlphaComponent(0.85).setStroke()
            path.stroke()
        }

        if isListening {
            // One breath, sine rather than sawtooth: a ring that restarts has an edge, and an
            // edge is what the eye keeps going back to.
            let breath = (sin(phase * 2 * .pi) + 1) / 2

            // The halo. Mostly your voice, a little the breath, so silence is not motionless
            // — it is just quiet.
            let r = 8 + shown * 4 + breath * 1.2
            let glow = NSGradient(colors: [
                Style.accent.withAlphaComponent(0.10 + shown * 0.22),
                Style.accent.withAlphaComponent(0),
            ])
            glow?.draw(in: NSRect(x: centre.x - r, y: centre.y - r, width: r * 2, height: r * 2),
                       relativeCenterPosition: .zero)

            // A single outer ring, faint enough to notice only when you look for it.
            let reach = r + 2 + breath * 2
            Style.accent.withAlphaComponent(0.05 + shown * 0.14).setStroke()
            let ring = NSBezierPath(ovalIn: NSRect(x: centre.x - reach, y: centre.y - reach,
                                                   width: reach * 2, height: reach * 2))
            ring.lineWidth = 1
            ring.stroke()
        }

        let name = isListening || isThinking ? "mic.fill" : "mic"
        guard let glyph = NSImage(systemSymbolName: name, accessibilityDescription: L.t.hintVoice)
        else { return }
        let config = NSImage.SymbolConfiguration(pointSize: 13, weight: .regular)
        let tinted = glyph.withSymbolConfiguration(config) ?? glyph
        let size = tinted.size
        let box = NSRect(x: centre.x - size.width / 2, y: centre.y - size.height / 2,
                         width: size.width, height: size.height)
        let colour: NSColor = isListening || isThinking ? Style.accent
            : (hovering ? .secondaryLabelColor : .tertiaryLabelColor)
        if hasSecondPass {
            let d: CGFloat = 3
            colour.withAlphaComponent(isListening ? 0.9 : 0.55).setFill()
            NSBezierPath(ovalIn: NSRect(x: box.maxX - d / 2, y: box.maxY - d,
                                        width: d, height: d)).fill()
        }
        // Tinted by asking for the colour, not by painting over the glyph afterwards.
        //
        // It used to draw the symbol and then `box.fill(using: .sourceAtop)`. That trick needs
        // the destination to be transparent everywhere the glyph is not — true on screen, where
        // each view has its own layer, and false in `clawdline://snapshot`, which renders the
        // whole tree into one bitmap that already has the card painted on it. So every frame of
        // every demo GIF had a solid orange square where the microphone should be, and nothing
        // about the running app ever showed it.
        let painted = glyph.withSymbolConfiguration(config.applying(
            NSImage.SymbolConfiguration(paletteColors: [colour]))) ?? tinted
        painted.draw(in: box, from: .zero, operation: .sourceOver, fraction: 1)
    }
}

/// The whole window as a drop target.
///
/// Dropping only onto the input box means aiming at a line of text, and by the time the pane is
/// open that line is a small part of what is on screen. Anywhere on the card is the same
/// intention, so it is the same result.
final class DropTargetView: NSView {
    var onDrop: (([String]) -> Void)?
    var onDragActive: ((Bool) -> Void)?

    func acceptDrops() { registerForDraggedTypes(Drop.acceptedTypes) }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        guard !Drop.paths(from: sender.draggingPasteboard).isEmpty else { return [] }
        onDragActive?(true)
        return .copy
    }

    override func draggingExited(_ sender: NSDraggingInfo?) { onDragActive?(false) }
    override func draggingEnded(_ sender: NSDraggingInfo) { onDragActive?(false) }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        onDragActive?(false)
        let paths = Drop.paths(from: sender.draggingPasteboard)
        guard !paths.isEmpty else { return false }
        onDrop?(paths)
        return true
    }
}

final class PromptTextView: NSTextView {
    var placeholder = L.t.placeholder
    private var droppedPaths: [ObjectIdentifier: String] = [:]

    /// Files landed in the box. The controller uses it to say what happened.
    var onDropped: ((Int) -> Void)?

    // MARK: - Files and images

    override func awakeFromNib() { super.awakeFromNib(); registerForDraggedTypes(Drop.acceptedTypes) }

    /// Registering has to happen once the view exists; this is the path the app actually takes,
    /// since the view is built in code rather than loaded from a nib.
    func acceptDrops() { registerForDraggedTypes(Drop.acceptedTypes) }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        Drop.paths(from: sender.draggingPasteboard).isEmpty
            ? super.draggingEntered(sender) : .copy
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        let paths = Drop.paths(from: sender.draggingPasteboard)
        guard !paths.isEmpty else { return super.performDragOperation(sender) }
        insertPaths(paths)
        return true
    }

    /// An image on the clipboard has no path, so pasting one writes a file and inserts that.
    /// Text on the clipboard is left entirely alone.
    override func paste(_ sender: Any?) {
        let paths = Drop.paths(from: .general)
        guard !paths.isEmpty, NSPasteboard.general.string(forType: .string) == nil else {
            super.paste(sender)
            return
        }
        insertPaths(paths)
    }

    /// What a file looks like in the box: a thumbnail, or the icon its type gets in Finder.
    ///
    /// The path is what will be **sent** — see `resolvedText()` — but a path is not what a person
    /// dropped. They dropped a picture, and forty characters of directory is a worse description
    /// of it than the picture is.
    func insertPaths(_ paths: [String]) {
        if let last = string.last, !last.isWhitespace {
            insertText(" ", replacementRange: selectedRange())
        }
        let run = NSMutableAttributedString()
        for path in paths {
            let attachment = NSTextAttachment()
            let thumbnail = Drop.thumbnail(for: path, height: 30)
            attachment.image = thumbnail
            // Sat on the baseline it hangs below the line like a dropped letter. Centre it on
            // the cap height instead, which is where the eye reads the middle of a line to be.
            let font = (baseAttributes[.font] as? NSFont) ?? NSFont.systemFont(ofSize: 13)
            attachment.bounds = NSRect(x: 0, y: font.capHeight / 2 - thumbnail.size.height / 2,
                                       width: thumbnail.size.width, height: thumbnail.size.height)
            droppedPaths[ObjectIdentifier(attachment)] = path
            run.append(NSAttributedString(attachment: attachment))
            run.append(NSAttributedString(string: " ", attributes: baseAttributes))
        }
        // Through insertText, not into the storage directly. Writing to the storage skips undo
        // registration, so ⌘Z undid whatever came *before* the drop: the box changed, the
        // attachment stayed, and the file went out with a message it had been taken out of.
        insertText(run, replacementRange: selectedRange())
        onDropped?(paths.count)
    }

    /// The text to send: every thumbnail turns back into the path it stands for.
    ///
    /// Kept apart from `string` on purpose. What is on screen is for the person, what this
    /// returns is for Claude Code, and the moment those two are the same string one of them is
    /// being made worse to suit the other.
    func resolvedText() -> String {
        resolvedPieces().map { piece in
            switch piece {
            case .text(let t): return t
            case .image(let path): return Drop.quoted(path)
            }
        }.joined()
    }

    /// The same thing, with the images still identifiable.
    ///
    /// `resolvedText()` flattens an attachment into its path, which is all a path-based send
    /// needs and everything a pasted one has to know. Built here rather than by searching the
    /// finished string for path-shaped substrings: the attachment already knows what it is, and
    /// a prompt that happens to mention a filename should not turn into a picture.
    func resolvedPieces() -> [Drop.Piece] {
        guard let storage = textStorage else { return string.isEmpty ? [] : [.text(string)] }
        var out: [Drop.Piece] = []
        func append(_ text: String) {
            guard !text.isEmpty else { return }
            if case .text(let last)? = out.last {
                out[out.count - 1] = .text(last + text)
            } else {
                out.append(.text(text))
            }
        }
        storage.enumerateAttributes(in: NSRange(location: 0, length: storage.length)) { attrs, range, _ in
            if let a = attrs[.attachment] as? NSTextAttachment,
               let path = droppedPaths[ObjectIdentifier(a)] {
                if Drop.isImage(path) { out.append(.image(path)) } else { append(Drop.quoted(path)) }
                return
            }
            append(storage.attributedSubstring(from: range).string)
        }
        return out
    }

    /// How typed text should look. Held here because rich text forgets: assigning `string`
    /// replaces the storage with an unattributed copy, and the view then falls back to a small
    /// black system font — which is what the box looked like the first time a thumbnail could
    /// go in it.
    var baseAttributes: [NSAttributedString.Key: Any] = [:] {
        didSet { typingAttributes = baseAttributes }
    }

    /// Replace everything in the box, keeping the way it looks. Use this rather than `string =`.
    func setPlainText(_ text: String) {
        textStorage?.setAttributedString(NSAttributedString(string: text,
                                                            attributes: baseAttributes))
        typingAttributes = baseAttributes
        didChangeText()
    }

    // MARK: - Dictation

    /// Where speech is currently writing. Nil when nothing is being dictated.
    private var dictationRange: NSRange?
    /// The exact text last written there, and the whole box as it stood afterwards. Both are
    /// needed: comparing only the dictated span misses an edit made anywhere else, and typing
    /// at the end is the commonest edit of all.
    private var lastDictated = ""
    private var lastBox = ""
    /// True while we are the ones writing, so our own edits are not mistaken for the user's.
    private var applyingDictation = false
    /// Called when the user edited what was dictated, so the speech side can stop counting the
    /// words it has already handed over.
    var onDictationDisplaced: (() -> Void)?

    /// The colour settled text has — what dictation fades from, and comes back to.
    private var settledColour: NSColor {
        (baseAttributes[.foregroundColor] as? NSColor) ?? textColor ?? .labelColor
    }

    /// Provisional text is the same colour, not yet fully arrived.
    ///
    /// It was an underline first, the mark macOS input methods have used for unsettled text for
    /// decades. That convention is real, but it only speaks to people who already know it, and a
    /// line under a sentence competes with the sentence for attention. Fading reads as "not all
    /// the way here" to anyone — and it puts the emphasis the right way round, because the words
    /// that have settled are the ones that look normal.
    ///
    /// Kept dynamic (`withAlphaComponent` rather than a resolved colour) so it still follows the
    /// system between light and dark.
    static func provisional(_ colour: NSColor) -> NSColor { colour.withAlphaComponent(0.45) }

    /// Marks the faded stretches as ours, so settling can find them by what they are rather than
    /// by where they were. The range is not enough on its own: text typed beside a provisional
    /// run inherits its colour and lands outside the range, which is how hand-typed words ended
    /// up half-erased with no way back.
    private static let provisionalKey = NSAttributedString.Key("clawdline.provisional")

    private var pendingAttributes: [NSAttributedString.Key: Any] {
        var attrs = baseAttributes
        attrs[.foregroundColor] = Self.provisional(settledColour)
        attrs[Self.provisionalKey] = true
        return attrs
    }

    /// Bring everything faded up to full: those words are final now.
    ///
    /// Sweeps by mark rather than by range. Typing after dictation came out faded and stayed
    /// faded for the rest of the message: AppKit refreshes `typingAttributes` from the text
    /// beside the caret whenever the selection moves, so parking the caret at the end of a
    /// provisional run makes the next thing typed provisional too — outside the dictated range,
    /// where settling by range never reached it. The words looked half-erased and the microphone
    /// was not even on.
    private func settle() {
        guard let storage = textStorage, storage.length > 0 else {
            typingAttributes = baseAttributes
            return
        }
        storage.enumerateAttribute(Self.provisionalKey,
                                   in: NSRange(location: 0, length: storage.length)) { value, found, _ in
            guard value != nil else { return }
            storage.removeAttribute(Self.provisionalKey, range: found)
            storage.addAttribute(.foregroundColor, value: settledColour, range: found)
        }
        // Or the next keystroke is provisional again, and we are back where we started.
        typingAttributes = baseAttributes
    }

    /// Start writing at the caret, keeping everything already in the box.
    ///
    /// At the caret rather than at the end, because that is where typing would have gone and
    /// speech is not a different kind of input. It used to append regardless, which meant going
    /// back to add a sentence in the middle put it at the bottom instead — and the machinery for
    /// this was already here: the mid-run path, the one that restarts after you edit something,
    /// has always resumed at the caret. Only the first range was pinned to the end.
    ///
    /// Anything selected is replaced, again because that is what typing does.
    ///
    /// Only this range is rewritten as the words arrive. Rebuilding the whole box from a string
    /// was simpler and wrong: the string form of a dropped image is its path, so dictating after
    /// dropping a screenshot turned the thumbnail into forty characters of directory.
    func beginDictation() {
        guard let storage = textStorage else { return }
        var at = NSRange(location: min(selectedRange().location, storage.length), length: 0)
        let selected = selectedRange()
        if selected.length > 0, selected.location + selected.length <= storage.length {
            insertText("", replacementRange: selected)
            at = NSRange(location: selected.location, length: 0)
        }
        // Keep speech from fusing onto the word in front of it. Judged from the character before
        // the caret rather than the last one in the box, which are the same thing only when the
        // caret is at the end.
        if at.location > 0 {
            let before = (storage.string as NSString)
                .substring(with: NSRange(location: at.location - 1, length: 1))
            if !(before.first?.isWhitespace ?? true) {
                insertText(" ", replacementRange: NSRange(location: at.location, length: 0))
                at.location += 1
            }
        }
        dictationRange = at
        lastDictated = ""
        lastBox = string
    }

    /// Replace what has been dictated so far with the latest version of it.
    ///
    /// Unless you have edited it in the meantime. Then what is there is yours, speech starts a
    /// fresh run at the caret, and the words already said stop being re-sent — otherwise fixing
    /// a typo mid-sentence made the next few words land in the middle of the old ones.
    func updateDictation(_ text: String) {
        guard let storage = textStorage, let range = dictationRange else { return }
        var safe = NSRange(location: min(range.location, storage.length),
                           length: min(range.length, max(0, storage.length - range.location)))
        if storage.string != lastBox || storage.attributedSubstring(from: safe).string != lastDictated {
            // Edited by hand: those words are the user's now, so they stop being provisional.
            settle()
            safe = NSRange(location: min(selectedRange().location, storage.length), length: 0)
            lastDictated = ""
            onDictationDisplaced?()
        }
        let run = NSAttributedString(string: text, attributes: pendingAttributes)
        guard shouldChangeText(in: safe, replacementString: text) else { return }
        applyingDictation = true
        storage.replaceCharacters(in: safe, with: run)
        didChangeText()
        applyingDictation = false
        dictationRange = NSRange(location: safe.location, length: run.length)
        lastDictated = text
        lastBox = storage.string
        setSelectedRange(NSRange(location: safe.location + run.length, length: 0))
    }

    /// Notice an edit the moment it happens, not the next time speech arrives.
    ///
    /// Waiting cost the user the thing they had just deleted: they cleared the box, stopped, and
    /// the audio behind those words was still queued — so it came back, transcribed, into a box
    /// they had emptied on purpose.
    override func didChangeText() {
        super.didChangeText()
        guard !applyingDictation, let range = dictationRange, let storage = textStorage else {
            onTextChanged?()
            return
        }
        let stillOurs = range.location + range.length <= storage.length
            && storage.attributedSubstring(from: range).string == lastDictated
            && storage.string == lastBox
        if !stillOurs {
            settle()
            dictationRange = NSRange(location: min(selectedRange().location, storage.length),
                                     length: 0)
            lastDictated = ""
            lastBox = storage.string
            onDictationDisplaced?()
        }
        onTextChanged?()
    }

    func endDictation() {
        settle()
        dictationRange = nil
        lastDictated = ""
        lastBox = ""
    }

    /// Clearing the box has to clear these too, or a dropped file would follow the next message
    /// it was never part of.
    func clearText() {
        droppedPaths.removeAll()
        dictationRange = nil
        setPlainText("")
    }

    var onSubmit: (() -> Void)?
    var onCancel: (() -> Void)?
    var onCycleTarget: ((Bool) -> Void)?
    var onToggleList: (() -> Void)?
    var onToggleMascots: (() -> Void)?
    var onToggleStacks: (() -> Void)?
    /// ⌘⇧n — stopping, kept off the key you press by reflex. Starting and restarting are
    /// recoverable; taking a public tunnel down is the one you would rather not do by aiming
    /// badly. (There is a button for it too; this is for hands that stay on the keyboard.)
    var onStopIndex: ((Int) -> Void)?
    var onToggleOutput: (() -> Void)?
    var onZoomOutput: ((Int) -> Void)?   // +1 bigger, -1 smaller, 0 reset
    var onPickIndex: ((Int) -> Void)?
    var onToggleDance: (() -> Void)?
    var onArrow: ((Int) -> Bool)?      // return true if the key was consumed
    /// Tab or Return completes an open suggestion before they keep their ordinary meanings.
    /// `true` means there was one and the key is spent.
    var onAcceptSuggestion: (() -> Bool)?
    var onTextChanged: (() -> Void)?
    var onToggleFullscreen: (() -> Void)?
    var onToggleOrder: (() -> Void)?
    var onToggleKeys: (() -> Void)?
    var onToggleVoice: (() -> Void)?

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
            case "s": onToggleStacks?(); return true
            case "j": onToggleOutput?(); return true
            case "f": onToggleFullscreen?(); return true
            case "r": onToggleOrder?(); return true
            case "/": onToggleKeys?(); return true
            case "l": onToggleVoice?(); return true
            // "+" arrives as "=" without shift and "+" with it; both mean the same thing here.
            case "=", "+": onZoomOutput?(1); return true
            case "-", "_": onZoomOutput?(-1); return true
            case "0": onZoomOutput?(0); return true
            case "d": onToggleDance?(); return true
            case "w": onCancel?(); return true
            case "\r": onSubmit?(); return true
            default:
                if let n = Int(key), n >= 1, n <= 9 { onPickIndex?(n - 1); return true }
            }
        }
        if cmdShift, key == "z" { undoManager?.redo(); return true }
        // ⌘⇧n arrives with the shifted character ("!" for 1), so read the unshifted one.
        if cmdShift, let raw = event.characters(byApplyingModifiers: .init())?.lowercased(),
           let n = Int(raw), n >= 1, n <= 9 {
            onStopIndex?(n - 1)
            return true
        }

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
            if shift { super.keyDown(with: event) }
            else if onAcceptSuggestion?() != true { onSubmit?() }
            return
        case 53:                                      // Esc
            onCancel?()
            return
        case 48:                                      // Tab
            if onAcceptSuggestion?() != true { onCycleTarget?(!shift) }
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

// MARK: - Measuring a single line

/// Where the runs carrying a given attribute landed, in a string drawn as one line.
///
/// Both places that need this draw one line with no wrapping, so accumulated run widths *are*
/// the layout — which also means it behaves the same whether the text is drawn by hand or lives
/// in a TextKit 1 or TextKit 2 view. Asking a layout manager would work in exactly one of those
/// three cases.
enum TextZones {
    static func of(_ s: NSAttributedString, key: NSAttributedString.Key,
                   x0: CGFloat, height: CGFloat) -> [(rect: NSRect, value: String)] {
        guard s.length > 0 else { return [] }
        var out: [(NSRect, String)] = []
        var x = x0
        s.enumerateAttributes(in: NSRange(location: 0, length: s.length)) { attrs, range, _ in
            let width = s.attributedSubstring(from: range).size().width
            if let value = attrs[key] as? String, !value.isEmpty {
                out.append((NSRect(x: x, y: 0, width: width, height: height), value))
            }
            x += width
        }
        return out
    }
}

/// What a run of the footer means, in words, for anyone who has not yet learned the marks.
///
/// A separate key from `.link` because a thing can be both — the address a tunnel serves is
/// somewhere to go *and* something to explain — and because `.toolTip` is not an attribute
/// AppKit knows how to act on by itself.
extension NSAttributedString.Key {
    static let clawdlineTip = NSAttributedString.Key("clawdlineTip")
}

// MARK: - One row of the session list

/// One row of whichever list is open — a session, a mascot pack, or a project's dev stack.
/// The lists look and behave the same, so they share the row rather than the row
/// knowing what a session is.
/// A spinner made of pixels, because everything else here is: the mascot, the project marks,
/// the blocks a deploy fills up. A rotating glyph from a font would be the one thing on the card
/// that came from somewhere else.
///
/// Eight cells around a three-by-three ring with the middle empty, one head cell and a two-cell
/// tail behind it. **Dim on purpose** — a session that is working wants nothing from you, and a
/// list where five rows are all flashing is a list nobody can read. Motion at low contrast is
/// enough to say "alive"; brightness would be saying "look at me", which is the next state along
/// and needs to stay distinguishable.
enum PixelSpinner {

    static let cell: CGFloat = 3
    static let gap: CGFloat = 1.5
    static var size: CGFloat { cell * 3 + gap * 2 }
    /// One step every eighth of a second: a full turn takes a second, which is a heartbeat rather
    /// than a machine.
    static let step: Double = 0.125

    /// Clockwise from the top-left, skipping the middle.
    private static let ring: [(Int, Int)] = [(0, 0), (1, 0), (2, 0), (2, 1),
                                             (2, 2), (1, 2), (0, 2), (0, 1)]

    static func draw(at origin: NSPoint, time: Double, colour: NSColor) {
        let head = Int(time / step) % ring.count
        // The head, then two behind it, fading. Drawn tail-first so the head lands on top of any
        // rounding overlap.
        // Three behind the head rather than two: at this size the tail is what reads as rotation,
        // and with one cell lit it reads as a speck that happens to move.
        for (i, alpha) in [(3, 0.12), (2, 0.24), (1, 0.45), (0, 0.90)] {
            let (cx, cy) = ring[(head - i + ring.count * 2) % ring.count]
            colour.withAlphaComponent(CGFloat(alpha)).setFill()
            NSRect(x: origin.x + CGFloat(cx) * (cell + gap),
                   // Rows read top-down, the view's origin is at the bottom.
                   y: origin.y + CGFloat(2 - cy) * (cell + gap),
                   width: cell, height: cell).fill()
        }
    }
}

final class TargetRow: NSView {
    let title: String
    let index: Int
    /// Set instead of `title` when the row has colour in it — a green mark and a red one in the
    /// same line, which a plain string cannot carry.
    let rich: NSAttributedString?
    var isSelected = false { didSet { needsDisplay = true } }
    /// The project's own mark, drawn before the label.
    ///
    /// A tab title is the *task*, and two projects can be working on tasks that read alike —
    /// which is the whole reason the footer leads with this mark rather than with the title. The
    /// list had exactly the same problem and none of the answer.
    var icon: NSImage? { didSet { needsDisplay = true } }
    /// What the row says *after* its label — the live line, or that it is waiting. Separate from
    /// `rich` so the spinner can sit between the two and the row can lay all three out.
    var detail: NSAttributedString?
    /// Draw the spinner between the label and the detail.
    var isBusy = false
    /// Pin the spinner's phase, for a filmstrip: frames are rendered as fast as the machine can
    /// draw them, so a wall clock would give a strip whose spinner turns at whatever speed that
    /// happened to be.
    var spinnerTime: Double?
    var onClick: (() -> Void)?

    /// Somewhere to go, when part of the row is a place rather than a label — the port a server
    /// is on, the address a tunnel is serving. Set by putting `.link` on a run of `rich`.
    ///
    /// The row draws itself rather than being a text view, so `.link` does nothing on its own;
    /// these are the same runs, measured, so that clicking the words that name a place goes
    /// there. Without it the address is something you read and then retype.
    var onOpen: ((String) -> Void)?

    /// One of the row's buttons.
    ///
    /// **A row must not be an invisible button.** An earlier version made the whole row pressable
    /// and worked out what to do from the state, so a single click could take a public site down
    /// for a minute with nothing on screen having offered to — and afterwards the only evidence
    /// was a count going down. If a press does something, it says so first.
    struct Button {
        /// An SF Symbol. Start, stop and restart are the three actions every media control in
        /// the world has already taught, so the glyph reads faster than a word — and it is the
        /// same glyph in all twenty languages.
        var symbol: String?
        /// Drawn when there is no symbol, and used as the accessibility description either way.
        /// Actions with no universal picture — "allow" — keep their word.
        var label: String
        /// What the press will do, in words, and the command it will run. This is what keeps an
        /// icon from being a guess, and it is the difference between "restart" and a minute of
        /// downtime.
        var tip: String
        /// Pressed once and waiting to be asked again: the button lights up and the line below
        /// the list spells out the command. Anything that can take a live site off the air goes
        /// through this; starting something already stopped does not.
        var armed = false
    }

    /// Drawn right-aligned in order — the last one is the rightmost, and the rightmost is where
    /// the hand goes, so that is where the everyday action lives.
    ///
    /// An array rather than named slots because the set is not fixed: a stopped stack offers
    /// start, a running one offers restart and stop, and both offer their log.
    /// **Assigning these has to redraw**, and for a while it did not.
    ///
    /// A stack row is built the moment the panel opens, before anything is known about what the
    /// project is doing — and at that moment there are no buttons to offer, because which ones
    /// apply depends on whether it is running. The state arrives a beat later from a subprocess,
    /// the buttons are worked out and set here, and the row was never told. So it kept the
    /// picture it had drawn when it knew nothing: the text updated (that setter does invalidate),
    /// the buttons never appeared, and the panel looked like a thing you could read and not act
    /// on. The tool tips went the same way, which is why the tracking areas are rebuilt too.
    var buttons: [Button] = [] {
        didSet {
            needsDisplay = true
            updateTrackingAreas()
        }
    }
    var onButton: ((Int) -> Void)?

    init(title: String, index: Int, rich: NSAttributedString? = nil) {
        self.title = title
        self.index = index
        self.rich = rich
        super.init(frame: .zero)
    }
    required init?(coder: NSCoder) { fatalError() }

    private let actionFont = NSFont.systemFont(ofSize: Style.hintSize, weight: .medium)

    private func symbolImage(_ b: Button) -> NSImage? {
        guard let name = b.symbol else { return nil }
        return NSImage(systemSymbolName: name, accessibilityDescription: b.label)
    }

    private func width(of b: Button) -> CGFloat {
        // Square for a glyph; sized to the word otherwise, because a fixed box would clip
        // "yeniden başlat" while looking half empty for "允許".
        if symbolImage(b) != nil { return 30 }
        let w = b.label.size(withAttributes: [.font: actionFont]).width
        return max(46, w + 18)
    }

    /// Laid out from the right edge inwards, so adding one shifts the others left rather than
    /// moving the one the hand already knows.
    private var buttonRects: [NSRect] {
        var out: [NSRect] = []
        var right = bounds.width - Style.padH
        for b in buttons.reversed() {
            let w = width(of: b)
            out.append(NSRect(x: right - w, y: bounds.midY - 11, width: w, height: 22))
            right -= w + 5
        }
        return out.reversed()
    }

    /// Where the buttons start, so the row's text stops before them instead of running through.
    private var buttonsLeftEdge: CGFloat {
        var edge = bounds.width - Style.padH
        for r in buttonRects where r.minX < edge { edge = r.minX }
        return edge
    }

    /// Where each linked run landed.
    /// Where the label starts. After the ⌘n badge, and after the icon when there is one.
    private var textX: CGFloat { Style.padH + 40 + (icon.map { $0.size.width + 8 } ?? 0) }

    private var linkZones: [(rect: NSRect, value: String)] {
        guard let rich else { return [] }
        return TextZones.of(rich, key: .link, x0: textX, height: bounds.height)
    }

    /// Where each run that has something to explain landed.
    ///
    /// The same measurement as `linkZones`, for the other thing a run can carry. A row is one
    /// line wide and a stack's failure is not: six processes can be down, for three different
    /// reasons, and only the first of them fits. Reaching the rest had meant opening the log
    /// pane — which is a place you go once you already suspect there is more to see.
    private var tipZones: [(rect: NSRect, value: String)] {
        guard let rich else { return [] }
        return TextZones.of(rich, key: .clawdlineTip, x0: textX, height: bounds.height)
    }

    private func draw(_ b: Button, in r: NSRect) {
        let path = NSBezierPath(roundedRect: r, xRadius: 6, yRadius: 6)
        (b.armed ? Style.accent.withAlphaComponent(0.22) : Style.chipFill).setFill()
        path.fill()
        (b.armed ? Style.accent : Style.chipEdge).setStroke()
        path.lineWidth = 1
        path.stroke()

        let tint: NSColor = b.armed ? Style.accent : NSColor.secondaryLabelColor
        if let image = symbolImage(b) {
            let config = NSImage.SymbolConfiguration(pointSize: 11, weight: .semibold)
            let glyph = image.withSymbolConfiguration(config) ?? image
            let size = glyph.size
            let box = NSRect(x: r.midX - size.width / 2, y: r.midY - size.height / 2,
                             width: size.width, height: size.height)
            // These are template images: drawing them straight comes out black in dark mode,
            // so the glyph is painted and then tinted through itself.
            glyph.draw(in: box, from: .zero, operation: .sourceOver, fraction: 1,
                       respectFlipped: true, hints: nil)
            tint.set()
            box.fill(using: .sourceAtop)
            return
        }
        let attrs: [NSAttributedString.Key: Any] = [.font: actionFont, .foregroundColor: tint]
        let size = b.label.size(withAttributes: attrs)
        b.label.draw(at: NSPoint(x: r.midX - size.width / 2, y: r.midY - size.height / 2),
                     withAttributes: attrs)
    }

    override func mouseDown(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        for (i, r) in buttonRects.enumerated() where r.contains(p) { onButton?(i); return }
        if let hit = linkZones.first(where: { $0.rect.contains(p) }) {
            onOpen?(hit.value)
            return
        }
        // Anywhere else on a row that has buttons: nothing. They are the only places that act,
        // which is what makes it possible to read the row without arming anything.
        if buttons.isEmpty { onClick?() }
    }

    override func resetCursorRects() {
        super.resetCursorRects()
        // Only over the things that do something. A pointing hand across the whole row would
        // promise that clicking anywhere works, and on the stack list it does not.
        for zone in linkZones { addCursorRect(zone.rect, cursor: .pointingHand) }
        for r in buttonRects { addCursorRect(r, cursor: .pointingHand) }
    }

    /// The strings the tool tips are made of, held here so that they are still alive when one
    /// is shown.
    ///
    /// **`addToolTip(_:owner:userData:)` does not retain its owner.** Passing `someString as
    /// NSString` creates a bridged object on the spot that nothing owns, and `NSToolTipManager`
    /// keeps only an unowned reference to it — so by the time the hover timer fires, half a
    /// second later, it is messaging freed memory. It crashed in
    /// `objc_opt_respondsToSelector`, called from `-[NSToolTipManager displayToolTip:]`, while
    /// asking the owner whether it implements the delegate method.
    ///
    /// Half a second is exactly long enough for it to look like "the app quits when I hover over
    /// the buttons", which is what it was reported as.
    private var tipOwners: [NSString] = []

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        removeAllToolTips()
        tipOwners = []
        // On a button, the hover says what the press will do — not what the row currently is.
        // Those are different questions, and only one of them is about to change something.
        // **This is what makes an icon safe to use**: the picture is the shortcut, the hover is
        // the contract.
        for (i, r) in buttonRects.enumerated() where !buttons[i].tip.isEmpty {
            let owner = buttons[i].tip as NSString
            tipOwners.append(owner)
            _ = addToolTip(r, owner: owner, userData: nil)
        }
        // And the row's own words, where they have more to say than fits. Clipped at the
        // buttons for the same reason the text is drawn clipped there: a run that continues on
        // underneath them is not on screen, and a tool tip hanging off a button that belongs to
        // a word hidden behind it is more confusing than no tool tip at all.
        let room = NSRect(x: 0, y: 0, width: buttonsLeftEdge, height: bounds.height)
        for zone in tipZones {
            let rect = zone.rect.intersection(room)
            guard rect.width > 4 else { continue }
            let owner = zone.value as NSString
            tipOwners.append(owner)
            _ = addToolTip(rect, owner: owner, userData: nil)
        }
    }

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

        if let icon {
            // Full strength on the selected row and a shade back on the others, the same way the
            // label is: the mark is part of the row, not a decoration beside it.
            icon.draw(in: NSRect(x: Style.padH + 40,
                                 y: (bounds.midY - icon.size.height / 2).rounded(),
                                 width: icon.size.width, height: icon.size.height),
                      from: .zero, operation: .sourceOver, fraction: isSelected ? 1 : 0.75)
        }

        let text = rich ?? NSAttributedString(string: title, attributes: [
            .font: NSFont.systemFont(ofSize: Style.listSize, weight: isSelected ? .medium : .regular),
            .foregroundColor: isSelected ? NSColor.labelColor : NSColor.secondaryLabelColor,
        ])
        let x = textX
        // Stop before the buttons, or a long row of ports draws straight through them.
        let right = buttons.isEmpty ? Style.padH : bounds.width - buttonsLeftEdge + 10
        let room = max(40, bounds.width - x - right)
        text.draw(in: NSRect(x: x, y: bounds.midY - 9, width: room, height: 18))

        // **Not a `guard … else { return }`.** It was, and the buttons were drawn after it — so a
        // row with no detail line returned before reaching them, and every row in the server
        // panel is exactly that: it puts everything into `rich` and has no detail at all. The
        // buttons existed, sat in the right place, answered the cursor and showed their tool
        // tips — because hit testing, cursor rects and tool tips are three separate passes that
        // never come through here. Only the painting did, and it had already left.
        if let detail {
            // After the label, measured, so the spinner and the state line follow the words
            // however long they are rather than sitting at a column that fits one language.
            var dx = x + min(text.size().width, room) + 12
            if isBusy {
                PixelSpinner.draw(at: NSPoint(x: dx, y: bounds.midY - PixelSpinner.size / 2),
                                  time: spinnerTime ?? CACurrentMediaTime(), colour: Style.accent)
                dx += PixelSpinner.size + 7
            }
            detail.draw(in: NSRect(x: dx, y: bounds.midY - 9,
                                   width: max(0, bounds.width - dx - right), height: 18))
        }

        for (i, r) in buttonRects.enumerated() { draw(buttons[i], in: r) }
    }
}

// MARK: - The activity strip

/// The "what is it doing right now" line, which is sometimes also a way into the pane.
///
/// A label rather than a button because it is a sentence — most of it is the live line, which
/// leads nowhere and should not look as though it does. Only the half about background agents
/// has anywhere to go, so the pointer is the one thing that can say "this is worth clicking"
/// while it is true and nothing at all while it is not.
final class ActivityLabel: NSTextField {
    var clickable = false {
        didSet {
            guard clickable != oldValue else { return }
            window?.invalidateCursorRects(for: self)
        }
    }

    override func resetCursorRects() {
        super.resetCursorRects()
        if clickable { addCursorRect(bounds, cursor: .pointingHand) }
    }
}

// MARK: - The pane's header

/// A name, the way out, and a row of tabs — pinned to the top of the ⌘J pane.
///
/// **Two things use it, because they turned out to be the same shape.** A stack's log is a
/// project, a tab per process and a way back to the transcript; a session's background agents
/// are a count, a tab per agent and a way back to the same place. Written twice they would have
/// been two views drifting apart at the edges — different paddings, different truncation, one of
/// them remembering to invalidate its cursor rects.
///
/// A real view rather than the first two lines of the text, because those scrolled away. The
/// tabs are the only means of getting anywhere in this pane, and a control that leaves the
/// screen the moment you start reading is a control you have to scroll back up to find.
///
/// Added with `NSScrollView.addFloatingSubview(_:for:)`, which is AppKit's own answer to this
/// and keeps it correct through live resize and elastic scrolling.
final class PaneHeader: NSView {
    var title = ""
    var titleColor: NSColor = .labelColor
    var backLabel = ""
    /// (label, value) — value nil is the "all" tab.
    var tabs: [(label: String, value: String?)] = []
    var current: String?
    var onTab: ((String?) -> Void)?
    var onBack: (() -> Void)?

    static let height: CGFloat = 46

    private var titleFont: NSFont { NSFont.systemFont(ofSize: Style.outputSize, weight: .semibold) }
    private var tabFont: NSFont { NSFont.systemFont(ofSize: Style.outputSize - 1) }

    override var isOpaque: Bool { true }

    /// Whether this header has anywhere to go back to. See `draw`.
    private var hasBack: Bool { !backLabel.isEmpty }

    private var backRect: NSRect {
        let w = ("← " + backLabel).size(withAttributes: [.font: tabFont]).width
        let t = title.size(withAttributes: [.font: titleFont]).width
        return NSRect(x: Style.padH + t + 14, y: bounds.height - 22, width: w, height: 18)
    }

    private var tabRects: [(rect: NSRect, value: String?)] {
        var out: [(NSRect, String?)] = []
        var x = Style.padH
        for tab in tabs {
            let w = tab.label.size(withAttributes: [.font: tabFont]).width
            out.append((NSRect(x: x, y: 4, width: w, height: 18), tab.value))
            x += w + 16
        }
        return out
    }

    override func draw(_ dirtyRect: NSRect) {
        // Its own background: it floats over the text, and without one the lines it covers show
        // straight through it.
        (NSColor.textBackgroundColor.withAlphaComponent(0)).setFill()
        Style.outputBg.setFill()
        bounds.fill()
        NSColor.windowBackgroundColor.withAlphaComponent(0.92).setFill()
        bounds.fill()

        title.draw(at: NSPoint(x: Style.padH, y: bounds.height - 22), withAttributes: [
            .font: titleFont, .foregroundColor: titleColor,
        ])
        // No label, no way back — and no arrow either. The agents' own strip is a list with
        // nothing behind it: an arrow there would offer to return somewhere the reader has not
        // been, and a control that does nothing is worse than one that is not drawn.
        if hasBack {
            ("← " + backLabel).draw(in: backRect, withAttributes: [
                .font: tabFont,
                .foregroundColor: NSColor.secondaryLabelColor,
                .underlineStyle: NSUnderlineStyle.single.rawValue,
            ])
        }

        for (i, zone) in tabRects.enumerated() {
            let isCurrent = zone.value == current
            var attrs: [NSAttributedString.Key: Any] = [
                .font: tabFont,
                .foregroundColor: isCurrent ? NSColor.labelColor : NSColor.secondaryLabelColor,
            ]
            if isCurrent {
                attrs[.underlineStyle] = NSUnderlineStyle.thick.rawValue
                attrs[.underlineColor] = Style.accent
            }
            tabs[i].label.draw(in: zone.rect, withAttributes: attrs)
        }

        Style.hairline.setStroke()
        let line = NSBezierPath()
        line.move(to: NSPoint(x: 0, y: 0.5))
        line.line(to: NSPoint(x: bounds.width, y: 0.5))
        line.stroke()
    }

    override func mouseDown(with event: NSEvent) {
        let p = convert(event.locationInWindow, from: nil)
        if hasBack, backRect.contains(p) { onBack?(); return }
        for zone in tabRects where zone.rect.insetBy(dx: -6, dy: -4).contains(p) {
            if zone.value != current { onTab?(zone.value) }
            return
        }
    }

    override func resetCursorRects() {
        super.resetCursorRects()
        if hasBack { addCursorRect(backRect, cursor: .pointingHand) }
        for zone in tabRects where zone.value != current {
            addCursorRect(zone.rect, cursor: .pointingHand)
        }
    }
}
