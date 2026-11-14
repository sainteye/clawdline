// Full paths are a native drag fact: browsers intentionally expose only names.
import AppKit
import WebKit

/// Each view owns a one-use snapshot made by an actual native drop. The bridge
/// never reads the current drag pasteboard in response to a page request.
final class FileDropPaths: NSObject, WKScriptMessageHandlerWithReply {
    static let name = "clawdlineDroppedPaths"
    private let trustedOrigin: URL
    var paths: [String] = []

    func capture(_ pasteboard: NSPasteboard) {
        let urls = pasteboard.readObjects(
            forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        paths = urls.map(\.path)
    }

    init(trustedOrigin: URL) { self.trustedOrigin = trustedOrigin }

    func userContentController(_ controller: WKUserContentController,
                               didReceive message: WKScriptMessage,
                               replyHandler: @escaping (Any?, String?) -> Void) {
        let origin = message.frameInfo.securityOrigin
        guard CloudPairingAccess.allows(mainFrame: message.frameInfo.isMainFrame,
            scheme: origin.`protocol`, host: origin.host, port: origin.port,
            page: message.webView?.url, trusted: trustedOrigin)
        else { replyHandler(nil, "untrusted_origin"); return }
        let dropped = paths
        paths = []
        replyHandler(dropped, nil)
    }
}

final class FileDropWebView: WKWebView {
    private let fileDrops: FileDropPaths

    init(frame: NSRect, configuration: WKWebViewConfiguration, trustedOrigin: URL) {
        fileDrops = FileDropPaths(trustedOrigin: trustedOrigin)
        configuration.userContentController.addScriptMessageHandler(
            fileDrops, contentWorld: .page, name: FileDropPaths.name)
        configuration.userContentController.addScriptMessageHandler(
            ProjectFolderChooser(trustedOrigin: trustedOrigin), contentWorld: .page, name: ProjectFolderChooser.name)
        super.init(frame: frame, configuration: configuration)
    }

    required init?(coder: NSCoder) { fatalError("this shell builds its views in code") }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        fileDrops.paths = []
        return super.draggingEntered(sender)
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        fileDrops.capture(sender.draggingPasteboard)
        let accepted = super.performDragOperation(sender)
        if !accepted { fileDrops.paths = [] }
        return accepted
    }
}
