import AppKit

/// The selected D-01 + C-03 + H-04 mascot, without the website's separate line.
/// Coordinates follow internal/domain/icon/brand.svg, cropped to its mascot.
enum RoundedBrandMascot {
    /// Existing installations have a copy of the original bundled pack. Only
    /// that exact copy follows a brand update; edited packs remain user artwork.
    static func replacesDefault(named name: String, data: Data, bundled: Data?) -> Bool {
        name == "clawd" && bundled == data
    }

    static func draw(in rect: NSRect, eyes: String) {
        guard rect.width > 0 else { return }
        NSGraphicsContext.saveGraphicsState()
        defer { NSGraphicsContext.restoreGraphicsState() }
        let transform = NSAffineTransform()
        transform.translateX(by: rect.minX, yBy: rect.minY)
        transform.scale(by: rect.width / 199)
        transform.concat()

        let tiles = NSBezierPath()
        for row in 0..<4 {
            for column in 0..<6 {
                var tile = NSRect(x: 29 + CGFloat(column) * 24,
                                  y: 93 - CGFloat(row) * 24 - 21,
                                  width: 21, height: 21)
                if row == 1 && (column == 1 || column == 4) {
                    guard eyes == "blink" || eyes == "happy" else { continue }
                    // Keep the existing half-closed blinking/sleeping expression.
                    tile.size.height /= 2
                    if eyes == "blink" { tile.origin.y += tile.height }
                }
                tiles.append(NSBezierPath(roundedRect: tile, xRadius: 3, yRadius: 3))
            }
        }
        for x in [CGFloat(0), 178] {
            tiles.append(NSBezierPath(roundedRect: NSRect(x: x, y: 24, width: 21, height: 21),
                                      xRadius: 3, yRadius: 3))
        }
        tiles.addClip()
        // The SVG's white .28 -> clear -> black .12 overlay on #d97757.
        let orange = NSColor(srgbRed: 217 / 255, green: 119 / 255, blue: 87 / 255, alpha: 1)
        let light = NSColor(srgbRed: (217 * 0.72 + 255 * 0.28) / 255,
                            green: (119 * 0.72 + 255 * 0.28) / 255,
                            blue: (87 * 0.72 + 255 * 0.28) / 255, alpha: 1)
        let dark = NSColor(srgbRed: 217 * 0.88 / 255, green: 119 * 0.88 / 255,
                           blue: 87 * 0.88 / 255, alpha: 1)
        NSGradient(colors: [light, orange, dark], atLocations: [0, 0.45, 1],
                   colorSpace: .sRGB)?.draw(from: NSPoint(x: 3.5, y: 94.5),
                                           to: NSPoint(x: 195.5, y: -1.5),
                                           options: [.drawsBeforeStartingLocation, .drawsAfterEndingLocation])
    }
}
