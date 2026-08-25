import CryptoKit
import Foundation

/// Making a phone in another room buzz when a session is waiting for an answer.
///
/// **This is the only thing in the app that leaves the machine on its own.** Everything else
/// either reads a terminal that is already here or answers a question somebody asked over a
/// tunnel they opened deliberately. A push message is different in kind: it is handed to Apple —
/// or Mozilla, or Google, whichever browser subscribed — and carried to a device that is not
/// this one, over infrastructure nobody here controls. Two things follow from that, and they are
/// the whole design.
///
/// **The courier does not get to read it.** RFC 8291 seals the payload with a key derived from
/// two secrets: an ECDH exchange with a public key the browser generated, and a 16-octet
/// `auth` secret it generated alongside. Both were handed to *this Mac* and to nothing else, so
/// the push service sees a P-256 point, a salt and a lump of AES-GCM ciphertext, and can say only
/// that a message went to a subscription. That is not a nicety — the alternative is posting the
/// name of every repository somebody works on to a third party, forever, as a side effect of
/// wanting their watch to tap them.
///
/// **Encryption does not help with what is on the screen.** The entire point of a push message is
/// that it lights up a locked phone lying face-up on a table in a room with other people in it.
/// So the encryption settles who may read it in transit and settles nothing about who reads it on
/// arrival. Session notifications therefore put the task in the title and the project and state
/// in the body, so the useful detail is visible without opening the app. See
/// ``send(title:body:url:completion:)``.
///
/// **The VAPID key pair is an identity, not a session, and losing it is silent.** A push service
/// binds a subscription to the application server key it was created with; mint a new pair and
/// every existing subscription starts failing — and it fails on the push service's side, with a
/// 403 that nobody is looking at, so what a person experiences is that their phone quietly
/// stopped buzzing. That is the same shape of bug as ``RemoteAuth/localToken()`` being re-minted
/// on every launch, which this codebase has already had to fix once. So the private key is
/// written once into `~/.config/clawdline/push.json` at mode `0600`, read back on the next
/// launch, and only ever replaced if what is on disk will not parse.
///
/// The store lives under ``RemoteAuth/directory`` and honours `CLAWDLINE_REMOTE_DIR` for the same
/// reason that does — a test run must not add subscriptions to somebody's real account.
enum WebPush {

    // MARK: - What a browser handed us

    /// One `PushSubscription`, as `pushManager.subscribe()` produced it.
    ///
    /// `p256dh` and `auth` are the browser's half of the encryption and are useless to anyone
    /// else: `p256dh` is a public key, and `auth` is a shared secret this Mac cannot use to read
    /// anything, only to write to that one device.
    struct Subscription: Equatable {
        let id: String
        /// Where the message is POSTed — e.g. `https://web.push.apple.com/…`.
        let endpoint: URL
        /// The browser's public key: an uncompressed P-256 point, 65 octets starting `0x04`.
        let p256dh: Data
        /// 16 octets, per RFC 8291 §3.2.
        let auth: Data
        /// Which paired device this belongs to, so revoking the device can take the
        /// subscription with it.
        let device: String
        let created: Date
    }

    /// The ways one message can be refused before it ever reaches the network. Each of these is a
    /// thing a push service answers with a 400 and an opaque body, so it is worth catching here
    /// where the reason is still in front of us.
    enum Failure: Error, CustomStringConvertible {
        case subscriberKey
        case saltLength(Int)
        case payloadTooLarge(Int)
        case noOrigin(URL)
        case badSubject(String)

        var description: String {
            switch self {
            case .subscriberKey:
                return "the subscription's p256dh is not an uncompressed P-256 point"
            case .saltLength(let count):
                return "the salt is \(count) octets and the aes128gcm header has room for 16"
            case .payloadTooLarge(let count):
                return "\(count) octets of payload, and the ceiling is \(maxPayload)"
            case .noOrigin(let url):
                return "no origin to put in aud — \(url.absoluteString)"
            case .badSubject(let subject):
                return "sub must be a mailto: or https: URI, and this is \(subject)"
            }
        }
    }

    // MARK: - The numbers the RFCs fix

    /// `salt(16) || rs(4) || idlen(1) || keyid(65)` — RFC 8188 §2.1, with the keyid length that
    /// RFC 8291 §4 pins to an uncompressed P-256 point. 86 octets, always, for this profile.
    static let headerLength = 16 + 4 + 1 + 65

