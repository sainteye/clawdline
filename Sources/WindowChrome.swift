import AppKit

/// Shared dark-window measurements and hand-drawn controls used by Settings and Home.
///
/// Keep these primitives here rather than giving either window ownership of the other one's
/// chrome. Settings' appearance is intentionally unchanged by this extraction.
// MARK: - Measurements and ink

enum Metric {
    static let pad: CGFloat = 26            // window edge to content
    static let gutter: CGFloat = 36         // between the two columns
    static let rowGap: CGFloat = 18         // between a label and the thing it names
    static let rowPad: CGFloat = 8          // above and below a row's contents
    static let stripHeight: CGFloat = 46
    static let footerHeight: CGFloat = 40
    static let minColumn: CGFloat = 316
    static let maxColumn: CGFloat = 452
    /// The widest a paragraph is allowed to get, whatever the band it sits in. Past about this
    /// the eye starts losing its place on the way back to the left margin.
    static let maxMeasure: CGFloat = 600

    static let labelFont = NSFont.systemFont(ofSize: 12)
    static let hintFont = NSFont.systemFont(ofSize: 10.5)
    static let headFont = NSFont.systemFont(ofSize: 10, weight: .semibold)
    static let tabFont = NSFont.systemFont(ofSize: 12.5, weight: .medium)
    static let noteFont = NSFont.systemFont(ofSize: 11.5)
    static let monoFont = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)

    /// Stated rather than taken from the system's semantic colours. The window is pinned to dark,
    /// so these are known quantities — and `tertiaryLabelColor` on this ground is dim enough that a
    /// hint written in it is a hint nobody reads.
    static let label = NSColor(white: 1, alpha: 0.92)
    static let soft = NSColor(white: 1, alpha: 0.62)
    static let faint = NSColor(white: 1, alpha: 0.44)
}

/// For text this file draws itself — a tab, the word on a chip.
func textWidth(_ text: String, _ font: NSFont) -> CGFloat {
    ceil(text.size(withAttributes: [.font: font]).width)
}

func makeLabel(_ text: String, _ font: NSFont, _ colour: NSColor,
                       width: CGFloat? = nil) -> NSTextField {
    let field = NSTextField(wrappingLabelWithString: text)
    field.font = font
    field.textColor = colour
    field.isSelectable = false
    field.drawsBackground = false
    field.lineBreakMode = .byWordWrapping
    if let width { field.preferredMaxLayoutWidth = width }
    return field
}

/// How tall a wrapping label actually comes out at a given width.
///
/// Measured with the control that is going to draw it, not with
/// `NSAttributedString.boundingRect`. The two disagree by the text cell's own inset — a couple of
/// points — and the disagreement only shows on the labels that *nearly* fit: they wrap to a second
/// line, and the frame computed from the string has room for one. Every clipped hint in the first
/// draft of this window was that, and it was invisible in English on the row it was measured
/// against and glaring one language over.
func labelHeight(_ text: String, _ font: NSFont, width: CGFloat) -> CGFloat {
    guard !text.isEmpty, width > 6 else { return 0 }
    // Measured four points short of the room it is going to get. `preferredMaxLayoutWidth` and a
    // frame of the same number are not the same width — the cell keeps a line fragment's padding
    // for itself — so a string that clears the measurement by a point wraps in the frame that was
    // sized for one line, and the second line is drawn outside it. Erring the other way costs a
    // few points of slack and nothing else.
    return ceil(makeLabel(text, font, .white, width: width - 4).fittingSize.height)
}

/// How wide it would be on one line — what a column has to offer before anything wraps. Plus the
/// same padding, for the same reason.
func labelWidth(_ text: String, _ font: NSFont) -> CGFloat {
    guard !text.isEmpty else { return 0 }
    return ceil(makeLabel(text, font, .white).fittingSize.width) + 4
}

/// A view that knows how tall it wants to be once it is told how wide it is — a wrapping row of
/// chips, a list of applications. Everything else in a column states a fixed size.
protocol SelfSizing {
    func height(forWidth width: CGFloat) -> CGFloat
}

// MARK: - A column of settings

/// A list of rows: what it is on the left, the thing that changes it on the right.
///
/// Not a label column and a control column. The old form right-aligned every label at a fixed 168
/// points, which is the arrangement that made the window read as generated — one hard vertical seam
/// down the middle of a page with nothing else in it. Here the label starts at the left edge, the
/// control finishes at the right edge, and a hairline runs between rows: a list, which is what a
/// page of settings is.
///
/// It also survives translation, which the fixed column did not. A German label half again as long
/// simply takes more of the row, and only wraps once it has genuinely run out of room.
final class SettingsColumn {

