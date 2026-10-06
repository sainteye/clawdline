// Service mode: the daemon is this user's LaunchAgent, not this shell's.
//
// `clawdline setup` installs the daemon as a LaunchAgent and writes
// `<state dir>/service.json` (internal/adapters/install `ServiceFile`). When
// that file is there the shell never starts the daemon in its bundle — two
// daemons would race for the port, and whichever won, the updater would
// health-check the wrong one — it shows the service's port instead, and its
// 開機時啟動 item turns the LaunchAgent's RunAtLoad on and off rather than
// registering the app with SMAppService. Without the file nothing here applies
// and the shell is exactly what it was.
import Foundation

struct ServiceMode {
    /// "launchd"; a service.json naming anything else is not this Mac's.
    let supervisor: String
    /// The LaunchAgent's label.
    let name: String
    let port: Int
    /// The plist setup wrote.
    let file: URL

    static var fileURL: URL { NextConfig.directory.appendingPathComponent("service.json") }

    /// The state directory's service.json, or nil when the daemon is not
    /// installed as a service. A file that cannot be read is logged and taken
    /// as absent: the shell then starts its own daemon, which finds the port
    /// held if a service is running after all, and says so.
    static func load() -> ServiceMode? {
        guard let data = try? Data(contentsOf: fileURL) else { return nil }
        guard let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let supervisor = obj["supervisor"] as? String, supervisor == "launchd",
              let name = obj["name"] as? String, !name.isEmpty,
              let port = obj["port"] as? Int, port > 0 else {
            shellLog("service mode: \(fileURL.path) is not a launchd service file; ignoring it")
            return nil
        }
        let file: URL
        if let path = obj["file"] as? String, !path.isEmpty {
            file = URL(fileURLWithPath: path)
        } else {
            file = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent("Library/LaunchAgents/\(name).plist")
        }
        return ServiceMode(supervisor: supervisor, name: name, port: port, file: file)
    }

    private func plist() -> [String: Any]? {
        guard let data = try? Data(contentsOf: file) else { return nil }
        return (try? PropertyListSerialization.propertyList(from: data, format: nil)) as? [String: Any]
    }

    /// Whether the LaunchAgent starts at login.
    var runsAtLoad: Bool { (plist()?["RunAtLoad"] as? Bool) ?? false }

    /// Turn start-at-login on or off. Only the plist on disk changes, which is
    /// what launchd reads at the next login; the running daemon, and the
    /// terminals it owns, are left alone. KeepAlive follows: a KeepAlive of
    /// true starts the job whenever it is loaded, so off keeps it only for a
    /// crash (install.LaunchdPlist says the same).
    func setRunsAtLoad(_ on: Bool) throws {
        guard var dict = plist() else {
            throw NSError(domain: "ServiceMode", code: 1,
                          userInfo: [NSLocalizedDescriptionKey: "\(file.path) cannot be read"])
        }
        dict["RunAtLoad"] = on
        dict["KeepAlive"] = on ? true as Any : ["Crashed": true] as Any
        let data = try PropertyListSerialization.data(fromPropertyList: dict, format: .xml, options: 0)
        try data.write(to: file, options: .atomic)
    }
}

/// Read once at launch: whether this shell runs in service mode.
let serviceMode = ServiceMode.load()