    /// The `rs` field: an unsigned 32-bit integer, network byte order, describing the **ciphertext**
    /// size of a record including its 16-octet tag.
    ///
    /// 4096 because that is the body size RFC 8030 §7.2 forbids a push service from rejecting, so
    /// it is the one number every service is known to accept. Only the *final* record may be
    /// shorter than `rs`, and RFC 8291 §4 requires a push message to be exactly one record — which
    /// is therefore the final one, so a single short record under a 4096 declaration is well
    /// formed rather than a fudge.
    static let recordSize: UInt32 = 4096

    /// What is left for the caller once the header, the padding delimiter and the tag have been
    /// taken out of 4096: 3993 octets. Far more than a notification needs, and checked anyway,
    /// because the failure it prevents is a 413 from the push service for a message whose length
    /// nobody was watching.
    static let maxPayload = 4096 - headerLength - 1 - 16

    /// How long a message is worth delivering. RFC 8030 §5.2 makes this header mandatory.
    ///
    /// The claim being made is "a session is waiting for you", and a claim like that goes stale.
    /// Zero would mean deliver-now-or-never, which throws away the case this feature exists for —
    /// a phone asleep on a bad network for ninety seconds. A day would mean a phone that spent the
    /// afternoon in a drawer buzzing at midnight about a question answered at noon, which teaches
    /// people to turn notifications off. An hour is longer than a lift, a tunnel or a meeting, and
    /// shorter than the point at which the sentence stops being true.
    static let ttl = 3600

    /// RFC 8030 §5.3. `high` is the level the RFC illustrates with "incoming phone call or
    /// time-sensitive alert", and it is the one a device in a low power state will not defer.
    ///
    /// A blocked agent is precisely a time-sensitive alert: it is doing nothing at all until
    /// somebody answers, and a notification that arrives after the session has been abandoned has
    /// cost battery to say nothing. What makes `high` defensible rather than rude is **rarity** —
    /// this must be called when a session becomes `waiting`, and not for every state change. A
    /// `high` message per turn would be an app somebody uninstalls.
    static let urgency = "high"

    /// RFC 8292 §2 caps `exp` at 24 hours and encourages reusing a token inside its window so the
    /// push service can cache the signature check. Twelve hours is half the ceiling, which leaves
    /// room for a phone and a Mac whose clocks disagree by more than they should.
    static let tokenLifetime: TimeInterval = 12 * 3600

    /// The `sub` claim: a contact URI for whoever is operating the application server.
    ///
    /// There is no operator here — the application server is somebody's laptop — so the honest
    /// answer is the project a push service would end up reading if it wanted to know what had
    /// been sending it messages. It is not optional in practice: Apple refuses a token without a
    /// syntactically valid `sub` and says only `BadJwtToken`.
    static let subject = "https://github.com/sainteye/clawdline"

    // MARK: - The store

    private static let lock = NSLock()
    private static var loaded = false
    private static var stored: [String: Subscription] = [:]
    /// The VAPID private key as its 32-octet scalar. See the note at the top of the file for why
    /// this being the *same* 32 octets tomorrow is the whole point.
    private static var vapidSeed: Data?

    /// Deliberately ``RemoteAuth/directory`` rather than a second copy of the same logic: the two
    /// files are the same secret material at the same trust level, and a test that redirected one
    /// and not the other would write half of its state into somebody's real config.
    static var directory: URL { RemoteAuth.directory }

    static var storeURL: URL { directory.appendingPathComponent("push.json") }

    static func load(force: Bool = false) {
        lock.lock()
        if loaded, !force { lock.unlock(); return }
        loaded = true
        lock.unlock()

        guard let data = try? Data(contentsOf: storeURL),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        var found: [String: Subscription] = [:]
        for row in obj["subscriptions"] as? [[String: Any]] ?? [] {
            guard let id = row["id"] as? String,
                  let endpoint = (row["endpoint"] as? String).flatMap(URL.init(string:)),
                  let p256dh = (row["p256dh"] as? String).flatMap(base64urlDecoded),
                  let auth = (row["auth"] as? String).flatMap(base64urlDecoded),
                  // Checked on the way in as well as on the way out. A row of the wrong shape
                  // cannot be encrypted to, and finding that out at send time means a log line an
                  // hour after whatever wrote it has been forgotten.
                  p256dh.count == 65, auth.count == 16 else { continue }
            found[id] = Subscription(
                id: id, endpoint: endpoint, p256dh: p256dh, auth: auth,
                device: row["device"] as? String ?? "?",
                created: Date(timeIntervalSince1970: row["created"] as? Double ?? 0))
        }
        let seed = (obj["vapid_private"] as? String).flatMap(base64urlDecoded)
        lock.lock()
        stored = found
        vapidSeed = seed?.count == 32 ? seed : nil
        lock.unlock()
    }

