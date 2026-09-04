import Foundation

/// Whether there is a newer Clawdline than the one running — and, when that cannot be answered,
/// which of the ways it could not be.
///
/// **Why this is here.** This app has no way of telling anybody it has moved on. It is installed
/// by downloading a zip, it does not run out of a store, and `tools/release.sh` publishes a
/// release without touching a single machine that already has one. So a person stays on whatever
/// they installed, forever — and that is not merely stale: what this app reads is another
/// program's screen —
/// a spinner, a dialog, a tab title, none of them promised to stay still (see ``Compat``) — so an
/// old build does not stop working loudly. It stops reading sessions correctly and says nothing,
/// which looks exactly like the sessions being idle.
///
/// **No dependency, and no self-installing.** The badge on the README says `dependencies-none`
/// and `Package.swift` names nothing; an update check is not the thing to spend that on, so this
/// is `URLSession` and `Foundation` and no more. And it only *says*. Replacing the running app
/// and restarting it is `build.sh`'s job, which closes and reopens the app somebody is using —
/// that is a decision with a cost, not a detail, and it belongs to the person, not to a
/// background timer.
///
/// **Three answers, not two.** "There is nothing newer" and "nobody could be asked" are different
/// facts, and the whole point of the type below is that they cannot be spelled the same way. A
/// check that answers `current` when it has in fact been rate-limited is worse than no check:
/// it is a silence that looks like an all-clear, which is the defect family this repository spent
/// 2026-09-04 pulling out of six other places.
// >>> clawdline update reading >>>
enum UpdateCheck {

    /// The release feed, unauthenticated — the same URL `install.sh` reads, for the same reason:
    /// it is the one endpoint that names the newest release without the asset's filename having
    /// to be known in advance.
    static let feed = "https://api.github.com/repos/sainteye/clawdline/releases/latest"

    /// Where a person is sent to act on the answer. The message that has no way to act on it is
    /// the message that gets read once and then ignored.
    static let releasesPage = "https://github.com/sainteye/clawdline/releases/latest"

    /// What came back from the network, before anything has been decided about it.
    ///
    /// The two arms are the two shapes a request can end in, and they are kept apart here so that
    /// ``outcome(for:installed:)`` below is the only place that decides anything: a transport
    /// answers what happened, it does not get a vote on what it means.
    enum Answer: Equatable {
        /// The server answered. Any status — 200, 403, 500 — with whatever body came with it.
        case http(status: Int, body: Data)
        /// Nothing answered: no network, DNS, a timeout, a refused connection. The string is the
        /// system's own description, kept for the log rather than for a person.
        case unreachable(String)
    }

    /// Why an answer could not be had. Every one of these is *not* "there is nothing newer".
    enum Failure: Equatable {
        /// GitHub's hourly ceiling for unauthenticated requests from this address — 60 an hour,
        /// shared by everyone behind it, which a shared office or a VPN can exhaust without
        /// anybody here doing anything. `install.sh` already tells this apart from other
        /// failures and says so out loud rather than exiting mute; this reads the body the same
        /// way, so the two agree about what a rate limit looks like.
        case rateLimited
        /// The server answered with something that is not 200 and does not say it is a limit.
        case http(Int)
        /// The request never reached anybody.
        case unreachable(String)
        /// 200, and no `tag_name` could be read out of it. **This is the case that must never
        /// quietly become "nothing newer"**: if GitHub's shape moves, or a proxy answers with a
        /// login page carrying a 200, the parse stops matching — and a scan that has stopped
        /// matching looks exactly like a scan that found nothing.
        case unreadable
        /// This process cannot say what version it is, so there is nothing to compare against.
        /// Outside an app bundle `Bundle.main` simply has no version key in it, and comparing
        /// "" against a real release reads as "you are on 0.0.0", which would announce an update
        /// to every test binary and every `swift` REPL that ever loaded this file.
        case unknownVersion
    }

