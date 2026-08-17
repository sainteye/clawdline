import AppKit
import UniformTypeIdentifiers

/// Files and images dropped or pasted onto the bar.
///
/// What gets sent is a **path**, not the bytes. Claude Code reads files itself — images
/// included — so a path is the whole handoff, and it is the same thing you would have typed.
/// Anything else would mean inventing a channel for attachments that the other end does not have.
///
/// An image on the clipboard has no path yet, so one is written for it. Those files are the only
/// thing here that lasts, which is why they are pruned: anything that only grows needs something
/// that makes it smaller, and that something has to say what it did.
enum Drop {

    /// The types worth accepting. Anything else falls through to the text view's own handling,
    /// which is the right answer for dragged text.
    static let acceptedTypes: [NSPasteboard.PasteboardType] = [.fileURL, .png, .tiff]

    static var directory: URL {
        let base = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent("dev.sainteye.clawdline/drops", isDirectory: true)
    }

    /// Paths for whatever is on a pasteboard, writing a file for image data that has none.
    ///
    /// File URLs win when both are present: a dragged PNG offers its bytes as well as its path,
    /// and copying it into the cache when it already has a home would leave two of it.
    static func paths(from pasteboard: NSPasteboard, now: Date = Date()) -> [String] {
        if let urls = pasteboard.readObjects(forClasses: [NSURL.self],
                                             options: [.urlReadingFileURLsOnly: true]) as? [URL],
           !urls.isEmpty {
            return urls.map(\.path)
        }
        for type in [NSPasteboard.PasteboardType.png, .tiff] {
            guard let data = pasteboard.data(forType: type) else { continue }
            if let path = write(data, as: type == .png ? "png" : "tiff", now: now) { return [path] }
        }
        return []
    }

    /// A picture of a file, at the size a line of text can hold.
    ///
    /// An image gets scaled down; anything else gets the icon its type already has in Finder,
    /// which says "a PDF" or "a folder" faster than the extension does.
    static func thumbnail(for path: String, height: CGFloat) -> NSImage {
        let source = NSImage(contentsOfFile: path) ?? NSWorkspace.shared.icon(forFile: path)
        let size = source.size
        guard size.height > 0 else { return source }
        let scaled = NSSize(width: max(1, (size.width / size.height * height).rounded()),
                            height: height)
        let out = NSImage(size: scaled)
        out.lockFocus()
        source.draw(in: NSRect(origin: .zero, size: scaled))
        out.unlockFocus()
        return out
    }

    /// One line to add to the prompt.
    static func insertion(for paths: [String]) -> String {
        paths.map(quoted).joined(separator: " ")
    }

    // MARK: - Handing an image over the way Claude Code expects one

    /// What a prompt is made of on the way out.
    ///
    /// A path is a perfectly good way to give Claude Code a file — it reads them — but an image
    /// sent that way arrives as forty characters of directory in the input line, and the thing
    /// it is a picture of is one tool call away. Pasted, it arrives as `[Image #3]`: in the
    /// message itself, numbered, and something you can point at in the sentence you are writing.
    enum Piece: Equatable {
        case text(String)
        case image(String)   // a path, to be handed over as bytes rather than as characters
    }

    /// Whether this is something Claude Code would show as an image.
    ///
    /// By type rather than by extension: a screenshot saved with no extension at all is still a
    /// PNG, and a `.txt` that somebody renamed is not an image however much it looks like one.
    static func isImage(_ path: String) -> Bool {
        guard let type = UTType(filenameExtension: (path as NSString).pathExtension.lowercased())
        else { return NSImage(contentsOfFile: path) != nil }
        return type.conforms(to: .image)
    }

    /// Split a prompt into what can be typed and what has to be pasted.
    ///
    /// Adjacent text is joined so the common case — no images — stays a single paste, exactly as
    /// it was. Only images are split out; a PDF or a folder is still a path, because a path is
    /// what Claude Code can do something with.
    static func pieces(text: String, imagePaths: [String]) -> [Piece] {
        guard !imagePaths.isEmpty else { return text.isEmpty ? [] : [.text(text)] }
        var out: [Piece] = []
        var rest = Substring(text)
        for path in imagePaths {
            let needle = quoted(path)
            guard let hit = rest.range(of: needle) else { continue }
            let before = String(rest[rest.startIndex..<hit.lowerBound])
            if !before.isEmpty { out.append(.text(before)) }
            out.append(.image(path))
            rest = rest[hit.upperBound...]
        }
        if !rest.isEmpty { out.append(.text(String(rest))) }
        return out
    }

    /// Put an image on the pasteboard. Whoever calls this owes the pasteboard back.
    ///
    /// It is one shared thing, and borrowing it without returning it means somebody loses what
    /// they copied five minutes ago to a feature they never asked for — so `contents(of:)` and
    /// `put(_:on:)` are the other half, and they belong together.
    static func offer(_ path: String, on pasteboard: NSPasteboard = .general) -> Bool {
        guard let image = NSImage(contentsOfFile: path) else { return false }
        pasteboard.clearContents()
        return pasteboard.writeObjects([image])
    }

    /// A copy of what is on the pasteboard, by value.
    ///
    /// The bytes rather than the items: `clearContents()` invalidates every `NSPasteboardItem`
    /// that was on it, so a snapshot holding references would restore nothing at all — and
    /// would look exactly like one that worked.
    static func contents(of pasteboard: NSPasteboard) -> [[NSPasteboard.PasteboardType: Data]] {
        (pasteboard.pasteboardItems ?? []).map { item in
            var copy: [NSPasteboard.PasteboardType: Data] = [:]
            for type in item.types {
                if let data = item.data(forType: type) { copy[type] = data }
            }
            return copy
        }
    }

    static func put(_ saved: [[NSPasteboard.PasteboardType: Data]], on pasteboard: NSPasteboard) {
        pasteboard.clearContents()
        guard !saved.isEmpty else { return }
        let items = saved.map { fields -> NSPasteboardItem in
            let item = NSPasteboardItem()
            for (type, data) in fields { item.setData(data, forType: type) }
            return item
        }
        pasteboard.writeObjects(items)
    }

    /// Quoted only when it has to be. A path in quotes that did not need them still reads fine,
    /// but most paths do not, and the prompt is something a person is about to read.
    static func quoted(_ path: String) -> String {
        let safe = CharacterSet(charactersIn:
            "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+=@~")
        if path.unicodeScalars.allSatisfy(safe.contains) { return path }
        return "'" + path.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    /// A name that sorts by time and says where it came from.
    static func filename(extension ext: String, now: Date) -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyyMMdd-HHmmss-SSS"
        return "clawdline-\(f.string(from: now)).\(ext)"
    }

    private static func write(_ data: Data, as ext: String, now: Date) -> String? {
        let dir = directory
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let url = dir.appendingPathComponent(filename(extension: ext, now: now))
        guard (try? data.write(to: url)) != nil else { return nil }
        prune()
        return url.path
    }

    /// Keep the most recent few. By count rather than by age: how often you paste an image is not
    /// something a clock knows, and a time limit either hoards on a quiet week or throws away
    /// this morning's on a busy one.
    static func prune(keeping: Int = 40) {
        let dir = directory
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: dir.path),
              names.count > keeping else { return }
        let sorted = names.sorted()   // the name carries the timestamp, so this is oldest first
        for name in sorted.prefix(names.count - keeping) {
            try? FileManager.default.removeItem(at: dir.appendingPathComponent(name))
        }
        Log.write("drops: pruned \(names.count - keeping), kept \(keeping)")
    }
}