    private enum Item {
        case head(String)
        case row(String, NSView, String?)
        case block(String?, NSView, String?)
        case mono(String)
    }

    private var items: [Item] = []

    func head(_ title: String) { items.append(.head(title)) }
    func row(_ label: String, _ control: NSView, hint: String? = nil) {
        items.append(.row(label, control, hint))
    }
    func block(label: String?, view: NSView, hint: String?) {
        items.append(.block(label, view, hint))
    }
    func mono(_ text: String) { items.append(.mono(text)) }

    var isEmpty: Bool { items.isEmpty }

    /// The width at which nothing wraps that should not have to.
    var naturalWidth: CGFloat {
        var widest: CGFloat = 0
        for item in items {
            switch item {
            case .row(let label, let control, _):
                widest = max(widest, labelWidth(label, Metric.labelFont)
                    + Metric.rowGap + control.frame.width)
            case .block(_, let view, _):
                widest = max(widest, view.frame.width)
            case .head, .mono:
                break   // these wrap or truncate, so they never decide how wide anything is
            }
        }
        return min(max(widest, Metric.minColumn), Metric.maxColumn)
    }

    func height(forWidth width: CGFloat) -> CGFloat { walk(width: width, into: nil) }

    @discardableResult
    func place(in parent: NSView, x: CGFloat, top: CGFloat, width: CGFloat) -> CGFloat {
        walk(width: width, into: parent, x: x, top: top)
    }

    /// Measuring and placing are the same walk, so the two cannot disagree about how tall a row is
    /// — which is the bug that leaves the last control of a tab hanging past the bottom edge.
    private func walk(width: CGFloat, into parent: NSView?,
                      x: CGFloat = 0, top: CGFloat = 0) -> CGFloat {
        var y = top
        var previousWasRow = false

        func add(_ view: NSView, _ frame: NSRect) {
            guard let parent else { return }
            view.frame = frame
            view.needsLayout = true
            parent.addSubview(view)
        }

        for item in items {
            switch item {
            case .head(let title):
                if y > top { y += 22 }
                add(makeLabel(title.uppercased(), Metric.headFont, Metric.faint),
                    NSRect(x: x, y: y, width: width, height: 13))
                y += 21
                previousWasRow = false

            case .row(let label, let control, let hint):
                if previousWasRow {
                    add(Hairline(), NSRect(x: x, y: y, width: width, height: 1))
                    y += 1
                }
                let controlWidth = control.frame.width
                let controlHeight = control.frame.height
                let textWidth = max(60, width - controlWidth - Metric.rowGap)
                let textHeight = max(15, labelHeight(label, Metric.labelFont, width: textWidth))
                let inner = max(textHeight, controlHeight)
                // A hand-drawn control has no label of its own, so the row lends it one. Without
                // this, VoiceOver reads a settings window as a column of anonymous checkboxes.
                if control.accessibilityLabel() == nil { control.setAccessibilityLabel(label) }
                add(makeLabel(label, Metric.labelFont, Metric.label, width: textWidth),
                    NSRect(x: x, y: y + Metric.rowPad + (inner - textHeight) / 2,
                           width: textWidth, height: textHeight))
                add(control, NSRect(x: x + width - controlWidth,
                                    y: y + Metric.rowPad + (inner - controlHeight) / 2,
                                    width: controlWidth, height: controlHeight))
                y += inner + Metric.rowPad * 2
                y += place(hint: hint, x: x, y: y, width: width, into: parent, lead: -3, trail: 8)
                previousWasRow = true

            case .block(let label, let view, let hint):
                let viewHeight = (view as? SelfSizing)?.height(forWidth: width) ?? view.frame.height
                // A reading with nothing to report — the tunnel status before a tunnel exists —
                // says so by taking up no room at all. An empty card is a control that looks
                // broken.
                guard viewHeight > 0 else { break }
                if y > top { y += 16 }
                if let label {
                    add(makeLabel(label, Metric.labelFont, Metric.label, width: width),
                        NSRect(x: x, y: y, width: width, height: 16))
                    y += 22
                }
                add(view, NSRect(x: x, y: y, width: width, height: viewHeight))
                y += viewHeight
                y += place(hint: hint, x: x, y: y, width: width, into: parent, lead: 7, trail: 4)
                previousWasRow = false

            case .mono(let text):
                if y > top { y += 10 }
                let field = NSTextField(labelWithString: text)
                field.font = Metric.monoFont
                field.textColor = Metric.faint
                field.isSelectable = true
                field.lineBreakMode = .byTruncatingMiddle
                add(field, NSRect(x: x, y: y, width: width, height: 16))
                y += 16
                previousWasRow = false
            }
        }
        return y - top
    }