    /// What one look at the feed came to.
    enum Outcome: Equatable {
        /// The feed names a release newer than the one running. `latest` has had its `v` taken
        /// off, so it is the same shape as ``Compat/compare(_:_:)`` expects on both sides.
        case newer(latest: String)
        /// The newest release is the one running, or older than it — which is the ordinary state
        /// of a machine that builds this from source.
        case current(latest: String)
        /// No answer. Which of the five is in the payload.
        case unavailable(Failure)

        /// Whether this is an answer about releases at all. The retry interval turns on it, and
        /// so does whether the reading is worth keeping for a day.
        var isAnswer: Bool {
            switch self {
            case .newer, .current: return true
            case .unavailable: return false
            }
        }

        /// The version to offer, or nothing. The menu asks exactly this, so that "there is an
        /// update" cannot be spelled by a caller reading the enum slightly wrong.
        var newerRelease: String? {
            if case .newer(let latest) = self { return latest }
            return nil
        }
    }

    /// One look at the feed, and when it was taken.
    ///
    /// `installed` is written down beside the outcome on purpose: a reading kept for a day
    /// outlives the app that took it, and an offer recorded against one build says nothing once
    /// the release it named has been installed over it.
    struct Reading: Equatable {
        let at: Date
        let installed: String
        let outcome: Outcome
    }

    // MARK: - Reading an answer

    /// The `tag_name` in a release document, with a leading `v` taken off — or nothing.
    ///
    /// `JSONSerialization` rather than a hand-rolled scan: the field is a JSON string and the
    /// standard library already knows how to read one, including the escapes a hand-rolled scan
    /// gets wrong. Nothing is guessed if it is missing: an object with no `tag_name`, a
    /// `tag_name` that is not a string, an array at the top level, and a body that is not JSON at
    /// all all answer the same nothing, and the caller turns that into ``Failure/unreadable``.
    ///
    /// A tag that does not start with a digit once its `v` is gone — `nightly`, `latest`, a
    /// release named after a person — is not a version this can compare, and saying so is better
    /// than comparing "nightly" as 0.
    static func tag(from body: Data) -> String? {
        guard let top = try? JSONSerialization.jsonObject(with: body),
              let object = top as? [String: Any],
              let raw = object["tag_name"] as? String else { return nil }
        var name = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if name.hasPrefix("v") || name.hasPrefix("V") { name.removeFirst() }
        guard let first = name.first, first.isNumber else { return nil }
        return name
    }

    /// Whether a body is GitHub saying this address has used up its hour.
    ///
    /// The same two words `install.sh` looks for, deliberately: one repository should not hold
    /// two different opinions about what a rate limit looks like. GitHub's message is
    /// "API rate limit exceeded for …"; the documentation link it sends alongside says
    /// "rate-limiting", with a hyphen, so this does not match on the explanation of the rule when
    /// the rule has not been hit.
    static func saysRateLimited(_ body: Data) -> Bool {
        String(decoding: body, as: UTF8.self).contains("rate limit")
    }

    /// The whole decision, in one place and with no IO in it.
    ///
    /// `installed` is what this app believes it is. Empty is refused rather than compared:
    /// see ``Failure/unknownVersion``.
    static func outcome(for answer: Answer, installed: String) -> Outcome {
        let running = installed.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !running.isEmpty else { return .unavailable(.unknownVersion) }
        switch answer {
        case .unreachable(let why):
            return .unavailable(.unreachable(why))
        case .http(let status, let body):
            guard status == 200 else {
                return .unavailable(saysRateLimited(body) ? .rateLimited : .http(status))
            }
            guard let latest = tag(from: body) else { return .unavailable(.unreadable) }
            return Compat.compare(latest, running) == .orderedDescending
                ? .newer(latest: latest)
                : .current(latest: latest)
        }
    }

    // MARK: - How often

    /// A day after an answer.
    ///
    /// The frequency is a choice and this is the reasoning: the ceiling is 60 unauthenticated
    /// requests an hour **per address**, shared with everything else behind it, and this app runs
    /// for days at a time on a machine somebody also uses to install things. Asking at every
    /// launch would be free on one Mac and rude on a shared address; asking once a week would
    /// mean a release lands and nobody hears about it until the following weekend. A day is the
    /// interval at which the message stays true and the cost stays at one request.
    static let afterAnswer: TimeInterval = 24 * 60 * 60

