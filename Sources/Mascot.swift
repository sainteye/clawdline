import AppKit

// MARK: - Pack format
//
// A mascot is data, not code. Everything about how it looks and moves lives in one JSON
// file, so swapping in a different character never means forking this repo — and an agent
// with a reference image can write that file for you.
//
// See docs/mascots.md for the format and an example prompt.

/// One frame of the animation: which pose to draw and how to transform it.
struct MascotFrame {
    var pose: String = ""
    var dx: CGFloat = 0
    var dy: CGFloat = 0
    var rot: CGFloat = 0        // radians
    var sx: CGFloat = 1
    var sy: CGFloat = 1
    var eyes: String = "open"   // open | blink | happy
}

struct MascotPack: Decodable {
    struct Grid: Decodable {
        let cols: Int
        let rows: Int
    }

    struct Key: Decodable {
        let t: Double           // 0…1, a fraction of the routine's duration
        var pose: String?
        var dx: Double?
        var dy: Double?
        var rot: Double?
        var sx: Double?
        var sy: Double?
        var eyes: String?
        var ease: String?       // linear (default) | inout | out
    }

    /// How big to draw it, independent of how fine the grid is.
    /// Without this, a 32x24 pack would render half the size of a 16x11 one for no
    /// reason other than having more cells — resolution would decide scale.
    struct Display: Decodable {
        var height: Double?     // intended sprite height in points
        var overlap: Double?    // how far the feet sink into the card
        var jumpRoom: Double?   // headroom for jumps and the pop overshoot
        var sideRoom: Double?   // slack each side for sway and horizontal scale
        var footInset: Double?  // gap between the sprite feet and the bottom of the view
    }

    struct Blink: Decodable {
        var everyMin: Double
        var everyMax: Double
        var duration: Double
    }

    struct Routine: Decodable {
        let duration: Double
        var loop: Bool?
        var keys: [Key]
        var blink: Blink?
    }

    let name: String
    var author: String?
    let grid: Grid
    /// Character in a pose row → colour. `accent` follows the app tint; `transparent` paints nothing.
    let palette: [String: String]
    /// Which characters are eyes. Blink hides the top row of them, happy hides the bottom row.
    var eyeChars: [String]?
    /// The character a closed eye turns into — whatever the face is made of.
    /// Without it a white character would blink in the app's accent colour.
    var skin: String?
    var display: Display?
    let poses: [String: [String]]
    let routines: [String: Routine]

    // Defaults chosen to reproduce the original hand-tuned Clawd geometry exactly.
    var spriteHeight: CGFloat { CGFloat(display?.height ?? 77) }
    var overlap: CGFloat { CGFloat(display?.overlap ?? 3) }
    var jumpRoom: CGFloat { CGFloat(display?.jumpRoom ?? 41) }
    var sideRoom: CGFloat { CGFloat(display?.sideRoom ?? 12) }
    var footInset: CGFloat { CGFloat(display?.footInset ?? 6) }

    /// One cell, in points. Rounded so pixel edges land on whole points and stay crisp.
    var cellSize: CGFloat { max(1, (spriteHeight / CGFloat(grid.rows)).rounded()) }
    var spriteSize: NSSize {
        NSSize(width: cellSize * CGFloat(grid.cols), height: cellSize * CGFloat(grid.rows))
    }
    /// The view is bigger than the sprite: jumps, the pop overshoot and the sway all
    /// happen inside it, and a view clips its own drawing.
    var boxSize: NSSize {
        NSSize(width: spriteSize.width + sideRoom * 2,
               height: spriteSize.height + jumpRoom + footInset)
    }

    // MARK: Loading

    /// Search order: the user's own packs first, then the ones bundled with the app.
    /// A user copy always wins, so editing a bundled pack never gets undone by an update.
    static func load(named name: String) -> (pack: MascotPack?, error: String?) {
        var candidates: [URL] = [userDirectory.appendingPathComponent("\(name).json")]
        if let bundled = Bundle.main.url(forResource: name, withExtension: "json", subdirectory: "mascots") {
            candidates.append(bundled)
        }
        guard let url = candidates.first(where: { FileManager.default.fileExists(atPath: $0.path) }) else {
            return (nil, "No mascot named \"\(name)\" in \(userDirectory.path)")
        }
        do {
            let pack = try JSONDecoder().decode(MascotPack.self, from: Data(contentsOf: url))
            if let problem = pack.validate() { return (nil, "\(url.lastPathComponent): \(problem)") }
            return (pack, nil)
        } catch {
            return (nil, "\(url.lastPathComponent): \(error.localizedDescription)")
        }
    }

