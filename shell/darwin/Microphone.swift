// What this shell answers for the console's page: the microphone, the image
// picker, and where a link that opens a new window goes.
//
// A WKWebView refuses `getUserMedia` unless its `WKUIDelegate` says otherwise,
// and it refuses it silently from the page's point of view — the promise
// rejects with `NotAllowedError`, which is the same thing a person clicking
// Deny produces. So without this the dictation button in the console would
// look broken inside the app and work in a browser, with nothing on screen to
// say which of the two was happening.
//
// **The camera is not granted, and that is not caution for its own sake.** The
// console asks for `{ audio: true }` and nothing else (`legacy/voice-bridge.ts`),
// so a camera request from this page would mean the page is not the page this
// shell shipped. Answering `.deny` for it costs a feature that does not exist
// and closes the one that would matter.
//
// The system still asks the person once, the first time: macOS shows the
// microphone prompt for the application, using `NSMicrophoneUsageDescription`
// from `Info.plist` (written by `tools/package-macos.sh`). A bundle without
// that key is killed rather than prompted, which is why the two changes belong
// to the same piece of work.
import AppKit
import UniformTypeIdentifiers
import WebKit

extension Shell: WKUIDelegate {
    /// The file input behind the composer's `+` button.
    ///
    /// Safari supplies this panel itself. A macOS WKWebView does not: when its
    /// UI delegate leaves this method unimplemented, clicking `<input
    /// type="file">` reaches WebKit and then opens nothing. That made the same
    /// attachment control work in the Cloud browser and look dead in the app.
    ///
    /// It is a sheet on the console window rather than a modal run, so the web
    /// view that is waiting for the completion handler keeps its run loop. The
    /// page declares `accept="image/*"`; the native boundary repeats that
    /// constraint instead of allowing an arbitrary file to enter the page.
    func webView(_ webView: WKWebView,
                 runOpenPanelWith parameters: WKOpenPanelParameters,
                 initiatedByFrame frame: WKFrameInfo,
                 completionHandler: @escaping ([URL]?) -> Void) {
        guard frame.isMainFrame, isOurs(frame.securityOrigin),
              let window = webView.window else {
            shellLog("image-picker: refused a file panel outside the console's main frame")
            completionHandler(nil)
            return
        }

        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = parameters.allowsMultipleSelection
        panel.allowedContentTypes = [.image]
        panel.beginSheetModal(for: window) { response in
            guard response == .OK else {
                completionHandler(nil)
                return
            }
            completionHandler(panel.urls)
        }
    }

    @available(macOS 12.0, *)
    func webView(_ webView: WKWebView,
                 requestMediaCapturePermissionFor origin: WKSecurityOrigin,
                 initiatedByFrame frame: WKFrameInfo,
                 type: WKMediaCaptureType,
                 decisionHandler: @escaping (WKPermissionDecision) -> Void) {
        // Only the daemon's own page. `origin` is the page asking, and the
        // only page this window ever loads is the one at 127.0.0.1 on this
        // machine's port — but a page can navigate, and a grant that does not
        // look at who is asking is one that follows it wherever it goes.
        guard type == .microphone, isOurs(origin) else {
            decisionHandler(.deny)
            return
        }
        // `.grant` and not `.prompt`: the person is asked once by the system,
        // for the application, and a second question from the web layer on top
        // of that one is a dialog that explains nothing and has to be answered
        // every time.
        decisionHandler(.grant)
    }

    /// `target="_blank"` in the console.
    ///
    /// Returning nil is what tells WebKit the request was handled here rather
    /// than refused. A link to somewhere else opens in the person's browser,
    /// which is what the Swift app does with every link it shows; a link to the
    /// console's own origin is a page of this window's and is loaded in it,
    /// with the token, rather than opened somewhere that has none.
    ///
    /// The Cloud tab beside this one has its own delegate and its own answer
    /// to both of these — see Browser.swift.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for action: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        guard let url = action.request.url else { return nil }
        if isConsole(url) {
            shellLog("browser: the console asked for a new window at \(url.absoluteString); loading it here")
            load(url)
        } else {
            shellLog("browser: the console asked for a new window at \(url.absoluteString);"
                     + " handing it to the browser")
            NSWorkspace.shared.open(url)
        }
        return nil
    }

    /// Whether this is the console the shell loaded, rather than somewhere it
    /// was navigated to. Host and port, compared as the URL's own pieces —
    /// never as a prefix of a string, which is how `127.0.0.1.example.com`
    /// gets to be this machine.
    func isOurs(_ origin: WKSecurityOrigin) -> Bool {
        guard origin.`protocol` == "http" || origin.`protocol` == "https" else { return false }
        guard origin.host == home.host else { return false }
        // WebKit reports the default port as 0; this page is never on one.
        // `port` is an `NSInteger` in the SDK on this machine (Swift 6.2,
        // macOS 15) and was written here as if it were an `NSNumber`, which
        // does not compile — the wave that added this file was never packaged.
        let asked: Int? = origin.port == 0 ? nil : origin.port
        return asked == (home.port ?? 80)
    }
}
