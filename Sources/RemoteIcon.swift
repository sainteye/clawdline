import AppKit
import Foundation

/// The mark the browser puts on a tab, drawn rather than shipped.
///
/// Drawn, because a pixel creature is nine hundred bytes of source and any file of it would be a
/// resampled version of the same thing at one particular size. Here it is rendered at whatever
/// size was asked for, with smoothing off, so a 16-point tab icon and a 512-point home screen
/// icon are both exactly the art and neither is a blurred copy of the other.
///
/// **The art is written down here rather than read from the mascot pack**, and that is deliberate
/// even though it duplicates nine lines. A pack is a preference — somebody can install `mochi` and
/// the character in the notch changes with it, which is the whole point of packs. What a browser
/// tab is showing is not a preference; it is *this app*, and the icon of a thing should not become
/// a different icon because its owner liked a different animal.
enum RemoteIcon {

    /// Clawd, standing. Sixteen wide and eleven tall, `#` body, `o` eye, `.` nothing — the same
    /// grid `Resources/mascots/clawd.json` draws from.
    private static let rows = [
        "................",
        "..############..",
        "..############..",
        "..###oo##oo###..",
        "..###oo##oo###..",
        "################",
        "################",
        "..############..",
        "..############..",
        "..##.##..##.##..",
        "..##.##..##.##..",
    ]

    private static let body = NSColor(srgbRed: 0.851, green: 0.467, blue: 0.341, alpha: 1)   // #D97757
    private static let eye = NSColor(srgbRed: 0.078, green: 0.078, blue: 0.086, alpha: 1)    // #141416
    private static let ground = NSColor(srgbRed: 0.055, green: 0.055, blue: 0.067, alpha: 1) // #0E0E11

    /// **On a ground rather than on nothing.** A transparent mark takes the colour of whatever is
    /// behind it, and a browser tab strip is white on one machine and near-black on the next — so
    /// a creature the colour of the accent would be invisible on exactly one of them. The dark
    /// tile is what makes it the same mark in both places.
    private static let lock = NSLock()
    private static var cache: [Int: Data] = [:]

    /// PNG at `size` points square. Nil only if AppKit refuses, which it does not.
    ///
    /// Rendered on the main thread and remembered. Drawing into an `NSImage` from anywhere else
    /// is the bug this project has already had once — a background thread taking `lockFocus`
    /// produces a blank image and no complaint.
    static func png(size: Int) -> Data? {
        lock.lock()
        if let hit = cache[size] { lock.unlock(); return hit }
        lock.unlock()

        let made = onMain { render(size: size) }
        guard let made else { return nil }
        lock.lock()
        cache[size] = made
        lock.unlock()
        return made
    }

    private static func onMain<T>(_ work: () -> T) -> T {
        if Thread.isMainThread { return work() }
        return DispatchQueue.main.sync(execute: work)
    }

