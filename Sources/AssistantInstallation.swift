import Foundation
#if canImport(ClawdlineApplication)
import ClawdlineApplication // W3-1 correction: real cross-module import, see Sources/HostPorts.swift
#endif

/// `Assistant.isInstalled`/`.available`, moved out of `Sources/Assistant.swift` (W3-1) so that
/// file can be a real `ClawdlineCore` SwiftPM target member.
///
/// This is the one place in the whole `Assistant` type that touches the filesystem
/// (`FileManager`) and, through `Codex.home`, another production file with platform effects of
/// its own — everything else in `Assistant.swift` is a closed enum over Foundation types with no
/// external reference. Moving these two members changes nothing about what they do: a Swift
/// extension in a different file is identical, at compile time and at runtime, to the same
/// members declared inline, so every existing caller of `assistant.isInstalled` or
/// `Assistant.available` keeps compiling and behaving exactly as before. What changes is only
/// which file `swiftc`/SwiftPM has to resolve to type-check the *rest* of `Assistant.swift`.
extension Assistant {
    /// Whether this assistant has ever run on this Mac.
    ///
    /// Its home directory being there, which is not the same question as the binary being on
    /// `PATH` — and is the only one that can be answered from an app launched from Finder, which
    /// inherits no login shell and therefore no `PATH` worth reading. It is also the better
    /// question: a directory full of sessions is proof the thing ran here, where a binary on a
    /// path is a promise that it could.
    ///
    /// What it decides is whether to *offer* starting one. Getting it wrong in the shy direction
    /// costs a button; getting it wrong the other way opens a tab that says "command not found".
    var isInstalled: Bool {
        let url: URL
        switch self {
        case .claude:
            url = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".claude", isDirectory: true)
        case .codex:
            url = Codex.home
        }
        var isDirectory: ObjCBool = false
        return FileManager.default.fileExists(atPath: url.path, isDirectory: &isDirectory)
            && isDirectory.boolValue
    }

    /// The ones worth offering, in the order they should be offered in.
    ///
    /// Never empty. A Mac with neither home directory on it is one where nothing has run yet,
    /// and answering "nothing" there would leave a person with no way to start the first
    /// session — so the answer is the whole list, which is what it was before any of this
    /// checked anything.
    static var available: [Assistant] {
        let installed = allCases.filter(\.isInstalled)
        return installed.isEmpty ? allCases : installed
    }
}
