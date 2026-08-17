import AppKit

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

    /// One line to add to the prompt.
    static func insertion(for paths: [String]) -> String {
        paths.map(quoted).joined(separator: " ")
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