    /// An hour after a failure, because that is GitHub's own window.
    ///
    /// A rate-limited address is told to wait an hour, so waiting an hour is the one retry that
    /// is certain not to make the thing it is retrying worse. Every other failure gets the same
    /// hour rather than a shorter one: an app that has been running for a week with no network
    /// must not be asking every minute, and nothing here is urgent enough to be worth finding out
    /// fifty-nine minutes sooner.
    static let afterFailure: TimeInterval = 60 * 60

    /// When the next attempt becomes allowed.
    static func nextAttempt(after reading: Reading) -> Date {
        reading.at.addingTimeInterval(reading.outcome.isAnswer ? afterAnswer : afterFailure)
    }

    /// Whether to ask now.
    ///
    /// A reading whose clock is in the future is treated as due rather than as a lock: a file
    /// copied from another Mac, or a clock that was wrong and got fixed, would otherwise stop
    /// this app ever checking again — and it would do it silently, which is the failure this
    /// whole file is about.
    static func isDue(now: Date, last: Reading?) -> Bool {
        guard let last else { return true }
        if now < last.at { return true }
        return now >= nextAttempt(after: last)
    }

    // MARK: - Saying it out loud, where it can be looked up

    /// The one line the log gets, every time a check finishes.
    ///
    /// The menu stays quiet unless there is something to act on. This is the other half of that
    /// bargain: quiet is only acceptable if the thing that was quiet can be looked up afterwards,
    /// and a failure that leaves no trace is a check that cannot be told from a check that never
    /// ran. Every arm names itself, so "no update" and "could not ask" are two different
    /// sentences in `~/Library/Logs/Clawdline.log`.
    static func logLine(_ reading: Reading) -> String {
        let head = "update check: "
        switch reading.outcome {
        case .newer(let latest):
            return head + "\(latest) is out, this is \(reading.installed)"
        case .current(let latest):
            return head + "\(reading.installed) is the newest release (\(latest))"
        case .unavailable(let why):
            return head + "could not tell — " + describe(why)
                 + "; next attempt after \(Int(afterFailure / 60)) minutes"
        }
    }

    /// Why there is no answer, in the words somebody debugging would use.
    static func describe(_ failure: Failure) -> String {
        switch failure {
        case .rateLimited:
            return "GitHub's hourly limit for unauthenticated requests from this address"
        case .http(let status):
            return "GitHub answered \(status)"
        case .unreachable(let why):
            return "nothing answered (\(why))"
        case .unreadable:
            return "GitHub answered 200 with no tag_name in it"
        case .unknownVersion:
            return "this process cannot say which version it is"
        }
    }

    // MARK: - Keeping it between launches

    /// The keys in the stored reading. Named once, because a key spelled twice is a key that gets
    /// spelled differently.
    enum Key {
        static let at = "at"
        static let installed = "installed"
        static let state = "state"
        static let latest = "latest"
        static let reason = "reason"
        static let detail = "detail"
    }

    /// The three states, as they are written on disk.
    enum State {
        static let newer = "newer"
        static let current = "current"
        static let unavailable = "unavailable"
    }

    /// The five ways there is no answer, as they are written on disk.
    enum Reason {
        static let rateLimited = "rate_limited"
        static let http = "http"
        static let unreachable = "unreachable"
        static let unreadable = "unreadable"
        static let unknownVersion = "unknown_version"
    }

