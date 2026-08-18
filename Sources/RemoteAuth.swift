import CommonCrypto
import CryptoKit
import Foundation

/// Who is allowed to ask, and what they are allowed to ask for.
///
/// **The thing to understand before reading any of this: once a tunnel is running, every request
/// arrives from `127.0.0.1`.** `cloudflared` connects to the local port like any other program on
/// the machine, so the socket address of a request from a phone in another country is
/// indistinguishable from the socket address of a `curl` in another terminal. Loopback is
/// therefore *not* an authorisation signal here, and anything built on "but it is only local"
/// would be wrong in exactly the situation it was meant to cover.
///
/// So the rule is the other way round: **the tunnel refuses to start until this is configured**,
/// and once it is configured every request needs a token, wherever it came from.
///
/// Two kinds of secret, hashed two different ways, and the difference is not an inconsistency:
///
/// - **A device token is 256 random bits.** There is nothing to guess, so there is nothing to slow
///   down; it is stored as a plain SHA-256 and compared in constant time. Stretching it would cost
///   a tenth of a second on every request to defend against an attack that cannot happen.
/// - **A password is chosen by a person.** That is a low-entropy secret and guessing it *is* the
///   attack, so it goes through PBKDF2-HMAC-SHA256 at 600,000 iterations — measured at 98ms on an
///   M-series Mac, which is the right cost for something typed once per login and far too slow to
///   grind through a wordlist.
///
/// The whole store is one file at `~/.config/clawdline/remote.json`, mode `0600`. That is the same
/// trust boundary a Unix socket would give: anything running as you can read it, and nothing else
/// can. A local plugin is meant to read the token out of it — see `docs/remote.md`.
enum RemoteAuth {

    /// What a token is allowed to do. Deliberately an explicit list rather than a level, because
    /// "read" and "send" are not points on one scale — reading leaks a repository name, and
    /// sending is remote code execution.
    enum Capability: String, CaseIterable {
        case read
        case send
        case admin
    }

    struct Device {
        var id: String
        var name: String
        /// SHA-256 of the token, hex. See the note above about why this is not stretched.
        var hash: String
        var caps: Set<Capability>
        var created: Date
        var lastSeen: Date?
        /// Approved on the Mac. A device is created the moment somebody asks to pair and stays
        /// here, unapproved and useless, until a person says yes at the machine.
        var approved: Bool
        /// The one this machine made for itself, so that a script or a plugin running as you can
        /// talk to the server. It counts for authentication and **not** for the tunnel interlock:
        /// a token this app wrote for its own use is not somebody deciding to be reachable.
        var local: Bool = false
    }

    // MARK: - The store

    private static let lock = NSLock()
    private static var loaded = false
    private static var devices: [String: Device] = [:]
    private static var password: (hash: Data, salt: Data, iterations: Int)?

