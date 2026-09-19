// The global hotkey, ported from the Swift app's HotKey.swift.
//
// Carbon's RegisterEventHotKey rather than an NSEvent global monitor: the
// monitor route needs the accessibility permission, and a tool that opens a
// window should not be able to read every key you press.
//
// Where the combination comes from is NextConfig. With nothing configured it is
// option+space, the Swift app's own default: that app is retired, and nothing
// on a stock Mac answers ⌥Space, so a fresh install has a hotkey without anyone
// having to choose one. A combination in the file is used as written, and an
// empty one registers nothing.
//
// What happens when something else already answers the combination depends on
// what that something is:
//
//   - One of macOS's own shortcuts (Spotlight's ⌘Space, the input source's
//     ⌃Space, …). RegisterEventHotKey does not refuse these, so `register` asks
//     CopySymbolicHotKeys first, registers nothing, and records why.
//   - Another app's RegisterEventHotKey. That is invisible from here: Carbon
//     takes the same combination from a second process without complaint, and
//     one press then fires in both (measured on macOS 15.6: both registrations
//     answered noErr and both handlers ran). The one such app this file can
//     name is the retired Swift app — see `legacyHolds`.
//   - A refusal from RegisterEventHotKey itself, kept with its status.
//
// Each is a `Trouble`, kept until the next attempt and said on the settings
// page, under the chip; a launch or a reload that finds one also says so once,
// in a short alert (main.swift). A combination detached because the frontmost
// app is outside its scope is not trouble; it is the scope working — and the
// scope is why the default is tolerable at all: out of the box it is held only
// while iTerm2 or this app is in front, so everywhere else ⌥Space stays
// whoever else's it is.
import AppKit
import Carbon.HIToolbox

private func nextHotKeyHandler(_ next: EventHandlerCallRef?,
                               _ event: EventRef?,
                               _ userData: UnsafeMutableRawPointer?) -> OSStatus {
    HotKey.shared?.fire()
    return noErr
}

final class HotKey {
    static var shared: HotKey?

    /// Why the configured combination is not simply working. The settings page
    /// gets the kind as a fact and has the words for it (copy.ts).
    enum Trouble: Equatable {
        /// The file's combination is not one `parse` can read.
        case unreadable
        /// An enabled macOS shortcut already answers it, so nothing was registered.
        case system
        /// RegisterEventHotKey refused it, with this status.
        case refused(OSStatus)

        var kind: String {
            switch self {
            case .unreadable: return "unreadable"
            case .system: return "system"
            case .refused: return "refused"
            }
        }
    }

    var onFire: (() -> Void)?
    private(set) var spec = ""
    /// What went wrong the last time a combination was asked for; nil once one
    /// registers, or once there is none to ask for.
    private(set) var trouble: Trouble?
    private var ref: EventHotKeyRef?
    private var handlerRef: EventHandlerRef?
    private var handlerInstalled = false

    init() { HotKey.shared = self }

    func fire() { onFire?() }

    /// Whether a combination is registered right now.
    var isRegistered: Bool { ref != nil }

    /// Whether `spec` can be registered at all, asked without registering it:
    /// a combination that does not parse, or one macOS already answers, is
    /// refused whatever the scope. Records what it finds; finding nothing
    /// leaves the record as it was.
    @discardableResult
    func check(_ spec: String) -> Trouble? {
        guard let (code, mods) = HotKey.parse(spec) else {
            trouble = .unreadable
            return trouble
        }
        if HotKey.systemAnswers(code: code, modifiers: mods) {
            trouble = .system
            return trouble
        }
        return nil
    }

    @discardableResult
    func register(_ spec: String) -> Bool {
        unregister()
        guard check(spec) == nil, let (code, mods) = HotKey.parse(spec) else { return false }

        if !handlerInstalled {
            var type = EventTypeSpec(eventClass: OSType(kEventClassKeyboard),
                                     eventKind: UInt32(kEventHotKeyPressed))
            InstallEventHandler(GetApplicationEventTarget(), nextHotKeyHandler, 1, &type, nil, &handlerRef)
            handlerInstalled = true
        }

        // 'CLNX', not the Swift app's 'EYLN': the ids are per process, but a
        // signature that names this app is what a hotkey inspector shows.
        let hkID = EventHotKeyID(signature: OSType(0x434C_4E58), id: 1)
        let status = RegisterEventHotKey(code, mods, hkID, GetApplicationEventTarget(), 0, &ref)
        if status == noErr {
            self.spec = spec
            trouble = nil
            return true
        }
        ref = nil
        trouble = .refused(status)
        return false
    }

    /// Let the combination go. What went wrong last time is kept: the scope
    /// detaching a combination that failed to attach has not fixed it.
    func unregister() {
        if let ref { UnregisterEventHotKey(ref) }
        ref = nil
        spec = ""
    }

    /// Nothing configured: nothing registered, and so nothing wrong.
    func turnOff() {
        unregister()
        trouble = nil
    }