    private static let clock: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    /// The reading as JSON, in the words above.
    ///
    /// Written by hand rather than by `Codable`'s synthesis for an enum with associated values,
    /// which produces a nested shape nobody can read: this file is the place a person looks to
    /// find out why they were never told about a release, so `"reason": "rate_limited"` has to be
    /// legible to somebody who has never seen this source.
    static func encode(_ reading: Reading) -> String {
        var fields: [String: Any] = [
            Key.at: clock.string(from: reading.at),
            Key.installed: reading.installed,
        ]
        switch reading.outcome {
        case .newer(let latest):
            fields[Key.state] = State.newer
            fields[Key.latest] = latest
        case .current(let latest):
            fields[Key.state] = State.current
            fields[Key.latest] = latest
        case .unavailable(let why):
            fields[Key.state] = State.unavailable
            switch why {
            case .rateLimited: fields[Key.reason] = Reason.rateLimited
            case .http(let status):
                fields[Key.reason] = Reason.http
                fields[Key.detail] = String(status)
            case .unreachable(let detail):
                fields[Key.reason] = Reason.unreachable
                fields[Key.detail] = detail
            case .unreadable: fields[Key.reason] = Reason.unreadable
            case .unknownVersion: fields[Key.reason] = Reason.unknownVersion
            }
        }
        guard let data = try? JSONSerialization.data(withJSONObject: fields,
                                                     options: [.sortedKeys, .prettyPrinted])
        else { return "" }
        return String(decoding: data, as: UTF8.self)
    }

    /// A stored reading, or nothing.
    ///
    /// Strict on the way in. A file this cannot read in full is discarded and the check is due
    /// again, which costs one request; guessing at half of it would cost the thing the file is
    /// for. A `state` this version does not know — written by a later one — is not a version of
    /// `current` and is refused too.
    static func decode(_ text: String) -> Reading? {
        guard let data = text.data(using: .utf8),
              let top = try? JSONSerialization.jsonObject(with: data),
              let fields = top as? [String: Any],
              let stamp = fields[Key.at] as? String,
              let at = clock.date(from: stamp),
              let installed = fields[Key.installed] as? String,
              let state = fields[Key.state] as? String else { return nil }
        let latest = fields[Key.latest] as? String
        let detail = fields[Key.detail] as? String
        switch state {
        case State.newer:
            guard let latest else { return nil }
            return Reading(at: at, installed: installed, outcome: .newer(latest: latest))
        case State.current:
            guard let latest else { return nil }
            return Reading(at: at, installed: installed, outcome: .current(latest: latest))
        case State.unavailable:
            guard let reason = fields[Key.reason] as? String,
                  let why = failure(reason: reason, detail: detail) else { return nil }
            return Reading(at: at, installed: installed, outcome: .unavailable(why))
        default:
            return nil
        }
    }

    static func failure(reason: String, detail: String?) -> Failure? {
        switch reason {
        case Reason.rateLimited: return .rateLimited
        case Reason.http:
            guard let detail, let status = Int(detail) else { return nil }
            return .http(status)
        case Reason.unreachable: return .unreachable(detail ?? "")
        case Reason.unreadable: return .unreadable
        case Reason.unknownVersion: return .unknownVersion
        default: return nil
        }
    }
}
// <<< clawdline update reading <<<

// MARK: - Where the answer comes from

/// One look at the release feed. A protocol so the suite never touches the network: a test that
/// needs GitHub to be up is a test that goes red on an aeroplane, and one that needs GitHub to be
/// *down* cannot be written at all.
protocol UpdateFeed {
    func latestRelease(_ completion: @escaping (UpdateCheck.Answer) -> Void)
}

/// The real one. `URLSession` and nothing else.
struct GitHubUpdateFeed: UpdateFeed {
    let session: URLSession
    let url: URL

    init(session: URLSession = .shared,
         url: URL = URL(string: UpdateCheck.feed)!) {
        self.session = session
        self.url = url
    }

    func latestRelease(_ completion: @escaping (UpdateCheck.Answer) -> Void) {
        var request = URLRequest(url: url)
        // GitHub's own advice for an unauthenticated caller, and it costs nothing to be nameable
        // in their logs when this app is the thing making the requests.
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("clawdline", forHTTPHeaderField: "User-Agent")
        // Short on purpose. Nothing on screen is waiting for this, and a request that hangs for
        // the default 60 seconds is one that is still holding a connection open long after the
        // menu it would have decorated was closed again.
        request.timeoutInterval = 15
        session.dataTask(with: request) { data, response, error in
            if let error {
                completion(.unreachable(error.localizedDescription))
                return
            }
            guard let http = response as? HTTPURLResponse else {
                completion(.unreachable("no HTTP response"))
                return
            }
            completion(.http(status: http.statusCode, body: data ?? Data()))
        }.resume()
    }
}

