import AppKit
import Carbon.HIToolbox

/// The global hotkey goes through Carbon's RegisterEventHotKey.
/// An NSEvent global monitor would also work, but that route needs the accessibility
/// permission — and a tool that opens a text box should not be able to read every key you press.
private func clawdlineHotKeyHandler(_ next: EventHandlerCallRef?,
                                  _ event: EventRef?,
                                  _ userData: UnsafeMutableRawPointer?) -> OSStatus {
    HotKey.shared?.fire()
    return noErr
}

final class HotKey {
    static var shared: HotKey?

    var onFire: (() -> Void)?
    private(set) var spec = ""
    private var ref: EventHotKeyRef?
    private var handlerRef: EventHandlerRef?
    private var handlerInstalled = false

    init() { HotKey.shared = self }

    func fire() { onFire?() }

    @discardableResult
    func register(_ spec: String) -> Bool {
        unregister()
        guard let (code, mods) = HotKey.parse(spec) else { return false }

        if !handlerInstalled {
            var type = EventTypeSpec(eventClass: OSType(kEventClassKeyboard),
                                     eventKind: UInt32(kEventHotKeyPressed))
            InstallEventHandler(GetApplicationEventTarget(), clawdlineHotKeyHandler, 1, &type, nil, &handlerRef)
            handlerInstalled = true
        }

        let hkID = EventHotKeyID(signature: OSType(0x45594C4E), id: 1)   // 'EYLN'
        let status = RegisterEventHotKey(code, mods, hkID, GetApplicationEventTarget(), 0, &ref)
        if status == noErr {
            self.spec = spec
            return true
        }
        ref = nil
        return false
    }

    func unregister() {
        if let ref { UnregisterEventHotKey(ref) }
        ref = nil
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

    /// A keypress, back into the spec string the config stores.
    ///
    /// The settings window records a combination by listening for one, and what it hears is a
    /// key code and a modifier mask — the opposite direction to everything else here. Written as
    /// its own table rather than by searching `keyCodes` backwards, because two names share a
    /// code there ("return"/"enter", "escape"/"esc") and which one came back would be whichever
    /// the dictionary happened to hash first.
    static func spec(forKeyCode code: UInt16, flags: NSEvent.ModifierFlags) -> String? {
        guard let name = codeNames[UInt32(code)] else { return nil }
        var parts: [String] = []
        if flags.contains(.control) { parts.append("control") }
        if flags.contains(.option) { parts.append("option") }
        if flags.contains(.shift) { parts.append("shift") }
        if flags.contains(.command) { parts.append("cmd") }
        // A bare letter is not a hotkey, it is a letter: registering one takes that key away from
        // every app that is frontmost. Function keys are the exception, being nobody's letter.
        guard !parts.isEmpty || name.hasPrefix("f") && name.count <= 3 else { return nil }
        return (parts + [name]).joined(separator: "+")
    }

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