    static var userDirectory: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline/mascots", isDirectory: true)
    }

    /// Copy the bundled packs into the user directory on first run.
    /// A file you can see and edit is worth more than a documented one you have to go find.
    static func installBundledPacks() {
        let fm = FileManager.default
        try? fm.createDirectory(at: userDirectory, withIntermediateDirectories: true)
        guard let dir = Bundle.main.resourceURL?.appendingPathComponent("mascots"),
              let names = try? fm.contentsOfDirectory(atPath: dir.path) else { return }
        for file in names where file.hasSuffix(".json") {
            let dest = userDirectory.appendingPathComponent(file)
            guard !fm.fileExists(atPath: dest.path) else { continue }
            try? fm.copyItem(at: dir.appendingPathComponent(file), to: dest)
        }
    }

    /// Every pack the user can pick from: their own directory plus anything bundled.
    /// Sorted, de-duplicated, and a user copy shadows a bundled one of the same name.
    static func available() -> [String] {
        let fm = FileManager.default
        var names = Set<String>()
        for dir in [userDirectory, Bundle.main.resourceURL?.appendingPathComponent("mascots")].compactMap({ $0 }) {
            for file in (try? fm.contentsOfDirectory(atPath: dir.path)) ?? [] where file.hasSuffix(".json") {
                names.insert(String(file.dropLast(5)))
            }
        }
        return names.sorted()
    }

    static func modificationDate(named name: String) -> Date? {
        let url = userDirectory.appendingPathComponent("\(name).json")
        return (try? FileManager.default.attributesOfItem(atPath: url.path))?[.modificationDate] as? Date
    }

    // MARK: Validation
    //
    // Every message here names the file, the key and what was expected. A mascot pack is
    // usually written by an agent that cannot see the screen, so the error text is the only
    // feedback it gets — "invalid pack" would leave it guessing.

    func validate() -> String? {
        guard grid.cols > 0, grid.rows > 0 else { return "grid.cols and grid.rows must be positive" }
        for (poseName, rows) in poses {
            guard rows.count == grid.rows else {
                return "pose \"\(poseName)\" has \(rows.count) rows, grid.rows says \(grid.rows)"
            }
            for (i, row) in rows.enumerated() where row.count != grid.cols {
                return "pose \"\(poseName)\" row \(i) is \(row.count) characters, grid.cols says \(grid.cols)"
            }
            for row in rows {
                for ch in row where palette[String(ch)] == nil {
                    return "pose \"\(poseName)\" uses \"\(ch)\", which is not in palette"
                }
            }
        }
        if poses.isEmpty { return "no poses defined" }
        for (routineName, routine) in routines {
            guard routine.duration > 0 else { return "routine \"\(routineName)\" needs a positive duration" }
            guard !routine.keys.isEmpty else { return "routine \"\(routineName)\" has no keys" }
            for key in routine.keys {
                if let p = key.pose, poses[p] == nil {
                    return "routine \"\(routineName)\" refers to pose \"\(p)\", which does not exist"
                }
            }
        }
        for required in ["idle"] where routines[required] == nil {
            return "routine \"\(required)\" is required"
        }
        return nil
    }

    // MARK: Sampling

    func color(for ch: Character, accent: NSColor) -> NSColor? {
        guard let spec = palette[String(ch)] else { return nil }
        switch spec.lowercased() {
        case "transparent", "none", "": return nil
        case "accent": return accent
        default: return NSColor(hex: spec) ?? accent
        }
    }

    var eyeCharacterSet: Set<Character> {
        Set((eyeChars ?? ["o"]).flatMap { $0 })
    }

    /// Interpolate the routine at `time` seconds. Transforms blend between keys;
    /// pose and eyes step, because a half-drawn pose is not a thing.
    func frame(routine routineName: String, at time: Double) -> MascotFrame {
        guard let routine = routines[routineName] ?? routines["idle"] else { return MascotFrame() }
        let looping = routine.loop ?? false
        let raw = looping ? time.truncatingRemainder(dividingBy: routine.duration) : min(time, routine.duration)
        let p = routine.duration > 0 ? raw / routine.duration : 0

        let keys = routine.keys.sorted { $0.t < $1.t }
        var before = keys[0]
        var after = keys[keys.count - 1]
        for (i, k) in keys.enumerated() where k.t <= p {
            before = k
            after = i + 1 < keys.count ? keys[i + 1] : k
        }

        let span = after.t - before.t
        var u = span > 0 ? (p - before.t) / span : 0
        u = max(0, min(1, u))
        switch after.ease ?? "linear" {
        case "inout": u = u < 0.5 ? 4 * u * u * u : 1 - pow(-2 * u + 2, 3) / 2
        case "out":   u = 1 - pow(1 - u, 3)
        default:      break
        }

        func mix(_ a: Double?, _ b: Double?, _ dflt: Double) -> CGFloat {
            let from = a ?? dflt, to = b ?? a ?? dflt
            return CGFloat(from + (to - from) * u)
        }

        var f = MascotFrame()
        f.dx = mix(before.dx, after.dx, 0)
        f.dy = mix(before.dy, after.dy, 0)
        f.rot = mix(before.rot, after.rot, 0)
        f.sx = mix(before.sx, after.sx, 1)
        f.sy = mix(before.sy, after.sy, 1)
        // Stepped: whatever the last key that mentioned one said.
        f.pose = lastValue(keys, upTo: p, \.pose) ?? poses.keys.sorted().first ?? ""
        f.eyes = lastValue(keys, upTo: p, \.eyes) ?? "open"
        return f
    }

    private func lastValue(_ keys: [Key], upTo p: Double, _ path: KeyPath<Key, String?>) -> String? {
        var found: String?
        for k in keys where k.t <= p {
            if let v = k[keyPath: path] { found = v }
        }
        return found ?? keys.first?[keyPath: path]
    }
}

