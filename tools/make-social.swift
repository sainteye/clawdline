#!/usr/bin/env swift
//
// Draw the social preview card — docs/assets/social.png, 1280×640.
//
// This is the picture that unfurls when the repository's URL is pasted into X, Slack, Discord or
// LinkedIn. **Without one, GitHub generates a card from the repository's metadata and puts the
// star count on it**, which for a young project means every share broadcasts how few stars it has.
// That is the one field on that card nobody would choose to feature, and uploading any image at
// all removes it.
//
// Designed rather than screenshotted. A screenshot at this size is scaled to a thumbnail in a
// feed, where its text becomes unreadable and it reads as a grey smear; a card with three things
// on it survives that. GitHub also crops the card in some surfaces, so nothing that matters goes
// near the edges.
//
// The mascot grid is copied from `tools/make-icon.swift`, which copied it from
// `Sources/RemoteIcon.swift` — a build tool that imports the app's sources is a build tool that
// stops working the day somebody refactors the app.
//
//   swift tools/make-social.swift
//
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
let eye = NSColor(srgbRed: 0.078, green: 0.078, blue: 0.086, alpha: 1)
let ground = NSColor(srgbRed: 0.078, green: 0.067, blue: 0.059, alpha: 1) // #14110F, the docs' dark
let quiet = NSColor(white: 1, alpha: 0.44)
let faint = NSColor(white: 1, alpha: 0.20)

let W = 1280, H = 640

guard let rep = NSBitmapImageRep(
    bitmapDataPlanes: nil, pixelsWide: W, pixelsHigh: H,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { exit(1) }

NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)

ground.setFill()
NSRect(x: 0, y: 0, width: W, height: H).fill()

// A warmth behind the mascot, the same one the bar draws under it. Radial, so the card has a
// centre without needing a border.
if let shade = NSGradient(colors: [body.withAlphaComponent(0.16), body.withAlphaComponent(0)]) {
    shade.draw(fromCenter: NSPoint(x: 232, y: 396), radius: 0,
               toCenter: NSPoint(x: 232, y: 396), radius: 300, options: [])
}

// The mascot, left, big enough to be the thing you recognise at thumbnail size.
let px: CGFloat = 15
let gridW = px * 16, gridH = px * CGFloat(rows.count)
// **Rounded to whole pixels.** An odd number of rows at 15pt puts the origin on a half pixel,
// and every cell then straddles two, which antialiasing renders as a seam through each row —
// visible as horizontal stripes across a character that is supposed to be made of squares.
let originX = (232 - gridW / 2).rounded(), originY = (396 - gridH / 2).rounded()
for (r, line) in rows.enumerated() {
    for (c, ch) in line.enumerated() where ch != "." {
        (ch == "o" ? eye : body).setFill()
        NSRect(x: originX + CGFloat(c) * px,
               y: originY + CGFloat(rows.count - 1 - r) * px,
               width: px, height: px).fill()
    }
}

func draw(_ s: String, _ x: CGFloat, _ y: CGFloat, _ size: CGFloat,
          _ colour: NSColor, weight: NSFont.Weight = .regular, mono: Bool = false) {
    let f = mono ? NSFont.monospacedSystemFont(ofSize: size, weight: weight)
                 : NSFont.systemFont(ofSize: size, weight: weight)
    NSAttributedString(string: s, attributes: [.font: f, .foregroundColor: colour])
        .draw(at: NSPoint(x: x, y: y))
}

// The name, then the one sentence. **The sentence is the fenced claim**, not a feature list:
// this card is read at a glance and only one idea survives that.
draw("Clawdline", 452, 430, 76, .white, weight: .bold)
draw("Every Claude Code session already running on your Mac —", 456, 372, 30, quiet)
draw("including the ones you started by hand.", 456, 330, 30, quiet)

// Three rows of the list, drawn rather than screenshotted, because at feed size a real screenshot
// of this would be an illegible grey block. One of them is waiting, in the accent colour, because
// that state is the entire argument for the product.
let listX: CGFloat = 456, listTop: CGFloat = 262
let items: [(String, String, NSColor)] = [
    ("investigate the webhook", "Crystallizing… (13m 46s)", faint),
    ("port the Android feature", "waiting for you", body),
    ("draft the release notes", "", faint),
]
for (i, item) in items.enumerated() {
    let y = listTop - CGFloat(i) * 44
    if item.2 == body {
        body.withAlphaComponent(0.10).setFill()
        NSBezierPath(roundedRect: NSRect(x: listX - 14, y: y - 9, width: 700, height: 38),
                     xRadius: 9, yRadius: 9).fill()
        body.setFill()
        NSRect(x: listX - 14, y: y - 9, width: 3, height: 38).fill()
    }
    draw(item.0, listX, y, 21, item.2 == body ? .white : quiet,
         weight: item.2 == body ? .semibold : .regular)
    if !item.1.isEmpty {
        draw(item.1, listX + 262, y + 1, 18, item.2, mono: true)
    }
}

// The claims that make somebody trust it, small, along the bottom.
draw("macOS  ·  Swift, no dependencies  ·  MIT  ·  answer them from your phone",
     456, 84, 19, faint)

NSGraphicsContext.restoreGraphicsState()

let out = "docs/assets/social.png"
guard let png = rep.representation(using: .png, properties: [:]) else { exit(1) }
try? png.write(to: URL(fileURLWithPath: out))
print("→ \(out)  \(W)×\(H)  \(png.count / 1024) KB")
