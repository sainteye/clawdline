// WebKit downloads stay in the app and ask where to save, rather than navigating.
import AppKit
import WebKit

@MainActor
final class WebDownloads: NSObject, WKDownloadDelegate {
    typealias ChooseDestination = @MainActor (NSWindow?, String, @escaping (URL?) -> Void) -> Void
    private struct Transfer {
        let download: WKDownload
        weak var window: NSWindow?
        var destination: URL?
        var staging: URL?
    }
    private var transfers: [ObjectIdentifier: Transfer] = [:]
    private let choose: ChooseDestination
    private let failed: @MainActor (Error) -> Void

    init(choose: ChooseDestination? = nil, failed: (@MainActor (Error) -> Void)? = nil) {
        self.choose = choose ?? WebDownloads.showSavePanel
        self.failed = failed ?? { NSApp.presentError($0) }
    }

    func start(_ download: WKDownload, in window: NSWindow?) {
        transfers[ObjectIdentifier(download)] = Transfer(download: download, window: window)
        download.delegate = self
    }

    static func showSavePanel(window: NSWindow?, name: String, completion: @escaping (URL?) -> Void) {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = (name as NSString).lastPathComponent
        panel.canCreateDirectories = true
        if let window {
            panel.beginSheetModal(for: window) { response in
                completion(response == .OK ? panel.url : nil)
            }
        } else {
            panel.begin { response in completion(response == .OK ? panel.url : nil) }
        }
    }

    func download(_ download: WKDownload, decideDestinationUsing response: URLResponse,
                  suggestedFilename: String, completionHandler: @escaping (URL?) -> Void) {
        let id = ObjectIdentifier(download)
        guard let transfer = transfers[id] else { completionHandler(nil); return }
        choose(transfer.window, suggestedFilename) { [self] destination in
            guard let destination else {
                transfers.removeValue(forKey: id)
                completionHandler(nil)
                return
            }
            do {
                // WebKit requires a nonexistent file. Stage beside the destination so an
                // interrupted download never destroys an existing file approved for replacement.
                let staging = try FileManager.default.url(for: .itemReplacementDirectory,
                    in: .userDomainMask, appropriateFor: destination, create: true)
                transfers[id]?.destination = destination
                transfers[id]?.staging = staging
                completionHandler(staging.appendingPathComponent("download"))
            } catch {
                transfers.removeValue(forKey: id)
                completionHandler(nil)
                failed(error)
            }
        }
    }

    func downloadDidFinish(_ download: WKDownload) {
        guard let transfer = transfers.removeValue(forKey: ObjectIdentifier(download)),
              let staging = transfer.staging, let destination = transfer.destination else { return }
        defer { try? FileManager.default.removeItem(at: staging) }
        do {
            let file = staging.appendingPathComponent("download")
            if FileManager.default.fileExists(atPath: destination.path) {
                _ = try FileManager.default.replaceItemAt(destination, withItemAt: file)
            } else {
                try FileManager.default.moveItem(at: file, to: destination)
            }
        } catch { failed(error) }
    }

    func download(_ download: WKDownload, didFailWithError error: Error, resumeData: Data?) {
        guard let transfer = transfers.removeValue(forKey: ObjectIdentifier(download)) else { return }
        if let staging = transfer.staging { try? FileManager.default.removeItem(at: staging) }
        failed(error)
    }
}