    /// A hint under the thing it explains.
    ///
    /// Capped in width even where the column is not. A wide band — the paired-device list runs the
    /// whole window — makes a paragraph eight hundred points across, and a line that long is one
    /// the eye loses its place in on the way back to the left margin.
    private func place(hint: String?, x: CGFloat, y: CGFloat, width: CGFloat,
                       into parent: NSView?, lead: CGFloat, trail: CGFloat) -> CGFloat {
        guard let hint, !hint.isEmpty else { return 0 }
        let measure = min(width, Metric.maxMeasure)
        let height = labelHeight(hint, Metric.hintFont, width: measure)
        if let parent {
            let field = makeLabel(hint, Metric.hintFont, Metric.faint, width: measure)
            field.frame = NSRect(x: x, y: y + lead, width: measure, height: height)
            parent.addSubview(field)
        }
        return lead + height + trail
    }
}

/// One tab: two columns, and an optional band underneath that runs the whole width.
///
/// The band exists for things that would be squeezed into nonsense in half a window — the paired
/// device list is a row of names and three buttons. Nothing is forced into a column that does not
/// want to be in one.
final class SettingsPane {
    let title: String
    let left = SettingsColumn()
    let right = SettingsColumn()
    let wide = SettingsColumn()
    let view = FlippedView()

    init(title: String) { self.title = title }

    var naturalWidth: CGFloat {
        if left.isEmpty || right.isEmpty {
            return max(left.naturalWidth, right.naturalWidth, wide.naturalWidth)
        }
        return max(max(left.naturalWidth, right.naturalWidth) * 2 + Metric.gutter, wide.naturalWidth)
    }

    /// Lay the whole tab out at a width and say how tall it came to.
    ///
    /// Done again on every switch rather than remembered, which is what lets a tab whose contents
    /// changed under it — a device paired, a hook installed, an app added to the scope — come back
    /// the right height instead of the height it was the first time it was looked at.
    func layout(width: CGFloat) -> CGFloat {
        view.subviews.forEach { $0.removeFromSuperview() }
        let twoUp = !left.isEmpty && !right.isEmpty
        let column = twoUp ? (width - Metric.gutter) / 2 : width

        var height: CGFloat = 0
        if !left.isEmpty {
            height = max(height, left.place(in: view, x: 0, top: 0, width: column))
        }
        if !right.isEmpty {
            height = max(height, right.place(in: view, x: twoUp ? column + Metric.gutter : 0,
                                             top: 0, width: column))
        }
        if !wide.isEmpty {
            if height > 0 { height += 24 }
            height += wide.place(in: view, x: 0, top: height, width: width)
        }
        view.frame.size = NSSize(width: width, height: height)
        return height
    }
}

/// Rows are added downwards, so the code that adds them counts downwards too. Anything else is
/// arithmetic waiting to be got wrong.
final class FlippedView: NSView {
    override var isFlipped: Bool { true }
}

// MARK: - The pieces drawn by hand

/// One point of `Style.hairline`. The separator this project already uses everywhere else, rather
/// than `NSBox`, whose separator is the system's colour and not this one.
final class Hairline: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    override func draw(_ dirtyRect: NSRect) {
        Style.hairline.setFill()
        bounds.fill()
    }
}

/// A square of colour saying what state something is in, and the app's own spinner when the honest
/// answer is "ask again in a moment".
///
/// A square rather than a dot, because everything else this app draws is made of squares — and
/// drawn rather than taken from SF Symbols for the same reason the mascot is.
final class PixelDot: NSView {
    enum State { case idle, warn, live, busy }

    var state: State = .idle {
        didSet {
            guard oldValue != state else { return }
            state == .busy ? start() : stop()
            needsDisplay = true
        }
    }

