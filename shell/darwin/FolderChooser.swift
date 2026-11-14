// The Project folder picker in the Mac app is the system's own panel.
import AppKit
import WebKit

/// Opens the macOS folder panel when the console asks to add a Project on
/// this Mac, and answers with the chosen path or null when cancelled.
///
/// The panel brings what a person expects from choosing a folder: search, the
/// Finder sidebar, new folders, and dropping a folder from Finder. A folder
/// chosen here needs no separate permission prompt, which a daemon walking the
/// home folder would have raised for Desktop, Documents and Downloads. The
/// first panel opens in Documents; later ones return to where the last ended.
final class ProjectFolderChooser: NSObject, WKScriptMessageHandlerWithReply {
    static let name = "clawdlineChooseFolder"
    private static let usedKey = "projectFolderChooserUsed"
    private let trustedOrigin: URL
    private var open = false

    init(trustedOrigin: URL) { self.trustedOrigin = trustedOrigin }

    func userContentController(_ controller: WKUserContentController,
                               didReceive message: WKScriptMessage,
                               replyHandler: @escaping (Any?, String?) -> Void) {
        let origin = message.frameInfo.securityOrigin
        guard CloudPairingAccess.allows(mainFrame: message.frameInfo.isMainFrame,
            scheme: origin.`protocol`, host: origin.host, port: origin.port,
            page: message.webView?.url, trusted: trustedOrigin)
        else { replyHandler(nil, "untrusted_origin"); return }
        guard let window = message.webView?.window else { replyHandler(nil, "no_window"); return }
        guard !open else { replyHandler(nil, "busy"); return }
        open = true

        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.canCreateDirectories = true
        panel.allowsMultipleSelection = false
        panel.message = L.t.folderPanelMessage
        panel.prompt = L.t.folderPanelPrompt
        let defaults = UserDefaults.standard
        if !defaults.bool(forKey: Self.usedKey) {
            panel.directoryURL = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first
        }
        panel.beginSheetModal(for: window) { [weak self] response in
            self?.open = false
            guard response == .OK, let url = panel.url else {
                replyHandler(NSNull(), nil)
                return
            }
            defaults.set(true, forKey: Self.usedKey)
            shellLog("folder-chooser: a Project folder was chosen")
            replyHandler(url.path, nil)
        }
    }
}
