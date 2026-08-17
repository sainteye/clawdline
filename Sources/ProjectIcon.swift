import AppKit

/// The little pixel mark and colour that belong to a project.
///
/// The registry is `~/.claude/project-icons.json`, written by
/// [claude-tools](https://github.com/sainteye/claude-tools) for the terminal status line. Reading
/// the same file is what makes the two agree: the icon in the bar and the icon in the terminal
/// are the same icon because they came from the same row, not because two programs were kept in
/// step by hand.
///
/// **Read only.** That file is usually a symlink into a checkout, and writing it through the link
/// replaces it with a plain copy — after which the repository's version is no longer the one
/// running. claude-tools has its own note about that; this side simply never writes.
///
/// With no registry, a colour is derived from the path instead, so the feature still does
/// something for everyone else.
enum ProjectIcon {

    struct Grid {
        var cells: [[NSColor?]]   // rows of colours, nil = transparent
        var accent: NSColor       // the project's colour, for tinting text
    }

    // The palette, ported so both sides land on the same colours. 16 hues around the wheel,
    // each in two tones.
    private static let hues: [Double] = [8, 28, 45, 62, 85, 118, 145, 165,
                                         188, 205, 222, 242, 262, 282, 305, 330]
    /// tone → (body saturation, body lightness, limb saturation, limb lightness)
    private static let tones: [(Double, Double, Double, Double)] = [
        (0.66, 0.62, 0.62, 0.44), (0.38, 0.80, 0.34, 0.62),
    ]

    // MARK: - Registry

    private static var cache: (stamp: String, projects: [String: [String: Any]])?