    private static func save() {
        lock.lock()
        let rows = stored.values.sorted { $0.created < $1.created }.map { subscription in
            [
                "id": subscription.id,
                "endpoint": subscription.endpoint.absoluteString,
                "p256dh": base64url(subscription.p256dh),
                "auth": base64url(subscription.auth),
                "device": subscription.device,
                "created": subscription.created.timeIntervalSince1970,
            ] as [String: Any]
        }
        var obj: [String: Any] = ["version": 1, "subscriptions": rows]
        if let seed = vapidSeed { obj["vapid_private"] = base64url(seed) }
        lock.unlock()

        guard let data = try? JSONSerialization.data(withJSONObject: obj,
                                                     options: [.prettyPrinted, .sortedKeys,
                                                               .withoutEscapingSlashes]) else {
            Log.write("push: could not serialise the store, nothing written")
            return
        }
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try? data.write(to: storeURL, options: .atomic)
        // Every time, not only at creation: an atomic write replaces the file, and the replacement
        // does not inherit the mode of what it replaced. Same reason as `RemoteAuth.save`.
        try? FileManager.default.setAttributes([.posixPermissions: 0o600],
                                               ofItemAtPath: storeURL.path)
    }

    // MARK: - The VAPID identity

    /// The signing key, made once and kept.
    ///
    /// Only replaced when the stored scalar will not parse, and that case is logged in the loudest
    /// terms available, because it silently unsubscribes every device: a push service will not
    /// accept a message for a subscription signed by a key that is not the one the browser named
    /// when it subscribed.
    static func vapidKey() -> P256.Signing.PrivateKey {
        load()
        lock.lock()
        let seed = vapidSeed
        lock.unlock()
        if let seed, let key = try? P256.Signing.PrivateKey(rawRepresentation: seed) { return key }
        if seed != nil {
            Log.write("push: the stored VAPID key will not parse — minting a new one, which "
                + "invalidates every existing subscription; they will have to subscribe again")
        }
        let made = P256.Signing.PrivateKey()
        lock.lock()
        vapidSeed = made.rawRepresentation
        lock.unlock()
        save()
        Log.write("push: minted a VAPID key pair into \(storeURL.path)")
        return made
    }

    /// What the page passes to `subscribe({ applicationServerKey })` — base64url of the
    /// uncompressed public point, which is what `Uint8Array.fromBase64`-style helpers expect.
    static var publicKey: String { base64url(vapidKey().publicKey.x963Representation) }

    // MARK: - Who gets told

    static func add(_ subscription: Subscription) {
        load()
        lock.lock()
        // One row per endpoint. A browser that is asked to subscribe twice — a reload, a
        // re-granted permission, a second tab — hands back the endpoint it already had, and
        // keeping both rows would POST the same message twice and buzz the phone twice for one
        // question.
        for (key, existing) in stored where existing.endpoint == subscription.endpoint {
            stored.removeValue(forKey: key)
        }
        stored[subscription.id] = subscription
        lock.unlock()
        save()
        // In the audit log with everything else: a new subscription is a new destination for
        // notices about what somebody is working on, which is exactly the kind of change the
        // question "what did they do while they were in" needs an answer for.
        RemoteAuth.audit("push.subscribe", ["id": subscription.id, "device": subscription.device,
                                            "host": subscription.endpoint.host ?? "?"])
    }

    static func remove(id: String) {
        load()
        lock.lock()
        let gone = stored.removeValue(forKey: id)
        lock.unlock()
        guard let gone else { return }
        save()
        RemoteAuth.audit("push.unsubscribe", ["id": id, "device": gone.device])
    }

    static var subscriptions: [Subscription] {
        load()
        lock.lock(); defer { lock.unlock() }
        return stored.values.sorted { $0.created < $1.created }
    }