/// Where the last reading is kept between launches.
///
/// A file rather than `UserDefaults` because it is meant to be *read by a person*: the menu is
/// silent when there is no update and silent again when the check could not be made, and this is
/// the difference between those two, written down where somebody can open it.
struct UpdateReadingStore {
    let url: URL

    static var appSupport: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/Clawdline", isDirectory: true)
            .appendingPathComponent("update-check.json")
    }

    func load() -> UpdateCheck.Reading? {
        guard let text = try? String(contentsOf: url, encoding: .utf8) else { return nil }
        return UpdateCheck.decode(text)
    }

    func save(_ reading: UpdateCheck.Reading) {
        let text = UpdateCheck.encode(reading)
        guard !text.isEmpty else { return }
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(),
                                                 withIntermediateDirectories: true)
        try? text.write(to: url, atomically: true, encoding: .utf8)
    }
}

/// The one that runs. Holds the last reading, decides when to ask again, and never asks twice at
/// once.
final class UpdateChecker {
    static let shared = UpdateChecker(
        feed: GitHubUpdateFeed(),
        store: UpdateReadingStore(url: UpdateReadingStore.appSupport),
        installed: ClawdlineClientIdentity.version(
            bundleVersion: ClawdlineClientIdentity.bundleShortVersion()))

    private let feed: UpdateFeed
    private let store: UpdateReadingStore
    private let installed: String
    private let lock = NSLock()
    private var last: UpdateCheck.Reading?
    private var asking = false
    private var timer: Timer?

    init(feed: UpdateFeed, store: UpdateReadingStore, installed: String) {
        self.feed = feed
        self.store = store
        self.installed = installed
        self.last = store.load()
    }

    /// What the menu reads. Nothing at all unless there is a release to move to, and nothing
    /// either when the stored reading was taken against a different version of this app — which
    /// is what a `newer` reading becomes the moment somebody installs the release it names.
    var newerRelease: String? {
        lock.lock()
        defer { lock.unlock() }
        guard let last, last.installed == installed else { return nil }
        return last.outcome.newerRelease
    }

    /// What this app believes it is, which the menu row names beside the release it offers.
    var installedVersion: String { installed }

    /// The last reading, whatever it says. For the places that want the third state.
    var reading: UpdateCheck.Reading? {
        lock.lock()
        defer { lock.unlock() }
        return last
    }

    /// Ask now if enough time has passed, and answer on the main queue when there is something
    /// new to know.
    func refreshIfDue(now: Date = Date(), then: @escaping () -> Void = {}) {
        lock.lock()
        let due = !asking && UpdateCheck.isDue(now: now, last: last)
        if due { asking = true }
        lock.unlock()
        guard due else { then(); return }

        feed.latestRelease { [weak self] answer in
            guard let self else { return }
            let outcome = UpdateCheck.outcome(for: answer, installed: self.installed)
            let reading = UpdateCheck.Reading(at: Date(), installed: self.installed,
                                              outcome: outcome)
            self.lock.lock()
            self.last = reading
            self.asking = false
            self.lock.unlock()
            self.store.save(reading)
            Log.write(UpdateCheck.logLine(reading))
            then()
        }
    }

    /// Check at launch, and once an hour after that.
    ///
    /// The hourly tick is not the frequency — ``UpdateCheck/isDue(now:last:)`` is, and it says a
    /// day. The tick exists because this app is a menu bar item that runs for weeks: a check that
    /// only happens at launch never happens at all on a Mac that is never restarted, which is
    /// most of them.
    func start() {
        refreshIfDue()
        let tick = Timer.scheduledTimer(withTimeInterval: UpdateCheck.afterFailure,
                                        repeats: true) { [weak self] _ in
            self?.refreshIfDue()
        }
        tick.tolerance = 300
        timer = tick
    }
}