    private static func render(size: Int) -> Data? {
        guard let rep = NSBitmapImageRep(
            bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { return nil }

        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
        NSGraphicsContext.current?.shouldAntialias = true

        let side = CGFloat(size)
        // A superellipse would be more macOS, and at sixteen points nobody can tell — a corner
        // radius of a fifth reads as "app icon" all the way down and stays a rectangle up close.
        let tile = NSBezierPath(roundedRect: NSRect(x: 0, y: 0, width: side, height: side),
                                xRadius: side / 5, yRadius: side / 5)
        ground.setFill()
        tile.fill()

        // The creature is 16×11, so it is fitted by width and centred by height, with a margin
        // that keeps it off the rounded corners.
        let columns = CGFloat(rows.first?.count ?? 16), lines = CGFloat(rows.count)
        let cell = (side * 0.78) / columns
        let originX = (side - cell * columns) / 2
        let originY = (side - cell * lines) / 2

        for (row, line) in rows.enumerated() {
            for (column, character) in line.enumerated() {
                let colour: NSColor
                switch character {
                case "#": colour = body
                case "o": colour = eye
                default: continue
                }
                colour.setFill()
                // Rows are written top-down and AppKit's origin is at the bottom, so the row index
                // counts from the other end. Ceiling on the size, because a pixel that is 6.4
                // points wide next to one that is 6.4 points wide leaves a hairline between them.
                let rect = NSRect(x: originX + CGFloat(column) * cell,
                                  y: originY + (lines - 1 - CGFloat(row)) * cell,
                                  width: ceil(cell), height: ceil(cell))
                NSBezierPath(rect: rect).fill()
            }
        }

        NSGraphicsContext.restoreGraphicsState()
        return rep.representation(using: .png, properties: [:])
    }

    /// A launch image, at whatever pixel size a device happens to be.
    ///
    /// **iOS still does not build one from the manifest**, ten years in: `background_color` is
    /// what Android uses, and Safari wants `apple-touch-startup-image` with a media query per
    /// device — twenty of them, matched on width, height, pixel ratio and orientation. Without
    /// them a home-screen app opens onto whatever Safari decides to draw, which on a dark page is
    /// a black rectangle for as long as the page takes to arrive, and reads as a hang.
    ///
    /// Drawn here rather than shipped as twenty files for the same reason the icon is: they would
    /// be twenty resampled copies of nine hundred bytes of source, and the list of devices grows
    /// every autumn. The page asks for the size it needs and gets it.
    ///
    /// The ground matches the manifest's `background_color` exactly, so the moment the page does
    /// arrive nothing changes colour — a splash that hands over to a different shade is a flash,
    /// and a flash is the thing this exists to remove.
    static func splash(width: Int, height: Int) -> Data? {
        let key = width * 100_000 + height
        lock.lock()
        if let hit = splashes[key] { lock.unlock(); return hit }
        lock.unlock()

        let made = onMain { renderSplash(width: width, height: height) }
        guard let made else { return nil }
        lock.lock()
        // Bounded: a device asks for one size and asks again next launch, so the cache should hold
        // the handful this machine actually serves rather than every size ever requested.
        if splashes.count > 12 { splashes.removeAll() }
        splashes[key] = made
        lock.unlock()
        return made
    }

    private static var splashes: [Int: Data] = [:]

    private static func renderSplash(width: Int, height: Int) -> Data? {
        guard width > 0, height > 0, width <= 4096, height <= 4096,
              let rep = NSBitmapImageRep(
                bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height,
                bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { return nil }

        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
        NSColor(srgbRed: 0.055, green: 0.055, blue: 0.067, alpha: 1).setFill()   // #0E0E11
        NSBezierPath(rect: NSRect(x: 0, y: 0, width: CGFloat(width), height: CGFloat(height))).fill()

        // A quarter of the short edge. Big enough to be the thing you see, small enough that it is
        // still a launch screen rather than a poster.
        let columns = CGFloat(rows.first?.count ?? 16), lines = CGFloat(rows.count)
        let cell = (CGFloat(min(width, height)) * 0.26 / columns).rounded()
        let originX = (CGFloat(width) - cell * columns) / 2
        let originY = (CGFloat(height) - cell * lines) / 2

        for (row, line) in rows.enumerated() {
            for (column, character) in line.enumerated() {
                let colour: NSColor
                switch character {
                case "#": colour = body
                case "o": colour = eye
                default: continue
                }
                colour.setFill()
                NSBezierPath(rect: NSRect(x: originX + CGFloat(column) * cell,
                                          y: originY + (lines - 1 - CGFloat(row)) * cell,
                                          width: cell, height: cell)).fill()
            }
        }

        NSGraphicsContext.restoreGraphicsState()
        return rep.representation(using: .png, properties: [:])
    }

    /// The same PNG, wrapped in the container `/favicon.ico` has to be.
    ///
    /// Worth the twenty-two bytes of header: a browser asks for `/favicon.ico` on its own, without
    /// being told to by the page, so this is the one icon that works before anybody has edited any
    /// HTML. ICO has been allowed to contain a PNG since Vista, so there is no second encoder here
    /// — it is a directory of one entry pointing at the bytes above.
    static func ico(size: Int = 64) -> Data? {
        guard let png = png(size: size) else { return nil }
        var out = Data()
        func u16(_ v: Int) { out.append(UInt8(v & 0xff)); out.append(UInt8((v >> 8) & 0xff)) }
        func u32(_ v: Int) { u16(v & 0xffff); u16((v >> 16) & 0xffff) }
        u16(0)                                  // reserved
        u16(1)                                  // an icon, not a cursor
        u16(1)                                  // one image in it
        out.append(UInt8(size >= 256 ? 0 : size))   // 0 means 256 — the field is one byte
        out.append(UInt8(size >= 256 ? 0 : size))
        out.append(0)                           // palette size, meaningless for a PNG
        out.append(0)                           // reserved
        u16(1)                                  // colour planes
        u16(32)                                 // bits per pixel
        u32(png.count)
        u32(22)                                 // where the bytes start: after this directory
        out.append(png)
        return out
    }
}