    /// A `PushSubscription.toJSON()` from the page, checked before it is believed.
    ///
    /// The checking is not tidiness. `endpoint` is a URL this Mac will be told to POST to, from
    /// inside the network the Mac is on, whenever a session changes state — so an unchecked one is
    /// a request-forgery primitive handed to whoever can reach the route. `https` only, and a real
    /// host. It is deliberately not an allowlist of push services: Apple, Mozilla, Google and
    /// anything self-hosted all have to work, and the property worth keeping is "not plaintext,
    /// not a scheme that reaches something local", not "a vendor we recognise".
    ///
    /// The key lengths are checked here too, because a compressed 33-octet point is a perfectly
    /// plausible thing for a client to send and the resulting failure is otherwise a CryptoKit
    /// error thrown an hour later with no mention of where the data came from.
    static func subscription(from json: [String: Any], device: String,
                             id: String = UUID().uuidString.lowercased()) -> Subscription? {
        guard let raw = json["endpoint"] as? String,
              let endpoint = URL(string: raw),
              endpoint.scheme?.lowercased() == "https",
              let host = endpoint.host, !host.isEmpty else { return nil }
        let keys = json["keys"] as? [String: Any] ?? [:]
        guard let p256dh = (keys["p256dh"] as? String).flatMap(base64urlDecoded),
              p256dh.count == 65, p256dh.first == 0x04,
              let auth = (keys["auth"] as? String).flatMap(base64urlDecoded),
              auth.count == 16 else { return nil }
        return Subscription(id: id, endpoint: endpoint, p256dh: p256dh, auth: auth,
                            device: device, created: Date())
    }

    // MARK: - One encrypted message

    /// The body of one push request: an RFC 8188 `aes128gcm` payload of exactly one record,
    /// keyed the RFC 8291 way.
    ///
    /// Pure, and takes both pieces of randomness as arguments. That is the only way this can be
    /// checked at all — the interesting assertion is not "it produced some bytes", it is "the
    /// subscriber's private key turns these bytes back into the plaintext", and that test needs
    /// both sides of a fixed key exchange. Same reason `HookBridge.merge` takes its clock.
    ///
    /// Both arguments must be **fresh per message**. RFC 8291 §3.1 has the application server
    /// generating a key pair and a salt when it sends, and discarding the pair afterwards; reusing
    /// either would reuse the AES-GCM key and nonce across two messages to the same subscriber,
    /// which is the one failure mode GCM does not survive. ``send(title:body:url:completion:)``
    /// makes new ones inside the loop, per subscriber, not per call.
    static func body(_ payload: Data, to subscription: Subscription,
                     ephemeral: P256.KeyAgreement.PrivateKey, salt: Data) throws -> Data {
        // 16 exactly, because the header field is 16 octets wide and a longer salt would silently
        // become a shorter salt plus a corrupted `rs`.
        guard salt.count == 16 else { throw Failure.saltLength(salt.count) }
        guard payload.count <= maxPayload else { throw Failure.payloadTooLarge(payload.count) }
        guard let subscriber = try? P256.KeyAgreement.PublicKey(
            x963Representation: subscription.p256dh) else { throw Failure.subscriberKey }

        let ours = ephemeral.publicKey.x963Representation      // 65, uncompressed
        let shared = try ephemeral.sharedSecretFromKeyAgreement(with: subscriber)

        // RFC 8291 §3.4. The ECDH secret alone is not enough — anyone who intercepted the
        // subscription's public key could do the same exchange — so the `auth` secret the browser
        // kept is folded in as the HKDF salt, and the two public keys go into the info string in
        // the order the RFC fixes: **user agent first, application server second**. Getting that
        // pair the wrong way round produces a key that is perfectly valid and that the browser
        // will never derive.
        var keyInfo = Data("WebPush: info".utf8)
        keyInfo.append(0)
        keyInfo.append(subscription.p256dh)
        keyInfo.append(ours)
        let ikm = shared.hkdfDerivedSymmetricKey(using: SHA256.self, salt: subscription.auth,
                                                 sharedInfo: keyInfo, outputByteCount: 32)

        // RFC 8188 §2.2, now with the per-message salt as the extract salt. The trailing NUL in
        // each info string is part of the string the RFC specifies, not a C artefact.
        let cek = HKDF<SHA256>.deriveKey(inputKeyMaterial: ikm, salt: salt,
                                         info: Data("Content-Encoding: aes128gcm\0".utf8),
                                         outputByteCount: 16)
        let baseNonce = HKDF<SHA256>.deriveKey(inputKeyMaterial: ikm, salt: salt,
                                               info: Data("Content-Encoding: nonce\0".utf8),
                                               outputByteCount: 12)
        // The nonce is the base nonce XOR the record sequence number, and this is record zero of
        // one — so the XOR is with 96 zero bits and the base nonce is used as it stands. Written
        // down rather than assumed: the day this grows a second record is the day the omission
        // becomes a nonce reuse.
        let nonce = try AES.GCM.Nonce(data: baseNonce.withUnsafeBytes { Data($0) })

        // RFC 8188 §2: the plaintext of a record ends in a padding delimiter, `0x01` for a record
        // with more to come and `0x02` for the last one. This is the last one and there is no
        // padding after it — padding exists to hide a message's length, and every message here is
        // the same handful of fields, so there is nothing to hide and a longer body to pay for.
        let sealed = try AES.GCM.seal(payload + Data([0x02]), using: cek, nonce: nonce)

        var out = salt
        out.append(contentsOf: withUnsafeBytes(of: recordSize.bigEndian, Array.init))
        out.append(UInt8(ours.count))
        out.append(ours)
        out.append(sealed.ciphertext)
        out.append(sealed.tag)
        return out
    }

