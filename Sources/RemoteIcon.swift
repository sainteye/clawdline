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

        let made = onMain(from: "RemoteIcon.onMain") { render(size: size) }
        guard let made else { return nil }
        lock.lock()
        cache[size] = made
        lock.unlock()
        return made
    }

    private static func onMain<T>(from site: String, _ work: () -> T) -> T {
        MainQueue.hop(from: site, alreadyOnMain: MainQueue.isCurrent, work)
    }

    /// Clear one otherwise harmless cache slot so the production `png` path must cross through
    /// `onMain`. Which site that was is read from ``MainQueue/endRecordingHopsForTesting()``, not
    /// returned from here.
    static func exerciseQueueCrossingsForTesting(size: Int) {
        lock.lock()
        cache.removeValue(forKey: size)
        lock.unlock()
        _ = png(size: size)
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

        let made = onMain(from: "RemoteIcon.onMain") {
            renderSplash(width: width, height: height)
        }
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

    // MARK: - The other mark

    /// A project's pixel mark, drawn on the same tile as the app's own.
    ///
    /// **This exists for one surface only: the icon on a push notification.** A phone showing
    /// "clawdline is waiting for an answer" is showing it beside the app's creature, which is
    /// correct and useless — every notification this app has ever sent looks like that one. What
    /// tells two of them apart at a glance is the thing the status line and the list already use
    /// to tell projects apart, and it costs nothing to draw it here too.
    ///
    /// On the app's own ground rather than on nothing, for the reason in ``ground``, and with a
    /// second consequence that only matters here: a notification drawn by the operating system
    /// sits on a background this app does not choose and cannot read. The dark tile is what makes
    /// the mark the same mark on a lock screen at night and in a notification centre at noon.
    ///
    /// Smoothing off for the cells and on for the tile, which is a size decision rather than a
    /// taste one — see ``project(cells:size:)``.
    static func project(cells: [[NSColor?]], size: Int) -> Data? {
        let key = "\(size)|\(signature(of: cells))"
        lock.lock()
        if let hit = projects[key] { lock.unlock(); return hit }
        lock.unlock()

        let made = onMain(from: "RemoteIcon.onMain") {
            renderProject(cells: cells, size: size)
        }
        guard let made else { return nil }
        lock.lock()
        // Bounded, and emptied rather than evicted one at a time. These are a kilobyte each and
        // arrive at the rate somebody starts projects, so the cap is never reached in a day's
        // use; what it is for is the case where it would grow without one.
        if projects.count >= 64 { projects.removeAll() }
        projects[key] = made
        lock.unlock()
        return made
    }

    private static var projects: [String: Data] = [:]

    private static func signature(of cells: [[NSColor?]]) -> String {
        pack(cells) ?? "?"
    }

    private static func renderProject(cells: [[NSColor?]], size: Int) -> Data? {
        let columns = cells.map(\.count).max() ?? 0
        guard columns > 0, !cells.isEmpty else { return nil }
        guard let rep = NSBitmapImageRep(
            bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
            colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { return nil }

        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)

        let side = CGFloat(size)
        // The tile is the one curve here, so it is the one thing smoothed. Everything after it is
        // an axis-aligned rectangle, and smoothing those costs bytes twice over: a soft edge is a
        // gradient, a gradient does not run-length encode, and this PNG is measured — with
        // smoothing left on for the cells the same drawing came out 45% larger, which matters
        // because it travels to a phone.
        NSGraphicsContext.current?.shouldAntialias = true
        ground.setFill()
        NSBezierPath(roundedRect: NSRect(x: 0, y: 0, width: side, height: side),
                     xRadius: side / 5, yRadius: side / 5).fill()
        NSGraphicsContext.current?.shouldAntialias = false

        // Fitted by whichever way round the mark is, so a 7x4 registry drawing and a square
        // hand-drawn one both sit inside the same margin instead of one of them overflowing it.
        let lines = CGFloat(cells.count)
        let across = CGFloat(columns)
        let cell = ((side * 0.78) / max(across, lines)).rounded()
        let originX = ((side - cell * across) / 2).rounded()
        let originY = ((side - cell * lines) / 2).rounded()

        for (row, line) in cells.enumerated() {
            for (column, colour) in line.enumerated() {
                guard let colour else { continue }
                colour.setFill()
                // Rows are written top-down and AppKit's origin is at the bottom.
                NSBezierPath(rect: NSRect(x: originX + CGFloat(column) * cell,
                                          y: originY + (lines - 1 - CGFloat(row)) * cell,
                                          width: cell, height: cell)).fill()
            }
        }

        NSGraphicsContext.restoreGraphicsState()
        return rep.representation(using: .png, properties: [:])
    }

    // MARK: - A mark that fits in a URL

    /// The largest grid that may travel this way, per side.
    ///
    /// The registry's own drawings are 7x4 and a hand-drawn one is a few rows more, so this is
    /// far above anything real. It is here because the packed form arrives from outside as a
    /// string somebody could have written by hand, and "how big a bitmap will you allocate for
    /// me" is not a question to leave open.
    static let projectGridLimit = 32

    /// A grid packed small enough to be a path component.
    ///
    /// **The picture is the name.** The alternative was an id — a hash of the project's path, or
    /// the session's — and both of them put a handle to a particular project into a URL that has
    /// to be fetchable without credentials, because the fetch is made by the operating system
    /// drawing a notification and not by the page. This carries the colours themselves and
    /// nothing else: no path, no session, no project id, nothing to enumerate, and two projects
    /// that happen to look alike honestly do share a URL. It is also why the answer can be cached
    /// for a year — a URL that is its own content can never go stale.
    ///
    /// One byte of width, one of height, then four bytes a cell, alpha zero for the ones that are
    /// not there. 7x4 comes to 114 bytes and 152 characters.
    static func pack(_ cells: [[NSColor?]]) -> String? {
        let columns = cells.map(\.count).max() ?? 0
        guard columns > 0, columns <= projectGridLimit,
              !cells.isEmpty, cells.count <= projectGridLimit else { return nil }
        var out = Data([UInt8(columns), UInt8(cells.count)])
        for row in cells {
            for column in 0..<columns {
                let colour = column < row.count ? row[column] : nil
                guard let c = colour?.usingColorSpace(.sRGB) else {
                    out.append(contentsOf: [0, 0, 0, 0]); continue
                }
                func byte(_ v: CGFloat) -> UInt8 { UInt8(max(0, min(255, (v * 255).rounded()))) }
                out.append(contentsOf: [byte(c.redComponent), byte(c.greenComponent),
                                        byte(c.blueComponent), 255])
            }
        }
        return WebPush.base64url(out)
    }

    /// The inverse, and it is the only thing that reads the string — a route hands it whatever
    /// was in the path, so every length and every byte count is checked here rather than trusted.
    static func unpack(_ text: String) -> [[NSColor?]]? {
        guard let data = WebPush.base64urlDecoded(text), data.count >= 2 else { return nil }
        let bytes = [UInt8](data)
        let columns = Int(bytes[0]), lines = Int(bytes[1])
        guard columns > 0, columns <= projectGridLimit,
              lines > 0, lines <= projectGridLimit,
              bytes.count == 2 + columns * lines * 4 else { return nil }
        var cells: [[NSColor?]] = []
        var at = 2
        for _ in 0..<lines {
            var row: [NSColor?] = []
            for _ in 0..<columns {
                let a = bytes[at + 3]
                row.append(a == 0 ? nil : NSColor(srgbRed: CGFloat(bytes[at]) / 255,
                                                  green: CGFloat(bytes[at + 1]) / 255,
                                                  blue: CGFloat(bytes[at + 2]) / 255,
                                                  alpha: CGFloat(a) / 255))
                at += 4
            }
            cells.append(row)
        }
        return cells
    }

    /// The path a notification points its icon at, or nothing when there is no mark to draw.
    ///
    /// A path and not the picture itself, and that is a measured choice rather than a cautious
    /// one: an iPhone ignores the field either way, and on the platforms that honour it the fetch
    /// is made once and cached for a year, where carrying 2 KB of PNG would be paid on every
    /// message. See the `push` handler in `RemoteServer.serviceWorker()` for what was measured.
    static func projectPath(for grid: ProjectIcon.Grid?, size: Int = 192) -> String? {
        guard let grid, let packed = pack(grid.cells) else { return nil }
        return "/project-\(size)-\(packed).png"
    }
}