extension NSColor {
    /// #RGB / #RRGGBB / #RRGGBBAA
    convenience init?(hex: String) {
        var s = hex.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("#") { s.removeFirst() }
        if s.count == 3 { s = s.map { "\($0)\($0)" }.joined() }
        guard s.count == 6 || s.count == 8, let v = UInt64(s, radix: 16) else { return nil }
        let shift = s.count == 8 ? 8 : 0
        let a = s.count == 8 ? CGFloat(v & 0xFF) / 255 : 1
        self.init(srgbRed: CGFloat((v >> (16 + shift)) & 0xFF) / 255,
                  green: CGFloat((v >> (8 + shift)) & 0xFF) / 255,
                  blue: CGFloat((v >> shift) & 0xFF) / 255,
                  alpha: a)
    }
}

// MARK: - View

/// Draws the current mascot pack.
///
/// The animation deliberately avoids Core Animation: a 60fps timer drives it and draw()
/// computes each offset and scale. A CALayer transform pivots on its anchorPoint, and for
/// AppKit layer-backed views that point depends on context — guess wrong and the sprite
/// grows out of a corner. Hand-drawn frames also show up in cacheDisplay(); CA output does not,
/// which is what lets `clawdline://snapshot` prove to an agent what it actually drew.
final class MascotView: NSView {
    private(set) var routine = "idle"
    private(set) var pack: MascotPack?
    private(set) var loadError: String?

    private var started = CACurrentMediaTime()
    private var fallback = "idle"
    private var nextBlink = Double.random(in: 1.8...4.0)
    private var blinkUntil = -1.0
    private var timer: Timer?

