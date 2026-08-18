#!/usr/bin/env swift
// Draw the app icon and build Clawdline.icns.
//
// Run it after changing the art: `swift tools/make-icon.swift`. The result is committed, because
// an icon is not something that changes with a build and asking every build to spend two seconds
// on one nobody edited is a tax on the thing this project is proudest of — that `./build.sh`
// takes a few seconds and needs nothing installed.
//
// `iconutil` is part of macOS, so this adds no dependency. The art is the same creature the web
// interface puts on a browser tab (`Sources/RemoteIcon.swift`); it is written out again here
// rather than shared, because a build tool that imports the app's sources is a build tool that
// stops working the day somebody moves a file.

import AppKit
import Foundation

let rows = [
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

let body = NSColor(srgbRed: 0.851, green: 0.467, blue: 0.341, alpha: 1)   // #D97757
let eye = NSColor(srgbRed: 0.078, green: 0.078, blue: 0.086, alpha: 1)    // #141416
let ground = NSColor(srgbRed: 0.086, green: 0.086, blue: 0.102, alpha: 1) // #16161A

func render(_ size: Int) -> Data? {
    guard let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size,
        bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
        colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { return nil }

    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    let side = CGFloat(size)

    // macOS leaves a margin around an app icon and rounds it at about 22% of the side. Drawn
    // rather than left to the system, because an icon that fills its square looks bigger than
    // every other icon in the Dock and reads as a mistake.
    let inset = side * 0.09
    let tile = NSRect(x: inset, y: inset, width: side - inset * 2, height: side - inset * 2)
    let path = NSBezierPath(roundedRect: tile,
                            xRadius: tile.width * 0.2237, yRadius: tile.width * 0.2237)
    ground.setFill()
    path.fill()

    let columns = CGFloat(rows[0].count), lines = CGFloat(rows.count)
    let cell = (tile.width * 0.72) / columns
    let originX = tile.midX - cell * columns / 2
    let originY = tile.midY - cell * lines / 2

    for (row, line) in rows.enumerated() {
        for (column, character) in line.enumerated() {
            let colour: NSColor
            switch character {
            case "#": colour = body
            case "o": colour = eye
            default: continue
            }
            colour.setFill()
            // Ceiling on the size: two adjacent pixels each 6.4 points wide leave a hairline
            // between them, and at 1024 that hairline is what somebody notices.
            NSBezierPath(rect: NSRect(x: originX + CGFloat(column) * cell,
                                      y: originY + (lines - 1 - CGFloat(row)) * cell,
                                      width: ceil(cell), height: ceil(cell))).fill()
        }
    }

    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])
}

let fm = FileManager.default
let root = URL(fileURLWithPath: fm.currentDirectoryPath)
let iconset = root.appendingPathComponent("Resources/Clawdline.iconset")
try? fm.removeItem(at: iconset)
try fm.createDirectory(at: iconset, withIntermediateDirectories: true)

// Every size the iconset wants, each written twice where a retina name shares its pixels.
for (points, scale) in [(16, 1), (16, 2), (32, 1), (32, 2), (128, 1), (128, 2),
                        (256, 1), (256, 2), (512, 1), (512, 2)] {
    guard let png = render(points * scale) else { continue }
    let name = scale == 1 ? "icon_\(points)x\(points).png" : "icon_\(points)x\(points)@2x.png"
    try png.write(to: iconset.appendingPathComponent(name))
}

let icns = Process()
icns.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
icns.arguments = ["-c", "icns", iconset.path,
                  "-o", root.appendingPathComponent("Resources/Clawdline.icns").path]
try icns.run()
icns.waitUntilExit()
try? fm.removeItem(at: iconset)
print(icns.terminationStatus == 0 ? "wrote Resources/Clawdline.icns" : "iconutil failed")