    // MARK: - Proving who is sending it

    /// The `Authorization` header value: RFC 8292 VAPID, which is an ES256 JWT and a copy of the
    /// public key that verifies it.
    ///
    /// Pure, and takes the key as an argument so a test can pin one — the default reads the stored
    /// identity, so every real caller writes `authorization(for:expires:subject:)` and gets the
    /// right thing. The signature is randomised (ECDSA picks a nonce), so what a test can assert
    /// is the structure and that the signature verifies, not a fixed string.
    ///
    /// `expires` is not clamped here, because clamping would need a clock and this has none on
    /// purpose. RFC 8292 §2 says `exp` MUST NOT be more than 24 hours out; ``tokenLifetime`` is
    /// the value every caller in this file uses and it is half of that.
    static func authorization(for endpoint: URL, expires: Date, subject: String,
                              key: P256.Signing.PrivateKey = vapidKey()) throws -> String {
        // Checked rather than trusted: a push service answers a malformed `sub` with a 403 and a
        // three-word body, and this is the one thing in the token a person is likely to have typed.
        guard subject.hasPrefix("mailto:") || subject.hasPrefix("https://") else {
            throw Failure.badSubject(subject)
        }
        // `aud` is the **origin** of the endpoint and not the endpoint — RFC 8292 §2 is explicit,
        // and putting the full path there is the classic way to get a token that verifies
        // perfectly and is rejected anyway.
        guard let audience = origin(of: endpoint) else { throw Failure.noOrigin(endpoint) }

        let header: [String: Any] = ["typ": "JWT", "alg": "ES256"]
        let claims: [String: Any] = ["aud": audience,
                                     "exp": Int(expires.timeIntervalSince1970),
                                     "sub": subject]
        let signingInput = try segment(header) + "." + segment(claims)
        // `rawRepresentation` is r‖s, 64 octets, which is exactly the JWS form for ES256 — the DER
        // encoding that most ECDSA APIs hand back would be rejected.
        let signature = try key.signature(for: Data(signingInput.utf8))
        let jwt = signingInput + "." + base64url(signature.rawRepresentation)
        return "vapid t=\(jwt), k=\(base64url(key.publicKey.x963Representation))"
    }

    /// RFC 6454 §6.1 serialisation: scheme, host, and the port only when it is not the default
    /// one. A `https://web.push.apple.com:443` in `aud` is a different string to the one the push
    /// service compares against, and the failure is a flat 401.
    static func origin(of url: URL) -> String? {
        guard let scheme = url.scheme?.lowercased(), let host = url.host, !host.isEmpty else {
            return nil
        }
        var origin = scheme + "://" + host.lowercased()
        if let port = url.port, !(scheme == "https" && port == 443),
           !(scheme == "http" && port == 80) {
            origin += ":\(port)"
        }
        return origin
    }