    private var timer: Timer?
    private var phase: Double = 0

    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    private func start() {
        guard timer == nil else { return }
        let t = Timer(timeInterval: PixelSpinner.step, repeats: true) { [weak self] _ in
            self?.phase += PixelSpinner.step
            self?.needsDisplay = true
        }
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    private func stop() {
        timer?.invalidate()
        timer = nil
        phase = 0
    }

    deinit { timer?.invalidate() }

    override func draw(_ dirtyRect: NSRect) {
        if state == .busy {
            PixelSpinner.draw(at: NSPoint(x: (bounds.midX - PixelSpinner.size / 2).rounded(),
                                          y: (bounds.midY - PixelSpinner.size / 2).rounded()),
                              time: phase, colour: Style.accent)
            return
        }
        let colour: NSColor
        switch state {
        case .live: colour = Style.accent
        case .warn: colour = Style.accent.withAlphaComponent(0.42)
        default:    colour = NSColor(white: 1, alpha: 0.20)
        }
        colour.setFill()
        NSRect(x: (bounds.midX - 2.5).rounded(), y: (bounds.midY - 2.5).rounded(),
               width: 5, height: 5).fill()
    }
}

/// A sentence in a chip, with a mark in front of it and sometimes a button after it.
///
/// The same fill and edge the key hints along the bottom of the card use. It is for the places in
/// this window that *report* rather than ask — what the hooks are doing, what the tunnel is doing,
/// what will run when a session changes state — and setting those apart from the rows is what keeps
/// a reading from looking like a setting you failed to change.
final class NoteCard: NSView, SelfSizing {

    private let label = NSTextField(wrappingLabelWithString: "")
    private let dotView = PixelDot()

    var trailing: ChipButton? {
        didSet {
            oldValue?.removeFromSuperview()
            if let trailing { addSubview(trailing) }
            needsLayout = true
        }
    }

    var text: String {
        get { label.stringValue }
        set {
            guard label.stringValue != newValue else { return }
            label.stringValue = newValue
            needsLayout = true
        }
    }

    var dot: PixelDot.State {
        get { dotView.state }
        set { dotView.state = newValue }
    }

    var mono = false {
        didSet { label.font = mono ? Metric.monoFont : Metric.noteFont }
    }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        label.font = Metric.noteFont
        label.textColor = Metric.soft
        label.isSelectable = true
        label.lineBreakMode = .byWordWrapping
        addSubview(dotView)
        addSubview(label)
    }
    required init?(coder: NSCoder) { fatalError() }

    private var textWidthAvailable: (CGFloat) -> CGFloat {
        { [weak self] width in
            let button = self?.trailing.map { $0.frame.width + 12 } ?? 0
            return max(40, width - 30 - 12 - button)
        }
    }

    func height(forWidth width: CGFloat) -> CGFloat {
        guard !label.stringValue.isEmpty || trailing != nil else { return 0 }
        return max(40, labelHeight(label.stringValue, label.font ?? Metric.noteFont,
                                   width: textWidthAvailable(width)) + 22)
    }

    override func layout() {
        super.layout()
        dotView.frame = NSRect(x: 12, y: bounds.midY - 6, width: 12, height: 12)
        if let trailing {
            trailing.frame.origin = NSPoint(x: bounds.width - 12 - trailing.frame.width,
                                            y: (bounds.midY - trailing.frame.height / 2).rounded())
        }
        let width = textWidthAvailable(bounds.width)
        label.preferredMaxLayoutWidth = width
        let height = labelHeight(label.stringValue, label.font ?? Metric.noteFont, width: width)
        label.frame = NSRect(x: 30, y: (bounds.midY - height / 2).rounded(),
                             width: width, height: height)
    }

    override func draw(_ dirtyRect: NSRect) {
        let path = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5), xRadius: 8, yRadius: 8)
        Style.chipFill.setFill()
        path.fill()
        Style.chipEdge.setStroke()
        path.lineWidth = 1
        path.stroke()
    }
}

/// A button, drawn as the app's chip.
///
/// The same shape as the buttons on a session row, and for the same reason as the switch above: an
/// `NSButton` in `.rounded` is the system's grey pill, and beside a hand-drawn strip and a
/// hand-drawn switch it is the one thing that arrived from somewhere else.
///
/// `prominent` is for the one action on a tab that is the reason somebody came to the tab. `armed`
/// is the state a stack row's button uses — pressed, and waiting for the next thing you do — which
/// is exactly what the hotkey recorder is doing while it listens.
final class ChipButton: NSView {

    var title: String {
        didSet {
            guard title != oldValue else { return }
            resize()
            setAccessibilityLabel(title)
            needsDisplay = true
        }
    }
    var armed = false { didSet { if armed != oldValue { needsDisplay = true } } }
    /// Greyed and unpressable while something it started is still in flight. Only the schedule
    /// form uses it — everything else in this window applies the moment it is touched, so there
    /// is no interval in which a second press would mean anything.
    var isEnabled = true {
        didSet {
            guard isEnabled != oldValue else { return }
            if !isEnabled { hovering = false; pressed = false }
            needsDisplay = true
        }
    }
    var minimumWidth: CGFloat = 0 { didSet { resize() } }
    var action: (() -> Void)?