    /// For snapshot verification: pin the clock and draw one specific frame of a routine.
    var frozenTime: Double?

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.masksToBounds = false
        reload()
    }
    required init?(coder: NSCoder) { fatalError() }

    // MARK: Pack

    @discardableResult
    func reload() -> String? {
        let result = MascotPack.load(named: Config.shared.mascot)
        if let p = result.pack {
            pack = p
            loadError = nil
        } else {
            loadError = result.error
            Log.write("mascot: \(result.error ?? "unknown error")")
        }
        needsDisplay = true
        return loadError
    }

    // MARK: Playback

    func play(_ r: String, then back: String = "idle") {
        routine = r
        fallback = back
        started = CACurrentMediaTime()
        nextBlink = Double.random(in: 1.8...4.0)
        startTimer()
        needsDisplay = true
    }

    func stop() {
        timer?.invalidate()
        timer = nil
    }

    private func startTimer() {
        guard timer == nil else { return }
        let t = Timer(timeInterval: 1.0 / 60.0, repeats: true) { [weak self] _ in
            self?.needsDisplay = true
        }
        // .common: keep animating while a menu is open or the panel is being dragged
        RunLoop.main.add(t, forMode: .common)
        timer = t
    }

    private func finishOneShot(_ routine: MascotPack.Routine, elapsed: Double) {
        guard !(routine.loop ?? false), elapsed >= routine.duration, self.routine != fallback else { return }
        // Changing state inside draw() only takes effect next tick, so bounce it to the main queue
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.play(self.fallback, then: self.fallback)
        }
    }

    // MARK: Geometry

    /// Where the sprite sits in view coordinates. Feet near the bottom edge, the rest is jump room.
    var spriteRect: NSRect {
        guard let pack else { return .zero }
        let s = pack.spriteSize
        return NSRect(x: (bounds.width - s.width) / 2, y: pack.footInset,
                      width: s.width, height: s.height)
    }

    /// Layout asks the pack how big it wants to be, rather than the other way round.
    var boxSize: NSSize { pack?.boxSize ?? NSSize(width: 136, height: 124) }
    var footInset: CGFloat { pack?.footInset ?? 6 }
    var overlap: CGFloat { pack?.overlap ?? 3 }

    // MARK: Painting

    override func draw(_ dirtyRect: NSRect) {
        guard let pack else { return }
        let t = frozenTime ?? (CACurrentMediaTime() - started)
        var f = pack.frame(routine: routine, at: t)

        if let r = pack.routines[routine] { finishOneShot(r, elapsed: t) }

        // Blinking is random rather than keyframed: a blink on a fixed beat looks mechanical.
        if let blink = pack.routines[routine]?.blink, frozenTime == nil {
            if t > nextBlink {
                if t < nextBlink + blink.duration { f.eyes = "blink" }
                else { nextBlink = t + Double.random(in: blink.everyMin...blink.everyMax) }
            }
        }

        guard let grid = pack.poses[f.pose] ?? pack.poses.values.first else { return }
        let rect = spriteRect
        let cell = rect.width / CGFloat(pack.grid.cols)
        let eyeChars = pack.eyeCharacterSet
        let eyeTop = grid.firstIndex { $0.contains(where: { eyeChars.contains($0) }) } ?? 0

        NSGraphicsContext.saveGraphicsState()
        let tf = NSAffineTransform()
        let cx = rect.midX, cy = rect.minY + rect.height * 0.42   // pivot low, so it turns on its feet
        tf.translateX(by: cx + f.dx, yBy: cy + f.dy)
        tf.rotate(byRadians: f.rot)
        tf.scaleX(by: f.sx, yBy: f.sy)
        tf.translateX(by: -cx, yBy: -cy)
        tf.concat()

        // Collect each colour into one path and fill once. Filling cell by cell antialiases every
        // boundary separately once scaling lands them off-pixel, and a faint grid appears on the body.
        var paths: [String: NSBezierPath] = [:]
        for (r, row) in grid.enumerated() {
            for (c, ch) in row.enumerated() {
                var ch = ch
                // Blink hides the top row of the eyes, happy hides the bottom row.
                if eyeChars.contains(ch) {
                    let hide = (f.eyes == "blink" && r == eyeTop) || (f.eyes == "happy" && r != eyeTop)
                    if hide { ch = pack.skinCharacter }
                }
                guard let color = pack.color(for: ch, accent: Style.accent) else { continue }
                let key = color.description
                let path = paths[key] ?? { let p = NSBezierPath(); paths[key] = p; return p }()
                path.appendRect(NSRect(x: rect.minX + CGFloat(c) * cell,
                                       y: rect.minY + rect.height - CGFloat(r + 1) * cell,
                                       width: cell, height: cell))
                colors[key] = color
            }
        }
        for (key, path) in paths {
            colors[key]?.setFill()
            path.fill()
        }
        NSGraphicsContext.restoreGraphicsState()
    }

    private var colors: [String: NSColor] = [:]
}

extension MascotPack {
    /// The character an eye turns into when it closes.
    var skinCharacter: Character {
        if let s = skin, let ch = s.first { return ch }
        for (ch, spec) in palette where spec.lowercased() == "accent" { return Character(ch) }
        return palette.keys.sorted().first.map { Character($0) } ?? "#"
    }
}