    static var registryURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".claude/project-icons.json")
    }

    private static func projects() -> [String: [String: Any]] {
        let url = registryURL
        let attrs = try? FileManager.default.attributesOfItem(atPath: url.path)
        let size = (attrs?[.size] as? Int) ?? 0
        let date = (attrs?[.modificationDate] as? Date)?.timeIntervalSince1970 ?? 0
        let stamp = "\(size)-\(Int(date))"
        if let c = cache, c.stamp == stamp { return c.projects }

        var found: [String: [String: Any]] = [:]
        if let data = try? Data(contentsOf: url),
           let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let list = root["projects"] as? [String: Any] {
            for (path, value) in list {
                if let row = value as? [String: Any] { found[path] = row }
            }
        }
        cache = (stamp, found)
        return found
    }

    /// The row for a directory: the longest registered path that contains it.
    ///
    /// Longest rather than exact, because a session sits in `atrium/frontend` while the entry
    /// that names the project is `atrium` — and `atrium/backend` may have a row of its own,
    /// which should win for anything inside it.
    static func entry(forCwd cwd: String, in list: [String: [String: Any]]) -> [String: Any]? {
        var best: (length: Int, row: [String: Any])?
        for (path, row) in list where cwd == path || cwd.hasPrefix(path + "/") {
            if best == nil || path.count > best!.length { best = (path.count, row) }
        }
        return best?.row
    }

    /// The whole row for a directory — the icon is only part of what a project registers.
    static func row(forCwd cwd: String) -> [String: Any]? {
        entry(forCwd: cwd, in: projects())
    }

    static func grid(forCwd cwd: String) -> Grid? {
        if let row = entry(forCwd: cwd, in: projects()) { return grid(for: row) }
        // No registry, or a project it has never seen. The path still decides the colour, so it
        // is at least stable from one launch to the next.
        return creature(seed: stableHash(cwd))
    }

    static func grid(for row: [String: Any]) -> Grid? {
        if let art = row["art"] as? [String: Any], let g = artGrid(art) { return g }
        guard let hue = row["hue"] as? Int else { return nil }
        return creature(hue: hue, tone: row["tone"] as? Int ?? 0, shape: row["shape"] as? Int ?? 0)
    }

    // MARK: - Hand-drawn art

    /// `{"bg": "#2F6B5E", "palette": {"W": "#EEF6F4"}, "rows": [".WWWWW.", …]}`
    ///
    /// Rows need not be the same length; they are padded. A dot, a space or a middle dot is the
    /// background, and an unknown character is too — an icon with a typo in it should come out
    /// slightly wrong, not blank.
    static func artGrid(_ art: [String: Any]) -> Grid? {
        guard let rows = art["rows"] as? [String], !rows.isEmpty else { return nil }
        let background = (art["bg"] as? String).flatMap(color(hex:))
        var look: [Character: NSColor] = [:]
        for (key, value) in (art["palette"] as? [String: String] ?? [:]) {
            if let ch = key.first, let c = color(hex: value) { look[ch] = c }
        }
        let width = rows.map(\.count).max() ?? 1
        let cells = rows.map { row -> [NSColor?] in
            let padded = Array(row) + Array(repeating: " ", count: width - row.count)
            return padded.map { ch in
                ".· ".contains(ch) ? background : (look[ch] ?? background)
            }
        }
        let accent = (art["accent"] as? String).flatMap(color(hex:))
            ?? look.values.first ?? .white
        return Grid(cells: cells, accent: accent)
    }

    // MARK: - Generated creature

    /// 5 wide, 4 tall, mirrored. `ears` and `legs` are three pixels each (1…7, never empty),
    /// `eye` picks which row the eyes sit in — 7 × 7 × 2 = 98 shapes, times 16 hues.
    static func creatureCells(shape: Int) -> [[Int]] {
        let shapes = (1...7).flatMap { e in (1...7).flatMap { l in [(e, l, 0), (e, l, 1)] } }
        let (ears, legs, eye) = shapes[((shape % shapes.count) + shapes.count) % shapes.count]
        var grid = [[Int]](repeating: [Int](repeating: 0, count: 5), count: 4)
        for c in 0..<3 {
            if ears >> c & 1 == 1 { grid[0][c] = 2; grid[0][4 - c] = 2 }
            if legs >> c & 1 == 1 { grid[3][c] = 2; grid[3][4 - c] = 2 }
        }
        for r in 1...2 { for c in 0..<5 { grid[r][c] = 1 } }
        let eyeRow = eye == 0 ? 2 : 1
        grid[eyeRow][1] = 0
        grid[eyeRow][3] = 0
        return grid
    }

    private static func creature(hue hueIndex: Int, tone toneIndex: Int, shape: Int) -> Grid {
        let h = hues[((hueIndex % hues.count) + hues.count) % hues.count]
        let (bs, bl, ls, ll) = tones[((toneIndex % tones.count) + tones.count) % tones.count]
        let body = hsl(h, bs, bl), limb = hsl(h, ls, ll)
        let cells = creatureCells(shape: shape).map { row in
            row.map { v -> NSColor? in v == 1 ? body : (v == 2 ? limb : nil) }
        }
        return Grid(cells: cells, accent: body)
    }

    private static func creature(seed: Int) -> Grid {
        creature(hue: seed % hues.count, tone: (seed / 16) % tones.count, shape: (seed / 512) % 98)
    }

    // MARK: - Colour

    /// Stable across launches, which `hashValue` is not — Swift seeds that per process, so the
    /// same project would change colour every time the app started.
    static func stableHash(_ text: String) -> Int {
        var h: UInt64 = 0xcbf29ce484222325
        for byte in Array(text.utf8) { h = (h ^ UInt64(byte)) &* 0x100000001b3 }
        return Int(h % 0x7fff_ffff)
    }

    static func color(hex: String) -> NSColor? {
        var s = hex.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6, let v = UInt32(s, radix: 16) else { return nil }
        return NSColor(srgbRed: CGFloat((v >> 16) & 0xff) / 255,
                       green: CGFloat((v >> 8) & 0xff) / 255,
                       blue: CGFloat(v & 0xff) / 255, alpha: 1)
    }

    static func hsl(_ hDegrees: Double, _ s: Double, _ l: Double) -> NSColor {
        let h = hDegrees / 360
        let c = (1 - abs(2 * l - 1)) * s
        let x = c * (1 - abs((h * 6).truncatingRemainder(dividingBy: 2) - 1))
        let m = l - c / 2
        let (r, g, b): (Double, Double, Double)
        switch Int(h * 6) % 6 {
        case 0: (r, g, b) = (c, x, 0)
        case 1: (r, g, b) = (x, c, 0)
        case 2: (r, g, b) = (0, c, x)
        case 3: (r, g, b) = (0, x, c)
        case 4: (r, g, b) = (x, 0, c)
        default: (r, g, b) = (c, 0, x)
        }
        return NSColor(srgbRed: CGFloat(r + m), green: CGFloat(g + m), blue: CGFloat(b + m), alpha: 1)
    }
}

/// Draws a `ProjectIcon.Grid` as actual pixels.
///
/// The status line has to fold two rows into one character with a half block; a window does not,
/// so the same grid comes out twice the height and square.
final class ProjectIconView: NSView {
    var grid: ProjectIcon.Grid? { didSet { needsDisplay = true } }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    /// How wide this grid wants to be at a given height, so the label can be placed after it.
    static func width(for grid: ProjectIcon.Grid?, height: CGFloat) -> CGFloat {
        guard let g = grid, let first = g.cells.first, !first.isEmpty else { return 0 }
        let cell = height / CGFloat(g.cells.count)
        return (cell * CGFloat(first.count)).rounded()
    }

    override func draw(_ dirtyRect: NSRect) {
        guard let g = grid, let first = g.cells.first, !first.isEmpty else { return }
        let cell = bounds.height / CGFloat(g.cells.count)
        for (r, row) in g.cells.enumerated() {
            for (c, colour) in row.enumerated() {
                guard let colour else { continue }
                colour.setFill()
                // Rows read top-down; the view's origin is at the bottom.
                NSRect(x: CGFloat(c) * cell,
                       y: bounds.height - CGFloat(r + 1) * cell,
                       width: cell, height: cell).fill()
            }
        }
    }
}