    private let prominent: Bool
    private var hovering = false
    private var pressed = false
    private var tracking: NSTrackingArea?

    init(title: String, prominent: Bool = false) {
        self.title = title
        self.prominent = prominent
        super.init(frame: NSRect(x: 0, y: 0, width: 100, height: 26))
        setAccessibilityRole(.button)
        setAccessibilityLabel(title)
        resize()
    }
    required init?(coder: NSCoder) { fatalError() }

    private var font: NSFont { NSFont.systemFont(ofSize: 11.5, weight: .medium) }

    private func resize() {
        frame.size = NSSize(width: max(minimumWidth, textWidth(title, font) + 26), height: 26)
        superview?.needsLayout = true
    }

    override var acceptsFirstResponder: Bool { true }
    override func becomeFirstResponder() -> Bool { needsDisplay = true; return true }
    override func resignFirstResponder() -> Bool { needsDisplay = true; return true }
    override func resetCursorRects() { addCursorRect(bounds, cursor: .pointingHand) }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let tracking { removeTrackingArea(tracking) }
        let area = NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeInKeyWindow],
                                  owner: self, userInfo: nil)
        addTrackingArea(area)
        tracking = area
    }

    override func mouseEntered(with event: NSEvent) {
        guard isEnabled else { return }
        hovering = true
        needsDisplay = true
    }
    override func mouseExited(with event: NSEvent) { hovering = false; needsDisplay = true }

    override func mouseDown(with event: NSEvent) {
        guard isEnabled else { return }
        pressed = true
        needsDisplay = true
    }

    override func mouseUp(with event: NSEvent) {
        pressed = false
        needsDisplay = true
        guard isEnabled, bounds.contains(convert(event.locationInWindow, from: nil)) else { return }
        window?.makeFirstResponder(self)
        action?()
    }

    override func keyDown(with event: NSEvent) {
        if isEnabled, event.keyCode == 49 || event.keyCode == 36 {
            action?()
        } else {
            super.keyDown(with: event)
        }
    }

    override func draw(_ dirtyRect: NSRect) {
        let path = NSBezierPath(roundedRect: bounds.insetBy(dx: 0.5, dy: 0.5), xRadius: 7, yRadius: 7)
        let lit = armed || prominent
        var fill = lit ? Style.accent.withAlphaComponent(armed ? 0.28 : 0.16) : Style.chipFill
        if pressed { fill = fill.blended(withFraction: 0.25, of: .black) ?? fill }
        else if hovering { fill = fill.blended(withFraction: 0.10, of: .white) ?? fill }
        fill.setFill()
        path.fill()
        (lit ? Style.accent : Style.chipEdge).setStroke()
        path.lineWidth = 1
        path.stroke()

        if window?.firstResponder === self {
            Style.accent.withAlphaComponent(0.55).setStroke()
            let ring = NSBezierPath(roundedRect: bounds.insetBy(dx: -2, dy: -2),
                                    xRadius: 9, yRadius: 9)
            ring.lineWidth = 1.5
            ring.stroke()
        }

        let attrs: [NSAttributedString.Key: Any] = [
            .font: font,
            .foregroundColor: (lit ? Style.accent : Metric.soft)
                .withAlphaComponent(isEnabled ? 1 : 0.4),
        ]
        let size = title.size(withAttributes: attrs)
        title.draw(at: NSPoint(x: (bounds.midX - size.width / 2).rounded(),
                               y: (bounds.midY - size.height / 2).rounded()),
                   withAttributes: attrs)
    }
}

/// Something to read, with the buttons that act on it underneath.
final class StackedRow: NSView, SelfSizing {

    var top: NSView? {
        didSet {
            oldValue?.removeFromSuperview()
            if let top { addSubview(top) }
            needsLayout = true
        }
    }

    var buttons: [ChipButton] = [] {
        didSet {
            oldValue.forEach { $0.removeFromSuperview() }
            buttons.forEach { addSubview($0) }
            needsLayout = true
        }
    }

    private func topHeight(_ width: CGFloat) -> CGFloat {
        (top as? SelfSizing)?.height(forWidth: width) ?? top?.frame.height ?? 0
    }

    func height(forWidth width: CGFloat) -> CGFloat {
        topHeight(width) + (buttons.isEmpty ? 0 : 40)
    }

    override func layout() {
        super.layout()
        let above = topHeight(bounds.width)
        top?.frame = NSRect(x: 0, y: bounds.height - above, width: bounds.width, height: above)
        var x: CGFloat = 0
        for button in buttons {
            button.frame.origin = NSPoint(x: x, y: 4)
            x += button.frame.width + 8
        }
    }
}
