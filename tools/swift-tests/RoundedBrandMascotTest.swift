import AppKit

@main struct RoundedBrandMascotTest {
    static func main() throws {
        let original = Data("bundled pack".utf8)
        precondition(RoundedBrandMascot.replacesDefault(named: "clawd", data: original, bundled: original))
        precondition(!RoundedBrandMascot.replacesDefault(named: "clawd", data: Data("custom drawing".utf8), bundled: original))
        precondition(!RoundedBrandMascot.replacesDefault(named: "mochi", data: original, bundled: original))
        precondition(!RoundedBrandMascot.replacesDefault(named: "clawd", data: original, bundled: nil))

        func render(_ eyes: String, width: Int = 199) -> NSBitmapImageRep {
            let height = Int(ceil(Double(width) * 93 / 199))
            let image = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height,
                bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                colorSpaceName: .deviceRGB, bytesPerRow: width * 4, bitsPerPixel: 32)!
            NSGraphicsContext.saveGraphicsState()
            NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: image)
            NSColor.clear.setFill()
            NSRect(x: 0, y: 0, width: width, height: height).fill(using: .copy)
            RoundedBrandMascot.draw(in: NSRect(x: 0, y: 0, width: width, height: height), eyes: eyes)
            NSGraphicsContext.restoreGraphicsState()
            return image
        }
        func components(_ image: NSBitmapImageRep) -> Int {
            let width = image.pixelsWide, height = image.pixelsHigh
            var seen = Set<Int>(), count = 0
            func painted(_ index: Int) -> Bool {
                // At 2x notch scale a gap is about one physical pixel. Exclude
                // its partly covered antialiasing pixels from the solid islands.
                image.colorAt(x: index % width, y: index / width)!.alphaComponent > 0.8
            }
            for index in 0..<(width * height) where !seen.contains(index) && painted(index) {
                count += 1
                var pending = [index]
                seen.insert(index)
                while let next = pending.popLast() {
                    let x = next % width, y = next / width
                    for (nx, ny) in [(x - 1, y), (x + 1, y), (x, y - 1), (x, y + 1)] {
                        guard nx >= 0, nx < width, ny >= 0, ny < height else { continue }
                        let neighbor = ny * width + nx
                        if !seen.contains(neighbor) && painted(neighbor) {
                            seen.insert(neighbor)
                            pending.append(neighbor)
                        }
                    }
                }
            }
            return count
        }
        let open = render("open"), blink = render("blink"), happy = render("happy")
        // Independent painted islands establish both the tile gaps and floating hands.
        precondition(components(open) == 24)
        precondition(components(blink) == 26 && components(happy) == 26)
        precondition(open.tiffRepresentation != blink.tiffRepresentation)
        precondition(blink.tiffRepresentation != happy.tiffRepresentation)
        let small = render("open", width: 64)
        precondition(components(small) == 24, "Tiles must remain separate at notch scale: \(components(small))")
        for image in [open, small] {
            var brightness = [CGFloat]()
            for y in 0..<image.pixelsHigh {
                for x in 0..<image.pixelsWide {
                    guard let color = image.colorAt(x: x, y: y)?.usingColorSpace(.sRGB),
                          color.alphaComponent > 0.9 else { continue }
                    precondition(color.redComponent > color.greenComponent && color.greenComponent > color.blueComponent)
                    brightness.append(color.redComponent + color.greenComponent + color.blueComponent)
                }
            }
            precondition(brightness.max()! - brightness.min()! > 0.4, "Warm light must remain visible")
        }
        if CommandLine.arguments.count == 2 {
            let directory = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            for (name, image) in [("open", open), ("blink", blink), ("happy", happy), ("notch-2x", small)] {
                try image.representation(using: .png, properties: [:])!.write(to: directory.appendingPathComponent("\(name).png"))
            }
        }
        print("Rounded brand: default-copy selection, custom-pack preservation, floating tiles, expressions and notch-scale warm light passed")
    }
}
