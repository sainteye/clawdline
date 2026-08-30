import AppKit

extension NSAttributedString.Key {
    fileprivate static let sessionImageArtifactID = NSAttributedString.Key(
        "com.tsunamiworks.clawdline.session-image-artifact-id")
    fileprivate static let sessionImageExpiry = NSAttributedString.Key(
        "com.tsunamiworks.clawdline.session-image-expiry")
    fileprivate static let sessionImageState = NSAttributedString.Key(
        "com.tsunamiworks.clawdline.session-image-state")
}

/// Turns byte-free transcript references into native presentation without crossing the owned
/// artifact-store boundary. The attributed string carries only a private opaque-id action; it
/// never prints the id, a path, a URL or image bytes.
enum SessionImagePresentation {
    static let maximumThumbnail = NSSize(width: 320, height: 180)
    private static let previewPrefix = "clawdline://session-image/"

    private enum Materialization {
        case live(SessionImageArtifact, Data)
        case expired(CGFloat)
    }

    static func render(_ artifact: SessionImageArtifact, size: CGFloat,
                       store: SessionImageArtifactStore = SessionImageArtifactStore(),
                       now: Date = Date()) -> NSAttributedString {
        guard Int(now.timeIntervalSince1970) < artifact.expiresAt else {
            return materialize(.expired(size))
        }
        guard case .live(let stored, let data) = store.lookup(id: artifact.id, now: now) else {
            return materialize(.expired(size))
        }
        return materialize(.live(stored, data))
    }

    /// All AppKit presentation objects are constructed inside one queue-identity boundary. The
    /// controller deliberately parses and renders transcripts on a worker; `Thread.isMainThread`
    /// is not a safe predicate after `dispatchMain()`, so this uses the app's named queue seam.
    private static func materialize(_ value: Materialization) -> NSAttributedString {
        materializeWithQueueIdentity(value).text
    }

    private static func materializeWithQueueIdentity(_ value: Materialization)
        -> (text: NSAttributedString, onMainQueue: Bool) {
        onMain(from: "SessionImagePresentation.materialize") {
            let text: NSAttributedString
            switch value {
            case .live(let stored, let data): text = liveTile(stored, data: data)
            case .expired(let size): text = expiredTile(size: size)
            }
            return (text, MainQueue.isCurrent)
        }
    }

    private static func onMain<T>(from site: String, _ work: () -> T) -> T {
        MainQueue.hop(from: site, alreadyOnMain: MainQueue.isCurrent, work)
    }

    private static func liveTile(_ stored: SessionImageArtifact,
                                 data: Data) -> NSAttributedString {
        guard let image = NSImage(data: data) else { return expiredTile(size: 12) }

        // Dimensions come back from the owned store beside the bytes. Transcript metadata is
        // only a reference and never gets to size native views by itself.
        let scale = min(maximumThumbnail.width / CGFloat(stored.width),
                        maximumThumbnail.height / CGFloat(stored.height), 1)
        let bounds = NSRect(x: 0, y: 0,
                            width: max(1, CGFloat(stored.width) * scale),
                            height: max(1, CGFloat(stored.height) * scale))
        image.size = bounds.size
        let attachment = NSTextAttachment()
        attachment.attachmentCell = NSTextAttachmentCell(imageCell: image)
        attachment.bounds = bounds
        let rendered = NSMutableAttributedString(attachment: attachment)
        rendered.addAttributes([
            .link: previewPrefix + stored.id,
            .sessionImageArtifactID: stored.id,
            .sessionImageExpiry: stored.expiresAt,
            .sessionImageState: "live",
            .cursor: NSCursor.pointingHand,
        ], range: NSRange(location: 0, length: rendered.length))
        rendered.append(NSAttributedString(string: "\n"))
        return rendered
    }

    /// Drive the same live-image materializer used by production so tests can observe a real
    /// worker-to-main hop, including NSTextAttachmentCell and NSCursor construction.
    static func exerciseQueueCrossingForTesting(
        artifact: SessionImageArtifact, data: Data) -> Bool {
        let rendered = materializeWithQueueIdentity(.live(artifact, data))
        return rendered.text.length > 0 && rendered.onMainQueue
    }

    static func artifactID(_ link: String) -> String? {
        guard link.hasPrefix(previewPrefix) else { return nil }
        let id = String(link.dropFirst(previewPrefix.count))
        return SessionImageArtifactStore.isArtifactID(id) ? id : nil
    }