    private static func segment(_ object: [String: Any]) throws -> String {
        // Sorted keys so the same claims always produce the same bytes, and no escaped slashes
        // because `aud` is a URL and `https:\/\/…` is legal JSON that reads like a mistake.
        let data = try JSONSerialization.data(withJSONObject: object,
                                              options: [.sortedKeys, .withoutEscapingSlashes])
        return base64url(data)
    }

    // MARK: - Sending

    /// Tell every subscribed browser something.
    ///
    /// **What goes in `title` and `body` is a decision about a lock screen, not about a network.**
    /// The transport is sealed end to end, so nothing here is a disclosure to Apple — and none of
    /// that matters for the thing that actually happens, which is text appearing on a phone lying
    /// on a table in a room with other people in it, quite possibly while its owner is in a
    /// meeting. Callers should therefore send only the useful session summary: its task title,
    /// project and state. Transcript text and prompt contents do not belong in a notification.
    ///
    /// `url` is a deep link the service worker opens on a tap. It carries a session id and no
    /// prose, for the same reason.
    ///
    /// Everything past the argument list happens off the main thread: this is a key derivation and
    /// one HTTPS round trip per subscriber, and it is called from a state-change observer that
    /// runs on the main thread.
    /// A short, legal `Topic` for a string that is neither.
    ///
    /// RFC 8030 §5.4 caps a topic at 32 characters from the URL-safe base64 alphabet, and a
    /// session id is a 36-character UUID with hyphens in it. So it is hashed down rather than
    /// truncated: two sessions whose ids happen to share a prefix would otherwise collapse into
    /// each other, and the whole point of a topic is that it is the *same* session.
    ///
    /// What it buys is the case this feature exists for. A phone in a pocket for ten minutes has
    /// notifications waiting at the push service, not on the phone — and with a topic, a second
    /// message about the same session **replaces** the first there rather than joining a queue.
    /// The `tag` in the payload does the same job for the ones that already arrived.
    static func topic(for key: String) -> String {
        let digest = SHA256.hash(data: Data(key.utf8))
        return String(base64url(Data(digest)).prefix(32))
    }

    /// Send to everything subscribed, or — with `device` — only to what that one device
    /// subscribed with.
    ///
    /// The filter exists for the test button. **Pressing "send me one" on a phone should buzz
    /// that phone**, not every device anybody has ever paired: a test whose blast radius is
    /// larger than the thing being tested teaches you to be careful with it, which is the
    /// opposite of what a test button is for.
    /// `icon` is a path on this origin — see ``RemoteIcon/projectPath(for:size:)``. It is a URL
    /// and not the picture itself for a reason worth stating: the picture would fit, just, and
    /// then the message would be a notification competing with its own decoration for the 3993
    /// octets a push service is obliged to accept. A 169-character path costs nothing and the
    /// phone fetches the mark once a year.
    static func send(title: String, body: String, url: String?, tag: String? = nil,
                     icon: String? = nil,
                     device: String? = nil, completion: (() -> Void)? = nil) {
        let targets = device.map { id in subscriptions.filter { $0.device == id } } ?? subscriptions
        guard !targets.isEmpty else {
            // Nothing to do, and in particular no VAPID key minted: a machine that has never had a
            // browser subscribe should not be growing key material because a session changed state.
            if let completion { DispatchQueue.main.async(execute: completion) }
            return
        }
        var payload: [String: Any] = ["title": title, "body": body,
                                      "at": Int(Date().timeIntervalSince1970)]
        if let url, !url.isEmpty { payload["url"] = url }
        if let tag, !tag.isEmpty { payload["tag"] = topic(for: tag) }
        if let icon, !icon.isEmpty { payload["icon"] = icon }

        DispatchQueue.global(qos: .utility).async {
            var payload = payload
            var made = try? JSONSerialization.data(
                withJSONObject: payload, options: [.withoutEscapingSlashes])
            // **The decoration goes before the message does.** A title is a task name and a
            // body is a project name, and both come from outside; between them they can crowd
            // out the octets a push service is obliged to accept. Losing the mark costs a
            // picture on one platform; losing the message costs the thing the file exists for.
            if let over = made, over.count > maxPayload, payload["icon"] != nil {
                Log.write("push: the mark did not fit in \(maxPayload) octets — sending without it")
                payload.removeValue(forKey: "icon")
                made = try? JSONSerialization.data(
                    withJSONObject: payload, options: [.withoutEscapingSlashes])
            }
            guard let plaintext = made else {
                Log.write("push: could not serialise the payload — nothing sent")
                if let completion { DispatchQueue.main.async(execute: completion) }
                return
            }
            let key = vapidKey()
            // One token for the whole fan-out. Endpoints on different hosts need different `aud`
            // claims, so it is re-signed per subscriber — but the expiry is taken once, so a slow
            // first request cannot leave the last one with a token that has already lapsed.
            let expires = Date().addingTimeInterval(tokenLifetime)
            let group = DispatchGroup()
            for subscription in targets {
                group.enter()
                post(plaintext, to: subscription, key: key, expires: expires,
                     topic: tag.map(topic(for:))) { group.leave() }
            }
            Log.write("push: \(title) → \(targets.count) subscription(s)")
            if let completion { group.notify(queue: .main, execute: completion) }
        }
    }

