// The one permission this shell answers for the page: the microphone.
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
import WebKit

extension Shell: WKUIDelegate {
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

    /// Whether this is the console the shell loaded, rather than somewhere it
    /// was navigated to. Host and port, compared as the URL's own pieces —
    /// never as a prefix of a string, which is how `127.0.0.1.example.com`
    /// gets to be this machine.
    func isOurs(_ origin: WKSecurityOrigin) -> Bool {
        guard origin.`protocol` == "http" || origin.`protocol` == "https" else { return false }
        guard origin.host == home.host else { return false }
        // WebKit reports the default port as 0; this page is never on one.
        let asked = origin.port == 0 ? nil : Int(truncating: origin.port)
        return asked == (home.port ?? 80)
    }
}