    /// A cached transcript with an image is valid only while the owned store still says that
    /// exact opaque id is live. This catches TTL, pruning and missing bytes without exposing a
    /// path or turning the transcript cache into a second artifact store.
    static func cacheIsCurrent(_ text: NSAttributedString,
                               store: SessionImageArtifactStore = SessionImageArtifactStore(),
                               now: Date = Date()) -> Bool {
        var current = true
        text.enumerateAttribute(.sessionImageArtifactID,
                                in: NSRange(location: 0, length: text.length)) { value, _, stop in
            guard let id = value as? String else { return }
            guard case .live = store.liveness(id: id, now: now) else {
                current = false
                stop.pointee = true
                return
            }
        }
        return current
    }

    static func cacheSignature(_ text: NSAttributedString) -> String {
        var states: [String] = []
        text.enumerateAttribute(.sessionImageState,
                                in: NSRange(location: 0, length: text.length)) { value, _, _ in
            if let value = value as? String { states.append(value) }
        }
        return states.isEmpty ? "no-images" : states.joined(separator: ",")
    }

    private static func expiredTile(size: CGFloat) -> NSAttributedString {
        let paragraph = NSMutableParagraphStyle()
        paragraph.alignment = .center
        paragraph.paragraphSpacingBefore = 7
        paragraph.paragraphSpacing = 7
        let value = NSMutableAttributedString(string: "▧  " + L.t.imageExpired + "\n", attributes: [
            .font: NSFont.systemFont(ofSize: max(11, size), weight: .semibold),
            .foregroundColor: NSColor.secondaryLabelColor,
            .backgroundColor: NSColor.controlBackgroundColor,
            .paragraphStyle: paragraph,
            .sessionImageState: "expired",
        ])
        return value
    }
}

/// An enlarged in-app view layered over the transcript panel. Keeping it inside the existing
/// panel avoids turning a preview click into an app switch or dismissing the transcript behind it.
final class SessionImagePreview: NSObject {
    private var overlay: NSView?
    private weak var returnFocus: NSView?

    func show(data: Data, over window: NSWindow, returnFocus: NSView?) {
        close()
        guard let image = NSImage(data: data), let host = window.contentView else { return }
        self.returnFocus = returnFocus

        let overlay = NSView(frame: host.bounds)
        overlay.autoresizingMask = [.width, .height]
        overlay.wantsLayer = true
        overlay.layer?.backgroundColor = NSColor.black.withAlphaComponent(0.90).cgColor
        overlay.layer?.cornerRadius = 16

        let close = NSButton(title: L.t.imageClose, target: self,
                             action: #selector(closePressed))
        close.bezelStyle = .rounded
        close.keyEquivalent = "\u{1b}"
        close.setAccessibilityLabel(L.t.imageClose)
        close.translatesAutoresizingMaskIntoConstraints = false

        let imageView = NSImageView()
        imageView.image = image
        imageView.imageScaling = .scaleProportionallyUpOrDown
        imageView.imageAlignment = .alignCenter
        imageView.setAccessibilityLabel(L.t.imagePreview)
        imageView.translatesAutoresizingMaskIntoConstraints = false

        overlay.addSubview(imageView)
        overlay.addSubview(close)
        host.addSubview(overlay)
        NSLayoutConstraint.activate([
            close.topAnchor.constraint(equalTo: overlay.topAnchor, constant: 18),
            close.trailingAnchor.constraint(equalTo: overlay.trailingAnchor, constant: -18),
            imageView.topAnchor.constraint(equalTo: close.bottomAnchor, constant: 14),
            imageView.leadingAnchor.constraint(equalTo: overlay.leadingAnchor, constant: 28),
            imageView.trailingAnchor.constraint(equalTo: overlay.trailingAnchor, constant: -28),
            imageView.bottomAnchor.constraint(equalTo: overlay.bottomAnchor, constant: -28),
        ])
        self.overlay = overlay
        window.makeFirstResponder(close)
    }

    func close() {
        overlay?.removeFromSuperview()
        overlay = nil
        if let returnFocus, let window = returnFocus.window {
            window.makeFirstResponder(returnFocus)
        }
        returnFocus = nil
    }

    @objc private func closePressed() { close() }
}