    private static func post(_ plaintext: Data, to subscription: Subscription,
                             key: P256.Signing.PrivateKey, expires: Date, topic: String? = nil,
                             done: @escaping () -> Void) {
        let sealed: Data
        let credential: String
        do {
            // Fresh per message and per subscriber, both of them. See ``body(_:to:ephemeral:salt:)``.
            var salt = Data(count: 16)
            _ = salt.withUnsafeMutableBytes {
                SecRandomCopyBytes(kSecRandomDefault, 16, $0.baseAddress!)
            }
            sealed = try body(plaintext, to: subscription,
                              ephemeral: P256.KeyAgreement.PrivateKey(), salt: salt)
            credential = try authorization(for: subscription.endpoint, expires: expires,
                                           subject: subject, key: key)
        } catch {
            Log.write("push: \(subscription.device) — \(error)")
            done()
            return
        }

        var request = URLRequest(url: subscription.endpoint)
        request.httpMethod = "POST"
        request.httpBody = sealed
        request.setValue(credential, forHTTPHeaderField: "Authorization")
        request.setValue("aes128gcm", forHTTPHeaderField: "Content-Encoding")
        request.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
        request.setValue(String(ttl), forHTTPHeaderField: "TTL")
        request.setValue(urgency, forHTTPHeaderField: "Urgency")
        // Replaces an undelivered message about the same session rather than joining it in the
        // queue — so a phone that was away for ten minutes finds one notification per session,
        // not one per transition. RFC 8030 §5.4.
        if let topic { request.setValue(topic, forHTTPHeaderField: "Topic") }
        // Shorter than the default sixty. A push service that has not answered in fifteen seconds
        // is not going to make this notification timely, and the request is holding a session.
        request.timeoutInterval = 15

        URLSession.shared.dataTask(with: request) { _, response, error in
            defer { done() }
            if let error {
                // The network being down is not the subscription being dead, so nothing is
                // removed here — a phone on a train would otherwise unsubscribe itself.
                Log.write("push: \(subscription.device) — \(error.localizedDescription)")
                return
            }
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            switch status {
            case 200..<300:
                break
            case 404, 410:
                // RFC 8030 §7.3: the subscription resource is gone. The browser was uninstalled,
                // the permission was revoked, or the service expired it — and none of those come
                // back. Retrying it forever means a request per state change, for the life of the
                // app, to a URL that will answer 410 every time.
                Log.write("push: \(subscription.device) is gone (\(status)) — dropping it")
                remove(id: subscription.id)
            default:
                // Everything else is left exactly where it is. A 429, a 502 or a 403 from a
                // service having a bad afternoon is not evidence about the subscription, and the
                // one thing worse than a missed notification is quietly unsubscribing somebody
                // because of it.
                Log.write("push: \(subscription.device) refused with \(status) — left alone")
            }
        }.resume()
    }

    // MARK: - Bits

    /// base64url, unpadded — RFC 7515 §2, which is what a JWS wants and what
    /// `applicationServerKey` on the page will be decoding.
    static func base64url(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    /// Lenient in both directions on purpose. A browser produces base64url without padding, a
    /// hand-written client produces plain base64 with it, and refusing one of those would be a
    /// bug report about a subscription that "just does not work" with no visible cause.
    static func base64urlDecoded(_ text: String) -> Data? {
        var padded = text.replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        while padded.count % 4 != 0 { padded += "=" }
        return Data(base64Encoded: padded)
    }
}