    /// Whether an enabled macOS shortcut is this combination. Only the four
    /// modifiers are compared; the system's entries can carry other bits.
    /// A list that cannot be read answers no, which leaves registration as it
    /// always was rather than refusing every combination.
    static func systemAnswers(code: UInt32, modifiers: UInt32) -> Bool {
        var list: Unmanaged<CFArray>?
        guard CopySymbolicHotKeys(&list) == noErr,
              let rows = list?.takeRetainedValue() as? [[String: Any]] else { return false }
        let four = UInt32(cmdKey | optionKey | controlKey | shiftKey)
        for row in rows where (row[kHISymbolicHotKeyEnabled as String] as? Bool) == true {
            guard let c = row[kHISymbolicHotKeyCode as String] as? Int,
                  let m = row[kHISymbolicHotKeyModifiers as String] as? Int else { continue }
            if UInt32(truncatingIfNeeded: c) == code, UInt32(truncatingIfNeeded: m) & four == modifiers {
                return true
            }
        }
        return false
    }

    /// The retired Swift app. It registers its hotkey the same way this file
    /// does, so if it is opened again both apps hold the combination and one
    /// press opens both input bars — Carbon refuses neither.
    static let legacyBundleID = "com.tsunamiworks.clawdline"

    /// Whether the Swift app is running with the same combination as `spec`.
    ///
    /// Its combination is read from its own file, read-only, by its own rule
    /// (`Config.load`): a non-empty `hotkey`, else option+space. That file is
    /// the Swift app's; this app never writes it.
    static func legacyHolds(_ spec: String) -> Bool {
        guard let ours = parse(spec),
              !NSRunningApplication.runningApplications(withBundleIdentifier: legacyBundleID).isEmpty
        else { return false }
        var theirs = "option+space"
        let file = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline/config.json")
        if let data = try? Data(contentsOf: file),
           let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let v = obj["hotkey"] as? String, !v.isEmpty {
            theirs = v
        }
        guard let other = parse(theirs) else { return false }
        return other == ours
    }

    /// Accepts "option+space", "cmd+shift+k", "⌥space".
    static func parse(_ raw: String) -> (UInt32, UInt32)? {
        var mods: UInt32 = 0
        var keyName = ""

        let normalized = raw.lowercased()
            .replacingOccurrences(of: "⌘", with: "cmd+")
            .replacingOccurrences(of: "⌥", with: "option+")
            .replacingOccurrences(of: "⌃", with: "control+")
            .replacingOccurrences(of: "⇧", with: "shift+")

        for part in normalized.split(separator: "+").map({ $0.trimmingCharacters(in: .whitespaces) }) {
            switch part {
            case "cmd", "command": mods |= UInt32(cmdKey)
            case "opt", "option", "alt": mods |= UInt32(optionKey)
            case "ctrl", "control": mods |= UInt32(controlKey)
            case "shift": mods |= UInt32(shiftKey)
            case "": continue
            default: keyName = part
            }
        }

        guard let code = keyCodes[keyName] else { return nil }
        return (code, mods)
    }

    static func display(_ raw: String) -> String {
        var out = ""
        let n = raw.lowercased()
        if n.contains("control") || n.contains("ctrl") || n.contains("⌃") { out += "⌃" }
        if n.contains("option") || n.contains("opt") || n.contains("alt") || n.contains("⌥") { out += "⌥" }
        if n.contains("shift") || n.contains("⇧") { out += "⇧" }
        if n.contains("cmd") || n.contains("command") || n.contains("⌘") { out += "⌘" }
        let key = n.split(separator: "+").last.map(String.init) ?? ""
        switch key {
        case "space": out += "Space"
        case "return", "enter": out += "↩"
        case "tab": out += "⇥"
        case "escape", "esc": out += "⎋"
        default: out += key.uppercased()
        }
        return out
    }

    /// The combination a key press names, for the settings page's recorder —
    /// the Swift app's `spec(forKeyCode:flags:)`. A bare letter is not a
    /// hotkey, it is a letter: registering one takes that key from every app
    /// that is frontmost. Function keys are the exception, being nobody's
    /// letter. (The original asks `hasPrefix("f") && count <= 3`, which lets
    /// the bare letter f through; this asks for a digit after it.)
    static func spec(forKeyCode code: UInt16, flags: NSEvent.ModifierFlags) -> String? {
        guard let name = codeNames[UInt32(code)] else { return nil }
        var parts: [String] = []
        if flags.contains(.control) { parts.append("control") }
        if flags.contains(.option) { parts.append("option") }
        if flags.contains(.shift) { parts.append("shift") }
        if flags.contains(.command) { parts.append("cmd") }
        let functionKey = name.count >= 2 && name.count <= 3 && name.hasPrefix("f")
            && name.dropFirst().allSatisfy(\.isNumber)
        guard !parts.isEmpty || functionKey else { return nil }
        return (parts + [name]).joined(separator: "+")
    }

    /// Each key code's longest name, so 36 is "return" rather than "enter".
    private static let codeNames: [UInt32: String] = {
        var out: [UInt32: String] = [:]
        for (name, code) in keyCodes where out[code] == nil || name.count > out[code]!.count {
            out[code] = name
        }
        return out
    }()

    private static let keyCodes: [String: UInt32] = [
        "a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9,
        "b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17,
        "1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "=": 24, "9": 25, "7": 26,
        "-": 27, "8": 28, "0": 29, "]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35,
        "return": 36, "enter": 36, "l": 37, "j": 38, "'": 39, "k": 40, ";": 41, "\\": 42,
        ",": 43, "/": 44, "n": 45, "m": 46, ".": 47, "tab": 48, "space": 49, "`": 50,
        "escape": 53, "esc": 53,
        "left": 123, "right": 124, "down": 125, "up": 126,
        "f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
        "f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
    ]
}