    static var storeURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline/remote.json")
    }

    /// True when a **person** has set this up — a paired device or a password.
    ///
    /// The local token deliberately does not count. It is created automatically so that things
    /// running as you keep working, and if it counted here then simply switching the server on
    /// would satisfy the interlock that stops a tunnel being opened to an endpoint nobody has
    /// decided to expose.
    static var isConfigured: Bool {
        load()
        lock.lock(); defer { lock.unlock() }
        return password != nil || devices.values.contains { $0.approved && !$0.local }
    }

    static var approvedDevices: [Device] {
        load()
        lock.lock(); defer { lock.unlock() }
        return devices.values.filter { $0.approved && !$0.local }.sorted { $0.created < $1.created }
    }

    static func load(force: Bool = false) {
        lock.lock()
        if loaded, !force { lock.unlock(); return }
        loaded = true
        lock.unlock()

        guard let data = try? Data(contentsOf: storeURL),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        var found: [String: Device] = [:]
        for row in obj["devices"] as? [[String: Any]] ?? [] {
            guard let id = row["id"] as? String, let hash = row["hash"] as? String else { continue }
            let caps = Set((row["caps"] as? [String] ?? []).compactMap(Capability.init(rawValue:)))
            found[id] = Device(
                id: id,
                name: row["name"] as? String ?? "?",
                hash: hash,
                caps: caps,
                created: Date(timeIntervalSince1970: row["created"] as? Double ?? 0),
                lastSeen: (row["last_seen"] as? Double).map(Date.init(timeIntervalSince1970:)),
                approved: row["approved"] as? Bool ?? false,
                local: row["local"] as? Bool ?? false)
        }
        var pw: (Data, Data, Int)?
        if let p = obj["password"] as? [String: Any],
           let h = (p["hash"] as? String).flatMap({ Data(base64Encoded: $0) }),
           let s = (p["salt"] as? String).flatMap({ Data(base64Encoded: $0) }) {
            pw = (h, s, p["iterations"] as? Int ?? 600_000)
        }
        lock.lock()
        devices = found
        password = pw
        lock.unlock()
    }

    private static func save() {
        lock.lock()
        let rows = devices.values.map { device -> [String: Any] in
            var row: [String: Any] = [
                "id": device.id, "name": device.name, "hash": device.hash,
                "caps": device.caps.map(\.rawValue).sorted(),
                "created": device.created.timeIntervalSince1970,
                "approved": device.approved,
                "local": device.local,
            ]
            if let seen = device.lastSeen { row["last_seen"] = seen.timeIntervalSince1970 }
            return row
        }
        var obj: [String: Any] = ["version": 1, "devices": rows]
        if let pw = password {
            obj["password"] = ["hash": pw.hash.base64EncodedString(),
                               "salt": pw.salt.base64EncodedString(),
                               "iterations": pw.iterations]
        }
        lock.unlock()

        guard let data = try? JSONSerialization.data(withJSONObject: obj,
                                                     options: [.prettyPrinted, .sortedKeys]) else { return }
        try? FileManager.default.createDirectory(at: storeURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? data.write(to: storeURL, options: .atomic)
        // Set every time rather than only at creation: an atomic write replaces the file, and a
        // replacement made by `Data.write` does not inherit the old one's mode.
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: storeURL.path)
    }

    // MARK: - Verifying a request

    /// What a request turned out to be allowed to do, or why it was not.
    enum Verdict: Equatable {
        case allowed(device: String, caps: Set<Capability>)
        case denied
    }

    /// `Authorization: Bearer <token>` → what it may do.
    ///
    /// Constant time all the way through, including the lookup: a token that names a device that
    /// does not exist takes the same path as one that names a device whose secret is wrong, so the
    /// error cannot be used to enumerate which device ids are real.
    static func verify(bearer: String?) -> Verdict {
        load()
        lock.lock()
        let all = devices
        lock.unlock()
        // No token, no answer — with no exception for "but it came from this machine". See
        // `localToken()` for why loopback is not a boundary.
        guard let token = bearer, !token.isEmpty else { return .denied }

        let digest = hex(SHA256.hash(data: Data(token.utf8)))
        var matched: Device?
        for device in all.values where device.approved {
            if constantTimeEquals(device.hash, digest) { matched = device }
        }
        guard let device = matched else { return .denied }

        lock.lock()
        devices[device.id]?.lastSeen = Date()
        lock.unlock()
        return .allowed(device: device.id, caps: device.caps)
    }

    /// Compare without letting the time taken say how much of it matched.
    ///
    /// Both sides here are hex digests of a fixed length, so the length check leaks nothing — and
    /// the loop deliberately does not stop early, which is the whole point of writing it out
    /// rather than using `==`.
    static func constantTimeEquals(_ a: String, _ b: String) -> Bool {
        let x = Array(a.utf8), y = Array(b.utf8)
        guard x.count == y.count else { return false }
        var difference: UInt8 = 0
        for i in 0..<x.count { difference |= x[i] ^ y[i] }
        return difference == 0
    }

    // MARK: - Pairing

    /// A device waiting to be let in.
    ///
    /// The code is shown **on the Mac** and typed into the browser, never the other way round.
    /// That direction is the entire security property: anybody can reach the pairing route, and
    /// only somebody sitting at the machine can finish it.
    struct Pending {
        let id: String
        let name: String
        let code: String
        let token: String
        let expires: Date
        var attempts: Int
    }

    private static var pending: [String: Pending] = [:]
    /// Shown by the settings window and the menu bar when somebody is asking to pair.
    static var onPairingRequest: ((Pending) -> Void)?

    static func beginPairing(name: String) -> Pending {
        let raw = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let pendingDevice = Pending(
            id: UUID().uuidString.lowercased(),
            name: raw.isEmpty ? "A browser" : String(raw.prefix(40)),
            code: String(format: "%06d", Int.random(in: 0...999_999)),
            token: newToken(),
            // Two minutes. Long enough to walk to the Mac, short enough that a code left on a
            // screen is not a key somebody finds later.
            expires: Date().addingTimeInterval(120),
            attempts: 0)
        lock.lock()
        // One at a time, and a new request replaces the old rather than joining it. Two live
        // codes means two chances to guess, and the person at the Mac is looking at one window.
        pending.removeAll()
        pending[pendingDevice.id] = pendingDevice
        lock.unlock()
        audit("pair.begin", ["device": pendingDevice.name])
        DispatchQueue.main.async { onPairingRequest?(pendingDevice) }
        return pendingDevice
    }

    enum PairResult: Equatable {
        case paired(token: String)
        case wrongCode(left: Int)
        case expired
    }

    /// Five guesses at a six-digit code, then it is gone. A million codes and five tries is not a
    /// number anybody grinds through, and the attempt counter is on the pending record rather than
    /// on a clock, so retrying from a different connection does not reset it.
    static func confirmPairing(id: String, code: String) -> PairResult {
        lock.lock()
        guard var entry = pending[id], entry.expires > Date() else {
            pending.removeValue(forKey: id)
            lock.unlock()
            return .expired
        }
        guard constantTimeEquals(entry.code, code.trimmingCharacters(in: .whitespaces)) else {
            entry.attempts += 1
            if entry.attempts >= 5 {
                pending.removeValue(forKey: id)
                lock.unlock()
                audit("pair.locked", ["device": entry.name])
                return .expired
            }
            pending[id] = entry
            lock.unlock()
            return .wrongCode(left: 5 - entry.attempts)
        }
        pending.removeValue(forKey: id)
        devices[entry.id] = Device(id: entry.id, name: entry.name,
                                   hash: hex(SHA256.hash(data: Data(entry.token.utf8))),
                                   // Reading only. Sending is armed per device, deliberately and
                                   // separately, because it is a different risk entirely.
                                   caps: [.read], created: Date(), lastSeen: nil, approved: true,
                                   local: false)
        lock.unlock()
        save()
        audit("pair.done", ["device": entry.name, "id": entry.id])
        return .paired(token: entry.token)
    }

    /// Approve straight from the Mac — the path a QR code takes, where the person scanning it is
    /// by definition holding the machine's screen.
    @discardableResult
    static func addDevice(name: String, caps: Set<Capability> = [.read],
                          local: Bool = false) -> (id: String, token: String) {
        let id = UUID().uuidString.lowercased()
        let token = newToken()
        lock.lock()
        devices[id] = Device(id: id, name: name, hash: hex(SHA256.hash(data: Data(token.utf8))),
                             caps: caps, created: Date(), lastSeen: nil, approved: true, local: local)
        lock.unlock()
        save()
        audit("device.add", ["device": name, "id": id])
        return (id, token)
    }

    static func revoke(id: String) {
        lock.lock()
        let name = devices[id]?.name ?? "?"
        devices.removeValue(forKey: id)
        lock.unlock()
        save()
        audit("device.revoke", ["device": name, "id": id])
    }

    /// Everything, at once. The control that exists because the moment you want it is the moment
    /// you do not want to be reading a settings window.
    static func revokeAll() {
        lock.lock()
        let count = devices.count
        devices.removeAll()
        password = nil
        pending.removeAll()
        lock.unlock()
        save()
        audit("device.revoke_all", ["count": "\(count)"])
    }

    static func setCapabilities(_ caps: Set<Capability>, for id: String) {
        lock.lock()
        devices[id]?.caps = caps
        lock.unlock()
        save()
        audit("device.caps", ["id": id, "caps": caps.map(\.rawValue).sorted().joined(separator: "+")])
    }

    /// The token this machine keeps for itself, created the first time the server starts.
    ///
    /// **Why there is no such thing as an unauthenticated local port here.** "It is only
    /// listening on loopback" sounds like a boundary and is not one: every process on the machine
    /// can reach loopback, and — the part that actually bites — **so can a web page the user is
    /// visiting**, because a page's JavaScript may `fetch` `http://127.0.0.1:<port>` and, with a
    /// DNS rebinding, may do it as same-origin. An open port here would mean any site could read
    /// somebody's repository names and task titles, and the port number is in a public repository.
    ///
    /// A token in a `0600` file is a real boundary against that specific and very common attack,
    /// because a page cannot read files. It is *not* a boundary against malware already running
    /// as you — nothing at this layer is — and that limit is written down in `docs/remote.md`
    /// rather than glossed over.
    static func localToken() -> String {
        load()
        lock.lock()
        let existing = devices.values.first { $0.local }
        lock.unlock()
        if let existing {
            // The plaintext lives in the token file and only its hash is in the store, so the two
            // are checked against each other rather than trusted. Reading it back matters: without
            // it every launch would revoke and re-mint, the file would change under any script
            // that had cached it, and the audit log would fill with a device being replaced by
            // itself. A mismatch is the only reason to start again.
            if let onDisk = try? String(contentsOf: tokenURL, encoding: .utf8) {
                let token = onDisk.trimmingCharacters(in: .whitespacesAndNewlines)
                if !token.isEmpty, constantTimeEquals(hex(SHA256.hash(data: Data(token.utf8))),
                                                      existing.hash) {
                    cachedLocalToken = token
                    return token
                }
            }
            if let cached = cachedLocalToken,
               constantTimeEquals(hex(SHA256.hash(data: Data(cached.utf8))), existing.hash) {
                writeLocalTokenFile(cached)
                return cached
            }
            revoke(id: existing.id)
        }
        let made = addDevice(name: "This Mac", caps: [.read, .send, .admin], local: true)
        cachedLocalToken = made.token
        writeLocalTokenFile(made.token)
        return made.token
    }

    private static var cachedLocalToken: String?

    static var tokenURL: URL {
        storeURL.deletingLastPathComponent().appendingPathComponent("remote-token")
    }

    /// Written where a script can find it, with the same mode as the store.
    private static func writeLocalTokenFile(_ token: String) {
        try? FileManager.default.createDirectory(at: tokenURL.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? Data(token.utf8).write(to: tokenURL, options: .atomic)
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: tokenURL.path)
    }

    // MARK: - Passwords

    /// PBKDF2-HMAC-SHA256. Slow on purpose — see the note at the top of the file for why this one
    /// is stretched and a device token is not.
    static let passwordIterations = 600_000

    static func setPassword(_ plain: String) {
        guard !plain.isEmpty else {
            lock.lock(); password = nil; lock.unlock(); save()
            audit("password.clear", [:])
            return
        }
        var salt = Data(count: 16)
        _ = salt.withUnsafeMutableBytes { SecRandomCopyBytes(kSecRandomDefault, 16, $0.baseAddress!) }
        let hash = derive(plain, salt: salt, iterations: passwordIterations)
        lock.lock(); password = (hash, salt, passwordIterations); lock.unlock()
        save()
        audit("password.set", [:])
    }

    /// A correct password mints a device token; it is not itself a credential the client keeps.
    /// So a password that leaks later cannot be replayed against a session already established,
    /// and revoking a device does not require changing the password.
    static func exchange(password plain: String, deviceName: String) -> String? {
        load()
        lock.lock()
        let stored = password
        lock.unlock()
        guard let stored else { return nil }
        let attempt = derive(plain, salt: stored.salt, iterations: stored.iterations)
        guard constantTimeEquals(hex(attempt), hex(stored.hash)) else {
            audit("password.fail", ["device": deviceName])
            return nil
        }
        return addDevice(name: deviceName).token
    }

    private static func derive(_ plain: String, salt: Data, iterations: Int) -> Data {
        var out = [UInt8](repeating: 0, count: 32)
        let pw = Array(plain.utf8)
        let s = Array(salt)
        _ = CCKeyDerivationPBKDF(CCPBKDFAlgorithm(kCCPBKDF2), pw, pw.count, s, s.count,
                                 CCPseudoRandomAlgorithm(kCCPRFHmacAlgSHA256),
                                 UInt32(iterations), &out, out.count)
        return Data(out)
    }

    // MARK: - The log

    static var auditURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/clawdline/remote-audit.jsonl")
    }

    /// Append-only, one JSON object per line, and the app can show it.
    ///
    /// This is a security control and not bookkeeping. If somebody does get in, the question you
    /// will have is *what did they do*, and that question has no answer at all unless it was
    /// written down while it was happening.
    static func audit(_ event: String, _ fields: [String: String]) {
        var row: [String: Any] = ["at": Int(Date().timeIntervalSince1970), "event": event]
        for (key, value) in fields { row[key] = value }
        guard let data = try? JSONSerialization.data(withJSONObject: row,
                                                     options: [.sortedKeys, .withoutEscapingSlashes]) else { return }
        let line = data + Data("\n".utf8)
        let fm = FileManager.default
        if !fm.fileExists(atPath: auditURL.path) {
            try? fm.createDirectory(at: auditURL.deletingLastPathComponent(), withIntermediateDirectories: true)
            fm.createFile(atPath: auditURL.path, contents: nil,
                          attributes: [.posixPermissions: 0o600])
        }
        guard let handle = try? FileHandle(forWritingTo: auditURL) else { return }
        defer { try? handle.close() }
        _ = try? handle.seekToEnd()
        try? handle.write(contentsOf: line)
    }

    /// The most recent entries, newest first, for the settings window.
    static func recentAudit(limit: Int = 200) -> [[String: Any]] {
        guard let text = try? String(contentsOf: auditURL, encoding: .utf8) else { return [] }
        return text.split(separator: "\n").suffix(limit).reversed().compactMap { line in
            guard let data = line.data(using: .utf8) else { return nil }
            return try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        }
    }

    // MARK: - Bits

    /// 256 bits from the system generator, URL-safe so it can live in a QR code and a fragment.
    static func newToken() -> String {
        var bytes = [UInt8](repeating: 0, count: 32)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return Data(bytes).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    static func hex<D: Sequence>(_ bytes: D) -> String where D.Element == UInt8 {
        bytes.map { String(format: "%02x", $0) }.joined()
    }
    static func hex(_ digest: SHA256Digest) -> String { hex(Array(digest)) }
}
